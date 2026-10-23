"""Go CLI version and interactive deletion; all secrets and homes are fixtures."""
import os
from pathlib import Path
import stat
import subprocess
import tempfile
import unittest

import test_go_compat as compatibility
import z_keystore as z

PASSWORD = compatibility.PASSWORD
SECRET = compatibility.SECRET
UNSELECTED = compatibility.UNSELECTED


class BasicsTests(unittest.TestCase):
    environment = compatibility.GoCompatibilityTests.environment
    seed = compatibility.GoCompatibilityTests.seed
    success = compatibility.GoCompatibilityTests.success

    def interactive(self, command, args, passwords=(), **kwargs):
        return compatibility.GoCompatibilityTests.interactive(
            self, command, args, passwords, secret_answers=passwords[:1], **kwargs)

    @classmethod
    def setUpClass(cls):
        binary = os.environ.get('ZKEY_GO')
        if not binary:
            raise unittest.SkipTest('set ZKEY_GO to the built Go zkey binary')
        cls.go = [str(Path(binary).resolve())]
        if not os.access(cls.go[0], os.X_OK):
            raise AssertionError('ZKEY_GO must name an executable binary')

    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory(prefix='zkeys-basics-')
        self.addCleanup(self.temporary.cleanup)
        self.home = Path(self.temporary.name) / 'home'
        (self.home / '.config').mkdir(parents=True, mode=0o700)
        self.store = z.Store(self.home / '.config' / 'z-keystore')

    def check_version(self, binary, name):
        expected = os.environ.get('ZKEY_EXPECTED_VERSION', 'dev')
        result = subprocess.run([binary, '--version'], stdin=subprocess.DEVNULL,
                                capture_output=True, env=self.environment(),
                                start_new_session=True, timeout=5)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(result.stdout, (name + ' ' + expected + '\n').encode())
        self.assertEqual(result.stderr, b'')
        self.assertFalse(self.store.directory.exists())
        for extra in ('extra', '--help'):
            result = subprocess.run([binary, '--version', extra], stdin=subprocess.DEVNULL,
                                    capture_output=True, env=self.environment(),
                                    start_new_session=True, timeout=5)
            self.assertEqual(result.returncode, 2, result.stderr)
            self.assertEqual(result.stdout, b'')
            self.assertFalse(self.store.directory.exists())

    def test_zkey_version_does_not_unlock_or_create_store(self):
        self.check_version(self.go[0], 'zkey')

    def test_zwork_version_does_not_start_tools(self):
        binary = os.environ.get('ZWORK_GO')
        if not binary:
            self.skipTest('set ZWORK_GO to the built Go zwork binary')
        self.check_version(str(Path(binary).resolve()), 'zwork')

    def test_delete_confirmed_preserves_other_entries_and_format(self):
        self.seed({'remove': SECRET, 'preserved': UNSELECTED})
        result = self.success(self.go, ['delete', 'remove'], [PASSWORD, 'y'])
        self.assertEqual(result.stdout, b'')
        self.assertIn(b'"remove"', result.terminal)
        for value in (SECRET, UNSELECTED):
            self.assertNotIn(value.encode(), result.stdout + result.stderr + result.terminal)
        with self.store.locked():
            self.assertEqual(self.store.read(PASSWORD), {'preserved': UNSELECTED})
        self.assertEqual(stat.S_IMODE(self.store.path.stat().st_mode), 0o600)

    def test_delete_cancel_missing_and_wrong_password_do_not_rewrite(self):
        self.seed({'remove': SECRET, 'preserved': UNSELECTED})
        before = self.store.path.read_bytes()
        metadata = self.store.path.stat()
        for args, answers, status in (
                (['delete', 'remove'], [PASSWORD, ''], 0),
                (['delete', 'remove'], [PASSWORD, 'n'], 0),
                (['delete', 'missing'], [PASSWORD], 1),
                (['delete', 'remove'], ['fake-wrong-password'], 1)):
            with self.subTest(args=args, status=status):
                result = self.interactive(self.go, args, answers)
                self.assertEqual(result.returncode, status, result.stderr)
                self.assertEqual(result.stdout, b'')
                self.assertEqual(self.store.path.read_bytes(), before)
                after = self.store.path.stat()
                self.assertEqual((after.st_ino, after.st_mtime_ns),
                                 (metadata.st_ino, metadata.st_mtime_ns))
                for value in (SECRET, UNSELECTED):
                    self.assertNotIn(value.encode(), result.stdout + result.stderr + result.terminal)

    def test_delete_ssh_snapshot_preserves_exported_files(self):
        self.seed({'ssh/work': '{"fake":"snapshot"}', 'preserved': UNSELECTED})
        exported = self.home / '.ssh' / 'zkeys' / 'work'
        exported.mkdir(parents=True, mode=0o700)
        files = {exported / 'config': b'fake exported config\n',
                 exported / 'private': b'fake exported private key\n'}
        for path, value in files.items():
            path.write_bytes(value)
            path.chmod(0o600)
        self.success(self.go, ['delete', 'ssh/work'], [PASSWORD, 'yes'])
        with self.store.locked():
            self.assertEqual(self.store.read(PASSWORD), {'preserved': UNSELECTED})
        for path, value in files.items():
            self.assertEqual(path.read_bytes(), value)
            self.assertEqual(stat.S_IMODE(path.stat().st_mode), 0o600)


if __name__ == '__main__':
    unittest.main()
