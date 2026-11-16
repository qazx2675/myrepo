package main

import (
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"
)

var ansiRe = regexp.MustCompile("\x1b\\[[0-9;]*m")

// lRecs 는 l 패널 골든용 서버들이다. tuiRecs(호스트파일 순서)에 설정 조사 결과를 채운다.
// 조사한 서버: c001(설치중) c1001~c1008·l001~l004(설치완료) d001(재부팅됨) s1001(ping X, 이전 값).
func lRecs() []Record {
	recs := tuiRecs()
	base := Check{Ver: ldapCmdVer, Kernel: "4.18.0-553.el8.x86_64", Appl: "nas01:/export/appl", DNS: "10.1.1.10 10.1.1.11",
		LdapURI: "ldap01 ldap02", BindDN: "uid=pm,ou=user,ou=ldap_user,dc=cae,dc=com", HT: "ON", LdapCheck: "INFO LDAP zxcv a1"}
	for i := range recs {
		r := &recs[i]
		switch r.Name {
		case "c001", "c1001", "c1002", "c1004", "c1005", "c1006", "c1007", "l001", "l002", "l003", "d001", "s1001":
			r.Check = base
		case "c1003": // URI 순서 다름
			r.Check = base
			r.Check.LdapURI = "ldap02 ldap01"
		case "c1008": // binddn 다름 + ldap_check 여러 줄
			r.Check = base
			r.Check.BindDN = "uid=other,dc=cae,dc=com"
			r.Check.LdapCheck = "OK /zxcv ldap.conf ; FAIL /zxcv sssd.conf"
		case "l004": // h/t OFF, 커널 못 읽음
			r.Check = base
			r.Check.HT, r.Check.Kernel = "OFF", ""
		}
		if r.Name == "s1001" {
			r.Check.Note = "최근 조사 실패: SSH 접속 실패"
		}
	}
	return recs
}

// lOklist 는 c1001~c1008·l001~l004·d001·c001 의 기준값이다(l003 은 oklist 에 없음). 경고 2줄.
func lOklist(t testing.TB) *Oklist {
	var sb strings.Builder
	sb.WriteString("hostname\tOS커널\tappl\tdns\tldap_URI\tbinddn\th/t\n")
	for _, h := range []string{"c001", "c1001", "c1002", "c1003", "c1004", "c1005", "c1006", "c1007", "c1008", "l001", "l002", "l004", "d001"} {
		sb.WriteString(h + "\t4.18.0-553.el8.x86_64\tnas01:/export/appl\t10.1.1.10  10.1.1.11\tldap01 ldap02\tuid=pm,ou=user,ou=ldap_user,dc=cae,dc=com\tON\n")
	}
	sb.WriteString("short\tline\n")
	sb.WriteString("c001\tdup\tx\tx\tx\tx\tx\n")
	ok, err := parseOklist(strings.NewReader(sb.String()))
	if err != nil {
		t.Fatal(err)
	}
	return ok
}

func lSnap(t testing.TB, withOK bool) Snapshot {
	var opts []snapOpt
	if withOK {
		opts = append(opts, withOklist(lOklist(t)))
	}
	return *buildSnapshot(defaultConfig(), lRecs(), 3, true, tNow.Add(-95*time.Second), "", opts...)
}

func TestLPanelGolden(t *testing.T) {
	notice := "점검 완료 11:59:00 · 응답 13대 · 무응답 1대(이전 값 유지): s1001"
	cases := []struct {
		name  string
		ok    bool
		color bool
		keys  []string
	}{
		{"l_result", false, false, nil},
		{"l_ok", true, true, nil},
		{"l_ok_m", true, true, []string{"m"}},
		{"l_ok_end", true, false, []string{"\x1b[F"}},
	}
	for _, w := range []int{60, 80, 120} {
		for _, c := range cases {
			t.Run(fmt.Sprintf("%s_%d", c.name, w), func(t *testing.T) {
				snap := lSnap(t, c.ok)
				v := newTestView(w, 24)
				v.Color = c.color
				press(v, snap, "l")
				v.setReport('l', notice, false)
				press(v, snap, c.keys...)
				got := render(*v, snap, w, 24)
				if n := strings.Count(got, "\n") + 1; n != 24 {
					t.Errorf("줄 수 %d, 24 여야 함", n)
				}
				for i, l := range strings.Split(got, "\n") {
					if sw := strWidth(ansiRe.ReplaceAllString(l, "")); sw > w {
						t.Errorf("%d번째 줄 폭 %d > %d: %q", i+1, sw, w, l)
					}
				}
				golden(t, fmt.Sprintf("tui_%s_%d", c.name, w), got+"\n")
			})
		}
	}
}

func TestLPanelContent(t *testing.T) {
	snap := lSnap(t, true)
	if snap.Cfg != (CfgSummary{HasOK: true, OK: 13, Checked: 15, Diff: 3, Warn: 2}) {
		t.Fatalf("집계 %+v", snap.Cfg)
	}
	v := newTestView(200, 60)
	v.Color = true
	press(v, snap, "l")
	got := render(*v, snap, 200, 60)
	for _, want := range []string{"[l] LDAP·설정", "oklist 13대 · 조사 15대 · 다름 3대 · 경고 2줄",
		"hostname  OS커널", "ldap_URI", "binddn", "h/t", "ldap_check결과", "m 다른 서버만",
		"\x1b[1;31mldap02 ldap01\x1b[0m", "\x1b[1;31muid=other,dc=cae,dc=com\x1b[0m", "\x1b[1;31mOFF\x1b[0m", "\x1b[1;31m(없음)\x1b[0m",
		"(조사 전)", "[최근 조사 실패: SSH 접속 실패]", "OK /zxcv ldap.conf ; FAIL /zxcv sssd.conf"} {
		if !strings.Contains(got, want) {
			t.Errorf("%q 없음\n%s", want, got)
		}
	}
	// 줄 순서는 호스트파일 순서(상태 순서가 아님): c001(설치중) 이 c1001(설치완료)보다 먼저, s1001(ping X) 은 d001 보다 먼저
	plain := ansiRe.ReplaceAllString(got, "")
	if i, j := strings.Index(plain, " c001 "), strings.Index(plain, " c1001 "); i < 0 || j < 0 || i > j {
		t.Errorf("호스트파일 순서 아님\n%s", plain)
	}
	if i, j := strings.Index(plain, " s1001 "), strings.Index(plain, " d001 "); i < 0 || j < 0 || i > j {
		t.Errorf("호스트파일 순서 아님(s1001/d001)\n%s", plain)
	}
	// l003 은 oklist 에 없으므로 비교하지 않는다(빨간색 없음)
	for _, l := range strings.Split(got, "\n") {
		if strings.Contains(l, " l003 ") && strings.Contains(l, "\x1b[1;31m") {
			t.Errorf("oklist 에 없는 서버가 칠해짐: %q", l)
		}
	}
	// m: 다른 서버만(c1003 c1008 l004)
	press(v, snap, "m")
	if !v.OnlyDiff {
		t.Fatal("m 으로 켜지지 않음")
	}
	v.prepare(snap, 200)
	if len(v.lRows) != 3 {
		t.Fatalf("m 필터 줄 %d", len(v.lRows))
	}
	got = render(*v, snap, 200, 60)
	if !strings.Contains(got, "oklist 와 다른 서버만 보기") || strings.Contains(got, " c1001 ") {
		t.Errorf("m 필터 화면\n%s", got)
	}
	press(v, snap, "m")
	if v.OnlyDiff {
		t.Fatal("m 다시 눌러 꺼지지 않음")
	}
	// 패널을 닫았다 다시 열면 필터는 꺼진다
	press(v, snap, "m", "l", "l")
	if v.OnlyDiff {
		t.Fatal("다시 열었는데 필터가 남음")
	}
}

func TestLPanelMKeyOnlyInLWithOklist(t *testing.T) {
	ok, plain := lSnap(t, true), lSnap(t, false)
	// l 패널 밖에서는 m 이 아무것도 하지 않는다
	v := newTestView(120, 24)
	if press(v, ok, "m") != actNone || v.OnlyDiff || v.Panel != 0 {
		t.Fatal("기본 화면에서 m 이 동작함")
	}
	press(v, ok, "c")
	if press(v, ok, "m"); v.OnlyDiff {
		t.Fatal("c 패널에서 m 이 동작함")
	}
	// oklist 가 없으면 l 패널에서도 m 이 없고, 색·집계 줄·m 안내도 없다
	v = newTestView(120, 24)
	v.Color = true
	press(v, plain, "l", "m")
	if v.OnlyDiff {
		t.Fatal("oklist 없이 m 이 동작함")
	}
	got := render(*v, plain, 120, 24)
	if strings.Contains(got, "\x1b[1;31m") || strings.Contains(got, "oklist") || strings.Contains(got, "m 다른") {
		t.Errorf("oklist 없는 화면\n%s", got)
	}
	// 푸터: m 은 l 패널 + oklist 일 때만
	v = newTestView(120, 24)
	press(v, ok, "l")
	render(*v, ok, 120, 24)
	v.prepare(ok, 120)
	if !strings.Contains(strings.Join(v.footerLines(120), " "), "m 다른 서버만") {
		t.Error("l 패널 푸터에 m 없음")
	}
	press(v, ok, "d")
	if strings.Contains(strings.Join(v.footerLines(120), " "), "m 다른") {
		t.Error("d 패널 푸터에 m 이 있음")
	}
}

// 8000대에서도 l 패널 열기·스크롤·m 이 가볍다(보이는 줄만 만든다).
func TestLPanel8000Fast(t *testing.T) {
	recs := make([]Record, 8000)
	rows := map[string][cfgN]string{}
	for i := range recs {
		n := fmt.Sprintf("node%05d", i)
		recs[i] = Record{Name: n, Vendor: "D", Status: Status(1 + i%7),
			Check: Check{Ver: ldapCmdVer, Kernel: "4.18.0-553", Appl: "nas:/a", DNS: "10.0.0.1 10.0.0.2", LdapURI: "s1 s2", BindDN: "uid=x", HT: "ON", LdapCheck: "INFO LDAP zxcv"}}
		row := [cfgN]string{"4.18.0-553", "nas:/a", "10.0.0.1 10.0.0.2", "s1 s2", "uid=x", "ON"}
		if i%10 == 0 {
			row[5] = "OFF"
		}
		rows[n] = row
	}
	snap := *buildSnapshot(defaultConfig(), recs, 1, false, tNow, "", withOklist(&Oklist{Rows: rows, Count: 8000}))
	if snap.Cfg.Diff != 800 {
		t.Fatalf("다름 %d", snap.Cfg.Diff)
	}
	v := newTestView(120, 40)
	steps := []struct {
		name string
		f    func()
	}{
		{"l 열기+렌더", func() { press(v, snap, "l"); v.prepare(snap, 120); render(*v, snap, 120, 40) }},
		{"스크롤+렌더", func() { press(v, snap, "\x1b[6~"); render(*v, snap, 120, 40) }},
		{"끝+렌더", func() { press(v, snap, "\x1b[F"); render(*v, snap, 120, 40) }},
		{"m+렌더", func() { press(v, snap, "m"); v.prepare(snap, 120); render(*v, snap, 120, 40) }},
	}
	for _, s := range steps {
		start := time.Now()
		s.f()
		if d := time.Since(start); d > 100*time.Millisecond {
			t.Errorf("%s: %v (100ms 초과)", s.name, d)
		}
	}
	if len(v.lRows) != 800 {
		t.Errorf("m 필터 줄 수 %d", len(v.lRows))
	}
}

func BenchmarkRenderLPanel8000(b *testing.B) {
	recs := make([]Record, 8000)
	for i := range recs {
		recs[i] = Record{Name: fmt.Sprintf("node%05d", i), Status: StatusDone,
			Check: Check{Ver: ldapCmdVer, Kernel: "4.18.0-553", Appl: "nas:/a", DNS: "10.0.0.1", LdapURI: "s1", BindDN: "uid=x", HT: "ON", LdapCheck: "INFO"}}
	}
	snap := *buildSnapshot(defaultConfig(), recs, 1, false, tNow, "")
	v := newTestView(120, 40)
	press(v, snap, "l")
	v.prepare(snap, 120)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		press(v, snap, "\x1b[B")
		_ = render(*v, snap, 120, 40)
	}
}
