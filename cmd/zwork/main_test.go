package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

func TestZworkProcess(t *testing.T) {
	if os.Getenv("ZWORK_TEST_PROCESS") != "1" {
		return
	}
	for i, arg := range os.Args {
		if arg == "--" {
			os.Exit(run(os.Args[i+1:], os.Stdout, os.Stderr))
		}
	}
	os.Exit(99)
}

type fixture struct {
	root, home, bindir, capture string
	listing, listError          string
	listStatus                  int
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	root := t.TempDir()
	f := &fixture{root: root, home: filepath.Join(root, "home"), capture: filepath.Join(root, "received")}
	f.bindir = filepath.Join(f.home, ".local", "bin")
	if err := os.MkdirAll(f.bindir, 0700); err != nil {
		t.Fatal(err)
	}
	write := func(name, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(f.bindir, name), []byte(content), 0700); err != nil {
			t.Fatal(err)
		}
	}
	write("zellij", "#!/bin/sh\nif [ \"$1\" = list-sessions ]; then\n  printf '%s' \"$ZWORK_TEST_LIST\"\n  printf '%s' \"$ZWORK_TEST_LIST_ERROR\" >&2\n  exit \"$ZWORK_TEST_LIST_STATUS\"\nfi\nprintf '%s\\n' \"$$\" \"$@\" > \"$ZWORK_TEST_CAPTURE\"\n")
	write("zkey", "#!/bin/sh\nprintf '%s\\n' \"$$\" \"$@\" > \"$ZWORK_TEST_CAPTURE\"\n")
	return f
}

func (f *fixture) config(t *testing.T, raw string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(f.root, ".zkey.json"), []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
}

func (f *fixture) call(t *testing.T, directory string, args ...string) (int, []string, string) {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command(executable, append([]string{"-test.run=^TestZworkProcess$", "--"}, args...)...)
	command.Dir = directory
	command.Env = append(os.Environ(),
		"ZWORK_TEST_PROCESS=1", "HOME="+f.home,
		"ZWORK_TEST_CAPTURE="+f.capture, "ZWORK_TEST_LIST="+f.listing,
		"ZWORK_TEST_LIST_ERROR="+f.listError,
		"ZWORK_TEST_LIST_STATUS="+strconv.Itoa(f.listStatus),
	)
	var output bytes.Buffer
	command.Stdout, command.Stderr = &output, &output
	err = command.Run()
	status := 0
	if err != nil {
		if exitError, ok := err.(*exec.ExitError); ok {
			status = exitError.ExitCode()
		} else {
			t.Fatal(err)
		}
	}
	data, readError := os.ReadFile(f.capture)
	if readError != nil && !os.IsNotExist(readError) {
		t.Fatal(readError)
	}
	var received []string
	if readError == nil {
		record := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
		if record[0] != strconv.Itoa(command.Process.Pid) {
			t.Fatalf("handoff changed PID: received %s, started %d", record[0], command.Process.Pid)
		}
		received = record[1:]
		if err := os.Remove(f.capture); err != nil {
			t.Fatal(err)
		}
	}
	return status, received, output.String()
}

func TestNewSessionProjectConfig(t *testing.T) {
	f := newFixture(t)
	f.config(t, `{"env":{"Z_KEY":"work/z","API_KEY":"work/api"}}`)
	status, received, output := f.call(t, f.root, "work")
	want := []string{"run", "--env", "Z_KEY=work/z", "--env", "API_KEY=work/api", "--", filepath.Join(f.bindir, "zellij"), "--session", "work"}
	if status != 0 || !reflect.DeepEqual(received, want) {
		t.Fatalf("status %d, received %#v, output %s", status, received, output)
	}
	if !strings.Contains(output, "Z_KEY <- work/z\n  API_KEY <- work/api") {
		t.Fatalf("missing ordered config summary: %s", output)
	}
}

func TestProjectDirectory(t *testing.T) {
	for _, gitProject := range []bool{false, true} {
		t.Run(fmt.Sprintf("git=%t", gitProject), func(t *testing.T) {
			f := newFixture(t)
			f.config(t, `{"env":{"API_KEY":"work/api"}}`)
			sub := filepath.Join(f.root, "src")
			if err := os.Mkdir(sub, 0700); err != nil {
				t.Fatal(err)
			}
			if gitProject {
				if output, err := exec.Command("git", "init", "-q", f.root).CombinedOutput(); err != nil {
					t.Fatalf("git init: %v: %s", err, output)
				}
				if err := os.WriteFile(filepath.Join(sub, ".zkey.json"), []byte("invalid"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			status, received, output := f.call(t, sub, "work")
			if gitProject {
				if status != 0 || !strings.Contains(strings.Join(received, " "), "API_KEY=work/api") {
					t.Fatalf("status %d, received %#v, output %s", status, received, output)
				}
			} else if status != 2 || received != nil {
				t.Fatalf("non-git parent config used: status %d, args %#v", status, received)
			}
		})
	}
}

func TestExplicitKeysOverrideConfig(t *testing.T) {
	for _, raw := range []string{"invalid", `{"env":{"API_KEY":"work/api"}}`} {
		f := newFixture(t)
		f.config(t, raw)
		status, received, output := f.call(t, f.root, "work", "TOKEN", "OTHER=work/other")
		want := []string{"run", "--env", "TOKEN=TOKEN", "--env", "OTHER=work/other", "--", filepath.Join(f.bindir, "zellij"), "--session", "work"}
		if status != 0 || !reflect.DeepEqual(received, want) || output != "" {
			t.Fatalf("status %d, received %#v, output %s", status, received, output)
		}
	}
}

func TestSessionListing(t *testing.T) {
	cases := []struct {
		name, session, listing, listError string
		listStatus, status                int
		keys                              []string
		attach                            bool
	}{
		{name: "active", session: "work", listing: "work [Created 1m ago]\n", attach: true},
		{name: "active with keys", session: "work", listing: "work [Created 1m ago]\n", keys: []string{"TOKEN"}, status: 2},
		{name: "exited", session: "work", listing: "work [Created 1m ago] (EXITED - attach to resurrect)\n", status: 2},
		{name: "active exited name", session: "work-EXITED", listing: "work-EXITED [Created 1m ago]\n", attach: true},
		{name: "different name", session: "work", listing: "work-other [Created 1m ago]\n"},
		{name: "no sessions", session: "work", listStatus: 1, listError: "No active zellij sessions found."},
		{name: "failure", session: "work", listStatus: 1, listError: "permission failure", status: 2},
		{name: "failure status", session: "work", listStatus: 2, listError: "No active zellij sessions found.", status: 2},
		{name: "failure with output", session: "work", listing: "unexpected", listStatus: 1, listError: "No active zellij sessions found.", status: 2},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			f := newFixture(t)
			f.listing, f.listError, f.listStatus = test.listing, test.listError, test.listStatus
			if !test.attach {
				f.config(t, `{"env":{"API_KEY":"work/api"}}`)
			}
			status, received, output := f.call(t, f.root, append([]string{test.session}, test.keys...)...)
			if status != test.status {
				t.Fatalf("status %d, want %d: %s", status, test.status, output)
			}
			if test.attach && !reflect.DeepEqual(received, []string{"attach", test.session}) {
				t.Fatalf("attach arguments %#v", received)
			}
			if test.status != 0 && received != nil {
				t.Fatalf("failed command handed off: %#v", received)
			}
		})
	}
}

func TestInvalidConfig(t *testing.T) {
	cases := []string{
		`[]`, `{}`, `{"env":{}}`, `{"env":[]}`, `{"env":{"KEY":null}}`,
		`{"env":{"KEY":3}}`, `{"env":{"KEY":true}}`, `{"env":{"KEY":"x"},"other":true}`,
		`{"env":{"BAD NAME":"x"}}`, `{"env":{"KEY":"fake-sensitive value"}}`, `{"env":{"KEY":""}}`,
		`{"env":{"KEY":"x","KEY":"y"}}`, `{"env":{"KEY":"x"},"env":{"KEY":"y"}}`,
		`{"env":{"KEY":"x","\u004bEY":"y"}}`, `{"env":{"KEY":{"x":1,"x":2}}}`,
		`fake-sensitive-invalid-content`, `{"env":{"KEY":"x"}} {}`, `{"env":{"KEY":"x"}`, "{\xff}",
	}
	for i, raw := range cases {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			f := newFixture(t)
			f.config(t, raw)
			status, received, output := f.call(t, f.root, "work")
			if status != 2 || received != nil || strings.Contains(output, "fake-sensitive") {
				t.Fatalf("status %d, received %#v, output %s", status, received, output)
			}
		})
	}
}

func TestInvalidArguments(t *testing.T) {
	for _, args := range [][]string{nil, {"bad name", "KEY"}, {"work", "A=x", "A=y"}, {"work", "1BAD"}, {"work", "A=bad value"}, {"work", "--unknown"}} {
		f := newFixture(t)
		status, received, output := f.call(t, f.root, args...)
		if status != 2 || received != nil {
			t.Fatalf("args %#v: status %d, received %#v, output %s", args, status, received, output)
		}
	}
}

func TestMissingConfigAndTools(t *testing.T) {
	f := newFixture(t)
	status, _, output := f.call(t, f.root, "work")
	if status != 2 || !strings.Contains(output, "新建会话需要项目 .zkey.json") {
		t.Fatalf("missing config: status %d, output %s", status, output)
	}
	if err := os.Remove(filepath.Join(f.bindir, "zkey")); err != nil {
		t.Fatal(err)
	}
	status, _, output = f.call(t, f.root, "work", "KEY")
	if status != 1 || !strings.Contains(output, "无法启动工具") {
		t.Fatalf("missing zkey: status %d, output %s", status, output)
	}
	if err := os.Remove(filepath.Join(f.bindir, "zellij")); err != nil {
		t.Fatal(err)
	}
	status, _, output = f.call(t, f.root, "work", "KEY")
	if status != 1 || !strings.Contains(output, "无法启动工具") {
		t.Fatalf("missing zellij: status %d, output %s", status, output)
	}
}

func TestHelp(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if status := run([]string{"--help"}, &stdout, &stderr); status != 0 || !strings.Contains(stdout.String(), usage) || stderr.Len() != 0 {
		t.Fatalf("status %d, stdout %q, stderr %q", status, stdout.String(), stderr.String())
	}
}
