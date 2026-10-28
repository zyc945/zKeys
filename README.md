# zKeys

`zkey` 管理个人加密密钥库，`zwork` 将选定密钥注入 Zellij 会话。支持项目级 `.zkey.json` 映射。

保持本地、手动解锁的使用方式：一个 `vault.json` 保存一组相关密钥，跨设备复制这个密文文件即可迁移数据。目标设备仍需安装工具和依赖；不提供自动同步、冲突合并或后台解锁。

## 安装

v0.7.0 新增 `zkey import PATH`，将独立密钥库合并到当前库；同名条目整次失败，保留导入文件和目标库原密码，keystore v1 格式保持不变。

当前正式版本使用 Go，保留 Python 版已有命令和 v1 keystore 格式。目标设备不需要 Python、pip、虚拟环境、Go 或单独的加密库。`zwork` 仍需安装 Zellij；从 Git 子目录发现项目配置时需要 Git。支持 macOS / Linux，不支持 Windows。

### 下载二进制

从 [GitHub Releases](https://github.com/zyc945/zKeys/releases) 下载对应压缩包及 `SHA256SUMS`。仓库为私有时，需要登录有权限的 GitHub 账户。

| 设备 | 压缩包平台后缀 |
| --- | --- |
| Linux x86_64 | `linux-amd64` |
| Linux aarch64 / ARM64 | `linux-arm64` |
| Mac Intel | `darwin-amd64` |
| Mac Apple Silicon | `darwin-arm64` |

使用 `uname -s` 和 `uname -m` 确认设备类型。不同平台需要不同二进制，`vault.json` 通用。压缩包包含 `zkey`、`zwork`、README 和 README 引用的 `docs/` 文档。

例如下载 `zkeys-v0.7.0-darwin-arm64.tar.gz` 后，在下载目录校验（Linux 可将 `shasum -a 256` 换成 `sha256sum`）：

```bash
shasum -a 256 zkeys-v0.7.0-darwin-arm64.tar.gz
```

确认 SHA-256 摘要与 `SHA256SUMS` 中对应文件的摘要一致，再解压：

```bash
tar -xzf zkeys-v0.7.0-darwin-arm64.tar.gz
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
)
export PATH="$HOME/.local/bin:$PATH"
```

安装不会初始化或替换 `vault.json`，也不会删除 `.lock`、旧 Python 文件或 `.venv`。Python 版已有库直接使用，**不要重新运行 `init`**。升级前可按[备份步骤](docs/migration.md#备份)保存密文；不需要结束已运行的 Zellij 服务，新命令生效也不会刷新旧会话的环境。

将 PATH 配置加入自己的 shell 启动文件（Mac 通常为 `~/.zshrc`）即可长期生效。只使用 `zkey` 时不需要 Zellij。`zwork` 优先使用原位置 `~/.local/bin/zellij`，仅在它不存在时从 PATH 查找；Homebrew 或系统安装通常不需要另建链接。已有可用链接继续有效。旧版 v0.4.0 要求 Zellij 位于 `~/.local/bin/zellij`，需要时可自行将它链接到已安装程序。

```bash
zkey --help
zwork --help
# 仅当依靠 PATH 查找 Zellij 时检查：
command -v zellij
zellij --version
```

zwork 优先使用实际 `zwork` 可执行文件同目录的 `zkey`；仅当它不存在时，回退到 `~/.local/bin/zkey`。依赖路径和失效链接会在新会话解锁前检查，详见[命令文档](docs/commands.md#可执行文件查找与信任范围)。不需要改写系统 Python，也不需要激活虚拟环境。

### 从源码构建

构建机需要 `go.mod` 指定的 Go 工具链（可由 Go 自动下载）；使用者运行预编译包时无需安装 Go。所有依赖固定在 `go.mod` / `go.sum` 中：

```bash
git clone https://github.com/zyc945/zKeys.git
cd zKeys
CGO_ENABLED=0 go build -trimpath -o zkey ./cmd/zkey
CGO_ENABLED=0 go build -trimpath -o zwork ./cmd/zwork
```

然后按上面的安装步骤安装。构建四个平台的包：

```bash
./scripts/build-release.sh v0.7.0
```

产物位于 `dist/v0.7.0/`，包含四个压缩包和 `SHA256SUMS`；已存在的发布目录会被拒绝覆盖。Linux 产物关闭 CGO，不依赖目标设备的 glibc 或 OpenSSL；macOS 仍使用操作系统提供的系统库。构建脚本不会发布或部署。

## 最小上手

以下示例适用于 v0.7.0。首次使用，在自己的交互终端中设置主密码。密码不回显，没有硬性长度限制；忘记密码无法恢复。已有或迁移过来的库请跳过 `init`。

```bash
zkey --version
zwork --version
zkey init
zkey set API_TOKEN
zkey run --env API_TOKEN=API_TOKEN -- ./my-program

# 启动长期会话：解锁一次，后续在会话中直接运行程序。
zwork dev-agent API_TOKEN
# 重连活跃会话，无需再次输入主密码。
zwork dev-agent
```

发布构建的版本输出为 `zkey VERSION` / `zwork VERSION`；普通源码构建为 `zkey dev` / `zwork dev`。

默认库为 `~/.config/z-keystore/vault.json`。备份或跨设备迁移只需复制这一完整密文文件，目标设备安装对应平台的工具后，用原密码解锁，不要重新初始化。目录权限为 `700`，文件为 `600`。

## 导出部分条目

`zkey export NAME... --out PATH` 将选定条目复制到独立的 v1 keystore，原库保持不变。此命令自 v0.6.0 提供。

```bash
zkey export PROJECT_API_KEY AWS_AK AWS_SK --out ./project-vault.json

# SSH 快照按整个条目复制，不拆分其中的主机或私钥。
zkey export ssh/work --out ./ssh-vault.json
```

- `NAME...`：至少一个已存在的库内名称，不可重复；不支持通配符。
- `--out PATH`：必填目标文件，也可写为 `--out=PATH`；父目录须已存在。目标已存在（包括符号链接）时拒绝覆盖，不接受 `-` 输出到终端。
- 先输入原库密码，再两次输入新库密码。均从交互终端隐藏读取，密码没有硬性长度限制。
- 新文件权限为 `600`，只含所选条目，不输出密钥值，不生成明文临时文件，不自动删除原条目。

导出的文件可按[跨设备迁移步骤](docs/migration.md#跨设备迁移与恢复)安装到目标设备的 `~/.config/z-keystore/vault.json`，使用新密码解锁；已有库先备份，不直接覆盖。导出不增加多库切换或合并功能。完整错误行为见[命令文档](docs/commands.md#导出部分条目)。

## 合并导入条目

`zkey import PATH` 将一个 v1 keystore 中的全部条目加入当前库，保留导入文件和目标库原密码。此命令自 v0.7.0 提供。

```bash
# 文件属于当前用户，权限为 600；传输后先确认权限。
chmod 600 ./project-vault.json
zkey import ./project-vault.json

# 使用目标库原密码，只查看合并后的名称。
zkey list
```

- `PATH`：一个必填文件路径；不接受 stdin、额外条目名或覆盖参数。
- 先输入当前目标库密码，再输入导入文件密码，均在交互终端隐藏输入。
- 目标库必须已初始化；任意同名条目（即使值相同）都会使整次导入失败，不写入任何条目。
- 校验通过后一次性原子保存，目标库继续使用原密码，导入文件不变；空源库报告导入 0 项且不改写目标。
- 导入文件必须为当前用户所有、权限 `600` 的普通文件，不接受符号链接、硬链接或目标库自身。SSH 快照作为完整条目合并。

不支持合并到指定的另一库路径，也不提供自动覆盖或删除。完整参数与错误行为见[命令文档](docs/commands.md#合并导入条目)。

## 安全边界

密钥名称和值一起加密保存，主密码仅从交互终端读取。优先用 `run` 或 `zwork` 将选定项注入程序，避免 `get` 的明文输出进入终端记录、Agent 对话或日志。

目标程序与同一 Zellij 会话内的程序可以读取获授权的密钥。工具不能向 Agent 提供密钥又阻止其读取，也不能防御 root、具有 Docker 管理权限的用户或同账号恶意进程；被篡改的程序可以截取密码。程序退出、断开会话和删除库内条目均不代表凭证已撤销。详细内存、文件和 SSH 限制见[命令文档](docs/commands.md)。

## 文档与支持范围

- [完整命令、参数、示例及测试](docs/commands.md)：日常操作、环境变量、项目映射、Zellij 和 SSH 支持范围。
- [备份、恢复和跨设备迁移](docs/migration.md)：保留原文件、权限和 SSH 恢复步骤。
- [文件格式与兼容约定](docs/format.md)：算法参数、恢复所需信息和 Python 参考实现边界。

Go 是唯一新增功能实现；Python 源码继续保留为 v1 格式参照和兼容验证入口，不追求功能同步。仓库仅保存源码、说明和虚拟测试数据，二进制通过 [Releases](https://github.com/zyc945/zKeys/releases) 分发。不要提交 `vault.json`、主密码、实际密钥或含密钥的环境文件；`.zkey.json` 中只写密钥名称。

安全边界与漏洞反馈方式见 [SECURITY.md](SECURITY.md)；二进制依赖的许可通知见[第三方许可证](docs/third-party-notices.md)。本项目自有代码采用 [MIT License](LICENSE)，版权署名为 `zyc945`；该授权也适用于已发布的 v0.2.0–v0.5.0。第三方组件继续遵循其各自许可。
