// Package history는 지금까지 접속했던 vCenter 목록(~/.vc-test-env/history.json)과
// 대상별 레시피 캐시(~/.vc-test-env/recipes/<target>.json)를 관리한다.
// 비밀번호는 절대 저장하지 않는다.
package history

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type History struct {
	Targets []string `json:"targets"`
}

func dir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("홈 디렉터리 확인 실패: %w", err)
	}
	return filepath.Join(home, ".vc-test-env"), nil
}

func historyPath() (string, error) {
	d, err := dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "history.json"), nil
}

// RecipePath는 target에 대응하는 캐시된 레시피 파일 경로를 반환한다.
func RecipePath(target string) (string, error) {
	d, err := dir()
	if err != nil {
		return "", err
	}
	safe := strings.NewReplacer(":", "_", "/", "_", "\\", "_").Replace(target)
	return filepath.Join(d, "recipes", safe+".json"), nil
}

func Load() (*History, error) {
	p, err := historyPath()
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(p)
	if os.IsNotExist(err) {
		return &History{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("히스토리 로드 실패: %w", err)
	}
	var h History
	if err := json.Unmarshal(b, &h); err != nil {
		return nil, fmt.Errorf("히스토리 파싱 실패: %w", err)
	}
	return &h, nil
}

func (h *History) Add(target string) error {
	for _, t := range h.Targets {
		if t == target {
			return nil
		}
	}
	h.Targets = append(h.Targets, target)

	p, err := historyPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
		return fmt.Errorf("히스토리 디렉터리 생성 실패: %w", err)
	}
	b, err := json.MarshalIndent(h, "", "  ")
	if err != nil {
		return fmt.Errorf("히스토리 직렬화 실패: %w", err)
	}
	if err := os.WriteFile(p, b, 0600); err != nil {
		return fmt.Errorf("히스토리 저장 실패: %w", err)
	}
	return nil
}
