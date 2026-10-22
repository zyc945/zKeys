package main

import "testing"

func TestParseSSH(t *testing.T) {
	for _, args := range [][]string{
		{"ssh", "import", "work", "--host", "server", "--host", "jump", "--config", "/tmp/config"},
		{"ssh", "export", "work", "--with-private-keys", "--install-config"},
		{"ssh", "export", "work"},
	} {
		o, err := parse(args)
		if err != nil || o.ssh == nil {
			t.Fatal(args, err)
		}
	}
	for _, args := range [][]string{
		{"ssh"}, {"ssh", "import", "work"}, {"ssh", "import", "work", "--host"},
		{"ssh", "import", "work", "--host", "x", "--host", "x"},
		{"ssh", "import", "work", "--host", "x", "--with-private-keys"},
		{"ssh", "export", "../work"}, {"ssh", "export", "work", "--host", "x"},
		{"ssh", "export", "work", "--install-config", "--install-config"},
		{"ssh", "export", "work", "--with-private-keys", "--with-private-keys"},
		{"ssh", "export", "work", "--config", "/tmp/config"},
	} {
		if _, err := parse(args); err == nil {
			t.Fatalf("accepted %q", args)
		}
	}
}
