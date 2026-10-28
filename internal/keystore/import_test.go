package keystore

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"golang.org/x/sys/unix"
)

func TestImportPreservesSourceAndTargetPassword(t *testing.T) {
	target, source := testStore(t), testStore(t)
	password, incomingPassword := []byte("target-password"), []byte("incoming-password")
	original := map[string]string{"keep": "fake-existing-value"}
	incoming := map[string]string{"new": "fake-multiline\n中文\n", "ssh/work": `{"fixture":"opaque-snapshot"}`}
	if err := target.Write(original, password); err != nil {
		t.Fatal(err)
	}
	if err := source.Write(incoming, incomingPassword); err != nil {
		t.Fatal(err)
	}
	before, metadata := importSnapshot(t, source.Path())
	lock, err := target.Lock()
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	values, err := target.Read(password)
	if err != nil {
		t.Fatal(err)
	}
	count, err := target.Import(source.Path(), values, password, incomingPassword)
	if err != nil || count != 2 {
		t.Fatal(count, err)
	}
	assertImportUnchanged(t, source.Path(), before, metadata)
	got, err := target.Read(password)
	want := map[string]string{"keep": original["keep"], "new": incoming["new"], "ssh/work": incoming["ssh/work"]}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatal("target password or values changed unexpectedly", err)
	}
	if _, err := target.Read(incomingPassword); err == nil {
		t.Fatal("target accepted incoming password")
	}
	st, err := os.Stat(target.Path())
	if err != nil || st.Mode().Perm() != 0600 {
		t.Fatal("target permissions changed", err)
	}
}

func TestImportConflictAndWrongPasswordDoNotWrite(t *testing.T) {
	for _, kind := range []string{"same-value", "different-value", "wrong-password"} {
		t.Run(kind, func(t *testing.T) {
			target, source := testStore(t), testStore(t)
			password := []byte("target-password")
			values := map[string]string{"same": "fake-existing"}
			incoming := map[string]string{"new": "fake-new", "same": "fake-existing"}
			if kind == "different-value" {
				incoming["same"] = "fake-different"
			}
			if err := target.Write(values, password); err != nil {
				t.Fatal(err)
			}
			if err := source.Write(incoming, []byte("incoming-password")); err != nil {
				t.Fatal(err)
			}
			before, metadata := importSnapshot(t, target.Path())
			sourceBefore, sourceMetadata := importSnapshot(t, source.Path())
			incomingPassword := []byte("incoming-password")
			if kind == "wrong-password" {
				incomingPassword = []byte("wrong")
			}
			count, err := target.Import(source.Path(), values, password, incomingPassword)
			if err == nil || count != 0 {
				t.Fatal("accepted invalid import", count, err)
			}
			assertImportUnchanged(t, target.Path(), before, metadata)
			assertImportUnchanged(t, source.Path(), sourceBefore, sourceMetadata)
			if !reflect.DeepEqual(values, map[string]string{"same": "fake-existing"}) {
				t.Fatal("partially merged entries before rejecting conflict")
			}
		})
	}
}

func TestImportEmptyAndSelfDoNotWrite(t *testing.T) {
	for _, kind := range []string{"empty-source", "self", "self-parent-link", "empty-self"} {
		t.Run(kind, func(t *testing.T) {
			target, source := testStore(t), testStore(t)
			password := []byte("test-password")
			values := map[string]string{"keep": "fake-existing"}
			if kind == "empty-self" {
				values = map[string]string{}
			}
			if err := target.Write(values, password); err != nil {
				t.Fatal(err)
			}
			if err := source.Write(map[string]string{}, password); err != nil {
				t.Fatal(err)
			}
			path := source.Path()
			if kind == "self" || kind == "empty-self" {
				path = target.Path()
			} else if kind == "self-parent-link" {
				link := filepath.Join(t.TempDir(), "linked-parent")
				if err := os.Symlink(target.Directory, link); err != nil {
					t.Fatal(err)
				}
				path = filepath.Join(link, "vault.json")
			}
			before, metadata := importSnapshot(t, target.Path())
			count, err := target.Import(path, values, password, password)
			if count != 0 || (kind == "empty-source") != (err == nil) {
				t.Fatal(count, err)
			}
			assertImportUnchanged(t, target.Path(), before, metadata)
		})
	}
}

func TestImportRejectsUnsafeOrMalformedInput(t *testing.T) {
	for _, kind := range []string{"missing", "directory", "permissions", "symlink", "dangling-symlink", "hardlink", "fifo", "malformed"} {
		t.Run(kind, func(t *testing.T) {
			target, source := testStore(t), testStore(t)
			password := []byte("test-password")
			values := map[string]string{"keep": "fake-existing"}
			if err := target.Write(values, password); err != nil {
				t.Fatal(err)
			}
			if err := source.Write(map[string]string{"new": "fake-new"}, password); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(t.TempDir(), "input")
			var err error
			switch kind {
			case "directory":
				err = os.Mkdir(path, 0700)
			case "permissions":
				path = source.Path()
				err = os.Chmod(path, 0644)
			case "symlink":
				err = os.Symlink(source.Path(), path)
			case "dangling-symlink":
				err = os.Symlink(path+"-missing", path)
			case "hardlink":
				err = os.Link(source.Path(), path)
			case "fifo":
				err = unix.Mkfifo(path, 0600)
			case "malformed":
				err = os.WriteFile(path, []byte(`{"not":"a keystore"}`), 0600)
			}
			if err != nil {
				t.Fatal(err)
			}
			before, metadata := importSnapshot(t, target.Path())
			if count, err := target.Import(path, values, password, password); err == nil || count != 0 {
				t.Fatal("accepted unsafe or invalid import", count, err)
			}
			assertImportUnchanged(t, target.Path(), before, metadata)
		})
	}
}

func importSnapshot(t *testing.T, path string) ([]byte, os.FileInfo) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return data, info
}

func assertImportUnchanged(t *testing.T, path string, before []byte, metadata os.FileInfo) {
	t.Helper()
	after, current := importSnapshot(t, path)
	if !bytes.Equal(before, after) || !os.SameFile(metadata, current) || !metadata.ModTime().Equal(current.ModTime()) || metadata.Mode() != current.Mode() {
		t.Fatal("import changed protected file")
	}
}
