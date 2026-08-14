// main.go: esxi-log-check — gossh -w 로 수집한 다중호스트 로그를 패턴 레지스트리로 점검한다.
//
// 가장 간단한 사용법 (M4: 원샷 모드) — gossh의 -w와 동일한 사용감:
//   esxi-log-check -w hosts.txt
//
// 이러면 내부적으로 gossh를 호출해 vobd.log/vmkernel.log/ipmi_sel을 수집하고,
// 매칭 + aggregate + correlation_chains 전부 돌려서 리포트까지 한 번에 낸다.
//
// 기존 수동 방식(이미 수집해 둔 파일을 지정)도 그대로 쓸 수 있다:
//   gossh -w hosts.txt -script "tail -300 /var/run/log/vobd.log"     > collected/vobd.txt
//   gossh -w hosts.txt -script "tail -300 /var/run/log/vmkernel.log" > collected/vmkernel.txt
//   gossh -w hosts.txt -script "localcli hardware ipmi sel list"     > collected/ipmi_sel.txt
//
//   esxi-log-check \
//     -patterns esxi_critical_patterns.yaml \
//     -hostlist hosts.txt \
//     -input vobd.log=collected/vobd.txt \
//     -input vmkernel.log=collected/vmkernel.txt \
//     -input ipmi_sel=collected/ipmi_sel.txt \
//     -format text
//
// 둘을 섞어 쓸 수도 있다: -w로 기본 3종을 자동 수집하면서, -input으로 추가
// 소스(예: hostd.log)를 더 얹는 식. 같은 source명을 -input으로도 지정하면
// -input 쪽이 우선한다.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"esxi-log-check/internal/correlate"
	"esxi-log-check/internal/gossh"
	"esxi-log-check/internal/match"
	"esxi-log-check/internal/registry"
	"esxi-log-check/internal/report"
)

// inputMap은 반복 지정되는 "-input source=path" 플래그를 담는다.
type inputMap map[string]string

func (m inputMap) String() string {
	if len(m) == 0 {
		return ""
	}
	parts := make([]string, 0, len(m))
	for k, v := range m {
		parts = append(parts, k+"="+v)
	}
	return strings.Join(parts, ",")
}

func (m *inputMap) Set(value string) error {
	parts := strings.SplitN(value, "=", 2)
	if len(parts) != 2 {
		return fmt.Errorf("형식 오류: -input <source>=<file> (예: -input vobd.log=collected/vobd.txt), got %q", value)
	}
	if *m == nil {
		*m = inputMap{}
	}
	(*m)[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
	return nil
}

func main() {
	patternsPath := flag.String("patterns", "esxi_critical_patterns.yaml", "패턴 레지스트리 YAML 경로")
	hostlistPath := flag.String("hostlist", "", "gossh -w 에 사용한 호스트 목록 파일 (선택, 무응답 호스트 계산용)")
	format := flag.String("format", "text", "출력 형식: text | json")
	outPath := flag.String("out", "", "출력 파일 경로 (미지정 시 stdout)")
	onlyProblems := flag.Bool("onlyProblems", false, "text 리포트 [1] 호스트별 요약에서 완전히 정상인 호스트는 개별 나열하지 않고 카운트만 표시 (수십~수백 대 규모용, 기본 false=전체 나열)")
	silentRebootGap := flag.Duration("silentRebootGap", correlate.DefaultSilentRebootGap, "vmkernel.log 타임스탬프 공백이 이 이상이면 무증상 리부팅 후보로 판단 (예: 5m, 10m)")
	noCorrelate := flag.Bool("noCorrelate", false, "M3 상관관계 분석(aggregate 격상은 항상 적용됨, correlation_chains/무증상 리부팅만) 비활성화")
	hostFile := flag.String("w", "", "gossh -w 와 동일한 사용감의 원샷 모드: 호스트 목록 파일 하나만 지정하면 내부적으로 gossh를 호출해 vobd.log/vmkernel.log/ipmi_sel을 자동 수집하고 점검+리포트까지 실행. -input과 병행 가능(같은 source명은 -input이 우선). -hostlist를 따로 안 주면 이 파일을 hostlist로도 사용함")
	gosshPath := flag.String("gosshPath", "/usr/local/bin/gossh", "-w 모드에서 내부적으로 호출할 gossh 바이너리 경로")
	tailLines := flag.Int("tailLines", 300, "-w 모드에서 vobd.log/vmkernel.log를 각각 몇 줄 tail 할지")

	var inputs inputMap
	flag.Var(&inputs, "input",
		"반복 지정: -input <source>=<gossh출력파일> (source는 patterns.yaml의 sources[].path/cmd 이름과 일치해야 매칭됨, 예: vobd.log, vmkernel.log, ipmi_sel)")

	flag.Parse()

	var tempFiles []string
	defer func() {
		for _, f := range tempFiles {
			os.Remove(f)
		}
	}()

	if *hostFile != "" {
		collected := collectAllViaGossh(*gosshPath, *hostFile, *tailLines)
		for name, path := range collected {
			tempFiles = append(tempFiles, path)
			if _, exists := inputs[name]; !exists { // -input이 이미 지정한 소스는 그대로 우선
				if inputs == nil {
					inputs = inputMap{}
				}
				inputs[name] = path
			}
		}
	}

	if len(inputs) == 0 {
		fmt.Fprintln(os.Stderr, "최소 -w <hostfile> 또는 -input 하나 이상이 필요합니다. 예: esxi-log-check -w hosts.txt")
		flag.Usage()
		os.Exit(2)
	}

	raw, err := registry.Load(*patternsPath)
	if err != nil {
		fatal(err)
	}
	compiled, err := registry.Compile(raw)
	if err != nil {
		fatal(err)
	}

	effectiveHostlist := *hostlistPath
	if effectiveHostlist == "" && *hostFile != "" {
		effectiveHostlist = *hostFile // -w 모드에선 별도 지정 없으면 -w에 준 파일을 hostlist로도 사용
	}

	var allHosts []string
	if effectiveHostlist != "" {
		allHosts, err = loadHostList(effectiveHostlist)
		if err != nil {
			fatal(err)
		}
	}

	var allFindings []match.Finding
	var allFailed []gossh.FailedHost
	var vmkernelLines []gossh.ParsedLine // 무증상 리부팅 탐지용 (타임스탬프 갭 분석은 매칭과 무관하게 원본 라인이 필요)

	// map 순회는 비결정적이므로 source명을 정렬해 재현 가능한 순서로 처리
	sourceNames := make([]string, 0, len(inputs))
	for s := range inputs {
		sourceNames = append(sourceNames, s)
	}
	sort.Strings(sourceNames)

	for _, srcName := range sourceNames {
		path := inputs[srcName]

		if _, ok := compiled.BySource[srcName]; !ok {
			fmt.Fprintf(os.Stderr, "경고: source=%q 에 해당하는 패턴이 registry에 없습니다 (오탈자 확인 — patterns.yaml의 source 필드와 -input의 source명이 정확히 일치해야 함)\n", srcName)
		}

		f, err := os.Open(path)
		if err != nil {
			fatal(fmt.Errorf("입력 파일 열기 실패(%s): %w", path, err))
		}
		lines, failed, err := gossh.ParseStream(f, compiled.GoSSH, srcName)
		f.Close()
		if err != nil {
			fatal(fmt.Errorf("gossh 스트림 파싱 실패(%s): %w", path, err))
		}

		findings := match.Match(lines, srcName, compiled)
		allFindings = append(allFindings, findings...)
		allFailed = append(allFailed, failed...)

		if srcName == "vmkernel.log" {
			vmkernelLines = lines
		}
	}

	// M3: correlation_chains(다중 이벤트 상관분석) + 무증상 리부팅 탐지.
	// 전체 소스를 다 모은 뒤에만 판단 가능해서 여기서(루프 밖) 실행한다.
	if !*noCorrelate {
		allFindings = append(allFindings, correlate.BuildChainFindings(allFindings, compiled.Chains)...)

		var silentRebootChain *registry.CompiledChain
		for i := range compiled.Chains {
			if compiled.Chains[i].Def.ID == "CHAIN_SILENT_REBOOT" {
				silentRebootChain = &compiled.Chains[i]
				break
			}
		}
		if len(vmkernelLines) > 0 {
			allFindings = append(allFindings, correlate.DetectSilentReboots(vmkernelLines, allFindings, *silentRebootGap, silentRebootChain)...)
		}
	}

	result := report.Build(allFindings, allFailed, allHosts)

	var w io.Writer = os.Stdout
	if *outPath != "" {
		of, err := os.Create(*outPath)
		if err != nil {
			fatal(err)
		}
		defer of.Close()
		w = of
	}

	switch *format {
	case "json":
		if err := report.WriteJSON(w, result); err != nil {
			fatal(err)
		}
	default:
		report.WriteText(w, result, report.TextOptions{OnlyProblems: *onlyProblems})
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "오류:", err)
	os.Exit(1)
}

// loadHostList는 gossh -w 에 쓴 것과 같은 형식의 호스트 목록 파일을 읽는다.
// '#'로 시작하는 줄과 인라인 주석은 무시한다.
func loadHostList(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var hosts []string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if idx := strings.Index(line, "#"); idx >= 0 {
			line = strings.TrimSpace(line[:idx])
		}
		if line != "" {
			hosts = append(hosts, line)
		}
	}
	return hosts, nil
}
