package main

// tui_profadd.go — [프로파일 점검] 화면의 p 키: 지금 쓰는 프로파일 파일에 태그·모델별 설정 행을 더합니다 (핵심 로직은 profile_add.go).
//
//	p → 범위 고르기 (1 지금 보는 태그 · 2 지금 보는 모델 · 3 태그·모델 직접 입력)
//	  → 설정 입력 (여러 줄, 처음엔 대표서버의 표준 항목 현재값이 채워져 있고 고쳐서 확정)
//	  → 미리보기 (추가할 행 / 이미 있음 / 충돌 / 건너뜀 + 사유)
//	  → "프로파일 <이름>에 N행을 추가할까요?" y/n → (충돌이 있으면) 덮어쓸지 y/n → 쓰기(백업·검증) → 프로파일 다시 읽기 → 영향 받는 서버만 재점검
//
// Esc·Ctrl+X·Tab 은 어느 단계에서든 아무것도 쓰지 않고 결과 보기로 돌아옵니다. BMC 에는 읽기(재점검)만 합니다.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const profAddNoWrite = "프로파일을 바꾸지 않았습니다"

// profAddKey 는 p 키입니다. 끝나면 결과 보기로 돌아오고 안내 줄에 결과를 남깁니다.
func (f *biosFlow) profAddKey() {
	msg := f.profAddFlow()
	f.ui.TakeTabOut() // Tab 으로 나왔어도 화면은 그대로 (쓴 것 없음)
	f.ui.SwitchTab(f.ui.Tab())
	if msg != "" {
		f.ui.SetMessage(msg)
	}
}

// profAddFlow 는 p 키 본체입니다. 돌려주는 글은 안내 줄에 보입니다 (프로파일을 바꿨는지 여부가 들어 있음).
func (f *biosFlow) profAddFlow() string {
	ui := f.ui
	if f.u == nil || f.u.Check == nil || f.rc == nil {
		return ""
	}
	dir := f.rc.Conf.ProfileDir
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		return fmt.Sprintf("프로파일 폴더 %s 을(를) 찾을 수 없어 프로파일에 추가할 수 없습니다 (bios.conf profile_dir)", filepath.ToSlash(dir))
	}
	if f.profName == "" {
		return "쓰고 있는 프로파일 파일이 없어 프로파일에 추가할 수 없습니다"
	}
	path := filepath.Join(dir, f.profName+".tsv")
	prof, err := loadProfile(path, f.profName)
	if err != nil {
		return "지금 쓰는 프로파일을 읽을 수 없어 추가할 수 없습니다: " + err.Error()
	}
	prot := protectFor(f.rc.Conf)

	models, label, tagName, msg := f.profAddScope()
	if msg != "" {
		return msg
	}

	// 설정 입력: 처음엔 대표서버의 표준 항목 현재값
	initial, from := f.profAddInitial(models, tagName, prot)
	help := []string{
		"한 줄에 하나:  <속성> <값>   또는   <std_name> <속성> <값>   (예: LogicalProc Enabled / hyper_threading LogicalProc Enabled)",
		"값은 A|B 로 허용값 여러 개 가능. 다 넣었으면 빈 줄에서 Enter (Esc·Ctrl+X: 취소)",
	}
	if from != "" {
		help = append(help, "채워진 줄은 대표서버 "+from+" 의 현재값입니다 — 고치거나 지워서 확정하십시오")
	}
	typed := initial
	var sets []profAddSet
	for sets == nil {
		f.wait("profadd-input")
		lines, ok := ui.InputLinesOpt("프로파일에 추가할 설정 — "+label, TUILinesOpt{TabText: true, Help: help, Initial: typed})
		if !ok {
			return profAddNoWrite
		}
		typed = lines
		s, errs := parseProfAddLines(lines)
		if len(errs) == 0 && len(s) == 0 {
			errs = []string{"입력한 설정이 없습니다"}
		}
		if len(errs) > 0 { // 잘못된 줄이 하나라도 있으면 아무것도 쓰지 않고 다시 입력받는다
			ui.SetText("프로파일 추가 — 입력 오류", errs)
			ui.SetMessage(fmt.Sprintf("입력 오류 %d건 — 고쳐서 다시 입력하십시오 (Ctrl+X 취소)", len(errs)))
			continue
		}
		sets = s
	}

	plan := buildProfileAdd(prof, models, sets, prot)
	lines := append([]string{"대상: " + label + fmt.Sprintf(" — 모델 %d종", len(models)), ""}, plan.Lines()...)
	ui.SetText("프로파일 추가 미리보기", lines)
	if len(plan.Add) == 0 && len(plan.Conflicts) == 0 {
		ui.SetMessage("추가할 행이 없습니다 — q 를 누르면 결과 보기로 돌아갑니다")
		f.wait("profadd-empty")
		ui.Browse()
		return "추가할 행이 없어 " + profAddNoWrite
	}
	if len(plan.Add) > 0 {
		f.wait("profadd-ask")
		if ui.Ask(fmt.Sprintf("프로파일 %s 에 %d행을 추가할까요?", f.profName, len(plan.Add))) != TUIYes {
			return profAddNoWrite
		}
	}
	overwrite := false
	if len(plan.Conflicts) > 0 {
		f.wait("profadd-overwrite")
		switch ui.Ask(fmt.Sprintf("같은 행이 다른 값으로 있는 %d행을 새 값으로 덮어쓸까요? (n 이면 그 행만 건너뜀)", len(plan.Conflicts))) {
		case TUIYes:
			overwrite = true
		case TUINo:
		default: // Esc·Ctrl+X: 전부 취소
			return profAddNoWrite
		}
	}
	if len(plan.Add) == 0 && !overwrite {
		return "덮어쓰지 않아 " + profAddNoWrite
	}

	res, err := profAddWrite(path, plan, overwrite, time.Now())
	if err != nil {
		f.warns = append(f.warns, "프로파일 추가 실패: "+err.Error())
		return "프로파일 추가 실패 (바꾸지 않음): " + err.Error()
	}
	note := fmt.Sprintf("프로파일 %s 에 %d행 추가", f.profName, res.Added)
	if res.Replaced > 0 {
		note += fmt.Sprintf(", %d행 덮어씀", res.Replaced)
	}
	note += " (백업 " + filepath.Base(res.Backup) + ")"

	np, err := loadProfile(path, f.profName)
	if err != nil {
		f.warns = append(f.warns, "쓴 뒤 프로파일을 다시 읽지 못했습니다: "+err.Error())
		f.profNote = append(f.profNote, note)
		return note + " — 쓴 뒤 다시 읽지 못해 재점검하지 않았습니다: " + err.Error()
	}
	f.u.Check.prof = np // 재점검·설정 계획이 새 프로파일을 씀
	if f.a.O.fromDump != "" {
		f.profNote = append(f.profNote, note+" (덤프 읽기라 재점검하지 않음)")
		return note + " — 덤프 읽기 모드라 재점검하지 않았습니다 (점검 화면은 이전 프로파일 기준)"
	}
	// 영향 받는 서버(행이 바뀐 모델의 서버)만 재점검해 프로파일 점검 화면에 합친다.
	oldNote := f.setNote
	f.setNote = ""
	f.recheckHostsFor(res.Idx, true) // 설정을 보낸 것이 아니라 프로파일만 바뀜: 비교 결과·수정 가능 상태를 건드리지 않음
	re := strings.TrimPrefix(f.setNote, " · ")
	f.setNote = oldNote
	if re != "" {
		note += " · " + re
	}
	f.profNote = append(f.profNote, note)
	return note
}

// profAddScope 는 범위(모델들)를 고릅니다. msg 가 비어 있지 않으면 거기서 끝 (취소 또는 안내).
func (f *biosFlow) profAddScope() (models []*profAddModel, label, tag, msg string) {
	ui := f.ui
	snaps := f.u.Snaps
	g := ui.group()
	var hosts []string
	if g != nil {
		hosts = g.Hosts
		if g.Vendor == tagGroupVendor {
			tag = g.Name
		}
	}
	tagModels, groupModels := []*profAddModel(nil), []*profAddModel(nil)
	item1, item2 := "지금 보는 태그 (지금 화면은 태그 그룹이 아닙니다)", "지금 보는 모델 (보이는 결과가 없습니다)"
	if tag != "" {
		tagModels, _, _ = profAddModelsForTag(snaps, tag)
		item1 = fmt.Sprintf("지금 보는 태그 %s (모델 %d종)", tag, len(tagModels))
	}
	if g != nil {
		groupModels, _, _ = profAddModelsForHosts(snaps, hosts)
		switch len(groupModels) {
		case 0:
			item2 = "지금 보는 모델 (읽은 서버가 없습니다)"
		case 1:
			item2 = "지금 보는 모델 " + groupModels[0].label()
		default:
			item2 = fmt.Sprintf("지금 보는 그룹의 모델 (%d종)", len(groupModels))
		}
	}
	f.wait("profadd-scope")
	i, ok := ui.Choose("프로파일에 설정을 추가할 범위", []string{item1, item2, "태그·모델 직접 입력"})
	if !ok {
		return nil, "", "", profAddNoWrite
	}
	switch i {
	case 0:
		if len(tagModels) == 0 {
			return nil, "", "", "지금 화면에서 고를 수 있는 태그가 없습니다 — 3 번으로 태그를 직접 입력하십시오"
		}
		return tagModels, "태그 " + tag, tag, ""
	case 1:
		if len(groupModels) == 0 {
			return nil, "", "", "지금 보는 그룹에 읽은 서버가 없어 모델을 알 수 없습니다"
		}
		return groupModels, "모델 " + joinModelLabels(groupModels), "", ""
	}
	f.wait("profadd-target")
	s, ok := ui.InputLineHelp("태그 또는 모델", "", []string{"태그 이름 (user.txt 둘째 칸) 또는 모델 \"벤더/모델\" (예: gold, DELL/R660)"})
	if !ok || s == "" {
		return nil, "", "", profAddNoWrite
	}
	if k := strings.Index(s, "/"); k > 0 && strings.TrimSpace(s[k+1:]) != "" {
		m := profAddModelFromSpec(s[:k], s[k+1:], snaps)
		return []*profAddModel{m}, "모델 " + m.label(), "", ""
	}
	ms, matched, unreadable := profAddModelsForTag(snaps, s)
	switch {
	case matched == 0:
		return nil, "", "", fmt.Sprintf("태그 %q 인 대상이 없습니다%s", s, profAddTagList(snaps))
	case len(ms) == 0:
		return nil, "", "", fmt.Sprintf("태그 %q 서버 %d대를 모두 읽지 못해 모델을 알 수 없습니다 — 프로파일을 바꾸지 않았습니다", s, matched)
	case unreadable > 0:
		f.warns = append(f.warns, fmt.Sprintf("프로파일 추가: 태그 %s 서버 %d대는 읽지 못해 그 모델은 빠졌을 수 있습니다", s, unreadable))
	}
	return ms, "태그 " + s, s, ""
}

func joinModelLabels(ms []*profAddModel) string {
	var parts []string
	for _, m := range ms {
		parts = append(parts, m.label())
	}
	return strings.Join(parts, ", ")
}

// profAddInitial 은 입력 상자의 처음 줄입니다: 범위에서 서버가 가장 많은 모델의 대표서버(전체비교와 같은 선정, 접속 없음)가 가진
// 표준 항목 현재값. from 은 그 대표서버 이름 (없으면 "").
func (f *biosFlow) profAddInitial(models []*profAddModel, tag string, prot *protectSet) (lines []string, from string) {
	var best *profAddModel
	for _, m := range models {
		if len(m.attrs) > 0 && (best == nil || len(m.Idx) > len(best.Idx)) {
			best = m
		}
	}
	if best == nil {
		return nil, ""
	}
	plan := f.u.autoPlanFor(f.autoPick)
	for _, b := range plan.Bundles {
		if tag != "" && b.Tag != tag {
			continue
		}
		for _, si := range b.Idx {
			if best.has(si) {
				rep := plan.RepSnap(b)
				if ls := profAddPrefill(rep, prot); len(ls) > 0 {
					return ls, fmt.Sprintf("%s (%s)", rep.Target.Hostname, best.label())
				}
				return nil, ""
			}
		}
	}
	return nil, ""
}
