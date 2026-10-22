#!/usr/bin/python3
"""Create a key-injected Zellij session, or reconnect without unlocking."""
import argparse
import json
import os
from pathlib import Path
import re
import subprocess
import sys


def make_bindings(keys, parser):
    bindings = []
    variables = set()
    for key in keys:
        env, separator, name = key.partition('=')
        if not separator:
            name = env
        if (not re.fullmatch(r'[A-Za-z_][A-Za-z0-9_]*', env)
                or not re.fullmatch(r'[A-Za-z0-9][A-Za-z0-9_.:/-]{0,127}', name)
                or env in variables):
            parser.error('密钥参数须为 KEY 或 ENV=KEY，且环境变量名不可重复。')
        variables.add(env)
        bindings.extend(['--env', env + '=' + name])
    return bindings


def unique_object(pairs):
    result = {}
    for key, value in pairs:
        if key in result:
            raise ValueError('duplicate JSON key')
        result[key] = value
    return result


def project_keys(parser):
    directory = Path.cwd()
    try:
        result = subprocess.run(['git', 'rev-parse', '--show-toplevel'],
                                capture_output=True, text=True)
        if result.returncode == 0 and result.stdout.strip():
            directory = Path(result.stdout.strip())
    except FileNotFoundError:
        pass
    path = directory / '.zkey.json'
    try:
        config = json.loads(path.read_text(encoding='utf-8'), object_pairs_hook=unique_object)
    except FileNotFoundError:
        parser.error('新建会话需要项目 .zkey.json 或显式密钥参数，例如：zwork work OPENAI_API_KEY')
    except (OSError, ValueError, UnicodeError):
        parser.error('无法读取 .zkey.json：请检查文件权限、JSON 格式及重复字段。')
    if (not isinstance(config, dict) or set(config) != {'env'}
            or not isinstance(config['env'], dict) or not config['env']
            or any(not isinstance(value, str) for value in config['env'].values())):
        parser.error('.zkey.json 必须仅包含非空 env 对象，值为密钥名称字符串。')
    keys = [env + '=' + name for env, name in config['env'].items()]
    bindings = make_bindings(keys, parser)
    print('项目密钥配置：' + str(path), file=sys.stderr)
    for env, name in config['env'].items():
        print('  ' + env + ' <- ' + name, file=sys.stderr)
    return bindings


def main(argv=None):
    parser = argparse.ArgumentParser(description='首次解锁并创建 zellij 会话；已有会话直接连接。')
    parser.add_argument('session', help='会话名')
    parser.add_argument('keys', nargs='*', metavar='KEY', help='密钥名称或 ENV=密钥名称；新建时省略则读取项目 .zkey.json，显式参数优先')
    args = parser.parse_args(argv)
    if not re.fullmatch(r'[A-Za-z0-9][A-Za-z0-9_.-]*', args.session):
        parser.error('会话名只能使用字母、数字、点、横线和下划线，且以字母或数字开头。')
    bindings = make_bindings(args.keys, parser)
    bindir = Path.home() / '.local/bin'
    zellij = str(bindir / 'zellij')
    result = subprocess.run([zellij, 'list-sessions', '--no-formatting'], capture_output=True, text=True)
    if result.returncode and not (result.returncode == 1 and not result.stdout.strip()
            and 'No active zellij sessions' in result.stderr):
        parser.error('无法读取 zellij 会话列表；请先运行 zellij list-sessions 检查。')
    records = [line for line in result.stdout.splitlines()
               if line.split() and line.split()[0] == args.session]
    if any(line.endswith('(EXITED - attach to resurrect)') for line in records):
        parser.error('该会话已退出，仅有恢复记录。请使用新的会话名，通过项目配置或显式密钥参数重新解锁创建。')
    existing = bool(records)
    if existing:
        if bindings:
            parser.error('会话已存在，不能重新注入密钥。请去掉密钥参数以连接，或使用新的会话名。')
        os.execv(zellij, [zellij, 'attach', args.session])
    else:
        if not bindings:
            bindings = project_keys(parser)
        zkey = str(bindir / 'zkey')
        os.execv(zkey, [zkey, 'run'] + bindings + ['--', zellij, '--session', args.session])
    return 0


if __name__ == '__main__':
    try:
        sys.exit(main())
    except OSError:
        print('zwork: 无法启动工具，请检查 ~/.local/bin/zkey 和 zellij。', file=sys.stderr)
        sys.exit(1)
