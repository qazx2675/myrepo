package ucsmxml

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// Probe 는 인증 정보 없이 고정된 2건(GET /, POST /nuova 쿠키 없음)만 보낸다.
func TestProbeSendsOnlyFixedRequests(t *testing.T) {
	var got []string
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		got = append(got, r.Method+" "+r.URL.Path+" "+string(b))
		if r.URL.Path == "/nuova" {
			w.Header().Set("Content-Type", "text/xml")
			_, _ = w.Write([]byte(`<configResolveClass cookie="" response="yes" errorCode="552" errorDescr="Authorization required"/>`))
			return
		}
		_, _ = w.Write([]byte(`<html><head><title> UCSM  Login </title></head></html>`))
	}))
	defer srv.Close()
	r := Probe(srv.URL, true, 5*time.Second)
	if r.Kind != KindXML || r.RootElement != "configResolveClass" || r.ErrCode != "552" || r.RootTitle != "UCSM Login" || r.Requests != 2 {
		t.Errorf("결과: %+v", r)
	}
	if len(got) != 2 || !strings.HasPrefix(got[0], "GET / ") || !strings.HasPrefix(got[1], "POST /nuova ") || strings.Contains(got[1], "aaaLogin") || strings.Contains(got[1], "inPassword") {
		t.Errorf("보낸 요청: %q", got)
	}
}

func TestProbeNotXML(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/nuova" {
			w.WriteHeader(404)
			return
		}
		_, _ = w.Write([]byte(`ok`))
	}))
	defer srv.Close()
	if r := Probe(srv.URL, true, 5*time.Second); r.Kind != KindOther || r.APIStatus != 404 {
		t.Errorf("404: %+v", r)
	}
	if r := Probe("http://x", true, time.Second); r.Requests != 0 || r.APIErr == "" {
		t.Errorf("http 거부: %+v", r)
	}
}
