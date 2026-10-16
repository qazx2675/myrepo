package mockucsm

import (
	"crypto/tls"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"biostool/internal/ucsmxml"
)

const testJSON = `{"user":"demo","password":"pw1","servers":[
 {"dn":"sys/chassis-1/blade-1","class":"computeBlade","model":"UCSX-210C-M7","sp_name":"c201",
  "bios":{"biosVfCPUPerformance":{"vpCPUPerformance":"hpc"},"biosVfIntelHyperThreadingTech":{"vpIntelHyperThreadingTech":"disabled"}}},
 {"dn":"sys/chassis-1/blade-2","class":"computeBlade","model":"UCSX-210C-M7"},
 {"dn":"sys/rack-unit-1","class":"computeRackUnit","model":"UCSC-C220-M7","sp_name":"r1"}]}`

func start(t *testing.T) (*Server, string) {
	t.Helper()
	s, err := Parse([]byte(testJSON))
	if err != nil {
		t.Fatal(err)
	}
	addr, err := s.ListenTLS("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return s, "https://" + addr
}

func client(url, pw string) *ucsmxml.Client {
	return ucsmxml.New(ucsmxml.Options{BaseURL: url, User: "demo", Pass: pw, Insecure: true, Timeout: 5 * time.Second,
		Classes: []string{"biosVfCPUPerformance", "biosVfIntelHyperThreadingTech", "biosVfNothing", "lsServer"}})
}

func rawPost(url, body string) (string, error) {
	hc := &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}}
	resp, err := hc.Post(url+"/nuova", "application/xml", strings.NewReader(body))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return string(b), nil
}

func TestLoginFail(t *testing.T) {
	_, url := start(t)
	if err := client(url, "bad").Login(); err == nil {
		t.Fatal("틀린 비밀번호인데 로그인됨")
	}
	resp, _ := rawPost(url, `<aaaLogin inName="demo" inPassword="bad"/>`)
	if !strings.Contains(resp, `errorCode="551"`) {
		t.Errorf("551 이 아님: %s", resp)
	}
}

func TestResolveClasses(t *testing.T) {
	s, url := start(t)
	c := client(url, "pw1")
	if err := c.Login(); err != nil {
		t.Fatal(err)
	}
	blades, err := c.ResolveClass("computeBlade")
	if err != nil || len(blades) != 2 || blades[0].Attrs["model"] != "UCSX-210C-M7" {
		t.Fatalf("computeBlade %v %v", blades, err)
	}
	// computeRackUnit / lsServer 는 ucsmxml 허용목록 밖일 수 있어 원시 요청으로 본다.
	lg, _ := rawPost(url, `<aaaLogin inName="demo" inPassword="pw1"/>`)
	ck := strings.Split(strings.Split(lg, `outCookie="`)[1], `"`)[0]
	rk, _ := rawPost(url, `<configResolveClass cookie="`+ck+`" inHierarchical="false" classId="computeRackUnit"/>`)
	if !strings.Contains(rk, `<computeRackUnit dn="sys/rack-unit-1" model="UCSC-C220-M7"/>`) {
		t.Fatalf("computeRackUnit %s", rk)
	}
	ls, _ := rawPost(url, `<configResolveClass cookie="`+ck+`" inHierarchical="false" classId="lsServer"/>`)
	if !strings.Contains(ls, `<lsServer dn="org-root/ls-c201" name="c201" pnDn="sys/chassis-1/blade-1"/>`) ||
		!strings.Contains(ls, `name="r1" pnDn="sys/rack-unit-1"`) {
		t.Fatalf("lsServer %s", ls)
	}
	bi, err := c.ResolveClass("biosVfIntelHyperThreadingTech")
	if err != nil || len(bi) != 1 || bi[0].Attrs["dn"] != "sys/chassis-1/blade-1/bios/bios-settings/IntelHyperThreadingTech" ||
		bi[0].Attrs["vpIntelHyperThreadingTech"] != "disabled" {
		t.Fatalf("bios %v %v", bi, err)
	}
	if none, err := c.ResolveClass("biosVfNothing"); err != nil || len(none) != 0 {
		t.Fatalf("모르는 클래스는 빈 결과여야 함: %v %v", none, err)
	}
	_ = c.Logout()
	calls := strings.Join(s.Calls(), ",")
	if !strings.HasPrefix(calls, "aaaLogin,configResolveClass computeBlade") || !strings.HasSuffix(calls, "aaaLogout") {
		t.Errorf("호출 순서 %s", calls)
	}
}

func TestRejectsConf(t *testing.T) {
	s, url := start(t)
	resp, err := rawPost(url, `<configConfMo cookie="x" dn="sys/chassis-1/blade-1"/>`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(resp, `errorCode="103"`) {
		t.Errorf("configConfMo 가 거절되지 않음: %s", resp)
	}
	if got := s.Calls(); len(got) != 1 || got[0] != "configConfMo" {
		t.Errorf("호출 기록 %v", got)
	}
	if resp, _ := rawPost(url, `<configResolveClass cookie="nope" classId="computeBlade"/>`); !strings.Contains(resp, `errorCode="552"`) {
		t.Errorf("쿠키 없는 조회가 거절되지 않음: %s", resp)
	}
}

func TestParseValidation(t *testing.T) {
	for _, bad := range []string{`{}`, `{"user":"a","password":"b","servers":[{"dn":"x","class":"foo"}]}`, `{"user":"a","password":"b","servers":[{"class":"computeBlade"}]}`} {
		if _, err := Parse([]byte(bad)); err == nil {
			t.Errorf("오류여야 함: %s", bad)
		}
	}
}
