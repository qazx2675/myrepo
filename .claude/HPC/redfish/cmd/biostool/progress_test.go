package main

import (
	"bytes"
	"strings"
	"sync"
	"testing"
	"time"
)

// 가짜 시계가 달린 진행 표시 (Start 없이 tick 을 직접 부른다).
func newTestProgress(interactive bool) (*progress, *bytes.Buffer, *time.Time) {
	var buf bytes.Buffer
	clock := time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC)
	p := newProgress(&buf, interactive, 4)
	p.now = func() time.Time { return clock }
	return p, &buf, &clock
}

func TestProgressInteractiveRender(t *testing.T) {
	p, buf, clock := newTestProgress(true)
	p.Stage(1, "설정 확인", 0)
	p.Stage(2, "접속·조회", 1000)
	for i := 0; i < 100; i++ {
		p.Add(TagOK)
	}
	for i := 0; i < 20; i++ {
		p.Add(TagDiff)
	}
	for i := 0; i < 3; i++ {
		p.Add(TagNA)
	}
	*clock = clock.Add(12 * time.Second)
	p.tick()
	want := "\r[2/4 접속·조회 123/1000 · OK 100 difference 20 N/A 3 오류 0 · 경과 00:12]\x1b[K"
	if !strings.HasSuffix(buf.String(), want) {
		t.Fatalf("마지막 그리기가 다름:\n%q\n끝이 %q 이어야 함", buf.String(), want)
	}
	// 1단계는 끝날 때 줄바꿈으로 닫혔다.
	if !strings.HasPrefix(buf.String(), "\r[1/4 설정 확인]\x1b[K\r[1/4 설정 확인]\x1b[K\n\r[2/4 접속·조회 0/1000") {
		t.Fatalf("단계 전환 출력이 다름: %q", buf.String())
	}
	p.Stop()
	if !strings.HasSuffix(buf.String(), want+"\n") {
		t.Fatalf("Stop 뒤에는 새 줄에서 끝나야 함: %q", buf.String())
	}
	n := buf.Len()
	p.Stop() // 두 번 불러도 아무것도 찍지 않는다
	p.tick()
	if buf.Len() != n {
		t.Fatalf("Stop 뒤 출력이 늘었음: %q", buf.String()[n:])
	}
}

func TestProgressHostLine(t *testing.T) {
	p, buf, _ := newTestProgress(true)
	p.Stage(3, "설정", 2)
	buf.Reset()
	p.HostLine("h1", "OK")
	want := "\r\x1b[Kh1: OK\n\r[3/4 설정 0/2 · OK 0 difference 0 N/A 0 오류 0 · 경과 00:00]\x1b[K"
	if buf.String() != want {
		t.Fatalf("got %q want %q", buf.String(), want)
	}

	q, b2, _ := newTestProgress(false)
	q.Stage(3, "설정", 2)
	b2.Reset()
	q.HostLine("h1", "OK")
	if b2.String() != "h1: OK\n" {
		t.Fatalf("비대화형 호스트 줄: %q", b2.String())
	}
}

func TestProgressPlainCadence(t *testing.T) {
	p, buf, clock := newTestProgress(false)
	p.Stage(2, "접속·조회", 10)
	if got := strings.Count(buf.String(), "\n"); got != 1 {
		t.Fatalf("단계 시작에 한 줄이어야 함: %q", buf.String())
	}
	p.Add(TagOK)
	for s := 1; s <= 9; s++ { // 9초까지는 아무것도 찍지 않는다
		*clock = clock.Add(time.Second)
		p.tick()
	}
	if got := strings.Count(buf.String(), "\n"); got != 1 {
		t.Fatalf("10초 전에 찍었음: %q", buf.String())
	}
	*clock = clock.Add(time.Second) // 10초
	p.tick()
	if got := strings.Count(buf.String(), "\n"); got != 2 {
		t.Fatalf("10초에 한 줄이어야 함: %q", buf.String())
	}
	if !strings.Contains(buf.String(), "[2/4 접속·조회 1/10 · OK 1 difference 0 N/A 0 오류 0 · 경과 00:10]\n") {
		t.Fatalf("줄 내용이 다름: %q", buf.String())
	}
	*clock = clock.Add(10 * time.Second)
	p.tick()
	if got := strings.Count(buf.String(), "\n"); got != 3 {
		t.Fatalf("다음 10초에 또 한 줄이어야 함: %q", buf.String())
	}
	p.Stop() // 단계 끝 한 줄
	if got := strings.Count(buf.String(), "\n"); got != 4 {
		t.Fatalf("단계 끝에 한 줄이어야 함: %q", buf.String())
	}
	if strings.ContainsAny(buf.String(), "\r\x1b") {
		t.Fatalf("비대화형에 제어 문자가 있음: %q", buf.String())
	}
}

func TestProgressPlainNoCounterStageOnce(t *testing.T) {
	p, buf, _ := newTestProgress(false)
	p.Stage(3, "CSV 저장", 0)
	p.Stage(4, "TUI", 0)
	p.Stop()
	if buf.String() != "[3/4 CSV 저장]\n[4/4 TUI]\n" {
		t.Fatalf("got %q", buf.String())
	}
}

func TestProgressConcurrentAdd(t *testing.T) {
	var buf bytes.Buffer
	p := newProgress(&buf, true, 4)
	p.Stage(2, "접속·조회", 800)
	p.Start()
	var wg sync.WaitGroup
	for w := 0; w < 8; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				p.Add(TagOK)
				if i%25 == 0 {
					p.HostLine("h", "x")
				}
			}
		}()
	}
	wg.Wait()
	p.Stop()
	if p.done != 800 || p.counts[TagOK] != 800 {
		t.Fatalf("done=%d ok=%d, want 800", p.done, p.counts[TagOK])
	}
	if !strings.Contains(buf.String(), "800/800 · OK 800") {
		t.Fatalf("마지막 줄에 800/800 이 없음: %q", buf.String())
	}
	if !strings.HasSuffix(buf.String(), "\n") {
		t.Fatalf("새 줄에서 끝나야 함")
	}
}
