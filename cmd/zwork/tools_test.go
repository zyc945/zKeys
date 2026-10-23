package main

import (
	"os"
	"path/filepath"
	"testing"
)

func writeTool(t *testing.T, path string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), mode); err != nil {
		t.Fatal(err)
	}
}

func TestFindZkeyPrefersRealExecutableDirectory(t *testing.T) {
	root := t.TempDir()
	executable := filepath.Join(root, "release", "zwork")
	sibling := filepath.Join(root, "release", "zkey")
	fallback := filepath.Join(root, "home", ".local", "bin", "zkey")
	writeTool(t, executable, 0700)
	writeTool(t, sibling, 0700)
	writeTool(t, fallback, 0700)
	link := filepath.Join(root, "zwork-link")
	if err := os.Symlink(executable, link); err != nil {
		t.Fatal(err)
	}
	path, err := findZkey(link, filepath.Join(root, "home"))
	realSibling, resolveError := filepath.EvalSymlinks(sibling)
	if err != nil || resolveError != nil || path != realSibling {
		t.Fatalf("path %q, err %v", path, err)
	}
	if err := os.Remove(sibling); err != nil {
		t.Fatal(err)
	}
	path, err = findZkey(link, filepath.Join(root, "home"))
	if err != nil || path != fallback {
		t.Fatalf("fallback path %q, err %v", path, err)
	}
}

func TestFindZkeyRejectsInvalidSiblingWithoutFallback(t *testing.T) {
	for _, kind := range []string{"nonexecutable", "directory", "broken-symlink"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			executable := filepath.Join(root, "release", "zwork")
			sibling := filepath.Join(root, "release", "zkey")
			home := filepath.Join(root, "home")
			writeTool(t, executable, 0700)
			writeTool(t, filepath.Join(home, ".local", "bin", "zkey"), 0700)
			switch kind {
			case "nonexecutable":
				writeTool(t, sibling, 0600)
			case "directory":
				if err := os.Mkdir(sibling, 0700); err != nil {
					t.Fatal(err)
				}
			case "broken-symlink":
				if err := os.Symlink(filepath.Join(root, "missing"), sibling); err != nil {
					t.Fatal(err)
				}
			}
			if path, err := findZkey(executable, home); err == nil {
				t.Fatalf("accepted %s: %q", kind, path)
			}
		})
	}
}

func TestFindZkeyRejectsMissingTools(t *testing.T) {
	root := t.TempDir()
	executable := filepath.Join(root, "zwork")
	writeTool(t, executable, 0700)
	for _, candidate := range []string{executable, filepath.Join(root, "missing")} {
		if path, err := findZkey(candidate, filepath.Join(root, "home")); err == nil {
			t.Fatalf("accepted missing tools: %q", path)
		}
	}
}

func TestFindZellijPrefersInstalledThenPath(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	installed := filepath.Join(home, ".local", "bin", "zellij")
	pathTool := filepath.Join(root, "path", "zellij")
	writeTool(t, installed, 0700)
	writeTool(t, pathTool, 0700)
	t.Setenv("PATH", filepath.Dir(pathTool))
	path, err := findZellij(home)
	if err != nil || path != installed {
		t.Fatalf("installed path %q, err %v", path, err)
	}
	if err := os.Remove(installed); err != nil {
		t.Fatal(err)
	}
	path, err = findZellij(home)
	if err != nil || path != pathTool {
		t.Fatalf("PATH path %q, err %v", path, err)
	}
}

func TestFindZellijRejectsInvalidInstalledWithoutFallback(t *testing.T) {
	for _, kind := range []string{"nonexecutable", "directory", "broken-symlink"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			home := filepath.Join(root, "home")
			installed := filepath.Join(home, ".local", "bin", "zellij")
			writeTool(t, filepath.Join(root, "path", "zellij"), 0700)
			t.Setenv("PATH", filepath.Join(root, "path"))
			if err := os.MkdirAll(filepath.Dir(installed), 0700); err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "nonexecutable":
				writeTool(t, installed, 0600)
			case "directory":
				if err := os.Mkdir(installed, 0700); err != nil {
					t.Fatal(err)
				}
			case "broken-symlink":
				if err := os.Symlink(filepath.Join(root, "missing"), installed); err != nil {
					t.Fatal(err)
				}
			}
			if path, err := findZellij(home); err == nil {
				t.Fatalf("accepted %s: %q", kind, path)
			}
		})
	}
}

func TestFindZellijRejectsMissingOrRelativePath(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	writeTool(t, filepath.Join(root, "bin", "zellij"), 0700)
	for _, search := range []string{"missing", "bin", "./bin", ""} {
		for _, godebug := range []string{"execerrdot=1", "execerrdot=0"} {
			t.Setenv("PATH", search)
			t.Setenv("GODEBUG", godebug)
			if path, err := findZellij(filepath.Join(root, "home")); err == nil {
				t.Fatalf("accepted PATH=%q GODEBUG=%q: %q", search, godebug, path)
			}
		}
	}
}
