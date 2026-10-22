package sshbundle

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func sourceFixture(t *testing.T, config string) (string, string) {
	t.Helper()
	home := t.TempDir()
	dir := filepath.Join(home, ".ssh")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string]string{"key": "TEST PRIVATE KEY\n", "key.pub": "TEST PUBLIC KEY\n", "known_hosts": "test.example ssh-ed25519 TEST\n"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(dir, "config")
	if err := os.WriteFile(path, []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	return path, home
}

func option(h Host, name string) []string {
	var result []string
	for _, o := range h.Options {
		if o.Name == name {
			result = append(result, o.Args...)
		}
	}
	return result
}

func TestImportStaticSemanticsAndFiles(t *testing.T) {
	config := `User testuser
IdentityFile ~/.ssh/key
Host app app-alt
  HostName app.example
  Port 2222
  ProxyJump jump
  UserKnownHostsFile ~/.ssh/known_hosts /dev/null
Host *
  User ignored-user
  Port 22
  ServerAliveInterval 30
Host jump
  HostName jump.example
  ProxyCommand ssh -W %h:%p jump-proxy
`
	path, home := sourceFixture(t, config)
	cert := []byte("TEST CERTIFICATE\n")
	if err := os.WriteFile(filepath.Join(home, ".ssh/key-cert.pub"), cert, 0600); err != nil {
		t.Fatal(err)
	}
	b, err := Import(path, home, []string{"app", "app-alt"})
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Hosts) != 3 || len(b.Files) != 4 {
		t.Fatalf("host/file counts = %d/%d", len(b.Hosts), len(b.Files))
	}
	app := b.Hosts[0]
	if got := option(app, "user"); len(got) != 1 || got[0] != "testuser" {
		t.Fatal("first global user lost")
	}
	if got := option(app, "port"); len(got) != 1 || got[0] != "2222" {
		t.Fatal("first matching port lost")
	}
	if got := option(app, "certificatefile"); len(got) != 1 {
		t.Fatal("certificate companion absent")
	}
	if got := option(b.Hosts[1], "proxycommand"); len(got) != 1 || got[0] != "ssh -W %h:%p jump-proxy" {
		t.Fatal("raw proxy command changed")
	}
	if got := option(app, "userknownhostsfile"); len(got) != 2 || got[1] != "/dev/null" {
		t.Fatal("known hosts null sentinel lost")
	}
	encoded, err := Encode(b)
	if err != nil {
		t.Fatal(err)
	}
	copy, err := Decode(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(copy.Files[0].Data, []byte("TEST PRIVATE KEY\n")) {
		t.Fatal("private key bytes changed")
	}
}

func TestImportFirstProxyAndAccumulatedIdentity(t *testing.T) {
	path, home := sourceFixture(t, `Host app
HostName host
User user
IdentityFile ~/.ssh/key
ProxyCommand printf "literal %h %p" # preserved tail
ProxyJump missing
Host *
IdentityFile ~/.ssh/key
Port 22
`)
	b, err := Import(path, home, []string{"app"})
	if err != nil {
		t.Fatal(err)
	}
	if len(option(b.Hosts[0], "identityfile")) != 1 || len(option(b.Hosts[0], "proxyjump")) != 0 {
		t.Fatal("first proxy/repeated key semantics changed")
	}
	if got := option(b.Hosts[0], "proxycommand")[0]; got != `printf "literal %h %p" # preserved tail` {
		t.Fatal("proxy command tail changed")
	}
}

func TestImportProxyPrecedenceMatchesOpenSSH(t *testing.T) {
	ssh, err := exec.LookPath("ssh")
	if err != nil {
		t.Skip("OpenSSH unavailable")
	}
	for name, proxy := range map[string]string{
		"jump-none-blocks-jump":       "ProxyJump none\nProxyJump jump\n",
		"command-none-blocks-jump":    "ProxyCommand none\nProxyJump jump\n",
		"command-none-blocks-command": "ProxyCommand none\nProxyCommand printf fixture\n",
		"active-jump-blocks-command":  "ProxyJump jump\nProxyCommand printf fixture\n",
		"active-command-blocks-jump":  "ProxyCommand printf fixture\nProxyJump jump\n",
	} {
		t.Run(name, func(t *testing.T) {
			path, home := sourceFixture(t, "User test\nIdentityFile ~/.ssh/key\nHost app\nHostName example.test\n"+proxy+"Host jump\nHostName jump.test\n")
			b, err := Import(path, home, []string{"app"})
			if err != nil {
				t.Fatal(err)
			}
			got := map[string]string{}
			for _, o := range b.Hosts[0].Options {
				if (o.Name == "proxyjump" || o.Name == "proxycommand") && o.Args[0] != "none" {
					got[o.Name] = o.Args[0]
				}
			}
			output, err := exec.Command(ssh, "-G", "-F", path, "app").Output()
			if err != nil {
				t.Fatal("OpenSSH fixture configuration rejected")
			}
			want := map[string]string{}
			for _, line := range strings.Split(string(output), "\n") {
				key, value, ok := strings.Cut(line, " ")
				if ok && (key == "proxyjump" || key == "proxycommand") {
					want[key] = value
				}
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("proxy precedence differs from OpenSSH: got %v, want %v", got, want)
			}
		})
	}
}

func TestImportRejectsVersionDependentProxyPrecedence(t *testing.T) {
	base := "User test\nIdentityFile ~/.ssh/key\nHost app\nHostName example.test\n"
	for name, config := range map[string]string{
		"command":        base + "ProxyJump none\nProxyCommand printf fixture\n",
		"command-none":   base + "ProxyJump none\nProxyCommand none\n",
		"wildcard":       base + "ProxyJump none\nHost *\nProxyCommand printf fixture\n",
		"repeated-block": base + "ProxyJump none\nHost app\nProxyCommand printf fixture\n",
		"global":         "ProxyJump none\n" + base + "ProxyCommand printf fixture\n",
	} {
		t.Run(name, func(t *testing.T) {
			path, home := sourceFixture(t, config)
			if _, err := Import(path, home, []string{"app"}); err == nil || !strings.Contains(err.Error(), "OpenSSH") {
				t.Fatal("version-dependent proxy configuration was not explicitly rejected")
			}
		})
	}
	path, home := sourceFixture(t, base+"Host other\nProxyJump none\nProxyCommand printf fixture\n")
	if _, err := Import(path, home, []string{"app"}); err != nil {
		t.Fatal("unselected proxy configuration affected import")
	}
}

func TestImportRefusesUnsupportedOrIncompleteConfiguration(t *testing.T) {
	base := "Host app\nHostName host\nUser user\nIdentityFile ~/.ssh/key\n"
	cases := map[string]string{
		"include": base + "Include other.conf\n", "match": base + "Match exec touch\n", "wildcard": "Host app*\nHostName host\n",
		"mixed-star": "Host app *\n", "unsupported": base + "LocalCommand ignored\n", "path-token": strings.Replace(base, "~/.ssh/key", "%d/key", 1),
		"path-variable": strings.Replace(base, "~/.ssh/key", "${HOME}/key", 1), "relative-path": strings.Replace(base, "~/.ssh/key", "key", 1),
		"other-user": strings.Replace(base, "~/.ssh/key", "~other/key", 1), "missing-user": strings.Replace(base, "User user\n", "", 1),
		"missing-hostname": strings.Replace(base, "HostName host\n", "", 1), "identity-none": strings.Replace(base, "~/.ssh/key", "none", 1),
		"missing-jump": base + "ProxyJump absent\n", "complex-jump": base + "ProxyJump user@jump:22\n", "bad-port": base + "Port 70000\n",
		"cycle": base + "ProxyJump jump\nHost jump\nHostName j\nUser u\nIdentityFile ~/.ssh/key\nProxyJump app\n",
	}
	for name, config := range cases {
		t.Run(name, func(t *testing.T) {
			path, home := sourceFixture(t, config)
			if _, err := Import(path, home, []string{"app"}); err == nil {
				t.Fatal("invalid configuration accepted")
			}
		})
	}
}

func TestImportSafeFilePermissionsAndLinks(t *testing.T) {
	base := "Host app\nHostName host\nUser user\nIdentityFile ~/.ssh/key\n"
	for _, kind := range []string{"private-world-readable", "private-group-write", "symlink", "hardlink", "config-writable", "empty-key"} {
		t.Run(kind, func(t *testing.T) {
			path, home := sourceFixture(t, base)
			key := filepath.Join(home, ".ssh/key")
			switch kind {
			case "private-world-readable":
				if err := os.Chmod(key, 0644); err != nil {
					t.Fatal(err)
				}
			case "private-group-write":
				if err := os.Chmod(key, 0620); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				if err := os.Rename(key, key+".real"); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(key+".real", key); err != nil {
					t.Fatal(err)
				}
			case "hardlink":
				if err := os.Link(key, key+".link"); err != nil {
					t.Fatal(err)
				}
			case "config-writable":
				if err := os.Chmod(path, 0620); err != nil {
					t.Fatal(err)
				}
			case "empty-key":
				if err := os.WriteFile(key, nil, 0600); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := Import(path, home, []string{"app"}); err == nil {
				t.Fatal("unsafe source accepted")
			}
		})
	}
}

func validBundle(t *testing.T) string {
	t.Helper()
	path, home := sourceFixture(t, "Host app\nHostName host\nUser user\nIdentityFile ~/.ssh/key\n")
	b, err := Import(path, home, []string{"app"})
	if err != nil {
		t.Fatal(err)
	}
	value, err := Encode(b)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func TestDecodeRejectsMalformedOrHostileBundle(t *testing.T) {
	value := validBundle(t)
	for name, bad := range map[string]string{
		"unknown-field":   strings.Replace(value, `"version":1`, `"version":1,"unknown":true`, 1),
		"duplicate-field": strings.Replace(value, `"version":1`, `"version":1,"version":1`, 1),
		"trailing-value":  value + "{}", "null": "null", "invalid-version": strings.Replace(value, `"version":1`, `"version":2`, 1),
		"injected-host":   strings.Replace(value, `"name":"app"`, `"name":"app\nHost evil"`, 1),
		"missing-ref":     strings.Replace(value, `"args":["f0001"]`, `"args":["f9999"]`, 1),
		"injected-option": strings.Replace(value, `"name":"hostname"`, `"name":"localcommand"`, 1),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Decode(bad); err == nil {
				t.Fatal("invalid bundle accepted")
			}
		})
	}
	b, err := Decode(value)
	if err != nil {
		t.Fatal(err)
	}
	b.Hosts[0].Options[0].Args[0] = "host\nLocalCommand unwanted"
	if err := Validate(b); err == nil {
		t.Fatal("newline scalar accepted")
	}
	if err := Validate(nil); err == nil {
		t.Fatal("nil bundle accepted")
	}
	b, err = Decode(value)
	if err != nil {
		t.Fatal(err)
	}
	b.Files[0].Kind = "public"
	if err := Validate(b); err == nil {
		t.Fatal("wrong file ref kind accepted")
	}
	if data, err := json.Marshal(b); err != nil || len(data) == 0 {
		t.Fatal("fixture marshaling failed")
	}
}

func TestImportDoesNotExecuteProxyCommand(t *testing.T) {
	path, home := sourceFixture(t, "Host app\nHostName host\nUser user\nIdentityFile ~/.ssh/key\n")
	marker := filepath.Join(home, "EXECUTED")
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.WriteString("ProxyCommand touch " + marker + "\n")
	f.Close()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Import(path, home, []string{"app"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("proxy command executed")
	}
}

func TestStaticWordsEscaping(t *testing.T) {
	for input, expected := range map[string]string{
		`foo#bar`: `foo#bar`, `foo # ignored`: `foo`, `"foo#bar"`: `foo#bar`,
		`foo\bar`: `foo\bar`, `foo\#bar`: `foo\#bar`, `foo\ bar`: `foo bar`,
		`"foo\bar"`: `foo\bar`, `"foo\ bar"`: `foo\ bar`, `"foo\"bar"`: `foo"bar`,
	} {
		got, err := words(input)
		if err != nil || len(got) != 1 || got[0] != expected {
			t.Fatalf("static token parsing changed for %q", input)
		}
	}
}
