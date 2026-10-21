package ucsmxml

import (
	"bytes"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// probe.go — 진단용 XML API 존재 확인입니다 (biostool trs-cisco). 인증 정보는 쓰지 않습니다.
//
// 보내는 요청은 고정된 2가지뿐입니다.
//   - GET /                 : 웹 페이지 머리(상태·Server·Location·제목)만 봅니다.
//   - POST /nuova           : 쿠키가 빈 configResolveClass 한 번. XML API 면 인증 요구(errorCode)로 답하고 아무것도 읽지 않습니다.
// 비밀번호·쿠키가 없으므로 계정 잠금 위험이 없습니다.

// probeBody 는 쿠키 없는 확인 요청 본문입니다 (고정 상수).
const probeBody = `<configResolveClass cookie="" classId="computeBlade" inHierarchical="false"/>`

// 응답 종류 (ProbeResult.Kind)
const (
	KindXML   = "xml"   // XML 로 응답함 (UCSM/CIMC XML API)
	KindHTML  = "html"  // 웹 페이지 (XML API 아님)
	KindOther = "other" // 그 밖의 응답
)

var (
	reTitle   = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)
	reRootEl  = regexp.MustCompile(`<\s*([A-Za-z][A-Za-z0-9_]*)`)
	reErrCode = regexp.MustCompile(`errorCode\s*=\s*"([^"]*)"`)
	reErrDesc = regexp.MustCompile(`errorDescr\s*=\s*"([^"]*)"`)
)

// ProbeResult 는 Probe 결과입니다. 문자열은 제어문자를 지우고 길이를 줄인 것입니다.
type ProbeResult struct {
	RootStatus      int // GET / 상태 (0 = 응답 없음)
	RootErr         string
	RootServer      string
	RootLocation    string
	RootContentType string
	RootTitle       string

	APIStatus      int // POST /nuova 상태 (0 = 응답 없음)
	APIErr         string
	APIContentType string
	Kind           string // KindXML | KindHTML | KindOther | "" (응답 없음)
	RootElement    string // XML 이면 루트 요소 이름
	ErrCode        string
	ErrDescr       string

	Requests int // 실제로 보낸 요청 수
}

// Probe 는 baseURL(https://호스트[:포트]) 에 XML API 가 있는지 인증 없이 확인합니다.
func Probe(baseURL string, insecure bool, timeout time.Duration) ProbeResult {
	var r ProbeResult
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	u, err := url.Parse(baseURL)
	if err != nil || !strings.EqualFold(u.Scheme, "https") || u.Host == "" || u.User != nil {
		r.RootErr, r.APIErr = "BaseURL 은 https://<호스트> 형식이어야 합니다", "BaseURL 은 https://<호스트> 형식이어야 합니다"
		return r
	}
	hc := &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			Proxy:                  nil,
			DialContext:            (&net.Dialer{Timeout: timeout}).DialContext,
			TLSHandshakeTimeout:    timeout,
			TLSClientConfig:        &tls.Config{InsecureSkipVerify: insecure},
			DisableKeepAlives:      true,
			MaxResponseHeaderBytes: 64 << 10,
		},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	defer hc.CloseIdleConnections()
	do := func(method, path string, body []byte) (*http.Response, []byte, error) {
		var rd io.Reader
		if body != nil {
			rd = bytes.NewReader(body)
		}
		req, err := http.NewRequest(method, (&url.URL{Scheme: "https", Host: u.Host, Path: path}).String(), rd)
		if err != nil {
			return nil, nil, err
		}
		req.Header.Set("User-Agent", "biostool-trs/1")
		if body != nil {
			req.Header.Set("Content-Type", "text/xml")
			req.Header.Set("Accept", "text/xml")
		}
		r.Requests++
		resp, err := hc.Do(req)
		if err != nil {
			var ue *url.Error
			if errors.As(err, &ue) {
				err = ue.Err
			}
			return nil, nil, err
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 256<<10))
		return resp, b, nil
	}

	if resp, b, err := do(http.MethodGet, "/", nil); err != nil {
		r.RootErr = clipText(err.Error())
	} else {
		r.RootStatus = resp.StatusCode
		r.RootServer = clipText(resp.Header.Get("Server"))
		r.RootLocation = clipText(resp.Header.Get("Location"))
		r.RootContentType = clipText(resp.Header.Get("Content-Type"))
		if m := reTitle.FindSubmatch(b); m != nil {
			r.RootTitle = clipText(strings.Join(strings.Fields(string(m[1])), " "))
		}
	}

	resp, b, err := do(http.MethodPost, endpoint, []byte(probeBody))
	if err != nil {
		r.APIErr = clipText(err.Error())
		return r
	}
	r.APIStatus = resp.StatusCode
	r.APIContentType = clipText(resp.Header.Get("Content-Type"))
	text := string(b)
	trim := strings.TrimSpace(text)
	head := strings.ToLower(trim)
	if len(head) > 200 {
		head = head[:200]
	}
	switch {
	case strings.Contains(head, "<html"):
		r.Kind = KindHTML
	case resp.StatusCode >= 200 && resp.StatusCode < 300 && strings.HasPrefix(trim, "<"):
		r.Kind = KindXML
		for _, m := range reRootEl.FindAllStringSubmatch(trim, 2) {
			if m[1] != "xml" {
				r.RootElement = clipText(m[1])
				break
			}
		}
		if m := reErrCode.FindStringSubmatch(text); m != nil {
			r.ErrCode = clipText(m[1])
		}
		if m := reErrDesc.FindStringSubmatch(text); m != nil {
			r.ErrDescr = clipText(m[1])
		}
	default:
		r.Kind = KindOther
	}
	return r
}

func clipText(s string) string {
	s = sanitize(s)
	if rs := []rune(s); len(rs) > 80 {
		s = string(rs[:80]) + "..."
	}
	return s
}
