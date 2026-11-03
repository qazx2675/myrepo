// dnscheck.go - DNS 점검 판정 엔진 (§3). 화면(a/n/s/q)은 호출하는 쪽이 만든다.
//
// /etc/resolv.conf 의 nameserver(최대 3)에 A 레코드를 직접 묻는다. /etc/hosts·nsswitch·getent 는 쓰지 않는다.
// Go 의 net.Resolver 는 /etc/hosts·nsswitch.conf 를 먼저 보고(PreferGo 여도) resolv.conf 경로·서버 고정이 안 되어
// 서버별 답을 비교할 수 없으므로, 표준 라이브러리만으로 최소 DNS 클라이언트(UDP, 잘림 시 TCP)를 직접 구현했다.
// 검색 도메인(search/domain/ndots)은 glibc·nslookup 과 같은 순서로 직접 붙여 조회한다.
package main

import (
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// DNSDisabledReason: 점검을 생략할 때 사용자에게 보이는 한 줄
const DNSDisabledReason = "DNS 조회 불가 — 점검 생략"

var (
	dnsResolvConf   = "/etc/resolv.conf" // 테스트에서 바꾼다
	dnsQueryTimeout = 2 * time.Second    // 질의 1회(시도)당 제한
	dnsAttempts     = 2                  // 시도 횟수 = 최초 1 + 재시도 1
	dnsConcurrency  = 32                 // 동시 질의 수
	dnsMaxServers   = 3                  // resolv.conf 에서 쓰는 nameserver 수
	dnsDeadAfter    = 3                  // 응답을 한 번도 못 받고 이만큼 실패하면 그 서버는 건너뜀
	dnsLocalDomain  = dnsHostnameDomain  // resolv.conf 에 search/domain 이 없을 때 쓰는 기본 도메인(glibc 동작), 테스트에서 바꾼다
)

// DNSEntry: 등록값 (hostname, IP)
type DNSEntry struct {
	Host string
	IP   string
}

// DNSStatus: 호스트별 판정
type DNSStatus int

const (
	DNSMatch        DNSStatus = iota // 일치: 응답한 서버 답이 모두 같고 A 1개 == 등록 IP
	DNSMismatch                      // 불일치: 합의된 A 1개 != 등록 IP (DNSIP 채움) — 교체 대상
	DNSAmbiguous                     // 판단 불가: 서버 간 답이 다르거나 A 가 여러 개 — 경고만, 교체 대상 아님
	DNSUnregistered                  // 미등록: 응답한 서버가 모두 NXDOMAIN(또는 A 없음)
	DNSNoAnswer                      // 모든 서버 시간 초과/오류 — 해당 호스트 판단 불가
)

// Label: 화면용 한글 이름
func (s DNSStatus) Label() string {
	switch s {
	case DNSMatch:
		return "일치"
	case DNSMismatch:
		return "불일치"
	case DNSAmbiguous:
		return "판단 불가"
	case DNSUnregistered:
		return "미등록"
	}
	return "조회 실패"
}

// DNSHostResult: 호스트 1건의 결과 (DNSReport.Hosts 는 입력 순서와 같다)
type DNSHostResult struct {
	Host     string
	IP       string // 등록 IP
	Status   DNSStatus
	DNSIP    string   // Match·Mismatch 일 때 합의된 DNS IP
	DNSIPs   []string // 서버들이 준 A 전체(중복 제거·정렬) — Ambiguous 표시용
	Note     string   // Ambiguous/Unregistered/NoAnswer 사유 한 줄
	Warnings []string // Mismatch 의 중복 경고: "등록IP x 은 y 의 IP (목록|DNS|PTR)"
}

// DNSReport: 점검 전체 결과
type DNSReport struct {
	Enabled bool     // 1대라도 어느 서버에서 A 레코드를 받았으면 true
	Reason  string   // Enabled=false 일 때 DNSDisabledReason
	Detail  string   // Enabled=false 의 구체 원인 (로그용)
	Servers []string // 질의한 nameserver (기본 포트 53 은 생략 표기)
	Hosts   []DNSHostResult
}

// Mismatches: 불일치 호스트 (입력 순서)
func (r DNSReport) Mismatches() []DNSHostResult {
	var out []DNSHostResult
	for _, h := range r.Hosts {
		if h.Status == DNSMismatch {
			out = append(out, h)
		}
	}
	return out
}

// Problems: 일치가 아닌 모든 호스트 (표 출력용, 입력 순서)
func (r DNSReport) Problems() []DNSHostResult {
	var out []DNSHostResult
	for _, h := range r.Hosts {
		if h.Status != DNSMatch {
			out = append(out, h)
		}
	}
	return out
}

// DNSMismatchHosts: 불일치 호스트 이름들 — `a 모두 DNS 로` 에서 ApplyDNS 에 그대로 넘긴다
func DNSMismatchHosts(rep DNSReport) []string {
	var out []string
	for _, h := range rep.Mismatches() {
		out = append(out, h.Host)
	}
	return out
}

// ---------------------------------------------------------------- resolv.conf

type dnsConf struct {
	servers []string // "ip:port"
	search  []string
	ndots   int
}

func dnsHostnameDomain() string {
	h, err := os.Hostname()
	if err != nil {
		return ""
	}
	if i := strings.Index(h, "."); i >= 0 && i+1 < len(h) {
		return h[i+1:]
	}
	return ""
}

// dnsParseServer: "1.2.3.4" | "fe80::1%eth0" | "1.2.3.4:5353" | "[::1]:5353" → "host:port". 해석 못 하면 "".
func dnsParseServer(s string) string {
	if host, port, err := net.SplitHostPort(s); err == nil {
		if net.ParseIP(strings.SplitN(host, "%", 2)[0]) != nil && port != "" {
			return net.JoinHostPort(host, port)
		}
		return ""
	}
	s = strings.TrimSuffix(strings.TrimPrefix(s, "["), "]")
	if net.ParseIP(strings.SplitN(s, "%", 2)[0]) != nil {
		return net.JoinHostPort(s, "53")
	}
	return ""
}

func readDNSConf(path string) dnsConf {
	c := dnsConf{ndots: 1}
	data, err := os.ReadFile(path)
	if err != nil {
		return c
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || line[0] == '#' || line[0] == ';' {
			continue
		}
		f := strings.Fields(line)
		switch f[0] {
		case "nameserver":
			if len(f) < 2 || len(c.servers) >= dnsMaxServers {
				continue
			}
			addr := dnsParseServer(f[1])
			dup := false
			for _, s := range c.servers {
				if s == addr {
					dup = true
				}
			}
			if addr != "" && !dup {
				c.servers = append(c.servers, addr)
			}
		case "domain": // search 와 같은 설정이며 나중 줄이 이긴다
			if len(f) >= 2 {
				c.search = []string{f[1]}
			}
		case "search":
			if len(f) >= 2 {
				c.search = append([]string(nil), f[1:]...)
				if len(c.search) > 6 {
					c.search = c.search[:6]
				}
			}
		case "options":
			for _, o := range f[1:] {
				if strings.HasPrefix(o, "ndots:") {
					if n, err := strconv.Atoi(o[len("ndots:"):]); err == nil && n >= 0 {
						if n > 15 {
							n = 15
						}
						c.ndots = n
					}
				}
			}
		}
	}
	if len(c.search) == 0 {
		if d := dnsLocalDomain(); d != "" {
			c.search = []string{d}
		}
	}
	return c
}

// dnsCandidates: 질의할 이름 순서. glibc/nslookup 과 같다 —
// 끝이 '.' 이면 그 이름만, 점 개수 >= ndots 면 이름 그대로 먼저 → search 도메인, 아니면 search 도메인 먼저 → 그대로.
func dnsCandidates(host string, c dnsConf) []string {
	h := strings.TrimSpace(host)
	if h == "" {
		return nil
	}
	if strings.HasSuffix(h, ".") {
		h = strings.TrimRight(h, ".")
		if h == "" {
			return nil
		}
		return []string{h}
	}
	var withSearch []string
	for _, s := range c.search {
		s = strings.Trim(s, ".")
		if s != "" {
			withSearch = append(withSearch, h+"."+s)
		}
	}
	if strings.Count(h, ".") >= c.ndots {
		return append([]string{h}, withSearch...)
	}
	return append(withSearch, h)
}

// ---------------------------------------------------------------- DNS 와이어 포맷

const (
	dnsTypeA   = 1
	dnsTypePTR = 12
)

type dnsProtoError struct{ msg string }

func (e *dnsProtoError) Error() string { return e.msg }

type dnsRecords struct {
	a   []string // A 레코드 (IPv4 문자열)
	ptr []string // PTR 대상 이름
}

func dnsBuildQuery(id uint16, name string, qtype uint16) ([]byte, error) {
	name = strings.TrimSuffix(name, ".")
	if name == "" || len(name) > 253 {
		return nil, errors.New("잘못된 이름")
	}
	b := make([]byte, 12, 12+len(name)+6)
	binary.BigEndian.PutUint16(b[0:], id)
	binary.BigEndian.PutUint16(b[2:], 0x0100) // RD
	binary.BigEndian.PutUint16(b[4:], 1)      // QDCOUNT
	for _, l := range strings.Split(name, ".") {
		if l == "" || len(l) > 63 {
			return nil, errors.New("잘못된 이름")
		}
		b = append(b, byte(len(l)))
		b = append(b, l...)
	}
	b = append(b, 0)
	b = append(b, byte(qtype>>8), byte(qtype), 0, 1) // QTYPE, QCLASS IN
	return b, nil
}

// dnsReadName: off 위치의 이름(압축 포인터 허용)을 읽고, 그 자리 다음 오프셋을 돌려준다
func dnsReadName(msg []byte, off int) (string, int, error) {
	var labels []string
	next, jumps := -1, 0
	for {
		if off >= len(msg) {
			return "", 0, &dnsProtoError{"이름이 잘림"}
		}
		c := int(msg[off])
		switch c & 0xc0 {
		case 0:
			if c == 0 {
				if next < 0 {
					next = off + 1
				}
				return strings.Join(labels, "."), next, nil
			}
			if off+1+c > len(msg) {
				return "", 0, &dnsProtoError{"라벨이 잘림"}
			}
			labels = append(labels, string(msg[off+1:off+1+c]))
			off += 1 + c
		case 0xc0:
			if off+1 >= len(msg) {
				return "", 0, &dnsProtoError{"포인터가 잘림"}
			}
			if next < 0 {
				next = off + 2
			}
			jumps++
			ptr := (c&0x3f)<<8 | int(msg[off+1])
			if jumps > 10 || ptr >= len(msg) {
				return "", 0, &dnsProtoError{"잘못된 포인터"}
			}
			off = ptr
		default:
			return "", 0, &dnsProtoError{"잘못된 라벨"}
		}
	}
}

// dnsParse: 응답 해석. rcode 는 NOERROR(0)·NXDOMAIN(3)만 정상으로 본다 (NXDOMAIN 은 A 없음과 같은 결과).
func dnsParse(msg []byte) (dnsRecords, bool, error) {
	var rec dnsRecords
	if len(msg) < 12 {
		return rec, false, &dnsProtoError{"응답이 짧음"}
	}
	flags := binary.BigEndian.Uint16(msg[2:])
	tc := flags&0x0200 != 0
	if tc {
		return rec, true, nil
	}
	if rc := int(flags & 0xf); rc != 0 && rc != 3 {
		return rec, false, &dnsProtoError{"rcode " + strconv.Itoa(rc)}
	}
	qd := int(binary.BigEndian.Uint16(msg[4:]))
	an := int(binary.BigEndian.Uint16(msg[6:]))
	off := 12
	for i := 0; i < qd; i++ {
		_, n, err := dnsReadName(msg, off)
		if err != nil || n+4 > len(msg) {
			return rec, false, &dnsProtoError{"질문부 오류"}
		}
		off = n + 4
	}
	for i := 0; i < an; i++ {
		_, n, err := dnsReadName(msg, off)
		if err != nil || n+10 > len(msg) {
			return rec, false, &dnsProtoError{"응답부 오류"}
		}
		typ := binary.BigEndian.Uint16(msg[n:])
		class := binary.BigEndian.Uint16(msg[n+2:])
		rdlen := int(binary.BigEndian.Uint16(msg[n+8:]))
		rd := n + 10
		if rd+rdlen > len(msg) {
			return rec, false, &dnsProtoError{"rdata 가 잘림"}
		}
		if class == 1 {
			switch {
			case typ == dnsTypeA && rdlen == 4:
				rec.a = append(rec.a, net.IP(msg[rd:rd+4]).String())
			case typ == dnsTypePTR:
				name, _, err := dnsReadName(msg, rd)
				if err != nil {
					return rec, false, err
				}
				rec.ptr = append(rec.ptr, name)
			}
		}
		off = rd + rdlen
	}
	return rec, false, nil
}

func dnsRandID() uint16 {
	var b [2]byte
	if _, err := rand.Read(b[:]); err != nil {
		return uint16(time.Now().UnixNano())
	}
	return binary.BigEndian.Uint16(b[:])
}

func dnsUDP(addr string, q []byte, id uint16, timeout time.Duration) (dnsRecords, bool, error) {
	d := net.Dialer{Timeout: timeout}
	conn, err := d.Dial("udp", addr)
	if err != nil {
		return dnsRecords{}, false, err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(timeout))
	if _, err := conn.Write(q); err != nil {
		return dnsRecords{}, false, err
	}
	buf := make([]byte, 4096)
	for {
		n, err := conn.Read(buf)
		if err != nil {
			return dnsRecords{}, false, err
		}
		if n < 12 || binary.BigEndian.Uint16(buf) != id || buf[2]&0x80 == 0 {
			continue // 다른 질의의 응답이거나 질의 패킷 — 무시
		}
		return dnsParse(buf[:n])
	}
}

func dnsTCP(addr string, q []byte, id uint16, timeout time.Duration) (dnsRecords, error) {
	d := net.Dialer{Timeout: timeout}
	conn, err := d.Dial("tcp", addr)
	if err != nil {
		return dnsRecords{}, err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(timeout))
	pkt := make([]byte, 2, 2+len(q))
	binary.BigEndian.PutUint16(pkt, uint16(len(q)))
	if _, err := conn.Write(append(pkt, q...)); err != nil {
		return dnsRecords{}, err
	}
	var lb [2]byte
	if _, err := readFull(conn, lb[:]); err != nil {
		return dnsRecords{}, err
	}
	resp := make([]byte, binary.BigEndian.Uint16(lb[:]))
	if _, err := readFull(conn, resp); err != nil {
		return dnsRecords{}, err
	}
	if len(resp) < 12 || binary.BigEndian.Uint16(resp) != id {
		return dnsRecords{}, &dnsProtoError{"TCP 응답 ID 불일치"}
	}
	rec, _, err := dnsParse(resp)
	return rec, err
}

func readFull(c net.Conn, b []byte) (int, error) {
	n := 0
	for n < len(b) {
		m, err := c.Read(b[n:])
		n += m
		if err != nil {
			return n, err
		}
	}
	return n, nil
}

// dnsExchange: 질의 1회(UDP, 잘림이면 TCP). 네트워크 오류·시간 초과는 일반 error, 응답 이상은 *dnsProtoError.
func dnsExchange(addr, name string, qtype uint16, timeout time.Duration) (dnsRecords, error) {
	id := dnsRandID()
	q, err := dnsBuildQuery(id, name, qtype)
	if err != nil {
		return dnsRecords{}, &dnsProtoError{err.Error()}
	}
	rec, tc, err := dnsUDP(addr, q, id, timeout)
	if err != nil {
		return rec, err
	}
	if tc {
		return dnsTCP(addr, q, id, timeout)
	}
	return rec, nil
}

// ---------------------------------------------------------------- 리졸버

type dnsServer struct {
	addr  string
	mu    sync.Mutex
	fails int
	oks   int
}

// dead: 응답을 한 번도 못 받고 연속으로 실패한 서버는 이후 질의를 건너뛴다 (죽은 서버 때문에 1000대가 4초씩 기다리지 않게)
func (s *dnsServer) dead() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.oks == 0 && s.fails >= dnsDeadAfter
}

func (s *dnsServer) mark(ok bool) {
	s.mu.Lock()
	if ok {
		s.oks++
	} else {
		s.fails++
	}
	s.mu.Unlock()
}

type dnsKind int

const (
	dnsFail  dnsKind = iota // 시간 초과/오류
	dnsGotA                 // A 레코드 있음
	dnsGotNX                // NXDOMAIN 또는 A 없음
)

type dnsAns struct {
	kind dnsKind
	ips  []string // 정렬·중복 제거
}

type dnsResolver struct {
	conf    dnsConf
	servers []*dnsServer
	sem     chan struct{}
}

func (r *dnsResolver) ask(s *dnsServer, name string, qtype uint16) (dnsRecords, error) {
	if s.dead() {
		return dnsRecords{}, errors.New("서버 응답 없음")
	}
	r.sem <- struct{}{}
	defer func() { <-r.sem }()
	var err error
	for i := 0; i < dnsAttempts; i++ {
		var rec dnsRecords
		rec, err = dnsExchange(s.addr, name, qtype, dnsQueryTimeout)
		if err == nil {
			s.mark(true)
			return rec, nil
		}
		var pe *dnsProtoError
		if errors.As(err, &pe) { // 서버는 살아 있음 (SERVFAIL·REFUSED·깨진 응답) — 재시도 안 함
			s.mark(true)
			return rec, err
		}
	}
	s.mark(false)
	return dnsRecords{}, err
}

func dnsSortUniq(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := append([]string(nil), in...)
	sort.Strings(out)
	j := 1
	for i := 1; i < len(out); i++ {
		if out[i] != out[i-1] {
			out[j] = out[i]
			j++
		}
	}
	return out[:j]
}

// lookupA: 한 서버에 후보 이름들을 차례로 물어 A 를 구한다 (A 가 나오는 첫 후보가 답)
func (r *dnsResolver) lookupA(s *dnsServer, host string) dnsAns {
	cands := dnsCandidates(host, r.conf)
	if len(cands) == 0 {
		return dnsAns{kind: dnsFail}
	}
	for _, c := range cands {
		rec, err := r.ask(s, c, dnsTypeA)
		if err != nil {
			return dnsAns{kind: dnsFail}
		}
		if len(rec.a) > 0 {
			return dnsAns{kind: dnsGotA, ips: dnsSortUniq(rec.a)}
		}
	}
	return dnsAns{kind: dnsGotNX}
}

// lookupAll: 모든 서버에 병렬 질의 (결과는 서버 순서)
func (r *dnsResolver) lookupAll(host string) []dnsAns {
	out := make([]dnsAns, len(r.servers))
	var wg sync.WaitGroup
	for i, s := range r.servers {
		wg.Add(1)
		go func(i int, s *dnsServer) {
			defer wg.Done()
			out[i] = r.lookupA(s, host)
		}(i, s)
	}
	wg.Wait()
	return out
}

// ptrNames: IPv4 의 PTR 이름들(짧은 이름, 중복 제거·정렬). 모든 서버의 답을 합친다.
func (r *dnsResolver) ptrNames(ip string) []string {
	p := net.ParseIP(strings.TrimSpace(ip))
	if p == nil || p.To4() == nil {
		return nil
	}
	v := p.To4()
	rev := fmt.Sprintf("%d.%d.%d.%d.in-addr.arpa", v[3], v[2], v[1], v[0])
	var mu sync.Mutex
	var names []string
	var wg sync.WaitGroup
	for _, s := range r.servers {
		wg.Add(1)
		go func(s *dnsServer) {
			defer wg.Done()
			rec, err := r.ask(s, rev, dnsTypePTR)
			if err != nil {
				return
			}
			mu.Lock()
			for _, n := range rec.ptr {
				names = append(names, dnsShort(n))
			}
			mu.Unlock()
		}(s)
	}
	wg.Wait()
	return dnsSortUniq(names)
}

// ---------------------------------------------------------------- 판정

// dnsShort: 대소문자 무시·FQDN 은 첫 라벨만 (비교용)
func dnsShort(h string) string {
	h = strings.ToLower(strings.TrimSpace(h))
	if i := strings.Index(h, "."); i >= 0 {
		h = h[:i]
	}
	return h
}

func dnsSameIP(a, b string) bool {
	a, b = strings.TrimSpace(a), strings.TrimSpace(b)
	pa, pb := net.ParseIP(a), net.ParseIP(b)
	if pa != nil && pb != nil {
		return pa.Equal(pb)
	}
	return a == b
}

// dnsJudge: 서버별 답 → 호스트 판정. 응답한 서버(A 또는 NX)만 본다.
func dnsJudge(regIP string, ans []dnsAns) (st DNSStatus, dnsIP string, ips []string, note string) {
	nA, nNX := 0, 0
	var all []string
	keys := map[string]bool{}
	for _, a := range ans {
		switch a.kind {
		case dnsGotA:
			nA++
			all = append(all, a.ips...)
			keys[strings.Join(a.ips, ",")] = true
		case dnsGotNX:
			nNX++
		}
	}
	switch {
	case nA+nNX == 0:
		return DNSNoAnswer, "", nil, "응답한 서버 없음"
	case nA == 0:
		return DNSUnregistered, "", nil, "미등록"
	}
	ips = dnsSortUniq(all)
	if nNX > 0 || len(keys) > 1 {
		return DNSAmbiguous, "", ips, "판단 불가: 서버 간 답이 다름"
	}
	if len(ips) != 1 {
		return DNSAmbiguous, "", ips, "판단 불가: A 레코드 여러 개"
	}
	if dnsSameIP(ips[0], regIP) {
		return DNSMatch, ips[0], ips, ""
	}
	return DNSMismatch, ips[0], ips, ""
}

// CheckDNS: resolv.conf 의 서버들로 entries 를 점검한다. 결과 Hosts 는 entries 와 같은 순서.
func CheckDNS(entries []DNSEntry) DNSReport {
	rep := DNSReport{Hosts: make([]DNSHostResult, len(entries))}
	for i, e := range entries {
		rep.Hosts[i] = DNSHostResult{Host: e.Host, IP: e.IP, Status: DNSNoAnswer, Note: "점검 안 함"}
	}
	disable := func(detail string) DNSReport {
		rep.Enabled, rep.Reason, rep.Detail = false, DNSDisabledReason, detail
		return rep
	}
	conf := readDNSConf(dnsResolvConf)
	for _, s := range conf.servers {
		rep.Servers = append(rep.Servers, strings.TrimSuffix(s, ":53"))
	}
	if len(conf.servers) == 0 {
		return disable("nameserver 없음")
	}
	if len(entries) == 0 {
		return disable("대상 없음")
	}
	r := &dnsResolver{conf: conf, sem: make(chan struct{}, dnsConcurrency)}
	for _, a := range conf.servers {
		r.servers = append(r.servers, &dnsServer{addr: a})
	}

	// 같은 이름은 한 번만 묻는다 (대소문자 무시)
	var uniq []string
	byName := map[string][]dnsAns{}
	for _, e := range entries {
		k := strings.ToLower(strings.TrimSpace(e.Host))
		if _, ok := byName[k]; !ok {
			byName[k] = nil
			uniq = append(uniq, k)
		}
	}
	var mu sync.Mutex
	jobs := make(chan string)
	var wg sync.WaitGroup
	for w := 0; w < dnsConcurrency; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for k := range jobs {
				a := r.lookupAll(k)
				mu.Lock()
				byName[k] = a
				mu.Unlock()
			}
		}()
	}
	for _, k := range uniq {
		jobs <- k
	}
	close(jobs)
	wg.Wait()

	anyA := false
	for i, e := range entries {
		ans := byName[strings.ToLower(strings.TrimSpace(e.Host))]
		for _, a := range ans {
			if a.kind == dnsGotA {
				anyA = true
			}
		}
		h := &rep.Hosts[i]
		h.Status, h.DNSIP, h.DNSIPs, h.Note = dnsJudge(e.IP, ans)
	}
	if !anyA {
		return disable("A 레코드 응답 없음")
	}
	rep.Enabled = true
	dnsAddWarnings(r, &rep)
	return rep
}

// dnsAddWarnings: Mismatch 호스트의 등록 IP 가 다른 호스트의 것인지 (목록 → DNS → PTR 순) 경고를 단다.
func dnsAddWarnings(r *dnsResolver, rep *DNSReport) {
	ptr := map[string][]string{} // 불일치 등록 IP → PTR 짧은 이름들
	var ips []string
	for _, h := range rep.Hosts {
		if ip := strings.TrimSpace(h.IP); h.Status == DNSMismatch {
			if _, ok := ptr[ip]; !ok {
				ptr[ip] = nil
				ips = append(ips, ip)
			}
		}
	}
	res := make([][]string, len(ips))
	var wg sync.WaitGroup
	for i, ip := range ips {
		wg.Add(1)
		go func(i int, ip string) { // 동시 수는 ask 안의 r.sem 이 제한한다
			defer wg.Done()
			res[i] = r.ptrNames(ip)
		}(i, ip)
	}
	wg.Wait()
	for i, ip := range ips {
		ptr[ip] = res[i]
	}

	for i := range rep.Hosts {
		h := &rep.Hosts[i]
		if h.Status != DNSMismatch || strings.TrimSpace(h.IP) == "" {
			continue
		}
		me := dnsShort(h.Host)
		seen := map[string]bool{}
		add := func(other, tag string) {
			k := tag + "/" + dnsShort(other)
			if seen[k] {
				return
			}
			seen[k] = true
			h.Warnings = append(h.Warnings, fmt.Sprintf("등록IP %s 은 %s 의 IP (%s)", strings.TrimSpace(h.IP), other, tag))
		}
		for j, o := range rep.Hosts {
			if j != i && dnsShort(o.Host) != me && dnsSameIP(o.IP, h.IP) {
				add(o.Host, "목록")
			}
		}
		for j, o := range rep.Hosts {
			if j != i && dnsShort(o.Host) != me && (o.Status == DNSMatch || o.Status == DNSMismatch) && dnsSameIP(o.DNSIP, h.IP) {
				add(o.Host, "DNS")
			}
		}
		for _, n := range ptr[strings.TrimSpace(h.IP)] {
			if n != me {
				add(n, "PTR")
			}
		}
	}
}

// ---------------------------------------------------------------- 표·교체

// RenderDNSTable: `hostname 등록IP DNS IP 비고` 표 (색 없음, 열 폭 맞춤, 마지막 줄 개행 없음). rows 가 비면 "".
// 보통 rep.Mismatches() 나 rep.Problems() 를 넘긴다. Ambiguous/NoAnswer 는 비고 앞에 ⚠ 를 붙인다.
func RenderDNSTable(rows []DNSHostResult) string {
	if len(rows) == 0 {
		return ""
	}
	table := [][4]string{{"hostname", "등록IP", "DNS IP", "비고"}}
	for _, h := range rows {
		dns := "-"
		switch {
		case h.Status == DNSMatch || h.Status == DNSMismatch:
			dns = h.DNSIP
		case len(h.DNSIPs) > 0:
			dns = strings.Join(h.DNSIPs, ",")
		}
		var notes []string
		if h.Note != "" {
			if h.Status == DNSAmbiguous || h.Status == DNSNoAnswer {
				notes = append(notes, "⚠ "+h.Note)
			} else {
				notes = append(notes, h.Note)
			}
		}
		for _, w := range h.Warnings {
			notes = append(notes, "⚠ "+w)
		}
		table = append(table, [4]string{cleanText(h.Host), cleanText(h.IP), cleanText(dns), strings.Join(notes, "  ")})
	}
	var wd [3]int
	for _, r := range table {
		for c := 0; c < 3; c++ {
			if w := strWidth(r[c]); w > wd[c] {
				wd[c] = w
			}
		}
	}
	lines := make([]string, 0, len(table))
	for _, r := range table {
		line := padTo(r[0], wd[0]) + "  " + padTo(r[1], wd[1]) + "  " + padTo(r[2], wd[2]) + "  " + r[3]
		lines = append(lines, strings.TrimRight(line, " "))
	}
	return strings.Join(lines, "\n")
}

// ApplyDNS: hosts(이름, 대소문자 무시) 중 rep 에서 Mismatch 인 항목의 등록 IP 를 DNS IP 로 entries 에 바로 고친다.
// 바뀐 항목마다 로그 줄 `[DNS] svr0001 1.1.1.1 -> 1.1.1.2` 를 돌려준다. Ambiguous 등은 hosts 에 있어도 건드리지 않는다.
func ApplyDNS(entries []DNSEntry, rep DNSReport, hosts []string) (changed []string) {
	want := map[string]bool{}
	for _, h := range hosts {
		want[strings.ToLower(strings.TrimSpace(h))] = true
	}
	to := map[string]string{}
	for _, h := range rep.Hosts {
		if h.Status == DNSMismatch && h.DNSIP != "" {
			to[strings.ToLower(strings.TrimSpace(h.Host))] = h.DNSIP
		}
	}
	for i := range entries {
		k := strings.ToLower(strings.TrimSpace(entries[i].Host))
		ip, ok := to[k]
		if !want[k] || !ok || dnsSameIP(entries[i].IP, ip) {
			continue
		}
		changed = append(changed, fmt.Sprintf("[DNS] %s %s -> %s", entries[i].Host, entries[i].IP, ip))
		entries[i].IP = ip
	}
	return changed
}
