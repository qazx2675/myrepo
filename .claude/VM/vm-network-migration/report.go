package main

import (
	"encoding/csv"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
)

// rollbackEntry 는 롤백 CSV 한 줄입니다.
// "이 vCenter 의 이 VM 의 이 NIC 를 원래 포트그룹으로 되돌려라" 라는 뜻입니다.
type rollbackEntry struct {
	VCenter string
	VMName  string
	NicKey  int32
	OldPG   string
}

// writeRollbackCSV 는 실제로 변경된 VM 의 이전 상태를 파일로 남깁니다.
// 이 파일이 있어야 -rollback 으로 되돌릴 수 있으므로, 마이그레이션 직후 반드시 보관하세요.
func writeRollbackCSV(path string, results []Result) (int, error) {
	f, w, err := newCSV(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	defer w.Flush()

	if err := w.Write([]string{"vcenter", "vm_name", "nic_key", "old_portgroup", "new_portgroup"}); err != nil {
		return 0, err
	}
	n := 0
	for _, r := range results {
		// 실제로 바뀐 것만 기록합니다. SKIPPED/FAILED 는 되돌릴 대상이 아닙니다.
		if r.Status != StatusSuccess || r.FromPG == "" {
			continue
		}
		row := []string{r.VCenter, r.VMName, strconv.FormatInt(int64(r.NicKey), 10), r.FromPG, r.ToPG}
		if err := w.Write(row); err != nil {
			return n, err
		}
		n++
	}
	return n, w.Error()
}

// readRollbackCSV 는 롤백 파일을 읽어 되돌릴 대상 목록을 만듭니다.
func readRollbackCSV(path string) ([]rollbackEntry, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	rows, err := csv.NewReader(f).ReadAll()
	if err != nil {
		return nil, err
	}
	if len(rows) < 2 {
		return nil, fmt.Errorf("%s 에 되돌릴 항목이 없습니다", path)
	}

	var entries []rollbackEntry
	for i, row := range rows[1:] { // 첫 줄은 헤더
		if len(row) < 4 {
			return nil, fmt.Errorf("%s %d번째 줄 형식이 잘못되었습니다(vcenter,vm_name,nic_key,old_portgroup 필요)", path, i+2)
		}
		key, err := strconv.ParseInt(row[2], 10, 32)
		if err != nil {
			return nil, fmt.Errorf("%s %d번째 줄 nic_key 파싱 실패: %v", path, i+2, err)
		}
		entries = append(entries, rollbackEntry{
			VCenter: row[0], VMName: row[1], NicKey: int32(key), OldPG: row[3],
		})
	}
	return entries, nil
}

// writeReportCSV 는 전체 처리 결과를 CSV 로 남깁니다.
func writeReportCSV(path string, results []Result) error {
	f, w, err := newCSV(path)
	if err != nil {
		return err
	}
	defer f.Close()
	defer w.Flush()

	header := []string{"vcenter", "datacenter", "vm_name", "status", "nic_key", "nic_label", "from_portgroup", "to_portgroup", "message"}
	if err := w.Write(header); err != nil {
		return err
	}
	for _, r := range results {
		row := []string{
			r.VCenter, r.Datacenter, r.VMName, r.Status,
			strconv.FormatInt(int64(r.NicKey), 10),
			r.NicName, r.FromPG, r.ToPG, r.Message,
		}
		if err := w.Write(row); err != nil {
			return err
		}
	}
	return w.Error()
}

// newCSV 는 디렉터리를 만들고 권한 0600 으로 CSV 파일을 엽니다.
func newCSV(path string) (*os.File, *csv.Writer, error) {
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, nil, err
		}
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return nil, nil, err
	}
	return f, csv.NewWriter(f), nil
}

// printSummary 는 콘솔에 결과를 상태별로 묶어 출력하고 실패 건수를 돌려줍니다.
// vCenter 가 여러 대면 어느 vCenter 결과인지 함께 보여줍니다.
func printSummary(results []Result, multiVC bool) int {
	sorted := make([]Result, len(results))
	copy(sorted, results)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].Status != sorted[j].Status {
			return statusRank(sorted[i].Status) < statusRank(sorted[j].Status)
		}
		if sorted[i].VCenter != sorted[j].VCenter {
			return sorted[i].VCenter < sorted[j].VCenter
		}
		return sorted[i].VMName < sorted[j].VMName
	})

	counts := map[string]int{}
	fmt.Println()
	fmt.Println("=================== 처리 결과 ===================")
	for _, r := range sorted {
		counts[r.Status]++
		if multiVC {
			fmt.Printf("[%-7s] %-22s %-28s %s\n", r.Status, r.VCenter, r.VMName, r.Message)
		} else {
			fmt.Printf("[%-7s] %-30s %s\n", r.Status, r.VMName, r.Message)
		}
	}
	fmt.Println("-------------------------------------------------")
	fmt.Printf("총 %d대 | 성공 %d | 건너뜀 %d | 실패 %d | 예행 %d\n",
		len(results), counts[StatusSuccess], counts[StatusSkipped], counts[StatusFailed], counts[StatusDryRun])
	fmt.Println("=================================================")
	return counts[StatusFailed]
}

// statusRank 는 요약 출력에서 실패를 먼저 보여주기 위한 정렬 우선순위입니다.
func statusRank(s string) int {
	switch s {
	case StatusFailed:
		return 0
	case StatusSkipped:
		return 1
	case StatusDryRun:
		return 2
	default:
		return 3
	}
}
