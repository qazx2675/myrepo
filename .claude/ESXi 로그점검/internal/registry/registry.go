// registry.go: esxi_critical_patterns.yaml 로딩 및 정규식 컴파일
package registry

import (
	"fmt"
	"os"
	"regexp"
	"time"

	"gopkg.in/yaml.v3"
)

// Raw는 YAML 파일을 그대로 언마샬한 구조체 (M0 산출물 스키마와 1:1 대응)
type Raw struct {
	Version string `yaml:"version"`
	Updated string `yaml:"updated"`
	Status  string `yaml:"status"`

	GoSSH struct {
		PrefixRegex        string   `yaml:"prefix_regex"`
		PrefixFallbacks     []string `yaml:"prefix_fallbacks"`
		OrphanLinePolicy    string   `yaml:"orphan_line_policy"`
		CollectionFailurePatterns []string `yaml:"collection_failure_patterns"`
	} `yaml:"gossh"`

	Sources []struct {
		Path     string `yaml:"path"`
		Aliases  []string `yaml:"aliases"`
		Cmd      string `yaml:"cmd"`
		Priority int    `yaml:"priority"`
		Note     string `yaml:"note"`
	} `yaml:"sources"`

	Patterns []PatternDef `yaml:"patterns"`

	SenseKeyMap   map[string]MapEntry `yaml:"sense_key_map"`
	HostStatusMap map[string]MapEntry `yaml:"host_status_map"`

	NoiseFilters []struct {
		ID     string `yaml:"id"`
		Regex  string `yaml:"regex"`
		Reason string `yaml:"reason"`
	} `yaml:"noise_filters"`

	// M3: correlation_chains 실제 파싱 시작 (M1/M2에서는 필드 자체가 없어 조용히 무시되고 있었음)
	CorrelationChains []ChainDef `yaml:"correlation_chains"`

	// verification_gaps는 여전히 파싱하지 않음 (문서용 텍스트, 로직에서 안 씀)
}

// ChainDef는 correlation_chains: 배열의 각 항목.
// YAML상 두 가지 형태가 섞여 있다:
//   - "sequence" 타입: Sequence에 나열된 패턴 ID들이 시간 순서대로 Window 이내에 전부
//     발생하면 체인 성립 (CHAIN_MEM_TO_HOSTD_HANG, CHAIN_NIC_TO_STORAGE)
//   - "condition/then" 타입: Condition이 자유 텍스트라 일반화해서 파싱할 수 없어서,
//     correlate 패키지에서 ID별로 특별 처리한다 (CHAIN_FABRIC_WIDE_APD, CHAIN_SILENT_REBOOT).
//     자세한 내용은 internal/correlate/correlate.go 상단 주석 참조.
type ChainDef struct {
	ID            string   `yaml:"id"`
	Window        string   `yaml:"window"`
	Sequence      []string `yaml:"sequence"`
	Condition     string   `yaml:"condition"`
	Then          []string `yaml:"then"`
	RequireSource string   `yaml:"require_source"`
	Verdict       string   `yaml:"verdict"`
	Severity      string   `yaml:"severity"`
	Note          string   `yaml:"note"`
	Label         string   `yaml:"label"`
}

// PatternDef는 patterns: 배열의 각 항목
type PatternDef struct {
	ID       string `yaml:"id"`
	Category string `yaml:"category"`
	Severity string `yaml:"severity"` // CRITICAL/HIGH/MEDIUM/LOW/CLEAR/DYNAMIC
	Source   string `yaml:"source"`
	Regex    string `yaml:"regex"`
	Hint     string `yaml:"hint"`
	Ref      string `yaml:"ref"`

	Extract        map[string]string     `yaml:"extract"`
	MatchCondition string                 `yaml:"match_condition"`
	EscalateWhen   map[string]interface{} `yaml:"escalate_when"`

	// RequiresPrevLineSuffix(선택): 값이 설정된 패턴만, "같은 호스트의 직전 줄"이
	// 이 접미사로 끝나야 확정(confirmed) 매칭으로 본다. 직전 줄 조건을 못 채우면
	// 매칭 자체는 유지하되 match.Finding.Suspected=true(의심)로 표시한다.
	// 빈 문자열(기본값)이면 기존과 동일하게 항상 확정 매칭 — 다른 패턴에는 영향 없음.
	RequiresPrevLineSuffix string `yaml:"requires_prev_line_suffix"`

	// Aggregate(M3에서 실제 적용 시작): 같은 호스트에서 이 패턴이 Window 이내에
	// Count회 이상 발생하면 EscalateTo로 심각도 격상.
	Aggregate *AggregateDef `yaml:"aggregate"`
}

// AggregateDef는 patterns[].aggregate: 필드.
type AggregateDef struct {
	Window     string `yaml:"window"`
	Count      int    `yaml:"count"`
	EscalateTo string `yaml:"escalate_to"`
	Label      string `yaml:"label"`
}

// MapEntry는 sense_key_map / host_status_map의 값
type MapEntry struct {
	Name     string `yaml:"name"`
	Severity string `yaml:"severity"`
	Hint     string `yaml:"hint"`
	Verify   bool   `yaml:"verify"`
}

// ---- 컴파일된 형태 (매칭 시 사용) ----

type CompiledPattern struct {
	Def          PatternDef
	Regex        *regexp.Regexp
	ExtractRegex map[string]*regexp.Regexp
}

type Compiled struct {
	// Source 이름(vobd.log, vmkernel.log, ipmi_sel ...) -> 해당 소스에 적용할 패턴 목록
	BySource map[string][]CompiledPattern

	SenseKeyMap   map[string]MapEntry
	HostStatusMap map[string]MapEntry

	NoiseFilters []CompiledNoiseFilter

	Chains []CompiledChain

	GoSSH CompiledGoSSH
}

// CompiledChain은 ChainDef + 파싱된 Window(time.Duration).
type CompiledChain struct {
	Def    ChainDef
	Window time.Duration
}

type CompiledNoiseFilter struct {
	ID     string
	Reason string
	Regex  *regexp.Regexp
}

type CompiledGoSSH struct {
	PrefixRegex               *regexp.Regexp
	PrefixFallbacks            []*regexp.Regexp
	OrphanLinePolicy           string
	CollectionFailurePatterns []*regexp.Regexp
}

// Load는 YAML 파일을 읽어 Raw 구조체로 언마샬한다.
func Load(path string) (*Raw, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("패턴 파일 읽기 실패: %w", err)
	}
	var raw Raw
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("패턴 YAML 파싱 실패: %w", err)
	}
	return &raw, nil
}

// Compile은 Raw의 모든 정규식 문자열을 *regexp.Regexp로 컴파일한다.
// 잘못된 regex가 하나라도 있으면 어떤 패턴 ID에서 실패했는지 명시하고 에러를 반환한다.
// (조용히 패턴을 건너뛰지 않는다 — 오탐/미탐 원인을 추적 가능하게)
func Compile(raw *Raw) (*Compiled, error) {
	c := &Compiled{
		BySource:      make(map[string][]CompiledPattern),
		SenseKeyMap:   raw.SenseKeyMap,
		HostStatusMap: raw.HostStatusMap,
	}

	for _, p := range raw.Patterns {
		re, err := regexp.Compile(p.Regex)
		if err != nil {
			return nil, fmt.Errorf("패턴 %q regex 컴파일 실패: %w", p.ID, err)
		}
		cp := CompiledPattern{Def: p, Regex: re, ExtractRegex: map[string]*regexp.Regexp{}}
		for key, exprStr := range p.Extract {
			exRe, err := regexp.Compile(exprStr)
			if err != nil {
				return nil, fmt.Errorf("패턴 %q extract[%s] regex 컴파일 실패: %w", p.ID, key, err)
			}
			cp.ExtractRegex[key] = exRe
		}
		c.BySource[p.Source] = append(c.BySource[p.Source], cp)
	}

	for _, nf := range raw.NoiseFilters {
		re, err := regexp.Compile(nf.Regex)
		if err != nil {
			return nil, fmt.Errorf("noise_filter %q regex 컴파일 실패: %w", nf.ID, err)
		}
		c.NoiseFilters = append(c.NoiseFilters, CompiledNoiseFilter{ID: nf.ID, Reason: nf.Reason, Regex: re})
	}

	gs, err := compileGoSSH(raw)
	if err != nil {
		return nil, err
	}
	c.GoSSH = gs

	for _, chain := range raw.CorrelationChains {
		if chain.Window == "" {
			return nil, fmt.Errorf("correlation_chains %q: window 필드가 없습니다", chain.ID)
		}
		d, err := time.ParseDuration(chain.Window)
		if err != nil {
			return nil, fmt.Errorf("correlation_chains %q window(%q) 파싱 실패: %w", chain.ID, chain.Window, err)
		}
		c.Chains = append(c.Chains, CompiledChain{Def: chain, Window: d})
	}

	return c, nil
}

func compileGoSSH(raw *Raw) (CompiledGoSSH, error) {
	var out CompiledGoSSH
	out.OrphanLinePolicy = raw.GoSSH.OrphanLinePolicy
	if out.OrphanLinePolicy == "" {
		out.OrphanLinePolicy = "carry"
	}

	re, err := regexp.Compile(raw.GoSSH.PrefixRegex)
	if err != nil {
		return out, fmt.Errorf("gossh.prefix_regex 컴파일 실패: %w", err)
	}
	out.PrefixRegex = re

	for i, fb := range raw.GoSSH.PrefixFallbacks {
		fre, err := regexp.Compile(fb)
		if err != nil {
			return out, fmt.Errorf("gossh.prefix_fallbacks[%d] 컴파일 실패: %w", i, err)
		}
		out.PrefixFallbacks = append(out.PrefixFallbacks, fre)
	}

	for i, cf := range raw.GoSSH.CollectionFailurePatterns {
		cre, err := regexp.Compile(cf)
		if err != nil {
			return out, fmt.Errorf("gossh.collection_failure_patterns[%d] 컴파일 실패: %w", i, err)
		}
		out.CollectionFailurePatterns = append(out.CollectionFailurePatterns, cre)
	}

	return out, nil
}
