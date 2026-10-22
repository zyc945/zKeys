package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/zyc945/zKeys/internal/keystore"
	"github.com/zyc945/zKeys/internal/sshbundle"
)

type sshOptions struct {
	action, name, config string
	hosts                []string
	private, install     bool
}

func parseSSH(args []string) (*sshOptions, error) {
	if len(args) < 2 || (args[0] != "import" && args[0] != "export") {
		return nil, usageError("用法: ssh import NAME --host HOST... [--config PATH] 或 ssh export NAME [--with-private-keys] [--install-config]。")
	}
	o := &sshOptions{action: args[0], name: args[1]}
	if err := sshbundle.CheckName(o.name); err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for i := 2; i < len(args); i++ {
		switch args[i] {
		case "--host":
			if o.action != "import" || i+1 == len(args) {
				return nil, usageError("import 的 --host 需要主机别名。")
			}
			i++
			if seen[args[i]] {
				return nil, usageError("--host 不可重复指定同一主机。")
			}
			seen[args[i]] = true
			o.hosts = append(o.hosts, args[i])
		case "--config":
			if o.action != "import" || o.config != "" || i+1 == len(args) {
				return nil, usageError("import 的 --config 需要一个文件路径，且只能指定一次。")
			}
			i++
			o.config = args[i]
			if o.config == "" {
				return nil, usageError("--config 路径不可为空。")
			}
		case "--with-private-keys":
			if o.action != "export" || o.private {
				return nil, usageError("--with-private-keys 仅用于 export，且只能指定一次。")
			}
			o.private = true
		case "--install-config":
			if o.action != "export" || o.install {
				return nil, usageError("--install-config 仅用于 export，且只能指定一次。")
			}
			o.install = true
		default:
			return nil, usageError("无法识别 SSH 参数；请使用 zkey ssh --help。")
		}
	}
	if o.action == "import" && len(o.hosts) == 0 {
		return nil, usageError("ssh import 需要至少一个 --host HOST。")
	}
	return o, nil
}

func operateSSH(o sshOptions, store keystore.Store) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	var bundle *sshbundle.Bundle
	if o.action == "import" {
		config := o.config
		if config == "" {
			config = filepath.Join(home, ".ssh", "config")
		}
		bundle, err = sshbundle.Import(config, home, o.hosts)
		if err != nil {
			return err
		}
	}
	lock, err := store.Lock()
	if err != nil {
		return err
	}
	defer lock.Close()
	password, err := hidden("主密码（不回显）: ")
	if err != nil {
		return err
	}
	defer clear(password)
	entries, err := store.Read(password)
	if err != nil {
		return err
	}
	key := "ssh/" + o.name
	if o.action == "import" {
		if _, exists := entries[key]; exists {
			return keystore.Error("同名 SSH 条目已经存在，拒绝覆盖；请使用新的名称。")
		}
		encoded, err := sshbundle.Encode(bundle)
		if err != nil {
			return err
		}
		entries[key] = encoded
		if err = store.Write(entries, password); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "已保存 SSH 条目 %s：%d 个主机、%d 个文件。\n", key, len(bundle.Hosts), len(bundle.Files))
	} else {
		encoded, exists := entries[key]
		if !exists {
			return keystore.Error("SSH 条目不存在。")
		}
		bundle, err = sshbundle.Decode(encoded)
		if err != nil {
			return err
		}
		config, err := sshbundle.Export(home, o.name, bundle, o.private, o.install)
		if err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "SSH 配置已导出到 %s。\n", config)
		if o.install {
			fmt.Fprintln(os.Stderr, "已安装到 ~/.ssh/config；可使用 ssh 主机别名。")
		} else {
			fmt.Fprintf(os.Stderr, "主配置未修改。可用 ssh -F %q 主机别名；或审查后在主配置合适位置加入 Include %q。\n", config, config)
		}
	}
	for _, host := range bundle.Hosts {
		for _, option := range host.Options {
			if option.Name == "proxycommand" {
				fmt.Fprintln(os.Stderr, "注意：保留了 ProxyCommand；未执行或打包其外部程序，请在目标设备检查依赖。")
				return nil
			}
		}
	}
	return nil
}
