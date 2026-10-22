package sshbundle

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/zyc945/zKeys/internal/keystore"
	"golang.org/x/sys/unix"
)

// Export restores one bundle into a new, private directory. It never merges SSH config.
func Export(home, name string, b *Bundle, withPrivate, installConfig bool) (configPath string, err error) {
	defer func() {
		if err != nil {
			var safe keystore.Error
			if !errors.As(err, &safe) {
				err = keystore.Error("SSH 导出失败，未覆盖已有配置。")
			}
		}
	}()
	if err = CheckName(name); err != nil {
		return "", err
	}
	if err = Validate(b); err != nil {
		return "", err
	}
	for _, f := range b.Files {
		if f.Kind == "private" && !withPrivate {
			return "", keystore.Error("包含私钥；导出须明确指定 --with-private-keys。")
		}
	}
	home, err = filepath.Abs(home)
	if err != nil {
		return "", err
	}
	if strings.ContainsAny(home, "%\\*?[]") || strings.Contains(home, "${") {
		return "", keystore.Error("SSH 导出目录不能包含百分号、反斜杠、通配符或环境变量展开语法。")
	}
	if err = privateDirectory(home, false, false); err != nil {
		return "", err
	}
	ssh := filepath.Join(home, ".ssh")
	if err = privateDirectory(ssh, true, true); err != nil {
		return "", err
	}
	root := filepath.Join(ssh, "zkeys")
	if err = privateDirectory(root, true, true); err != nil {
		return "", err
	}
	lock, err := exportLock(filepath.Join(root, ".export.lock"))
	if err != nil {
		return "", err
	}
	defer lock.Close()
	target := filepath.Join(root, name)
	if err = mustBeAbsent(target); err != nil {
		return "", err
	}
	main := filepath.Join(ssh, "config")
	var original os.FileInfo
	if installConfig {
		original, err = inspectMain(main)
		if err != nil {
			return "", err
		}
	}
	configPath = filepath.Join(target, "config")
	config, err := renderConfig(b, filepath.Join(target, "files"))
	if err != nil {
		return "", err
	}
	stage, err := os.MkdirTemp(root, ".export-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(stage)
	if err = os.Mkdir(filepath.Join(stage, "files"), 0700); err != nil {
		return "", err
	}
	for _, f := range b.Files {
		if err = writePrivate(filepath.Join(stage, "files", f.ID), f.Data); err != nil {
			return "", err
		}
	}
	if err = writePrivate(filepath.Join(stage, "config"), []byte(config)); err != nil {
		return "", err
	}
	if err = syncDirectory(filepath.Join(stage, "files")); err != nil {
		return "", err
	}
	if err = syncDirectory(stage); err != nil {
		return "", err
	}
	if installConfig {
		current, checkErr := inspectMain(main)
		if checkErr != nil || !unchanged(original, current) {
			return "", keystore.Error("SSH 主配置在导出期间发生变化，已停止。")
		}
	}
	if err = mustBeAbsent(target); err != nil {
		return "", err
	}
	if err = os.Rename(stage, target); err != nil {
		return "", err
	}
	published := true
	defer func() {
		if err != nil && published {
			os.RemoveAll(target)
		}
	}()
	if err = syncDirectory(root); err != nil {
		return "", err
	}
	if installConfig {
		include, quoteErr := quoteSSH(configPath)
		if quoteErr != nil {
			return "", quoteErr
		}
		if err = installMain(main, []byte("Include "+include+"\n"), original); err != nil {
			return "", err
		}
	}
	published = false
	return configPath, nil
}

func privateDirectory(path string, create, strict bool) error {
	if create {
		if err := os.Mkdir(path, 0700); err != nil && !errors.Is(err, os.ErrExist) {
			return err
		}
	}
	var st unix.Stat_t
	if err := unix.Lstat(path, &st); err != nil {
		return err
	}
	if st.Mode&unix.S_IFMT != unix.S_IFDIR || st.Uid != uint32(os.Getuid()) || (strict && st.Mode&07777 != 0700) || st.Mode&0022 != 0 {
		return keystore.Error("SSH 目录必须为当前用户所有的真实目录；.ssh 和 zkeys 目录权限须为 700。")
	}
	return nil
}

func exportLock(path string) (*os.File, error) {
	fd, err := unix.Open(path, unix.O_RDWR|unix.O_CREAT|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0600)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), path)
	var st unix.Stat_t
	if err = unix.Fstat(fd, &st); err == nil {
		if st.Mode&unix.S_IFMT != unix.S_IFREG || st.Uid != uint32(os.Getuid()) || st.Mode&07777 != 0600 || st.Nlink != 1 {
			err = keystore.Error("SSH 导出锁必须为当前用户所有、权限 600 的普通文件且无硬链接。")
		}
	}
	if err == nil {
		err = unix.Flock(fd, unix.LOCK_EX)
	}
	if err != nil {
		f.Close()
		return nil, err
	}
	return f, nil
}

func mustBeAbsent(path string) error {
	_, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	return keystore.Error("SSH 导出目录已存在，请使用其他名称或手动处理已有目录。")
}

func inspectMain(path string) (os.FileInfo, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var st unix.Stat_t
	if err = unix.Lstat(path, &st); err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || st.Uid != uint32(os.Getuid()) || st.Nlink != 1 || info.Mode().Perm()&0022 != 0 || info.Size() != 0 {
		return nil, keystore.Error("--install-config 仅适用于不存在或为空的当前用户普通 SSH 配置，且不得可被组或其他用户写入、含链接。")
	}
	return info, nil
}

func unchanged(a, b os.FileInfo) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return os.SameFile(a, b) && a.Size() == b.Size() && a.Mode() == b.Mode() && a.ModTime() == b.ModTime()
}

func writePrivate(path string, data []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	if err = f.Chmod(0600); err != nil {
		return err
	}
	if _, err = f.Write(data); err != nil {
		return err
	}
	if err = f.Sync(); err != nil {
		return err
	}
	return f.Close()
}

func syncDirectory(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}

// installMain links the new config exclusively. An old empty config is moved to
// a uniquely named backup first, so even a concurrent edit is preserved.
func installMain(main string, data []byte, original os.FileInfo) (err error) {
	tmp, err := os.CreateTemp(filepath.Dir(main), ".config.zkeys-new-")
	if err != nil {
		return err
	}
	tempPath := tmp.Name()
	defer os.Remove(tempPath)
	if err = tmp.Chmod(0600); err == nil {
		_, err = tmp.Write(data)
	}
	if err == nil {
		err = tmp.Sync()
	}
	closeErr := tmp.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	var backup string
	linked := false
	newInfo, err := os.Lstat(tempPath)
	if err != nil {
		return err
	}
	defer func() {
		if err == nil {
			return
		}
		if linked {
			current, statErr := os.Lstat(main)
			if statErr == nil && os.SameFile(current, newInfo) {
				os.Remove(main)
			}
		}
		if backup != "" && os.Link(backup, main) == nil {
			os.Remove(backup)
		}
	}()
	if original != nil {
		current, err := inspectMain(main)
		if err != nil || !unchanged(original, current) {
			return keystore.Error("SSH 主配置在导出期间发生变化，已停止。")
		}
		saved, err := os.CreateTemp(filepath.Dir(main), ".config.zkeys-backup-")
		if err != nil {
			return err
		}
		backup = saved.Name()
		if err = saved.Close(); err != nil {
			os.Remove(backup)
			return err
		}
		if err = os.Rename(main, backup); err != nil {
			os.Remove(backup)
			return err
		}
		moved, checkErr := inspectMain(backup)
		if checkErr != nil || !unchanged(original, moved) {
			// Exclusive linking restores only an absent main path; a concurrent file is retained.
			return keystore.Error("SSH 主配置在导出期间发生变化，原文件已保留于备份。")
		}
		if err = os.Chmod(backup, 0600); err != nil {
			return err
		}
	}
	if err = os.Link(tempPath, main); err != nil {
		return keystore.Error("SSH 主配置写入冲突，已保留已有文件。")
	}
	linked = true
	if err = os.Remove(tempPath); err != nil {
		return err
	}
	return syncDirectory(filepath.Dir(main))
}

func quoteSSH(value string) (string, error) {
	if !utf8.ValidString(value) || strings.ContainsAny(value, "\x00\r\n") {
		return "", keystore.Error("SSH 配置包含无效的单行文本。")
	}
	// OpenSSH consumes double quotes and backslash escapes while tokenizing.
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(value) + `"`, nil
}

func renderConfig(b *Bundle, files string) (string, error) {
	var out strings.Builder
	for _, host := range b.Hosts {
		name, err := quoteSSH(host.Name)
		if err != nil {
			return "", err
		}
		out.WriteString("Host " + name + "\n")
		for _, option := range host.Options {
			out.WriteString("    " + option.Name)
			for _, arg := range option.Args {
				out.WriteByte(' ')
				if option.Name == "proxycommand" || option.Name == "proxyjump" {
					if !utf8.ValidString(arg) || strings.ContainsAny(arg, "\x00\r\n") {
						return "", keystore.Error("SSH 代理配置须为单行文本。")
					}
					out.WriteString(arg)
					continue
				}
				if (option.Name == "identityfile" || option.Name == "certificatefile" || option.Name == "userknownhostsfile") && arg != "none" && arg != "/dev/null" {
					arg = filepath.Join(files, arg)
				}
				value, err := quoteSSH(arg)
				if err != nil {
					return "", err
				}
				out.WriteString(value)
			}
			out.WriteByte('\n')
		}
		out.WriteByte('\n')
	}
	return out.String(), nil
}
