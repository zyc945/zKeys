#!/usr/bin/python3
"""Small encrypted personal key store. Passwords are read only from /dev/tty."""
import argparse
import base64
from contextlib import contextmanager
import fcntl
import getpass
import json
import os
from pathlib import Path
import re
import resource
import stat
import sys
import tempfile
import warnings

from cryptography.exceptions import InvalidTag
from cryptography.hazmat.primitives.ciphers.aead import AESGCM
from cryptography.hazmat.primitives.kdf.scrypt import Scrypt

AAD = b'z-keystore:v1:scrypt-32768-8-1:aes-256-gcm'
NAME = re.compile(r'[A-Za-z0-9][A-Za-z0-9_.:/-]{0,127}\Z')
ENV_NAME = re.compile(r'[A-Za-z_][A-Za-z0-9_]*\Z')


class StoreError(Exception):
    pass


def hidden(prompt):
    try:
        with open('/dev/tty', 'w') as tty:
            if not tty.isatty():
                raise StoreError('需要交互终端；请在自己的 SSH 终端中运行。')
            with warnings.catch_warnings():
                warnings.simplefilter('error', getpass.GetPassWarning)
                return getpass.getpass(prompt, stream=tty)
    except (OSError, getpass.GetPassWarning, EOFError):
        raise StoreError('需要交互终端；请在自己的 SSH 终端中运行。') from None


def new_password():
    password = hidden('设置主密码（不回显）: ')
    if password != hidden('再次输入主密码: '):
        raise StoreError('两次主密码不一致。')
    return password


def check_name(name):
    if not NAME.fullmatch(name):
        raise StoreError('密钥名称须为 1–128 个字母、数字或 _ . : / -，以字母或数字开头。')
    return name


def derive(password, salt):
    return Scrypt(salt=salt, length=32, n=32768, r=8, p=1).derive(password.encode('utf-8'))


class Store:
    def __init__(self, directory):
        self.directory = Path(directory)
        self.path = self.directory / 'vault.json'

    def prepare(self):
        try:
            self.directory.mkdir(mode=0o700)
        except FileExistsError:
            pass
        info = self.directory.lstat()
        if not stat.S_ISDIR(info.st_mode) or info.st_uid != os.getuid():
            raise StoreError('密钥库目录必须为当前用户所有的真实目录。')
        if stat.S_IMODE(info.st_mode) != 0o700:
            raise StoreError('密钥库目录权限必须为 700。')

    def open_private(self, path, flags):
        fd = os.open(path, flags | os.O_NOFOLLOW | os.O_NONBLOCK, 0o600)
        info = os.fstat(fd)
        if (not stat.S_ISREG(info.st_mode) or info.st_uid != os.getuid()
                or stat.S_IMODE(info.st_mode) != 0o600 or info.st_nlink != 1):
            os.close(fd)
            raise StoreError('密钥库文件必须为当前用户所有、权限 600 的普通文件，且不可有硬链接。')
        return fd

    @contextmanager
    def locked(self):
        self.prepare()
        fd = self.open_private(self.directory / '.lock', os.O_RDWR | os.O_CREAT)
        try:
            fcntl.flock(fd, fcntl.LOCK_EX)
            yield self
        finally:
            os.close(fd)

    def read(self, password):
        try:
            fd = self.open_private(self.path, os.O_RDONLY)
        except FileNotFoundError:
            raise StoreError('尚未初始化，请先运行 zkey init。') from None
        try:
            with os.fdopen(fd) as stream:
                envelope = json.load(stream)
            if set(envelope) != {'version', 'salt', 'nonce', 'ciphertext'} or envelope['version'] != 1:
                raise ValueError()
            salt, nonce, ciphertext = [base64.b64decode(envelope[key], validate=True)
                                       for key in ('salt', 'nonce', 'ciphertext')]
            if len(salt) != 16 or len(nonce) != 12:
                raise ValueError()
            entries = json.loads(AESGCM(derive(password, salt)).decrypt(nonce, ciphertext, AAD))
            if (not isinstance(entries, dict) or any(not isinstance(k, str) or not NAME.fullmatch(k)
                    or not isinstance(v, str) or '\0' in v for k, v in entries.items())):
                raise ValueError()
            return entries
        except (ValueError, TypeError, KeyError, InvalidTag, UnicodeError):
            raise StoreError('无法解锁：主密码错误或密钥库已损坏。') from None

    def write(self, entries, password):
        salt, nonce = os.urandom(16), os.urandom(12)
        ciphertext = AESGCM(derive(password, salt)).encrypt(
            nonce, json.dumps(entries, ensure_ascii=False).encode('utf-8'), AAD)
        envelope = dict(version=1, **{k: base64.b64encode(v).decode('ascii')
                        for k, v in [('salt', salt), ('nonce', nonce), ('ciphertext', ciphertext)]})
        fd, temporary = tempfile.mkstemp(prefix='.vault-', dir=self.directory)
        try:
            os.fchmod(fd, 0o600)
            with os.fdopen(fd, 'w') as stream:
                json.dump(envelope, stream)
                stream.write('\n')
                stream.flush()
                os.fsync(stream.fileno())
            os.replace(temporary, self.path)
            directory_fd = os.open(self.directory, os.O_RDONLY | os.O_DIRECTORY)
            try:
                os.fsync(directory_fd)
            finally:
                os.close(directory_fd)
        finally:
            if os.path.lexists(temporary):
                os.unlink(temporary)

    def initialize(self, password):
        if os.path.lexists(self.path):
            raise StoreError('密钥库已经存在，拒绝覆盖。')
        self.write({}, password)


def command_parser():
    parser = argparse.ArgumentParser(description='加密密钥库；主密码仅从交互终端读取。')
    sub = parser.add_subparsers(dest='action', required=True)
    sub.add_parser('init', help='设置主密码并创建空密钥库')
    setter = sub.add_parser('set', help='写入或替换一项密钥，默认隐藏输入')
    setter.add_argument('name')
    setter.add_argument('--stdin', action='store_true', help='从标准输入读取值（保留所有换行）')
    getter = sub.add_parser('get', help='把一项密钥原样写到 stdout；注意避免泄露到日志')
    getter.add_argument('name')
    sub.add_parser('list', help='解锁后仅列出名称')
    sub.add_parser('passwd', help='更改主密码')
    runner = sub.add_parser('run', help='将指定密钥注入子进程环境，直接运行命令')
    runner.add_argument('--env', action='append', required=True, metavar='ENV=NAME')
    runner.add_argument('command', nargs=argparse.REMAINDER)
    return parser


def main(argv=None, directory=None):
    args = command_parser().parse_args(argv)
    resource.setrlimit(resource.RLIMIT_CORE, (0, 0))
    store = Store(directory or Path.home() / '.config' / 'z-keystore')
    if args.action in ('set', 'get'):
        check_name(args.name)
    bindings = {}
    if args.action == 'run':
        for binding in args.env:
            env, sep, key = binding.partition('=')
            if not sep or not ENV_NAME.fullmatch(env) or env in bindings:
                raise StoreError('--env 必须为 ENV=NAME，且环境变量名不可重复。')
            bindings[env] = check_name(key)
        if args.command[:1] == ['--']:
            args.command = args.command[1:]
        if not args.command:
            raise StoreError('run 需要在 -- 后提供命令。')
    child_env = None
    with store.locked():
        if args.action == 'init':
            if os.path.lexists(store.path):
                raise StoreError('密钥库已经存在，拒绝覆盖。')
            store.initialize(new_password())
            print('密钥库已创建。请保管主密码；忘记后无法恢复。', file=sys.stderr)
            return 0
        password = hidden('主密码（不回显）: ')
        entries = store.read(password)
        if args.action == 'set':
            value = sys.stdin.read() if args.stdin else hidden('密钥值（不回显）: ')
            if not value or '\0' in value:
                raise StoreError('密钥值不可为空或包含 NUL。')
            entries[args.name] = value
            store.write(entries, password)
            print('密钥已保存。', file=sys.stderr)
        elif args.action == 'get':
            if args.name not in entries:
                raise StoreError('密钥不存在。')
            sys.stdout.write(entries[args.name])
        elif args.action == 'list':
            for name in sorted(entries):
                print(name)
        elif args.action == 'passwd':
            store.write(entries, new_password())
            print('主密码已更新。旧备份仍使用旧密码。', file=sys.stderr)
        elif args.action == 'run':
            if any(name not in entries for name in bindings.values()):
                raise StoreError('请求的密钥不存在。')
            child_env = os.environ.copy()
            child_env.update({env: entries[name] for env, name in bindings.items()})
    if child_env is not None:
        os.execvpe(args.command[0], args.command, child_env)
    return 0


def entrypoint():
    try:
        return main()
    except (StoreError, OSError) as error:
        # OS exception text can include command arguments; never print it.
        message = str(error) if isinstance(error, StoreError) else '操作失败，请检查路径、权限或目标命令。'
        print('zkey: ' + message, file=sys.stderr)
        return 1
    except KeyboardInterrupt:
        print('\nzkey: 已取消。', file=sys.stderr)
        return 130


if __name__ == '__main__':
    sys.exit(entrypoint())
