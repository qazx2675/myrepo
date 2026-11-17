package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"
)

// awxStore 는 실제 AWX 저장 동작(끝 공백/줄바꿈 제거)을 흉내 낸다.
func awxStore(v string) string { return strings.TrimRight(v, " \t\r\n") }

type protoCase struct {
	name string
	data []byte
	cs   int
}

func protoCases() []protoCase {
	const cs = 64
	danger := "key: value # c\n- item\n'single' \"double\" {a: 1} [x, y]\n| > & * ! % @ `\n---\n...\n? q\n"
	big := repeatTo("  한글\t탭: 값 # - '\" {}[]|>&*!%\r\n끝 공백   \r\n\r\n", 10<<20)
	return []protoCase{
		{"empty", nil, cs},
		{"one_byte", []byte("x"), cs},
		{"one_newline", []byte("\n"), cs},
		{"edge_minus1", bytes.Repeat([]byte("a"), cs-1), cs},
		{"edge_exact", bytes.Repeat([]byte("a"), cs), cs},
		{"edge_plus1", bytes.Repeat([]byte("a"), cs+1), cs},
		{"edge_newline_at_cut", []byte(strings.Repeat("a", cs-1) + "\n" + "b"), cs},
		{"tabs", []byte("\t\ta\tb\t\n\t\n\t"), cs},
		{"crlf", []byte("a\r\nb\r\n\r\nc\r\n"), cs},
		{"crlf_no_final", []byte("a\r\nb"), cs},
		{"cr_only", []byte("a\rb\r\rc\r"), cs},
		{"mixed_eol", []byte("a\r\nb\nc\rd\r\n\n\r"), cs},
		{"no_final_newline", []byte("line1\nline2"), cs},
		{"trailing_ws_many", []byte("text\n\n\n   \n\t\n  \t  \n\n\n"), cs},
		{"trailing_spaces_eof", []byte("text   "), cs},
		{"leading_space_first_line", []byte("   first\n  second\n"), cs},
		{"leading_tab_first_line", []byte("\tfirst\n"), cs},
		{"only_spaces", []byte("     "), cs},
		{"empty_lines", []byte("\n\n\na\n\n\nb\n\n"), cs},
		{"korean", []byte("가나다라마바사 아자차카타파하\n한글 줄바꿈\n"), 5},
		{"danger_chars", []byte(danger), cs},
		{"yaml_like_indent", []byte("awx_copy:\n  session: x\n    chunk_text: |\n  ack: \"9\"\n"), cs},
		{"cut_at_trailing_ws", []byte("aaa   \n   bbb   \n\n   "), 4},
		{"control_chars", []byte("a\x00b\x01c\x1b[31mred\x1b[0m\x7f\x0c"), cs},
		{"unicode_breaks", []byte("a" + string(rune(0x85)) + "b" + string(rune(0x2028)) + "c" + string(rune(0x2029)) + "d" + string(rune(0xfeff)) + "e" + string(rune(0xfffd)) + "f"), cs},
		{"invalid_utf8", []byte{'a', 0xff, 0xfe, 'b', 0xe3, 0x81, '\n', 0xc0, 0x80, 0xed, 0xa0, 0x80}, cs},
		{"binary", randBytes(3000), 100},
		{"big_10mib", big, 1 << 20},
	}
}

// repeatTo 는 p 를 반복해 정확히 n 바이트를 만든다.
func repeatTo(p string, n int) []byte {
	return bytes.Repeat([]byte(p), n/len(p)+1)[:n]
}

// protoRoundTrip 은 직렬화 → (AWX 저장 흉내) → 파싱 → 복원을 한다.
func protoRoundTrip(t *testing.T, data []byte, cs int, trim bool, dump func(k int, vars, text string)) []byte {
	t.Helper()
	eol := detectEOL(data)
	chunks := splitText(data, eol, cs, maxChunkBytes)
	meta := Block{
		"version": protoVersion, "session": "s1", "state": stateSending, "file_name": "f.txt", "type": "txt",
		"encoding": encText, "eol": eol, "final_newline": strconv.FormatBool(hasFinalNewline(data)),
		"total_bytes": strconv.Itoa(len(data)), "sha256": sha256Hex(data),
		"chunk_total": strconv.Itoa(len(chunks)), "backup_b64": "", "ack": "0", "updated_at": nowStamp(),
	}
	var parts [][]byte
	for k, c := range chunks {
		text, esc := encodeChunk(c, eol)
		if !isCleanText(text) || strings.ContainsRune(text, '\r') {
			t.Fatalf("조각 %d: 블록에 넣을 수 없는 문자가 남았습니다", k+1)
		}
		b := meta.clone()
		b["chunk_index"] = strconv.Itoa(k + 1)
		b["chunk_text"], b["chunk_raw"], b["chunk_sha256"] = text, esc, sha256Hex(c)
		vars := b.Render()
		if !isCleanText(vars) {
			t.Fatalf("조각 %d: Render 결과에 YAML 금지 문자", k+1)
		}
		if trim {
			vars = awxStore(vars)
		}
		if dump != nil {
			dump(k+1, vars, text)
		}
		got, ok := parseBlock(vars)
		if !ok {
			t.Fatalf("조각 %d: 파싱 실패", k+1)
		}
		for _, key := range blockKeys {
			if key != "chunk_data" && got[key] != b[key] {
				t.Fatalf("조각 %d: 키 %s 불일치: %q != %q", k+1, key, got[key], b[key])
			}
		}
		raw, err := decodeChunk(got["chunk_text"], got["chunk_raw"], got["eol"])
		if err != nil {
			t.Fatalf("조각 %d: %v", k+1, err)
		}
		if sha256Hex(raw) != got["chunk_sha256"] {
			t.Fatalf("조각 %d: 조각 SHA-256 불일치 %q != %q", k+1, raw, c)
		}
		parts = append(parts, raw)
	}
	return assembleText(meta, parts)
}

func TestTextProtoRoundTrip(t *testing.T) {
	for _, trim := range []bool{false, true} {
		for _, tc := range protoCases() {
			name := tc.name
			if trim {
				name = "awxtrim/" + name
			}
			t.Run(name, func(t *testing.T) {
				got := protoRoundTrip(t, tc.data, tc.cs, trim, nil)
				if !bytes.Equal(got, tc.data) || sha256Hex(got) != sha256Hex(tc.data) {
					t.Fatalf("복원 불일치: %d bytes vs %d bytes", len(got), len(tc.data))
				}
			})
		}
	}
}

func TestSplitTextBoundaries(t *testing.T) {
	// 멀티바이트 문자 중간에서 자르지 않는다. 문자 수 상한을 지킨다.
	data := []byte(strings.Repeat("가a나😀\t", 1000))
	for _, cs := range []int{1, 2, 3, 7, 100} {
		for i, c := range splitText(data, eolLF, cs, maxChunkBytes) {
			if !utf8.Valid(c) {
				t.Fatalf("cs=%d 조각 %d: UTF-8 중간에서 잘렸습니다", cs, i)
			}
			if n := utf8.RuneCount(c); n > cs {
				t.Fatalf("cs=%d 조각 %d: %d문자", cs, i, n)
			}
		}
	}
	// CRLF 를 가르지 않는다.
	for _, c := range splitText([]byte(strings.Repeat("a\r\n", 50)), eolCRLF, 1, maxChunkBytes) {
		if bytes.HasSuffix(c, []byte("\r")) {
			t.Fatal("CRLF 가 갈렸습니다")
		}
	}
	// 본문 바이트 상한도 지킨다.
	for _, c := range splitText([]byte(strings.Repeat("한", 1000)), eolLF, 1<<20, 300) {
		if len(c) > 300 {
			t.Fatalf("바이트 상한 초과 %d", len(c))
		}
	}
}

func TestDetectEOL(t *testing.T) {
	for in, want := range map[string]string{"": eolLF, "a\nb": eolLF, "a\r\nb\r\n": eolCRLF, "a\rb": eolCR, "a\r\nb\n": eolMixed, "a\rb\n": eolMixed} {
		if got := detectEOL([]byte(in)); got != want {
			t.Errorf("%q: %s, 기대 %s", in, got, want)
		}
	}
}

func TestRenderLayout(t *testing.T) {
	b := Block{"session": "s", "chunk_text": "  lead\n", "ack": "1"}
	r := b.Render()
	if !strings.Contains(r, "  chunk_text: |2+\n      lead\n  ack: \"1\"\n") {
		t.Fatalf("블록 형식:\n%s", r)
	}
	b["chunk_text"] = "x  "
	if !strings.Contains(b.Render(), "  chunk_text: |2-\n    x  \n  ack:") {
		t.Fatalf("블록 형식:\n%s", b.Render())
	}
	// 블록에 넣을 수 없는 값은 큰따옴표로 대신 쓴다.
	b["chunk_text"] = "a\rb\x01"
	got, ok := parseBlock(b.Render())
	if !ok || got["chunk_text"] != "a\rb\x01" {
		t.Fatalf("따옴표 대체 실패: %q", got["chunk_text"])
	}
}

// TestParseVariants 는 지시자 없는 블록, CRLF 문서, JSON 형식도 읽는지 본다.
func TestParseVariants(t *testing.T) {
	y := "awx_copy:\r\n  session: \"s\"\r\n  chunk_text: |\r\n    a\r\n\r\n     b\r\n  ack: \"2\"\r\n"
	got, ok := parseBlock(y)
	if !ok || got["chunk_text"] != "a\n\n b\n" || got["ack"] != "2" {
		t.Fatalf("자동 들여쓰기/CRLF: %#v", got)
	}
	j, _ := json.Marshal(map[string]map[string]string{"awx_copy": {"session": "s", "chunk_text": "a\tb\n\n"}})
	got, ok = parseBlock(string(j))
	if !ok || got["chunk_text"] != "a\tb\n\n" {
		t.Fatalf("JSON: %#v", got)
	}
}

// TestDumpForPyYAML 은 AWX_COPY_DUMP 경로가 있으면 렌더 결과와 기대 값을 파일로 남긴다.
// test/check_yaml.py 로 PyYAML(AWX 와 같은 파서)이 같은 값으로 읽는지 확인한다.
func TestDumpForPyYAML(t *testing.T) {
	dir := os.Getenv("AWX_COPY_DUMP")
	if dir == "" {
		t.Skip("AWX_COPY_DUMP 없음")
	}
	for _, tc := range protoCases() {
		if len(tc.data) > 1<<20 {
			continue
		}
		protoRoundTrip(t, tc.data, tc.cs, true, func(k int, vars, text string) {
			base := filepath.Join(dir, tc.name+"_"+strconv.Itoa(k))
			_ = os.WriteFile(base+".yaml", []byte(vars), 0o600)
			exp, _ := json.Marshal(text)
			_ = os.WriteFile(base+".json", exp, 0o600)
		})
	}
}
