# 命令、参数与示例

[返回 README](../README.md) · [备份与迁移](migration.md) · [文件格式](format.md)

本文描述 v0.5.0 及当前源码新增的 `export`（尚未发布，需从源码构建）。`delete`、`--version` 和改进的 zwork 路径查找自此版本提供。

个人加密密钥库。运行 Go 二进制无需额外语言运行时或加密依赖。默认数据目录为 `~/.config/z-keystore`，密钥名称和值一起加密保存到 `vault.json`。目录权限 700，数据和锁文件 600。使用 Scrypt（N=32768、r=8、p=1）派生 AES-256-GCM 密钥，每次保存生成新 salt 和 nonce，写入使用文件锁与原子替换。

首次使用，请你本人在交互终端运行，设置主密码，无硬性长度限制，建议使用较长的随机口令。主密码不回显，不接受命令参数或环境变量。忘记主密码无法恢复。

```bash
zkey init
```

如果是迁移已有密钥库，跳过 `init`，按[迁移步骤](migration.md#跨设备迁移与恢复)导入 `vault.json`。远程操作时使用交互 SSH 终端，例如 `ssh -t user@host.example`，再运行上述命令。

以下命令在安装工具的机器执行，若 PATH 尚未更新，用 `~/.local/bin/zkey` 替代 `zkey`。

### 命令与参数

以下语法中，`[]` 表示可选参数，`...` 表示可重复；这些符号不需要原样输入。

| 命令 | 参数及行为 | 示例 |
| --- | --- | --- |
| `zkey init` | 无参数；输入两次新主密码，已有库时拒绝覆盖 | `zkey init` |
| `zkey set NAME [--stdin]` | `NAME` 为库中名称；默认隐藏输入值，`--stdin` 从标准输入读取完整文本；覆盖同名条目 | `zkey set work/token` |
| `zkey import-env NAME...` | 至少一个已 export 的变量名，不可重复；保存为同名条目，不接受 `ENV=KEY` 映射或变量值 | `zkey import-env API_TOKEN SERVICE_TOKEN` |
| `zkey export NAME... --out PATH` | 至少一个不重复的已存在名称；必填输出文件，父目录须存在，拒绝覆盖；原库保留，新库单独设置密码；源码新增 | `zkey export work/token ssh/work --out ./project-vault.json` |
| `zkey delete NAME` | 删除指定条目；显示名称并交互确认，不显示值，不提供强制删除参数 | `zkey delete work/token` |
| `zkey get NAME` | 读取一个条目，将明文原样输出到 stdout，不追加换行 | `zkey get work/token` |
| `zkey list` | 无参数；解锁后仅列出名称，按名称排序 | `zkey list` |
| `zkey passwd` | 无参数；先输入当前密码，再输入两次新密码 | `zkey passwd` |
| `zkey run --env ENV=KEY ... -- COMMAND [ARG...]` | `--env` 至少一次，可重复；`ENV` 是目标环境变量，`KEY` 是库中名称，`ENV` 不可重复；`--` 后是程序及其参数 | `zkey run --env API_TOKEN=work/token -- python3 app.py` |
| `zkey ssh import/export` | 加密保存和恢复指定 SSH 配置及密钥，参数见下文 SSH 章节 | `zkey ssh import work --host server-a` |
| `zkey --version` | 输出 `zkey VERSION`，无需解锁；源码开发构建为 `zkey dev` | `zkey --version` |
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

# 删除前先显示指定名称并确认；取消时保留原库。
zkey delete work/token

# 更改主密码，旧备份仍需旧密码解锁。
zkey passwd
```

`delete` 解锁后显示条目名称，输入 `y` 或 `yes`（忽略大小写）确认；回车或其他输入取消，确认输入也不回显。只删除指定条目，确认后沿用文件锁与原子写入，其他条目保留。取消、条目不存在或密码错误时不改写库；没有批量删除或 `--force` 参数。删除 `ssh/NAME` 只删除库内快照，已经导出的 SSH 配置和私钥文件仍然存在；备份中的旧数据也仍然存在。

`import-env` 需要 zkey v0.3.0 或更新版本（Go 实现）。它只读取明确指定的变量，已有同名条目会被替换，其他条目保留。任一变量未 export、为空或不是有效 UTF-8 文本时，在解锁前停止，整个密钥库不写入。全部有效后只输入一次主密码，并将本批条目一次性写入；只报告数量，不输出值。变量名称需以字母开头，只含字母、数字和下划线，最长 128 个字符；不接受以下划线开头的名称。请在持有这些变量的终端中执行，不要将值作为命令参数。

批量导入示例：如果变量已在当前 shell 中赋值，先将其 export（不会打印值），再明确列出需要保存的名称：

```bash
export API_TOKEN SERVICE_TOKEN REGION
zkey import-env API_TOKEN SERVICE_TOKEN REGION

# 只检查库中的名称，不输出值。
zkey list
```

上例假定变量已有非空值；`export NAME` 不会自动生成值。命令只读取当前终端传给进程的环境，不读取 `.env` 文件，也不自动加载 shell 配置。导入完成后，重新运行相同命令可更新这些条目；其他条目保持不变。

### 导出部分条目

```bash
# NAME 是库内名称，不是当前 shell 的环境变量。
zkey export work/api work/private-key --out ./project-vault.json

# 参数可放在名称前；--out=PATH 与 --out PATH 等价。
zkey export --out=./ssh-vault.json ssh/work
```

先输入原库主密码，全部名称存在后，为新库输入两次主密码；新旧密码可不同，没有长度限制。只有明确指定的条目进入新库，名称及值一起加密；多行文本和 SSH 快照原样保留。`ssh/work` 按一个完整条目复制，与 `zkey ssh export work` 释放 SSH 配置/私钥到磁盘的行为不同。

`--out` 必须且只能指定一次，不支持 stdout（`-`）、覆盖、通配符或自动删除。名称重复/无效、缺少参数、条目不存在、原密码错误、新密码不一致或输入被取消时失败，不改写原库，不生成目标库。目标已存在时（普通文件、目录、符号链接或原库本身）拒绝覆盖；目标父目录须已存在，命令不会创建父目录。写入先生成权限 `600` 的临时密文，再以不可覆盖方式发布到目标路径；目标所在文件系统须支持硬链接，失败时不会退回覆盖写入。不生成明文中间文件，正常结束会移除临时密文；强制终止可能留下临时密文，不应将它当作成功导出的库。

只报告成功导出的数量，不输出值。源库所有条目保持不变。迁移时将新文件作为目标设备的 `vault.json` 使用，并输入新密码；详见[跨设备迁移](migration.md#跨设备迁移与恢复)。忘记新密码时可以从仍保留的原库重新导出，但无法恢复已忘记的密码。

Python 程序使用 `run` 注入的变量即可：

```python
import os
api_key = os.environ['OPENAI_API_KEY']
# 在程序内部使用，避免打印 api_key。
```

`run` 不通过 shell 执行命令，会在完成解锁并释放锁后直接替换 keystore 进程。目标程序继承原有环境，加上你明确选择的密钥；退出状态和信号按目标程序处理。主密码和整库解密结果不会由一个等待程序结束的 keystore 父进程长期保留。不创建明文临时文件，也不提供后台解锁服务或密码缓存。每次命令重新输入主密码。

仅对你信任且允许读取这些密钥的目标程序使用 `run`；如果目标是 Agent 或其代码、子进程，它可以读取所注入的条目。请在你自己控制的终端输入主密码，不要把主密码发送到 Agent 对话。不要让 Agent 调用 `get` 并把输出带入工具记录或日志。无交互终端时，工具拒绝读取主密码，不降级为明文输入。程序若主动打印环境变量或密钥，工具无法阻止泄露。

此工具用于保护静态密钥，不能保证对 root、具有 Docker 管理权限的用户或同账号恶意进程保密。解锁后目标程序能够读取、复制获授权的密钥，子进程也可能继承；程序退出不等于凭证被撤销。Go 不保证所有秘密内存副本被安全擦除；工具将 core 文件大小上限设为 0（目标程序继承此限制），但不能保证系统级崩溃收集器不采集内存，也无法控制 swap、休眠、管理员调试或所有程序日志。

单个文本形式的 SSH 私钥仍可通过 `set --stdin` 保存。需要连同连接配置一起迁移时，使用下文的 `ssh import/export`。工具不提供 SSH agent。普通条目只支持文本，不支持任意二进制文件或包含 NUL 字符的内容。

## zellij 会话入口 zwork

### 参数与示例

```text
zwork SESSION [KEY | ENV=KEY ...]
zwork -h
zwork --help
zwork --version
```

| 参数 | 含义 | 示例 |
| --- | --- | --- |
| `SESSION` | 必填会话名；以英文字母或数字开头，其余只允许英文字母、数字、`_ . -` | `dev-agent` |
| `KEY` | 可选库中名称，同时作为环境变量名；必须满足两种名称规则 | `API_TOKEN` |
| `ENV=KEY` | 可选映射；将库中 `KEY` 注入环境变量 `ENV`；多个参数的 `ENV` 不可重复 | `API_TOKEN=work/token` |
| `-h` / `--help` | 显示帮助，无需解锁 | `zwork --help` |
| `--version` | 输出 `zwork VERSION`，无需解锁；源码开发构建为 `zwork dev` | `zwork --version` |

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

注入的密钥可被会话内继承环境的程序读取，插件也应视为该会话的信任范围；不要运行不可信插件或打印环境变量。关闭 session 并不撤销服务端凭证，脱离 session 生存的进程也可能保留密钥。

### 可执行文件查找与信任范围

以下查找行为适用于 v0.5.0；旧版 v0.4.0 固定使用 `~/.local/bin/zkey` 和 `~/.local/bin/zellij`。

- `zkey`：先解析实际 `zwork` 可执行文件的符号链接，优先使用其所在目录的 `zkey`；只有该项不存在时才兼容回退到 `~/.local/bin/zkey`。因此将两个二进制放在同一目录即可使用，不从 PATH 随意选择 `zkey`。
- Zellij：优先使用旧位置 `~/.local/bin/zellij`；只有该项不存在时才从 PATH 查找。保留已有安装和链接的优先级，也支持 Homebrew 或系统包管理器直接安装。
- 找到的项必须解析为可执行的普通文件；坏链接、非普通文件或不可执行文件会明确报错，不再回退。拒绝 PATH 查找产生的相对路径结果。
- 新建会话在读取主密码前检查所需程序；重连已有活跃会话只需 Zellij，不要求 `zkey` 存在。

这些检查用于发现安装错误，二进制所在目录、符号链接目标和 PATH 本身都属于信任范围。它们不能防御同账号恶意进程或 root 篡改程序。请只使用自己信任的安装目录和 PATH。

如果会话显示 EXITED，zwork 会拒绝直接恢复，请使用新的会话名，通过项目配置或显式参数重新解锁创建。断开 SSH 或 detach 不会清除活跃会话中的密钥；只有结束会话及相关进程才能停止其继续保留。

## SSH 配置与密钥迁移

需要 zkey v0.4.0 或更新版本。只处理明确选择的连接及它们引用的跳板别名；不自动打包整个 `.ssh`，不连接服务器、不执行代理命令、不提供 SSH agent。SSH bundle 存在普通条目 `ssh/NAME` 中，其内容为带类型和版本的 JSON 字符串；外层 keystore v1 格式不变，原有 Python 参考实现仍能读写该库。

### 命令与参数

```text
zkey ssh import NAME --host HOST [--host HOST...] [--config PATH]
zkey ssh export NAME [--with-private-keys] [--install-config]
zkey ssh --help
```

| 参数 | 用途与默认值 | 示例 |
| --- | --- | --- |
| `NAME` | bundle 名称，1–64 个英文字母、数字、`_ . -`，以字母或数字开头；不能含 `/` | `work` |
| `import --host HOST` | 必填，明确选择配置中的具体主机别名；可重复选择不同别名 | `--host server-a --host server-b` |
| `import --config PATH` | 源 SSH 配置文件；默认 `~/.ssh/config` | `--config "$HOME/.ssh/work-config"` |
| `export --with-private-keys` | 明确允许将私钥写入目标磁盘；bundle 含私钥而未指定时，整个导出拒绝执行 | `--with-private-keys` |
| `export --install-config` | 将生成配置接入 `~/.ssh/config`，仅允许该文件不存在或为空；已有非空配置时整批停止 | `--install-config` |

导入和导出各输入一次 keystore 主密码。导入不覆盖已有的 `ssh/NAME` 条目；重新保存快照时使用新名称，或先通过 `zkey delete ssh/NAME` 明确删除旧快照后再导入。导出不覆盖已有 `~/.ssh/zkeys/NAME` 目录，发生冲突时使用另一个 bundle 名称或先自行备份并移走旧目录；没有强制覆盖参数。

### 导入示例

假设源配置包含如下静态连接；实际密钥文件需已经存在：

```sshconfig
Host jump-a
    HostName jump.example.com
    User deploy
    Port 22
    IdentityFile ~/.ssh/work_ed25519

Host server-a
    HostName 192.0.2.10
    User deploy
    Port 2222
    IdentityFile ~/.ssh/work_ed25519
    ProxyJump jump-a
    IdentitiesOnly yes
```

```bash
# 保存 server-a 及其引用的 jump-a；共享的文件只存一份。
zkey ssh import work --host server-a

# 从另一个配置文件选择多个连接，保存为独立快照。
zkey ssh import work-next --config "$HOME/.ssh/work-config" --host server-a --host server-b

# 只检查条目名称，应包含 ssh/work；不要用 get 打印 bundle。
zkey list
```

导入保留所引用私钥的原始字节、存在的公钥/证书伴随文件，以及显式引用的完整 known_hosts 文件。不会删除源配置或源密钥。已有私钥口令不会移除或保存到额外字段；将来 SSH 认证时可能仍需要该口令，它与 keystore 主密码相互独立。

### 在另一台设备恢复

先按[跨设备迁移步骤](migration.md#跨设备迁移与恢复)复制加密 `vault.json` 并安装兼容版本，不能重新 `init`。然后在目标设备执行：

```bash
# 适合新设备：配置不存在或为空时，恢复文件并接入主配置。
zkey ssh export work --with-private-keys --install-config
ssh server-a
```

导出目录如下；文件名由工具生成，不按包内原始路径写入目标设备：

```text
~/.ssh/zkeys/work/
├── config
└── files/
    ├── f0001
    └── ...
```

生成的 `IdentityFile`、`CertificateFile`、`UserKnownHostsFile` 路径会指向目标目录。目录权限 `700`、文件权限 `600`；HOME 必须属于当前用户且不可被组或其他用户写入，已有 `.ssh` 和 `zkeys` 目录必须为 `700`。拒绝这些目录为符号链接，以及已存在的 bundle 目录。HOME 路径不得包含 `%`、`${...}`、反斜线或 glob 字符，避免 OpenSSH 路径展开。`--install-config` 遇到原有空配置时先备份；遇到非空配置时不会发布私钥目录或修改主配置。导出不会改写 keystore。

已有正常使用的主配置时，先仅导出到独立目录：

```bash
zkey ssh export work --with-private-keys
ssh -F "$HOME/.ssh/zkeys/work/config" server-a
```

此时 `~/.ssh/config` 保持不变。如需用普通 `ssh server-a`，自行备份并审查已有配置后，在合适位置添加：

```sshconfig
Include ~/.ssh/zkeys/work/config
```

OpenSSH 多数选项采用首次匹配的值，`Host *` 和 Include 的位置会影响行为；工具不自动合并已有规则。导出成功只说明文件恢复完成，不代表目标网络、代理程序和远端授权一定可用。

### 第一版支持范围与限制

- 支持具体别名的 `Host`、同一块多个具体别名，以及独立的 `Host *` 默认块。按源文件顺序处理默认值；`IdentityFile`、`CertificateFile` 可累加，`ProxyJump` 与 `ProxyCommand` 按支持范围内的 OpenSSH 优先级处理。`ProxyJump none` 后接 `ProxyCommand` 在不同 OpenSSH 版本中行为不同，第一版明确拒绝此组合；请将连接配置明确为一种代理方式。
- 所选连接必须能从指定配置文件取得明确的 `HostName`、`User` 和文件型 `IdentityFile`；端口未指定时使用 22。不自动读取 `/etc/ssh/ssh_config` 或发现隐式默认私钥。
- `ProxyJump` 支持逗号分隔的配置别名和 `none`；递归包含引用的别名，缺失或循环依赖拒绝导入。第一版不支持在跳板参数中直接写 `user@host:port`，请定义为独立 Host。
- 支持的静态选项为 `HostName`、`User`、`Port`、`IdentityFile`、`CertificateFile`、`ProxyJump`、`ProxyCommand`、`IdentitiesOnly`、`ServerAliveInterval`、`ServerAliveCountMax`、`HostKeyAlias`、`CheckHostIP`、`StrictHostKeyChecking`、`UserKnownHostsFile`、`BatchMode`、`PasswordAuthentication`、`KbdInteractiveAuthentication`、`PubkeyAuthentication`、`PreferredAuthentications`、`ConnectTimeout`、`ConnectionAttempts`、`AddressFamily`、`Compression`。时间/计数选项仅支持整数；字段值还需符合对应的静态格式。
- `ProxyCommand` 只保存单行原文，不执行，也不打包它引用的程序或脚本；目标设备需要自行具备相同依赖。
- 不支持 `Include`、`Match`、复杂 Host 模式、动态路径 token 或环境变量展开；遇到不支持的指令明确报错，不静默忽略。密钥文件路径支持绝对路径和 `~/`；不支持相对路径、`~其他用户`、`%` 或 `${...}` 展开。整个指定配置文件都会检查；即使不支持的指令位于未选择的 Host 块，也会停止导入。
- 保留显式的主机身份校验设置及指纹文件。源配置若使用 `/dev/null` 或 `none`，不会把它当作可信指纹文件，也不会自动增加信任或关闭校验。
- 只迁移本地可读的文件型密钥；不迁移硬件令牌内的密钥、SSH agent 内存状态、登录密码或服务端 `authorized_keys`。

私钥导出后受目标文件系统保护；如果原私钥没有口令，写出的就是明文私钥。共享服务器上只恢复确实需要的连接，`600` 权限无法防御 root 或同账号恶意进程。配置、主机信息与公钥也会以普通文件形式落盘。工具不会将私钥放入环境变量或输出到终端；不要对 `ssh/NAME` 使用会输出完整内容的 `zkey get`。


## 验证与 Python 兼容性

Python 源码 `z_keystore.py`、`zwork.py` 保留为格式参照和兼容验证入口，不参与 Go 版运行。Python 版仍需 `requirements.txt` 中的依赖；需要运行参考实现时，可用已安装该依赖的解释器显式运行旧脚本。Go 是唯一新增功能实现；Python 参考版不支持新增的 `import-env`、`export`、`ssh`、`delete` 或 `--version`，不追求两套实现的功能同步，也不删除已有参考代码或测试。两种实现均可直接读写 v1 文件，改密码后另一实现使用新密码即可。

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

`tests/test_export.py` 通过真实伪终端验证选择性导出、Python 回读、Go 重新读取/导出、空/短密码、错误密码、缺失条目、密码不一致、取消、无终端和拒绝覆盖；检查原库内容及 inode/mtime 保持不变，目标权限为 `600`。Go 测试另验证并发导出同一路径只有一个成功。 已实际验证 macOS ARM64 导出 → Linux x86_64 解锁并用新密码再次导出 → macOS 解锁，Go 与 Python 均可读取；全程使用虚拟数据，原库不变。

SSH 导入导出集成测试只使用临时 HOME 和生成的测试密钥。设置 `ZKEY_SSHD` 后，还会以当前用户启动随机回环端口的临时 SSH 服务，检查直连与 ProxyJump 认证；不修改系统 SSH 服务：

```bash
ZKEY_GO=/tmp/zkey-compat ZKEY_SSHD=/usr/sbin/sshd python3 -m unittest discover -s tests -p test_ssh.py -v
```

需要 `ssh`、`ssh-keygen`，真实认证测试还需要可用的 `sshd`。不设置 `ZKEY_SSHD` 时跳过真实服务测试，文件迁移测试仍运行。SSH 测试覆盖原始密钥/证书/指纹文件保留、私钥导出开关、配置优先级、跳板依赖、冲突、权限和异常路径；测试输出不包含私钥。已实际进行测试 keystore 的 Mac → Linux → Mac 往返复制，并用 Python 回读/改写后由 Go 导出。

发布验证边界：`darwin-arm64` 与 `linux-amd64` 实际运行测试及双向文件互通；`darwin-amd64` 与 `linux-arm64` 交叉编译并检查产物，未在对应硬件运行。Zellij 实际会话验证使用 macOS 0.45.0、Linux 0.45.1。二进制依赖的最低系统版本以构建所用 Go 工具链支持范围为准。



### CI 与固定格式向量

源码仓库中的 `.github/workflows/test.yml` 在 push、pull request 或手动触发时，分别使用 `ubuntu-latest` 和 `macos-latest` 运行原生二进制。每个任务记录实际 `uname`、`GOOS`、`GOARCH` 和 Go 版本，执行 `go test -race ./...`、`go vet ./...`、原有 Python 测试及 Go/Python 兼容测试；不能据此推断其他交叉编译架构已实际运行。

CI 必须具备 `ssh`、`ssh-keygen` 和 `/usr/sbin/sshd`，SSH 真实认证为必跑项，缺少依赖会失败。服务只监听随机回环端口，使用生成的测试密钥和临时 HOME；不读取真实 keystore 或 SSH 配置。测试还需要可用的伪终端和 `/dev/tty`。

Zellij 是可选依赖：runner 上有可用程序时设置 `ZELLIJ_BIN` 并运行真实会话测试；没有时在任务摘要和 unittest 结果中明确报告跳过。跳过不表示通过。工作流已配置不代表远端运行已经成功；以具体提交对应的 Actions 结果为准。

源码仓库中的固定 v1 测试向量 `tests/fixtures/keystore-v1.json` 包含公开虚拟口令、预先确定的密文和预期条目，分别由 Go 的 `internal/keystore/vectors_test.go` 和 Python 的 `tests/test_format_vectors.py` 验证。覆盖中文、多行、空字符串条目、空密码，以及错误密码与字段篡改拒绝。固定 salt/nonce 只用于兼容测试，绝不可复用于真实密钥加密。
