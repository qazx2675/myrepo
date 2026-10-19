// os6_parity_test.go - v0.8.7 os6 경유 호스트가 os8 직접 호스트와 같은 흐름으로 처리되는지:
// ping 정보 없음에도 진행, os8 이름 해석 실패 → os6 경유, os6 probe 세션 재시작 때 상태 유지, probe 원격 경로(디렉터리·파일·빈 값),
// gossh 래퍼의 결과 파일 반환·경로별 분리 실행(os8 호스트는 이 서버 gossh, os6 호스트만 os6_mgmt).
package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

// os6 경로로 판별된 뒤 os6 probe 정보가 끊겨도(Known=false) 준비확인으로 설치중·READY 를 판정해 run·완료까지 간다
func TestOS6NoPingStillCompletes(t *testing.T) {
	setupDir(t)
	withRoute6(t, "mgmt", "/os6/gossh")
	h1 := install(60, 120, 400, 600)
	h1.remote = true
	w := newWorld(map[string]*simHost{"h1": h1})
	id := submit(t, "u1", 0, "h1")
	d := drive(t, w, newTestDaemon(w), 0, 30)
	if r := d.jobs[id].Hosts["h1"].Route; r != "os6" {
		t.Fatalf("os6 경로 판별: %q", r)
	}
	w.mu.Lock()
	h1.unknown = true // 이후 os6 probe 정보 없음
	w.mu.Unlock()
	drive(t, w, d, 35, 1200)
	j := doneJob(t, id)
	if j.Hosts["h1"].Processed == "" {
		h := j.Hosts["h1"]
		t.Fatalf("ping 정보 없이도 완료돼야 함: stage=%s ready=%d checks=%v", h.Stage, h.ReadyAt, w.checks)
	}
	if len(w.runs) != 1 || !w.runs[0].OS6 {
		t.Fatalf("os6 포함 run 1회: %+v", w.runs)
	}
}

// os8_mgmt 에서 이름이 계속 안 풀리는 호스트는 os6 경유로 감시·체크 (IP 없이 이름으로)
func TestUnresolvableOnOS8GoesViaOS6(t *testing.T) {
	setupDir(t)
	withRoute6(t, "mgmt", "/os6/gossh")
	h1 := install(60, 120, 400, 600)
	h1.remote = true
	w := newWorld(map[string]*simHost{"h1": h1, "h2": install(60, 120, 400, 600)})
	w.lookupFail["h1"] = 1 << 30
	id := submit(t, "u1", 0, "h1", "h2")
	d := drive(t, w, newTestDaemon(w), 0, 15)
	if h := d.jobs[id].Hosts["h1"]; h.Route != "" {
		t.Fatalf("일시 실패(2회)까지는 재시도: %+v", h)
	}
	d = drive(t, w, d, 20, 1500)
	j := doneJob(t, id)
	if h := j.Hosts["h1"]; h.Route != "os6" || h.IP != "" || h.Processed == "" {
		t.Fatalf("이름 해석 실패 호스트도 os6 경유로 완료돼야 함: %+v", h)
	}
	if h := j.Hosts["h2"]; h.Route != "local" || h.Processed == "" {
		t.Fatalf("일반 호스트: %+v", h)
	}
}

// ping 2주기보다 빠른 재부팅(down 을 못 봄)도 waitCheck 확인으로 READY → 완료 (os8·os6 경로 같게)
func TestMissedDownStillCompletes(t *testing.T) {
	setupDir(t)
	withRoute6(t, "mgmt", "/os6/gossh")
	fast := func(remote bool) *simHost {
		return &simHost{dd: 100, ana: never, reboot: never, boot: 104, remote: remote} // 100~104 초만 down
	}
	w := newWorld(map[string]*simHost{"l1": fast(false), "r6": fast(true)})
	id := submit(t, "u1", 0, "l1", "r6")
	drive(t, w, newTestDaemon(w), 0, 900)
	j := doneJob(t, id)
	for _, n := range []string{"l1", "r6"} {
		if h := j.Hosts[n]; h.Processed == "" || h.SeenDown {
			t.Fatalf("%s: down 을 못 봤어도 완료돼야 함: %+v", n, h)
		}
	}
	if j.Hosts["r6"].Route != "os6" || j.Hosts["l1"].Route != "local" {
		t.Fatalf("경로: %+v", j.Hosts)
	}
}

// os6 probe 세션이 목록 변경으로 재시작돼도 남는 호스트는 마지막 상태를 바로 돌려준다
func TestOS6SessionsKeepStateOnRestart(t *testing.T) {
	withFakeSSH(t, fakeProbeScript)
	s := newTestSessions()
	defer s.Close()
	ts := []PingTarget{{Host: "h1", IP: "10.0.0.1", Route: "os6"}, {Host: "h2", IP: "10.0.0.2", Route: "os6"}}
	s.Ping(ts)
	waitFor(t, "상태 수신", func() bool { return s.Ping(ts)["h2"].Known })
	ts2 := append(ts, PingTarget{Host: "h3", IP: "10.0.0.3", Route: "os6"})
	r := s.Ping(ts2) // 재시작 직후
	if r["h1"] != (PingResult{Up: true, Known: true}) || r["h2"] != (PingResult{Up: false, Known: true}) {
		t.Fatalf("재시작 직후 남는 호스트 상태 유지: %+v", r)
	}
	if r["h3"].Known {
		t.Fatal("새 호스트는 첫 응답 전까지 Known=false")
	}
}

// probe 원격 명령: os6_autosetup 이 디렉터리·실행 파일 경로·빈 값(PATH) 어느 것이어도 auto_setup probe 를 실행
func TestProbeRemotePathForms(t *testing.T) {
	needBash(t)
	tmp := t.TempDir()
	bin := filepath.Join(tmp, "auto_setup")
	os.WriteFile(bin, []byte("#!/bin/bash\necho \"RUN $*\"\n"), 0755)
	for _, dir := range []string{tmp, tmp + "/", bin, ""} {
		remote := probeRemote(dir, 10*time.Second)
		cmd := exec.Command("bash", "-c", remote)
		cmd.Env = append(os.Environ(), "PATH="+tmp+string(os.PathListSeparator)+os.Getenv("PATH"))
		out, err := cmd.CombinedOutput()
		if err != nil || strings.TrimSpace(string(out)) != "RUN probe -i 10s" {
			t.Errorf("dir=%q: %v %q", dir, err, out)
		}
	}
}

// gossh 래퍼 테스트 환경: 이 서버의 gossh(local)·os6_mgmt 의 gossh(remote)·ssh(로컬에서 bash 로 실행) 가짜.
// 가짜 gossh 는 목록의 호스트마다 "<host>: <이름> ok" 를 내고, off 로 시작하는 호스트는 <목록>_res_off 에 적는다.
func wrapperRig(t *testing.T) (tmp, fakeBin, localG, logDir string) {
	t.Helper()
	needBash(t)
	setupDir(t)
	oldM, oldG := os6_mgmt, os6_gossh
	t.Cleanup(func() { os6_mgmt, os6_gossh = oldM, oldG })
	tmp = t.TempDir()
	logDir = filepath.Join(tmp, "log")
	os.MkdirAll(logDir, 0755)
	fake := func(name string) string {
		return `#!/bin/bash
prev=
for a in "$@"; do
  if [ "$prev" = "-w" ]; then list=$a; fi
  prev=$a
done
cp "$list" '` + logDir + `/` + name + `.list'
: > "${list}_res_off"
while read -r h; do
  [ -z "$h" ] && continue
  case "$h" in off*) echo "$h" >> "${list}_res_off" ;; *) echo "$h: ` + name + ` ok" ;; esac
done < "$list"
echo "` + name + ` 요약 줄" >&2
exit 0
`
	}
	localG = filepath.Join(tmp, "local_gossh")
	os.WriteFile(localG, []byte(fake("local")), 0755)
	remoteG := filepath.Join(tmp, "remote gossh")
	os.WriteFile(remoteG, []byte(fake("remote")), 0755)
	fakeBin = filepath.Join(tmp, "bin")
	os.MkdirAll(fakeBin, 0755)
	os.WriteFile(filepath.Join(fakeBin, "ssh"), []byte("#!/bin/bash\nwhile [ \"$1\" = -o ]; do shift 2; done\nshift\nexec bash -c \"$1\"\n"), 0755)
	os6_mgmt, os6_gossh = "mgmtstub", remoteG
	return
}

func runWrapper(t *testing.T, script, fakeBin string, env []string, args ...string) (string, string) {
	t.Helper()
	w := filepath.Join(binDir(), "gossh")
	if err := os.WriteFile(w, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("bash", append([]string{w}, args...)...)
	cmd.Env = append(append(os.Environ(), "PATH="+fakeBin+string(os.PathListSeparator)+os.Getenv("PATH")), env...)
	var o, e strings.Builder
	cmd.Stdout, cmd.Stderr = &o, &e
	if err := cmd.Run(); err != nil {
		t.Fatalf("래퍼 실패: %v\n%s\n%s", err, o.String(), e.String())
	}
	return o.String(), e.String()
}

func sortedLines(s string) string {
	ls := strings.Split(strings.TrimSpace(s), "\n")
	sort.Strings(ls)
	return strings.Join(ls, ",")
}

// 경로 정보가 없는 기존 형태(전부 os6_mgmt 경유)도 결과 파일(<목록>_res_off)을 os8 쪽 목록 옆으로 가져온다
func TestOS6WrapperReturnsResultFiles(t *testing.T) {
	tmp, fakeBin, _, _ := wrapperRig(t)
	os.MkdirAll(binDir(), 0755)
	list := filepath.Join(tmp, "targets.txt")
	os.WriteFile(list, []byte("h1\noff1\nh2\n"), 0644)
	out, _ := runWrapper(t, os6WrapperScript(""), fakeBin, nil, "-pm", "-script", "-w", list, "bash run.sh pd; :")
	if sortedLines(out) != "h1: remote ok,h2: remote ok" {
		t.Fatalf("출력(표지줄·결과 내용이 섞이면 안 됨):\n%s", out)
	}
	if b, err := os.ReadFile(list + "_res_off"); err != nil || string(b) != "off1\n" {
		t.Fatalf("결과 파일을 가져와야 함: %q %v", b, err)
	}
}

// AUTO_SETUP_OS6_HOSTS 가 있으면 os8 호스트는 이 서버의 gossh, os6 호스트만 os6_mgmt 경유 — 출력·결과 파일은 합친다
func TestOS6WrapperSplitsByRoute(t *testing.T) {
	tmp, fakeBin, localG, logDir := wrapperRig(t)
	os.MkdirAll(binDir(), 0755)
	list := filepath.Join(tmp, "targets.txt")
	os.WriteFile(list, []byte("a1\nb6\noffa\noffb6\na2\n"), 0644)
	o6 := filepath.Join(tmp, "os6_hosts.txt")
	os.WriteFile(o6, []byte("b6\noffb6\n"), 0644)
	out, errOut := runWrapper(t, os6WrapperScript(localG), fakeBin, []string{"AUTO_SETUP_OS6_HOSTS=" + o6}, "-pm", "-w", list, "uptime")
	if sortedLines(out) != "a1: local ok,a2: local ok,b6: remote ok" {
		t.Fatalf("경로별 실행 결과:\n%s", out)
	}
	if !strings.Contains(errOut, "local 요약 줄") || !strings.Contains(errOut, "remote 요약 줄") {
		t.Errorf("stderr 도 둘 다: %q", errOut)
	}
	lb, _ := os.ReadFile(filepath.Join(logDir, "local.list"))
	rb, _ := os.ReadFile(filepath.Join(logDir, "remote.list"))
	if string(lb) != "a1\noffa\na2\n" || string(rb) != "b6\noffb6\n" {
		t.Fatalf("나뉜 목록: local=%q remote=%q", lb, rb)
	}
	if b, _ := os.ReadFile(list + "_res_off"); sortedLines(string(b)) != "offa,offb6" {
		t.Fatalf("결과 파일 합치기: %q", b)
	}
	// os6 호스트가 없으면 이 서버의 gossh 만 (원래 인자 그대로)
	os.WriteFile(o6, []byte("\n"), 0644)
	out, _ = runWrapper(t, os6WrapperScript(localG), fakeBin, []string{"AUTO_SETUP_OS6_HOSTS=" + o6}, "-pm", "-w", list, "uptime")
	if !strings.Contains(out, "b6: local ok") || strings.Contains(out, "remote") {
		t.Fatalf("os6 호스트 없음 → 전부 로컬:\n%s", out)
	}
	// 전부 os6 호스트
	os.WriteFile(o6, []byte("a1\nb6\noffa\noffb6\na2\n"), 0644)
	out, _ = runWrapper(t, os6WrapperScript(localG), fakeBin, []string{"AUTO_SETUP_OS6_HOSTS=" + o6}, "-pm", "-w", list, "uptime")
	if strings.Contains(out, "local") || !strings.Contains(out, "a1: remote ok") {
		t.Fatalf("전부 os6 → 전부 원격:\n%s", out)
	}
}

// Runner 는 os6 호스트가 있는 run 에 이번 run 의 os6 경로 호스트 목록을 래퍼로 넘긴다
func TestRunnerPassesOS6HostList(t *testing.T) {
	needBash(t)
	setupDir(t)
	withRoute6(t, "mgmtstub", "/os6/gossh")
	oldC := os_check_sh
	t.Cleanup(func() { os_check_sh = oldC })
	tmp := t.TempDir()
	os_check_sh = filepath.Join(tmp, "oc.sh")
	os.WriteFile(os_check_sh, []byte("#!/bin/bash\necho \"O6=$AUTO_SETUP_OS6_HOSTS\"\ncat \"$AUTO_SETUP_OS6_HOSTS\"\ncommand -v gossh\n"), 0755)
	j := &Job{ID: "j1", User: "u1", Hosts: map[string]*Host{"a1": {Route: "local"}, "b6": {Route: "os6"}, "c6": {Route: "os6"}}}
	res, err := NewRealRunner().Run(j, []string{"a1", "b6", "c6"}, true)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(runsDir(), res.Code, "os_check.log"))
	log := string(b)
	if !strings.Contains(log, "O6="+filepath.Join(runsDir(), res.Code, "os6_hosts.txt")) || !strings.Contains(log, "b6\nc6\n") ||
		!strings.Contains(log, filepath.Join(binDir(), "gossh")) {
		t.Fatalf("os6 목록·래퍼 전달:\n%s", log)
	}
}

// 재점검 결과에 "host: ERROR: SSH 접속 실패" 만 있는 호스트는 처리(완료)로 치지 않는다 (config_check 완료기록 규칙과 같게)
func TestPostapplyErrorHostNotProcessed(t *testing.T) {
	post := "a1: OK hostname a1\noff1: ERROR: SSH 접속 실패: dial tcp 10.0.0.9:22: connect: no route to host\nb6: OK x\nb6: ERROR: 종료코드 1\n"
	if got := parsePostapplyHosts(post, []string{"a1", "off1", "b6"}); strings.Join(got, ",") != "a1" {
		t.Fatalf("ERROR 호스트는 제외: %v", got)
	}
}

// 준비확인 해석: 설치 기록(anaconda-ks.cfg)이 uptime 보다 48시간 넘게 오래되면 기존 OS, 방금 설치면 새 OS, 파일 없으면 판단 안 함
func TestParseCheckOldOS(t *testing.T) {
	now := int64(1760000000)
	out := strings.Join([]string{
		"old1: 120.5 300.1", "old1: " + fmt.Sprint(now), "old1: " + fmt.Sprint(now-90*86400),
		"new1: 120.5 300.1", "new1: " + fmt.Sprint(now), "new1: " + fmt.Sprint(now-400),
		"tz1: 120.5 300.1", "tz1: " + fmt.Sprint(now), "tz1: " + fmt.Sprint(now-9*3600-400), // 설치기·OS 시간대 차이 9시간
		"nof: 120.5 300.1", "nof: " + fmt.Sprint(now), "nof: stat: cannot stat '/root/anaconda-ks.cfg': No such file or directory",
	}, "\n")
	res := parseCheck([]string{"old1", "new1", "tz1", "nof"}, out, "")
	if !res["old1"].OldOS || res["new1"].OldOS || res["tz1"].OldOS || res["nof"].OldOS {
		t.Fatalf("OldOS 판정: %+v", res)
	}
	for _, h := range []string{"old1", "new1", "tz1", "nof"} {
		if !res[h].Responded || res[h].Uptime != 120.5 {
			t.Fatalf("%s uptime: %+v", h, res[h])
		}
	}
}
