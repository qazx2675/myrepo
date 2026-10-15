package main

// checkres.go — auto_setup 이 읽는 호스트별 한 줄 결과 파일 check.res_<user> (계획서_v0.2_TUI.md).
//
//	hpe-node01: OK
//	hpe-node02: FAIL difference 2건 (CollabPowerControl Enabled→Disabled, ProcHyperthreading Disabled→Enabled)
//	hpe-node03: FAIL N/A 접속실패(UNREACHABLE)

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// checkResMaxItems 는 한 줄에 풀어 쓰는 difference 속성 수의 상한입니다. 넘으면 " 외 N건" 으로 줄입니다.
const checkResMaxItems = 10

// checkResLine 은 호스트 1대의 판정 줄입니다. BMC·호스트에서 온 글자(호스트·비고·속성·값)는 lineSafe 로
// 제어 문자(개행·ESC 등)를 공백으로 바꿔, 결과 파일이 호스트당 정확히 한 줄이고 터미널 이스케이프가 없게 합니다.
//
// 판정 규칙:
//   - 호스트 오류 줄(접속·조회 실패)이 있으면 FAIL N/A 접속실패(비고)
//   - difference 가 1건이라도 있으면 FAIL difference N건 (속성 현재값→기준값, ...)
//   - 속성 수준 N/A(MAPPING_MISSING·NO_DUMP·ONLY_REF)만 있는 경우는 실패가 아니다: OK (N/A N건).
//     N/A 는 "판정할 수 없음"이지 "다름"이 아니므로 auto_setup 을 막지 않고, 건수만 남겨 둔다.
//   - 줄이 하나도 없으면 FAIL N/A 결과없음
func checkResLine(host string, lines []ResultLine) string {
	host = lineSafe(host)
	if len(lines) == 0 {
		return host + ": FAIL N/A 결과없음"
	}
	var items []string
	na := 0
	for _, l := range lines {
		if l.HostError {
			msg := "접속실패"
			if l.Note != "" {
				msg += "(" + lineSafe(l.Note) + ")"
			}
			return host + ": FAIL N/A " + msg
		}
		switch l.Tag {
		case TagDiff:
			items = append(items, lineSafe(l.Attr+" "+showValue(l.Current)+"→"+showValue(l.Expected)))
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

// removeCheckRes 는 비대화형 실행을 시작할 때 이전 실행의 결과 파일들을 지웁니다: 이번 실행이 치명적 오류로
// 끝나도 auto_setup 이 옛 OK 줄을 읽지 않게 하려는 것입니다. 없으면 넘어가고, 지우지 못하면 오류입니다.
// os.Remove 는 심볼릭 링크면 링크 자체만 지운다 (가리키는 파일은 건드리지 않음).
func removeCheckRes(paths []string) error {
	for _, p := range paths {
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("이전 결과 %s 삭제 실패: %w", p, err)
		}
	}
	return nil
}

// writeFileAtomic 은 path 와 같은 폴더에 임시 파일을 새로 만들어(os.CreateTemp — O_EXCL) 쓰고 fsync·chmod 한 뒤
// rename 으로 바꿔 끼웁니다. 읽는 쪽은 옛 파일 또는 새 파일 전체만 보고, 반쯤 쓴 파일은 보지 않습니다.
// rename 은 대상 경로에 미리 있던 심볼릭 링크를 따라가지 않고 링크 자체를 바꾸므로, 링크가 가리키는 파일을 덮어쓰지 않습니다.
// 결과 폴더 자체가 심볼릭 링크인 경우는 막지 않습니다 (호출한 쪽이 -res-dir 로 고른 작업 폴더이므로 그대로 따른다).
func writeFileAtomic(path string, data []byte, perm os.FileMode) (err error) {
	f, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer func() {
		if err != nil {
			os.Remove(tmp)
		}
	}()
	if _, err = f.Write(data); err == nil {
		err = f.Sync()
	}
	if err == nil {
		err = f.Chmod(perm) // CreateTemp 는 0600 — umask 와 상관없이 perm 으로 맞춘다
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// writeCheckRes 는 호스트마다 한 줄씩 path 에 씁니다 (writeFileAtomic). 순서는 hostsOrder 이고, hostsOrder 에 없는데
// 줄이 있는 호스트는 그 뒤에 처음 나온 순서로 붙습니다. auto_setup 이 다른 사용자·프로세스로 읽으므로 0644 입니다.
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
	return writeFileAtomic(path, []byte(b.String()), 0o644)
}
