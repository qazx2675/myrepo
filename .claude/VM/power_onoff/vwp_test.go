package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestFindVWP(t *testing.T) {
	vms := []*VMInfo{
		{Name: "a", FolderPath: "DC1/vm/svr1"},
		{Name: "b", FolderPath: "DC1/vm/VWP-Prod/svr1"},
		{Name: "c", FolderPath: "DC1/vm/x", PortGroups: []string{"vwp-cae-10-1-2-0", "other"}},
		{Name: "d", FolderPath: "DC1/vm/Vwp/sub/leaf"},
		{Name: "e", FolderPath: "DC1/vm/x", PortGroups: []string{"VM Network"}},
		{Name: "f", FolderPath: "DC1/vm/vwp", PortGroups: []string{"vWp1"}},
	}
	hits := FindVWP(vms)
	var got []string
	for _, h := range hits {
		got = append(got, h.VM.Name)
	}
	if strings.Join(got, ",") != "b,c,d,f" {
		t.Fatalf("hits=%v", got)
	}
	if r := hits[0].Reasons[0]; r != "폴더: DC1/vm/VWP-Prod/svr1 ('VWP-Prod')" {
		t.Errorf("reason=%q", r)
	}
	if r := hits[1].Reasons[0]; r != "포트그룹: vwp-cae-10-1-2-0" {
		t.Errorf("reason=%q", r)
	}
	if len(hits[3].Reasons) != 2 {
		t.Errorf("f reasons=%v", hits[3].Reasons)
	}
	if len(FindVWP([]*VMInfo{vms[0], vms[4]})) != 0 {
		t.Error("해당 없음인데 걸림")
	}
}

func TestPrintVWPBanner(t *testing.T) {
	hits := FindVWP([]*VMInfo{
		{Name: "svr1ev01", FolderPath: "DC1/vm/VWP-Prod/svr1", PortGroups: []string{"vwp-x"}},
	})
	for _, color := range []bool{false, true} {
		var b bytes.Buffer
		PrintVWPBanner(&b, hits, 5, color)
		s := b.String()
		for _, want := range []string{
			"[경고] vwp대상 장비가 포함되어있습니다.",
			"vwp장비의 경우 vwp담당자에게 확인이 필요합니다.",
			"svr1ev01", "포트그룹: vwp-x", "(VWP 해당 1대 / 전체 5대)", "!!!!!!",
		} {
			if !strings.Contains(s, want) {
				t.Errorf("color=%v: %q 없음\n%s", color, want, s)
			}
		}
		if esc := strings.Contains(s, "\x1b["); esc != color {
			t.Errorf("color=%v 인데 ANSI=%v", color, esc)
		}
		if !color && strings.Index(s, "!!!!!!") != 0 {
			t.Error("맨 위가 테두리가 아님")
		}
	}
}
