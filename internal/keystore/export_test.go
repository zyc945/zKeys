package keystore

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
)

func TestExportKeepsSourceAndUsesV1(t *testing.T) {
	source := testStore(t)
	values := map[string]string{"work/key": "fake-selected-secret\n第二行\n", "ssh/work": `{"fake":"ssh-bundle"}`}
	if err := source.Write(values, []byte("original-password")); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(source.Path())
	if err != nil {
		t.Fatal(err)
	}
	metadata, err := os.Stat(source.Path())
	if err != nil {
		t.Fatal(err)
	}
	for _, password := range []string{"", "x", "中文-password"} {
		directory := t.TempDir()
		path := filepath.Join(directory, "vault.json")
		if err := Export(path, values, []byte(password)); err != nil {
			t.Fatal(err)
		}
		got, err := (Store{Directory: directory}).Read([]byte(password))
		if err != nil || !reflect.DeepEqual(got, values) {
			t.Fatal("exported vault not readable using the existing v1 reader", err)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		for name, value := range values {
			if bytes.Contains(data, []byte(name)) || bytes.Contains(data, []byte(value)) {
				t.Fatal("export exposed plaintext")
			}
		}
		st, err := os.Stat(path)
		if err != nil || st.Mode().Perm() != 0600 {
			t.Fatal("export permissions are not 600", err)
		}
		assertNoExportTemps(t, directory)
	}
	after, err := os.ReadFile(source.Path())
	if err != nil {
		t.Fatal(err)
	}
	current, err := os.Stat(source.Path())
	if err != nil || !bytes.Equal(before, after) || !metadata.ModTime().Equal(current.ModTime()) || !os.SameFile(metadata, current) {
		t.Fatal("export changed the source", err)
	}
}

func TestExportRefusesExistingPaths(t *testing.T) {
	for _, kind := range []string{"file", "directory", "symlink", "dangling-symlink", "source", "hardlink"} {
		t.Run(kind, func(t *testing.T) {
			source := testStore(t)
			if err := source.Write(map[string]string{"original": "fake-original-value"}, []byte("source-password")); err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadFile(source.Path())
			if err != nil {
				t.Fatal(err)
			}
			directory := t.TempDir()
			path := filepath.Join(directory, "export.json")
			switch kind {
			case "file":
				err = os.WriteFile(path, []byte("existing-file"), 0600)
			case "directory":
				err = os.Mkdir(path, 0700)
			case "symlink":
				err = os.Symlink(source.Path(), path)
			case "dangling-symlink":
				err = os.Symlink(filepath.Join(directory, "missing"), path)
			case "source":
				path = source.Path()
			case "hardlink":
				err = os.Link(source.Path(), path)
			}
			if err != nil {
				t.Fatal(err)
			}
			metadata, err := os.Lstat(path)
			if err != nil {
				t.Fatal(err)
			}
			if err := Export(path, map[string]string{"new": "fake-new-value"}, []byte("new-password")); err == nil {
				t.Fatal("export replaced existing path")
			}
			current, err := os.Lstat(path)
			if err != nil || !os.SameFile(metadata, current) || !metadata.ModTime().Equal(current.ModTime()) {
				t.Fatal("existing path changed", err)
			}
			after, err := os.ReadFile(source.Path())
			if err != nil || !bytes.Equal(before, after) {
				t.Fatal("source bytes changed", err)
			}
			assertNoExportTemps(t, directory)
		})
	}
}

func TestExportRequiresExistingParent(t *testing.T) {
	directory := t.TempDir()
	missing := filepath.Join(directory, "missing")
	if err := Export(filepath.Join(missing, "vault.json"), map[string]string{"key": "fake-value"}, []byte("password")); err == nil {
		t.Fatal("export created missing parents")
	}
	if _, err := os.Stat(missing); !os.IsNotExist(err) {
		t.Fatal("missing parent was created")
	}
	assertNoExportTemps(t, directory)
}

func TestConcurrentExportsDoNotOverwrite(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "vault.json")
	password := []byte("test-password")
	var group sync.WaitGroup
	results := make(chan error, 2)
	for _, value := range []string{"first-fake-value", "second-fake-value"} {
		group.Go(func() { results <- Export(path, map[string]string{"key": value}, password) })
	}
	group.Wait()
	close(results)
	successes := 0
	for err := range results {
		if err == nil {
			successes++
		}
	}
	if successes != 1 {
		t.Fatalf("expected exactly one export, got %d", successes)
	}
	values, err := (Store{Directory: directory}).Read(password)
	if err != nil || len(values) != 1 || values["key"] != "first-fake-value" && values["key"] != "second-fake-value" {
		t.Fatal("winning export is not a complete v1 vault", err)
	}
	assertNoExportTemps(t, directory)
}

func assertNoExportTemps(t *testing.T, directory string) {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(directory, ".vault-*"))
	if err != nil || len(matches) != 0 {
		t.Fatal("temporary files left after export", matches, err)
	}
}
