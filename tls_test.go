package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// 자체서명 https 서버: 기본값은 접속되고, verify_tls=true 면 x509 오류가 난다.
func TestTLSSelfSigned(t *testing.T) {
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }))
	defer ts.Close()
	cfg := &Config{URL: ts.URL, Username: "u", Password: "p", InventoryID: 1, Timeout: 5 * time.Second}
	if resp, err := newClient(cfg).hc.Get(ts.URL); err != nil {
		t.Fatalf("기본값(인증서 검증 안 함)에서 접속 실패: %v", err)
	} else {
		resp.Body.Close()
	}
	cfg.VerifyTLS = true
	if _, err := newClient(cfg).hc.Get(ts.URL); err == nil || !strings.Contains(err.Error(), "x509") {
		t.Fatalf("verify_tls=true 인데 x509 오류가 나지 않음: %v", err)
	}
}

func TestParseAwxURL(t *testing.T) {
	for in, want := range map[string]struct {
		base string
		inv  int
	}{
		"https://h/awx/#/inventories/inventory/33/details": {"https://h/awx", 33},
		"http://h:30080/#/inventories/inventory/5/details": {"http://h:30080", 5},
		"https://h/api/v2/": {"https://h", 0},
		"http://h:1/":       {"http://h:1", 0},
	} {
		b, n := parseAwxURL(in)
		if b != want.base || n != want.inv {
			t.Errorf("%q → %q,%d (want %q,%d)", in, b, n, want.base, want.inv)
		}
	}
}
