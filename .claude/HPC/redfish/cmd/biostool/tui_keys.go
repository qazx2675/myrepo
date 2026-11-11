package main

// tui_keys.go — TUI 키 파서: 터미널에서 한 번에 읽은 바이트를 키 이벤트로 바꾼다 (auto_setup tui.go parseKeys 이식).
// 방향키(ESC [ A / ESC O A), PgUp/PgDn(ESC [5~ / [6~), Home/End, Shift+Tab(ESC [Z), Ctrl 키, UTF-8 문자.

import "unicode/utf8"

type tuiKeyKind int

const (
	tkUp tuiKeyKind = iota + 1
	tkDown
	tkLeft
	tkRight
	tkEnter
	tkEsc
	tkRune // 일반 문자·Ctrl 키·Tab·Backspace (r 에 값)
	tkEOF  // 입력 끝 (터미널이 닫힘)
	tkPgUp
	tkPgDn
	tkHome
	tkEnd
	tkBackTab // Shift+Tab

	tkPasteStart // ESC [ 200 ~ (붙여넣기 시작 표시 — readKeys 가 먹고 전달하지 않는다)
	tkPasteEnd   // ESC [ 201 ~ (붙여넣기 끝 표시)
)

type tuiKey struct {
	k tuiKeyKind
	r rune
	// paste 는 이 키가 붙여넣기의 일부이거나 뒤에 이미 읽어 둔 입력이 더 있었다는 표시입니다 (사람이 친 Enter 와 구분).
	paste bool
}

// Ctrl 키·특수 문자 (raw 모드에서 ISIG 를 껐으므로 Ctrl+C/Ctrl+Z 도 문자로 들어온다)
const (
	tuiCtrlC     = 0x03
	tuiCtrlD     = 0x04
	tuiCtrlX     = 0x18
	tuiCtrlZ     = 0x1a
	tuiTab       = '\t'
	tuiBackspace = 0x7f
)

// tuiParseKeys 는 읽은 바이트를 키 이벤트로 바꿉니다. 끝이 잘린 ESC 시퀀스와 모르는 시퀀스는 버립니다.
func tuiParseKeys(b []byte) []tuiKey {
	var out []tuiKey
	for i := 0; i < len(b); {
		c := b[i]
		switch {
		case c == 0x1b:
			if i+1 < len(b) && (b[i+1] == '[' || b[i+1] == 'O') {
				j := i + 2
				for j < len(b) && (b[j] < 0x40 || b[j] > 0x7e) {
					j++
				}
				if j >= len(b) { // 끝이 잘린 시퀀스는 버림
					return out
				}
				switch b[j] {
				case '~': // ESC [ n ~ : 5 PgUp, 6 PgDn, 1·7 Home, 4·8 End
					switch string(b[i+2 : j]) {
					case "5":
						out = append(out, tuiKey{k: tkPgUp})
					case "6":
						out = append(out, tuiKey{k: tkPgDn})
					case "1", "7":
						out = append(out, tuiKey{k: tkHome})
					case "4", "8":
						out = append(out, tuiKey{k: tkEnd})
					case "200":
						out = append(out, tuiKey{k: tkPasteStart})
					case "201":
						out = append(out, tuiKey{k: tkPasteEnd})
					}
				case 'A':
					out = append(out, tuiKey{k: tkUp})
				case 'B':
					out = append(out, tuiKey{k: tkDown})
				case 'C':
					out = append(out, tuiKey{k: tkRight})
				case 'D':
					out = append(out, tuiKey{k: tkLeft})
				case 'H':
					out = append(out, tuiKey{k: tkHome})
				case 'F':
					out = append(out, tuiKey{k: tkEnd})
				case 'Z':
					out = append(out, tuiKey{k: tkBackTab})
				}
				i = j + 1
				continue
			}
			out = append(out, tuiKey{k: tkEsc})
			i++
		case c == '\r' || c == '\n':
			out = append(out, tuiKey{k: tkEnter})
			i++
			if c == '\r' && i < len(b) && b[i] == '\n' {
				i++
			}
		case c == tuiCtrlC || c == tuiCtrlD || c == tuiCtrlX || c == tuiCtrlZ || c == tuiTab:
			out = append(out, tuiKey{k: tkRune, r: rune(c)})
			i++
		case c == 0x7f || c == 0x08:
			out = append(out, tuiKey{k: tkRune, r: tuiBackspace})
			i++
		case c >= 0x20 && c < 0x7f:
			out = append(out, tuiKey{k: tkRune, r: rune(c)})
			i++
		case c >= 0x80:
			r, n := utf8.DecodeRune(b[i:])
			out = append(out, tuiKey{k: tkRune, r: r})
			i += n
		default:
			i++
		}
	}
	return out
}

// tuiIncompleteTail 은 b 끝이 잘린 ESC [ / ESC O 시퀀스로 끝나면 그 시작 위치를, 아니면 -1 을 돌려줍니다
// (읽기 경계에서 ESC [ 200 ~ 가 둘로 갈라져도 이어 붙여 파싱하기 위함). 혼자 온 ESC 는 Esc 키라 해당 없음.
func tuiIncompleteTail(b []byte) int {
	for i := len(b) - 1; i >= 0 && i >= len(b)-16; i-- {
		if b[i] != 0x1b {
			continue
		}
		if i+1 >= len(b) || (b[i+1] != '[' && b[i+1] != 'O') {
			return -1
		}
		for _, c := range b[i+2:] {
			if c >= 0x40 && c <= 0x7e {
				return -1
			}
		}
		return i
	}
	return -1
}

// isRune 은 키가 문자 r 중 하나인지 봅니다.
func (k tuiKey) isRune(rs ...rune) bool {
	if k.k != tkRune {
		return false
	}
	for _, r := range rs {
		if k.r == r {
			return true
		}
	}
	return false
}
