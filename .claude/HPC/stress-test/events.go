package main

import (
	"strings"
)

// new_setup 이 읽는 -batch 전용 stdout 줄이다 (대화형에서는 내지 않는다).
//
//	[제외] stage=a hosts=h1,h2 reason=접속불가   (launch 에서 뺀 호스트가 있을 때, 사유별로 한 줄)
//	[job] stage=a id=123                         (AWX job 이 만들어진 직후)
//	[결과] stage=b done=h1 notrun=h3 nomon=h4    (감시 종료 때, 빈 목록은 = 뒤를 비운다)
//	[삭제확인] before=3 after=0                  (인벤토리 삭제 확인 통과 때, 제외 호스트는 뺀 대수)

// stageLetter 는 단계 이름을 줄에 쓰는 글자(a=cpu, b=stress-ng)로 바꾼다.
func stageLetter(stage string) string {
	if stage == stageStress {
		return "b"
	}
	return "a"
}

func hostCSV(list []*target) string {
	names := make([]string, 0, len(list))
	for _, t := range list {
		names = append(names, t.Host)
	}
	return strings.Join(names, ",")
}

// emitExcluded 는 단계 시작 전에, 접속체크·DNS 실패로 launch 대상에서 빠진 호스트를 [제외] 줄로 낸다.
// -ignore-offline 로 launch 에 남긴 호스트와 빠진 호스트가 없는 경우는 내지 않는다.
func (a *app) emitExcluded(stage string) {
	if !a.batchMode {
		return
	}
	var off, dns []*target
	for _, t := range a.targets {
		switch t.Res[stage] {
		case resOffline:
			off = append(off, t)
		case resDNSFail:
			dns = append(dns, t)
		}
	}
	if len(off) > 0 {
		a.logf("%sstage=%s hosts=%s reason=접속불가", excludedMark, stageLetter(stage), hostCSV(off))
	}
	if len(dns) > 0 {
		a.logf("%sstage=%s hosts=%s reason=DNS실패", excludedMark, stageLetter(stage), hostCSV(dns))
	}
}

// emitJob 은 AWX job 이 만들어진 직후 [job] 줄을 낸다.
func (a *app) emitJob(stage string, job int) {
	if !a.batchMode {
		return
	}
	a.logf("%sstage=%s id=%d", jobMark, stageLetter(stage), job)
}

// emitResult 는 감시가 끝났을 때 [결과] 줄을 낸다. done=감시 대상 중 실행 확인, notrun=남은 호스트, nomon=감시불가.
func (a *app) emitResult(stage string, monList, remaining, noMon []*target) {
	if !a.batchMode {
		return
	}
	rem := map[*target]bool{}
	for _, t := range remaining {
		rem[t] = true
	}
	var done []*target
	for _, t := range monList {
		if !rem[t] {
			done = append(done, t)
		}
	}
	a.logf("%sstage=%s done=%s notrun=%s nomon=%s", resultMark, stageLetter(stage), hostCSV(done), hostCSV(remaining), hostCSV(noMon))
}

// emitDeleteCheck 는 인벤토리 삭제 확인이 통과했을 때 [삭제확인] 줄을 낸다.
func (a *app) emitDeleteCheck(before, after int) {
	if !a.batchMode {
		return
	}
	a.logf("%sbefore=%d after=%d", deleteCheckMark, before, after)
}
