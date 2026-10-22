// Package keystore reads and writes the existing z-keystore v1 format.
package keystore

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"

	"golang.org/x/crypto/scrypt"
	"golang.org/x/sys/unix"
)

const aad = "z-keystore:v1:scrypt-32768-8-1:aes-256-gcm"

var Name = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.:/-]{0,127}$`)
var EnvName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

type Error string

func (e Error) Error() string { return string(e) }

const InvalidVault = Error("无法解锁：主密码错误或密钥库已损坏。")

type Store struct{ Directory string }

func (s Store) Path() string { return filepath.Join(s.Directory, "vault.json") }
func CheckName(name string) error {
	if !Name.MatchString(name) {
		return Error("密钥名称须为 1–128 个字母、数字或 _ . : / -，以字母或数字开头。")
	}
	return nil
}
func (s Store) Lock() (*os.File, error) {
	if err := os.Mkdir(s.Directory, 0700); err != nil && !errors.Is(err, os.ErrExist) {
		return nil, err
	}
	var st unix.Stat_t
	if err := unix.Lstat(s.Directory, &st); err != nil {
		return nil, err
	}
	if st.Mode&unix.S_IFMT != unix.S_IFDIR || st.Uid != uint32(os.Getuid()) {
		return nil, Error("密钥库目录必须为当前用户所有的真实目录。")
	}
	if st.Mode&07777 != 0700 {
		return nil, Error("密钥库目录权限必须为 700。")
	}
	f, err := openPrivate(filepath.Join(s.Directory, ".lock"), unix.O_RDWR|unix.O_CREAT)
	if err != nil {
		return nil, err
	}
	if err = unix.Flock(int(f.Fd()), unix.LOCK_EX); err != nil {
		f.Close()
		return nil, err
	}
	return f, nil
}
func openPrivate(path string, flags int) (*os.File, error) {
	fd, err := unix.Open(path, flags|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0600)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), path)
	var st unix.Stat_t
	if err = unix.Fstat(fd, &st); err != nil {
		f.Close()
		return nil, err
	}
	if st.Mode&unix.S_IFMT != unix.S_IFREG || st.Uid != uint32(os.Getuid()) || st.Mode&07777 != 0600 || st.Nlink != 1 {
		f.Close()
		return nil, Error("密钥库文件必须为当前用户所有、权限 600 的普通文件，且不可有硬链接。")
	}
	return f, nil
}
func aead(password, salt []byte) (cipher.AEAD, error) {
	key, err := scrypt.Key(password, salt, 32768, 8, 1, 32)
	if err != nil {
		return nil, err
	}
	defer clear(key)
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}
func (s Store) Read(password []byte) (map[string]string, error) {
	f, err := openPrivate(s.Path(), unix.O_RDONLY)
	if errors.Is(err, os.ErrNotExist) {
		return nil, Error("尚未初始化，请先运行 zkey init。")
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(f)
	if err != nil {
		return nil, err
	}
	var env map[string]json.RawMessage
	if !utf8.Valid(data) || json.Unmarshal(data, &env) != nil || len(env) != 4 || string(env["version"]) != "1" {
		return nil, InvalidVault
	}
	parts := make([][]byte, 3)
	for i, name := range []string{"salt", "nonce", "ciphertext"} {
		var v any
		if json.Unmarshal(env[name], &v) != nil {
			return nil, InvalidVault
		}
		encoded, ok := v.(string)
		if !ok || strings.ContainsAny(encoded, "\r\n") {
			return nil, InvalidVault
		}
		parts[i], err = base64.StdEncoding.DecodeString(encoded)
		if err != nil {
			return nil, InvalidVault
		}
	}
	if len(parts[0]) != 16 || len(parts[1]) != 12 {
		return nil, InvalidVault
	}
	gcm, err := aead(password, parts[0])
	if err != nil {
		return nil, err
	}
	plain, err := gcm.Open(nil, parts[1], parts[2], []byte(aad))
	if err != nil {
		return nil, InvalidVault
	}
	defer clear(plain)
	var values map[string]any
	if !utf8.Valid(plain) || json.Unmarshal(plain, &values) != nil || values == nil {
		return nil, InvalidVault
	}
	result := make(map[string]string, len(values))
	for k, v := range values {
		value, ok := v.(string)
		if !ok || !Name.MatchString(k) || strings.ContainsRune(value, 0) {
			return nil, InvalidVault
		}
		result[k] = value
	}
	return result, nil
}
func (s Store) Write(values map[string]string, password []byte) error {
	salt, nonce := make([]byte, 16), make([]byte, 12)
	if _, err := rand.Read(salt); err != nil {
		return err
	}
	if _, err := rand.Read(nonce); err != nil {
		return err
	}
	gcm, err := aead(password, salt)
	if err != nil {
		return err
	}
	plain, err := json.Marshal(values)
	if err != nil {
		return err
	}
	defer clear(plain)
	ciphertext := gcm.Seal(nil, nonce, plain, []byte(aad))
	envelope := struct {
		Version    int    `json:"version"`
		Salt       string `json:"salt"`
		Nonce      string `json:"nonce"`
		Ciphertext string `json:"ciphertext"`
	}{1, base64.StdEncoding.EncodeToString(salt), base64.StdEncoding.EncodeToString(nonce), base64.StdEncoding.EncodeToString(ciphertext)}
	f, err := os.CreateTemp(s.Directory, ".vault-")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if err = f.Chmod(0600); err != nil {
		return err
	}
	if err = json.NewEncoder(f).Encode(envelope); err != nil {
		return err
	}
	if err = f.Sync(); err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	if err = os.Rename(f.Name(), s.Path()); err != nil {
		return err
	}
	dir, err := os.Open(s.Directory)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}
func (s Store) Exists() (bool, error) {
	_, err := os.Lstat(s.Path())
	if err == nil {
		return true, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return false, err
}
