package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"fmt"
	"io"
	"os"
	"strings"
)

// cryptutil 은 BMC 비밀번호를 AES-256-GCM 으로 대칭 암복호화합니다.
// (패스워드변경자동화/cmd/pwreset/cryptutil.go 와 같은 방식)
//
// 키 파일(key.bin)은 32바이트 난수이며 권한 0600 으로 생성합니다. 평문
// 비밀번호는 메모리에서만 다루고 로그/디스크/명령행 인자에 남기지 않습니다.

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

// newGCM 은 32바이트 키만 받습니다 (aes.NewCipher 는 16/24바이트도 허용하므로 직접 막는다).
func newGCM(key []byte) (cipher.AEAD, error) {
	if len(key) != keySize {
		return nil, fmt.Errorf("키 길이가 %d바이트여야 합니다 (현재 %d)", keySize, len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// encryptFromReader 는 r 에서 평문 비밀번호를 읽어(끝 개행 제거) 암호화해 outPath 에 저장합니다.
// 키 파일이 없으면 새로 만듭니다. 결과 파일 권한은 0600 입니다.
func encryptFromReader(r io.Reader, outPath, keyPath string) error {
	raw, err := io.ReadAll(r)
	if err != nil {
		return fmt.Errorf("표준입력: %w", err)
	}
	plain := strings.TrimRight(string(raw), "\r\n")
	if plain == "" {
		return fmt.Errorf("비밀번호가 비어 있습니다")
	}
	k, err := loadOrCreateKey(keyPath)
	if err != nil {
		return fmt.Errorf("키 파일: %w", err)
	}
	ct, err := encrypt([]byte(plain), k)
	if err != nil {
		return fmt.Errorf("암호화: %w", err)
	}
	if err := os.WriteFile(outPath, ct, 0o600); err != nil {
		return fmt.Errorf("암호문 저장: %w", err)
	}
	// 기존 파일을 덮어쓴 경우 WriteFile 은 권한을 바꾸지 않으므로 명시적으로 맞춘다.
	if err := os.Chmod(outPath, 0o600); err != nil {
		return fmt.Errorf("암호문 권한 설정: %w", err)
	}
	return nil
}

// readPassword 는 pass_file 을 key_file 로 복호화해 평문 비밀번호를 돌려줍니다.
// 반환값은 메모리에서만 쓰고 출력하지 않습니다.
func readPassword(passFile, keyFile string) (string, error) {
	k, err := loadKey(keyFile)
	if err != nil {
		return "", fmt.Errorf("키 파일: %w", err)
	}
	enc, err := os.ReadFile(passFile)
	if err != nil {
		return "", fmt.Errorf("비밀번호 파일: %w", err)
	}
	plain, err := decrypt(enc, k)
	if err != nil {
		return "", fmt.Errorf("비밀번호 복호화 실패 (pass.enc 와 key.bin 이 짝이 맞는지 확인): %w", err)
	}
	return string(plain), nil
}
