package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestParseStressEvent(t *testing.T) {
	cases := []struct {
		line string
		want stressEvent
		ok   bool
	}{
		{"[제외] stage=a hosts=h1,h2 reason=접속불가", stressEvent{Kind: stressExcludedMark, Stage: "a", Hosts: []string{"h1", "h2"}, Reason: "접속불가"}, true},
		{"  [제외] stage=b hosts=h3 reason=DNS실패  ", stressEvent{Kind: stressExcludedMark, Stage: "b", Hosts: []string{"h3"}, Reason: "DNS실패"}, true},
		{"[job] stage=b id=123", stressEvent{Kind: stressJobMark, Stage: "b", JobID: 123}, true},
		{"[결과] stage=b done=h1,h2 notrun=h3 nomon=h4", stressEvent{Kind: stressResultMark, Stage: "b", Done: []string{"h1", "h2"}, NotRun: []string{"h3"}, NoMon: []string{"h4"}}, true},
		{"[결과] stage=a done= notrun= nomon=", stressEvent{Kind: stressResultMark, Stage: "a"}, true},
		// 모양이 틀린 줄은 무시
		{"[제외] stage=c hosts=h1 reason=접속불가", stressEvent{}, false},
		{"[제외] stage=a hosts= reason=접속불가", stressEvent{}, false},
		{"[제외] stage=a hosts=h1", stressEvent{}, false},
		{"[job] stage=a id=abc", stressEvent{}, false},
		{"[job] stage=a", stressEvent{}, false},
		{"[결과] stage=a done=h1", stressEvent{}, false},
		{"[결과] done=h1 notrun= nomon=", stressEvent{}, false},
		{"[감시] a 경과", stressEvent{}, false},
		{"", stressEvent{}, false},
	}
	for _, c := range cases {
		got, ok := parseStressEvent(c.line)
		if ok != c.ok || (ok && !reflect.DeepEqual(got, c.want)) {
			t.Errorf("%q → %+v ok=%v, 기대 %+v ok=%v", c.line, got, ok, c.want, c.ok)
		}
	}
}

func TestStressEventsAccumulate(t *testing.T) {
	var s stressEvents
	if st := s.Stage("a"); st.JobSeen || st.Result.Have || len(st.Excluded) != 0 {
		t.Errorf("빈 값이어야 함: %+v", st)
	}
	add := func(l string) {
		ev, ok := parseStressEvent(l)
		if !ok {
			t.Fatalf("읽기 실패: %s", l)
		}
		s.Add(ev)
	}
	add("[제외] stage=a hosts=h1,h2 reason=접속불가")
	add("[제외] stage=a hosts=h3 reason=DNS실패")
	add("[job] stage=a id=7")
	add("[결과] stage=a done=h4 notrun=h5 nomon=")
	add("[job] stage=a id=9")
	a := s.Stage("a")
	if strings.Join(a.Excluded, ",") != "h1,h2,h3" || a.Reason["h3"] != "DNS실패" || !a.JobSeen || a.JobID != 9 ||
		!a.Result.Have || strings.Join(a.Result.Done, ",") != "h4" || strings.Join(a.Result.NotRun, ",") != "h5" || len(a.Result.NoMon) != 0 {
		t.Errorf("a = %+v", a)
	}
	if b := s.Stage("b"); b.JobSeen || b.Result.Have {
		t.Errorf("b 는 비어야 함: %+v", b)
	}
}

// lineWatch 는 세 줄을 ev 로 넘기고 쪼개져 들어온 줄도 한 번만 부른다.
func TestLineWatchEvents(t *testing.T) {
	var got []string
	lw := &lineWatch{ev: func(l string) { got = append(got, l) }}
	lw.Write([]byte("[i] x\n[제외] stage=a hosts=h1 rea"))
	lw.Write([]byte("son=접속불가\n[job] stage=a id=5\n[감시] a 경과\n[결과] stage=a done=h1 notrun= nomon=\n"))
	want := []string{"[제외] stage=a hosts=h1 reason=접속불가", "[job] stage=a id=5", "[결과] stage=a done=h1 notrun= nomon="}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got=%q", got)
	}
}

// 실행기(realExec)가 가짜 stress-test 의 세 줄을 읽어 넘기고, ev 가 nil 이어도(옛 호출) 정상이다.
func TestRealExecStressEvents(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "fake_st.sh")
	script := `#!/bin/bash
echo "[제외] stage=a hosts=h9 reason=접속불가"
echo "[job] stage=a id=11"
echo "[감시] a 경과"
echo "[결과] stage=a done=h1 notrun= nomon=h2"
`
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	conf := parseConf("")
	conf.StressTest = bin
	e := &realExec{conf: conf, user: "tester", dir: dir, log: &strings.Builder{}}
	list := filepath.Join(dir, "l.txt")
	os.WriteFile(list, []byte("h1\n"), 0o644)
	var evs stressEvents
	launched := 0
	code, err := e.StressTestEv("a", list, []string{"h1"}, "", func() { launched++ }, nil, nil, evs.Add)
	a := evs.Stage("a")
	if err != nil || code != 0 || launched != 1 || a.JobID != 11 || strings.Join(a.Excluded, ",") != "h9" || strings.Join(a.Result.NoMon, ",") != "h2" {
		t.Errorf("code=%d err=%v launched=%d a=%+v", code, err, launched, a)
	}
	if code, err = e.StressTest("a", list, []string{"h1"}, "", nil, nil, nil); err != nil || code != 0 {
		t.Errorf("옛 호출: code=%d err=%v", code, err)
	}
}
