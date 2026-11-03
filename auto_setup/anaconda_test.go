// anaconda_test.go - v0.7.1: ping X 를 못 봤어도 준비확인에서 anaconda 가 보이면 설치중으로 올린다 (applyCheck, g 재확인)
package main

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// 대기(SeenDown=false) 호스트 1대를 만들어 둔 데몬. 호스트는 처음엔 기존 OS 로 떠 있다.
func queuedDaemon(t *testing.T, h *simHost) (*world, *Daemon, string) {
	t.Helper()
	setupDir(t)
	withRoute6(t, "", "")
	w := newWorld(map[string]*simHost{"h1": h})
	id := submit(t, "u1", 0, "h1")
	d := drive(t, w, newTestDaemon(w), 0, 0)
	hh := d.jobs[id].Hosts["h1"]
	if hh.SeenDown || hh.DownAt != 0 || effectiveStage(hh, base) != StageQueued {
		t.Fatalf("전제: 대기 호스트여야 함: %+v", hh)
	}
	return w, d, id
}

// (a) 대기 호스트에 anaconda 응답 → 설치중, SeenDown=true, DownAt 설정
func TestApplyCheckAnacondaWithoutSeenDown(t *testing.T) {
	_, d, id := queuedDaemon(t, oldOS())
	j := d.jobs[id]
	at := base + 30
	d.applyCheck(j, "h1", CheckResult{Responded: true, Anaconda: true, Uptime: 12}, at)
	h := j.Hosts["h1"]
	if !h.SeenDown || h.DownAt != at || Stage(h.Stage) != StageInstalling || effectiveStage(h, at) != StageInstalling {
		t.Fatalf("anaconda → 설치중·SeenDown·DownAt 이어야 함: %+v", h)
	}
	if !d.dirty[id] {
		t.Error("변경 후 job 이 dirty 여야 함 (저장)")
	}
	// 이미 SeenDown 인 호스트는 DownAt 을 덮어쓰지 않는다 (재부팅 ping X 동안 anaconda 가 다시 보여도)
	d.applyCheck(j, "h1", CheckResult{Responded: true, Anaconda: true, Uptime: 40}, at+60)
	if h.DownAt != at {
		t.Errorf("DownAt 이 바뀌면 안 됨: %d (기대 %d)", h.DownAt, at)
	}
}

// (b) 같은 호스트에 일반 응답(uptime 김) → 대기 유지
func TestApplyCheckNormalResponseKeepsQueued(t *testing.T) {
	_, d, id := queuedDaemon(t, oldOS())
	j := d.jobs[id]
	d.applyCheck(j, "h1", CheckResult{Responded: true, Uptime: 9e6}, base+30)
	h := j.Hosts["h1"]
	if h.SeenDown || h.DownAt != 0 || effectiveStage(h, base+30) != StageQueued {
		t.Fatalf("일반 응답은 대기 유지여야 함: %+v", h)
	}
}

// (c) g 재확인: anaconda 응답이면 Detail 에 `단계 대기 → 설치중`
func TestRecheckAnacondaShowsStageChange(t *testing.T) {
	w, d, id := queuedDaemon(t, install(10, 12, 400, 600)) // 0 초에는 기존 OS, 15 초에는 anaconda
	now := time.Unix(base+15, 0)
	w.clock.Set(now)
	if r := d.recheckHosts(id, allView, now); r != "" {
		t.Fatalf("재확인 거부: %q", r)
	}
	rc := readRecheck(dataDir(), id)
	if rc == nil || !rc.Done || len(rc.Hosts) != 1 || rc.Hosts[0].Result != "os8" ||
		!strings.Contains(rc.Hosts[0].Detail, "단계 대기 → 설치중") {
		t.Fatalf("재확인 기록: %+v", rc)
	}
	if h := d.jobs[id].Hosts["h1"]; !h.SeenDown || Stage(h.Stage) != StageInstalling {
		t.Fatalf("재확인 후 설치중이어야 함: %+v", h)
	}
	// 단계가 안 바뀌면 종전 표기 (단계 설치중)
	now = time.Unix(base+20, 0)
	w.clock.Set(now)
	d.recheckHosts(id, allView, now)
	rc = readRecheck(dataDir(), id)
	if got := rc.Hosts[0].Detail; got != "단계 설치중" {
		t.Fatalf("변화 없으면 `단계 설치중`: %q", got)
	}
}

// (d) 종료된 job 은 anaconda 가 보여도 단계 불변 (경로만 갱신)
func TestRecheckAnacondaClosedJobKeepsStage(t *testing.T) {
	setupDir(t)
	withRoute6(t, "", "")
	h1 := install(60, 120, 400, 600)
	w := newWorld(map[string]*simHost{"h1": h1})
	id := submit(t, "u1", 0, "h1")
	d := drive(t, w, newTestDaemon(w), 0, 1200)
	j := doneJob(t, id)
	hh := j.Hosts["h1"]
	hh.Processed, hh.SeenDown, hh.DownAt, hh.Stage, hh.ReadyAt = "", false, 0, string(StageQueued), 0
	if _, err := writeJobFile(filepath.Join(doneDir(), id+".json"), j); err != nil {
		t.Fatal(err)
	}
	w.hosts["h1"] = &simHost{dd: 0, ana: 0, reboot: never, boot: never} // 지금은 anaconda 로 응답
	now := time.Unix(base+1205, 0)
	w.clock.Set(now)
	if r := d.recheckHosts(id, allView, now); r != "" {
		t.Fatalf("재확인 거부: %q", r)
	}
	got := doneJob(t, id).Hosts["h1"]
	if got.SeenDown || got.DownAt != 0 || got.Stage != string(StageQueued) {
		t.Fatalf("종료된 job 은 단계 불변: %+v", got)
	}
	rc := readRecheck(dataDir(), id)
	if rc == nil || len(rc.Hosts) != 1 || strings.Contains(rc.Hosts[0].Detail, "→ 설치중") {
		t.Fatalf("종료 job 재확인 기록: %+v", rc)
	}
}
