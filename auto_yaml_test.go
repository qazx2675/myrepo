package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// -auto 의 launch 직전 yaml 확인(3단계): 인벤토리 소스의 yml·호스트가 우리가 등록한 것과 다르면 y/s 질문,
// 답을 기다리는 동안에도 설정수정·재판정은 계속된다. 첫 launch 는 묻지 않는다.

// tamperOnFirst 는 첫 등록 직후 다른 사람이 소스 yml 과 호스트를 바꾼 것을 흉내 낸다.
func tamperOnFirst(f *autoFake, n int) {
	if n == 1 {
		f.awxYml = "os_install.yml"
		f.awxHosts = []string{"x", "y", "z"}
	}
}

// 첫 launch(stress-ng c)는 묻지 않고, 그 직후 바뀐 것은 CPU launch 직전에 질문 → 엉뚱한 답(다른 id)은 무시 → 25분 뒤 y 로 덮어쓰고 launch.
// 기다리는 25분 동안 10분 재판정·설정수정(e)이 계속되고 12시간 타이머(Auto.Start)는 그대로 흐른다.
func TestAutoYamlChangedAskThenYes(t *testing.T) {
	hosts := map[string]*autoHost{
		"a": {check: "O", cpu: cpuNotRun, stress: cpuNotRun, cpuAfter: 45 * time.Minute},
		"c": {check: "O", cpu: "O", stress: cpuNotRun},
		"e": {unreachUntil: 15 * time.Minute, check: "X", fixOn: 1, cpu: cpuNotRun, stress: cpuNotRun, cpuAfter: 45 * time.Minute},
	}
	f := newAutoFake(t, autoT0, []string{"a", "c", "e"}, hosts)
	f.onReg = tamperOnFirst
	bogus := false
	f.onQ = func(f *autoFake, q *question) {
		age := f.qAge(q)
		if age >= 5*time.Minute && !bogus {
			bogus = true
			f.answerQ("q99", "y") // 다른 질문의 답: 무시돼야 한다
		}
		if age >= 25*time.Minute {
			f.answerQ(q.ID, "y")
		}
	}
	w := runAutoFake(t, f, testConf())

	want := "judge:@0 ng:c@0 judge:@10 judge:@20 fix:e@20 judge:@20 cpu:a,e@25 probe:a,e@75 ng:a,e@75"
	if got := strings.Join(f.events, " "); got != want {
		t.Errorf("사건\n기대: %s\n실제: %s", want, got)
	}
	// 첫 launch 는 확인하지 않는다: YamlState 는 CPU 질문 때(@0)와 두 번째 stress-ng 때(@75)만
	eqList(t, "YamlState 호출", f.yamlCalls, []string{"@0", "@75"})
	if len(f.qTexts) != 1 {
		t.Fatalf("질문 %d개: %v", len(f.qTexts), f.qTexts)
	}
	q := f.qTexts[0]
	for _, s := range []string{"CPU 테스트(1대)", "지금 os_install.yml / 우리가 등록한 auto_1_1ea.yml", "지금 3대 / 우리가 등록한 1대", "지금만 있는 호스트 3대: x y z", "없어진 호스트 1대: c", "y:", "s:"} {
		if !strings.Contains(q, s) {
			t.Errorf("질문 문구에 %q 없음:\n%s", s, q)
		}
	}
	// 기다리는 동안 진행 설명에 질문 대기와 남은 시간이 보인다
	joined := strings.Join(f.qDetail, "\n")
	if !strings.Contains(joined, "yaml 바뀜 질문 답 기다리는 중") || !strings.Contains(joined, "남은 시간") {
		t.Errorf("대기 중 진행 설명: %v", f.qDetail)
	}
	// y: 우리 yaml 을 새로 등록(CPU 묶음 a,e)했고 그 값을 다시 기억한다. 질문은 사라졌다
	eqList(t, "등록", f.regs, []string{"auto_1_1ea.yml@0", "auto_2_2ea.yml@25", "auto_3_2ea.yml@75"})
	if w.st.Question != nil || w.yamlQ != "" || w.yamlOK {
		t.Errorf("질문 정리 안 됨: %+v %q %v", w.st.Question, w.yamlQ, w.yamlOK)
	}
	if w.st.Auto.Yml != "auto_3_2ea.yml" || strings.Join(w.st.Auto.YmlHosts, ",") != "a,e" {
		t.Errorf("기억한 yml: %s %v", w.st.Auto.Yml, w.st.Auto.YmlHosts)
	}
	if !w.st.Auto.Start.Equal(autoT0) || w.st.Stage != stageFinish || !strings.Contains(w.st.Detail, "모두 O 3대") {
		t.Errorf("끝 상태: start=%s %d %s", w.st.Auto.Start, w.st.Stage, w.st.Detail)
	}
	// 덮어쓴 다른 작업의 내용은 로그에 남는다
	lg := f.logb.String()
	for _, s := range []string{"무시한 답: id=q99", "질문 q1 (y/s, CPU launch 보류, yaml 바뀜)", "지금 인벤토리 호스트 3대(y 로 덮어쓰기 전 기록): x y z", "답 q1: y", "등록한 yml 기억: auto_2_2ea.yml"} {
		if !strings.Contains(lg, s) {
			t.Errorf("로그에 %q 없음", s)
		}
	}
}

// s: 질문에 s 로 답하면 launch 하지 않고 작업을 중지한다(질문은 걷힌다).
func TestAutoYamlChangedStop(t *testing.T) {
	hosts := map[string]*autoHost{
		"a": {check: "O", cpu: cpuNotRun, stress: cpuNotRun, cpuAfter: 45 * time.Minute},
		"c": {check: "O", cpu: "O", stress: cpuNotRun},
	}
	f := newAutoFake(t, autoT0, []string{"a", "c"}, hosts)
	f.onReg = tamperOnFirst
	f.onQ = func(f *autoFake, q *question) {
		if f.qAge(q) >= 3*time.Minute {
			f.answerQ(q.ID, "s")
		}
	}
	w := runAutoFake(t, f, testConf())
	if got, want := strings.Join(f.events, " "), "judge:@0 ng:c@0"; got != want {
		t.Errorf("사건\n기대: %s\n실제: %s", want, got)
	}
	if !w.st.Stopped || w.st.Question != nil || !strings.Contains(w.st.Detail, "중지") {
		t.Errorf("중지 상태: stopped=%v q=%+v detail=%s", w.st.Stopped, w.st.Question, w.st.Detail)
	}
	if a := w.st.row("a"); a.CPUTries != 0 || a.CPU != cpuNotRun {
		t.Errorf("a 는 launch 되지 않아야 함: %+v", *a)
	}
	if got := f.Now().Sub(autoT0); got > 4*time.Minute {
		t.Errorf("중지가 늦음: 시작 뒤 %s", got)
	}
}

// 호스트만 바뀌어도(yml 은 같음) 질문한다. 호스트 이름 순서만 다른 것은 바뀐 것이 아니다.
func TestAutoYamlHostsOnlyChanged(t *testing.T) {
	hosts := map[string]*autoHost{
		"a": {check: "O", cpu: cpuNotRun, stress: cpuNotRun, cpuAfter: 45 * time.Minute},
		"c": {check: "O", cpu: "O", stress: cpuNotRun},
	}
	f := newAutoFake(t, autoT0, []string{"a", "c"}, hosts)
	f.onReg = func(f *autoFake, n int) {
		if n == 1 {
			f.awxHosts = []string{"c", "intruder"}
		}
	}
	f.onQ = func(f *autoFake, q *question) { f.answerQ(q.ID, "y") }
	w := runAutoFake(t, f, testConf())
	if len(f.qTexts) != 1 || !strings.Contains(f.qTexts[0], "지금 auto_1_1ea.yml / 우리가 등록한 auto_1_1ea.yml") || !strings.Contains(f.qTexts[0], "지금 2대 / 우리가 등록한 1대") || !strings.Contains(f.qTexts[0], "지금만 있는 호스트 1대: intruder") {
		t.Errorf("질문: %v", f.qTexts)
	}
	if got := f.pick("cpu", "ng"); got != "ng:c@0 cpu:a@0 ng:a@50" {
		t.Errorf("사건: %s", got)
	}
	if w.st.Stage != stageFinish {
		t.Errorf("끝 상태: %s", w.st.Detail)
	}
}

// 호스트 이름 집합이 같으면(순서만 다름) 질문하지 않는다. 등록 때 호스트를 못 읽었으면 yml 만 비교한다.
func TestAutoYamlNoQuestionWhenSame(t *testing.T) {
	hosts := map[string]*autoHost{
		"a": {check: "O", cpu: cpuNotRun, stress: cpuNotRun, cpuAfter: 45 * time.Minute},
		"b": {check: "O", cpu: cpuNotRun, stress: cpuNotRun, cpuAfter: 45 * time.Minute},
		"c": {check: "O", cpu: "O", stress: cpuNotRun},
	}
	f := newAutoFake(t, autoT0, []string{"a", "b", "c"}, hosts)
	// onQ 를 두지 않았으므로 질문이 나오면 autoFake.Sleep 이 실패로 처리한다
	f.onReg = func(f *autoFake, n int) { // 같은 호스트를 거꾸로(순서만 다름)
		for i, j := 0, len(f.awxHosts)-1; i < j; i, j = i+1, j-1 {
			f.awxHosts[i], f.awxHosts[j] = f.awxHosts[j], f.awxHosts[i]
		}
	}
	w := runAutoFake(t, f, testConf())
	if w.st.Stage != stageFinish || !strings.Contains(w.st.Detail, "모두 O 3대") {
		t.Errorf("끝 상태: %s", w.st.Detail)
	}
	if n := len(f.yamlCalls); n != 2 {
		t.Errorf("YamlState %d회: %v", n, f.yamlCalls)
	}

	// 호스트를 못 읽은 등록: 호스트가 달라져도 yml 이 같으면 묻지 않는다
	hosts2 := map[string]*autoHost{
		"a": {check: "O", cpu: cpuNotRun, stress: cpuNotRun, cpuAfter: 45 * time.Minute},
		"c": {check: "O", cpu: "O", stress: cpuNotRun},
	}
	f2 := newAutoFake(t, autoT0, []string{"a", "c"}, hosts2)
	f2.noRegHosts = true
	f2.onReg = func(f *autoFake, n int) { f.awxHosts = []string{"someone", "else"} }
	w2 := runAutoFake(t, f2, testConf())
	if w2.st.Stage != stageFinish || len(f2.qTexts) != 0 {
		t.Errorf("호스트 모름: %s 질문 %v", w2.st.Detail, f2.qTexts)
	}
}

// 확인이 실패하거나(YamlState 오류) stress-test 가 [등록] 줄을 내지 않으면(옛 버전) 묻지 않고 그대로 진행한다.
func TestAutoYamlUnavailable(t *testing.T) {
	mk := func() map[string]*autoHost {
		return map[string]*autoHost{
			"a": {check: "O", cpu: cpuNotRun, stress: cpuNotRun, cpuAfter: 45 * time.Minute},
			"c": {check: "O", cpu: "O", stress: cpuNotRun},
		}
	}
	f := newAutoFake(t, autoT0, []string{"a", "c"}, mk())
	f.onReg = tamperOnFirst
	f.yamlErr = errors.New("AWX 불통")
	w := runAutoFake(t, f, testConf())
	if w.st.Stage != stageFinish || len(f.qTexts) != 0 || !strings.Contains(f.logb.String(), "launch 전 yaml 확인 실패(확인 없이 진행): AWX 불통") {
		t.Errorf("YamlState 오류: %s 질문 %v", w.st.Detail, f.qTexts)
	}
	f2 := newAutoFake(t, autoT0, []string{"a", "c"}, mk())
	f2.onReg = tamperOnFirst
	f2.noReg = true
	w2 := runAutoFake(t, f2, testConf())
	if w2.st.Stage != stageFinish || len(f2.qTexts) != 0 || len(f2.yamlCalls) != 0 || w2.st.Auto.Yml != "" {
		t.Errorf("[등록] 줄 없음: %s 질문 %v 호출 %v yml=%q", w2.st.Detail, f2.qTexts, f2.yamlCalls, w2.st.Auto.Yml)
	}
}

// 답이 안 오는 채로 제한 시간(시험은 60분)이 지나면 질문을 거두고(새 launch 는 없다) 끝낸다. 그동안 12시간 타이머는 흐른다.
func TestAutoYamlPendingAtLimit(t *testing.T) {
	hosts := map[string]*autoHost{
		"a": {check: "O", cpu: cpuNotRun, stress: cpuNotRun},
		"c": {check: "O", cpu: "O", stress: cpuNotRun},
	}
	f := newAutoFake(t, autoT0, []string{"a", "c"}, hosts)
	f.onReg = tamperOnFirst
	f.onQ = func(f *autoFake, q *question) {} // 끝내 답하지 않음
	conf := testConf()
	conf.AutoLimit = 60 * time.Minute
	w := runAutoFake(t, f, conf)
	if got := f.pick("cpu", "ng"); got != "ng:c@0" {
		t.Errorf("사건: %s", got)
	}
	if w.st.Question != nil || w.yamlQ != "" || !w.st.Auto.Ending || !strings.Contains(w.st.Detail, "제한 시간 지남") {
		t.Errorf("끝 상태: q=%+v %s", w.st.Question, w.st.Detail)
	}
	if got := f.Now().Sub(autoT0); got < 60*time.Minute || got > 61*time.Minute {
		t.Errorf("끝난 시각: 시작 뒤 %s", got)
	}
	if !strings.Contains(f.logb.String(), "질문 q1 취소") {
		t.Errorf("취소 로그 없음")
	}
}

func TestYamlChangedAndDelta(t *testing.T) {
	ours := []string{"a", "b"}
	for _, c := range []struct {
		name string
		yml  string
		cur  yamlInfo
		want bool
	}{
		{"같음", "x.yml", yamlInfo{"x.yml", []string{"b", "a"}}, false},
		{"yml 다름", "x.yml", yamlInfo{"y.yml", []string{"a", "b"}}, true},
		{"호스트 추가", "x.yml", yamlInfo{"x.yml", []string{"a", "b", "c"}}, true},
		{"호스트 빠짐", "x.yml", yamlInfo{"x.yml", []string{"a"}}, true},
		{"인벤토리 비움", "x.yml", yamlInfo{"x.yml", nil}, true},
	} {
		if got := yamlChanged(c.yml, ours, c.cur); got != c.want {
			t.Errorf("%s: %v", c.name, got)
		}
	}
	if yamlChanged("x.yml", nil, yamlInfo{"x.yml", []string{"q"}}) {
		t.Errorf("기억한 호스트가 없으면 yml 만 비교해야 함")
	}
	add, del := hostDelta([]string{"a", "b", "c"}, []string{"c", "d", "b", "e"})
	eqList(t, "추가", add, []string{"d", "e"})
	eqList(t, "빠짐", del, []string{"a"})
	if got := listMax([]string{"a", "b", "c", "d", "e", "f", "g"}, 5); got != "a b c d e 외 2대" {
		t.Errorf("listMax: %s", got)
	}
	if listMax(nil, 5) != "없음" {
		t.Errorf("listMax 빈 목록")
	}
}

func TestParseYamlLines(t *testing.T) {
	if y, h, ok := parseRegistered("yml=tester_cpu_2ea.yml hosts=n1,n2"); !ok || y != "tester_cpu_2ea.yml" || strings.Join(h, ",") != "n1,n2" {
		t.Errorf("parseRegistered: %q %v %v", y, h, ok)
	}
	if y, h, ok := parseRegistered("yml=x.yml"); !ok || y != "x.yml" || h != nil {
		t.Errorf("hosts 없음: %q %v %v", y, h, ok)
	}
	if _, _, ok := parseRegistered("hosts=a"); ok {
		t.Errorf("yml 없는 줄을 받아들임")
	}
	out := "[i] 무시\n[yaml상태] yml=other.yml\n[yaml상태] source=11 inventory=7\n[yaml상태] count=2\n[yaml상태] hosts=a,b\n"
	st, err := parseYamlStateOutput(out)
	if err != nil || st.Yml != "other.yml" || strings.Join(st.Hosts, ",") != "a,b" {
		t.Errorf("parseYamlStateOutput: %+v %v", st, err)
	}
	if st, err := parseYamlStateOutput("[yaml상태] yml=z.yml\n[yaml상태] count=0\n[yaml상태] hosts=\n"); err != nil || st.Yml != "z.yml" || len(st.Hosts) != 0 {
		t.Errorf("빈 호스트: %+v %v", st, err)
	}
	if _, err := parseYamlStateOutput("오류: 설정 파일이 없습니다\n"); err == nil {
		t.Errorf("yml 없는 출력은 오류여야 함")
	}
}

// lineWatch 는 "[등록]" 줄을 머리말을 떼고 reg 로 넘긴다(감시 줄·사용 중 줄과 따로).
func TestLineWatchRegistered(t *testing.T) {
	var regs []string
	n := 0
	lw := &lineWatch{mark: []byte(stressMonitorMark), fn: func() { n++ }, regMark: []byte(stressRegisteredMark), reg: func(r string) { regs = append(regs, r) }}
	lw.Write([]byte("[i] 묶음 등록\n[등록] yml=a_2ea.yml hos"))
	lw.Write([]byte("ts=n1,n2\n[감시] a 경과\n"))
	if n != 1 || len(regs) != 1 || regs[0] != "yml=a_2ea.yml hosts=n1,n2" {
		t.Errorf("n=%d regs=%q", n, regs)
	}
}

// 진짜 stress-test 대신 가짜 stress-test 스크립트를 써서 실행기(realExec)의 [등록] 줄 전달과 -yaml-state 호출·파싱을 본다.
func TestRealExecYamlWithFakeScript(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "fake_st.sh")
	script := `#!/bin/bash
for a in "$@"; do [ "$a" = "-yaml-state" ] && ys=1; done
if [ -n "${ys:-}" ]; then
  echo "[yaml상태] yml=${FAKE_YML:-cur.yml}"
  echo "[yaml상태] source=11 inventory=7"
  echo "[yaml상태] count=2"
  echo "[yaml상태] hosts=h1,h2"
  exit "${FAKE_YS_EXIT:-0}"
fi
echo "[등록] yml=reg_1ea.yml hosts=h1"
echo "[감시] a 경과"
`
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	var log strings.Builder
	conf := parseConf("")
	conf.StressTest = bin
	e := &realExec{conf: conf, user: "tester", dir: dir, log: &log}
	st, err := e.YamlState()
	if err != nil || st.Yml != "cur.yml" || strings.Join(st.Hosts, ",") != "h1,h2" {
		t.Fatalf("YamlState: %+v %v", st, err)
	}
	if !strings.Contains(log.String(), "-user tester -yaml-state") {
		t.Errorf("호출 인자: %s", log.String())
	}
	t.Setenv("FAKE_YS_EXIT", "2")
	if _, err := e.YamlState(); err == nil {
		t.Errorf("종료코드 2 는 오류여야 함")
	}
	list := filepath.Join(dir, "l.txt")
	os.WriteFile(list, []byte("h1\n"), 0o644)
	var gotYml string
	var gotHosts []string
	launched := 0
	code, err := e.StressTest("a", list, []string{"h1"}, "", func() { launched++ }, func(y string, h []string) { gotYml, gotHosts = y, h }, nil)
	if err != nil || code != 0 || launched != 1 || gotYml != "reg_1ea.yml" || strings.Join(gotHosts, ",") != "h1" {
		t.Errorf("StressTest: code=%d err=%v launched=%d yml=%q hosts=%v", code, err, launched, gotYml, gotHosts)
	}
}
