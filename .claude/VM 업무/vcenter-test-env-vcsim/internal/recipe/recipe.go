// Package recipe는 inventory.Inventory를 JSON 파일로 저장/로드한다.
package recipe

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"vc-test-env/internal/inventory"
)

type Recipe struct {
	Source      string               `json:"source"`
	ExtractedAt string               `json:"extractedAt"`
	Inventory   *inventory.Inventory `json:"inventory"`
}

func Save(path string, r *Recipe) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return fmt.Errorf("레시피 저장 디렉터리 생성 실패: %w", err)
	}
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return fmt.Errorf("레시피 직렬화 실패: %w", err)
	}
	if err := os.WriteFile(path, b, 0600); err != nil {
		return fmt.Errorf("레시피 저장 실패(%s): %w", path, err)
	}
	return nil
}

func Load(path string) (*Recipe, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("레시피 로드 실패(%s): %w", path, err)
	}
	var r Recipe
	if err := json.Unmarshal(b, &r); err != nil {
		return nil, fmt.Errorf("레시피 파싱 실패(%s): %w", path, err)
	}
	return &r, nil
}
