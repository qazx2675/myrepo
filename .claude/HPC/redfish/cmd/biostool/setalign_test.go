package main

// setalign.go 시험 (대표서버 기준으로 맞추기). BMC 에 접속하지 않는 계획 단계라 비교 결과(allHost)를 compareAttrs 로 직접 만든다.

import (
	"fmt"
	"strings"
	"testing"
)

// alignSpec 은 시험용 호스트 1대입니다. ref 면 그 묶음의 대표서버입니다.
type alignSpec struct {
	name, tag, bios string
	vendor          string // 비면 HPE
	ref             bool
	attrs           map[string]string
	profile         []itemResult // 프로파일 점검 항목 (충돌 시험용)
}

// alignFixture 는 묶음마다 대표 1대(ref=true)와 나머지를 비교한 u·cmp 를 만듭니다 (모델 키 hpe|m1, 같은 태그끼리 한 묶음).
func alignFixture(specs ...alignSpec) (*unifiedRun, *compareResult) {
	u := &unifiedRun{Check: &checkRun{}}
	cmp := &compareResult{Ignore: withUnique(newIgnoreSet(defaultIgnore, "t")), TagMode: false}
	for _, s := range specs {
		if s.tag != "" {
			cmp.TagMode = true
		}
	}
	reps := map[string]*allRef{}
	var snaps []*biosSnap
	for _, s := range specs {
		vendor := s.vendor
		if vendor == "" {
			vendor = "HPE"
		}
		t := Target{Hostname: s.name, Tag: s.tag}
		sn := &biosSnap{Target: t, Vendor: vendor, Model: "M1", Key: strings.ToLower(vendor) + "|m1", BiosVersion: s.bios, Attrs: s.attrs}
		snaps = append(snaps, sn)
		hc := &hostCheck{Target: t, Items: s.profile, Info: &SystemInfo{Vendor: vendor, Model: "M1", SettingsPath: "/redfish/v1/Systems/1/Bios/Settings"}}
		u.Check.Hosts = append(u.Check.Hosts, hc)
		u.Snaps = append(u.Snaps, sn)
		if s.ref {
			reps[s.tag] = &allRef{Target: t, ID: targetID(t), Snap: sn}
		}
	}
	for i, s := range specs {
		sn := snaps[i]
		h := &allHost{Target: sn.Target, Snap: sn}
		if s.ref {
			h.Status = StatusIsReference
		} else if r := reps[s.tag]; r != nil {
			h.Ref = r
			if sn.BiosVersion != r.Snap.BiosVersion {
				h.Notes = append(h.Notes, hostNote{noteBiosVerDiff, "대상=" + sn.BiosVersion + " 기준=" + r.Snap.BiosVersion})
			}
			compareAttrs(h, r.Snap.Attrs, sn.Attrs, cmp.Ignore)
			h.Status = StatusSame
			if len(h.Diffs) > 0 {
				h.Status = StatusDiff
			}
		} else {
			h.Status = StatusNoReference
		}
		cmp.Hosts = append(cmp.Hosts, h)
	}
	return u, cmp
}

func alignRowIdx(t *testing.T, c *alignCands, attr string) int {
	t.Helper()
	for i, r := range c.Rows() {
		if !r.Group && !r.Disabled && strings.HasPrefix(r.Label, attr+" ") {
			return i
		}
	}
	t.Fatalf("체크 줄 %s 없음: %v", attr, c.Rows())
	return -1
}

func alignOffOf(c *alignCands, attr string) string {
	for _, r := range c.Rows() {
		if r.Disabled && r.Label == attr {
			return r.Reason
		}
	}
	return "(없음)"
}

// 고른 속성만 계획의 항목이 되고, 고르지 않은 속성은 버려진다 (수정제외로 세지 않음). 줄은 묶음(태그·모델)별 속성이고 한 줄이 그 묶음의 다른 서버 전부.
func TestAlignOnlySelectedAttrsPlanned(t *testing.T) {
	base := map[string]string{"LogicalProc": "Enabled", "ProcTurbo": "Enabled", "Same": "1"}
	mod := func(lp, tb string) map[string]string {
		return map[string]string{"LogicalProc": lp, "ProcTurbo": tb, "Same": "1"}
	}
	u, cmp := alignFixture(
		alignSpec{name: "g-ref", tag: "gold", bios: "1.0", ref: true, attrs: base},
		alignSpec{name: "g-a", tag: "gold", bios: "1.0", attrs: mod("Disabled", "Disabled")},
		alignSpec{name: "g-b", tag: "gold", bios: "1.0", attrs: mod("Auto", "Enabled")},
		alignSpec{name: "g-c", tag: "gold", bios: "1.0", attrs: mod("Disabled", "Enabled")},
		alignSpec{name: "s-ref", tag: "silver", bios: "1.0", ref: true, attrs: base},
		alignSpec{name: "s-a", tag: "silver", bios: "1.0", attrs: mod("Disabled", "Enabled")},
	)
	c := buildAlignCands(&Config{}, u, cmp)
	if c.Msg != "" || len(c.Bundles) != 2 {
		t.Fatalf("Msg=%q 묶음=%d", c.Msg, len(c.Bundles))
	}
	rows := c.Rows()
	if !rows[0].Group || !strings.Contains(rows[0].Label, "[gold] M1") || !strings.Contains(rows[0].Label, "대표서버 g-ref") {
		t.Errorf("머리 줄 = %q", rows[0].Label)
	}
	lp := rows[alignRowIdx(t, c, "LogicalProc")].Label
	if lp != "LogicalProc   Disabled(2) Auto(1) -> Enabled   (3대)" {
		t.Errorf("LogicalProc 줄 = %q", lp)
	}
	if tb := rows[alignRowIdx(t, c, "ProcTurbo")].Label; tb != "ProcTurbo   Disabled -> Enabled   (1대)" {
		t.Errorf("ProcTurbo 줄 = %q", tb)
	}
	for _, r := range rows {
		if r.Checked {
			t.Error("처음엔 모두 해제여야 함")
		}
	}

	p := alignPlan(c, []int{alignRowIdx(t, c, "LogicalProc")})
	want := []string{"g-a|LogicalProc|Disabled|Enabled|compare|false", "g-b|LogicalProc|Auto|Enabled|compare|false", "g-c|LogicalProc|Disabled|Enabled|compare|false"}
	if got := planKeys(p); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("항목 =\n%v\n기대\n%v", got, want)
	}
	if len(p.Excluded)+len(p.Protected) != 0 {
		t.Errorf("고르지 않은 줄이 수정제외로 남음: %v %v", p.Excluded, p.Protected)
	}
	if len(p.nCmp) == 0 {
		t.Error("비교서버 항목 안내(nCmp)가 꺼져 있음")
	}
	// 아무것도 안 고르면 빈 계획 (대표서버 자신은 항목이 되지 않음)
	if q := alignPlan(c, nil); !q.Empty() {
		t.Errorf("고른 것이 없는데 항목 %v", planKeys(q))
	}
	// 머리 줄·범위 밖 번호는 무시
	if q := alignPlan(c, []int{0, -1, 999}); !q.Empty() {
		t.Errorf("머리 줄·범위 밖: %v", planKeys(q))
	}
	// 두 묶음에 걸쳐 고르기: silver 의 LogicalProc 도 별도 줄
	var all []int
	for i, r := range rows {
		if !r.Group && !r.Disabled {
			all = append(all, i)
		}
	}
	if q := alignPlan(c, all); len(q.Items) != 3+1+1 {
		t.Errorf("전체 선택 항목 %d건: %v", len(q.Items), planKeys(q))
	}
	for _, it := range p.Items {
		if it.Host == "g-ref" || it.Host == "s-ref" {
			t.Errorf("대표서버 %s 가 대상이 됨", it.Host)
		}
	}
}

// 대표서버에만 있는 속성·대상에만 있는 속성은 그 속성만 제외하고 (사유 문구 고정), 같은 호스트의 다른 DIFF 는 후보로 남는다.
func TestAlignExcludesOnlyRefOnlyHost(t *testing.T) {
	u, cmp := alignFixture(
		alignSpec{name: "ref", bios: "1.0", ref: true, attrs: map[string]string{"A": "1", "OnlyOnRef": "x"}},
		alignSpec{name: "h", bios: "1.0", attrs: map[string]string{"A": "2", "OnlyOnHost": "y"}},
	)
	if h := cmp.Hosts[1]; h.NOnlyRef != 1 || h.NOnlyHost != 1 || h.sameAttrNames() {
		t.Fatalf("시험 준비: 속성 목록이 달라야 함 %+v", h)
	}
	c := buildAlignCands(&Config{}, u, cmp)
	if got := alignOffOf(c, "OnlyOnRef"); got != "대표서버에만 있음 — Redfish 로 속성을 추가할 수 없음" {
		t.Errorf("OnlyOnRef 사유 = %q", got)
	}
	if got := alignOffOf(c, "OnlyOnHost"); got != "대상에만 있음 — 맞출 기준값 없음" {
		t.Errorf("OnlyOnHost 사유 = %q", got)
	}
	rows := c.Rows()
	for i, r := range rows {
		if r.Disabled && c.rowCand[i] != nil {
			t.Errorf("제외 줄 %q 가 후보에 연결됨", r.Label)
		}
	}
	p := alignPlan(c, []int{alignRowIdx(t, c, "A")})
	if fmt.Sprint(planKeys(p)) != "[h|A|2|1|compare|false]" {
		t.Errorf("항목 = %v", planKeys(p))
	}
	if r := reasonOf(p.Excluded, "h", "OnlyOnRef"); r != alignReasonOnlyRef {
		t.Errorf("확인 화면 OnlyOnRef 사유 = %q", r)
	}
	if r := reasonOf(p.Excluded, "h", "OnlyOnHost"); r != alignReasonOnlyHost {
		t.Errorf("확인 화면 OnlyOnHost 사유 = %q", r)
	}
	if ex := strings.Join(p.ExcludedLines(), "\n"); !strings.Contains(ex, "h  OnlyOnRef  <없음> -> x (수정제외: 대표서버에만 있음") ||
		!strings.Contains(ex, "h  OnlyOnHost  y -> <없음> (수정제외: 대상에만 있음") {
		t.Errorf("수정제외 확인:\n%s", ex)
	}
}

// 속성 이름 목록이 달라도(sameAttrNames=false) 그 호스트의 나머지 DIFF 는 후보다 — 호스트 통째로 빠지지 않는다 (기존 점검 결과 기준 수정과 다름).
func TestAlignKeepsOtherDiffsWhenAttrNamesDiffer(t *testing.T) {
	u, cmp := alignFixture(
		alignSpec{name: "ref", bios: "1.0", ref: true, attrs: map[string]string{"A": "1", "B": "1", "OnlyRef": "r"}},
		alignSpec{name: "h", bios: "1.0", attrs: map[string]string{"A": "2", "B": "1"}},
	)
	if cmp.Hosts[1].sameAttrNames() {
		t.Fatal("시험 준비: 속성 이름 목록이 달라야 함")
	}
	// 기존 경로는 호스트를 통째로 제외한다 (allcheck_set=on 이어도) — 이 시험은 맞추기 경로만 바뀌었음을 같이 확인
	cfg := &Config{AllcheckSet: true}
	if old := buildSetPlan(cfg, u, cmp); !old.Empty() {
		t.Errorf("기존 경로는 속성 목록이 다르면 항목이 없어야 함: %v", planKeys(old))
	}
	c := buildAlignCands(cfg, u, cmp)
	p := alignPlan(c, []int{alignRowIdx(t, c, "A")})
	if len(p.Items) != 1 || p.Items[0].Attr != "A" || p.Items[0].Host != "h" || p.Items[0].To != "1" {
		t.Errorf("항목 = %v", planKeys(p))
	}
	if alignOffOf(c, "OnlyRef") != alignReasonOnlyRef {
		t.Errorf("OnlyRef 는 [-] 줄이어야 함: %v", c.Rows())
	}
}

// 보호 속성은 어떤 줄로도 고를 수 없고 번호를 억지로 넘겨도 항목이 되지 않는다 (bios.conf protect_attrs 포함). 사유와 함께 Protected 로 남는다.
func TestAlignProtectedNeverSelectable(t *testing.T) {
	u, cmp := alignFixture(
		alignSpec{name: "ref", bios: "1.0", ref: true, attrs: map[string]string{"BootMode": "Uefi", "MyCustom": "a", "ProcTurbo": "Enabled", "NicBoot1": "x"}},
		alignSpec{name: "h", bios: "1.0", attrs: map[string]string{"BootMode": "LegacyBios", "MyCustom": "b", "ProcTurbo": "Disabled", "NicBoot1": "y"}},
	)
	cfg := &Config{ProtectAttrs: []string{"MyCustom"}}
	c := buildAlignCands(cfg, u, cmp)
	rows := c.Rows()
	for _, a := range []string{"BootMode", "MyCustom", "NicBoot1"} {
		if r := alignOffOf(c, a); !strings.HasPrefix(r, "보호 속성") {
			t.Errorf("%s 사유 = %q, 기대 보호 속성", a, r)
		}
	}
	var every []int
	for i := range rows { // 머리·제외 줄 포함 전부를 골라도
		every = append(every, i)
	}
	p := alignPlan(c, every)
	if got := fmt.Sprint(planKeys(p)); got != "[h|ProcTurbo|Disabled|Enabled|compare|false]" {
		t.Errorf("항목 = %s (보호 속성이 섞임)", got)
	}
	for _, a := range []string{"BootMode", "MyCustom", "NicBoot1"} {
		if !hasItem(p.Protected, "h", a) {
			t.Errorf("%s 가 Protected 에 없음", a)
		}
	}
	if ex := strings.Join(p.ExcludedLines(), "\n"); !strings.Contains(ex, "h  BootMode  LegacyBios -> Uefi (수정제외: 보호 속성") {
		t.Errorf("수정제외 확인:\n%s", ex)
	}
}

// 프로파일이 정하는 속성: 목표값이 다르면 [충돌] 로 둘 다 설정하지 않고, 같으면 프로파일 항목에 맡긴다 (비교서버 값으로 따로 안 함).
func TestAlignProfileConflictExcluded(t *testing.T) {
	prof := []itemResult{
		{Std: "hyper_threading", Attr: "ProcHt", Expected: "Enabled", Result: StatusFail, Verified: true},
		{Std: "turbo", Attr: "ProcTurbo", Expected: "Disabled", Result: StatusFail, Verified: true},
	}
	u, cmp := alignFixture(
		alignSpec{name: "ref", bios: "1.0", ref: true, attrs: map[string]string{"ProcHt": "Disabled", "ProcTurbo": "Disabled", "Free": "1"}},
		alignSpec{name: "h", bios: "1.0", attrs: map[string]string{"ProcHt": "Enabled", "ProcTurbo": "Enabled", "Free": "2"}, profile: prof},
	)
	c := buildAlignCands(&Config{}, u, cmp)
	if r := alignOffOf(c, "ProcHt"); !strings.HasPrefix(r, "충돌: 프로파일 목표값 Enabled") || !strings.Contains(r, "둘 다 설정하지 않음") {
		t.Errorf("ProcHt (프로파일 Enabled, 대표 Disabled) 사유 = %q", r)
	}
	if r := alignOffOf(c, "ProcTurbo"); !strings.Contains(r, "프로파일 항목(turbo, FAIL)이 정하는 속성") {
		t.Errorf("ProcTurbo (프로파일과 대표 같음) 사유 = %q", r)
	}
	p := alignPlan(c, []int{alignRowIdx(t, c, "Free")})
	if fmt.Sprint(planKeys(p)) != "[h|Free|2|1|compare|false]" {
		t.Errorf("항목 = %v", planKeys(p))
	}
	if len(p.Conflicts) != 1 || p.Conflicts[0].Attr != "ProcHt" || p.Conflicts[0].ProfileTo != "Enabled" || p.Conflicts[0].CompareTo != "Disabled" {
		t.Errorf("충돌 = %v", p.Conflicts)
	}
	if !hasItem(p.Excluded, "h", "ProcHt") || !hasItem(p.Excluded, "h", "ProcTurbo") {
		t.Errorf("수정제외 = %v", p.Excluded)
	}
}

// 대표서버와 BIOS 버전이 다른 호스트도 경고를 붙여 고를 수 있다 (처음엔 해제). 확인 화면 참고에 경고가 남는다.
func TestAlignBiosVerDiffRule(t *testing.T) {
	u, cmp := alignFixture(
		alignSpec{name: "ref", bios: "2.0", ref: true, attrs: map[string]string{"A": "1"}},
		alignSpec{name: "same", bios: "2.0", attrs: map[string]string{"A": "2"}},
		alignSpec{name: "old", bios: "1.0", attrs: map[string]string{"A": "3"}},
	)
	c := buildAlignCands(&Config{}, u, cmp)
	rows := c.Rows()
	// 같은 버전 호스트와 BIOS 가 다른 호스트는 다른 줄 (같은 값으로 맞출 때도). 줄은 둘 다 처음에 해제.
	same, warn := -1, -1
	for i, r := range rows {
		if r.Group || r.Disabled || !strings.HasPrefix(r.Label, "A ") {
			continue
		}
		if strings.Contains(r.Label, "⚠ BIOS 버전 다름 1대") {
			warn = i
		} else {
			same = i
		}
		if r.Checked {
			t.Errorf("처음엔 해제여야 함: %+v", r)
		}
	}
	if same < 0 || warn < 0 || strings.Contains(rows[same].Label, "⚠") || !strings.Contains(rows[same].Label, "(1대)") {
		t.Fatalf("줄이 둘로 나뉘어야 함: %+v", rows)
	}
	p := alignPlan(c, []int{warn})
	if len(p.Items) != 1 || p.Items[0].Host != "old" || !strings.Contains(p.Items[0].Note, "⚠ BIOS 버전 다름") {
		t.Fatalf("항목 = %v", planKeys(p))
	}
	if len(p.Excluded) != 0 {
		t.Errorf("BIOS 버전이 달라도 수정제외가 아님: %v", p.Excluded)
	}
	if cl := strings.Join(p.ConfirmLines(), "\n"); !strings.Contains(cl, "old  ⚠ BIOS 버전 다름 (대상=1.0 기준=2.0)") {
		t.Errorf("확인 화면 참고에 BIOS 경고 없음:\n%s", cl)
	}
}

// 같은 버전 줄만 체크하면 BIOS 가 다른 호스트는 계획 항목이 되지 않는다 (같은 버전 호스트에만 적용).
func TestAlignBiosVerDiffSeparateRow(t *testing.T) {
	u, cmp := alignFixture(
		alignSpec{name: "ref", bios: "2.0", ref: true, attrs: map[string]string{"A": "1"}},
		alignSpec{name: "same", bios: "2.0", attrs: map[string]string{"A": "2"}},
		alignSpec{name: "old1", bios: "1.0", attrs: map[string]string{"A": "2"}},
		alignSpec{name: "old2", bios: "1.0", attrs: map[string]string{"A": "2"}},
	)
	c := buildAlignCands(&Config{}, u, cmp)
	same, warn := -1, -1
	for i, r := range c.Rows() {
		switch {
		case r.Group || r.Disabled:
		case strings.Contains(r.Label, "⚠ BIOS 버전 다름 2대"):
			warn = i
		case strings.HasPrefix(r.Label, "A "):
			same = i
		}
	}
	if same < 0 || warn < 0 {
		t.Fatalf("줄 = %+v", c.Rows())
	}
	p := alignPlan(c, []int{same})
	if len(p.Items) != 1 || p.Items[0].Host != "same" {
		t.Errorf("같은 버전 줄만 체크했는데 항목 = %v", planKeys(p))
	}
	if p = alignPlan(c, []int{same, warn}); len(p.Items) != 3 {
		t.Errorf("둘 다 체크하면 3대 = %v", planKeys(p))
	}
}

// 빈 경우: 대표 1대뿐 · 차이 없음 · 후보가 전부 제외 (제외 줄만 보이고 고른 것은 빈 계획).
func TestAlignEmptyAndAllExcluded(t *testing.T) {
	u, cmp := alignFixture(alignSpec{name: "only", tag: "gold", bios: "1.0", ref: true, attrs: map[string]string{"A": "1"}})
	c := buildAlignCands(&Config{}, u, cmp)
	if c.Msg != "비교할 서버가 없습니다(대표 1대뿐)" || c.Rows() != nil {
		t.Errorf("대표 1대뿐: Msg=%q", c.Msg)
	}

	u, cmp = alignFixture(
		alignSpec{name: "ref", bios: "1.0", ref: true, attrs: map[string]string{"A": "1"}},
		alignSpec{name: "h", bios: "1.0", attrs: map[string]string{"A": "1"}},
	)
	if c = buildAlignCands(&Config{}, u, cmp); c.Msg != "대표서버와 다른 항목이 없습니다" {
		t.Errorf("차이 없음: Msg=%q", c.Msg)
	}

	// 전부 제외: 대표에만 있는 속성 + 보호 속성 + Cisco 아닌 비쓰기 벤더
	u, cmp = alignFixture(
		alignSpec{name: "ref", bios: "1.0", ref: true, attrs: map[string]string{"OnlyRef": "1", "BootMode": "Uefi"}},
		alignSpec{name: "h", bios: "1.0", attrs: map[string]string{"BootMode": "Legacy"}},
	)
	c = buildAlignCands(&Config{}, u, cmp)
	rows := c.Rows()
	if c.Msg != "" || len(rows) != 3 || !rows[0].Group || !rows[1].Disabled || !rows[2].Disabled {
		t.Fatalf("전부 제외: Msg=%q rows=%+v", c.Msg, rows)
	}
	if p := alignPlan(c, []int{0, 1, 2}); !p.Empty() || len(p.Protected) != 1 || len(p.Excluded) != 1 {
		t.Errorf("전부 제외 계획: 항목 %v 보호 %v 제외 %v", planKeys(p), p.Protected, p.Excluded)
	}

	// 호스트 관문 (addItems 재사용): 쓰기 벤더가 아니면 후보가 아니라 [-] 줄
	u, cmp = alignFixture(
		alignSpec{name: "ref", bios: "1.0", ref: true, vendor: "Supermicro", attrs: map[string]string{"A": "1"}},
		alignSpec{name: "h", bios: "1.0", vendor: "Supermicro", attrs: map[string]string{"A": "2"}},
	)
	c = buildAlignCands(&Config{}, u, cmp)
	if r := alignOffOf(c, "A"); !strings.HasPrefix(r, "쓰기 대상 벤더가 아님") || strings.Contains(r, "건 제외") {
		t.Errorf("비쓰기 벤더 사유 = %q", r)
	}
	if p := alignPlan(c, []int{0, 1, 2}); !p.Empty() {
		t.Errorf("비쓰기 벤더 항목 %v", planKeys(p))
	}
}

// 값이 MAC·UUID·IP·iqn 모양이면 속성 이름과 상관없이 제외 (비교는 이미 거르지만 계획 단계에서도 다시 막음).
func TestAlignUniqueValueExcluded(t *testing.T) {
	u, cmp := alignFixture(
		alignSpec{name: "ref", bios: "1.0", ref: true, attrs: map[string]string{"Free": "1"}},
		alignSpec{name: "h", bios: "1.0", attrs: map[string]string{"Free": "2"}},
	)
	h := cmp.Hosts[1] // compareAttrs 가 거른 뒤에도 들어온 값 모양 줄을 직접 더해 계획 단계의 방어를 시험
	for _, d := range []attrDiffRow{
		{"Setting1", "aa:bb:cc:dd:ee:01", "aa:bb:cc:dd:ee:02", attrDiff},
		{"Setting2", "10.1.2.3", "10.1.2.4", attrDiff},
		{"Setting3", "iqn.1998-01.com.vmware:esx1", "iqn.1998-01.com.vmware:esx2", attrDiff},
		{"Setting4", "123e4567-e89b-12d3-a456-426614174000", "123e4567-e89b-12d3-a456-426614174001", attrDiff},
		{"Setting5", "fe80::1", "fe80::2", attrDiff},
		{"Setting6", "1", "fe80::1", attrDiff}, // 한쪽 값만 고유값 모양이면 진짜 차이일 수 있어 제외하지 않음 (양쪽 모두일 때만 제외)
	} {
		h.Diffs = append(h.Diffs, d)
	}
	c := buildAlignCands(&Config{}, u, cmp)
	for _, a := range []string{"Setting1", "Setting2", "Setting3", "Setting4", "Setting5"} {
		if r := alignOffOf(c, a); r != "값이 서버 고유값 모양" {
			t.Errorf("%s 사유 = %q", a, r)
		}
	}
	if alignRowIdx(t, c, "Setting6") < 0 {
		t.Error("한쪽만 고유값 모양인 Setting6 은 고를 수 있어야 함")
	}
	every := []int{}
	for i := range c.Rows() {
		every = append(every, i)
	}
	if p := alignPlan(c, every); fmt.Sprint(planKeys(p)) != "[h|Free|2|1|compare|false h|Setting6|fe80::1|1|compare|false]" {
		t.Errorf("고유값이 항목이 됨: %v", planKeys(p))
	}
	// Enabled·숫자 같은 일반 값은 제외하지 않음
	if looksUnique("Enabled") || looksUnique("10") || looksUnique("1.5") {
		t.Error("일반 값을 고유값으로 봄")
	}
}
