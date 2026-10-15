package main

// checkres.go — auto_setup 이 읽는 호스트별 한 줄 결과 파일 check.res_<user> (계획서_v0.2_TUI.md).
//
//	hpe-node01: OK
//	hpe-node02: FAIL difference 2건 (CollabPowerControl Enabled→Disabled, ProcHyperthreading Disabled→Enabled)
//	hpe-node03: FAIL N/A 접속실패(UNREACHABLE)

import (
	"os"
	"strconv"
	"strings"
)

// checkResMaxItems 는 한 줄에 풀어 쓰는 difference 속성 수의 상한입니다. 넘으면 " 외 N건" 으로 줄입니다.
const checkResMaxItems = 10

// oneLine 은 값 안의 개행을 공백으로 바꿔 결과 파일이 호스트당 정확히 한 줄이 되게 합니다.
func oneLine(s string) string {
	return strings.NewReplacer("\r", " ", "\n", " ").Replace(s)
}

// checkResLine 은 호스트 1대의 판정 줄입니다.
//
// 판정 규칙:
//   - 호스트 오류 줄(접속·조회 실패)이 있으면 FAIL N/A 접속실패(비고)
//   - difference 가 1건이라도 있으면 FAIL difference N건 (속성 현재값→기준값, ...)
//   - 속성 수준 N/A(MAPPING_MISSING·NO_DUMP·ONLY_REF)만 있는 경우는 실패가 아니다: OK (N/A N건).
//     N/A 는 "판정할 수 없음"이지 "다름"이 아니므로 auto_setup 을 막지 않고, 건수만 남겨 둔다.
//   - 줄이 하나도 없으면 FAIL N/A 결과없음
func checkResLine(host string, lines []ResultLine) string {
	if len(lines) == 0 {
		return host + ": FAIL N/A 결과없음"
	}
	var items []string
	na := 0
	for _, l := range lines {
		if l.HostError {
			msg := "접속실패"
			if l.Note != "" {
				msg += "(" + oneLine(l.Note) + ")"
			}
			return host + ": FAIL N/A " + msg
		}
		switch l.Tag {
		case TagDiff:
			items = append(items, oneLine(l.Attr+" "+showValue(l.Current)+"→"+showValue(l.Expected)))
		case TagNA:
			na++
		}
	}
	if len(items) > 0 {
		n := len(items)
		list := strings.Join(items, ", ")
		if n > checkResMaxItems {
			list = strings.Join(items[:checkResMaxItems], ", ") + " 외 " + strconv.Itoa(n-checkResMaxItems) + "건"
		}
		return host + ": FAIL difference " + strconv.Itoa(n) + "건 (" + list + ")"
	}
	if na > 0 {
		return host + ": OK (N/A " + strconv.Itoa(na) + "건)"
	}
	return host + ": OK"
}

// writeCheckRes 는 호스트마다 한 줄씩 path 에 씁니다. 순서는 hostsOrder 이고, hostsOrder 에 없는데 줄이 있는
// 호스트는 그 뒤에 처음 나온 순서로 붙습니다. auto_setup 이 다른 사용자·프로세스로 읽으므로 0644 입니다.
func writeCheckRes(path string, lines []ResultLine, hostsOrder []string) error {
	order := append([]string(nil), hostsOrder...)
	known := map[string]bool{}
	for _, h := range order {
		known[h] = true
	}
	byHost := map[string][]ResultLine{}
	for _, l := range lines {
		if !known[l.Host] {
			known[l.Host] = true
			order = append(order, l.Host)
		}
		byHost[l.Host] = append(byHost[l.Host], l)
	}
	var b strings.Builder
	for _, h := range order {
		b.WriteString(checkResLine(h, byHost[h]))
		b.WriteByte('\n')
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		return err
	}
	return os.Chmod(path, 0o644) // 이미 있던 파일이거나 umask 가 막아도 0644 로 맞춘다
}
