// Package secrets implements the plan's "옵션 A" credential storage: vCenter/ESXi
// passwords are AES-256-GCM encrypted at the application layer before hitting
// SQLite, with the master key kept in a separate 0600 file outside the DB
// (or, optionally, in HashiCorp Vault — see vault.go / LoadMasterKey).
package secrets

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const keySize = 32 // AES-256

// LoadMasterKey resolves the AES-256 master key using the configured backend
// ("file" — default, or "vault"). See config.Config.SecretBackend.
func LoadMasterKey(backend, fileKeyPath, vaultAddr, vaultToken, vaultSecretPath, vaultKeyField string) ([]byte, error) {
	switch backend {
	case "", "file":
		return LoadOrCreateMasterKey(fileKeyPath)
	case "vault":
		return LoadMasterKeyFromVault(vaultAddr, vaultToken, vaultSecretPath, vaultKeyField)
	default:
		return nil, fmt.Errorf("알 수 없는 SecretBackend: %q (file 또는 vault)", backend)
	}
}

// LoadOrCreateMasterKey reads the master key from path, generating and
// persisting a new random one (mode 0600) the first time the file is missing.
func LoadOrCreateMasterKey(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err == nil {
		if len(data) != keySize {
			return nil, errors.New("master key file has unexpected length; delete it to regenerate (this invalidates stored credentials)")
		}
		return data, nil
	}
	if !os.IsNotExist(err) {
		return nil, err
	}

	key := make([]byte, keySize)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, key, 0600); err != nil {
		return nil, err
	}
	return key, nil
}

// Encrypt returns (ciphertext, nonce). The nonce must be stored alongside
// the ciphertext and passed back into Decrypt.
func Encrypt(key, plaintext []byte) (ciphertext, nonce []byte, err error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, nil, err
	}
	nonce = make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, nil, err
	}
	ciphertext = gcm.Seal(nil, nonce, plaintext, nil)
	return ciphertext, nonce, nil
}

func Decrypt(key, ciphertext, nonce []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return gcm.Open(nil, nonce, ciphertext, nil)
}
