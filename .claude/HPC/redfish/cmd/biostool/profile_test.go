package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// profile.go / modelnorm.go 시험 (요구사항 7-(b) 와 프로파일 형식 오류).

const profFixture = "../../testdata/profiles-test/VM.tsv"

func tabs(s string) string { return strings.ReplaceAll(s, "@", "\t") }

func mustProfile(t *testing.T, text string) *Profile {
	t.Helper()
	p, err := parseProfile(tabs(text), "t.tsv")
	if err != nil {
		t.Fatalf("parseProfile: %v", err)
	}
	return p
}

func TestNormalizeModel(t *testing.T) {
	cases := map[string]string{
		"DL360Gen11":               "dl360gen11",
		"ProLiant DL360 Gen11":     "dl360gen11",
		"DL360 Gen11":              "dl360gen11",
		"HPE ProLiant DL360 Gen11": "dl360gen11",
		"  dl360-gen_11 ":          "dl360gen11",
		"PowerEdge R660":           "r660",
		"Dell PowerEdge R660":      "r660",
		"R660":                     "r660",
		"ThinkSystem SR650 V3":     "sr650v3",
		"SR650 V3":                 "sr650v3",
		"UCSC-C220-M7S":            "ucscc220m7s",
		"X12DPi-N6":                "x12dpin6",
		"ProLiant":                 "proliant", // 접두만 남으면 떼지 않는다
		"":                         "",
	}
	for in, want := range cases {
		if got := normalizeModel(in); got != want {
			t.Errorf("normalizeModel(%q) = %q, 기대 %q", in, got, want)
		}
	}
}

func TestParseProfileFixtureAndRealVM(t *testing.T) {
	p, err := loadProfile(profFixture, "VM")
	if err != nil {
		t.Fatalf("픽스처: %v", err)
	}
	want := []string{"system_profile", "hyper_threading", "llc_prefetch", "sub_numa_cluster", "virtualization"}
	if !reflect.DeepEqual(p.Items, want) {
		t.Errorf("Items = %v, 기대 %v", p.Items, want)
	}
	// 저장소의 실제 profiles/VM.tsv 도 읽혀야 한다.
	if _, err := loadProfile("../../profiles/VM.tsv", "VM"); err != nil {
		t.Errorf("profiles/VM.tsv: %v", err)
	}
}

func TestParseProfileFormat(t *testing.T) {
	text := "\ufeff# 주석\r\n\r\nvendor@model@std_name@attribute@value@verified\r\n" +
		"DELL@R660@hyper_threading@LogicalProc@Enabled | Disabled@y@@\r\n" + // 끝의 빈 열과 CRLF 는 무시
		"# 중간 주석\nHPE@*@extra_item@Foo@A|B@N\n"
	p := mustProfile(t, text)
	if len(p.Rows) != 2 {
		t.Fatalf("행 수 %d", len(p.Rows))
	}
	r := p.Rows[0]
	if !r.Verified || !reflect.DeepEqual(r.Allowed, []string{"Enabled", "Disabled"}) || r.Line != 4 {
		t.Errorf("행 해석: %+v", r)
	}
	// 표준 4항목이 앞, 추가 항목이 뒤
	if got := p.Items; len(got) != 5 || got[0] != "system_profile" || got[4] != "extra_item" {
		t.Errorf("Items = %v", got)
	}
}

func TestParseProfileErrors(t *testing.T) {
	hdr := "vendor@model@std_name@attribute@value@verified\n"
	cases := []struct{ name, text, want string }{
		{"머리글 없음", "DELL@R660@a@b@c@Y\n", ":1:"},
		{"머리글 틀림", "vendor@model@name@attribute@value@verified\n", "머리글"},
		{"열 부족", hdr + "# c\nDELL@R660@a@b@Y\n", ":3: 탭으로 구분된 6개"},
		{"공백 구분", hdr + "DELL R660 a b c Y\n", ":2: 탭으로 구분된 6개"},
		{"빈 값", hdr + "DELL@R660@a@@c@Y\n", ":2: attribute 이(가) 비어"},
		{"verified 값", hdr + "DELL@R660@a@b@c@Maybe\n", ":2: verified 는 Y 또는 N"},
		{"std_name 문자", hdr + "DELL@R660@Hyper-Threading@b@c@Y\n", ":2: std_name"},
		{"빈 허용값", hdr + "DELL@R660@a@b@x||y@Y\n", ":2: value"},
		{"중복", hdr + "DELL@R660@a@b@c@Y\nDell Inc.@PowerEdge R660@a@B@d@N\n", ":3:"},
		{"행 없음", "# 주석\n" + hdr, "행이 하나도 없습니다"},
		{"빈 파일", "", "머리글"},
	}
	for _, c := range cases {
		_, err := parseProfile(tabs(c.text), "VM.tsv")
		if err == nil || !strings.Contains(err.Error(), c.want) || !strings.Contains(err.Error(), "VM.tsv") {
			t.Errorf("%s: 오류 %v, 기대 %q 포함", c.name, err, c.want)
		}
	}
}

// 모델 정규화로 같은 키가 되는 세 표기는 같은 행에 맞아야 하고, 정확 일치가 * 보다 우선한다.
func TestRowsForModelMatchAndWildcard(t *testing.T) {
	for _, profModel := range []string{"DL360Gen11", "ProLiant DL360 Gen11", "DL360 Gen11"} {
		p := mustProfile(t, "vendor@model@std_name@attribute@value@verified\n"+
			"HPE@"+profModel+"@hyper_threading@Exact@Enabled@Y\n"+
			"HPE@*@hyper_threading@Wild@Enabled@N\n"+ // * 행은 verified=N 만 허용 (점검 비교용)
			"HPE@*@llc_prefetch@WildOnly@Enabled@N\n")
		for _, hostModel := range []string{"ProLiant DL360 Gen11", "DL360Gen11", "dl360 gen11"} {
			rows := p.rowsFor("HPE", hostModel, "hyper_threading")
			if len(rows) != 1 || rows[0].Attr != "Exact" {
				t.Errorf("프로파일 %q / 호스트 %q: %+v", profModel, hostModel, rows)
			}
		}
		// 다른 모델은 * 로 폴백, 정확 행이 없는 항목도 * 사용
		if rows := p.rowsFor("HPE", "ProLiant DL380 Gen11", "hyper_threading"); len(rows) != 1 || rows[0].Attr != "Wild" {
			t.Errorf("* 폴백: %+v", rows)
		}
		if rows := p.rowsFor("Hewlett Packard Enterprise", "ProLiant DL360 Gen11", "llc_prefetch"); len(rows) != 1 || rows[0].Attr != "WildOnly" {
			t.Errorf("항목별 * 폴백/벤더 정규화: %+v", rows)
		}
		// 다른 벤더·없는 항목은 비어 있다
		if rows := p.rowsFor("Dell", "DL360 Gen11", "hyper_threading"); len(rows) != 0 {
			t.Errorf("다른 벤더: %+v", rows)
		}
		if rows := p.rowsFor("HPE", "DL360 Gen11", "sub_numa_cluster"); len(rows) != 0 {
			t.Errorf("행 없는 항목: %+v", rows)
		}
	}
}

func TestRowsForCandidateOrder(t *testing.T) {
	p := mustProfile(t, "vendor@model@std_name@attribute@value@verified\n"+
		"DELL@R660@hyper_threading@First@Enabled@Y\nDELL@R660@hyper_threading@Second@Enabled@N\nDELL@R660@hyper_threading@Third@Enabled@Y\n")
	rows := p.rowsFor("Dell Inc.", "PowerEdge R660", "hyper_threading")
	if len(rows) != 3 || rows[0].Attr != "First" || rows[1].Attr != "Second" || rows[2].Attr != "Third" || rows[1].Verified {
		t.Errorf("후보 순서/행별 verified: %+v", rows)
	}
}

func TestProfileNames(t *testing.T) {
	for _, bad := range []string{"", "../x", "a/b", `a\b`} {
		if validProfileName(bad) == nil {
			t.Errorf("validProfileName(%q) 통과", bad)
		}
	}
	if err := validProfileName("VM"); err != nil {
		t.Error(err)
	}
	dir := t.TempDir()
	for _, n := range []string{"VM.tsv", "BM.tsv", "note.txt"} {
		os.WriteFile(filepath.Join(dir, n), []byte("x"), 0o644)
	}
	if got := listProfileNames(dir); !reflect.DeepEqual(got, []string{"BM", "VM"}) {
		t.Errorf("listProfileNames = %v", got)
	}
	_, err := loadProfile(filepath.Join(dir, "XX.tsv"), "XX")
	if err == nil || !strings.Contains(err.Error(), "사용 가능: BM, VM") {
		t.Errorf("없는 프로파일 오류: %v", err)
	}
}
