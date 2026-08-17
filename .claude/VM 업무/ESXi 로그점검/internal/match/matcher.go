// matcher.go: 파싱된 로그 라인에 패턴 레지스트리를 적용해 Finding을 생성한다.
//
// M1 범위: 단일 라인 매칭 + DYNAMIC(sense_key_map) 심각도 해석 + noise_filters 억제.
// M3 범위 밖(TODO): correlation_chains(다중 이벤트 상관분석)은 여러 소스를 넘나들어야 해서
// 이 파일이 아니라 internal/correlate 패키지에서 전체 findings를 모은 뒤 처리한다.
//
// [추가] requires_prev_line_suffix(패턴별 옵션): "같은 호스트의 직전 줄"이 지정된
// 접미사로 끝나는지 확인하는 최소한의 1줄 윈도우 상관관계. 만족하면 확정(confirmed),
// 못 만족하면 Finding.Suspected=true(의심)로 표시한다. 이 필드를 안 쓰는 패턴은
// 기존과 동일하게 항상 확정 매칭이라 동작에 영향이 없다.
//
// [추가, M3] aggregate(패턴별 옵션): 같은 호스트에서 이 패턴이 window 이내에 count회
// 이상 발생하면 전부 escalate_to로 격상한다 (단일 소스 내에서 판단 가능하므로 여기서 처리).
// 이미 NOISE로 억제된 항목은 격상 대상에서 제외한다 — "잦은 노이즈"는 문제 신호가 아님.
package match

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"esxi-log-check/internal/gossh"
	"esxi-log-check/internal/registry"
)

// 심각도 정렬 우선순위 (낮을수록 심각)
var severityRank = map[string]int{
	"CRITICAL": 0,
	"HIGH":     1,
	"MEDIUM":   2,
	"LOW":      3,
	"CLEAR":    4,
	"NOISE":    5,
}

func SeverityRank(s string) int {
	if r, ok := severityRank[s]; ok {
		return r
	}
	return 99 // 알 수 없는 심각도 값 -> 맨 뒤로, 리포트에서 눈에 띄게
}

// Finding은 패턴 하나가 로그 한 줄에 매칭된 결과.
type Finding struct {
	PatternID     string
	Category      string
	Severity      string
	Host          string
	Source        string
	Line          string
	LineNo        int
	Timestamp     time.Time `json:",omitempty"` // 로그 본문에서 파싱한 타임스탬프. 못 찾으면 zero value.
	Extracted     map[string]string
	Recommendation string
	Ref           string
	NoiseReason   string // 비어있지 않으면 noise_filters에 의해 NOISE로 강등됨
	DynamicNote   string // DYNAMIC 심각도 해석 근거/경고
	Suspected     bool   // true면 requires_prev_line_suffix 조건 불충족 — 확정 아닌 의심 항목
	AggregateNote string // 비어있지 않으면 aggregate 조건 충족으로 심각도가 격상됐다는 근거
}

// Match는 하나의 로그 소스(sourceName)에 대해 파싱된 라인들을 패턴과 매칭한다.
func Match(lines []gossh.ParsedLine, sourceName string, c *registry.Compiled) []Finding {
	patterns := c.BySource[sourceName]
	if len(patterns) == 0 {
		return nil
	}

	var findings []Finding
	// 호스트별 "직전 줄" 1줄 윈도우. requires_prev_line_suffix가 있는 패턴만 참조한다.
	lastLineByHost := map[string]gossh.ParsedLine{}

	for _, pl := range lines {
		noiseReason := matchNoise(pl.Raw, c.NoiseFilters)

		for _, cp := range patterns {
			m := cp.Regex.FindStringSubmatch(pl.Text)
			if m == nil {
				continue
			}

			extracted := extractGroups(cp, pl.Text, m)

			severity := cp.Def.Severity
			dynNote := ""
			if severity == "DYNAMIC" {
				severity, dynNote = resolveDynamicSeverity(extracted, c)
			}

			if cp.Def.MatchCondition != "" && !evalMatchCondition(cp.Def.MatchCondition, extracted) {
				continue
			}

			if cp.Def.EscalateWhen != nil {
				if to, ok := evalEscalateWhen(cp.Def.EscalateWhen, extracted); ok {
					severity = to
				}
			}

			suspected := false
			if cp.Def.RequiresPrevLineSuffix != "" {
				prev, ok := lastLineByHost[pl.Host]
				if !ok || !strings.HasSuffix(strings.TrimRight(prev.Text, " \t"), cp.Def.RequiresPrevLineSuffix) {
					suspected = true
				}
			}

			if noiseReason != "" {
				severity = "NOISE"
			}

			findings = append(findings, Finding{
				PatternID:   cp.Def.ID,
				Category:    cp.Def.Category,
				Severity:    severity,
				Host:        pl.Host,
				Source:      sourceName,
				Line:        pl.Raw,
				LineNo:      pl.LineNo,
				Timestamp:   pl.Timestamp,
				Extracted:   extracted,
				Recommendation: cp.Def.Recommendation,
				Ref:         cp.Def.Ref,
				NoiseReason: noiseReason,
				DynamicNote: dynNote,
				Suspected:   suspected,
			})
		}

		lastLineByHost[pl.Host] = pl
	}

	applyAggregate(findings, patterns)
	return findings
}

// applyAggregate는 aggregate가 정의된 패턴에 대해, 같은 호스트에서 window 이내에
// count회 이상 발생했는지 슬라이딩 윈도우로 확인하고, 만족하면 그 (호스트,패턴)
// 그룹의 findings 전체를 escalate_to로 격상한다.
//
// [설계 결정] "윈도우를 만족시킨 특정 N개"만 골라 격상하지 않고, 조건이 한 번이라도
// 만족되면 그 호스트의 해당 패턴 findings 전부를 격상한다 — "이 패턴이 이 호스트에서
// 반복되고 있다"는 신호 자체가 중요하지, 정확히 몇 번째 발생부터인지는 운영상 의미가
// 크지 않다고 판단했다. 타임스탬프가 없는 라인(zero value)은 윈도우 판단에서 제외한다.
// NOISE로 이미 억제된 finding은 격상 대상에서 뺀다.
func applyAggregate(findings []Finding, patterns []registry.CompiledPattern) {
	aggByID := map[string]*registry.AggregateDef{}
	for _, cp := range patterns {
		if cp.Def.Aggregate != nil {
			aggByID[cp.Def.ID] = cp.Def.Aggregate
		}
	}
	if len(aggByID) == 0 {
		return
	}

	// (host, patternID) -> 그 그룹에 속한 finding 인덱스 목록
	type groupKey struct{ host, patternID string }
	groups := map[groupKey][]int{}
	for i, f := range findings {
		if _, ok := aggByID[f.PatternID]; !ok {
			continue
		}
		if f.NoiseReason != "" {
			continue
		}
		groups[groupKey{f.Host, f.PatternID}] = append(groups[groupKey{f.Host, f.PatternID}], i)
	}

	for key, idxs := range groups {
		agg := aggByID[key.patternID]
		window, err := time.ParseDuration(agg.Window)
		if err != nil || agg.Count <= 0 {
			continue
		}

		// 타임스탬프 있는 것만 시간순 정렬
		var withTs []int
		for _, i := range idxs {
			if !findings[i].Timestamp.IsZero() {
				withTs = append(withTs, i)
			}
		}
		sort.Slice(withTs, func(a, b int) bool {
			return findings[withTs[a]].Timestamp.Before(findings[withTs[b]].Timestamp)
		})

		triggered := false
		lo := 0
		for hi := 0; hi < len(withTs); hi++ {
			for findings[withTs[hi]].Timestamp.Sub(findings[withTs[lo]].Timestamp) > window {
				lo++
			}
			if hi-lo+1 >= agg.Count {
				triggered = true
				break
			}
		}

		if !triggered {
			continue
		}

		note := fmt.Sprintf("aggregate 조건 충족(%s 이내 %d회 이상) → %s로 격상", agg.Window, agg.Count, agg.EscalateTo)
		if agg.Label != "" {
			note = fmt.Sprintf("[%s] %s", agg.Label, note)
		}
		for _, i := range idxs {
			findings[i].Severity = agg.EscalateTo
			findings[i].AggregateNote = note
		}
	}
}

func matchNoise(raw string, filters []registry.CompiledNoiseFilter) string {
	for _, nf := range filters {
		if nf.Regex.MatchString(raw) {
			return nf.Reason
		}
	}
	return ""
}

// extractGroups는 메인 정규식의 named group + extract: 서브 정규식 결과를 합친다.
func extractGroups(cp registry.CompiledPattern, text string, mainMatch []string) map[string]string {
	out := map[string]string{}
	for i, name := range cp.Regex.SubexpNames() {
		if name != "" && i < len(mainMatch) {
			out[name] = mainMatch[i]
		}
	}
	for key, exRe := range cp.ExtractRegex {
		if v := exRe.FindString(text); v != "" {
			out[key] = v
		}
	}
	return out
}

// resolveDynamicSeverity는 SCSI_CMD_FAILED 류(sense key 기반) 패턴의 실제 심각도를 결정한다.
// 캡처된 "sk"(sense key) 그룹을 sense_key_map과 대조한다.
func resolveDynamicSeverity(extracted map[string]string, c *registry.Compiled) (string, string) {
	skRaw, ok := extracted["sk"]
	if !ok || skRaw == "" {
		return "MEDIUM", "DYNAMIC 심각도 해석 실패: sk 그룹 미검출 — 수동 확인 필요"
	}
	n, err := strconv.ParseInt(skRaw, 16, 64)
	if err != nil {
		return "MEDIUM", fmt.Sprintf("sense key 파싱 실패(%q) — 수동 확인 필요", skRaw)
	}
	key := fmt.Sprintf("0x%x", n)
	entry, ok := c.SenseKeyMap[key]
	if !ok {
		return "MEDIUM", fmt.Sprintf("알 수 없는 sense key %s — sense_key_map 보강 필요", key)
	}
	note := entry.Name
	if entry.Hint != "" {
		note = note + ": " + entry.Hint
	}
	return entry.Severity, note
}

// evalMatchCondition은 "field == literal" 형태의 단순 조건만 지원한다.
// (YAML 상 match_condition 사용처가 NET_ZERO_UPLINK 하나뿐 — 필요시 확장)
func evalMatchCondition(cond string, extracted map[string]string) bool {
	parts := strings.SplitN(cond, "==", 2)
	if len(parts) != 2 {
		return true // 파싱 불가한 조건식은 통과시키되, 운영 전 반드시 확인 필요
	}
	field := strings.TrimSpace(parts[0])
	want := strings.TrimSpace(parts[1])
	got, ok := extracted[field]
	if !ok {
		return false
	}
	return strings.TrimSpace(got) == want
}

// evalEscalateWhen은 escalate_when 맵의 "to" 이외 키가 모두 캡처 그룹 값과 일치할 때만 격상한다.
// 참조 필드가 같은 패턴의 캡처 그룹에 없는 경우(예: NET_UPLINK_TRANSITION_DOWN의 uplinks_up)는
// 이 패턴 단독으로 판단 불가하므로 격상하지 않는다 — 해당 케이스는 M3 상관관계 분석으로 이관.
func evalEscalateWhen(m map[string]interface{}, extracted map[string]string) (string, bool) {
	toRaw, hasTo := m["to"]
	if !hasTo {
		return "", false
	}
	to, _ := toRaw.(string)
	for k, v := range m {
		if k == "to" {
			continue
		}
		got, ok := extracted[k]
		if !ok {
			return "", false
		}
		want := fmt.Sprintf("%v", v)
		if got != want {
			return "", false
		}
	}
	return to, true
}
