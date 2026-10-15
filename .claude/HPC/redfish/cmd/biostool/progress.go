package main

// progress.go — 진행 표시 (계획서_v0.2_TUI.md "진행 표시").
//
//	[1/4 설정 확인] [2/4 접속·조회 123/1000 · OK 100 difference 20 N/A 3 오류 0 · 경과 00:12] [3/4 CSV 저장] [4/4 TUI]
//
// 지금 단계 하나만 한 줄로 보여 줍니다.
//   - 대화형(터미널): 한 줄을 "\r"+지우기로 제자리 갱신 (약 200ms 마다, 단계가 바뀔 때), 단계가 끝나면 줄바꿈.
//   - 비대화형: "\r" 없이 10초마다 한 줄 (단계 시작·끝에도 한 줄).
//
// 카운터(Add)는 워커 고루틴에서 불러도 됩니다. 색은 쓰지 않습니다 (결과 줄과 섞이지 않게 평문).

import (
	"fmt"
	"io"
	"os"
	"sync"
	"time"
)

// progTagErr 는 호스트 단위 오류 카운터 이름입니다 (TagOK/TagDiff/TagNA 는 resultline.go).
const progTagErr = "오류"

const (
	progTickTTY   = 200 * time.Millisecond // 대화형 갱신 주기
	progTickPlain = time.Second            // 비대화형은 1초마다 깨어 10초가 지났는지 본다
	progPlainGap  = 10 * time.Second       // 비대화형 출력 간격
)

type progress struct {
	mu          sync.Mutex
	w           io.Writer
	interactive bool
	sink        func(line string) // nil 이 아니면 w 대신 진행 줄을 여기로만 넘긴다 (TUI: TUIJob.Set)
	now         func() time.Time  // 테스트에서 바꿔 끼움
	stages      int               // 전체 단계 수 (예: 4)

	idx    int // 지금 단계 번호 (0 = 아직 시작 안 함)
	name   string
	total  int // 0 이면 카운터를 보여 주지 않는 단계
	done   int
	counts map[string]int
	begin  time.Time
	last   time.Time // 마지막으로 그린(찍은) 시각
	drawn  bool      // 대화형: 진행 줄이 화면에 남아 있음 (줄바꿈 전)
	active bool      // 단계 진행 중

	quit chan struct{}
	wg   sync.WaitGroup
}

// newProgress 는 w 에 쓰는 진행 표시를 만듭니다. interactive 가 true 면 제자리 갱신입니다.
func newProgress(w io.Writer, interactive bool, stages int) *progress {
	return &progress{w: w, interactive: interactive, now: time.Now, stages: stages, counts: map[string]int{}}
}

// newStderrProgress 는 표준에러에 쓰는 진행 표시입니다 (표준출력은 결과 줄 전용). 표준에러가 터미널이면 대화형.
func newStderrProgress(stages int) *progress {
	return newProgress(os.Stderr, ttyIsTerminal(int(os.Stderr.Fd())), stages)
}

// newSinkProgress 는 진행 줄을 sink 로만 넘기는 진행 표시입니다 (TUI 가 열려 있는 동안 표준출력·표준에러에 쓰지 않게).
// 대화형처럼 약 200ms 마다·단계가 바뀔 때 sink 를 부른다.
func newSinkProgress(stages int, sink func(line string)) *progress {
	p := newProgress(io.Discard, true, stages)
	p.sink = sink
	return p
}

// Start 는 주기 갱신 고루틴을 시작합니다 (Stop 으로 끝낸다).
func (p *progress) Start() {
	p.mu.Lock()
	if p.quit != nil {
		p.mu.Unlock()
		return
	}
	p.quit = make(chan struct{})
	every := progTickPlain
	if p.interactive {
		every = progTickTTY
	}
	quit := p.quit
	p.wg.Add(1)
	p.mu.Unlock()
	go func() {
		defer p.wg.Done()
		t := time.NewTicker(every)
		defer t.Stop()
		for {
			select {
			case <-t.C:
				p.tick()
			case <-quit:
				return
			}
		}
	}()
}

// Stage 는 이전 단계를 마치고 idx 번째 단계를 시작합니다. total 이 0 이 아니면 done/total·집계·경과를 보여 줍니다.
func (p *progress) Stage(idx int, name string, total int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.finishLocked()
	p.idx, p.name, p.total, p.done = idx, name, total, 0
	p.counts = map[string]int{}
	p.begin = p.now()
	p.active = true
	p.drawLocked()
}

// Add 는 호스트 1대를 처리했다고 알리고 tag(TagOK/TagDiff/TagNA/progTagErr)를 센다. 화면은 다음 주기에 갱신된다.
func (p *progress) Add(tag string) {
	p.mu.Lock()
	p.done++
	p.counts[tag]++
	p.mu.Unlock()
}

// HostLine 은 진행 줄 위에 한 줄(host: msg)을 찍고 진행 줄을 다시 그린다 (설정 단계의 호스트별 줄).
func (p *progress) HostLine(host, msg string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.interactive && p.drawn {
		fmt.Fprint(p.w, "\r\x1b[K")
		p.drawn = false
	}
	fmt.Fprintf(p.w, "%s: %s\n", host, msg)
	if p.interactive && p.active {
		p.drawLocked()
	}
}

// Stop 은 지금 단계를 마치고 고루틴을 멈춘다. 터미널은 새 줄에서 끝난다.
func (p *progress) Stop() {
	p.mu.Lock()
	p.finishLocked()
	q := p.quit
	p.quit = nil
	p.mu.Unlock()
	if q != nil {
		close(q)
		p.wg.Wait()
	}
}

// tick 은 주기마다 불린다: 대화형은 다시 그리고, 비대화형은 10초가 지났을 때만 한 줄 찍는다.
func (p *progress) tick() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.active {
		return
	}
	if !p.interactive && p.now().Sub(p.last) < progPlainGap {
		return
	}
	p.drawLocked()
}

// finishLocked 는 진행 중인 단계를 마무리한다 (마지막 상태를 그리고 줄바꿈).
func (p *progress) finishLocked() {
	if !p.active {
		return
	}
	if p.interactive {
		p.drawLocked()
		fmt.Fprint(p.w, "\n")
		p.drawn = false
	} else if p.total > 0 { // 카운터 없는 단계는 시작 때 찍은 줄과 같으므로 다시 찍지 않는다
		p.drawLocked()
	}
	p.active = false
}

// drawLocked 는 진행 줄을 그린다 (대화형은 제자리, 비대화형은 새 줄).
func (p *progress) drawLocked() {
	line := p.lineLocked()
	if p.sink != nil {
		p.sink(line)
		p.last = p.now()
		return
	}
	if p.interactive {
		fmt.Fprint(p.w, "\r"+line+"\x1b[K")
		p.drawn = true
	} else {
		fmt.Fprintln(p.w, line)
	}
	p.last = p.now()
}

// lineLocked 는 진행 줄 문자열입니다.
func (p *progress) lineLocked() string {
	head := fmt.Sprintf("%d/%d %s", p.idx, p.stages, p.name)
	if p.total == 0 {
		return "[" + head + "]"
	}
	el := int(p.now().Sub(p.begin) / time.Second)
	return fmt.Sprintf("[%s %d/%d · OK %d difference %d N/A %d 오류 %d · 경과 %02d:%02d]", head, p.done, p.total,
		p.counts[TagOK], p.counts[TagDiff], p.counts[TagNA], p.counts[progTagErr], el/60, el%60)
}
