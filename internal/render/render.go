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

//go:embed fixgw_body.sh
var fixgwBody string

const hostMapPlaceholder = "__HOST_MAP__"

const gwMapPlaceholder = "__GW_MAP__"

// ApplyOptions 는 apply 스크립트 헤더에 넣는 실행 옵션입니다.
type ApplyOptions struct {
	// CheckOnly 가 true 면 노드는 바꿀 내용만 출력하고 아무 파일도 쓰지 않습니다 (-check).
	CheckOnly bool
	// BondMode 는 새 bond 프로필의 모드입니다 (-bond-mode).
	// "" 또는 "active-backup" 이면 active-backup, "lacp" 또는 "802.3ad" 이면 802.3ad 입니다.
	BondMode string
}

// NormalizeBondMode 는 -bond-mode 값을 스크립트에 넣을 bonding 모드 이름으로 바꿉니다.
func NormalizeBondMode(s string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "active-backup":
		return "active-backup", nil
	case "lacp", "802.3ad":
		return "802.3ad", nil
	}
	return "", fmt.Errorf("알 수 없는 bond 모드입니다: %q (active-backup 또는 lacp)", s)
}

// ForcedDir 는 conf 의 network_scripts_dir 가 기본값과 다를 때만 그 경로를 돌려줍니다.
// 기본값이면 "" 입니다 — 이때 노드는 nmcli 조회와 ifcfg/keyfile 디렉토리를 자동으로 찾습니다.
// (config.Load 는 지정하지 않아도 기본값을 채우므로, 기본값과 같으면 강제 경로로 보지 않습니다)
func ForcedDir(cfg *config.Config) string {
	d := strings.TrimSpace(cfg.NetworkScriptsDir)
	if d == "" || strings.TrimRight(d, "/") == config.DefaultNetworkScriptsDir {
		return ""
	}
	return d
}

// ApplyScript 는 대상 목록 전체에 대한 apply 스크립트 전문을 기본 옵션으로 만듭니다.
func ApplyScript(entries []target.Entry, cfg *config.Config) (string, error) {
	return ApplyScriptWithOptions(entries, cfg, ApplyOptions{})
}

// ApplyScriptWithOptions 는 대상 목록 전체에 대한 apply 스크립트 전문을 만듭니다.
func ApplyScriptWithOptions(entries []target.Entry, cfg *config.Config, opts ApplyOptions) (string, error) {
	bondMode, err := NormalizeBondMode(opts.BondMode)
	if err != nil {
		return "", err
	}
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

	checkOnly := "0"
	if opts.CheckOnly {
		checkOnly = "1"
	}

	fmt.Fprintf(&b, "NETWORK_SCRIPTS_DIR=%s\n", shQuote(cfg.NetworkScriptsDir))
	fmt.Fprintf(&b, "RHEL9_PATH=%s\n", shQuote(cfg.RHEL9Path))
	fmt.Fprintf(&b, "FORCED_DIR=%s\n", shQuote(ForcedDir(cfg)))
	fmt.Fprintf(&b, "CHECK_ONLY=%s\n", shQuote(checkOnly))
	fmt.Fprintf(&b, "BOND_MODE=%s\n\n", shQuote(bondMode))

	b.WriteString(body)
	return b.String(), nil
}

// RollbackScript 는 대상 노드에서 최근(또는 지정 STAMP) 백업(/var/lib/ip_change/backup/<STAMP>,
// 없으면 옛 ifcfg .bak.<STAMP>)을 되돌리는
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

// GatewayFix 는 예전 게이트웨이를 고칠 노드 하나의 정보입니다 (수정계획서 4.8).
// Files 는 노드의 절대 경로입니다.
type GatewayFix struct {
	Host  string
	OldGW string
	NewGW string
	Files []string
	// Stamp 는 apply 결과 비고의 BACKUP=<STAMP> 입니다. 고침 백업이 apply 와 같은
	// STAMP 디렉토리에 들어가야 -rollback 한 번으로 함께 되돌아갑니다. 숫자여야 합니다.
	Stamp string
}

// FixGatewayScript 는 예전 게이트웨이를 고치는 두 번째 원격 스크립트 전문을 만듭니다.
// 각 노드는 자신의 hostname 으로 표에서 자기 줄만 찾아 씁니다.
func FixGatewayScript(items []GatewayFix) (string, error) {
	if len(items) == 0 {
		return "", fmt.Errorf("고칠 대상이 없습니다")
	}

	var gwMap strings.Builder
	for _, it := range items {
		if it.Host == "" || it.OldGW == "" || it.NewGW == "" || len(it.Files) == 0 || !isDigits(it.Stamp) {
			return "", fmt.Errorf("게이트웨이 고침 항목이 올바르지 않습니다: %+v", it)
		}
		files := strings.Join(it.Files, ",")
		for _, f := range []string{it.Host, it.OldGW, it.NewGW, files, it.Stamp} {
			if strings.ContainsAny(f, " \t\r\n") {
				return "", fmt.Errorf("게이트웨이 고침 항목에 공백이 있습니다: %q", f)
			}
		}
		fmt.Fprintf(&gwMap, "%s %s %s %s %s\n", it.Host, it.OldGW, it.NewGW, files, it.Stamp)
	}

	var b strings.Builder
	b.WriteString("#!/bin/bash\n")
	b.WriteString("# ip-change-engine 이 자동 생성한 파일입니다. 직접 편집하지 마십시오.\n")
	b.WriteString("set -u\n\n")
	b.WriteString(strings.Replace(fixgwBody, gwMapPlaceholder, gwMap.String(), 1))
	return b.String(), nil
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// shQuote 는 문자열을 셸 작은따옴표로 감쌉니다.
func shQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
