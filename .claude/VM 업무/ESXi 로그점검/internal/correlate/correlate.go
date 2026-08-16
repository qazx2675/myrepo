// correlate.go: 여러 소스/여러 이벤트에 걸친 상관관계 분석 (M3).
//
// matcher.go의 Match()는 소스 하나 안에서만 동작해서 다중 소스 상관관계를 볼 수 없다.
// 그래서 main.go가 모든 소스를 다 처리해서 allFindings를 모은 "다음"에 이 패키지를
// 별도로 호출해 추가 Finding(체인 결과)을 만들고 append한다.
//
// esxi_critical_patterns.yaml의 correlation_chains: 는 형태가 두 가지로 섞여 있다:
//   1) "sequence" 타입 — 패턴 ID 목록이 시간순으로 window 이내에 다 나오면 성립.
//      CHAIN_MEM_TO_HOSTD_HANG, CHAIN_NIC_TO_STORAGE 이 여기 해당하고, 일반화해서
//      buildSequenceChain()으로 처리한다.
//   2) "condition/then" 타입 — condition이 자연어 문장이라 일반 파서로 못 다룬다.
//      CHAIN_FABRIC_WIDE_APD, CHAIN_SILENT_REBOOT 두 개뿐이라, ID로 분기해서
//      각각 전용 함수(buildFabricWideAPD, DetectSilentReboots)로 하드코딩 처리했다.
//      YAML 스키마 자체는 안 건드렸다 — Go 코드에서 이 두 ID만 특별 취급하는 것뿐이다.
package correlate

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"esxi-log-check/internal/gossh"
	"esxi-log-check/internal/match"
	"esxi-log-check/internal/registry"
)

// BuildChainFindings는 compiled.Chains에 정의된 상관관계 체인 중, sequence 타입과
// CHAIN_FABRIC_WIDE_APD를 검사해서 성립하는 체인마다 Finding을 하나씩 만든다.
// CHAIN_SILENT_REBOOT는 vmkernel.log 원본 라인이 별도로 필요해서 DetectSilentReboots로 분리했다.
func BuildChainFindings(allFindings []match.Finding, chains []registry.CompiledChain) []match.Finding {
	var out []match.Finding
	for _, cc := range chains {
		switch {
		case len(cc.Def.Sequence) > 0:
			out = append(out, buildSequenceChain(allFindings, cc)...)
		case cc.Def.ID == "CHAIN_FABRIC_WIDE_APD":
			out = append(out, buildFabricWideAPD(allFindings, cc)...)
		case cc.Def.ID == "CHAIN_SILENT_REBOOT":
			// DetectSilentReboots에서 별도 처리 (vmkernel.log 원본 라인 필요)
		default:
			// 알려지지 않은 형태의 체인 — 조용히 무시하지 않고 최소한 흔적을 남긴다.
			out = append(out, match.Finding{
				PatternID: cc.Def.ID,
				Category:  "CORRELATION",
				Severity:  "LOW",
				Host:      "(전체)",
				Source:    "correlation",
				Line:      "이 체인은 sequence/condition 형태를 자동 인식하지 못해 미구현 상태입니다 — 수동 확인 필요",
				Hint:      cc.Def.Condition,
			})
		}
	}
	return out
}

// buildSequenceChain은 Sequence에 나열된 패턴 ID들이 같은 호스트에서 시간 순서대로
// (첫 이벤트 기준 window 이내에) 전부 발생했는지 확인한다. 성립하면 호스트당 1건만 만든다.
func buildSequenceChain(allFindings []match.Finding, cc registry.CompiledChain) []match.Finding {
	byHostPattern := map[string]map[string][]time.Time{}
	for _, f := range allFindings {
		if f.Suspected || f.Timestamp.IsZero() {
			continue
		}
		if byHostPattern[f.Host] == nil {
			byHostPattern[f.Host] = map[string][]time.Time{}
		}
		byHostPattern[f.Host][f.PatternID] = append(byHostPattern[f.Host][f.PatternID], f.Timestamp)
	}

	var out []match.Finding
	for host, byPattern := range byHostPattern {
		firstTimes := append([]time.Time(nil), byPattern[cc.Def.Sequence[0]]...)
		if len(firstTimes) == 0 {
			continue
		}
		sort.Slice(firstTimes, func(i, j int) bool { return firstTimes[i].Before(firstTimes[j]) })

		for _, t0 := range firstTimes {
			cur := t0
			deadline := t0.Add(cc.Window)
			ok := true
			for _, pid := range cc.Def.Sequence[1:] {
				next, found := earliestInRange(byPattern[pid], cur, deadline)
				if !found {
					ok = false
					break
				}
				cur = next
			}
			if ok {
				out = append(out, match.Finding{
					PatternID: cc.Def.ID,
					Category:  "CORRELATION",
					Severity:  cc.Def.Severity,
					Host:      host,
					Source:    "correlation",
					Line:      fmt.Sprintf("체인 성립: %s (window=%s, 시작=%s)", strings.Join(cc.Def.Sequence, " -> "), cc.Def.Window, t0.Format(time.RFC3339)),
					Timestamp: t0,
					Hint:      cc.Def.Verdict,
				})
				break // 호스트당 1건만 (같은 체인이 여러 번 성립해도 반복 보고 안 함)
			}
		}
	}
	return out
}

// earliestInRange는 times 중 [after, before] 구간(포함)에 있는 가장 이른 시각을 찾는다.
func earliestInRange(times []time.Time, after, before time.Time) (time.Time, bool) {
	var best time.Time
	found := false
	for _, t := range times {
		if t.Before(after) || t.After(before) {
			continue
		}
		if !found || t.Before(best) {
			best = t
			found = true
		}
	}
	return best, found
}

// buildFabricWideAPD는 CHAIN_FABRIC_WIDE_APD 전용 로직.
// condition: "SCSI_PATH_DEAD 이벤트가 서로 다른 vmhba 2개 이상에서 발생" + then: [STORAGE_APD_START]
// 를 그대로 구현: 같은 호스트에서 window 이내에 SCSI_PATH_DEAD가 서로 다른 vmhba 2개
// 이상에서 발생하고, 같은 window 안에 STORAGE_APD_START도 있으면 체인 성립.
func buildFabricWideAPD(allFindings []match.Finding, cc registry.CompiledChain) []match.Finding {
	deadByHost := map[string][]match.Finding{}
	apdByHost := map[string][]match.Finding{}
	for _, f := range allFindings {
		if f.Suspected || f.Timestamp.IsZero() {
			continue
		}
		switch f.PatternID {
		case "SCSI_PATH_DEAD":
			deadByHost[f.Host] = append(deadByHost[f.Host], f)
		case "STORAGE_APD_START":
			apdByHost[f.Host] = append(apdByHost[f.Host], f)
		}
	}

	var out []match.Finding
	for host, deads := range deadByHost {
		apds := apdByHost[host]
		if len(apds) == 0 {
			continue
		}
		sort.Slice(deads, func(i, j int) bool { return deads[i].Timestamp.Before(deads[j].Timestamp) })

		found := false
		for i := 0; i < len(deads) && !found; i++ {
			windowEnd := deads[i].Timestamp.Add(cc.Window)
			distinct := map[string]bool{}
			for j := i; j < len(deads) && !deads[j].Timestamp.After(windowEnd); j++ {
				if vmhba := vmhbaPrefix(deads[j].Extracted["path"]); vmhba != "" {
					distinct[vmhba] = true
				}
			}
			if len(distinct) < 2 {
				continue
			}
			for _, a := range apds {
				if !a.Timestamp.Before(deads[i].Timestamp) && !a.Timestamp.After(windowEnd) {
					out = append(out, match.Finding{
						PatternID: cc.Def.ID,
						Category:  "CORRELATION",
						Severity:  cc.Def.Severity,
						Host:      host,
						Source:    "correlation",
						Line:      fmt.Sprintf("체인 성립: SCSI_PATH_DEAD(vmhba %d개) -> STORAGE_APD_START (window=%s)", len(distinct), cc.Def.Window),
						Timestamp: deads[i].Timestamp,
						Hint:      cc.Def.Verdict,
					})
					found = true
					break
				}
			}
		}
	}
	return out
}

var vmhbaRe = regexp.MustCompile(`^(vmhba\d+)`)

func vmhbaPrefix(path string) string {
	m := vmhbaRe.FindStringSubmatch(path)
	if m == nil {
		return ""
	}
	return m[1]
}

// DefaultSilentRebootGap: vmkernel.log에 이 이상 공백이 있으면 무증상 리부팅 후보로 본다.
// YAML/M0 문서에 정확한 임계값이 없어서 처음엔 사용자가 예시로 든 "5분"을 기본값으로
// 채택했으나, 실제 ESXi(.59) 로그로 검증하는 과정에서 5분은 너무 민감하다는 게 실측으로
// 확인됐다: "NMP: nmp_ThrottleLogForDevice"류 스로틀링된 커널 로그가 딱 5분 간격으로
// 반복 출력되는 게 정상 동작인데, 그게 전부 "공백"으로 오인되어 300줄짜리 실측 로그에서
// 100건의 오탐이 발생했다(같은 데이터로 재현 가능). 그래서 기본값을 30분으로 올렸다.
// 이 값도 여전히 추정치이므로, 환경별로 -silentRebootGap 플래그로 조정 권장.
// CHAIN_SILENT_REBOOT의 window(10m)는 "갭 이후 얼마나까지를 부팅 시퀀스로 볼지"이지
// "갭 자체의 최소 길이"가 아니라서 별도 상수로 뒀다 — 이 둘을 혼동하지 않도록 주의.
const DefaultSilentRebootGap = 30 * time.Minute

// bootMarkerRe: 갭 직후 줄이 "재부팅 직후 로그"로 보이는지 확인하는 보조 신호.
// [주의] 실제 ESXi 재부팅 시 vmkernel.log 첫 줄 문구를 실측 샘플로 확인 못 했다
// (테스트에 쓴 .59 호스트가 캡처 구간 동안 재부팅 이력이 없었음). 아래 키워드는
// VMware 공개 자료 기준 통상적인 초기 부팅 로그 문구로 추정한 것이라, 실제 재부팅
// 로그로 반드시 재검증 필요 — 매칭 안 돼도 갭 자체만으로 SILENT_REBOOT는 발생시킨다
// (문구 매칭은 신뢰도를 보강하는 부가 신호일 뿐, 필수 조건이 아니다).
var bootMarkerRe = regexp.MustCompile(`(?i)(Starting VMware ESXi|VMware ESXi \d|vmkBoot|Located module|BootBankInitialize|Initializing shared)`)

// DetectSilentReboots는 vmkernel.log의 호스트별 타임스탬프 공백을 찾아서, gapThreshold
// 이상이면 무증상 리부팅 후보로 Finding을 만든다. allFindings에서 같은 호스트의
// IPMI_PROC_NONRECOVERABLE/IPMI_POWER_FAULT가 그 시간대에 있으면 보강 근거로 덧붙인다.
// chain이 주어지면(YAML의 CHAIN_SILENT_REBOOT) 그 Severity/Verdict를 그대로 쓴다.
func DetectSilentReboots(vmkernelLines []gossh.ParsedLine, allFindings []match.Finding, gapThreshold time.Duration, chain *registry.CompiledChain) []match.Finding {
	byHost := map[string][]gossh.ParsedLine{}
	for _, pl := range vmkernelLines {
		if pl.Timestamp.IsZero() {
			continue
		}
		byHost[pl.Host] = append(byHost[pl.Host], pl)
	}

	severity := "CRITICAL"
	verdict := "vmkernel.log에 장시간 공백 후 로그가 재개됨 — PSOD/에러 없이 하드웨어 레벨에서 즉시 halt/reset된 무증상 리부팅 의심. IPMI SEL의 Processor Non-recoverable / Power Input Lost 대조 필요."
	if chain != nil {
		if chain.Def.Severity != "" {
			severity = chain.Def.Severity
		}
		if chain.Def.Verdict != "" {
			verdict = chain.Def.Verdict
		}
	}

	var out []match.Finding
	for host, lines := range byHost {
		sort.Slice(lines, func(i, j int) bool { return lines[i].Timestamp.Before(lines[j].Timestamp) })

		for i := 1; i < len(lines); i++ {
			gap := lines[i].Timestamp.Sub(lines[i-1].Timestamp)
			if gap < gapThreshold {
				continue
			}

			bootConfirmed := bootMarkerRe.MatchString(lines[i].Text)
			dynNote := "재부팅 로그 문구 미확인(휴리스틱 실측 검증 필요) — 갭 길이만으로 판단"
			if bootConfirmed {
				dynNote = "재부팅 직후 통상 로그 문구 확인됨(휴리스틱, 실측 검증 권장)"
			}

			var selEvidence []string
			for _, f := range allFindings {
				if f.Host != host || f.Timestamp.IsZero() {
					continue
				}
				if f.PatternID != "IPMI_PROC_NONRECOVERABLE" && f.PatternID != "IPMI_POWER_FAULT" {
					continue
				}
				if f.Timestamp.Before(lines[i-1].Timestamp.Add(-gapThreshold)) || f.Timestamp.After(lines[i].Timestamp.Add(gapThreshold)) {
					continue
				}
				selEvidence = append(selEvidence, fmt.Sprintf("%s@%s", f.PatternID, f.Timestamp.Format(time.RFC3339)))
			}
			if len(selEvidence) > 0 {
				dynNote += fmt.Sprintf(" | SEL 상관 근거 발견: %s", strings.Join(selEvidence, ", "))
			}

			patternID := "CHAIN_SILENT_REBOOT"
			out = append(out, match.Finding{
				PatternID:   patternID,
				Category:    "CORRELATION",
				Severity:    severity,
				Host:        host,
				Source:      "vmkernel.log(gap)",
				Line: fmt.Sprintf("로그 공백 %s (직전: %s, 재개: %s) 직후: %s",
					gap.Round(time.Second), lines[i-1].Timestamp.Format(time.RFC3339), lines[i].Timestamp.Format(time.RFC3339), lines[i].Text),
				Timestamp:   lines[i].Timestamp,
				Hint:        verdict,
				DynamicNote: dynNote,
			})
		}
	}
	return out
}
