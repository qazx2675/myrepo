package secrets

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

// kv2Response mirrors the parts of Vault's KV v2 read response we need:
// GET {addr}/v1/{path} -> {"data": {"data": {<field>: "<base64 key>"}}}.
type kv2Response struct {
	Data struct {
		Data map[string]string `json:"data"`
	} `json:"data"`
}

// LoadMasterKeyFromVault fetches the AES-256 master key from a HashiCorp
// Vault KV v2 secret engine over its HTTP API. This is M9's optional
// alternative to the local master-key file (LoadOrCreateMasterKey) — it
// does not create or rotate the secret, only reads it, since managing the
// Vault-side secret lifecycle is out of scope for a CLI-wrapping portal.
//
// The secret must already exist at secretPath with a field (default "key")
// holding the 32-byte AES key, standard-base64 encoded.
func LoadMasterKeyFromVault(addr, token, secretPath, keyField string) ([]byte, error) {
	if addr == "" || token == "" {
		return nil, errors.New("vault: VAULT_ADDR / VAULT_TOKEN 이 설정되지 않았습니다")
	}
	if keyField == "" {
		keyField = "key"
	}

	url := addr + "/v1/" + secretPath
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("vault: 요청 생성 실패: %w", err)
	}
	req.Header.Set("X-Vault-Token", token)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("vault: %s 접속 실패: %w", addr, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("vault: 응답 읽기 실패: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("vault: %s 응답 %d: %s", secretPath, resp.StatusCode, string(body))
	}

	var parsed kv2Response
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("vault: 응답 파싱 실패: %w", err)
	}

	encoded, ok := parsed.Data.Data[keyField]
	if !ok {
		return nil, fmt.Errorf("vault: %s 경로의 시크릿에 %q 필드가 없습니다", secretPath, keyField)
	}

	key, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("vault: %s 필드가 유효한 base64 가 아닙니다: %w", keyField, err)
	}
	if len(key) != keySize {
		return nil, fmt.Errorf("vault: 키 길이가 %d바이트가 아니라 %d바이트입니다", keySize, len(key))
	}
	return key, nil
}
