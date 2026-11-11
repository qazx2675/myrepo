package main

import (
	"strings"
	"testing"
)

func TestEmitLinesBatch(t *testing.T) {
	a, _, _ := newTestApp(t)
	var out strings.Builder
	a.out = &out
	a.batchMode = true
	h := func(n string, res string) *target {
		return &target{Host: n, Res: map[string]string{stageCPU: res, stageStress: res}}
	}
	a.targets = []*target{h("n1", resNotRun), h("n2", resOffline), h("n3", resOffline), h("n4", resDNSFail)}
	a.emitExcluded(stageCPU)
	a.emitJob(stageStress, 31)
	ts := []*target{h("a1", ""), h("a2", ""), h("a3", "")}
	a.emitResult(stageStress, ts[:2], ts[1:2], ts[2:])
	a.emitResult(stageCPU, ts[:1], nil, nil)
	want := "[제외] stage=a hosts=n2,n3 reason=접속불가\n[제외] stage=a hosts=n4 reason=DNS실패\n" +
		"[job] stage=b id=31\n" +
		"[결과] stage=b done=a1 notrun=a2 nomon=a3\n" +
		"[결과] stage=a done=a1 notrun= nomon=\n"
	if out.String() != want {
		t.Errorf("출력:\n%s\n기대:\n%s", out.String(), want)
	}
	// 빠진 호스트가 없으면 [제외] 없음 (-ignore-offline 로 launch 에 남긴 경우 포함)
	out.Reset()
	a.targets = []*target{h("n1", resNotRun), h("n2", resNotRun)}
	a.emitExcluded(stageCPU)
	if out.Len() != 0 {
		t.Errorf("낼 줄이 없어야 함: %q", out.String())
	}
}

func TestEmitLinesNotInBatchOff(t *testing.T) {
	a, _, _ := newTestApp(t)
	var out strings.Builder
	a.out = &out
	a.targets = []*target{{Host: "n2", Res: map[string]string{stageCPU: resOffline}}}
	a.emitExcluded(stageCPU)
	a.emitJob(stageCPU, 1)
	a.emitResult(stageCPU, a.targets, nil, nil)
	if out.Len() != 0 {
		t.Errorf("-batch 가 아니면 내지 않아야 함: %q", out.String())
	}
}

func TestLaunchBatchJobLine(t *testing.T) {
	for _, batchMode := range []bool{true, false} {
		a, _, _ := newTestApp(t)
		var out strings.Builder
		a.out = &out
		a.batchMode = batchMode
		if !a.launchBatch(stageStress, batch{name: "stress", tc: a.cfg.Stress, list: twoTargets()}, 0) {
			t.Fatal("성공이어야 함")
		}
		got := strings.Contains(out.String(), "\n[job] stage=b id=31\n") || strings.HasPrefix(out.String(), "[job] stage=b id=31\n")
		if got != batchMode {
			t.Errorf("batchMode=%v [job] 줄=%v\n%s", batchMode, got, out.String())
		}
	}
}
