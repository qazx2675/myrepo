package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"fmt"
	"io"
	"os"
)

// cryptutil 은 새 비밀번호를 AES-256-GCM 으로 대칭 암복호화합니다.
//
// 키 파일(key.bin)은 32바이트 난수이며 권한 0600 으로 생성합니다. 평문
// 비밀번호는 메모리에서만 다루고 로그/디스크에 남기지 않습니다.

const keySize = 32 // AES-256

// loadOrCreateKey 는 키 파일을 읽어들이고, 없으면 새로 만들어(0600) 돌려줍니다.
func loadOrCreateKey(path string) ([]byte, error) {
	key, err := os.ReadFile(path)
	if err == nil {
		if len(key) != keySize {
			return nil, fmt.Errorf("키 파일 %s: 길이가 %d바이트여야 합니다 (현재 %d)", path, keySize, len(key))
		}
		return key, nil
	}
	if !os.IsNotExist(err) {
		return nil, err
	}
	key = make([]byte, keySize)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, key, 0o600); err != nil {
		return nil, err
	}
	return key, nil
}

// loadKey 는 기존 키 파일을 읽어들입니다 (복호화용, 없으면 오류).
func loadKey(path string) ([]byte, error) {
	key, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(key) != keySize {
		return nil, fmt.Errorf("키 파일 %s: 길이가 %d바이트여야 합니다 (현재 %d)", path, keySize, len(key))
	}
	return key, nil
}

// encrypt 는 평문을 AES-256-GCM 으로 암호화해 nonce||ciphertext 를 돌려줍니다.
func encrypt(plaintext, key []byte) ([]byte, error) {
	gcm, err := newGCM(key)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	return gcm.Seal(nonce, nonce, plaintext, nil), nil
}

// decrypt 는 nonce||ciphertext 를 복호화해 평문을 돌려줍니다.
func decrypt(data, key []byte) ([]byte, error) {
	gcm, err := newGCM(key)
	if err != nil {
		return nil, err
	}
	ns := gcm.NonceSize()
	if len(data) < ns {
		return nil, fmt.Errorf("암호문이 너무 짧습니다")
	}
	nonce, ct := data[:ns], data[ns:]
	return gcm.Open(nil, nonce, ct, nil)
}

func newGCM(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}
