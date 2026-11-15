package storage

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sync"
)

// Ошибки хранилища
var (
	// документа с таким именем еще нет
	ErrNotFound = errors.New("document not found")
	// файл поврежден, подменен или зашифрован другим ключом
	ErrCorrupt = errors.New("document is corrupt")
	// ключ данных не удалось развернуть (другая учетная
	// запись или другой компьютер)
	ErrKeyUnavailable = errors.New("data key is unavailable")
	// недопустимое имя документа
	ErrBadName = errors.New("bad document name")
)

const (
	keyFile = "key.bin"
	docExt  = ".dfx"
	keySize = 32
)

var (
	magic     = []byte("DFX1")
	nameRegex = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)
)

// заворачивает ключ данных средствами операционной системы.
type KeyProtector interface {
	Protect(plain []byte) ([]byte, error)
	Unprotect(blob []byte) ([]byte, error)
}

type Vault struct {
	dir  string
	aead cipher.AEAD
	mu   sync.Mutex
}

func Open(dir string, kp KeyProtector) (*Vault, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}
	key, err := loadOrCreateKey(filepath.Join(dir, keyFile), kp)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("init cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("init cipher mode: %w", err)
	}
	return &Vault{dir: dir, aead: aead}, nil
}

func (v *Vault) Save(name string, value any) error {
	path, err := v.path(name)
	if err != nil {
		return err
	}
	plain, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("save %q: encode: %w", name, err)
	}
	nonce := make([]byte, v.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return fmt.Errorf("save %q: nonce: %w", name, err)
	}

	// Имя документа входит в подпись
	buf := make([]byte, 0, len(magic)+len(nonce)+len(plain)+v.aead.Overhead())
	buf = append(buf, magic...)
	buf = append(buf, nonce...)
	buf = v.aead.Seal(buf, nonce, plain, []byte(name))

	v.mu.Lock()
	defer v.mu.Unlock()
	if err := writeAtomic(path, buf); err != nil {
		return fmt.Errorf("save %q: %w", name, err)
	}
	return nil
}

func (v *Vault) Load(name string, out any) error {
	path, err := v.path(name)
	if err != nil {
		return err
	}

	v.mu.Lock()
	raw, err := os.ReadFile(path)
	v.mu.Unlock()
	if errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("load %q: %w", name, ErrNotFound)
	}
	if err != nil {
		return fmt.Errorf("load %q: %w", name, err)
	}

	head := len(magic) + v.aead.NonceSize()
	if len(raw) < head || !bytes.Equal(raw[:len(magic)], magic) {
		return fmt.Errorf("load %q: %w: bad header", name, ErrCorrupt)
	}
	plain, err := v.aead.Open(nil, raw[len(magic):head], raw[head:], []byte(name))
	if err != nil {
		return fmt.Errorf("load %q: %w: %w", name, ErrCorrupt, err)
	}
	if err := json.Unmarshal(plain, out); err != nil {
		return fmt.Errorf("load %q: %w: %w", name, ErrCorrupt, err)
	}
	return nil
}

func (v *Vault) Delete(name string) error {
	path, err := v.path(name)
	if err != nil {
		return err
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("delete %q: %w", name, err)
	}
	return nil
}

func (v *Vault) path(name string) (string, error) {
	if !nameRegex.MatchString(name) {
		return "", fmt.Errorf("%w: %q", ErrBadName, name)
	}
	return filepath.Join(v.dir, name+docExt), nil
}

func loadOrCreateKey(path string, kp KeyProtector) ([]byte, error) {
	blob, err := os.ReadFile(path)
	switch {
	case err == nil:
		key, err := kp.Unprotect(blob)
		if err != nil {
			return nil, fmt.Errorf("%w: %w", ErrKeyUnavailable, err)
		}
		if len(key) != keySize {
			return nil, fmt.Errorf("%w: unexpected key size %d", ErrKeyUnavailable, len(key))
		}
		return key, nil
	case !errors.Is(err, fs.ErrNotExist):
		return nil, fmt.Errorf("read data key: %w", err)
	}

	key := make([]byte, keySize)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("generate data key: %w", err)
	}
	blob, err = kp.Protect(key)
	if err != nil {
		return nil, fmt.Errorf("protect data key: %w", err)
	}
	if err := writeAtomic(path, blob); err != nil {
		return nil, fmt.Errorf("store data key: %w", err)
	}
	return key, nil
}

func writeAtomic(path string, data []byte) (err error) {
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*.tmp")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tmp.Close()
			_ = os.Remove(tmp.Name())
		}
	}()

	if _, err = tmp.Write(data); err != nil {
		return fmt.Errorf("write temp file: %w", err)
	}
	if err = tmp.Sync(); err != nil {
		return fmt.Errorf("flush temp file: %w", err)
	}
	if err = tmp.Close(); err != nil {
		return fmt.Errorf("close temp file: %w", err)
	}
	if err = os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("replace file: %w", err)
	}
	return nil
}
