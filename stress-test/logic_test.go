package main

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestParseHosts(t *testing.T) {
	old := lookupHost
	defer func() { lookupHost = old }()
	lookupHost = func(h string) ([]string, error) {
		switch h {
		case "node01":
			return []string{"fe80::1", "10.0.0.1", "10.0.0.9"}, nil
		case "NODE01":
			return []string{"10.0.0.1"}, nil
		}
		return nil, errors.New("no such host")
	}
	text := "\xef\xbb\xbf# 주석\n\nnode01\r\n" +
		"a b c rec01 10.0.0.2 aa:bb eth0 sda sda5 100 8.10 uefi\n" +
		"a b c rec02 10.0.0.3 aa:cc eth0 sda sda5 100 8.10 uefi 분류\n" +
		"NODE01\n" + // 중복(대소문자 무시) → 무시
		"ghost\n"
	list, errs := parseHosts(text)
	if len(errs) != 0 {
		t.Fatalf("errs = %v", errs)
	}
	if len(list) != 4 {
		t.Fatalf("len = %d", len(list))
	}
	want0 := []string{"tmp", "tmp", "tmp", "node01", "10.0.0.1", "02:00:00:00:00:03", "eth0", "sda", "sda5", "100", "8.10", "uefi"}
	if !reflect.DeepEqual(list[0].Fields, want0) || list[0].DNSFail {
		t.Errorf("node01 = %v dns=%v", list[0].Fields, list[0].DNSFail)
	}
	if list[1].Host != "rec01" || len(list[1].Fields) != 12 {
		t.Errorf("rec01 = %+v", list[1])
	}
	if list[2].Host != "rec02" || len(list[2].Fields) != 12 || list[2].Fields[11] != "uefi" {
		t.Errorf("rec02 13필드 처리 = %v", list[2].Fields)
	}
	if !list[3].DNSFail || list[3].Fields[4] != "" {
		t.Errorf("ghost = %+v", list[3])
	}

	_, errs = parseHosts("node01\na b c\n")
	if len(errs) != 1 || !strings.Contains(errs[0], "2번째 줄") {
		t.Errorf("오류 줄 = %v", errs)
	}
}

func TestMacFromLine(t *testing.T) {
	if got := macFromLine(0x010203); got != "02:00:00:01:02:03" {
		t.Errorf("mac = %s", got)
	}
	if macFromLine(1) == macFromLine(2) {
		t.Error("줄마다 달라야 함")
	}
}

func TestParseErrorList(t *testing.T) {
	text := "\xef\xbb\xbf# 주석\nNode01\t팬 소음\r\nnode01\tPSU|교체\nnode02\n\nnode03\t  \nnode04 공백 구분은 무시\n"
	got := parseErrorList(text)
	want := map[string]string{"node01": "팬 소음, PSU/교체"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v", got)
	}
}

func TestParseSteps(t *testing.T) {
	for in, want := range map[string][]string{"a": {stageCPU}, "b": {stageStress}, "ab": {stageCPU, stageStress}, "ba": {stageStress, stageCPU}} {
		got, err := parseSteps(in)
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Errorf("%s → %v %v", in, got, err)
		}
	}
	for _, bad := range []string{"", "c", "aa", "abc", "a b"} {
		if _, err := parseSteps(bad); err == nil {
			t.Errorf("%q 는 오류여야 함", bad)
		}
	}
}

func TestParseVendors(t *testing.T) {
	out := "h1: vendor_id\t: GenuineIntel\nh2: vendor_id\t: AuthenticAMD\nh3: vendor_id\t: HygonGenuine\nh5: garbage\n"
	got := parseVendors(out, map[string]bool{"h4": true}, []string{"h1", "h2", "h3", "h4", "h5", "h6"})
	want := map[string]string{"h1": "Intel", "h2": "AMD", "h3": "unknown", "h4": "", "h5": "unknown", "h6": ""}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v", got)
	}
}

func TestParseLoads(t *testing.T) {
	out := "h1: 3.52 1.00 0.50 2/300 123\nh2: 0.05 0.03 0.00 1/745 616465\nh3: ERROR: x\nnoise\n"
	got := parseLoads(out)
	want := map[string]float64{"h1": 3.52, "h2": 0.05}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v", got)
	}
}

func TestParseConfig(t *testing.T) {
	c, err := parseConfig("awx_url = http://x # 주석\nmonitor_sec = 30\nload_threshold = 2.5\ninventory_source =\nunknown = 1\n", "t.conf")
	if err != nil {
		t.Fatal(err)
	}
	if c.AWXURL != "http://x" || c.MonitorSec != 30 || c.LoadThreshold != 2.5 || c.MonitorInterval != 10 || c.RepoDir != "/root/Inventory" || c.SourceField != "source_path" {
		t.Errorf("cfg = %+v", c)
	}
	if _, err := parseConfig("monitor_sec = abc\n", "t.conf"); err == nil {
		t.Error("숫자 아님 오류여야 함")
	}
	if _, err := parseConfig("novalue\n", "t.conf"); err == nil {
		t.Error("= 없음 오류여야 함")
	}
	if err := c.validate("b"); err == nil || !strings.Contains(err.Error(), "stress_template") {
		t.Errorf("validate = %v", err)
	}
}

func TestResultLines(t *testing.T) {
	ts := []*target{
		{Host: "h1", Vendor: "Intel", Res: map[string]string{stageCPU: resDone, stageStress: resNotRun}, Notes: []string{"팬 소음"}},
		{Host: "h2", Res: map[string]string{stageStress: resOffline}},
		{Host: "h3", Vendor: "unknown", Res: map[string]string{stageStress: resDone}},
	}
	got := resultLines(ts)
	want := []string{
		"hostname|CPU벤더|cpu|stress-ng|비고",
		"h1|Intel|실행|미실행|팬 소음",
		"h2|-|-|접속불가|",
		"h3|unknown|-|실행|",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %q", got)
	}
	if !hasProblem(ts) || hasProblem(ts[1:]) {
		t.Error("hasProblem 판정 오류")
	}
}

func TestYamlName(t *testing.T) {
	if yamlName("hong", "intel", 0) != "hong_intel.yaml" || yamlName("hong", "stress", 2) != "hong_retry2_stress.yaml" {
		t.Error("yaml 이름")
	}
}

func TestEaMismatch(t *testing.T) {
	if n, bad := eaMismatch("x_3ea.yml", 2); !bad || n != 3 {
		t.Error("불일치여야 함")
	}
	if _, bad := eaMismatch("x_2ea.yml", 2); bad {
		t.Error("일치")
	}
	if _, bad := eaMismatch("x.yml", 2); bad {
		t.Error("ea 없음")
	}
}

// fakeClock 은 sleepFn 이 호출되면 시간을 그만큼 앞으로 민다.
func fakeClock(t *testing.T) {
	cur := time.Unix(1000, 0)
	oldNow, oldSleep := nowFn, sleepFn
	nowFn = func() time.Time { return cur }
	sleepFn = func(d time.Duration) { cur = cur.Add(d) }
	t.Cleanup(func() { nowFn, sleepFn = oldNow, oldSleep })
}

func TestMonitorLoad(t *testing.T) {
	fakeClock(t)
	ts := []*target{{Host: "h1"}, {Host: "h2"}, {Host: "h3"}}
	calls := 0
	check := func(hosts []string) (map[string]float64, error) {
		calls++
		// 1회차: h1 만 부하, 이후: h2 도 부하, h3 은 계속 응답없음
		if calls == 1 {
			return map[string]float64{"h1": 5, "h2": 0.1}, nil
		}
		return map[string]float64{"h2": 4}, nil
	}
	var b strings.Builder
	rem := monitorLoad(&b, "stress-ng", ts, check, 30, 10, 3, false)
	if len(rem) != 1 || rem[0].Host != "h3" || calls != 3 {
		t.Errorf("rem=%v calls=%d", rem, calls)
	}
	lines := strings.Split(strings.TrimSpace(b.String()), "\n")
	if len(lines) != 3 || !strings.Contains(lines[0], "h2(0.10)") || !strings.Contains(lines[2], "h3(응답없음)") || !strings.Contains(lines[2], "30/30초") {
		t.Errorf("요약:\n%s", b.String())
	}

	// 목록이 비면 즉시 종료
	calls = 0
	b.Reset()
	rem = monitorLoad(&b, "cpu", []*target{{Host: "h1"}}, func([]string) (map[string]float64, error) {
		calls++
		return map[string]float64{"h1": 9}, nil
	}, 120, 10, 3, true)
	if len(rem) != 0 || calls != 1 || !strings.HasPrefix(b.String(), "\033[H\033[2J") {
		t.Errorf("rem=%v calls=%d out=%q", rem, calls, b.String())
	}
}

func TestRenderScreen(t *testing.T) {
	rows := []*monRow{{t: &target{Host: "node01", Vendor: "Intel", Notes: []string{"팬 소음"}}, load: 0.5, ok: true}, {t: &target{Host: "n2", Vendor: "AMD"}}}
	s := renderScreen("cpu", rows, 20, 120, 3, 5)
	for _, w := range []string{"\033[H\033[2J", "hostname", "CPU벤더", "node01", "0.50", "팬 소음", "응답없음", "부하 확인 3/5대"} {
		if !strings.Contains(s, w) {
			t.Errorf("화면에 %q 없음:\n%s", w, s)
		}
	}
}
