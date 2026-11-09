package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ioTarget 은 -ignore-offline 시험용 호스트다.
func ioTarget(host string) *target {
	return &target{Host: host, Fields: strings.Fields("tmp tmp tmp " + host + " 10.0.0.1 02:00:00:00:00:01 eth0 sda sda5 100 8.10 uefi"), Res: map[string]string{}}
}

// ioApp 은 가짜 gossh 를 붙인 app 이다. 가짜 gossh: off* 호스트는 접속불가, 나머지는 Intel, 로드는 loadVal.
// 호출 기록(명령: 호스트들)은 반환하는 로그 파일에 남는다.
func ioApp(t *testing.T, loadVal string, ignore bool) (*app, *fakeAWX, *strings.Builder, string) {
	a, f, _ := newTestApp(t)
	fakeClock(t)
	dir := t.TempDir()
	logPath := filepath.Join(dir, "gossh_calls.log")
	t.Setenv("FAKE_GOSSH_LOG", logPath)
	g := filepath.Join(dir, "gossh")
	writeScript(t, g, `hf=$2; cmd=$4
echo "$cmd: $(tr '\n' ' ' < "$hf")" >> "$FAKE_GOSSH_LOG"
case "$cmd" in
*vendor_id*) for h in $(cat "$hf"); do case $h in off*) echo $h >> "${hf}_res_off";; *) printf '%s: vendor_id\t: GenuineIntel\n' $h;; esac; done;;
*loadavg*) for h in $(cat "$hf"); do echo "$h: `+loadVal+` 1.00 1.00 1/100 1"; done;;
esac
`)
	a.gossh = &gosshRunner{path: g, workDir: dir, log: &strings.Builder{}}
	out := &strings.Builder{}
	a.out = out
	a.ignoreOffline = ignore
	a.cfg.MonitorSec, a.cfg.MonitorInterval, a.cfg.LoadThreshold = 10, 10, 3
	return a, f, out, logPath
}

func ioRun(t *testing.T, a *app, ts []*target) {
	t.Helper()
	a.targets = ts
	if err := a.checkVendors(); err != nil {
		t.Fatal(err)
	}
	a.initResults([]string{stageCPU})
	a.runStage(stageCPU)
}

func ioLoadCalls(t *testing.T, logPath string) string {
	b, _ := os.ReadFile(logPath)
	var loads []string
	for _, l := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(l, loadCmd+":") {
			loads = append(loads, l)
		}
	}
	return strings.Join(loads, "\n")
}

func TestIgnoreOfflineOff(t *testing.T) {
	a, f, out, logPath := ioApp(t, "5.00", false)
	ts := []*target{ioTarget("n1"), ioTarget("off1")}
	ioRun(t, a, ts)
	if ts[1].Res[stageCPU] != resOffline || ts[0].Res[stageCPU] != resDone {
		t.Errorf("결과 = %v / %v", ts[0].Res, ts[1].Res)
	}
	if ev := f.launches["/api/v2/job_templates/6/launch/"]; ev["hosts"] != "n1" {
		t.Errorf("launch extra_vars = %v", ev)
	}
	if strings.Contains(ioLoadCalls(t, logPath), "off1") || strings.Contains(out.String(), "감시 제외") {
		t.Errorf("접속불가는 감시·launch 대상이 아니어야 함:\n%s", out.String())
	}
	if hasProblem(ts) {
		t.Error("문제 없음이어야 함")
	}
}

func TestIgnoreOfflineOn(t *testing.T) {
	a, f, out, logPath := ioApp(t, "5.00", true)
	dns := ioTarget("dns1")
	dns.DNSFail = true
	ts := []*target{ioTarget("n1"), ioTarget("off1"), dns}
	ioRun(t, a, ts)
	if ts[0].Res[stageCPU] != resDone || ts[1].Res[stageCPU] != resLaunchedNoMon || ts[2].Res[stageCPU] != resDNSFail {
		t.Errorf("결과 = %v / %v / %v", ts[0].Res, ts[1].Res, ts[2].Res)
	}
	if ev := f.launches["/api/v2/job_templates/6/launch/"]; ev["hosts"] != "n1\noff1" {
		t.Errorf("launch extra_vars = %v", ev)
	}
	if loads := ioLoadCalls(t, logPath); !strings.Contains(loads, "n1") || strings.Contains(loads, "off1") {
		t.Errorf("로드 확인에는 n1 만: %q", loads)
	}
	for _, w := range []string{"접속불가 무시: 접속체크에 응답 없는 1대도 launch", "[감시] cpu 감시 제외(접속불가 무시) 1대"} {
		if !strings.Contains(out.String(), w) {
			t.Errorf("출력에 %q 없음:\n%s", w, out.String())
		}
	}
	if hasProblem(ts) {
		t.Error("감시불가는 미실행이 아님 (종료코드 1 이 되면 안 됨)")
	}
	lines := resultLines(ts)
	if lines[2] != "off1|-|실행(감시불가)|-|접속불가 무시 launch (부하 감시 제외)" {
		t.Errorf("결과표 = %q", lines)
	}
}

// 감시할 호스트가 하나도 없어도 "[감시]" 줄은 나가고, 로드 확인은 부르지 않는다.
func TestIgnoreOfflineOnlyUnmonitored(t *testing.T) {
	a, _, out, logPath := ioApp(t, "5.00", true)
	ts := []*target{ioTarget("off1"), ioTarget("off2")}
	ioRun(t, a, ts)
	if ts[0].Res[stageCPU] != resLaunchedNoMon || ts[1].Res[stageCPU] != resLaunchedNoMon {
		t.Errorf("결과 = %v / %v", ts[0].Res, ts[1].Res)
	}
	if !strings.Contains(out.String(), "\n[감시] cpu 감시 제외(접속불가 무시) 2대") {
		t.Errorf("[감시] 줄 없음:\n%s", out.String())
	}
	if loads := ioLoadCalls(t, logPath); loads != "" {
		t.Errorf("로드 확인을 부르면 안 됨: %q", loads)
	}
}

// 감시 대상이 미실행이면 그것만 재실행하고, 감시불가 호스트는 재실행·미실행 집계에 끼지 않는다.
func TestIgnoreOfflineRetryExcludes(t *testing.T) {
	a, f, _, _ := ioApp(t, "0.10", true)
	a.retry = 1
	ts := []*target{ioTarget("n1"), ioTarget("off1")}
	ioRun(t, a, ts)
	if ts[0].Res[stageCPU] != resNotRun || ts[1].Res[stageCPU] != resLaunchedNoMon {
		t.Errorf("결과 = %v / %v", ts[0].Res, ts[1].Res)
	}
	if ev := f.launches["/api/v2/job_templates/6/launch/"]; ev["hosts"] != "n1" {
		t.Errorf("재실행 대상은 n1 뿐이어야 함: %v", ev)
	}
	if _, err := os.Stat(filepath.Join(os.Getenv("FAKE_REPO"), "hong_retry1_cpu.yaml")); err != nil {
		t.Errorf("재실행 yaml 없음: %v", err)
	}
	if !hasProblem(ts) {
		t.Error("n1 이 미실행이므로 문제 있음")
	}
}

// 묶음 launch 가 실패하면 접속불가 호스트도 다른 호스트와 같은 실패 결과를 받는다.
func TestIgnoreOfflineLaunchFail(t *testing.T) {
	a, f, _, logPath := ioApp(t, "5.00", true)
	f.failCPUJob = true
	ts := []*target{ioTarget("n1"), ioTarget("off1")}
	ioRun(t, a, ts)
	for _, x := range ts {
		if x.Res[stageCPU] != resFail {
			t.Errorf("%s 결과 = %v", x.Host, x.Res)
		}
	}
	if ioLoadCalls(t, logPath) != "" {
		t.Error("launch 실패면 감시 없음")
	}
}
