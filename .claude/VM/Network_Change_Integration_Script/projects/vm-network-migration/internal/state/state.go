// Package state 는 롤백에 필요한 "작업 전 상태"를 파일로 남기고 다시 읽습니다.
//
// 프로세스가 중간에 죽거나, 며칠 뒤에 별도 롤백 바이너리만 실행하는 경우에도
// 원복이 가능해야 하므로 메모리가 아니라 state_{user}.json 파일에 남깁니다.
package state

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Record 는 VM 한 대의 작업 전 네트워크 상태와 목표 상태입니다.
type Record struct {
	VMName  string `json:"vm_name"`
	VMUUID  string `json:"vm_uuid"` // 이름이 바뀌어도 VM 을 다시 찾기 위한 식별자
	VCenter string `json:"vcenter"`
	BMHost  string `json:"bm_host"` // 백업 시점에 VM 이 올라가 있던 ESXi 호스트

	NicKey   int32  `json:"nic_key"`
	NicLabel string `json:"nic_label"`

	// 작업 전 상태 (롤백 대상)
	OrigPG             string `json:"orig_portgroup"`
	OrigConnected      bool   `json:"orig_connected"`
	OrigStartConnected bool   `json:"orig_start_connected"`

	// 목표 상태 (worklist 에서 결정)
	TargetPG   string `json:"target_portgroup"`
	TargetVLAN int32  `json:"target_vlan"`
}

// File 은 state_{user}.json 전체 구조입니다.
type File struct {
	User      string    `json:"user"`
	CreatedAt time.Time `json:"created_at"`
	NicIndex  int       `json:"nic_index"`
	Records   []Record  `json:"records"`
}

// Save 는 상태 파일을 원자적으로 씁니다.
// 같은 디렉터리에 임시 파일을 만든 뒤 rename 하므로, 쓰는 도중 죽어도
// 반쯤 잘린 상태 파일이 남지 않습니다.
func Save(path string, f *File) error {
	buf, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	buf = append(buf, '\n')

	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".state-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // rename 성공 시엔 이미 없어서 무해

	if _, err := tmp.Write(buf); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

// Load 는 상태 파일을 읽습니다.
func Load(path string) (*File, error) {
	buf, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var f File
	if err := json.Unmarshal(buf, &f); err != nil {
		return nil, fmt.Errorf("%s 파싱 실패: %w", path, err)
	}
	if len(f.Records) == 0 {
		return nil, fmt.Errorf("%s 에 복구할 레코드가 없습니다", path)
	}
	return &f, nil
}

// Remove 는 지정한 VM 들의 레코드를 상태에서 뺍니다.
//
// 이미 원본으로 되돌린 VM 을 남겨두면, 뒤이어 진행되는 단계들이 그 VM 을 다시
// 건드리게 됩니다. 롤백이 끝난 VM 은 이번 작업 대상에서 빠져야 합니다.
func (f *File) Remove(names []string) int {
	if len(names) == 0 {
		return 0
	}
	drop := make(map[string]bool, len(names))
	for _, n := range names {
		drop[strings.ToLower(n)] = true
	}
	kept := make([]Record, 0, len(f.Records))
	removed := 0
	for _, r := range f.Records {
		if drop[strings.ToLower(r.VMName)] {
			removed++
			continue
		}
		kept = append(kept, r)
	}
	f.Records = kept
	return removed
}

// Filter 는 지정한 VM 이름들만 남깁니다. names 가 비면 전체를 그대로 돌려줍니다.
// 실패한 VM 만 골라서 롤백할 때 씁니다.
func (f *File) Filter(names []string) ([]Record, error) {
	if len(names) == 0 {
		return f.Records, nil
	}
	want := make(map[string]bool, len(names))
	for _, n := range names {
		want[strings.ToLower(n)] = true
	}

	var out []Record
	for _, r := range f.Records {
		if want[strings.ToLower(r.VMName)] {
			out = append(out, r)
			delete(want, strings.ToLower(r.VMName))
		}
	}
	if len(want) > 0 {
		missing := make([]string, 0, len(want))
		for n := range want {
			missing = append(missing, n)
		}
		return nil, fmt.Errorf("상태 파일에 없는 VM 이 지정되었습니다: %s", strings.Join(missing, ", "))
	}
	return out, nil
}
