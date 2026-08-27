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
// 기대 양식(공백 또는 탭 구분): 자산ID  hostname  상태  위치
//   - 헤더 행(2번째 필드가 "hostname")과 빈 줄, '#' 주석은 건너뛴다.
//   - 필드가 2개 미만인 행은 건너뛰고 stderr 에 경고한다.
//   - 위치에 공백이 있을 수 있으므로 4번째 이후 필드는 이어붙여 위치로 본다.
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
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			fmt.Fprintf(os.Stderr, "[warn] 필드 부족으로 건너뜀: %q\n", line)
			continue
		}
		host := fields[1]
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
			row.Location = strings.Join(fields[3:], " ")
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
