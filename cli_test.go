package main

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestParseArgsU(t *testing.T) {
	cases := []struct {
		args       []string
		user, file string
		typ        string
		bad        bool
	}{
		{[]string{"-u", "test.kim", "a.txt"}, "test.kim", "a.txt", "txt", false},
		{[]string{"-u", "test.kim", "a.csv", "-csv"}, "test.kim", "a.csv", "csv", false},
		{[]string{"-u=bob", "a.csv", "-csv"}, "bob", "a.csv", "csv", false},
		{[]string{"--user", "bob", "a.txt"}, "bob", "a.txt", "txt", false},
		{[]string{"-u", "bob"}, "bob", "", "", false},
		{[]string{}, "", "", "", false},
		{[]string{"-u"}, "", "", "", true},
		{[]string{"-x"}, "", "", "", true},
		{[]string{"-u", "bob", "a.txt", "-csv", "more"}, "", "", "", true},
		{[]string{"-u", "bob", "-csv", "a.txt"}, "", "", "", true},
		{[]string{"-u", "bob", "a.txt", "xml"}, "", "", "", true},
	}
	for _, c := range cases {
		o, err := parseArgs(c.args)
		var file, typ string
		if err == nil {
			file, typ, err = uploadArgs(o.positional)
		}
		if c.bad {
			if err == nil {
				t.Errorf("%v: 오류여야 합니다", c.args)
			}
			continue
		}
		if err != nil || o.userName != c.user || file != c.file || typ != c.typ {
			t.Errorf("%v: user=%q file=%q typ=%q err=%v", c.args, o.userName, file, typ, err)
		}
	}
	if o, _ := parseArgs([]string{"--status", "--config", "x.conf"}); !o.status || o.cfgPath != "x.conf" {
		t.Error("v1 옵션 유지 실패")
	}
}

// 리눅스 규칙: OS 사용자 자동 선택 없음, 새 설정 만들기 없음, root.conf 제외, TUI 선택.
func linuxSel(t *testing.T, tty bool) (*selector, *bytes.Buffer) {
	t.Helper()
	var out, errb bytes.Buffer
	return &selector{
		Dir: filepath.Join(t.TempDir(), "etc"), OSUser: "alice", IsTTY: tty,
		In: bufio.NewReader(strings.NewReader("")), Out: &out, Err: &errb,
	}, &errb
}

func TestLinuxSelectorRules(t *testing.T) {
	s, errb := linuxSel(t, false)
	writeConf(t, s.Dir, "alice.conf", "alice", "7")
	writeConf(t, s.Dir, "bob.conf", "bob", "8")
	writeConf(t, s.Dir, rootConfName, "root", "0")
	if _, _, err := s.resolveConfig("", ""); err == nil {
		t.Fatal("OS 사용자 이름 자동 선택은 없어야 합니다")
	}
	if strings.Contains(errb.String(), "SECRET-PW") || strings.Contains(errb.String(), rootConfName) {
		t.Fatalf("목록에 비밀번호 또는 root.conf: %s", errb.String())
	}
	if cfg, _, err := s.resolveConfig("", "bob"); err != nil || cfg.InventoryID != 8 || confUser(cfg) != "bob" {
		t.Fatalf("-u bob: %v %+v", err, cfg)
	}
	// TUI 선택
	var shown []string
	s.IsTTY = true
	s.Choose = func(title string, items []string) (int, error) { shown = items; return 1, nil }
	cfg, _, err := s.resolveConfig("", "")
	if err != nil || cfg.Username != "bob" {
		t.Fatalf("선택: %v %+v", err, cfg)
	}
	if len(shown) != 2 || !strings.Contains(shown[0], "alice") || !strings.Contains(shown[0], "inventory_id=7") || !strings.Contains(shown[0], "http://x:1") {
		t.Fatalf("목록 내용: %q", shown)
	}
	for _, l := range shown {
		if strings.Contains(l, "SECRET-PW") {
			t.Fatal("비밀번호 표시")
		}
	}
	s.Choose = func(string, []string) (int, error) { return -1, nil }
	if _, _, err := s.resolveConfig("", ""); err == nil {
		t.Fatal("취소는 오류여야 합니다")
	}
}

func TestConfPermission(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("리눅스 규칙")
	}
	p := writeConf(t, t.TempDir(), "u.conf", "u", "5")
	if _, err := loadConfig(p); err != nil {
		t.Fatalf("600 은 허용: %v", err)
	}
	for _, m := range []os.FileMode{0o644, 0o640, 0o400, 0o700} {
		_ = os.Chmod(p, m)
		if _, err := loadConfig(p); err == nil || !strings.Contains(err.Error(), "600") {
			t.Errorf("%o 는 거부해야 합니다: %v", m, err)
		}
	}
}

func TestRootPassword(t *testing.T) {
	h := hashPassword("11111111!")
	if !checkRootPassword("11111111!", h) || !checkRootPassword("11111111!", strings.ToUpper(h)) {
		t.Fatal("맞는 비밀번호")
	}
	for _, w := range []string{"", "11111111", "11111111!!", "x"} {
		if checkRootPassword(w, h) {
			t.Fatalf("틀린 비밀번호 %q 통과", w)
		}
	}
	if checkRootPassword("11111111!", "zz") || checkRootPassword("11111111!", h[:10]) {
		t.Fatal("잘못된 해시 통과")
	}
	read := func(in *bufio.Reader, _ io.Writer) (string, error) {
		l, err := in.ReadString('\n')
		return strings.TrimRight(l, "\n"), err
	}
	var out bytes.Buffer
	if err := rootLogin(bufio.NewReader(strings.NewReader("a\nb\nc\n11111111!\n")), &out, read, h); err != errRootAuth {
		t.Fatalf("3회 실패여야 합니다: %v", err)
	}
	out.Reset()
	if err := rootLogin(bufio.NewReader(strings.NewReader("bad\n11111111!\n")), &out, read, h); err != nil {
		t.Fatalf("2번째에 성공해야 합니다: %v", err)
	}
	if strings.Contains(out.String(), "11111111") || strings.Contains(out.String(), "bad") {
		t.Fatal("비밀번호가 출력됐습니다")
	}
	// root.conf 읽기 (소유자 검사는 리눅스 root 만 통과)
	d := t.TempDir()
	rp := filepath.Join(d, rootConfName)
	if err := writeRootHash(rp, "11111111!"); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(rp); strings.Contains(string(b), "11111111!") {
		t.Fatal("평문 저장")
	}
	got, err := readRootHash(rp)
	if runtime.GOOS == "windows" || euid() == 0 {
		if err != nil || got != h {
			t.Fatalf("root.conf 읽기: %v %q", err, got)
		}
	} else if err == nil {
		t.Fatal("root 소유가 아니면 거부해야 합니다")
	}
}

func TestHistoryWriteMerge(t *testing.T) {
	d := filepath.Join(t.TempDir(), ".awx_copy")
	recs := []histRec{
		{TS: "2026-11-17T10:00:00+09:00", User: "alice", File: "/a/1.txt", Type: "txt", Size: 10, Chunks: 1, Result: resultSuccess},
		{TS: "2026-11-17T11:00:00+09:00", User: "bob", File: "/b/2.csv", Type: "csv", Size: 2048, Chunks: 2, Result: resultFailed, Message: "다운로더 수신 실패"},
		{TS: "2026-11-17T12:00:00+09:00", User: "alice", File: "/a/3.txt", Type: "txt", Size: 5, Chunks: 1, Result: resultAborted},
	}
	for _, r := range recs {
		if err := appendHistory(d, r); err != nil {
			t.Fatal(err)
		}
	}
	a, err := loadHistory(d, "alice")
	if err != nil || len(a) != 2 || a[0].File != "/a/3.txt" {
		t.Fatalf("alice 기록: %v %+v", err, a)
	}
	all, bad := loadAllHistory(d)
	if bad != 0 || len(all) != 3 || all[0].User != "alice" || all[1].User != "bob" {
		t.Fatalf("병합: %+v", all)
	}
	header, rows := historyRows(all, true)
	if !strings.Contains(header, "사용자") || !strings.Contains(rows[1], "bob") || !strings.Contains(rows[1], "실패") {
		t.Fatalf("표: %s\n%v", header, rows)
	}
	if h, _ := historyRows(a, false); strings.Contains(h, "사용자") {
		t.Fatal("사용자 열은 root 화면만")
	}
	// 깨진 줄은 건너뛴다
	f, _ := os.OpenFile(histFile(d, "alice"), os.O_APPEND|os.O_WRONLY, 0)
	_, _ = f.WriteString("{깨짐\n")
	f.Close()
	if a, _ := loadHistory(d, "alice"); len(a) != 2 {
		t.Fatal("깨진 줄 처리")
	}
	if runtime.GOOS != "windows" {
		st, _ := os.Stat(histFile(d, "alice"))
		if st.Mode().Perm() != 0o600 {
			t.Fatalf("기록 파일 권한 %o", st.Mode().Perm())
		}
		dst, _ := os.Stat(d)
		if dst.Mode()&os.ModeSticky == 0 || dst.Mode().Perm() != 0o777 {
			t.Fatalf("기록 폴더 권한 %v", dst.Mode())
		}
		// 심볼릭 링크에는 쓰지 않는다
		target := filepath.Join(t.TempDir(), "victim")
		_ = os.WriteFile(target, []byte("x"), 0o600)
		_ = os.Symlink(target, histFile(d, "eve"))
		if err := appendHistory(d, histRec{TS: "x", User: "eve"}); err == nil {
			t.Fatal("심볼릭 링크 기록 파일은 거부해야 합니다")
		}
		if b, _ := os.ReadFile(target); string(b) != "x" {
			t.Fatal("링크 대상이 바뀌었습니다")
		}
	}
}

// TestUploadRecordsReceiverResult 는 수신 결과가 업로더 결과와 기록에 반영되는지 본다.
func TestUploadRecordsReceiverResult(t *testing.T) {
	for _, c := range []struct {
		name string
		data string
		want string
	}{
		{"ok.txt", "a\tb  \r\nc\r\n", resultSuccess},
		{"bad.txt", "a\x01b\n", resultFailed},
	} {
		t.Run(c.name, func(t *testing.T) {
			fs, cfg := setup(t, 4096)
			fs.TrimRight = true
			fs.SetVars(5, "orig: 1")
			cfg.Path = filepath.Join(t.TempDir(), "kim.conf")
			src := filepath.Join(t.TempDir(), c.name)
			_ = os.WriteFile(src, []byte(c.data), 0o600)
			var wg sync.WaitGroup
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, _ = fetchFile(context.Background(), newClient(cfg), cfg, t.TempDir(), &bytes.Buffer{})
			}()
			var out bytes.Buffer
			rec, err := doUpload(context.Background(), newClient(cfg), cfg, confUser(cfg), src, "txt", &out, false)
			wg.Wait()
			if rec.Result != c.want || (c.want == resultSuccess) != (err == nil) {
				t.Fatalf("기록 %+v, err=%v", rec, err)
			}
			if rec.User != "kim" || rec.Chunks != 1 || rec.Size != int64(len(c.data)) {
				t.Fatalf("기록 값: %+v", rec)
			}
			if fs.Vars(5) != "orig: 1" {
				t.Fatalf("원복 안 됨: %q", fs.Vars(5))
			}
			// 비터미널 폴백: 한 줄 로그, ANSI 없음
			if strings.Contains(out.String(), "\x1b[") || !strings.Contains(out.String(), "[1/1] 전송 중") {
				t.Fatalf("비터미널 출력: %q", out.String())
			}
		})
	}
}

// TestFetcherFallbackRestore 는 다운로더가 결과를 남겼는데 업로더가 사라지면 다운로더가 원복하는지 본다.
func TestFetcherFallbackRestore(t *testing.T) {
	fs, cfg := setup(t, 4096)
	cfg.Timeout = 1500 * time.Millisecond
	fs.SetVars(5, "orig: 9")
	data := []byte("hello\n")
	b := Block{"version": protoVersion, "session": "s1", "state": stateSending, "file_name": "h.txt", "type": "txt",
		"encoding": encText, "eol": eolLF, "final_newline": "true", "total_bytes": "6", "sha256": sha256Hex(data),
		"chunk_total": "1", "chunk_index": "1", "chunk_sha256": sha256Hex(data), "chunk_text": "hello\n", "ack": "0",
		"backup_b64": "b3JpZzogOQ==", "updated_at": nowStamp()}
	fs.SetVars(5, b.Render()) // 업로더는 이 블록을 쓰고 죽었다
	saved, err := fetchFile(context.Background(), newClient(cfg), cfg, t.TempDir(), &bytes.Buffer{})
	if err != nil || saved == "" {
		t.Fatal(err)
	}
	if fs.Vars(5) != "orig: 9" {
		t.Fatalf("다운로더가 원복해야 합니다: %q", fs.Vars(5))
	}
}

func TestTUIFallbackAndKeys(t *testing.T) {
	var out bytes.Buffer
	showTable(&out, false, "제목", "머리", []string{"줄1", "줄2"})
	if strings.Contains(out.String(), "\x1b[") || !strings.Contains(out.String(), "줄2") {
		t.Fatalf("비터미널 표: %q", out.String())
	}
	keys := bufio.NewReader(strings.NewReader("\x1b[B\x1b[B\x1b[A\r"))
	n, err := chooseLoop(keys, &out, "t", []string{"a", "b", "c"}, 24, 80)
	if err != nil || n != 1 {
		t.Fatalf("화살표 선택: %d %v", n, err)
	}
	if n, _ := chooseLoop(bufio.NewReader(strings.NewReader("3")), &out, "t", []string{"a", "b", "c"}, 24, 80); n != 2 {
		t.Fatalf("숫자 선택: %d", n)
	}
	if n, _ := chooseLoop(bufio.NewReader(strings.NewReader("q")), &out, "t", []string{"a"}, 24, 80); n != -1 {
		t.Fatal("q 취소")
	}
	rows := make([]string, 100)
	for i := range rows {
		rows[i] = "row"
	}
	tableLoop(bufio.NewReader(strings.NewReader("\x1b[6~\x1b[5~\x1b[B\x1b[F\x1b[Hq")), &out, "t", "h", rows, 24, 80)
	var p bytes.Buffer
	pr := newTTYProgress(&p, "f.txt")
	pr.Begin(4, 400)
	pr.Update(2, 4, 200, "전송 중")
	_, _ = pr.Write([]byte("로그 줄\n"))
	pr.End()
	if s := p.String(); !strings.Contains(s, "[2/4] 전송 중") || !strings.Contains(s, " 50.0%") || !strings.Contains(s, "로그 줄") {
		t.Fatalf("진행 막대: %q", s)
	}
	if fit("가나다", 5) != "가나 " || dispWidth("가a") != 3 {
		t.Fatal("폭 계산")
	}
}
