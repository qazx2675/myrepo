package main

// 입력 파일(user.txt)·error_list.txt 파싱

import (
	"fmt"
	"net"
	"strings"
)

// 단계 결과 값
const (
	resDone    = "실행"
	resNotRun  = "미실행"
	resFail    = "실패"
	resOffline = "접속불가"
	resDNSFail = "DNS실패"
	resNone    = "-"
)

// target 은 입력 파일의 호스트 한 대다.
type target struct {
	Line    int      // 입력 줄 번호(1부터)
	Host    string   // hostname (4번째 필드)
	Fields  []string // yaml 에 기록할 12필드
	DNSFail bool
	Offline bool
	Vendor  string            // Intel / AMD / unknown (접속불가·DNS실패는 "")
	Notes   []string          // 비고 (error_list 사유 + 실행 중 생긴 사유)
	Res     map[string]string // 단계("cpu"/"stress") → 결과 값
}

func (t *target) addNote(s string) {
	s = strings.ReplaceAll(s, "|", "/")
	for _, n := range t.Notes {
		if n == s {
			return
		}
	}
	t.Notes = append(t.Notes, s)
}

// lookupHost 는 DNS 조회 함수다. 테스트에서 바꾼다.
var lookupHost = net.LookupHost

// macFromLine 은 hostname 한 줄 양식에 쓸 MAC 을 입력 줄 번호로 만든다 (02:00:00:xx:xx:xx).
func macFromLine(n int) string {
	return fmt.Sprintf("02:00:00:%02x:%02x:%02x", (n>>16)&0xff, (n>>8)&0xff, n&0xff)
}

// firstIPv4 는 hostname 을 DNS 조회해 첫 IPv4 를 돌려준다. 없으면 "".
func firstIPv4(host string) string {
	addrs, err := lookupHost(host)
	if err != nil {
		return ""
	}
	for _, a := range addrs {
		if ip := net.ParseIP(a); ip != nil && ip.To4() != nil {
			return a
		}
	}
	return ""
}

// parseHosts 는 입력 파일 본문을 줄 단위로 판정해 target 목록을 만든다.
//   - 필드 1개 = hostname 한 줄 양식 → tmp tmp tmp <host> <IP> <MAC> eth0 sda sda5 100 8.10 uefi
//   - 필드 12/13개 = AWX 설치양식 레코드 (yaml 에는 앞 12필드만)
//   - 그 외 = 오류 (줄 번호와 함께 모두 모아 돌려줌)
//   - 빈 줄·# 줄 무시, 중복 hostname(대소문자 무시)은 첫 줄만
func parseHosts(text string) ([]*target, []string) {
	var list []*target
	var errs []string
	seen := map[string]bool{}
	text = strings.TrimPrefix(text, "\xef\xbb\xbf")
	for i, ln := range strings.Split(text, "\n") {
		ln = strings.TrimSpace(ln)
		if ln == "" || strings.HasPrefix(ln, "#") {
			continue
		}
		f := strings.Fields(ln)
		t := &target{Line: i + 1, Res: map[string]string{}}
		switch len(f) {
		case 1:
			t.Host = f[0]
		case 12, 13:
			t.Host = f[3]
			t.Fields = f[:12]
		default:
			errs = append(errs, fmt.Sprintf("%d번째 줄: 필드가 %d개입니다 (1개=hostname, 12/13개=설치양식만 가능): %s", i+1, len(f), ln))
			continue
		}
		key := strings.ToLower(t.Host)
		if seen[key] {
			continue
		}
		seen[key] = true
		if t.Fields == nil {
			ip := firstIPv4(t.Host)
			if ip == "" {
				t.DNSFail = true
			}
			t.Fields = []string{"tmp", "tmp", "tmp", t.Host, ip, macFromLine(t.Line), "eth0", "sda", "sda5", "100", "8.10", "uefi"}
		}
		list = append(list, t)
	}
	return list, errs
}

// defaultErrorList 는 -er 를 주지 않았을 때 현재 경로에서 찾는 장애 목록 파일 이름.
const defaultErrorList = "error_list.txt"

// parseErrorList 는 "hostname 장애사유" 줄들을 읽어 호스트(소문자) → 비고 맵을 만든다 (setup_check -er 와 같은 규칙).
//   - 첫 공백(스페이스/탭) 앞이 호스트, 뒤 전체가 사유. 빈 줄·# 주석·사유 없는 줄은 건너뜀
//   - 같은 호스트가 여러 줄이면 ", " 로 이어 붙임
//   - 사유 안의 '|' 는 '/' 로 바꾼다. BOM·CR 도 뗀다.
func parseErrorList(text string) map[string]string {
	m := map[string]string{}
	text = strings.TrimPrefix(text, "\xef\xbb\xbf")
	for _, ln := range strings.Split(text, "\n") {
		ln = strings.TrimSpace(ln)
		if ln == "" || strings.HasPrefix(ln, "#") {
			continue
		}
		i := strings.IndexByte(ln, '\t') // setup_check 와 같은 규칙: 탭으로만 구분(사유에 공백 허용)
		if i < 0 {
			continue
		}
		host := strings.ToLower(strings.TrimSpace(ln[:i]))
		why := strings.ReplaceAll(strings.TrimSpace(ln[i+1:]), "|", "/")
		if why == "" {
			continue
		}
		if m[host] != "" {
			m[host] += ", " + why
		} else {
			m[host] = why
		}
	}
	return m
}

// parseSteps 는 단계 문자열(a=cpu, b=stress)을 순서대로 단계 이름 목록으로 바꾼다. 중복·다른 글자는 오류.
func parseSteps(s string) ([]string, error) {
	if s == "" {
		return nil, fmt.Errorf("-steps 가 비어 있습니다 (a=cpu, b=stress, 예: ab, ba)")
	}
	var out []string
	used := map[rune]bool{}
	for _, r := range s {
		if used[r] {
			return nil, fmt.Errorf("-steps 에 같은 글자가 두 번 있습니다: %q", s)
		}
		used[r] = true
		switch r {
		case 'a':
			out = append(out, stageCPU)
		case 'b':
			out = append(out, stageStress)
		default:
			return nil, fmt.Errorf("-steps 는 a(cpu)·b(stress) 만 쓸 수 있습니다: %q", s)
		}
	}
	return out, nil
}
