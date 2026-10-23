"""Optional real Zellij test: set ZKEY_GO, ZWORK_GO, and ZELLIJ_BIN."""
import errno
import fcntl
import json
import os
from pathlib import Path
import pty
import select
import signal
import struct
import subprocess
import sys
import tempfile
import termios
import time
import unittest
import uuid


PASSWORD = 'fake-zellij-test-password'
SECRET = 'fake-zellij-selected-value'
VARIABLE = 'ZKEY_ZELLIJ_TEST'


class ZellijIntegrationTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        names = ('ZKEY_GO', 'ZWORK_GO', 'ZELLIJ_BIN')
        if any(not os.environ.get(name) for name in names):
            raise unittest.SkipTest('set ZKEY_GO, ZWORK_GO, and ZELLIJ_BIN for real Zellij tests')
        cls.binaries = {name: Path(os.environ[name]).resolve() for name in names}
        for name, path in cls.binaries.items():
            if not path.is_file() or not os.access(path, os.X_OK):
                raise AssertionError(name + ' must name an executable binary')

    def setUp(self):
        import z_keystore as z

        # Short paths keep Unix session sockets within macOS's path limit.
        self.temporary = tempfile.TemporaryDirectory(prefix='zkeys-zellij-', dir='/tmp')
        self.root = Path(self.temporary.name)
        self.session = 'zkt-' + uuid.uuid4().hex[:12]
        self.clients = []
        self.environment = {}
        self.addCleanup(self.cleanup_session)
        self.home = self.root / 'home'
        config = self.home / '.config'
        cache = self.home / '.cache'
        data = self.home / '.local' / 'share'
        runtime = self.root / 'runtime'
        bindir = self.home / '.local' / 'bin'
        for directory in (config, cache, data, runtime, bindir):
            directory.mkdir(parents=True, mode=0o700)
        self.environment = {
            'HOME': str(self.home), 'PATH': os.defpath, 'SHELL': '/bin/sh',
            'LANG': 'en_US.UTF-8', 'TERM': 'xterm-256color', 'TMPDIR': '/tmp',
            'XDG_CONFIG_HOME': str(config), 'XDG_CACHE_HOME': str(cache),
            'XDG_DATA_HOME': str(data), 'XDG_RUNTIME_DIR': str(runtime),
            'ZELLIJ_SOCKET_DIR': str(runtime),
        }
        (bindir / 'zkey').symlink_to(self.binaries['ZKEY_GO'])
        (bindir / 'zellij').symlink_to(self.binaries['ZELLIJ_BIN'])
        self.zellij = str(bindir / 'zellij')
        store = z.Store(config / 'z-keystore')
        with store.locked():
            store.initialize(PASSWORD)
            store.write({'work/integration': SECRET, 'unselected': 'fake-unused-value'}, PASSWORD)
        self.project = self.root / 'project'
        self.project.mkdir()
        (self.project / '.zkey.json').write_text(json.dumps({'env': {VARIABLE: 'work/integration'}}))

    def command(self, *args):
        return subprocess.run([self.zellij, *args], env=self.environment,
                              cwd=self.project, capture_output=True, timeout=5)

    def cleanup_session(self):
        try:
            if self.environment and hasattr(self, 'zellij'):
                self.command('kill-session', self.session)
                self.command('delete-session', '--force', self.session)
        finally:
            for client in self.clients:
                pid, master, _, _ = client
                os.close(master)
                if self.poll(client) is None:
                    try:
                        os.kill(pid, signal.SIGTERM)
                    except (ProcessLookupError, PermissionError):
                        pass
                    deadline = time.monotonic() + 3
                    while self.poll(client) is None and time.monotonic() < deadline:
                        time.sleep(0.05)
            self.temporary.cleanup()

    def poll(self, client):
        if client[3] is None:
            pid, status = os.waitpid(client[0], os.WNOHANG)
            if pid:
                client[3] = os.waitstatus_to_exitcode(status)
        return client[3]

    def start_client(self):
        # forkpty sets the controlling terminal and foreground process group.
        pid, master = pty.fork()
        if pid == 0:
            fcntl.ioctl(0, termios.TIOCSWINSZ, struct.pack('HHHH', 24, 100, 0, 0))
            os.chdir(self.project)
            os.execve(str(self.binaries['ZWORK_GO']),
                      ['zwork', self.session], self.environment)
        client = [pid, master, bytearray(), None]
        self.clients.append(client)
        return client

    def drain(self, client):
        _, master, output, _ = client
        if select.select([master], [], [], 0.05)[0]:
            try:
                output.extend(os.read(master, 65536))
            except OSError as error:
                if error.errno != errno.EIO:
                    raise

    def wait_for(self, client, condition, description, timeout=20):
        deadline = time.monotonic() + timeout
        while time.monotonic() < deadline:
            self.drain(client)
            if condition():
                return
            if self.poll(client) is not None:
                # The child can exit between the condition and poll calls.
                if condition():
                    return
                self.fail('zwork exited before ' + description + ': '
                          + client[2].decode('utf-8', 'replace')[-3000:])
        self.fail('timed out waiting for ' + description + ': '
                  + client[2].decode('utf-8', 'replace')[-3000:])

    def check_pane(self, client, name):
        marker = self.project / name
        checker = self.project / 'check_environment.py'
        checker.write_text(
            'import os, pathlib, sys\n'
            'ok = os.environ.get(' + repr(VARIABLE) + ') == ' + repr(SECRET) + '\n'
            'pathlib.Path(sys.argv[1]).write_text("ok" if ok else "wrong environment")\n')
        self.assertNotIn(VARIABLE, self.environment)
        result = self.command('--session', self.session, 'action', 'new-pane',
                              '--', sys.executable, str(checker), str(marker))
        self.assertEqual(result.returncode, 0, result.stderr.decode('utf-8', 'replace'))
        self.wait_for(client, marker.exists, 'pane environment check')
        self.assertEqual(marker.read_text(), 'ok')

    def detach(self, client):
        # Detach the actual interactive client. A separate CLI action can
        # target its own transient client rather than this PTY connection.
        os.write(client[1], b'\x0f')
        time.sleep(0.1)
        os.write(client[1], b'd')
        self.wait_for(client, lambda: self.poll(client) is not None, 'client detach')
        self.assertEqual(self.poll(client), 0)

    def test_project_unlock_pane_inheritance_and_attach_without_unlock(self):
        first = self.start_client()
        prompt = '主密码'.encode('utf-8')
        self.wait_for(first, lambda: prompt in first[2], 'password prompt')
        self.assertFalse(termios.tcgetattr(first[1])[3] & termios.ECHO)
        os.write(first[1], (PASSWORD + '\n').encode('utf-8'))
        self.wait_for(first, lambda: b'\x1b[' in first[2], 'new terminal display')
        deadline = time.monotonic() + 3
        while time.monotonic() < deadline:
            self.drain(first)
        self.check_pane(first, 'created.marker')
        self.detach(first)
        self.assertNotIn(PASSWORD.encode(), first[2])
        self.assertNotIn(SECRET.encode(), first[2])

        # Removing the vault and config makes an accidental second unlock fail.
        (self.home / '.config' / 'z-keystore' / 'vault.json').unlink()
        (self.project / '.zkey.json').unlink()
        attached = self.start_client()
        self.wait_for(attached, lambda: b'\x1b[' in attached[2], 'attached terminal display')
        self.check_pane(attached, 'attached.marker')
        self.detach(attached)
        self.assertNotIn(prompt, attached[2])
        self.assertNotIn(PASSWORD.encode(), attached[2])
        self.assertNotIn(SECRET.encode(), attached[2])


if __name__ == '__main__':
    unittest.main()
