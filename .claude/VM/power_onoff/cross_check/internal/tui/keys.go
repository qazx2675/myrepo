package tui

// keys.go - 전체 화면 TUI 의 키 읽기: 터미널에서 읽은 바이트를 키 이벤트로 바꾼다.
// auto_setup tui.go 의 parseKeys 를 옮겨 와, 읽기 경계에서 잘린 시퀀스(ESC [ … / UTF-8 조각)를 다음 읽기까지 들고 있도록
// 상태를 가진 decoder 로 만들었다. 붙여넣기 구간(bracketed paste: ESC[200~ … ESC[201~)의 키에는 paste 표시를 붙인다
// (목록 화면에서 붙여넣은 글이 단축키로 해석되지 않게, 붙여넣기 칸에서 붙여넣은 빈 줄이 입력을 끝내지 않게).

import "unicode/utf8"

type keyKind int

const (
	kUp keyKind = iota + 1
	kDown
	kLeft
	kRight
	kEnter
	kEsc
	kRune
	kEOF
	kPgUp
	kPgDn
	kHome
	kEnd
	kBackTab // Shift+Tab (ESC [ Z)
)

type keyEv struct {
	k     keyKind
	r     rune
	paste bool // 붙여넣기 구간(ESC[200~ … ESC[201~) 안에서 온 키
}

const (
	runeCtrlC     = 0x03
	runeCtrlD     = 0x04
	runeCtrlU     = 0x15
	runeCtrlX     = 0x18
	runeCtrlZ     = 0x1a
	runeBackspace = 0x7f
)

// maxPending 은 끝나지 않은 시퀀스를 들고 있는 최대 바이트 수(넘으면 버린다 — 깨진 입력이 쌓이지 않게).
const maxPending = 32

// keyDecoder 는 읽은 바이트 조각을 이어 받아 키 이벤트로 바꾼다.
type keyDecoder struct {
	pending []byte // 다음 읽기와 이어 붙일, 끝나지 않은 시퀀스
	paste   bool   // 지금 붙여넣기 구간 안인지
}

// feed 는 새로 읽은 바이트를 처리한다. 끝나지 않은 ESC 시퀀스·UTF-8 조각은 다음 feed 까지 들고 있는다.
// 단독 ESC 가 맨 끝이면 그것도 들고 있다(뒤에 '[' 가 올 수 있음) — flush 가 kEsc 로 내보낸다.
func (d *keyDecoder) feed(b []byte) []keyEv {
	if len(d.pending) > 0 {
		b = append(append([]byte{}, d.pending...), b...)
		d.pending = nil
	}
	var out []keyEv
	emit := func(ev keyEv) {
		ev.paste = d.paste
		out = append(out, ev)
	}
	for i := 0; i < len(b); {
		c := b[i]
		switch {
		case c == 0x1b:
			if i+1 >= len(b) { // ESC 가 끝: 시퀀스의 시작일 수 있어 들고 있는다
				d.keep(b[i:])
				return out
			}
			if b[i+1] == '[' || b[i+1] == 'O' {
				j := i + 2
				for j < len(b) && (b[j] < 0x40 || b[j] > 0x7e) {
					j++
				}
				if j >= len(b) { // 끝이 잘린 시퀀스: 다음 읽기와 이어 붙인다
					d.keep(b[i:])
					return out
				}
				params := string(b[i+2 : j])
				switch b[j] {
				case '~':
					switch params {
					case "5":
						emit(keyEv{k: kPgUp})
					case "6":
						emit(keyEv{k: kPgDn})
					case "1", "7":
						emit(keyEv{k: kHome})
					case "4", "8":
						emit(keyEv{k: kEnd})
					case "200":
						d.paste = true
					case "201":
						d.paste = false
					}
				case 'A':
					emit(keyEv{k: kUp})
				case 'B':
					emit(keyEv{k: kDown})
				case 'C':
					emit(keyEv{k: kRight})
				case 'D':
					emit(keyEv{k: kLeft})
				case 'Z':
					emit(keyEv{k: kBackTab})
				case 'H':
					emit(keyEv{k: kHome})
				case 'F':
					emit(keyEv{k: kEnd})
				}
				i = j + 1
				continue
			}
			emit(keyEv{k: kEsc})
			i++
		case c == '\r' || c == '\n':
			emit(keyEv{k: kEnter})
			i++
			if c == '\r' && i < len(b) && b[i] == '\n' {
				i++
			}
		case c == runeCtrlC || c == runeCtrlZ || c == runeCtrlD || c == runeCtrlX || c == runeCtrlU || c == '\t':
			emit(keyEv{k: kRune, r: rune(c)})
			i++
		case c == 0x7f || c == 0x08:
			emit(keyEv{k: kRune, r: runeBackspace})
			i++
		case c >= 0x20 && c < 0x7f:
			emit(keyEv{k: kRune, r: rune(c)})
			i++
		case c >= 0x80:
			if !utf8.FullRune(b[i:]) { // UTF-8 조각이 잘렸다: 다음 읽기와 이어 붙인다
				d.keep(b[i:])
				return out
			}
			r, n := utf8.DecodeRune(b[i:])
			if r != utf8.RuneError {
				emit(keyEv{k: kRune, r: r})
			}
			i += n
		default:
			i++
		}
	}
	return out
}

func (d *keyDecoder) keep(b []byte) {
	if len(b) > maxPending {
		return
	}
	d.pending = append([]byte{}, b...)
}

// flush 는 더 올 입력이 없을 때(잠시 조용하거나 EOF) 들고 있던 조각을 정리한다: 단독 ESC 는 kEsc, 나머지는 버린다.
func (d *keyDecoder) flush() []keyEv {
	p := d.pending
	d.pending = nil
	if len(p) == 1 && p[0] == 0x1b {
		return []keyEv{{k: kEsc, paste: d.paste}}
	}
	return nil
}

// hasLoneEsc 는 단독 ESC 하나만 들고 있는지(이것만 시간이 지나면 kEsc 로 내보낸다).
func (d *keyDecoder) hasLoneEsc() bool { return len(d.pending) == 1 && d.pending[0] == 0x1b }

// parseKeys 는 한 번에 받은 바이트를 키로 바꾼다(테스트·간단한 경우용: 끝에 남은 단독 ESC 는 kEsc).
func parseKeys(b []byte) []keyEv {
	var d keyDecoder
	out := d.feed(b)
	return append(out, d.flush()...)
}
