# 备份与迁移

[返回 README](../README.md) · [完整命令](commands.md) · [文件格式](format.md)

## 备份

只需备份完整的 `~/.config/z-keystore/vault.json`；它包含加密的名称和值，以及解密所需的版本、salt 和 nonce。加密算法参数由兼容版本的工具定义，建议同时记录源码版本（在源码目录运行 `git rev-parse HEAD`）。主密码单独保管，不放进备份目录。

先等待所有 `zkey` 命令结束，备份期间不要执行 `init`、`set`、`import-env`、`import`、`ssh import`、`delete` 或 `passwd`。已启动的 Zellij 会话可以继续运行，它不需要持续读取密钥库。以下命令创建独立备份目录，不覆盖旧备份：

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

当前每个用户使用固定路径 `~/.config/z-keystore/vault.json`，没有多库切换功能。复制 vault 文件迁移的是加密数据，不是运行中的会话。SSH 连接配置需在目标设备显式执行 `ssh export` 恢复；不修改远端服务器的 `authorized_keys`。

macOS 和 Linux 使用相同的 v1 文件格式，不绑定用户名、UID 或设备。已用虚拟密钥验证 macOS 写入 → Linux 解锁并重新保存 → macOS 解锁，包含中文口令和多行文本；无需转换密文。两端分别安装工具，只复制 `vault.json`。

若目标机器已有库且希望保留原有条目，可使用源码新增的 [`zkey import PATH`](commands.md#合并导入条目) 合并（v0.6.0 发布二进制尚不支持）；遇到任何同名条目整次失败，不覆盖。下面的步骤用于安装整个库文件，不是合并导入。

### 1. 准备目标设备

按 [README 安装步骤](../README.md#安装)安装相同或兼容版本的工具和依赖，以目标用户操作。**不要运行 `zkey init`**。源端暂停密钥库写入，目标端停止所有 `zkey` 操作。

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

指定一台设备作为主要修改端，其他设备按需复制最新密文。两个设备各自执行 `set`、`import-env`、`ssh import`、`delete` 或 `passwd` 会产生独立版本；工具不自动同步或合并，复制整个文件会替换全部内容。不要把它当成多人或多端并发编辑的数据库。

## SSH 配置与密钥恢复

先按上文迁移 `vault.json`，并用 `zkey list` 验证解锁。SSH 快照与普通条目一起保存在密文内；复制密文不会自动写出私钥。原始私钥若有自己的口令，SSH 认证时仍需该口令。

新设备的 `~/.ssh/config` 不存在或为空时，可恢复并接入主配置：

```bash
zkey ssh export work --with-private-keys --install-config
ssh server-a
```

已有主配置时，先导出到独立目录并指定它使用：

```bash
zkey ssh export work --with-private-keys
ssh -F "$HOME/.ssh/zkeys/work/config" server-a
```

导出目录为 `~/.ssh/zkeys/work/`；目录为 `700`，文件为 `600`，已有目录不会覆盖。HOME 必须属于当前用户且不可被组或其他用户写入，已有 `.ssh` 和 `zkeys` 目录必须为 `700`。工具不会自动修改不满足要求的目录权限。已有非空主配置时，`--install-config` 整批停止；原主配置为空时会先备份它。

恢复不会删除源密钥、不修改远端 `authorized_keys`。连接还取决于网络、远端授权及 `ProxyCommand` 的本机依赖；成功导出不代表已经验证 SSH 登录。完整参数、配置合并注意事项和静态语法限制见 [SSH 命令](commands.md#ssh-配置与密钥迁移)。

删除 `ssh/work` 只删除库内快照；已经导出的文件和旧备份需自行管理，凭证撤销需在授权它的服务端进行。
