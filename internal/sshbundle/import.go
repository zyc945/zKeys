package sshbundle

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/zyc945/zKeys/internal/keystore"
	"golang.org/x/sys/unix"
)

type configOption struct {
	hosts  []string
	option Option
}

func configError(line int, directive string) error {
	if directive != "" && strings.IndexFunc(directive, func(r rune) bool { return r < 'a' || r > 'z' }) < 0 {
		return keystore.Error(fmt.Sprintf("SSH 配置第 %d 行：不支持或无效的 %s 指令。", line, directive))
	}
	return keystore.Error(fmt.Sprintf("SSH 配置第 %d 行无效。", line))
}

// words handles static OpenSSH quoting without evaluating shell expressions.
func words(s string) ([]string, error) {
	var result []string
	var b strings.Builder
	quote := rune(0)
	started := false
	flush := func() {
		if started {
			result = append(result, b.String())
			b.Reset()
			started = false
		}
	}
	runes := []rune(s)
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		if r == '\\' {
			if i+1 < len(runes) {
				next := runes[i+1]
				if next == '\\' || next == quote || (quote == 0 && (unicode.IsSpace(next) || next == '\'' || next == '"')) {
					b.WriteRune(next)
					i++
					started = true
					continue
				}
			}
			b.WriteRune(r)
			started = true
			continue
		}
		if quote != 0 {
			if r == quote {
				quote = 0
			} else {
				b.WriteRune(r)
			}
			started = true
			continue
		}
		if r == '\'' || r == '"' {
			quote = r
			started = true
			continue
		}
		if r == '#' && !started {
			break
		}
		if unicode.IsSpace(r) {
			flush()
			continue
		}
		b.WriteRune(r)
		started = true
	}
	if quote != 0 {
		return nil, invalid()
	}
	flush()
	return result, nil
}

func parseConfig(data []byte) ([]configOption, map[string]bool, error) {
	if !utf8.Valid(data) || strings.ContainsRune(string(data), 0) {
		return nil, nil, invalid()
	}
	var options []configOption
	aliases := map[string]bool{}
	var hosts []string
	for i, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		end := strings.IndexFunc(line, func(r rune) bool { return unicode.IsSpace(r) || r == '=' })
		if end < 0 {
			return nil, nil, configError(i+1, "")
		}
		name := strings.ToLower(line[:end])
		rest := strings.TrimSpace(line[end:])
		rest = strings.TrimSpace(strings.TrimPrefix(rest, "="))
		if name == "host" {
			args, err := words(rest)
			if err != nil || len(args) == 0 {
				return nil, nil, configError(i+1, name)
			}
			for _, arg := range args {
				if !hostName.MatchString(arg) && !(arg == "*" && len(args) == 1) {
					return nil, nil, configError(i+1, name)
				}
				if arg != "*" {
					aliases[arg] = true
				}
			}
			hosts = args
			continue
		}
		if !optionNames[name] {
			return nil, nil, configError(i+1, name)
		}
		var args []string
		var err error
		if name == "proxycommand" {
			args = []string{rest}
		} else {
			args, err = words(rest)
		}
		o := Option{Name: name, Args: args}
		if err != nil || !validateOption(o) {
			return nil, nil, configError(i+1, name)
		}
		options = append(options, configOption{hosts, o})
	}
	return options, aliases, nil
}

func sourceFile(path string, private bool) ([]byte, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), "SSH input")
	defer f.Close()
	var st unix.Stat_t
	if unix.Fstat(fd, &st) != nil || st.Mode&unix.S_IFMT != unix.S_IFREG || st.Uid != uint32(os.Getuid()) || st.Nlink != 1 || st.Mode&0022 != 0 {
		return nil, keystore.Error("SSH 源文件必须为当前用户所有、不可被组或其他用户写入的普通文件，且不可为链接。")
	}
	if private && st.Mode&07777 != 0600 && st.Mode&07777 != 0400 {
		return nil, keystore.Error("SSH 私钥权限必须为 600 或 400。")
	}
	data, err := io.ReadAll(f)
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return nil, keystore.Error("SSH 源文件不可为空。")
	}
	return data, nil
}

func sourcePath(value, home string) (string, error) {
	if strings.ContainsAny(value, "%$") {
		return "", keystore.Error("SSH 文件路径不支持变量或令牌。")
	}
	if strings.HasPrefix(value, "~/") {
		value = filepath.Join(home, value[2:])
	} else if strings.ContainsRune(value, '~') {
		return "", keystore.Error("SSH 文件路径只支持当前用户的 ~/。")
	}
	if !filepath.IsAbs(value) {
		return "", keystore.Error("SSH 文件路径必须为绝对路径或 ~/ 路径。")
	}
	return filepath.Clean(value), nil
}

func Import(configPath, home string, selected []string) (*Bundle, error) {
	if len(selected) == 0 || !filepath.IsAbs(home) {
		return nil, keystore.Error("必须指定至少一个 SSH 连接及绝对用户目录。")
	}
	data, err := sourceFile(configPath, false)
	if err != nil {
		return nil, keystore.Error("无法安全读取 SSH 配置文件。")
	}
	options, aliases, err := parseConfig(data)
	if err != nil {
		return nil, err
	}
	b := &Bundle{Type: "zkeys-ssh", Version: 1}
	done := map[string]bool{}
	visiting := map[string]bool{}
	addFile := func(path, kind string, optional bool) (string, error) {
		data, err := sourceFile(path, kind == "private")
		if optional && errors.Is(err, os.ErrNotExist) {
			return "", nil
		}
		if err != nil {
			return "", keystore.Error("无法安全读取 SSH 引用文件；请检查存在性、所有者、链接和权限。")
		}
		for _, f := range b.Files {
			if sameFile(f, kind, data) {
				return f.ID, nil
			}
		}
		id := fmt.Sprintf("f%04d", len(b.Files)+1)
		b.Files = append(b.Files, File{ID: id, Kind: kind, Data: data})
		return id, nil
	}
	var collect func(string) error
	collect = func(name string) error {
		if !hostName.MatchString(name) || !aliases[name] {
			return keystore.Error("指定的 SSH 连接或跳板别名不存在，或不受支持。")
		}
		if visiting[name] {
			return keystore.Error("SSH ProxyJump 存在循环依赖。")
		}
		if done[name] {
			return nil
		}
		visiting[name] = true
		h := Host{Name: name}
		seen := map[string]bool{}
		accumulated := map[string]bool{}
		jumpActive := false
		for _, r := range options {
			match := len(r.hosts) == 0
			for _, pattern := range r.hosts {
				if pattern == "*" || pattern == name {
					match = true
				}
			}
			if !match {
				continue
			}
			o := r.option
			multi := o.Name == "identityfile" || o.Name == "certificatefile"
			if !multi && seen[o.Name] {
				continue
			}
			if o.Name == "proxycommand" && seen["proxyjump"] && !jumpActive {
				return keystore.Error("ProxyJump none 后的 ProxyCommand 在不同 OpenSSH 版本中行为不同；请仅保留一种代理配置。")
			}
			if (o.Name == "proxyjump" && seen["proxycommand"]) || (o.Name == "proxycommand" && jumpActive) {
				continue
			}
			if o.Name == "proxyjump" {
				jumpActive = o.Args[0] != "none"
			}
			seen[o.Name] = true
			if multi {
				key := o.Name + "\x00" + o.Args[0]
				if accumulated[key] {
					continue
				}
				accumulated[key] = true
			}
			h.Options = append(h.Options, Option{Name: o.Name, Args: append([]string(nil), o.Args...)})
		}
		if !seen["hostname"] || !seen["user"] || !seen["identityfile"] {
			return keystore.Error("每个 SSH 连接必须显式配置 HostName、User 和 IdentityFile。")
		}
		if !seen["port"] {
			h.Options = append(h.Options, Option{Name: "port", Args: []string{"22"}})
		}
		var certificates []Option
		for i := range h.Options {
			o := &h.Options[i]
			kind := ""
			switch o.Name {
			case "identityfile":
				kind = "private"
			case "certificatefile":
				kind = "certificate"
			case "userknownhostsfile":
				kind = "known_hosts"
			default:
				continue
			}
			for j, value := range o.Args {
				if (o.Name == "identityfile" && value == "none") || (o.Name == "userknownhostsfile" && (value == "none" || value == "/dev/null")) {
					continue
				}
				path, err := sourcePath(value, home)
				if err != nil {
					return err
				}
				id, err := addFile(path, kind, false)
				if err != nil {
					return err
				}
				o.Args[j] = id
				if kind == "private" {
					if _, err := addFile(path+".pub", "public", true); err != nil {
						return err
					}
					if !seen["certificatefile"] {
						cert, err := addFile(path+"-cert.pub", "certificate", true)
						if err != nil {
							return err
						}
						if cert != "" {
							certificates = append(certificates, Option{Name: "certificatefile", Args: []string{cert}})
						}
					}
				}
			}
		}
		// Paths with identical contents or automatic certificate companions may converge.
		unique := map[string]bool{}
		compact := h.Options[:0]
		for _, o := range append(h.Options, certificates...) {
			if o.Name == "identityfile" || o.Name == "certificatefile" {
				key := o.Name + "\x00" + o.Args[0]
				if unique[key] {
					continue
				}
				unique[key] = true
			}
			compact = append(compact, o)
		}
		h.Options = compact
		b.Hosts = append(b.Hosts, h)
		for _, o := range h.Options {
			if o.Name == "proxyjump" && o.Args[0] != "none" {
				for _, dep := range strings.Split(o.Args[0], ",") {
					if err := collect(dep); err != nil {
						return err
					}
				}
			}
		}
		delete(visiting, name)
		done[name] = true
		return nil
	}
	for _, name := range selected {
		if err := collect(name); err != nil {
			return nil, err
		}
	}
	if err := Validate(b); err != nil {
		return nil, err
	}
	return b, nil
}
