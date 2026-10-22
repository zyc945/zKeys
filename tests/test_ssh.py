"""SSH import/export integration; use ZKEY_GO and optionally ZKEY_SSHD."""
import getpass
import json
import os
from pathlib import Path
import shlex
import shutil
import socket
import stat
import subprocess
import tempfile
import time
import unittest

import test_go_compat as compatibility
import z_keystore as z

PASSWORD = compatibility.PASSWORD


def generate_key(path, password=''):
    subprocess.run(['ssh-keygen', '-q', '-t', 'ed25519', '-N', password,
                    '-C', 'fake-zkeys-integration', '-f', str(path)],
                   check=True, capture_output=True, timeout=10)
    for item in (path, Path(str(path) + '.pub')):
        item.chmod(0o600)


def file_options(config, name):
    """Return generated file references without invoking SSH/config commands."""
    result = []
    for line in config.read_text().splitlines():
        fields = shlex.split(line, comments=True)
        if fields and fields[0].lower() == name.lower():
            result.extend(fields[1:])
    return result


class SSHIntegrationTests(unittest.TestCase):
    # Reuse the PTY driver without inheriting and rerunning unrelated tests.
    environment = compatibility.GoCompatibilityTests.environment
    seed = compatibility.GoCompatibilityTests.seed
    interactive = compatibility.GoCompatibilityTests.interactive
    success = compatibility.GoCompatibilityTests.success

    @classmethod
    def setUpClass(cls):
        binary = os.environ.get('ZKEY_GO')
        if not binary:
            raise unittest.SkipTest('set ZKEY_GO to the built Go zkey binary')
        cls.go = [str(Path(binary).resolve())]
        if not os.access(cls.go[0], os.X_OK):
            raise AssertionError('ZKEY_GO must name an executable binary')
        if not shutil.which('ssh-keygen'):
            raise unittest.SkipTest('ssh-keygen is required for SSH fixtures')

    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory(prefix='zkeys-ssh-', dir='/tmp')
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        self.home = self.root / 'home'
        self.home.mkdir(mode=0o700)
        (self.home / '.config').mkdir(mode=0o700)
        self.ssh = self.home / '.ssh'
        self.ssh.mkdir(mode=0o700)
        self.source = self.ssh / 'source'
        self.source.mkdir(mode=0o700)
        self.store = z.Store(self.home / '.config' / 'z-keystore')
        self.seed({'preserved': 'fake-existing-secret'})
        self.key = self.source / 'plain'
        self.encrypted = self.source / 'encrypted'
        self.hostkey = self.source / 'host'
        generate_key(self.key)
        generate_key(self.encrypted, 'fake-original-ssh-key-passphrase')
        generate_key(self.hostkey)
        self.known = self.source / 'known_hosts'
        self.known.write_text('fixture.invalid ' + Path(str(self.hostkey) + '.pub').read_text())
        self.known.chmod(0o600)
        self.config = self.ssh / 'config'
        self.write_config()

    def write_config(self, extra='', port=2222):
        self.config.write_text(
            'Host app alias\n'
            '  HostName 127.0.0.1\n'
            '  User ' + getpass.getuser() + '\n'
            '  Port ' + str(port) + '\n'
            '  IdentityFile ' + str(self.key) + '\n'
            '  IdentityFile ' + str(self.encrypted) + '\n'
            '  UserKnownHostsFile ' + str(self.known) + '\n'
            '  IdentitiesOnly yes\n' + extra +
            'Host jump\n'
            '  HostName 127.0.0.2\n'
            '  User fixture\n'
            '  IdentityFile ' + str(self.key) + '\n'
            'Host *\n'
            '  ServerAliveInterval 15\n')
        self.config.chmod(0o600)

    def import_bundle(self, name='work', hosts=('app',), config=None):
        args = ['ssh', 'import', name]
        for host in hosts:
            args.extend(['--host', host])
        if config is not None:
            args.extend(['--config', str(config)])
        result = self.success(self.go, args, [PASSWORD])
        self.assertEqual(result.terminal.count('主密码'.encode()), 1)
        return result

    def export_bundle(self, name='work', home=None, install=False):
        args = ['ssh', 'export', name, '--with-private-keys']
        if install:
            args.append('--install-config')
        result = self.success(self.go, args, [PASSWORD], home=home)
        self.assertEqual(result.terminal.count('主密码'.encode()), 1)
        return (home or self.home) / '.ssh' / 'zkeys' / name

    def contents(self):
        with self.store.locked():
            return self.store.read(PASSWORD)

    def assert_private_absent_from_output(self, result):
        for path in (self.key, self.encrypted):
            self.assertNotIn(path.read_bytes(), result.stdout + result.stderr + result.terminal)

    def test_import_export_preserves_files_permissions_and_python_format(self):
        ca = self.source / 'ca'
        generate_key(ca)
        subprocess.run(['ssh-keygen', '-q', '-s', str(ca), '-I', 'fake-cert',
                        str(self.key) + '.pub'], check=True, capture_output=True, timeout=10)
        certificate = Path(str(self.key) + '-cert.pub')
        certificate.chmod(0o600)
        before_config = self.config.read_bytes()
        result = self.import_bundle(hosts=('app', 'alias'))
        self.assert_private_absent_from_output(result)
        entries = self.contents()
        self.assertEqual(entries['preserved'], 'fake-existing-secret')
        self.assertIsInstance(entries['ssh/work'], str)
        bundle = json.loads(entries['ssh/work'])
        self.assertIsInstance(bundle, dict)
        # The unchanged string-to-string Python vault accepts and rewrites SSH entries.
        with self.store.locked():
            self.store.write(entries, PASSWORD)
        output = self.export_bundle()
        self.assertEqual(self.config.read_bytes(), before_config)
        self.assertTrue((output / 'config').is_file())
        expected = [self.key, Path(str(self.key) + '.pub'), certificate,
                    self.encrypted, Path(str(self.encrypted) + '.pub'), self.known]
        exported = [path for path in output.rglob('*') if path.is_file() and path.name != 'config']
        self.assertCountEqual([path.read_bytes() for path in exported],
                              [path.read_bytes() for path in expected])
        self.assertEqual(len(exported), len(expected), 'shared keys must be stored only once')
        restored_encrypted = next(path for path in exported
                                  if path.read_bytes() == self.encrypted.read_bytes())
        public = subprocess.run(
            ['ssh-keygen', '-y', '-P', 'fake-original-ssh-key-passphrase',
             '-f', str(restored_encrypted)],
            check=True, capture_output=True, env=self.environment(), timeout=10)
        self.assertEqual(public.stdout.split()[:2],
                         Path(str(self.encrypted) + '.pub').read_bytes().split()[:2])
        for path in output.rglob('*'):
            self.assertFalse(path.is_symlink())
            self.assertEqual(stat.S_IMODE(path.stat().st_mode), 0o700 if path.is_dir() else 0o600)
        self.assertEqual(stat.S_IMODE(output.stat().st_mode), 0o700)
        for option in ('IdentityFile', 'CertificateFile', 'UserKnownHostsFile'):
            for reference in file_options(output / 'config', option):
                self.assertTrue(Path(reference).is_file(), reference)
                self.assertTrue(Path(reference).is_relative_to(output), reference)
        self.assertNotIn(str(self.source), (output / 'config').read_text())
        self.assertNotIn(self.key.read_bytes(), self.store.path.read_bytes())

    def test_proxyjump_dependency_and_first_scalar_wins(self):
        self.write_config(extra='  ProxyJump jump\n  Port 9999\n')
        self.import_bundle()
        output = self.export_bundle()
        config = (output / 'config').read_text()
        self.assertIn('jump', config)
        self.assertIn('2222', config)
        self.assertNotIn('9999', config)
        # Config evaluation is safe for this fixture (no Match exec/ProxyCommand).
        result = subprocess.run(['ssh', '-G', '-F', str(output / 'config'), 'app'],
                                capture_output=True, env=self.environment(), timeout=5)
        self.assertEqual(result.returncode, 0, result.stderr)
        options = dict(line.split(' ', 1) for line in result.stdout.decode().splitlines())
        self.assertEqual(options['proxyjump'], 'jump')
        self.assertEqual(options['port'], '2222')
        self.assertEqual(options['serveraliveinterval'], '15')

    def test_duplicate_import_wrong_password_and_export_conflict_leave_files_unchanged(self):
        self.import_bundle()
        before = self.store.path.read_bytes()
        duplicate = self.interactive(self.go, ['ssh', 'import', 'work', '--host', 'app'], [PASSWORD])
        self.assertNotEqual(duplicate.returncode, 0)
        self.assertEqual(self.store.path.read_bytes(), before)
        wrong = self.interactive(self.go, ['ssh', 'export', 'work', '--with-private-keys'],
                                 ['fake-wrong-password'])
        self.assertNotEqual(wrong.returncode, 0)
        self.assertFalse((self.ssh / 'zkeys' / 'work').exists())
        output = self.export_bundle()
        snapshot = {str(path.relative_to(output)): path.read_bytes()
                    for path in output.rglob('*') if path.is_file()}
        again = self.interactive(self.go, ['ssh', 'export', 'work', '--with-private-keys'], [PASSWORD])
        self.assertNotEqual(again.returncode, 0)
        self.assertEqual(snapshot, {str(path.relative_to(output)): path.read_bytes()
                                    for path in output.rglob('*') if path.is_file()})
        self.assertEqual(self.store.path.read_bytes(), before)

    def test_private_key_export_requires_explicit_flag(self):
        self.import_bundle()
        result = self.interactive(self.go, ['ssh', 'export', 'work'], [PASSWORD])
        self.assertNotEqual(result.returncode, 0)
        self.assertFalse((self.ssh / 'zkeys' / 'work').exists())
        self.assert_private_absent_from_output(result)

    def test_install_config_refuses_existing_config_without_partial_export(self):
        self.import_bundle()
        before = self.config.read_bytes()
        result = self.interactive(self.go,
                                  ['ssh', 'export', 'work', '--with-private-keys', '--install-config'],
                                  [PASSWORD])
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(self.config.read_bytes(), before)
        self.assertFalse((self.ssh / 'zkeys' / 'work').exists())

    def test_install_config_on_fresh_home(self):
        self.import_bundle()
        fresh = self.root / 'fresh'
        fresh.mkdir(mode=0o700)
        (fresh / '.config').mkdir(mode=0o700)
        copied = z.Store(fresh / '.config' / 'z-keystore')
        with copied.locked():
            copied.initialize(PASSWORD)
            copied.write(self.contents(), PASSWORD)
        output = self.export_bundle(home=fresh, install=True)
        main = fresh / '.ssh' / 'config'
        self.assertIn(str(output / 'config'), main.read_text())
        self.assertEqual(stat.S_IMODE(main.stat().st_mode), 0o600)
        self.assertEqual(stat.S_IMODE(main.parent.stat().st_mode), 0o700)

    def test_invalid_cli_and_unsafe_config_never_modify_vault(self):
        before = self.store.path.read_bytes()
        cases = [
            ['ssh', 'import', '../escape', '--host', 'app'],
            ['ssh', 'import', 'work'],
            ['ssh', 'import', 'work', '--host', 'app', '--unknown'],
            ['ssh', 'export', '../escape', '--with-private-keys'],
            ['ssh', 'export', 'work', '--unknown'],
        ]
        for args in cases:
            with self.subTest(args=args):
                result = subprocess.run(self.go + args, stdin=subprocess.DEVNULL,
                                        capture_output=True, env=self.environment(),
                                        start_new_session=True, timeout=5)
                self.assertNotEqual(result.returncode, 0)
                self.assertNotIn('主密码'.encode(), result.stderr)
                self.assertEqual(self.store.path.read_bytes(), before)
        for directive in ('Include other.conf', 'Match exec "touch marker"',
                          'Host app*', 'Host !app'):
            with self.subTest(directive=directive):
                self.write_config()
                self.config.write_text(self.config.read_text() + directive + '\n')
                result = self.interactive(self.go,
                                          ['ssh', 'import', 'work', '--host', 'app'], [PASSWORD])
                self.assertNotEqual(result.returncode, 0)
                self.assertEqual(self.store.path.read_bytes(), before)

    def test_proxycommand_is_preserved_but_never_executed(self):
        marker = self.root / 'must-not-exist'
        self.write_config(extra='  ProxyCommand sh -c "touch ' + str(marker) + '"\n')
        self.import_bundle()
        output = self.export_bundle()
        self.assertFalse(marker.exists())
        self.assertIn('touch ' + str(marker), (output / 'config').read_text())

    def test_insecure_key_sources_are_rejected(self):
        before = self.store.path.read_bytes()
        self.key.chmod(0o644)
        result = self.interactive(self.go, ['ssh', 'import', 'work', '--host', 'app'], [PASSWORD])
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(self.store.path.read_bytes(), before)
        self.key.chmod(0o600)
        moved = self.source / 'plain-real'
        self.key.rename(moved)
        self.key.symlink_to(moved)
        result = self.interactive(self.go, ['ssh', 'import', 'work', '--host', 'app'], [PASSWORD])
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(self.store.path.read_bytes(), before)
        self.key.unlink()
        moved.rename(self.key)
        os.link(self.key, self.root / 'extra-hardlink')
        result = self.interactive(self.go, ['ssh', 'import', 'work', '--host', 'app'], [PASSWORD])
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(self.store.path.read_bytes(), before)

    def test_real_ssh_authentication_uses_exported_private_key(self):
        sshd = os.environ.get('ZKEY_SSHD')
        if not sshd:
            self.skipTest('set ZKEY_SSHD for an isolated loopback SSH authentication test')
        if not Path(sshd).is_file():
            self.fail('ZKEY_SSHD does not exist')
        with socket.socket() as listener:
            listener.bind(('127.0.0.1', 0))
            port = listener.getsockname()[1]
        self.write_config(port=port)
        self.known.write_text('[127.0.0.1]:' + str(port) + ' ' +
                              Path(str(self.hostkey) + '.pub').read_text())
        self.import_bundle()
        direct = self.export_bundle()
        self.write_config(extra='  ProxyJump jump\n', port=port)
        jump_config = self.config.read_text().replace('HostName 127.0.0.2', 'HostName 127.0.0.1')
        jump_config = jump_config.replace(
            '  User fixture\n', '  User ' + getpass.getuser() + '\n'
            '  Port ' + str(port) + '\n'
            '  UserKnownHostsFile ' + str(self.known) + '\n')
        self.config.write_text(jump_config)
        self.import_bundle(name='via-jump')
        via_jump = self.export_bundle(name='via-jump')
        daemon_config = self.root / 'sshd_config'
        daemon_config.write_text(
            'ListenAddress 127.0.0.1\nPort ' + str(port) + '\n'
            'HostKey ' + str(self.hostkey) + '\n'
            'AuthorizedKeysFile ' + str(self.key) + '.pub\n'
            'PidFile ' + str(self.root / 'sshd.pid') + '\n'
            'StrictModes no\nUsePAM no\nPasswordAuthentication no\n'
            'KbdInteractiveAuthentication no\nPubkeyAuthentication yes\n'
            'LogLevel ERROR\n')
        daemon = subprocess.Popen([sshd, '-D', '-e', '-f', str(daemon_config)],
                                  stdin=subprocess.DEVNULL, stdout=subprocess.PIPE,
                                  stderr=subprocess.PIPE, env=self.environment())
        try:
            deadline = time.monotonic() + 5
            ready = False
            while time.monotonic() < deadline and daemon.poll() is None:
                try:
                    with socket.create_connection(('127.0.0.1', port), timeout=0.1):
                        ready = True
                        break
                except OSError:
                    time.sleep(0.05)
            if not ready:
                daemon.terminate()
                _, error = daemon.communicate(timeout=5)
                self.fail('isolated sshd did not start: ' + error.decode('utf-8', 'replace'))
            # Remove source client keys to prove the rewritten destination is used.
            self.key.unlink()
            self.encrypted.unlink()
            for output in (direct, via_jump):
                with self.subTest(route=output.name):
                    result = subprocess.run(
                        ['ssh', '-F', str(output / 'config'), '-o', 'IdentityAgent=none',
                         '-o', 'IdentitiesOnly=yes', '-o', 'BatchMode=yes',
                         '-o', 'StrictHostKeyChecking=yes', '-o', 'ConnectTimeout=5',
                         'app', 'printf zkeys-ssh-authenticated'],
                        capture_output=True, env=self.environment(), timeout=10)
                    self.assertEqual(result.returncode, 0, result.stderr.decode('utf-8', 'replace'))
                    self.assertEqual(result.stdout, b'zkeys-ssh-authenticated')
        finally:
            if daemon.poll() is None:
                daemon.terminate()
            daemon.communicate(timeout=5)


if __name__ == '__main__':
    unittest.main()
