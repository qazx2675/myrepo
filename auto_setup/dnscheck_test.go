package main

import (
	"encoding/binary"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// ---------------------------------------------------------------- 가짜 DNS 서버 (UDP, 선택적으로 TCP)

type fakeAns struct {
	drop  bool     // 무응답
	nx    bool     // NXDOMAIN
	rcode int      // 0 이 아니고 nx 도 아니면 그 rcode (SERVFAIL 등)
	tc    bool     // UDP 응답을 잘림(TC)으로 — TCP 로 다시 묻게 함
	a     []string // A 레코드
	ptr   []string // PTR 이름
}

type fakeDNS struct {
	udp     *net.UDPConn
	tcp     net.Listener
	port    string
	handler func(name string, qtype uint16) fakeAns
	queries int32
}

func newFakeDNS(t *testing.T, withTCP bool, h func(name string, qtype uint16) fakeAns) *fakeDNS {
	t.Helper()
	uc, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeDNS{udp: uc, handler: h}
	f.port = strconv.Itoa(uc.LocalAddr().(*net.UDPAddr).Port)
	if withTCP {
		l, err := net.Listen("tcp", "127.0.0.1:"+f.port)
		if err != nil {
			uc.Close()
			t.Fatal(err)
		}
		f.tcp = l
		go f.serveTCP()
	}
	go f.serveUDP()
	t.Cleanup(func() {
		uc.Close()
		if f.tcp != nil {
			f.tcp.Close()
		}
	})
	return f
}

func (f *fakeDNS) addr() string { return "127.0.0.1:" + f.port }

// fakeParseQ: 질의 패킷에서 이름·타입을 읽는다
func fakeParseQ(b []byte) (name string, qtype uint16, qend int, ok bool) {
	n, off, err := dnsReadName(b, 12)
	if err != nil || off+4 > len(b) {
		return "", 0, 0, false
	}
	return strings.ToLower(n), binary.BigEndian.Uint16(b[off:]), off + 4, true
}

func fakeEncodeName(n string) []byte {
	var b []byte
	for _, l := range strings.Split(strings.TrimSuffix(n, "."), ".") {
		b = append(b, byte(len(l)))
		b = append(b, l...)
	}
	return append(b, 0)
}

func (f *fakeDNS) reply(q []byte, a fakeAns, udp bool) []byte {
	_, _, qend, _ := fakeParseQ(q)
	resp := append([]byte(nil), q[:qend]...)
	flags := uint16(0x8180) // QR RD RA
	switch {
	case a.nx:
		flags |= 3
	case a.rcode != 0:
		flags |= uint16(a.rcode)
	}
	if a.tc && udp {
		flags |= 0x0200
	}
	binary.BigEndian.PutUint16(resp[2:], flags)
	var ans []byte
	n := 0
	if !(a.tc && udp) {
		for _, ip := range a.a {
			ans = append(ans, 0xc0, 0x0c, 0, 1, 0, 1, 0, 0, 0, 60, 0, 4)
			ans = append(ans, net.ParseIP(ip).To4()...)
			n++
		}
		for _, p := range a.ptr {
			rd := fakeEncodeName(p)
			ans = append(ans, 0xc0, 0x0c, 0, 12, 0, 1, 0, 0, 0, 60, byte(len(rd)>>8), byte(len(rd)))
			ans = append(ans, rd...)
			n++
		}
	}
	binary.BigEndian.PutUint16(resp[6:], uint16(n))
	return append(resp, ans...)
}

func (f *fakeDNS) handle(q []byte, udp bool) []byte {
	name, qtype, _, ok := fakeParseQ(q)
	if !ok {
		return nil
	}
	atomic.AddInt32(&f.queries, 1)
	a := f.handler(name, qtype)
	if a.drop {
		return nil
	}
	return f.reply(q, a, udp)
}

func (f *fakeDNS) serveUDP() {
	buf := make([]byte, 1500)
	for {
		n, from, err := f.udp.ReadFromUDP(buf)
		if err != nil {
			return
		}
		q := append([]byte(nil), buf[:n]...)
		go func() {
			if r := f.handle(q, true); r != nil {
				f.udp.WriteToUDP(r, from)
			}
		}()
	}
}

func (f *fakeDNS) serveTCP() {
	for {
		c, err := f.tcp.Accept()
		if err != nil {
			return
		}
		go func(c net.Conn) {
			defer c.Close()
			var lb [2]byte
			if _, err := readFull(c, lb[:]); err != nil {
				return
			}
			q := make([]byte, binary.BigEndian.Uint16(lb[:]))
			if _, err := readFull(c, q); err != nil {
				return
			}
			r := f.handle(q, false)
			if r == nil {
				return
			}
			out := make([]byte, 2, 2+len(r))
			binary.BigEndian.PutUint16(out, uint16(len(r)))
			c.Write(append(out, r...))
		}(c)
	}
}

// fakeZone: 이름 → A, IP(역조회 이름) → PTR 표로 만든 핸들러. 없는 이름은 NXDOMAIN, "drop:" 접두 이름은 무응답.
type fakeZone struct {
	a   map[string][]string
	ptr map[string][]string // "9.0.0.10.in-addr.arpa" → names
	hit sync.Map            // 질의된 이름 기록

	delay  time.Duration // 응답 지연 (동시 질의가 겹쳐 보이게)
	cur    int32
	maxCur int32 // 이 zone 을 쓰는 모든 서버에서 동시에 처리 중이던 질의 최대치
}

func (z *fakeZone) handler(name string, qtype uint16) fakeAns {
	z.hit.Store(name, true)
	if z.delay > 0 {
		c := atomic.AddInt32(&z.cur, 1)
		for {
			m := atomic.LoadInt32(&z.maxCur)
			if c <= m || atomic.CompareAndSwapInt32(&z.maxCur, m, c) {
				break
			}
		}
		time.Sleep(z.delay)
		atomic.AddInt32(&z.cur, -1)
	}
	if qtype == dnsTypePTR {
		if p, ok := z.ptr[name]; ok {
			return fakeAns{ptr: p}
		}
		return fakeAns{nx: true}
	}
	if v, ok := z.a[name]; ok {
		if len(v) == 1 && v[0] == "drop" {
			return fakeAns{drop: true}
		}
		if len(v) == 1 && v[0] == "servfail" {
			return fakeAns{rcode: 2}
		}
		return fakeAns{a: v}
	}
	return fakeAns{nx: true}
}

func ptrName(ip string) string {
	v := net.ParseIP(ip).To4()
	return fmt.Sprintf("%d.%d.%d.%d.in-addr.arpa", v[3], v[2], v[1], v[0])
}

// useConf: 임시 resolv.conf 를 쓰도록 바꾸고, 테스트용 옵션을 줄인 뒤 끝나면 되돌린다
func useConf(t *testing.T, content string) {
	t.Helper()
	p := filepath.Join(t.TempDir(), "resolv.conf")
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	oc, ot, od := dnsResolvConf, dnsQueryTimeout, dnsLocalDomain
	dnsResolvConf, dnsQueryTimeout = p, 150*time.Millisecond
	dnsLocalDomain = func() string { return "" }
	t.Cleanup(func() { dnsResolvConf, dnsQueryTimeout, dnsLocalDomain = oc, ot, od })
}

func nsLines(fs ...*fakeDNS) string {
	var sb strings.Builder
	for _, f := range fs {
		sb.WriteString("nameserver " + f.addr() + "\n")
	}
	return sb.String()
}

func byHost(rep DNSReport, h string) DNSHostResult {
	for _, r := range rep.Hosts {
		if r.Host == h {
			return r
		}
	}
	return DNSHostResult{Host: "(없음) " + h}
}

// ---------------------------------------------------------------- 단위 테스트

func TestReadDNSConf(t *testing.T) {
	useConf(t, "# 주석\n; 주석\nnameserver 10.0.0.1\nnameserver 10.0.0.1\nnameserver 10.0.0.2\nnameserver fe80::1%eth0\nnameserver 10.0.0.4\n"+
		"nameserver 이상한값\ndomain a.example\nsearch b.example c.example\noptions timeout:1 ndots:2 rotate\n")
	c := readDNSConf(dnsResolvConf)
	if want := []string{"10.0.0.1:53", "10.0.0.2:53", "[fe80::1%eth0]:53"}; !reflect.DeepEqual(c.servers, want) {
		t.Fatalf("servers = %v, want %v", c.servers, want)
	}
	if want := []string{"b.example", "c.example"}; !reflect.DeepEqual(c.search, want) {
		t.Fatalf("search = %v (나중 줄이 이겨야 함)", c.search)
	}
	if c.ndots != 2 {
		t.Fatalf("ndots = %d", c.ndots)
	}
	if got := dnsParseServer("[::1]:5353"); got != "[::1]:5353" {
		t.Fatalf("host:port = %q", got)
	}
	// 파일 없음
	dnsResolvConf = filepath.Join(t.TempDir(), "없음")
	if c := readDNSConf(dnsResolvConf); len(c.servers) != 0 || c.ndots != 1 {
		t.Fatalf("파일 없음: %+v", c)
	}
	// search/domain 이 없으면 호스트명의 도메인
	useConf(t, "nameserver 10.0.0.1\n")
	dnsLocalDomain = func() string { return "lab.local" }
	if c := readDNSConf(dnsResolvConf); !reflect.DeepEqual(c.search, []string{"lab.local"}) {
		t.Fatalf("기본 도메인 = %v", c.search)
	}
}

func TestDNSCandidates(t *testing.T) {
	c := dnsConf{search: []string{"example.com", "lab.local"}, ndots: 1}
	cases := []struct {
		host string
		want []string
	}{
		{"svr0001", []string{"svr0001.example.com", "svr0001.lab.local", "svr0001"}}, // 점 0 < ndots → search 먼저
		{"svr0001.dc1", []string{"svr0001.dc1", "svr0001.dc1.example.com", "svr0001.dc1.lab.local"}},
		{"svr0001.example.com.", []string{"svr0001.example.com"}}, // 끝 점 = 절대 이름
		{"  ", nil},
	}
	for _, tc := range cases {
		if got := dnsCandidates(tc.host, c); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%q: %v, want %v", tc.host, got, tc.want)
		}
	}
	c.ndots = 2
	if got := dnsCandidates("a.b", c); got[0] != "a.b.example.com" {
		t.Errorf("ndots=2: %v", got)
	}
	if got := dnsCandidates("svr", dnsConf{ndots: 1}); !reflect.DeepEqual(got, []string{"svr"}) {
		t.Errorf("search 없음: %v", got)
	}
}

func TestDNSParseCompressedPTR(t *testing.T) {
	// 질문 "1.2.3.4.in-addr.arpa" (in-addr 라벨은 오프셋 20), 답의 rdata = "svr0005" + 포인터(20)
	q, _ := dnsBuildQuery(7, "1.2.3.4.in-addr.arpa", dnsTypePTR)
	msg := append([]byte(nil), q...)
	binary.BigEndian.PutUint16(msg[2:], 0x8180)
	binary.BigEndian.PutUint16(msg[6:], 1)
	rd := append([]byte{7}, "svr0005"...)
	rd = append(rd, 0xc0, 20)
	msg = append(msg, 0xc0, 0x0c, 0, 12, 0, 1, 0, 0, 0, 60, 0, byte(len(rd)))
	msg = append(msg, rd...)
	rec, tc, err := dnsParse(msg)
	if err != nil || tc || len(rec.ptr) != 1 || rec.ptr[0] != "svr0005.in-addr.arpa" {
		t.Fatalf("rec=%+v tc=%v err=%v", rec, tc, err)
	}
	// 자기를 가리키는 포인터는 오류 (무한 루프 방지)
	bad := append([]byte(nil), q[:12]...)
	bad[5], bad[7] = 1, 0
	bad = append(bad, 0xc0, 12, 0, 1, 0, 1)
	if _, _, err := dnsParse(bad); err == nil {
		t.Fatal("포인터 루프를 거부해야 함")
	}
	if _, _, err := dnsParse([]byte{1, 2, 3}); err == nil {
		t.Fatal("짧은 응답을 거부해야 함")
	}
}

func TestDNSJudge(t *testing.T) {
	A := func(ips ...string) dnsAns { return dnsAns{kind: dnsGotA, ips: ips} }
	NX := dnsAns{kind: dnsGotNX}
	F := dnsAns{kind: dnsFail}
	cases := []struct {
		name string
		reg  string
		ans  []dnsAns
		want DNSStatus
	}{
		{"일치", "1.1.1.1", []dnsAns{A("1.1.1.1"), A("1.1.1.1"), F}, DNSMatch},
		{"불일치(실패 서버 무시)", "1.1.1.1", []dnsAns{A("1.1.1.2"), F, A("1.1.1.2")}, DNSMismatch},
		{"서버 간 다름", "1.1.1.1", []dnsAns{A("1.1.1.1"), A("1.1.1.2")}, DNSAmbiguous},
		{"A 여러 개", "1.1.1.1", []dnsAns{A("1.1.1.1", "1.1.1.2")}, DNSAmbiguous},
		{"A 와 NX 섞임", "1.1.1.1", []dnsAns{A("1.1.1.1"), NX}, DNSAmbiguous},
		{"미등록", "1.1.1.1", []dnsAns{NX, F, NX}, DNSUnregistered},
		{"전부 실패", "1.1.1.1", []dnsAns{F, F}, DNSNoAnswer},
		{"IP 표기 공백", " 1.1.1.1 ", []dnsAns{A("1.1.1.1")}, DNSMatch},
	}
	for _, tc := range cases {
		if st, _, _, _ := dnsJudge(tc.reg, tc.ans); st != tc.want {
			t.Errorf("%s: %v, want %v", tc.name, st, tc.want)
		}
	}
}

// ---------------------------------------------------------------- 가짜 서버 통합 테스트

func TestCheckDNSStatuses(t *testing.T) {
	// s1·s2 는 같은 zone, s3 은 svr0004 에서 다른 답을 주고 svr0006 은 모두 무응답
	zone := func(extra map[string][]string) *fakeZone {
		z := &fakeZone{a: map[string][]string{
			"svr0001.example.com": {"10.0.0.1"},
			"svr0002.example.com": {"10.0.0.2"},
			"svr0004.example.com": {"10.0.0.4"},
			"svr0005.example.com": {"10.0.0.5", "10.0.0.55"},
			"svr0006.example.com": {"drop"},
			"svr0007.example.com": {"servfail"},
		}}
		for k, v := range extra {
			z.a[k] = v
		}
		return z
	}
	z1, z2, z3 := zone(nil), zone(nil), zone(map[string][]string{"svr0004.example.com": {"10.0.0.44"}})
	s1, s2, s3 := newFakeDNS(t, false, z1.handler), newFakeDNS(t, false, z2.handler), newFakeDNS(t, false, z3.handler)
	useConf(t, nsLines(s1, s2, s3)+"search example.com\n")

	rep := CheckDNS([]DNSEntry{
		{"svr0001", "10.0.0.1"},             // 일치
		{"SVR0002", "10.0.0.20"},            // 불일치 (대소문자 무시)
		{"svr0003", "10.0.0.3"},             // 미등록
		{"svr0004", "10.0.0.4"},             // 서버 간 불일치 → 판단 불가
		{"svr0005", "10.0.0.5"},             // A 여러 개 → 판단 불가
		{"svr0006", "10.0.0.6"},             // 전부 무응답
		{"svr0007", "10.0.0.7"},             // SERVFAIL → 조회 실패
		{"svr0001.example.com", "10.0.0.1"}, // FQDN 도 일치
	})
	if !rep.Enabled || rep.Reason != "" {
		t.Fatalf("Enabled=%v Reason=%q", rep.Enabled, rep.Reason)
	}
	if len(rep.Servers) != 3 || len(rep.Hosts) != 8 {
		t.Fatalf("servers=%v hosts=%d", rep.Servers, len(rep.Hosts))
	}
	want := []DNSStatus{DNSMatch, DNSMismatch, DNSUnregistered, DNSAmbiguous, DNSAmbiguous, DNSNoAnswer, DNSNoAnswer, DNSMatch}
	for i, w := range want {
		if rep.Hosts[i].Status != w {
			t.Errorf("%s: %s, want %s", rep.Hosts[i].Host, rep.Hosts[i].Status.Label(), w.Label())
		}
	}
	if h := rep.Hosts[1]; h.DNSIP != "10.0.0.2" || h.Host != "SVR0002" {
		t.Errorf("불일치 DNSIP/Host: %+v", h)
	}
	if h := rep.Hosts[3]; !reflect.DeepEqual(h.DNSIPs, []string{"10.0.0.4", "10.0.0.44"}) || h.DNSIP != "" {
		t.Errorf("서버 간 불일치 DNSIPs: %+v", h)
	}
	if h := rep.Hosts[4]; !reflect.DeepEqual(h.DNSIPs, []string{"10.0.0.5", "10.0.0.55"}) {
		t.Errorf("A 여러 개 DNSIPs: %+v", h)
	}
	if got := len(rep.Mismatches()); got != 1 {
		t.Errorf("Mismatches = %d", got)
	}
	if got := len(rep.Problems()); got != 6 {
		t.Errorf("Problems = %d", got)
	}
	if !reflect.DeepEqual(DNSMismatchHosts(rep), []string{"SVR0002"}) {
		t.Errorf("DNSMismatchHosts = %v", DNSMismatchHosts(rep))
	}
	// svr0003 은 search 도메인 붙인 이름 → 이름 그대로 순서로 물었어야 한다
	if _, ok := z1.hit.Load("svr0003.example.com"); !ok {
		t.Error("search 도메인을 붙여 조회하지 않음")
	}
	if _, ok := z1.hit.Load("svr0003"); !ok {
		t.Error("search 로 못 찾았을 때 짧은 이름 그대로 조회하지 않음")
	}
	if _, ok := z1.hit.Load("svr0001"); ok {
		t.Error("search 로 찾았는데 짧은 이름을 또 조회함")
	}
}

func TestCheckDNSSearchPreferredOverBareName(t *testing.T) {
	z := &fakeZone{a: map[string][]string{
		"svr0001.example.com": {"10.0.0.1"},
		"svr0001":             {"10.9.9.9"}, // 이름 그대로는 뒤 순서
	}}
	s := newFakeDNS(t, false, z.handler)
	useConf(t, nsLines(s)+"domain example.com\n")
	rep := CheckDNS([]DNSEntry{{"svr0001", "10.0.0.1"}})
	if rep.Hosts[0].Status != DNSMatch {
		t.Fatalf("%+v", rep.Hosts[0])
	}
	// ndots:0 이면 이름 그대로 먼저
	useConf(t, nsLines(s)+"domain example.com\noptions ndots:0\n")
	rep = CheckDNS([]DNSEntry{{"svr0001", "10.0.0.1"}})
	if rep.Hosts[0].Status != DNSMismatch || rep.Hosts[0].DNSIP != "10.9.9.9" {
		t.Fatalf("ndots:0 %+v", rep.Hosts[0])
	}
}

func TestCheckDNSDuplicateWarnings(t *testing.T) {
	z := &fakeZone{
		a: map[string][]string{
			"svr0001.x.test": {"10.0.1.2"}, // 등록값 10.0.1.1 과 다름 → 불일치
			"svr0005.x.test": {"10.0.1.5"},
			"svr0009.x.test": {"10.0.1.1"}, // DNS 가 10.0.1.1 을 준다
			"svr0011.x.test": {"10.0.2.2"},
			"svr0020.x.test": {"10.0.3.2"},
			"svr0030.x.test": {"10.0.4.2"},
			"svr0031.x.test": {"10.0.4.31"},
			"svr0040.x.test": {"10.0.5.1"},
		},
		ptr: map[string][]string{
			ptrName("10.0.3.1"): {"SVR0021.X.test."}, // PTR 이 다른 이름
			ptrName("10.0.4.1"): {"svr0030.x.test."}, // PTR 이 자기 이름 → 경고 없음
		},
	}
	s1 := newFakeDNS(t, false, z.handler)
	s2 := newFakeDNS(t, false, z.handler)
	useConf(t, nsLines(s1, s2)+"search x.test\n")

	rep := CheckDNS([]DNSEntry{
		{"svr0001", "10.0.1.1"},  // DNS 합의 10.0.1.2 → 불일치. 10.0.1.1 은 svr0005(목록)·svr0009(DNS) 것
		{"svr0005", "10.0.1.1"},  // 목록에서 같은 IP 를 쓰는 다른 호스트
		{"svr0009", "10.0.1.99"}, // DNS 가 10.0.1.1
		{"svr0011", "10.0.2.1"},  // 경고 없음 (단순 불일치)
		{"svr0020", "10.0.3.1"},  // PTR → svr0021
		{"svr0030", "10.0.4.1"},  // PTR 이 자기 자신 → 경고 없음
		{"svr0031", "10.0.4.30"}, // 불일치지만 10.0.4.30 은 아무도 안 씀
		{"svr0040", "10.0.5.1"},  // 일치 — PTR 질의 대상이 아님
	})
	get := func(h string) []string { return byHost(rep, h).Warnings }
	if byHost(rep, "svr0001").Status != DNSMismatch {
		t.Fatalf("svr0001: %+v", byHost(rep, "svr0001"))
	}
	wantW1 := []string{"등록IP 10.0.1.1 은 svr0005 의 IP (목록)", "등록IP 10.0.1.1 은 svr0009 의 IP (DNS)"}
	if !reflect.DeepEqual(get("svr0001"), wantW1) {
		t.Errorf("svr0001 경고 = %q\n  want %q", get("svr0001"), wantW1)
	}
	// svr0005: DNS 합의 10.0.1.5 == 등록 10.0.1.1? 아니므로 불일치. 목록에서는 svr0001 이 같은 IP.
	if byHost(rep, "svr0005").Status != DNSMismatch {
		t.Fatalf("svr0005: %+v", byHost(rep, "svr0005"))
	}
	if w := get("svr0005"); !reflect.DeepEqual(w, []string{"등록IP 10.0.1.1 은 svr0001 의 IP (목록)", "등록IP 10.0.1.1 은 svr0009 의 IP (DNS)"}) {
		t.Errorf("svr0005 경고 = %q", w)
	}
	if w := get("svr0011"); len(w) != 0 {
		t.Errorf("svr0011 경고 = %q, 없어야 함", w)
	}
	if w := get("svr0020"); !reflect.DeepEqual(w, []string{"등록IP 10.0.3.1 은 svr0021 의 IP (PTR)"}) {
		t.Errorf("svr0020 경고 = %q", w)
	}
	if w := get("svr0030"); len(w) != 0 {
		t.Errorf("svr0030(PTR 자기 자신) 경고 = %q", w)
	}
	if w := get("svr0031"); len(w) != 0 {
		t.Errorf("svr0031 경고 = %q", w)
	}
	// PTR 은 불일치 등록 IP 만 묻는다: 일치 호스트의 IP 는 PTR 질의 대상이 아님
	if _, ok := z.hit.Load(ptrName("10.0.5.1")); ok {
		t.Error("일치 호스트의 IP 에 대해 PTR 을 물음")
	}
	if _, ok := z.hit.Load(ptrName("10.0.3.1")); !ok {
		t.Error("불일치 등록 IP 의 PTR 을 묻지 않음")
	}
}

func TestCheckDNSAllFailDisabled(t *testing.T) {
	dead := func(string, uint16) fakeAns { return fakeAns{drop: true} }
	s1, s2 := newFakeDNS(t, false, dead), newFakeDNS(t, false, dead)
	useConf(t, nsLines(s1, s2))
	start := time.Now()
	rep := CheckDNS([]DNSEntry{{"a", "1.1.1.1"}, {"b", "1.1.1.2"}})
	if rep.Enabled || rep.Reason != DNSDisabledReason {
		t.Fatalf("Enabled=%v Reason=%q", rep.Enabled, rep.Reason)
	}
	if d := time.Since(start); d > 3*time.Second {
		t.Errorf("너무 오래 걸림: %v", d)
	}
	for _, h := range rep.Hosts {
		if h.Status != DNSNoAnswer {
			t.Errorf("%s: %v", h.Host, h.Status)
		}
	}
	// NXDOMAIN 만 있어도 A 를 한 번도 못 받았으니 점검 생략
	nx := func(string, uint16) fakeAns { return fakeAns{nx: true} }
	s3 := newFakeDNS(t, false, nx)
	useConf(t, nsLines(s3))
	if rep := CheckDNS([]DNSEntry{{"a", "1.1.1.1"}}); rep.Enabled || rep.Hosts[0].Status != DNSUnregistered {
		t.Errorf("NX 만: Enabled=%v %+v", rep.Enabled, rep.Hosts[0])
	}
	// nameserver 없음 / 파일 없음 / 대상 없음
	useConf(t, "# nameserver 없음\nsearch x\n")
	if rep := CheckDNS([]DNSEntry{{"a", "1.1.1.1"}}); rep.Enabled || rep.Reason != DNSDisabledReason || rep.Detail != "nameserver 없음" {
		t.Errorf("nameserver 없음: %+v", rep)
	}
	dnsResolvConf = filepath.Join(t.TempDir(), "없음")
	if rep := CheckDNS([]DNSEntry{{"a", "1.1.1.1"}}); rep.Enabled {
		t.Errorf("파일 없음인데 Enabled")
	}
	if rep := CheckDNS(nil); rep.Enabled || len(rep.Hosts) != 0 {
		t.Errorf("빈 목록: %+v", rep)
	}
}

func TestCheckDNSOneServerDown(t *testing.T) {
	z := &fakeZone{a: map[string][]string{}}
	var entries []DNSEntry
	for i := 1; i <= 150; i++ {
		n := fmt.Sprintf("svr%04d", i)
		ip := fmt.Sprintf("10.1.%d.%d", i/200, i%200+1)
		z.a[n+".x.test"] = []string{ip}
		entries = append(entries, DNSEntry{n, ip})
	}
	good1 := newFakeDNS(t, false, z.handler)
	good2 := newFakeDNS(t, false, z.handler)
	down := newFakeDNS(t, false, func(string, uint16) fakeAns { return fakeAns{drop: true} })
	useConf(t, nsLines(down, good1, good2)+"search x.test\n")
	rep := CheckDNS(entries)
	if !rep.Enabled {
		t.Fatal("서버 1대가 죽어도 점검은 되어야 함")
	}
	for _, h := range rep.Hosts {
		if h.Status != DNSMatch {
			t.Fatalf("%s: %v", h.Host, h.Status)
		}
	}
	// 죽은 서버는 처음 몇 건 이후로 건너뛴다 (150대 × 2시도 = 300 이 아니라 동시 32 × 2시도 안팎)
	if q := atomic.LoadInt32(&down.queries); q > 2*int32(dnsConcurrency)+10 {
		t.Errorf("죽은 서버에 %d 건이나 질의함", q)
	}
}

func TestCheckDNSTCPFallbackAndRetry(t *testing.T) {
	var n int32
	h := func(name string, qtype uint16) fakeAns {
		switch name {
		case "big.x.test":
			return fakeAns{tc: true, a: []string{"10.0.0.1", "10.0.0.2", "10.0.0.3"}}
		case "retry.x.test": // 첫 질의는 버리고 두 번째에 답 — 재시도 1회
			if atomic.AddInt32(&n, 1) == 1 {
				return fakeAns{drop: true}
			}
			return fakeAns{a: []string{"10.0.0.9"}}
		}
		return fakeAns{nx: true}
	}
	s := newFakeDNS(t, true, h)
	useConf(t, nsLines(s)+"search x.test\n")
	rep := CheckDNS([]DNSEntry{{"big", "10.0.0.1"}, {"retry", "10.0.0.9"}})
	if rep.Hosts[0].Status != DNSAmbiguous || len(rep.Hosts[0].DNSIPs) != 3 {
		t.Errorf("TCP 폴백: %+v", rep.Hosts[0])
	}
	if rep.Hosts[1].Status != DNSMatch {
		t.Errorf("재시도: %+v", rep.Hosts[1])
	}
}

func TestCheckDNSNoSystemHosts(t *testing.T) {
	// /etc/hosts·getent 를 쓰지 않으므로 서버가 모르는 이름은 hosts 에 있어도 미등록 (localhost 가 대표적)
	z := &fakeZone{a: map[string][]string{"other.x.test": {"10.0.0.1"}}}
	s := newFakeDNS(t, false, z.handler)
	useConf(t, nsLines(s)+"search x.test\n")
	rep := CheckDNS([]DNSEntry{{"localhost", "127.0.0.1"}, {"other", "10.0.0.1"}})
	if rep.Hosts[0].Status != DNSUnregistered {
		t.Errorf("localhost: %+v", rep.Hosts[0])
	}
}

func TestCheckDNSScale1000(t *testing.T) {
	z := &fakeZone{a: map[string][]string{}, delay: 5 * time.Millisecond}
	var entries []DNSEntry
	for i := 0; i < 1000; i++ {
		n := fmt.Sprintf("svr%04d", i)
		ip := fmt.Sprintf("10.%d.%d.%d", 2+i/65536, i/256%256, i%256)
		z.a[n+".x.test"] = []string{ip}
		if i%10 == 0 {
			entries = append(entries, DNSEntry{n, fmt.Sprintf("10.99.%d.%d", i/256, i%256)}) // 100대 불일치
		} else {
			entries = append(entries, DNSEntry{n, ip})
		}
	}
	s1, s2, s3 := newFakeDNS(t, false, z.handler), newFakeDNS(t, false, z.handler), newFakeDNS(t, false, z.handler)
	useConf(t, nsLines(s1, s2, s3)+"search x.test\n")
	dnsQueryTimeout = 2 * time.Second
	start := time.Now()
	rep := CheckDNS(entries)
	el := time.Since(start)
	t.Logf("1000대 점검: %v (서버당 질의 %d, 최대 동시 %d)", el, atomic.LoadInt32(&s1.queries), atomic.LoadInt32(&z.maxCur))
	if !rep.Enabled || len(rep.Mismatches()) != 100 {
		t.Fatalf("Enabled=%v mismatch=%d", rep.Enabled, len(rep.Mismatches()))
	}
	if el > 10*time.Second {
		t.Errorf("1000대가 %v — 10초 목표 초과", el)
	}
	if m := atomic.LoadInt32(&z.maxCur); m > int32(dnsConcurrency) {
		t.Errorf("동시 질의 %d > 제한 %d", m, dnsConcurrency)
	} else if m < 8 {
		t.Errorf("병렬 질의가 거의 일어나지 않음 (최대 동시 %d)", m)
	}
}

// ---------------------------------------------------------------- 표·교체

func TestRenderDNSTableAndApply(t *testing.T) {
	rep := DNSReport{Enabled: true, Hosts: []DNSHostResult{
		{Host: "svr0001", IP: "1.1.1.1", Status: DNSMismatch, DNSIP: "1.1.1.2", Warnings: []string{"등록IP 1.1.1.1 은 svr0005 의 IP (DNS)"}},
		{Host: "서버긴이름-02", IP: "10.10.10.10", Status: DNSAmbiguous, DNSIPs: []string{"1.1.1.3", "1.1.1.4"}, Note: "판단 불가: 서버 간 답이 다름"},
		{Host: "svr0003", IP: "1.1.1.5", Status: DNSUnregistered, Note: "미등록"},
		{Host: "svr0004", IP: "1.1.1.6", Status: DNSMatch, DNSIP: "1.1.1.6"},
	}}
	out := RenderDNSTable(rep.Problems())
	lines := strings.Split(out, "\n")
	if len(lines) != 4 {
		t.Fatalf("줄 수 %d:\n%s", len(lines), out)
	}
	if !strings.HasPrefix(lines[0], "hostname") || !strings.Contains(lines[0], "등록IP") || !strings.HasSuffix(lines[0], "비고") {
		t.Errorf("헤더: %q", lines[0])
	}
	// 열 시작 위치(표시 폭)가 모든 줄에서 같아야 한다
	col := func(l string, sub string) int {
		i := strings.Index(l, sub)
		if i < 0 {
			t.Fatalf("%q 에 %q 없음", l, sub)
		}
		return strWidth(l[:i])
	}
	if col(lines[0], "등록IP") != col(lines[1], "1.1.1.1") || col(lines[0], "등록IP") != col(lines[2], "10.10.10.10") {
		t.Errorf("등록IP 열 불일치:\n%s", out)
	}
	if col(lines[0], "DNS IP") != col(lines[1], "1.1.1.2") || col(lines[0], "DNS IP") != col(lines[2], "1.1.1.3,1.1.1.4") {
		t.Errorf("DNS IP 열 불일치:\n%s", out)
	}
	if !strings.Contains(lines[1], "⚠ 등록IP 1.1.1.1 은 svr0005 의 IP (DNS)") || !strings.Contains(lines[2], "⚠ 판단 불가") || strings.Contains(lines[3], "⚠") {
		t.Errorf("비고:\n%s", out)
	}
	if strings.ContainsAny(out, "\x1b") || strings.HasSuffix(out, "\n") || strings.HasSuffix(lines[3], " ") {
		t.Errorf("색/여분 문자:\n%q", out)
	}
	if RenderDNSTable(nil) != "" {
		t.Error("빈 표는 빈 문자열")
	}

	entries := []DNSEntry{{"svr0001", "1.1.1.1"}, {"서버긴이름-02", "10.10.10.10"}, {"svr0003", "1.1.1.5"}, {"SVR0001", "1.1.1.1"}}
	// hosts 가 비면 아무것도 안 바꾼다
	if c := ApplyDNS(entries, rep, nil); len(c) != 0 || entries[0].IP != "1.1.1.1" {
		t.Fatalf("hosts 없음: %q", c)
	}
	// Ambiguous·미등록은 hosts 에 넣어도 건드리지 않는다. 이름 비교는 대소문자 무시 → SVR0001 도 같이 바뀐다
	got := ApplyDNS(entries, rep, []string{"svr0001", "서버긴이름-02", "svr0003", "없는호스트"})
	want := []string{"[DNS] svr0001 1.1.1.1 -> 1.1.1.2", "[DNS] SVR0001 1.1.1.1 -> 1.1.1.2"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("changed = %q", got)
	}
	if entries[0].IP != "1.1.1.2" || entries[1].IP != "10.10.10.10" || entries[2].IP != "1.1.1.5" || entries[3].IP != "1.1.1.2" {
		t.Errorf("entries = %+v", entries)
	}
	// 이미 DNS 값이면 다시 호출해도 변화 없음 (멱등)
	if c := ApplyDNS(entries, rep, DNSMismatchHosts(rep)); len(c) != 0 {
		t.Errorf("재호출: %q", c)
	}
}
