// done_test.go - 완료기록 인정 규칙 단위 테스트: 부팅 이전·전달 이전 거부(로그 1회), 인정 시 run 제외·applied 이동·중복 실행 없음, 수동 완료
package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func putDone(t *testing.T, host string, epoch int64, src string) {
	t.Helper()
	if err := writeDoneRecord(host, doneRecord{Epoch: epoch, User: "u1", Sha: "abc", Source: src}); err != nil {
		t.Fatal(err)
	}
}

func logCount(t *testing.T, sub string) int {
	t.Helper()
	b, _ := os.ReadFile(logPath())
	return strings.Count(string(b), sub)
}

func TestJudgeDoneRecord(t *testing.T) {
	j := &Job{Submitted: 1000}
	cases := []struct {
		name          string
		rec           doneRecord
		h             Host
		ok, pend      bool
		reasonContain string
	}{
		{"전달 이전", doneRecord{Epoch: 999, Source: "os6"}, Host{SeenDown: true, DownAt: 1100}, false, false, "전달 이전"},
		{"부팅 이전(uptime)", doneRecord{Epoch: 1500, Source: "os6"}, Host{SeenDown: true, DownAt: 1100, BootAt: 1600}, false, false, "부팅 이전"},
		{"부팅 이후", doneRecord{Epoch: 1600, Source: "os6"}, Host{SeenDown: true, DownAt: 1100, BootAt: 1600}, true, false, ""},
		{"uptime 없음 → seen_down 이후", doneRecord{Epoch: 1200, Source: "os6"}, Host{SeenDown: true, DownAt: 1100}, true, false, ""},
		{"seen_down 이전", doneRecord{Epoch: 1050, Source: "os6"}, Host{SeenDown: true, DownAt: 1100}, false, false, "부팅 이전"},
		{"READY 후 재부팅 이전", doneRecord{Epoch: 1700, Source: "os6"}, Host{SeenDown: true, DownAt: 1800, BootAt: 1600}, false, false, "부팅 이전"},
		{"재설치 증거 없음", doneRecord{Epoch: 1200, Source: "os6"}, Host{BootAt: 10}, false, true, ""},
		{"전달 후 부팅 확인(증거)", doneRecord{Epoch: 1300, Source: "os6"}, Host{BootAt: 1200}, true, false, ""},
		{"수동", doneRecord{Epoch: 1001, Source: "manual"}, Host{}, true, false, ""},
		{"수동 전달 이전", doneRecord{Epoch: 900, Source: "manual"}, Host{}, false, false, "전달 이전"},
	}
	for _, c := range cases {
		h := c.h
		ok, pend, reason := judgeDoneRecord(c.rec, j, &h)
		if ok != c.ok || pend != c.pend || !strings.Contains(reason, c.reasonContain) {
			t.Fatalf("%s: ok=%v pend=%v reason=%q", c.name, ok, pend, reason)
		}
	}
	if _, err := parseDoneRecord([]byte("12 u sha\n")); err == nil {
		t.Fatal("필드 3개는 형식 오류")
	}
	if r, err := parseDoneRecord([]byte("12 u sha os6\nextra\n")); err != nil || r.Epoch != 12 || r.Source != "os6" {
		t.Fatalf("파싱 이상: %+v %v", r, err)
	}
}

// 인정: 로컬에서 ssh 가 안 되는 h2 를 타 경로가 먼저 완료 → processed(external), run 대상 제외, applied 로 이동, 중복 실행 없음
func TestDoneRecordAcceptedExcludedFromRun(t *testing.T) {
	setupDir(t)
	x := newX(map[string]*simHost{"h1": install(60, 125, 400, 600), "h2": install(60, 125, 400, 600)})
	x.noSSH["h2"] = true
	id := submit(t, "u1", 0, "h1", "h2")
	d := drive(t, x.world, newXDaemon(x, false, false), 0, 695)
	if len(x.runs) != 0 {
		t.Fatalf("7분 대기 중이어야 함: %+v", x.runs)
	}
	putDone(t, "h2", base+700, "os6")
	drive(t, x.world, d, 700, 1500)
	wantRuns(t, x.world, []runCall{{At: 700, Hosts: []string{"h1"}}})
	j := doneJob(t, id)
	if h := j.Hosts["h2"]; h.Processed != DoneSrcExternal || h.DoneSrc != DoneSrcExternal || h.Stage != string(StageDone) {
		t.Fatalf("h2 인정 이상: %+v", h)
	}
	if h := j.Hosts["h1"]; h.Processed != "0001" || h.DoneSrc != DoneSrcRun {
		t.Fatalf("h1 이상: %+v", h)
	}
	if _, err := os.Stat(filepath.Join(doneRecDir(), "h2")); !os.IsNotExist(err) {
		t.Fatal("인정된 기록이 done/ 에 남음")
	}
	if _, err := os.Stat(filepath.Join(doneAppliedDir(), "h2")); err != nil {
		t.Fatal("done/applied/h2 없음")
	}
	if len(x.walls) != 1 || !strings.Contains(x.walls[0], "\nh1 (1대)\n") {
		t.Fatalf("wall 이상: %q", x.walls)
	}
}

// 거부: 부팅 이전(h1)·전달 이전(h2) 기록은 그대로 두고 로그 1회, 증거 없는 호스트(h3)는 보류(로그 없음)
func TestDoneRecordRejected(t *testing.T) {
	setupDir(t)
	x := newX(map[string]*simHost{"h1": install(60, 125, 400, 600), "h2": install(60, 125, 400, 600), "h3": oldOS()})
	x.noSSH["h2"] = true
	x.runFn = func(n int, o int64, hosts []string) RunResult { return RunResult{Processed: []string{"h1"}} }
	id := submit(t, "u1", 0, "h1", "h2", "h3")
	d := drive(t, x.world, newXDaemon(x, false, false), 0, 605) // h1 READY(boot_at=600)
	putDone(t, "h1", base+300, "os6")
	putDone(t, "h2", base-10, "os6")
	putDone(t, "h3", base+5, "os6")
	d = drive(t, x.world, d, 610, 1100)
	wantRuns(t, x.world, []runCall{{At: 1020, Hosts: []string{"h1", "h2", "h3"}}})
	j := d.jobs[id]
	if j.Hosts["h1"].Processed != "0001" || j.Hosts["h2"].Processed != "" || j.Hosts["h3"].Processed != "" {
		t.Fatalf("processed 이상: %+v %+v %+v", j.Hosts["h1"], j.Hosts["h2"], j.Hosts["h3"])
	}
	for _, h := range []string{"h1", "h2", "h3"} {
		if _, err := os.Stat(filepath.Join(doneRecDir(), h)); err != nil {
			t.Fatalf("거부/보류 기록 %s 이 사라짐", h)
		}
	}
	if n := logCount(t, "완료기록 거부: h1 "); n != 1 || !strings.Contains(mustRead(t, logPath()), "마지막 부팅 이전 기록") {
		t.Fatalf("h1 거부 로그 %d회 (1회여야 함)", n)
	}
	if n := logCount(t, "완료기록 거부: h2 "); n != 1 || !strings.Contains(mustRead(t, logPath()), "전달 이전 기록") {
		t.Fatalf("h2 거부 로그 %d회 (1회여야 함)", n)
	}
	if n := logCount(t, "완료기록 거부: h3"); n != 0 {
		t.Fatalf("h3 는 보류(로그 없음)여야 함: %d", n)
	}
}

// 위조(미래) 시각: 지금+doneFutureSlack 을 넘는 기록은 거부(로그 1회), 인정·이동 없음
func TestDoneRecordFutureRejected(t *testing.T) {
	setupDir(t)
	x := newX(map[string]*simHost{"h1": install(60, 125, 400, 600), "h2": install(60, 125, 400, 600)})
	x.noSSH["h2"] = true
	x.runFn = func(n int, o int64, hosts []string) RunResult { return RunResult{Processed: []string{"h1"}} }
	id := submit(t, "u1", 0, "h1", "h2")
	d := drive(t, x.world, newXDaemon(x, false, false), 0, 695)
	putDone(t, "h2", base+100000, "os6")
	d = drive(t, x.world, d, 700, 1500)
	if j := d.jobs[id]; j == nil || j.Hosts["h2"].Processed != "" {
		t.Fatalf("미래 시각 기록이 인정됨: %+v", j)
	}
	if _, err := os.Stat(filepath.Join(doneRecDir(), "h2")); err != nil {
		t.Fatal("거부된 기록이 사라짐")
	}
	if n := logCount(t, "완료기록 거부: h2 "); n != 1 || !strings.Contains(mustRead(t, logPath()), "미래 시각 기록") {
		t.Fatalf("h2 거부 로그 %d회 (1회여야 함)", n)
	}
}

// 수동 완료: markDone → 출처 manual, 부팅 검증 없이 인정 + 경고 로그
func TestMarkDoneManual(t *testing.T) {
	setupDir(t)
	x := newX(map[string]*simHost{"h1": oldOS(), "h2": oldOS()})
	id := submit(t, "u1", 0, "h1", "h2")
	d := drive(t, x.world, newXDaemon(x, false, false), 0, 10)
	if err := markDone([]string{"h1"}, DoneSrcManual); err != nil {
		t.Fatal(err)
	}
	if err := markDone([]string{"../x"}, DoneSrcManual); err == nil {
		t.Fatal("잘못된 호스트명은 에러")
	}
	d = drive(t, x.world, d, 15, 20)
	if h := d.jobs[id].Hosts["h1"]; h.Processed != DoneSrcManual || h.DoneSrc != DoneSrcManual {
		t.Fatalf("수동 완료 이상: %+v", h)
	}
	if logCount(t, "수동 완료 처리(부팅 시각 검증 없음): h1") != 1 || logCount(t, "수동 완료 요청") != 1 {
		t.Fatalf("경고 로그 없음:\n%s", mustRead(t, logPath()))
	}
	if len(x.runs) != 0 {
		t.Fatalf("run 이 실행됨: %+v", x.runs)
	}
}

func mustRead(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
