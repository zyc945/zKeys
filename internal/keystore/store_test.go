package keystore

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

func testStore(t *testing.T) Store {
	t.Helper()
	s := Store{Directory: filepath.Join(t.TempDir(), "store")}
	lock, err := s.Lock()
	if err != nil {
		t.Fatal(err)
	}
	lock.Close()
	return s
}
func TestFreshEncryptionAndLock(t *testing.T) {
	s := testStore(t)
	password := []byte("dummy")
	lock, err := s.Lock()
	if err != nil {
		t.Fatal(err)
	}
	other, err := openPrivate(filepath.Join(s.Directory, ".lock"), unix.O_RDWR)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	if err = unix.Flock(int(other.Fd()), unix.LOCK_EX|unix.LOCK_NB); err == nil {
		t.Fatal("lock not held")
	}
	if err = s.Write(map[string]string{"key": "dummy"}, password); err != nil {
		t.Fatal(err)
	}
	first, _ := os.ReadFile(s.Path())
	if err = s.Write(map[string]string{"key": "dummy"}, password); err != nil {
		t.Fatal(err)
	}
	second, _ := os.ReadFile(s.Path())
	var a, b map[string]any
	json.Unmarshal(first, &a)
	json.Unmarshal(second, &b)
	for _, name := range []string{"salt", "nonce", "ciphertext"} {
		if a[name] == b[name] {
			t.Fatalf("reused %s", name)
		}
	}
	lock.Close()
	if err = unix.Flock(int(other.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		t.Fatal("lock not released", err)
	}
	matches, _ := filepath.Glob(filepath.Join(s.Directory, ".vault-*"))
	if len(matches) != 0 {
		t.Fatal("temporary files left")
	}
}
func TestRejectMalformedEnvelopeAndPlaintext(t *testing.T) {
	s := testStore(t)
	pw := []byte("dummy")
	if err := s.Write(map[string]string{}, pw); err != nil {
		t.Fatal(err)
	}
	original, _ := os.ReadFile(s.Path())
	for _, key := range []string{"version", "salt", "nonce", "ciphertext"} {
		t.Run(key+"-null", func(t *testing.T) {
			var v map[string]any
			json.Unmarshal(original, &v)
			v[key] = nil
			data, _ := json.Marshal(v)
			os.WriteFile(s.Path(), data, 0600)
			if _, err := s.Read(pw); !errors.Is(err, InvalidVault) {
				t.Fatalf("accepted null: %v", err)
			}
		})
	}
	for _, plain := range []string{`null`, `[]`, `{"key":null}`, `{"key":1}`, `{"bad name":"x"}`, `{"key":"\u0000"}`} {
		t.Run(plain, func(t *testing.T) {
			var v map[string]any
			json.Unmarshal(original, &v)
			salt, _ := base64.StdEncoding.DecodeString(v["salt"].(string))
			nonce, _ := base64.StdEncoding.DecodeString(v["nonce"].(string))
			gcm, _ := aead(pw, salt)
			v["ciphertext"] = base64.StdEncoding.EncodeToString(gcm.Seal(nil, nonce, []byte(plain), []byte(aad)))
			data, _ := json.Marshal(v)
			os.WriteFile(s.Path(), data, 0600)
			if _, err := s.Read(pw); !errors.Is(err, InvalidVault) {
				t.Fatalf("accepted invalid plaintext: %v", err)
			}
		})
	}
}
