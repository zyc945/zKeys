package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
)

func executableFile(path string) (string, error) {
	if !filepath.IsAbs(path) {
		return "", errStartTool
	}
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return "", errStartTool
	}
	if _, err := exec.LookPath(path); err != nil {
		return "", errStartTool
	}
	return path, nil
}

func findZkey(executable, home string) (string, error) {
	real, err := filepath.EvalSymlinks(executable)
	if err != nil {
		return "", errStartTool
	}
	path := filepath.Join(filepath.Dir(real), "zkey")
	if _, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) {
		path = filepath.Join(home, ".local", "bin", "zkey")
	} else if err != nil {
		return "", errStartTool
	}
	return executableFile(path)
}

func findZellij(home string) (string, error) {
	path := filepath.Join(home, ".local", "bin", "zellij")
	if _, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) {
		path, err = exec.LookPath("zellij")
		if err != nil {
			return "", errStartTool
		}
	} else if err != nil {
		return "", errStartTool
	}
	return executableFile(path)
}
