// newhost_test.go - 신규 설치 양식 순수 로직: 붙여넣기 인식(구분자·순서·MAC 4형식·오류·중복), 고치기 명령, 지정 대상,
// 템플릿 저장·재로딩·충돌, 이력 추천(같은 hostname 우선·접두어 최장·최신·상한), 저장(백업·원자적·권한·이력), AWX 환경변수.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestNhParseLine(t *testing.T) {
	ok := []struct{ in, host, ip, mac string }{
		{"svr001 10.0.0.1 aa:bb:cc:dd:ee:01", "svr001", "10.0.0.1", "aa:bb:cc:dd:ee:01"},
		{"svr001\t10.0.0.1\tAA-BB-CC-DD-EE-01", "svr001", "10.0.0.1", "aa:bb:cc:dd:ee:01"},
		{"svr001,10.0.0.1,aabb.ccdd.ee01", "svr001", "10.0.0.1", "aa:bb:cc:dd:ee:01"},
		{"  10.0.0.1 , AABBCCDDEE01   svr001 ", "svr001", "10.0.0.1", "aa:bb:cc:dd:ee:01"},
		{"aa:bb:cc:dd:ee:01 svr001 10.0.0.1", "svr001", "10.0.0.1", "aa:bb:cc:dd:ee:01"},
		{"AaBb.CcDd.Ee01,\t10.0.0.1 spice-a01", "spice-a01", "10.0.0.1", "aa:bb:cc:dd:ee:01"},
		// 12자리 hex hostname — 구분자 있는 쪽이 MAC
		{"cafebabe0001 10.0.0.9 00:11:22:33:44:55", "cafebabe0001", "10.0.0.9", "00:11:22:33:44:55"},
		{"00-11-22-33-44-55 cafebabe0001 10.0.0.9", "cafebabe0001", "10.0.0.9", "00:11:22:33:44:55"},
	}
	for _, c := range ok {
		r := nhParseLine(c.in)
		if r.Err != "" || r.Host != c.host || r.IP != c.ip || r.MAC != c.mac {
			t.Errorf("%q → %+v", c.in, r)
		}
	}
	bad := []struct{ in, want string }{
		{"svr001 10.0.0.1", "값이 2개"},
		{"svr001 10.0.0.1 aa:bb:cc:dd:ee:01 x", "값이 4개"},
		{"svr001 10.0.0.300 aa:bb:cc:dd:ee:01", "IP 형식 오류"},
		{"svr001 10.0.1 aa:bb:cc:dd:ee:01", "IP 형식 오류"},
		{"svr001 svr002 aa:bb:cc:dd:ee:01", "IP 가 없음"},
		{"10.0.0.1 10.0.0.2 aa:bb:cc:dd:ee:01", "IP 가 2개"},
		{"svr001 10.0.0.1 aa:bb:cc:dd:ee", "MAC 이 없음"},
		{"svr001 10.0.0.1 aa:bb:cc:dd:ee:0g", "MAC 이 없음"},
		{"cafebabe0001 10.0.0.1 aabbccddee01", "구분할 수 없음"},
		{"svr_01! 10.0.0.1 aa:bb:cc:dd:ee:01", "hostname 형식 오류"},
	}
	for _, c := range bad {
		if r := nhParseLine(c.in); !strings.Contains(r.Err, c.want) {
			t.Errorf("%q: Err=%q, %q 기대", c.in, r.Err, c.want)
		}
	}
}

func TestNhCheckDups(t *testing.T) {
	var rows []nhRow
	for _, l := range []string{
		"svr001 10.0.0.1 aa:bb:cc:dd:ee:01",
		"SVR001 10.0.0.2 aa:bb:cc:dd:ee:02", // hostname 대소문자 무시 중복
		"svr003 10.0.0.1 aa:bb:cc:dd:ee:03", // IP 중복 (1행)
		"svr004 10.0.0.4 AABB.CCDD.EE03",    // MAC 중복 (3행, 형식 달라도 같은 값)
		"svr005 10.0.0.5",                   // 형식 오류 — 중복 검사 제외
		"svr006 10.0.0.6 aa:bb:cc:dd:ee:06",
	} {
		rows = append(rows, nhParseLine(l))
	}
	nhCheckDups(rows)
	want := []string{"hostname 중복 (행 1,2); IP 중복 (행 1,3)", "hostname 중복 (행 1,2)", "IP 중복 (행 1,3); MAC 중복 (행 3,4)", "MAC 중복 (행 3,4)", "", ""}
	for i, w := range want {
		if rows[i].Dup != w {
			t.Errorf("행 %d Dup=%q, %q 기대", i+1, rows[i].Dup, w)
		}
	}
	if rows[4].bad() == "" || rows[5].bad() != "" {
		t.Errorf("bad(): 형식 오류 행만 오류여야 함")
	}
	rows[1] = nhParseLine("svr002 10.0.0.2 aa:bb:cc:dd:ee:02") // 고치고 다시 검사하면 사라짐
	nhCheckDups(rows)
	if rows[0].Dup != "IP 중복 (행 1,3)" || rows[1].Dup != "" {
		t.Errorf("다시 검사: %q / %q", rows[0].Dup, rows[1].Dup)
	}
}

func TestNhParseRowCmd(t *testing.T) {
	cases := []struct {
		in   string
		kind byte
		nums []int
		err  bool
	}{
		{"d 3", 'd', []int{3}, false},
		{"D 3-5,8", 'd', []int{3, 4, 5, 8}, false},
		{"d 8 3", 'd', []int{3, 8}, false},
		{"e 2", 'e', []int{2}, false},
		{"e 2,3", 'e', nil, true},
		{"d 11", 'd', nil, true},
		{"d 5-3", 'd', nil, true},
		{"svr001 10.0.0.1 aa:bb:cc:dd:ee:01", 0, nil, false},
		{"d 10.0.0.1 aabbccddee01", 0, nil, false},
		{"dx 3", 0, nil, false},
	}
	for _, c := range cases {
		k, nums, err := nhParseRowCmd(c.in, 10)
		if k != c.kind || (err != nil) != c.err || (!c.err && !reflect.DeepEqual(nums, c.nums)) {
			t.Errorf("%q → %q %v %v", c.in, k, nums, err)
		}
	}
}

// nhRowsOf: hostname 들로 행 (IP·MAC 은 순번)
func nhRowsOf(hosts ...string) []nhRow {
	var rows []nhRow
	for i, h := range hosts {
		rows = append(rows, nhRow{Host: h, IP: fmt.Sprintf("10.0.0.%d", i+1), MAC: fmt.Sprintf("aa:bb:cc:dd:ee:%02x", i)})
	}
	return rows
}

func TestNhTargets(t *testing.T) {
	rows := nhRowsOf("svr001", "svr002", "svr010", "svr020", "svr021", "SPICE-a01", "spice-b02", "svr1-2")
	cases := []struct {
		target string
		want   []int
		err    bool
	}{
		{"svr001-020", []int{0, 1, 2, 3}, false},    // 자릿수 보존 (svr001..svr020)
		{"svr001-svr002", []int{0, 1}, false},       // 오른쪽에도 접두어
		{"svr01-02", nil, true},                     // svr01·svr02 는 없음 (자릿수 다름)
		{"svr020-001", nil, true},                   // 거꾸로
		{"spice*", []int{5, 6}, false},              // 글롭 (대소문자 무시)
		{"spice-?0[12]", []int{5, 6}, false},        // 글롭
		{"2-3", []int{1, 2}, false},                 // 행 번호
		{"8", []int{7}, false},                      // 행 번호
		{"7-20", []int{6, 7}, false},                // 넘치는 행 번호는 무시
		{"9-12", nil, true},                         // 없는 행
		{"*", []int{0, 1, 2, 3, 4, 5, 6, 7}, false}, // 전체
		{"SVR010", []int{2}, false},                 // 정확한 hostname (대소문자 무시)
		{"svr1-2", []int{7}, false},                 // 범위처럼 보여도 정확한 hostname 이 먼저
		{"zzz", nil, true},                          // 없음
		{"[", nil, true},                            // 잘못된 패턴
	}
	for _, c := range cases {
		got, err := nhTargets(rows, c.target)
		if (err != nil) != c.err || (!c.err && !reflect.DeepEqual(got, c.want)) {
			t.Errorf("%q → %v %v (기대 %v err=%v)", c.target, got, err, c.want, c.err)
		}
	}
}

// ---- 템플릿 ----

// setNewhostTemplates: conf newhost_template_1~9 를 테스트 동안 바꾼다
func setNewhostTemplates(t *testing.T, v ...string) {
	t.Helper()
	ptrs := []*string{&newhost_template_1, &newhost_template_2, &newhost_template_3, &newhost_template_4, &newhost_template_5,
		&newhost_template_6, &newhost_template_7, &newhost_template_8, &newhost_template_9}
	old := make([]string, len(ptrs))
	for i, p := range ptrs {
		old[i] = *p
		*p = ""
	}
	t.Cleanup(func() {
		for i, p := range ptrs {
			*p = old[i]
		}
	})
	for i, s := range v {
		*ptrs[i] = s
	}
}

func TestNewhostTemplatesAddReloadConflict(t *testing.T) {
	setNewhostTemplates(t, "A|sec bond sda 500 8.10 uefi", "mec|mec eth0 sda 500 9.6 uefi", "", "mec eth0 nvme 400 9.6 uefi")
	st := t.TempDir()
	list, probs := loadNewhostTemplates(st, "alice")
	if len(list) != 3 || len(probs) != 0 || list[2].No != 4 || list[2].User {
		t.Fatalf("conf 템플릿: %+v %v", list, probs)
	}
	if i, ok := nhFindTpl(list, "3"); !ok || i != 2 {
		t.Errorf("번호 3 → %d %v", i, ok)
	}
	if i, ok := nhFindTpl(list, "MEC"); !ok || i != 1 {
		t.Errorf("별칭 MEC → %d %v", i, ok)
	}
	if i, ok := nhFindTpl(list, "Mec-Eth0-NVME-400-9.6-uefi"); !ok || i != 2 {
		t.Errorf("자동이름 → %d %v", i, ok)
	}
	if _, ok := nhFindTpl(list, "4"); ok {
		t.Errorf("없는 번호 4 는 실패여야 함")
	}

	tp, err := nhParsePlus("+ gpu bond nvme 900 8.10 legacy B")
	if err != nil || tp.Alias != "B" || tp.Part != "nvme0n1p8" {
		t.Fatalf("+ 파싱: %+v %v", tp, err)
	}
	p, err := addNewhostTemplate(st, "alice", list, tp)
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(p); string(b) != "B|gpu bond nvme 900 8.10 legacy\n" {
		t.Errorf("templates 내용 %q", b)
	}
	if fi, _ := os.Stat(p); fi.Mode().Perm() != 0600 {
		t.Errorf("templates 권한 %v", fi.Mode().Perm())
	}
	for _, d := range []string{filepath.Join(st, "newhost"), filepath.Join(st, "newhost", "alice")} {
		if fi, _ := os.Stat(d); fi.Mode().Perm() != 0700 {
			t.Errorf("%s 권한 %v", d, fi.Mode().Perm())
		}
	}
	list, probs = loadNewhostTemplates(st, "alice") // 다음에도 목록에 나옴
	if len(list) != 4 || !list[3].User || list[3].Alias != "B" || len(probs) != 0 {
		t.Fatalf("재로딩: %+v %v", list, probs)
	}
	if other, _ := loadNewhostTemplates(st, "bob"); len(other) != 3 {
		t.Errorf("user 별: bob 에는 alice 템플릿이 보이면 안 됨")
	}

	for _, c := range []struct{ line, want string }{
		{"+ x eth0 sda 1 9 uefi a", "별칭 a 이 1 번과 겹침"},
		{"+ x eth0 sda 1 9 uefi sec-bond-sda-500-8.10-uefi", "1 번 이름과 같음"},
		{"+ sec bond sda 500 8.10 UEFI", "같은 값의 템플릿이 이미 1 번"},
	} {
		tp, err := nhParsePlus(c.line)
		if err == nil {
			_, err = addNewhostTemplate(st, "alice", list, tp)
		}
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%q: %v, %q 기대", c.line, err, c.want)
		}
	}
	for _, line := range []string{"+ sec bond hdd 500 8.10 uefi", "+ sec bond sda 5x 8.10 uefi", "+ sec bond sda", "+ a b sda 1 2 uefi 12"} {
		if _, err := nhParsePlus(line); err == nil {
			t.Errorf("%q: 형식 오류여야 함", line)
		}
	}

	// 파일에 잘못된 줄·겹치는 줄이 있으면 빼고 사유
	os.WriteFile(p, []byte("B|gpu bond nvme 900 8.10 legacy\n# 주석\n\nbad line\nmec|x y sda 1 2 uefi\n"), 0600)
	list, probs = loadNewhostTemplates(st, "alice")
	if len(list) != 4 || len(probs) != 2 || !strings.Contains(probs[0], "templates 4행") || !strings.Contains(probs[1], "별칭 mec") {
		t.Errorf("잘못된 줄: %d %v", len(list), probs)
	}
}

func TestNhCheckCol(t *testing.T) {
	for _, c := range []struct{ col, val, want string }{
		{"disk", "NVME", "nvme"}, {"boot", "Legacy", "legacy"}, {"boot", "레거시", "레거시"}, {"size", "960", "960"}, {"os", "9.6", "9.6"},
	} {
		if v, err := nhCheckCol(c.col, c.val); err != nil || v != c.want {
			t.Errorf("%s=%s → %q %v", c.col, c.val, v, err)
		}
	}
	for _, c := range [][2]string{{"disk", "hdd"}, {"size", "1.1T"}, {"boot", "bios"}, {"os", ""}, {"part", "sda5"}, {"nic", "a|b"}} {
		if _, err := nhCheckCol(c[0], c[1]); err == nil {
			t.Errorf("%s=%s: 오류여야 함", c[0], c[1])
		}
	}
	if nhColName("용량") != "size" || nhColName("OS") != "os" || nhColName("svr") != "" {
		t.Errorf("열 이름")
	}
}

// ---- 이력 추천 ----

func writeHist(t *testing.T, p string, mt time.Time, lines ...string) {
	t.Helper()
	os.MkdirAll(filepath.Dir(p), 0700)
	if err := os.WriteFile(p, []byte(strings.Join(lines, "\n")+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	os.Chtimes(p, mt, mt)
}

func TestNewhostRecommend(t *testing.T) {
	st, awx := t.TempDir(), t.TempDir()
	base := time.Unix(1700000000, 0)
	// 오래된 순: awx(bob.txt) < bob history < alice backup < alice history(가장 최근)
	writeHist(t, filepath.Join(awx, "bob.txt"), base,
		"dell r750 sec svr0005 1.1.1.5 aa:bb:cc:dd:ee:05 bond sda sda5 500 8.10 uefi",
		"dell r750 old spice01 1.1.1.6 aa:bb:cc:dd:ee:06 eth0 sda sda5 100 7.9 uefi",
		"dell r750 bad gpu01 1.1.1.7 aa:bb:cc:dd:ee:07 eth0 sda sda5 1.1T 7.9 uefi", // 용량 1.1T → 건너뜀
		"짧은 줄")
	writeHist(t, filepath.Join(st, "newhost", "bob", "history", "1.txt"), base.Add(time.Hour),
		"tmp tmp sec svr0100 1.1.2.1 aa:bb:cc:dd:ef:01 bond nvme nvme0n1p8 900 8.10 uefi",
		"tmp tmp mec spice-a07 1.1.2.2 aa:bb:cc:dd:ef:02 eth0 sda sda5 500 9.6 uefi")
	writeHist(t, filepath.Join(st, "newhost", "alice", "backup", "2.txt"), base.Add(2*time.Hour),
		"tmp tmp sec svr0005 1.1.3.5 aa:bb:cc:dd:ee:15 bond sda sda5 777 8.10 legacy")
	writeHist(t, filepath.Join(st, "newhost", "alice", "history", "3.txt"), base.Add(3*time.Hour),
		"tmp tmp sec svr0200 1.1.4.1 aa:bb:cc:dd:ee:21 bond sda sda5 300 8.10 uefi",
		"tmp tmp sec svr0201 1.1.4.2 aa:bb:cc:dd:ee:22 bond sda sda5 301 8.10 uefi") // 한 파일 안에서는 뒤 줄이 최근
	files := newhostHistFiles(st, awx)
	if len(files) != 4 || !strings.HasSuffix(files[0], "3.txt") || !strings.HasSuffix(files[3], "bob.txt") {
		t.Fatalf("이력 파일 순서: %v", files)
	}
	ix := loadNewhostHistory(files)
	cases := []struct{ host, from, size string }{
		{"svr0005", "svr0005", "777"},     // 같은 hostname 최우선 (가장 최근 기록)
		{"SVR0099", "svr0201", "301"},     // 접두어 svr — 가장 최근 줄
		{"spice-a01", "spice-a07", "500"}, // 접두어 spice-a 가 spice 보다 길게 일치
		{"spice-b01", "spice01", "100"},   // spice-b 는 없음 → 경계에서 끝나는 spice
		{"spicex01", "", ""},              // spice 뒤가 영문 → 경계 아님
		{"s01", "", ""},                   // s 는 없음
		{"gpu02", "", ""},                 // gpu 줄은 값 오류로 제외
		{"001abc", "", ""},                // 접두어 없음
	}
	for _, c := range cases {
		h, ok := ix.recommend(c.host)
		if ok != (c.from != "") || (ok && (h.Host != c.from || h.T.Size != c.size)) {
			t.Errorf("%s → %+v %v (기대 %s %s)", c.host, h, ok, c.from, c.size)
		}
	}

	// 상한: 파일 수 (최근 것만), 파일 크기 (뒤쪽 줄은 읽지 않음)
	oldF, oldB := newhostHistMaxFiles, newhostHistMaxBytes
	defer func() { newhostHistMaxFiles, newhostHistMaxBytes = oldF, oldB }()
	newhostHistMaxFiles = 1
	if fs := newhostHistFiles(st, awx); len(fs) != 1 || !strings.HasSuffix(fs[0], "3.txt") {
		t.Errorf("파일 수 상한: %v", fs)
	}
	newhostHistMaxFiles, newhostHistMaxBytes = oldF, 80 // 첫 줄(약 75바이트)만
	ix = loadNewhostHistory(files)
	if h, _ := ix.recommend("svr0300"); h.Host != "svr0200" {
		t.Errorf("크기 상한: %+v (svr0200 기대 — svr0201 줄은 상한 밖)", h)
	}
	var nilIx *nhHistIndex
	if _, ok := nilIx.recommend("svr001"); ok {
		t.Errorf("nil 색인")
	}
}

// ---- 저장 ----

func TestNewhostSave(t *testing.T) {
	awx, st := t.TempDir(), t.TempDir()
	now := time.Date(2026, 10, 12, 14, 5, 6, 0, time.Local)
	res, err := newhostSave(awx, st, "alice", []string{"tmp tmp sec a 1.1.1.1 aa:bb:cc:dd:ee:01 bond sda sda5 500 8.10 uefi"}, now)
	if err != nil || res.Backup != "" || res.HistoryErr != "" {
		t.Fatalf("첫 저장: %+v %v", res, err)
	}
	if fi, _ := os.Stat(res.Path); res.Path != filepath.Join(awx, "alice.txt") || fi.Mode().Perm() != 0644 {
		t.Errorf("새 파일은 0644: %v", fi.Mode().Perm())
	}
	if res.History != filepath.Join(st, "newhost", "alice", "history", "20261012-140506.txt") {
		t.Errorf("이력 경로 %s", res.History)
	}
	if fi, _ := os.Stat(res.History); fi.Mode().Perm() != 0600 {
		t.Errorf("이력 권한 %v", fi.Mode().Perm())
	}
	for _, d := range []string{"newhost", "newhost/alice", "newhost/alice/backup", "newhost/alice/history"} {
		if fi, _ := os.Stat(filepath.Join(st, d)); fi.Mode().Perm() != 0700 {
			t.Errorf("%s 권한 %v", d, fi.Mode().Perm())
		}
	}

	// 기존 파일: 권한 유지(0640), 백업에 옛 내용, 같은 초 두 번째 저장은 _2
	os.Chmod(res.Path, 0640)
	res2, err := newhostSave(awx, st, "alice", []string{"L1", "L2"}, now)
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(res2.Path); string(b) != "L1\nL2\n" {
		t.Errorf("새 내용 %q", b)
	}
	if fi, _ := os.Stat(res2.Path); fi.Mode().Perm() != 0640 {
		t.Errorf("기존 권한 유지 실패: %v", fi.Mode().Perm())
	}
	if b, _ := os.ReadFile(res2.Backup); !strings.HasPrefix(string(b), "tmp tmp sec a") || filepath.Base(res2.Backup) != "20261012-140506.txt" {
		t.Errorf("백업 %s %q", res2.Backup, b)
	}
	if filepath.Base(res2.History) != "20261012-140506_2.txt" {
		t.Errorf("같은 초 이력 이름: %s", res2.History)
	}
	if b, _ := os.ReadFile(res2.History); string(b) != "L1\nL2\n" {
		t.Errorf("이력 내용 %q", b)
	}
	if ms, _ := filepath.Glob(filepath.Join(awx, ".*tmp*")); len(ms) != 0 {
		t.Errorf("임시 파일이 남음: %v", ms)
	}

	// awx_dir 에 쓸 수 없으면 실패하고 기존 파일은 그대로 (백업은 남아도 됨)
	if os.Geteuid() != 0 {
		os.Chmod(awx, 0500)
		defer os.Chmod(awx, 0700)
		if _, err := newhostSave(awx, st, "alice", []string{"X"}, now.Add(time.Second)); err == nil {
			t.Errorf("쓰기 불가 awx_dir: 오류여야 함")
		}
		if b, _ := os.ReadFile(res2.Path); string(b) != "L1\nL2\n" {
			t.Errorf("실패 시 기존 내용이 바뀜: %q", b)
		}
	}
	if _, err := newhostSave(awx, st, "..", []string{"X"}, now); err == nil {
		t.Errorf("user .. 는 거부")
	}
}

func TestNhLine(t *testing.T) {
	tp, _ := parseNewhostTpl("sec bond nvme 400 9.6 legacy")
	got := nhLine(nhRow{Host: "svr001", IP: "10.0.0.1", MAC: "aa:bb:cc:dd:ee:01"}, tp)
	if got != "tmp tmp sec svr001 10.0.0.1 aa:bb:cc:dd:ee:01 bond nvme nvme0n1p8 400 9.6 legacy" || len(strings.Fields(got)) != 12 {
		t.Errorf("12필드: %q", got)
	}
}

// ---- AWX 환경 ----

func envMap(env []string) map[string]string {
	m := map[string]string{}
	for _, e := range env {
		if i := strings.IndexByte(e, '='); i > 0 {
			m[e[:i]] = e[i+1:] // 같은 키는 뒤가 이김 (exec 와 같음)
		}
	}
	return m
}

func TestNewhostAwxEnv(t *testing.T) {
	for _, k := range []string{"AWX_AUTO", "AWX_AUTO_NODEINFO", "AWX_AUTO_OS", "AWX_DNS_CHECKED", "AWX_USER", "AWX_VERIFY_FILE"} {
		t.Setenv(k, "")
		os.Unsetenv(k)
	}
	p := &AwxProfile{No: 2, Nodeinfo: "y", OS: "2026"}
	m := envMap(newhostAwxEnv(p, "alice", "/tmp/v", true))
	if m["AWX_AUTO"] != "1" || m["AWX_AUTO_NODEINFO"] != "n" || m["AWX_AUTO_OS"] != "2026" || m["AWX_USER"] != "alice" ||
		m["AWX_VERIFY_FILE"] != "/tmp/v" || m["AWX_DNS_CHECKED"] != "1" {
		t.Errorf("프로파일: %v", m)
	}
	if p.Nodeinfo != "y" {
		t.Errorf("프로파일 원본이 바뀜")
	}
	m = envMap(newhostAwxEnv(nil, "bob", "", false))
	if _, ok := m["AWX_AUTO"]; ok {
		t.Errorf("일반 실행에 AWX_AUTO 가 있으면 안 됨: %v", m)
	}
	if _, ok := m["AWX_DNS_CHECKED"]; ok {
		t.Errorf("DNS 미점검인데 AWX_DNS_CHECKED: %v", m)
	}
	if _, ok := m["AWX_VERIFY_FILE"]; ok || m["AWX_AUTO_NODEINFO"] != "n" || m["AWX_USER"] != "bob" {
		t.Errorf("일반 실행: %v", m)
	}
	if !strings.Contains(newhostAwxNote(nil), "N 으로 실행") || !strings.Contains(newhostAwxNote(p), "프로파일 2 의 nodeinfo=y 와 무관") {
		t.Errorf("안내 줄")
	}
}

// 기존 <user>.txt 의 소유자와 심볼릭 링크를 유지한다 (root 로 실행할 때 소유자 확인)
func TestNewhostSaveOwnerLink(t *testing.T) {
	awx, st := t.TempDir(), t.TempDir()
	now := time.Date(2026, 10, 13, 9, 0, 0, 0, time.Local)
	real := filepath.Join(t.TempDir(), "real.txt")
	if err := os.WriteFile(real, []byte("old\n"), 0640); err != nil {
		t.Fatal(err)
	}
	if os.Geteuid() == 0 {
		os.Chown(real, 1234, 4321)
	}
	link := filepath.Join(awx, "bob.txt")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	if _, err := newhostSave(awx, st, "bob", []string{"N1"}, now); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Lstat(link); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("링크가 일반 파일로 바뀜: %v", err)
	}
	b, _ := os.ReadFile(real)
	fi, _ := os.Stat(real)
	if string(b) != "N1\n" || fi.Mode().Perm() != 0640 {
		t.Errorf("대상 파일 %q %v", b, fi.Mode().Perm())
	}
	if os.Geteuid() == 0 {
		if s, ok := fi.Sys().(*syscall.Stat_t); !ok || s.Uid != 1234 || s.Gid != 4321 {
			t.Errorf("소유자 유지 실패: %+v", fi.Sys())
		}
	}
	if ms, _ := filepath.Glob(filepath.Join(filepath.Dir(real), ".*tmp*")); len(ms) != 0 {
		t.Errorf("임시 파일이 남음: %v", ms)
	}
}
