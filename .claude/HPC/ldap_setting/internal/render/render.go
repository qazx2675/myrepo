// Package render 는 사이트별 apply 스크립트를 만들어냅니다.
//
// 노드마다 다른 스크립트를 만들지 않습니다. RHEL 버전과 hostname(s4) 판정은
// 노드 현장에서 이뤄지므로, 같은 사이트의 노드는 모두 같은 스크립트를 받습니다.
package render

import (
	_ "embed"
	"fmt"
	"strings"

	"ldap-automation/internal/config"
)

//go:embed lib_common.sh
var libCommon string

//go:embed apply_body.sh
var applyBody string

//go:embed rollback_body.sh
var rollbackBody string

// ApplyScript 는 (인프라, 사이트) 조합 하나에 대한 apply 스크립트 전문을 만듭니다.
func ApplyScript(in config.Infra, s4 config.S4Rule, site string) (string, error) {
	st, ok := in.Sites[site]
	if !ok {
		return "", fmt.Errorf("인프라 %q 에 사이트 %q 가 정의되어 있지 않습니다", in.Name, site)
	}

	uris := in.URIList(site)
	if len(uris) == 0 {
		return "", fmt.Errorf("인프라 %q 사이트 %q: 사용할 URI 가 하나도 없습니다", in.Name, site)
	}

	var b strings.Builder
	b.WriteString("#!/bin/bash\n")
	b.WriteString("# ldap-config-engine 이 자동 생성한 파일입니다. 직접 편집하지 마십시오.\n")
	b.WriteString(fmt.Sprintf("# infra=%s site=%s\n", in.Name, site))
	b.WriteString("set -u\n")
	b.WriteString("TAB=$'\\t'\n\n")

	b.WriteString("###############################################################################\n")
	b.WriteString("# 사이트별 값 (Go 엔진 생성)\n")
	b.WriteString("###############################################################################\n\n")

	writeVar(&b, "INFRA", in.Name)
	writeVar(&b, "SITE", site)
	writeVar(&b, "URI_LINE", strings.Join(uris, " "))
	writeVar(&b, "BINDDN", in.BindDN)
	writeVar(&b, "BINDPW", in.BindPW)
	writeVar(&b, "DNS_LIST", strings.Join(in.DNS, "\n"))
	writeVar(&b, "NTP_LIST", strings.Join(in.NTP, "\n"))
	writeVar(&b, "APPL_STORAGE", st.Storage)
	writeVar(&b, "APPL_MOUNT", st.Mountpoint)
	writeVar(&b, "WAPPL_MOUNT", st.WapplMount)

	writeVar(&b, "S4_ENABLED", boolVar(s4.Enabled))
	writeVar(&b, "S4_PREFIX", s4.Prefix)
	writeVar(&b, "S4_NSLCD", boolVar(hasService(s4, "nslcd")))
	writeVar(&b, "S4_NTP", boolVar(hasService(s4, "ntp")))

	b.WriteString("\n")
	b.WriteString(libCommon)
	b.WriteString("\n")
	b.WriteString(applyBody)
	return b.String(), nil
}

// RollbackMode 는 되돌리기 방식입니다.
type RollbackMode string

const (
	// RollbackList 는 남아 있는 백업 시점만 조회합니다. 아무것도 바꾸지 않습니다.
	RollbackList RollbackMode = "list"
	// RollbackLatest 는 가장 최근 시점으로 되돌립니다.
	RollbackLatest RollbackMode = "latest"
	// RollbackStamp 는 지정한 시점으로 되돌립니다.
	RollbackStamp RollbackMode = "stamp"
)

// RollbackScript 는 되돌리기 스크립트 전문을 만듭니다.
//
// 적용 스크립트와 달리 인프라·사이트 값이 필요 없습니다.
// 노드에 남아 있는 <파일>.bak.<STAMP> 만 보고 판단하기 때문입니다.
func RollbackScript(mode RollbackMode, stamp string) (string, error) {
	switch mode {
	case RollbackList, RollbackLatest:
		stamp = ""
	case RollbackStamp:
		if !validStamp(stamp) {
			return "", fmt.Errorf("시점(STAMP)은 숫자 14자리여야 합니다: %q", stamp)
		}
	default:
		return "", fmt.Errorf("알 수 없는 롤백 방식: %q", mode)
	}

	var b strings.Builder
	b.WriteString("#!/bin/bash\n")
	b.WriteString("# ldap-config-engine 이 자동 생성한 파일입니다. 직접 편집하지 마십시오.\n")
	b.WriteString(fmt.Sprintf("# rollback mode=%s stamp=%s\n", mode, stamp))
	b.WriteString("set -u\n\n")
	writeVar(&b, "MODE", string(mode))
	writeVar(&b, "STAMP", stamp)
	b.WriteString("\n")
	b.WriteString(libCommon)
	b.WriteString("\n")
	b.WriteString(rollbackBody)
	return b.String(), nil
}

// validStamp 은 apply 가 만드는 %Y%m%d%H%M%S 형식인지 봅니다.
// 원격 셸로 나가는 값이라 숫자만 허용합니다.
func validStamp(s string) bool {
	if len(s) != 14 {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// writeVar 는 값을 홑따옴표로 감싸 bash 변수 대입문을 씁니다.
// 값 안의 홑따옴표는 '\'' 로 탈출합니다.
func writeVar(b *strings.Builder, name, val string) {
	b.WriteString(name)
	b.WriteString("='")
	b.WriteString(strings.ReplaceAll(val, "'", `'\''`))
	b.WriteString("'\n")
}

func boolVar(v bool) string {
	if v {
		return "1"
	}
	return "0"
}

func hasService(s4 config.S4Rule, name string) bool {
	if !s4.Enabled {
		return false
	}
	for _, s := range s4.Services {
		if s == name {
			return true
		}
	}
	return false
}
