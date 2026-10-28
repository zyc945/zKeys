"""Selected-entry export through a real TTY, using only temporary fixture vaults."""
import json
import os
from pathlib import Path
import shutil
import stat
import subprocess
import tempfile
import unittest

import test_go_compat as compatibility
import z_keystore as z

PASSWORD = compatibility.PASSWORD
SECRET = compatibility.SECRET
UNSELECTED = compatibility.UNSELECTED
NEW_PASSWORD = 'fake-export-新-password'
SNAPSHOT = '{"version":1,"fixture":"SSH bundle copied intact"}'


class ExportTests(unittest.TestCase):
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

    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory(prefix='zkeys-export-')
        self.addCleanup(self.temporary.cleanup)
        self.home = Path(self.temporary.name) / 'source'
        (self.home / '.config').mkdir(parents=True)
        self.store = z.Store(self.home / '.config' / 'z-keystore')
        self.seed({'work/key': SECRET, 'other': UNSELECTED, 'ssh/work': SNAPSHOT})
        self.before = self.store.path.read_bytes()
        self.metadata = self.store.path.stat()
        self.destination = Path(self.temporary.name) / 'selected.json'

    def assert_source_unchanged(self):
        self.assertEqual(self.store.path.read_bytes(), self.before)
        after = self.store.path.stat()
        self.assertEqual((after.st_ino, after.st_mtime_ns),
                         (self.metadata.st_ino, self.metadata.st_mtime_ns))

    def assert_no_values(self, result):
        self.assertEqual(result.stdout, b'')
        for secret in (SECRET, UNSELECTED, SNAPSHOT):
            self.assertNotIn(secret.encode(), result.stdout + result.stderr + result.terminal)

    def test_selected_export_python_read_go_read_and_reexport(self):
        result = self.success(self.go, ['export', 'work/key', 'ssh/work', '--out', str(self.destination)],
                              [PASSWORD, NEW_PASSWORD, NEW_PASSWORD])
        self.assert_no_values(result)
        self.assert_source_unchanged()
        self.assertEqual(stat.S_IMODE(self.destination.stat().st_mode), 0o600)
        self.assertEqual(self.destination.stat().st_nlink, 1)
        envelope = json.loads(self.destination.read_text())
        self.assertEqual(set(envelope), {'version', 'salt', 'nonce', 'ciphertext'})
        self.assertEqual(envelope['version'], 1)
        for secret in (SECRET, UNSELECTED, SNAPSHOT, PASSWORD, NEW_PASSWORD, 'work/key'):
            self.assertNotIn(secret.encode(), self.destination.read_bytes())

        # Install the exported file into a separate fixture HOME, as on another device.
        home = Path(self.temporary.name) / 'recipient'
        directory = home / '.config' / 'z-keystore'
        directory.mkdir(parents=True, mode=0o700)
        shutil.copyfile(self.destination, directory / 'vault.json')
        (directory / 'vault.json').chmod(0o600)
        store = z.Store(directory)
        with store.locked():
            self.assertEqual(store.read(NEW_PASSWORD), {'work/key': SECRET, 'ssh/work': SNAPSHOT})
            with self.assertRaises(z.StoreError):
                store.read(PASSWORD)
        result = self.success(self.go, ['list'], [NEW_PASSWORD], home=home)
        self.assertEqual(result.stdout, b'ssh/work\nwork/key\n')
        again = Path(self.temporary.name) / 'again.json'
        self.success(self.go, ['export', 'ssh/work', '--out', str(again)],
                     [NEW_PASSWORD, PASSWORD, PASSWORD], home=home)
        self.assertTrue(again.is_file())
        self.assert_source_unchanged()

    def test_empty_and_short_new_passwords(self):
        for index, password in enumerate(('', 'q')):
            destination = Path(self.temporary.name) / str(index) / 'vault.json'
            destination.parent.mkdir(mode=0o700)
            self.success(self.go, ['export', 'work/key', '--out', str(destination)],
                         [PASSWORD, password, password])
            with z.Store(destination.parent).locked():
                self.assertEqual(z.Store(destination.parent).read(password), {'work/key': SECRET})
        self.assert_source_unchanged()

    def test_missing_wrong_password_and_mismatch_leave_no_output(self):
        for names, answers in ((['work/key', 'missing'], [PASSWORD]),
                               (['work/key'], ['fake-wrong']),
                               (['work/key'], [PASSWORD, 'first', 'second'])):
            with self.subTest(names=names, answers=len(answers)):
                result = self.interactive(self.go, ['export', *names, '--out', str(self.destination)], answers)
                self.assertNotEqual(result.returncode, 0)
                self.assertFalse(self.destination.exists())
                self.assert_no_values(result)
                self.assert_source_unchanged()

    def test_existing_targets_and_links_are_never_overwritten(self):
        existing = Path(self.temporary.name) / 'existing'
        existing.write_bytes(b'preserve-existing')
        symlink = Path(self.temporary.name) / 'link'
        symlink.symlink_to(existing)
        dangling = Path(self.temporary.name) / 'dangling'
        dangling.symlink_to(Path(self.temporary.name) / 'absent')
        for destination in (existing, symlink, dangling, self.store.path):
            with self.subTest(destination=destination.name):
                result = self.interactive(self.go, ['export', 'work/key', '--out', str(destination)],
                                          [PASSWORD, NEW_PASSWORD, NEW_PASSWORD])
                self.assertNotEqual(result.returncode, 0)
                self.assert_no_values(result)
                self.assert_source_unchanged()
                self.assertEqual(existing.read_bytes(), b'preserve-existing')
                self.assertTrue(symlink.is_symlink())
                self.assertTrue(dangling.is_symlink())
                self.assertFalse((Path(self.temporary.name) / 'absent').exists())

    def test_missing_parent_and_interruption_leave_no_output(self):
        missing = Path(self.temporary.name) / 'missing' / 'vault.json'
        result = self.interactive(self.go, ['export', 'work/key', '--out', str(missing)],
                                  [PASSWORD, NEW_PASSWORD, NEW_PASSWORD])
        self.assertNotEqual(result.returncode, 0)
        self.assertFalse(missing.parent.exists())
        result = self.interactive(self.go, ['export', 'work/key', '--out', str(self.destination)],
                                  interrupt=True)
        self.assertEqual(result.returncode, 130)
        self.assertFalse(self.destination.exists())
        self.assert_source_unchanged()

    def test_no_tty_and_invalid_arguments_do_not_create_output(self):
        for args in (['export', 'work/key', '--out', str(self.destination)],
                     ['export', '--out', str(self.destination)],
                     ['export', 'work/key'],
                     ['export', 'work/key', 'work/key', '--out', str(self.destination)],
                     ['export', 'bad name', '--out', str(self.destination)]):
            result = subprocess.run(self.go + args, stdin=subprocess.DEVNULL,
                                    capture_output=True, env=self.environment(),
                                    start_new_session=True, timeout=5)
            self.assertNotEqual(result.returncode, 0)
            self.assertEqual(result.stdout, b'')
            self.assertFalse(self.destination.exists())
            self.assert_source_unchanged()


if __name__ == '__main__':
    unittest.main()
