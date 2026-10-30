package main

// xmodel.go 시험: 엔진(합집합·판정 4종·제외)과 TUI 흐름(묻기·CSV·N·같은 모델·diff.txt).

import (
	"encoding/csv"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"biostool/internal/mockbmc"
)

func xmSnap(host, ip, model string, attrs map[string]string) *biosSnap {
	s := &biosSnap{Target: Target{Hostname: host, IP: ip}, Vendor: "HPE", Model: model, Attrs: attrs}
	s.Key = strings.ToLower(s.Vendor) + "|" + normalizeModel(model)
	return s
}

func TestXModelEngine(t *testing.T) {
	b := xmSnap("ref", "10.0.0.1", "DL360Gen11", map[string]string{
		"Same": "1", "Diff": "1", "OnlyB": "x", "PartLack": "1", "ProcTurbo": "On", "Ign": "1", "IscsiIqn": "b", "BootMode": "Uefi",
	})
	a1 := xmSnap("a1", "10.0.0.2", "DL360Gen10", map[string]string{
		"Same": "1", "Diff": "2", "OnlyA": "y", "PartLack": "1", "ProcTurbo": "On", "Ign": "9", "IscsiIqn": "a", "BootMode": "Legacy",
	})
	a2 := xmSnap("a2", "", "DL360Gen10", map[string]string{ // IP 없음 → hostname, PartLack 없음 → 다름
		"Same": "1", "Diff": "1", "OnlyA": "y", "ProcTurbo": "On",
	})
	ign := newIgnoreSet([]string{"Ign"}, "test")
	c := buildXModel(b, []*biosSnap{a1, a2}, ign, protectFor(nil))
	want := map[string]string{
		"Diff": xmDiff, "OnlyA": "DL360Gen10에만", "OnlyB": "DL360Gen11에만", "PartLack": xmDiff, "ProcTurbo": xmSame, "Same": xmSame,
	}
	got := map[string]string{}
	var order []string
	for _, a := range c.Attrs {
		got[a.Name] = a.Verdict
		order = append(order, a.Name)
	}
	if len(got) != len(want) {
		t.Errorf("속성 = %v, 기대 %v", got, want)
	}
	for n, v := range want {
		if got[n] != v {
			t.Errorf("%s = %q, 기대 %q", n, got[n], v)
		}
	}
	if strings.Join(order, ",") != "Diff,OnlyA,OnlyB,PartLack,ProcTurbo,Same" {
		t.Errorf("정렬 = %v", order)
	}
	if c.Excluded != 3 || c.Same != 2 || c.Diff != 2 || c.OnlyA != 1 || c.OnlyB != 1 { // Ign, IscsiIqn, BootMode 제외
		t.Errorf("개수 = %+v", c)
	}
	rows := c.Rows("ref")
	if strings.Join(rows[0], "|") != "Attribute|판정|DL360Gen11 비교서버(ref)|10.0.0.2|a2" {
		t.Errorf("머리글 = %v", rows[0])
	}
	for _, r := range rows {
		if r[0] == "OnlyA" && strings.Join(r, "|") != "OnlyA|DL360Gen10에만|-|y|y" {
			t.Errorf("OnlyA 줄 = %v", r)
		}
		if r[0] == "PartLack" && strings.Join(r, "|") != "PartLack|다름|1|1|-" {
			t.Errorf("PartLack 줄 = %v", r)
		}
	}
	if c.FileName() != "compare_xmodel_DL360Gen10_vs_DL360Gen11.csv" {
		t.Errorf("파일 이름 = %s", c.FileName())
	}
	if want := "같음 2 · 다름 2 · DL360Gen10에만 1 · DL360Gen11에만 1 (제외 3)"; c.Summary() != want {
		t.Errorf("요약 = %q", c.Summary())
	}
}

func TestXModelNamesFallbackToLabel(t *testing.T) {
	b := xmSnap("b", "1", "DL360", nil)
	a := xmSnap("a", "2", "DL360", nil)
	a.Vendor = "Dell"
	bn, an := xmodelNames(b, a)
	if bn != "HPE DL360" || an != "Dell DL360" {
		t.Errorf("이름 = %q %q", bn, an)
	}
}

// xmodelFlow 는 대상 gen10 2대, 기준 ref(비교서버 1대 또는 diff.txt) 로 흐름을 돌립니다.
func xmodelFlow(t *testing.T, refTree string, useDiff bool, answer string) (*flowRig, string) {
	t.Helper()
	_, u1 := startAllSrv(t, "hpe-dl360gen10", nil)
	_, u2 := startAllSrv(t, "hpe-dl360gen10", func(s *mockbmc.Server) {
		setAttrs(t, s, map[string]interface{}{"ProcHyperthreading": "Disabled"})
	})
	_, ur := startAllSrv(t, refTree, nil)
	e, o := flowEnv(t, "")
	o.user, o.profile = e.writeFile("targets.txt", u1, u2), "VM"
	swapFlowPlan(t, func(*Config, *unifiedRun, *compareResult) *setPlan { return &setPlan{} }, nil)
	ref := compareRef{Hosts: []string{ur}}
	if useDiff {
		ref = compareRef{DiffFile: e.writeFile("diff.txt", ur)}
	}
	script := []flowStep{{"compare-auto", ""}}
	if answer != "" {
		script = append(script, flowStep{"xmodel", answer})
	}
	script = append(script, flowStep{"profile-result", "\t"}, flowStep{"compare-result", "qq"})
	var msg string
	fr, err := runFlowTC(t, biosTUIArgs{O: o, UserSet: true, Ref: ref}, "", script, nil, func(i int, f *biosFlow) {
		if script[i].name == "profile-result" {
			msg = f.ui.msg
		}
	})
	if err != nil {
		t.Fatalf("흐름 오류: %v", err)
	}
	return fr, msg
}

func xmodelFiles(fr *flowRig) []string {
	m, _ := filepath.Glob(filepath.Join(fr.f.u.Check.Dir, "compare_xmodel_*.csv"))
	return m
}

func TestBiosFlowXModelYes(t *testing.T) {
	fr, msg := xmodelFlow(t, "hpe-dl360gen11", false, "y")
	files := xmodelFiles(fr)
	if len(files) != 1 || filepath.Base(files[0]) != "compare_xmodel_ProLiant_DL360_Gen10_vs_ProLiant_DL360_Gen11.csv" {
		t.Fatalf("파일 = %v", files)
	}
	if !strings.Contains(fr.screen(), "모델이 다른 대상이 있습니다 (") {
		t.Errorf("질문이 화면에 없음")
	}
	b, err := os.ReadFile(files[0])
	must(t, err)
	rows, err := csv.NewReader(strings.NewReader(strings.TrimPrefix(string(b), "\xef\xbb\xbf"))).ReadAll()
	must(t, err)
	if len(rows) < 2 || len(rows[0]) != 5 || rows[0][0] != "Attribute" || rows[0][1] != "판정" ||
		!strings.HasPrefix(rows[0][2], "ProLiant DL360 Gen11 비교서버(") {
		t.Fatalf("머리글 = %v (%d줄)", rows[0], len(rows))
	}
	prot := protectFor(nil)
	for _, r := range rows[1:] {
		if prot.match(r[0]) != "" {
			t.Errorf("보호 속성 %s 가 들어 있음", r[0])
		}
	}
	for _, want := range []string{"모델이 다른 비교 (ProLiant DL360 Gen10 2대 ↔ ProLiant DL360 Gen11): 같음 ", " · 다름 ", "(제외 ", "compare_xmodel_"} {
		if !strings.Contains(msg, want) {
			t.Errorf("안내에 %q 없음: %q", want, msg)
		}
	}
	for _, h := range fr.f.cmp.Hosts {
		if h.Status != StatusNoReference {
			t.Errorf("%s = %s, 기대 NO_REFERENCE (설정에 쓰이면 안 됨)", h.Target.Hostname, h.Status)
		}
	}
	if !inList(fr.f.csvs, files[0]) || !strings.Contains(fr.out.String(), "compare_xmodel_") {
		t.Errorf("파일 목록에 없음:\n%s", fr.out.String())
	}
}

func TestBiosFlowXModelNo(t *testing.T) {
	fr, msg := xmodelFlow(t, "hpe-dl360gen11", false, "n")
	if f := xmodelFiles(fr); len(f) != 0 {
		t.Errorf("N 인데 파일 = %v", f)
	}
	if strings.Contains(msg, "모델이 다른 비교") {
		t.Errorf("안내 = %q", msg)
	}
}

func TestBiosFlowXModelSameModelNoPrompt(t *testing.T) {
	fr, _ := xmodelFlow(t, "hpe-dl360gen10", false, "") // xmodel 단계가 나오면 예상 밖 단계로 실패
	if f := xmodelFiles(fr); len(f) != 0 {
		t.Errorf("파일 = %v", f)
	}
}

func TestBiosFlowXModelDiffFileNoPrompt(t *testing.T) {
	fr, _ := xmodelFlow(t, "hpe-dl360gen11", true, "")
	if f := xmodelFiles(fr); len(f) != 0 {
		t.Errorf("파일 = %v", f)
	}
}

func TestXModelPlanQuestion(t *testing.T) {
	b := xmSnap("ref", "10.0.0.1", "DL360Gen11", nil)
	a1 := xmSnap("a1", "10.0.0.2", "DL360Gen10", nil)
	a2 := xmSnap("a2", "10.0.0.3", "DL360Gen10", nil)
	same := xmSnap("s", "10.0.0.4", "DL360Gen11", nil)
	bad := xmSnap("x", "10.0.0.5", "DL380", nil)
	bad.Status = StatusXMLReadOnly
	cmp := &compareResult{Ref: compareRef{Hosts: []string{"ref"}}, Refs: []*allRef{{Target: b.Target, ID: targetID(b.Target), Snap: b}}}
	p := xmodelPlanFor(cmp, []*biosSnap{a1, a2, same, bad})
	if p == nil {
		t.Fatal("계획 없음")
	}
	want := "모델이 다른 대상이 있습니다 (HPE DL360Gen10 2대, HPE DL360Gen11 2대). 모델이 달라도 비교할까요? (y/N)"
	if p.Question() != want {
		t.Errorf("질문 = %q", p.Question())
	}
	if xmodelPlanFor(cmp, []*biosSnap{same}) != nil {
		t.Error("같은 모델뿐이면 묻지 않음")
	}
	cmp.Ref = compareRef{DiffFile: "diff.txt"}
	if xmodelPlanFor(cmp, []*biosSnap{a1}) != nil {
		t.Error("diff.txt 는 묻지 않음")
	}
}
