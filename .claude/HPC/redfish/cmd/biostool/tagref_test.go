package main

// 전체비교 기준을 태그로 고르는 규칙 시험 (tagref): diff.txt 둘째 칸 = 태그, 태그 모드(대상에 태그 있음)에서만
// 같은 태그의 기준만 쓴다. 가짜 스냅샷으로 classify·failedFor·xmodel 을, mock BMC 로 CompareCancel 을 확인한다.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// mustParseRefList 는 시험용입니다: 오류 없는 diff.txt 텍스트를 해석합니다.
func mustParseRefList(text string) []refEntry {
	es, err := parseRefList(text)
	if err != nil {
		panic(err)
	}
	return es
}

// tagSnap 은 가짜 스냅샷입니다 (모델 키는 hpe|<model>). ip 를 호스트 이름으로도 씁니다.
func tagSnap(host, tag, model, bios string) *biosSnap {
	s := &biosSnap{Target: Target{Input: host, Hostname: host, IP: host, Tag: tag}, Vendor: "HPE", Model: model, BiosVersion: bios,
		Attrs: map[string]string{"A": "1"}}
	s.Key = "hpe|" + normalizeModel(model)
	return s
}

// tagEnv 는 refs(이름·태그·스냅샷) 로 태그 모드 환경을 만듭니다.
func tagEnv(tagMode bool, refs ...*allRef) *allEnv {
	e := &allEnv{ign: newIgnoreSet(nil, "test"), refByID: map[string]*allRef{}, byKey: map[string][]*allRef{}, tagMode: tagMode, refLabel: "diff.txt"}
	for _, r := range refs {
		e.refByID[r.ID] = r
	}
	e.indexRefs(refs)
	return e
}

func tagRef(s *biosSnap, tag string) *allRef {
	return &allRef{Entry: refEntry{Input: s.Target.Hostname, Tag: tag}, Target: s.Target, ID: targetID(s.Target), Snap: s}
}

func TestParseRefListTag(t *testing.T) {
	es, err := parseRefList("a\nb  asdf\t# 주석\nc qwer\nb zzz\n")
	if err != nil {
		t.Fatal(err)
	}
	if len(es) != 3 || es[0].Tag != "" || es[1].Input != "b" || es[1].Tag != "asdf" || es[2].Tag != "qwer" {
		t.Errorf("해석 = %+v (같은 hostname 은 처음 것만)", es)
	}
	// 3칸 이상: 읽기는 되고(예전 모델 메모 호환) 태그 모드에서만 오류
	es, err = parseRefList("a\nb asdf 모델 같은 메모\n")
	if err != nil || len(es) != 2 || es[1].Tag != "asdf" || es[1].Extra != "모델 같은 메모" {
		t.Fatalf("3칸 해석 = %+v %v", es, err)
	}
	if err := refTagCheck(es, false); err != nil {
		t.Errorf("태그 모드 아님 = %v (오류 아님이어야 함)", err)
	}
	if err := refTagCheck(es, true); err == nil || !strings.Contains(err.Error(), "칸이 3개 이상입니다") || !strings.Contains(err.Error(), "b asdf 모델 같은 메모") {
		t.Errorf("태그 모드 3칸 오류 = %v", err)
	}
	p := filepath.Join(t.TempDir(), "diff.txt")
	writeFileT(t, p, "a\nb DL360 Gen11\n")
	if _, err := loadRefList(p); err != nil {
		t.Errorf("예전 diff.txt(공백 있는 모델 메모) 읽기 = %v", err)
	}
}

func TestClassifyTagMode(t *testing.T) {
	ra := tagRef(tagSnap("r-asdf", "asdf", "DL360Gen11", "1.0"), "asdf")
	rq := tagRef(tagSnap("r-qwer", "qwer", "DL360Gen11", "1.0"), "qwer")
	ru := tagRef(tagSnap("r-none", "", "DL360Gen11", "1.0"), "")
	rOther := tagRef(tagSnap("r-gen10", "zxcv", "DL360Gen10", "1.0"), "zxcv")
	e := tagEnv(true, rq, ra, ru, rOther) // qwer 가 먼저 나와도 asdf 대상은 asdf 기준만

	h := &allHost{Target: Target{Hostname: "t1", IP: "t1", Tag: "asdf"}, Snap: tagSnap("t1", "asdf", "DL360Gen11", "1.0")}
	e.classify(h)
	if h.Ref != ra || h.Status != StatusSame {
		t.Errorf("asdf 대상 = %s 기준 %v, 기대 r-asdf", h.Status, h.Ref)
	}
	h = &allHost{Target: Target{Hostname: "t2", IP: "t2"}, Snap: tagSnap("t2", "", "DL360Gen11", "1.0")}
	e.classify(h)
	if h.Ref != ru {
		t.Errorf("태그 없는 대상의 기준 = %v, 기대 태그 없는 r-none", h.Ref)
	}
	// 태그 있는 기준이 모델이 달라도: 태그만 있고 모델 키가 다르면 모델이 다름
	h = &allHost{Target: Target{Hostname: "t3", IP: "t3", Tag: "zxcv"}, Snap: tagSnap("t3", "zxcv", "DL360Gen11", "1.0")}
	e.classify(h)
	if h.Status != StatusNoReference || h.Detail != "태그 zxcv 기준과 모델이 다름 (대상 모델 키 "+h.Snap.Key+")" {
		t.Errorf("같은 태그·다른 모델 = %s %q", h.Status, h.Detail)
	}
	// 그 태그의 기준이 아예 없음
	h = &allHost{Target: Target{Hostname: "t4", IP: "t4", Tag: "nope"}, Snap: tagSnap("t4", "nope", "DL360Gen11", "1.0")}
	e.classify(h)
	if h.Status != StatusNoReference || h.Detail != "태그 nope 의 기준이 diff.txt 에 없음" {
		t.Errorf("기준 없는 태그 = %s %q", h.Status, h.Detail)
	}
	// 태그 없는 대상인데 태그 없는 기준이 없는 경우
	e2 := tagEnv(true, ra)
	h = &allHost{Target: Target{Hostname: "t5", IP: "t5"}, Snap: tagSnap("t5", "", "DL360Gen11", "1.0")}
	e2.classify(h)
	if h.Status != StatusNoReference || !strings.Contains(h.Detail, "태그 없음 의 기준이") {
		t.Errorf("태그 없는 대상 = %s %q", h.Status, h.Detail)
	}
	// BIOS 버전 우선은 태그로 거른 뒤에 적용
	ra2 := tagRef(tagSnap("r-asdf2", "asdf", "DL360Gen11", "2.0"), "asdf")
	rq2 := tagRef(tagSnap("r-qwer2", "qwer", "DL360Gen11", "3.0"), "qwer")
	e3 := tagEnv(true, ra, ra2, rq2)
	h = &allHost{Target: Target{Hostname: "t6", IP: "t6", Tag: "asdf"}, Snap: tagSnap("t6", "asdf", "DL360Gen11", "3.0")}
	e3.classify(h)
	if h.Ref != ra || !strings.Contains(h.notesText(), noteMultiRef) {
		t.Errorf("t6 기준 = %v notes %q, 기대 asdf 중 첫 기준 (qwer 의 같은 BIOS 는 쓰지 않음)", h.Ref, h.notesText())
	}
}

func TestClassifyNonTagModeIgnoresRefTags(t *testing.T) {
	ra := tagRef(tagSnap("r-asdf", "asdf", "DL360Gen11", "1.0"), "asdf")
	e := tagEnv(false, ra)
	h := &allHost{Target: Target{Hostname: "t1", IP: "t1"}, Snap: tagSnap("t1", "", "DL360Gen11", "1.0")}
	e.classify(h)
	if h.Ref != ra || h.Status != StatusSame {
		t.Errorf("태그 모드가 아니면 기준 태그는 무시 = %s %v", h.Status, h.Ref)
	}
	h = &allHost{Target: Target{Hostname: "t2", IP: "t2"}, Snap: tagSnap("t2", "", "DL360Gen10", "1.0")}
	e.classify(h)
	if h.Status != StatusNoReference || h.Detail != "모델 키 "+h.Snap.Key+" 의 기준이 diff.txt 에 없음" {
		t.Errorf("일반 NO_REFERENCE 문구 = %q", h.Detail)
	}
}

func TestFailedForTag(t *testing.T) {
	bad := func(host, tag string) *allRef {
		s := tagSnap(host, tag, "", "")
		s.Status = StatusUnreachable
		return tagRef(s, tag)
	}
	e := tagEnv(true, bad("dead-asdf", "asdf"), bad("dead-qwer", "qwer"))
	got := e.failedFor(tagSnap("t", "asdf", "DL360Gen11", "1"))
	if len(got) != 1 || got[0] != "dead-asdf(UNREACHABLE)" {
		t.Errorf("태그 모드 failedFor = %v", got)
	}
	if got := e.failedFor(tagSnap("t", "", "DL360Gen11", "1")); len(got) != 0 {
		t.Errorf("태그 없는 대상 failedFor = %v", got)
	}
	e.tagMode = false
	if got := e.failedFor(tagSnap("t", "asdf", "DL360Gen11", "1")); len(got) != 2 {
		t.Errorf("일반 failedFor = %v, 기대 2개 (모델 칸 없이 모두 후보)", got)
	}
	// classify 에서도: 태그가 같은 읽기 실패 기준이 있으면 REF_UNREACHABLE, 없으면 NO_REFERENCE
	e.tagMode = true
	h := &allHost{Target: Target{Hostname: "t", IP: "t", Tag: "asdf"}, Snap: tagSnap("t", "asdf", "DL360Gen11", "1")}
	e.classify(h)
	if h.Status != StatusRefUnreachable || strings.Contains(h.Detail, "dead-qwer") {
		t.Errorf("asdf = %s %q", h.Status, h.Detail)
	}
	h = &allHost{Target: Target{Hostname: "t", IP: "t", Tag: "zzz"}, Snap: tagSnap("t", "zzz", "DL360Gen11", "1")}
	e.classify(h)
	if h.Status != StatusNoReference || h.Detail != "태그 zzz 의 기준이 diff.txt 에 없음" {
		t.Errorf("zzz = %s %q", h.Status, h.Detail)
	}
}

// 실제 mock BMC: 태그 있는 대상 + 비교서버 직접 입력 ("hostname 태그" 줄) → CompareCancel 의 태그 모드.
func TestCompareTagModeManualRefs(t *testing.T) {
	_, ra := startAllSrv(t, "hpe-dl360gen11", nil)
	_, rq := startAllSrv(t, "hpe-dl360gen11", nil)
	_, ta := startAllSrv(t, "hpe-dl360gen11", nil)
	_, tn := startAllSrv(t, "hpe-dl360gen11", nil)
	rc := checkRCFor(t, 2, "", ta+" asdf", tn)
	u := runUnifiedT(t, rc, newIgnoreSet(nil, "test"))
	if !u.tagMode() {
		t.Fatal("태그 있는 대상인데 태그 모드가 아님")
	}
	// 공백이 구분자가 아니라 "hostname 태그" 줄 (태그 모드)
	res := compareT(t, u, compareRef{Hosts: []string{ra + " asdf", rq + " qwer"}})
	if !res.TagMode {
		t.Error("compareResult.TagMode false")
	}
	if h := cmpHostOf(t, res, ta); h.Ref == nil || h.Ref.Target.Hostname != ra {
		t.Errorf("asdf 대상 기준 = %v (%s %s), 기대 %s", h.Ref, h.Status, h.Detail, ra)
	}
	h := cmpHostOf(t, res, tn)
	if h.Status != StatusNoReference || h.Detail != "태그 없음 의 기준이 비교서버 입력 에 없음" {
		t.Errorf("태그 없는 대상 = %s %q (비교서버 모두 태그 있음)", h.Status, h.Detail)
	}
	// 줄에 3칸이면 오류
	if _, err := u.Compare(compareRef{Hosts: []string{ra + " asdf 메모"}}, progStage{}); err == nil || !strings.Contains(err.Error(), "칸이 3개 이상") {
		t.Errorf("3칸 오류 = %v", err)
	}
	// 태그 모드에서 -ref 처럼 태그 없는 비교서버면 태그 있는 대상은 NO_REFERENCE (명확한 문구)
	res = compareT(t, u, compareRef{Hosts: []string{ra}})
	if h := cmpHostOf(t, res, ta); h.Status != StatusNoReference || h.Detail != "태그 asdf 의 기준이 비교서버 입력 에 없음" {
		t.Errorf("태그 없는 비교서버 = %s %q", h.Status, h.Detail)
	}
	if h := cmpHostOf(t, res, tn); h.Ref == nil {
		t.Errorf("태그 없는 대상은 태그 없는 비교서버와 비교 = %s %q", h.Status, h.Detail)
	}
}

// 태그가 없는 실행은 diff.txt 의 둘째 칸이 있어도 지금까지와 같다.
func TestCompareNonTagModeIgnoresDiffTags(t *testing.T) {
	_, ra := startAllSrv(t, "hpe-dl360gen11", nil)
	_, ta := startAllSrv(t, "hpe-dl360gen11", nil)
	rc := checkRCFor(t, 2, "", ta)
	u := runUnifiedT(t, rc, newIgnoreSet(nil, "test"))
	if u.tagMode() {
		t.Fatal("태그가 없는데 태그 모드")
	}
	diff := filepath.Join(t.TempDir(), "diff.txt")
	writeFileT(t, diff, ra+" qwer\n")
	res := compareT(t, u, compareRef{DiffFile: diff})
	if res.TagMode {
		t.Error("TagMode true")
	}
	if h := cmpHostOf(t, res, ta); h.Ref == nil || h.Status != StatusSame {
		t.Errorf("대상 = %s %q, 기대 기준 태그 무시하고 비교", h.Status, h.Detail)
	}
}

func TestXModelPlansPerTag(t *testing.T) {
	b1 := tagSnap("b-asdf", "asdf", "DL360Gen11", "1")
	b2a := tagSnap("b-qwer1", "qwer", "DL360Gen11", "1")
	b2b := tagSnap("b-qwer2", "qwer", "DL360Gen11", "1")
	a1 := tagSnap("a1", "asdf", "DL360Gen10", "1")
	a2 := tagSnap("a2", "asdf", "DL360Gen10", "1")
	a3 := tagSnap("a3", "qwer", "DL360Gen10", "1") // qwer 는 기준이 2대 → 묻지 않음
	a4 := tagSnap("a4", "", "DL360Gen10", "1")     // 태그 없음 → 묻지 않음
	same := tagSnap("s1", "asdf", "DL360Gen11", "1")
	cmp := &compareResult{TagMode: true, Ref: compareRef{DiffFile: "diff.txt"},
		Refs: []*allRef{tagRef(b1, "asdf"), tagRef(b2a, "qwer"), tagRef(b2b, "qwer")}}
	ps := xmodelPlansFor(cmp, []*biosSnap{a1, a2, a3, a4, same})
	if len(ps) != 1 || ps[0].Tag != "asdf" || ps[0].BHost != "b-asdf" || len(ps[0].Groups) != 1 || len(ps[0].Groups[0]) != 2 {
		t.Fatalf("계획 = %+v", ps)
	}
	want := "[태그 asdf] 모델이 다른 대상이 있습니다 (HPE DL360Gen10 2대, HPE DL360Gen11 2대). 모델이 달라도 비교할까요? (y/N)"
	if ps[0].Question() != want {
		t.Errorf("질문 = %q", ps[0].Question())
	}
	c := buildXModel(ps[0].B, ps[0].Groups[0], newIgnoreSet(nil, "t"), protectFor(nil))
	c.Tag = ps[0].Tag
	if got := c.FileName(); got != "compare_xmodel_asdf_DL360Gen10_vs_DL360Gen11.csv" {
		t.Errorf("파일 이름 = %s", got)
	}
	c.Tag = "한글/태그"
	if got := c.FileName(); got != "compare_xmodel_한글_태그_DL360Gen10_vs_DL360Gen11.csv" {
		t.Errorf("태그 정리 파일 이름 = %s", got)
	}
	// 읽지 못한 기준은 세지 않는다: qwer 기준 하나가 실패하면 나머지 1대가 단독 기준이 된다
	b2b.Status = StatusUnreachable
	if ps = xmodelPlansFor(cmp, []*biosSnap{a1, a3}); len(ps) != 2 || ps[1].Tag != "qwer" {
		t.Errorf("실패 기준 제외 후 계획 = %+v", ps)
	}
	// 태그 모드가 아니면 기존 규칙 (diff.txt 는 묻지 않음)
	cmp.TagMode = false
	if ps = xmodelPlansFor(cmp, []*biosSnap{a1}); len(ps) != 0 {
		t.Errorf("일반 diff.txt 는 묻지 않음: %+v", ps)
	}
}

func writeFileT(t *testing.T, path, text string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
}
