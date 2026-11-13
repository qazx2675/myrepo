package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func surveyTestData() (cmds, hosts []string, res map[string]surveyHost) {
	cmds = []string{"uname -r", "cat /etc/motd", "head -2 /etc/passwd"}
	hosts = []string{"svr1", "svr2", "svr3"}
	res = map[string]surveyHost{
		"svr1": {reachable: true, cells: []surveyCell{{"4.18.0\n", true}, {"", true}, {"root:x:0\nbin:x:1\n", true}}},
		"svr2": {reachable: true, cells: []surveyCell{{"  3.10.0  \n\n", true}, {"hello\r\n", true}, {"root:x:0\n", true}}},
		"svr3": {err: "i/o timeout"}, // 접속불가
	}
	return
}

func TestBuildSurveyTable(t *testing.T) {
	cmds, hosts, res := surveyTestData()

	// 줄바꿈을 그대로 둔 표(재입력 전 단계)에서는 칸 안에 \n 이 남는다.
	raw := buildSurveyTable(cmds, hosts, res, false)
	if got := raw[1][3]; got != "root:x:0\nbin:x:1" {
		t.Errorf("join=false svr1 3번 칸 = %q", got)
	}

	rows := buildSurveyTable(cmds, hosts, res, true)
	want := [][]string{
		{"hostname", "uname -r", "cat /etc/motd", "head -2 /etc/passwd"},
		{"svr1", "4.18.0", "", "root:x:0/bin:x:1"}, // 빈 결과 = 빈 칸, 여러 줄 = /
		{"svr2", "3.10.0", "hello", "root:x:0"},    // 앞뒤 공백·줄바꿈·\r 정리
		{"svr3", "-", "-", "-"},                    // 접속불가 = 전부 -
	}
	if len(rows) != len(want) {
		t.Fatalf("줄 수 = %d, want %d", len(rows), len(want))
	}
	for i := range want {
		if strings.Join(rows[i], "\x00") != strings.Join(want[i], "\x00") {
			t.Errorf("줄 %d = %q, want %q", i, rows[i], want[i])
		}
	}

	txt := renderSurveyTable(rows, "|")
	wantTxt := "hostname|uname -r|cat /etc/motd|head -2 /etc/passwd\n" +
		"svr1|4.18.0||root:x:0/bin:x:1\nsvr2|3.10.0|hello|root:x:0\nsvr3|-|-|-\n"
	if txt != wantTxt {
		t.Errorf("| 표 =\n%q\nwant\n%q", txt, wantTxt)
	}
}

func TestSurveyTSV(t *testing.T) {
	cmds := []string{"echo a\tb", "uname -r"}
	hosts := []string{"h1", "h2"}
	res := map[string]surveyHost{
		"h1": {reachable: true, cells: []surveyCell{{"x\ty\n", true}, {"k\n", true}}},
	}
	tsv := renderSurveyTable(buildSurveyTable(cmds, hosts, res, true), "\t")
	lines := strings.Split(strings.TrimSuffix(tsv, "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("줄 수 = %d", len(lines))
	}
	for i, l := range lines {
		if n := len(strings.Split(l, "\t")); n != len(cmds)+1 {
			t.Errorf("줄 %d 칸 수 = %d, want %d: %q", i, n, len(cmds)+1, l)
		}
	}
	if lines[2] != "h2\t-\t-" {
		t.Errorf("접속불가 줄 = %q", lines[2])
	}
}

func TestFindSurveyMulti(t *testing.T) {
	cmds, hosts, res := surveyTestData()
	m := findSurveyMulti(cmds, hosts, res)
	if len(m) != 1 || m[0].cmdIdx != 2 || len(m[0].hosts) != 1 || m[0].hosts[0] != "svr1" {
		t.Errorf("multi = %+v", m)
	}
	if got := surveyHostSample([]string{"a", "b", "c", "d"}, 2); got != "a, b 외 2대" {
		t.Errorf("surveyHostSample = %q", got)
	}
}

func TestParsePreviewKey(t *testing.T) {
	cases := []struct {
		in    string
		nCmds int
		want  previewKey
	}{
		{"", 3, previewKey{kind: pvRunFirst}},
		{"   ", 3, previewKey{kind: pvRunFirst}},
		{"next", 3, previewKey{kind: pvNext}},
		{"a", 3, previewKey{kind: pvAdd}},
		{"2i", 3, previewKey{kind: pvEdit, n: 2}},
		{"3d", 3, previewKey{kind: pvDel, n: 3}},
		{"svr9", 3, previewKey{kind: pvRunHost, host: "svr9"}},
		{"192.168.0.60", 3, previewKey{kind: pvRunHost, host: "192.168.0.60"}},
	}
	for _, c := range cases {
		if got := parsePreviewKey(c.in, c.nCmds); got != c.want {
			t.Errorf("parsePreviewKey(%q,%d) = %+v, want %+v", c.in, c.nCmds, got, c.want)
		}
	}
	// 범위를 벗어난 번호, 명령어가 1개일 때의 삭제, 공백이 낀 입력은 알 수 없는 입력이다.
	for _, c := range []struct {
		in    string
		nCmds int
	}{{"4i", 3}, {"0d", 3}, {"1d", 1}, {"a b", 3}} {
		if got := parsePreviewKey(c.in, c.nCmds); got.kind != pvBad || got.msg == "" {
			t.Errorf("parsePreviewKey(%q,%d) = %+v, want 안내가 있는 pvBad", c.in, c.nCmds, got)
		}
	}
	// 명령어가 1개여도 수정(1i)은 된다.
	if got := parsePreviewKey("1i", 1); got.kind != pvEdit {
		t.Errorf("1i = %+v", got)
	}
}

func TestSurveyScriptKeepsCommand(t *testing.T) {
	cmd := `awk -F: '$1=="root"{print $7}' /etc/passwd | head -1; echo "a b" 'c|d' \$HOME`
	s := surveyScript(cmd)
	if !strings.Contains(s, "\n"+cmd+"\n") {
		t.Errorf("명령어가 그대로 들어 있지 않음: %q", s)
	}
}

// 질문 → 미리보기(2i 유지, a 추가, 1d 삭제, 호스트 지정) → 전체 실행 → 여러 줄 y 로 재입력 → 최종 n 흐름.
func TestSurveyFlow(t *testing.T) {
	calls := 0
	multiOnce := true
	fake := func(hs, cmds []string, full bool) map[string]surveyHost {
		calls++
		res := map[string]surveyHost{}
		for _, h := range hs {
			r := surveyHost{reachable: true}
			for _, c := range cmds {
				out := "ok:" + c
				if c == "multi" && multiOnce {
					out = "a\nb"
				}
				r.cells = append(r.cells, surveyCell{out: out, ok: true})
			}
			res[h] = r
		}
		return res
	}
	in := []string{
		"abc", "0", "2", // 수량: 잘못된 입력 두 번 뒤 2
		"multi", "x 'y' \"z\" $1 | \\",
		"2i", "", // 2번 그대로 유지
		"a", "third", // 추가
		"1d",          // 삭제 -> [x.., third]
		"srvX",        // 목록에 없는 호스트
		"2i", "multi", // 2번을 multi 로 바꿈
		"next",
		"y",                    // 여러 줄이니 다시 입력
		"2i", "single", "next", // 고치고 다시 실행
	}
	withInput(in, func() {
		saveErr := os.Stderr
		devnull, _ := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
		os.Stderr = devnull
		defer func() { os.Stderr = saveErr; devnull.Close() }()
		cmds, res, join := surveyFlow([]string{"h1", "h2"}, fake)
		if len(cmds) != 2 || cmds[0] != `x 'y' "z" $1 | \` || cmds[1] != "single" {
			t.Errorf("cmds = %q", cmds)
		}
		if join {
			t.Errorf("재입력 뒤 여러 줄이 없는데 join=true")
		}
		if len(res) != 2 {
			t.Errorf("res = %+v", res)
		}
	})
	if calls != 3 { // 호스트 지정 미리보기 1 + 전체 2
		t.Errorf("실행 횟수 = %d, want 3", calls)
	}
}

func TestSurveyFlowJoinOnNo(t *testing.T) {
	fake := func(hs, cmds []string, full bool) map[string]surveyHost {
		res := map[string]surveyHost{}
		for _, h := range hs {
			res[h] = surveyHost{reachable: true, cells: []surveyCell{{"a\nb\n", true}}}
		}
		return res
	}
	withInput([]string{"1", "head -2 f", "next", ""}, func() { // 빈 답 = N
		saveErr := os.Stderr
		devnull, _ := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
		os.Stderr = devnull
		defer func() { os.Stderr = saveErr; devnull.Close() }()
		_, res, join := surveyFlow([]string{"h1"}, fake)
		if !join {
			t.Errorf("n 인데 join=false")
		}
		if got := surveyCellText(res["h1"].cells[0], join); got != "a/b" {
			t.Errorf("cell = %q", got)
		}
	})
}

func TestPrintSurveyDoneFiles(t *testing.T) {
	dir := t.TempDir()
	hostFile := filepath.Join(dir, "hosts")
	cmds, hosts, res := surveyTestData()
	out := captureOutput(func() { printSurveyDone(cmds, hosts, res, true, hostFile) })
	for _, want := range []string{hostFile + sufSurveyTSV, hostFile + sufSurveyTxt, "접속불가 1대"} {
		if !strings.Contains(out, want) {
			t.Errorf("출력에 %q 없음:\n%s", want, out)
		}
	}
	tsv, err := os.ReadFile(hostFile + sufSurveyTSV)
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range strings.Split(strings.TrimSuffix(string(tsv), "\n"), "\n") {
		if n := len(strings.Split(l, "\t")); n != len(cmds)+1 {
			t.Errorf("TSV 칸 수 = %d: %q", n, l)
		}
	}
	if txt, _ := os.ReadFile(hostFile + sufSurveyTxt); !strings.Contains(string(txt), "svr3|-|-|-\n") {
		t.Errorf("txt = %q", txt)
	}
}

// -sv 예시는 질문·경고·표·파일 경로를 모두 보여주고 현재 폴더를 건드리지 않는다.
func TestHelpExampleSurvey(t *testing.T) {
	d, ok := findOptionDoc("-sv")
	if !ok {
		t.Fatal("-sv 옵션 설명 없음")
	}
	before, _ := os.ReadDir(".")
	text := helpExampleText(d, false)
	for _, want := range []string{
		"1. 조사할 명령어 수량 : 3", "2. 조사 명령어 3/3 : ls /boot/vmlinuz-*", "[확인] svr1", "[경고] 결과가 2줄 이상인 칸이 있습니다.",
		"명령어를 다시 입력하시겠습니까? (y/N) : n", "hostname|uname -r|cat /etc/redhat-release|ls /boot/vmlinuz-*",
		"svr4|-|-|-", exampleHostFile + sufSurveyTSV,
	} {
		if !strings.Contains(text, want) {
			t.Errorf("-sv 예시에 %q 없음:\n%s", want, text)
		}
	}
	if after, _ := os.ReadDir("."); len(after) != len(before) {
		t.Errorf("예시 실행 후 현재 폴더 파일 수가 바뀜")
	}
}
