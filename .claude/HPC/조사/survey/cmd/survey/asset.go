package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// AssetRow 는 표1의 한 행에서 뽑아낸 정보다. 자산ID는 표2에 불필요하므로 버린다.
type AssetRow struct {
	Hostname string
	Status   string
	Location string
}

// LoadAsset 은 표1 텍스트를 읽어 조사 대상 hostname 순서 목록과
// hostname -> (상태, 위치) 매핑을 돌려준다.
//
// 기대 양식: 자산ID <TAB> hostname <TAB> 상태 <TAB> 위치
//   - 탭(\t) 구분. 상태·위치에 공백이 들어갈 수 있으므로 탭이 있으면 탭으로만 나눈다.
//   - 탭이 없는 줄은 공백으로 나눈다(구식 파일 대비).
//   - 헤더 행(2번째 필드가 "hostname")과 빈 줄, '#' 주석은 건너뛴다.
//   - 필드가 2개 미만이거나 hostname 이 비면 건너뛰고 stderr 에 경고한다.
func LoadAsset(path string) (order []string, rows map[string]AssetRow, err error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()

	rows = map[string]AssetRow{}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		raw := sc.Text()
		if strings.TrimSpace(raw) == "" || strings.HasPrefix(strings.TrimSpace(raw), "#") {
			continue
		}

		var fields []string
		if strings.Contains(raw, "\t") {
			fields = strings.Split(raw, "\t")
		} else {
			fields = strings.Fields(raw)
		}
		for i := range fields {
			fields[i] = strings.TrimSpace(fields[i])
		}
		for len(fields) > 0 && fields[len(fields)-1] == "" {
			fields = fields[:len(fields)-1]
		}
		if len(fields) < 2 {
			fmt.Fprintf(os.Stderr, "[warn] 필드 부족으로 건너뜀: %q\n", raw)
			continue
		}
		host := fields[1]
		if host == "" {
			fmt.Fprintf(os.Stderr, "[warn] hostname 열이 비어 건너뜀: %q\n", raw)
			continue
		}
		if strings.EqualFold(host, "hostname") {
			continue // 헤더 행
		}
		if _, dup := rows[host]; dup {
			fmt.Fprintf(os.Stderr, "[warn] 중복 hostname, 첫 행 유지: %s\n", host)
			continue
		}
		row := AssetRow{Hostname: host}
		if len(fields) >= 3 {
			row.Status = fields[2]
		}
		if len(fields) >= 4 {
			row.Location = fields[3]
		}
		order = append(order, host)
		rows[host] = row
	}
	if err := sc.Err(); err != nil {
		return nil, nil, err
	}
	if len(order) == 0 {
		return nil, nil, fmt.Errorf("자산양식에서 hostname을 찾지 못함: %s", path)
	}
	return order, rows, nil
}
