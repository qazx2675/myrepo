package main

// yum update 모드의 집계·코멘트·표(계획서 4.8, 6.3 의 21단계) 단위 테스트.

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var yNow = time.Date(2026, 11, 17, 15, 0, 0, 0, time.UTC)

// yrec 는 yum 서버 기록 하나다. sinceMin 이 0 보다 크면 그 분 전에 기준 커널을 기록한 것으로 한다.
func yrec(name, vendor string, st Status, sinceMin int) Record {
	r := Record{Name: name, Vendor: vendor, Status: st, Mode: ModeYum, YumKernel: "4.18.0-513.el8", YumNow: "4.18.0-513.el8"}
	if sinceMin > 0 {
		r.YumSince = yNow.Add(-time.Duration(sinceMin) * time.Minute)
	}
	if st == StatusRebooted { // 기본은 재부팅 뒤 접속해 커널 동일을 확인한 기록(yumKernelSame). 재부팅 중은 yrebooting 으로 만든다
		r.Reason = "재부팅됨, 커널 동일 4.18.0-513.el8"
	}
	return r
}

func orec(name, vendor string, st Status) Record {
	return Record{Name: name, Vendor: vendor, Status: st}
}

func snapOf(recs ...Record) Snapshot {
	return *buildSnapshot(defaultConfig(), recs, 1, false, time.Time{}, "", withYumNow(yNow))
}

// 계획서 4.8 예시의 yum 부분: 12대 중 8대 완료, 접속가능 1, 재부팅됨 2, ping X 1, 지연 2.
func planYumRecs() []Record {
	var recs []Record
	for i := 1; i <= 8; i++ {
		r := yrec(fmt.Sprintf("svr%03d", i), "D", StatusYumDone, 200)
		r.YumNow = "4.18.0-553.el8"
		recs = append(recs, r)
	}
	px := yrec("svr010", "D", StatusPingX, 0)
	px.YumKernel, px.YumNow = "", "" // 기준 커널을 기록하기 전 ping X
	recs = append(recs, px)
	recs = append(recs, yrec("svr011", "D", StatusAlive, 85))     // 지연 A
	recs = append(recs, yrec("svr012", "D", StatusRebooted, 120)) // 지연 B
	recs = append(recs, yrec("svr013", "D", StatusRebooted, 10))  // 재부팅됨, 아직 지연 아님
	return recs
}

func TestInstallCommentYumMixed(t *testing.T) {
	recs := planYumRecs()
	recs = append(recs, orec("os1", "D", StatusDone), orec("os2", "D", StatusDone), orec("os3", "D", StatusInstalling),
		orec("os4", "sds", StatusDisk), orec("os5", "sds", StatusPingX), orec("os6", "L", StatusRebooted))
	got := InstallComment(snapOf(recs...))
	want := strings.Join([]string{
		"설치현황",
		"6대 중 3대 설치완료, 1대 설치중, 1대 디스크 미인식, ping X 1대",
		"yum update 12대 중 8대 커널 변경 완료(yum_update/접속가능), 접속가능 1대, 재부팅됨 2대(커널 동일 2대), ping X 1대 (지연 2대)",
		"yum 접속불가 1대",
		"svr010",
		"yum 재부팅 대기 1대",
		"svr011",
		"yum 재부팅됨 커널 동일 2대",
		"svr012 svr013",
		"sds",
		"디스크 미인식 1대",
		"os4",
		"접속불가 1대",
		"os5",
		"재부팅됨 1대",
		"os6",
	}, "\n")
	if got != want {
		t.Errorf("혼합 코멘트\n--- want\n%s\n--- got\n%s", want, got)
	}
}

func TestInstallCommentYumOnly(t *testing.T) {
	// OS 서버가 없으면 OS 요약 줄을 쓰지 않는다
	got := InstallComment(snapOf(planYumRecs()...))
	want := "설치현황\nyum update 12대 중 8대 커널 변경 완료(yum_update/접속가능), 접속가능 1대, 재부팅됨 2대(커널 동일 2대), ping X 1대 (지연 2대)\n" +
		"yum 접속불가 1대\nsvr010\nyum 재부팅 대기 1대\nsvr011\nyum 재부팅됨 커널 동일 2대\nsvr012 svr013"
	if got != want {
		t.Errorf("yum 전용 코멘트\n%s", got)
	}
	// 0대 항목은 빼고 yum update·커널 변경 완료만 항상 쓴다. 완료후꺼짐은 따로 센다.
	got = InstallComment(snapOf(yrec("a", "D", StatusYumDone, 200), yrec("b", "D", StatusOff, 200), yrec("c", "D", StatusNone, 0)))
	want = "설치현황\nyum update 3대 중 1대 커널 변경 완료(yum_update/접속가능), 완료후꺼짐 1대"
	if got != want {
		t.Errorf("0대 생략: %q", got)
	}
	got = InstallComment(snapOf(yrec("c", "D", StatusNone, 0)))
	if want = "설치현황\nyum update 1대 중 0대 커널 변경 완료(yum_update/접속가능)"; got != want {
		t.Errorf("확인전만: %q", got)
	}
}

// v0.3.0 의 InstallComment·DelayComment 원본(수정 전 코드)이다. yum 서버가 없는 스냅샷은 이것과 바이트 단위로 같아야 한다.
func legacyInstallComment(s Snapshot) string {
	c := s.Counts
	installing := c[StatusInstalling] + c[StatusDelayed]
	var b strings.Builder
	b.WriteString("설치현황\n")
	fmt.Fprintf(&b, "%d대 중 %d대 설치완료, %d대 설치중", s.Total, c[StatusDone]+c[StatusRebooted], installing)
	if n := c[StatusDelayed]; n > 0 {
		fmt.Fprintf(&b, " (지연 %d대)", n)
	}
	if n := c[StatusDisk]; n > 0 {
		fmt.Fprintf(&b, ", %d대 디스크 미인식", n)
	}
	if n := c[StatusPingX]; n > 0 {
		fmt.Fprintf(&b, ", ping X %d대", n)
	}
	if n := c[StatusOff]; n > 0 {
		fmt.Fprintf(&b, ", 완료후꺼짐 %d대", n)
	}
	for _, g := range s.Vendors {
		disk, px, off := g.Hosts[StatusDisk], g.Hosts[StatusPingX], g.Hosts[StatusOff]
		if len(disk)+len(px)+len(off) == 0 {
			continue
		}
		b.WriteString("\n" + g.Vendor)
		writeList(&b, "디스크 미인식", disk)
		writeList(&b, "접속불가", px)
		writeList(&b, "완료후꺼짐", off)
	}
	var reb []string
	for _, r := range s.Records {
		if r.Status == StatusRebooted {
			reb = append(reb, r.Name)
		}
	}
	writeList(&b, "재부팅됨", reb)
	return b.String()
}

func legacyDelayComment(s Snapshot) string {
	var blocks []string
	for _, g := range s.Vendors {
		hosts := g.Hosts[StatusDelayed]
		if len(hosts) == 0 {
			continue
		}
		blocks = append(blocks, fmt.Sprintf("안녕하세요 %s 담당자님\n하기서버 %d대는 OS 설치가 지연되고 있습니다.\n점검부탁드립니다.\n감사합니다.\n%s",
			g.Vendor, len(hosts), strings.Join(hosts, " ")))
	}
	if len(blocks) == 0 {
		return "지연된 서버가 없습니다."
	}
	return strings.Join(blocks, "\n\n")
}

func TestNoYumOutputIdenticalToV030(t *testing.T) {
	snaps := map[string]Snapshot{
		"빈":   mkSnap(nil, nil, nil),
		"기본":  mkSnap(nil, []spec{{"D", StatusDone, 30}, {"D", StatusInstalling, 20}, {"sds", StatusDisk, 3}, {"sds", StatusPingX, 2}}, nil),
		"지연":  mkSnap(nil, []spec{{"D", StatusDelayed, 2}, {"sds", StatusDelayed, 1}, {"L", StatusDone, 3}, {"기타", StatusDisk, 1}}, nil),
		"완료후": mkSnap(nil, []spec{{"D", StatusDone, 4}, {"D", StatusOff, 2}, {"sds", StatusOff, 1}, {"D", StatusRebooted, 3}, {"D", StatusNone, 2}}, nil),
	}
	for name, s := range snaps {
		if s.Yum.Total != 0 || s.Yum.Vendors != nil {
			t.Errorf("%s: yum 이 없는데 집계가 있음: %+v", name, s.Yum)
		}
		if got, want := InstallComment(s), legacyInstallComment(s); got != want {
			t.Errorf("%s: 설치현황이 v0.3.0 과 다름\n%q\n%q", name, got, want)
		}
		if got, want := DelayComment(s), legacyDelayComment(s); got != want {
			t.Errorf("%s: 점검요청이 v0.3.0 과 다름\n%q\n%q", name, got, want)
		}
	}
}

// 설치완료 집계·벤더 블록·재부팅됨 목록은 OS 서버만이고 Counts 는 모든 서버를 센다.
func TestYumSnapshotAggregation(t *testing.T) {
	recs := append(planYumRecs(), orec("os1", "D", StatusDone), orec("os2", "D", StatusRebooted), orec("os3", "D", StatusPingX), orec("os4", "D", StatusOff))
	s := snapOf(recs...)
	if s.Total != 16 || s.Yum.Total != 12 {
		t.Errorf("Total=%d Yum.Total=%d", s.Total, s.Yum.Total)
	}
	if s.Done() != 2 { // os1 + os2(재부팅됨). yum 재부팅됨 2대는 완료가 아니다
		t.Errorf("Done()=%d, want 2", s.Done())
	}
	if s.Counts[StatusRebooted] != 3 || s.Counts[StatusYumDone] != 8 || s.Counts[StatusAlive] != 1 || s.Counts[StatusPingX] != 2 || s.Counts[StatusOff] != 1 {
		t.Errorf("Counts 는 모든 서버를 센다: %v", s.Counts)
	}
	y := s.Yum
	if y.Done != 8 || y.Alive != 1 || y.Rebooted != 2 || y.PingX != 1 || y.None != 0 || y.Off != 0 || y.Delayed != 2 || y.DelayMin != 60 {
		t.Errorf("Yum=%+v", y)
	}
	for _, g := range s.Vendors {
		for st, hs := range g.Hosts {
			for _, h := range hs {
				if strings.HasPrefix(h, "svr") {
					t.Errorf("벤더 블록에 yum 서버 %s (%v, %s)", h, st, g.Vendor)
				}
			}
		}
	}
	if got := strings.Join(s.Vendors[0].Hosts[StatusPingX], ","); got != "os3" {
		t.Errorf("D 의 ping X=%q", got)
	}
	if strings.Join(y.PingXHosts, ",") != "svr010" || strings.Join(y.WaitingHosts, ",") != "svr011" || strings.Join(y.SameKernelHosts, ",") != "svr012,svr013" {
		t.Errorf("목록: %v %v %v", y.PingXHosts, y.WaitingHosts, y.SameKernelHosts)
	}
	// 재부팅됨 목록은 OS 서버만(코멘트 끝)
	if c := InstallComment(s); !strings.HasSuffix(c, "\n재부팅됨 1대\nos2") {
		t.Errorf("재부팅됨 목록:\n%s", c)
	}
	// 지연은 now 가 없으면 세지 않는다
	if n := buildSnapshot(defaultConfig(), planYumRecs(), 1, false, time.Time{}, "").Yum.Delayed; n != 0 {
		t.Errorf("now 없이 지연 %d", n)
	}
	// 소켓으로 화면에 가므로 json 왕복에서 집계가 유지된다
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	var back Snapshot
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatal(err)
	}
	if InstallComment(back) != InstallComment(s) || DelayComment(back) != DelayComment(s) || YumTable(back, yNow) != YumTable(s, yNow) || back.Done() != s.Done() {
		t.Errorf("json 왕복 뒤 출력이 달라짐")
	}
}

func TestDelayCommentYum(t *testing.T) {
	recs := []Record{
		orec("o1", "D", StatusDelayed), orec("o2", "D", StatusInstalling),
		yrec("d2", "D", StatusAlive, 90), yrec("d1", "D", StatusAlive, 61), yrec("d3", "D", StatusRebooted, 100),
		yrec("d4", "D", StatusAlive, 30),      // 아직 지연 아님
		yrec("s1", "sds", StatusRebooted, 70), // sds 는 yum 지연만
		yrec("l1", "L", StatusYumDone, 500),   // 완료는 지연이 아님
		yrec("l2", "L", StatusPingX, 0),       // 기준 전 ping X 는 지연이 아님(YumSince 없음)
	}
	got := DelayComment(snapOf(recs...))
	want := strings.Join([]string{
		"안녕하세요 D 담당자님", "하기서버 1대는 OS 설치가 지연되고 있습니다.", "점검부탁드립니다.", "감사합니다.", "o1",
		"",
		"안녕하세요 D 담당자님", "하기서버 2대는 yum update 후 재부팅이 확인되지 않고 있습니다.", "점검부탁드립니다.", "감사합니다.", "d1 d2",
		"",
		"안녕하세요 D 담당자님", "하기서버 1대는 yum update 후 재부팅은 되었으나 커널이 변경되지 않았습니다.", "점검부탁드립니다.", "감사합니다.", "d3",
		"",
		"안녕하세요 sds 담당자님", "하기서버 1대는 yum update 후 재부팅은 되었으나 커널이 변경되지 않았습니다.", "점검부탁드립니다.", "감사합니다.", "s1",
	}, "\n")
	if got != want {
		t.Errorf("yum 점검요청\n--- want\n%s\n--- got\n%s", want, got)
	}
}

func TestDelayCommentYumOnlyAndNone(t *testing.T) {
	// OS 지연이 없어도 yum 지연이 있으면 안내 문구가 아니다
	got := DelayComment(snapOf(yrec("a", "sds", StatusAlive, 70), orec("o", "D", StatusInstalling)))
	want := "안녕하세요 sds 담당자님\n하기서버 1대는 yum update 후 재부팅이 확인되지 않고 있습니다.\n점검부탁드립니다.\n감사합니다.\na"
	if got != want {
		t.Errorf("yum 전용 지연: %q", got)
	}
	// OS·yum 모두 지연이 없을 때만 안내 문구
	if got := DelayComment(snapOf(yrec("a", "D", StatusAlive, 5), yrec("b", "D", StatusYumDone, 500), orec("o", "D", StatusInstalling))); got != "지연된 서버가 없습니다." {
		t.Errorf("지연 없음: %q", got)
	}
	// 지연 기준은 yum_delay_min
	cfg := defaultConfig()
	cfg.YumDelayMin = 10
	s := buildSnapshot(cfg, []Record{yrec("a", "D", StatusAlive, 15)}, 1, false, time.Time{}, "", withYumNow(yNow))
	if s.Yum.Delayed != 1 || s.Yum.DelayMin != 10 {
		t.Errorf("yum_delay_min=10: %+v", s.Yum)
	}
}

func TestYumTable(t *testing.T) {
	done := yrec("svr002", "D", StatusYumDone, 300)
	done.YumNow = "4.18.0-553.el8"
	done.RebootedAt = time.Date(2026, 11, 17, 14, 20, 0, 0, time.UTC)
	wait := yrec("svr011", "D", StatusAlive, 85)
	wait.Reason = "기준 커널 4.18.0-513.el8"
	reb := yrec("svr012", "sds", StatusRebooted, 20)
	reb.Reason = "재부팅됨, 커널 동일 4.18.0-513.el8"
	reb.RebootedAt = time.Date(2026, 11, 17, 14, 50, 0, 0, time.UTC)
	none := Record{Name: "svr020", Vendor: "D", Status: StatusNone, Mode: ModeYum, Reason: "ssh 확인 실패: 응답 없음"}
	s := snapOf(none, reb, orec("os1", "D", StatusDone), wait, done)
	// 상태 번호 순(재부팅됨 6 -> 접속가능 8 -> 완료 9, 확인전은 끝)
	want := strings.Join([]string{
		"hostname\t벤더\t상태\t기준 커널\t현재 커널\t재부팅\t비고",
		"svr012\tsds\t재부팅됨\t4.18.0-513.el8\t4.18.0-513.el8\t11-17 14:50\t재부팅됨, 커널 동일 4.18.0-513.el8",
		"svr011\tD\t접속가능\t4.18.0-513.el8\t4.18.0-513.el8\t-\t기준 커널 4.18.0-513.el8; 지연 85분",
		"svr002\tD\tyum_update/접속가능\t4.18.0-513.el8\t4.18.0-553.el8\t11-17 14:20\t",
		"svr020\tD\t확인전\t-\t-\t-\tssh 확인 실패: 응답 없음",
	}, "\n")
	if got := YumTable(s, yNow); got != want {
		t.Errorf("y 표\n--- want\n%s\n--- got\n%s", want, got)
	}
	// 지연 분은 넘겨 준 now 로 센다
	if got := YumTable(s, yNow.Add(15*time.Minute)); !strings.Contains(got, "지연 100분") {
		t.Errorf("now 반영: %s", got)
	}
	// yum 서버가 없으면 안내 문구(OS 서버는 표에 나오지 않는다)
	if got := YumTable(snapOf(orec("os1", "D", StatusDone)), yNow); got != "yum 서버가 없습니다." {
		t.Errorf("yum 없음: %q", got)
	}
	if got := YumTable(mkSnap(nil, nil, nil), yNow); got != "yum 서버가 없습니다." {
		t.Errorf("빈 스냅샷: %q", got)
	}
}

// 계획서 4.9: OS 모드 서버는 상태와 관계없이 l 재조사 대상, yum 서버는 yum_update/접속가능만.
func TestLdapTargetYum(t *testing.T) {
	yes := []Record{orec("a", "D", StatusDone), orec("b", "D", StatusRebooted), yrec("c", "D", StatusYumDone, 100),
		orec("d", "D", StatusOff), orec("e", "D", StatusInstalling), orec("f", "D", StatusPingX), orec("n", "D", StatusNone)}
	no := []Record{yrec("g", "D", StatusAlive, 100), yrec("h", "D", StatusRebooted, 100), yrec("i", "D", StatusOff, 100),
		yrec("j", "D", StatusPingX, 0), yrec("k", "D", StatusNone, 0)}
	for _, r := range yes {
		if !ldapTarget(r) {
			t.Errorf("%s(%v, yum=%v) 는 대상이어야 함", r.Name, r.Status, r.IsYum())
		}
	}
	for _, r := range no {
		if ldapTarget(r) {
			t.Errorf("%s(%v, yum=%v) 는 대상이 아니어야 함", r.Name, r.Status, r.IsYum())
		}
	}
	// 같은 상태 이름이라도 모드로 갈린다: OS 서버의 재부팅됨은 대상, yum 서버의 재부팅됨은 아님
	if !ldapTarget(orec("x", "D", StatusRebooted)) || ldapTarget(yrec("x", "D", StatusRebooted, 1)) {
		t.Errorf("재부팅됨 모드 구분 실패")
	}
	// 자동 첫 조사: OS 서버는 ping 이 닿는 상태(확인전·ping X·완료후꺼짐 제외), yum 서버는 yum_update/접속가능만
	autoYes := []Record{orec("a", "D", StatusDone), orec("b", "D", StatusInstalling), orec("c", "D", StatusDelayed),
		orec("d", "D", StatusDisk), orec("e", "D", StatusRebooted), yrec("f", "D", StatusYumDone, 100)}
	autoNo := []Record{orec("g", "D", StatusPingX), orec("h", "D", StatusOff), orec("i", "D", StatusNone),
		yrec("j", "D", StatusAlive, 100), yrec("k", "D", StatusRebooted, 100)}
	for _, r := range autoYes {
		if !autoTarget(r) {
			t.Errorf("자동: %s(%v) 는 대상이어야 함", r.Name, r.Status)
		}
	}
	for _, r := range autoNo {
		if autoTarget(r) {
			t.Errorf("자동: %s(%v) 는 대상이 아니어야 함", r.Name, r.Status)
		}
	}
}

func TestLdapTargetsAndTableWithYum(t *testing.T) {
	cfg := testCfg()
	hosts := []string{"os1", "os2", "os3", "y1", "y2", "y3", "y4"}
	col, err := NewCollector(cfg, hosts, filepath.Join(t.TempDir(), "x.pm_state.json"), Deps{},
		WithYum(map[string]bool{"y1": true, "y2": true, "y3": true, "y4": true}))
	if err != nil {
		t.Fatal(err)
	}
	set := func(name string, st Status) {
		col.mu.Lock()
		r := col.recs[name]
		r.Status = st
		if r.IsYum() {
			r.YumKernel, r.YumSince = "k1", yNow.Add(-time.Hour)
		}
		col.recs[name] = r
		col.mu.Unlock()
	}
	set("os1", StatusDone)
	set("os2", StatusRebooted)
	set("os3", StatusInstalling)
	set("y1", StatusYumDone)
	set("y2", StatusAlive)
	set("y3", StatusRebooted)
	set("y4", StatusOff)
	col.publish(false)
	if got := strings.Join(col.ldapTargets(), ","); got != "os1,os2,os3,y1" {
		t.Errorf("대상=%s, want os1,os2,os3,y1", got)
	}
	if col.LDAPPending() != 4 {
		t.Errorf("pending=%d", col.LDAPPending())
	}
	// 표에는 모든 서버가 호스트파일 순서로 나오고, 결과가 있는 서버만 값이 찬다
	ck := Check{Ver: ldapCmdVer, Kernel: "k", HT: "ON", LdapCheck: "INFO LDAP zxcv"}
	if err := col.SetChecks(map[string]Check{"os1": ck, "y1": ck}); err != nil {
		t.Fatal(err)
	}
	tbl := ldapTable(col.Snapshot())
	want := "hostname\tOS커널\tappl\tdns\tldap_URI\tbinddn\th/t\tldap_check결과\n" +
		"os1\tk\t\t\t\t\tON\tINFO LDAP zxcv\n" +
		"os2\t\t\t\t\t\t\t(조사 전)\n" +
		"os3\t\t\t\t\t\t\t(조사 전)\n" +
		"y1\tk\t\t\t\t\tON\tINFO LDAP zxcv\n" +
		"y2\t\t\t\t\t\t\t(조사 전)\n" +
		"y3\t\t\t\t\t\t\t(조사 전)\n" +
		"y4\t\t\t\t\t\t\t(조사 전)"
	if tbl != want {
		t.Errorf("표:\n%q\nwant\n%q", tbl, want)
	}
}

func TestPingXHostsExcludesYum(t *testing.T) {
	s := snapOf(orec("o2", "D", StatusPingX), orec("o1", "sds", StatusPingX), yrec("y1", "D", StatusPingX, 0),
		yrec("y2", "D", StatusRebooted, 100), orec("o3", "D", StatusDone))
	if got := strings.Join(pingXHosts(&s), ","); got != "o1,o2" {
		t.Errorf("d 대상=%q, want o1,o2", got)
	}
	// yum ping X 만 있으면 d 는 대상 없음 안내
	s2 := snapOf(yrec("y1", "D", StatusPingX, 0))
	if out, err := DHCPQuery(&Config{DHCPServer: "dhcp"}, pingXHosts(&s2)); err != nil || out != dhcpNoHosts {
		t.Errorf("out=%q err=%v", out, err)
	}
}

// 집계는 회차마다 한 번 만든다. 8000대에서도 스냅샷 생성이 빨라야 한다(키 반응 50ms 와는 별개로 넉넉한 상한).
func TestBuildSnapshotYumAt8000(t *testing.T) {
	recs := make([]Record, 8000)
	for i := range recs {
		recs[i] = Record{Name: fmt.Sprintf("node%05d", i), Vendor: "D", Status: Status(1 + i%9)}
		if i%2 == 0 {
			recs[i] = yrec(recs[i].Name, "D", Status(StatusRebooted+Status(i%4)), 90)
			if recs[i].Status == StatusOff {
				recs[i].Status = StatusPingX
			}
		}
	}
	start := time.Now()
	s := snapOf(recs...)
	if d := time.Since(start); d > 200*time.Millisecond {
		t.Errorf("8000대 스냅샷 생성 %v", d)
	}
	if s.Yum.Total != 4000 || s.Total != 8000 {
		t.Errorf("Total=%d Yum.Total=%d", s.Total, s.Yum.Total)
	}
}
