package main

import "testing"

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
