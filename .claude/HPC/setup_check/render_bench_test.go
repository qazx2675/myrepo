package main

import (
	"fmt"
	"testing"
	"time"
)

// bigState 는 n 대짜리 상태(일부는 O 가 아닌 호스트)를 만든다.
func bigState(n int) *runState {
	st := sampleState()
	st.Hosts = nil
	for i := 0; i < n; i++ {
		r := hostRow{Host: fmt.Sprintf("node%05d", i), Status: stDone, Check: "O", CPU: "O", Stress: "O"}
		if i%7 == 0 {
			r.Stress, r.Note = cpuNotRun, "stress-ng 미실행 사유 예시"
		}
		st.Hosts = append(st.Hosts, r)
	}
	return st
}

func benchRender(b *testing.B, onlyBad bool) {
	st := bigState(8000)
	v := viewState{OnlyBad: onlyBad, Scroll: 3000}
	now := time.Now()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		renderRun(st, v, 200, 50, now, true)
	}
}

// 8000대 화면 한 장 그리기 비용(스크롤이 중간인 200x50 터미널).
func BenchmarkRenderRun8000(b *testing.B)        { benchRender(b, false) }
func BenchmarkRenderRun8000OnlyBad(b *testing.B) { benchRender(b, true) }
