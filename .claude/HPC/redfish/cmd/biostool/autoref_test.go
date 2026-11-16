package main

// autoref.go 시험: 묶음 나누기(태그+모델)·대표 선정 순서(다수결 최고 → 동점이면 다수 BIOS 버전 → 프로파일 difference 0 → 입력 순서)·
// 추가 접속 없음·묶음별 직접 고르기. 가짜 스냅샷으로 규칙을, mock BMC 로 접속 횟수를 확인한다.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"biostool/internal/mockbmc"
)

// arAttrs 는 Opt0..Opt9 = "1" 인 속성에 over 를 덮어쓴 것입니다.
func arAttrs(over map[string]string) map[string]string {
	m := map[string]string{}
	for i := 0; i < 10; i++ {
		m[fmt.Sprintf("Opt%d", i)] = "1"
	}
	for k, v := range over {
		m[k] = v
	}
	return m
}

func arSnap(host, tag, model, bios string, attrs map[string]string) *biosSnap {
	s := &biosSnap{Target: Target{Input: host, Hostname: host, IP: host, Tag: tag}, Vendor: "HPE", Model: model, BiosVersion: bios, Attrs: attrs}
	s.Key = "hpe|" + normalizeModel(model)
	return s
}

func arRun(snaps ...*biosSnap) *unifiedRun {
	return &unifiedRun{Snaps: snaps, Ignore: withUnique(newIgnoreSet(nil, "test"))}
}

func arBundle(t *testing.T, p *autoPlan, name string) *autoBundle {
	t.Helper()
	for _, b := range p.Bundles {
		if b.Name == name {
			return b
		}
	}
	t.Fatalf("묶음 %q 없음: %v", name, arNames(p))
	return nil
}

func arNames(p *autoPlan) []string {
	var out []string
	for _, b := range p.Bundles {
		out = append(out, b.Name)
	}
	return out
}

func arRepHost(p *autoPlan, name string) string {
	for _, b := range p.Bundles {
		if b.Name == name {
			return p.RepSnap(b).Target.Hostname
		}
	}
	return ""
}

func TestAutoRefGroupsByTagAndModel(t *testing.T) {
	u := arRun(
		arSnap("g1", "gold", "R660", "1.0", arAttrs(nil)),
		arSnap("g2", "gold", "R660", "1.0", arAttrs(nil)),
		arSnap("g3", "gold", "DL360 Gen11", "1.0", arAttrs(nil)),
		arSnap("s1", "silver", "R660", "1.0", arAttrs(nil)),
		arSnap("n1", "", "R660", "1.0", arAttrs(nil)),
	)
	p := u.autoPlanFor(nil)
	if !p.TagMode || fmt.Sprint(arNames(p)) != "[[gold] R660 [gold] DL360 Gen11 [silver] R660 [태그 없음] R660]" {
		t.Fatalf("태그 모드 묶음 = %v tagMode=%v (처음 나온 순서)", arNames(p), p.TagMode)
	}
	if b := arBundle(t, p, "[gold] R660"); fmt.Sprint(b.Idx) != "[0 1]" || b.Rep != 0 {
		t.Errorf("[gold] R660 = 구성원 %v 대표 %d", b.Idx, b.Rep)
	}
	// 태그가 없으면(태그 모드 아님) 모델만으로 나눈다
	u2 := arRun(
		arSnap("a", "", "R660", "1.0", arAttrs(nil)),
		arSnap("b", "", "DL360 Gen11", "1.0", arAttrs(nil)),
		arSnap("c", "", "R660", "1.0", arAttrs(nil)),
	)
	p2 := u2.autoPlanFor(nil)
	if p2.TagMode || fmt.Sprint(arNames(p2)) != "[R660 DL360 Gen11]" || len(p2.Bundles[0].Idx) != 2 {
		t.Errorf("태그 없는 묶음 = %v", arNames(p2))
	}
	// 기준 = 대표 호스트 (대표 자신의 태그)
	refs := p.autoRefs()
	if len(refs) != 4 || refs[2].Entry.Tag != "silver" || refs[2].Target.Hostname != "s1" || refs[2].ID != targetID(refs[2].Target) {
		t.Errorf("기준 = %+v", refs)
	}
}

func TestAutoRefMajorityPick(t *testing.T) {
	odd := arAttrs(map[string]string{"Opt0": "9", "Opt1": "9", "Opt2": "9", "Opt3": "9"}) // 입력 순서로 첫 호스트인데 이상 값
	u := arRun(
		arSnap("h1", "", "R660", "1.0", odd),
		arSnap("h2", "", "R660", "1.0", arAttrs(map[string]string{"Opt4": "2"})), // 1개만 다름
		arSnap("h3", "", "R660", "1.0", arAttrs(nil)),
		arSnap("h4", "", "R660", "1.0", arAttrs(nil)),
		arSnap("h5", "", "R660", "1.0", arAttrs(nil)),
	)
	p := u.autoPlanFor(nil)
	b := p.Bundles[0]
	rep := p.RepSnap(b).Target.Hostname
	if rep == "h1" || rep != "h3" { // 완전 일치 3대(h3~h5) 중 입력 순서가 앞선 h3
		t.Errorf("대표 = %s, 기대 h3 (이상 호스트 h1 은 대표가 되면 안 됨)", rep)
	}
	if b.Total != 10 || b.Scores[0] != 6 || b.Scores[2] != 10 {
		t.Errorf("다수결 점수 = %v / 분모 %d", b.Scores, b.Total)
	}
	if want := "BIOS 1.0 5/5대 · 다수결 일치 10/10"; b.Reason != want {
		t.Errorf("사유 = %q, 기대 %q", b.Reason, want)
	}
	// 같은 입력 → 같은 결과 (맵 순서와 무관)
	for i := 0; i < 20; i++ {
		if r := arRepHost(u.autoPlanFor(nil), "R660"); r != "h3" {
			t.Fatalf("반복 %d: 대표 = %s", i, r)
		}
	}
}

func TestAutoRefTieUsesInputOrder(t *testing.T) {
	u := arRun(
		arSnap("z9", "", "R660", "1.0", arAttrs(nil)),
		arSnap("a1", "", "R660", "1.0", arAttrs(nil)),
		arSnap("m5", "", "R660", "1.0", arAttrs(nil)),
	)
	if r := arRepHost(u.autoPlanFor(nil), "R660"); r != "z9" {
		t.Errorf("동률 대표 = %s, 기대 입력 순서 첫 z9 (hostname 순서가 아님)", r)
	}
	// 속성 값이 반반으로 갈려도(동수) 입력 순서가 앞선 쪽 값이 다수결 값
	u = arRun(
		arSnap("x1", "", "R660", "1.0", arAttrs(map[string]string{"Opt0": "A"})),
		arSnap("x2", "", "R660", "1.0", arAttrs(map[string]string{"Opt0": "B"})),
	)
	if r := arRepHost(u.autoPlanFor(nil), "R660"); r != "x1" {
		t.Errorf("반반 대표 = %s, 기대 x1", r)
	}
}

func TestAutoRefPrefersMajorBiosVersion(t *testing.T) {
	// 다수결 일치 점수가 같을 때만 다수 BIOS 버전이 가른다 (h1 은 입력 순서로 첫째지만 BIOS 소수)
	u := arRun(
		arSnap("h1", "", "R660", "2.0", arAttrs(nil)),
		arSnap("h2", "", "R660", "1.0", arAttrs(nil)),
		arSnap("h3", "", "R660", "1.0", arAttrs(nil)),
	)
	p := u.autoPlanFor(nil)
	b := p.Bundles[0]
	if r := p.RepSnap(b).Target.Hostname; r != "h2" {
		t.Errorf("대표 = %s, 기대 h2 (동점 → 다수 BIOS 1.0)", r)
	}
	if !strings.HasPrefix(b.Reason, "BIOS 1.0 2/3대 · 다수결 일치 10/10") {
		t.Errorf("사유 = %q", b.Reason)
	}
	// 점수가 다르면 BIOS 버전이 소수여도 다수결 최고가 대표 (다수결 우선)
	u = arRun(
		arSnap("k1", "", "R660", "2.0", arAttrs(nil)),
		arSnap("k2", "", "R660", "1.0", arAttrs(map[string]string{"Opt0": "2"})),
		arSnap("k3", "", "R660", "1.0", arAttrs(map[string]string{"Opt1": "2"})),
	)
	if r := arRepHost(u.autoPlanFor(nil), "R660"); r != "k1" {
		t.Errorf("대표 = %s, 기대 k1 (점수가 최고면 BIOS 소수여도 대표)", r)
	}
}

func TestAutoRefPrefersProfileOKThenSkips(t *testing.T) {
	mkCheck := func(results ...string) *checkRun {
		c := &checkRun{}
		for _, r := range results {
			c.Hosts = append(c.Hosts, &hostCheck{Items: []itemResult{{Result: r}}})
		}
		return c
	}
	mk := func() *unifiedRun { // 세 호스트의 다수결 점수가 같음 (동점)
		return arRun(
			arSnap("h1", "", "R660", "1.0", arAttrs(nil)),
			arSnap("h2", "", "R660", "1.0", arAttrs(nil)),
			arSnap("h3", "", "R660", "1.0", arAttrs(nil)),
		)
	}
	// 동점이면 프로파일 difference 0건인 h2 가 대표
	u := mk()
	u.Check = mkCheck(StatusFail, StatusOK, StatusFail)
	p := u.autoPlanFor(nil)
	if r := p.RepSnap(p.Bundles[0]).Target.Hostname; r != "h2" || !strings.Contains(p.Bundles[0].Reason, "프로파일 difference 0건") {
		t.Errorf("프로파일 우선 대표 = %s (%s)", r, p.Bundles[0].Reason)
	}
	// 점수가 다르면 프로파일 OK 여도 다수결 최고가 대표 (다수결 우선)
	u = arRun(
		arSnap("h1", "", "R660", "1.0", arAttrs(nil)),
		arSnap("h2", "", "R660", "1.0", arAttrs(map[string]string{"Opt0": "2"})),
		arSnap("h3", "", "R660", "1.0", arAttrs(nil)),
	)
	u.Check = mkCheck(StatusFail, StatusOK, StatusFail)
	if r := arRepHost(u.autoPlanFor(nil), "R660"); r != "h1" {
		t.Errorf("점수 우선 대표 = %s, 기대 h1", r)
	}
	// 0건인 호스트가 없으면 건너뜀 → 다수결 (h1)
	u = mk()
	u.Check = mkCheck(StatusFail, StatusFail, StatusFail)
	p = u.autoPlanFor(nil)
	if r := p.RepSnap(p.Bundles[0]).Target.Hostname; r != "h1" || strings.Contains(p.Bundles[0].Reason, "프로파일") {
		t.Errorf("프로파일 건너뜀 대표 = %s (%s)", r, p.Bundles[0].Reason)
	}
	// 점검 결과를 모르면(Check 없음) 건너뜀
	if r := arRepHost(mk().autoPlanFor(nil), "R660"); r != "h1" {
		t.Errorf("점검 결과 없음 대표 = %s", r)
	}
}

func TestAutoRefSkipsFailedAndXML(t *testing.T) {
	bad := arSnap("bad", "", "R660", "1.0", nil)
	bad.Status = StatusUnsupported
	xml := arSnap("xml", "", "UCS", "", nil)
	xml.Status = StatusXMLReadOnly
	u := arRun(bad, xml, arSnap("ok1", "", "R660", "1.0", arAttrs(nil)), arSnap("ok2", "", "R660", "1.0", arAttrs(map[string]string{"Opt0": "2"})))
	p := u.autoPlanFor(nil)
	if len(p.Bundles) != 1 || fmt.Sprint(p.Bundles[0].Idx) != "[2 3]" {
		t.Fatalf("묶음 = %v (읽기 실패·XML 은 빠져야 함)", arNames(p))
	}
	res, err := u.Compare(compareRef{Auto: true}, progStage{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Hosts[0].Status != StatusUnsupported || res.Hosts[1].Status != StatusXMLReadOnly {
		t.Errorf("읽기 실패·XML 상태 = %s / %s", res.Hosts[0].Status, res.Hosts[1].Status)
	}
	if res.Hosts[2].Status != StatusIsReference || res.Hosts[3].Status != StatusDiff || res.Hosts[3].Ref.Target.Hostname != "ok1" {
		t.Errorf("정상 호스트 = %s / %s", res.Hosts[2].Status, res.Hosts[3].Status)
	}
	// 읽은 서버가 하나도 없으면 오류
	if _, err := arRun(bad, xml).Compare(compareRef{Auto: true}, progStage{}); err == nil || !strings.Contains(err.Error(), "대표서버를 고를 수 있는 서버가 없습니다") {
		t.Errorf("대상 없음 오류 = %v", err)
	}
}

func TestAutoRefSingleHostIsReference(t *testing.T) {
	u := arRun(
		arSnap("g1", "gold", "R660", "1.0", arAttrs(nil)),
		arSnap("s1", "silver", "DL360 Gen11", "2.0", arAttrs(nil)),
	)
	res, err := u.Compare(compareRef{Auto: true}, progStage{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Label != "대표서버 자동 (2대)" || res.Auto == nil || !res.TagMode {
		t.Errorf("라벨 %q auto=%v tag=%v", res.Label, res.Auto, res.TagMode)
	}
	for _, h := range res.Hosts {
		if h.Status != StatusIsReference || h.Ref != nil {
			t.Errorf("%s = %s, 기대 1대뿐인 묶음은 REFERENCE", h.Target.Hostname, h.Status)
		}
	}
	if r := res.Auto.Bundles[0].Reason; !strings.Contains(r, "1대뿐") {
		t.Errorf("사유 = %q", r)
	}
	if res.Auto.Choosable() {
		t.Error("1대뿐인 묶음만 있으면 대표를 바꿀 수 없음")
	}
	// 대표 자신은 REFERENCE, 같은 묶음의 다른 서버는 그 대표와 비교 (다른 태그·모델의 대표와는 비교하지 않음)
	u = arRun(
		arSnap("g1", "gold", "R660", "1.0", arAttrs(nil)),
		arSnap("g2", "gold", "R660", "1.0", arAttrs(map[string]string{"Opt3": "2"})),
		arSnap("s1", "silver", "R660", "1.0", arAttrs(nil)),
	)
	res, _ = u.Compare(compareRef{Auto: true}, progStage{})
	if h := res.Hosts[0]; h.Status != StatusIsReference {
		t.Errorf("g1 = %s", h.Status)
	}
	if h := res.Hosts[1]; h.Status != StatusDiff || h.Ref.Target.Hostname != "g1" || h.NDiff != 1 {
		t.Errorf("g2 = %s ref %v diff %d", h.Status, h.Ref, h.NDiff)
	}
	if h := res.Hosts[2]; h.Status != StatusIsReference {
		t.Errorf("s1 = %s (다른 태그의 대표와 비교하면 안 됨)", h.Status)
	}
}

func TestAutoRefUniqueAttrsNotCounted(t *testing.T) {
	// ServiceTag·MAC 모양 값은 호스트마다 달라도 다수결에서 빠진다 (비교와 같은 기준)
	mk := func(tag, mac string) map[string]string {
		return arAttrs(map[string]string{"ServiceTag": tag, "MyNic": mac})
	}
	u := arRun(
		arSnap("h1", "", "R660", "1.0", mk("T1", "aa:bb:cc:dd:ee:01")),
		arSnap("h2", "", "R660", "1.0", mk("T2", "aa:bb:cc:dd:ee:02")),
	)
	b := u.autoPlanFor(nil).Bundles[0]
	if b.Total != 10 || b.Scores[0] != 10 || b.Scores[1] != 10 {
		t.Errorf("고유값 제외 점수 = %v / %d", b.Scores, b.Total)
	}
}

func TestAutoRefOverridePick(t *testing.T) {
	u := arRun(
		arSnap("h1", "gold", "R660", "1.0", arAttrs(nil)),
		arSnap("h2", "gold", "R660", "1.0", arAttrs(map[string]string{"Opt0": "2"})),
		arSnap("h3", "gold", "R660", "1.0", arAttrs(map[string]string{"Opt1": "2"})),
		arSnap("s1", "silver", "R660", "1.0", arAttrs(nil)),
	)
	base := u.autoPlanFor(nil)
	b := base.Bundles[0]
	id := b.ID(base.TagMode)
	if b.Manual || base.RepSnap(b).Target.Hostname != "h1" || !base.Choosable() {
		t.Fatalf("자동 대표 = %s manual=%v", base.RepSnap(b).Target.Hostname, b.Manual)
	}
	pick := map[string]string{id: targetID(u.Snaps[2].Target)}
	p := u.autoPlanFor(pick)
	b = p.Bundles[0]
	if !b.Manual || p.RepSnap(b).Target.Hostname != "h3" || b.AutoRep != 0 || !strings.Contains(b.Reason, "직접 지정") || !strings.Contains(b.Reason, "자동 선정은 h1") {
		t.Errorf("직접 지정 = %s manual=%v 사유 %q", p.RepSnap(b).Target.Hostname, b.Manual, b.Reason)
	}
	if p.Bundles[1].Manual {
		t.Error("다른 묶음은 그대로여야 함")
	}
	// 같은 묶음의 구성원이 아닌 호스트·자동 대표 자신을 고르면 자동 그대로
	for _, bad := range []string{targetID(u.Snaps[3].Target), "nope", targetID(u.Snaps[0].Target)} {
		q := u.autoPlanFor(map[string]string{id: bad})
		if q.Bundles[0].Manual || q.RepSnap(q.Bundles[0]).Target.Hostname != "h1" {
			t.Errorf("무시해야 할 지정 %q → 대표 %s manual=%v", bad, q.RepSnap(q.Bundles[0]).Target.Hostname, q.Bundles[0].Manual)
		}
	}
	// 직접 지정한 대표로 다시 비교 (접속 없이): h3 가 REFERENCE, h1 은 h3 와 비교
	res, err := u.Compare(compareRef{Auto: true, AutoPick: pick}, progStage{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Hosts[2].Status != StatusIsReference || res.Hosts[0].Ref == nil || res.Hosts[0].Ref.Target.Hostname != "h3" || res.Hosts[0].NDiff != 1 {
		t.Errorf("재비교: h3=%s h1 ref=%v diff=%d", res.Hosts[2].Status, res.Hosts[0].Ref, res.Hosts[0].NDiff)
	}
	if res.Ref.count() != 1 {
		t.Error("AutoPick 은 기준 종류로 세지 않아야 함 (count 는 늘 1)")
	}
	// 직접 고르기 뒤 안내·compare_refs.tsv 에 반영
	if a := res.Auto.Announce(); !strings.Contains(a, "[gold] R660 → h3") || !strings.Contains(a, "[silver] R660 → s1") {
		t.Errorf("안내 = %q", a)
	}
	dir := t.TempDir()
	path, err := writeAutoRefs(dir, res.Auto)
	if err != nil {
		t.Fatal(err)
	}
	b2, _ := os.ReadFile(path)
	txt := string(b2)
	if filepath.Base(path) != "compare_refs.tsv" || !strings.HasPrefix(txt, "bundle\trepresentative\tcount\tbios_version\treason\n") ||
		!strings.Contains(txt, "[gold] R660\th3\t3\t1.0\t직접 지정") || !strings.Contains(txt, "[silver] R660\ts1\t1\t1.0\t묶음에 1대뿐") {
		t.Errorf("compare_refs.tsv:\n%s", txt)
	}
}

func TestAutoRefAnnounceCutsLong(t *testing.T) {
	var snaps []*biosSnap
	for i := 0; i < 12; i++ {
		snaps = append(snaps, arSnap(fmt.Sprintf("host%04d", i), fmt.Sprintf("tag%02d", i), "R660", "1.0", arAttrs(nil)))
	}
	a := arRun(snaps...).autoPlanFor(nil).Announce()
	if !strings.HasPrefix(a, "대표서버: [tag00] R660 → host0000 · [tag01] R660 → host0001") || !strings.Contains(a, " 외 ") || !strings.HasSuffix(a, "묶음") {
		t.Errorf("긴 안내 = %q", a)
	}
}

// xmodel(x 키) 은 대표서버 자동에서는 켜지지 않는다 (모델마다 대표가 있고, 일반·태그 모드 모두 계획이 없어야 함).
func TestAutoRefNoXModelPlan(t *testing.T) {
	for _, tagged := range []bool{false, true} {
		tag := func(s string) string {
			if tagged {
				return s
			}
			return ""
		}
		u := arRun(
			arSnap("a1", tag("gold"), "R660", "1.0", arAttrs(nil)),
			arSnap("a2", tag("gold"), "R660", "1.0", arAttrs(nil)),
			arSnap("b1", tag("gold"), "DL360 Gen11", "1.0", arAttrs(nil)),
		)
		res, err := u.Compare(compareRef{Auto: true}, progStage{})
		if err != nil {
			t.Fatal(err)
		}
		if plans := xmodelPlansFor(res, u.Snaps); len(plans) != 0 {
			t.Errorf("태그=%v: 모델이 달라도 x 계획이 없어야 함: %d건", tagged, len(plans))
		}
		for _, h := range res.Hosts {
			if h.Status == StatusNoReference {
				t.Errorf("태그=%v: %s = NO_REFERENCE (모델마다 대표가 있어야 함)", tagged, h.Target.Hostname)
			}
		}
	}
}

// 대표서버 자동은 이미 읽은 스냅샷만 쓰므로 비교·재비교(직접 지정 포함)가 mock BMC 에 새 요청을 보내지 않는다.
func TestAutoRefNoExtraConnection(t *testing.T) {
	a, ua := startAllSrv(t, "hpe-dl360gen11", nil)
	b, ub := startAllSrv(t, "hpe-dl360gen11", func(s *mockbmc.Server) {
		setAttrs(t, s, map[string]interface{}{"ProcHyperthreading": "Disabled"})
	})
	c, uc := startAllSrv(t, "hpe-dl360gen11", nil)
	d, ud := startAllSrv(t, "dell-r660", nil)
	all := []*mockbmc.Server{a, b, c, d}
	u := runUnifiedT(t, checkRCFor(t, 4, "", ua, ub, uc, ud), nil)
	for _, s := range all {
		assertOneSession(t, s)
	}
	before := callCounts(all...)
	res := compareT(t, u, compareRef{Auto: true})
	if fmt.Sprint(callCounts(all...)) != fmt.Sprint(before) {
		t.Errorf("대표서버 자동이 대상에 다시 접속함: %v → %v", before, callCounts(all...))
	}
	if len(res.Auto.Bundles) != 2 || res.Label != "대표서버 자동 (2대)" {
		t.Fatalf("묶음 = %v 라벨 %q", arNames(res.Auto), res.Label)
	}
	if h := cmpHostOf(t, res, ud); h.Status != StatusIsReference {
		t.Errorf("Dell 1대뿐 = %s", h.Status)
	}
	// 직접 지정 재비교 (ub 를 대표로)
	pick := map[string]string{res.Auto.Bundles[0].ID(res.Auto.TagMode): targetID(u.Snaps[1].Target)}
	res2 := compareT(t, u, compareRef{Auto: true, AutoPick: pick})
	if h := cmpHostOf(t, res2, ub); h.Status != StatusIsReference {
		t.Errorf("직접 지정한 ub = %s", h.Status)
	}
	if h := cmpHostOf(t, res2, ua); h.Status != StatusDiff || h.Ref.Target.Hostname != ub {
		t.Errorf("ua = %s ref %v", h.Status, h.Ref)
	}
	if fmt.Sprint(callCounts(all...)) != fmt.Sprint(before) {
		t.Errorf("재비교가 대상에 다시 접속함: %v → %v", before, callCounts(all...))
	}
	for _, s := range all {
		assertOneSession(t, s)
		assertClean(t, s)
	}
}
