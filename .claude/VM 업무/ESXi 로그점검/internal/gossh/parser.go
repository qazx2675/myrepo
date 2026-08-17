// parser.go: "hostname : 로그원문" 형태의 gossh -w 출력을 호스트 단위로 분리한다.
package gossh

import (
	"fmt"
	"os"
	"os/exec"
	"bufio"
	"io"
	"regexp"
	"strconv"
	"time"

	"esxi-log-check/internal/registry"
)

// timestampRe는 ESXi 로그 본문 맨 앞의 ISO8601 타임스탬프를 뽑는다.
// 예: "2026-08-08T21:54:19.462Z In(14) vobd[524534]: ..."
// M3(aggregate/correlation_chains/무증상 리부팅)는 전부 이 타임스탬프에 의존한다.
// 못 찾으면 ParsedLine.Timestamp는 zero value로 남고, 시간 기반 로직에서는
// (타임스탬프 없음 -> 판단 불가로 스킵) 처리한다 — 단일 라인 매칭에는 영향 없음.
var timestampRe = regexp.MustCompile(`^(\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?Z)`)

func parseTimestamp(text string) time.Time {
	m := timestampRe.FindStringSubmatch(text)
	if m == nil {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339Nano, m[1])
	if err != nil {
		return time.Time{}
	}
	return t
}

// --- IPMI SEL 전용 타임스탬프 파서 ---
//
// [주의] 이 환경(록키리눅스/테스트용 ESXi)엔 실제 BMC 하드웨어가 없어서
// `localcli hardware ipmi sel list` 결과가 항상 0건이었고, 실측 원본 포맷을
// 직접 캡처하지 못했다. 아래는 VMware esxcli 계열 명령의 통상적인 SEL 표기
// (날짜 MM/DD/YYYY, 시각 HH:MM:SS, 다른 필드와 구분자로 분리) 기준으로 구현한
// 것이라, 실제 BMC가 있는 호스트로 검증되기 전까지는 "최선 추정" 상태다.
//
// 특정 컬럼 위치(예: "2번째, 3번째 필드")에 고정 의존하지 않고, 줄 안에서
// MM/DD/YYYY 패턴과 HH:MM:SS 패턴을 각각 찾아 조합하는 방식을 택했다 —
// 파이프(|) 구분이든 공백 정렬 테이블이든 구분자와 무관하게 동작하도록 하기 위함.
var ipmiDateRe = regexp.MustCompile(`\b(\d{1,2})/(\d{1,2})/(\d{4})\b`)
var ipmiTimeRe = regexp.MustCompile(`\b(\d{1,2}):(\d{2}):(\d{2})\b`)

func parseIPMITimestamp(text string) time.Time {
	dm := ipmiDateRe.FindStringSubmatch(text)
	tm := ipmiTimeRe.FindStringSubmatch(text)
	if dm == nil || tm == nil {
		return time.Time{}
	}
	mo, errMo := strconv.Atoi(dm[1])
	day, errDay := strconv.Atoi(dm[2])
	year, errYear := strconv.Atoi(dm[3])
	hh, errHH := strconv.Atoi(tm[1])
	mm, errMM := strconv.Atoi(tm[2])
	ss, errSS := strconv.Atoi(tm[3])
	if errMo != nil || errDay != nil || errYear != nil || errHH != nil || errMM != nil || errSS != nil {
		return time.Time{}
	}
	if mo < 1 || mo > 12 || day < 1 || day > 31 || hh > 23 || mm > 59 || ss > 59 {
		return time.Time{}
	}
	// SEL 타임스탬프는 통상 BMC 로컬시각이고 타임존 표기가 없다 — UTC로 취급한다
	// (다른 소스의 상관관계 판단과 마찬가지로 "같은 기준으로 비교 가능"하면 충분).
	return time.Date(year, time.Month(mo), day, hh, mm, ss, 0, time.UTC)
}

// ipmiSourceName: 이 이름의 소스일 때만 parseIPMITimestamp를 쓴다.
// main.go의 -input ipmi_sel=... 관례와 일치시켜야 한다.
const ipmiSourceName = "ipmi_sel"

// ParsedLine은 gossh 출력 한 줄을 호스트/본문으로 분리한 결과.
type ParsedLine struct {
	Host      string    // 파싱된 호스트명. Orphan이면 "직전 호스트"로 귀속된 값.
	Text      string    // "hostname :" 접두사를 제거한 로그 본문 (패턴 매칭 대상)
	Raw       string    // 원본 줄 전체 (noise filter / 재현용)
	LineNo    int
	Orphan    bool      // prefix 파싱에 실패해 orphan_line_policy로 처리된 경우 true
	Timestamp time.Time // Text 맨 앞 ISO8601 타임스탬프. 못 찾으면 zero value.
}

// FailedHost는 SSH 접속/실행 실패로 판정된 호스트와 근거 라인.
type FailedHost struct {
	Host     string
	Reason   string // 매칭된 collection_failure_patterns 정규식 문자열
	Evidence string
}

// ParseStream은 하나의 gossh 출력 스트림(한 로그 소스 = 한 스트림)을 읽어
// 정상 파싱 라인과, 수집 실패로 판정된 호스트 목록을 분리해서 반환한다.
//
// 중요: 수집 실패 호스트는 정상 라인과 절대 섞이지 않는다.
// "크리티컬 0건"과 "SSH 실패로 0건"을 구분하기 위함.
//
// sourceName은 타임스탬프 파서 선택에만 쓰인다: sourceName이 "ipmi_sel"이면
// parseIPMITimestamp(MM/DD/YYYY + HH:MM:SS)를, 그 외에는 기존 ISO8601
// parseTimestamp를 쓴다. 다른 파싱 로직(prefix/orphan/실패판정)에는 영향 없다.
func ParseStream(r io.Reader, gs registry.CompiledGoSSH, sourceName string) (lines []ParsedLine, failed []FailedHost, err error) {
	sc := bufio.NewScanner(r)
	// PSOD 스택트레이스 등 긴 줄 대비 버퍼 확장 (기본 64KB -> 10MB)
	buf := make([]byte, 0, 64*1024)
	sc.Buffer(buf, 10*1024*1024)

	lastHost := ""
	lineNo := 0

	for sc.Scan() {
		lineNo++
		raw := sc.Text()
		if raw == "" {
			continue
		}

		host, text, matched := tryPrefix(raw, gs)

		if matched {
			lastHost = host
		} else {
			// prefix 파싱 실패 -> orphan 정책 적용
			switch gs.OrphanLinePolicy {
			case "drop":
				continue
			case "unknown":
				host = "unknown"
				text = raw
			default: // "carry"
				host = lastHost
				text = raw
			}
		}

		// 수집 실패 패턴 검사는 라인 본문(text) 기준으로 수행.
		// gossh 구현체에 따라 에러 메시지에 hostname 접두사가 없을 수 있으므로,
		// 접두사 파싱이 안 된 채로 실패 패턴이 매칭되면 host는 lastHost/unknown으로 귀속된다.
		// -> 실제 gossh 출력 샘플로 M0 검증 시 반드시 재확인할 것.
		if pat, ok := matchAny(text, gs.CollectionFailurePatterns); ok {
			failedHost := host
			if failedHost == "" {
				failedHost = "unknown"
			}
			failed = append(failed, FailedHost{Host: failedHost, Reason: pat.String(), Evidence: raw})
			continue // 실패 라인 자체는 패턴 매칭 대상에서 제외
		}

		ts := parseTimestamp(text)
		if sourceName == ipmiSourceName {
			ts = parseIPMITimestamp(text)
		}

		lines = append(lines, ParsedLine{
			Host:      host,
			Text:      text,
			Raw:       raw,
			LineNo:    lineNo,
			Orphan:    !matched,
			Timestamp: ts,
		})
	}

	if scErr := sc.Err(); scErr != nil {
		return lines, failed, scErr
	}
	return lines, failed, nil
}

// tryPrefix는 PrefixRegex -> PrefixFallbacks 순서로 "host : 본문" 분리를 시도한다.
func tryPrefix(raw string, gs registry.CompiledGoSSH) (host, text string, matched bool) {
	if gs.PrefixRegex != nil {
		if h, t, ok := applyPrefixRegex(gs.PrefixRegex, raw); ok {
			return h, t, true
		}
	}
	for _, fb := range gs.PrefixFallbacks {
		if h, t, ok := applyPrefixRegex(fb, raw); ok {
			return h, t, true
		}
	}
	return "", "", false
}

// applyPrefixRegex는 named group "host"/"line"을 갖는 정규식으로 접두사를 분리한다.
func applyPrefixRegex(re *regexp.Regexp, raw string) (host, text string, ok bool) {
	m := re.FindStringSubmatch(raw)
	if m == nil {
		return "", "", false
	}
	for i, name := range re.SubexpNames() {
		switch name {
		case "host":
			host = m[i]
		case "line":
			text = m[i]
		}
	}
	if host == "" {
		return "", "", false
	}
	return host, text, true
}

func matchAny(text string, patterns []*regexp.Regexp) (*regexp.Regexp, bool) {
	for _, p := range patterns {
		if p.MatchString(text) {
			return p, true
		}
	}
	return nil, false
}

// RunCommand is used by -test-mock to execute esxi_mock_logger remotely.
func RunCommand(gosshPath, hostFile, cmd string) {
	c := exec.Command(gosshPath, "-w", hostFile, "-script", cmd)
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	if err := c.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "[RunCommand] gossh failed: %v\n", err)
	}
}
