// report.go: Finding/FailedHost를 사람이 읽을 텍스트 또는 JSON(Splunk 연동용)으로 출력한다.
package report

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"time"

	"esxi-log-check/internal/gossh"
	"esxi-log-check/internal/match"
)

type Result struct {
	GeneratedAt  string             `json:"generated_at"`
	Findings     []match.Finding    `json:"findings"`
	FailedHosts  []gossh.FailedHost `json:"failed_hosts"`
	SilentHosts  []string           `json:"silent_hosts"` // hostlist엔 있으나 findings/failed 어디에도 안 나온 호스트
	HostSummary  map[string]Summary `json:"host_summary"`
	Total        Summary            `json:"total"`          // 전체 호스트 합산 (M2: 대규모 환경 한눈에 보기용)
	ProblemHosts int                `json:"problem_hosts"`  // CRITICAL/HIGH/MEDIUM/LOW/SUSPECTED 중 하나라도 있는 호스트 수
	CleanHosts   int                `json:"clean_hosts"`    // findings는 있었지만(NOISE/CLEAR 뿐) 문제로 볼 건 없는 호스트 수
	TargetHostCount int             `json:"target_host_count"` // 이번 점검 대상 총 호스트 수 (findings+실패+무응답 합)
	FailedHostCount int             `json:"failed_host_count"` // 수집 실패한 고유 호스트 수 (FailedHosts는 소스별로 중복 등장 가능)
}

type Summary struct {
	Critical  int `json:"critical"`
	High      int `json:"high"`
	Medium    int `json:"medium"`
	Low       int `json:"low"`
	Clear     int `json:"clear"`
	Noise     int `json:"noise"`
	Suspected int `json:"suspected"` // requires_prev_line_suffix 조건 불충족 — 확정 카운트에는 안 들어감
}

// Build은 findings/failed/전체 호스트목록으로부터 Result를 구성한다.
// allHosts가 비어있으면(즉 -hostlist 미지정) SilentHosts 계산은 생략한다.
func Build(findings []match.Finding, failed []gossh.FailedHost, allHosts []string) Result {
	seen := map[string]bool{}
	summary := map[string]Summary{}

	bump := func(host, sev string) {
		s := summary[host]
		switch sev {
		case "CRITICAL":
			s.Critical++
		case "HIGH":
			s.High++
		case "MEDIUM":
			s.Medium++
		case "LOW":
			s.Low++
		case "CLEAR":
			s.Clear++
		case "NOISE":
			s.Noise++
		}
		summary[host] = s
	}

	for _, f := range findings {
		seen[f.Host] = true
		if f.Suspected {
			s := summary[f.Host]
			s.Suspected++
			summary[f.Host] = s
			continue
		}
		bump(f.Host, f.Severity)
	}
	failedHostSet := map[string]bool{}
	for _, fh := range failed {
		seen[fh.Host] = true
		failedHostSet[fh.Host] = true
	}

	var silent []string
	if len(allHosts) > 0 {
		for _, h := range allHosts {
			if !seen[h] {
				silent = append(silent, h)
			}
		}
		sort.Strings(silent)
	}

	// 대상 호스트 총수: -hostlist가 있으면 그 목록 길이를 그대로 쓰고,
	// 없으면 관측된 호스트(findings + 수집실패 + 무응답)를 합쳐 근사한다.
	targetHostCount := len(allHosts)
	if targetHostCount == 0 {
		all := map[string]bool{}
		for h := range summary {
			all[h] = true
		}
		for h := range failedHostSet {
			all[h] = true
		}
		for _, h := range silent {
			all[h] = true
		}
		targetHostCount = len(all)
	}

	// M2: 전체 집계 + 문제호스트/정상호스트 카운트 (수십~수백 대 규모에서 한눈에 보기용)
	var total Summary
	problemHosts, cleanHosts := 0, 0
	for _, s := range summary {
		total.Critical += s.Critical
		total.High += s.High
		total.Medium += s.Medium
		total.Low += s.Low
		total.Clear += s.Clear
		total.Noise += s.Noise
		total.Suspected += s.Suspected

		if hostSeverityScore(s) > 0 {
			problemHosts++
		} else {
			cleanHosts++
		}
	}

	return Result{
		GeneratedAt:     time.Now().Format(time.RFC3339),
		Findings:        findings,
		FailedHosts:     failed,
		SilentHosts:     silent,
		HostSummary:     summary,
		Total:           total,
		ProblemHosts:    problemHosts,
		CleanHosts:      cleanHosts,
		TargetHostCount: targetHostCount,
		FailedHostCount: len(failedHostSet),
	}
}

// hostSeverityScore는 호스트 정렬/필터링 기준 점수.
// CRITICAL/HIGH/MEDIUM/LOW/SUSPECTED만 "문제"로 취급 (CLEAR/NOISE는 정상 범주).
// 값이 클수록 더 심각한 호스트 — [1] 정렬과 -onlyProblems 필터링에 공통으로 쓴다.
func hostSeverityScore(s Summary) int {
	return s.Critical*100000 + s.High*10000 + s.Medium*1000 + s.Low*100 + s.Suspected*10
}

// TextOptions는 텍스트 리포트 출력 옵션 (M2: 대규모 호스트 환경 대응).
type TextOptions struct {
	// OnlyProblems가 true면 [1] 호스트별 요약에서 완전히 정상(CRITICAL/HIGH/MEDIUM/LOW/
	// SUSPECTED 전부 0)인 호스트는 개별 나열 대신 카운트만 표시한다.
	// 기본값(false)은 기존과 동일하게 전체 호스트를 나열한다 — 하위호환.
	OnlyProblems bool
}

// WriteText는 사람이 읽기 위한 리포트를 출력한다.
// 구성: [0] 전체 집계 -> [1] 호스트별 요약(심각도 내림차순) -> [2] CRITICAL/HIGH 하이라이트
//      -> [2-1] 의심 -> [3] 수집 실패/무응답 호스트 -> [4] 전체 상세
func WriteText(w io.Writer, r Result, opts TextOptions) {
	fmt.Fprintf(w, "=== ESXi 크리티컬 로그 점검 리포트 (%s) ===\n\n", r.GeneratedAt)

	fmt.Fprintln(w, "[0] 전체 집계")
	fmt.Fprintf(w, "  대상 호스트 %d대 (findings 있음:%d / 수집실패:%d / 무응답:%d) — 문제호스트:%d 정상호스트:%d\n",
		r.TargetHostCount, len(r.HostSummary), r.FailedHostCount, len(r.SilentHosts), r.ProblemHosts, r.CleanHosts)
	fmt.Fprintf(w, "  CRITICAL:%-4d HIGH:%-4d MEDIUM:%-4d LOW:%-4d CLEAR:%-4d NOISE:%-4d SUSPECTED:%-4d\n",
		r.Total.Critical, r.Total.High, r.Total.Medium, r.Total.Low, r.Total.Clear, r.Total.Noise, r.Total.Suspected)
	fmt.Fprintln(w)

	fmt.Fprintln(w, "[1] 호스트별 요약 (심각도 높은 순)")
	hosts := make([]string, 0, len(r.HostSummary))
	for h := range r.HostSummary {
		hosts = append(hosts, h)
	}
	sort.Slice(hosts, func(i, j int) bool {
		si, sj := r.HostSummary[hosts[i]], r.HostSummary[hosts[j]]
		scoreI, scoreJ := hostSeverityScore(si), hostSeverityScore(sj)
		if scoreI != scoreJ {
			return scoreI > scoreJ // 심각도 높은 호스트가 먼저
		}
		return hosts[i] < hosts[j]
	})
	if len(hosts) == 0 {
		fmt.Fprintln(w, "  (findings 없음)")
	}
	hiddenClean := 0
	for _, h := range hosts {
		s := r.HostSummary[h]
		if opts.OnlyProblems && hostSeverityScore(s) == 0 {
			hiddenClean++
			continue
		}
		fmt.Fprintf(w, "  %-20s CRITICAL:%-3d HIGH:%-3d MEDIUM:%-3d LOW:%-3d CLEAR:%-3d NOISE:%-3d SUSPECTED:%-3d\n",
			h, s.Critical, s.High, s.Medium, s.Low, s.Clear, s.Noise, s.Suspected)
	}
	if hiddenClean > 0 {
		fmt.Fprintf(w, "  (정상 호스트 %d대는 -onlyProblems 옵션으로 생략됨)\n", hiddenClean)
	}
	fmt.Fprintln(w)

	fmt.Fprintln(w, "[2] CRITICAL / HIGH 하이라이트")
	highlighted := filterAndSort(r.Findings, "CRITICAL", "HIGH")
	if len(highlighted) == 0 {
		fmt.Fprintln(w, "  없음")
	}
	for _, f := range highlighted {
		printFinding(w, f)
	}
	fmt.Fprintln(w)

	suspected := suspectedOnly(r.Findings)
	if len(suspected) > 0 {
		fmt.Fprintln(w, "[2-1] 의심(SUSPECTED) 항목 — 패턴 자체는 매칭됐으나 직전 줄 상관관계 조건(requires_prev_line_suffix) 불충족, 확정 아님")
		for _, f := range suspected {
			printFinding(w, f)
		}
		fmt.Fprintln(w)
	}

	fmt.Fprintln(w, "[3] 수집 실패 호스트 (SSH/명령 실패 — 아래 호스트는 '크리티컬 0건'이 아니라 '미확인' 상태)")
	if len(r.FailedHosts) == 0 {
		fmt.Fprintln(w, "  없음")
	}
	for _, fh := range r.FailedHosts {
		fmt.Fprintf(w, "  %-20s reason=%s\n    evidence: %s\n", fh.Host, fh.Reason, fh.Evidence)
	}
	fmt.Fprintln(w)

	if len(r.SilentHosts) > 0 {
		fmt.Fprintln(w, "[3-1] 무응답/미수집 호스트 (hostlist엔 있으나 findings/실패목록 어디에도 없음 — 원인 확인 필요)")
		for _, h := range r.SilentHosts {
			fmt.Fprintf(w, "  %s\n", h)
		}
		fmt.Fprintln(w)
	}

	fmt.Fprintln(w, "[4] 전체 상세 (MEDIUM 이하 포함)")
	all := filterAndSort(r.Findings, "CRITICAL", "HIGH", "MEDIUM", "LOW", "CLEAR", "NOISE")
	for _, f := range all {
		printFinding(w, f)
	}
}

func printFinding(w io.Writer, f match.Finding) {
	fmt.Fprintf(w, "  [%s] %s / %s @ %s (source=%s line=%d)\n", f.Severity, f.PatternID, f.Category, f.Host, f.Source, f.LineNo)
	fmt.Fprintf(w, "    %s\n", f.Line)
	if f.Recommendation != "" {
		fmt.Fprintf(w, "    hint: %s\n", f.Recommendation)
	}
	if f.DynamicNote != "" {
		fmt.Fprintf(w, "    dynamic: %s\n", f.DynamicNote)
	}
	if f.AggregateNote != "" {
		fmt.Fprintf(w, "    aggregate: %s\n", f.AggregateNote)
	}
	if f.NoiseReason != "" {
		fmt.Fprintf(w, "    noise 억제 사유: %s\n", f.NoiseReason)
	}
	if len(f.Extracted) > 0 {
		fmt.Fprintf(w, "    extracted: %v\n", f.Extracted)
	}
}

// filterAndSort는 확정(Suspected=false) findings만 대상으로 한다.
// 의심 항목은 별도 섹션([2-1])에서만 다룬다 — 여기 섞이면 확정/의심 구분이 무너짐.
func filterAndSort(findings []match.Finding, severities ...string) []match.Finding {
	want := map[string]bool{}
	for _, s := range severities {
		want[s] = true
	}
	var out []match.Finding
	for _, f := range findings {
		if f.Suspected {
			continue
		}
		if want[f.Severity] {
			out = append(out, f)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if match.SeverityRank(out[i].Severity) != match.SeverityRank(out[j].Severity) {
			return match.SeverityRank(out[i].Severity) < match.SeverityRank(out[j].Severity)
		}
		if out[i].Host != out[j].Host {
			return out[i].Host < out[j].Host
		}
		return out[i].LineNo < out[j].LineNo
	})
	return out
}

func suspectedOnly(findings []match.Finding) []match.Finding {
	var out []match.Finding
	for _, f := range findings {
		if f.Suspected {
			out = append(out, f)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Host != out[j].Host {
			return out[i].Host < out[j].Host
		}
		return out[i].LineNo < out[j].LineNo
	})
	return out
}

// WriteJSON은 Splunk 등 후속 파이프라인 연동을 위한 JSON 출력.
func WriteJSON(w io.Writer, r Result) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(r)
}
