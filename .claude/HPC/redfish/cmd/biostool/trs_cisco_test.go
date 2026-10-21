package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// ciscoServer 는 Cisco 블레이드 BMC 흉내입니다: Redfish(Bios 링크 유무 선택)와 선택적인 XML API(/nuova).
type ciscoServer struct {
	withBios bool // System 에 Bios 링크와 속성이 있음
	xmlAPI   bool // /nuova 가 UCSM 처럼 응답
	xmlAuth  bool // aaaLogin 을 받아 줌 (false 면 errorCode)
	posts    []string
	gets     []string
}

func (s *ciscoServer) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimRight(r.URL.Path, "/")
		if r.Method == http.MethodPost && p == "/nuova" {
			b, _ := io.ReadAll(r.Body)
			body := string(b)
			s.posts = append(s.posts, "nuova:"+body)
			if !s.xmlAPI {
				w.WriteHeader(404)
				return
			}
			w.Header().Set("Content-Type", "text/xml")
			switch {
			case strings.Contains(body, "<aaaLogin"):
				if !s.xmlAuth {
					_, _ = w.Write([]byte(`<aaaLogin cookie="" response="yes" errorCode="551" errorDescr="Authentication failed"/>`))
					return
				}
				_, _ = w.Write([]byte(`<aaaLogin cookie="" response="yes" outCookie="ck-1" outRefreshPeriod="600"/>`))
			case strings.Contains(body, "<aaaLogout"):
				_, _ = w.Write([]byte(`<aaaLogout cookie="" response="yes" outStatus="success"/>`))
			case strings.Contains(body, `cookie=""`):
				_, _ = w.Write([]byte(`<configResolveClass cookie="" response="yes" errorCode="552" errorDescr="Authorization required"/>`))
			case strings.Contains(body, `classId="computeBlade"`):
				_, _ = w.Write([]byte(`<configResolveClass cookie="ck-1" response="yes" classId="computeBlade"><outConfigs><computeBlade dn="sys/chassis-1/blade-3" model="UCSX-210C-M7"/></outConfigs></configResolveClass>`))
			default:
				_, _ = w.Write([]byte(`<configResolveClass cookie="ck-1" response="yes" classId="x"><outConfigs/></configResolveClass>`))
			}
			return
		}
		if r.Method == http.MethodPost && p == "/redfish/v1/SessionService/Sessions" {
			w.Header().Set("X-Auth-Token", "tok-1")
			w.Header().Set("Location", "/redfish/v1/SessionService/Sessions/7")
			w.WriteHeader(201)
			_, _ = w.Write([]byte(`{}`))
			return
		}
		if r.Method == http.MethodDelete {
			w.WriteHeader(204)
			return
		}
		s.gets = append(s.gets, p)
		if p == "" && r.Method == http.MethodGet {
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte(`<html><head><title>Cisco BMC</title></head></html>`))
			return
		}
		if p == "/nuova" {
			w.WriteHeader(404)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if p != "/redfish/v1" && r.Header.Get("X-Auth-Token") != "tok-1" {
			if _, _, ok := r.BasicAuth(); !ok {
				w.WriteHeader(401)
				return
			}
		}
		sys := `{"Manufacturer":"Cisco Systems Inc","Model":"UCSX-210C-M7","BiosVersion":"X210M7.4.3","PowerState":"On","Oem":{"Cisco":{"Foo":{"@odata.id":"/redfish/v1/Systems/N1/Oem/Cisco/Foo"}}}`
		if s.withBios {
			sys += `,"Bios":{"@odata.id":"/redfish/v1/Systems/N1/Bios"}`
		}
		sys += `}`
		switch p {
		case "/redfish/v1":
			_, _ = w.Write([]byte(`{"Vendor":"Cisco","Product":"Cisco BMC","RedfishVersion":"1.15.0"}`))
		case "/redfish/v1/Systems":
			_, _ = w.Write([]byte(`{"Members":[{"@odata.id":"/redfish/v1/Systems/N1"}]}`))
		case "/redfish/v1/Systems/N1":
			_, _ = w.Write([]byte(sys))
		case "/redfish/v1/Systems/N1/Bios":
			if !s.withBios {
				w.WriteHeader(404)
				return
			}
			_, _ = w.Write([]byte(`{"AttributeRegistry":"BiosAttributeRegistry","Attributes":{"A":"1","B":"2"}}`))
		case "/redfish/v1/Systems/N1/Oem/Cisco/Foo":
			_, _ = w.Write([]byte(`{"Name":"foo"}`))
		case "/redfish/v1/Chassis":
			_, _ = w.Write([]byte(`{"Members":[{"@odata.id":"/redfish/v1/Chassis/1"}]}`))
		case "/redfish/v1/Chassis/1":
			_, _ = w.Write([]byte(`{"ChassisType":"Blade","Manufacturer":"Cisco","Model":"UCSX-9508"}`))
		case "/redfish/v1/Managers":
			_, _ = w.Write([]byte(`{"Members":[{"@odata.id":"/redfish/v1/Managers/CIMC"}]}`))
		case "/redfish/v1/Managers/CIMC":
			_, _ = w.Write([]byte(`{"ManagerType":"BMC","FirmwareVersion":"5.2(2.240074)"}`))
		case "/redfish/v1/Registries":
			_, _ = w.Write([]byte(`{"Members":[{"@odata.id":"/redfish/v1/Registries/Base.1.8"}]}`))
		case "/redfish/v1/UpdateService":
			_, _ = w.Write([]byte(`{"FirmwareInventory":{"@odata.id":"/redfish/v1/UpdateService/FirmwareInventory"}}`))
		case "/redfish/v1/UpdateService/FirmwareInventory":
			_, _ = w.Write([]byte(`{"Members":[{"@odata.id":"/redfish/v1/UpdateService/FirmwareInventory/BIOS"}]}`))
		case "/redfish/v1/UpdateService/FirmwareInventory/BIOS":
			_, _ = w.Write([]byte(`{"Name":"BIOS","Version":"X210M7.4.3"}`))
		default:
			w.WriteHeader(404)
		}
	})
}

func ciscoRun(t *testing.T, s *ciscoServer) (string, *ciscoHost) {
	t.Helper()
	srv := httptest.NewTLSServer(s.handler())
	defer srv.Close()
	conf := &Config{User: "admin", Insecure: true, Timeout: 5 * time.Second, XMLHostPrefix: []string{"c"}}
	tg := trsTarget(t, srv)
	tg.Hostname = "c101"
	th := newTrsHost(tg, conf, "S3cretPw!", nil)
	defer th.pr.Close()
	h := &ciscoHost{trsHost: th}
	h.run(nil)
	if testing.Verbose() {
		t.Log("\n" + h.out.String())
	}
	return h.out.String(), h
}

// UCSX-210C-M7 처럼 System 에 Bios 링크가 없고 XML API 도 없는 주소: 두 원인을 모두 짚고, XML 에는 비밀번호를 보내지 않는다.
func TestTrsCiscoNoBiosNoXML(t *testing.T) {
	s := &ciscoServer{}
	rep, h := ciscoRun(t, s)
	for _, want := range []string{
		"Model=UCSX-210C-M7", "System 에 Bios 링크가 없습니다", "BIOS 설정 리소스가 없습니다",
		"BIOS 이름이 든 항목: /redfish/v1/UpdateService/FirmwareInventory/BIOS", "XML 실패 → Redfish",
		"XML API(/nuova)를 제공하지 않습니다", "이 진단은 비밀번호를 보내지 않았고", "UCSM VIP", "종합:",
		"Oem.Cisco 항목: Foo",
	} {
		if !strings.Contains(rep, want) {
			t.Errorf("리포트에 %q 없음:\n%s", want, rep)
		}
	}
	if strings.Contains(rep, "S3cretPw!") || strings.Contains(rep, "tok-1") {
		t.Error("비밀번호·토큰이 리포트에 있음")
	}
	for _, p := range s.posts {
		if strings.Contains(p, "aaaLogin") || strings.Contains(p, "S3cretPw!") {
			t.Errorf("XML API 가 없는 주소에 로그인 요청을 보냄: %s", p)
		}
	}
	if h.x.loginTried || h.x.kind == "xml" {
		t.Errorf("XML 상태: %+v", h.x)
	}
}

// XML API 가 있는 주소(UCSM 형): 로그인 1번 뒤 서버 목록을 읽고, Redfish 에 Bios 가 없다는 것도 함께 보고한다.
func TestTrsCiscoXMLWorks(t *testing.T) {
	s := &ciscoServer{xmlAPI: true, xmlAuth: true}
	rep, h := ciscoRun(t, s)
	for _, want := range []string{"✔ aaaLogin 성공", "computeBlade: 1개", "sys/chassis-1/blade-3", "XML API 가 정상 동작합니다", "BIOS 설정 리소스가 없습니다"} {
		if !strings.Contains(rep, want) {
			t.Errorf("리포트에 %q 없음:\n%s", want, rep)
		}
	}
	logins := 0
	for _, p := range s.posts {
		if strings.Contains(p, "<aaaLogin") {
			logins++
		}
	}
	if logins != 1 {
		t.Errorf("aaaLogin %d번 (1번이어야 함)", logins)
	}
	if strings.Contains(rep, "S3cretPw!") || len(h.x.servers) != 1 {
		t.Errorf("servers=%d", len(h.x.servers))
	}
}

// 로그인이 거부되면 그 사유를 보여 주고 다시 시도하지 않는다.
func TestTrsCiscoXMLAuthFail(t *testing.T) {
	s := &ciscoServer{xmlAPI: true, xmlAuth: false}
	rep, _ := ciscoRun(t, s)
	if !strings.Contains(rep, "aaaLogin 실패: AUTH_FAIL") || !strings.Contains(rep, "로그인이 거부되었습니다") {
		t.Errorf("로그인 거부 설명 없음:\n%s", rep)
	}
	n := 0
	for _, p := range s.posts {
		if strings.Contains(p, "<aaaLogin") {
			n++
		}
	}
	if n != 1 {
		t.Errorf("aaaLogin %d번", n)
	}
}

// Bios 가 정상인 Cisco: 읽을 수 있다고 보고한다.
func TestTrsCiscoBiosOK(t *testing.T) {
	s := &ciscoServer{withBios: true}
	rep, _ := ciscoRun(t, s)
	if !strings.Contains(rep, "Redfish 로는 BIOS 를 읽을 수 있습니다") || strings.Contains(rep, "BIOS 설정 리소스가 없습니다") {
		t.Errorf("정상 Bios 판정:\n%s", rep)
	}
}
