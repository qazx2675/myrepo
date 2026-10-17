// firstdown_test.go - AWX 실행(접수) 때부터 접속불가였던 서버의 흐름 (os8 경로·os6 경로 각각):
// 접속불가 → (기존 OS 부팅 → 재부팅) → anaconda(보이거나 안 보이거나) → 새 OS, 끝내 안 올라옴, 기존 OS 로만 올라옴 등.
// 기준: 새 OS 로 올라온 뒤에만 run 이 처리하고, 기존 OS 로 잠깐 올라온 동안에는 READY·처리하지 않는다.
package main

import (
	"fmt"
	"testing"
)

const (
	tlDown = iota // 꺼짐·응답 없음 (PXE·POST·anaconda 중 ssh 불가 포함)
	tlOld         // 기존 OS 로 부팅됨
	tlAna         // anaconda 설치 환경 (gossh 가 설치중으로 봄)
	tlNew         // 새 OS
)

// tlSeg: from 초부터 kind 상태 (다음 구간 전까지)
type tlSeg struct {
	from int64
	kind int
}

func (s *simHost) tlAt(o int64) tlSeg {
	cur := tlSeg{kind: tlDown}
	for _, g := range s.tl {
		if o >= g.from {
			cur = g
		}
	}
	return cur
}

// tlCheck: 준비확인 결과. 기존 OS 는 설치 기록(anaconda-ks.cfg)이 오래됨 → OldOS, 새 OS 는 방금 설치.
func (s *simHost) tlCheck(o int64) CheckResult {
	g := s.tlAt(o)
	switch g.kind {
	case tlOld:
		return CheckResult{Responded: true, Uptime: float64(o - g.from), OldOS: true}
	case tlAna:
		return CheckResult{Responded: true, Anaconda: true, Uptime: float64(o - g.from)}
	case tlNew:
		return CheckResult{Responded: true, Uptime: float64(o - g.from)}
	}
	return CheckResult{}
}

type fdCase struct {
	name     string
	tl       []tlSeg
	complete bool  // 끝내 완료돼야 하나
	until    int64 // 진행 시간
}

func firstDownCases() []fdCase {
	return []fdCase{
		{"1 접속불가→기존OS 부팅→재부팅→anaconda→완료",
			[]tlSeg{{0, tlDown}, {300, tlOld}, {600, tlDown}, {660, tlAna}, {1500, tlDown}, {1560, tlNew}}, true, 3000},
		{"2 접속불가→anaconda→완료",
			[]tlSeg{{0, tlDown}, {300, tlAna}, {1200, tlDown}, {1260, tlNew}}, true, 2700},
		{"3 접속불가→(anaconda 안 보임)→완료",
			[]tlSeg{{0, tlDown}, {1500, tlNew}}, true, 3000},
		{"4 접속불가→기존OS 부팅→재부팅→(anaconda 안 보임)→완료",
			[]tlSeg{{0, tlDown}, {300, tlOld}, {600, tlDown}, {1800, tlNew}}, true, 3300},
		{"5 접속불가→기존OS 부팅 여러 번→anaconda→완료",
			[]tlSeg{{0, tlDown}, {200, tlOld}, {400, tlDown}, {460, tlOld}, {700, tlDown}, {760, tlAna}, {1400, tlDown}, {1460, tlNew}}, true, 3000},
		{"6 접속불가→기존OS 로만 올라옴(PXE 실패)",
			[]tlSeg{{0, tlDown}, {300, tlOld}}, false, 5000},
		{"7 끝내 안 올라옴",
			[]tlSeg{{0, tlDown}}, false, 5000},
		{"8 접수 직후 잠깐 기존OS→곧바로 anaconda",
			[]tlSeg{{0, tlOld}, {20, tlDown}, {80, tlAna}, {900, tlDown}, {960, tlNew}}, true, 2500},
	}
}

func TestFirstDownScenarios(t *testing.T) {
	for _, route := range []string{"os8", "os6"} {
		for _, c := range firstDownCases() {
			t.Run(route+" "+c.name, func(t *testing.T) {
				setupDir(t)
				withRoute6(t, "mgmt", "/os6/gossh")
				h := &simHost{tl: c.tl, remote: route == "os6"}
				w := newWorld(map[string]*simHost{"h1": h})
				id := submit(t, "u1", 0, "h1")
				d := drive(t, w, newTestDaemon(w), 0, c.until)
				for _, r := range w.runs {
					if k := h.tlAt(r.At).kind; k != tlNew {
						t.Fatalf("새 OS 가 아닐 때(%s) run: %+v", map[int]string{tlDown: "꺼짐", tlOld: "기존OS", tlAna: "anaconda"}[k], w.runs)
					}
				}
				var hh *Host
				if j := d.jobs[id]; j != nil {
					hh = j.Hosts["h1"]
				} else {
					hh = doneJob(t, id).Hosts["h1"]
				}
				if c.complete && hh.Processed == "" {
					t.Fatalf("완료돼야 함: %+v runs=%v", hh, w.runs)
				}
				if !c.complete {
					if hh.Processed != "" {
						t.Fatalf("완료되면 안 됨: %+v", hh)
					}
					if st := effectiveStage(hh, base+c.until); st != StageStuck {
						t.Fatalf("정체로 보여야 함: %s %+v", st, hh)
					}
				}
				if c.complete && (route == "os6" && hh.Route != "os6" || route == "os8" && hh.Route != "local") { // 끝내 안 올라오면 경로는 정해질 수 없음
					t.Fatalf("경로: %q", hh.Route)
				}
				_ = fmt.Sprint()
			})
		}
	}
}
