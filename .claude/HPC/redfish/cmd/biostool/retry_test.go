package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"

	"biostool/internal/mockbmc"
)

// retry.go (-retry-from: 실패 건 재점검 + 결과 병합) 시험. 실제 BMC 는 쓰지 않고 internal/mockbmc 와
// 실제 서브커맨드(cmdCheck/cmdAllCheck)를 프로세스 안에서 돌린다. 헬퍼 이름은 rt 로 시작한다.

// ---- 환경 ----

type rtEnv struct {
	t       *testing.T
	dir     string // 시험용 작업 폴더
	conf    string
	hosts   string
	results string
}

// newRTEnv 는 conf·암호화한 비밀번호·프로파일(VM, OTHER)·hosts 파일이 있는 작업 폴더를 만듭니다.
func newRTEnv(t *testing.T, authStop int, hostsText string) *rtEnv {
	t.Helper()
	fastCheck(t)
	dir := t.TempDir()
	prof := filepath.Join(dir, "profiles")
	must(t, os.MkdirAll(prof, 0o700))
	b, err := os.ReadFile(profFixture)
	must(t, err)
	for _, n := range []string{"VM", "OTHER"} {
		must(t, os.WriteFile(filepath.Join(prof, n+".tsv"), b, 0o600))
	}
	pass, key := filepath.Join(dir, "pass.enc"), filepath.Join(dir, "key.bin")
	must(t, encryptFromReader(strings.NewReader(testPass), pass, key))
	e := &rtEnv{t: t, dir: dir, conf: filepath.Join(dir, "bios.conf"), hosts: filepath.Join(dir, "hosts"), results: filepath.Join(dir, "results")}
	must(t, os.WriteFile(e.conf, []byte(fmt.Sprintf(
		"user=%s\npass_file=%s\nkey_file=%s\nresult_dir=%s\nprofile_dir=%s\nconcurrency=3\ntimeout=5\nretries=0\nauth_fail_stop=%d\n",
		testUser, pass, key, e.results, prof, authStop)), 0o600))
	must(t, os.WriteFile(e.hosts, []byte(hostsText), 0o644))
	return e
}

func (e *rtEnv) writeFile(name string, lines ...string) string {
	e.t.Helper()
	p := filepath.Join(e.dir, name)
	must(e.t, os.WriteFile(p, []byte(strings.Join(lines, "\n")+"\n"), 0o600))
	return p
}

// captureStdout 은 fn 이 os.Stdout 에 쓴 내용을 돌려줍니다.
func rtCapture(t *testing.T, fn func() error) (string, error) {
	t.Helper()
	r, w, err := os.Pipe()
	must(t, err)
	old := os.Stdout
	os.Stdout = w
	done := make(chan string)
	go func() {
		b, _ := io.ReadAll(r)
		done <- string(b)
	}()
	func() {
		defer func() { os.Stdout = old }()
		err = fn()
	}()
	w.Close()
	return <-done, err
}

func (e *rtEnv) check(extra ...string) (string, error) {
	e.t.Helper()
	args := append([]string{"-conf", e.conf, "-hosts", e.hosts, "-profile", "VM", "-no-prompt"}, extra...)
	return rtCapture(e.t, func() error { return cmdCheck(args) })
}

func (e *rtEnv) allcheck(extra ...string) (string, error) {
	e.t.Helper()
	args := append([]string{"-conf", e.conf, "-hosts", e.hosts}, extra...)
	return rtCapture(e.t, func() error { return cmdAllCheck(args) })
}

// dirs 는 결과 폴더 아래의 폴더 이름(정렬)입니다. 결과 폴더가 아직 없으면 빈 목록입니다.
func (e *rtEnv) dirs() []string {
	ents, err := os.ReadDir(e.results)
	if err != nil {
		return nil
	}
	var out []string
	for _, en := range ents {
		out = append(out, en.Name())
	}
	sort.Strings(out)
	return out
}

// newDir 은 before 이후 새로 생긴 결과 폴더 1개를 돌려줍니다 (정확히 1개가 아니면 실패).
func (e *rtEnv) newDir(before []string) string {
	e.t.Helper()
	old := map[string]bool{}
	for _, n := range before {
		old[n] = true
	}
	var added []string
	for _, n := range e.dirs() {
		if !old[n] {
			added = append(added, n)
		}
	}
	if len(added) != 1 {
		e.t.Fatalf("새 결과 폴더 %v, 기대 1개", added)
	}
	return filepath.Join(e.results, added[0])
}

func (e *rtEnv) noNewDir(before []string) {
	e.t.Helper()
	if fmt.Sprint(e.dirs()) != fmt.Sprint(before) {
		e.t.Errorf("결과 폴더가 달라짐: %v → %v (아무것도 만들면 안 됨)", before, e.dirs())
	}
}

// ---- 파일 도우미 ----

// rtSnapshot 은 폴더의 모든 파일 (상대경로 → 크기·SHA-256) 입니다.
func rtSnapshot(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	must(t, filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(b)
		rel, _ := filepath.Rel(dir, p)
		out[filepath.ToSlash(rel)] = fmt.Sprintf("%d:%s", len(b), hex.EncodeToString(sum[:]))
		return nil
	}))
	return out
}

func rtSameSnapshot(t *testing.T, what string, before, after map[string]string) {
	t.Helper()
	if len(before) == 0 {
		t.Fatalf("%s: 비교할 파일이 없음", what)
	}
	for k, v := range before {
		if after[k] != v {
			t.Errorf("%s: 이전 폴더 파일 %s 가 바뀜/사라짐 (%q → %q)", what, k, v, after[k])
		}
	}
	for k := range after {
		if _, ok := before[k]; !ok {
			t.Errorf("%s: 이전 폴더에 새 파일 %s 가 생김", what, k)
		}
	}
}

// rtLines 는 파일을 줄로 읽습니다 (끝의 빈 줄 없음, 없으면 nil).
func rtLines(t *testing.T, p string) []string {
	t.Helper()
	b, err := os.ReadFile(p)
	must(t, err)
	s := strings.TrimRight(string(b), "\n")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

func rtCol(line string, col int) string { return strings.Split(line, "\t")[col] }

// rtExpectMerged 는 병합 기대값입니다 (줄 단위로 독립 구현): prev 에서 retried 호스트의 첫 행 자리에 cur 의 그 호스트 행을 넣고
// 같은 호스트의 나머지 prev 행은 버리며, prev 에 없던 호스트의 cur 행은 끝에 붙입니다. 머리글은 제외한 줄입니다.
func rtExpectMerged(prev, cur []string, col int, retried map[string]bool) []string {
	of := func(l string) string { return strings.ToLower(rtCol(l, col)) }
	var out []string
	placed := map[string]bool{}
	for _, l := range prev {
		h := of(l)
		if !retried[h] {
			out = append(out, l)
			continue
		}
		if placed[h] {
			continue
		}
		placed[h] = true
		for _, c := range cur {
			if of(c) == h {
				out = append(out, c)
			}
		}
	}
	for _, c := range cur {
		if !placed[of(c)] {
			out = append(out, c)
		}
	}
	return out
}

// rtHostRows 는 (머리글 제외) 줄을 호스트별로 센 것입니다. 같은 호스트의 줄이 떨어져 있으면 실패합니다.
func rtHostRows(t *testing.T, what string, lines []string, col int) (order []string, n map[string]int) {
	t.Helper()
	n = map[string]int{}
	last := ""
	for _, l := range lines {
		h := rtCol(l, col)
		if h != last && n[h] > 0 {
			t.Errorf("%s: 호스트 %s 의 행이 떨어져 있음 (겹침/중복)", what, h)
		}
		if n[h] == 0 {
			order = append(order, h)
		}
		n[h]++
		last = h
	}
	return order, n
}

func rtStates(t *testing.T, lines []string) map[string][]string {
	t.Helper()
	out := map[string][]string{}
	for _, l := range lines {
		out[rtCol(l, 0)] = append(out[rtCol(l, 0)], rtCol(l, 11))
	}
	return out
}

func rtAll(states []string, want string) bool {
	for _, s := range states {
		if s != want {
			return false
		}
	}
	return len(states) > 0
}

// rtRestart 는 닫힌 mock 서버를 같은 주소에 새 mock 으로 다시 띄웁니다 ("닫힌 포트를 mock 으로 띄움").
func rtRestart(t *testing.T, tree, addr string, mod func(s *mockbmc.Server)) *mockbmc.Server {
	t.Helper()
	s, err := mockbmc.New("../../testdata/"+tree, mockbmc.Options{User: testUser, Pass: testPass})
	must(t, err)
	if mod != nil {
		mod(s)
	}
	if _, err := s.ListenTLS(addr); err != nil {
		t.Fatalf("같은 주소(%s)에 mock 을 다시 띄울 수 없음: %v", addr, err)
	}
	t.Cleanup(s.Close)
	return s
}

// rtWritePrev 는 retry.txt 와 result.tsv 만 있는 가짜 이전 결과 폴더를 만듭니다.
func rtWritePrev(t *testing.T, dir string, header []string, rows []string, retry []string) string {
	t.Helper()
	must(t, os.MkdirAll(dir, 0o700))
	content := strings.Join(header, "\t") + "\n"
	for _, r := range rows {
		content += r + "\n"
	}
	must(t, os.WriteFile(filepath.Join(dir, "result.tsv"), []byte(content), 0o600))
	if retry != nil {
		must(t, os.WriteFile(filepath.Join(dir, "retry.txt"), []byte(strings.Join(retry, "\n")+"\n"), 0o600))
	}
	return dir
}

func rtErrRow(host, status string) string {
	return strings.Join([]string{host, "127.0.0.1", "VM", "-", "-", "-", "-", "-", "-", "-", "-", status}, "\t")
}

// ---- 메인 시나리오: check ----

func TestRetryCheckMerge(t *testing.T) {
	env := newRTEnv(t, 3, "127.0.0.1 node07-m\n")
	nItems := len(testProf(t).Items)

	// A 정상 / B 503→복구 / C 닫힘→복구(FAIL) / D 비밀번호 틀림 / E 계속 503 / G 이름 입력(node07-m:포트) 503→복구 / ghost 이름 해석 실패
	srvA, inA := startAllSrv(t, "dell-r660", nil)
	fault503 := func(s *mockbmc.Server) { s.SetFault("/redfish/v1/Systems*", mockbmc.Fault{Status: 503}) }
	srvB, inB := startAllSrv(t, "dell-r660", fault503)
	deadSrv, inC := startAllSrv(t, "dell-r660", nil)
	deadSrv.Close()
	srvD := startMockTree(t, "dell-r660", func(o *mockbmc.Options) { o.Pass = "다른-비밀번호" })
	inD := strings.TrimPrefix(srvD.URL(), "https://")
	srvE, inE := startAllSrv(t, "dell-r660", fault503)
	srvG, inG0 := startAllSrv(t, "dell-r660", fault503)
	inG := "node07-m:" + strings.TrimPrefix(inG0, "127.0.0.1:")
	hostG := "node07:" + strings.TrimPrefix(inG0, "127.0.0.1:") // result.tsv 의 hostname 열 (입력과 표기가 다름)
	user := env.writeFile("user.txt", inA, inB, inC, inD, inE, inG, "ghost")

	// ---- 1차 실행 ----
	before := env.dirs()
	out1, err := env.check("-user", user)
	if err != nil {
		t.Fatalf("1차 check: %v\n%s", err, out1)
	}
	r1 := env.newDir(before)
	st1 := rtStates(t, rtLines(t, filepath.Join(r1, "result.tsv"))[1:])
	for host, want := range map[string]string{inB: StatusBMCError, inC: StatusUnreachable, inD: StatusAuthFail, inE: StatusBMCError, hostG: StatusBMCError, "ghost": StatusNoHostsEntry} {
		if !rtAll(st1[host], want) || len(st1[host]) != 1 {
			t.Errorf("1차 %s = %v, 기대 [%s]", host, st1[host], want)
		}
	}
	if !rtAll(st1[inA], StatusOK) || len(st1[inA]) != nItems {
		t.Errorf("1차 A = %v", st1[inA])
	}
	// retry.txt: 일시 오류만, 원문(Input) 그대로, AUTH_FAIL·NO_HOSTS_ENTRY 는 없음
	if got := rtLines(t, filepath.Join(r1, "retry.txt")); fmt.Sprint(got) != fmt.Sprint([]string{inB, inC, inE, inG}) {
		t.Fatalf("1차 retry.txt = %v, 기대 [B C E G]", got)
	}
	if srvA.Sessions().Created != 1 || srvD.Sessions().LoginFailed != 1 {
		t.Fatalf("1차 세션: A=%+v D=%+v", srvA.Sessions(), srvD.Sessions())
	}

	// ---- 장애 해소: B·G 복구, C 는 같은 주소에 새 mock(FAIL 항목 1개), E 는 계속 503 ----
	srvB.ClearFaults()
	srvG.ClearFaults()
	srvC := rtRestart(t, "dell-r660", inC, func(s *mockbmc.Server) { must(t, s.SetBiosAttr("LlcPrefetch", "Disabled")) })
	snap1 := rtSnapshot(t, r1)

	// ---- 2차: 폴더 경로로 -retry-from ----
	before = env.dirs()
	out2, err := env.check("-retry-from", r1)
	if err != nil {
		t.Fatalf("2차 check -retry-from: %v\n%s", err, out2)
	}
	r2 := env.newDir(before)
	rtSameSnapshot(t, "2차 실행 후", snap1, rtSnapshot(t, r1)) // 이전 폴더 모든 파일 해시 동일

	// 이번 실행: 재시도 대상 4대만 (A·D·ghost 는 접속도 기록도 없음)
	cur2 := rtLines(t, filepath.Join(r2, "result.tsv"))[1:]
	if _, n := rtHostRows(t, "2차 result.tsv", cur2, 0); len(n) != 4 || n[inB] != nItems || n[inC] != nItems || n[hostG] != nItems || n[inE] != 1 {
		t.Errorf("2차 result.tsv 호스트별 행 수 = %v", n)
	}
	if srvA.Sessions().Created != 1 || srvD.Sessions().LoginFailed != 1 || srvD.Sessions().Created != 0 {
		t.Errorf("재시도 대상이 아닌 호스트에 접속함: A=%+v D=%+v", srvA.Sessions(), srvD.Sessions())
	}
	if srvB.Sessions().Created != 2 || srvC.Sessions().Created != 1 || srvG.Sessions().Created != 2 {
		t.Errorf("복구 호스트 세션: B=%+v C=%+v G=%+v (B·G 는 1차에서 로그인까지 갔으므로 2회, C 는 새 서버라 1회 기대)", srvB.Sessions(), srvC.Sessions(), srvG.Sessions())
	}

	// 병합본: 제자리 교체, 나머지 행 바이트 동일
	prev1 := rtLines(t, filepath.Join(r1, "result.tsv"))[1:]
	retried := map[string]bool{strings.ToLower(inB): true, strings.ToLower(inC): true, strings.ToLower(inE): true, strings.ToLower(hostG): true}
	mergedLines := rtLines(t, filepath.Join(r2, "merged_result.tsv"))
	if strings.Join(rtCol2(mergedLines[0]), "\t") != strings.Join(resultHeader, "\t") {
		t.Errorf("merged_result.tsv 머리글 = %q", mergedLines[0])
	}
	want := rtExpectMerged(prev1, cur2, 0, retried)
	if strings.Join(mergedLines[1:], "\n") != strings.Join(want, "\n") {
		t.Errorf("merged_result.tsv 가 기대와 다름:\n%s\n--- 기대\n%s", strings.Join(mergedLines[1:], "\n"), strings.Join(want, "\n"))
	}
	order, n := rtHostRows(t, "merged_result.tsv", mergedLines[1:], 0)
	if fmt.Sprint(order) != fmt.Sprint([]string{inA, inB, inC, inD, inE, hostG, "ghost"}) {
		t.Errorf("병합본 호스트 순서 = %v (이전 순서 유지 기대)", order)
	}
	for _, h := range []string{inA, inB, inC, hostG} {
		if n[h] != nItems {
			t.Errorf("병합본 %s 행 수 %d, 기대 %d", h, n[h], nItems)
		}
	}
	for _, h := range []string{inD, inE, "ghost"} {
		if n[h] != 1 {
			t.Errorf("병합본 %s 행 수 %d, 기대 1 (호스트 오류 1행)", h, n[h])
		}
	}
	// 재시도 대상이 아닌 호스트(A, D, ghost)의 행은 이전 result.tsv 의 줄과 한 글자도 다르지 않다
	for _, h := range []string{inA, inD, "ghost"} {
		var p, m []string
		for _, l := range prev1 {
			if rtCol(l, 0) == h {
				p = append(p, l)
			}
		}
		for _, l := range mergedLines[1:] {
			if rtCol(l, 0) == h {
				m = append(m, l)
			}
		}
		if strings.Join(p, "\n") != strings.Join(m, "\n") {
			t.Errorf("재시도 대상이 아닌 %s 의 행이 바뀜:\n%v\n%v", h, p, m)
		}
	}
	stm := rtStates(t, mergedLines[1:])
	if !rtAll(stm[inB], StatusOK) || !rtAll(stm[hostG], StatusOK) {
		t.Errorf("복구 호스트 B/G 행이 정상으로 교체되지 않음: B=%v G=%v", stm[inB], stm[hostG])
	}
	if got := checkRowsState(stm[inC]); got != StatusFail || len(stm[inC]) != nItems {
		t.Errorf("C 는 복구되어 FAIL 항목 1개를 가진 점검 결과여야 함: %v", stm[inC])
	}
	if !rtAll(stm[inE], StatusBMCError) || !rtAll(stm[inD], StatusAuthFail) {
		t.Errorf("E=%v D=%v", stm[inE], stm[inD])
	}
	// merged_ok / merged_retry / merged_run_info
	if got := rtLines(t, filepath.Join(r2, "merged_ok.txt")); fmt.Sprint(got) != fmt.Sprint([]string{inA, inB, hostG}) {
		t.Errorf("merged_ok.txt = %v, 기대 [A B G] (OK 호스트의 hostname)", got)
	}
	if got := rtLines(t, filepath.Join(r2, "merged_retry.txt")); fmt.Sprint(got) != fmt.Sprint([]string{inE}) {
		t.Errorf("merged_retry.txt = %v, 기대 [E] (여전히 실패한 것만, AUTH_FAIL 은 없음)", got)
	}
	if got := rtLines(t, filepath.Join(r2, "retry.txt")); fmt.Sprint(got) != fmt.Sprint([]string{inE}) {
		t.Errorf("2차 retry.txt = %v", got)
	}
	info := mustRead(t, filepath.Join(r2, "merged_run_info.txt"))
	for _, w := range []string{"재시도한 호스트: 4", "복구(호스트 오류 없이 점검 끝남): 3", "여전히 일시오류: 1", "다른 오류: 0", "병합본 호스트: 7", "OK: 3", "FAIL: 1", "BMC_ERROR: 1", "AUTH_FAIL: 1", "NO_HOSTS_ENTRY: 1"} {
		if !strings.Contains(info, w) {
			t.Errorf("merged_run_info.txt 에 %q 없음:\n%s", w, info)
		}
	}
	// 기존 산출물도 그대로 만들어진다 (이번 실행분)
	for _, f := range []string{"result.tsv", "ok.txt", "fail.tsv", "retry.txt", "run_info.txt", "merged_result.tsv", "merged_ok.txt", "merged_retry.txt", "merged_run_info.txt"} {
		fi, err := os.Stat(filepath.Join(r2, f))
		if err != nil {
			t.Errorf("%s 없음: %v", f, err)
			continue
		}
		if runtime.GOOS != "windows" && fi.Mode().Perm() != 0o600 {
			t.Errorf("%s 권한 %v, 기대 0600", f, fi.Mode().Perm())
		}
	}

	// 터미널: [재시도 병합] 블록이 맨 끝에 있고 숫자가 맞다
	for _, w := range []string{"[재시도 병합]", "재시도 4대 → 복구 3대 / 여전히 일시오류 1대 (→ merged_retry.txt) / 다른 오류 0대",
		"병합본 전체 7대: AUTH_FAIL 1, BMC_ERROR 1, FAIL 1, NO_HOSTS_ENTRY 1, OK 3", "merged_result.tsv"} {
		if !strings.Contains(out2, w) {
			t.Errorf("2차 출력에 %q 없음:\n%s", w, out2)
		}
	}
	tail := out2[strings.Index(out2, "[재시도 병합]"):]
	if strings.Count(out2, "[재시도 병합]") != 1 || strings.Count(tail, "\n") != 4 || !strings.Contains(out2[:strings.Index(out2, "[재시도 병합]")], "결과 파일:") {
		t.Errorf("[재시도 병합] 블록이 출력 맨 끝 4줄이 아님:\n%s", out2)
	}
	if strings.Contains(out1, "[재시도 병합]") {
		t.Error("일반 실행 출력에 [재시도 병합] 이 있음")
	}

	// ---- 2b: BOM + CRLF 인 이전 결과, retry.txt 파일 경로로 지정 → 같은 병합 결과여야 한다 ----
	copyDir := filepath.Join(env.dir, "r1copy")
	must(t, os.MkdirAll(copyDir, 0o700))
	for _, f := range []string{"result.tsv", "retry.txt"} {
		s := strings.ReplaceAll(mustRead(t, filepath.Join(r1, f)), "\n", "\r\n")
		must(t, os.WriteFile(filepath.Join(copyDir, f), []byte("\xef\xbb\xbf"+s), 0o600))
	}
	before = env.dirs()
	out2b, err := env.check("-retry-from", filepath.Join(copyDir, "retry.txt"))
	if err != nil {
		t.Fatalf("2b check: %v\n%s", err, out2b)
	}
	r2b := env.newDir(before)
	if a, b := mustRead(t, filepath.Join(r2, "merged_result.tsv")), mustRead(t, filepath.Join(r2b, "merged_result.tsv")); a != b {
		t.Errorf("BOM/CRLF 이전 결과 + retry.txt 파일 경로의 병합 결과가 다름:\n%s\n---\n%s", a, b)
	}
	if a, b := mustRead(t, filepath.Join(r2, "merged_retry.txt")), mustRead(t, filepath.Join(r2b, "merged_retry.txt")); a != b {
		t.Errorf("merged_retry.txt 가 다름: %q vs %q", a, b)
	}
	if srvA.Sessions().Created != 1 || srvD.Sessions().LoginFailed != 1 {
		t.Errorf("2b 에서 재시도 대상이 아닌 호스트에 접속함")
	}

	// ---- 3차: 2차 결과 폴더에서 이어서 (E 복구). 대상은 merged_retry.txt, 바탕은 merged_result.tsv ----
	srvE.ClearFaults()
	snap2 := rtSnapshot(t, r2)
	before = env.dirs()
	out3, err := env.check("-retry-from", r2)
	if err != nil {
		t.Fatalf("3차 check: %v\n%s", err, out3)
	}
	r3 := env.newDir(before)
	rtSameSnapshot(t, "3차 실행 후", snap2, rtSnapshot(t, r2))
	m3 := rtLines(t, filepath.Join(r3, "merged_result.tsv"))[1:]
	want3 := rtExpectMerged(mergedLines[1:], rtLines(t, filepath.Join(r3, "result.tsv"))[1:], 0, map[string]bool{strings.ToLower(inE): true})
	if strings.Join(m3, "\n") != strings.Join(want3, "\n") {
		t.Errorf("3차 병합 결과가 기대와 다름:\n%s\n--- 기대\n%s", strings.Join(m3, "\n"), strings.Join(want3, "\n"))
	}
	order3, n3 := rtHostRows(t, "3차 merged_result.tsv", m3, 0)
	if len(order3) != 7 || n3[inE] != nItems || !rtAll(rtStates(t, m3)[inE], StatusOK) {
		t.Errorf("3차 병합본: 호스트 %v E=%v", order3, rtStates(t, m3)[inE])
	}
	if got := rtLines(t, filepath.Join(r3, "merged_retry.txt")); len(got) != 0 {
		t.Errorf("3차 merged_retry.txt = %v, 기대 빈 파일", got)
	}
	if !strings.Contains(out3, "재시도 1대 → 복구 1대 / 여전히 일시오류 0대") || !strings.Contains(out3, "병합본 전체 7대") {
		t.Errorf("3차 출력:\n%s", out3)
	}
	// 3차 폴더에 retry.txt 는 비어 있으므로 다시 -retry-from 하면 "대상 없음" 으로 끝난다 (병합본 재시도 목록 기준)
	before = env.dirs()
	out4, err := env.check("-retry-from", r3)
	if err != nil || !strings.Contains(out4, "재시도할 대상이 없습니다") {
		t.Errorf("전부 복구된 폴더에서 -retry-from: err=%v out=%q", err, out4)
	}
	env.noNewDir(before)

	// 비밀번호·토큰은 어디에도 없다 (결과 파일·터미널 출력)
	for _, s := range []string{out1, out2, out2b, out3} {
		rtNoSecret(t, "터미널 출력", s)
	}
	for _, d := range []string{r1, r2, r2b, r3} {
		for name := range rtSnapshot(t, d) {
			rtNoSecret(t, d+"/"+name, mustRead(t, filepath.Join(d, name)))
		}
	}
}

func rtCol2(line string) []string { return strings.Split(line, "\t") }

func mustRead(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	must(t, err)
	return string(b)
}

func rtNoSecret(t *testing.T, what, s string) {
	t.Helper()
	for _, secret := range []string{testPass, "S3cr3t", "다른-비밀번호", "X-Auth-Token", "Authorization", "Basic "} {
		if strings.Contains(s, secret) {
			t.Errorf("%s 에 %q 가 노출됨", what, secret)
		}
	}
}

// ---- SKIPPED_AUTH_STOP 재시도 흐름 ----

func TestRetryCheckSkippedAuthStop(t *testing.T) {
	env := newRTEnv(t, 2, "")
	bad := func(o *mockbmc.Options) { o.Pass = "다른-비밀번호" }
	srvX1 := startMockTree(t, "dell-r660", bad)
	srvX2 := startMockTree(t, "dell-r660", bad)
	inX1, inX2 := strings.TrimPrefix(srvX1.URL(), "https://"), strings.TrimPrefix(srvX2.URL(), "https://")
	var goods []*mockbmc.Server
	var inGood []string
	for i := 0; i < 3; i++ {
		s, in := startAllSrv(t, "dell-r660", nil)
		goods = append(goods, s)
		inGood = append(inGood, in)
	}
	nItems := len(testProf(t).Items)
	user := env.writeFile("user.txt", append([]string{inX1, inX2}, inGood...)...)

	// 처음 2대가 모두 AUTH_FAIL → 차단기가 멈추고 나머지는 접속하지 않고 SKIPPED_AUTH_STOP
	before := env.dirs()
	out1, err := env.check("-user", user)
	if err != nil {
		t.Fatalf("1차: %v\n%s", err, out1)
	}
	r1 := env.newDir(before)
	st1 := rtStates(t, rtLines(t, filepath.Join(r1, "result.tsv"))[1:])
	if !rtAll(st1[inX1], StatusAuthFail) || !rtAll(st1[inX2], StatusAuthFail) {
		t.Fatalf("1차 AUTH_FAIL = %v %v", st1[inX1], st1[inX2])
	}
	for _, in := range inGood {
		if !rtAll(st1[in], StatusSkippedAuthStop) {
			t.Fatalf("1차 %s = %v, 기대 SKIPPED_AUTH_STOP", in, st1[in])
		}
	}
	if got := rtLines(t, filepath.Join(r1, "retry.txt")); fmt.Sprint(got) != fmt.Sprint(inGood) {
		t.Fatalf("1차 retry.txt = %v, 기대 SKIPPED 3대만 (AUTH_FAIL 호스트는 없음)", got)
	}
	for i, s := range goods {
		if s.Sessions().Created != 0 {
			t.Fatalf("차단기 뒤 호스트 %d 에 접속함", i)
		}
	}
	snap1 := rtSnapshot(t, r1)

	// 계정 문제가 해결됐다고 보고(SKIPPED 호스트는 원래 정상 계정) → 재시도
	before = env.dirs()
	out2, err := env.check("-retry-from", r1)
	if err != nil {
		t.Fatalf("2차: %v\n%s", err, out2)
	}
	r2 := env.newDir(before)
	rtSameSnapshot(t, "SKIPPED 재시도 후", snap1, rtSnapshot(t, r1))
	m := rtLines(t, filepath.Join(r2, "merged_result.tsv"))[1:]
	prev := rtLines(t, filepath.Join(r1, "result.tsv"))[1:]
	want := rtExpectMerged(prev, rtLines(t, filepath.Join(r2, "result.tsv"))[1:], 0, map[string]bool{inGood[0]: true, inGood[1]: true, inGood[2]: true})
	if strings.Join(m, "\n") != strings.Join(want, "\n") {
		t.Errorf("병합 결과:\n%s\n--- 기대\n%s", strings.Join(m, "\n"), strings.Join(want, "\n"))
	}
	stm := rtStates(t, m)
	for _, in := range inGood {
		if !rtAll(stm[in], StatusOK) || len(stm[in]) != nItems {
			t.Errorf("%s 가 정상 행으로 교체되지 않음: %v", in, stm[in])
		}
	}
	if !rtAll(stm[inX1], StatusAuthFail) || !rtAll(stm[inX2], StatusAuthFail) {
		t.Errorf("AUTH_FAIL 행이 유지되지 않음")
	}
	if got := rtLines(t, filepath.Join(r2, "merged_retry.txt")); len(got) != 0 {
		t.Errorf("merged_retry.txt = %v (AUTH_FAIL 은 재시도 대상이 아니므로 비어야 함)", got)
	}
	if srvX1.Sessions().LoginFailed != 1 || srvX2.Sessions().LoginFailed != 1 {
		t.Errorf("AUTH_FAIL 호스트에 다시 로그인 시도함: %+v %+v", srvX1.Sessions(), srvX2.Sessions())
	}
	if !strings.Contains(out2, "재시도 3대 → 복구 3대 / 여전히 일시오류 0대 (→ merged_retry.txt) / 다른 오류 0대") {
		t.Errorf("출력:\n%s", out2)
	}
}

// ---- 재시도 대상 없음 / 인자·이전 결과 오류: 아무것도 쓰지 않고, 접속하지 않는다 ----

func TestRetryCheckNoTargetsAndErrors(t *testing.T) {
	env := newRTEnv(t, 3, "")
	srv, in := startAllSrv(t, "dell-r660", nil)
	prevRows := []string{rtErrRow(in, StatusUnreachable)}
	notContacted := func(what string) {
		t.Helper()
		if ss := srv.Sessions(); ss.Created != 0 || ss.LoginFailed != 0 || srv.CallCount() != 0 {
			t.Errorf("%s: BMC 에 접속함 %+v 호출 %d", what, ss, srv.CallCount())
		}
	}

	t.Run("retry.txt 없음/비어 있음은 안내 후 종료코드 0, 쓰기 없음", func(t *testing.T) {
		for name, retry := range map[string][]string{"없음": nil, "빈 파일": {}, "공백과 주석뿐": {"", "  ", "# 주석"}} {
			prev := rtWritePrev(t, filepath.Join(env.dir, "prev-"+name), resultHeader, prevRows, retry)
			if name == "빈 파일" {
				must(t, os.WriteFile(filepath.Join(prev, "retry.txt"), nil, 0o600))
			}
			snap := rtSnapshot(t, prev)
			before := env.dirs()
			out, err := env.check("-retry-from", prev)
			if err != nil {
				t.Errorf("%s: 종료코드 0(오류 없음) 기대, err=%v", name, err)
			}
			if !strings.Contains(out, "재시도할 대상이 없습니다") {
				t.Errorf("%s: 안내 없음: %q", name, out)
			}
			env.noNewDir(before)
			rtSameSnapshot(t, name, snap, rtSnapshot(t, prev))
			// allcheck 도 같다
			out, err = env.allcheck("-diff", env.writeFile("diff.txt", in), "-retry-from", prev)
			if err != nil || !strings.Contains(out, "재시도할 대상이 없습니다") {
				t.Errorf("%s allcheck: err=%v out=%q", name, err, out)
			}
			env.noNewDir(before)
		}
	})

	t.Run("없는 폴더는 오류(종료코드 1)", func(t *testing.T) {
		before := env.dirs()
		if _, err := env.check("-retry-from", filepath.Join(env.dir, "없는폴더")); err == nil {
			t.Error("없는 폴더인데 오류가 아님")
		}
		env.noNewDir(before)
	})

	t.Run("-user 와 함께 쓰면 오류", func(t *testing.T) {
		prev := rtWritePrev(t, filepath.Join(env.dir, "prev-user"), resultHeader, prevRows, []string{in})
		user := env.writeFile("user.txt", in)
		for _, form := range [][]string{{"-user", user}, {"-user=" + user}, {"--user", user}, {"--user=" + user}} {
			before := env.dirs()
			_, err := env.check(append([]string{"-retry-from", prev}, form...)...)
			if err == nil || !strings.Contains(err.Error(), "함께 쓸 수 없습니다") {
				t.Errorf("%v: err = %v", form, err)
			}
			env.noNewDir(before)
			_, err = env.allcheck(append([]string{"-diff", user, "-retry-from", prev}, form...)...)
			if err == nil || !strings.Contains(err.Error(), "함께 쓸 수 없습니다") {
				t.Errorf("allcheck %v: err = %v", form, err)
			}
		}
		// 비어 있는 retry.txt 여도 -user 충돌이 먼저다
		empty := rtWritePrev(t, filepath.Join(env.dir, "prev-user2"), resultHeader, prevRows, []string{})
		if _, err := env.check("-retry-from", empty, "-user", user); err == nil {
			t.Error("빈 retry.txt + -user 가 오류가 아님")
		}
		notContacted("-user 충돌")
	})

	t.Run("이전 결과가 잘못되면 접속 전에 오류", func(t *testing.T) {
		short := resultHeader[:len(resultHeader)-1] // 열 수 불일치
		renamed := append([]string(nil), resultHeader...)
		renamed[3] = "vendr"
		good := rtErrRow(in, StatusUnreachable)
		cases := []struct {
			name, want string
			header     []string
			rows       []string
			extra      []string
		}{
			{"머리글 열 수 불일치", "열 수", short, []string{good}, nil},
			{"머리글 열 이름 불일치", "vendr", renamed, []string{good}, nil},
			{"데이터 행 열 수 불일치", "열 수", resultHeader, []string{good, "a\tb\tc"}, nil},
			{"프로파일 다름", "프로파일", resultHeader, []string{good}, []string{"-profile", "OTHER"}},
		}
		for _, c := range cases {
			prev := rtWritePrev(t, filepath.Join(env.dir, "prev-bad-"+c.name), c.header, c.rows, []string{in})
			snap := rtSnapshot(t, prev)
			before := env.dirs()
			args := []string{"-retry-from", prev}
			if c.extra != nil {
				// env.check 가 앞에 -profile VM 을 두므로 뒤의 -profile 이 이긴다
				args = append(args, c.extra...)
			}
			_, err := env.check(args...)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Errorf("%s: err = %v, 기대 %q 포함", c.name, err, c.want)
			}
			env.noNewDir(before)
			rtSameSnapshot(t, c.name, snap, rtSnapshot(t, prev))
		}
		// result.tsv 가 아예 없는 폴더(retry.txt 만 있음)
		only := filepath.Join(env.dir, "prev-only-retry")
		must(t, os.MkdirAll(only, 0o700))
		must(t, os.WriteFile(filepath.Join(only, "retry.txt"), []byte(in+"\n"), 0o600))
		before := env.dirs()
		if _, err := env.check("-retry-from", filepath.Join(only, "retry.txt")); err == nil || !strings.Contains(err.Error(), "result.tsv") {
			t.Errorf("result.tsv 없음: err = %v", err)
		}
		env.noNewDir(before)
		notContacted("이전 결과 오류")
	})
}

// ---- allcheck ----

func TestRetryAllCheckMerge(t *testing.T) {
	env := newRTEnv(t, 3, "")
	fault503 := func(s *mockbmc.Server) { s.SetFault("/redfish/v1/Systems*", mockbmc.Fault{Status: 503}) }
	diffAttr := func(s *mockbmc.Server) { setAttrs(t, s, map[string]interface{}{"ProcTurbo": "Disabled"}) }
	both := func(a, b func(s *mockbmc.Server)) func(s *mockbmc.Server) {
		return func(s *mockbmc.Server) { a(s); b(s) }
	}
	_, inRef := startAllSrv(t, "hpe-dl360gen11", nil)
	srvT1, inT1 := startAllSrv(t, "hpe-dl360gen11", diffAttr)                 // 처음부터 정상 (DIFF 1건)
	srvT2, inT2 := startAllSrv(t, "hpe-dl360gen11", both(diffAttr, fault503)) // 503 → 복구
	deadSrv, inT3 := startAllSrv(t, "hpe-dl360gen11", diffAttr)               // 닫힘 → 복구
	deadSrv.Close()
	srvT4 := startMockTree(t, "hpe-dl360gen11", func(o *mockbmc.Options) { o.Pass = "다른-비밀번호" }) // AUTH_FAIL
	inT4 := strings.TrimPrefix(srvT4.URL(), "https://")
	srvT5, inT5 := startAllSrv(t, "hpe-dl360gen10", nil)                      // 기준 없는 모델 → NO_REFERENCE
	srvT6, inT6 := startAllSrv(t, "hpe-dl360gen11", both(diffAttr, fault503)) // 계속 503
	diff := env.writeFile("diff.txt", inRef)
	user := env.writeFile("user.txt", inT1, inT2, inT3, inT4, inT5, inT6)

	before := env.dirs()
	out1, err := env.allcheck("-diff", diff, "-user", user)
	if err != nil {
		t.Fatalf("1차 allcheck: %v\n%s", err, out1)
	}
	r1 := env.newDir(before)
	sum1 := rtLines(t, filepath.Join(r1, "summary.tsv"))
	status := func(lines []string) map[string]string {
		m := map[string]string{}
		for _, l := range lines {
			m[rtCol(l, 0)] = rtCol(l, 11)
		}
		return m
	}
	st1 := status(sum1[1:])
	for h, w := range map[string]string{inT1: StatusDiff, inT2: StatusBMCError, inT3: StatusUnreachable, inT4: StatusAuthFail, inT5: StatusNoReference, inT6: StatusBMCError} {
		if st1[h] != w {
			t.Errorf("1차 %s = %s, 기대 %s", h, st1[h], w)
		}
	}
	if got := rtLines(t, filepath.Join(r1, "retry.txt")); fmt.Sprint(got) != fmt.Sprint([]string{inT2, inT3, inT6}) {
		t.Fatalf("1차 retry.txt = %v, 기대 [T2 T3 T6] (AUTH_FAIL·NO_REFERENCE 제외)", got)
	}

	// 장애 해소: T2 복구, T3 는 같은 주소에 새 mock, T6 은 계속 503
	srvT2.ClearFaults()
	srvT3 := rtRestart(t, "hpe-dl360gen11", inT3, diffAttr)
	snap1 := rtSnapshot(t, r1)
	t1Sessions, t4Sessions, t5Sessions := srvT1.Sessions(), srvT4.Sessions(), srvT5.Sessions()
	t2Created, t6Created := srvT2.Sessions().Created, srvT6.Sessions().Created // 1차에서 로그인까지는 갔다 (503 은 Systems 부터)

	before = env.dirs()
	out2, err := env.allcheck("-diff", diff, "-retry-from", r1)
	if err != nil {
		t.Fatalf("2차 allcheck -retry-from: %v\n%s", err, out2)
	}
	r2 := env.newDir(before)
	rtSameSnapshot(t, "allcheck 재시도 후", snap1, rtSnapshot(t, r1))

	// 재시도 대상이 아닌 호스트는 다시 읽지 않는다
	if srvT1.Sessions() != t1Sessions || srvT4.Sessions() != t4Sessions || srvT5.Sessions() != t5Sessions {
		t.Errorf("재시도 대상이 아닌 호스트에 접속함")
	}
	if srvT3.Sessions().Created != 1 || srvT2.Sessions().Created != t2Created+1 || srvT6.Sessions().Created != t6Created+1 {
		t.Errorf("재시도 호스트 세션: T2=%+v T3=%+v T6=%+v", srvT2.Sessions(), srvT3.Sessions(), srvT6.Sessions())
	}

	retried := map[string]bool{inT2: true, inT3: true, inT6: true}
	// merged_summary: 제자리 교체 + 호스트당 1행
	prevSum, curSum := sum1[1:], rtLines(t, filepath.Join(r2, "summary.tsv"))[1:]
	mSum := rtLines(t, filepath.Join(r2, "merged_summary.tsv"))
	if strings.Join(rtCol2(mSum[0]), "\t") != strings.Join(summaryHeader, "\t") {
		t.Errorf("merged_summary.tsv 머리글 = %q", mSum[0])
	}
	wantSum := rtExpectMerged(prevSum, curSum, 0, retried)
	if strings.Join(mSum[1:], "\n") != strings.Join(wantSum, "\n") {
		t.Errorf("merged_summary.tsv:\n%s\n--- 기대\n%s", strings.Join(mSum[1:], "\n"), strings.Join(wantSum, "\n"))
	}
	order, n := rtHostRows(t, "merged_summary.tsv", mSum[1:], 0)
	if fmt.Sprint(order) != fmt.Sprint([]string{inT1, inT2, inT3, inT4, inT5, inT6}) {
		t.Errorf("merged_summary 호스트 순서 = %v", order)
	}
	for h, c := range n {
		if c != 1 {
			t.Errorf("merged_summary 에서 %s 가 %d행 (호스트당 1행 기대)", h, c)
		}
	}
	stm := status(mSum[1:])
	for h, w := range map[string]string{inT1: StatusDiff, inT2: StatusDiff, inT3: StatusDiff, inT4: StatusAuthFail, inT5: StatusNoReference, inT6: StatusBMCError} {
		if stm[h] != w {
			t.Errorf("병합 %s = %s, 기대 %s", h, stm[h], w)
		}
	}
	// 재시도 대상이 아닌 호스트(T1, T4, T5) 줄은 그대로
	for _, h := range []string{inT1, inT4, inT5} {
		for _, l := range prevSum {
			if rtCol(l, 0) == h && !containsLine(mSum[1:], l) {
				t.Errorf("재시도 대상이 아닌 %s 의 summary 줄이 바뀜: %q", h, l)
			}
		}
	}
	// merged_all_diff: T1 줄 그대로 + 복구된 T2·T3 의 차이 행 (T6 은 아직 없음)
	prevDiff, curDiff := rtLines(t, filepath.Join(r1, "all_diff.tsv"))[1:], rtLines(t, filepath.Join(r2, "all_diff.tsv"))[1:]
	mDiff := rtLines(t, filepath.Join(r2, "merged_all_diff.tsv"))
	if strings.Join(rtCol2(mDiff[0]), "\t") != strings.Join(allDiffHeader, "\t") {
		t.Errorf("merged_all_diff.tsv 머리글 = %q", mDiff[0])
	}
	wantDiff := rtExpectMerged(prevDiff, curDiff, 2, retried)
	if strings.Join(mDiff[1:], "\n") != strings.Join(wantDiff, "\n") {
		t.Errorf("merged_all_diff.tsv:\n%s\n--- 기대\n%s", strings.Join(mDiff[1:], "\n"), strings.Join(wantDiff, "\n"))
	}
	_, dn := rtHostRows(t, "merged_all_diff.tsv", mDiff[1:], 2)
	if len(dn) != 3 || dn[inT1] != 1 || dn[inT2] != 1 || dn[inT3] != 1 {
		t.Errorf("merged_all_diff 호스트별 행 = %v, 기대 T1·T2·T3 각 1", dn)
	}
	if len(prevDiff) != 1 || mDiff[1] != prevDiff[0] {
		t.Errorf("T1 의 all_diff 줄이 그대로 앞에 있어야 함: prev=%v merged=%v", prevDiff, mDiff[1:])
	}
	if got := rtLines(t, filepath.Join(r2, "merged_retry.txt")); fmt.Sprint(got) != fmt.Sprint([]string{inT6}) {
		t.Errorf("merged_retry.txt = %v, 기대 [T6]", got)
	}
	for _, f := range []string{"summary.tsv", "all_diff.tsv", "retry.txt", "run_info.txt", "merged_summary.tsv", "merged_all_diff.tsv", "merged_retry.txt", "merged_run_info.txt"} {
		fi, err := os.Stat(filepath.Join(r2, f))
		if err != nil {
			t.Errorf("%s 없음: %v", f, err)
		} else if runtime.GOOS != "windows" && fi.Mode().Perm() != 0o600 {
			t.Errorf("%s 권한 %v, 기대 0600", f, fi.Mode().Perm())
		}
	}
	for _, w := range []string{"[재시도 병합]", "재시도 3대 → 복구 2대 / 여전히 일시오류 1대 (→ merged_retry.txt) / 다른 오류 0대",
		"병합본 전체 6대: AUTH_FAIL 1, BMC_ERROR 1, DIFF 3, NO_REFERENCE 1"} {
		if !strings.Contains(out2, w) {
			t.Errorf("출력에 %q 없음:\n%s", w, out2)
		}
	}
	if !strings.HasSuffix(strings.TrimRight(out2, "\n"), filepath.ToSlash(r2)+"/ (merged_summary.tsv · merged_all_diff.tsv · merged_retry.txt · merged_run_info.txt)") {
		t.Errorf("[재시도 병합] 블록이 출력 맨 끝이 아님:\n%s", out2)
	}
	// 3차: 병합 폴더에서 이어서 (T6 복구) — 바탕은 merged_*, 대상은 merged_retry.txt
	srvT6.ClearFaults()
	snap2 := rtSnapshot(t, r2)
	before = env.dirs()
	out3, err := env.allcheck("-diff", diff, "-retry-from", r2)
	if err != nil {
		t.Fatalf("3차: %v\n%s", err, out3)
	}
	r3 := env.newDir(before)
	rtSameSnapshot(t, "allcheck 3차 후", snap2, rtSnapshot(t, r2))
	mSum3 := rtLines(t, filepath.Join(r3, "merged_summary.tsv"))[1:]
	wantSum3 := rtExpectMerged(mSum[1:], rtLines(t, filepath.Join(r3, "summary.tsv"))[1:], 0, map[string]bool{inT6: true})
	if strings.Join(mSum3, "\n") != strings.Join(wantSum3, "\n") || status(mSum3)[inT6] != StatusDiff || len(mSum3) != 6 {
		t.Errorf("3차 merged_summary.tsv:\n%s", strings.Join(mSum3, "\n"))
	}
	if _, dn3 := rtHostRows(t, "3차 merged_all_diff", rtLines(t, filepath.Join(r3, "merged_all_diff.tsv"))[1:], 2); len(dn3) != 4 {
		t.Errorf("3차 merged_all_diff 호스트별 = %v, 기대 T1·T2·T3·T6", dn3)
	}
	if got := rtLines(t, filepath.Join(r3, "merged_retry.txt")); len(got) != 0 {
		t.Errorf("3차 merged_retry.txt = %v", got)
	}
	for _, s := range []string{out1, out2, out3} {
		rtNoSecret(t, "allcheck 출력", s)
	}
	for _, d := range []string{r1, r2, r3} {
		for name := range rtSnapshot(t, d) {
			rtNoSecret(t, d+"/"+name, mustRead(t, filepath.Join(d, name)))
		}
	}
}

func containsLine(lines []string, l string) bool {
	for _, x := range lines {
		if x == l {
			return true
		}
	}
	return false
}

// ---- 단위: TSV 읽기·행 병합 ----

func TestReadTSVTable(t *testing.T) {
	dir := t.TempDir()
	hdr := []string{"a", "b", "c"}
	write := func(name, s string) string {
		p := filepath.Join(dir, name)
		must(t, os.WriteFile(p, []byte(s), 0o600))
		return p
	}
	// BOM + CRLF + 빈 줄은 무시
	tb, err := readTSVTable(write("ok", "\xef\xbb\xbfa\tb\tc\r\n1\t2\t3\r\n\r\n4\t-\t6\r\n"), hdr)
	if err != nil || len(tb.Rows) != 2 || strings.Join(tb.Rows[1], "|") != "4|-|6" {
		t.Errorf("정상 파일: %+v err=%v", tb, err)
	}
	if tb, err = readTSVTable(write("headonly", "a\tb\tc\n"), hdr); err != nil || len(tb.Rows) != 0 {
		t.Errorf("머리글만 있는 파일: %+v err=%v", tb, err)
	}
	for name, c := range map[string]struct{ text, want string }{
		"empty":    {"", "비어"},
		"blanks":   {"\n\r\n", "비어"},
		"hdrcols":  {"a\tb\n1\t2\n", "열 수"},
		"hdrname":  {"a\tb\tx\n1\t2\t3\n", "x"},
		"rowshort": {"a\tb\tc\n1\t2\n", "2번째 줄"},
		"rowlong":  {"a\tb\tc\n1\t2\t3\n4\t5\t6\t7\n", "3번째 줄"},
	} {
		if _, err := readTSVTable(write(name, c.text), hdr); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err = %v, 기대 %q 포함", name, err, c.want)
		}
	}
	if _, err := readTSVTable(filepath.Join(dir, "없음"), hdr); !os.IsNotExist(err) {
		t.Errorf("없는 파일: err = %v", err)
	}
}

func TestMergeRowsByHost(t *testing.T) {
	row := func(h, v string) []string { return []string{h, v} }
	join := func(rows [][]string) string {
		var s []string
		for _, r := range rows {
			s = append(s, r[0]+"="+r[1])
		}
		return strings.Join(s, " ")
	}
	prev := [][]string{row("a", "1"), row("B", "old1"), row("B", "old2"), row("c", "3"), row("d", "4")}
	cases := []struct {
		name    string
		cur     [][]string
		retried map[string]bool
		want    string
	}{
		{"제자리 교체: 대소문자 무시, 이전 2행 → 새 3행", [][]string{row("b", "n1"), row("b", "n2"), row("b", "n3")}, map[string]bool{"b": true}, "a=1 b=n1 b=n2 b=n3 c=3 d=4"},
		{"이전에 없던 호스트의 새 행은 끝에", [][]string{row("e", "5")}, map[string]bool{"e": true}, "a=1 B=old1 B=old2 c=3 d=4 e=5"},
		{"새 행이 없는 재시도 호스트의 이전 행은 사라짐 (all_diff 의 차이 해소)", nil, map[string]bool{"c": true}, "a=1 B=old1 B=old2 d=4"},
		{"여러 호스트 동시", [][]string{row("a", "x"), row("d", "y")}, map[string]bool{"a": true, "d": true}, "a=x B=old1 B=old2 c=3 d=y"},
		{"재시도 목록에 없어도 cur 에 나온 호스트는 교체 대상", [][]string{row("c", "z")}, map[string]bool{}, "a=1 B=old1 B=old2 c=z d=4"},
		{"재시도 없음", nil, map[string]bool{}, "a=1 B=old1 B=old2 c=3 d=4"},
	}
	for _, c := range cases {
		if got := join(mergeRowsByHost(prev, c.cur, 0, c.retried)); got != c.want {
			t.Errorf("%s:\n got  %s\n want %s", c.name, got, c.want)
		}
	}
	if join(prev) != "a=1 B=old1 B=old2 c=3 d=4" {
		t.Error("mergeRowsByHost 가 입력(prev)을 바꿈")
	}
}

func TestCheckRowsState(t *testing.T) {
	for _, c := range []struct {
		in   []string
		want string
	}{
		{[]string{"OK", "OK"}, StatusOK},
		{[]string{"OK", "PENDING_OK"}, StatusPendingOK},
		{[]string{"OK", "UNVERIFIED", "FAIL"}, StatusFail},
		{[]string{"OK", "MAPPING_MISSING", "UNVERIFIED"}, StatusUnverified},
		{[]string{"UNREACHABLE"}, StatusUnreachable},
		{[]string{"AUTH_FAIL"}, StatusAuthFail},
		{[]string{"SKIPPED_AUTH_STOP"}, StatusSkippedAuthStop},
		{[]string{"NO_HOSTS_ENTRY"}, StatusNoHostsEntry},
	} {
		if got := checkRowsState(c.in); got != c.want {
			t.Errorf("checkRowsState(%v) = %s, 기대 %s", c.in, got, c.want)
		}
	}
}

// dump 는 -retry-from 을 지원하지 않는다 (공통 플래그라 조용히 무시되면 안 됨).
func TestDumpRejectsRetryFrom(t *testing.T) {
	if err := cmdDump([]string{"-retry-from", "results/x"}); err == nil || !strings.Contains(err.Error(), "check, allcheck") {
		t.Errorf("err = %v", err)
	}
}
