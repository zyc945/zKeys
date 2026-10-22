package main

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"unicode/utf8"

	"github.com/zyc945/zKeys/internal/keystore"
	"golang.org/x/sys/unix"
	"golang.org/x/term"
)

type options struct {
	action, name string
	names        []string
	stdin        bool
	bindings     map[string]string
	command      []string
}
type usageError string

func (e usageError) Error() string { return string(e) }
func help() {
	fmt.Println("用法: zkey {init,set NAME [--stdin],import-env NAME...,get NAME,list,passwd,run --env ENV=NAME ... -- COMMAND ...}\n主密码仅从交互终端读取；get 会原样输出密钥，请避免日志记录。")
}
func parse(args []string) (options, error) {
	var o options
	if len(args) == 0 {
		return o, usageError("需要指定命令。")
	}
	o.action = args[0]
	args = args[1:]
	switch o.action {
	case "init", "list", "passwd":
		if len(args) != 0 {
			return o, usageError("该命令不接受其他参数。")
		}
	case "set", "get":
		for _, a := range args {
			if a == "--stdin" && o.action == "set" {
				o.stdin = true
			} else if o.name == "" && !strings.HasPrefix(a, "-") {
				o.name = a
			} else {
				return o, usageError("请指定一个密钥名称；set 可使用 --stdin。")
			}
		}
		if o.name == "" {
			return o, usageError("需要指定密钥名称。")
		}
		if err := keystore.CheckName(o.name); err != nil {
			return o, err
		}
	case "import-env":
		if len(args) == 0 {
			return o, usageError("import-env 需要至少一个环境变量名称。")
		}
		seen := map[string]bool{}
		for _, name := range args {
			if !keystore.EnvName.MatchString(name) || seen[name] {
				return o, usageError("环境变量名称无效或重复。")
			}
			if err := keystore.CheckName(name); err != nil {
				return o, err
			}
			seen[name] = true
		}
		o.names = args
	case "run":
		o.bindings = map[string]string{}
		for len(args) > 0 {
			var binding string
			if args[0] == "--env" {
				if len(args) < 2 {
					return o, usageError("--env 需要 ENV=NAME。")
				}
				binding = args[1]
				args = args[2:]
			} else if strings.HasPrefix(args[0], "--env=") {
				binding = strings.TrimPrefix(args[0], "--env=")
				args = args[1:]
			} else {
				if args[0] == "--" {
					args = args[1:]
				}
				o.command = args
				break
			}
			env, name, ok := strings.Cut(binding, "=")
			if _, duplicate := o.bindings[env]; !ok || !keystore.EnvName.MatchString(env) || duplicate {
				return o, keystore.Error("--env 必须为 ENV=NAME，且环境变量名不可重复。")
			}
			if err := keystore.CheckName(name); err != nil {
				return o, err
			}
			o.bindings[env] = name
		}
		if len(o.bindings) == 0 {
			return o, usageError("run 需要至少一个 --env ENV=NAME。")
		}
		if len(o.command) == 0 {
			return o, keystore.Error("run 需要在 -- 后提供命令。")
		}
	default:
		return o, usageError("未知命令。")
	}
	return o, nil
}
func hidden(prompt string) ([]byte, error) {
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return nil, keystore.Error("需要交互终端；请在自己的 SSH 终端中运行。")
	}
	defer tty.Close()
	fd := int(tty.Fd())
	state, err := term.GetState(fd)
	if err != nil {
		return nil, keystore.Error("需要交互终端；请在自己的 SSH 终端中运行。")
	}
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(signals)
	// Restore echo also when interrupted while reading the password.
	defer term.Restore(fd, state)
	// Only this goroutine changes terminal state. Disable echo before
	// showing the prompt; the reader never changes it during cancellation.
	settings, err := unix.IoctlGetTermios(fd, readTermios)
	if err != nil {
		return nil, err
	}
	settings.Lflag &^= unix.ECHO | unix.ECHONL
	settings.Lflag |= unix.ICANON | unix.ISIG
	settings.Iflag |= unix.ICRNL
	if err = unix.IoctlSetTermios(fd, writeTermios, settings); err != nil {
		return nil, err
	}
	fmt.Fprint(tty, prompt)
	type result struct {
		value []byte
		err   error
	}
	done := make(chan result, 1)
	go func() {
		value, err := bufio.NewReader(tty).ReadBytes('\n')
		if err == nil {
			value = value[:len(value)-1]
		}
		done <- result{value, err}
	}()
	select {
	case <-signals:
		// On Darwin, closing a tty can wait for the other goroutine
		// blocked reading the password. Restore echo before exiting directly;
		// the OS closes descriptors and releases the vault lock.
		term.Restore(fd, state)
		fmt.Fprintln(os.Stderr, "\nzkey: 已取消。")
		os.Exit(130)
		return nil, nil
	case r := <-done:
		fmt.Fprintln(tty)
		if r.err != nil {
			return nil, keystore.Error("需要交互终端；请在自己的 SSH 终端中运行。")
		}
		if !utf8.Valid(r.value) {
			clear(r.value)
			return nil, keystore.Error("输入必须为 UTF-8 文本。")
		}
		return r.value, nil
	}
}
func newPassword() ([]byte, error) {
	a, err := hidden("设置主密码（不回显）: ")
	if err != nil {
		return nil, err
	}
	b, err := hidden("再次输入主密码: ")
	defer clear(b)
	if err != nil {
		clear(a)
		return nil, err
	}
	if !bytes.Equal(a, b) {
		clear(a)
		return nil, keystore.Error("两次主密码不一致。")
	}
	return a, nil
}
func operate(o options, s keystore.Store) ([]string, error) {
	var imported map[string]string
	if o.action == "import-env" {
		imported = make(map[string]string, len(o.names))
		for _, name := range o.names {
			value, present := os.LookupEnv(name)
			if !present || value == "" || !utf8.ValidString(value) || strings.IndexByte(value, 0) >= 0 {
				return nil, keystore.Error("环境变量 " + name + " 未 export、为空或不是有效 UTF-8 文本（不可包含 NUL）。")
			}
			imported[name] = value
		}
	}
	lock, err := s.Lock()
	if err != nil {
		return nil, err
	}
	defer lock.Close()
	if o.action == "init" {
		exists, err := s.Exists()
		if err != nil {
			return nil, err
		}
		if exists {
			return nil, keystore.Error("密钥库已经存在，拒绝覆盖。")
		}
		pw, err := newPassword()
		if err != nil {
			return nil, err
		}
		defer clear(pw)
		if err = s.Write(map[string]string{}, pw); err != nil {
			return nil, err
		}
		fmt.Fprintln(os.Stderr, "密钥库已创建。请保管主密码；忘记后无法恢复。")
		return nil, nil
	}
	pw, err := hidden("主密码（不回显）: ")
	if err != nil {
		return nil, err
	}
	defer clear(pw)
	values, err := s.Read(pw)
	if err != nil {
		return nil, err
	}
	switch o.action {
	case "import-env":
		for name, value := range imported {
			values[name] = value
		}
		if err = s.Write(values, pw); err != nil {
			return nil, err
		}
		fmt.Fprintf(os.Stderr, "已保存 %d 个环境变量。\n", len(imported))
	case "set":
		var value []byte
		if o.stdin {
			value, err = io.ReadAll(os.Stdin)
		} else {
			value, err = hidden("密钥值（不回显）: ")
		}
		defer clear(value)
		if err != nil {
			return nil, err
		}
		if len(value) == 0 || bytes.IndexByte(value, 0) >= 0 || !utf8.Valid(value) {
			return nil, keystore.Error("密钥值须为非空 UTF-8 文本，且不可包含 NUL。")
		}
		values[o.name] = string(value)
		if err = s.Write(values, pw); err != nil {
			return nil, err
		}
		fmt.Fprintln(os.Stderr, "密钥已保存。")
	case "get":
		v, ok := values[o.name]
		if !ok {
			return nil, keystore.Error("密钥不存在。")
		}
		_, err = fmt.Fprint(os.Stdout, v)
		return nil, err
	case "list":
		names := make([]string, 0, len(values))
		for k := range values {
			names = append(names, k)
		}
		sort.Strings(names)
		for _, name := range names {
			if _, err = fmt.Fprintln(os.Stdout, name); err != nil {
				return nil, err
			}
		}
	case "passwd":
		next, err := newPassword()
		if err != nil {
			return nil, err
		}
		defer clear(next)
		if err = s.Write(values, next); err != nil {
			return nil, err
		}
		fmt.Fprintln(os.Stderr, "主密码已更新。旧备份仍使用旧密码。")
	case "run":
		selected := map[string]string{}
		for env, key := range o.bindings {
			v, ok := values[key]
			if !ok {
				return nil, keystore.Error("请求的密钥不存在。")
			}
			selected[env] = v
		}
		env := make([]string, 0, len(os.Environ())+len(selected))
		for _, entry := range os.Environ() {
			key, _, _ := strings.Cut(entry, "=")
			if _, ok := selected[key]; !ok {
				env = append(env, entry)
			}
		}
		for key, value := range selected {
			env = append(env, key+"="+value)
		}
		return env, nil
	}
	return nil, nil
}

// Resolve PATH from the child's environment, matching Python os.execvpe.
func execute(argv, env []string) error {
	if strings.ContainsRune(argv[0], '/') {
		return unix.Exec(argv[0], argv, env)
	}
	path := "/bin:/usr/bin"
	for _, entry := range env {
		if strings.HasPrefix(entry, "PATH=") {
			path = strings.TrimPrefix(entry, "PATH=")
			break
		}
	}
	var first error
	for _, dir := range strings.Split(path, ":") {
		err := unix.Exec(filepath.Join(dir, argv[0]), argv, env)
		if !errors.Is(err, unix.ENOENT) && !errors.Is(err, unix.ENOTDIR) && first == nil {
			first = err
		}
	}
	if first != nil {
		return first
	}
	return unix.ENOENT
}
func wantsHelp(args []string) bool {
	if len(args) == 0 {
		return false
	}
	if args[0] == "--help" || args[0] == "-h" {
		return true
	}
	for i := 1; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			return false
		}
		if arg == "-h" || arg == "--help" {
			return true
		}
		if args[0] == "run" {
			if arg == "--env" {
				i++
				continue
			}
			if strings.HasPrefix(arg, "--env=") {
				continue
			}
			return false
		}
	}
	return false
}
func run(args []string) error {
	if wantsHelp(args) {
		help()
		return nil
	}
	o, err := parse(args)
	if err != nil {
		return err
	}
	if err = unix.Setrlimit(unix.RLIMIT_CORE, &unix.Rlimit{Cur: 0, Max: 0}); err != nil {
		return err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	env, err := operate(o, keystore.Store{Directory: filepath.Join(home, ".config", "z-keystore")})
	if err != nil {
		return err
	}
	if env != nil {
		return execute(o.command, env)
	}
	return nil
}
func main() {
	err := run(os.Args[1:])
	if err == nil {
		return
	}
	var expected keystore.Error
	var usage usageError
	switch {
	case errors.As(err, &expected):
		fmt.Fprintln(os.Stderr, "zkey:", expected)
	case errors.As(err, &usage):
		fmt.Fprintln(os.Stderr, "zkey:", usage)
		os.Exit(2)
	default:
		fmt.Fprintln(os.Stderr, "zkey: 操作失败，请检查路径、权限或目标命令。")
	}
	os.Exit(1)
}
