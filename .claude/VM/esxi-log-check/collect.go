// collect.go: -w 모드 전용 — 내부적으로 gossh를 서브프로세스로 호출해서
// vobd.log/vmkernel.log/ipmi_sel을 수집한다.
//
// M2에서 정한 "수집은 gossh, 분석은 esxi-log-check" 역할 분리는 그대로 유지한다.
// 여기서도 SSH를 직접 구현하지 않고, 이미 검증된 gossh 바이너리를 exec으로
// 호출할 뿐이다 — gossh 내부의 워커풀(-c 옵션)을 그대로 재사용한다.
package main

import (
	"fmt"
	"os"
	"os/exec"
)

// autoSource는 -w 모드에서 자동으로 수집할 로그 소스 하나.
// Name은 esxi_critical_patterns.yaml의 source 필드/-input의 source명과
// 반드시 일치해야 매칭된다.
type autoSource struct {
	Name string
	Cmd  func(tailLines int) string
}

// autoSources: -w 모드가 자동 수집하는 소스 목록.
// 다른 소스(예: hostd.log)를 자동 수집에 추가하고 싶으면 이 슬라이스에
// 항목을 하나 더 넣으면 된다 — main.go 나머지 로직은 손댈 필요 없다.
var autoSources = []autoSource{
	{Name: "vobd.log", Cmd: func(n int) string { return fmt.Sprintf("tail -%d /var/run/log/vobd.log", n) }},
	{Name: "vmkernel.log", Cmd: func(n int) string { return fmt.Sprintf("tail -%d /var/run/log/vmkernel.log", n) }},
	{Name: "ipmi_sel", Cmd: func(int) string { return "localcli hardware ipmi sel list" }},
}

// collectAllViaGossh는 autoSources 전부를 gossh 서브프로세스로 순회 수집해서
// {source명: 임시파일경로} 맵을 만든다.
//
// 특정 소스(혹은 gossh 자체)가 실패해도 나머지는 계속 진행한다 — 실패한 소스는
// stderr 경고만 남기고 결과 맵에서 빠지며, main.go 쪽에서는 "그 소스는 findings
// 0건"으로 자연스럽게 처리된다 (전체 실행이 죽지 않는다).
func collectAllViaGossh(gosshPath, hostFile string, tailLines int) map[string]string {
	collected := map[string]string{}

	if _, err := exec.LookPath(gosshPath); err != nil {
		fmt.Fprintf(os.Stderr, "경고: gossh 바이너리(%q)를 찾을 수 없습니다 — -w 자동 수집을 전부 건너뜁니다 (필요하면 -gosshPath로 경로 지정): %v\n", gosshPath, err)
		return collected
	}

	for _, src := range autoSources {
		path, err := collectOneViaGossh(gosshPath, hostFile, src, tailLines)
		if err != nil {
			fmt.Fprintf(os.Stderr, "경고: source=%q 자동 수집 실패 — 이 소스는 이번 점검에서 빠집니다: %v\n", src.Name, err)
			continue
		}
		collected[src.Name] = path
	}
	return collected
}

func collectOneViaGossh(gosshPath, hostFile string, src autoSource, tailLines int) (string, error) {
	tmp, err := os.CreateTemp("", "esxi-log-check-"+src.Name+"-*.txt")
	if err != nil {
		return "", fmt.Errorf("임시파일 생성 실패: %w", err)
	}
	defer tmp.Close()

	cmdStr := src.Cmd(tailLines)
	// 실측 확인된 gossh 특성: 플래그(-w, -script)는 반드시 원격 명령 문자열보다
	// 앞에 와야 한다. 뒤에 두면 "-script"가 명령 문자열에 그대로 붙어버려서
	// 원격 셸이 이상하게 해석한다 (예: tail -300 ... -script -> "invalid number").
	c := exec.Command(gosshPath, "-w", hostFile, "-script", cmdStr)
	c.Stdout = tmp
	c.Stderr = tmp // SSH 실패 라인도 여기 섞여야 collection_failure_patterns가 잡는다

	if err := c.Run(); err != nil {
		return "", fmt.Errorf("gossh 실행 실패(cmd=%q): %w", cmdStr, err)
	}
	return tmp.Name(), nil
}
