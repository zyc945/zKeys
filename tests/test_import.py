"""Merge encrypted files with real CLI/TTY interaction and disposable homes."""
import base64
import json
import os
from pathlib import Path
import stat
import subprocess
import tempfile
import unittest

import test_go_compat as compatibility
import z_keystore as z

PASSWORD = compatibility.PASSWORD
SOURCE_PASSWORD = 'fake-import-中文-password'
EXISTING = 'fake-existing-secret'
INCOMING = 'fake-incoming-secret\n第二行\n'
BUNDLE = '{"fixture":"SSH snapshot preserved verbatim"}'


class ImportTests(unittest.TestCase):
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
        self.temporary = tempfile.TemporaryDirectory(prefix='zkeys-import-')
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        self.home = self.root / 'target'
        (self.home / '.config').mkdir(parents=True)
        self.store = z.Store(self.home / '.config/z-keystore')
        self.seed({'retained': EXISTING})
        self.source = z.Store(self.root / 'source')
        self.seed({'incoming': INCOMING, 'ssh/work': BUNDLE}, SOURCE_PASSWORD, self.source)

    def snapshot(self, path):
        info = path.stat()
        return (path.read_bytes(), info.st_ino, info.st_mtime_ns, stat.S_IMODE(info.st_mode))

    def assert_quiet(self, result):
        self.assertEqual(result.stdout, b'')
        for value in (EXISTING, INCOMING, BUNDLE):
            self.assertNotIn(value.encode(), result.stdout + result.stderr + result.terminal)

    def test_merge_preserves_source_old_entries_and_target_password(self):
        source_before = self.snapshot(self.source.path)
        result = self.success(self.go, ['import', str(self.source.path)], [PASSWORD, SOURCE_PASSWORD])
        self.assert_quiet(result)
        self.assertEqual(self.snapshot(self.source.path), source_before)
        with self.store.locked():
            self.assertEqual(self.store.read(PASSWORD),
                             {'retained': EXISTING, 'incoming': INCOMING, 'ssh/work': BUNDLE})
            with self.assertRaises(z.StoreError):
                self.store.read(SOURCE_PASSWORD)
        self.assertEqual(stat.S_IMODE(self.store.path.stat().st_mode), 0o600)
        names = self.success(self.go, ['list'], [PASSWORD]).stdout
        self.assertEqual(names, b'incoming\nretained\nssh/work\n')
        self.assertEqual(set(json.loads(self.store.path.read_text())),
                         {'version', 'salt', 'nonce', 'ciphertext'})
        before = self.snapshot(self.store.path)
        repeated = self.interactive(self.go, ['import', str(self.source.path)], [PASSWORD, SOURCE_PASSWORD])
        self.assertNotEqual(repeated.returncode, 0)
        self.assertEqual(self.snapshot(self.store.path), before)
        self.assertEqual(self.snapshot(self.source.path), source_before)

    def test_any_conflict_rejects_every_entry_even_identical_values(self):
        for value in (EXISTING, 'fake-different-secret'):
            with self.source.locked():
                self.source.write({'fresh': INCOMING, 'retained': value}, SOURCE_PASSWORD)
            before = self.snapshot(self.store.path)
            source_before = self.snapshot(self.source.path)
            result = self.interactive(self.go, ['import', str(self.source.path)], [PASSWORD, SOURCE_PASSWORD])
            self.assertNotEqual(result.returncode, 0)
            self.assert_quiet(result)
            self.assertEqual(self.snapshot(self.store.path), before)
            self.assertEqual(self.snapshot(self.source.path), source_before)

    def test_wrong_passwords_and_missing_file_do_not_change_target(self):
        for path, answers in ((self.source.path, ['fake-wrong']),
                              (self.source.path, [PASSWORD, 'fake-wrong']),
                              (self.root / 'missing', [PASSWORD, SOURCE_PASSWORD])):
            before = self.snapshot(self.store.path)
            source_before = self.snapshot(self.source.path)
            result = self.interactive(self.go, ['import', str(path)], answers)
            self.assertNotEqual(result.returncode, 0)
            self.assert_quiet(result)
            self.assertEqual(self.snapshot(self.store.path), before)
            self.assertEqual(self.snapshot(self.source.path), source_before)

    def test_corrupt_source_leaves_both_files_unchanged(self):
        original = self.source.path.read_bytes()
        envelope = json.loads(original)
        cipher = bytearray(base64.b64decode(envelope['ciphertext']))
        cipher[0] ^= 1
        envelope['ciphertext'] = base64.b64encode(cipher).decode()
        for data in (b'not-json', json.dumps(envelope).encode()):
            self.source.path.write_bytes(data)
            before, source_before = self.snapshot(self.store.path), self.snapshot(self.source.path)
            result = self.interactive(self.go, ['import', str(self.source.path)], [PASSWORD, SOURCE_PASSWORD])
            self.assertNotEqual(result.returncode, 0)
            self.assertEqual(self.snapshot(self.store.path), before)
            self.assertEqual(self.snapshot(self.source.path), source_before)

    def test_source_links_and_unsafe_permissions_are_rejected(self):
        symlink = self.root / 'symlink'
        symlink.symlink_to(self.source.path)
        hardlink = self.root / 'hardlink'
        os.link(self.source.path, hardlink)
        for path in (symlink, hardlink):
            before, source_before = self.snapshot(self.store.path), self.snapshot(self.source.path)
            result = self.interactive(self.go, ['import', str(path)], [PASSWORD, SOURCE_PASSWORD])
            self.assertNotEqual(result.returncode, 0)
            self.assertEqual(self.snapshot(self.store.path), before)
            self.assertEqual(self.snapshot(self.source.path), source_before)
        hardlink.unlink()
        self.source.path.chmod(0o644)
        before, source_before = self.snapshot(self.store.path), self.snapshot(self.source.path)
        result = self.interactive(self.go, ['import', str(self.source.path)], [PASSWORD, SOURCE_PASSWORD])
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(self.snapshot(self.store.path), before)
        self.assertEqual(self.snapshot(self.source.path), source_before)

    def test_same_file_and_aliased_parent_are_rejected_even_for_empty_target(self):
        alias = self.root / 'alias'
        alias.symlink_to(self.store.directory, target_is_directory=True)
        for values in ({'retained': EXISTING}, {}):
            with self.store.locked():
                self.store.write(values, PASSWORD)
            for path in (self.store.path, alias / 'vault.json'):
                before = self.snapshot(self.store.path)
                result = self.interactive(self.go, ['import', str(path)], [PASSWORD, PASSWORD])
                self.assertNotEqual(result.returncode, 0)
                self.assertEqual(self.snapshot(self.store.path), before)

    def test_empty_source_is_noop_and_short_passwords_work(self):
        for password, entries in (('', {}), ('q', {'short/password': INCOMING})):
            with self.source.locked():
                self.source.write(entries, password)
            source_before, target_before = self.snapshot(self.source.path), self.snapshot(self.store.path)
            result = self.success(self.go, ['import', str(self.source.path)], [PASSWORD, password])
            self.assert_quiet(result)
            self.assertEqual(self.snapshot(self.source.path), source_before)
            if not entries:
                self.assertEqual(self.snapshot(self.store.path), target_before)
            with self.store.locked():
                self.assertEqual(self.store.read(PASSWORD), dict({'retained': EXISTING}, **entries))

    def test_missing_target_requires_init_and_does_not_create_vault(self):
        home = self.root / 'uninitialized'
        (home / '.config').mkdir(parents=True)
        source_before = self.snapshot(self.source.path)
        result = self.interactive(self.go, ['import', str(self.source.path)], [PASSWORD, SOURCE_PASSWORD], home=home)
        self.assertNotEqual(result.returncode, 0)
        self.assertFalse((home / '.config/z-keystore/vault.json').exists())
        self.assertEqual(self.snapshot(self.source.path), source_before)

    def test_invalid_arguments_no_tty_and_cancellation_preserve_files(self):
        before, source_before = self.snapshot(self.store.path), self.snapshot(self.source.path)
        for args in (['import'], ['import', '-'], ['import', str(self.source.path), '--force'],
                     ['import', str(self.source.path), str(self.source.path)], ['import', str(self.source.path)]):
            result = subprocess.run(self.go + args, stdin=subprocess.DEVNULL,
                                    capture_output=True, env=self.environment(), start_new_session=True, timeout=5)
            self.assertNotEqual(result.returncode, 0)
            self.assertEqual(result.stdout, b'')
        result = self.interactive(self.go, ['import', str(self.source.path)], interrupt=True)
        self.assertEqual(result.returncode, 130)
        self.assertEqual(self.snapshot(self.store.path), before)
        self.assertEqual(self.snapshot(self.source.path), source_before)


if __name__ == '__main__':
    unittest.main()
