package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// CollectResult 는 설정값(appl) gossh 수집 결과다.
type CollectResult struct {
	ConfigValue string // 예: nas-a:/appl2/appl2
	Reached     bool   // 접속 실패가 아닌 상태. true 이고 ConfigValue 가 "" 이면 "설정값 없음"(출력 없음)
	Note        string // 특이사항 사유 ("" 이면 정상)
}

// InfraResult 는 인프라망 gossh 조사 결과다.
type InfraResult struct {
	Value string
	Note  string
}

// runGossh 는 hostnames 로 임시 hostfile 을 만들고 gossh 를 1회 실행한 뒤
// pdsh 형식(`hostname: 결과`) 출력을 hostname -> 라인들 로 파싱해 돌려준다.
// withConc 가 true 면 `-c <concurrency>` 를 붙인다(설정값 조사에만).
func runGossh(cfg *Config, hostnames []string, script string, withConc bool) (lines map[string][]string, globalErr bool) {
	lines = map[string][]string{}

	hostfile, err := os.CreateTemp("", "survey-hosts-*.txt")
	if err != nil {
		return lines, true
	}
	defer os.Remove(hostfile.Name())
	for _, h := range hostnames {
		fmt.Fprintln(hostfile, h)
	}
	hostfile.Close()

	var args []string
	if withConc && cfg.GosshConc > 0 {
		args = append(args, "-c", strconv.Itoa(cfg.GosshConc))
	}
	if strings.TrimSpace(cfg.GosshArgs) != "" {
		args = append(args, strings.Fields(cfg.GosshArgs)...)
	}
	args = append(args, "-w", hostfile.Name(), "-script", script)

	cmd := exec.Command(cfg.GosshBin, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	runErr := cmd.Run()

	for _, raw := range append(splitLines(stdout.String()), splitLines(stderr.String())...) {
		host, rest, ok := parsePdshLine(raw)
		if !ok {
			continue
		}
		lines[host] = append(lines[host], rest)
	}
	return lines, runErr != nil && stdout.Len() == 0
}

// Collect 는 `gossh -c <concurrency> -w <hostfile> -script "<config_value>"` 를 1회 실행하고
// 각 호스트의 auto.appl 매칭 행에서 설정값을 뽑는다.
func Collect(cfg *Config, hostnames []string) map[string]CollectResult {
	res := make(map[string]CollectResult, len(hostnames))
	lines, globalErr := runGossh(cfg, hostnames, cfg.ConfigValue, true)

	for _, h := range hostnames {
		hl := lines[h]
		// 접속 실패 흔적(ssh 에러 등)이 있으면 접속불가/타임아웃.
		if note := detectError(hl); note != "" {
			res[h] = CollectResult{Note: note}
			continue
		}
		// gossh 자체가 통째로 실패했으면 실행 실패.
		if len(hl) == 0 && globalErr {
			res[h] = CollectResult{Note: "gossh 실행 실패"}
			continue
		}
		// 그 외 — 출력이 아예 없거나 auto.appl 매칭 행이 없으면 설정값 "없음".
		// (사용자 정의: "설정값 없음" = 해당 호스트 출력이 없는 상태)
		res[h] = CollectResult{ConfigValue: extractConfigValue(hl), Reached: true}
	}
	return res
}

// CollectInfra 는 `gossh -w <hostfile> -script "bash <infra_net>"` 를 1회 실행하고
// 각 호스트 출력(`hostname: 출력값`)의 출력값에 infra_regex 를 적용한다.
// infra_net 이 비어 있으면 조사하지 않는다(인프라망 열 공란).
func CollectInfra(cfg *Config, hostnames []string) map[string]InfraResult {
	res := make(map[string]InfraResult, len(hostnames))
	if strings.TrimSpace(cfg.InfraNet) == "" {
		return res
	}
	lines, _ := runGossh(cfg, hostnames, "bash "+cfg.InfraNet, false)

	for _, h := range hostnames {
		hl := lines[h]
		if len(hl) == 0 {
			continue // 값 없음. 접속불가 사유는 설정값 조사에서 특이사항으로 남음
		}
		joined := strings.ToLower(strings.Join(hl, "\n"))
		if strings.Contains(joined, "no such file") ||
			strings.Contains(joined, "command not found") ||
			strings.Contains(joined, "not found") {
			res[h] = InfraResult{Note: "인프라 스크립트 없음"}
			continue
		}
		res[h] = InfraResult{Value: applyInfraRegex(cfg.InfraRe, firstNonEmptyLine(strings.Join(hl, "\n")))}
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

// detectError 는 해당 host 의 출력 라인에서 "접속" 실패 흔적만 찾는다.
// (원격 명령이 매칭 없이 종료한 경우는 실패가 아니라 "설정값 없음" 으로 취급하므로 여기서 잡지 않는다)
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

// extractConfigValue 는 auto.appl 매칭 행(`/` 로 시작)에서 마운트 대상 토큰(마지막 필드)을 뽑는다.
// `/` 로 시작하지 않는 줄(gossh/grep 에러 등)은 무시한다. 매칭 행이 없으면 "".
func extractConfigValue(lines []string) string {
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l == "" || strings.HasPrefix(l, "#") || !strings.HasPrefix(l, "/") {
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
