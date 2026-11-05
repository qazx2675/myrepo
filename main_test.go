package main

import (
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"
)

func TestParseHostLines(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []string
	}{
		{"기본", "a\nb\n", []string{"a", "b"}},
		{"주석·빈줄·공백", "  # c\n\n  a  \n#b\n", []string{"a"}},
		{"쉼표·공백 혼용", "a,b c ,d\te", []string{"a", "b", "c", "d", "e"}},
		{"범위", "n[1-3]", []string{"n1", "n2", "n3"}},
		{"0 패딩", "n[01-03]", []string{"n01", "n02", "n03"}},
		{"쉼표는 먼저 분리됨(gossh 동일)", "a[1,3,5-7]", []string{"a[1", "3", "5-7]"}},
		{"범위 안 단일값", "a[1-2]b", []string{"a1b", "a2b"}},
		{"역순 범위", "n[3-1]", []string{"n1", "n2", "n3"}},
		{"괄호 여러 개", "h[01-02]ev[1-2]", []string{"h01ev1", "h01ev2", "h02ev1", "h02ev2"}},
		{"중복 제거(첫 순서)", "b\na\nb\nn[1-2]\nn1", []string{"b", "a", "n1", "n2"}},
		{"CRLF", "a\r\nb\r\n", []string{"a", "b"}},
	}
	for _, c := range cases {
		if got := parseHostLines(c.in); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: got %v want %v", c.name, got, c.want)
		}
	}
}

func TestParseConf(t *testing.T) {
	c := parseConf("# 주석\nGOSSH = /opt/gossh \nDISK_PATTERN=Disk \"\" given\nDISK_PATTERN=second=one\nERROR_PATTERN=\nERROR_PATTERN=fatal error\n\nBOGUS\n")
	if c.Gossh != "/opt/gossh" {
		t.Errorf("GOSSH=%q", c.Gossh)
	}
	if want := []string{`Disk "" given`, "second=one"}; !reflect.DeepEqual(c.DiskPatterns, want) {
		t.Errorf("disk=%v", c.DiskPatterns)
	}
	if want := []string{"fatal error"}; !reflect.DeepEqual(c.ErrPatterns, want) {
		t.Errorf("err=%v", c.ErrPatterns)
	}
	d := parseConf("")
	if len(d.DiskPatterns) != 1 || d.DiskPatterns[0] != defaultDisk || len(d.ErrPatterns) != 0 {
		t.Errorf("기본값 오류: %+v", d)
	}
}

func TestShQuote(t *testing.T) {
	cases := map[string]string{
		"abc":    "'abc'",
		"it's":   `'it'\''s'`,
		`a "" b`: `'a "" b'`,
		"$x `y`": "'$x `y`'",
		"":       "''",
	}
	for in, want := range cases {
		if got := shQuote(in); got != want {
			t.Errorf("shQuote(%q)=%q want %q", in, got, want)
		}
	}
}

func TestBuildRemoteCmd(t *testing.T) {
	cmd := buildRemoteCmd([]string{"d1", "it's"}, nil)
	if !strings.Contains(cmd, `grep -qF -e 'd1' -e 'it'\''s' /tmp/anaconda.log && echo NS_DISK`) {
		t.Errorf("디스크 줄 오류: %s", cmd)
	}
	if strings.Contains(cmd, "NS_ERR") {
		t.Errorf("ERROR 패턴이 없으면 NS_ERR 줄이 없어야 함: %s", cmd)
	}
	if !strings.Contains(cmd, "NS_NOLOG") {
		t.Errorf("NS_NOLOG 누락: %s", cmd)
	}
	cmd = buildRemoteCmd([]string{"d"}, []string{"e1", "e2"})
	if !strings.Contains(cmd, `grep -qF -e 'e1' -e 'e2' /tmp/anaconda.log && echo NS_ERR`) {
		t.Errorf("ERROR 줄 오류: %s", cmd)
	}
	// 기본 명령은 gossh 위험어 검사를 통과해야 한다
	if isDangerousCommand(buildRemoteCmd([]string{defaultDisk}, []string{"fatal error"})) {
		t.Errorf("기본 명령이 위험어로 판정됨")
	}
}

func TestDanger(t *testing.T) {
	for _, p := range []string{"reboot now", "System SHUTDOWN", "poweroff", "halt", "init 0", "init 6", "xddcx"} {
		if len(dangerousPatterns([]string{p})) != 1 {
			t.Errorf("%q 는 거부되어야 함", p)
		}
	}
	for _, p := range []string{`Disk "" given in clearpart command does not exist`, "fatal error", "No space left"} {
		if len(dangerousPatterns([]string{p})) != 0 {
			t.Errorf("%q 는 허용되어야 함", p)
		}
	}
}

func TestParseRetryOutput(t *testing.T) {
	out := "n1: NS_DISK\nn2: NS_NOLOG\nn3: Welcome banner\nn4: NS_ERR\nn4: NS_DISK\r\nnoise\n: NS_ERR\n"
	got := parseRetryOutput(out)
	want := map[string]map[string]bool{
		"n1": {mkDisk: true},
		"n2": {mkNoLog: true},
		"n4": {mkErr: true, mkDisk: true},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v want %v", got, want)
	}
}

func TestRetryStatusPriority(t *testing.T) {
	cases := []struct {
		m    map[string]bool
		want string
	}{
		{map[string]bool{mkDisk: true, mkErr: true}, stNoDisk},
		{map[string]bool{mkErr: true}, stStopped},
		{map[string]bool{mkNoLog: true}, stInstall},
		{map[string]bool{}, stInstall},
		{nil, stInstall},
	}
	for _, c := range cases {
		if got := retryStatus(c.m); got != c.want {
			t.Errorf("%v: got %s want %s", c.m, got, c.want)
		}
	}
}

func TestClassify(t *testing.T) {
	hosts := []string{"ok", "off", "disk", "err", "none", "nolog", "lost", "both"}
	off := toSet([]string{"off", "both"})
	cand := toSet([]string{"disk", "err", "none", "nolog", "lost", "both"})
	retry := map[string]map[string]bool{
		"disk":  {mkDisk: true},
		"err":   {mkErr: true},
		"nolog": {mkNoLog: true},
		"both":  {mkDisk: true},
		"none":  {},
	}
	want := []string{stDone, stUnreach, stNoDisk, stStopped, stInstall, stInstall, stInstall, stUnreach}
	if got := classify(hosts, off, cand, retry); !reflect.DeepEqual(got, want) {
		t.Errorf("got %v want %v", got, want)
	}
}

func TestResolveGosshPath(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "gossh")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"), 0755); err != nil {
		t.Fatal(err)
	}
	cases := map[string]string{
		"":               "",
		bin:              bin,
		`"` + bin + `"`:  bin,
		"'" + bin + "'":  bin,
		dir:              bin,
		dir + "/":        bin,
		`"` + dir + `/"`: bin,
		"/no/such/gossh": "/no/such/gossh",
	}
	for in, want := range cases {
		if got := resolveGosshPath(in); got != want {
			t.Errorf("resolveGosshPath(%q) = %q, want %q", in, got, want)
		}
	}
	home, _ := os.UserHomeDir()
	if got := resolveGosshPath("~/x/gossh"); got != filepath.Join(home, "x/gossh") {
		t.Errorf("~ 확장 실패: %q", got)
	}
}

func TestParseConfV2(t *testing.T) {
	d := parseConf("")
	if d.CPUIntelLog != "/var/cpudiag/result.log" || d.CPUIntelOK != "Completed successfully. No issues detected." ||
		d.CPUAMDLog != "/var/cpudiag/results.json" || d.CPUAMDOK != "PASS" || d.CPUAMDLine != "Result" ||
		d.CPUIntelLine != "" || d.OSCheckSh != "" || d.OSCheckExclude != "" {
		t.Errorf("v2 기본값 오류: %+v", d)
	}
	c := parseConf("OS_CHECK_SH = ~/cc.sh \nOS_CHECK_EXCLUDE=a|b\nCPU_INTEL_LOG=/x\nCPU_INTEL_OK=fine=ok\nCPU_AMD_LOG=/y\nCPU_AMD_OK=amd ok\n")
	if c.OSCheckSh != "~/cc.sh" || c.OSCheckExclude != "a|b" || c.CPUIntelLog != "/x" || c.CPUIntelOK != "fine=ok" ||
		c.CPUAMDLog != "/y" || c.CPUAMDOK != "amd ok" {
		t.Errorf("v2 키 오류: %+v", c)
	}
	// 따옴표를 쓴 CPU 값도 동작해야 한다
	q := parseConf("CPU_AMD_LOG=\"/var/x.json\"\nCPU_AMD_OK='PASS'\nCPU_AMD_LINE=\"Result\"\nCPU_INTEL_LINE=x\n")
	if q.CPUAMDLog != "/var/x.json" || q.CPUAMDOK != "PASS" || q.CPUAMDLine != "Result" || q.CPUIntelLine != "x" {
		t.Errorf("CPU 따옴표 처리 오류: %+v", q)
	}
}

func TestExpandPath(t *testing.T) {
	home, _ := os.UserHomeDir()
	cases := map[string]string{
		`"/a/b.sh"`: "/a/b.sh",
		"'/a/b.sh'": "/a/b.sh",
		"~/c.sh":    filepath.Join(home, "c.sh"),
		"/a/b.sh":   "/a/b.sh",
		"":          "",
	}
	for in, want := range cases {
		if got := expandPath(in); got != want {
			t.Errorf("expandPath(%q)=%q want %q", in, got, want)
		}
	}
}

func TestBuildInfoCmd(t *testing.T) {
	c := parseConf("")
	cmd := buildInfoCmd(c)
	for _, w := range []string{
		"grep -m1 vendor_id /proc/cpuinfo;",
		"[ -f '/var/cpudiag/result.log' ] || echo NS_CPU_INTEL_NOLOG;",
		"grep -qiF -e 'Completed successfully. No issues detected.' '/var/cpudiag/result.log' && echo NS_CPU_INTEL;",
		"[ -f '/var/cpudiag/results.json' ] || echo NS_CPU_AMD_NOLOG;",
		`awk 'index(tolower($0),"result"){n++; if (index(tolower($0),"pass")) p++} END{if (n>0 && n==p) print "NS_CPU_AMD"}' '/var/cpudiag/results.json';`,
		"grep -rqs --include='messages*' --exclude='*.gz' stress-ng /var/log && echo NS_STRESS;",
		"lscpu | grep -i 'per core';",
		"echo NS_KERNEL `uname -r`",
	} {
		if !strings.Contains(cmd, w) {
			t.Errorf("누락 %q in %s", w, cmd)
		}
	}
	// csh 안전: 작은따옴표 밖에는 중괄호·변수 대입이 없고, 어디에도 ! ·exit 가 없음
	bare := regexp.MustCompile(`'[^']*'`).ReplaceAllString(cmd, "''")
	for _, bad := range []string{"{", "}"} {
		if strings.Contains(bare, bad) {
			t.Errorf("따옴표 밖 csh 비호환 문자열 %q: %s", bad, cmd)
		}
	}
	for _, bad := range []string{"!", "exit", "uptime"} {
		if strings.Contains(cmd, bad) {
			t.Errorf("csh 비호환 문자열 %q: %s", bad, cmd)
		}
	}
	if regexp.MustCompile(`(^|[;&|\s])\w+=`).MatchString(bare) {
		t.Errorf("변수 대입 형태 포함: %s", cmd)
	}
	if isDangerousCommand(cmd) {
		t.Errorf("기본 정보수집 명령이 위험어로 판정됨")
	}
	c.CPUAMDLog, c.CPUAMDOK, c.CPUAMDLine = "/a/log", "it's ok", ""
	cmd = buildInfoCmd(c)
	if !strings.Contains(cmd, `grep -qiF -e 'it'\''s ok' '/a/log' && echo NS_CPU_AMD;`) {
		t.Errorf("AMD 줄 오류: %s", cmd)
	}
	// 파일 이름 와일드카드: 셸 글롭 대신 ls -t | grep -x 로 최신 파일
	c.CPUAMDLog = "/var/cpudiag/run_*.json"
	if w := "/var/cpudiag/`ls -t '/var/cpudiag' | grep -m1 -x -e 'run_.*\\.json'`"; !strings.Contains(buildInfoCmd(c), w) {
		t.Errorf("와일드카드 경로 오류: %s", buildInfoCmd(c))
	}
	c.CPUAMDOK = ""
	if strings.Contains(buildInfoCmd(c), "NS_CPU_AMD") {
		t.Errorf("AMD 문구가 비면 줄이 없어야 함")
	}
	c.CPUIntelOK = ""
	if strings.Contains(buildInfoCmd(c), "NS_CPU_INTEL") {
		t.Errorf("Intel 문구가 비면 줄이 없어야 함")
	}
}

func TestParseInfoOutput(t *testing.T) {
	out := "a: vendor_id\t: GenuineIntel\na: NS_CPU_INTEL\na: NS_STRESS\na: Thread(s) per core:                     2\na: NS_KERNEL 4.18.0-1.el8\r\n" +
		"b: vendor_id\t: AuthenticAMD\nb: Thread(s) per core:  1\nb: NS_KERNEL \nb: banner text\n" +
		"c: Welcome\nnoise\n"
	out += "d: NS_CPU_INTEL_NOLOG\nd: NS_CPU_AMD_NOLOG\n"
	got := parseInfoOutput(out)
	if d := got["d"]; d == nil || !d.noIntel || !d.noAMD || d.cpuIntel || d.cpuAMD {
		t.Errorf("d=%+v", d)
	}
	a := got["a"]
	if a == nil || a.vendor != "GenuineIntel" || !a.cpuIntel || a.cpuAMD || !a.stress || a.tpc != 2 || a.kernel != "4.18.0-1.el8" {
		t.Errorf("a=%+v", a)
	}
	b := got["b"]
	if b == nil || b.vendor != "AuthenticAMD" || b.cpuIntel || b.stress || b.tpc != 1 || b.kernel != "" {
		t.Errorf("b=%+v", b)
	}
	if c := got["c"]; c == nil || c.vendor != "" || c.tpc != 0 {
		t.Errorf("c=%+v", c)
	}
}

func TestHostInfoResults(t *testing.T) {
	cases := []struct {
		name                 string
		h                    *hostInfo
		cpu, stress, ht, osv string
	}{
		{"Intel ok", &hostInfo{vendor: "GenuineIntel", cpuIntel: true, stress: true, tpc: 2, kernel: "k1"}, "O", "O", "ON", "k1"},
		{"Intel ng", &hostInfo{vendor: "GenuineIntel", tpc: 1}, "X", "NOTRUN", "OFF", "-"},
		{"Intel 인데 AMD 표식", &hostInfo{vendor: "GenuineIntel", cpuAMD: true}, "X", "NOTRUN", "-", "-"},
		{"AMD ok", &hostInfo{vendor: "AuthenticAMD", cpuAMD: true, tpc: 4}, "O", "NOTRUN", "ON", "-"},
		{"AMD ng", &hostInfo{vendor: "AuthenticAMD"}, "X", "NOTRUN", "-", "-"},
		{"Intel 로그 없음", &hostInfo{vendor: "GenuineIntel", noIntel: true}, "NOTRUN", "NOTRUN", "-", "-"},
		{"AMD 로그 없음", &hostInfo{vendor: "AuthenticAMD", noAMD: true}, "NOTRUN", "NOTRUN", "-", "-"},
		{"AMD 인데 Intel 로그만 없음", &hostInfo{vendor: "AuthenticAMD", noIntel: true}, "X", "NOTRUN", "-", "-"},
		{"벤더 없음", &hostInfo{cpuIntel: true, cpuAMD: true}, "X", "NOTRUN", "-", "-"},
		{"nil", nil, "X", "X", "-", "-"},
	}
	for _, c := range cases {
		if c.h.cpuResult() != c.cpu || c.h.stressResult() != c.stress || c.h.htResult() != c.ht || c.h.osResult() != c.osv {
			t.Errorf("%s: %s %s %s %s", c.name, c.h.cpuResult(), c.h.stressResult(), c.h.htResult(), c.h.osResult())
		}
	}
}

func TestParseCheckRes(t *testing.T) {
	text := "a: OK something\na: PASS x\nb: OK\nb:   FAIL net\nc: warn FAIL inside\nd: FAIL\r\nnoise\n"
	// 체크 스크립트 경로가 마운트되지 않은 경우: 셸 오류 줄만 있음 → 출력없음 (O 로 나오면 안 됨)
	text += "f: bash: /user/svrauto/SA/check/os/run.sh: No such file or directory\n"
	hosts := []string{"a", "b", "c", "d", "e", "f", "ex-1", "ex-2"}
	text += "ex-1: FAIL zz\n"
	got := parseCheckRes(text, " ex-1 | |qq", hosts)
	want := map[string]string{"a": "O", "b": "X", "c": noOutput, "d": "X", "e": noOutput, "f": noOutput, "ex-1": "O", "ex-2": noOutput}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v want %v", got, want)
	}
	// 제외 문자열이 비면 아무것도 제외하지 않는다
	if got := parseCheckRes(text, "|  |", hosts); got["ex-1"] != "X" || got["ex-2"] != noOutput {
		t.Errorf("빈 제외 처리 오류: %v", got)
	}
}

func TestFormatRow(t *testing.T) {
	h := &hostInfo{vendor: "GenuineIntel", cpuIntel: true, stress: true, tpc: 2, kernel: "4.18"}
	if got, want := formatRow("n1", stDone, "O", h, "메모리 불량"), "n1|완료|O|O|O|ON|4.18|메모리 불량"; got != want {
		t.Errorf("got %q want %q", got, want)
	}
	if got, want := formatRow("n2", stDone, "-", nil, ""), "n2|완료|-|X|X|-|-|"; got != want {
		t.Errorf("got %q want %q", got, want)
	}
	for _, st := range []string{stNoDisk, stStopped, stInstall, stUnreach} {
		got := formatRow("n3", st, "O", h, "x")
		if got != "n3|"+st+"|-|-|-|-|-|x" || strings.Count(got, "|") != 7 {
			t.Errorf("미완료 행 오류: %q", got)
		}
	}
	if header != "hostname|상태|설정체크|CPU테스트|stress-ng|H/T|OS|비고" {
		t.Errorf("제목 줄 오류: %q", header)
	}
}

func TestParseErrorList(t *testing.T) {
	// 탭으로만 구분: 사유 안의 공백은 그대로, 공백으로만 구분한 줄(node05)은 무시
	text := "\xef\xbb\xbf# 주석\r\nnode01\t메모리 불량 (DIMM A1)\r\n\r\nNODE02\t전원 | 케이블\nnode01\t디스크 교체\nnode03\nnode04\t \nnode05 공백 구분\n"
	got := parseErrorList(text)
	want := map[string]string{"node01": "메모리 불량 (DIMM A1), 디스크 교체", "node02": "전원 / 케이블"}
	if len(got) != len(want) {
		t.Fatalf("got %v want %v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s: got %q want %q", k, got[k], v)
		}
	}
}

func TestParseConfRunKeys(t *testing.T) {
	c := parseConf("")
	if c.CPUWaitStart != 47*time.Minute || c.CPUWaitInt != 2*time.Minute || c.CPUWaitMax != 30*time.Minute ||
		c.GosshTimeout != 10*time.Minute || c.CheckTimeout != 40*time.Minute || c.StressTimeout != 60*time.Minute ||
		c.StressTest != "" || len(c.Warnings) != 0 {
		t.Errorf("기본값 오류: %+v", c)
	}
	c = parseConf("STRESS_TEST=/opt/st/stress-test\nSTRESS_TEST_CONF=\"/opt/st/a.conf\"\nUSER_ROUTE=/opt/route\n" +
		"CPU_WAIT_START=90s\nCPU_WAIT_INTERVAL=3\nCPU_WAIT_MAX=1h\nGOSSH_TIMEOUT=5m\nCHECK_TIMEOUT=1.5\nSTRESS_TIMEOUT=\"2h\"\n")
	if c.StressTest != "/opt/st/stress-test" || c.StressTestConf != "/opt/st/a.conf" || c.UserRoute != "/opt/route" ||
		c.CPUWaitStart != 90*time.Second || c.CPUWaitInt != 3*time.Minute || c.CPUWaitMax != time.Hour ||
		c.GosshTimeout != 5*time.Minute || c.CheckTimeout != 90*time.Second || c.StressTimeout != 2*time.Hour ||
		len(c.Warnings) != 0 {
		t.Errorf("지정값 오류: %+v", c)
	}
	// 잘못된 값·0·음수는 기본값 유지 + 경고
	c = parseConf("CPU_WAIT_START=abc\nCPU_WAIT_INTERVAL=0\nGOSSH_TIMEOUT=-5m\nCHECK_TIMEOUT=10m\n")
	if c.CPUWaitStart != 47*time.Minute || c.CPUWaitInt != 2*time.Minute || c.GosshTimeout != 10*time.Minute ||
		c.CheckTimeout != 10*time.Minute || len(c.Warnings) != 3 {
		t.Errorf("잘못된 값 처리 오류: %+v", c)
	}
}

func TestParseDuration(t *testing.T) {
	cases := []struct {
		in   string
		want time.Duration
		ok   bool
	}{
		{"47m", 47 * time.Minute, true}, {"30s", 30 * time.Second, true}, {"5", 5 * time.Minute, true},
		{" 2 ", 2 * time.Minute, true}, {"1h30m", 90 * time.Minute, true},
		{"", 0, false}, {"x", 0, false}, {"0", 0, false}, {"-1m", 0, false},
	}
	for _, c := range cases {
		if d, ok := parseDuration(c.in); ok != c.ok || (ok && d != c.want) {
			t.Errorf("%q: got %v,%v want %v,%v", c.in, d, ok, c.want, c.ok)
		}
	}
}
