"""Public, fixed v1 ciphertexts; no operator environment or real vault is read."""
import base64
import json
from pathlib import Path
import tempfile
import unittest

import z_keystore as z


class FormatVectorTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        path = Path(__file__).parent / 'fixtures' / 'keystore-v1.json'
        cls.vectors = json.loads(path.read_text())['vectors']
        if len(cls.vectors) != 2:
            raise AssertionError('expected both fixed v1 compatibility vectors')

    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory(prefix='zkeys-vectors-')
        self.addCleanup(self.temporary.cleanup)
        self.store = z.Store(Path(self.temporary.name) / 'store')

    def write_fixture(self, envelope):
        self.store.path.write_text(json.dumps(envelope))
        self.store.path.chmod(0o600)

    def test_fixed_v1_ciphertexts_remain_readable(self):
        with self.store.locked():
            for vector in self.vectors:
                with self.subTest(vector=vector['name']):
                    self.write_fixture(vector['vault'])
                    self.assertEqual(self.store.read(vector['password']), vector['entries'])
                    with self.assertRaises(z.StoreError):
                        self.store.read(vector['password'] + '-wrong')

    def test_tampered_fixed_v1_ciphertexts_are_rejected(self):
        with self.store.locked():
            for vector in self.vectors:
                for field in ('version', 'salt', 'nonce', 'ciphertext'):
                    with self.subTest(vector=vector['name'], field=field):
                        envelope = dict(vector['vault'])
                        if field == 'version':
                            envelope[field] = 2
                        else:
                            value = bytearray(base64.b64decode(envelope[field]))
                            value[0] ^= 1
                            envelope[field] = base64.b64encode(value).decode('ascii')
                        self.write_fixture(envelope)
                        with self.assertRaises(z.StoreError):
                            self.store.read(vector['password'])


if __name__ == '__main__':
    unittest.main()
