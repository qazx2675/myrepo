package main

import (
	"strings"
	"testing"
	"time"
)

// stress-ng O 기준(계획서_auto_막힘.md 4.5, P8) 시험: launch 가 받아들여져도 바로 O 가 아니라
// stress-test 감시가 끝나 [결과] 줄을 받은 뒤에 O 가 된다.

// autoResFake 는 stress-ng stress-test 가 끝날 때 [결과] 줄을 내는 가짜 실행기다(eventStresser).
// plan 의 앞 항목을 stress-ng launch 마다 하나씩 꺼내 쓴다: 호스트 → "done" / "notrun" / "nomon" / ""(어느 목록에도 없음).
// 항목이 없으면 [결과] 줄을 내지 않는다(옛 stress-test). hold 가 있으면 launch 뒤 그만큼 가짜 시각이 흐를 때까지 끝나지 않는다(감시 중).
type autoResFake struct {
	*autoFake
	plan []map[string]string
	hold time.Duration

	onHold func(f *autoResFake) // hold 중 가짜 시각이 hold/2 를 넘었을 때 한 번(워커 상태를 본다)
}

func (f *autoResFake) StressTestEv(steps, listFile string, hosts []string, end string, launched func(), registered func(yml string, hosts []string), busy func(msg string) string, ev func(stressEvent)) (int, error) {
	code, err := f.autoFake.StressTest(steps, listFile, hosts, end, launched, registered, busy)
	if steps != "b" || ev == nil || code != 0 {
		return code, err
	}
	f.mu.Lock()
	var p map[string]string
	if len(f.plan) > 0 {
		p, f.plan = f.plan[0], f.plan[1:]
	}
	t0 := f.now
	f.mu.Unlock()
	if f.hold > 0 {
		called := false
		for {
			f.mu.Lock()
			el := f.now.Sub(t0)
			f.mu.Unlock()
			if !called && el >= f.hold/2 && f.onHold != nil {
				called = true
				f.onHold(f)
			}
			if el >= f.hold {
				break
			}
			time.Sleep(time.Millisecond)
		}
	}
	if p != nil {
		r := stressEvent{Kind: stressResultMark, Stage: "b"}
		for _, h := range hosts {
			switch p[h] {
			case "done":
				r.Done = append(r.Done, h)
			case "notrun":
				r.NotRun = append(r.NotRun, h)
			case "nomon":
				r.NoMon = append(r.NoMon, h)
			}
		}
		ev(r)
	}
	return code, err
}

func ngHost() *autoHost { return &autoHost{check: "O", cpu: "O", stress: cpuNotRun} }

// done → O(실행 확인 중 해제), nomon → O + 비고 "감시 제외".
func TestAutoNGResultDoneNoMon(t *testing.T) {
	hosts := map[string]*autoHost{"d": ngHost(), "m": ngHost()}
	base := newAutoFake(t, autoT0, []string{"d", "m"}, hosts)
	f := &autoResFake{autoFake: base, plan: []map[string]string{{"d": "done", "m": "nomon"}}}
	w := runAutoEv(t, base, f, testConf())
	if got := base.pick("ng"); got != "ng:d,m@0" {
		t.Errorf("사건: %s", got)
	}
	d, m := w.st.row("d"), w.st.row("m")
	if d.Stress != "O" || !d.NGLaunched || d.NGAccepted || d.Running != "" || d.AutoErr != "" || d.NGNote != "" {
		t.Errorf("d: %+v", *d)
	}
	if m.Stress != "O" || !m.NGLaunched || m.NGAccepted || m.Running != "" || m.NGNote != "감시 제외" || !strings.Contains(m.progressText(), "감시 제외") {
		t.Errorf("m: %+v", *m)
	}
	if w.st.Stage != stageFinish || !strings.Contains(w.st.Detail, "모두 O 2대") {
		t.Errorf("끝 상태: %s", w.st.Detail)
	}
}

// notrun → O 가 아님, 시도 1회, AutoErr, NGWait(5분) 뒤 다시 launch, 두 번째에 done 이면 O.
func TestAutoNGNotRunRelaunch(t *testing.T) {
	hosts := map[string]*autoHost{"a": ngHost(), "b": ngHost()}
	base := newAutoFake(t, autoT0, []string{"a", "b"}, hosts)
	f := &autoResFake{autoFake: base, plan: []map[string]string{{"a": "done", "b": "notrun"}, {"b": "done"}}}
	w := runAutoEv(t, base, f, testConf())
	if got := base.pick("ng"); got != "ng:a,b@0 ng:b@5" {
		t.Errorf("사건: %s", got)
	}
	a, b := w.st.row("a"), w.st.row("b")
	if a.Stress != "O" || a.NGTries != 1 {
		t.Errorf("a: %+v", *a)
	}
	if b.Stress != "O" || b.NGTries != 2 || b.AutoErr != "" || b.GaveUp != "" || b.Running != "" {
		t.Errorf("b: %+v", *b)
	}
	if !strings.Contains(base.logb.String(), "stress-ng 미실행(부하 안 오름, 시도 1회로 셈") {
		t.Errorf("로그에 미실행 줄이 없음:\n%s", base.logb.String())
	}
}

// notrun 3회면 포기. 처음 두 번은 O 가 아니고 오류 문구가 남는다.
func TestAutoNGNotRunGiveUp(t *testing.T) {
	hosts := map[string]*autoHost{"n": ngHost()}
	base := newAutoFake(t, autoT0, []string{"n"}, hosts)
	nr := map[string]string{"n": "notrun"}
	f := &autoResFake{autoFake: base, plan: []map[string]string{nr, nr, nr}}
	w := runAutoEv(t, base, f, testConf())
	if got := base.pick("ng"); got != "ng:n@0 ng:n@5 ng:n@10" {
		t.Errorf("사건: %s", got)
	}
	n := w.st.row("n")
	if n.GaveUp != "stress-ng" || n.NGTries != 3 || n.Stress == "O" || n.NGLaunched || n.Running != "" || n.AutoErr != "stress-ng 미실행(부하 안 오름)" {
		t.Errorf("n: %+v", *n)
	}
	if !strings.Contains(n.progressText(), "포기(stress-ng)") || !strings.Contains(w.st.Detail, "포기 1대") {
		t.Errorf("표시: %s / %s", n.progressText(), w.st.Detail)
	}
}

// [결과] 의 어느 목록에도 없는 launch 호스트는 notrun 과 같게 다룬다.
func TestAutoNGInNoListLikeNotRun(t *testing.T) {
	hosts := map[string]*autoHost{"a": ngHost(), "x": ngHost()}
	base := newAutoFake(t, autoT0, []string{"a", "x"}, hosts)
	f := &autoResFake{autoFake: base, plan: []map[string]string{{"a": "done"}, {"x": "done"}}}
	w := runAutoEv(t, base, f, testConf())
	if got := base.pick("ng"); got != "ng:a,x@0 ng:x@5" {
		t.Errorf("사건: %s", got)
	}
	if x := w.st.row("x"); x.Stress != "O" || x.NGTries != 2 {
		t.Errorf("x: %+v", *x)
	}
}

// [결과] 줄이 없으면(이벤트를 아는 실행기가 끝까지 줄을 안 냄): stress-test 가 끝난 때 launch 된 호스트를 모두 O 로 둔다.
func TestAutoNGNoResultFallbackAtEnd(t *testing.T) {
	hosts := map[string]*autoHost{"a": ngHost(), "b": ngHost()}
	base := newAutoFake(t, autoT0, []string{"a", "b"}, hosts)
	f := &autoResFake{autoFake: base}
	w := runAutoEv(t, base, f, testConf())
	if got := base.pick("ng"); got != "ng:a,b@0" {
		t.Errorf("사건: %s", got)
	}
	for _, h := range []string{"a", "b"} {
		if r := w.st.row(h); r.Stress != "O" || !r.NGLaunched || r.NGAccepted || r.Running != "" || r.NGTries != 1 {
			t.Errorf("%s: %+v", h, *r)
		}
	}
	if !strings.Contains(base.logb.String(), "[결과] 줄이 없어 launch 된 2대를 O 로 표시") {
		t.Errorf("로그:\n%s", base.logb.String())
	}
}

// 이벤트를 모르는 실행기(가짜 autoFake 자체)는 launch 를 받아들이는 때 바로 O 다(옛 동작): StressTest 가 끝나기 전에도 O.
func TestAutoNGLegacyExecutorImmediateO(t *testing.T) {
	hosts := map[string]*autoHost{"a": ngHost()}
	base := newAutoFake(t, autoT0, []string{"a"}, hosts)
	w := runAutoFake(t, base, testConf())
	if r := w.st.row("a"); r.Stress != "O" || !r.NGLaunched || r.NGAccepted || r.NGTries != 1 {
		t.Errorf("a: %+v", *r)
	}
	if strings.Contains(base.logb.String(), "실행 확인 중") {
		t.Errorf("옛 실행기인데 실행 확인 중 로그가 나옴:\n%s", base.logb.String())
	}
}

// 감시가 도는 동안: O 가 아니고 "실행 확인 중", 스케줄러는 끝나지 않고, 대상으로 다시 뽑히지 않는다. 끝나면 O 로 끝난다.
func TestAutoNGRunningBlocksFinish(t *testing.T) {
	hosts := map[string]*autoHost{"a": ngHost()}
	base := newAutoFake(t, autoT0, []string{"a"}, hosts)
	f := &autoResFake{autoFake: base, plan: []map[string]string{{"a": "done"}}, hold: 60 * time.Minute}
	var seen []string
	f.onHold = func(f *autoResFake) {
		f.w.mu.Lock()
		defer f.w.mu.Unlock()
		r := f.w.st.row("a")
		seen = append(seen, r.Running, r.Stress, r.progressText(), f.w.st.Detail)
		if f.w.st.Stage == stageFinish {
			seen = append(seen, "finished")
		}
		if !r.NGAccepted || r.NGLaunched {
			seen = append(seen, "flags")
		}
	}
	w := runAutoEv(t, base, f, testConf())
	if len(seen) < 4 || seen[0] != prgNGRun || seen[1] != cpuNotRun || !strings.Contains(seen[2], prgNGRun) || !strings.Contains(seen[3], "실행 확인 중") || len(seen) != 4 {
		t.Errorf("감시 중 상태: %q", seen)
	}
	if got := base.pick("ng"); got != "ng:a@0" {
		t.Errorf("다시 launch 됨: %s", got)
	}
	if r := w.st.row("a"); r.Stress != "O" || !r.NGLaunched || r.Running != "" || w.st.Stage != stageFinish {
		t.Errorf("끝 상태: %+v", *r)
	}
}

// 재기동 복구: launch 가 받아들여진 뒤(NGAccepted) 죽었으면 launch 된 것으로 보고 O, launch 도중(NGAccepted 아님)이면 대상으로 남아 다시 launch 한다.
func TestAutoNGRestartRecovery(t *testing.T) {
	hosts := map[string]*autoHost{"acc": ngHost(), "mid": ngHost()}
	base := newAutoFake(t, autoT0, []string{"acc", "mid"}, hosts)
	st := &runState{Version: stateVersion, User: "tester", Stage: stageJudge, Auto: &autoState{},
		Hosts: []hostRow{
			{Host: "acc", Running: prgNGRun, NGAccepted: true, NGTries: 1},
			{Host: "mid", Running: prgNGRun, NGTries: 1},
		}}
	base.logb = &strings.Builder{}
	w := newWorker(t.TempDir(), st, base, testConf(), nil, base.logb)
	base.w = w
	w.run()
	if got := base.pick("ng"); got != "ng:mid@0" {
		t.Errorf("사건: %s", got)
	}
	acc, mid := w.st.row("acc"), w.st.row("mid")
	if acc.Stress != "O" || !acc.NGLaunched || acc.NGAccepted || acc.Running != "" || acc.NGTries != 1 {
		t.Errorf("acc: %+v", *acc)
	}
	if mid.Stress != "O" || mid.NGTries != 2 || mid.Running != "" {
		t.Errorf("mid: %+v", *mid)
	}
}
