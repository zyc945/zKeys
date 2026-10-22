package sshbundle

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func secureHome(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	return dir
}

func exportFixture() *Bundle {
	return &Bundle{Type: "zkeys-ssh", Version: 1, Hosts: []Host{{Name: "test-server", Options: []Option{
		{Name: "hostname", Args: []string{"127.0.0.1"}},
		{Name: "user", Args: []string{"tester"}},
		{Name: "port", Args: []string{"2222"}},
		{Name: "identityfile", Args: []string{"f0001"}},
		{Name: "userknownhostsfile", Args: []string{"f0003"}},
	}}}, Files: []File{
		{ID: "f0001", Kind: "private", Data: []byte("encrypted key bytes\nunchanged\x00")},
		{ID: "f0002", Kind: "public", Data: []byte("ssh-ed25519 test\n")},
		{ID: "f0003", Kind: "known_hosts", Data: []byte("[127.0.0.1]:2222 ssh-ed25519 test\n")},
	}}
}

func checkMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	st, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != want {
		t.Errorf("mode %s = %o, want %o", path, st.Mode().Perm(), want)
	}
}

func TestExportPermissionsAndBytes(t *testing.T) {
	home := secureHome(t)
	b := exportFixture()
	config, err := Export(home, "work", b, true, false)
	if err != nil {
		t.Fatal(err)
	}
	if config != filepath.Join(home, ".ssh", "zkeys", "work", "config") {
		t.Fatal(config)
	}
	for _, path := range []string{filepath.Join(home, ".ssh"), filepath.Dir(filepath.Dir(config)), filepath.Dir(config), filepath.Join(filepath.Dir(config), "files")} {
		checkMode(t, path, 0700)
	}
	checkMode(t, config, 0600)
	for _, f := range b.Files {
		path := filepath.Join(filepath.Dir(config), "files", f.ID)
		checkMode(t, path, 0600)
		got, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(got, f.Data) {
			t.Fatalf("file %s not preserved", f.ID)
		}
	}
	if _, err := os.Lstat(filepath.Join(home, ".ssh", "config")); !os.IsNotExist(err) {
		t.Fatal("main config was touched")
	}
	if _, err := Export(home, "work", b, true, false); err == nil {
		t.Fatal("existing export overwritten")
	}
	entries, err := os.ReadDir(filepath.Dir(filepath.Dir(config)))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".export-") {
			t.Fatal("staging directory leaked")
		}
	}
}

func TestExportPrivateFlagAndValidationNoFiles(t *testing.T) {
	for _, name := range []string{"work", "../escape"} {
		home := secureHome(t)
		if _, err := Export(home, name, exportFixture(), false, false); err == nil {
			t.Fatal("unsafe export accepted")
		}
		entries, _ := os.ReadDir(home)
		if len(entries) != 0 {
			t.Fatal("rejected export wrote files")
		}
	}
	home := secureHome(t)
	b := exportFixture()
	b.Files[0].ID = "../escape"
	if _, err := Export(home, "work", b, true, false); err == nil {
		t.Fatal("traversal accepted")
	}
	entries, _ := os.ReadDir(home)
	if len(entries) != 0 {
		t.Fatal("invalid bundle wrote files")
	}
}

func TestExportMainInstall(t *testing.T) {
	for _, empty := range []bool{false, true} {
		t.Run(map[bool]string{false: "absent", true: "empty"}[empty], func(t *testing.T) {
			home := secureHome(t)
			ssh := filepath.Join(home, ".ssh")
			main := filepath.Join(ssh, "config")
			if err := os.Mkdir(ssh, 0700); err != nil {
				t.Fatal(err)
			}
			if empty {
				if err := os.WriteFile(main, nil, 0644); err != nil {
					t.Fatal(err)
				}
			}
			config, err := Export(home, "work", exportFixture(), true, true)
			if err != nil {
				t.Fatal(err)
			}
			got, err := os.ReadFile(main)
			if err != nil || !strings.Contains(string(got), config) || !strings.HasPrefix(string(got), "Include ") {
				t.Fatal("missing Include")
			}
			checkMode(t, main, 0600)
			backups, err := filepath.Glob(filepath.Join(ssh, ".config.zkeys-backup-*"))
			if err != nil {
				t.Fatal(err)
			}
			want := 0
			if empty {
				want = 1
			}
			if len(backups) != want {
				t.Fatalf("backup count=%d, want %d", len(backups), want)
			}
			for _, p := range backups {
				checkMode(t, p, 0600)
				data, _ := os.ReadFile(p)
				if len(data) != 0 {
					t.Fatal("backup changed")
				}
			}
		})
	}
}

func TestExportMainConflictsBeforePublication(t *testing.T) {
	for _, kind := range []string{"nonempty", "symlink", "hardlink", "directory", "writable"} {
		t.Run(kind, func(t *testing.T) {
			home := secureHome(t)
			ssh := filepath.Join(home, ".ssh")
			main := filepath.Join(ssh, "config")
			if err := os.Mkdir(ssh, 0700); err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "nonempty":
				os.WriteFile(main, []byte("Host original\n"), 0600)
			case "symlink":
				os.Symlink(filepath.Join(home, "elsewhere"), main)
			case "hardlink":
				other := filepath.Join(home, "other")
				os.WriteFile(other, nil, 0600)
				os.Link(other, main)
			case "directory":
				os.Mkdir(main, 0700)
			case "writable":
				os.WriteFile(main, nil, 0600)
				os.Chmod(main, 0666)
			}
			if _, err := Export(home, "work", exportFixture(), true, true); err == nil {
				t.Fatal("conflicting main accepted")
			}
			if _, err := os.Lstat(filepath.Join(ssh, "zkeys", "work")); !os.IsNotExist(err) {
				t.Fatal("partial published export")
			}
			if kind == "nonempty" {
				data, _ := os.ReadFile(main)
				if string(data) != "Host original\n" {
					t.Fatal("main changed")
				}
			}
		})
	}
}

func TestExportDirectoryAndLockConflicts(t *testing.T) {
	for _, kind := range []string{"ssh-symlink", "root-symlink", "ssh-mode", "target-symlink", "lock-symlink", "lock-hardlink"} {
		t.Run(kind, func(t *testing.T) {
			home := secureHome(t)
			outside := secureHome(t)
			ssh := filepath.Join(home, ".ssh")
			root := filepath.Join(ssh, "zkeys")
			if kind == "ssh-symlink" {
				os.Symlink(outside, ssh)
			} else {
				os.Mkdir(ssh, 0700)
				if kind == "root-symlink" {
					os.Symlink(outside, root)
				} else {
					os.Mkdir(root, 0700)
					switch kind {
					case "ssh-mode":
						os.Chmod(ssh, 0755)
					case "target-symlink":
						os.Symlink(outside, filepath.Join(root, "work"))
					case "lock-symlink":
						os.Symlink(filepath.Join(outside, "lock"), filepath.Join(root, ".export.lock"))
					case "lock-hardlink":
						p := filepath.Join(outside, "lock")
						os.WriteFile(p, nil, 0600)
						os.Link(p, filepath.Join(root, ".export.lock"))
					}
				}
			}
			if _, err := Export(home, "work", exportFixture(), true, false); err == nil {
				t.Fatal("unsafe path accepted")
			}
			entries, _ := os.ReadDir(outside)
			if kind != "lock-hardlink" && len(entries) != 0 {
				t.Fatal("wrote through symlink")
			}
		})
	}
}

func TestExportConfigQuotedAndProxyRaw(t *testing.T) {
	ssh, err := exec.LookPath("ssh")
	if err != nil {
		t.Skip("ssh unavailable")
	}
	home := filepath.Join(secureHome(t), "a space")
	if err := os.Mkdir(home, 0700); err != nil {
		t.Fatal(err)
	}
	b := exportFixture()
	raw := `sh -c 'echo should-not-run; nc %h %p'`
	b.Hosts[0].Options = append(b.Hosts[0].Options, Option{Name: "proxycommand", Args: []string{raw}})
	config, err := Export(home, "work", b, true, false)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(config)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "proxycommand "+raw+"\n") {
		t.Fatal("ProxyCommand rewritten")
	}
	output, err := exec.Command(ssh, "-G", "-F", config, "test-server").CombinedOutput()
	if err != nil {
		t.Fatalf("ssh rejects exported config: %s", output)
	}
	if !strings.Contains(string(output), "hostname 127.0.0.1\n") || !strings.Contains(string(output), "port 2222\n") || !strings.Contains(string(output), "identityfile "+filepath.Join(filepath.Dir(config), "files", "f0001")+"\n") {
		t.Fatalf("resolved config mismatch: %s", output)
	}
}

func TestInstallMainConcurrentChangesPreserved(t *testing.T) {
	dir := secureHome(t)
	main := filepath.Join(dir, "config")
	if err := os.WriteFile(main, nil, 0600); err != nil {
		t.Fatal(err)
	}
	original, err := inspectMain(main)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(main, []byte("user edit\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := installMain(main, []byte("Include new\n"), original); err == nil {
		t.Fatal("concurrent edit overwritten")
	}
	data, _ := os.ReadFile(main)
	if string(data) != "user edit\n" {
		t.Fatal("edit lost")
	}
}

func TestExportPathQuoting(t *testing.T) {
	ssh, err := exec.LookPath("ssh")
	if err != nil {
		t.Skip("ssh unavailable")
	}
	for _, name := range []string{`a "quote"`} {
		t.Run(name, func(t *testing.T) {
			home := filepath.Join(secureHome(t), name)
			if err := os.Mkdir(home, 0700); err != nil {
				t.Fatal(err)
			}
			config, err := Export(home, "work", exportFixture(), true, true)
			if err != nil {
				t.Fatal(err)
			}
			output, err := exec.Command(ssh, "-G", "-F", filepath.Join(home, ".ssh", "config"), "test-server").CombinedOutput()
			if err != nil {
				t.Fatalf("ssh rejects quoted paths: %s", output)
			}
			if !strings.Contains(string(output), "identityfile "+filepath.Join(filepath.Dir(config), "files", "f0001")+"\n") {
				t.Fatalf("wrong quoted path: %s", output)
			}
		})
	}
}

func TestExportRejectsExpandedHome(t *testing.T) {
	for _, name := range []string{"percent%path", "${ENV}", `a \backslash`, "glob*path", "glob?path", "glob[path]"} {
		home := filepath.Join(secureHome(t), name)
		if err := os.Mkdir(home, 0700); err != nil {
			t.Fatal(err)
		}
		if _, err := Export(home, "work", exportFixture(), true, false); err == nil {
			t.Fatal("path expansion accepted")
		}
		entries, _ := os.ReadDir(home)
		if len(entries) != 0 {
			t.Fatal("rejected home wrote files")
		}
	}
}

func TestExportProxyJumpNotQuoted(t *testing.T) {
	ssh, err := exec.LookPath("ssh")
	if err != nil {
		t.Skip("ssh unavailable")
	}
	b := exportFixture()
	jump := b.Hosts[0]
	jump.Name = "jump"
	b.Hosts = append(b.Hosts, jump)
	b.Hosts[0].Options = append(b.Hosts[0].Options, Option{Name: "proxyjump", Args: []string{"jump"}})
	config, err := Export(secureHome(t), "work", b, true, false)
	if err != nil {
		t.Fatal(err)
	}
	output, err := exec.Command(ssh, "-G", "-F", config, "test-server").CombinedOutput()
	if err != nil {
		t.Fatalf("ssh rejects ProxyJump: %s", output)
	}
	if !strings.Contains(string(output), "proxyjump jump\n") {
		t.Fatalf("ProxyJump aliases changed: %s", output)
	}
}
