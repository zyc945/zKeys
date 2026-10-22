# zKeys

`zkey` 管理个人加密密钥库，`zwork` 将选定密钥注入 Zellij 会话。支持项目级 `.zkey.json` 映射。

## 安装

适用于 Linux。脚本使用 `/usr/bin/python3`，该解释器需要安装 `requirements.txt` 中的依赖；请按系统的 Python 包管理方式安装。`zwork` 另需 Zellij（已验证 0.45.1）及可选的 Git。

```bash
/usr/bin/python3 -c 'import cryptography'
install -d -m 700 "$HOME/.config/z-keystore" "$HOME/.local/bin"
install -m 700 z_keystore.py "$HOME/.config/z-keystore/z_keystore.py"
install -m 700 zwork.py "$HOME/.config/z-keystore/zwork.py"
install -m 600 README.md "$HOME/.config/z-keystore/README.md"
ln -sfn "$HOME/.config/z-keystore/z_keystore.py" "$HOME/.local/bin/zkey"
ln -sfn "$HOME/.config/z-keystore/zwork.py" "$HOME/.local/bin/zwork"
export PATH="$HOME/.local/bin:$PATH"
```

将 PATH 配置加入自己的 shell 启动文件即可长期生效。请确保 `~/.local/bin/zellij` 是可执行文件或指向已安装 Zellij 的链接。安装命令仅安装程序及说明，不复制或初始化 `vault.json`；更新前请停用正在执行的工具命令。

## 使用

个人加密密钥库。依赖系统 `/usr/bin/python3` 和 `cryptography`。默认数据目录为 `~/.config/z-keystore`，密钥名称和值一起加密保存到 `vault.json`。目录权限 700，数据和锁文件 600。使用 Scrypt（N=32768、r=8、p=1）派生 AES-256-GCM 密钥，每次保存生成新 salt 和 nonce，写入使用文件锁与原子替换。

首次使用，请你本人在交互终端运行，设置主密码，无硬性长度限制，建议使用较长的随机口令。主密码不回显，不接受命令参数或环境变量。忘记主密码无法恢复。

```bash
zkey init
```

以下命令在安装工具的机器执行，若 PATH 尚未更新，用 `~/.local/bin/zkey` 替代 `zkey`。

```bash
# 输入主密码，再隐藏输入密钥值。已有同名项会被替换。
zkey set OPENAI_API_KEY

# 解锁后列出名称，不显示值。
zkey list

# 推荐：只将指定项注入目标程序环境，输入一次主密码后启动程序。
zkey run --env OPENAI_API_KEY=OPENAI_API_KEY -- python3 app.py

# 可以一次选择多项；变量名与库中名称可以不同。
zkey run --env SERVICE_TOKEN=work/api --env OTHER_TOKEN=other/api -- ./my-program

# 显式读取单项到 stdout，原样输出且不附加换行。
zkey get OPENAI_API_KEY

# 导入多行内容：stdin 是密钥值，主密码仍从交互终端输入。
# 输入的换行原样保留；不要用带密钥字面量的 echo 命令，避免留下 shell 历史。
zkey set work/private-key --stdin < /path/to/private-file

# 更改主密码，旧备份仍需旧密码解锁。
zkey passwd
```

Python 程序使用 `run` 注入的变量即可：

```python
import os
api_key = os.environ['OPENAI_API_KEY']
# 在程序内部使用，避免打印 api_key。
```

`run` 不通过 shell 执行命令，会在完成解锁并释放锁后直接替换 keystore 进程。目标程序继承原有环境，加上你明确选择的密钥；退出状态和信号按目标程序处理。主密码和整库解密结果不会由一个等待程序结束的 keystore 父进程长期保留。不创建明文临时文件，也不提供后台解锁服务或密码缓存。每次命令重新输入主密码。

Agent 优先使用 `run` 启动需要密钥的代码。请在你自己控制的终端输入主密码，不要把主密码发送到 Agent 对话。不要让 Agent 调用 `get` 并把输出带入工具记录或日志。无交互终端时，工具拒绝读取主密码，不降级为明文输入。程序若主动打印环境变量或密钥，工具无法阻止泄露。

此工具用于保护静态密钥，不能保证对 root、具有 Docker 管理权限的用户或同账号恶意进程保密。解锁后目标程序能够读取、复制获授权的密钥，子进程也可能继承；程序退出不等于凭证被撤销。Python 不保证秘密内存安全擦除；工具将 core 文件大小上限设为 0（目标程序继承此限制），但不能保证系统级崩溃收集器不采集内存，也无法控制 swap、休眠、管理员调试或所有程序日志。

备份时保存完整的 `vault.json`，并单独保管主密码。备份文件同样设置为 600。不要在写入期间直接复制；先停止相关写操作。保留旧备份意味着旧密码仍能解锁当时的数据。本工具不提供防回滚保护。

## zellij 会话入口 zwork

项目根目录可保存 `.zkey.json`，只保存环境变量到密钥名称的映射，不填写密钥值：

```json
{
  "env": {
    "OPENAI_API_KEY": "work/openai",
    "GITHUB_TOKEN": "work/github"
  }
}
```

先用 `zkey set work/openai` 等命令保存对应密钥；然后在项目目录执行：

```bash
zwork work
```

新建会话时展示请求的变量名和密钥名称，由你在终端输入主密码后创建会话。配置属于需求声明，不是权限边界；输入密码前检查映射，尤其是仓库配置发生变化时。

配置发现规则：在 Git 工作区内读取该工作区根目录的 `.zkey.json`，从子目录启动也一样；不在 Git 工作区时只读取当前目录的 `.zkey.json`。不向其他父目录搜索。配置必须仅包含非空 `env` 对象；重复字段、无效名称、非字符串值或格式错误均会阻止新建，不回退到其他配置。

仍可显式指定密钥。显式参数完全覆盖项目配置，不合并，也不读取配置文件：

```bash
zwork other OPENAI_API_KEY SERVICE_TOKEN=work/token
```

已有活跃会话执行 `zwork work` 直接连接，不读取项目配置或再次解锁。修改配置不会更新已有会话；已有会话若同时指定密钥则拒绝。要使用新映射，请新建一个不同名称的会话。

所选密钥作为环境变量传给新会话及后续 pane，密钥值不会由 zwork 写入配置。会话内直接启动程序即可，不再调用 zkey。不自动加载全库；未找到密钥时 zkey 会停止启动。

zwork 要求 Zellij 位于 `~/.local/bin/zellij`（已验证版本 0.45.1）；zwork 使用这个固定路径。注入的密钥可被会话内继承环境的程序读取，插件也应视为该会话的信任范围；不要运行不可信插件或打印环境变量。关闭 session 并不撤销服务端凭证，脱离 session 生存的进程也可能保留密钥。

如果会话显示 EXITED，zwork 会拒绝直接恢复，请使用新的会话名，通过项目配置或显式参数重新解锁创建。断开 SSH 或 detach 不会清除活跃会话中的密钥；只有结束会话及相关进程才能停止其继续保留。

## 测试

在仓库根目录运行（需要 cryptography）：

```bash
/usr/bin/python3 -m unittest discover -s tests -v
```

测试只使用临时目录、虚拟口令和虚拟密钥，不读取个人密钥库，不访问云服务，不修改现有 Zellij 会话。

仓库仅保存源码、说明和测试。不要提交 `vault.json`、主密码、实际密钥或含密钥的环境文件；`.zkey.json` 中只写密钥名称。
