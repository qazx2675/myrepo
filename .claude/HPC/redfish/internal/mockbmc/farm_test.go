package mockbmc

import (
	"fmt"
	"reflect"
	"runtime"
	"testing"
	"time"
)

// Farm 시험: 한 리스너가 접속한 로컬 IP 로 가상 BMC 를 구분한다. 리눅스(127.0.0.0/8 전체가 루프백)에서만 돈다.

func newFarmT(t *testing.T, fo FarmOptions) (*Farm, int) {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("127.0.0.0/8 전체를 루프백으로 쓰는 리눅스에서만 시험한다")
	}
	f := NewFarm(fo)
	if _, err := f.Listen("0.0.0.0:0"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(f.Close)
	return f, f.Port()
}

func farmCli(t *testing.T, ip string, port int) *cli {
	return newCli(t, fmt.Sprintf("https://%s:%d", ip, port), 3*time.Second)
}

func mustTree(t *testing.T, name string) *Tree {
	t.Helper()
	tr, err := LoadTree(treeDir(name))
	if err != nil {
		t.Fatal(err)
	}
	return tr
}

func TestFarmSeparatesHostsByLocalIP(t *testing.T) {
	f, port := newFarmT(t, FarmOptions{})
	dell, hpe := mustTree(t, "dell-r660"), mustTree(t, "hpe-dl360gen11")
	ho := HostOptions{User: tUser, Pass: tPass}
	over := ho
	over.AttrOverrides = map[string]interface{}{"LogicalProc": "Disabled"}
	for ip, a := range map[string]struct {
		tr *Tree
		o  HostOptions
	}{"127.0.1.1": {dell, ho}, "127.0.1.2": {dell, over}, "127.0.1.3": {hpe, ho}} {
		if err := f.AddHost(ip, a.tr, a.o); err != nil {
			t.Fatal(err)
		}
	}
	if err := f.AddHost("127.0.1.1", dell, ho); err == nil {
		t.Error("같은 IP 를 두 번 등록하면 오류여야 함")
	}
	if err := f.AddHost("not-an-ip", dell, ho); err == nil {
		t.Error("IP 형식이 아니면 오류여야 함")
	}

	const dellBios = "/redfish/v1/Systems/System.Embedded.1/Bios"
	c1, c2, c3 := farmCli(t, "127.0.1.1", port), farmCli(t, "127.0.1.2", port), farmCli(t, "127.0.1.3", port)
	for _, c := range []*cli{c1, c2, c3} {
		c.login()
	}
	if got := c1.attrs(dellBios)["LogicalProc"]; got != "Enabled" {
		t.Errorf("호스트1 LogicalProc = %v, 기대 Enabled (덮어쓰기가 다른 호스트로 새면 안 됨)", got)
	}
	if got := c2.attrs(dellBios)["LogicalProc"]; got != "Disabled" {
		t.Errorf("호스트2 LogicalProc = %v, 기대 Disabled", got)
	}
	if got := c3.attrs("/redfish/v1/systems/1/bios")["ProcHyperthreading"]; got == nil {
		t.Error("호스트3 은 HPE 트리로 응답해야 함")
	}

	// 호출 기록·세션 집계는 호스트별로 따로 쌓인다.
	h1, h2, h3 := f.Host("127.0.1.1"), f.Host("127.0.1.2"), f.Host("127.0.1.3")
	if h1.Count("GET", dellBios) != 1 || h2.Count("GET", dellBios) != 1 || h3.Count("GET", dellBios) != 0 {
		t.Error("다른 호스트의 호출이 섞이면 안 됨")
	}
	for _, h := range []*Host{h1, h2, h3} {
		if ss := h.Sessions(); ss.Created != 1 || ss.LoginFailed != 0 {
			t.Errorf("세션 = %+v", ss)
		}
	}
	tot := f.Totals()
	if tot.Hosts != 3 || tot.Sessions.Created != 3 || tot.Writes != 0 || tot.LogHits != 0 || tot.Calls != h1.CallCount()+h2.CallCount()+h3.CallCount() {
		t.Errorf("합계 = %+v", tot)
	}

	// 트리는 호스트 간에 같은 메모리를 공유한다 (복사 아님).
	if reflect.ValueOf(h1.tree).Pointer() != reflect.ValueOf(h2.tree).Pointer() {
		t.Error("같은 Tree 로 등록한 호스트는 트리 맵을 공유해야 함")
	}
	// 호스트별 변경(Pending)도 개별이다.
	c1.must("PATCH", "/redfish/v1/Systems/System.Embedded.1/Bios/Settings", patchBody("LogicalProc", "Disabled"), 200)
	if len(h1.Pending()) != 1 || len(h2.Pending()) != 0 {
		t.Errorf("Pending 이 호스트별이 아님: %v %v", h1.Pending(), h2.Pending())
	}
	if tot := f.Totals(); tot.Writes != 1 {
		t.Errorf("Writes = %d, 기대 1", tot.Writes)
	}
}

func TestFarmUnknownIPDroppedOrStatus(t *testing.T) {
	f, port := newFarmT(t, FarmOptions{})
	if err := f.AddHost("127.0.2.1", mustTree(t, "dell-r660"), HostOptions{User: tUser, Pass: tPass}); err != nil {
		t.Fatal(err)
	}
	c := farmCli(t, "127.0.2.9", port)
	if _, _, _, err := c.do("GET", "/redfish/v1", nil); err == nil {
		t.Error("등록되지 않은 IP 는 연결이 끊겨 전송 오류여야 함")
	}
	if f.Dropped() != 1 || f.Totals().Calls != 0 {
		t.Errorf("Dropped = %d, 호출 = %d", f.Dropped(), f.Totals().Calls)
	}

	f2, port2 := newFarmT(t, FarmOptions{UnknownStatus: 404})
	st, _, _, err := farmCli(t, "127.0.2.9", port2).do("GET", "/redfish/v1", nil)
	if err != nil || st != 404 {
		t.Errorf("UnknownStatus=404: %d %v", st, err)
	}
	if f2.Dropped() != 0 {
		t.Errorf("UnknownStatus 가 있으면 끊지 않음: Dropped=%d", f2.Dropped())
	}
}

func TestFarmPerHostCredsFaultsAndConnCounts(t *testing.T) {
	f, port := newFarmT(t, FarmOptions{})
	tr := mustTree(t, "dell-r660")
	_ = f.AddHost("127.0.3.1", tr, HostOptions{User: tUser, Pass: tPass})
	_ = f.AddHost("127.0.3.2", tr, HostOptions{User: tUser, Pass: "다른-비밀번호"})
	_ = f.AddHost("127.0.3.3", tr, HostOptions{User: tUser, Pass: tPass, Fail: map[string]Fault{"/redfish/v1/Systems*": {Status: 503}}})
	_ = f.AddHost("127.0.3.4", tr, HostOptions{User: tUser, Pass: tPass, NoSessions: true})

	c1 := farmCli(t, "127.0.3.1", port)
	c1.login()
	c2 := farmCli(t, "127.0.3.2", port)
	if st, _, _, _ := c2.do("POST", "/redfish/v1/SessionService/Sessions", map[string]string{"UserName": tUser, "Password": tPass}); st != 401 {
		t.Errorf("호스트2 로그인 = %d, 기대 401 (호스트별 비밀번호)", st)
	}
	if ss := f.Host("127.0.3.2").Sessions(); ss.LoginFailed != 1 || ss.Created != 0 {
		t.Errorf("호스트2 세션 = %+v", ss)
	}
	c3 := farmCli(t, "127.0.3.3", port)
	c3.login()
	if st, _, _, _ := c3.do("GET", "/redfish/v1/Systems", nil); st != 503 {
		t.Errorf("호스트3 장애 주입 = %d, 기대 503", st)
	}
	if st, _, _, _ := c1.do("GET", "/redfish/v1/Systems", nil); st != 200 {
		t.Errorf("장애 주입이 호스트1 에 새면 안 됨: %d", st)
	}
	c4 := farmCli(t, "127.0.3.4", port)
	if st, _, _, _ := c4.do("POST", "/redfish/v1/SessionService/Sessions", map[string]string{"UserName": tUser, "Password": tPass}); st != 404 {
		t.Errorf("NoSessions 호스트 세션 POST = %d, 기대 404", st)
	}
	if tot := f.Totals(); tot.Sessions.LoginFailed != 1 || tot.Sessions.Created != 2 {
		t.Errorf("합계 세션 = %+v", tot.Sessions)
	}

	if f.MaxConns() < 1 || f.Accepted() < 1 {
		t.Errorf("연결 집계: max=%d accepted=%d", f.MaxConns(), f.Accepted())
	}
	deadline := time.Now().Add(2 * time.Second) // DisableKeepAlives 클라이언트라 곧 모두 닫힌다
	for f.OpenConns() != 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if f.OpenConns() != 0 {
		t.Errorf("OpenConns = %d, 기대 0", f.OpenConns())
	}
}
