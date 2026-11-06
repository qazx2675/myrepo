// Package info 는 info.txt (계정 수동 마운트 정보)와 hosts.txt (대상 호스트 목록)를
// 읽어들여 형식을 검증하고 렌더링 데이터를 생성합니다.
//
// info.txt 양식 (줄바꿈 단위, 공백/탭 구분):
//   계정 GID/UID(둘다값이같음) 스토리지
//   예) asdf 1234 qwer12345:/asdf/asdf
package info

import (
	"bufio"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
)

var validUserRe = regexp.MustCompile(`^[a-zA-Z0-9._-]+$`)

// Account 는 info.txt 한 줄의 계정 마운트 정보를 나타냅니다.
type Account struct {
	User    string // 계정명 (예: asdf)
	ID      string // UID/GID 동일값 (예: 1234)
	Storage string // 스토리지 경로 (예: qwer12345:/asdf/asdf, 끝 슬래시 제거됨)
}

// PasswdLine 은 /etc/passwd 에 기록될 한 줄을 반환합니다.
// 형식: 계정:x:UID:GID:r계정:/user/계정:/bin/csh
func (a Account) PasswdLine() string {
	return fmt.Sprintf("%s:x:%s:%s:r%s:/user/%s:/bin/csh", a.User, a.ID, a.ID, a.User, a.User)
}

// GroupLine 은 /etc/group 에 기록될 한 줄을 반환합니다.
// 형식: 계정:x:UID:
func (a Account) GroupLine() string {
	return fmt.Sprintf("%s:x:%s:", a.User, a.ID)
}

// AutoHomeLine 은 /etc/auto.home 에 기록될 한 줄을 반환합니다.
// 형식: 계정 -rw,hard,actimeo=1,proto=tcp,vers=3 스토리지/계정
func (a Account) AutoHomeLine() string {
	return fmt.Sprintf("%s -rw,hard,actimeo=1,proto=tcp,vers=3 %s/%s", a.User, a.Storage, a.User)
}

// LoadAccounts 는 지정한 경로(기본 info.txt)에서 계정 목록을 읽어 검증합니다.
// 빈 줄과 '#' 으로 시작하는 주석 줄은 건너뜁니다.
// 동일한 계정명이 여러 번 나오면 오류를 반환합니다.
func LoadAccounts(path string) ([]Account, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	seenUser := map[string]int{}
	var out []Account

	sc := bufio.NewScanner(f)
	lineNo := 0
	for sc.Scan() {
		lineNo++
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		fields := strings.Fields(line)
		if len(fields) != 3 {
			return nil, fmt.Errorf("%s:%d: \"계정 UID/GID 스토리지\" 3개 필드 형식이 아닙니다: %q", path, lineNo, line)
		}
		user, idStr, storage := fields[0], fields[1], fields[2]

		if !validUserRe.MatchString(user) {
			return nil, fmt.Errorf("%s:%d: 유효하지 않은 계정명입니다: %q", path, lineNo, user)
		}
		uid, err := strconv.Atoi(idStr)
		if err != nil || uid <= 0 {
			return nil, fmt.Errorf("%s:%d: UID/GID 는 양의 정수여야 합니다: %q", path, lineNo, idStr)
		}
		storage = strings.TrimRight(storage, "/")
		if storage == "" || !strings.Contains(storage, ":/") {
			return nil, fmt.Errorf("%s:%d: 스토리지는 \"호스트:/경로\" 형식이어야 합니다: %q", path, lineNo, fields[2])
		}
		if strings.ContainsAny(storage, " \t:") && strings.Count(storage, ":") != 1 {
			return nil, fmt.Errorf("%s:%d: 스토리지 형식에 콜론(:)이 올바르지 않습니다: %q", path, lineNo, fields[2])
		}

		if prevLine, dup := seenUser[user]; dup {
			return nil, fmt.Errorf("%s:%d: 계정 %q 이 중복됩니다 (이전 등장: %d번째 줄)", path, lineNo, user, prevLine)
		}
		seenUser[user] = lineNo

		out = append(out, Account{
			User:    user,
			ID:      strconv.Itoa(uid),
			Storage: storage,
		})
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%s: 등록할 계정 정보가 하나도 없습니다", path)
	}
	return out, nil
}

// LoadHosts 는 대상 호스트 목록 파일에서 hostname/IP 목록을 읽어들입니다.
// 빈 줄과 '#' 으로 시작하는 줄은 건너뛰며, 중복 호스트는 오류로 거부합니다.
func LoadHosts(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	seen := map[string]int{}
	var out []string

	sc := bufio.NewScanner(f)
	lineNo := 0
	for sc.Scan() {
		lineNo++
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 1 {
			return nil, fmt.Errorf("%s:%d: 한 줄에 호스트(hostname 또는 IP) 하나만 적어야 합니다: %q", path, lineNo, line)
		}
		host := fields[0]
		if prev, dup := seen[host]; dup {
			return nil, fmt.Errorf("%s:%d: 호스트 %q 이 중복됩니다 (이전 등장: %d번째 줄)", path, lineNo, host, prev)
		}
		seen[host] = lineNo
		out = append(out, host)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%s: 대상 호스트가 하나도 없습니다", path)
	}
	return out, nil
}
