package main

import (
	"bytes"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// trs.go 는 `biostool trs` — BMC 접속 장애(403 등)를 단계별로 나눠 원인을 좁히는 진단 명령입니다.
//
//   - 읽기 전용입니다: GET/HEAD/OPTIONS 와 세션 생성(POST)·삭제(DELETE, 이번 실행이 만든 세션만) 외에는 보내지 않습니다.
//     로그/Actions 경로는 GET 하지 않습니다 (redfish.go 의 deniedGetSeg 와 같은 규칙).
//   - 계정 잠금 방지: 인증 시도는 Basic 1회 + 세션 생성 1회(+ 2순위 계정 1회)뿐이고, 401 이 나오면 즉시 중단합니다.
//   - 비밀번호·토큰은 어떤 출력·파일에도 싣지 않습니다 (passwordForms 로 변형까지 지움).
//   - 각 단계의 상태 코드·응답 헤더·Redfish 오류 메시지(MessageId)를 그대로 보여 주고, 헤더 변형 비교와 원인 추정을 덧붙입니다.
//     새 장애 유형을 만나면 trsDiagnose 에 규칙을 추가합니다.

const (
	trsBodyShow = 700 // 터미널·파일에 싣는 오류 본문 최대 글자 수
	trsMaxAcct  = 16  // 내 계정을 찾으려고 읽는 계정 멤버 수 상한
	trsMaxDeny  = 2   // 헤더 변형 비교를 하는 403 경로 수
)

type trsReq struct {
	method, path string
	auth         string // "" | "basic" | "token"
	user, pass   string // basic 용 (세션 생성 본문은 body 로)
	token        string
	body         []byte
	ua           string
	uaSet        bool // ua 가 "" 여도 지정한 것으로 봄 (User-Agent 생략 시험)
	noOData      bool
	noAccept     bool
	contentType  string
}

type trsRes struct {
	status int
	hdr    http.Header
	body   []byte
	err    error
}

// trsFacts 는 진단 규칙(trsDiagnose)이 보는 관찰 결과입니다.
type trsFacts struct {
	tcpErr, tlsErr     string
	rootStatus         int
	rootErr            string
	vendor, product    string
	uaDiff             []string // 비인증 ServiceRoot 가 403 일 때 헤더·UA 변형으로 상태가 달라진 경우
	basicStatus        int
	loginStatus        int
	tokenOK, basicOK   bool
	halted             bool // 401 → 계정 잠금 방지로 이후 인증 시도 중단
	msgs               []string
	denied             []string         // 인증이 통과한 상태에서 403 이 난 경로
	failed             []trsFail        // 인증 후 4xx(404 제외)가 난 경로 — 변형 비교 대상
	codes              map[int][]string // 4xx 상태 코드별 "METHOD 경로" (진단 표용)
	replayRan          bool             // bios.sh 와 같은 방식(실제 Client) 재현을 실행했는지
	replayErr          string           // 재현 실패 요약 (비면 성공)
	replayUAGo         int              // 재현 실패 시: 기본 UA(Go-http-client)로 보낸 요청 상태
	replayKA           []int            // 재현 실패 시: 연결 재사용으로 연속 2회 보낸 요청 상태
	rootNoSlash        int              // 비인증 GET /redfish/v1 (끝 슬래시 없음) 상태
	rootNoSlashLoc     string           // 그 응답의 Location
	sessionVariantOK   string           // 세션 생성이 4xx 였는데 성공한 변형 이름
	variantDiff        []string
	acctFound          bool
	acctEnabled        *bool
	acctLocked         *bool
	acctPwChange       *bool
	acctRole           string
	fbTried            bool
	fbStatus           int
	sessionErr         bool
	basicDisabledHint  bool
	protoOff           []string // NetworkProtocol 에서 꺼진 서비스
	systemsOK, biosOK  bool
	extraStatus        map[string]int
	hadAuthedReadError bool
}

type trsFail struct {
	path   string
	status int
}

type trsHost struct {
	conf     *Config
	t        Target
	user     string
	pass     string
	fb       *fallbackCreds
	pr       *Prober
	hostport string
	tlsShown bool
	out      *bytes.Buffer
	secrets  []string
	n        int
	token    string
	f        trsFacts
}

var (
	reTrsName = regexp.MustCompile(`[^A-Za-z0-9._-]`)
)

func cmdTrs(args []string) error {
	fs, o := newFlagSet("trs")
	targets := fs.String("targets", "", "대상(쉼표 또는 공백 구분, ip:포트 가능). 지정하면 -user 파일 대신 사용")
	extra := fs.String("path", "", "추가로 GET 해 볼 Redfish 경로 (쉼표 구분, 예: /redfish/v1/Systems/1/Bios/Settings)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	rc, err := prepare(o, *targets, "")
	if err != nil {
		return err
	}
	var paths []string
	for _, p := range strings.FieldsFunc(*extra, func(r rune) bool { return r == ',' || r == ' ' }) {
		paths = append(paths, p)
	}

	n := len(rc.Targets)
	reports := make([]string, n)
	workers := rc.Conf.Concurrency
	if workers > 4 {
		workers = 4
	}
	if workers < 1 {
		workers = 1
	}
	sem := make(chan struct{}, workers)
	var wg sync.WaitGroup
	for i, t := range rc.Targets {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, t Target) {
			defer wg.Done()
			defer func() { <-sem }()
			if t.Err != "" || t.BaseURL() == "" {
				reports[i] = fmt.Sprintf("== %s ==\n  대상을 해석하지 못했습니다 (%s)\n", t.Input, t.Err)
				return
			}
			reports[i], _ = trsRunHost(t, rc.Conf, rc.Password, rc.Fallback, paths)
		}(i, t)
	}
	wg.Wait()

	dir, derr := newRunDir(rc.Conf.ResultDir, time.Now())
	if derr == nil {
		if os.Rename(dir, dir+"_trs") == nil {
			dir += "_trs"
		}
	}
	for i, rep := range reports {
		fmt.Print(rep)
		fmt.Println()
		if derr == nil {
			name := reTrsName.ReplaceAllString(rc.Targets[i].Hostname, "_")
			if name == "" {
				name = fmt.Sprintf("host%d", i+1)
			}
			_ = os.WriteFile(filepath.Join(dir, name+".txt"), []byte(rep), 0o600)
		}
	}
	if derr == nil {
		fmt.Printf("상세 기록: %s/  (호스트별 .txt, 비밀번호·토큰 없음)\n", dir)
	}
	fmt.Println("주의: 이 진단은 원인을 좁히는 참고용입니다. 결과를 이 대화에 붙여 주시면 로직을 보완합니다.")
	return nil
}

// trsRunHost 는 호스트 1대를 진단해 리포트 텍스트를 돌려줍니다.
func trsRunHost(t Target, conf *Config, pass string, fb *fallbackCreds, extraPaths []string) (string, trsFacts) {
	timeout := conf.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	h := &trsHost{conf: conf, t: t, user: conf.User, pass: pass, fb: fb, out: &bytes.Buffer{}}
	h.secrets = append(h.secrets, passwordForms(pass)...)
	h.secrets = append(h.secrets, base64.StdEncoding.EncodeToString([]byte(conf.User+":"+pass)))
	h.hostport = t.IP
	if strings.Contains(h.hostport, ":") {
		h.hostport = "[" + h.hostport + "]"
	}
	if t.Port != "" {
		h.hostport += ":" + t.Port
	} else {
		h.hostport += ":443"
	}
	h.pr = NewProber(h.hostport, conf.Insecure, timeout)
	defer h.pr.Close()

	if addr := strings.TrimPrefix(t.BaseURL(), "https://"); addr == t.Hostname {
		h.log("== %s ==", t.Hostname)
	} else {
		h.log("== %s  (%s) ==", t.Hostname, addr)
	}
	h.log("계정: %s (bios.conf user) / 시각: %s", conf.User, time.Now().Format("2006-01-02 15:04:05"))
	h.stepNetwork()
	h.stepRoot()
	if h.f.rootStatus != 0 {
		h.stepAuth()
		h.stepReads(extraPaths)
		h.stepReplay()
	}
	h.stepLogout()
	h.log("")
	h.log("[진단] 요청 %d건", h.n)
	for _, s := range trsDiagnose(&h.f) {
		h.log("  %s", s)
	}
	return h.out.String(), h.f
}

func (h *trsHost) log(format string, a ...interface{}) {
	fmt.Fprintf(h.out, format+"\n", a...)
}

// clean 은 비밀번호·토큰을 지우고 제어문자를 무력화합니다.
func (h *trsHost) clean(s string) string {
	for _, x := range h.secrets {
		s = strings.ReplaceAll(s, x, "***")
	}
	return sanitizeDetail(s)
}

func (h *trsHost) addSecret(s string) {
	if s != "" {
		h.secrets = append(h.secrets, s)
	}
}

// ---- 1. 네트워크·TLS ----

func (h *trsHost) stepNetwork() {
	h.log("")
	h.log("[1] 네트워크")
	d, err := h.pr.Dial()
	if err != nil {
		h.f.tcpErr = h.clean(err.Error())
		h.log("  TCP %s  실패: %s", h.hostport, h.f.tcpErr)
		return
	}
	h.log("  TCP %s  연결 성공 (%dms)", h.hostport, d.Milliseconds())
}

func (h *trsHost) tlsInfo(st *tls.ConnectionState) {
	if st == nil || h.tlsShown {
		return
	}
	h.tlsShown = true
	ver := map[uint16]string{tls.VersionTLS10: "TLS1.0", tls.VersionTLS11: "TLS1.1", tls.VersionTLS12: "TLS1.2", tls.VersionTLS13: "TLS1.3"}[st.Version]
	line := fmt.Sprintf("  TLS %s %s", ver, tls.CipherSuiteName(st.CipherSuite))
	if len(st.PeerCertificates) > 0 {
		c := st.PeerCertificates[0]
		line += fmt.Sprintf(" / 인증서 CN=%s 발급=%s 만료=%s", h.clean(c.Subject.CommonName), h.clean(c.Issuer.CommonName), c.NotAfter.Format("2006-01-02"))
		if time.Now().After(c.NotAfter) {
			line += " (만료됨)"
		}
	}
	h.log("%s", line)
}

var trsShowHdr = []string{"Server", "Www-Authenticate", "Allow", "Content-Type", "Location", "Retry-After", "Via", "Odata-Version", "Connection"}

// req 는 Prober 로 요청을 보내고 결과를 리포트에 기록합니다. 허용 검사는 Prober 가 합니다.
func (h *trsHost) req(label string, r trsReq) trsRes {
	pr := h.pr.Send(ProbeReq{Method: r.method, Path: r.path, Auth: r.auth, User: r.user, Pass: r.pass, Token: r.token,
		Body: r.body, UA: r.ua, UASet: r.uaSet, NoOData: r.noOData, NoAccept: r.noAccept, ContentType: r.contentType})
	if pr.Sent == "" {
		h.log("  ! %s: 보내지 않음 (%v)", label, pr.Err)
		return trsRes{err: pr.Err}
	}
	authDesc := map[string]string{"": "인증 없음", "basic": "Basic", "token": "세션 토큰"}[r.auth]
	h.n++
	n := h.n
	if pr.Err != nil {
		h.log("  [%d] %s %s  (%s) %s → 실패 %dms: %s", n, r.method, pr.Sent, authDesc, label, pr.Elapsed.Milliseconds(), h.clean(pr.Err.Error()))
		return trsRes{err: pr.Err}
	}
	h.tlsInfo(pr.TLS)
	h.log("  [%d] %s %s  (%s) %s → %d %s  %s %dms", n, r.method, pr.Sent, authDesc, label, pr.Status, http.StatusText(pr.Status), pr.Proto, pr.Elapsed.Milliseconds())
	for _, k := range trsShowHdr {
		if v := pr.Header.Get(k); v != "" {
			h.log("        %s: %s", k, h.clean(v))
		}
	}
	if pr.Header.Get("X-Auth-Token") != "" {
		h.log("        X-Auth-Token: (있음)")
	}
	if cs := pr.Header.Values("Set-Cookie"); len(cs) > 0 {
		var names []string
		for _, c := range cs {
			names = append(names, h.clean(strings.SplitN(c, "=", 2)[0]))
		}
		h.log("        Set-Cookie: %s (이름만)", strings.Join(names, ","))
	}
	if pr.Status >= 400 && pr.Status < 500 && !(r.auth == "" && pr.Status == http.StatusUnauthorized) { // 비인증 401 은 정상 동작
		if h.f.codes == nil {
			h.f.codes = map[int][]string{}
		}
		if l := h.f.codes[pr.Status]; len(l) < 6 {
			h.f.codes[pr.Status] = append(l, r.method+" "+pr.Sent)
		}
	}
	if pr.Status >= 400 {
		msgs := trsMessages(pr.Body)
		for _, m := range msgs {
			h.log("        오류: %s", h.clean(m))
			h.f.msgs = append(h.f.msgs, strings.ToLower(m))
		}
		if pr.Status == 403 || pr.Status == 401 || len(msgs) == 0 {
			if s := h.clean(string(pr.Body)); s != "" {
				if rs := []rune(s); len(rs) > trsBodyShow {
					s = string(rs[:trsBodyShow]) + "..."
				}
				h.log("        본문: %s", s)
			}
		}
	}
	return trsRes{status: pr.Status, hdr: pr.Header, body: pr.Body}
}

type trsExt struct {
	MessageID  string `json:"MessageId"`
	Message    string `json:"Message"`
	Resolution string `json:"Resolution"`
}

// trsMessages 는 Redfish 오류 본문에서 코드·메시지·MessageId·조치를 뽑습니다.
func trsMessages(b []byte) []string {
	var v struct {
		Error struct {
			Code    string   `json:"code"`
			Message string   `json:"message"`
			Ext     []trsExt `json:"@Message.ExtendedInfo"`
		} `json:"error"`
		Ext []trsExt `json:"@Message.ExtendedInfo"`
	}
	if json.Unmarshal(b, &v) != nil {
		return nil
	}
	var out []string
	if v.Error.Code != "" || v.Error.Message != "" {
		out = append(out, strings.TrimSpace(v.Error.Code+" — "+v.Error.Message))
	}
	for _, e := range append(v.Error.Ext, v.Ext...) {
		s := e.MessageID + " — " + e.Message
		if e.Resolution != "" {
			s += " → 조치: " + e.Resolution
		}
		out = append(out, strings.TrimSpace(s))
	}
	return out
}

// ---- 2. 비인증 ServiceRoot ----

func (h *trsHost) stepRoot() {
	h.log("")
	h.log("[2] 비인증 ServiceRoot (Redfish 서비스가 응답하는지)")
	r := h.req("ServiceRoot", trsReq{method: http.MethodGet, path: "/redfish/v1/"})
	if r.err != nil {
		e := h.clean(r.err.Error())
		h.f.rootErr = e
		if le := strings.ToLower(e); strings.Contains(le, "tls") || strings.Contains(le, "handshake") {
			h.f.tlsErr = e
		}
		return
	}
	h.f.rootStatus = r.status
	if r.status >= 200 && r.status < 300 {
		ns := h.req("ServiceRoot 끝 슬래시 없음 (bios.sh 가 쓰는 경로)", trsReq{method: http.MethodGet, path: "/redfish/v1"})
		if ns.err == nil {
			h.f.rootNoSlash = ns.status
			if ns.hdr != nil {
				h.f.rootNoSlashLoc = h.clean(ns.hdr.Get("Location"))
			}
		}
		h.f.vendor = vendorFromServiceRoot(r.body)
		var root struct {
			Product, Vendor, RedfishVersion string
		}
		_ = json.Unmarshal(r.body, &root)
		h.f.product = root.Product
		h.log("        Product=%s Vendor=%s RedfishVersion=%s 벤더판별=%s", h.clean(root.Product), h.clean(root.Vendor), h.clean(root.RedfishVersion), h.f.vendor)
		return
	}
	if r.status == http.StatusUnauthorized {
		h.log("  (이 BMC 는 비인증 ServiceRoot 조회를 허용하지 않습니다 — 인증 단계에서 계속 확인)")
		return
	}
	if r.status < 400 || r.status >= 500 {
		return
	}
	// 비인증 ServiceRoot 가 막히면: 헤더·UA·경로 변형으로 무엇에 반응하는지 본다.
	h.log("  비인증 ServiceRoot 가 %d — 헤더·경로 변형 비교:", r.status)
	variants := []struct {
		name string
		r    trsReq
	}{
		{"User-Agent=curl", trsReq{method: http.MethodGet, path: "/redfish/v1/", ua: "curl/7.76.1", uaSet: true}},
		{"User-Agent=Mozilla", trsReq{method: http.MethodGet, path: "/redfish/v1/", ua: "Mozilla/5.0", uaSet: true}},
		{"User-Agent 없음", trsReq{method: http.MethodGet, path: "/redfish/v1/", ua: "", uaSet: true}},
		{"Accept·OData-Version 없음", trsReq{method: http.MethodGet, path: "/redfish/v1/", noAccept: true, noOData: true}},
		{"끝 슬래시 없음 (/redfish/v1)", trsReq{method: http.MethodGet, path: "/redfish/v1"}},
	}
	for _, v := range variants {
		vr := h.req(v.name, v.r)
		if vr.err == nil && vr.status != r.status {
			h.f.uaDiff = append(h.f.uaDiff, fmt.Sprintf("%s → %d", v.name, vr.status))
			if h.f.vendor == "" && vr.status < 300 {
				h.f.vendor = vendorFromServiceRoot(vr.body)
			}
		}
	}
}

// ---- 3. 인증 (Basic 1회 + 세션 1회) ----

func (h *trsHost) stepAuth() {
	h.log("")
	h.log("[3] 인증 (계정 잠금 방지: Basic 1회 + 세션 생성 1회, 401 이면 즉시 중단)")
	b := h.req("Basic 인증", trsReq{method: http.MethodGet, path: "/redfish/v1/Systems", auth: "basic", user: h.user, pass: h.pass})
	if b.err == nil {
		h.f.basicStatus = b.status
		h.f.basicOK = b.status >= 200 && b.status < 300
		if b.status == http.StatusUnauthorized {
			h.f.halted = true
			h.log("  → 401: 계정·비밀번호가 맞지 않습니다. 잠금 방지를 위해 더 시도하지 않습니다.")
		}
	} else {
		h.f.halted = true
	}
	if !h.f.halted {
		body, _ := json.Marshal(map[string]string{"UserName": h.user, "Password": h.pass})
		sess := trsReq{method: http.MethodPost, path: "/redfish/v1/SessionService/Sessions", body: body}
		l := h.req("세션 생성", sess)
		if l.err == nil {
			h.f.loginStatus = l.status
			switch {
			case l.status >= 200 && l.status < 300:
				h.onLogin(l)
			case l.status == http.StatusUnauthorized:
				h.f.halted = true
			case l.status >= 500:
				h.f.sessionErr = true
			case l.status >= 400 && l.status != http.StatusForbidden && l.status != http.StatusTooManyRequests:
				// 400/405/406/411/415 ... 형식·협상 문제로 보이면 요청 모양을 바꿔 1회씩만 더 시험한다 (자격증명 거부가 아니므로 잠금 위험 낮음).
				h.sessionVariants(sess, l.status)
			}
		}
	}
	if h.f.basicStatus == 403 && h.f.tokenOK {
		h.f.basicDisabledHint = true
	}
	h.stepFallback()
}

// onLogin 은 세션 생성 성공 응답에서 토큰·세션 URI 를 보관합니다.
func (h *trsHost) onLogin(l trsRes) {
	tok := l.hdr.Get("X-Auth-Token")
	h.addSecret(tok)
	if tok != "" {
		h.token = tok
		h.f.tokenOK = true
	}
	loc := l.hdr.Get("Location")
	if loc == "" {
		var od struct {
			ID string `json:"@odata.id"`
		}
		_ = json.Unmarshal(l.body, &od)
		loc = od.ID
	}
	h.pr.SetSession(loc)
	if tok == "" {
		h.log("  → 세션은 만들어졌지만 X-Auth-Token 헤더가 없습니다")
	}
}

// sessionVariants 는 세션 생성이 4xx(형식·협상 오류)일 때 헤더·Content-Type 을 바꿔 보며 어떤 모양이면 되는지 찾습니다.
func (h *trsHost) sessionVariants(base trsReq, orig int) {
	h.log("  세션 생성이 %d — 요청 모양 변형 비교 (성공하면 그 세션을 이후 단계에 씁니다):", orig)
	type vv struct {
		name string
		mod  func(*trsReq)
	}
	for _, x := range []vv{
		{"Accept·OData-Version 없음", func(r *trsReq) { r.noAccept, r.noOData = true, true }},
		{"Content-Type=application/json;charset=utf-8", func(r *trsReq) { r.contentType = "application/json;charset=utf-8" }},
		{"User-Agent=curl", func(r *trsReq) { r.ua, r.uaSet = "curl/7.76.1", true }},
		{"끝 슬래시 추가", func(r *trsReq) { r.path += "/" }},
	} {
		v := base
		x.mod(&v)
		r := h.req("세션 생성 · "+x.name, v)
		if r.err != nil {
			continue
		}
		if r.status >= 200 && r.status < 300 {
			h.f.loginStatus = r.status
			h.f.sessionVariantOK = x.name
			h.f.variantDiff = append(h.f.variantDiff, fmt.Sprintf("세션 생성: %s → %d", x.name, r.status))
			h.onLogin(r)
			return
		}
		if r.status == http.StatusUnauthorized || r.status == http.StatusForbidden || r.status == http.StatusTooManyRequests {
			h.log("  → %d: 자격증명/호출 제한 응답이라 변형 시험을 멈춥니다.", r.status)
			return
		}
	}
}

// stepFallback: 1순위 계정으로 인증이 안 되면 rf.conf 의 2순위 계정으로 Basic 1회만 비교한다.
func (h *trsHost) stepFallback() {
	if h.f.tokenOK || h.f.basicOK || h.f.halted || h.fb == nil || h.f.vendor == "" {
		return
	}
	u, p, ok := h.fb.lookup(h.f.vendor)
	if !ok {
		return
	}
	h.addSecret(base64.StdEncoding.EncodeToString([]byte(u + ":" + p)))
	for _, x := range passwordForms(p) {
		h.addSecret(x)
	}
	h.f.fbTried = true
	r := h.req("2순위 계정 비교 (rf.conf, Basic 1회)", trsReq{method: http.MethodGet, path: "/redfish/v1/Systems", auth: "basic", user: u, pass: p})
	if r.err == nil {
		h.f.fbStatus = r.status
	}
}

// authed: 증명된 인증 방식으로 요청한다 (토큰 우선).
func (h *trsHost) authed(label, p string) trsRes {
	r := trsReq{method: http.MethodGet, path: p}
	switch {
	case h.f.tokenOK:
		r.auth, r.token = "token", h.token
	case h.f.basicOK:
		r.auth, r.user, r.pass = "basic", h.user, h.pass
	default:
		return trsRes{err: fmt.Errorf("인증 안 됨")}
	}
	return h.req(label, r)
}

// ---- 4. 인증 후 읽기 ----

type trsLink struct {
	ID string `json:"@odata.id"`
}

func (h *trsHost) note403(p string, r trsRes) {
	if r.err != nil || r.status < 400 {
		return
	}
	if r.status == http.StatusForbidden {
		h.f.denied = append(h.f.denied, p)
	}
	if r.status != http.StatusNotFound {
		h.f.hadAuthedReadError = true
		h.f.failed = append(h.f.failed, trsFail{path: p, status: r.status})
	}
}

func firstMember(b []byte) string {
	var v struct{ Members []trsLink }
	if json.Unmarshal(b, &v) == nil && len(v.Members) > 0 {
		return v.Members[0].ID
	}
	return ""
}

func (h *trsHost) stepReads(extra []string) {
	if !h.f.tokenOK && !h.f.basicOK {
		return
	}
	h.log("")
	h.log("[4] 인증 후 읽기 (%s)", map[bool]string{true: "세션 토큰", false: "Basic"}[h.f.tokenOK])
	get := func(label, p string) trsRes {
		r := h.authed(label, p)
		h.note403(p, r)
		return r
	}
	var sysPath, mgrPath string

	if r := get("Systems", "/redfish/v1/Systems"); r.err == nil && r.status < 300 {
		h.f.systemsOK = true
		sysPath = firstMember(r.body)
	}
	if sysPath != "" {
		if r := get("System", sysPath); r.err == nil && r.status < 300 {
			var s struct {
				Manufacturer, Model, BiosVersion, PowerState string
			}
			_ = json.Unmarshal(r.body, &s)
			h.log("        Manufacturer=%s Model=%s BiosVersion=%s PowerState=%s", h.clean(s.Manufacturer), h.clean(s.Model), h.clean(s.BiosVersion), h.clean(s.PowerState))
		}
		biosPath := strings.TrimRight(sysPath, "/") + "/Bios"
		if r := get("Bios", biosPath); r.err == nil && r.status < 300 {
			h.f.biosOK = true
			var bi struct {
				Attributes map[string]json.RawMessage
				Settings   struct {
					Obj trsLink `json:"SettingsObject"`
				} `json:"@Redfish.Settings"`
			}
			_ = json.Unmarshal(r.body, &bi)
			h.log("        BIOS 속성 %d개", len(bi.Attributes))
			if bi.Settings.Obj.ID != "" {
				get("Bios Settings(Pending)", bi.Settings.Obj.ID)
			}
		}
	}
	if r := get("Managers", "/redfish/v1/Managers"); r.err == nil && r.status < 300 {
		mgrPath = firstMember(r.body)
	}
	if mgrPath != "" {
		if r := get("Manager", mgrPath); r.err == nil && r.status < 300 {
			var m struct {
				Model, FirmwareVersion string
				NP                     trsLink `json:"NetworkProtocol"`
			}
			_ = json.Unmarshal(r.body, &m)
			h.log("        Model=%s FirmwareVersion=%s", h.clean(m.Model), h.clean(m.FirmwareVersion))
			if m.NP.ID != "" {
				if nr := get("NetworkProtocol", m.NP.ID); nr.err == nil && nr.status < 300 {
					h.netProtocols(nr.body)
				}
			}
		}
	}
	if r := get("SessionService", "/redfish/v1/SessionService"); r.err == nil && r.status < 300 {
		var s struct {
			ServiceEnabled *bool
			SessionTimeout int
		}
		_ = json.Unmarshal(r.body, &s)
		h.log("        SessionTimeout=%ds ServiceEnabled=%v", s.SessionTimeout, s.ServiceEnabled != nil && *s.ServiceEnabled)
	}
	h.stepAccount(get)
	for _, p := range extra {
		r := get("사용자 지정", p)
		if r.err == nil {
			if h.f.extraStatus == nil {
				h.f.extraStatus = map[string]int{}
			}
			h.f.extraStatus[p] = r.status
		}
	}
	h.stepVariants()
}

func (h *trsHost) netProtocols(b []byte) {
	var m map[string]json.RawMessage
	if json.Unmarshal(b, &m) != nil {
		return
	}
	var parts []string
	for _, k := range []string{"HTTPS", "HTTP", "IPMI", "SSH", "SNMP", "VirtualMedia", "KVMIP"} {
		raw, ok := m[k]
		if !ok {
			continue
		}
		var p struct {
			ProtocolEnabled *bool
			Port            *int
		}
		if json.Unmarshal(raw, &p) != nil || p.ProtocolEnabled == nil {
			continue
		}
		s := k + ":" + map[bool]string{true: "on", false: "off"}[*p.ProtocolEnabled]
		if p.Port != nil && *p.Port != 0 {
			s += fmt.Sprintf("(%d)", *p.Port)
		}
		parts = append(parts, s)
		if !*p.ProtocolEnabled {
			h.f.protoOff = append(h.f.protoOff, k)
		}
	}
	if len(parts) > 0 {
		h.log("        서비스: %s", strings.Join(parts, " "))
	}
}

// stepAccount: 계정 서비스에서 이 계정(bios.conf user)의 상태를 찾는다.
func (h *trsHost) stepAccount(get func(label, p string) trsRes) {
	r := get("AccountService", "/redfish/v1/AccountService")
	if r.err != nil || r.status >= 300 {
		return
	}
	var as struct {
		Accounts                 trsLink `json:"Accounts"`
		AccountLockoutThreshold  *int
		AccountLockoutDuration   *int
		AccountLockoutCounterRst *int `json:"AccountLockoutCounterResetAfter"`
	}
	_ = json.Unmarshal(r.body, &as)
	if as.AccountLockoutThreshold != nil {
		h.log("        잠금 정책: 임계=%d회 잠금시간=%v초", *as.AccountLockoutThreshold, ptrInt(as.AccountLockoutDuration))
	}
	if as.Accounts.ID == "" {
		return
	}
	cr := get("Accounts", as.Accounts.ID)
	if cr.err != nil || cr.status >= 300 {
		return
	}
	var coll struct{ Members []trsLink }
	_ = json.Unmarshal(cr.body, &coll)
	for i, m := range coll.Members {
		if i >= trsMaxAcct {
			h.log("        (계정 %d개 이상 — 앞 %d개만 확인)", len(coll.Members), trsMaxAcct)
			break
		}
		mr := get("계정 멤버", m.ID)
		if mr.err != nil || mr.status >= 300 {
			continue
		}
		var a struct {
			UserName           string
			RoleID             string `json:"RoleId"`
			Enabled            *bool
			Locked             *bool
			PasswordChangeReq  *bool `json:"PasswordChangeRequired"`
			PasswordExpiration string
			Oem                json.RawMessage
		}
		_ = json.Unmarshal(mr.body, &a)
		if !strings.EqualFold(a.UserName, h.user) {
			continue
		}
		h.f.acctFound = true
		h.f.acctEnabled, h.f.acctLocked, h.f.acctPwChange, h.f.acctRole = a.Enabled, a.Locked, a.PasswordChangeReq, a.RoleID
		h.log("        내 계정(%s): Role=%s Enabled=%s Locked=%s PasswordChangeRequired=%s PasswordExpiration=%s",
			h.clean(a.UserName), h.clean(a.RoleID), ptrBool(a.Enabled), ptrBool(a.Locked), ptrBool(a.PasswordChangeReq), h.clean(a.PasswordExpiration))
		return
	}
	h.log("        내 계정(%s)을 계정 목록에서 찾지 못했습니다 (목록 권한 부족이거나 다른 이름)", h.user)
}

func ptrInt(p *int) interface{} {
	if p == nil {
		return "?"
	}
	return *p
}

func ptrBool(p *bool) string {
	if p == nil {
		return "?"
	}
	if *p {
		return "true"
	}
	return "false"
}

// ---- 6. bios.sh 와 같은 방식 재현 ----

func trsErrText(err error) string {
	var rf *RFError
	if errors.As(err, &rf) {
		return fmt.Sprintf("%s HTTP %d: %s", rf.Status, rf.HTTP, rf.Detail)
	}
	return err.Error()
}

// stepReplay 는 bios.sh(check)가 쓰는 실제 Client 로 같은 순서의 조회를 해 봅니다:
// 로그인 → ServiceRoot(/redfish/v1) → Systems → System → Bios → Settings → 속성 레지스트리 파일 → 레지스트리 JSON.
// trs 의 다른 단계는 진단용 Prober 를 쓰므로(헤더·연결 재사용·경로 형태가 다름) 이 단계가 "도구가 실제로 하는 요청" 입니다.
func (h *trsHost) stepReplay() {
	if !h.f.tokenOK && !h.f.basicOK {
		return
	}
	h.f.replayRan = true
	h.log("")
	h.log("[6] bios.sh 와 같은 방식 재현 (실제 Client: 기본 User-Agent, 연결 재사용, /redfish/v1 경로)")
	c := NewClient(ClientOpts{BaseURL: h.t.BaseURL(), User: h.user, Pass: h.pass, Insecure: h.conf.Insecure, Timeout: h.conf.Timeout, Retries: 0, Mode: ModeReadOnly})
	defer c.Close()
	fail := func(step string, err error) {
		h.f.replayErr = step + ": " + h.clean(trsErrText(err))
		h.log("  ✘ %s → %s", step, h.f.replayErr[len(step)+2:])
	}
	info, err := detectSystem(c)
	if err != nil {
		fail("시스템 판별(detectSystem)", err)
		h.replayProbes()
		return
	}
	h.log("  ✔ 시스템 판별: Vendor=%s Model=%s BiosVersion=%s", h.clean(info.Vendor), h.clean(info.Model), h.clean(info.BiosVersion))
	h.log("    Bios=%s Settings=%s(%s) Registry=%s", h.clean(info.BiosPath), h.clean(info.SettingsPath), info.SettingsFrom, h.clean(info.RegistryPath))
	for _, n := range info.Notes {
		h.log("    참고: %s", h.clean(n))
	}
	for _, g := range []struct{ name, p string }{{"Bios 속성", info.BiosPath}, {"Settings", info.SettingsPath}, {"속성 레지스트리", info.RegistryPath}} {
		if g.p == "" {
			continue
		}
		b, err := c.GetRaw(g.p)
		if err != nil {
			fail(g.name+" 조회", err)
			h.replayProbes()
			return
		}
		h.log("  ✔ %s 조회: %d 바이트", g.name, len(b))
	}
	st := c.Stats()
	h.log("  요청 수: GET %d / 세션 %d / 인증 %s", st.Gets, st.Sessions, st.AuthMode)
}

// replayProbes: 재현이 실패했을 때, Prober 로 UA·연결 재사용 차이를 가려 봅니다.
func (h *trsHost) replayProbes() {
	h.log("  재현 실패 — 같은 요청을 UA·연결 재사용을 바꿔 확인:")
	mk := func(p string, mod func(*trsReq)) trsReq {
		r := trsReq{method: http.MethodGet, path: p}
		if h.f.tokenOK {
			r.auth, r.token = "token", h.token
		} else {
			r.auth, r.user, r.pass = "basic", h.user, h.pass
		}
		if mod != nil {
			mod(&r)
		}
		return r
	}
	g := h.req("기본 User-Agent=Go-http-client/1.1", mk("/redfish/v1/Systems", func(r *trsReq) { r.ua, r.uaSet = "Go-http-client/1.1", true }))
	if g.err == nil {
		h.f.replayUAGo = g.status
	}
	ka := NewProber(h.hostport, h.conf.Insecure, h.conf.Timeout)
	defer ka.Close()
	ka.EnableKeepAlive()
	for i := 1; i <= 2; i++ {
		rq := mk("/redfish/v1/Systems", nil)
		pr := ka.Send(ProbeReq{Method: rq.method, Path: rq.path, Auth: rq.auth, User: rq.user, Pass: rq.pass, Token: rq.token})
		if pr.Err != nil {
			h.f.replayKA = append(h.f.replayKA, 0)
			h.log("  [연결 재사용 %d회째] 실패: %s", i, h.clean(pr.Err.Error()))
			continue
		}
		h.f.replayKA = append(h.f.replayKA, pr.Status)
		h.log("  [연결 재사용 %d회째] GET /redfish/v1/Systems → %d", i, pr.Status)
	}
}

// ---- 5. 4xx 경로의 헤더·인증 변형 비교 ----

func (h *trsHost) stepVariants() {
	if len(h.f.failed) == 0 {
		return
	}
	h.log("")
	h.log("[5] 인증 후 4xx 가 난 경로를 변형해 비교 (인증은 이미 통과한 상태)")
	seen := map[string]bool{}
	done := 0
	for _, fl := range h.f.failed {
		p := fl.path
		if seen[p] || done >= trsMaxDeny {
			continue
		}
		seen[p] = true
		done++
		base := trsReq{method: http.MethodGet, path: p}
		cur := base
		if h.f.tokenOK {
			cur.auth, cur.token = "token", h.token
		} else {
			cur.auth, cur.user, cur.pass = "basic", h.user, h.pass
		}
		type vv struct {
			name string
			r    trsReq
		}
		vs := []vv{}
		v := cur
		v.noOData, v.noAccept = true, true
		vs = append(vs, vv{"Accept·OData-Version 없음", v})
		v = cur
		v.ua, v.uaSet = "curl/7.76.1", true
		vs = append(vs, vv{"User-Agent=curl", v})
		v = cur
		if strings.HasSuffix(p, "/") {
			v.path = strings.TrimRight(p, "/")
		} else {
			v.path = p + "/"
		}
		vs = append(vs, vv{"끝 슬래시 토글", v})
		if h.f.tokenOK && h.f.basicOK {
			v = base
			v.auth, v.user, v.pass = "basic", h.user, h.pass
			vs = append(vs, vv{"Basic 으로 (원래 토큰)", v})
		}
		if fl.status == http.StatusMethodNotAllowed {
			v = cur
			v.method = http.MethodOptions
			vs = append(vs, vv{"OPTIONS (허용 메서드 확인)", v})
		}
		for _, x := range vs {
			r := h.req(p+" · "+x.name, x.r)
			if x.r.method == http.MethodOptions {
				if a := r.hdr.Get("Allow"); a != "" {
					h.f.variantDiff = append(h.f.variantDiff, fmt.Sprintf("%s: 허용 메서드 Allow=%s", p, a))
				}
				continue
			}
			if r.err == nil && r.status != fl.status {
				h.f.variantDiff = append(h.f.variantDiff, fmt.Sprintf("%s: %s → %d (원래 %d)", p, x.name, r.status, fl.status))
			}
		}
	}
}

func (h *trsHost) stepLogout() {
	if h.pr.Session() == "" {
		return
	}
	h.log("")
	h.log("[정리] 이번 실행이 만든 세션 삭제")
	h.req("세션 삭제", trsReq{method: http.MethodDelete, path: h.pr.Session(), auth: "token", token: h.token})
}

// ---- 진단 규칙 ----

func (f *trsFacts) hasMsg(subs ...string) bool {
	for _, m := range f.msgs {
		for _, s := range subs {
			if strings.Contains(m, s) {
				return true
			}
		}
	}
	return false
}

// trsDiagnose 는 관찰 결과에서 원인 후보를 만듭니다 (확정이 아니라 가능성이 높은 순서).
func trsDiagnose(f *trsFacts) []string {
	var out []string
	add := func(s string) { out = append(out, s) }
	switch {
	case f.tcpErr != "":
		add("● 네트워크: TCP 연결이 안 됩니다 (" + f.tcpErr + ") — BMC IP·관리망 라우팅·방화벽(443)·BMC 의 HTTPS 서비스 사용 여부를 확인하십시오.")
		return out
	case f.tlsErr != "":
		add("● TLS: 핸드셰이크 실패 (" + f.tlsErr + ") — BMC 의 TLS 버전/암호 정책(보안 모드 TLS1.2 이상, 인증서 갱신 중)을 확인하십시오.")
		return out
	case f.rootStatus == 0:
		add("● ServiceRoot 응답이 없습니다 (" + f.rootErr + ") — 타임아웃이면 BMC 부하/세션 포화, 연결 거부면 웹 서비스 비활성입니다.")
		return out
	}
	if f.rootStatus == 403 {
		if len(f.uaDiff) > 0 {
			add("● 비인증 ServiceRoot 가 막히지만 요청 모양을 바꾸면 달라집니다 (" + strings.Join(f.uaDiff, "; ") + ") — 헤더(User-Agent/Accept/OData-Version)·경로 형태에 반응하는 접근 필터입니다. 도구의 요청 헤더를 해당 값에 맞추면 됩니다.")
		} else {
			add("● 비인증 ServiceRoot 부터 403 이고 변형에도 같습니다 — 계정 문제가 아니라 접속 자체가 막힌 것입니다: BMC 의 IP 접근 제한(IP Blocking/Allow list), 관리 PC IP 대역 허용 여부, 중간 방화벽·프록시를 확인하십시오 (Lenovo XCC: 네트워크 > IP 차단 / 보안 설정).")
		}
	}
	if f.halted {
		add("● 401: 계정/비밀번호 불일치입니다. 잠금 방지를 위해 중단했습니다 — pass.enc 가 이 BMC 의 비밀번호와 같은지, 계정이 존재하는지 확인한 뒤 다시 실행하십시오.")
	}
	if f.hasMsg("passwordchangerequired", "password change", "password expired", "passwordexpired") || (f.acctPwChange != nil && *f.acctPwChange) {
		add("● 비밀번호 변경 필요: 이 계정은 최초 로그인/만료로 비밀번호 변경이 요구되어 읽기·쓰기가 403 으로 막힙니다 — BMC 웹/IPMI 에서 비밀번호를 바꾸거나 `bin/biostool account change` 를 사용하십시오.")
	}
	if f.acctLocked != nil && *f.acctLocked || f.hasMsg("accountlocked", "account is locked", "locked out", "lockout") {
		add("● 계정 잠김: 로그인 실패 누적으로 잠금되었습니다 — 잠금 시간이 지나길 기다리거나 BMC 에서 해제한 뒤 재시도하십시오.")
	}
	if f.acctEnabled != nil && !*f.acctEnabled {
		add("● 계정 비활성: 이 계정의 Enabled=false 입니다.")
	}
	if f.hasMsg("insufficientprivilege", "insufficient privilege", "privilege") && !f.hasMsg("passwordchangerequired") {
		add("● 권한 부족: 계정 Role(" + f.acctRole + ")로는 해당 리소스에 접근할 수 없습니다 — Administrator(또는 Configure 권한) 역할이 필요합니다.")
	} else if f.acctFound && f.acctRole != "" && !strings.EqualFold(f.acctRole, "Administrator") && len(f.denied) > 0 {
		add("● 계정 Role 이 " + f.acctRole + " 입니다 — Administrator 가 아니라 일부 리소스가 403 일 수 있습니다.")
	}
	if f.basicDisabledHint {
		add("● Basic 인증만 403 이고 세션(토큰) 인증은 됩니다 — BMC 가 Basic 인증을 막은 상태입니다. 세션 인증을 쓰면 됩니다 (이 도구의 기본 방식). Basic 만 쓰는 다른 도구가 있다면 그쪽이 막힌 원인입니다.")
	}
	if f.loginStatus == 403 && !f.tokenOK {
		if f.fbTried && f.fbStatus >= 200 && f.fbStatus < 300 {
			add("● 2순위 계정(rf.conf)으로는 접속됩니다 — 1순위 계정(bios.conf user)의 문제입니다 (계정 없음/Role/비밀번호 변경 필요/잠김). `bash account_create.sh` 로 1순위 계정을 만들 수 있습니다.")
		} else if f.basicStatus == 403 {
			add("● 세션 생성과 Basic 이 모두 403 — 계정 상태(비밀번호 변경 필요·권한·잠김)이거나 접속 IP 제한입니다. 위 [3] 의 오류 MessageId 를 확인하십시오.")
		}
	}
	if f.sessionErr || f.loginStatus == 503 || f.hasMsg("sessionlimit", "too many", "maximum number", "resourceexhaust", "serviceunavailable") {
		add("● 세션 한도/서비스 일시 불가: BMC 에 열린 세션이 가득 찼거나(이전 실행이 정리하지 못한 세션) 서비스가 바쁩니다 — BMC 웹의 세션 목록에서 정리하거나 잠시 후 재시도하십시오.")
	}
	if len(f.protoOff) > 0 {
		add("● BMC 서비스 설정에서 꺼진 항목: " + strings.Join(f.protoOff, ",") + " — HTTPS 가 꺼져 있지 않은데도 403 이면 웹/Redfish 서비스 제한 설정을 확인하십시오.")
	}
	if len(f.denied) > 0 {
		add("● 인증은 통과했지만 일부 경로만 403: " + strings.Join(f.denied, ", ") + " — 해당 리소스의 권한(Role) 또는 BMC 에서 그 기능이 비활성인 경우입니다.")
	}
	out = append(out, trs4xxNotes(f)...)
	if len(f.variantDiff) > 0 {
		add("● 변형 비교에서 결과가 달라진 것: " + strings.Join(f.variantDiff, "; ") + " — 요청 헤더/인증 방식/경로 형태/Content-Type 에 따라 결과가 갈립니다 (도구가 그 방식을 쓰도록 수정 대상).")
	} else if len(f.failed) > 0 || (len(f.uaDiff) == 0 && f.rootStatus >= 400 && f.rootStatus < 500 && f.rootStatus != 401) {
		add("  (헤더·인증 방식·슬래시 변형 모두 같은 결과 — 요청 모양 문제가 아니라 계정 권한/BMC 정책/리소스 상태입니다)")
	}
	if f.rootNoSlash >= 300 && f.rootNoSlash < 400 {
		if f.replayRan && f.replayErr == "" {
			add(fmt.Sprintf("  (슬래시 없는 /redfish/v1 은 %d 리다이렉트(Location=%s) — 도구가 같은 BMC 안의 리다이렉트를 따라가므로 정상 동작합니다)", f.rootNoSlash, f.rootNoSlashLoc))
		} else {
			add(fmt.Sprintf("● 슬래시 없는 /redfish/v1 이 %d 리다이렉트(Location=%s) — 도구가 리다이렉트를 따라가지 못하면 bios.sh 에서 HTTP_ERROR 가 됩니다.", f.rootNoSlash, f.rootNoSlashLoc))
		}
	} else if f.rootNoSlash >= 400 && f.rootNoSlash != 401 {
		add(fmt.Sprintf("● 슬래시 없는 /redfish/v1 이 %d — bios.sh 가 쓰는 경로 형태가 이 BMC 에서 거부됩니다. 도구가 /redfish/v1/ 로 요청하도록 수정할 대상입니다.", f.rootNoSlash))
	}
	if f.replayRan {
		switch {
		case f.replayErr != "":
			add("● bios.sh 와 같은 방식 요청이 실패했습니다 — " + f.replayErr)
			lerr := strings.ToLower(f.replayErr)
			switch {
			case strings.Contains(lerr, "8mb"):
				add("  원인: 응답 본문이 도구 상한(8MB)을 넘었습니다 (보통 속성 레지스트리 JSON). 도구의 본문 상한을 올리거나 레지스트리를 건너뛰도록 수정할 대상입니다.")
			case strings.Contains(lerr, "리다이렉트"):
				add("  원인: BMC 가 리다이렉트로 응답합니다. 도구가 따라가지 않아 실패합니다 — Location 대상 경로로 요청하도록 수정할 대상입니다.")
			case f.replayUAGo >= 400 && f.replayUAGo != 401:
				add(fmt.Sprintf("  원인 후보: 도구의 기본 User-Agent(Go-http-client/1.1)가 %d 로 거부됩니다 (진단용 UA 로는 통과). 도구가 고유 User-Agent 를 보내도록 수정할 대상입니다.", f.replayUAGo))
			case len(f.replayKA) == 2 && f.replayKA[0] < 400 && f.replayKA[0] > 0 && (f.replayKA[1] == 0 || f.replayKA[1] >= 400):
				add("  원인 후보: 같은 연결에서 두 번째 요청이 실패합니다 — 연결 재사용(keep-alive)을 BMC 가 받지 않습니다. 도구가 요청마다 새 연결을 쓰도록 수정할 대상입니다.")
			default:
				add("  UA·연결 재사용·경로 형태로는 설명되지 않습니다 — 위 [6] 의 실패 줄(상태 코드·내용)을 알려 주십시오.")
			}
		}
	}
	for p, st := range f.extraStatus {
		add(fmt.Sprintf("  사용자 지정 경로 %s → %d", p, st))
	}
	if len(out) == 0 {
		if f.tokenOK || f.basicOK {
			if f.hadAuthedReadError {
				add("● 접속·인증은 정상이나 일부 읽기에서 4xx 가 있었습니다 — 위 [4] 의 해당 줄과 상태 코드 설명을 확인하십시오.")
			} else {
				add("● 접속·인증·기본 읽기 모두 정상입니다 (이 시점에는 403 재현 안 됨). 403 이 특정 작업(설정 PATCH 등)에서만 난다면 `-path` 로 그 경로를 지정하거나 해당 단계의 오류 메시지를 알려 주십시오.")
			}
		} else {
			add("● 원인을 특정하지 못했습니다 — 위 [3] 의 상태 코드와 오류 MessageId 를 알려 주십시오.")
		}
	}
	if f.replayRan && f.replayErr == "" {
		add("● bios.sh 와 같은 방식(실제 Client)으로 시스템 판별·Bios·Settings·레지스트리 조회까지 정상입니다. 그래도 bios.sh 에서만 HTTP_ERROR 면: ① 동시 접속 수(bios.conf concurrency)를 1~5 로 낮춰 보기 ② user.txt 의 대상 표기(IP/이름/포트)가 같은지 ③ bios.sh 비고의 전체 문구(HTTP 코드·내용)를 알려 주십시오.")
	}
	return out
}

// trs4xx 는 4xx 상태 코드별 일반 설명과 점검 방법입니다 (BMC 가 준 오류 MessageId 가 있으면 그것이 우선).
var trs4xx = map[int][2]string{
	400: {"잘못된 요청", "요청 형식·헤더·본문을 BMC 가 해석하지 못함 — Accept/OData-Version/Content-Type/Host 헤더, JSON 본문, 너무 긴 헤더를 의심. 변형 비교에서 통과하는 모양이 있는지 확인"},
	401: {"인증 실패", "인증 후에도 401 이면 세션 토큰 만료·거부 (SessionTimeout 이 짧거나 세션이 정리됨)"},
	402: {"결제 필요", "BMC 가 라이선스 기능(Dell iDRAC Enterprise, HPE iLO Advanced, Lenovo XCC Enterprise 등)을 요구하는 것일 수 있음 — 라이선스 확인"},
	404: {"리소스 없음", "해당 모델/펌웨어에 없는 경로이거나 ID(Systems/1 등)가 다름 — 일부 선택 리소스(AccountService 등)의 404 는 정상일 수 있음. /redfish/v1 자체가 404 면 Redfish 미지원·웹서비스 비활성"},
	405: {"허용되지 않은 메서드", "그 경로가 해당 메서드(GET/POST)를 지원하지 않음 — 응답 Allow 헤더·OPTIONS 결과 확인. 세션 생성이 405 면 세션 미지원이라 Basic 인증으로 폴백해야 함"},
	406: {"Accept 불일치", "BMC 가 Accept 헤더의 형식을 제공하지 못함 — Accept 를 빼거나 application/json 으로 맞춤(변형 비교 참고)"},
	407: {"프록시 인증 필요", "중간 프록시가 응답 중 — 관리망은 프록시를 거치지 않게 해야 함 (이 도구는 환경 프록시를 쓰지 않음)"},
	408: {"요청 시간 초과", "BMC 가 요청 수신을 기다리다 끊음 — 네트워크 지연/BMC 과부하, timeout 값 상향"},
	409: {"충돌", "세션 한도 초과·자원 사용 중·이미 존재 — 열린 세션 정리, 진행 중인 작업(Job/Pending) 확인"},
	410: {"영구 삭제됨", "예전 경로가 제거됨 — 펌웨어 버전에 맞는 경로 사용"},
	411: {"Content-Length 필요", "POST 본문 길이가 필요하다는 응답 — 청크 전송을 거부하는 BMC"},
	412: {"사전조건 실패", "ETag/If-Match 불일치 — 대상 리소스가 바뀜, 다시 읽고 시도"},
	413: {"본문이 너무 큼", "요청 본문 크기 제한 초과"},
	414: {"URI 가 너무 김", "경로/쿼리 길이 제한 초과"},
	415: {"지원하지 않는 Content-Type", "POST/PATCH 의 Content-Type 을 BMC 가 거부 — application/json 계열로 맞춤(변형 비교 참고)"},
	416: {"범위 오류", "Range 요청 범위가 잘못됨"},
	417: {"Expect 실패", "Expect: 100-continue 를 BMC 가 지원하지 않음 — 클라이언트가 Expect 헤더를 보내지 않게"},
	421: {"잘못 연결됨", "TLS SNI/Host 와 서버 이름이 맞지 않음 — IP 대신 호스트 이름 접속 시 발생 가능"},
	422: {"처리 불가 본문", "JSON 은 맞지만 값이 BMC 규칙에 맞지 않음 — 오류 MessageId(PropertyValueNotInList 등) 확인"},
	423: {"잠김", "리소스/계정이 잠김 — 계정 잠금 해제 또는 진행 중인 작업 완료 대기"},
	426: {"업그레이드 필요", "더 높은 TLS/프로토콜 요구 — BMC 보안 정책(TLS1.2 이상)과 클라이언트 TLS 버전 확인"},
	428: {"사전조건 필요", "If-Match(ETag) 헤더가 필요한 변경 요청"},
	429: {"요청 과다", "BMC 가 호출 빈도를 제한 — Retry-After 만큼 기다리고 동시 접속·호출 간격을 줄임"},
	431: {"헤더 필드가 너무 큼", "요청 헤더(쿠키/토큰/UA)가 너무 큼 — 불필요한 헤더 제거"},
	451: {"법적 사유로 차단", "정책에 의해 차단됨 — 보안 장비/BMC 정책 확인"},
}

// trs4xxNotes 는 이번 실행에서 본 4xx 코드마다 설명 한 줄(+ 해당 경로)을 만듭니다.
// 403 은 개별 규칙으로 설명하고, 401 은 인증된 상태에서 난 경우만 여기서 덧붙입니다.
func trs4xxNotes(f *trsFacts) []string {
	var codes []int
	for c := range f.codes {
		codes = append(codes, c)
	}
	sort.Ints(codes)
	var out []string
	for _, c := range codes {
		if c == 403 || (c == 401 && !(f.tokenOK || f.basicOK)) {
			continue
		}
		info, ok := trs4xx[c]
		if !ok {
			info = [2]string{"기타 4xx", "표준 설명이 없는 상태 코드 — 응답 본문의 MessageId 를 확인"}
		}
		out = append(out, fmt.Sprintf("● HTTP %d %s: %s  [%s]", c, info[0], info[1], strings.Join(f.codes[c], ", ")))
	}
	return out
}
