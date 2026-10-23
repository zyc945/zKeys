"""Real-process interoperability checks; run with ZKEY_GO=/path/to/zkey."""
import base64
import errno
import fcntl
import json
import os
from pathlib import Path
import pty
import re
import select
import signal
import stat
import struct
import subprocess
import sys
import tempfile
import termios
import time
import unittest

import z_keystore as z

PASSWORD = 'fake-test-中文-password'
SECRET = 'fake-selected-secret\n第二行\n'
UNSELECTED = 'fake-unselected-secret-only'
ROOT = Path(__file__).resolve().parents[1]


class GoCompatibilityTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        binary = os.environ.get('ZKEY_GO')
        if not binary:
            raise unittest.SkipTest('set ZKEY_GO to the built Go zkey binary')
        cls.go = [str(Path(binary).resolve())]
        if not os.path.isfile(cls.go[0]):
            raise unittest.SkipTest('ZKEY_GO binary does not exist')
        cls.python = [sys.executable, str(ROOT / 'z_keystore.py')]

    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory(prefix='zkey-go-test-')
        self.addCleanup(self.temporary.cleanup)
        self.home = Path(self.temporary.name) / 'home'
        self.home.mkdir()
        (self.home / '.config').mkdir()
        self.store = z.Store(self.home / '.config' / 'z-keystore')

    def environment(self, home=None):
        # Do not inherit the operator's credentials or other secret variables.
        return {'HOME': str(home or self.home), 'PATH': os.defpath,
                'LANG': 'en_US.UTF-8', 'PYTHONUTF8': '1', 'TERM': 'xterm'}

    def seed(self, entries=None, password=PASSWORD, store=None):
        store = store or self.store
        with store.locked():
            store.initialize(password)
            if entries:
                store.write(entries, password)

    def interactive(self, command, args, passwords=(), stdin=None, home=None,
                    interrupt=False, immediate=False, environment=None, secret_answers=None):
        master, slave = pty.openpty()
        fcntl.ioctl(slave, termios.TIOCSWINSZ, struct.pack('HHHH', 24, 100, 0, 0))

        def terminal_session():
            os.setsid()
            fcntl.ioctl(slave, termios.TIOCSCTTY, 0)

        process = None
        streams = {}
        output = {'stdout': bytearray(), 'stderr': bytearray(), 'terminal': bytearray()}
        pending = bytearray()
        answers = iter(passwords)
        next_answer = next(answers, None)
        try:
            process = subprocess.Popen(
                command + args, stdin=subprocess.PIPE if stdin is not None else slave,
                stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                env=environment if environment is not None else self.environment(home),
                preexec_fn=terminal_session,
                pass_fds=(slave,), cwd=ROOT)
            os.close(slave)
            slave = None
            streams = {master: 'terminal', process.stdout.fileno(): 'stdout',
                       process.stderr.fileno(): 'stderr'}
            if stdin is not None:
                process.stdin.write(stdin.encode('utf-8'))
                process.stdin.close()
            deadline = time.monotonic() + 20
            while streams:
                if time.monotonic() >= deadline:
                    self.fail('PTY command timed out: ' + ' '.join(args[:2]))
                ready, _, _ = select.select(list(streams), [], [], 0.05)
                for fd in ready:
                    try:
                        chunk = os.read(fd, 65536)
                    except OSError as error:
                        if error.errno != errno.EIO:
                            raise
                        chunk = b''
                    if not chunk:
                        del streams[fd]
                        continue
                    output[streams[fd]].extend(chunk)
                    if streams[fd] == 'terminal':
                        pending.extend(chunk)
                prompt = re.search(rb'(?:: |\xef\xbc\x9a ?)$', pending)
                echo = bool(termios.tcgetattr(master)[3] & termios.ECHO)
                if (next_answer is not None or interrupt) and master in streams and prompt:
                    if immediate:
                        self.assertFalse(echo, 'password prompt appeared before echo was disabled')
                    elif echo:
                        continue
                    os.write(master, b'\x03' if interrupt else (next_answer + '\n').encode('utf-8'))
                    pending.clear()
                    if interrupt:
                        interrupt = False
                    else:
                        next_answer = next(answers, None)
            result = subprocess.CompletedProcess(command + args, process.wait(timeout=1),
                                                 bytes(output['stdout']), bytes(output['stderr']))
            result.terminal = bytes(output['terminal'])
            result.pid = process.pid
            result.echo = bool(termios.tcgetattr(master)[3] & termios.ECHO)
            for password in passwords if secret_answers is None else secret_answers:
                if password:
                    self.assertNotIn(password.encode('utf-8'), result.terminal)
                    self.assertNotIn(password.encode('utf-8'), result.stderr)
                    self.assertNotIn(password.encode('utf-8'), result.stdout)
            return result
        finally:
            if process is not None:
                if process.poll() is None:
                    os.killpg(process.pid, signal.SIGKILL)
                process.wait()
                for stream in (process.stdin, process.stdout, process.stderr):
                    if stream is not None:
                        stream.close()
            os.close(master)
            if slave is not None:
                os.close(slave)

    def success(self, *args, **kwargs):
        result = self.interactive(*args, **kwargs)
        self.assertEqual(result.returncode, 0, result.stderr.decode('utf-8', 'replace'))
        return result

    def test_python_write_go_list_get_set_and_change_password(self):
        self.seed({'work/key': SECRET, 'another': UNSELECTED})
        result = self.success(self.go, ['list'], [PASSWORD])
        self.assertEqual(result.stdout, b'another\nwork/key\n')
        for value in (SECRET, UNSELECTED):
            self.assertNotIn(value.encode('utf-8'), result.stdout + result.stderr + result.terminal)
        self.assertEqual(self.success(self.go, ['get', 'work/key'], [PASSWORD]).stdout,
                         SECRET.encode('utf-8'))
        changed = 'fake-go-value\n多行\n\n'
        self.success(self.go, ['set', 'from/go', '--stdin'], [PASSWORD], stdin=changed)
        with self.store.locked():
            self.assertEqual(self.store.read(PASSWORD)['from/go'], changed)
        new_password = 'fake-changed-中文-password'
        self.success(self.go, ['passwd'], [PASSWORD, new_password, new_password])
        with self.store.locked():
            self.assertEqual(self.store.read(new_password),
                             {'work/key': SECRET, 'another': UNSELECTED, 'from/go': changed})
            with self.assertRaises(z.StoreError):
                self.store.read(PASSWORD)

    def test_go_init_and_set_python_read_with_unrestricted_passwords(self):
        for index, password in enumerate(('', 'x', PASSWORD)):
            with self.subTest(password=password):
                home = self.home / str(index)
                (home / '.config').mkdir(parents=True)
                store = z.Store(home / '.config' / 'z-keystore')
                self.success(self.go, ['init'], [password, password], home=home)
                self.success(self.go, ['set', 'work/key', '--stdin'], [password],
                             stdin=SECRET, home=home)
                with store.locked():
                    self.assertEqual(store.read(password), {'work/key': SECRET})
                result = self.success(self.python, ['get', 'work/key'], [password], home=home)
                self.assertEqual(result.stdout, SECRET.encode('utf-8'))
                self.assertEqual(stat.S_IMODE(store.directory.stat().st_mode), 0o700)
                self.assertEqual(stat.S_IMODE(store.path.stat().st_mode), 0o600)

    def test_hidden_set_does_not_echo_value(self):
        self.seed()
        value = 'fake-hidden-value-only'
        result = self.success(self.go, ['set', 'hidden'], [PASSWORD, value])
        self.assertNotIn(value.encode(), result.stdout + result.stderr + result.terminal)
        with self.store.locked():
            self.assertEqual(self.store.read(PASSWORD), {'hidden': value})

    def test_import_env_merges_only_selected_values_with_one_password(self):
        self.seed({'PRESENT': 'fake-old-value', 'preserved': UNSELECTED})
        environment = self.environment()
        environment.update({'PRESENT': SECRET, 'SECOND': 'fake-second-value',
                            'NOT_SELECTED': 'fake-unselected-environment'})
        result = self.success(self.go, ['import-env', 'PRESENT', 'SECOND'], [PASSWORD],
                              environment=environment)
        self.assertEqual(result.stdout, b'')
        self.assertEqual(result.terminal.count('主密码'.encode('utf-8')), 1)
        for value in (SECRET, 'fake-second-value', 'fake-unselected-environment'):
            self.assertNotIn(value.encode('utf-8'),
                             result.stdout + result.stderr + result.terminal)
        with self.store.locked():
            self.assertEqual(self.store.read(PASSWORD),
                             {'PRESENT': SECRET, 'SECOND': 'fake-second-value',
                              'preserved': UNSELECTED})
        self.assertEqual(stat.S_IMODE(self.store.path.stat().st_mode), 0o600)

    def test_import_env_missing_empty_or_invalid_aborts_before_password_or_write(self):
        self.seed({'preserved': UNSELECTED})
        before = self.store.path.read_bytes()
        for value in (None, '', 'fake-invalid-\udcff-text'):
            with self.subTest(value_kind='missing' if value is None else 'present'):
                environment = self.environment()
                environment['FIRST'] = SECRET
                if value is not None:
                    environment['SECOND'] = value
                # Holding the vault lock also proves validation happens before locking.
                with self.store.locked():
                    result = subprocess.run(self.go + ['import-env', 'FIRST', 'SECOND'],
                                            stdin=subprocess.DEVNULL, capture_output=True,
                                            env=environment, start_new_session=True, timeout=5)
                self.assertEqual(result.returncode, 1, result.stderr)
                self.assertEqual(result.stdout, b'')
                self.assertIn(b'SECOND', result.stderr)
                self.assertNotIn('主密码'.encode('utf-8'), result.stderr)
                self.assertNotIn(SECRET.encode('utf-8'), result.stderr)
                self.assertNotIn(b'fake-invalid-', result.stderr)
                self.assertEqual(self.store.path.read_bytes(), before)

    def test_import_env_wrong_password_leaves_vault_unchanged(self):
        self.seed({'preserved': UNSELECTED})
        before = self.store.path.read_bytes()
        environment = self.environment()
        environment['TOKEN'] = SECRET
        result = self.interactive(self.go, ['import-env', 'TOKEN'], ['fake-wrong-password'],
                                  environment=environment)
        self.assertEqual(result.returncode, 1)
        self.assertEqual(result.stdout, b'')
        self.assertNotIn(SECRET.encode('utf-8'), result.stderr + result.terminal)
        self.assertEqual(self.store.path.read_bytes(), before)

    def test_run_exec_selects_only_requested_key_and_preserves_exit_code(self):
        self.seed({'one': SECRET, 'two': UNSELECTED})
        script = ('import fcntl,json,os,sys; '
                  'fd=os.open(' + repr(str(self.store.directory / '.lock')) + ',os.O_RDWR); '
                  'fcntl.flock(fd,fcntl.LOCK_EX|fcntl.LOCK_NB); '
                  'print(json.dumps({"token":os.environ["TOKEN"],'
                  '"unselected":' + repr(UNSELECTED) + ' in os.environ.values(),'
                  '"key_exported":"two" in os.environ,"pid":os.getpid(),'
                  '"args":sys.argv[1:]})); sys.exit(23)')
        for command in (self.go, self.python):
            with self.subTest(command=Path(command[0]).name):
                result = self.interactive(command, ['run', '--env', 'TOKEN=one', '--',
                                          sys.executable, '-c', script, 'literal;argument'], [PASSWORD])
                self.assertEqual(result.returncode, 23, result.stderr)
                self.assertEqual(json.loads(result.stdout),
                                 {'token': SECRET, 'unselected': False, 'key_exported': False,
                                  'pid': result.pid, 'args': ['literal;argument']})
                self.assertNotIn(UNSELECTED.encode(), result.stdout + result.stderr + result.terminal)
                self.assertNotIn(SECRET.encode(), result.stderr + result.terminal)

    def test_run_searches_in_injected_path(self):
        bindir = self.home / 'bin'
        bindir.mkdir()
        executable = bindir / 'fake-zkey-probe'
        executable.write_text('#!' + sys.executable + '\nimport json,os\n'
                              'print(json.dumps([os.environ["PATH"],os.getpid()]))\n')
        executable.chmod(0o700)
        self.seed({'search/path': str(bindir)})
        for command in (self.go, self.python):
            result = self.success(command, ['run', '--env', 'PATH=search/path', '--',
                                           executable.name], [PASSWORD])
            self.assertEqual(json.loads(result.stdout), [str(bindir), result.pid])

    def test_ctrl_c_returns_130_and_restores_terminal_echo(self):
        self.seed({'work/key': SECRET})
        for command in (self.go, self.python):
            with self.subTest(command=Path(command[0]).name):
                result = self.interactive(command, ['get', 'work/key'], interrupt=True)
                self.assertEqual(result.returncode, 130, result.stderr)
                self.assertTrue(result.echo, 'terminal echo was not restored after Ctrl-C')
                self.assertEqual(result.stdout, b'')

    def test_prompt_is_ready_for_immediate_input_and_ctrl_c(self):
        for index in range(3):
            with self.subTest(attempt=index):
                home = self.home / str(index)
                (home / '.config').mkdir(parents=True)
                result = self.success(self.go, ['init'], [PASSWORD, PASSWORD],
                                      home=home, immediate=True)
                self.assertTrue(result.echo)
                store = z.Store(home / '.config' / 'z-keystore')
                with store.locked():
                    store.write({'work/key': SECRET}, PASSWORD)
                result = self.success(self.go, ['get', 'work/key'], [PASSWORD],
                                      home=home, immediate=True)
                self.assertEqual(result.stdout, SECRET.encode('utf-8'))
                self.assertTrue(result.echo)
                result = self.interactive(self.go, ['get', 'work/key'], home=home,
                                          interrupt=True, immediate=True)
                self.assertEqual(result.returncode, 130, result.stderr)
                self.assertTrue(result.echo)
                self.assertEqual(result.stdout, b'')

    def test_set_and_get_help_do_not_require_password(self):
        for command in (self.go, self.python):
            for action in ('set', 'get'):
                with self.subTest(command=Path(command[0]).name, action=action):
                    result = subprocess.run(command + [action, 'work/key', '--help'],
                                            stdin=subprocess.DEVNULL, capture_output=True,
                                            env=self.environment(), start_new_session=True,
                                            timeout=10)
                    self.assertEqual(result.returncode, 0, result.stderr)
                    self.assertTrue(result.stdout)
                    self.assertFalse(self.store.directory.exists())

    def test_run_forwards_target_help_argument(self):
        self.seed({'one': SECRET})
        script = 'import json,sys; print(json.dumps(sys.argv[1:]))'
        for command in (self.go, self.python):
            result = self.success(command, ['run', '--env', 'TOKEN=one', '--',
                                           sys.executable, '-c', script, '--help'], [PASSWORD])
            self.assertEqual(json.loads(result.stdout), ['--help'])

    def test_no_controlling_tty_rejects_stdin_password(self):
        self.seed({'work/key': SECRET})
        for command in (self.go, self.python):
            with self.subTest(command=Path(command[0]).name):
                result = subprocess.run(command + ['get', 'work/key'],
                                        input=(PASSWORD + '\n').encode('utf-8'), capture_output=True,
                                        env=self.environment(), start_new_session=True, timeout=10)
                self.assertEqual(result.returncode, 1)
                self.assertEqual(result.stdout, b'')
                self.assertNotIn(SECRET.encode('utf-8'), result.stderr)
                self.assertNotIn(PASSWORD.encode('utf-8'), result.stderr)

    def test_wrong_password_and_tampering_fail_without_rewrite(self):
        self.seed({'work/key': SECRET})
        before = self.store.path.read_bytes()
        for command in (self.go, self.python):
            result = self.interactive(command, ['get', 'work/key'], ['fake-wrong-password'])
            self.assertEqual(result.returncode, 1)
            self.assertEqual(result.stdout, b'')
            self.assertEqual(self.store.path.read_bytes(), before)
        envelope = json.loads(before)
        ciphertext = bytearray(base64.b64decode(envelope['ciphertext']))
        ciphertext[-1] ^= 1
        envelope['ciphertext'] = base64.b64encode(ciphertext).decode('ascii')
        self.store.path.write_text(json.dumps(envelope))
        tampered = self.store.path.read_bytes()
        for command in (self.go, self.python):
            result = self.interactive(command, ['get', 'work/key'], [PASSWORD])
            self.assertEqual(result.returncode, 1)
            self.assertEqual(result.stdout, b'')
            self.assertEqual(self.store.path.read_bytes(), tampered)

    def test_unsafe_permissions_and_links_are_rejected(self):
        cases = ('directory-mode', 'vault-mode', 'lock-mode', 'directory-symlink',
                 'vault-symlink', 'vault-hardlink', 'lock-symlink', 'lock-hardlink')
        for case in cases:
            with self.subTest(case=case):
                home = self.home / case
                (home / '.config').mkdir(parents=True)
                store = z.Store(home / '.config' / 'z-keystore')
                self.seed({'work/key': SECRET}, store=store)
                target = store.directory if case.startswith('directory') else (
                    store.directory / '.lock' if case.startswith('lock') else store.path)
                original = store.path.read_bytes()
                if case.endswith('mode'):
                    target.chmod(0o755 if target == store.directory else 0o644)
                elif case.endswith('symlink'):
                    backup = target.with_name(target.name + '.original')
                    target.rename(backup)
                    target.symlink_to(backup)
                else:
                    os.link(target, target.with_name(target.name + '.link'))
                for command in (self.go, self.python):
                    result = self.interactive(command, ['get', 'work/key'], [PASSWORD], home=home)
                    self.assertEqual(result.returncode, 1, case)
                    self.assertEqual(result.stdout, b'')
                    self.assertEqual(store.path.read_bytes(), original)

    def test_init_refuses_to_overwrite_existing_vault(self):
        self.seed({'work/key': SECRET})
        before = self.store.path.read_bytes()
        for command in (self.go, self.python):
            result = self.interactive(command, ['init'])
            self.assertEqual(result.returncode, 1)
            self.assertEqual(result.stdout, b'')
            self.assertEqual(self.store.path.read_bytes(), before)


if __name__ == '__main__':
    unittest.main()
