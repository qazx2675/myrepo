package main

import (
	"bytes"
	"encoding/csv"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func csvLine(host, mgmt, attr, tag, exp, cur, note string) ResultLine {
	return ResultLine{Kind: KindProfile, Tag: tag, Host: host, IP: "ip-" + host, Vendor: "HPE", Model: "DL380",
		BiosVersion: "v1", MgmtModel: mgmt, MgmtVersion: mgmt + " 1.0", Attr: attr, Expected: exp, Current: cur, Note: note}
}

// csvSampleLines: HPE iLO 5·iLO 4 는 속성 이름 목록이 같아 한 그룹, iLO 6 은 다른 목록이라 별도 그룹, Dell 은 접속 실패.
func csvSampleLines() []ResultLine {
	return []ResultLine{
		csvLine("h1", "iLO 5", "A", TagOK, "Enabled", "Enabled", "OK"),
		csvLine("h1", "iLO 5", "B", TagDiff, "X", "Y", "FAIL"),
		csvLine("h2", "iLO 5", "A", TagDiff, "Enabled", "Disabled", "FAIL"),
		csvLine("h2", "iLO 5", "B", TagOK, "X", "X", "OK"),
		csvLine("h3", "iLO 4", "A", TagOK, "Enabled", "Enabled", "OK"),
		csvLine("h3", "iLO 4", "B", TagNA, "X", "-", "ONLY_REF"),
		csvLine("h4", "iLO 6", "A", TagOK, "Enabled", "Enabled", "OK"),
		csvLine("h4", "iLO 6", "C", TagOK, "1", "1", "OK"),
		{Kind: KindProfile, Tag: TagNA, Host: "dell1", IP: "ip-dell1", Vendor: "Dell", Model: "-", BiosVersion: "-",
			MgmtModel: "-", Attr: "-", Expected: "-", Current: "-", Note: "UNREACHABLE", HostError: true},
	}
}

func readCSVFile(t *testing.T, path string) [][]string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(b, []byte("\xef\xbb\xbf")) {
		t.Fatalf("%s: BOM 이 없다", path)
	}
	rows, err := csv.NewReader(bytes.NewReader(b[3:])).ReadAll()
	if err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	return rows
}

func TestWriteResultCSVs(t *testing.T) {
	dir := t.TempDir()
	paths, err := writeResultCSVs(dir, "p_", csvSampleLines())
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, p := range paths {
		names = append(names, filepath.Base(p))
		if runtime.GOOS != "windows" {
			if fi, _ := os.Stat(p); fi.Mode().Perm() != 0o600 {
				t.Errorf("%s 권한 %v", p, fi.Mode().Perm())
			}
		}
	}
	wantNames := []string{
		"p_summary.csv",
		"p_wide_hosts_HPE_iLO_5_iLO_4.csv", "p_wide_attrs_HPE_iLO_5_iLO_4.csv",
		"p_wide_hosts_HPE_iLO_6.csv", "p_wide_attrs_HPE_iLO_6.csv",
		"p_wide_hosts_Dell_-.csv", "p_wide_attrs_Dell_-.csv",
		"p_long.csv",
	}
	if !reflect.DeepEqual(names, wantNames) {
		t.Fatalf("파일 목록\n got %v\nwant %v", names, wantNames)
	}

	// summary
	sum := readCSVFile(t, filepath.Join(dir, "p_summary.csv"))
	wantSum := [][]string{
		{"Hostname", "IP", "Vendor", "Model", "BIOS", "관리컨트롤러", "불일치 건수", "N/A 건수", "불일치 속성"},
		{"h1", "ip-h1", "HPE", "DL380", "v1", "iLO 5 1.0", "1", "0", "B: Y / X"},
		{"h2", "ip-h2", "HPE", "DL380", "v1", "iLO 5 1.0", "1", "0", "A: Disabled / Enabled"},
		{"h3", "ip-h3", "HPE", "DL380", "v1", "iLO 4 1.0", "0", "1", ""},
		{"h4", "ip-h4", "HPE", "DL380", "v1", "iLO 6 1.0", "0", "0", "모두 동일"},
		{"dell1", "ip-dell1", "Dell", "-", "-", "-", "0", "1", "UNREACHABLE"},
	}
	if !reflect.DeepEqual(sum, wantSum) {
		t.Fatalf("summary\n got %q\nwant %q", sum, wantSum)
	}

	// wide_hosts: 기준 행 + [DIFF] 표시 + N/A 칸
	wh := readCSVFile(t, filepath.Join(dir, "p_wide_hosts_HPE_iLO_5_iLO_4.csv"))
	wantWH := [][]string{
		{"구분", "Hostname", "IP", "Vendor", "Model", "BIOS", "불일치 건수", "A", "B"},
		{"기준", "", "", "", "", "", "", "Enabled", "X"},
		{"대상", "h1", "ip-h1", "HPE", "DL380", "v1", "1", "Enabled", "[DIFF] Y"},
		{"대상", "h2", "ip-h2", "HPE", "DL380", "v1", "1", "[DIFF] Disabled", "X"},
		{"대상", "h3", "ip-h3", "HPE", "DL380", "v1", "0", "Enabled", "(N/A)"},
	}
	if !reflect.DeepEqual(wh, wantWH) {
		t.Fatalf("wide_hosts\n got %q\nwant %q", wh, wantWH)
	}
	// iLO 6 은 속성 목록이 달라 별도 파일 (A, C)
	wh6 := readCSVFile(t, filepath.Join(dir, "p_wide_hosts_HPE_iLO_6.csv"))
	if got := wh6[0][7:]; !reflect.DeepEqual(got, []string{"A", "C"}) {
		t.Fatalf("iLO 6 속성 열 %q", got)
	}

	// wide_attrs
	wa := readCSVFile(t, filepath.Join(dir, "p_wide_attrs_HPE_iLO_5_iLO_4.csv"))
	wantWA := [][]string{
		{"Attribute", "차이", "기준값", "h1", "h2", "h3"},
		{"A", "1대", "Enabled", "Enabled", "Disabled", "Enabled"},
		{"B", "1대", "X", "Y", "X", "(N/A)"},
	}
	if !reflect.DeepEqual(wa, wantWA) {
		t.Fatalf("wide_attrs\n got %q\nwant %q", wa, wantWA)
	}

	// 오류 호스트만 있는 그룹: 속성 열 없이 호스트 행만
	whd := readCSVFile(t, filepath.Join(dir, "p_wide_hosts_Dell_-.csv"))
	if len(whd) != 3 || whd[2][1] != "dell1" {
		t.Fatalf("Dell wide_hosts %q", whd)
	}

	// long
	lg := readCSVFile(t, filepath.Join(dir, "p_long.csv"))
	if len(lg) != 1+len(csvSampleLines()) {
		t.Fatalf("long 행 수 %d", len(lg))
	}
	if !reflect.DeepEqual(lg[0], csvLineHeader) {
		t.Fatalf("long 헤더 %q", lg[0])
	}
	if want := []string{"difference", "h1", "ip-h1", "HPE", "DL380", "v1", "iLO 5 1.0", "B", "X", "Y", "FAIL"}; !reflect.DeepEqual(lg[2], want) {
		t.Fatalf("long 행 got %q want %q", lg[2], want)
	}
	if want := []string{"N/A", "h3", "ip-h3", "HPE", "DL380", "v1", "iLO 4 1.0", "B", "X", "(N/A)", "ONLY_REF"}; !reflect.DeepEqual(lg[6], want) {
		t.Fatalf("long 행 got %q want %q", lg[6], want)
	}
}

// 기준값이 호스트마다 다르면 가장 많은 값을 쓴다.
func TestWideExpectedMostCommon(t *testing.T) {
	lines := []ResultLine{
		csvLine("h1", "iLO 5", "A", TagOK, "on", "on", "OK"),
		csvLine("h2", "iLO 5", "A", TagDiff, "off", "on", "FAIL"),
		csvLine("h3", "iLO 5", "A", TagDiff, "off", "on", "FAIL"),
	}
	g := groupLines(lines)[0].Groups[0]
	rows := wideHostsRows(g)
	if rows[1][7] != "off" {
		t.Fatalf("기준 행 %q", rows[1])
	}
}

// 파일 이름에 쓸 수 없는 글자는 "_" 로 바뀐다.
func TestCSVFileNameSanitize(t *testing.T) {
	if got := csvFileName("iLO 4+iLO 5/x"); got != "iLO_4_iLO_5_x" {
		t.Fatalf("got %q", got)
	}
	if got := csvFileName(""); got != "-" {
		t.Fatalf("got %q", got)
	}
	if strings.Contains(csvFileName("../x"), "/") {
		t.Fatal("경로 구분자가 남았다")
	}
}

func TestCSVSafe(t *testing.T) {
	for in, want := range map[string]string{
		"":            "",
		"Enabled":     "Enabled",
		"-":           "-",
		"-5":          "-5",
		"-1.5":        "-1.5",
		"(N/A)":       "(N/A)",
		"[DIFF] =1+1": "[DIFF] =1+1",
		"=1+1":        "'=1+1",
		"+cmd":        "'+cmd",
		"@SUM(A1)":    "'@SUM(A1)",
		"\t=x":        "'\t=x",
		"\r=x":        "'\r=x",
		"-x":          "'-x",
		"-1+1":        "'-1+1",
		"--":          "'--",
		"iLO 5 1.0":   "iLO 5 1.0",
		"10.0.0.1":    "10.0.0.1",
	} {
		if got := csvSafe(in); got != want {
			t.Errorf("csvSafe(%q) = %q, want %q", in, got, want)
		}
	}
}

// BMC·호스트에서 온 칸(본문·속성 이름 헤더·호스트 이름 헤더)은 수식으로 열리지 않게 ' 가 붙고, 고정 헤더는 그대로다.
func TestWriteResultCSVsFormula(t *testing.T) {
	dir := t.TempDir()
	l := csvLine("@h", "iLO 5", "=HYPERLINK(\"x\")", TagDiff, "-5", "+1", "-cmd")
	if _, err := writeResultCSVs(dir, "", []ResultLine{l}); err != nil {
		t.Fatal(err)
	}
	lg := readCSVFile(t, filepath.Join(dir, "long.csv"))
	if !reflect.DeepEqual(lg[0], csvLineHeader) {
		t.Fatalf("long 헤더가 바뀜 %q", lg[0])
	}
	want := []string{"difference", "'@h", "ip-@h", "HPE", "DL380", "v1", "iLO 5 1.0", "'=HYPERLINK(\"x\")", "-5", "'+1", "'-cmd"}
	if !reflect.DeepEqual(lg[1], want) {
		t.Fatalf("long 행 got %q want %q", lg[1], want)
	}
	wh := readCSVFile(t, filepath.Join(dir, "wide_hosts_HPE_iLO_5.csv"))
	if wh[0][7] != "'=HYPERLINK(\"x\")" || wh[2][1] != "'@h" || wh[2][7] != "[DIFF] +1" {
		t.Fatalf("wide_hosts %q", wh)
	}
	wa := readCSVFile(t, filepath.Join(dir, "wide_attrs_HPE_iLO_5.csv"))
	if !reflect.DeepEqual(wa[0], []string{"Attribute", "차이", "기준값", "'@h"}) || wa[1][0] != "'=HYPERLINK(\"x\")" || wa[1][3] != "'+1" {
		t.Fatalf("wide_attrs %q", wa)
	}
	// 행 함수 자체는 원래 값을 돌려준다 (TUI 등 다른 쓰임에 ' 가 섞이지 않게)
	if rows := longRows([]ResultLine{l}); rows[1][1] != "@h" {
		t.Fatalf("longRows 가 바뀜 %q", rows[1])
	}
}

func TestWriteResultCSVsEmpty(t *testing.T) {
	dir := t.TempDir()
	paths, err := writeResultCSVs(dir, "", nil)
	if err != nil || len(paths) != 2 { // summary.csv, long.csv
		t.Fatalf("paths=%v err=%v", paths, err)
	}
}

// 전체비교 줄에 기준 호스트가 있으면 wide_hosts 의 기준 행에 hostname·IP 가 나오고, 여러 기준이면 " / " 로 잇는다. 없으면 빈 칸.
func TestWideHostsRefHost(t *testing.T) {
	a := csvLine("h1", "iLO 5", "A", TagOK, "on", "on", "SAME")
	b := csvLine("h2", "iLO 5", "A", TagOK, "on", "on", "SAME")
	c := csvLine("h3", "iLO 5", "A", TagOK, "on", "on", "SAME")
	a.RefHost, a.RefIP = "ref1", "10.0.0.1"
	b.RefHost, b.RefIP = "ref1", "10.0.0.1"
	c.RefHost, c.RefIP = "ref2", "10.0.0.2"
	rows := wideHostsRows(groupLines([]ResultLine{a, b, c})[0].Groups[0])
	if rows[1][1] != "ref1 / ref2" || rows[1][2] != "10.0.0.1 / 10.0.0.2" {
		t.Fatalf("기준 행 %q", rows[1])
	}
	plain := wideHostsRows(groupLines([]ResultLine{csvLine("h1", "iLO 5", "A", TagOK, "on", "on", "OK")})[0].Groups[0])
	if plain[1][1] != "" || plain[1][2] != "" {
		t.Fatalf("프로파일 점검은 빈 칸이어야 함: %q", plain[1])
	}
}
