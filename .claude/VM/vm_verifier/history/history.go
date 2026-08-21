// Package history는 5단계(UUID 이력 대조)에 쓸 hostname -> 마지막 확인된 UUID를
// 로컬 JSON 파일로 보관한다. PLAN.md 6장 "UUID 이력 저장소" 항목의 v1 임시 구현이며,
// 중앙 감사 로그 저장소가 확정되면 그쪽으로 옮길 수 있다.
package history

import (
	"encoding/json"
	"os"
)

// Load는 저장 파일을 읽는다. 파일이 없으면 빈 맵을 반환한다(최초 실행).
func Load(path string) (map[string]string, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return map[string]string{}, nil
	}
	if err != nil {
		return nil, err
	}
	var m map[string]string
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	return m, nil
}

// Save는 hostname -> UUID 맵을 파일에 기록한다.
func Save(path string, m map[string]string) error {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}
