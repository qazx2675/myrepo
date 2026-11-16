package main

// profile_add.go (프로파일에 태그·모델별 설정 행 추가) 시험. 파일은 모두 임시 폴더에 만들고 실제 profiles/ 는 건드리지 않는다.
// 핵심 로직은 BMC 없이(합성 스냅샷), 화면(p 키)은 mock BMC + 가짜 TUI 로 본다.

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"biostool/internal/mockbmc"
)

// paBase 는 시험용 프로파일입니다: [redfish] 구역(DELL * 행과 모델 행, HPE 모델 행은 verified=Y), 주석, 빈 줄, [xml] 구역.
const paBase = "vendor\tmodel\tstd_name\tattribute\tvalue\tverified\n" +
	"# 시험용 주석 하나\n" +
	"# [redfish]\n" +
	"DELL\tR660\thyper_threading\tLogicalProc\tEnabled\tN\n" +
	"DELL\t*\tllc_prefetch\tLlcPrefetch\tEnabled\tN\n" +
	"HPE\tDL360 Gen11\thyper_threading\tProcHyperthreading\tEnabled\tY\n" +
	"\n" +
	"# [xml]\n" +
	"# xml 구역 주석\n" +
	"CISCO\tUCS B200 M4\tsystem_profile\tbiosVfCPUPerformance.vpCPUPerformance\thpc\tN\n"

// paFlowBase 는 화면 시험용 프로파일입니다 (mock Dell R660·HPE DL360 Gen11 에 맞춤). Dell 의 virtualization 행은 값이 Disabled 라
// 같은 행을 Enabled 로 넣으면 충돌입니다.
const paFlowBase = "vendor\tmodel\tstd_name\tattribute\tvalue\tverified\n" +
	"# [redfish]\n" +
	"DELL\tR660\thyper_threading\tLogicalProc\tEnabled\tN\n" +
	"DELL\tR660\tvirtualization\tProcVirtualization\tDisabled\tY\n" +
	"HPE\tDL360 Gen11\thyper_threading\tProcHyperthreading\tEnabled\tN\n" +
	"\n" +
	"# [xml]\n" +
	"CISCO\tUCS B200 M4\tsystem_profile\tbiosVfCPUPerformance.vpCPUPerformance\thpc\tN\n"

var paNow = time.Date(2026, 11, 16, 10, 30, 0, 0, time.UTC)

func paWrite(t *testing.T, text string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "VM.tsv")
	must(t, os.WriteFile(p, []byte(text), 0o640))
	return p
}

func paLoad(t *testing.T, path string) *Profile {
	t.Helper()
	p, err := loadProfile(path, "VM")
	must(t, err)
	return p
}

func paRead(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	must(t, err)
	return string(b)
}

func paSnap(host, tag, vendor, model string, attrs map[string]string) *biosSnap {
	return &biosSnap{Target: Target{Hostname: host, Tag: tag}, Vendor: vendor, Model: model, Attrs: attrs, Key: strings.ToLower(vendor) + "|" + normalizeModel(model)}
}

func paModel(vendor, model string, idx int, attrs map[string]string) *profAddModel {
	return &profAddModel{Vendor: vendor, Model: model, Idx: []int{idx}, Hosts: []string{fmt.Sprintf("h%d", idx)}, attrs: []map[string]string{attrs}}
}

func paBakFiles(t *testing.T, path string) []string {
	t.Helper()
	m, err := filepath.Glob(path + ".bak.*")
	must(t, err)
	return m
}

func TestProfileAddByModel(t *testing.T) {
	path := paWrite(t, paBase)
	prof := paLoad(t, path)
	dell := paModel("Dell", "R660", 0, map[string]string{"ProcTurbo": "Enabled", "LogicalProc": "Enabled"})
	plan := buildProfileAdd(prof, []*profAddModel{dell}, []profAddSet{{Attr: "procturbo", Value: "Disabled"}, {Std: "turbo_mode", Attr: "ProcTurbo", Value: "Enabled|Disabled"}}, nil)
	if len(plan.Add) != 2 || len(plan.Skipped)+len(plan.Conflicts)+len(plan.Same)+len(plan.Rejected) != 0 {
		t.Fatalf("계획: %+v", plan)
	}
	r := plan.Add[0]
	// 속성 이름은 BMC 표기(ProcTurbo), std_name 은 속성 이름의 소문자·밑줄, vendor·model 은 BMC 가 보고한 값
	if r.Vendor != "Dell" || r.Model != "R660" || r.Attr != "ProcTurbo" || r.Std != "proc_turbo" || r.Value != "Disabled" || r.WildNote {
		t.Errorf("첫 행: %+v", r)
	}
	if r2 := plan.Add[1]; r2.Std != "turbo_mode" || r2.Value != "Enabled|Disabled" {
		t.Errorf("std_name 을 준 행: %+v", r2)
	}
	res, err := profAddWrite(path, plan, false, paNow)
	if err != nil {
		t.Fatalf("쓰기: %v", err)
	}
	if res.Added != 2 || res.Replaced != 0 {
		t.Errorf("결과: %+v", res)
	}
	np := paLoad(t, path)
	rows := np.rowsFor("DELL", "R660", "proc_turbo")
	if len(rows) != 1 || rows[0].Attr != "ProcTurbo" || rows[0].Verified || rows[0].Value != "Disabled" {
		t.Errorf("다시 읽은 행: %+v", rows)
	}
	// 모델을 이름으로만 주면(접속 안 함) 입력한 이름 그대로, 속성 이름도 입력 그대로
	m := profAddModelFromSpec("Dell Inc.", "PowerEdge R660", nil)
	p2 := buildProfileAdd(np, []*profAddModel{m}, []profAddSet{{Attr: "SomeAttr", Value: "X"}}, nil)
	if len(p2.Add) != 1 || p2.Add[0].Attr != "SomeAttr" || !p2.Add[0].Unchecked || p2.Add[0].Std != "some_attr" {
		t.Errorf("이름만 준 모델: %+v", p2)
	}
	// 읽은 스냅샷에 같은 모델이 있으면 그 서버의 표기·속성을 쓴다
	snaps := []*biosSnap{paSnap("a", "", "Dell", "PowerEdge R660", map[string]string{"SomeAttr": "1"})}
	m = profAddModelFromSpec("DELL", "R660", snaps)
	if m.Model != "PowerEdge R660" || len(m.attrs) != 1 {
		t.Errorf("스냅샷 모델: %+v", m)
	}
}

func TestProfileAddByTagOneRowPerModel(t *testing.T) {
	path := paWrite(t, paBase)
	prof := paLoad(t, path)
	attrs := func() map[string]string { return map[string]string{"LlcPrefetch": "Disabled"} }
	snaps := []*biosSnap{
		paSnap("h0", "gold", "Dell", "PowerEdge R660", attrs()),
		paSnap("h1", "gold", "Dell", "PowerEdge R660", attrs()),
		paSnap("h2", "gold", "HPE", "ProLiant DL360 Gen11", attrs()),
		paSnap("h3", "silver", "Dell", "PowerEdge R660", attrs()),
		{Target: Target{Hostname: "h4", Tag: "gold"}, Status: StatusUnreachable},
	}
	models, matched, unreadable := profAddModelsForTag(snaps, "gold")
	if len(models) != 2 || matched != 4 || unreadable != 1 {
		t.Fatalf("모델 %d종 matched=%d unreadable=%d", len(models), matched, unreadable)
	}
	if fmt.Sprint(models[0].Idx) != "[0 1]" || fmt.Sprint(models[0].All) != "[0 1 3]" || models[1].Model != "ProLiant DL360 Gen11" {
		t.Errorf("모델 묶음: %+v / %+v", models[0], models[1])
	}
	plan := buildProfileAdd(prof, models, []profAddSet{{Attr: "LlcPrefetch", Value: "Enabled"}}, nil)
	if len(plan.Add) != 2 { // 서버 3대가 아니라 모델 2종마다 1행
		t.Fatalf("행 수: %+v", plan)
	}
	dell, hpe := plan.Add[0], plan.Add[1]
	if dell.Std != "llc_prefetch" || hpe.Std != "llc_prefetch" || dell.Model != "PowerEdge R660" || hpe.Vendor != "HPE" {
		t.Errorf("행: %+v / %+v", dell, hpe)
	}
	// DELL 은 * 행(llc_prefetch)이 있어 이 행이 그것을 가린다 → 알림. HPE 는 * 행이 없다
	if !dell.WildNote || hpe.WildNote {
		t.Errorf("* 행 알림: dell=%v hpe=%v", dell.WildNote, hpe.WildNote)
	}
	if text := strings.Join(plan.Lines(), "\n"); !strings.Contains(text, "※ 벤더 공통(*) 행 대신 이 행이 쓰입니다") {
		t.Errorf("미리보기에 * 행 알림이 없음:\n%s", text)
	}
	// 재점검 대상은 태그와 상관없이 그 모델인 서버 전부 (프로파일 행이 모델 기준이라)
	if fmt.Sprint(plan.Affected(false)) != "[0 1 2 3]" {
		t.Errorf("재점검 대상 = %v", plan.Affected(false))
	}
	if _, err := profAddWrite(path, plan, false, paNow); err != nil {
		t.Fatal(err)
	}
	np := paLoad(t, path)
	if r := np.rowsFor("DELL", "R660", "llc_prefetch"); len(r) != 1 || r[0].Value != "Enabled" {
		t.Errorf("DELL R660 행: %+v", r)
	}
	if r := np.rowsFor("HPE", "DL360 Gen11", "llc_prefetch"); len(r) != 1 {
		t.Errorf("HPE 행: %+v", r)
	}
	// 그 태그 서버가 하나도 없으면 모델을 모른다
	if ms, matched, unread := profAddModelsForTag(snaps, "none"); len(ms) != 0 || matched != 0 || unread != 0 {
		t.Errorf("없는 태그: %v %d %d", ms, matched, unread)
	}
}

func TestProfileAddSkipsModelWithoutAttr(t *testing.T) {
	path := paWrite(t, paBase)
	prof := paLoad(t, path)
	dell := paModel("Dell", "PowerEdge R660", 0, map[string]string{"ProcTurbo": "Enabled"})
	hpe := paModel("HPE", "ProLiant DL360 Gen11", 1, map[string]string{"ProcHyperthreading": "Enabled"})
	plan := buildProfileAdd(prof, []*profAddModel{dell, hpe}, []profAddSet{{Attr: "ProcTurbo", Value: "Disabled"}}, nil)
	if len(plan.Add) != 1 || plan.Add[0].Vendor != "Dell" {
		t.Fatalf("행: %+v", plan.Add)
	}
	if len(plan.Skipped) != 1 || plan.Skipped[0].Vendor != "HPE" || plan.Skipped[0].Reason != "이 모델에 없는 속성" {
		t.Errorf("건너뜀: %+v", plan.Skipped)
	}
	if text := strings.Join(plan.Lines(), "\n"); !strings.Contains(text, "(건너뜀: 이 모델에 없는 속성)") {
		t.Errorf("미리보기:\n%s", text)
	}
	// 어느 모델에도 없으면 행이 하나도 없고 쓰지 않는다
	none := buildProfileAdd(prof, []*profAddModel{hpe}, []profAddSet{{Attr: "ProcTurbo", Value: "Disabled"}}, nil)
	if len(none.Add) != 0 {
		t.Fatalf("행이 없어야 함: %+v", none)
	}
	if _, err := profAddWrite(path, none, false, paNow); err == nil || paRead(t, path) != paBase {
		t.Errorf("쓸 행이 없으면 오류이고 파일은 그대로여야 함: %v", err)
	}
}

func TestProfileAddConflictSameValueSkip(t *testing.T) {
	path := paWrite(t, paBase)
	prof := paLoad(t, path)
	dell := paModel("Dell", "PowerEdge R660", 0, map[string]string{"LogicalProc": "Enabled"})
	plan := buildProfileAdd(prof, []*profAddModel{dell}, []profAddSet{{Attr: "logicalproc", Value: "Enabled"}}, nil)
	if len(plan.Add) != 0 || len(plan.Conflicts) != 0 || len(plan.Same) != 1 {
		t.Fatalf("같은 값은 이미 있음: %+v", plan)
	}
	if text := strings.Join(plan.Lines(), "\n"); !strings.Contains(text, "[이미 있음]") {
		t.Errorf("미리보기:\n%s", text)
	}
	if _, err := profAddWrite(path, plan, true, paNow); err == nil {
		t.Error("이미 있는 행뿐이면 쓸 행이 없어 오류여야 함")
	}
	if paRead(t, path) != paBase || len(paBakFiles(t, path)) != 0 {
		t.Error("파일이 바뀌었거나 백업이 생김")
	}
}

// 값이 다르면 충돌: 핵심 함수는 Conflicts 로 알리고 (덮어쓸지는 부르는 쪽), 명령은 -force 가 없으면 아무것도 쓰지 않고 오류로 끝난다.
func TestProfileAddConflictNeedsForce(t *testing.T) {
	prof := paLoad(t, paWrite(t, paBase))
	dell := paModel("Dell", "PowerEdge R660", 0, map[string]string{"LogicalProc": "Enabled"})
	plan := buildProfileAdd(prof, []*profAddModel{dell}, []profAddSet{{Attr: "LogicalProc", Value: "Disabled"}}, nil)
	if len(plan.Conflicts) != 1 || plan.Conflicts[0].Old != "Enabled" || plan.Conflicts[0].OldLine != 4 || len(plan.Add) != 0 {
		t.Fatalf("충돌: %+v", plan)
	}
	if text := strings.Join(plan.Lines(), "\n"); !strings.Contains(text, "기존 Enabled → 새 Disabled") {
		t.Errorf("미리보기:\n%s", text)
	}

	e := newRTEnv(t, 3, "")
	vm := filepath.Join(e.dir, "profiles", "VM.tsv")
	before := paRead(t, vm)
	args := []string{"-conf", e.conf, "-profile", "VM", "-model", "DELL/R660", "-set", "LogicalProc=Disabled", "-set", "ProcTurbo=Enabled"}
	var out strings.Builder
	err := cmdProfileAdd(args, &out)
	if err == nil || !strings.Contains(err.Error(), "-force") {
		t.Fatalf("충돌이면 -force 안내와 함께 실패해야 함: %v\n%s", err, out.String())
	}
	if paRead(t, vm) != before || len(paBakFiles(t, vm)) != 0 {
		t.Error("충돌로 중단했는데 파일이 바뀌었거나 백업이 생김 (충돌 아닌 행도 쓰면 안 됨)")
	}
	out.Reset()
	if err := cmdProfileAdd(append(args, "-force"), &out); err != nil {
		t.Fatalf("-force: %v\n%s", err, out.String())
	}
	found := false
	for _, r := range paLoad(t, vm).rowsFor("DELL", "R660", "hyper_threading") {
		if r.Attr == "LogicalProc" {
			found = true
			if r.Value != "Disabled" || r.Verified { // 값이 바뀌었으니 쓰기 허가(Y)도 N 으로
				t.Errorf("덮어쓴 행: %+v", r)
			}
		}
	}
	if !found || len(paBakFiles(t, vm)) != 1 || !strings.Contains(out.String(), "백업") {
		t.Errorf("덮어쓰기 결과 found=%v bak=%v\n%s", found, paBakFiles(t, vm), out.String())
	}
	if rows := paLoad(t, vm).rowsFor("DELL", "R660", "proc_turbo"); len(rows) != 1 {
		t.Errorf("충돌 아닌 행도 같이 들어가야 함: %+v", rows)
	}
}

// paOtherStd 는 같은 속성 행이 일반적인 이름이 아닌 std_name 으로 이미 있는 프로파일입니다 (R660 행과 * 행).
const paOtherStd = "vendor\tmodel\tstd_name\tattribute\tvalue\tverified\n" +
	"# [redfish]\n" +
	"DELL\tR660\tmy_ht\tLogicalProc\tEnabled\tN\n" +
	"DELL\t*\twild_pf\tLlcPrefetch\tEnabled\tN\n"

// std_name 을 적지 않으면 같은 벤더·모델(없으면 *)·속성의 기존 행 std_name 을 그대로 쓴다 (같은 속성 행이 둘이 되지 않게).
func TestProfileAddReusesExistingStd(t *testing.T) {
	path := paWrite(t, paOtherStd)
	prof := paLoad(t, path)
	dell := paModel("Dell", "PowerEdge R660", 0, map[string]string{"LogicalProc": "Enabled", "LlcPrefetch": "Disabled"})
	// 같은 값 → 기존 std_name(my_ht) 의 행과 같아 "이미 있음"
	plan := buildProfileAdd(prof, []*profAddModel{dell}, []profAddSet{{Attr: "LogicalProc", Value: "Enabled"}}, nil)
	if len(plan.Same) != 1 || plan.Same[0].Std != "my_ht" || len(plan.Add) != 0 || len(plan.Conflicts) != 0 {
		t.Fatalf("기존 std_name 재사용: %+v", plan)
	}
	// 다른 값 → 기존 행과의 충돌 (std_name 은 my_ht)
	plan = buildProfileAdd(prof, []*profAddModel{dell}, []profAddSet{{Attr: "LogicalProc", Value: "Disabled"}}, nil)
	if len(plan.Conflicts) != 1 || plan.Conflicts[0].Std != "my_ht" || plan.Conflicts[0].WantStd != "" || len(plan.Add) != 0 {
		t.Fatalf("기존 std_name 의 충돌: %+v", plan)
	}
	// 모델 행이 없고 * 행만 있으면 * 행의 std_name 으로 모델 행을 추가
	plan = buildProfileAdd(prof, []*profAddModel{dell}, []profAddSet{{Attr: "LlcPrefetch", Value: "Disabled"}}, nil)
	if len(plan.Add) != 1 || plan.Add[0].Std != "wild_pf" {
		t.Fatalf("* 행 std_name 재사용: %+v", plan)
	}
}

// 다른 std_name 을 명시하면 같은 속성 행을 하나 더 만들지 않고 충돌 (덮어쓸 때만 기존 행의 값을 바꿈).
func TestProfileAddSameAttrOtherStdIsConflict(t *testing.T) {
	path := paWrite(t, paOtherStd)
	prof := paLoad(t, path)
	dell := paModel("Dell", "R660", 0, map[string]string{"LogicalProc": "Enabled"})
	plan := buildProfileAdd(prof, []*profAddModel{dell}, []profAddSet{{Std: "other_std", Attr: "LogicalProc", Value: "Disabled"}}, nil)
	if len(plan.Add) != 0 || len(plan.Conflicts) != 1 || plan.Conflicts[0].Std != "my_ht" || plan.Conflicts[0].WantStd != "other_std" {
		t.Fatalf("다른 std_name 은 충돌: %+v", plan)
	}
	if text := strings.Join(plan.Lines(), "\n"); !strings.Contains(text, "other_std") || !strings.Contains(text, "my_ht") {
		t.Errorf("미리보기에 두 std_name 이 보여야 함:\n%s", text)
	}
	// 덮어쓰기 없이는 쓸 행이 없고, 덮어쓰면 행 수는 그대로 값만 바뀜
	if _, err := profAddWrite(path, plan, false, paNow); err == nil {
		t.Error("덮어쓰기 없이는 쓰지 않아야 함")
	}
	if _, err := profAddWrite(path, plan, true, paNow); err != nil {
		t.Fatal(err)
	}
	rows := paLoad(t, path).rowsFor("DELL", "R660", "my_ht")
	if len(rows) != 1 || rows[0].Value != "Disabled" || rows[0].Verified || len(paLoad(t, path).rowsFor("DELL", "R660", "other_std")) != 0 {
		t.Errorf("덮어쓴 결과: %+v", rows)
	}
}

func TestProfileAddKeepsCommentsAndXMLSection(t *testing.T) {
	path := paWrite(t, paBase)
	prof := paLoad(t, path)
	dell := paModel("Dell", "R660", 0, map[string]string{"ProcTurbo": "Enabled"})
	plan := buildProfileAdd(prof, []*profAddModel{dell}, []profAddSet{{Attr: "ProcTurbo", Value: "Disabled"}}, nil)
	if _, err := profAddWrite(path, plan, false, paNow); err != nil {
		t.Fatal(err)
	}
	got := paRead(t, path)
	newRow := "Dell\tR660\tproc_turbo\tProcTurbo\tDisabled\tN\n"
	if strings.Count(got, newRow) != 1 {
		t.Fatalf("새 행이 한 번 있어야 함:\n%s", got)
	}
	if strings.Replace(got, newRow, "", 1) != paBase { // 새 행만 빼면 원본과 글자 그대로 같다 (주석·구역·빈 줄·기존 행 그대로)
		t.Errorf("기존 내용이 바뀜:\n%s", got)
	}
	// 위치: [redfish] 구역의 마지막 행 바로 뒤, 빈 줄과 `# [xml]` 앞
	if i, j, k := strings.Index(got, "HPE\tDL360 Gen11\thyper_threading"), strings.Index(got, newRow), strings.Index(got, "# [xml]"); !(i < j && j < k) {
		t.Errorf("행 위치 %d %d %d", i, j, k)
	}
	if !strings.Contains(got, "\n"+newRow+"\n# [xml]") {
		t.Errorf("새 행은 마지막 행 바로 뒤, 빈 줄 앞이어야 함:\n%s", got)
	}
	// [xml] 구역이 없으면 파일 끝
	noXML := "vendor\tmodel\tstd_name\tattribute\tvalue\tverified\n# [redfish]\nDELL\tR660\thyper_threading\tLogicalProc\tEnabled\tN\n"
	p2 := paWrite(t, noXML)
	plan = buildProfileAdd(paLoad(t, p2), []*profAddModel{dell}, []profAddSet{{Attr: "ProcTurbo", Value: "Disabled"}}, nil)
	if _, err := profAddWrite(p2, plan, false, paNow); err != nil {
		t.Fatal(err)
	}
	if got := paRead(t, p2); got != noXML+newRow {
		t.Errorf("파일 끝에 붙어야 함:\n%q", got)
	}
}

func TestProfileAddAlwaysVerifiedN(t *testing.T) {
	path := paWrite(t, paBase)
	prof := paLoad(t, path)
	dell := paModel("Dell", "R660", 0, map[string]string{"A1": "x", "A2": "y"})
	hpe := paModel("HPE", "DL360 Gen11", 1, map[string]string{"ProcHyperthreading": "Enabled", "A1": "x"})
	plan := buildProfileAdd(prof, []*profAddModel{dell, hpe}, []profAddSet{{Attr: "A1", Value: "1"}, {Attr: "A2", Value: "2"}, {Attr: "ProcHyperthreading", Value: "Disabled"}}, nil)
	if len(plan.Conflicts) != 1 || len(plan.Add) != 3 {
		t.Fatalf("계획: %+v", plan)
	}
	if _, err := profAddWrite(path, plan, true, paNow); err != nil { // 충돌 덮어쓰기(HPE verified=Y 행)도 N 이 된다
		t.Fatal(err)
	}
	n := 0
	for _, l := range strings.Split(paRead(t, path), "\n") {
		if strings.Contains(l, "\tA1\t") || strings.Contains(l, "\tA2\t") || strings.Contains(l, "\tProcHyperthreading\t") {
			n++
			if !strings.HasSuffix(l, "\tN") {
				t.Errorf("verified 가 N 이 아님: %q", l)
			}
		}
	}
	if n != 4 {
		t.Errorf("새·덮어쓴 행 %d줄, 기대 4", n)
	}
	for _, r := range paLoad(t, path).Rows {
		if r.Verified && (r.Attr == "A1" || r.Attr == "A2" || r.Attr == "ProcHyperthreading") {
			t.Errorf("Y 인 행: %+v", r)
		}
	}
}

func TestProfileAddResultParses(t *testing.T) {
	for _, tc := range []struct{ name, text string }{
		{"LF", paBase},
		{"CRLF", strings.ReplaceAll(paBase, "\n", "\r\n")},
		{"끝 줄바꿈 없음", strings.TrimSuffix(paBase, "\n")},
		{"BOM", "\ufeff" + paBase},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := paWrite(t, tc.text)
			prof := paLoad(t, path)
			n := len(prof.Rows)
			snaps := []*biosSnap{
				paSnap("h0", "gold", "Dell", "PowerEdge R660", map[string]string{"LlcPrefetch": "Enabled", "ProcTurbo": "Enabled"}),
				paSnap("h1", "gold", "HPE", "ProLiant DL360 Gen11", map[string]string{"LlcPrefetch": "Enabled", "ProcTurbo": "Enabled"}),
				paSnap("h2", "gold", "Supermicro", "X12DPi-NT6", map[string]string{"LlcPrefetch": "Enabled"}),
			}
			models, _, _ := profAddModelsForTag(snaps, "gold")
			plan := buildProfileAdd(prof, models, []profAddSet{{Attr: "LlcPrefetch", Value: "Enabled"}, {Attr: "ProcTurbo", Value: "Disabled|Enabled"}}, nil)
			if len(plan.Add) != 5 || len(plan.Skipped) != 1 {
				t.Fatalf("계획: add=%d skip=%d", len(plan.Add), len(plan.Skipped))
			}
			if _, err := profAddWrite(path, plan, false, paNow); err != nil {
				t.Fatal(err)
			}
			np := paLoad(t, path) // 로더가 그대로 읽는다 (행 수 + 5)
			if len(np.Rows) != n+5 {
				t.Errorf("행 수 %d, 기대 %d", len(np.Rows), n+5)
			}
			got := paRead(t, path)
			if strings.HasPrefix(tc.text, "\ufeff") != strings.HasPrefix(got, "\ufeff") {
				t.Error("BOM 유지 안 됨")
			}
			if strings.Contains(tc.text, "\r\n") && strings.Contains(strings.ReplaceAll(got, "\r\n", ""), "\n") {
				t.Errorf("CRLF 파일에 LF 줄이 섞임: %q", got)
			}
			if len(np.XML.Rows) != 1 {
				t.Errorf("[xml] 구역이 바뀜: %d", len(np.XML.Rows))
			}
		})
	}
	// 같은 속성을 두 번 입력하면 뒤의 것은 거절 (로더의 중복 행 오류가 나지 않게)
	path := paWrite(t, paBase)
	plan := buildProfileAdd(paLoad(t, path), []*profAddModel{paModel("Dell", "R660", 0, map[string]string{"X": "1"})}, []profAddSet{{Attr: "x", Value: "1"}, {Attr: "X", Value: "2"}}, nil)
	if len(plan.Add) != 1 || len(plan.Rejected) != 1 {
		t.Errorf("같은 속성을 두 번 입력: %+v", plan)
	}
}

func TestProfileAddRejectsProtectedAttr(t *testing.T) {
	path := paWrite(t, paBase)
	prof := paLoad(t, path)
	dell := paModel("Dell", "R660", 0, map[string]string{"BootMode": "Uefi", "ServiceTag": "ABC", "ProcTurbo": "Enabled"})
	sets := []profAddSet{{Attr: "bootmode", Value: "Bios"}, {Attr: "ServiceTag", Value: "X"}, {Attr: "ProcTurbo", Value: "Disabled"}}
	plan := buildProfileAdd(prof, []*profAddModel{dell}, sets, nil)
	if len(plan.Rejected) != 2 || len(plan.Add) != 1 || plan.Add[0].Attr != "ProcTurbo" {
		t.Fatalf("보호 속성은 행이 되면 안 됨: %+v", plan)
	}
	if !strings.Contains(plan.Rejected[0], "보호 속성") || !strings.Contains(strings.Join(plan.Lines(), "\n"), "[입력 거절]") {
		t.Errorf("사유: %v", plan.Rejected)
	}
	// bios.conf protect_attrs 로 더한 패턴도 막는다
	plan = buildProfileAdd(prof, []*profAddModel{dell}, sets[2:], protectFor(&Config{ProtectAttrs: []string{"ProcTurbo"}}))
	if len(plan.Add) != 0 || len(plan.Rejected) != 1 {
		t.Errorf("conf 보호 패턴: %+v", plan)
	}
	// std_name 이 올바르지 않거나 값이 잘못되어도 거절
	plan = buildProfileAdd(prof, []*profAddModel{dell}, []profAddSet{{Std: "Bad Name", Attr: "ProcTurbo", Value: "x"}, {Attr: "ProcTurbo", Value: "a||b"}, {Attr: "ProcTurbo", Value: "a\tb"}}, nil)
	if len(plan.Add) != 0 || len(plan.Rejected) != 3 {
		t.Errorf("형식 오류: %+v", plan)
	}
	// 명령은 거절이 하나라도 있으면 아무것도 쓰지 않는다
	e := newRTEnv(t, 3, "")
	vm := filepath.Join(e.dir, "profiles", "VM.tsv")
	before := paRead(t, vm)
	err := cmdProfileAdd([]string{"-conf", e.conf, "-profile", "VM", "-model", "DELL/R660", "-set", "BootMode=Uefi", "-set", "ProcTurbo=Enabled"}, &strings.Builder{})
	if err == nil || paRead(t, vm) != before {
		t.Errorf("보호 속성 입력은 중단해야 함: %v", err)
	}
}

func TestProfileAddBackupCreated(t *testing.T) {
	path := paWrite(t, paBase)
	prof := paLoad(t, path)
	dell := paModel("Dell", "R660", 0, map[string]string{"A1": "x", "A2": "y"})
	plan := buildProfileAdd(prof, []*profAddModel{dell}, []profAddSet{{Attr: "A1", Value: "1"}}, nil)
	res, err := profAddWrite(path, plan, false, paNow)
	if err != nil {
		t.Fatal(err)
	}
	if want := path + ".bak.20261116-103000"; res.Backup != want {
		t.Errorf("백업 이름 = %s, 기대 %s", res.Backup, want)
	}
	if b := paRead(t, res.Backup); b != paBase {
		t.Errorf("백업 내용이 원본과 다름:\n%s", b)
	}
	if runtime.GOOS != "windows" {
		for _, p := range []string{path, res.Backup} {
			if st, err := os.Stat(p); err != nil || st.Mode().Perm() != 0o640 {
				t.Errorf("%s 권한 = %v (원본 0640 유지)", p, st.Mode())
			}
		}
	}
	// 같은 초에 한 번 더 쓰면 백업 이름이 겹치지 않는다
	plan2 := buildProfileAdd(paLoad(t, path), []*profAddModel{dell}, []profAddSet{{Attr: "A2", Value: "2"}}, nil)
	res2, err := profAddWrite(path, plan2, false, paNow)
	if err != nil || res2.Backup == res.Backup || len(paBakFiles(t, path)) != 2 {
		t.Errorf("두 번째 백업: %v %v", res2, err)
	}
	// 백업 파일은 프로파일 목록에 나오지 않는다 (*.tsv 만)
	if names := listProfileNames(filepath.Dir(path)); fmt.Sprint(names) != "[VM]" {
		t.Errorf("프로파일 목록 = %v", names)
	}
	// 원본이 깨져 있으면(검증 실패) 쓰지도 백업하지도 않는다
	broken := paWrite(t, "vendor\tmodel\tstd_name\tattribute\tvalue\tverified\nDELL\tR660\tx\n")
	if _, err := profAddWrite(broken, plan, false, paNow); err == nil || len(paBakFiles(t, broken)) != 0 {
		t.Errorf("깨진 프로파일: %v", err)
	}
}

func TestProfileAddParseInput(t *testing.T) {
	sets, errs := parseProfAddLines([]string{"LogicalProc Enabled", "hyper_threading LogicalProc Enabled", "  # 주석", "ProcTurbo Max Performance", "SysProfile Perf Optimized", "onlyname", "hyperthreading enabled"})
	if len(errs) != 1 || !strings.Contains(errs[0], "6번째") {
		t.Errorf("오류: %v", errs)
	}
	want := []profAddSet{
		{Attr: "LogicalProc", Value: "Enabled"},
		{Std: "hyper_threading", Attr: "LogicalProc", Value: "Enabled"},
		{Attr: "ProcTurbo", Value: "Max Performance"},
		{Attr: "SysProfile", Value: "Perf Optimized"},
		{Attr: "hyperthreading", Value: "enabled"}, // 첫 칸이 소문자뿐이어도 칸이 둘뿐이면 속성·값
	}
	if fmt.Sprint(sets) != fmt.Sprint(want) {
		t.Errorf("sets = %+v", sets)
	}
	s, err := parseProfAddFlag("hyper_threading:LogicalProc=Enabled|Disabled")
	if err != nil || s != (profAddSet{Std: "hyper_threading", Attr: "LogicalProc", Value: "Enabled|Disabled"}) {
		t.Errorf("-set: %+v %v", s, err)
	}
	for _, bad := range []string{"novalue", "=x", "A=", "Bad Std:A=1"} {
		if _, err := parseProfAddFlag(bad); err == nil {
			t.Errorf("%q 는 오류여야 함", bad)
		}
	}
	for attr, want := range map[string]string{"LogicalProc": "hyper_threading", "LlcPrefetch": "llc_prefetch", "SubNumaClustering": "sub_numa_cluster", "ProcTurbo": "proc_turbo", "": "attr"} {
		if got := profAddStdFor(attr); got != want {
			t.Errorf("std(%q) = %s, 기대 %s", attr, got, want)
		}
	}
}

func TestProfileAddPrefillStandardItems(t *testing.T) {
	rep := paSnap("rep", "gold", "Dell", "R660", map[string]string{"LogicalProc": "Enabled", "LlcPrefetch": "Disabled", "ServerName": "x", "ProcTurbo": "On", "SysProfile": "PerfOptimized"})
	got := profAddPrefill(rep, nil)
	want := []string{"system_profile SysProfile PerfOptimized", "hyper_threading LogicalProc Enabled", "llc_prefetch LlcPrefetch Disabled"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("채움 줄 = %q, 기대 %q", got, want)
	}
}

// ---- 화면: [프로파일 점검] 에서 p ----

// profAddFlowRig: gold = Dell R660 + HPE DL360 Gen11, silver = HPE DL360 Gen11. 프로파일은 임시 폴더의 VM.tsv (paFlowBase).
func profAddFlowRig(t *testing.T) (srv []*mockbmc.Server, vm string, o *commonOpts) {
	t.Helper()
	a, ua := startAllSrv(t, "dell-r660", nil)
	b, ub := startAllSrv(t, "hpe-dl360gen11", nil)
	c, uc := startAllSrv(t, "hpe-dl360gen11", nil)
	e, o := flowEnv(t, "")
	vm = filepath.Join(e.dir, "profiles", "VM.tsv")
	must(t, os.WriteFile(vm, []byte(paFlowBase), 0o600))
	o.user, o.profile = e.writeFile("targets.txt", ua+" gold", ub+" gold", uc+" silver"), "VM"
	return []*mockbmc.Server{a, b, c}, vm, o
}

// 프로파일이 바뀌지 않았고 백업·재점검 접속도 없음을 확인합니다.
func profAddFlowDenied(t *testing.T, vm string, srv []*mockbmc.Server) {
	t.Helper()
	if got := paRead(t, vm); got != paFlowBase || len(paBakFiles(t, vm)) != 0 {
		t.Errorf("프로파일이 바뀌었거나 백업이 생김:\n%s", got)
	}
	for _, s := range srv {
		assertOneSession(t, s)
		assertClean(t, s)
	}
}

// p → 1 (지금 보는 태그 gold) → 설정 입력 → y → 쓰기 → 그 모델인 서버 전부 재점검.
func TestFlowProfileAddByTag(t *testing.T) {
	srv, vm, o := profAddFlowRig(t)
	swapFlowPlan(t, func(*Config, *unifiedRun, *compareResult) *setPlan { return &setPlan{} }, nil)
	script := []flowStep{
		{"profile-result", "p"},
		{"profadd-scope", "1"},
		{"profadd-input", "ProcTurbo Disabled\r\r"}, // 채워진 줄(대표서버 현재값) 뒤에 직접 친 줄
		{"profadd-ask", "y"},
		{"profile-result", "qq"},
	}
	var pre []string
	var from, preview string
	fr, err := runFlowTC(t, biosTUIArgs{O: o, UserSet: true}, "", script, nil, func(i int, f *biosFlow) {
		switch i {
		case 2:
			ms, _, _ := profAddModelsForTag(f.u.Snaps, "gold")
			pre, from = f.profAddInitial(ms, "gold", protectFor(f.rc.Conf))
		case 3: // 미리보기 전체 (화면 높이 때문에 아래쪽은 그려지지 않을 수 있어 글 줄을 직접 본다)
			preview = strings.Join(f.ui.text, "\n")
		}
	})
	if err != nil {
		t.Fatalf("흐름 오류: %v", err)
	}
	if fmt.Sprint(fr.steps) != fmt.Sprint(flowNames(script)) {
		t.Errorf("단계 = %v", fr.steps)
	}
	// 입력 상자 처음 줄 = 서버가 가장 많은 모델(동수면 먼저 나온 Dell)의 대표서버 현재값
	if !strings.Contains(strings.Join(pre, "\n"), "hyper_threading LogicalProc Enabled") || !strings.Contains(from, "PowerEdge R660") {
		t.Errorf("대표서버 값 채움: %q from=%q", pre, from)
	}
	sc := fr.screen()
	for _, w := range []string{"지금 보는 태그 gold (모델 2종)", "지금 보는 그룹의 모델 (2종)", "태그·모델 직접 입력", "프로파일 추가 미리보기", "프로파일 VM 에 ", "행을 추가할까요? [y/n]", "p 프로파일 추가"} {
		if !strings.Contains(sc, w) {
			t.Errorf("화면에 %q 없음", w)
		}
	}
	for _, w := range []string{"[추가할 행]", "HPE ProLiant DL360 Gen11  proc_turbo  ProcTurbo = Disabled", "[이미 있음]", "(건너뜀: 이 모델에 없는 속성)", "대상: 태그 gold — 모델 2종"} {
		if !strings.Contains(preview, w) {
			t.Errorf("미리보기에 %q 없음:\n%s", w, preview)
		}
	}
	got := paRead(t, vm)
	if !strings.Contains(got, "HPE\tProLiant DL360 Gen11\tproc_turbo\tProcTurbo\tDisabled\tN\n") {
		t.Errorf("프로파일에 HPE 행이 없음:\n%s", got)
	}
	if strings.Contains(got, "\tproc_turbo\tProcTurbo\tDisabled\tN\n") && strings.Contains(got, "Dell\tPowerEdge R660\tproc_turbo") {
		t.Error("Dell 은 ProcTurbo 가 없어 행이 없어야 함")
	}
	added := 0
	for _, l := range strings.Split(got, "\n") {
		if l != "" && !strings.Contains(paFlowBase, l+"\n") {
			added++
			if !strings.HasSuffix(l, "\tN") {
				t.Errorf("새 행의 verified 가 N 이 아님: %q", l)
			}
		}
	}
	if added != 5 { // Dell: system_profile·llc_prefetch·sub_numa_cluster (hyper_threading 은 이미 있음), HPE: llc_prefetch·proc_turbo
		t.Errorf("추가된 행 %d줄, 기대 5:\n%s", added, got)
	}
	if bak := paBakFiles(t, vm); len(bak) != 1 || paRead(t, bak[0]) != paFlowBase {
		t.Errorf("백업 %v", bak)
	}
	if rows := fr.f.u.Check.prof.rowsFor("HPE", "DL360 Gen11", "proc_turbo"); len(rows) != 1 || rows[0].Verified {
		t.Errorf("점검에 쓰는 프로파일이 새로 읽혀야 함: %+v", rows)
	}
	// 행이 생긴 모델의 서버는 태그 밖의 silver 까지 전부 재점검 (점검 1 + 재점검 1), BMC 쓰기는 없다
	for i, s := range srv {
		if ss := s.Sessions(); ss.Created != 2 || ss.Deleted != 2 {
			t.Errorf("서버 %d 세션 = %+v, 기대 2", i, ss)
		}
		if len(patchBodies(s)) != 0 {
			t.Errorf("서버 %d 에 PATCH 가 감: %v", i, patchBodies(s))
		}
		assertClean(t, s)
	}
	if out := fr.out.String(); !strings.Contains(out, "프로파일 변경: 프로파일 VM 에 5행 추가") || !strings.Contains(out, "재점검 3대 완료") {
		t.Errorf("요약:\n%s", out)
	}
}

// 프로파일 행을 쓴 뒤 재점검은 설정을 보낸 것이 아니므로: 전체 설정 비교(f.cmp)를 비우지 않고, 재점검을 Ctrl+X 로 취소해도
// 수정(e)을 막는 f.unchecked 를 남기지 않는다.
func TestFlowProfileAddRecheckKeepsCompareAndEditable(t *testing.T) {
	_, ua := startAllSrv(t, "dell-r660", nil)
	slow := startMockTree(t, "hpe-dl360gen11", func(o *mockbmc.Options) {
		o.Fail = map[string]mockbmc.Fault{"POST /redfish/v1/SessionService/Sessions": {Delay: 400 * time.Millisecond}}
	})
	ub := strings.TrimPrefix(slow.URL(), "https://")
	_, uc := startAllSrv(t, "hpe-dl360gen11", nil)
	e, o := flowEnv(t, "")
	vm := filepath.Join(e.dir, "profiles", "VM.tsv")
	must(t, os.WriteFile(vm, []byte(paFlowBase), 0o600))
	o.user, o.profile = e.writeFile("targets.txt", ua+" gold", ub+" gold", uc+" silver"), "VM"
	swapFlowPlan(t, func(*Config, *unifiedRun, *compareResult) *setPlan { return &setPlan{} }, nil)
	script := []flowStep{
		{"profile-result", "p"},
		{"profadd-scope", "1"},
		{"profadd-input", "ProcTurbo Disabled\r\r"},
		{"profadd-ask", "y\x18"}, // 쓴 뒤 재점검 진행 화면에서 Ctrl+X (b 는 로그인 400ms 지연)
		{"profile-result", "qq"},
	}
	sentinel := &compareResult{}
	_, err := runFlowTC(t, biosTUIArgs{O: o, UserSet: true}, "", script, nil, func(i int, f *biosFlow) {
		switch i {
		case 0:
			f.cmp = sentinel
		case 4:
			if f.cmp != sentinel {
				t.Error("프로파일 추가 뒤 전체 설정 비교 결과(f.cmp)가 지워짐")
			}
			if len(f.unchecked) != 0 {
				t.Errorf("설정을 보내지 않았는데 unchecked = %v (e 가 막힘)", f.unchecked)
			}
		}
	})
	if err != nil {
		t.Fatalf("흐름 오류: %v", err)
	}
	if got := paRead(t, vm); !strings.Contains(got, "proc_turbo") {
		t.Errorf("프로파일에 행이 들어가야 함:\n%s", got)
	}
}

// 확인에서 n·Esc·Ctrl+X, 범위·입력에서 Esc·Ctrl+X·Tab → 프로파일·백업·재점검 모두 없음.
func TestFlowProfileAddDeclineWritesNothing(t *testing.T) {
	in := "ProcTurbo Disabled\r\r"
	for _, c := range []struct {
		name   string
		script []flowStep
	}{
		{"확인 n", []flowStep{{"profile-result", "p"}, {"profadd-scope", "1"}, {"profadd-input", in}, {"profadd-ask", "n"}, {"profile-result", "qq"}}},
		{"확인 Esc", []flowStep{{"profile-result", "p"}, {"profadd-scope", "1"}, {"profadd-input", in}, {"profadd-ask", "\x1b"}, {"profile-result", "qq"}}},
		{"확인 Ctrl+X", []flowStep{{"profile-result", "p"}, {"profadd-scope", "1"}, {"profadd-input", in}, {"profadd-ask", "\x18"}, {"profile-result", "qq"}}},
		{"범위 Esc", []flowStep{{"profile-result", "p"}, {"profadd-scope", "\x1b"}, {"profile-result", "qq"}}},
		{"범위 Ctrl+X", []flowStep{{"profile-result", "p"}, {"profadd-scope", "\x18"}, {"profile-result", "qq"}}},
		{"범위 Tab", []flowStep{{"profile-result", "p"}, {"profadd-scope", "\t"}, {"profile-result", "qq"}}},
		{"입력 Ctrl+X", []flowStep{{"profile-result", "p"}, {"profadd-scope", "1"}, {"profadd-input", "ProcTurbo Disabled\r\x18"}, {"profile-result", "qq"}}},
	} {
		t.Run(c.name, func(t *testing.T) {
			srv, vm, o := profAddFlowRig(t)
			swapFlowPlan(t, func(*Config, *unifiedRun, *compareResult) *setPlan { return &setPlan{} }, nil)
			fr, err := runFlowT(t, biosTUIArgs{O: o, UserSet: true}, "", c.script, nil)
			if err != nil {
				t.Fatalf("흐름 오류: %v", err)
			}
			if fmt.Sprint(fr.steps) != fmt.Sprint(flowNames(c.script)) {
				t.Errorf("단계 = %v", fr.steps)
			}
			profAddFlowDenied(t, vm, srv)
			if strings.Contains(fr.out.String(), "프로파일 변경") {
				t.Errorf("요약에 프로파일 변경이 있음:\n%s", fr.out.String())
			}
		})
	}
}

// 충돌(Dell virtualization Disabled → Enabled)은 덮어쓸지 따로 묻는다: y 면 덮어씀(verified N), n 이면 그 행만 건너뜀, Esc 면 전부 취소.
func TestFlowProfileAddConflictAsksOverwrite(t *testing.T) {
	const confl = "DELL\tR660\tvirtualization\tProcVirtualization\tDisabled\tY\n"
	for _, c := range []struct {
		name, key string
		written   bool // Add 행이 들어갔는가
		replaced  bool // 충돌 행을 덮어썼는가
	}{
		{"덮어쓰기 y", "y", true, true},
		{"덮어쓰기 n", "n", true, false},
		{"덮어쓰기 Esc", "\x1b", false, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			srv, vm, o := profAddFlowRig(t)
			swapFlowPlan(t, func(*Config, *unifiedRun, *compareResult) *setPlan { return &setPlan{} }, nil)
			script := []flowStep{
				{"profile-result", "p"},
				{"profadd-scope", "1"},
				{"profadd-input", "virtualization ProcVirtualization Enabled\r\r"},
				{"profadd-ask", "y"},
				{"profadd-overwrite", c.key},
				{"profile-result", "qq"},
			}
			var preview string
			fr, err := runFlowTC(t, biosTUIArgs{O: o, UserSet: true}, "", script, nil, func(i int, f *biosFlow) {
				if i == 4 {
					preview = strings.Join(f.ui.text, "\n")
				}
			})
			if err != nil {
				t.Fatalf("흐름 오류: %v", err)
			}
			if fmt.Sprint(fr.steps) != fmt.Sprint(flowNames(script)) {
				t.Errorf("단계 = %v", fr.steps)
			}
			if !strings.Contains(fr.screen(), "새 값으로 덮어쓸까요?") {
				t.Error("덮어쓰기 질문이 화면에 없음")
			}
			for _, w := range []string{"[충돌]", "기존 Disabled → 새 Enabled (기존 verified=Y 는 N 으로 바뀜)"} {
				if !strings.Contains(preview, w) {
					t.Errorf("미리보기에 %q 없음:\n%s", w, preview)
				}
			}
			got := paRead(t, vm)
			if !c.written {
				profAddFlowDenied(t, vm, srv)
				return
			}
			if hasOld := strings.Contains(got, confl); hasOld == c.replaced {
				t.Errorf("기존 충돌 행 유지=%v, 덮어쓰기=%v:\n%s", hasOld, c.replaced, got)
			}
			if c.replaced && !strings.Contains(got, "DELL\tR660\tvirtualization\tProcVirtualization\tEnabled\tN\n") {
				t.Errorf("덮어쓴 행(verified N)이 없음:\n%s", got)
			}
			if !strings.Contains(got, "HPE\tProLiant DL360 Gen11\tvirtualization\tProcVirtualization\tEnabled\tN\n") {
				t.Errorf("HPE 추가 행이 없음:\n%s", got)
			}
		})
	}
}

// 프로파일 폴더가 없거나 쓰고 있는 프로파일 파일이 없으면 한글 안내만 하고 아무것도 하지 않는다.
func TestFlowProfileAddNoProfileFile(t *testing.T) {
	srv, vm, o := profAddFlowRig(t)
	swapFlowPlan(t, func(*Config, *unifiedRun, *compareResult) *setPlan { return &setPlan{} }, nil)
	var msgs []string
	script := []flowStep{{"profile-result", "p"}, {"profile-result", "p"}, {"profile-result", "qq"}}
	fr, err := runFlowTC(t, biosTUIArgs{O: o, UserSet: true}, "", script, nil, func(i int, f *biosFlow) {
		switch i {
		case 0: // 첫 p 전에 프로파일 파일이 사라짐
			must(t, os.Remove(vm))
		case 1: // 첫 p 의 안내를 보고, 두 번째 p 전에 프로파일 폴더째 사라짐
			msgs = append(msgs, f.ui.msg)
			must(t, os.RemoveAll(filepath.Dir(vm)))
		case 2:
			msgs = append(msgs, f.ui.msg)
		}
	})
	if err != nil {
		t.Fatalf("흐름 오류: %v", err)
	}
	if len(msgs) != 2 || !strings.Contains(msgs[0], "지금 쓰는 프로파일을 읽을 수 없어") || !strings.Contains(msgs[1], "프로파일 폴더") {
		t.Errorf("안내 줄 = %q", msgs)
	}
	_ = fr
	for _, s := range srv {
		assertOneSession(t, s)
	}
}
