// flow_tui.go - v0.8.0 전체 화면 흐름의 공통 부품 (신규 설치 양식이 쓰고, gossh 작업 화면 등이 재사용할 수 있게 분리):
//   - flowFrame / renderFlow / flowScroll : 제목·고정 머리·스크롤 본문·고정 입력 줄·안내 줄로 된 한 화면 (높이에 맞춤, ↑↓ PgUp/PgDn)
//   - pasteBuf                            : 여러 줄 붙여넣기 입력 (한 줄씩 확정, 빈 줄·"."·Ctrl+D 로 끝)
//   - bgTask / startBG / fastTickWhile    : 오래 걸리는 계산을 백그라운드로 돌리고 1초 tick 으로 결과를 꺼내는 비동기 패턴
package main

import (
	"fmt"
	"strings"
	"time"
)

// ---------------------------------------------------------------- 화면 틀

// flowFrame: 전체 화면 한 장. Head·Body·Foot 은 이미 폭에 맞게 줄바꿈된 화면 줄이다.
type flowFrame struct {
	Title  string   // 맨 위 제목 (굵게, 넘치면 줄바꿈) — 다음 줄은 구분선
	Head   []string // 제목 아래 고정 줄
	Body   []string // 스크롤 영역 (넘치면 flowScroll 에 따라 일부만)
	Foot   []string // 본문 아래 고정 줄 (입력 줄 등 — 항상 보임)
	Msg    string   // 구분선 아래 안내 한 줄 (노랑, MsgErr 면 빨강)
	MsgErr bool
	Hint   string // 키 안내 (hintLines — "\n" 은 묶음 경계)
}

// flowScroll: 본문 스크롤 — Follow 면 끝(최근)을 따라가고, 아니면 Top 부터. Max·Page 는 마지막 렌더에서 계산된다.
type flowScroll struct {
	Top, Max, Page int
	Follow         bool
}

// key: ↑↓ PgUp PgDn 로 스크롤 (끝까지 내리면 다시 끝 따라가기). 처리했으면 true.
func (s *flowScroll) key(ev keyEv) bool {
	page := s.Page
	if page < 1 {
		page = 10
	}
	cur := s.Top
	if s.Follow {
		cur = s.Max
	}
	switch ev.k {
	case kUp:
		cur--
	case kDown:
		cur++
	case kPgUp:
		cur -= page
	case kPgDn:
		cur += page
	default:
		return false
	}
	s.Top = rMax(rMin(cur, s.Max), 0)
	s.Follow = s.Top >= s.Max
	return true
}

// show: 본문 i 번째 줄(0부터)이 보이게 스크롤
func (s *flowScroll) show(i int) {
	s.Follow = false
	s.Top = rMax(i-1, 0)
}

// renderFlow: 틀을 그린다. height>0 이면 본문을 남는 높이에 맞추고, 위·아래에 가려진 줄이 있으면
// 첫·끝 줄을 `(위 N줄 — ↑ PgUp)` / `(아래 N줄 — ↓ PgDn)` 표시로 바꾼다. 줄 수는 항상 height (height>0).
func renderFlow(f flowFrame, sc *flowScroll, width, height int, color bool) string {
	width = clampWidth(width)
	head := append(wrapSegs(color, width, 1, "   ", seg{" " + f.Title, stBold}), sepLine(width))
	head = append(head, f.Head...)
	foot := append(append([]string{}, f.Foot...), sepLine(width))
	if f.Msg != "" {
		st := stYellow
		if f.MsgErr {
			st = stRed
		}
		foot = append(foot, wrapSegs(color, width, 1, "   ", seg{" " + f.Msg, st})...)
	}
	foot = append(foot, hintLines(color, width, f.Hint)...)
	body := f.Body
	if height > 0 {
		room := height - len(head) - len(foot)
		if room < 1 {
			room = 1
		}
		if sc == nil {
			sc = &flowScroll{}
		}
		sc.Page = rMax(room-2, 1)
		sc.Max = rMax(len(body)-room, 0)
		if sc.Follow || sc.Top > sc.Max {
			sc.Top = sc.Max
		}
		if sc.Top < 0 {
			sc.Top = 0
		}
		if len(body) > room {
			win := append([]string{}, body[sc.Top:sc.Top+room]...)
			mark := func(t string) string { return segLine(color, width, seg{"   " + t, stGray}) }
			if sc.Top > 0 && room >= 2 {
				win[0] = mark(fmt.Sprintf("(위 %d줄 — ↑ PgUp)", sc.Top+1))
			}
			if below := len(body) - sc.Top - room; below > 0 && room >= 2 {
				win[room-1] = mark(fmt.Sprintf("(아래 %d줄 — ↓ PgDn)", below+1))
			}
			body = win
		}
		for len(body) < room {
			body = append(body, "")
		}
		out := append(append(head, body...), foot...)
		if len(out) > height { // 머리·꼬리만으로도 넘치는 아주 작은 창: 아래(입력·안내)를 남긴다
			out = out[len(out)-height:]
		}
		return strings.Join(out, "\n")
	}
	return strings.Join(append(append(head, body...), foot...), "\n")
}

// ---------------------------------------------------------------- 붙여넣기 입력

// pasteBuf: 여러 줄 붙여넣기 입력. 키가 한 글자씩 들어와도(대량 붙여넣기 포함) 줄 단위로 확정해 넘긴다.
type pasteBuf struct {
	cur       []rune // 아직 확정되지 않은 현재 줄
	ctrlDOnly bool   // true 면 입력 끝은 Ctrl+D 만 — 빈 줄·"." 의 Enter 는 무시 (엑셀 붙여넣기의 빈 행이 입력을 끝내지 않게)
}

// pasteEv: pasteBuf.key 의 결과
type pasteEv int

const (
	pasteNone   pasteEv = iota // 처리하지 않은 키 (호출자가 처리)
	pasteEdited                // 현재 줄 편집
	pasteLine                  // 한 줄 확정 (line 에 앞뒤 공백을 뺀 내용)
	pasteEnd                   // 입력 끝 (빈 줄·"." 의 Enter, 또는 Ctrl+D) — 확정 안 된 현재 줄이 있으면 line 에 담아 함께 넘김
	pasteBack                  // 빈 줄에서 Backspace — 호출자가 앞 줄을 꺼내 set 으로 다시 편집하게 할 수 있음
)

// key: 키 하나 처리
func (p *pasteBuf) key(ev keyEv) (pasteEv, string) {
	switch {
	case ev.k == kEnter:
		line := strings.TrimSpace(string(p.cur))
		p.cur = nil
		if line == "" || line == "." {
			if p.ctrlDOnly {
				return pasteEdited, ""
			}
			return pasteEnd, ""
		}
		return pasteLine, line
	case ev.k == kRune && ev.r == runeCtrlD:
		line := strings.TrimSpace(string(p.cur))
		p.cur = nil
		if line == "." {
			line = ""
		}
		return pasteEnd, line
	case ev.k == kRune && ev.r == runeBackspace:
		if len(p.cur) == 0 {
			return pasteBack, ""
		}
		p.cur = p.cur[:len(p.cur)-1]
		return pasteEdited, ""
	case ev.k == kRune && ev.r == '\t':
		p.cur = append(p.cur, ' ')
		return pasteEdited, ""
	case ev.k == kRune && ev.r >= 0x20 && ev.r != runeBackspace:
		p.cur = append(p.cur, ev.r)
		return pasteEdited, ""
	}
	return pasteNone, ""
}

// set: 현재 줄을 s 로 (앞 줄 다시 편집 등)
func (p *pasteBuf) set(s string) { p.cur = []rune(s) }

// text: 현재 줄
func (p *pasteBuf) text() string { return string(p.cur) }

// ---------------------------------------------------------------- 비동기

// bgTask: 백그라운드 계산 하나. 결과는 poll 로 꺼낸다 (채널 버퍼 1 — 아무도 꺼내지 않아도 goroutine 이 막히지 않음).
type bgTask[T any] struct {
	ch      chan T
	Started time.Time
}

// startBG: f 를 goroutine 에서 실행
func startBG[T any](now time.Time, f func() T) *bgTask[T] {
	t := &bgTask[T]{ch: make(chan T, 1), Started: now}
	go func() { t.ch <- f() }()
	return t
}

// poll: 끝났으면 (결과, true) — 한 번만 true
func (t *bgTask[T]) poll() (T, bool) {
	select {
	case v := <-t.ch:
		return v, true
	default:
		var zero T
		return zero, false
	}
}

// fastTickWhile: running 이면 1초 tick 을 켜 두고(경과 시간 갱신·결과 확인), 아니면 끈다 (runTUILoop 의 fastTick)
func (st *tuiState) fastTickWhile(running bool) {
	switch {
	case running && st.fastTick == nil:
		st.fastTick, st.fastStop = st.env.Tick(time.Second)
	case !running && st.fastTick != nil:
		st.stopFastTick()
	}
}
