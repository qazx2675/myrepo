package main

// 평문 텍스트 프로토콜(version 2).
//
// 파일 바이트를 조각으로 나누고, 조각마다 YAML 리터럴 블록(chunk_text: |2- 또는 |2+)에 그대로 넣는다.
//
// 실측(AWX 192.168.0.58, 인벤토리 5):
//   - variables 는 보낸 문자열 그대로 저장된다. JSON 변환이나 들여쓰기 변경은 없다.
//   - 단, 문자열 끝의 공백/줄바꿈은 제거된다.
//   - YAML 로 해석할 수 없으면 400 이다. 단독 CR, 제어문자(0x01 등), NEL(U+0085),
//     U+2028/U+2029 는 리터럴 블록을 깨거나 거부된다.
//
// 그래서:
//   - chunk_text 뒤에는 항상 다른 키 줄이 온다. 조각 끝 공백/줄바꿈은 문서 끝이 아니므로 잘리지 않는다.
//   - 들여쓰기 지시자 2 를 항상 쓴다. 첫 줄이 공백으로 시작해도 된다.
//   - 조각 끝이 줄바꿈이면 keep(+), 아니면 strip(-) 을 쓴다. YAML 의미도 원문과 같다.
//   - eol: lf(CR 없음) | crlf(모든 줄바꿈이 CRLF) | cr(모든 줄바꿈이 CR) | mixed.
//     crlf/cr 은 블록 안에서 LF 로 바꾸고 받을 때 되돌린다. mixed 는 LF 는 그대로, CR 은 chunk_raw 로 보낸다.
//   - YAML 에 넣을 수 없는 바이트(제어문자, 잘못된 UTF-8, NEL, U+2028/2029, BOM 등)는 블록에서 빼고
//     chunk_raw 에 "오프셋:16진수" 로 적는다. 오프셋은 chunk_text(UTF-8 바이트) 기준 삽입 위치다.
//   - chunk_sha256 은 조각의 원본 파일 바이트 해시다. sha256 은 전체 파일 해시다.

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"
)

const (
	encText = "text"

	eolLF    = "lf"
	eolCRLF  = "crlf"
	eolCR    = "cr"
	eolMixed = "mixed"

	// maxChunkBytes 는 조각 하나의 PATCH 본문(JSON) 추정 크기 상한이다. AWX 한도 4.94 MiB 안쪽.
	maxChunkBytes = 3 << 20
	// litIndent 는 chunk_text 블록 내용의 들여쓰기(부모 키 2칸 + 지시자 2)다.
	litIndent = "    "
)

// detectEOL 은 파일의 줄바꿈 방식을 정한다.
func detectEOL(data []byte) string {
	var crlf, cr, lf int
	for i := 0; i < len(data); i++ {
		switch data[i] {
		case '\r':
			if i+1 < len(data) && data[i+1] == '\n' {
				crlf++
				i++
			} else {
				cr++
			}
		case '\n':
			lf++
		}
	}
	switch {
	case crlf == 0 && cr == 0:
		return eolLF
	case lf == 0 && cr == 0:
		return eolCRLF
	case lf == 0 && crlf == 0:
		return eolCR
	}
	return eolMixed
}

func eolSeq(eol string) string {
	switch eol {
	case eolCRLF:
		return "\r\n"
	case eolCR:
		return "\r"
	}
	return "\n"
}

// hasFinalNewline 은 파일이 줄바꿈(LF 또는 CR)으로 끝나는지 본다.
func hasFinalNewline(data []byte) bool {
	n := len(data)
	return n > 0 && (data[n-1] == '\n' || data[n-1] == '\r')
}

// textRuneOK 는 리터럴 블록에 그대로 넣어도 되는 문자인지 본다 (PyYAML printable 에서 줄바꿈 계열과 BOM 제외).
func textRuneOK(r rune) bool {
	switch {
	case r == '\t' || r == '\n':
		return true
	case r >= 0x20 && r <= 0x7e:
		return true
	case r >= 0xa0 && r <= 0xd7ff:
		return r != 0x2028 && r != 0x2029
	case r >= 0xe000 && r <= 0xfffd:
		return r != 0xfeff
	case r >= 0x10000 && r <= 0x10ffff:
		return true
	}
	return false
}

// isCleanText 는 문자열 전체가 블록에 넣을 수 있는 문자로만 되어 있는지 본다.
func isCleanText(s string) bool {
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		if (r == utf8.RuneError && size <= 1) || !textRuneOK(r) {
			return false
		}
		i += size
	}
	return true
}

// unit 은 data[i:] 에서 쪼갤 수 없는 단위의 길이와 비용(문자 수, 본문 바이트 추정)을 돌려준다.
func unit(data []byte, i int, eol string) (size, chars, cost int) {
	b := data[i]
	if b == '\r' {
		if eol == eolCRLF && i+1 < len(data) && data[i+1] == '\n' {
			return 2, 1, 2 + len(litIndent) // JSON "\n" + 들여쓰기
		}
		if eol == eolCR {
			return 1, 1, 2 + len(litIndent)
		}
	}
	r, n := utf8.DecodeRune(data[i:])
	if (r == utf8.RuneError && n <= 1) || !textRuneOK(r) {
		return n, 2 * n, 2*n + 12 // 16진수 + 오프셋 몫
	}
	switch r {
	case '\n':
		return 1, 1, 2 + len(litIndent)
	case '\t', '"', '\\':
		return 1, 1, 2
	case '<', '>', '&':
		return 1, 1, 1
	}
	return n, 1, n
}

// splitText 는 파일 바이트를 조각으로 나눈다. 문자 수 maxChars, 본문 추정 maxBytes 를 넘지 않는다.
// 멀티바이트 문자와 CRLF(eol=crlf) 중간에서는 자르지 않는다. 빈 파일도 조각 1개다.
func splitText(data []byte, eol string, maxChars, maxBytes int) [][]byte {
	if maxChars <= 0 {
		maxChars = 1 << 30
	}
	if maxBytes <= 0 {
		maxBytes = 1 << 30
	}
	var out [][]byte
	start, chars, cost := 0, 0, 0
	for i := 0; i < len(data); {
		size, c, b := unit(data, i, eol)
		if i > start && (chars+c > maxChars || cost+b > maxBytes) {
			out = append(out, data[start:i])
			start, chars, cost = i, 0, 0
		}
		chars += c
		cost += b
		i += size
	}
	out = append(out, data[start:])
	return out
}

// encodeChunk 는 조각 바이트를 chunk_text 와 chunk_raw 로 바꾼다.
func encodeChunk(raw []byte, eol string) (text, esc string) {
	var tb strings.Builder
	tb.Grow(len(raw))
	var entries []string
	var pend []byte
	off := 0
	flush := func() {
		if len(pend) > 0 {
			entries = append(entries, strconv.Itoa(off)+":"+hex.EncodeToString(pend))
			pend = pend[:0]
		}
	}
	for i := 0; i < len(raw); {
		if raw[i] == '\r' {
			if eol == eolCRLF && i+1 < len(raw) && raw[i+1] == '\n' {
				flush()
				tb.WriteByte('\n')
				i += 2
				continue
			}
			if eol == eolCR {
				flush()
				tb.WriteByte('\n')
				i++
				continue
			}
		}
		r, n := utf8.DecodeRune(raw[i:])
		if (r == utf8.RuneError && n <= 1) || !textRuneOK(r) {
			if len(pend) == 0 {
				off = tb.Len()
			}
			pend = append(pend, raw[i:i+n]...)
			i += n
			continue
		}
		flush()
		tb.Write(raw[i : i+n])
		i += n
	}
	flush()
	return tb.String(), strings.Join(entries, " ")
}

// decodeChunk 는 encodeChunk 의 역이다.
func decodeChunk(text, esc, eol string) ([]byte, error) {
	conv := func(dst *bytes.Buffer, s string) {
		switch eol {
		case eolCRLF:
			dst.WriteString(strings.ReplaceAll(s, "\n", "\r\n"))
		case eolCR:
			dst.WriteString(strings.ReplaceAll(s, "\n", "\r"))
		default:
			dst.WriteString(s)
		}
	}
	var out bytes.Buffer
	out.Grow(len(text) + len(text)/16)
	pos := 0
	for _, e := range strings.Fields(esc) {
		i := strings.IndexByte(e, ':')
		if i < 0 {
			return nil, fmt.Errorf("chunk_raw 항목 형식 오류: %q", e)
		}
		off, err := strconv.Atoi(e[:i])
		if err != nil || off < pos || off > len(text) {
			return nil, fmt.Errorf("chunk_raw 오프셋 오류: %q", e)
		}
		raw, err := hex.DecodeString(e[i+1:])
		if err != nil || len(raw) == 0 {
			return nil, fmt.Errorf("chunk_raw 16진수 오류: %q", e)
		}
		conv(&out, text[pos:off])
		out.Write(raw)
		pos = off
	}
	conv(&out, text[pos:])
	return out.Bytes(), nil
}

// assembleText 는 받은 조각을 합친다. final_newline 이 true 인데 끝 줄바꿈이 없으면 eol 로 채운다.
func assembleText(meta Block, parts [][]byte) []byte {
	data := bytes.Join(parts, nil)
	if data == nil {
		data = []byte{}
	}
	if meta["final_newline"] == "true" && !hasFinalNewline(data) {
		data = append(data, eolSeq(meta["eol"])...)
	}
	return data
}

// renderLiteral 은 chunk_text 값을 쓴다. 키 "  chunk_text: " 뒤에 붙는다.
func renderLiteral(sb *strings.Builder, s string) {
	if s == "" || !isCleanText(s) {
		sb.WriteString(yamlQuote(s))
		sb.WriteByte('\n')
		return
	}
	body := s
	if strings.HasSuffix(s, "\n") {
		sb.WriteString("|2+\n")
		body = s[:len(s)-1]
	} else {
		sb.WriteString("|2-\n")
	}
	for _, ln := range strings.Split(body, "\n") {
		if ln != "" {
			sb.WriteString(litIndent)
			sb.WriteString(ln)
		}
		sb.WriteByte('\n')
	}
}

// parseLiteral 은 lines[i:] 에서 리터럴 블록 내용을 읽는다. parent 는 키 줄의 들여쓰기, header 는 "|2-" 등이다.
// 블록 다음 줄 번호를 돌려준다.
func parseLiteral(lines []string, i, parent int, header string) (string, int, error) {
	h := header[1:]
	if j := strings.Index(h, " #"); j >= 0 {
		h = h[:j]
	}
	h = strings.TrimSpace(h)
	chomp, ind := byte(0), 0
	for k := 0; k < len(h); k++ {
		switch c := h[k]; {
		case c == '+' || c == '-':
			chomp = c
		case c >= '1' && c <= '9':
			ind = int(c - '0')
		default:
			return "", i, fmt.Errorf("블록 헤더 형식 오류: %q", header)
		}
	}
	indent := -1
	if ind > 0 {
		indent = parent + ind
	}
	var content []string
	for ; i < len(lines); i++ {
		ln := strings.TrimRight(lines[i], "\r")
		sp := len(ln) - len(strings.TrimLeft(ln, " "))
		if indent < 0 && sp < len(ln) { // 지시자가 없으면 첫 내용 줄로 정한다
			if sp <= parent {
				break
			}
			indent = sp
		}
		if indent >= 0 && sp >= indent {
			content = append(content, ln[indent:])
			continue
		}
		if sp == len(ln) { // 공백뿐인 줄(들여쓰기보다 짧음) = 빈 줄
			content = append(content, "")
			continue
		}
		break
	}
	v := strings.Join(content, "\n")
	switch chomp {
	case '+':
		if len(content) > 0 {
			v += "\n"
		}
	case '-':
		v = strings.TrimRight(v, "\n")
	default:
		v = strings.TrimRight(v, "\n")
		if v != "" {
			v += "\n"
		}
	}
	return v, i, nil
}
