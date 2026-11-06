package main

// stress-ng 종료 시각 입력 질문과 인벤토리 사용 중(단계 중간) 질문의 워커 시험.

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func loadWorld() (map[string]*fakeHost, []string) {
	return map[string]*fakeHost{
		"a": {status: stDone, check: "O", cpu: cpuNotRun, stress: cpuNotRun, cpuReady: 5 * time.Minute},
		"b": {status: stDone, check: "O", cpu: "O", stress: cpuNotRun},
	}, []string{"a", "b"}
}

func logText(w *worker) string { return w.log.(*strings.Builder).String() }

// 부하 단계(3)와 stress-ng 단계(4) 모두 실행 전에 종료 시각을 묻고, 값은 stress-ng(b) 에만 넘긴다. CPU(a) 는 값이 없다.
func TestWorkerEndQuestion(t *testing.T) {
	world, order := loadWorld()
	f := newFake(t, order, world, "c", "c", "c")
	w := runFake(t, f, nil)
	if w.st.Stage != stageFinish || w.st.Stopped {
		t.Fatalf("완료가 아님: %d %v", w.st.Stage, w.st.Stopped)
	}
	eqList(t, "입력 질문", f.endAsked, []string{"부하:cs", "stress-ng:cs"})
	eqList(t, "일반 질문", f.asked, []string{"판정:crs=c", "부하:crs=c", "stress-ng:crs=c"})
	eqList(t, "-end 전달", f.ends, []string{"a:", "b:2026,11,06,18,00", "b:2026,11,06,18,00"})
	if w.st.StressEnd != "2026,11,06,18,00" {
		t.Errorf("stress_end=%q", w.st.StressEnd)
	}
	// 3단계: 종료 시각 질문이 CPU 시작보다 먼저
	if q, s := f.idx("endq:부하"), f.idx("start:a"); q < 0 || s < 0 || q > s {
		t.Errorf("종료 시각 질문이 CPU 시작 뒤: %v", f.events)
	}
	if q, s := f.idx("endq:stress-ng"), f.idx("start:b:a"); q < 0 || s < 0 || q > s {
		t.Errorf("4단계 종료 시각 질문이 stress-ng 시작 뒤: %v", f.events)
	}
	b, _ := os.ReadFile(filepath.Join(w.dir, stateFile))
	if !strings.Contains(string(b), `"stress_end": "2026,11,06,18,00"`) {
		t.Errorf("state.json 에 stress_end 없음:\n%s", b)
	}
}

// stress-ng 대상이 없으면(CPU 만) 3단계에서 종료 시각을 묻지 않는다. 입력은 그대로("07") 전달.
func TestWorkerEndQuestionOnlyWithStressNG(t *testing.T) {
	world := map[string]*fakeHost{"a": {status: stDone, check: "O", cpu: cpuNotRun, stress: cpuNotRun, cpuReady: 5 * time.Minute}}
	f := newFake(t, []string{"a"}, world, "c", "c", "c")
	f.endVals = map[string]string{"end_year": "2026", "end_month": "12", "end_day": "07", "end_hour": "08", "end_min": "05"}
	w := runFake(t, f, nil)
	eqList(t, "입력 질문(4단계만)", f.endAsked, []string{"stress-ng:cs"})
	eqList(t, "-end 전달", f.ends, []string{"a:", "b:2026,12,07,08,05"})
	if w.st.StressEnd != "2026,12,07,08,05" {
		t.Errorf("stress_end=%q", w.st.StressEnd)
	}
}

// 틀린 값이 담긴 답은 무시하고 다시 기다린다(마지막 값만 받아들임).
func TestWorkerEndQuestionRejectsBadValues(t *testing.T) {
	world, order := loadWorld()
	f := newFake(t, order, world, "c", "c", "c")
	bad := map[string]string{"end_year": "2020", "end_month": "11", "end_day": "06", "end_hour": "18", "end_min": "00"}
	past := map[string]string{"end_year": "2026", "end_month": "11", "end_day": "05", "end_hour": "08", "end_min": "59"}
	f.endSeq = []map[string]string{bad, past}
	w := runFake(t, f, nil)
	if w.st.StressEnd != "2026,11,06,18,00" {
		t.Errorf("stress_end=%q", w.st.StressEnd)
	}
	if n := strings.Count(logText(w), "무시한 답: 입력값 오류"); n != 2 {
		t.Errorf("입력값 오류 무시 2회 기대, %d회:\n%s", n, logText(w))
	}
}

// 종료 시각 질문에서 중지: stress-test 를 띄우지 않고 중지
func TestWorkerEndQuestionStop(t *testing.T) {
	world, order := loadWorld()
	f := newFake(t, order, world, "c")
	f.endChoice = ansStop
	w := runFake(t, f, nil)
	if !w.st.Stopped || w.st.Stage != stageLoad {
		t.Fatalf("부하 단계에서 중지여야 함: %d %v", w.st.Stage, w.st.Stopped)
	}
	if f.idx("start:") >= 0 || w.st.StressEnd != "" {
		t.Errorf("중지인데 stress-test 가 시작됨: %v end=%q", f.events, w.st.StressEnd)
	}
	if _, err := os.Stat(filepath.Join(w.dir, resultFile)); err != nil {
		t.Errorf("중지 시에도 result.txt: %v", err)
	}
}

// stress-ng(b) 가 인벤토리 사용 중 → 단계 그대로 r/x 질문 → r 이면 계속
func TestWorkerBusyRetry(t *testing.T) {
	world, order := loadWorld()
	f := newFake(t, order, world, "c", "r", "c", "c")
	f.busyMsg = map[string]string{"b": "템플릿 stress(7) 인벤토리가 hong 입니다(DEFAULT 아님)"}
	w := runFake(t, f, nil)
	if w.st.Stage != stageFinish || w.st.Stopped {
		t.Fatalf("완료가 아님: %d %v", w.st.Stage, w.st.Stopped)
	}
	eqList(t, "질문", f.asked, []string{"판정:crs=c", "부하:rx=r", "부하:crs=c", "stress-ng:crs=c"})
	eqList(t, "busy 답", f.busyAns, []string{"b:r"})
	if w.st.Question != nil {
		t.Errorf("질문이 남음: %+v", w.st.Question)
	}
	if !strings.Contains(logText(w), "stress-ng 인벤토리 사용 중") {
		t.Errorf("로그에 인벤토리 사용 중 질문이 없음:\n%s", logText(w))
	}
}

// x 이면 그 묶음 중단: stress-test 종료코드 1 → 부하 단계 질문에 안내가 붙고 stress-ng 는 NOTRUN 그대로
func TestWorkerBusyExclude(t *testing.T) {
	world := map[string]*fakeHost{"b": {status: stDone, check: "O", cpu: "O", stress: cpuNotRun}}
	f := newFake(t, []string{"b"}, world, "c", "x", "s")
	f.busyMsg = map[string]string{"b": "사용 중"}
	w := runFake(t, f, nil)
	eqList(t, "질문", f.asked, []string{"판정:crs=c", "부하:rx=x", "부하:crxs=s"})
	if !w.st.Stopped || row(t, w, "b").Stress != cpuNotRun {
		t.Errorf("stopped=%v stress=%s", w.st.Stopped, row(t, w, "b").Stress)
	}
	if !strings.Contains(logText(w), "미실행/실패 호스트가 있습니다") {
		t.Errorf("종료코드 1 안내가 질문에 없음:\n%s", logText(w))
	}
}

// 두 프로세스가 동시에 걸려도 질문은 한 번에 하나(앞 질문에 답해야 다음 질문이 뜬다)
func TestWorkerBusySerialized(t *testing.T) {
	f := newFake(t, nil, nil, "r", "x")
	w := newWorker(t.TempDir(), &runState{Version: stateVersion, Stage: stageLoad}, f, testConf(), nil, &strings.Builder{})
	f.w = w
	var wg sync.WaitGroup
	res := make([]string, 2)
	for i, name := range []string{"CPU", "stress-ng"} {
		wg.Add(1)
		go func(i int, name string) {
			defer wg.Done()
			res[i] = w.busyAsk(name, "msg "+name)
		}(i, name)
	}
	wg.Wait()
	if !(res[0] == "r" && res[1] == "x") && !(res[0] == "x" && res[1] == "r") {
		t.Errorf("각자 한 번씩 답을 받아야 함: %v", res)
	}
	if len(f.asked) != 2 || w.st.QSeq != 2 || w.st.Question != nil {
		t.Errorf("asked=%v seq=%d q=%v", f.asked, w.st.QSeq, w.st.Question)
	}
	if !w.st.Busy {
		t.Errorf("답한 뒤 busy 가 복원돼야 함")
	}
}

// 인벤토리 사용 중 질문 대기 중 중지 요청 → x
func TestWorkerBusyStop(t *testing.T) {
	f := newFake(t, nil, nil)
	f.stopOnAsk = true
	w := newWorker(t.TempDir(), &runState{Version: stateVersion, Stage: stageLoad, Busy: true, Detail: "부하"}, f, testConf(), nil, &strings.Builder{})
	f.w = w
	if got := w.busyAsk("CPU", "사용 중"); got != ansExclude {
		t.Errorf("중지 요청이면 x 여야 함: %q", got)
	}
	if w.st.Question != nil {
		t.Errorf("질문이 남음: %+v", w.st.Question)
	}
}
