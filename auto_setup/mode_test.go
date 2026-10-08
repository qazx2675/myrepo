// mode_test.go - 설정체크만(t, check 모드) / 설정체크 + 설정수정(c) 구분, 수동 실행 결과 화면, g 재확인 진행 화면
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 설정체크만 수동 실행: 환경설정 수정 없는 모드로 run 되고, 호스트를 완료 처리하지 않으며, Run 기록에 mode=check
func TestManualCheckOnlyKeepsHostsUndone(t *testing.T) {
	setupDir(t)
	x := newX(map[string]*simHost{"h1": install(60, 125, 400, 600), "h2": oldOS()})
	id := submitBody(t, 0, fmt.Sprintf(groupJobBody, base))
	d := drive(t, x.world, newXDaemon(x, false, false), 0, 700)
	if h := d.jobs[id].Hosts["h1"]; h.ReadyAt == 0 || h.Processed != "" {
		t.Fatalf("전제: h1 READY·미처리: %+v", h)
	}
	bad := req(t, ReqManualRun, map[string]string{"jobid": id, "yml": "a.yml", "mode": "apply2"})
	chk := req(t, ReqManualRun, map[string]string{"jobid": id, "yml": "a.yml", "mode": ModeCheck})
	d = drive(t, x.world, d, 705, 745)
	if r := rejectedReason(t, bad); !strings.Contains(r, "mode") {
		t.Fatalf("알 수 없는 mode 거부 이상: %q", r)
	}
	if _, err := os.Stat(filepath.Join(reqRejectedDir(), chk)); !os.IsNotExist(err) {
		t.Fatal("check 모드 요청이 거부됨")
	}
	if len(x.runs) != 1 || x.runs[0].Mode != ModeCheck {
		t.Fatalf("check 모드 run 1회여야 함: %+v", x.runs)
	}
	j := d.jobs[id]
	if h := j.Hosts["h1"]; h.Processed != "" || h.DoneSrc != "" {
		t.Fatalf("설정체크만은 완료 처리하면 안 됨: %+v", h)
	}
	if len(j.Runs) != 1 || j.Runs[0].Mode != ModeCheck || !j.Runs[0].Manual || j.Runs[0].Yml != "a.yml" {
		t.Fatalf("Run 기록: %+v", j.Runs)
	}
	if lg, _ := os.ReadFile(logPath()); !strings.Contains(string(lg), "(설정체크만 (설정 수정 안 함))") {
		t.Fatalf("로그에 모드 없음:\n%s", lg)
	}
	// 이어서 설정체크 + 설정수정(c)은 종전처럼 완료 처리
	req(t, ReqManualRun, map[string]string{"jobid": id, "yml": "a.yml"})
	d = drive(t, x.world, d, 750, 790)
	if len(x.runs) != 2 || x.runs[1].Mode != "" || d.jobs[id].Hosts["h1"].Processed == "" {
		t.Fatalf("c 는 수정 포함 + 완료 처리: %+v %+v", x.runs, d.jobs[id].Hosts["h1"])
	}
}

// Runner: check 모드는 -auto-check 로 실행하고 점검 결과 파일(check.res_<user>)을 읽으며 code 본문 첫 줄에 모드를 밝힌다
func TestRunnerCheckOnlyMode(t *testing.T) {
	needBash(t)
	setupDir(t)
	oldC := os_check_sh
	t.Cleanup(func() { os_check_sh = oldC })
	td, _ := filepath.Abs("testdata")
	tmp := t.TempDir()
	os_check_sh = filepath.Join(tmp, "os_check_stub.sh")
	os.WriteFile(os_check_sh, []byte("#!/bin/bash\necho \"$@\" > args.txt\ncat '"+td+"/os_check.log'\ncp '"+td+"/check.res_user1_postapply' check.res_user1\n"), 0755)

	j := &Job{ID: "j1", User: "user1", RunMode: ModeCheck}
	res, err := NewRealRunner().Run(j, []string{"host01", "host02", "host03", "host04"}, false)
	if err != nil || res.Abnormal {
		t.Fatalf("결과: %+v %v", res, err)
	}
	args, _ := os.ReadFile(filepath.Join(runsDir(), res.Code, "args.txt"))
	if strings.TrimSpace(string(args)) != "-auto-check user1 targets.txt" {
		t.Fatalf("os_check 인자: %q", args)
	}
	if strings.Join(res.Processed, ",") != "host01,host02,host03" {
		t.Fatalf("processed: %v", res.Processed)
	}
	b, _ := readCode(res.Code)
	if !strings.HasPrefix(string(b), "작업 : 설정체크만 (설정 수정 안 함)\n") || !strings.Contains(string(b), "설정체크 (설정 수정 안 함)") ||
		strings.Contains(string(b), "설정 수정 후 재점검") {
		t.Fatalf("code 본문:\n%s", b)
	}
	// 수정 포함(기본)
	res, _ = NewRealRunner().Run(&Job{ID: "j2", User: "user1"}, []string{"host01"}, false)
	b, _ = readCode(res.Code)
	if !strings.HasPrefix(string(b), "작업 : 설정체크 + 설정수정\n") {
		t.Fatalf("기본 모드 첫 줄:\n%s", b)
	}
}

// g 재확인: 단계별 진행 줄과 호스트별 결과가 recheck/<jobid>.json 에 남는다 (어디서 실패했는지 포함)
func TestRecheckProgressRecorded(t *testing.T) {
	setupDir(t)
	withRoute6(t, "mgmt", "/os6/gossh")
	h2 := install(60, 120, 400, 600)
	h2.noLocalSSH = true
	h3 := install(60, 120, 400, 600)
	h3.downs = [][2]int64{{0, 1000}}
	w := newWorld(map[string]*simHost{"h1": install(60, 120, 400, 600), "h2": h2, "h3": h3})
	id := submit(t, "u1", 0, "h1", "h2", "h3")
	d := drive(t, w, newTestDaemon(w), 0, 10)
	if _, err := writeRequest(ReqRecheck, map[string]string{"jobid": id, "yml": allView}); err != nil {
		t.Fatal(err)
	}
	drive(t, w, d, 15, 15)
	rc := readRecheck(dataDir(), id)
	if rc == nil || !rc.Done || rc.Yml != allView {
		t.Fatalf("기록: %+v", rc)
	}
	text := strings.Join(rc.Lines, "\n")
	for _, want := range []string{"재확인 시작: 3대", "1/3 os8_mgmt", "os8_mgmt 응답 1대 / 무응답 2대", "2/3 os6_mgmt 경유로 확인 중 (2대)", "os6_mgmt 응답 1대 / 무응답 1대", "3/3 최종 결과: 응답 2대 (os8 1, os6 경유 1), 접속불가 1대"} {
		if !strings.Contains(text, want) {
			t.Fatalf("진행 줄에 %q 없음:\n%s", want, text)
		}
	}
	got := map[string]RecheckHost{}
	for _, h := range rc.Hosts {
		got[h.Host] = h
	}
	if got["h1"].Result != "os8" || got["h2"].Result != "os6" || got["h3"].Result != "fail" || got["h3"].Detail != "os8 무응답 → os6 무응답" {
		t.Fatalf("호스트별 결과: %+v", rc.Hosts)
	}
	// 스냅샷에 실림
	if s := BuildSnapshot(dataDir(), w.clock.Now()); len(s.Jobs) != 1 || s.Jobs[0].Recheck == nil || !s.Jobs[0].Recheck.Done {
		t.Fatalf("스냅샷에 recheck 없음: %+v", s.Jobs)
	}
	// os6 미설정: 2단계 건너뜀을 밝힌다
	withRoute6(t, "", "")
	writeRequest(ReqRecheck, map[string]string{"jobid": id, "yml": allView})
	drive(t, w, d, 20, 20)
	rc = readRecheck(dataDir(), id)
	if !strings.Contains(strings.Join(rc.Lines, "\n"), "건너뜀 (os6_mgmt/os6_gossh 미설정)") {
		t.Fatalf("os6 미설정 안내 없음:\n%s", strings.Join(rc.Lines, "\n"))
	}
}

// ---- TUI ----

func runSnapJob(runs ...Run) Snapshot {
	s := twoGroupSnap()
	s.Jobs[0].Runs = runs
	return s
}

// c: 확인 문구에 '설정체크 + 설정수정', 요청 후 결과 화면(요청 전달 대기). t: '설정체크만', payload mode=check
func TestTUIManualModesAndResultView(t *testing.T) {
	src := &fakeSrc{snap: twoGroupSnap(), local: true}
	r := runKeys(t, src, "\rc")
	if f := lastFrame(r); !strings.Contains(f, "설정수정 (환경설정 수정함)") || !strings.Contains(f, "[y/n]") {
		t.Fatalf("c 확인 문구:\n%s", f)
	}
	r = runKeys(t, src, "\rcy")
	if len(src.reqs) != 1 || src.reqs[0].payload["mode"] != "" {
		t.Fatalf("c 요청에는 mode 없음: %+v", src.reqs)
	}
	if f := lastFrame(r); !strings.Contains(f, "수동 실행 [설정체크 + 설정수정] - a.yml") || !strings.Contains(f, "요청 전달됨") {
		t.Fatalf("결과 화면:\n%s", f)
	}
	src = &fakeSrc{snap: twoGroupSnap(), local: true}
	r = runKeys(t, src, "\rt")
	if f := lastFrame(r); !strings.Contains(f, "'설정체크만'") || !strings.Contains(f, "수정 안 함") {
		t.Fatalf("t 확인 문구:\n%s", f)
	}
	r = runKeys(t, src, "\rty")
	if len(src.reqs) != 1 || src.reqs[0].payload["mode"] != ModeCheck {
		t.Fatalf("t 요청 mode=check: %+v", src.reqs)
	}
	if f := lastFrame(r); !strings.Contains(f, "수동 실행 [설정체크만 (설정 수정 안 함)] - a.yml") {
		t.Fatalf("t 결과 화면:\n%s", f)
	}
}

// 결과 화면: 데몬이 run 을 끝내면(새 Run 기록) code 본문이 화면에 표시된다
func TestTUIResultViewShowsCodeWhenDone(t *testing.T) {
	text := "작업 : 설정체크만 (설정 수정 안 함)\n\n설정체크 (설정 수정 안 함)\na1: FAIL selinux\nNO FAIL : a2 (1대)\n원본 : /x\n"
	base := twoGroupSnap()
	src := &fakeSrc{snap: base, local: true, codes: map[string]string{"0042": text}}
	src.nextSnap = func(n int) Snapshot {
		if n < 2 {
			return base
		}
		return runSnapJob(Run{Code: "0042", Hosts: []string{"a1", "a2"}, At: 1, Manual: true, Yml: "a.yml", Mode: ModeCheck})
	}
	r := runKeys(t, src, "\rtyr") // 열기 → r(새로고침) → 완료 결과 표시
	f := lastFrame(r)
	for _, want := range []string{"상태: 완료 - code 0042", "a1: FAIL selinux", "NO FAIL : a2 (1대)", "작업 : 설정체크만"} {
		if !strings.Contains(f, want) {
			t.Fatalf("결과 화면에 %q 없음:\n%s", want, f)
		}
	}
	// v: 최근 결과 다시 보기
	src2 := &fakeSrc{snap: runSnapJob(Run{Code: "0042", Hosts: []string{"a1"}, Manual: true, Yml: "a.yml"}), local: true,
		codes: map[string]string{"0042": "작업 : 설정체크 + 설정수정\nOK\n"}}
	r = runKeys(t, src2, "\rv")
	if f := lastFrame(r); !strings.Contains(f, "최근 수동 실행 결과 [설정체크 + 설정수정] - a.yml") || !strings.Contains(f, "code 0042") {
		t.Fatalf("v 결과:\n%s", f)
	}
	r = runKeys(t, &fakeSrc{snap: twoGroupSnap(), local: true}, "\rv")
	if f := lastFrame(r); !strings.Contains(f, "수동 실행 결과가 아직 없습니다") {
		t.Fatalf("결과 없음 안내:\n%s", f)
	}
}

// g: 재확인 진행 화면 — 진행 줄·호스트별 결과(어디서 안 됐는지)·최종 결과
func TestTUIRecheckProgressView(t *testing.T) {
	base := twoGroupSnap()
	done := twoGroupSnap()
	done.Jobs[0].Recheck = &Recheck{Job: "J1", Yml: "a.yml", At: 100, Done: true,
		Lines: []string{"[10:00:00] 재확인 시작: 2대", "[10:00:01] 1/3 os8_mgmt 에서 직접 확인 중 (2대)…", "[10:00:03] 3/3 최종 결과: 응답 1대 (os8 1, os6 경유 0), 접속불가 1대"},
		Hosts: []RecheckHost{{Host: "a1", Result: "os8", Detail: "단계 완료"}, {Host: "a2", Result: "fail", Detail: "os8 무응답 → os6 무응답"}}}
	src := &fakeSrc{snap: base, local: true}
	src.nextSnap = func(n int) Snapshot {
		if n < 2 {
			return base
		}
		return done
	}
	r := runKeys(t, src, "\rg")
	if f := lastFrame(r); !strings.Contains(f, "서버 상태 재확인 (g) - a.yml") || !strings.Contains(f, "요청 전달됨") {
		t.Fatalf("g 직후:\n%s", f)
	}
	src.calls = 0
	r = runKeys(t, src, "\rgr")
	f := lastFrame(r)
	for _, want := range []string{"상태: 완료", "1/3 os8_mgmt", "3/3 최종 결과", "접속불가", "os8 무응답 → os6 무응답", "g 다시 재확인"} {
		if !strings.Contains(f, want) {
			t.Fatalf("진행 화면에 %q 없음:\n%s", want, f)
		}
	}
}

// 색: 설정수정(환경설정을 실제로 수정하는 모드)은 빨강, 설정체크만은 빨강 아님
func TestSetupModifyIsRed(t *testing.T) {
	g := &SnapGroup{Yml: "a.yml", Counts: StageCounts{Total: 2}}
	red := "\x1b[1;31m설정수정"
	if l := manualLine(selection{}, g, 160, true); !strings.Contains(l, red) {
		t.Fatalf("하단 줄에서 설정수정이 빨강이어야 함: %q", l)
	}
	if l := manualLine(selection{Confirm: true}, g, 160, true); !strings.Contains(l, "\x1b[1;31m설정수정 (환경설정 수정함)") {
		t.Fatalf("c 확인 문구: %q", l)
	}
	if l := manualLine(selection{Confirm: true, Mode: ModeCheck}, g, 160, true); strings.Contains(l, "1;31m") {
		t.Fatalf("t 확인 문구는 빨강 없음: %q", l)
	}
	v := &viewState{kind: viewRun, title: "수동 실행 [설정체크 + 설정수정] - a.yml", lines: []string{"작업 : 설정체크 + 설정수정"}}
	if o := renderView(v, 100, 12, true); strings.Count(o, red) != 2 {
		t.Fatalf("결과 창 제목·첫 줄의 설정수정 빨강: %q", o)
	}
	// "무응답 0대" 는 빨강 아님, 접속불가 호스트 줄은 빨강
	if lineStyle("[09:26:06]     os8_mgmt 응답 3대 / 무응답 0대") == stRed || lineStyle("3/3 최종 결과: 응답 3대 (os8 3, os6 경유 0), 접속불가 0대") != stGreen ||
		lineStyle("127.0.0.14  접속불가  os8 무응답 → os6 무응답") != stRed {
		t.Fatal("줄 색 규칙")
	}
}

// g 진행 창(높이 24 이상): 위 호스트표 + 아래 진행 과정이 한 화면에
func TestRecheckSplitLayout(t *testing.T) {
	done := twoGroupSnap()
	done.Jobs[0].Recheck = &Recheck{Job: "J1", Yml: "a.yml", At: 100, Done: true, Lines: []string{"[10:00:00] 재확인 시작: 2대"},
		Hosts: []RecheckHost{{Host: "a1", Result: "os8", Detail: "단계 완료"}}}
	src := &fakeSrc{snap: twoGroupSnap(), local: true}
	n := 0
	src.nextSnap = func(int) Snapshot {
		n++
		if n < 2 {
			return twoGroupSnap()
		}
		return done
	}
	f := lastFrame(runKeys(t, src, "\rgr"))
	i, j := strings.Index(f, "a2"), strings.Index(f, "서버 상태 재확인 (g)")
	if i < 0 || j < 0 || i > j || !strings.Contains(f, "재확인 시작: 2대") {
		t.Fatalf("호스트표(위) + 진행(아래):\n%s", f)
	}
}

// 종료된(jobs/done) job 에서도 g 가 동작: 경로만 갱신하고 단계는 건드리지 않으며, 없는 job 은 진행 창에 사유가 보인다
func TestRecheckClosedJobAndMissingJob(t *testing.T) {
	setupDir(t)
	withRoute6(t, "mgmt", "/os6/gossh")
	x := newX(map[string]*simHost{"h1": install(60, 125, 400, 600)})
	id := submit(t, "u1", 0, "h1")
	d := drive(t, x.world, newXDaemon(x, false, false), 0, 610)
	j := doneJob(t, id)
	j.Hosts["h1"].Processed, j.Hosts["h1"].Route, j.Hosts["h1"].Fails = "", "os6", 3 // 미완료·os6 로 남은 호스트
	if _, err := writeJobFile(filepath.Join(doneDir(), id+".json"), j); err != nil {
		t.Fatal(err)
	}
	writeRequest(ReqRecheck, map[string]string{"jobid": id, "yml": allView})
	writeRequest(ReqRecheck, map[string]string{"jobid": "20200101-000000-nobody", "yml": allView})
	drive(t, x.world, d, 615, 615)
	rc := readRecheck(dataDir(), id)
	if rc == nil || !rc.Done || len(rc.Hosts) != 1 || rc.Hosts[0].Result != "os8" || !strings.Contains(rc.Hosts[0].Detail, "경로 os6 → local") {
		t.Fatalf("종료 job 재확인 기록: %+v", rc)
	}
	if h := doneJob(t, id).Hosts["h1"]; h.Route != "local" || h.Processed != "" || h.Fails != 3 {
		t.Fatalf("종료 job 은 경로만 바뀌어야 함: %+v", h)
	}
	nb := readRecheck(dataDir(), "20200101-000000-nobody")
	if nb == nil || !nb.Done || !strings.Contains(strings.Join(nb.Lines, "\n"), "job 을 찾을 수 없습니다") {
		t.Fatalf("없는 job 기록: %+v", nb)
	}
}

// 키 안내 줄: 색이 꺼지면 원문 그대로, 켜지면 항목별 색 (c 빨강, t 초록, g 노랑, x 자홍, w 파랑, 이동 청록)
func TestHintSegsColors(t *testing.T) {
	h := " ↑↓ 이동  ← 목록  c 체크+수정  t 체크만  g 재확인  x 단독 체크  w AWX  q 복귀"
	if got := segLine(false, 200, hintSegs(h)...); got != h {
		t.Fatalf("색 없음이면 원문 그대로여야 함:\n%q\n%q", got, h)
	}
	got := segLine(true, 200, hintSegs(h)...)
	for _, want := range []string{"\x1b[1;36m↑↓ 이동", "\x1b[1;31mc 체크+수정", "\x1b[1;32mt 체크만", "\x1b[1;33mg 재확인", "\x1b[1;35mx 단독 체크", "\x1b[1;34mw AWX", "\x1b[1mq 복귀"} {
		if !strings.Contains(got, want) {
			t.Errorf("색 항목 %q 없음: %q", want, got)
		}
	}
	// 폭이 모자라 잘려도 … 가 반복되지 않는다
	if s := segLine(false, 20, hintSegs(h)...); strings.Count(s, "…") != 1 {
		t.Errorf("말줄임표 중복: %q", s)
	}
}
