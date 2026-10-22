package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zyc945/zKeys/internal/keystore"
)

func TestParseImportEnv(t *testing.T) {
	o, err := parse([]string{"import-env", "TOKEN_A", "TOKEN_B"})
	if err != nil || len(o.names) != 2 || o.names[0] != "TOKEN_A" || o.names[1] != "TOKEN_B" {
		t.Fatal(o, err)
	}
	for _, names := range [][]string{nil, {"TOKEN", "TOKEN"}, {"1TOKEN"}, {"work/key"}, {"TOKEN=value"}, {"--all"}, {"_TOKEN"}, {strings.Repeat("A", 129)}} {
		if _, err := parse(append([]string{"import-env"}, names...)); err == nil {
			t.Fatalf("accepted %q", names)
		}
	}
}

func TestImportEnvRejectsInvalidValuesBeforeLock(t *testing.T) {
	for name, value := range map[string]string{"empty": "", "invalid-utf8": "fake-invalid-\xff-value"} {
		t.Run(name, func(t *testing.T) {
			t.Setenv("ZKEY_TEST_IMPORT", value)
			directory := filepath.Join(t.TempDir(), "keystore")
			_, err := operate(options{action: "import-env", names: []string{"ZKEY_TEST_IMPORT"}}, keystore.Store{Directory: directory})
			if err == nil || !strings.Contains(err.Error(), "ZKEY_TEST_IMPORT") || strings.Contains(err.Error(), "fake-invalid") {
				t.Fatal("missing sanitized validation error")
			}
			if _, err := os.Stat(directory); !os.IsNotExist(err) {
				t.Fatal("invalid environment created the store directory")
			}
		})
	}
}

func TestParseRunAndSet(t *testing.T) {
	for _, args := range [][]string{{"run", "--env", "TOKEN=work/api", "--", "program", "--literal"}, {"run", "--env=TOKEN=work/api", "program", "--literal"}} {
		o, err := parse(args)
		if err != nil {
			t.Fatal(err)
		}
		if o.bindings["TOKEN"] != "work/api" || len(o.command) != 2 || o.command[1] != "--literal" {
			t.Fatal(o)
		}
	}
	for _, args := range [][]string{{"set", "key", "--stdin"}, {"set", "--stdin", "key"}} {
		o, err := parse(args)
		if err != nil || !o.stdin || o.name != "key" {
			t.Fatal(o, err)
		}
	}
	for _, args := range [][]string{{"run", "--env", "A=x", "--env", "A=y", "--", "x"}, {"run", "--env", "1A=x", "--", "x"}, {"run", "--env", "A=bad name", "--", "x"}, {"run", "--env", "A=x"}, {"run", "--", "x"}, {"set", "a", "b"}, {"get", "bad name"}, {"list", "extra"}, {"get"}} {
		if _, err := parse(args); err == nil {
			t.Fatalf("accepted %q", args)
		}
	}
}
