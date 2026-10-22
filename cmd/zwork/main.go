// zwork creates a key-injected Zellij session, or reconnects without unlocking.
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"unicode/utf8"
)

var (
	sessionPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]*$`)
	envPattern     = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	keyPattern     = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.:/-]{0,127}$`)
)

const bindingError = "密钥参数须为 KEY 或 ENV=KEY，且环境变量名不可重复。"

var errStartTool = errors.New("unable to start tool")

func makeBindings(keys []string) ([]string, error) {
	var bindings []string
	variables := make(map[string]bool)
	for _, key := range keys {
		env, name, found := strings.Cut(key, "=")
		if !found {
			name = env
		}
		if !envPattern.MatchString(env) || !keyPattern.MatchString(name) || variables[env] {
			return nil, errors.New(bindingError)
		}
		variables[env] = true
		bindings = append(bindings, "--env", env+"="+name)
	}
	return bindings, nil
}

type member struct {
	name  string
	value any
}

type object []member

// Read objects in input order and reject duplicate fields at every level.
func readJSON(decoder *json.Decoder) (any, error) {
	token, err := decoder.Token()
	if err != nil {
		return nil, err
	}
	delimiter, ok := token.(json.Delim)
	if !ok {
		return token, nil
	}
	if delimiter == '{' {
		result := object{}
		seen := make(map[string]bool)
		for decoder.More() {
			token, err := decoder.Token()
			if err != nil {
				return nil, err
			}
			name, ok := token.(string)
			if !ok || seen[name] {
				return nil, errors.New("invalid or duplicate JSON field")
			}
			seen[name] = true
			value, err := readJSON(decoder)
			if err != nil {
				return nil, err
			}
			result = append(result, member{name, value})
		}
		_, err := decoder.Token()
		return result, err
	}
	if delimiter == '[' {
		var result []any
		for decoder.More() {
			value, err := readJSON(decoder)
			if err != nil {
				return nil, err
			}
			result = append(result, value)
		}
		_, err := decoder.Token()
		return result, err
	}
	return nil, errors.New("invalid JSON delimiter")
}

func projectKeys(stderr io.Writer) ([]string, error) {
	directory, err := os.Getwd()
	if err != nil {
		return nil, errStartTool
	}
	output, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	var exitError *exec.ExitError
	if err != nil && !errors.As(err, &exitError) && !errors.Is(err, exec.ErrNotFound) && !errors.Is(err, os.ErrNotExist) {
		return nil, errStartTool
	}
	if err == nil && strings.TrimSpace(string(output)) != "" {
		directory = strings.TrimSpace(string(output))
	}
	path := filepath.Join(directory, ".zkey.json")
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, errors.New("新建会话需要项目 .zkey.json 或显式密钥参数，例如：zwork work OPENAI_API_KEY")
	}
	readError := errors.New("无法读取 .zkey.json：请检查文件权限、JSON 格式及重复字段。")
	if err != nil || !utf8.Valid(data) {
		return nil, readError
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	config, err := readJSON(decoder)
	if err != nil {
		return nil, readError
	}
	if _, err := decoder.Token(); err != io.EOF {
		return nil, readError
	}
	shapeError := errors.New(".zkey.json 必须仅包含非空 env 对象，值为密钥名称字符串。")
	root, ok := config.(object)
	if !ok || len(root) != 1 || root[0].name != "env" {
		return nil, shapeError
	}
	env, ok := root[0].value.(object)
	if !ok || len(env) == 0 {
		return nil, shapeError
	}
	keys := make([]string, 0, len(env))
	for _, field := range env {
		name, ok := field.value.(string)
		if !ok {
			return nil, shapeError
		}
		keys = append(keys, field.name+"="+name)
	}
	bindings, err := makeBindings(keys)
	if err != nil {
		return nil, err
	}
	fmt.Fprintln(stderr, "项目密钥配置："+path)
	for _, field := range env {
		fmt.Fprintf(stderr, "  %s <- %s\n", field.name, field.value)
	}
	return bindings, nil
}

const usage = "usage: zwork [-h] session [KEY ...]"

func run(args []string, stdout, stderr io.Writer) int {
	fail := func(message string) int {
		fmt.Fprintln(stderr, usage)
		fmt.Fprintln(stderr, "zwork: error: "+message)
		return 2
	}
	var positional []string
	options := true
	for _, arg := range args {
		if options && arg == "--" {
			options = false
			continue
		}
		if options && (arg == "-h" || arg == "--help") {
			fmt.Fprintln(stdout, usage+"\n\n首次解锁并创建 zellij 会话；已有会话直接连接。\n\nsession  会话名\nKEY      密钥名称或 ENV=密钥名称；新建时省略则读取项目 .zkey.json，显式参数优先")
			return 0
		}
		if options && strings.HasPrefix(arg, "-") && arg != "-" {
			return fail("无法识别的参数。")
		}
		positional = append(positional, arg)
	}
	if len(positional) == 0 {
		return fail("缺少会话名。")
	}
	session := positional[0]
	if !sessionPattern.MatchString(session) {
		return fail("会话名只能使用字母、数字、点、横线和下划线，且以字母或数字开头。")
	}
	bindings, err := makeBindings(positional[1:])
	if err != nil {
		return fail(err.Error())
	}
	toolError := func() int {
		fmt.Fprintln(stderr, "zwork: 无法启动工具，请检查 ~/.local/bin/zkey 和 zellij。")
		return 1
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return toolError()
	}
	bindir := filepath.Join(home, ".local", "bin")
	zellij := filepath.Join(bindir, "zellij")
	command := exec.Command(zellij, "list-sessions", "--no-formatting")
	var listing, listingError bytes.Buffer
	command.Stdout, command.Stderr = &listing, &listingError
	if err := command.Run(); err != nil {
		var exitError *exec.ExitError
		if !errors.As(err, &exitError) {
			return toolError()
		}
		if exitError.ExitCode() != 1 || strings.TrimSpace(listing.String()) != "" || !strings.Contains(listingError.String(), "No active zellij sessions") {
			return fail("无法读取 zellij 会话列表；请先运行 zellij list-sessions 检查。")
		}
	}
	existing := false
	for _, line := range strings.Split(listing.String(), "\n") {
		line = strings.TrimSuffix(line, "\r")
		fields := strings.Fields(line)
		if len(fields) > 0 && fields[0] == session {
			if strings.HasSuffix(line, "(EXITED - attach to resurrect)") {
				return fail("该会话已退出，仅有恢复记录。请使用新的会话名，通过项目配置或显式密钥参数重新解锁创建。")
			}
			existing = true
		}
	}
	if existing {
		if len(bindings) > 0 {
			return fail("会话已存在，不能重新注入密钥。请去掉密钥参数以连接，或使用新的会话名。")
		}
		if err := syscall.Exec(zellij, []string{zellij, "attach", session}, os.Environ()); err != nil {
			return toolError()
		}
	} else {
		if len(bindings) == 0 {
			bindings, err = projectKeys(stderr)
			if errors.Is(err, errStartTool) {
				return toolError()
			}
			if err != nil {
				return fail(err.Error())
			}
		}
		zkey := filepath.Join(bindir, "zkey")
		argv := append([]string{zkey, "run"}, bindings...)
		argv = append(argv, "--", zellij, "--session", session)
		if err := syscall.Exec(zkey, argv, os.Environ()); err != nil {
			return toolError()
		}
	}
	return 0
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}
