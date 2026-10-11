// Package render 는 apply 스크립트를 만들어냅니다.
//
// 대상마다 IP/게이트웨이가 다르므로, 노드별로 스크립트를 따로 만들지 않고
// 스크립트 하나에 "hostname 변경IP" 전체 표를 싣습니다. 각 노드는 실행 시
// 자신의 hostname 으로 그 표에서 자기 행만 찾아 씁니다 — gossh 한 번으로
// 대상 전체에 같은 스크립트를 뿌릴 수 있습니다.
package render

import (
	_ "embed"
	"fmt"
	"strings"

	"ip-change/internal/config"
	"ip-change/internal/target"
)

//go:embed apply_body.sh
var applyBody string

//go:embed rollback_body.sh
var rollbackBody string

const hostMapPlaceholder = "__HOST_MAP__"

// ApplyScript 는 대상 목록 전체에 대한 apply 스크립트 전문을 만듭니다.
func ApplyScript(entries []target.Entry, cfg *config.Config) (string, error) {
	if len(entries) == 0 {
		return "", fmt.Errorf("대상이 없습니다")
	}

	var hostMap strings.Builder
	for _, e := range entries {
		fmt.Fprintf(&hostMap, "%s %s\n", e.Host, e.NewIP)
	}

	body := strings.Replace(applyBody, hostMapPlaceholder, hostMap.String(), 1)

	var b strings.Builder
	b.WriteString("#!/bin/bash\n")
	b.WriteString("# ip-change-engine 이 자동 생성한 파일입니다. 직접 편집하지 마십시오.\n")
	b.WriteString("set -u\n\n")

	fmt.Fprintf(&b, "NETWORK_SCRIPTS_DIR=%s\n", shQuote(cfg.NetworkScriptsDir))
	fmt.Fprintf(&b, "RHEL9_PATH=%s\n\n", shQuote(cfg.RHEL9Path))

	b.WriteString(body)
	return b.String(), nil
}

// RollbackScript 는 대상 노드에서 최근(또는 지정 STAMP) ifcfg 백업을 되돌리는
// 스크립트 전문을 만듭니다. 대상별로 값이 다르지 않으므로 host map 은 없습니다 —
// 어느 노드에 뿌릴지는 gossh 의 호스트 목록이 결정합니다.
//
// stampTo 가 빈 문자열이면 가장 최근 백업을 되돌립니다.
func RollbackScript(cfg *config.Config, stampTo string) (string, error) {
	var b strings.Builder
	b.WriteString("#!/bin/bash\n")
	b.WriteString("# ip-change-engine 이 자동 생성한 파일입니다. 직접 편집하지 마십시오.\n")
	b.WriteString("set -u\n\n")

	fmt.Fprintf(&b, "NETWORK_SCRIPTS_DIR=%s\n", shQuote(cfg.NetworkScriptsDir))
	fmt.Fprintf(&b, "RHEL9_PATH=%s\n", shQuote(cfg.RHEL9Path))
	fmt.Fprintf(&b, "ROLLBACK_TO=%s\n\n", shQuote(stampTo))

	b.WriteString(rollbackBody)
	return b.String(), nil
}

// shQuote 는 문자열을 셸 작은따옴표로 감쌉니다.
func shQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
