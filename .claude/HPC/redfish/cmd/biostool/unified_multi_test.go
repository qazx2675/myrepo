package main

// unified_multi_test.go — 대상 100대, 벤더·모델 혼합 통합 시험 (mockbmc.Farm, 리눅스 전용, -short 면 건너뜀).
// 수집 → 프로파일 줄 → 전체비교(diff.txt 모델별 기준 / -ref 비교서버) → groupLines(벤더 탭·관리망 그룹 병합)
// → CSV → 설정 계획까지 한 번에 돌려, 같은 모델끼리만 비교하고 다른 모델 값으로 설정 항목이 생기지 않는지 본다.
//
// 대상 i (0..99) 의 트리 = multiTrees[i%5]:
//
//	HPE DL360 Gen10 (iLO 5) — 그중 (i/5) 가 홀수인 대는 Manager Model 만 "iLO 5 B (mock)" 로 바꿈 (속성 목록이 같아 한 그룹으로 합쳐져야 함)
//	HPE DL360 Gen11 (iLO 6) — 대상 1 이 diff.txt 의 Gen11 기준 (대상 목록 안의 기준 → 스냅샷 재사용, REFERENCE)
//	Dell R660 / Lenovo SR650 V3 — 대상 밖의 기준 호스트
//	Cisco C220 M7 — diff.txt 에 기준 없음 → NO_REFERENCE
//
// (i/5)%4 == 0 인 대(모델마다 5대, Gen11 은 기준인 대상 1 을 빼고 4대)는 프로파일 밖 속성(터보) 값을 바꿔 DIFF 가 된다.

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"biostool/internal/mockbmc"
)

var multiTrees = []struct {
	tree, turbo, off string // 트리, 값을 바꿀 속성(프로파일에 없는 것), 바꿀 값
}{
	{"hpe-dl360gen10", "ProcTurbo", "Disabled"},
	{"hpe-dl360gen11", "ProcTurbo", "Disabled"},
	{"dell-r660", "ProcTurboMode", "Disabled"},
	{"lenovo-sr650v3", "Processors_TurboMode", "Disable"},
	{"cisco-c220m7", "IntelTurboBoostTech", "disabled"},
}

func TestUnifiedMultiVendor100(t *testing.T) {
	if testing.Short() {
		t.Skip("-short: 100대 혼합 통합 시험 생략")
	}
	const n = 100
	f, port := newFarmOrSkip(t, mockbmc.FarmOptions{})
	fastCheck(t)
	trees := map[string]*mockbmc.Tree{}
	for _, m := range multiTrees {
		trees[m.tree] = loadTreeT(t, m.tree)
	}
	addr := func(ip string) string { return ip + ":" + itoa(port) }
	ips := make([]string, n)
	diffAt := map[string]bool{} // 값이 바뀐 대상 hostname
	for i := 0; i < n; i++ {
		m := multiTrees[i%5]
		ips[i] = farmIP(i)
		var over map[string]interface{}
		if (i/5)%4 == 0 && i != 1 {
			over = map[string]interface{}{m.turbo: m.off}
			diffAt[addr(ips[i])] = true
		}
		must(t, f.AddHost(ips[i], trees[m.tree], mockbmc.HostOptions{User: testUser, Pass: testPass, AttrOverrides: over}))
		if i%5 == 0 && (i/5)%2 == 1 {
			must(t, f.Host(ips[i]).Modify("/redfish/v1/managers/1", func(mm map[string]interface{}) { mm["Model"] = "iLO 5 B (mock)" }))
		}
	}
	// 대상 밖 기준 호스트: Gen10 / Dell / Lenovo (Cisco 는 기준 없음)
	refIP := map[string]string{"hpe-dl360gen10": farmIP(n), "dell-r660": farmIP(n + 1), "lenovo-sr650v3": farmIP(n + 2)}
	for tree, ip := range refIP {
		must(t, f.AddHost(ip, trees[tree], mockbmc.HostOptions{User: testUser, Pass: testPass}))
	}
	refAddr := map[string]string{"hpe-dl360gen11": addr(ips[1])} // 트리 → 기준 주소
	for tree, ip := range refIP {
		refAddr[tree] = addr(ip)
	}

	rc := farmRC(t, ips, port, 20, 3)
	rc.Conf.AllcheckSet = true
	hostsPath := filepath.Join(t.TempDir(), "hosts")
	t0 := time.Now()
	u, err := runUnified(rc, unifiedOpts{Profile: testProf(t), ResultDir: t.TempDir(), HostsPath: hostsPath, Now: checkNow})
	if err != nil || u == nil {
		t.Fatalf("runUnified: %v", err)
	}
	tCollect := time.Since(t0)
	treeOf := map[string]string{} // hostname → 트리
	for i, h := range u.Check.Hosts {
		if h.Status != "" {
			t.Fatalf("대상 %d 조회 실패: %s %s", i, h.Status, h.Detail)
		}
		treeOf[h.Target.Hostname] = multiTrees[i%5].tree
	}

	// ---- 프로파일 줄 → 벤더 탭 ----
	plines := u.ProfileLines()
	pv := groupLines(plines)
	var vendors []string
	for _, vg := range pv {
		vendors = append(vendors, vg.Vendor)
	}
	wantV := []string{u.Snaps[0].Vendor, u.Snaps[2].Vendor, u.Snaps[3].Vendor, u.Snaps[4].Vendor} // HPE, Dell, Lenovo, Cisco (처음 나온 순서)
	if strings.Join(vendors, ",") != strings.Join(wantV, ",") {
		t.Errorf("프로파일 벤더 탭 = %v, 기대 %v", vendors, wantV)
	}

	// ---- 전체비교: diff.txt 모델별 기준 ----
	diffTxt := filepath.Join(t.TempDir(), "diff.txt")
	must(t, os.WriteFile(diffTxt, []byte(strings.Join([]string{
		refAddr["hpe-dl360gen10"], refAddr["hpe-dl360gen11"] + " ProLiant DL360 Gen11", refAddr["dell-r660"], refAddr["lenovo-sr650v3"],
	}, "\n")+"\n"), 0o600))
	t1 := time.Now()
	cmp := compareT(t, u, compareRef{DiffFile: diffTxt})
	tCmp := time.Since(t1)
	snapOf := map[string]*biosSnap{}
	for _, s := range u.Snaps {
		snapOf[s.Target.Hostname] = s
	}
	st := map[string]int{}
	for _, h := range cmp.Hosts {
		name, tree := h.Target.Hostname, treeOf[h.Target.Hostname]
		st[h.Status]++
		switch {
		case tree == "cisco-c220m7":
			if h.Status != StatusNoReference {
				t.Errorf("%s (Cisco): %s, 기대 NO_REFERENCE", name, h.Status)
			}
		case name == refAddr["hpe-dl360gen11"]:
			if h.Status != StatusIsReference {
				t.Errorf("%s (Gen11 기준 자신): %s", name, h.Status)
			}
		default:
			want := StatusSame
			if diffAt[name] {
				want = StatusDiff
			}
			if h.Status != want || h.Ref == nil {
				t.Errorf("%s (%s): %s, 기대 %s", name, tree, h.Status, want)
				continue
			}
			// 같은 모델의 기준과만 비교 (모델 키·기준 주소)
			if h.Ref.Snap.Key != h.Snap.Key || h.Ref.Target.Hostname != refAddr[tree] {
				t.Errorf("%s (%s): 기준 %s (키 %s ≠ %s)", name, tree, h.Ref.Target.Hostname, h.Ref.Snap.Key, h.Snap.Key)
			}
			if want == StatusDiff && (h.NDiff != 1 || h.Diffs[0].Attr != multiTrees[indexTree(tree)].turbo) {
				t.Errorf("%s: 차이 %+v", name, h.Diffs)
			}
		}
	}
	if st[StatusDiff] != 19 || st[StatusSame] != 60 || st[StatusNoReference] != 20 || st[StatusIsReference] != 1 {
		t.Errorf("전체비교 상태 집계 %v", st)
	}

	// ---- groupLines: 벤더 탭 · 관리망 그룹 (속성 이름 목록이 같으면 합침) ----
	vgs := groupLines(cmp.Lines)
	if len(vgs) != 4 {
		t.Fatalf("벤더 탭 %d개, 기대 4", len(vgs))
	}
	gen10Groups := 0
	for _, vg := range vgs {
		for _, g := range vg.Groups {
			var key string
			trees := map[string]bool{}
			for _, h := range g.Hosts {
				trees[treeOf[h]] = true
				if s := snapOf[h]; s.Vendor != vg.Vendor {
					t.Errorf("탭 %s 에 다른 벤더 호스트 %s (%s)", vg.Vendor, h, s.Vendor)
				}
			}
			byHost := map[string][]string{}
			for _, l := range g.Lines {
				if isAttrLine(l) {
					byHost[l.Host] = append(byHost[l.Host], l.Attr)
				}
			}
			for h, as := range byHost { // 한 그룹 안에서는 속성 이름 목록이 모두 같다
				sort.Strings(as)
				k := strings.Join(as, ",")
				if key == "" {
					key = k
				} else if k != key {
					t.Errorf("그룹 %s/%s: %s 의 속성 목록이 다름", vg.Vendor, g.Name, h)
				}
			}
			if trees["hpe-dl360gen10"] {
				gen10Groups++
				if len(g.Mgmt) != 2 || !strings.Contains(g.Name, "+") || len(g.Hosts) != 20 {
					t.Errorf("Gen10 의 iLO 5 / iLO 5 B 는 한 그룹 (20대): 이름 %q 관리망 %v 호스트 %d", g.Name, g.Mgmt, len(g.Hosts))
				}
			}
			if len(trees) != 1 {
				t.Errorf("그룹 %s/%s 에 모델이 섞임: %v", vg.Vendor, g.Name, trees)
			}
		}
	}
	if gen10Groups != 1 {
		t.Errorf("Gen10 그룹 %d개, 기대 1", gen10Groups)
	}

	// ---- CSV ----
	paths, err := u.WriteOutputs("compare_", cmp.Lines)
	must(t, err)
	ng := 0
	for _, vg := range vgs {
		ng += len(vg.Groups)
	}
	if len(paths) != 2+2*ng {
		t.Errorf("CSV %d개, 기대 %d (summary·long + 그룹 %d × 2)", len(paths), 2+2*ng, ng)
	}
	for _, p := range paths {
		if fi, err := os.Stat(p); err != nil || fi.Size() == 0 {
			t.Errorf("CSV %s: %v", p, err)
		}
	}

	// ---- 설정 계획: 다른 모델 값으로 설정 항목이 생기지 않는다 ----
	checkPlan := func(label string, cmp *compareResult, wantHosts map[string]bool) {
		t.Helper()
		p := buildSetPlan(rc.Conf, u, cmp)
		got := map[string]bool{}
		for _, it := range p.Items {
			if it.Source != SetSrcCompare {
				continue
			}
			tree := treeOf[it.Host]
			got[it.Host] = true
			h := cmpHostOf(t, cmp, it.Host)
			if !h.compared() || h.Ref.Snap.Key != h.Snap.Key || h.Ref.Target.Hostname != refAddr[tree] {
				t.Errorf("%s: %s 항목이 다른 모델 기준에서 옴 (기준 %v)", label, it.Host, h.Ref)
				continue
			}
			if m := multiTrees[indexTree(tree)]; it.Attr != m.turbo || it.From != m.off || it.To != h.Ref.Snap.Attrs[m.turbo] {
				t.Errorf("%s: 항목 %+v", label, it)
			}
		}
		if len(got) != len(wantHosts) {
			t.Errorf("%s: 전체비교 항목 호스트 %d대, 기대 %d대", label, len(got), len(wantHosts))
		}
		for h := range wantHosts {
			if !got[h] {
				t.Errorf("%s: %s 항목 없음", label, h)
			}
		}
	}
	want := map[string]bool{}
	for h := range diffAt {
		if treeOf[h] != "cisco-c220m7" { // Cisco 는 기준 없음 (그리고 쓰기 대상 벤더도 아님)
			want[h] = true
		}
	}
	checkPlan("diff.txt", cmp, want)

	// ---- -ref 비교서버 1대 (Dell): Dell 만 비교, 나머지는 모델이 달라 NO_REFERENCE ----
	cmp2 := compareT(t, u, compareRef{Hosts: []string{refAddr["dell-r660"]}})
	for _, h := range cmp2.Hosts {
		dell := treeOf[h.Target.Hostname] == "dell-r660"
		if dell != h.compared() || (!dell && (h.Status != StatusNoReference || !strings.Contains(h.Detail, "모델이 다름"))) {
			t.Errorf("-ref Dell: %s (%s) %s %s", h.Target.Hostname, treeOf[h.Target.Hostname], h.Status, h.Detail)
		}
	}
	wantDell := map[string]bool{}
	for h := range want {
		if treeOf[h] == "dell-r660" {
			wantDell[h] = true
		}
	}
	checkPlan("-ref", cmp2, wantDell)

	// 대상은 수집 때 1번만 접속 (전체비교는 스냅샷 재사용), 대상 밖 Dell 기준은 비교 2번에 1번씩
	for i, ip := range ips {
		if ss := f.Host(ip).Sessions(); ss.Created != 1 {
			t.Errorf("대상 %d 세션 %+v, 기대 1", i, ss)
		}
	}
	if ss := f.Host(refIP["dell-r660"]).Sessions(); ss.Created != 2 {
		t.Errorf("Dell 기준 세션 %+v, 기대 2", ss)
	}
	if tot := f.Totals(); tot.Writes != 0 {
		t.Errorf("쓰기 %d, 기대 0", tot.Writes)
	}
	elapsed := time.Since(t0)
	t.Logf("100대: 수집 %v, 전체비교 %v, 전체 %v (줄 프로파일 %d / 전체비교 %d, 그룹 %d)", tCollect.Round(time.Millisecond),
		tCmp.Round(time.Millisecond), elapsed.Round(time.Millisecond), len(plines), len(cmp.Lines), ng)
	if elapsed > 30*time.Second {
		t.Errorf("100대가 %v 걸림 (30초 초과)", elapsed)
	}
}

func indexTree(tree string) int {
	for i, m := range multiTrees {
		if m.tree == tree {
			return i
		}
	}
	return -1
}
