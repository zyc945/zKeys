import base64
from contextlib import redirect_stdout
import io
import json
from pathlib import Path
import stat
import tempfile
import unittest
from unittest.mock import patch
import z_keystore as z

PASSWORD = 'fake-test-password-only'
SECRET = 'fake-test-secret-only'

class StoreTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix='zkey-test-')
        self.directory = Path(self.temp.name) / 'store'
        self.store = z.Store(self.directory)
        with self.store.locked(): self.store.initialize(PASSWORD)
    def tearDown(self): self.temp.cleanup()
    def test_encrypted_roundtrip_and_permissions(self):
        with self.store.locked():
            self.store.write({'work/key': SECRET}, PASSWORD)
            self.assertEqual(self.store.read(PASSWORD), {'work/key': SECRET})
        content = self.store.path.read_text()
        for value in (PASSWORD, SECRET, 'work/key'): self.assertNotIn(value, content)
        self.assertEqual(stat.S_IMODE(self.directory.stat().st_mode), 0o700)
        self.assertEqual(stat.S_IMODE(self.store.path.stat().st_mode), 0o600)
    def test_wrong_password(self):
        with self.store.locked(), self.assertRaises(z.StoreError): self.store.read('wrong')
    def test_tampered_ciphertext(self):
        envelope = json.loads(self.store.path.read_text())
        value = bytearray(base64.b64decode(envelope['ciphertext'])); value[0] ^= 1
        envelope['ciphertext'] = base64.b64encode(value).decode()
        self.store.path.write_text(json.dumps(envelope))
        with self.store.locked(), self.assertRaises(z.StoreError): self.store.read(PASSWORD)
    def test_duplicate_init_preserves_file(self):
        before = self.store.path.read_bytes()
        with self.store.locked(), self.assertRaises(z.StoreError): self.store.initialize('other')
        self.assertEqual(before, self.store.path.read_bytes())
    def test_unsafe_permissions_rejected(self):
        self.store.path.chmod(0o644)
        with self.store.locked(), self.assertRaises(z.StoreError): self.store.read(PASSWORD)
    def test_set_get_preserves_multiline(self):
        value = SECRET + '\nsecond line\n'
        with patch.object(z, 'hidden', return_value=PASSWORD), patch('sys.stdin', io.StringIO(value)):
            z.main(['set', 'key', '--stdin'], self.directory)
        output = io.StringIO()
        with patch.object(z, 'hidden', return_value=PASSWORD), redirect_stdout(output):
            z.main(['get', 'key'], self.directory)
        self.assertEqual(output.getvalue(), value)
    def test_password_has_no_length_constraint(self):
        for password in ('', 'x', '短口令'):
            with patch.object(z, 'hidden', side_effect=[password, password]):
                self.assertEqual(z.new_password(), password)
        with patch.object(z, 'hidden', side_effect=['a', 'b']), self.assertRaises(z.StoreError): z.new_password()
    def test_run_selects_requested_key(self):
        with self.store.locked(): self.store.write({'one': SECRET, 'two': 'fake-unrequested-secret'}, PASSWORD)
        with patch.object(z, 'hidden', return_value=PASSWORD), patch.object(z.os, 'execvpe') as execute:
            z.main(['run', '--env', 'TOKEN=one', '--', 'program', 'literal;argument'], self.directory)
        executable, argv, env = execute.call_args.args
        self.assertEqual(argv, ['program', 'literal;argument'])
        self.assertEqual(env['TOKEN'], SECRET)
        self.assertNotIn('fake-unrequested-secret', env.values())
    def test_no_tty_rejected(self):
        with patch('builtins.open', side_effect=OSError), self.assertRaises(z.StoreError): z.hidden('prompt')

if __name__ == '__main__': unittest.main()
