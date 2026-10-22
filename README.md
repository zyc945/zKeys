# zKeys

`zkey` 管理个人加密密钥库，`zwork` 将选定密钥注入 Zellij 会话。支持项目级 `.zkey.json` 映射。

保持本地、手动解锁的使用方式：一个 `vault.json` 保存一组相关密钥，跨设备复制这个密文文件即可迁移数据。目标设备仍需安装工具和依赖；不提供自动同步、冲突合并或后台解锁。

## 安装

当前正式版本使用 Go，保留 Python 版已有命令和 v1 keystore 格式。目标设备不需要 Python、pip、虚拟环境、Go 或单独的加密库。`zwork` 仍需安装 Zellij；从 Git 子目录发现项目配置时需要 Git。支持 macOS / Linux，不支持 Windows。

### 下载二进制

从 [GitHub Releases](https://github.com/zyc945/zKeys/releases) 下载对应压缩包及 `SHA256SUMS`。仓库为私有时，需要登录有权限的 GitHub 账户。

| 设备 | 压缩包平台后缀 |
| --- | --- |
| Linux x86_64 | `linux-amd64` |
| Linux aarch64 / ARM64 | `linux-arm64` |
| Mac Intel | `darwin-amd64` |
| Mac Apple Silicon | `darwin-arm64` |

使用 `uname -s` 和 `uname -m` 确认设备类型。不同平台需要不同二进制，`vault.json` 通用。每个压缩包包含 `zkey`、`zwork` 和 README。

例如下载 `zkeys-v0.3.0-darwin-arm64.tar.gz` 后，在下载目录校验（Linux 可将 `shasum -a 256` 换成 `sha256sum`）：

```bash
shasum -a 256 zkeys-v0.3.0-darwin-arm64.tar.gz
```

确认 SHA-256 摘要与 `SHA256SUMS` 中对应文件的摘要一致，再解压：

```bash
tar -xzf zkeys-v0.3.0-darwin-arm64.tar.gz
```

校验和用于检查下载完整性，下载来源仍应为该仓库的 Release。若浏览器下载的文件被 macOS Gatekeeper 拦截，先按系统提示确认来源并允许运行，不要关闭系统安全机制。

### 安装或升级

在解压目录中，以日常用户执行，不加 sudo。先等待正在执行的 `zkey` / `zwork` 命令结束。下面先移除旧命令入口，因此既兼容 Python 版的符号链接，也兼容 Mac 的 shell 启动文件；不会沿旧链接覆盖 Python 源码：

```bash
(
  set -eu
  install -d -m 700 "$HOME/.config/z-keystore" "$HOME/.local/bin"
  rm -f "$HOME/.local/bin/zkey" "$HOME/.local/bin/zwork"
  install -m 700 zkey "$HOME/.local/bin/zkey"
  install -m 700 zwork "$HOME/.local/bin/zwork"
  install -m 600 README.md "$HOME/.config/z-keystore/README.md"
)
export PATH="$HOME/.local/bin:$PATH"
```

安装不会初始化或替换 `vault.json`，也不会删除 `.lock`、旧 Python 文件或 `.venv`。Python 版已有库直接使用，**不要重新运行 `init`**。升级前可按备份章节保存密文；不需要结束已运行的 Zellij 服务，新命令生效也不会刷新旧会话的环境。

将 PATH 配置加入自己的 shell 启动文件（Mac 通常为 `~/.zshrc`）即可长期生效。只使用 `zkey` 时不需要 Zellij。使用 `zwork` 时，确保 `~/.local/bin/zellij` 是已安装 Zellij 的可执行文件或链接：

```bash
# 替换为 command -v zellij 显示的实际路径；目标已存在时先检查，不覆盖。
ln -s /absolute/path/to/zellij "$HOME/.local/bin/zellij"
zkey --help
zwork --help
"$HOME/.local/bin/zellij" --version
```

Apple Silicon Homebrew 常见路径为 `/opt/homebrew/bin/zellij`。不需要为本工具改写系统 Python，也不需要激活任何虚拟环境。

### 从源码构建

构建机需要 `go.mod` 指定的 Go 工具链（可由 Go 自动下载）；使用者运行预编译包时无需安装 Go。所有依赖固定在 `go.mod` / `go.sum` 中：

```bash
git clone https://github.com/zyc945/zKeys.git
cd zKeys
CGO_ENABLED=0 go build -trimpath -o zkey ./cmd/zkey
CGO_ENABLED=0 go build -trimpath -o zwork ./cmd/zwork
```

然后按上面的安装步骤安装。发布四个平台的包：

```bash
./scripts/build-release.sh v0.3.0
```

产物位于 `dist/v0.3.0/`，包含四个压缩包和 `SHA256SUMS`；已存在的发布目录会被拒绝覆盖。Linux 产物关闭 CGO，不依赖目标设备的 glibc 或 OpenSSL；macOS 仍使用操作系统提供的系统库。构建脚本不会发布或部署。

## 使用

个人加密密钥库。运行 Go 二进制无需额外语言运行时或加密依赖。默认数据目录为 `~/.config/z-keystore`，密钥名称和值一起加密保存到 `vault.json`。目录权限 700，数据和锁文件 600。使用 Scrypt（N=32768、r=8、p=1）派生 AES-256-GCM 密钥，每次保存生成新 salt 和 nonce，写入使用文件锁与原子替换。

首次使用，请你本人在交互终端运行，设置主密码，无硬性长度限制，建议使用较长的随机口令。主密码不回显，不接受命令参数或环境变量。忘记主密码无法恢复。

```bash
zkey init
```

如果是迁移已有密钥库，跳过 `init`，按下文导入 `vault.json`。远程操作时使用交互 SSH 终端，例如 `ssh -t user@host.example`，再运行上述命令。

以下命令在安装工具的机器执行，若 PATH 尚未更新，用 `~/.local/bin/zkey` 替代 `zkey`。

### 命令与参数

以下语法中，`[]` 表示可选参数，`...` 表示可重复；这些符号不需要原样输入。

| 命令 | 参数及行为 | 示例 |
| --- | --- | --- |
| `zkey init` | 无参数；输入两次新主密码，已有库时拒绝覆盖 | `zkey init` |
| `zkey set NAME [--stdin]` | `NAME` 为库中名称；默认隐藏输入值，`--stdin` 从标准输入读取完整文本；覆盖同名条目 | `zkey set work/token` |
| `zkey import-env NAME...` | 至少一个已 export 的变量名，不可重复；保存为同名条目，不接受 `ENV=KEY` 映射或变量值 | `zkey import-env API_TOKEN SERVICE_TOKEN` |
| `zkey get NAME` | 读取一个条目，将明文原样输出到 stdout，不追加换行 | `zkey get work/token` |
| `zkey list` | 无参数；解锁后仅列出名称，按名称排序 | `zkey list` |
| `zkey passwd` | 无参数；先输入当前密码，再输入两次新密码 | `zkey passwd` |
| `zkey run --env ENV=KEY ... -- COMMAND [ARG...]` | `--env` 至少一次，可重复；`ENV` 是目标环境变量，`KEY` 是库中名称，`ENV` 不可重复；`--` 后是程序及其参数 | `zkey run --env API_TOKEN=work/token -- python3 app.py` |
| `zkey -h` / `zkey --help` | 查看帮助，无需解锁；子命令也支持 `--help` | `zkey import-env --help` |

库中名称 `NAME` / `KEY` 长度为 1–128 个字符，以英文字母或数字开头，其余只允许英文字母、数字、`_ . : / -`。`run` 的 `ENV` 以英文字母或下划线开头，其余只允许英文字母、数字和下划线。`import-env` 的名称必须同时满足两种规则，因此只能以英文字母开头。值必须是非空 UTF-8 文本，不能包含 NUL；允许多行。

`--env API_TOKEN=work/token` 也可写为 `--env=API_TOKEN=work/token`。所有命令的主密码都从交互终端读取，不支持密码参数。下面给出常用组合示例。

```bash
# 输入主密码，再隐藏输入密钥值。已有同名项会被替换。
zkey set OPENAI_API_KEY

# 将当前终端已 export 的指定变量保存为同名条目，只输入一次主密码。
zkey import-env OPENAI_API_KEY ANTHROPIC_API_KEY

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

`import-env` 需要 zkey v0.3.0 或更新版本（Go 实现）。它只读取明确指定的变量，已有同名条目会被替换，其他条目保留。任一变量未 export、为空或不是有效 UTF-8 文本时，在解锁前停止，整个密钥库不写入。全部有效后只输入一次主密码，并将本批条目一次性写入；只报告数量，不输出值。变量名称需以字母开头，只含字母、数字和下划线，最长 128 个字符；不接受以下划线开头的名称。请在持有这些变量的终端中执行，不要将值作为命令参数。

批量导入示例：如果变量已在当前 shell 中赋值，先将其 export（不会打印值），再明确列出需要保存的名称：

```bash
export API_TOKEN SERVICE_TOKEN REGION
zkey import-env API_TOKEN SERVICE_TOKEN REGION

# 只检查库中的名称，不输出值。
zkey list
```

上例假定变量已有非空值；`export NAME` 不会自动生成值。命令只读取当前终端传给进程的环境，不读取 `.env` 文件，也不自动加载 shell 配置。导入完成后，重新运行相同命令可更新这些条目；其他条目保持不变。

Python 程序使用 `run` 注入的变量即可：

```python
import os
api_key = os.environ['OPENAI_API_KEY']
# 在程序内部使用，避免打印 api_key。
```

`run` 不通过 shell 执行命令，会在完成解锁并释放锁后直接替换 keystore 进程。目标程序继承原有环境，加上你明确选择的密钥；退出状态和信号按目标程序处理。主密码和整库解密结果不会由一个等待程序结束的 keystore 父进程长期保留。不创建明文临时文件，也不提供后台解锁服务或密码缓存。每次命令重新输入主密码。

Agent 优先使用 `run` 启动需要密钥的代码。请在你自己控制的终端输入主密码，不要把主密码发送到 Agent 对话。不要让 Agent 调用 `get` 并把输出带入工具记录或日志。无交互终端时，工具拒绝读取主密码，不降级为明文输入。程序若主动打印环境变量或密钥，工具无法阻止泄露。

此工具用于保护静态密钥，不能保证对 root、具有 Docker 管理权限的用户或同账号恶意进程保密。解锁后目标程序能够读取、复制获授权的密钥，子进程也可能继承；程序退出不等于凭证被撤销。Go 不保证所有秘密内存副本被安全擦除；工具将 core 文件大小上限设为 0（目标程序继承此限制），但不能保证系统级崩溃收集器不采集内存，也无法控制 swap、休眠、管理员调试或所有程序日志。

文本形式的 SSH 私钥可通过 `set --stdin` 保存，但工具不提供 SSH agent，也不会自动配置 SSH 登录。当前值是文本，不支持任意二进制文件或包含 NUL 字符的内容。

## zellij 会话入口 zwork

### 参数与示例

```text
zwork SESSION [KEY | ENV=KEY ...]
zwork -h
zwork --help
```

| 参数 | 含义 | 示例 |
| --- | --- | --- |
| `SESSION` | 必填会话名；以英文字母或数字开头，其余只允许英文字母、数字、`_ . -` | `dev-agent` |
| `KEY` | 可选库中名称，同时作为环境变量名；必须满足两种名称规则 | `API_TOKEN` |
| `ENV=KEY` | 可选映射；将库中 `KEY` 注入环境变量 `ENV`；多个参数的 `ENV` 不可重复 | `API_TOKEN=work/token` |
| `-h` / `--help` | 显示帮助，无需解锁 | `zwork --help` |

```bash
# 新建会话，显式选择两个同名条目。
zwork dev-agent API_TOKEN SERVICE_TOKEN

# 新建另一个会话，使用不同的环境变量名称。
zwork dev-mapped API_TOKEN=work/token

# 重连已存在的活跃会话，不重复解锁，不再指定密钥。
zwork dev-agent

# 在含 .zkey.json 的项目中，新会话从配置读取映射。
zwork project-agent
```

### 项目配置

项目根目录可保存 `.zkey.json`，只保存环境变量到密钥名称的映射，不填写密钥值：

```json
{
  "env": {
    "OPENAI_API_KEY": "work/openai",
    "GITHUB_TOKEN": "work/github"
  }
}
```

| 配置项 | 含义 | 示例 |
| --- | --- | --- |
| `env` | 唯一允许的顶层字段，必须为非空对象 | `{ "env": { "API_TOKEN": "work/token" } }` |
| `env` 中的字段名 | 注入会话的环境变量名，规则与 `zkey run` 的 `ENV` 相同 | `API_TOKEN` |
| `env` 中的字符串值 | keystore 中已有的密钥名称，不是密钥值 | `work/token` |

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

先等待所有 `zkey` 命令结束，备份期间不要执行 `init`、`set`、`import-env` 或 `passwd`。已启动的 Zellij 会话可以继续运行，它不需要持续读取密钥库。以下命令创建独立备份目录，不覆盖旧备份：

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

指定一台设备作为主要修改端，其他设备按需复制最新密文。两个设备各自执行 `set`、`import-env` 或 `passwd` 会产生独立版本；工具不自动同步或合并，复制整个文件会替换全部内容。不要把它当成多人或多端并发编辑的数据库。

## 验证与 Python 兼容性

Python 源码 `z_keystore.py`、`zwork.py` 保留为格式参照和回退入口，不参与 Go 版运行。Python 版仍需 `requirements.txt` 中的依赖；回退时可用已安装该依赖的解释器显式运行旧脚本。Python 参考版不支持新增的 `import-env` 命令；两种实现均可直接读写 v1 文件，改密码后另一实现使用新密码即可。

运行 Go 单元测试和静态检查：

```bash
go test ./...
go vet ./...
```

运行原有 Python 测试及 Go/Python 真实进程兼容测试（测试解释器需要 cryptography；ZKEY_GO 指向当前机器的 Go 可执行文件）：

```bash
CGO_ENABLED=0 go build -o /tmp/zkey-compat ./cmd/zkey
ZKEY_GO=/tmp/zkey-compat python3 -m unittest discover -s tests -v
```

可选的真实 Zellij 会话测试（只创建和清理唯一命名的测试会话）：

```bash
CGO_ENABLED=0 go build -o /tmp/zwork-compat ./cmd/zwork
ZKEY_GO=/tmp/zkey-compat ZWORK_GO=/tmp/zwork-compat ZELLIJ_BIN="$HOME/.local/bin/zellij" python3 -m unittest discover -s tests -p test_zellij.py -v
```

测试只使用临时 HOME、虚拟口令和虚拟密钥。跨语言用例覆盖双向创建/读取、改写/改密码、多行与中文、空/短口令、错误密码/篡改、权限/链接、无终端拒绝、Ctrl-C 回显恢复，以及 run 的 PID/退出码/PATH/锁释放。测试环境必须允许创建伪终端并打开 `/dev/tty`；不设置 `ZKEY_GO` 时跳过跨语言测试。

发布验证边界：`darwin-arm64` 与 `linux-amd64` 实际运行测试及双向文件互通；`darwin-amd64` 与 `linux-arm64` 交叉编译并检查产物，未在对应硬件运行。Zellij 实际会话验证使用 macOS 0.45.0、Linux 0.45.1。二进制依赖的最低系统版本以构建所用 Go 工具链支持范围为准。

仓库仅保存源码、说明和测试，二进制通过 Release 分发。不要提交 `vault.json`、主密码、实际密钥或含密钥的环境文件；`.zkey.json` 中只写密钥名称。
