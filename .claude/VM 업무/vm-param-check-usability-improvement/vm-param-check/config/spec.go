// spec.go: vCenter 인벤토리 폴더명으로 스펙 정의 파일(_spec.txt)을 찾아 읽는다.
//
// 현업에서는 VM 스펙별로 폴더명이 이미 규칙화되어 있어서(예: TST-CAE001-SAMP48c-QRST),
// 체크 대상 VM이 속한 폴더 이름만 알면 어떤 기대값을 써야 하는지가 정해진다. 그래서
// 매번 -cpu/-cores/-numa/... 를 손으로 붙이는 대신, 폴더명에 대응하는 스펙 파일을 찾아
// 같은 옵션들을 자동으로 채워 넣는다.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// caeRecord는 폴더명 2번째 레코드(차수 정보)를 알아보는 패턴이다. CAE001/CAE002/... 처럼
// 뒤의 숫자만 다른 경우는 같은 스펙으로 취급해야 해서, 비교 전에 숫자를 떼어낸다.
var caeRecord = regexp.MustCompile(`(?i)^CAE\d+$`)

// folderRecordCount는 CAE 폴더명이 가지는 하이픈 구분 레코드 개수다.
// 예: TST-CAE001-SAMP48c-QRST -> [TST, CAE001, SAMP48c, QRST]
const folderRecordCount = 4

// NormalizeFolderName은 폴더명을 스펙 비교용 형태로 정규화한다.
// 1·3·4번째 레코드는 그대로 두고, 2번째(차수) 레코드의 숫자만 떼어낸다.
// 예: TST-CAE001-SAMP48c-QRST, TST-CAE003-SAMP48c-QRST -> 둘 다 TST-CAE-SAMP48c-QRST
//
// 레코드가 4개가 아니거나 2번째가 CAE<숫자> 형태가 아니면 이 규칙의 대상이 아니므로
// ok=false를 돌려준다(Task 폴더 등 예외 경로에서 따로 처리해야 하는 케이스).
func NormalizeFolderName(name string) (string, bool) {
	records := strings.Split(name, "-")
	if len(records) != folderRecordCount {
		return "", false
	}
	if !caeRecord.MatchString(records[1]) {
		return "", false
	}
	normalized := make([]string, len(records))
	copy(normalized, records)
	normalized[1] = "CAE"
	return strings.Join(normalized, "-"), true
}

// SpecOption은 스펙 파일에 적힌 옵션 1개다. Name은 이 도구의 CLI 플래그 이름과 같다
// (예: cores, numa, affinity-ev02).
type SpecOption struct {
	Name  string
	Value string
}

// SpecMatch는 폴더명으로 찾아낸 스펙 1건이다.
type SpecMatch struct {
	FolderName string // 매칭에 사용한 vCenter 인벤토리 폴더 이름
	SpecDir    string // specRoot 하위에서 매칭된 스펙 디렉터리 경로
	SpecFile   string // 실제로 읽은 _spec.txt 경로
	Options    []SpecOption
}

// FindSpec은 specRoot 바로 아래 디렉터리들 중 folderName과 같은 스펙으로 간주되는 것을
// 찾아, 그 안의 <디렉터리명>_spec.txt 를 읽어 옵션 목록으로 돌려준다.
//
// 매칭되는 디렉터리가 없으면 (nil, nil)을 돌려준다 — 호출자가 "스펙 없음"을 사용자에게
// 알리고 멈출지 판단한다. 매칭이 여러 개면 어느 쪽인지 단정할 수 없으므로 에러다.
func FindSpec(specRoot, folderName string) (*SpecMatch, error) {
	wanted, ok := NormalizeFolderName(folderName)
	if !ok {
		return nil, fmt.Errorf("폴더명 %q은(는) CAE 폴더 규칙(하이픈 %d개 레코드, 2번째가 CAE<숫자>)과 맞지 않습니다", folderName, folderRecordCount)
	}

	entries, err := os.ReadDir(specRoot)
	if err != nil {
		return nil, fmt.Errorf("스펙 루트 %s 읽기 실패: %w", specRoot, err)
	}

	var matched []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if got, ok := NormalizeFolderName(e.Name()); ok && got == wanted {
			matched = append(matched, e.Name())
		}
	}
	if len(matched) == 0 {
		return nil, nil
	}
	if len(matched) > 1 {
		return nil, fmt.Errorf("폴더 %q에 매칭되는 스펙 디렉터리가 여러 개입니다(%s) — 하나만 남겨주세요",
			folderName, strings.Join(matched, ", "))
	}

	specDir := filepath.Join(specRoot, matched[0])
	specFile := filepath.Join(specDir, matched[0]+"_spec.txt")
	options, err := ParseSpecFile(specFile)
	if err != nil {
		return nil, err
	}
	return &SpecMatch{FolderName: folderName, SpecDir: specDir, SpecFile: specFile, Options: options}, nil
}

// ParseSpecFile은 스펙 파일을 옵션 목록으로 바꾼다.
// 형식은 이 도구의 CLI 플래그를 그대로 적는 방식이라, 한 줄에 여러 개를 적어도 되고
// 앞의 하이픈은 있어도 없어도 된다. '#' 뒤는 주석.
//
//	ht=on --cores=20 --numa=20
//	-cpu=40
//	affinity-ev02=affinity-ev02.txt   # 상대경로는 스펙 파일이 있는 폴더 기준
func ParseSpecFile(path string) ([]SpecOption, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("스펙 파일 읽기 실패: %w", err)
	}

	var options []SpecOption
	for lineNo, line := range strings.Split(string(data), "\n") {
		if i := strings.Index(line, "#"); i >= 0 {
			line = line[:i]
		}
		for _, token := range strings.Fields(line) {
			token = strings.TrimLeft(token, "-")
			name, value, found := strings.Cut(token, "=")
			if !found || name == "" {
				return nil, fmt.Errorf("%s:%d 형식 오류 (이름=값 형태가 아님): %q", path, lineNo+1, token)
			}
			options = append(options, SpecOption{Name: name, Value: value})
		}
	}
	if len(options) == 0 {
		return nil, fmt.Errorf("%s 에 옵션이 하나도 없습니다", path)
	}
	return options, nil
}
