package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"io"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"awx_copy/internal/fakeawx"
)

func setup(t *testing.T, chunk int) (*fakeawx.Server, *Config) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	fs := fakeawx.New("u", "secret-pw")
	ts := httptest.NewServer(fs)
	t.Cleanup(ts.Close)
	return fs, &Config{URL: ts.URL, Username: "u", Password: "secret-pw", InventoryID: 5, ChunkSize: chunk, Timeout: 20 * time.Second}
}

func randBytes(n int) []byte {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return b
}

// transfer 는 업로더와 다운로더를 동시에 돌려 결과 파일 경로를 돌려준다.
func transfer(t *testing.T, cfg *Config, name string, data []byte, typ string) (string, string) {
	t.Helper()
	src := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(src, data, 0o600); err != nil {
		t.Fatal(err)
	}
	out := t.TempDir()
	var wg sync.WaitGroup
	var sendErr, fetchErr error
	var saved string
	var slog, flog bytes.Buffer
	wg.Add(2)
	go func() {
		defer wg.Done()
		sendErr = sendFile(context.Background(), newClient(cfg), cfg, src, typ, &slog)
	}()
	go func() {
		defer wg.Done()
		saved, fetchErr = fetchFile(context.Background(), newClient(cfg), cfg, out, &flog)
	}()
	wg.Wait()
	if sendErr != nil || fetchErr != nil {
		t.Fatalf("send=%v fetch=%v\nsend log:\n%s\nfetch log:\n%s", sendErr, fetchErr, slog.String(), flog.String())
	}
	if strings.Contains(slog.String()+flog.String(), cfg.Password) {
		t.Fatal("비밀번호가 로그에 나왔습니다")
	}
	return saved, slog.String()
}

func TestRoundTrip(t *testing.T) {
	const cs = 4096 // 문자 수
	text := "a\tb\tc\nline2\n한글 줄: 값 # 주석 - 'single' \"double\" {a: 1} [x, y]\n" +
		"key: value\n- item\n? q\n& * ! | > % @ `\n마지막 줄바꿈 없음"
	csv := "id,이름,메모\r\n1,\"홍길동\",\"a, b\"\r\n2,김철수,\"say \"\"hi\"\"\"\r\n3,탭\t포함,\"줄\r\n바꿈\"\r\n"
	big := repeatTo("  들여쓰기\t탭: 값 # - '\" {}[]|>&*!%\r\n끝 공백   \r\n\r\n", 10<<20)
	cases := []struct {
		name string
		data []byte
		typ  string
		cs   int
	}{
		{"empty.txt", nil, "txt", cs},
		{"one.txt", []byte("x"), "txt", cs},
		{"edge_minus1.txt", bytes.Repeat([]byte("a"), cs-1), "txt", cs},
		{"edge_exact.txt", bytes.Repeat([]byte("a"), cs), "txt", cs},
		{"edge_plus1.txt", bytes.Repeat([]byte("a"), cs+1), "txt", cs},
		{"times5_korean.txt", bytes.Repeat([]byte("가나 \n"), cs*5/4), "txt", cs},
		{"special: #-'\"{}[] 한글.txt", []byte(text), "txt", cs},
		{"data.csv", []byte(csv), "csv", cs},
		{"trailing_ws.txt", []byte("  첫 줄 공백\n끝 공백  \n\n \t\n\n\n   "), "txt", 16},
		{"crlf_tabs_big.txt", bytes.Repeat([]byte("col1\tcol2\t한글\r\n"), 20000), "txt", cs},
		{"cr_only.txt", []byte("a\rb\r\r끝  \r"), "txt", cs},
		{"ten_mib.txt", big, "txt", 1 << 20},
	}
	for _, trim := range []bool{false, true} {
		for _, tc := range cases {
			tc, trim := tc, trim
			name := tc.name
			if trim {
				name = "awxtrim/" + name
			}
			t.Run(name, func(t *testing.T) {
				fs, cfg := setup(t, tc.cs)
				fs.TrimRight = trim
				orig := "# 원본 주석\nfoo: \"bar\"\nlist: [1, 2]\n"
				if trim {
					orig = strings.TrimRight(orig, "\n") // 실제 AWX 는 저장 값 끝 줄바꿈이 없다
				}
				fs.SetVars(5, orig)
				saved, _ := transfer(t, cfg, tc.name, tc.data, tc.typ)
				got, err := os.ReadFile(saved)
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(got, tc.data) || sha256Hex(got) != sha256Hex(tc.data) {
					t.Fatalf("내용 불일치: %d vs %d bytes", len(got), len(tc.data))
				}
				if v := fs.Vars(5); v != orig {
					t.Fatalf("원복 안 됨: %q", v)
				}
				for p := range fs.Patches() {
					if p != "/api/v2/inventories/5/" {
						t.Fatalf("허용되지 않은 PATCH: %s", p)
					}
				}
			})
		}
	}
}

// TestNonTextFailsSafely 는 텍스트가 아닌 파일을 보냈을 때 패닉 없이 실패하고,
// 파일을 저장하지 않고, variables 를 원복하는지 본다.
func TestNonTextFailsSafely(t *testing.T) {
	cases := map[string][]byte{
		"binary.bin":  randBytes(20000),
		"control.txt": []byte("ok\nbell\x07\x1b[0m\n"),
		"mixed.txt":   []byte("a\r\nb\nc\n"),
	}
	for name, data := range cases {
		name, data := name, data
		t.Run(name, func(t *testing.T) {
			fs, cfg := setup(t, 4096)
			fs.TrimRight = true
			fs.SetVars(5, "orig: 3")
			src := filepath.Join(t.TempDir(), name)
			_ = os.WriteFile(src, data, 0o600)
			out := t.TempDir()
			var wg sync.WaitGroup
			var sendErr, fetchErr error
			var slog bytes.Buffer
			wg.Add(2)
			go func() {
				defer wg.Done()
				sendErr = sendFile(context.Background(), newClient(cfg), cfg, src, "txt", &slog)
			}()
			go func() {
				defer wg.Done()
				_, fetchErr = fetchFile(context.Background(), newClient(cfg), cfg, out, io.Discard)
			}()
			wg.Wait()
			if fetchErr == nil || !strings.Contains(fetchErr.Error(), "SHA-256") {
				t.Fatalf("해시 불일치로 실패해야 합니다: fetch=%v", fetchErr)
			}
			// 마지막 조각에서 실패하면 업로더는 원복만 보고 성공으로 끝난다(알려진 한계, Step 3 에서 다룬다).
			_ = sendErr
			if !strings.Contains(slog.String(), "경고") {
				t.Fatalf("업로더 경고가 없습니다:\n%s", slog.String())
			}
			if ents, _ := os.ReadDir(out); len(ents) != 0 {
				t.Fatalf("파일을 저장하면 안 됩니다: %d개", len(ents))
			}
			if v := fs.Vars(5); v != "orig: 3" {
				t.Fatalf("원복 안 됨: %q", v)
			}
		})
	}
}

// TestFetchRejectsChunkRaw 는 v2 블록에 chunk_raw 가 있으면 수신기가 거부하는지 본다.
func TestFetchRejectsChunkRaw(t *testing.T) {
	fs, cfg := setup(t, 4096)
	b := Block{"version": protoVersion, "session": "s9", "state": stateSending, "file_name": "a.txt", "type": "txt",
		"encoding": encText, "eol": eolLF, "final_newline": "false", "total_bytes": "1", "sha256": sha256Hex([]byte("a")),
		"chunk_total": "1", "chunk_index": "1", "chunk_sha256": sha256Hex([]byte("a")), "chunk_text": "a", "ack": "0",
		"backup_b64": "", "updated_at": nowStamp()}
	vars := strings.Replace(b.Render(), "  chunk_text:", "  chunk_raw: \"0:00\"\n  chunk_text:", 1)
	fs.SetVars(5, vars)
	_, err := fetchFile(context.Background(), newClient(cfg), cfg, t.TempDir(), io.Discard)
	if err == nil || !strings.Contains(err.Error(), "chunk_raw") {
		t.Fatalf("chunk_raw 를 거부해야 합니다: %v", err)
	}
}

func TestChunkCounts(t *testing.T) {
	for _, c := range []struct{ n, want int }{{0, 1}, {1, 1}, {4095, 1}, {4096, 1}, {4097, 2}, {4096 * 5, 5}} {
		got := len(splitText(bytes.Repeat([]byte("a"), c.n), eolLF, 4096, maxChunkBytes))
		if got != c.want {
			t.Errorf("문자 %d: 조각 %d, 기대 %d", c.n, got, c.want)
		}
	}
}

// v1(gzip+base64) 함수는 지우지 않았다. 계속 왕복되는지만 본다.
func TestV1PayloadStillWorks(t *testing.T) {
	data := randBytes(5000)
	p, enc := encodePayload(data)
	got, err := decodePayload(base64.StdEncoding.EncodeToString(p), enc)
	if err != nil || !bytes.Equal(got, data) {
		t.Fatalf("v1 왕복 실패: %v", err)
	}
	if n := len(splitChunks("abcdefgh", 4)); n != 2 {
		t.Fatalf("splitChunks %d", n)
	}
}

func TestDuplicateName(t *testing.T) {
	fs, cfg := setup(t, 4096)
	_ = fs
	src := filepath.Join(t.TempDir(), "dup.txt")
	_ = os.WriteFile(src, []byte("hello"), 0o600)
	out := t.TempDir()
	for i := 0; i < 2; i++ {
		var wg sync.WaitGroup
		var saved string
		var e1, e2 error
		wg.Add(2)
		go func() {
			defer wg.Done()
			e1 = sendFile(context.Background(), newClient(cfg), cfg, src, "txt", io.Discard)
		}()
		go func() {
			defer wg.Done()
			saved, e2 = fetchFile(context.Background(), newClient(cfg), cfg, out, io.Discard)
		}()
		wg.Wait()
		if e1 != nil || e2 != nil {
			t.Fatal(e1, e2)
		}
		want := []string{"dup.txt", "dup_1.txt"}[i]
		if filepath.Base(saved) != want {
			t.Fatalf("저장 이름 %s, 기대 %s", filepath.Base(saved), want)
		}
	}
}

func TestBusyRefused(t *testing.T) {
	fs, cfg := setup(t, 4096)
	busy := Block{"version": "1", "session": "other"}.Render()
	fs.SetVars(5, busy)
	src := filepath.Join(t.TempDir(), "a.txt")
	_ = os.WriteFile(src, []byte("x"), 0o600)
	err := sendFile(context.Background(), newClient(cfg), cfg, src, "txt", io.Discard)
	if !errors.Is(err, errBusy) {
		t.Fatalf("거부되어야 합니다: %v", err)
	}
	if fs.Vars(5) != busy {
		t.Fatal("다른 세션 블록을 건드렸습니다")
	}
}

func TestSenderCancelRestores(t *testing.T) {
	fs, cfg := setup(t, 4096)
	fs.SetVars(5, "orig: 1\n")
	src := filepath.Join(t.TempDir(), "a.bin")
	_ = os.WriteFile(src, randBytes(9000), 0o600)
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(700 * time.Millisecond); cancel() }() // 다운로더 없음 → ack 대기 중 Ctrl+C
	err := sendFile(ctx, newClient(cfg), cfg, src, "txt", io.Discard)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("취소 오류여야 합니다: %v", err)
	}
	if fs.Vars(5) != "orig: 1\n" {
		t.Fatalf("원복 안 됨: %q", fs.Vars(5))
	}
}

func TestRestoreCommand(t *testing.T) {
	fs, cfg := setup(t, 4096)
	fs.SetVars(5, "orig: 2\n")
	src := filepath.Join(t.TempDir(), "a.bin")
	_ = os.WriteFile(src, randBytes(9000), 0o600)
	// 업로더가 강제 종료(kill -9)된 상황: 블록이 남는다
	c := newClient(cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 600*time.Millisecond)
	defer cancel()
	go func() { _ = sendFile(ctx, c, cfg, src, "txt", io.Discard) }()
	time.Sleep(300 * time.Millisecond)
	if !hasBlock(fs.Vars(5)) {
		t.Fatal("블록이 있어야 합니다")
	}
	// 강제 종료 흉내: 블록을 다시 써 둔다(원복 전 상태)
	left := fs.Vars(5)
	time.Sleep(800 * time.Millisecond)
	fs.SetVars(5, left)
	if err := runRestore(context.Background(), c, cfg, io.Discard); err != nil {
		t.Fatal(err)
	}
	if fs.Vars(5) != "orig: 2\n" {
		t.Fatalf("복원 실패: %q", fs.Vars(5))
	}
	// 블록이 깨졌을 때 로컬 백업으로 복원
	fs.SetVars(5, "awx_copy:\n  broken\n")
	if err := runRestore(context.Background(), c, cfg, io.Discard); err != nil {
		t.Fatal(err)
	}
	if fs.Vars(5) != "orig: 2\n" {
		t.Fatalf("로컬 백업 복원 실패: %q", fs.Vars(5))
	}
}

func TestFetcherAbortStopsSender(t *testing.T) {
	fs, cfg := setup(t, 4096)
	fs.SetVars(5, "")
	src := filepath.Join(t.TempDir(), "a.bin")
	_ = os.WriteFile(src, randBytes(3072*100), 0o600)
	ctx, cancel := context.WithCancel(context.Background())
	var sendErr error
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		sendErr = sendFile(context.Background(), newClient(cfg), cfg, src, "txt", io.Discard)
	}()
	go func() { time.Sleep(900 * time.Millisecond); cancel() }()
	_, ferr := fetchFile(ctx, newClient(cfg), cfg, t.TempDir(), io.Discard)
	wg.Wait()
	if ferr == nil || sendErr == nil {
		t.Fatalf("둘 다 실패해야 합니다: fetch=%v send=%v", ferr, sendErr)
	}
	if fs.Vars(5) != "" {
		t.Fatalf("원복 안 됨: %q", fs.Vars(5))
	}
}

func TestBlockQuoting(t *testing.T) {
	b := Block{"session": "s", "file_name": "a: #-'\"{}[]\t\\ 한글\r\n", "chunk_data": "QUJD+/=="}
	got, ok := parseBlock("# 앞 주석\n" + b.Render())
	if !ok || got["file_name"] != b["file_name"] || got["chunk_data"] != b["chunk_data"] {
		t.Fatalf("왕복 실패: %#v", got)
	}
}

func TestConfig(t *testing.T) {
	d := t.TempDir()
	p := filepath.Join(d, "c.conf")
	_ = os.WriteFile(p, []byte("# c\nurl = http://x:1/\nusername: u\npassword = \"p w\"\ninventory_id=5\nchunk_size = 1001\ntimeout=7\n"), 0o600)
	c, err := loadConfig(p)
	if err != nil {
		t.Fatal(err)
	}
	if c.URL != "http://x:1" || c.Password != "p w" || c.ChunkSize != 1001 || c.Timeout != 7*time.Second {
		t.Fatalf("%+v", c)
	}
	if strings.Contains(c.String(), "p w") {
		t.Fatal("String() 에 비밀번호")
	}
}

func TestSanitize(t *testing.T) {
	for in, want := range map[string]string{"../../a.txt": "a.txt", "a:b?.csv": "a_b_.csv", "CON.txt": "_CON.txt", "": "awx_copy_file", "한글.txt": "한글.txt"} {
		if got := sanitizeFileName(in); got != want {
			t.Errorf("%q → %q, 기대 %q", in, got, want)
		}
	}
}
