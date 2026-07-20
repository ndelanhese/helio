package storage

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

// SecretStore persists integration credentials encrypted at rest. Its key is
// supplied by the operator through HELIO_SECRETS_KEY and never stored in SQLite.
type SecretStore struct {
	db  *DB
	gcm cipher.AEAD
}

// LoadOrCreateSecretsKey keeps a generated key outside the SQLite backup.
// Operators may instead supply HELIO_SECRETS_KEY for externally managed keys.
func LoadOrCreateSecretsKey(databasePath string) ([]byte, error) {
	path := filepath.Join(filepath.Dir(databasePath), ".helio-secrets.key")
	raw, err := os.ReadFile(path)
	if err == nil {
		key, err := base64.RawStdEncoding.DecodeString(string(raw))
		if err != nil || len(key) != 32 {
			return nil, errors.New("stored secrets key is invalid")
		}
		return key, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("read secrets key: %w", err)
	}
	key := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return nil, fmt.Errorf("generate secrets key: %w", err)
	}
	if err := os.WriteFile(path, []byte(base64.RawStdEncoding.EncodeToString(key)), 0o600); err != nil {
		return nil, fmt.Errorf("store secrets key: %w", err)
	}
	return key, nil
}

func NewSecretStore(db *DB, key []byte) (*SecretStore, error) {
	if len(key) != 32 {
		return nil, errors.New("HELIO_SECRETS_KEY must be a base64-encoded 32-byte key")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("create secrets cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create secrets encryption: %w", err)
	}
	return &SecretStore{db: db, gcm: gcm}, nil
}

func (s *SecretStore) Put(ctx context.Context, name string, value any) error {
	if s == nil || s.db == nil || s.gcm == nil {
		return errors.New("encrypted secrets are unavailable")
	}
	plain, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("encode secret: %w", err)
	}
	nonce := make([]byte, s.gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return fmt.Errorf("generate secret nonce: %w", err)
	}
	ciphertext := s.gcm.Seal(nil, nonce, plain, []byte(name))
	payload := base64.RawStdEncoding.EncodeToString(append(nonce, ciphertext...))
	_, err = s.db.sql.ExecContext(ctx, `
		INSERT INTO integration_secrets(name, ciphertext, updated_at) VALUES(?, ?, ?)
		ON CONFLICT(name) DO UPDATE SET ciphertext=excluded.ciphertext, updated_at=excluded.updated_at`, name, payload, formatTime(time.Now().UTC()))
	if err != nil {
		return fmt.Errorf("save secret: %w", err)
	}
	return nil
}

func (s *SecretStore) Get(ctx context.Context, name string, dst any) (bool, error) {
	if s == nil || s.db == nil || s.gcm == nil {
		return false, errors.New("encrypted secrets are unavailable")
	}
	var payload string
	err := s.db.sql.QueryRowContext(ctx, `SELECT ciphertext FROM integration_secrets WHERE name=?`, name).Scan(&payload)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("load secret: %w", err)
	}
	raw, err := base64.RawStdEncoding.DecodeString(payload)
	if err != nil || len(raw) < s.gcm.NonceSize() {
		return false, errors.New("stored secret is corrupt")
	}
	plain, err := s.gcm.Open(nil, raw[:s.gcm.NonceSize()], raw[s.gcm.NonceSize():], []byte(name))
	if err != nil {
		return false, errors.New("stored secret cannot be decrypted")
	}
	if err := json.Unmarshal(plain, dst); err != nil {
		return false, errors.New("stored secret is corrupt")
	}
	return true, nil
}
