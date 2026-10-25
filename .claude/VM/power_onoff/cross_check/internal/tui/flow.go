package tui

// flow.go - 전체 화면 흐름(상세·승인 단계·입력 화면)의 공통 부품. auto_setup flow_tui.go 를 옮겨 와 고친 것:
//   - flowFrame / renderFlow / flowScroll : 제목·고정 머리·스크롤 본문·고정 입력 줄·안내 줄로 된 한 화면 (높이에 맞춤, ↑↓ PgUp/PgDn)
//   - field                              : 한 줄 입력(글자 또는 마스킹 패스워드). 화면에는 "_" 가짜 커서(실제 커서는 숨김)
//   - pasteBox                           : 여러 줄 붙여넣기 입력(줄 단위 확정, 빈 줄 Enter·Ctrl+D 로 끝, 붙여넣기 구간의 빈 줄은 무시)

import (
	"fmt"
	"strings"

	"cross_check/internal/verify"
)

// ---------------------------------------------------------------- 화면 틀

// flowFrame: 전체 화면 한 장. Head·Body·Foot 은 이미 폭에 맞게 줄바꿈된 화면 줄이다.
type flowFrame struct {
	Title  string   // 맨 위 제목 (굵게, 넘치면 줄바꿈) — 다음 줄은 구분선
	Head   []string // 제목 아래 고정 줄
	Body   []string // 스크롤 영역 (넘치면 flowScroll 에 따라 일부만)
	Foot   []string // 본문 아래 고정 줄 (입력 줄 등 — 항상 보임)
	Msg    string   // 구분선 아래 안내 한 줄 (노랑, MsgErr 면 빨강, MsgOK 면 초록)
	MsgErr bool
	MsgOK  bool
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
	case kHome:
		cur = 0
	case kEnd:
		cur = s.Max
	default:
		return false
	}
	s.Top = rMax(rMin(cur, s.Max), 0)
	s.Follow = s.Top >= s.Max
	return true
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
		switch {
		case f.MsgErr:
			st = stRed
		case f.MsgOK:
			st = stGreen
		}
		foot = append(foot, wrapSegs(color, width, 1, "   ", seg{" " + f.Msg, st})...)
	}
	foot = append(foot, hintLines(color, width, f.Hint)...)
	body := f.Body
	if height <= 0 {
		return strings.Join(append(append(head, body...), foot...), "\n")
	}
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

// kv 는 상세 화면의 "이름  값" 한 줄(이름 칸 폭 14, 값은 단어 단위 줄바꿈)
func kv(color bool, width int, name string, val seg) []string {
	return wrapSegs(color, width, 1, strings.Repeat(" ", 17), seg{"  " + padR(name, 15), stBold}, val)
}

// ---------------------------------------------------------------- 한 줄 입력

// maxField 는 한 줄 입력의 최대 글자 수(패스워드 규칙 128자보다 넉넉히).
const maxField = 256

// field 는 한 줄 입력. secret 이면 화면에는 '*' 만 보인다(글자 수만큼). 값은 화면 렌더 함수에 넘기지 않는다.
type field struct {
	rs     []rune
	secret bool
}

// key 는 편집 키를 처리한다: 인쇄 가능 글자 추가, Backspace, Ctrl+U(전부 지우기). 처리했으면 true.
// Enter·Esc 는 호출자가 처리한다.
func (f *field) key(ev keyEv) bool {
	if ev.k != kRune {
		return false
	}
	switch {
	case ev.r == runeBackspace:
		if len(f.rs) > 0 {
			f.rs[len(f.rs)-1] = 0
			f.rs = f.rs[:len(f.rs)-1]
		}
		return true
	case ev.r == runeCtrlU:
		f.clear()
		return true
	case ev.r == '\t':
		return true // 탭은 무시(한 줄 입력)
	case ev.r >= 0x20 && ev.r != runeBackspace:
		if len(f.rs) < maxField {
			f.rs = append(f.rs, ev.r)
		}
		return true
	}
	return false
}

// text 는 입력값.
func (f *field) text() string { return string(f.rs) }

// clear 는 입력을 0 으로 덮어쓴 뒤 비운다(패스워드가 메모리에 남는 시간을 줄임).
func (f *field) clear() {
	for i := range f.rs {
		f.rs[i] = 0
	}
	f.rs = f.rs[:0]
}

// line 은 " <라벨> > 값_" 한 줄(secret 이면 '*'). 폭을 넘으면 앞부분을 잘라 끝(커서 쪽)을 보인다.
func (f *field) line(color bool, width int, label string) string {
	val := string(f.rs)
	if f.secret {
		val = strings.Repeat("*", len(f.rs))
	}
	val = cleanText(val)
	prefix := " " + label + " > "
	room := width - strWidth(prefix) - 1
	for room > 0 && strWidth(val) > room {
		_, n := firstRune(val)
		val = val[n:]
	}
	return segLine(color, width, seg{prefix, stBold}, seg{val, ""}, seg{"_", stBold})
}

func firstRune(s string) (rune, int) {
	for i, r := range s {
		if i > 0 {
			return r, i
		}
	}
	return 0, len(s)
}

// ---------------------------------------------------------------- 붙여넣기 입력

// maxPaste 는 붙여넣기 입력의 최대 바이트 수(데몬 요청 한 줄 한도 1 MiB 안쪽).
const maxPaste = 512 << 10

// pasteBox 는 대상 목록 붙여넣기 입력. 키가 한 글자씩 들어와도(대량 붙여넣기 포함) 줄 단위로 확정한다.
// 끝: 붙여넣기 구간 밖에서 빈 줄 Enter, 또는 Ctrl+D. 붙여넣기 구간(bracketed paste) 안의 빈 줄은 끝으로 보지 않는다.
type pasteBox struct {
	lines []string // 확정된 줄
	cur   []rune   // 아직 확정되지 않은 현재 줄
	size  int      // 지금까지 받은 바이트 수
	full  bool     // maxPaste 에 닿아 더 받지 않음
}

// pasteResult 는 pasteBox.key 의 결과.
type pasteResult int

const (
	pasteNone   pasteResult = iota // 처리하지 않은 키
	pasteEdited                    // 편집함
	pasteDone                      // 입력 끝(빈 줄 Enter 또는 Ctrl+D)
)

func (p *pasteBox) key(ev keyEv) pasteResult {
	switch {
	case ev.k == kEnter:
		line := string(p.cur)
		p.cur = nil
		if strings.TrimSpace(line) == "" {
			if ev.paste { // 붙여넣은 글 속의 빈 줄: 구분자일 뿐 입력 끝이 아니다
				return pasteEdited
			}
			return pasteDone
		}
		p.lines = append(p.lines, line)
		return pasteEdited
	case ev.k == kRune && ev.r == runeCtrlD:
		if len(p.cur) > 0 {
			p.lines = append(p.lines, string(p.cur))
			p.cur = nil
		}
		return pasteDone
	case ev.k == kRune && ev.r == runeBackspace:
		if len(p.cur) == 0 {
			if n := len(p.lines); n > 0 { // 빈 줄에서 Backspace: 앞 줄을 다시 편집
				p.cur = []rune(p.lines[n-1])
				p.lines = p.lines[:n-1]
			}
			return pasteEdited
		}
		p.cur = p.cur[:len(p.cur)-1]
		return pasteEdited
	case ev.k == kRune && ev.r == runeCtrlU:
		p.cur = nil
		return pasteEdited
	case ev.k == kRune && (ev.r == '\t' || ev.r >= 0x20):
		if p.size >= maxPaste {
			p.full = true
			return pasteEdited
		}
		p.cur = append(p.cur, ev.r)
		p.size += len(string(ev.r))
		return pasteEdited
	}
	return pasteNone
}

// text 는 붙여넣은 글 전체(줄바꿈으로 이음). 데몬이 같은 규칙(verify.Parse)으로 나눈다.
func (p *pasteBox) text() string {
	all := append(append([]string{}, p.lines...), string(p.cur))
	return strings.Join(all, "\n")
}

// count 는 지금까지 인식한 이름 수(줄바꿈·탭·공백 구분). 판정(일치 여부)은 화면에 보이지 않는다.
func (p *pasteBox) count() int { return len(verify.Parse(p.text())) }

// reset 은 입력을 지운다.
func (p *pasteBox) reset() { *p = pasteBox{} }
