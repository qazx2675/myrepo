package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"awxkit/awx"
	"awxkit/config"
)

// 줄 단위 입력을 차례로 돌려주는 입력 함수를 만든다. 다 쓰면 EOF처럼 빈 문자열을 반환한다.
func fakeInput(lines ...string) func(string) string {
	i := 0
	return func(string) string {
		if i >= len(lines) {
			return ""
		}
		l := lines[i]
		i++
		return l
	}
}

func setup(t *testing.T, okID int, answers []string, yes bool) (*awx.Client, *config.Config, string, *[]string) {
	t.Helper()
	var calls []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Method+" "+r.URL.Path)
		wantPatch := fmt.Sprintf("/api/v2/inventory_sources/%d/", okID)
		wantSync := fmt.Sprintf("/api/v2/inventory_sources/%d/update/", okID)
		switch {
		case r.Method == http.MethodPatch && r.URL.Path == wantPatch:
			w.Write([]byte(`{"id":1}`))
		case r.Method == http.MethodPost && r.URL.Path == wantSync:
			w.Write([]byte(`{"inventory_update":55}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	dir := t.TempDir()
	confPath := filepath.Join(dir, "u_setting.conf")
	if err := os.WriteFile(confPath, []byte("s2_inventory = 7\ns2_inventory_source = 12 # 소스\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(confPath)
	if err != nil {
		t.Fatal(err)
	}
	cfg.HistoryFile = filepath.Join(dir, "hist.log")

	origLine, origYesNo := promptLine, promptYesNo
	promptLine = fakeInput(answers...)
	promptYesNo = func(string) bool { return yes }
	t.Cleanup(func() { promptLine, promptYesNo = origLine, origYesNo })

	return awx.NewClient(srv.URL, "u", "p", false, 5*time.Second), cfg, confPath, &calls
}

func TestRecoverOn404Success(t *testing.T) {
	client, cfg, confPath, calls := setup(t, 34, []string{"abc", "0", "34"}, true)

	_, err := patchAndSync(client, cfg, 12, "a.yml")
	newID, ok := recoverSourceID(cfg, confPath, "u", err)
	if !ok || newID != 34 || cfg.S2InventorySource != "34" {
		t.Fatalf("복구 실패: ok=%v newID=%d cfg=%s", ok, newID, cfg.S2InventorySource)
	}
	updateID, err := patchAndSync(client, cfg, newID, "a.yml")
	if err != nil || updateID != 55 {
		t.Fatalf("재시도 실패: %d %v", updateID, err)
	}
	if len(*calls) != 3 || (*calls)[0] != "PATCH /api/v2/inventory_sources/12/" {
		t.Fatalf("호출 순서 이상: %v", *calls)
	}

	b, _ := os.ReadFile(confPath)
	if string(b) != "s2_inventory = 7\ns2_inventory_source = 34 # 소스\n" {
		t.Fatalf("conf 내용 이상: %q", b)
	}
	h, _ := os.ReadFile(cfg.HistoryFile)
	if !strings.Contains(string(h), "status=source_updated old=12 new=34") {
		t.Fatalf("이력 이상: %q", h)
	}
}

func TestRecoverDeclinedOrNoInput(t *testing.T) {
	// n 응답
	client, cfg, confPath, _ := setup(t, 34, []string{"34"}, false)
	_, err := patchAndSync(client, cfg, 12, "a.yml")
	if _, ok := recoverSourceID(cfg, confPath, "u", err); ok {
		t.Fatal("n 이면 실패여야 합니다")
	}
	if b, _ := os.ReadFile(confPath); !strings.Contains(string(b), "= 12") {
		t.Fatalf("conf 가 바뀌면 안 됩니다: %q", b)
	}

	// EOF(빈 입력)
	client, cfg, confPath, _ = setup(t, 34, nil, true)
	_, err = patchAndSync(client, cfg, 12, "a.yml")
	if _, ok := recoverSourceID(cfg, confPath, "u", err); ok {
		t.Fatal("EOF 면 실패여야 합니다")
	}

	// 3번 모두 숫자가 아님
	client, cfg, confPath, _ = setup(t, 34, []string{"x", "y", "-3", "34"}, true)
	_, err = patchAndSync(client, cfg, 12, "a.yml")
	if _, ok := recoverSourceID(cfg, confPath, "u", err); ok {
		t.Fatal("3번 틀리면 실패여야 합니다")
	}
}

func TestRecoverSkipped(t *testing.T) {
	client, cfg, confPath, _ := setup(t, 34, []string{"34"}, true)
	_, err404 := patchAndSync(client, cfg, 12, "a.yml")

	// 자동탐색(소스 ID 비어 있음)이면 404 라도 묻지 않는다.
	cfg.S2InventorySource = ""
	if _, ok := recoverSourceID(cfg, confPath, "u", err404); ok {
		t.Fatal("자동탐색은 종전 동작이어야 합니다")
	}
	// 404 가 아닌 오류
	cfg.S2InventorySource = "12"
	if _, ok := recoverSourceID(cfg, confPath, "u", fmt.Errorf("기타 오류")); ok {
		t.Fatal("404 가 아니면 종전 동작이어야 합니다")
	}
	if _, ok := recoverSourceID(cfg, confPath, "u", &awx.APIError{StatusCode: 500}); ok {
		t.Fatal("500 은 종전 동작이어야 합니다")
	}
}
