# zKeys

`zkey` 管理个人加密密钥库，`zwork` 将选定密钥注入 Zellij 会话。支持项目级 `.zkey.json` 映射。

保持本地、手动解锁的使用方式：一个 `vault.json` 保存一组相关密钥，跨设备复制这个密文文件即可迁移数据。目标设备仍需安装工具和依赖；不提供自动同步、冲突合并或后台解锁。

## 安装

适用于 Linux 和 macOS。依赖 Python 3 和 `requirements.txt` 中的 `cryptography`。源码默认使用 `/usr/bin/python3`；macOS 按下文使用绑定独立虚拟环境的启动文件。`zwork` 另需 Zellij 及可选的 Git。Windows 不在当前支持范围内。

先获取源码，然后在仓库根目录执行安装。私有仓库需要当前设备已有 GitHub 访问权限：

```bash
git clone https://github.com/zyc945/zKeys.git
cd zKeys
```

### Linux

例如 Debian / Ubuntu 可通过系统包安装依赖：

```bash
sudo apt-get update
sudo apt-get install python3 python3-cryptography
```

其他发行版使用对应包管理器；不要只在虚拟环境安装依赖，因为脚本固定使用 `/usr/bin/python3`。以下文件安装操作由日常使用工具的用户执行，不加 sudo：

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

### macOS

先确保已安装可用的 Python 3（可使用已有 Python 或 Homebrew Python）。不要给 Apple 系统 Python 全局安装依赖。以下命令在源码目录运行，创建专用虚拟环境与固定解释器路径的启动文件；不会改变源码、密钥库位置或加密格式。日常使用无需激活虚拟环境，也不依赖当前项目的 Python 环境：

```bash
(
  set -eu
  install -d -m 700 "$HOME/.config/z-keystore" "$HOME/.local/bin"
  python3 -m venv "$HOME/.config/z-keystore/.venv"
  "$HOME/.config/z-keystore/.venv/bin/python3" -m pip install -r requirements.txt
  "$HOME/.config/z-keystore/.venv/bin/python3" - <<'PY'
from pathlib import Path
import shlex

directory = Path.home() / '.config/z-keystore'
bindir = Path.home() / '.local/bin'
for command, filename in [('zkey', 'z_keystore.py'), ('zwork', 'zwork.py')]:
    target = directory / filename
    target.write_bytes(Path(filename).read_bytes())
    target.chmod(0o700)
    launcher = bindir / command
    if launcher.is_symlink():
        launcher.unlink()
    launcher.write_text('#!/bin/sh\nexec ' + shlex.quote(str(directory / '.venv/bin/python3'))
                        + ' ' + shlex.quote(str(target)) + ' "$@"\n')
    launcher.chmod(0o700)
PY
  install -m 600 README.md "$HOME/.config/z-keystore/README.md"
)
export PATH="$HOME/.local/bin:$PATH"
```

不要再执行 Linux 章节的同名符号链接命令。更新工具时重复本节步骤即可，不需要初始化或重建已有库。`.venv` 和启动文件属于本机安装，跨设备时不复制它们。

### PATH 与 Zellij

将 PATH 配置加入自己的 shell 启动文件（macOS 通常为 `~/.zshrc`）即可长期生效。请确保 `~/.local/bin/zellij` 是可执行文件或指向已安装 Zellij 的链接。安装命令仅安装程序及说明，不复制或初始化 `vault.json`；更新前请停用正在执行的工具命令。

若 Zellij 安装在其他位置，可建立链接（将源路径替换为实际可执行文件路径；目标已存在时先检查，不覆盖）：

```bash
ln -s /absolute/path/to/zellij "$HOME/.local/bin/zellij"
```

检查安装：

```bash
zkey --help
zwork --help
"$HOME/.local/bin/zellij" --version
```

只使用 `zkey` 时不需要安装 Zellij。Git 用于 zwork 从项目子目录发现仓库根目录；缺少 Git 时只能按当前目录查找配置。macOS 的 Apple Silicon Homebrew 通常将 Zellij 安装到 `/opt/homebrew/bin/zellij`，以当前设备 `command -v zellij` 的输出为准。

## 使用

个人加密密钥库。依赖安装步骤选定的 Python 3 和 `cryptography`。默认数据目录为 `~/.config/z-keystore`，密钥名称和值一起加密保存到 `vault.json`。目录权限 700，数据和锁文件 600。使用 Scrypt（N=32768、r=8、p=1）派生 AES-256-GCM 密钥，每次保存生成新 salt 和 nonce，写入使用文件锁与原子替换。

首次使用，请你本人在交互终端运行，设置主密码，无硬性长度限制，建议使用较长的随机口令。主密码不回显，不接受命令参数或环境变量。忘记主密码无法恢复。

```bash
zkey init
```

如果是迁移已有密钥库，跳过 `init`，按下文导入 `vault.json`。远程操作时使用交互 SSH 终端，例如 `ssh -t user@host.example`，再运行上述命令。

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

文本形式的 SSH 私钥可通过 `set --stdin` 保存，但工具不提供 SSH agent，也不会自动配置 SSH 登录。当前值是文本，不支持任意二进制文件或包含 NUL 字符的内容。

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

## 备份

只需备份完整的 `~/.config/z-keystore/vault.json`；它包含加密的名称和值，以及解密所需的版本、salt 和 nonce。加密算法参数由兼容版本的工具定义，建议同时记录源码版本（在源码目录运行 `git rev-parse HEAD`）。主密码单独保管，不放进备份目录。

先等待所有 `zkey` 命令结束，备份期间不要执行 `init`、`set` 或 `passwd`。已启动的 Zellij 会话可以继续运行，它不需要持续读取密钥库。以下命令创建独立备份目录，不覆盖旧备份：

```bash
(
  set -eu
  umask 077
  install -d -m 700 "$HOME/.local/share/zkey-backups"
  backup_dir=$(mktemp -d "$HOME/.local/share/zkey-backups/backup.XXXXXXXX")
  install -m 600 "$HOME/.config/z-keystore/vault.json" "$backup_dir/vault.json"
  cmp "$HOME/.config/z-keystore/vault.json" "$backup_dir/vault.json"
  printf '备份目录：%s\n' "$backup_dir"
)
```

`cmp` 成功时不输出内容，只确认复制一致，不验证口令或解密。另保存一份异机或离线副本，以免机器或磁盘故障导致原件与备份一起丢失。备份仍是敏感文件，权限保持为 `600`，不要提交到源码仓库。

无需备份 `.lock`、`.vault-*` 临时文件或正在运行的 Zellij 状态。`.zkey.json` 只含映射，可随项目单独保存。更改主密码只更新当前库，旧备份仍能用旧密码解锁；本工具不提供防回滚保护。

## 跨设备迁移与恢复

当前每个用户使用固定路径 `~/.config/z-keystore/vault.json`，没有多库切换功能。迁移的是密钥数据，不是运行中的会话，也不会自动部署 SSH 公钥或登录配置。

macOS 和 Linux 使用相同的 v1 文件格式，不绑定用户名、UID 或设备。已用虚拟密钥验证 macOS 写入 → Linux 解锁并重新保存 → macOS 解锁，包含中文口令和多行文本；无需转换密文。两端分别安装工具，只复制 `vault.json`。

### 1. 准备目标设备

按安装章节安装相同或兼容版本的工具和依赖，以目标用户操作。**不要运行 `zkey init`**。源端暂停密钥库写入，目标端停止所有 `zkey` 操作。

### 2. 传输密文并安装

以下命令在目标设备执行。将 `source-user@source-host` 替换为源设备的 SSH 用户与主机；远程相对路径从该用户的家目录开始。脚本遇到已有密钥库会停止，不覆盖它：

```bash
(
  set -eu
  umask 077
  install -d -m 700 "$HOME/.config/z-keystore"
  vault_path="$HOME/.config/z-keystore/vault.json"
  if [ -e "$vault_path" ] || [ -L "$vault_path" ]; then
    printf '%s\n' '目标密钥库已存在：先备份并移走旧文件，再迁移。' >&2
    exit 1
  fi
  incoming_dir=$(mktemp -d "$HOME/.config/z-keystore/incoming.XXXXXXXX")
  trap 'rm -f "$incoming_dir/vault.json"; rmdir "$incoming_dir"' EXIT
  scp source-user@source-host:.config/z-keystore/vault.json "$incoming_dir/vault.json"
  install -m 600 "$incoming_dir/vault.json" "$vault_path"
  cmp "$incoming_dir/vault.json" "$vault_path"
)
```

恢复备份采用相同步骤，将上面的 `scp` 行替换为以下命令，并填入实际备份路径：

```bash
cp /absolute/path/to/backup/vault.json "$incoming_dir/vault.json"
```

若目标已经有库，先执行备份章节并确认成功，再将旧 `vault.json` 移到安全位置后导入；不要直接覆盖，也不要删除备份。复制由目标用户执行，因此新文件归目标用户所有，不依赖两台设备的用户名或 UID 相同。目录和数据文件必须是真实目录和普通文件，不使用符号链接或硬链接。新设备的 `.lock` 会自动创建；若迁移到已有安装，保留其正常的本地锁文件，不复制源端锁文件。

### 3. 验证解锁与使用

在目标设备的交互终端执行：

```bash
zkey list
```

输入源密钥库对应的主密码，确认名称列表正确。恢复旧备份时使用该备份创建时的密码。此操作验证能否解密，不打印密钥值；不需要调用 `get`。失败时保留源文件和旧备份，不要运行 `init` 尝试修复。

确认后可用 `zkey run` 启动程序，或复制项目及其 `.zkey.json` 后运行 `zwork 新会话名`。新设备需要重新解锁创建会话。恢复或替换库不会刷新已经运行的程序环境；要使用恢复后的值，应重新启动相关程序或创建新会话。

### 4. 多设备使用约定

指定一台设备作为主要修改端，其他设备按需复制最新密文。两个设备各自执行 `set` 或 `passwd` 会产生独立版本；工具不自动同步或合并，复制整个文件会替换全部内容。不要把它当成多人或多端并发编辑的数据库。

## 测试

在仓库根目录运行（需要 cryptography；macOS 将 `/usr/bin/python3` 替换为 `"$HOME/.config/z-keystore/.venv/bin/python3"`）：

```bash
/usr/bin/python3 -m unittest discover -s tests -v
```

测试只使用临时目录、虚拟口令和虚拟密钥，不读取个人密钥库，不访问云服务，不修改现有 Zellij 会话。

兼容性验证环境：macOS 的 Python 3.9.6 / 3.9.13、cryptography 50.0.2，以及 Linux 的 Python 3.10.12、cryptography 3.4.8；两端均通过 26 项测试。macOS Zellij 0.45.0 已验证交互解锁和新 pane 的密钥继承，Linux 原有验证版本为 0.45.1。这些是已验证组合，不代表要求固定到这些旧 Python 版本。

仓库仅保存源码、说明和测试。不要提交 `vault.json`、主密码、实际密钥或含密钥的环境文件；`.zkey.json` 中只写密钥名称。
