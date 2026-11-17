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
	go func() { defer wg.Done(); sendErr = sendFile(context.Background(), newClient(cfg), cfg, src, typ, &slog) }()
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
	const cs = 4096 // base64 4096자 = 원본 3072 bytes (비압축)
	text := "a\tb\tc\r\nline2\n한글 줄: 값 # 주석 - 'single' \"double\" {a: 1} [x, y]\n" +
		"key: value\n- item\n? q\n& * ! | > % @ `\n마지막 줄바꿈 없음"
	csv := "id,이름,메모\r\n1,\"홍길동\",\"a, b\"\r\n2,김철수,\"say \"\"hi\"\"\"\r\n3,탭\t포함,\"줄\n바꿈\"\r\n"
	cases := []struct {
		name string
		data []byte
		typ  string
		cs   int
	}{
		{"empty.txt", nil, "txt", cs},
		{"one.txt", []byte("x"), "txt", cs},
		{"edge_minus1.bin", randBytes(3071), "txt", cs},
		{"edge_exact.bin", randBytes(3072), "txt", cs},
		{"edge_plus1.bin", randBytes(3073), "txt", cs},
		{"times5.bin", randBytes(3072 * 5), "txt", cs},
		{"special: #-'\"{}[] 한글.txt", []byte(text), "txt", cs},
		{"data.csv", []byte(csv), "csv", cs},
		{"crlf_tabs_big.txt", bytes.Repeat([]byte("col1\tcol2\t한글\r\n"), 20000), "txt", cs},
		{"ten_mib.bin", randBytes(10 << 20), "txt", 1 << 20},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			fs, cfg := setup(t, tc.cs)
			orig := "# 원본 주석\nfoo: \"bar\"\nlist: [1, 2]\n"
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

func TestChunkCounts(t *testing.T) {
	for _, c := range []struct{ raw, want int }{{0, 1}, {1, 1}, {3071, 1}, {3072, 1}, {3073, 2}, {3072 * 5, 5}} {
		p, _ := encodePayload(randBytes(c.raw))
		got := len(splitChunks(base64.StdEncoding.EncodeToString(p), 4096))
		if got != c.want {
			t.Errorf("raw %d: 조각 %d, 기대 %d", c.raw, got, c.want)
		}
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
		go func() { defer wg.Done(); e1 = sendFile(context.Background(), newClient(cfg), cfg, src, "txt", io.Discard) }()
		go func() { defer wg.Done(); saved, e2 = fetchFile(context.Background(), newClient(cfg), cfg, out, io.Discard) }()
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
	go func() { defer wg.Done(); sendErr = sendFile(context.Background(), newClient(cfg), cfg, src, "txt", io.Discard) }()
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
	if c.URL != "http://x:1" || c.Password != "p w" || c.ChunkSize != 1000 || c.Timeout != 7*time.Second {
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
