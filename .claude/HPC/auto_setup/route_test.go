package main

import (
	"reflect"
	"strings"
	"testing"
)

func withRoute6(t *testing.T, mgmt, gossh string) {
	t.Helper()
	oldM, oldG := os6_mgmt, os6_gossh
	os6_mgmt, os6_gossh = mgmt, gossh
	t.Cleanup(func() { os6_mgmt, os6_gossh = oldM, oldG })
}

// route=local 로 정해진 뒤 os8 에서 안 보이게 된 호스트(설치 중 망 변경, os6_mgmt 설정 전 판별 등)
// → 로컬 무응답이면 os6 에도 물어 응답 시 route=os6 로 전환, 이후 일반 흐름(설치중→READY→run)과 같게 진행
func TestLocalDownFallsBackToOS6Ping(t *testing.T) {
	setupDir(t)
	withRoute6(t, "mgmt", "")
	h1 := install(60, 120, 400, 600)
	w := newWorld(map[string]*simHost{"h1": h1})
	id := submit(t, "u1", 0, "h1")
	d := drive(t, w, newTestDaemon(w), 0, 15)
	if r := d.jobs[id].Hosts["h1"].Route; r != "local" {
		t.Fatalf("처음엔 로컬 응답 → local 이어야 함: %q", r)
	}
	h1.remote = true // 이후 os8 에서 ping·ssh 불가
	d = drive(t, w, d, 20, 125)
	h := d.jobs[id].Hosts["h1"]
	if h.Route != "os6" || Stage(h.Stage) != StageInstalling || !h.SeenDown {
		t.Fatalf("os6 전환 후 설치중이어야 함: %+v", h)
	}
	drive(t, w, d, 130, 1100)
	if len(w.runs) != 1 || !w.runs[0].OS6 || !reflect.DeepEqual(w.runs[0].Hosts, []string{"h1"}) {
		t.Fatalf("os6 경유 run 1회여야 함: %+v", w.runs)
	}
	if j := doneJob(t, id); j.Hosts["h1"].Processed != "0001" {
		t.Fatalf("처리 안 됨: %+v", j.Hosts["h1"])
	}
}

// os6_mgmt 가 비어 있으면 종전과 같음: 로컬 무응답은 그냥 down (both 질의 없음)
func TestNoOS6KeepsLocalOnly(t *testing.T) {
	setupDir(t)
	withRoute6(t, "", "")
	h1 := install(60, 120, 400, 600)
	w := newWorld(map[string]*simHost{"h1": h1})
	id := submit(t, "u1", 0, "h1")
	d := drive(t, w, newTestDaemon(w), 0, 15)
	h1.remote = true
	d = drive(t, w, d, 20, 200)
	h := d.jobs[id].Hosts["h1"]
	if h.Route != "local" || Stage(h.Stage) != StageDeploying {
		t.Fatalf("os6 없으면 local 유지 + 배포중이어야 함: %+v", h)
	}
}

// ping 은 되지만 os8 에서 준비확인(gossh)이 안 되는 호스트 → 같은 주기에 os6 경유 재확인, 응답하면 route=os6
func TestCheckFallsBackToOS6(t *testing.T) {
	setupDir(t)
	withRoute6(t, "mgmt", "/os6/gossh")
	h1 := install(60, 120, 400, 600)
	h1.noLocalSSH = true
	w := newWorld(map[string]*simHost{"h1": h1, "h2": install(60, 120, 400, 600)})
	id := submit(t, "u1", 0, "h1", "h2")
	d := drive(t, w, newTestDaemon(w), 0, 125)
	if want := []string{"120 local h1,h2", "120 os6 h1"}; !reflect.DeepEqual(w.checks, want) {
		t.Fatalf("로컬 무응답만 os6 재확인: %v", w.checks)
	}
	if j := d.jobs[id]; j.Hosts["h1"].Route != "os6" || j.Hosts["h2"].Route != "local" {
		t.Fatalf("route: h1=%q h2=%q", j.Hosts["h1"].Route, j.Hosts["h2"].Route)
	}
	drive(t, w, d, 130, 1100)
	if len(w.runs) != 1 || !w.runs[0].OS6 {
		t.Fatalf("os6 포함 run 이어야 함: %+v", w.runs)
	}
	j := doneJob(t, id)
	if j.Hosts["h1"].Processed == "" || j.Hosts["h2"].Processed == "" {
		t.Fatalf("둘 다 처리돼야 함: %+v %+v", j.Hosts["h1"], j.Hosts["h2"])
	}
}

// os6_gossh 가 비면 준비확인 대체 없음 (종전 동작)
func TestCheckNoFallbackWithoutGossh(t *testing.T) {
	setupDir(t)
	withRoute6(t, "mgmt", "")
	h1 := install(60, 120, 400, 600)
	h1.noLocalSSH = true
	w := newWorld(map[string]*simHost{"h1": h1})
	submit(t, "u1", 0, "h1")
	drive(t, w, newTestDaemon(w), 0, 125)
	if want := []string{"120 local h1"}; !reflect.DeepEqual(w.checks, want) {
		t.Fatalf("대체 확인이 없어야 함: %v", w.checks)
	}
}

// refresh 요청 → 주기와 관계없이 그 step 에서 ping·준비확인 수행, 요청 파일은 남지 않음
func TestRefreshRequestRunsNow(t *testing.T) {
	setupDir(t)
	w := newWorld(map[string]*simHost{"h1": install(60, 120, 400, 600)})
	submit(t, "u1", 0, "h1")
	d := drive(t, w, newTestDaemon(w), 0, 130)
	n := len(w.checks)
	if _, err := writeRequest(ReqRefresh, nil); err != nil {
		t.Fatal(err)
	}
	d = drive(t, w, d, 135, 135) // 정규 준비확인은 150
	if len(w.checks) != n+1 || !strings.HasPrefix(w.checks[n], "135 ") {
		t.Fatalf("refresh 즉시 준비확인 없음: %v", w.checks[n:])
	}
	if d.lastPing.Unix() != base+135 {
		t.Fatalf("refresh 즉시 ping 없음: %v", d.lastPing)
	}
	if fs := listReqFiles(requestsDir()); len(fs) != 0 {
		t.Fatalf("요청 파일 남음: %v", fs)
	}
}

func TestMergeBoth(t *testing.T) {
	cases := []struct {
		loc, o6, want PingResult
	}{
		{PingResult{Up: true, Known: true}, PingResult{Known: true}, PingResult{Up: true, Known: true, Via: "local"}},
		{PingResult{Known: true}, PingResult{Up: true, Known: true}, PingResult{Up: true, Known: true, Via: "os6"}},
		{PingResult{Known: true}, PingResult{Known: true}, PingResult{Known: true, Via: "os6"}},
		{PingResult{Known: true}, PingResult{}, PingResult{Known: true, Via: "local"}},
		{PingResult{}, PingResult{}, PingResult{Via: "local"}},
	}
	for i, c := range cases {
		if got := mergeBoth(c.loc, c.o6); got != c.want {
			t.Errorf("%d: got %+v want %+v", i, got, c.want)
		}
	}
}

// ping 정보 없음이 1분 이상 → 비고 표시 (os6 경로면 probe 확인 안내)
func TestNoPingNote(t *testing.T) {
	now := int64(10000)
	h := &Host{IP: "x", Route: "os6", SeenDown: true, Stage: string(StageDeploying), NoPing: now - 59}
	if n := snapHost("h1", h, now).Note; strings.Contains(n, "ping 정보없음") {
		t.Fatalf("1분 전엔 표시 안 함: %q", n)
	}
	h.NoPing = now - 60
	if n := snapHost("h1", h, now).Note; n != "os6경유, ping 정보없음(os6 probe 확인)" {
		t.Fatalf("비고: %q", n)
	}
}

// Known=false 가 이어지면 no_ping 기록, 정보가 오면 0
func TestNoPingRecorded(t *testing.T) {
	setupDir(t)
	s := install(60, 120, 400, 600)
	s.unknown = true
	w := newWorld(map[string]*simHost{"h1": s})
	id := submit(t, "u1", 0, "h1")
	d := drive(t, w, newTestDaemon(w), 0, 20)
	if h := d.jobs[id].Hosts["h1"]; h.NoPing != base {
		t.Fatalf("no_ping=%d", h.NoPing)
	}
	s.unknown = false
	d = drive(t, w, d, 25, 30)
	if h := d.jobs[id].Hosts["h1"]; h.NoPing != 0 {
		t.Fatalf("정보가 오면 0: %d", h.NoPing)
	}
}

// TUI r: refresh 요청 1회 (연타는 refreshReqHold 안에서 생략)
func TestTUIRefreshKeyRequests(t *testing.T) {
	src := &fakeSrc{snap: twoGroupSnap(), local: true}
	runKeys(t, src, "rr")
	if len(src.reqs) != 1 || src.reqs[0].kind != ReqRefresh {
		t.Fatalf("refresh 요청 1회여야 함: %+v", src.reqs)
	}
}

func TestRequestRefreshArgs(t *testing.T) {
	if a, err := requestArgs(ReqRefresh, nil); err != nil || !reflect.DeepEqual(a, []string{"request", "refresh"}) {
		t.Fatalf("requestArgs: %v %v", a, err)
	}
	if !relayArgsOK([]string{"request", "refresh"}) || relayArgsOK([]string{"request", "refresh", "x"}) {
		t.Fatal("relayArgsOK refresh")
	}
}

// wall 로케일: C 로케일(cron 데몬)이면 ko_KR.UTF-8 로 강제, 이미 UTF-8 이면 유지
func TestWallLocale(t *testing.T) {
	get := func(env []string, k string) string { return envValue(env, k) }
	c := wallLocale([]string{"PATH=/bin", "LANG=C", "LC_ALL=POSIX"})
	if get(c, "LANG") != "ko_KR.UTF-8" || get(c, "LC_ALL") != "ko_KR.UTF-8" || get(c, "PATH") != "/bin" {
		t.Fatalf("C 로케일 → ko_KR.UTF-8: %v", c)
	}
	if e := wallLocale([]string{"PATH=/bin"}); get(e, "LC_ALL") != "ko_KR.UTF-8" {
		t.Fatalf("LANG 없음: %v", e)
	}
	u := wallLocale([]string{"LANG=en_US.UTF-8"})
	if get(u, "LC_ALL") != "en_US.UTF-8" || get(u, "LANG") != "en_US.UTF-8" {
		t.Fatalf("UTF-8 유지: %v", u)
	}
	n := 0
	for _, e := range wallLocale([]string{"LANG=C", "LC_CTYPE=C"}) {
		if strings.HasPrefix(e, "LANG=") || strings.HasPrefix(e, "LC_ALL=") || strings.HasPrefix(e, "LC_CTYPE=") {
			n++
		}
	}
	if n != 2 {
		t.Fatalf("중복 로케일 변수: %d", n)
	}
}

func TestUnreachableLine(t *testing.T) {
	if unreachableLine(nil) != "" {
		t.Fatal("없으면 빈 문자열")
	}
	if got := unreachableLine([]string{"a", "b"}); got != "[!] 접속불가·미응답 2대: a b\n" {
		t.Fatalf("%q", got)
	}
}

// 기본 알림은 끔 (wall 을 보내지 않음)
func TestDefaultNotifierIsNop(t *testing.T) {
	if _, ok := newNotifier().(nopNotifier); !ok {
		t.Fatalf("기본 Notifier 가 nop 이 아님: %T", newNotifier())
	}
	newNotifier().Wall("무시됨") // 패닉·부작용 없음
}

// 수동 run 전 경로 재판별: os8(local) 무응답·os6 응답 호스트는 route=os6 로 바뀌어 run 이 os6 래퍼를 쓴다
func TestManualRunRedetectsOS6Route(t *testing.T) {
	setupDir(t)
	withRoute6(t, "mgmt", "/os6/gossh")
	h1 := install(60, 120, 400, 600)
	h1.noLocalSSH = true // ping 은 되지만 os8 에서 gossh 무응답 (경로가 local 로 남은 상태)
	w := newWorld(map[string]*simHost{"h1": h1, "h2": oldOS()})
	id := submit(t, "u1", 0, "h1", "h2")
	d := drive(t, w, newTestDaemon(w), 0, 5)
	if r := d.jobs[id].Hosts["h1"].Route; r != "local" {
		t.Fatalf("사전 조건: route=local 이어야 함: %q", r)
	}
	req(t, ReqManualRun, map[string]string{"jobid": id, "yml": allGroup})
	d = drive(t, w, d, 10, 20)
	if len(w.runs) != 1 || !w.runs[0].OS6 {
		t.Fatalf("수동 run 이 os6 경유로 실행되어야 함: %+v", w.runs)
	}
	if r := d.jobs[id].Hosts["h1"].Route; r != "os6" {
		t.Fatalf("h1 route=os6 로 전환되어야 함: %q", r)
	}
	if r := d.jobs[id].Hosts["h2"].Route; r != "local" {
		t.Fatalf("로컬 응답 호스트는 local 유지: %q", r)
	}
}

// os6 설정이 없으면 수동 run 전 확인을 하지 않는다 (기존 동작)
func TestManualRunNoRedetectWithoutOS6(t *testing.T) {
	setupDir(t)
	withRoute6(t, "", "")
	h1 := install(60, 120, 400, 600)
	w := newWorld(map[string]*simHost{"h1": h1})
	id := submit(t, "u1", 0, "h1")
	d := drive(t, w, newTestDaemon(w), 0, 5)
	n := len(w.checks)
	req(t, ReqManualRun, map[string]string{"jobid": id, "yml": allGroup})
	drive(t, w, d, 10, 20)
	if len(w.runs) != 1 || w.runs[0].OS6 || len(w.checks) != n {
		t.Fatalf("os6 미설정이면 추가 확인·os6 경유 없음: runs=%+v checks=%v", w.runs, w.checks[n:])
	}
}

// 설치 중 os6 로 붙은 뒤 os8 에서도 직접 응답하는 호스트: 수동 run 전에 route=local 로 되돌려 os6 래퍼를 쓰지 않는다
func TestManualRunBackToLocalRoute(t *testing.T) {
	setupDir(t)
	withRoute6(t, "mgmt", "/os6/gossh")
	w := newWorld(map[string]*simHost{"h1": install(60, 120, 400, 600)})
	id := submit(t, "u1", 0, "h1")
	d := drive(t, w, newTestDaemon(w), 0, 5)
	d.jobs[id].Hosts["h1"].Route = "os6" // 이전에 os6 로 판별되어 남은 상태
	req(t, ReqManualRun, map[string]string{"jobid": id, "yml": allGroup})
	d = drive(t, w, d, 10, 20)
	if len(w.runs) != 1 || w.runs[0].OS6 {
		t.Fatalf("os8 에서 응답하면 os6 래퍼 없이 실행되어야 함: %+v", w.runs)
	}
	if r := d.jobs[id].Hosts["h1"].Route; r != "local" {
		t.Fatalf("route=local 로 되돌아가야 함: %q", r)
	}
}
