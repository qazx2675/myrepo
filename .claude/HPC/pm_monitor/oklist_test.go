package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ---- oklist 읽기 ----

func TestParseOklist(t *testing.T) {
	in := "HostName\tOS커널\tappl\tdns\tldap_URI\tbinddn\th/t\r\n" + // 머리줄(대소문자 무시), CRLF
		"\r\n" + // 빈 줄
		"# 주석\tx\tx\tx\tx\tx\tx\n" +
		"  svr1 \t 4.18.0-553 \tnas01:/a\t10.0.0.1   10.0.0.2\t s1  s2 \tuid=x, dc=cae\tON\textra\tmore\n" + // 다듬기, 8번째 칸부터 무시
		"svr2\tk\ta\td\tu\tb\n" + // 칸 부족 -> 경고
		"svr1\tdup\tx\tx\tx\tx\tx\n" + // 중복 -> 경고(처음 줄 사용)
		"\tk\ta\td\tu\tb\tON\n" + // hostname 없음 -> 경고
		"svr3\t\t\t\t\t\t\r\n" // 빈 칸 7개는 정상(모두 빈 값)
	ok, err := parseOklist(strings.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	if ok.Count != 2 || ok.Warn != 3 {
		t.Fatalf("count=%d warn=%d", ok.Count, ok.Warn)
	}
	if got := ok.Rows["svr1"]; got != [cfgN]string{"4.18.0-553", "nas01:/a", "10.0.0.1 10.0.0.2", "s1 s2", "uid=x, dc=cae", "ON"} {
		t.Errorf("svr1 = %q", got)
	}
	if got := ok.Rows["svr3"]; got != [cfgN]string{} {
		t.Errorf("svr3 = %q", got)
	}
	if _, has := ok.Rows["svr2"]; has {
		t.Error("칸 부족 줄이 들어감")
	}
}

func TestLoadOklistErrors(t *testing.T) {
	if _, err := loadOklist(filepath.Join(t.TempDir(), "없음.txt")); err == nil {
		t.Error("없는 파일인데 오류 없음")
	}
	if _, err := loadOklist(t.TempDir()); err == nil {
		t.Error("폴더인데 오류 없음")
	}
	p := filepath.Join(t.TempDir(), "ok.txt")
	os.WriteFile(p, []byte("a\tk\tap\td\tu\tb\tON\n"), 0o644)
	ok, err := loadOklist(p)
	if err != nil || ok.Count != 1 || ok.Path != p {
		t.Errorf("ok=%+v err=%v", ok, err)
	}
}

// ---- 비교 ----

func TestCfgDiff(t *testing.T) {
	ck := Check{Ver: ldapCmdVer, Kernel: "4.18.0-553", Appl: "nas:/a", DNS: "10.0.0.1 10.0.0.2", LdapURI: "s1 s2", BindDN: "uid=x,dc=cae", HT: "ON"}
	row := [cfgN]string{"4.18.0-553", "nas:/a", "10.0.0.1 10.0.0.2", "s1 s2", "uid=x,dc=cae", "ON"}
	if m := cfgDiff(ck, row); m != 0 {
		t.Errorf("같은데 다름 %b", m)
	}
	cases := []struct {
		name string
		mod  func(r *[cfgN]string)
		want uint8
	}{
		{"순서 다름(dns)", func(r *[cfgN]string) { r[2] = "10.0.0.2 10.0.0.1" }, 1 << 2},
		{"순서 다름(URI)", func(r *[cfgN]string) { r[3] = "s2 s1" }, 1 << 3},
		{"대소문자 다름(binddn)", func(r *[cfgN]string) { r[4] = "UID=x,dc=cae" }, 1 << 4},
		{"대소문자 다름(h/t)", func(r *[cfgN]string) { r[5] = "on" }, 1 << 5},
		{"커널·appl", func(r *[cfgN]string) { r[0], r[1] = "x", "y" }, 1<<0 | 1<<1},
	}
	for _, c := range cases {
		r := row
		c.mod(&r)
		if m := cfgDiff(ck, r); m != c.want {
			t.Errorf("%s: %06b, want %06b", c.name, m, c.want)
		}
	}
	// 조사값이 비었으면 oklist 칸이 비지 않은 한 다르다. 둘 다 비면 같다
	empty := ck
	empty.HT = ""
	if m := cfgDiff(empty, row); m != 1<<5 {
		t.Errorf("빈 조사값 %06b", m)
	}
	r := row
	r[5] = ""
	if m := cfgDiff(empty, r); m != 0 {
		t.Errorf("둘 다 빔 %06b", m)
	}
	// 조사값의 공백도 같은 규칙으로 다듬어 비교한다
	sp := ck
	sp.DNS = "10.0.0.1  10.0.0.2"
	if m := cfgDiff(sp, row); m != 0 {
		t.Errorf("공백 다듬기 %06b", m)
	}
}

// ---- 설치/yumupdate여부 (InstallDone) ----

func TestInstallDoneSticky(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.json")
	f := newFake()
	f.pmDone["a"] = true
	f.pmInst["b"] = true
	col, _ := NewCollector(testCfg(), []string{"a", "b"}, path, f.deps())
	col.Round(context.Background())
	if !recOf(col, "a").InstallDone || recOf(col, "b").InstallDone {
		t.Fatalf("설치완료에서 켜져야 함: a=%v b=%v", recOf(col, "a").InstallDone, recOf(col, "b").InstallDone)
	}
	// 완료후꺼짐(ping X) -> 재부팅됨 이어도 유지
	f.pingDown["a"] = true
	col.CheckCompleted(context.Background())
	if r := recOf(col, "a"); r.Status != StatusOff || !r.InstallDone {
		t.Fatalf("완료후꺼짐: %v %v", r.Status, r.InstallDone)
	}
	f.pingDown["a"] = false
	col.CheckCompleted(context.Background())
	if r := recOf(col, "a"); r.Status != StatusRebooted || !r.InstallDone {
		t.Fatalf("재부팅됨: %v %v", r.Status, r.InstallDone)
	}
	// 다시 실행(저장·읽기)해도 유지
	col2, err := NewCollector(testCfg(), []string{"a", "b"}, path, f.deps())
	if err != nil {
		t.Fatal(err)
	}
	if !recOf(col2, "a").InstallDone {
		t.Fatal("재시작 후 꺼짐")
	}
	if b, _ := os.ReadFile(path); !strings.Contains(string(b), `"install_done": true`) || !strings.Contains(string(b), `"version": 2`) {
		t.Errorf("state.json:\n%s", b)
	}
	// 모드가 바뀌면(OS -> yum) 기록을 새로 시작하므로 꺼진다
	col3, _ := NewCollector(testCfg(), []string{"a", "b"}, path, f.deps(), WithYum(map[string]bool{"a": true}))
	if recOf(col3, "a").InstallDone {
		t.Fatal("모드 변경 뒤에도 켜져 있음")
	}
}

// OS 서버의 ping X(설치 중 끊김)는 InstallDone 을 끄지 않는다. 설치완료 전 ping X 는 켜지 않는다.
func TestInstallDonePingX(t *testing.T) {
	r := stickDone(apply(Record{Name: "a", Status: StatusInstalling, InstallDone: true}, Observation{Ping: false}, t0, time.Hour))
	if !r.InstallDone || r.Status != StatusPingX {
		t.Errorf("ping X 에서 꺼짐: %+v", r)
	}
	if stickDone(apply(Record{Name: "b"}, Observation{Ping: false}, t0, time.Hour)).InstallDone {
		t.Error("설치완료 없이 켜짐")
	}
}

func TestInstallDoneYum(t *testing.T) {
	y := Record{Name: "y", Mode: ModeYum, YumKernel: "k1", Status: StatusAlive}
	// 재부팅됨(커널 동일)만으로는 완료가 아니다
	y.YumBoot = "2026-11-17 09:00:00"
	r := stickDone(applyYum(y, true, PortOpen, &YumObs{Kernel: "k1", Boot: "2026-11-17 12:00:00"}, t0))
	if r.Status != StatusRebooted || r.InstallDone {
		t.Fatalf("재부팅됨인데 완료: %+v", r)
	}
	r = stickDone(applyYum(r, true, PortOpen, &YumObs{Kernel: "k2"}, t0))
	if r.Status != StatusYumDone || !r.InstallDone {
		t.Fatalf("yum_update/접속가능에서 켜져야 함: %+v", r)
	}
	r = stickDone(ApplyCompletedPing(r, false, t0))
	if r.Status != StatusOff || !r.InstallDone {
		t.Fatalf("완료후꺼짐에서 꺼짐: %+v", r)
	}
	// v0.4.0 state 이행 규칙
	cases := []struct {
		r    Record
		want bool
	}{
		{Record{Status: StatusDone}, true},
		{Record{Status: StatusOff}, true},
		{Record{Status: StatusRebooted}, true},
		{Record{Status: StatusInstalling}, false},
		{Record{Status: StatusPingX}, false},
		{Record{Mode: ModeYum, Status: StatusYumDone}, true},
		{Record{Mode: ModeYum, Status: StatusOff, YumDoneAt: t0}, true},
		{Record{Mode: ModeYum, Status: StatusRebooted}, false},
		{Record{Mode: ModeYum, Status: StatusAlive}, false},
		{Record{Mode: ModeYum, Status: StatusOff}, false},
	}
	for _, c := range cases {
		if got := migrateDone(c.r).InstallDone; got != c.want {
			t.Errorf("이행 %v yum=%v: %v, want %v", c.r.Status, c.r.IsYum(), got, c.want)
		}
	}
}

// ---- res 파일 ----

func TestResContent(t *testing.T) {
	ck := Check{Ver: ldapCmdVer, Kernel: "k1", Appl: "nas:/a", DNS: "10.0.0.1 10.0.0.2", LdapURI: "s1 s2", BindDN: "uid=x", HT: "ON", LdapCheck: "INFO LDAP zxcv"}
	bad := ck
	bad.LdapURI = "s2 s1"
	tab := ck
	tab.BindDN = "uid=a\tb\nc"
	recs := []Record{
		{Name: "z1", Status: StatusDone, InstallDone: true, Check: ck},       // oklist 같음 -> O O
		{Name: "a2", Status: StatusPingX, InstallDone: true, Check: bad},     // 완료 뒤 ping X, 다름 -> O X
		{Name: "m3", Status: StatusInstalling},                               // 조사 전 -> 빈 칸, X, 빈
		{Name: "b4", Status: StatusDone, Check: ck},                          // oklist 에 없음 -> 설정동일 빈 칸
		{Name: "y5", Mode: ModeYum, Status: StatusRebooted, Check: tab},      // yum 재부팅됨 -> X, 탭·줄바꿈은 공백
		{Name: "o6", Status: StatusOff, Check: Check{Ver: 3, Kernel: "old"}}, // 옛 형식 결과는 비움
	}
	ok := &Oklist{Rows: map[string][cfgN]string{
		"z1": {"k1", "nas:/a", "10.0.0.1 10.0.0.2", "s1 s2", "uid=x", "ON"},
		"a2": {"k1", "nas:/a", "10.0.0.1 10.0.0.2", "s1 s2", "uid=x", "ON"},
		"m3": {"k1", "", "", "", "", ""},
		"y5": {"k1", "nas:/a", "10.0.0.1 10.0.0.2", "s1 s2", "uid=a b c", "ON"},
	}, Count: 4}
	got := string(resContent(buildSnapshot(defaultConfig(), recs, 1, false, time.Time{}, "", withOklist(ok))))
	want := "hostname\tOS커널\tappl\tdns\tldap_URI\tbinddn\th/t\t설치/yumupdate여부\t설정동일여부\n" +
		"z1\tk1\tnas:/a\t10.0.0.1 10.0.0.2\ts1 s2\tuid=x\tON\tO\tO\n" +
		"a2\tk1\tnas:/a\t10.0.0.1 10.0.0.2\ts2 s1\tuid=x\tON\tO\tX\n" +
		"m3\t\t\t\t\t\t\tX\t\n" +
		"b4\tk1\tnas:/a\t10.0.0.1 10.0.0.2\ts1 s2\tuid=x\tON\tO\t\n" +
		"y5\tk1\tnas:/a\t10.0.0.1 10.0.0.2\ts1 s2\tuid=a b c\tON\tX\tO\n" +
		"o6\t\t\t\t\t\t\tX\t\n"
	if got != want {
		t.Errorf("got\n%q\nwant\n%q", got, want)
	}
	// oklist 가 없으면 9번째 칸은 모두 빈 칸
	got = string(resContent(buildSnapshot(defaultConfig(), recs, 1, false, time.Time{}, "")))
	for _, l := range strings.Split(strings.TrimSuffix(got, "\n"), "\n")[1:] {
		if !strings.HasSuffix(l, "\t") || strings.Count(l, "\t") != 8 {
			t.Errorf("oklist 없음: %q", l)
		}
	}
}

func TestWriteResFile(t *testing.T) {
	dir := t.TempDir()
	res := filepath.Join(dir, "hosts.txt.res")
	f := newFake()
	f.pmDone["a"] = true
	ok := &Oklist{Rows: map[string][cfgN]string{"a": {"k-a", "nas01:/export/appl", "10.10.1.10 10.10.1.11", "ldap01.demo ldap02.demo", "uid=pmuser,ou=user,ou=ldap_user,dc=cae,dc=com", "ON"}}, Count: 1}
	cf := &cfgFake{fail: map[string]bool{}}
	deps := f.deps()
	deps.Cfg = cf.query
	var logs []string
	col, err := NewCollector(testCfg(), []string{"b", "a"}, filepath.Join(dir, "s.json"), deps,
		WithOklist(ok), WithResFile(res, func(m string) { logs = append(logs, m) }))
	if err != nil {
		t.Fatal(err)
	}
	// 시작하자마자 머리줄 + 모든 서버(호스트파일 순서)
	b, err := os.ReadFile(res)
	if err != nil || string(b) != resHeader+"\nb\t\t\t\t\t\t\tX\t\na\t\t\t\t\t\t\tX\t\n" {
		t.Fatalf("시작 res: %q %v", b, err)
	}
	if st, _ := os.Stat(res); st.Mode().Perm() != 0o644 {
		t.Errorf("권한 %v", st.Mode().Perm())
	}
	col.Round(context.Background()) // a: 설치완료 + 자동 첫 조사
	b, _ = os.ReadFile(res)
	if !strings.Contains(string(b), "\na\tk-a\tnas01:/export/appl\t10.10.1.10 10.10.1.11\tldap01.demo ldap02.demo\tuid=pmuser,ou=user,ou=ldap_user,dc=cae,dc=com\tON\tO\tO\n") {
		t.Fatalf("회차 뒤 res:\n%s", b)
	}
	// 내용이 같으면 다시 쓰지 않는다(수정 시각 그대로)
	old := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	os.Chtimes(res, old, old)
	col.publish(false)
	if st, _ := os.Stat(res); !st.ModTime().Equal(old) {
		t.Error("같은 내용인데 다시 씀")
	}
	// 임시 파일이 남지 않는다
	if m, _ := filepath.Glob(filepath.Join(dir, ".pm_state_*.tmp")); len(m) != 0 {
		t.Errorf("임시 파일 남음 %v", m)
	}
	// 쓰기 실패는 수집을 멈추지 않고, 로그는 한 번만, 오류 줄에 보인다
	os.Remove(res)
	os.Mkdir(res, 0o755) // 같은 이름 폴더 -> rename 실패
	col.resMu.Lock()
	col.resLast = nil // 내용이 같아도 다시 쓰게 한다
	col.resMu.Unlock()
	f.advance(time.Minute)
	if err := col.Round(context.Background()); err != nil {
		t.Fatalf("res 실패로 회차가 실패함: %v", err)
	}
	col.publish(false)
	if len(logs) != 1 || !strings.Contains(logs[0], "res 저장 실패") {
		t.Errorf("로그 %v", logs)
	}
	if !strings.Contains(col.Snapshot().LastErr, "res 저장 실패") {
		t.Errorf("오류 줄 %q", col.Snapshot().LastErr)
	}
}
