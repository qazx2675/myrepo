// hpcbot — code 발급·공용 로그 테스트
package main

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestQLogCodeAndWrite(t *testing.T) {
	p := filepath.Join(t.TempDir(), "sub", "query.log")
	t.Setenv("HPCBOT_LOG", p)
	q := NewQLog()
	seen := map[string]bool{}
	for range 500 {
		c := q.NewCode()
		if len(c) != 4 || c[0] == '0' || seen[c] {
			t.Fatalf("code 이상: %q", c)
		}
		seen[c] = true
	}
	code := q.NewCode()
	if err := q.Write(LogEntry{Code: code, Input: "gpu 설치", Mode: "answer", ID: "gpu_driver", Answer: "줄1\n줄2"}); err != nil {
		t.Fatal(err)
	}
	if err := q.Write(LogEntry{Code: q.NewCode(), Input: "x", Mode: "nomatch"}); err != nil {
		t.Fatal(err)
	}
	f, _ := os.Open(p)
	defer f.Close()
	sc := bufio.NewScanner(f)
	n := 0
	for sc.Scan() {
		var e LogEntry
		if err := json.Unmarshal(sc.Bytes(), &e); err != nil {
			t.Fatalf("JSON 1줄 아님: %v", err)
		}
		if n == 0 && e.Answer != "줄1\n줄2" {
			t.Errorf("answer 전문 보존 실패: %q", e.Answer)
		}
		n++
	}
	if n != 2 {
		t.Errorf("줄 수 %d, want 2", n)
	}
	// 재시작해도 로그에 있는 code 는 다시 발급되지 않는다
	q2 := NewQLog()
	if !q2.used[code] {
		t.Error("기존 code 를 읽지 못함")
	}
	if runtime.GOOS != "windows" {
		if st, _ := os.Stat(p); st.Mode().Perm() != 0o666 {
			t.Errorf("파일 권한 %v, want 0666", st.Mode().Perm())
		}
		if st, _ := os.Stat(filepath.Dir(p)); st.Mode()&os.ModeSticky == 0 || st.Mode().Perm() != 0o777 {
			t.Errorf("디렉터리 권한 %v, want 1777", st.Mode())
		}
	}
}

func TestQLogRotate(t *testing.T) {
	p := filepath.Join(t.TempDir(), "query.log")
	t.Setenv("HPCBOT_LOG", p)
	if err := os.WriteFile(p, []byte(strings.Repeat("x", maxLogSize+1)), 0o666); err != nil {
		t.Fatal(err)
	}
	q := NewQLog()
	if err := q.Write(LogEntry{Code: "1234", Input: "a", Mode: "nomatch"}); err != nil {
		t.Fatal(err)
	}
	if st, err := os.Stat(p + ".1"); err != nil || st.Size() <= maxLogSize {
		t.Errorf("회전본 없음: %v", err)
	}
	if st, _ := os.Stat(p); st.Size() > 1000 {
		t.Errorf("새 로그가 큼: %d", st.Size())
	}
}

func TestQLogWriteFailure(t *testing.T) {
	// 디렉터리를 만들 수 없는 경로 → 오류 반환 (호출자는 경고만 출력)
	f := filepath.Join(t.TempDir(), "file")
	os.WriteFile(f, nil, 0o644)
	t.Setenv("HPCBOT_LOG", filepath.Join(f, "x", "query.log"))
	if err := NewQLog().Write(LogEntry{Code: "1111"}); err == nil {
		t.Error("실패해야 함")
	}
}

func TestSplitLabel(t *testing.T) {
	if s, ti := splitLabel("§6.1 GPU 드라이버 설치"); s != "§6.1" || ti != "GPU 드라이버 설치" {
		t.Errorf("%q %q", s, ti)
	}
}
