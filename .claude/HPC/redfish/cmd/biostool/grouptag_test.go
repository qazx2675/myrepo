package main

// grouptag_test.go — user.txt 그룹 태그(HostTag)별 결과 묶음과 CSV 파일 이름.

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
)

func gtLine(hostTag, vendor, host, mgmt, attr string) ResultLine {
	return ResultLine{Kind: KindProfile, Tag: TagOK, HostTag: hostTag, Host: host, IP: "ip-" + host, Vendor: vendor, Model: "M",
		BiosVersion: "v1", MgmtModel: mgmt, MgmtVersion: mgmt + " 1.0", Attr: attr, Expected: "on", Current: "on", Note: "OK"}
}

func groupNames(vg VendorGroups) []string {
	var n []string
	for _, g := range vg.Groups {
		n = append(n, g.Name)
	}
	return n
}

// (a) 태그만 있는 줄: 벤더 2종·iLO 모델 2종이 섞여도 최상위 "태그" 1개, 그룹은 태그 순서대로.
func TestGroupLinesTagsOnly(t *testing.T) {
	lines := []ResultLine{
		gtLine("asdf", "HPE", "h1", "iLO 5", "A"),
		gtLine("qwer", "Dell", "d1", "iDRAC 9", "B"),
		gtLine("asdf", "HPE", "h2", "iLO 6", "C"), // 같은 태그, 다른 iLO 모델·다른 속성 집합
		gtLine("asdf", "Dell", "d2", "iDRAC 9", "A"),
		gtLine("qwer", "HPE", "h3", "iLO 5", "B"),
	}
	vgs := groupLines(lines)
	if len(vgs) != 1 || vgs[0].Vendor != tagGroupVendor {
		t.Fatalf("최상위 = %+v, 기대: \"태그\" 1개", vgs)
	}
	if got := groupNames(vgs[0]); !reflect.DeepEqual(got, []string{"asdf", "qwer"}) {
		t.Fatalf("그룹 = %v", got)
	}
	g := vgs[0].Groups[0]
	if g.Vendor != tagGroupVendor || !reflect.DeepEqual(g.Hosts, []string{"h1", "h2", "d2"}) || len(g.Lines) != 3 {
		t.Errorf("asdf 그룹 = %+v", g)
	}
	if !reflect.DeepEqual(g.Mgmt, []string{"iLO 5", "iLO 6", "iDRAC 9"}) {
		t.Errorf("asdf Mgmt = %v", g.Mgmt)
	}
	if h := vgs[0].Groups[1].Hosts; !reflect.DeepEqual(h, []string{"d1", "h3"}) {
		t.Errorf("qwer 호스트 = %v", h)
	}
	// 혼합 벤더 그룹에서도 wide CSV 가 호스트마다 자기 벤더를 보인다
	rows := wideHostsRows(g)
	if len(rows) != 5 || rows[2][3] != "HPE" || rows[4][3] != "Dell" {
		t.Errorf("wide_hosts 행 = %v", rows)
	}
}

// (b) 태그 줄과 없는 줄이 섞이면: 없는 줄은 벤더 → 관리망 규칙 그대로, 태그 항목은 처음 나온 자리에 놓인다.
func TestGroupLinesMixedTagUntagged(t *testing.T) {
	lines := []ResultLine{
		gtLine("", "HPE", "h1", "iLO 5", "A"),
		gtLine("tagA", "Dell", "d1", "iDRAC 9", "A"),
		gtLine("", "Dell", "d2", "iDRAC 9", "A"),
		gtLine("", "HPE", "h2", "iLO 6", "B"),
		gtLine("tagA", "HPE", "h3", "iLO 5", "A"),
	}
	vgs := groupLines(lines)
	var order []string
	for _, vg := range vgs {
		order = append(order, vg.Vendor)
	}
	if !reflect.DeepEqual(order, []string{"HPE", tagGroupVendor, "Dell"}) {
		t.Fatalf("최상위 순서 = %v", order)
	}
	if got := groupNames(vgs[0]); !reflect.DeepEqual(got, []string{"iLO 5", "iLO 6"}) { // 속성 집합이 달라 합쳐지지 않음
		t.Errorf("HPE 그룹 = %v", got)
	}
	if len(vgs[0].Groups[0].Hosts) != 1 || vgs[0].Groups[0].Hosts[0] != "h1" {
		t.Errorf("HPE/iLO 5 에 태그 줄이 섞임: %v", vgs[0].Groups[0].Hosts)
	}
	if len(vgs[1].Groups) != 1 || !reflect.DeepEqual(vgs[1].Groups[0].Hosts, []string{"d1", "h3"}) {
		t.Errorf("태그 항목 = %+v", vgs[1])
	}
	if len(vgs[2].Groups) != 1 || !reflect.DeepEqual(vgs[2].Groups[0].Hosts, []string{"d2"}) {
		t.Errorf("Dell = %+v", vgs[2])
	}
}

// 태그가 없으면 예전과 같다: groupLines 가 태그 항목을 만들지 않는다.
func TestGroupLinesNoTagNoTagEntry(t *testing.T) {
	for _, vg := range groupLines([]ResultLine{gtLine("", "HPE", "h1", "iLO 5", "A")}) {
		if vg.Vendor == tagGroupVendor {
			t.Fatalf("태그 없는 줄에서 태그 항목이 생김: %+v", vg)
		}
	}
}

// (c) CSV 파일 이름: 태그 그룹은 wide_*_tag_<태그>.csv, 이름 조각이 겹치면 _2.
func TestWriteResultCSVsTagFileNames(t *testing.T) {
	lines := []ResultLine{
		gtLine("asdf", "HPE", "h1", "iLO 5", "A"),
		gtLine("a/b", "Dell", "d1", "iDRAC 9", "A"),
		gtLine("a|b", "Dell", "d2", "iDRAC 9", "A"), // "a/b" 와 같은 이름 조각 "a_b"
		gtLine("서버", "HPE", "h2", "iLO 5", "A"),
		gtLine("서버!", "HPE", "h3", "iLO 5", "A"), // "!" 가 _ 로 바뀌어 "서버_" (한글은 유지)
		gtLine("A_B", "HPE", "h4", "iLO 5", "A"), // 대소문자만 다름: a_b
		gtLine("", "HPE", "h5", "iLO 5", "A"),
	}
	dir := t.TempDir()
	paths, err := writeResultCSVs(dir, "p_", lines)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, p := range paths {
		got = append(got, filepath.Base(p))
	}
	sort.Strings(got)
	// "A_B" 와 "a_b" 는 대소문자 구분 없는 파일시스템에서 겹치므로 _3 이 붙는다. 한글 태그는 그대로 남는다.
	want := []string{
		"p_long.csv", "p_summary.csv",
		"p_wide_attrs_HPE_iLO_5.csv", "p_wide_attrs_tag_a_b.csv", "p_wide_attrs_tag_a_b_2.csv", "p_wide_attrs_tag_A_B_3.csv",
		"p_wide_attrs_tag_asdf.csv", "p_wide_attrs_tag_서버.csv", "p_wide_attrs_tag_서버_.csv",
		"p_wide_hosts_HPE_iLO_5.csv", "p_wide_hosts_tag_a_b.csv", "p_wide_hosts_tag_a_b_2.csv", "p_wide_hosts_tag_A_B_3.csv",
		"p_wide_hosts_tag_asdf.csv", "p_wide_hosts_tag_서버.csv", "p_wide_hosts_tag_서버_.csv",
	}
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("파일 이름\n got %v\nwant %v", got, want)
	}
	for _, p := range paths {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("파일 없음: %v", err)
		}
	}
	// 태그가 있으면 summary·long 끝에 호스트태그 열
	sr := summaryRows(lines)
	if sr[0][len(sr[0])-1] != hostTagHeader || sr[1][len(sr[1])-1] != "asdf" {
		t.Errorf("summary 행 = %v", sr[:2])
	}
	lr := longRows(lines)
	if lr[0][len(lr[0])-1] != hostTagHeader || lr[2][len(lr[2])-1] != "a/b" {
		t.Errorf("long 행 = %v", lr[:3])
	}
	// 태그가 없으면 예전 양식 그대로
	if h := longRows(lines[6:])[0]; len(h) != len(csvLineHeader) {
		t.Errorf("태그 없는 long 헤더가 바뀜: %v", h)
	}
	if h := summaryRows(lines[6:])[0]; len(h) != 9 {
		t.Errorf("태그 없는 summary 헤더가 바뀜: %v", h)
	}
}
