// hpcbot — 한 줄 실행 종료 코드·REPL 후보 선택·--jev 테스트
package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func runCLI(t *testing.T, stdin string, args ...string) (int, string, string) {
	t.Helper()
	t.Setenv("HPCBOT_LOG", filepath.Join(t.TempDir(), "query.log"))
	t.Setenv("JEV_API_KEY", "")
	var out, errb bytes.Buffer
	code := run(args, strings.NewReader(stdin), &out, &errb)
	return code, out.String(), errb.String()
}

func TestOneShotExitCodes(t *testing.T) {
	cases := []struct {
		q    string
		exit int
		must string
	}{
		{"docker 설치", 0, "§8.1"},
		{"설치", 2, "번호를 입력하세요"},
		{"점심 메뉴", 1, "찾지 못했습니다"},
	}
	for _, c := range cases {
		code, out, _ := runCLI(t, "", c.q)
		if code != c.exit || !strings.Contains(out, c.must) || !strings.Contains(out, "code ") {
			t.Errorf("%q → exit %d (want %d)\n%s", c.q, code, c.exit, out)
		}
	}
}

func TestREPLCandidateChoice(t *testing.T) {
	code, out, _ := runCLI(t, "설치\n2\nexit\n")
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(out, "번호를 입력하세요") || !strings.Contains(out, sepLine) || !strings.Contains(out, "종료합니다") {
		t.Errorf("후보→선택→종료 흐름 이상:\n%s", out)
	}
}

func TestLogRecordsCodeAndAnswer(t *testing.T) {
	lp := filepath.Join(t.TempDir(), "query.log")
	t.Setenv("HPCBOT_LOG", lp)
	t.Setenv("JEV_API_KEY", "")
	var out, errb bytes.Buffer
	run([]string{"docker 설치"}, strings.NewReader(""), &out, &errb)
	b, _ := os.ReadFile(lp)
	var e LogEntry
	if err := json.Unmarshal(bytes.TrimSpace(b), &e); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "code "+e.Code) || e.ID != "docker" || e.Mode != "answer" || !strings.Contains(e.Answer, "[§8.1") {
		t.Errorf("로그/화면 불일치: %+v", e)
	}
}

func TestLogFailureStillAnswers(t *testing.T) {
	f := filepath.Join(t.TempDir(), "file")
	os.WriteFile(f, nil, 0o644)
	t.Setenv("HPCBOT_LOG", filepath.Join(f, "x", "query.log"))
	t.Setenv("JEV_API_KEY", "")
	var out, errb bytes.Buffer
	if code := run([]string{"docker 설치"}, strings.NewReader(""), &out, &errb); code != 0 {
		t.Errorf("exit %d", code)
	}
	if !strings.Contains(out.String(), "§8.1") || !strings.Contains(errb.String(), "로그 기록 실패") {
		t.Errorf("out=%q err=%q", out.String(), errb.String())
	}
}

func TestJevOptionPicksOnAmbiguous(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer testkey" {
			http.Error(w, "no", 401)
			return
		}
		json.NewEncoder(w).Encode(SystemOneResponse{Answers: map[string]Answer{
			"intent": {Choice: "gpu_driver", Probabilities: map[string]float64{"gpu_driver": 0.9, "os_install": 0.1}}}})
	}))
	defer srv.Close()
	t.Setenv("HPCBOT_LOG", filepath.Join(t.TempDir(), "query.log"))
	d, err := LoadData()
	if err != nil {
		t.Fatal(err)
	}
	var out, errb bytes.Buffer
	a := &app{d: d, m: NewMatcher(d), ql: NewQLog(), out: &out, errOut: &errb, jevKey: "testkey", jevURL: srv.URL}
	if exit := a.handle("설치", false); exit != 0 || !strings.Contains(out.String(), "[Jev]") {
		t.Errorf("exit %d\n%s", exit, out.String())
	}
	// 로컬이 이미 답변이면 Jev 를 부르지 않는다 (서버가 401 을 주도록 키를 틀리게)
	out.Reset()
	a.jevKey = "wrong"
	if exit := a.handle("docker 설치", false); exit != 0 || errb.Len() != 0 {
		t.Errorf("로컬 답변인데 Jev 호출: exit %d err=%q", exit, errb.String())
	}
	// 실패하면 로컬 결과 유지 + 경고
	out.Reset()
	if exit := a.handle("설치", false); exit != 2 || !strings.Contains(errb.String(), "--jev 호출 실패") {
		t.Errorf("exit %d err=%q", exit, errb.String())
	}
}

func TestParseEnvKey(t *testing.T) {
	if k := parseEnvKey("# c\nexport JEV_API_KEY=\"abc\"\n"); k != "abc" {
		t.Errorf("%q", k)
	}
	if k := parseEnvKey("OTHER=1"); k != "" {
		t.Errorf("%q", k)
	}
}
