package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// CollectResult 는 gossh 원격 수집 결과다.
type CollectResult struct {
	ConfigValue string // 예: nas-a:/appl2/appl2
	Reached     bool   // gossh 응답을 정상 수신했는지 (true 이고 ConfigValue 가 "" 이면 "설정값 없음")
	Note        string // 특이사항 사유 ("" 이면 정상)
}

// Collect 는 표1 hostname 목록으로 임시 hostfile 을 만들고
// `gossh -c <concurrency> -w <hostfile> -script "<config_value>"` 를 1회 실행한 뒤
// pdsh 형식(`hostname: 결과`) 출력을 hostname 기준으로 파싱한다.
// (인프라망 조사는 gossh 가 아니라 rule.go 의 InfraNet 이 호스트마다 따로 실행한다)
func Collect(cfg *Config, hostnames []string) map[string]CollectResult {
	res := make(map[string]CollectResult, len(hostnames))
	for _, h := range hostnames {
		res[h] = CollectResult{Note: "접속불가"} // 응답이 확인될 때까지 기본값
	}

	hostfile, err := os.CreateTemp("", "survey-hosts-*.txt")
	if err != nil {
		for _, h := range hostnames {
			res[h] = CollectResult{Note: "hostfile 생성 실패"}
		}
		return res
	}
	defer os.Remove(hostfile.Name())
	for _, h := range hostnames {
		fmt.Fprintln(hostfile, h)
	}
	hostfile.Close()

	var args []string
	if cfg.GosshConc > 0 {
		args = append(args, "-c", strconv.Itoa(cfg.GosshConc))
	}
	if strings.TrimSpace(cfg.GosshArgs) != "" {
		args = append(args, strings.Fields(cfg.GosshArgs)...)
	}
	args = append(args, "-w", hostfile.Name(), "-script", cfg.ConfigValue)

	cmd := exec.Command(cfg.GosshBin, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	runErr := cmd.Run()

	lines := map[string][]string{}
	for _, raw := range append(splitLines(stdout.String()), splitLines(stderr.String())...) {
		host, rest, ok := parsePdshLine(raw)
		if !ok {
			continue
		}
		lines[host] = append(lines[host], rest)
	}

	for _, h := range hostnames {
		hl := lines[h]
		if len(hl) == 0 {
			if runErr != nil && stdout.Len() == 0 {
				res[h] = CollectResult{Note: "gossh 실행 실패"}
			}
			continue // 기본값 "접속불가" 유지
		}
		if note := detectError(hl); note != "" {
			res[h] = CollectResult{Note: note}
			continue
		}
		// 응답은 받았으나 매칭 행이 없으면 ConfigValue "" + Reached true → "없음" 으로 출력
		res[h] = CollectResult{ConfigValue: extractConfigValue(hl), Reached: true}
	}
	return res
}

func splitLines(s string) []string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.TrimRight(s, "\n")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

// parsePdshLine 은 "host: content" 한 줄을 분해한다.
// host 는 첫 ':' 앞부분이며 공백을 포함하지 않는다.
func parsePdshLine(line string) (host, rest string, ok bool) {
	line = strings.TrimRight(line, "\r")
	if line == "" {
		return "", "", false
	}
	i := strings.Index(line, ":")
	if i <= 0 {
		return "", "", false
	}
	host = strings.TrimSpace(line[:i])
	if host == "" || strings.ContainsAny(host, " \t") {
		return "", "", false
	}
	rest = line[i+1:]
	rest = strings.TrimPrefix(rest, " ")
	return host, rest, true
}

// detectError 는 해당 host 의 출력 라인에서 접속/실행 실패 흔적을 찾는다.
func detectError(lines []string) string {
	joined := strings.ToLower(strings.Join(lines, "\n"))
	switch {
	case strings.Contains(joined, "timed out"), strings.Contains(joined, "timeout"):
		return "타임아웃"
	case strings.Contains(joined, "connection refused"),
		strings.Contains(joined, "connection closed"),
		strings.Contains(joined, "no route to host"),
		strings.Contains(joined, "permission denied"),
		strings.Contains(joined, "could not resolve"),
		strings.Contains(joined, "name or service not known"),
		strings.Contains(joined, "host key verification failed"),
		strings.Contains(joined, "ssh:"):
		return "접속불가"
	}
	return ""
}

// extractConfigValue 는 auto.appl 매칭 행에서 마운트 대상 토큰(마지막 필드)을 뽑는다.
// 여러 행이면 첫 유효 행을 쓴다. (계획: /appl 매칭 행은 1개로 가정)
func extractConfigValue(lines []string) string {
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l == "" || strings.HasPrefix(l, "#") {
			continue
		}
		fields := strings.Fields(l)
		if len(fields) == 0 {
			continue
		}
		last := fields[len(fields)-1]
		if strings.Contains(last, ":") {
			return last
		}
		for i := len(fields) - 1; i >= 0; i-- {
			if strings.Contains(fields[i], ":") {
				return fields[i]
			}
		}
		return last
	}
	return ""
}
