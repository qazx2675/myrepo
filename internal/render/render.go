// Package render 는 대상 노드에서 실행될 bash 스크립트(적용 및 롤백)를 조립합니다.
package render

import (
	_ "embed"
	"fmt"
	"strings"

	"account-mount/internal/info"
)

//go:embed lib_common.sh
var libCommon string

//go:embed apply_body.sh
var applyBody string

//go:embed rollback_body.sh
var rollbackBody string

const accountsPlaceholder = "__ACCOUNTS_DATA__"

// RollbackMode 는 롤백 동작 모드입니다.
type RollbackMode string

const (
	RollbackList   RollbackMode = "list"
	RollbackLatest RollbackMode = "latest"
	RollbackStamp  RollbackMode = "stamp"
)

// ApplyScript 는 info.txt 에서 읽은 계정 목록을 바탕으로 대상 노드에서 실행될
// 설정 적용 스크립트를 생성합니다.
func ApplyScript(accounts []info.Account) (string, error) {
	if len(accounts) == 0 {
		return "", fmt.Errorf("적용할 계정이 없습니다")
	}

	var data strings.Builder
	for _, a := range accounts {
		fmt.Fprintf(&data, "%s %s %s\n", a.User, a.ID, a.Storage)
	}

	body := strings.ReplaceAll(applyBody, accountsPlaceholder, strings.TrimRight(data.String(), "\n"))

	var b strings.Builder
	b.WriteString("#!/bin/bash\n")
	b.WriteString("# account-mount-engine 이 자동 생성한 파일입니다. 직접 편집하지 마십시오.\n")
	b.WriteString("set -u\n\n")
	b.WriteString(libCommon)
	b.WriteString("\n")
	b.WriteString(body)
	return b.String(), nil
}

// RollbackScript 는 대상 노드의 백업(.bak.<STAMP>)을 조회하거나 복원하는 스크립트를 생성합니다.
func RollbackScript(mode RollbackMode, stamp string) (string, error) {
	switch mode {
	case RollbackList, RollbackLatest:
	case RollbackStamp:
		if strings.TrimSpace(stamp) == "" {
			return "", fmt.Errorf("되돌릴 STAMP 가 비어 있습니다")
		}
	default:
		return "", fmt.Errorf("알 수 없는 롤백 모드: %q", mode)
	}

	var b strings.Builder
	b.WriteString("#!/bin/bash\n")
	b.WriteString("# account-mount-engine 이 자동 생성한 파일입니다. 직접 편집하지 마십시오.\n")
	b.WriteString("set -u\n\n")
	fmt.Fprintf(&b, "MODE=%s\n", shQuote(string(mode)))
	fmt.Fprintf(&b, "STAMP=%s\n\n", shQuote(stamp))
	b.WriteString(libCommon)
	b.WriteString("\n")
	b.WriteString(rollbackBody)
	return b.String(), nil
}

func shQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

