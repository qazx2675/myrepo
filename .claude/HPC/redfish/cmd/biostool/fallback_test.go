package main

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"biostool/internal/mockbmc"
)

// fallback.go 시험 (2순위 계정). 실제 BMC 는 쓰지 않고 internal/mockbmc 만 쓴다.
// mock 의 계정을 2순위 계정(fbUser/fbPass)으로 두면 1순위(testUser/testPass)는 401 이 된다.

const (
	fbUser = "fb-admin-Q7"
	fbPass = `Fb#Pa/ss"9z<>`
)

const sessPathFB = "/redfish/v1/SessionService/Sessions"

// fbConf 는 vendor 의 2순위 계정 rf.conf 본문입니다.
func fbConf(vendor string) string {
	return vendor + "_user=" + fbUser + "\n" + vendor + "_pass=" + fbPass + "\n"
}

func fbCreds(t *testing.T, text string) *fallbackCreds {
	t.Helper()
	fb, err := parseRFConf(text, "rf.conf")
	must(t, err)
	return fb
}

// startFB 는 계정이 2순위 계정(fbUser/fbPass)인 mock 입니다. open 이면 ServiceRoot 를 인증 없이 응답한다.
func startFB(t *testing.T, tree string, open bool) *mockbmc.Server {
	t.Helper()
	return startMockTree(t, tree, func(o *mockbmc.Options) {
		o.User, o.Pass, o.OpenServiceRoot = fbUser, fbPass, open
	})
}

// probes 는 벤더 식별용 비인증 ServiceRoot GET(/redfish/v1/, 끝 슬래시) 횟수입니다.
// 점검의 인증된 ServiceRoot 조회는 끝 슬래시 없는 /redfish/v1 이라 섞이지 않는다.
func probes(s *mockbmc.Server) int {
	n := 0
	for _, c := range s.Calls() {
		if c.Method == "GET" && c.Path == "/redfish/v1/" {
			n++
		}
	}
	return n
}

func reportText(run *checkRun) string {
	var b bytes.Buffer
	writeReport(&b, run, reportOpts{ListMax: 100, FailMax: 200})
	return b.String()
}

// assertNoFBSecrets 는 텍스트와 결과 폴더 파일 어디에도 1순위·2순위 비밀번호, 2순위 계정명, 토큰이 없는지 봅니다.
func assertNoFBSecrets(t *testing.T, dir string, texts ...string) {
	t.Helper()
	if dir != "" {
		must(t, filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
			if err == nil && !d.IsDir() {
				b, rerr := os.ReadFile(p)
				must(t, rerr)
				texts = append(texts, string(b))
			}
			return err
		}))
	}
	for i, text := range texts {
		for _, secret := range []string{testPass, "S3cr3t", fbPass, "Fb#Pa", fbUser, "X-Auth-Token", "Authorization", "Basic "} {
			if strings.Contains(text, secret) {
				t.Errorf("출력/파일 %d 에 %q 가 있음", i, secret)
			}
		}
	}
}

// ① rf.conf 파서: 주석·빈 줄·CRLF·BOM·벤더 대소문자·빈 user·알 수 없는 키, 값은 오류 메시지에 없음.
func TestParseRFConf(t *testing.T) {
	text := "\xef\xbb\xbf# 2순위 계정\r\n\r\nDELL_USER = root\r\nDell_Pass=" + fbPass + "\r\n" +
		"hpe_user=\nhpe_pass=무시됨\n" + // user 가 비면 예비 계정 없음
		"  # 들여쓴 주석\nlenovo_user=USERID\nlenovo_pass=a=b=c\n" + // 값의 = 는 그대로
		"supermicro_user=ADMIN\nsupermicro_pass=x\nsupermicro_pass=y\n" // 마지막 값
	fb := fbCreds(t, text)
	if got := strings.Join(fb.vendors(), ","); got != "dell,lenovo,supermicro" {
		t.Errorf("vendors = %s", got)
	}
	for _, c := range []struct{ v, user, pass string }{
		{"dell", "root", fbPass}, {"lenovo", "USERID", "a=b=c"}, {"supermicro", "ADMIN", "y"},
	} {
		if u, p, ok := fb.lookup(c.v); !ok || u != c.user || p != c.pass {
			t.Errorf("lookup(%s) = %q %q %v", c.v, u, p, ok)
		}
	}
	for _, v := range []string{"hpe", "cisco", "Dell", ""} {
		if _, _, ok := fb.lookup(v); ok {
			t.Errorf("lookup(%q) 는 없어야 함", v)
		}
	}
	// fmt 로 통째로 찍어도 계정이 나오지 않는다
	for _, verb := range []string{"%v", "%+v", "%#v", "%s"} {
		if s := fmt.Sprintf(verb, fb); strings.Contains(s, fbPass) || strings.Contains(s, "USERID") {
			t.Errorf("%s 출력에 계정이 보임: %s", verb, s)
		}
	}
	if (*fallbackCreds)(nil).vendors() != nil {
		t.Error("nil 의 vendors 는 비어야 함")
	}

	empty := fbCreds(t, "# 빈 파일\n\n")
	if len(empty.vendors()) != 0 {
		t.Errorf("빈 rf.conf vendors = %v", empty.vendors())
	}

	for _, c := range []struct{ name, text, want string }{
		{"알 수 없는 벤더", "ibm_user=root\n", `알 수 없는 키 "ibm_user"`},
		{"알 수 없는 필드", "dell_password=" + fbPass + "\n", `알 수 없는 키 "dell_password"`},
		{"벤더만", "dell=" + fbPass + "\n", `알 수 없는 키 "dell"`},
		{"= 없음", "dell_user root\n", "key=value"},
		{"pass 없음", "cisco_user=" + fbUser + "\n", "cisco_pass 가 비어"},
		{"pass 빈 값", "cisco_user=" + fbUser + "\ncisco_pass=\n", "cisco_pass 가 비어"},
	} {
		_, err := parseRFConf(c.text, "rf.conf")
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: 오류 %v, 기대 %q 포함", c.name, err, c.want)
			continue
		}
		if strings.Contains(err.Error(), fbPass) || strings.Contains(err.Error(), fbUser) {
			t.Errorf("%s: 오류 메시지에 값이 있음: %v", c.name, err)
		}
	}
}

func TestLoadRFConfFile(t *testing.T) {
	dir := t.TempDir()
	// 없으면 기능 꺼짐 (오류 아님)
	fb, warn, err := loadRFConf(filepath.Join(dir, "없음.conf"))
	if fb != nil || warn != "" || err != nil {
		t.Errorf("없는 파일: %v %q %v", fb, warn, err)
	}
	// 폴더·형식 오류는 오류
	if _, _, err := loadRFConf(dir); err == nil {
		t.Error("폴더는 오류여야 함")
	}
	bad := filepath.Join(dir, "bad.conf")
	must(t, os.WriteFile(bad, []byte("dell_user="+fbUser+"\nfoo="+fbPass+"\n"), 0o600))
	if _, _, err := loadRFConf(bad); err == nil || strings.Contains(err.Error(), fbPass) {
		t.Errorf("형식 오류: %v", err)
	}
	// 정상 + 권한 경고
	p := filepath.Join(dir, "rf.conf")
	must(t, os.WriteFile(p, []byte(fbConf("dell")), 0o600))
	must(t, os.Chmod(p, 0o600))
	fb, warn, err = loadRFConf(p)
	if err != nil || fb == nil || warn != "" {
		t.Fatalf("0600: %v %q %v", fb, warn, err)
	}
	if runtime.GOOS != "windows" {
		must(t, os.Chmod(p, 0o644))
		fb, warn, err = loadRFConf(p)
		if err != nil || fb == nil || !strings.Contains(warn, "그룹/기타") || strings.Contains(warn, fbPass) || strings.Contains(warn, fbUser) {
			t.Errorf("0644 경고: %v %q %v", fb, warn, err)
		}
	}
}

func TestRFConfPath(t *testing.T) {
	t.Setenv(envRFConf, "")
	if p, ex := rfConfPath(""); p != defaultRFConf || ex {
		t.Errorf("기본값 = %s %v", p, ex)
	}
	t.Setenv(envRFConf, "/env/rf.conf")
	if p, ex := rfConfPath(""); p != "/env/rf.conf" || !ex {
		t.Errorf("환경변수 = %s %v", p, ex)
	}
	if p, ex := rfConfPath("/flag/rf.conf"); p != "/flag/rf.conf" || !ex {
		t.Errorf("플래그 우선 = %s %v", p, ex)
	}
}

func TestVendorFromServiceRoot(t *testing.T) {
	for _, c := range []struct{ body, want string }{
		{`{"Vendor":"Dell"}`, "dell"},
		{`{"Vendor":"Dell Inc."}`, "dell"},
		{`{"Vendor":"HPE"}`, "hpe"},
		{`{"Vendor":"Lenovo"}`, "lenovo"},
		{`{"Vendor":"Supermicro"}`, "supermicro"},
		{`{"Vendor":"Cisco Systems Inc."}`, "cisco"},
		{`{"Oem":{"Hpe":{}}}`, "hpe"},
		{`{"Oem":{"Hp":{}}}`, "hpe"}, // iLO4
		{`{"Vendor":"AMI","Oem":{"Supermicro":{}}}`, "supermicro"},
		{`{"Oem":{"Dell":{},"MockData":true}}`, "dell"},
		{`{"Oem":{"Dell":{},"Hpe":{}}}`, ""}, // 여러 벤더 → 판단하지 않음
		{`{"Oem":{"MockData":true},"Name":"Root Service"}`, ""},
		{`{"Product":"Integrated Dell Remote Access Controller"}`, "dell"},
		{`{"Vendor":"Unknown","Name":"Root Service"}`, ""},
		{`{}`, ""},
		{`not json`, ""},
	} {
		if got := vendorFromServiceRoot([]byte(c.body)); got != c.want {
			t.Errorf("%s → %q, 기대 %q", c.body, got, c.want)
		}
	}
}

// ② 1순위 성공: 예비 계정을 조회하지 않고(accts 호출 0) 비인증 ServiceRoot 도 읽지 않으며 로그인 1회.
func TestFallbackPrimaryOKNoLookup(t *testing.T) {
	s := startMockTree(t, "dell-r660", func(o *mockbmc.Options) { o.OpenServiceRoot = true })
	lookups := 0
	m := map[string]fallbackAcct{"dell": {user: fbUser, pass: fbPass}}
	rc := checkRCFor(t, 1, "", s.URL())
	rc.Fallback = &fallbackCreds{accts: func() map[string]fallbackAcct { lookups++; return m }}
	run := runCheckT(t, rc, "")
	h := run.Hosts[0]
	if h.Status != "" || h.CredSource != credPrimary {
		t.Fatalf("Status=%q CredSource=%q", h.Status, h.CredSource)
	}
	if lookups != 0 {
		t.Errorf("1순위 성공인데 예비 계정을 %d번 조회함", lookups)
	}
	if probes(s) != 0 {
		t.Errorf("비인증 ServiceRoot 조회 %d회, 기대 0", probes(s))
	}
	assertOneSession(t, s)
	if _, err := os.Stat(filepath.Join(run.Dir, fallbackLoginFile)); !os.IsNotExist(err) {
		t.Errorf("2순위 접속이 없는데 %s 가 있음", fallbackLoginFile)
	}
	if info := readFileT(t, filepath.Join(run.Dir, "run_info.txt")); !strings.Contains(info, "2순위 계정(rf.conf)으로 접속: 0") {
		t.Errorf("run_info 건수 줄 없음:\n%s", info)
	}
}

func readFileT(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	must(t, err)
	return string(b)
}

// ③⑦ 1순위 401 + 2순위 성공: 점검을 끝까지 하고 fallback_login.txt·리포트 구역, 비밀번호·계정명 없음.
// 웜업 중 2순위 성공은 AUTH_FAIL 처럼 세지만(1순위 공통 계정이 틀렸을 수 있음), N=2 이므로 다음 호스트(1순위 성공)는 점검한다.
func TestFallbackSecondarySucceeds(t *testing.T) {
	s := startFB(t, "dell-r660", true)
	must(t, s.SetBiosAttr("LogicalProc", "Disabled"))
	other := startMockTree(t, "cisco-c220m7", nil) // 1순위로 붙는 호스트
	rc := checkRCFor(t, 1, "", s.URL(), other.URL())
	rc.Conf.AuthFailStop = 2
	rc.Conf.Retries = 3 // 2순위 로그인은 이와 무관하게 재시도 0
	rc.Fallback = fbCreds(t, "# 예비\n"+fbConf("dell")+fbConf("cisco"))
	run := runCheckT(t, rc, "")

	h := run.Hosts[0]
	if h.Status != "" || h.CredSource != credFallback || len(h.Items) != 5 {
		t.Fatalf("Status=%q Detail=%q CredSource=%q 항목 %d", h.Status, h.Detail, h.CredSource, len(h.Items))
	}
	if it := itemOf(t, h, "hyper_threading"); it.Result != StatusFail {
		t.Errorf("2순위로 점검이 진행되지 않음: %+v", it)
	}
	if ss := s.Sessions(); ss.LoginFailed != 1 || ss.Created != 1 || ss.Deleted != 1 {
		t.Errorf("세션 %+v, 기대 실패 1 / 생성 1 / 삭제 1", ss)
	}
	if n := s.Count("POST", sessPathFB); n != 2 || probes(s) != 1 {
		t.Errorf("로그인 POST %d회(기대 2), 벤더 조회 %d회(기대 1)", n, probes(s))
	}
	if h.Stats.Posts != 2 || h.Stats.Sessions != 1 || h.Stats.AuthMode != "session" {
		t.Errorf("호출 통계 합산: %+v", h.Stats)
	}
	if o := run.Hosts[1]; o.Status != "" || o.CredSource != credPrimary {
		t.Errorf("auth_fail_stop=1 인데 다음 호스트가 처리되지 않음: %q %q", o.Status, o.CredSource)
	}
	assertClean(t, s)

	if got := readFileT(t, filepath.Join(run.Dir, fallbackLoginFile)); got != h.Target.Hostname+"\n" {
		t.Errorf("%s = %q", fallbackLoginFile, got)
	}
	if runtime.GOOS != "windows" {
		if fi, err := os.Stat(filepath.Join(run.Dir, fallbackLoginFile)); err != nil || fi.Mode().Perm() != 0o600 {
			t.Errorf("%s 권한: %v %v", fallbackLoginFile, fi, err)
		}
	}
	if info := readFileT(t, filepath.Join(run.Dir, "run_info.txt")); !strings.Contains(info, "2순위 계정(rf.conf)으로 접속: 1") {
		t.Errorf("run_info:\n%s", info)
	}
	rep := reportText(run)
	for _, w := range []string{"[2순위 계정(rf.conf)으로 접속] 1대", h.Target.Hostname + "(Dell)", fallbackLoginFile} {
		if !strings.Contains(rep, w) {
			t.Errorf("리포트에 %q 없음:\n%s", w, rep)
		}
	}
	if strings.Contains(rep, "[계정 오류") || strings.Contains(rep, "[접속불가]") {
		t.Errorf("오류 구역이 나오면 안 됨:\n%s", rep)
	}
	// result.tsv 는 14열 그대로
	for _, l := range readLines(t, filepath.Join(run.Dir, "result.tsv")) {
		if n := len(strings.Split(l, "\t")); n != len(resultHeader) || n != 14 {
			t.Fatalf("result.tsv 열 %d: %q", n, l)
		}
	}
	assertNoFBSecrets(t, run.Dir, rep, fmt.Sprintf("%+v %#v %+v", rc.Fallback, rc.Fallback, h))
}

// L4/L5(보안 리뷰): 키 자리에 비밀번호가 섞이면 오류 메시지에 쓰지 않고, 일반 파일이 아니거나 너무 크면 거부한다.
func TestRFConfKeyNotEchoedAndSizeCap(t *testing.T) {
	_, err := parseRFConf("dell_pass Secret=1\n", "rf.conf")
	if err == nil || strings.Contains(err.Error(), "ecret") {
		t.Errorf("오류에 키 자리 문자열이 보임: %v", err)
	}
	dir := t.TempDir()
	if _, _, err := loadRFConf(dir); err == nil {
		t.Error("폴더는 거부되어야 함")
	}
	big := filepath.Join(dir, "big.conf")
	must(t, os.WriteFile(big, []byte(strings.Repeat("# x\n", 40000)), 0o600))
	if _, _, err := loadRFConf(big); err == nil || !strings.Contains(err.Error(), "너무 큽니다") {
		t.Errorf("큰 파일: %v", err)
	}
}

// M1(보안 리뷰): 웜업 N대가 모두 "1순위 거부 + 2순위 성공" 이면 차단기가 멈춘다 (1순위 공통 비밀번호 오류가 예비 계정에 가려지지 않게).
func TestFallbackWarmupBreaker(t *testing.T) {
	a := startFB(t, "dell-r660", true)
	b := startFB(t, "dell-r660", true)
	rc := checkRCFor(t, 1, "", a.URL(), b.URL())
	rc.Conf.AuthFailStop = 1
	rc.Fallback = fbCreds(t, fbConf("dell"))
	run := runCheckT(t, rc, "")
	if h := run.Hosts[0]; h.Status != "" || h.CredSource != credFallback {
		t.Fatalf("첫 호스트: %q %q", h.Status, h.CredSource)
	}
	if h := run.Hosts[1]; h.Status != StatusSkippedAuthStop {
		t.Errorf("웜업 1대가 2순위 로그인이면 차단기가 멈춰야 함: 둘째 호스트 %q", h.Status)
	}
	if n := b.Count("POST", sessPathFB); n != 0 {
		t.Errorf("차단 후 둘째 BMC 에 로그인 %d회", n)
	}
}

// 웜업 중 1순위로 로그인에 성공하면 이후에는 2순위 사용이 많아도 차단하지 않는다 (일부 BMC 만 1순위 계정이 없는 정상 상황).
func TestFallbackAfterWarmupNotCounted(t *testing.T) {
	ok := startMockTree(t, "cisco-c220m7", nil)
	a := startFB(t, "dell-r660", true)
	b := startFB(t, "dell-r660", true)
	c := startFB(t, "dell-r660", true)
	rc := checkRCFor(t, 1, "", ok.URL(), a.URL(), b.URL(), c.URL())
	rc.Conf.AuthFailStop = 2
	rc.Fallback = fbCreds(t, fbConf("dell")+fbConf("cisco"))
	run := runCheckT(t, rc, "")
	for i, h := range run.Hosts[1:] {
		if h.Status != "" || h.CredSource != credFallback {
			t.Errorf("호스트 %d: %q %q (차단되면 안 됨)", i+1, h.Status, h.CredSource)
		}
	}
}

// ④⑦ 1순위·2순위 모두 401 → AUTH_FAIL, 로그인 시도 정확히 2회 (Retries 와 무관). 차단기는 최종 AUTH_FAIL 을 센다.
func TestFallbackBothFail(t *testing.T) {
	s := startMockTree(t, "hpe-dl360gen11", func(o *mockbmc.Options) {
		o.User, o.Pass, o.OpenServiceRoot = fbUser, "BMC-쪽-다른-비밀번호", true
	})
	next := startMockTree(t, "dell-r660", nil)
	rc := checkRCFor(t, 1, "", s.URL(), next.URL())
	rc.Conf.AuthFailStop = 1
	rc.Conf.Retries = 5
	rc.Fallback = fbCreds(t, fbConf("hpe"))
	run := runCheckT(t, rc, "")

	h := run.Hosts[0]
	if h.Status != StatusAuthFail || h.CredSource != "" || !strings.Contains(h.Detail, "2순위 계정도 실패") || !strings.Contains(h.Detail, "HPE") {
		t.Fatalf("Status=%q Detail=%q CredSource=%q", h.Status, h.Detail, h.CredSource)
	}
	if ss := s.Sessions(); ss.LoginFailed != 2 || ss.Created != 0 {
		t.Errorf("세션 %+v, 기대 로그인 실패 정확히 2", ss)
	}
	if n := s.Count("POST", sessPathFB); n != 2 {
		t.Errorf("로그인 POST %d회, 기대 2", n)
	}
	if s.CallCount() != 3 { // POST, 비인증 GET /redfish/v1/, POST
		t.Errorf("요청 %d건, 기대 3: %+v", s.CallCount(), s.Calls())
	}
	if run.Hosts[1].Status != StatusSkippedAuthStop || next.CallCount() != 0 {
		t.Errorf("최종 AUTH_FAIL 은 차단기에 세야 함: 다음 호스트 %q 요청 %d", run.Hosts[1].Status, next.CallCount())
	}
	if _, err := os.Stat(filepath.Join(run.Dir, fallbackLoginFile)); !os.IsNotExist(err) {
		t.Errorf("%s 가 생기면 안 됨", fallbackLoginFile)
	}
	if retry := readFileT(t, filepath.Join(run.Dir, "retry.txt")); strings.Contains(retry, strings.TrimPrefix(s.URL(), "https://")) {
		t.Errorf("AUTH_FAIL 은 retry.txt 에 넣지 않음: %q", retry)
	}
	rep := reportText(run)
	for _, w := range []string{"[계정 오류 AUTH_FAIL] 1대 (1순위, 2순위 계정 모두 실패 또는 예비 계정 없음)", "2순위 계정도 실패"} {
		if !strings.Contains(rep, w) {
			t.Errorf("리포트에 %q 없음:\n%s", w, rep)
		}
	}
	if strings.Contains(rep, "[2순위 계정(rf.conf)으로 접속]") {
		t.Errorf("2순위 접속 구역이 나오면 안 됨")
	}
	assertNoFBSecrets(t, run.Dir, rep)
}

// ⑤ 벤더를 식별하지 못하거나 그 벤더의 예비 계정이 없으면 2순위 로그인은 0회 (다른 벤더 계정을 시험하지 않음).
func TestFallbackNoVendorNoAttempt(t *testing.T) {
	all := fbCreds(t, fbConf("dell")+fbConf("hpe")+fbConf("lenovo")+fbConf("supermicro")+fbConf("cisco"))
	cases := []struct {
		name  string
		start func(t *testing.T) *mockbmc.Server
		fb    *fallbackCreds
		want  string
		probe int
	}{
		{"ServiceRoot 가 인증을 요구 (401)", func(t *testing.T) *mockbmc.Server { return startFB(t, "dell-r660", false) }, all, "식별하지 못해", 1},
		{"ServiceRoot 에 벤더 단서 없음", func(t *testing.T) *mockbmc.Server {
			s := startFB(t, "dell-r660", true)
			must(t, s.Modify("/redfish/v1", func(m map[string]interface{}) { delete(m, "Vendor") }))
			return s
		}, all, "식별하지 못해", 1},
		{"벤더는 HPE 인데 rf.conf 에 hpe 계정 없음", func(t *testing.T) *mockbmc.Server { return startFB(t, "hpe-dl360gen11", true) },
			fbCreds(t, fbConf("dell")+fbConf("lenovo")), "HPE 예비 계정 없음", 1},
		{"rf.conf 에 계정이 하나도 없음", func(t *testing.T) *mockbmc.Server { return startFB(t, "dell-r660", true) },
			fbCreds(t, "# 비어 있음\n"), "예비 계정 없음", 0},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			s := c.start(t)
			rc := checkRCFor(t, 1, "", s.URL())
			rc.Fallback = c.fb
			h := runCheckT(t, rc, "").Hosts[0]
			if h.Status != StatusAuthFail || !strings.Contains(h.Detail, c.want) {
				t.Errorf("Status=%q Detail=%q, 기대 AUTH_FAIL + %q", h.Status, h.Detail, c.want)
			}
			if ss := s.Sessions(); ss.LoginFailed != 1 || s.Count("POST", sessPathFB) != 1 {
				t.Errorf("로그인 시도: %+v POST=%d, 기대 1 (2순위 0회)", ss, s.Count("POST", sessPathFB))
			}
			if probes(s) != c.probe {
				t.Errorf("벤더 조회 %d회, 기대 %d", probes(s), c.probe)
			}
		})
	}
}

// ⑥ 연결 거부·타임아웃·5xx 는 인증 문제가 아니므로 예비 계정도, 벤더 조회도 하지 않고 [접속불가] 로 보인다.
func TestFallbackNotOnNetworkErrors(t *testing.T) {
	dead := startFB(t, "dell-r660", true)
	deadURL := dead.URL()
	dead.Close()
	slow := startFB(t, "dell-r660", true)
	slow.SetFault("POST "+sessPathFB, mockbmc.Fault{Delay: 3 * time.Second})
	busy := startFB(t, "lenovo-sr650v3", true)
	busy.SetFault("POST "+sessPathFB, mockbmc.Fault{Status: 503})

	rc := checkRCFor(t, 1, "", deadURL, slow.URL(), busy.URL())
	rc.Conf.Timeout = 300 * time.Millisecond
	rc.Fallback = fbCreds(t, fbConf("dell")+fbConf("lenovo"))
	run := runCheckT(t, rc, "")
	for i, want := range []string{StatusUnreachable, StatusTimeout, StatusBMCError} {
		if h := run.Hosts[i]; h.Status != want || h.CredSource != "" {
			t.Errorf("호스트 %d: %q (%s), 기대 %s", i, h.Status, h.Detail, want)
		}
	}
	for _, s := range []*mockbmc.Server{slow, busy} {
		if n := s.Count("POST", sessPathFB); n != 1 || probes(s) != 0 || s.Sessions().LoginFailed != 0 {
			t.Errorf("로그인 POST %d회 / 벤더 조회 %d회 / %+v, 기대 1/0", n, probes(s), s.Sessions())
		}
	}
	rep := reportText(run)
	for _, w := range []string{"[접속불가] 3대 (네트워크·포트·TLS·타임아웃 — 계정 문제 아님)", StatusUnreachable, StatusTimeout, StatusBMCError, "retry.txt"} {
		if !strings.Contains(rep, w) {
			t.Errorf("리포트에 %q 없음:\n%s", w, rep)
		}
	}
	if strings.Contains(rep, "[계정 오류") || strings.Contains(rep, "[기타 오류]") {
		t.Errorf("네트워크 오류가 계정 오류·기타 오류로 보임:\n%s", rep)
	}
	if got := len(readLines(t, filepath.Join(run.Dir, "retry.txt"))); got != 3 {
		t.Errorf("retry.txt %d줄, 기대 3", got)
	}
}

// ⑧ rf.conf 가 없으면 기존 동작과 같다: 벤더 조회 없음, 로그인 1회, AUTH_FAIL 사유·run_info 그대로.
func TestFallbackAbsentRegression(t *testing.T) {
	fb, warn, err := loadRFConf(filepath.Join(t.TempDir(), "rf.conf"))
	if fb != nil || warn != "" || err != nil {
		t.Fatalf("없는 rf.conf: %v %q %v", fb, warn, err)
	}
	good := startMockTree(t, "dell-r660", func(o *mockbmc.Options) { o.OpenServiceRoot = true })
	bad := startFB(t, "hpe-dl360gen11", true)
	rc := checkRCFor(t, 1, "", good.URL(), bad.URL())
	rc.Fallback = fb
	run := runCheckT(t, rc, "")
	if h := run.Hosts[0]; h.Status != "" || h.CredSource != credPrimary {
		t.Errorf("정상 호스트: %q %q", h.Status, h.CredSource)
	}
	h := run.Hosts[1]
	if h.Status != StatusAuthFail || h.Detail != "세션 생성 요청이 실패했습니다 (HTTP 401)" || h.CredSource != "" {
		t.Errorf("AUTH_FAIL 호스트: %q %q %q", h.Status, h.Detail, h.CredSource)
	}
	if bad.CallCount() != 1 || probes(good) != 0 || probes(bad) != 0 {
		t.Errorf("rf.conf 없음인데 추가 요청: bad %d건 %+v", bad.CallCount(), bad.Calls())
	}
	assertOneSession(t, good)
	if _, err := os.Stat(filepath.Join(run.Dir, fallbackLoginFile)); !os.IsNotExist(err) {
		t.Errorf("%s 가 생기면 안 됨", fallbackLoginFile)
	}
	if info := readFileT(t, filepath.Join(run.Dir, "run_info.txt")); strings.Contains(info, "2순위") {
		t.Errorf("rf.conf 없음인데 run_info 에 2순위 줄:\n%s", info)
	}
	if rep := reportText(run); strings.Contains(rep, "2순위 계정(rf.conf)") {
		t.Errorf("rf.conf 없음인데 2순위 구역:\n%s", rep)
	}
}

// ⑨ set(Y 적용)·dry-run·dump 도 같은 헬퍼로 같은 규칙(1순위 401 → 벤더 예비 계정 1회)으로 로그인한다.
func TestFallbackSetDryRunDump(t *testing.T) {
	fastDump(t)
	s := startFB(t, "dell-r660", true)
	must(t, s.SetBiosAttr("LogicalProc", "Disabled"))
	rc := checkRCFor(t, 1, "", s.URL())
	rc.Fallback = fbCreds(t, fbConf("dell"))
	run := runCheckP(t, rc, setProf(t, setProfText))
	if h := run.Hosts[0]; h.Status != "" || h.CredSource != credFallback {
		t.Fatalf("점검: %q %q %q", h.Status, h.Detail, h.CredSource)
	}

	var dry bytes.Buffer
	must(t, doDryRun(rc, run, &dry))
	if s.Writes() != 0 || !strings.Contains(dry.String(), "LogicalProc") {
		t.Errorf("dry-run: Writes=%d\n%s", s.Writes(), dry.String())
	}
	out := applyY(t, rc, run)
	wantStatus(t, applyResults(t, run), s, "LogicalProc", StatusApplied)
	if len(patchBodies(s)) != 1 {
		t.Errorf("PATCH %d회, 기대 1", len(patchBodies(s)))
	}

	// 점검·dry-run·적용 3번 모두 1순위 1회 실패 + 2순위 1회 성공
	if n := s.Count("POST", sessPathFB); n != 6 || probes(s) != 3 {
		t.Errorf("로그인 POST %d(기대 6), 벤더 조회 %d(기대 3)", n, probes(s))
	}
	rs, err := runDump(dumpRCWithFB(t, s.URL(), rc.Fallback), dumpOpts{Out: t.TempDir()})
	must(t, err)
	if rs[0].Status != "OK" || rs[0].Stats.Posts != 2 {
		t.Errorf("dump: %s %+v", rs[0].Status, rs[0].Stats)
	}
	if ss := s.Sessions(); ss.LoginFailed != 4 || ss.Created != 4 || ss.Deleted != 4 {
		t.Errorf("세션 %+v, 기대 (점검·dry-run·적용·덤프) 실패 4 / 생성 4 / 삭제 4", ss)
	}
	if n := s.Count("POST", sessPathFB); n != 8 {
		t.Errorf("로그인 POST %d, 기대 8", n)
	}
	assertNoFBSecrets(t, run.Dir, out, dry.String())

	// 2순위도 거부되면 set 도 AUTH_FAIL (로그인 2회, PATCH 없음)
	s2 := startMockTree(t, "dell-r660", func(o *mockbmc.Options) {
		o.User, o.Pass, o.OpenServiceRoot = testUser, testPass, true
	})
	must(t, s2.SetBiosAttr("LogicalProc", "Disabled"))
	rc2 := checkRCFor(t, 1, "", s2.URL())
	rc2.Fallback = fbCreds(t, fbConf("dell"))
	run2 := runCheckP(t, rc2, setProf(t, setProfText))
	s2.SetFault("POST "+sessPathFB, mockbmc.Fault{Status: 401}) // 점검 뒤 1·2순위 모두 거부
	applyY(t, rc2, run2)
	wantStatus(t, applyResults(t, run2), s2, "LogicalProc", StatusAuthFail)
	if n := s2.Count("POST", sessPathFB); n != 3 || s2.Count("PATCH", "/") != 0 {
		t.Errorf("set 2순위 실패: 로그인 POST %d(기대 점검 1 + set 2), PATCH %d", n, s2.Count("PATCH", "/"))
	}
}

func dumpRCWithFB(t *testing.T, url string, fb *fallbackCreds) *runContext {
	t.Helper()
	rc := dumpRC(t, 1, url)
	rc.Fallback = fb
	return rc
}
