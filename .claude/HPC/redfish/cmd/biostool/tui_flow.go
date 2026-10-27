package main

// tui_flow.go — `biostool bios` 대화형 흐름 (계획서_v0.2_TUI.md "흐름" 1~6, 단계 6). TUI 세션 1개 안에서 제자리 갱신만 한다.
//
//	대상 입력(파일 / 붙여넣기, IP 가 있으면 Cisco(XML) 대상도) → 프로파일 선택 → [2/4 접속·조회] → [3/4 CSV 저장] → 결과 화면 [프로파일 점검]
//	  Tab → [전체 설정 비교] 화면 (처음이면 기준 선택 → 비교서버 조회 → 결과), 다시 Tab 이면 [프로파일 점검] (화면마다 위치 유지)
//	  r (전체 설정 비교 화면) → 기준 다시 선택 → 다시 비교
//	  -ref/-ref-json/-diff 를 줬으면 수집 직후 바로 전체 설정 비교까지 (Tab 을 기다리지 않음)
//	  q 는 한 단계 뒤로 (관리망 그룹 줄 → 벤더 줄, 미리보기·설정 결과 → 결과 보기, 프로파일 고르기 → 대상 입력)
//	  벤더 줄에서 q 두 번 → 묻지 않고 끝. e → 어디서든 수정 단계 (수정 방법: 점검 결과 기준 수정 / BIOS 값 직접 입력)
//	→ (BIOS 값 직접 입력이면 "속성 이름 설정값" 붙여넣기 → 점검한 Redfish 호스트별 미리보기·확인 → 아래 Pending 질문부터 같음, setdirect.go)
//	→ 수정 확인 1: 차이 항목 전부를 "현재값 -> 변경값 (수정대상)" / "(수정제외: 사유)" 로 보이고
//	  "위 수정대상을 적용할까요?" (프로파일 + 전체비교(했으면) difference 를 함께)
//	→ 수정 확인 2: 수정제외 항목이 있을 때만 그것만 다시 보이고 "수정제외 항목을 확인했습니다. 진행할까요?"
//	→ Pending 장비가 있으면 포함 여부(y 포함 / n 제외 / Ctrl+X 취소) → 설정 적용 → 설정 결과
//	  → 설정을 보낸 호스트만 묻지 않고 재점검해 프로파일 점검 화면에 합침 (PENDING_OK 확인, 다시 e 로 수정 가능)
//	  (확인 1·2 에서 n/Esc/Ctrl+X 면 아무것도 쓰지 않고 결과 보기로 돌아옴)
//	→ TUI 를 닫고 표준출력에 짧은 요약 (결과 폴더·CSV·건수)
//
// 재부팅은 하지 않습니다. -from-dump 이면 BMC 에 접속하지 않으므로 수정 단계를 건너뜁니다.
// TUI 가 열려 있는 동안 표준출력·표준에러에 쓰지 않습니다 (진행은 TUIJob.Set, prepare 경고는 버퍼에 모았다가 닫은 뒤 찍음).

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// 설정 계획·적용 (setplan.go). 테스트에서 바꿔 끼운다.
var (
	flowBuildPlan = buildSetPlan
	flowApplyPlan = applySetPlan
)

// flowDiffFile 은 기준 메뉴의 "diff.txt (모델별 기준)" 이 가리키는 파일입니다 (allcheck -diff 기본값과 같음).
const flowDiffFile = "diff.txt"

// flowNoReboot 은 설정 결과 화면·요약 끝에 붙는 안내입니다.
const flowNoReboot = "재부팅 후 반영됩니다 — 재부팅은 하지 않았습니다"

// biosTUIArgs 는 cmdBios 가 대화형 흐름에 넘기는 옵션입니다.
type biosTUIArgs struct {
	O       *commonOpts
	UserSet bool       // -user 를 직접 줌 → 대상을 묻지 않고 그 파일을 쓴다
	Ref     compareRef // -ref / -ref-json / -diff (비었으면 기준 메뉴)
	Ignore  *ignoreSet // loadIgnore 결과
}

// biosFlow 는 대화형 흐름 1회의 상태입니다.
type biosFlow struct {
	ui *TUI
	a  biosTUIArgs
	// step 은 키를 기다리기 직전에 단계 이름으로 불립니다 (테스트가 키를 넣는 곳, nil 이면 안 부름).
	step func(name string)

	rc       *runContext
	profName string
	u        *unifiedRun
	cmp      *compareResult
	csvs     []string
	warns    []string
	setNote  string // 요약의 "설정:" 줄 (비면 찍지 않음)
	canceled bool   // 대상·프로파일 고르기에서 취소
	declined bool   // 수정 단계에서 취소·거절함 → 다음 종료(q q)는 다시 묻지 않고 끝 (새로 비교하면 다시 물음)
	applied  bool   // 설정 적용을 시도함 → 종료 때 요약의 "설정:" 줄을 settable() 로 덮어쓰지 않음
	// unchecked 는 설정을 보냈지만 재점검 결과를 아직 합치지 못한 호스트(점검 결과 순번)입니다. 있으면 결과가 적용 전 값이라 다시 설정하지 않음.
	unchecked map[int]bool
}

// runBiosTUI 는 열린 TUI 로 대화형 흐름을 돌리고, TUI 를 닫은 뒤 out 에 요약을 찍습니다.
func runBiosTUI(ui *TUI, a biosTUIArgs, out io.Writer) error {
	return (&biosFlow{ui: ui, a: a}).runAll(out, os.Stderr)
}

// runAll 은 흐름 → TUI 닫기 → (모아 둔 경고는 errOut) → 요약(out) 순서입니다.
// 흐름 중 패닉이어도 터미널과 prepare 경고 출력 곳은 되돌린다 (패닉은 그대로 위로 올라감).
func (f *biosFlow) runAll(out, errOut io.Writer) error {
	var warnBuf bytes.Buffer
	old := prepareWarnOut
	prepareWarnOut = &warnBuf
	restore := func() {
		f.ui.Close() // 여러 번 불러도 됨
		prepareWarnOut = old
	}
	defer restore()
	err := f.run()
	restore()
	io.Copy(errOut, &warnBuf)
	for _, w := range f.warns {
		fmt.Fprintln(errOut, "경고: "+w)
	}
	f.summary(out)
	// 1순위 계정이 없어 2순위(rf.conf)로 점검한 호스트가 있으면 TUI 를 닫은 뒤 계정 생성을 묻는다 (check 와 같음, 기본 Y).
	if err == nil && f.u != nil {
		if aerr := offerAccountCreate(f.rc, f.u.Check, false, false, false, os.Stdin, out, reportOpts{Color: useColor()}); aerr != nil {
			fmt.Fprintln(errOut, "경고: 계정 생성 단계 오류: "+aerr.Error())
		}
	}
	return err
}

func (f *biosFlow) wait(name string) {
	if f.step != nil {
		f.step(name)
	}
}

// run 은 흐름 본체입니다. Quit(시그널·입력 끝)이면 그 자리에서 끝낸다.
func (f *biosFlow) run() error {
	ui := f.ui
	ui.SetText("BIOS 점검", flowIntro)
	var prof *Profile
	for prof == nil {
		targets, ok := f.askTargets()
		if !ok {
			f.canceled = true
			return nil
		}
		rc, err := prepare(f.a.O, targets, "")
		if err != nil {
			return err
		}
		f.rc = rc
		p, ok, err := f.askProfile()
		if err != nil {
			return err
		}
		if !ok && (f.a.UserSet || ui.Quit()) { // 대상을 옵션으로 줬으면 돌아갈 단계가 없어 끝
			f.canceled = true
			return nil
		}
		prof = p // 프로파일 고르기에서 q/Esc 면 nil → 대상 입력으로 한 단계 뒤로
	}
	// 기준 파일은 접속 전에 확인한다 (오래 수집한 뒤에 경로 오류로 끝나지 않게).
	if f.a.Ref.JSONPath != "" {
		f.a.Ref.JSONPath = resolveRefJSONPath(f.a.Ref.JSONPath, f.rc.Conf.RefsDir)
		if _, err := readRefJSON(f.a.Ref.JSONPath); err != nil {
			return err
		}
	}
	if f.a.Ref.DiffFile != "" {
		if _, err := loadRefList(f.a.Ref.DiffFile); err != nil {
			return err
		}
	}

	if err := f.collect(prof); err != nil || f.u == nil {
		return err
	}
	plines := f.u.ProfileLines()
	ui.Progress("[3/4 CSV 저장]", func(j *TUIJob) error {
		f.writeCSV("profile_", plines, j.Canceled())
		return nil
	})
	ui.SetTabs(flowTabs)
	ui.SetResults("BIOS 점검 결과 — 프로파일 "+f.profName, groupLines(plines))
	// -ref/-ref-json/-diff 를 줬으면 기다리지 않고 바로 전체 설정 비교까지 한다 (두 화면 모두 준비, 첫 화면은 프로파일 점검).
	// 실패·취소면 안내만 남기고 다음 Tab 에 다시 시도한다. 설정은 하지 않는다 (수정 단계의 확인은 그대로).
	if f.a.Ref.count() > 0 && !ui.Quit() {
		ui.SwitchTab(1)
		f.wait("compare-auto")
		f.compareStep(false)
		ui.SwitchTab(0)
	}
	if n := f.countStatus(StatusCanceled); n > 0 {
		ui.SetMessage(fmt.Sprintf("취소했습니다 — 조회하지 않은 %d대는 %s (retry.txt)", n, StatusCanceled))
	}
	// 결과 보기 ↔ 수정 단계: e(어디서든)로만 수정 단계로 가고, 거기서 취소·거절하면 결과 보기로 돌아온다.
	// 맨 위에서 q 두 번은 수정 여부를 묻지 않고 끝낸다.
	for !ui.Quit() {
		act := f.browse()
		if ui.Quit() {
			break
		}
		if act == TUIEdit && len(f.unchecked) > 0 {
			ui.SetMessage(fmt.Sprintf("설정을 보낸 %d대를 재점검하지 못했습니다 — 결과가 적용 전 값이라 다시 점검한 뒤 설정하십시오", len(f.unchecked)))
			continue
		}
		if act != TUIEdit { // q 두 번은 묻지 않고 끝 (수정 단계는 e 로만). 요약의 "설정:" 줄만 남긴다
			if !f.applied && !f.declined {
				f.settable()
			}
			break
		}
		if f.editStep() {
			f.applied = true
		} else {
			f.declined = true
		}
		ui.SwitchTab(ui.Tab()) // 미리보기·설정 결과 글 보기 → 결과 보기
		if f.applied {
			ui.SetMessage("설정: " + f.setNote + " — q 두 번: 종료, e: 수정 단계")
		} else if f.setNote != "" {
			ui.SetMessage(f.setNote + " — 결과 보기로 돌아왔습니다 (q 두 번: 종료, e: 수정 단계)")
		}
	}
	return nil
}

// settable 은 설정할 항목이 있는지입니다 (덤프 읽기가 아니고 항목이 있으면 true). 아니면 요약의 "설정:" 줄을 남긴다.
// 아니면 요약의 "설정:" 줄을 남긴다.
func (f *biosFlow) settable() bool {
	if f.a.O.fromDump != "" {
		f.setNote = "덤프 읽기 모드라 설정하지 않았습니다"
		return false
	}
	if flowBuildPlan(f.rc.Conf, f.u, f.cmp).Empty() {
		f.setNote = "설정할 항목이 없습니다"
		return false
	}
	return true
}

// flowTabs 는 결과 화면 탭입니다: 0 프로파일 점검 결과, 1 전체 설정 비교 결과.
var flowTabs = []string{"프로파일 점검", "전체 설정 비교"}

// browse 는 결과 보기입니다. Tab 은 두 화면을 오가고(전체 설정 비교로 처음 넘어가면 기준을 고르고 비교),
// 전체 설정 비교 화면의 r 은 기준을 다시 고른다. 벤더 줄에서 q 두 번(TUIQuit) 또는 e(TUIEdit)면 돌아간다.
func (f *biosFlow) browse() TUIAction {
	ui := f.ui
	for {
		if ui.Tab() == 0 {
			f.wait("profile-result")
		} else {
			f.wait("compare-result")
		}
		switch act := ui.Browse(); act {
		case TUITab:
			if ui.Tab() == 1 {
				ui.SwitchTab(0)
				continue
			}
			ui.SwitchTab(1)
			if f.cmp == nil && !f.compareStep(false) { // 비교하지 못했으면(취소·실패 안내는 그대로) 프로파일 화면으로, 다음 Tab 에 다시 시도
				ui.TakeTabOut()
				ui.SwitchTab(0)
			}
		case TUIRef:
			if !f.compareStep(true) && ui.TakeTabOut() { // 기준을 다시 고르다 Tab 이면 프로파일 화면으로 (앞의 비교 결과는 그대로)
				ui.SwitchTab(0)
			}
		default:
			return act
		}
		if ui.Quit() {
			return TUIQuit
		}
	}
}

// flowIntro 는 시작 화면 글입니다 (대상을 물을 때는 Cisco(XML) 안내 줄을 덧붙임).
var flowIntro = []string{
	"대상과 프로파일을 고르면 호스트당 1회 접속해 BIOS 전체 속성을 조회합니다.",
	"설정은 마지막에 묻고, 재부팅은 하지 않습니다.",
}

// flowCiscoHint 는 대상 입력 화면의 Cisco(XML) 안내 2줄입니다 (좁은 화면에서 잘리지 않게 나눔).
// pfx 는 bios.conf xml_host_prefix (비면 이름도 자동으로 나누지 않음).
func flowCiscoHint(pfx []string) []string {
	if len(pfx) == 0 {
		return []string{"Cisco 장비는 XML 로 점검합니다 —", "  IP·이름 모두 Cisco(XML) 대상에 넣으십시오"}
	}
	return []string{"Cisco 장비는 XML 로 점검합니다 —",
		"  hostname 이 " + strings.Join(pfx, ", ") + " 로 시작하면 자동, IP 는 Cisco(XML) 대상에 따로 넣으십시오"}
}

// anyIPInput 은 입력 중 IP(포트 붙은 것 포함)가 하나라도 있는지 봅니다.
func anyIPInput(inputs []string) bool {
	for _, in := range inputs {
		if isIPInput(in) {
			return true
		}
	}
	return false
}

// askTargets 는 대상을 정합니다. 돌려준 문자열이 비면 대상 목록 파일(o.user)을 쓴다. ok=false 는 취소.
// 대상에 IP 가 있으면(xml_host_prefix 가 비면 늘) Cisco(XML) 대상을 따로 묻는다: 파일은 o.userXML, 붙여넣기는 o.userXMLArg
// (hostname 은 xml_host_prefix 로 자동으로 나뉘므로 묻지 않음).
func (f *biosFlow) askTargets() (string, bool) {
	if f.a.UserSet {
		return "", true
	}
	ui := f.ui
	o := f.a.O
	var pfx []string
	if conf, err := loadConf(o.conf); err == nil { // conf 오류는 cmdBios·prepare 가 알린다 (여기서는 안내 줄만)
		pfx = conf.XMLHostPrefix
	}
	ui.SetText("BIOS 점검", append(append(append([]string{}, flowIntro...), ""), flowCiscoHint(pfx)...))
	askXML := func(inputs []string) bool { return len(pfx) == 0 || anyIPInput(inputs) }
	xmlFileTitle, xmlPasteTitle := "Cisco(XML) 대상 파일 (IP 로 적은 Cisco 장비, 비우면 없음)",
		"Cisco(XML) 대상 붙여넣기 (IP 로 적은 Cisco 장비만, 없으면 바로 Ctrl+D)"
	if len(pfx) == 0 {
		xmlFileTitle, xmlPasteTitle = "Cisco(XML) 대상 파일 (Cisco 장비, 비우면 없음)",
			"Cisco(XML) 대상 붙여넣기 (Cisco 장비만, 없으면 바로 Ctrl+D)"
	}
	o.userXML, o.userXMLArg = "", "" // 대상 입력으로 돌아오면 앞에서 넣은 Cisco(XML) 대상은 버린다
	for {
		f.wait("targets")
		i, ok := ui.Choose("대상 입력", []string{"대상 파일 지정", "직접 입력 (붙여넣기: hostname / hostname-m / IP)"})
		if !ok {
			return "", false
		}
		if i == 0 {
			f.wait("targets-file")
			p, ok := ui.InputLine("대상 파일", f.a.O.user)
			if !ok {
				continue
			}
			if _, err := os.Stat(p); p == "" || err != nil {
				ui.SetMessage("대상 파일을 찾을 수 없습니다: " + p)
				continue
			}
			f.a.O.user = p
			if in, _ := loadList(p); askXML(in) { // 읽기 오류는 prepare 가 알린다
				x, ok := f.askXMLFile(xmlFileTitle)
				if !ok {
					if ui.Quit() {
						return "", false
					}
					continue
				}
				o.userXML = x
			}
			return "", true
		}
		f.wait("targets-paste")
		lines, ok := ui.InputLines("대상 붙여넣기 (한 줄에 하나, Ctrl+D 완료 · Ctrl+X 취소)")
		if !ok {
			if ui.Quit() {
				return "", false
			}
			continue
		}
		s := strings.Join(lines, "\n")
		in := splitTargets(s)
		if len(in) == 0 {
			ui.SetMessage("입력한 대상이 없습니다")
			continue
		}
		if askXML(in) {
			f.wait("targets-xml-paste")
			xl, ok := ui.InputLines(xmlPasteTitle)
			if !ok {
				if ui.Quit() {
					return "", false
				}
				continue
			}
			o.userXMLArg = strings.Join(xl, "\n")
		}
		return s, true
	}
}

// askXMLFile 은 Cisco(XML) 대상 파일을 묻습니다. 비우면 없음(""). ok=false 는 취소 (대상 입력 처음으로).
func (f *biosFlow) askXMLFile(title string) (string, bool) {
	for {
		f.wait("targets-xml-file")
		p, ok := f.ui.InputLine(title, "")
		if !ok {
			return "", false
		}
		if p == "" {
			return "", true
		}
		if _, err := os.Stat(p); err != nil {
			f.ui.SetMessage("Cisco(XML) 대상 파일을 찾을 수 없습니다: " + p)
			continue
		}
		return p, true
	}
}

// askProfile 은 -profile 이 없으면 profile_dir/*.tsv 에서 고르게 하고 프로파일을 읽습니다. ok=false 는 취소.
func (f *biosFlow) askProfile() (*Profile, bool, error) {
	dir := f.rc.Conf.ProfileDir
	name := f.a.O.profile
	if name == "" {
		names := listProfileNames(dir)
		if len(names) == 0 {
			return nil, false, fmt.Errorf("프로파일이 없습니다 (%s/*.tsv)", filepath.ToSlash(dir))
		}
		f.wait("profile")
		i, ok := f.ui.Choose("프로파일 선택", names)
		if !ok {
			return nil, false, nil
		}
		name = names[i]
	}
	prof, err := loadProfile(filepath.Join(dir, name+".tsv"), name)
	if err != nil {
		return nil, false, err
	}
	f.profName = name
	return prof, true, nil
}

// collect 는 [2/4 접속·조회] 입니다. Ctrl+X 면 아직 시작하지 않은 호스트는 접속하지 않는다 (CANCELED).
func (f *biosFlow) collect(prof *Profile) error {
	var err error
	f.ui.Progress("[2/4 접속·조회]", func(j *TUIJob) error {
		p := newSinkProgress(4, j.Set)
		p.Start()
		defer p.Stop()
		f.u, err = runUnified(f.rc, unifiedOpts{
			Profile: prof, FromDump: f.a.O.fromDump, ResultDir: f.rc.Conf.ResultDir, HostsPath: f.a.O.hosts, Ignore: f.a.Ignore,
			Collect: progStage{P: p, Idx: 2, Name: "접속·조회"}, Cancel: j.Canceled(), ProbeUser2: true,
		})
		return nil
	})
	if f.u == nil {
		return err
	}
	if err != nil {
		f.warns = append(f.warns, "결과 파일 기록 중 오류: "+err.Error())
	}
	return nil
}

// writeCSV 는 결과 폴더에 CSV 를 씁니다. cancel 이 닫히면 남은 파일은 쓰지 않고 경고로 남긴다.
func (f *biosFlow) writeCSV(prefix string, lines []ResultLine, cancel <-chan struct{}) {
	f.writeCSVIn(f.u.Check.Dir, prefix, lines, cancel)
}

// writeCSVIn 은 writeCSV 를 dir 폴더에 쓰는 것입니다 (재점검 결과 폴더용).
func (f *biosFlow) writeCSVIn(dir, prefix string, lines []ResultLine, cancel <-chan struct{}) {
	paths, err := writeResultCSVsCancel(dir, prefix, lines, cancel)
	for _, p := range paths { // 기준을 다시 골라 덮어쓴 파일은 한 번만 센다
		if !inList(f.csvs, p) {
			f.csvs = append(f.csvs, p)
		}
	}
	if err != nil {
		f.warns = append(f.warns, err.Error())
	}
}

// compareStep 은 기준 선택 → 비교 → 전체 설정 비교 화면(지금 탭)에 결과를 넣습니다. again 이면 r 키로 다시 고르는 것이라
// -ref/-ref-json/-diff 를 줬어도 메뉴를 보인다. 기준 선택 취소·실패면 false (실패는 안내 줄, 앞 결과는 그대로).
func (f *biosFlow) compareStep(again bool) bool {
	ui := f.ui
	ref, ok := f.chooseRef(again)
	if !ok {
		if !ui.tabOut { // Tab 으로 나온 것이면 안내 없이 다른 화면으로 (부르는 쪽이 TakeTabOut)
			ui.SetMessage("전체 설정 비교 기준을 고르지 않았습니다")
		}
		return false
	}
	var cmp *compareResult
	// Ctrl+X 면 아직 조회하지 않은 비교서버는 접속하지 않고, 그 기준이 필요한 대상은 REF_CANCELED(N/A, 설정 제외)로 남긴다.
	err := ui.Progress("비교서버 조회", func(j *TUIJob) error {
		p := newSinkProgress(1, j.Set)
		p.Start()
		defer p.Stop()
		var err error
		cmp, err = f.u.CompareCancel(ref, progStage{P: p, Idx: 1, Name: "기준 조회"}, j.Canceled())
		return err
	})
	if err != nil {
		f.warns = append(f.warns, "전체비교 실패: "+err.Error())
		ui.SetMessage("전체비교 실패: " + err.Error())
		return false
	}
	// CSV 는 따로 진행 표시를 둔다 (조회를 취소했어도 비교한 만큼은 저장하고, 저장 중 Ctrl+X 면 남은 파일만 건너뜀).
	// r 로 다시 비교하면 같은 이름의 compare_ CSV 를 새 결과로 덮어쓴다.
	ui.Progress("CSV 저장 (전체비교)", func(j *TUIJob) error {
		f.writeCSV("compare_", cmp.Lines, j.Canceled())
		return nil
	})
	f.cmp = cmp
	f.declined = false // 설정 계획이 바뀌었을 수 있어 종료 때 다시 묻는다
	ui.SetResults("전체비교 — 기준 "+cmp.Label, groupLines(cmp.Lines))
	ui.SetRefKey(true)
	if cmp.RefCanceled > 0 {
		ui.SetMessage(fmt.Sprintf("취소했습니다 — 비교서버 %d대 조회 안 함 (비교하지 못한 대상 %d대는 %s, 설정 제외)",
			cmp.RefCanceled, f.countCmp(StatusRefCanceled), StatusRefCanceled))
	}
	return true
}

// chooseRef 는 전체비교 기준입니다: -ref/-ref-json/-diff 를 줬으면 그것(again 이면 무시하고 메뉴), 아니면 메뉴
// (비교서버 hostname 입력 / refs_dir 의 JSON / diff.txt). hostname 을 비워 두면 diff.txt.
func (f *biosFlow) chooseRef(again bool) (compareRef, bool) {
	if f.a.Ref.count() > 0 && !again {
		return f.a.Ref, true
	}
	refs, err := listRefJSON(f.rc.Conf.RefsDir)
	if err != nil {
		f.ui.SetMessage(err.Error())
	}
	items := []string{"비교서버 hostname 입력"}
	var good []refFile
	for _, r := range refs {
		if r.Err != "" { // 기준으로 쓸 수 없는 파일은 고르지 못하게 뺀다
			continue
		}
		good = append(good, r)
		items = append(items, fmt.Sprintf("%s (%s %s %s)", r.Name, tuiDash(r.Vendor), tuiDash(r.Model), tuiDash(r.BiosVersion)))
	}
	items = append(items, flowDiffFile+" (모델별 기준)")
	f.wait("ref")
	i, ok := f.ui.Choose("전체비교 기준 선택", items)
	switch {
	case !ok:
		return compareRef{}, false
	case i == 0:
		f.wait("ref-host")
		hs, ok := f.ui.InputLines("비교서버 hostname (한 줄에 하나 또는 공백 구분, Ctrl+D 완료 · Ctrl+X 취소, 바로 Ctrl+D 면 " + flowDiffFile + ")")
		if !ok {
			return compareRef{}, false
		}
		if h := splitTargets(strings.Join(hs, "\n")); len(h) > 0 {
			return compareRef{Hosts: h}, true
		}
		return compareRef{DiffFile: flowDiffFile}, true
	case i <= len(good):
		return compareRef{JSONPath: good[i-1].Path}, true
	}
	return compareRef{DiffFile: flowDiffFile}, true
}

// flowEditMenu 는 수정 단계(e)의 수정 방법입니다: 0 점검 결과 기준 수정(setStep), 1 BIOS 값 직접 입력(directStep).
var flowEditMenu = []string{"점검 결과 기준 수정", "BIOS 값 직접 입력"}

// editStep 은 수정 단계 입구입니다: 수정 방법을 고른다. 덤프 읽기면 고르지 않고 false, 고르기 취소면 false (결과 보기로).
func (f *biosFlow) editStep() bool {
	if f.a.O.fromDump != "" {
		f.setNote = "덤프 읽기 모드라 설정하지 않았습니다"
		return false
	}
	f.wait("edit-menu")
	i, ok := f.ui.Choose("수정 방법 선택", flowEditMenu)
	if !ok {
		f.ui.TakeTabOut() // Tab 으로 접었어도 결과 보기로 (화면 탭은 그대로)
		return false
	}
	if i == 1 {
		return f.directStep()
	}
	return f.setStep()
}

// setStep 은 마지막 수정 단계입니다: 프로파일 + 전체비교 difference 를 한 계획으로 미리 보이고 한 번만 묻는다.
// true 면 설정 적용을 시도하고 결과를 보인 뒤 설정을 보낸 호스트를 재점검함 (recheckHosts, 흐름은 결과 보기로 돌아감).
// false 면 취소·거절·설정할 항목 없음·덤프 읽기 — 결과 보기로 돌아간다.
func (f *biosFlow) setStep() bool {
	ui := f.ui
	if f.a.O.fromDump != "" {
		f.setNote = "덤프 읽기 모드라 설정하지 않았습니다"
		return false
	}
	plan := flowBuildPlan(f.rc.Conf, f.u, f.cmp)
	if plan.Empty() {
		f.setNote = "설정할 항목이 없습니다"
		if cl := plan.ConfirmLines(); len(cl) > 0 { // 수정제외·충돌·제외 사유가 있으면 보여 준다
			ui.SetText("수정 미리보기", cl)
			ui.SetMessage("설정할 항목이 없습니다 — q 를 누르면 결과 보기로 돌아갑니다")
			f.wait("preview-empty")
			ui.Browse()
		}
		return false
	}
	// 확인 1: 차이 항목 전부 (호스트별 "현재값 -> 변경값 (수정대상)" / "(수정제외: 사유)", 값은 자르지 않음)
	ui.SetText("수정 미리보기", plan.ConfirmLines())
	f.wait("ask-set")
	if ui.Ask("위 수정대상을 적용할까요?") != TUIYes {
		f.setNote = "설정하지 않았습니다"
		return false
	}
	// 확인 2: 수정제외(보호 속성 BootMode 등·제외 사유)가 있으면 그것만 한 번 더 보이고 묻는다
	if ex := plan.ExcludedLines(); len(ex) > 0 {
		ui.SetText("수정제외 확인", ex)
		f.wait("ask-excluded")
		if ui.Ask("수정제외 항목을 확인했습니다. 진행할까요?") != TUIYes {
			f.setNote = "설정하지 않았습니다 (수정제외 확인에서 멈춤)"
			return false
		}
	}
	_, ok := f.applyStep(plan)
	return ok
}

// applyStep 은 확인을 마친 계획을 적용합니다: 남의 Pending 포함 여부 → 설정 적용 → 설정 결과 → 설정을 보낸 호스트 재점검
// (setStep·directStep 공용). Pending 질문에서 취소하면 (nil, false), 적용을 시도했으면 ok=true (결과는 오류면 nil).
func (f *biosFlow) applyStep(plan *setPlan) (*setOutcome, bool) {
	ui := f.ui
	include := false
	if len(plan.Pending) > 0 {
		ui.SetText("Pending 이 있는 장비", plan.PendingLines())
		f.wait("ask-pending")
		switch ui.Ask("Pending 이 있는 장비도 포함할까요? (y 포함 / n 제외 / Ctrl+X 취소)") {
		case TUIYes:
			include = true
		case TUINo:
		default:
			f.setNote = "설정을 취소했습니다"
			return nil, false
		}
	}
	var mu sync.Mutex
	var progLines []string // 호스트별 진행 줄 (결과에 줄이 없을 때 대신 보임)
	var res *setOutcome
	err := ui.Progress("설정 적용", func(j *TUIJob) error {
		var err error
		res, err = flowApplyPlan(f.rc, f.u, plan, include, func(host, msg string) {
			mu.Lock()
			progLines = append(progLines, host+": "+msg)
			n := len(progLines)
			mu.Unlock()
			j.Set(fmt.Sprintf("설정 적용 · 진행 %d줄 · %s: %s", n, host, msg))
		}, j.Canceled())
		return err
	})
	var lines []string
	if res != nil && len(res.Lines) > 0 {
		lines = append(lines, res.Lines...)
	} else {
		lines = append(lines, progLines...)
	}
	f.setNote = "설정 오류"
	if res != nil {
		f.setNote = fmt.Sprintf("Pending 확인 %d대 · 실패·미반영 %d대", res.OK, res.Fail)
		lines = append(lines, "", f.setNote)
	}
	if err != nil {
		lines = append(lines, "오류: "+err.Error())
		f.warns = append(f.warns, "설정 적용: "+err.Error())
	}
	lines = append(lines, flowNoReboot)
	f.setNote += " (" + flowNoReboot + ")"
	ui.SetText("설정 결과", lines)
	f.wait("set-result")
	ui.Browse()
	if res != nil { // 설정을 보낸 호스트만 묻지 않고 다시 점검해 결과 화면에 합친다 (Pending 반영 확인)
		ui.SwitchTab(ui.Tab())
		f.recheckHosts(res.Tried)
	}
	return res, true
}

// flowDirectGuide 는 BIOS 값 직접 입력 화면의 안내입니다.
var flowDirectGuide = []string{
	"한 줄에 하나: <Redfish 속성 이름><공백 또는 탭><설정값>   예) SysProfile PerfOptimized · WorkloadProfile Virtualization-MaxPerformance",
	"속성 이름은 BMC 가 보고하는 Redfish 이름입니다 (대소문자 무시). 값은 첫 공백 뒤 전부 (앞뒤 공백 제외). 빈 줄·# 줄은 무시.",
	"대상: 이번에 점검한 Redfish 호스트 전부 (Cisco(XML)·점검 오류 호스트는 건너뜀). 프로파일과 상관없습니다.",
	"보호 속성(서버 고유값·부팅·보안·RAID·PXE·NIC)·ReadOnly·허용값 밖·이미 같은 값은 건너뜁니다. BMC Pending 까지만 설정, 재부팅 없음.",
}

// directStep 은 BIOS 값 직접 입력입니다: 붙여넣기 → 호스트별 미리보기 → "적용할까요?" → applyStep(Pending 질문·적용·결과·재점검)
// → 재점검한 호스트의 입력 속성 현재값·Pending (directCheck). true 면 적용을 시도함, false 면 취소·거절·설정할 항목 없음.
func (f *biosFlow) directStep() bool {
	ui := f.ui
	ui.SetText("BIOS 값 직접 입력", flowDirectGuide)
	var reqs []directReq
	for len(reqs) == 0 {
		f.wait("direct-input")
		lines, ok := ui.InputLinesTab("BIOS 값 직접 입력 (속성 이름 설정값, Ctrl+D 완료 · Ctrl+X 취소)")
		if !ok {
			ui.TakeTabOut()
			f.setNote = "설정하지 않았습니다"
			return false
		}
		r, errs := parseDirectLines(lines)
		if len(errs) == 0 && len(r) == 0 {
			errs = []string{"입력한 속성이 없습니다"}
		}
		if len(errs) > 0 { // 잘못된 줄이 하나라도 있으면 아무것도 쓰지 않고 다시 입력받는다
			ui.SetText("BIOS 값 직접 입력 — 입력 오류", append(append([]string{}, errs...), append([]string{""}, flowDirectGuide...)...))
			ui.SetMessage(fmt.Sprintf("입력 오류 %d건 — 고쳐서 다시 입력하십시오 (Ctrl+X 취소)", len(errs)))
			continue
		}
		reqs = r
	}
	plan := buildDirectPlan(f.rc.Conf, f.u, reqs)
	ui.SetText("직접 입력 미리보기", plan.DirectLines())
	if plan.Empty() {
		f.setNote = "설정할 항목이 없습니다"
		ui.SetMessage("설정할 항목이 없습니다 — q 를 누르면 결과 보기로 돌아갑니다")
		f.wait("direct-empty")
		ui.Browse()
		return false
	}
	f.wait("direct-ask")
	if ui.Ask("위 수정대상을 적용할까요?") != TUIYes {
		f.setNote = "설정하지 않았습니다"
		return false
	}
	res, ok := f.applyStep(plan)
	if !ok {
		return false
	}
	f.directCheck(plan, res)
	return true
}

// directCheck 는 재점검을 마친 호스트의 직접 입력 속성 현재값·Pending 을 보입니다 (프로파일 점검 화면에 없는 속성일 수 있어서).
// 재점검하지 못한 호스트(f.unchecked)는 처음 값이라 빼고, 보일 줄이 없으면 아무것도 하지 않습니다.
func (f *biosFlow) directCheck(plan *setPlan, res *setOutcome) {
	if res == nil || f.ui.Quit() {
		return
	}
	done := map[int]bool{}
	for _, i := range res.Tried {
		done[i] = !f.unchecked[i]
	}
	var lines []string
	n, good := 0, 0
	for _, it := range plan.Items {
		if !done[it.hostIdx] {
			continue
		}
		n++
		hc, s := f.u.Check.Hosts[it.hostIdx], f.u.Snaps[it.hostIdx]
		if hc == nil || hc.Status != "" || s == nil {
			st := "결과 없음"
			if hc != nil {
				st = hc.Status
			}
			lines = append(lines, fmt.Sprintf("%s  %s: 재점검 오류 (%s)", it.Host, sanitizeDetail(it.Attr), st))
			continue
		}
		cur := s.Attrs[it.Attr]
		pend, hasP := hc.pendDiff[it.Attr]
		tag := "목표값 아님"
		switch {
		case hasP && directSame(pend, it.To):
			tag, good = "Pending 목표값 확인, 재부팅 대기", good+1
		case !hasP && directSame(cur, it.To):
			tag, good = "현재값이 목표값", good+1
		}
		if !hasP {
			pend = "-"
		}
		lines = append(lines, fmt.Sprintf("%s  %s: 현재값 %s · Pending %s (%s)", it.Host, sanitizeDetail(it.Attr), fullVal(cur), fullVal(pend), tag))
	}
	if n == 0 {
		return
	}
	f.setNote += fmt.Sprintf(" · 직접 입력 재점검 목표값 %d/%d건", good, n)
	head := fmt.Sprintf("직접 입력 확인 (재점검): 목표값 확인 %d/%d건 — %s", good, n, flowNoReboot)
	f.ui.SetText("직접 입력 확인", append([]string{head}, lines...))
	f.wait("direct-check")
	f.ui.Browse()
}

// recheckHosts 는 설정을 보낸 호스트(idx: 점검 결과 순번 = setOutcome.Tried)만 다시 점검해 f.u 의 해당 호스트 결과
// (프로파일 판정·스냅샷)를 바꾸고 프로파일 점검 화면에 새 결과를 보입니다. 쓰기는 없습니다 (check 와 같은 읽기 전용).
//   - 재점검 결과 파일은 결과 폴더/recheck/<일시>/ 에 두고, 합친 전체 결과 CSV(profile_merged_*)를 거기에 씁니다.
//     처음 점검 파일(profile_* CSV·result.tsv)은 적용 전 기록이라 덮어쓰지 않습니다.
//   - 전체 설정 비교는 적용 전 스냅샷 기준이라 비웁니다 (f.cmp=nil) → 다음 Tab 에 다시 비교합니다.
//   - Ctrl+X 로 취소했거나 접속하지 못한 호스트(CANCELED·SKIPPED_AUTH_STOP)는 처음 결과를 그대로 두고 f.unchecked 에 남깁니다
//     (남아 있는 동안 다시 설정하지 않음). 요약 줄은 f.setNote 에 붙입니다.
//
// 설정을 보내는 다른 경로(값 직접 입력)도 보낸 호스트 순번으로 이 함수를 부릅니다.
func (f *biosFlow) recheckHosts(idx []int) {
	ui := f.ui
	if len(idx) == 0 || f.u == nil || f.u.Check == nil || f.u.Check.prof == nil || f.a.O.fromDump != "" || ui.Quit() {
		return
	}
	if f.unchecked == nil {
		f.unchecked = map[int]bool{}
	}
	sub := *f.rc
	sub.Targets = make([]Target, len(idx))
	for k, i := range idx {
		sub.Targets[k] = f.rc.Targets[i]
		f.unchecked[i] = true
	}
	var re *unifiedRun
	var err error
	ui.Progress("재점검 (설정을 보낸 호스트)", func(j *TUIJob) error {
		p := newSinkProgress(1, j.Set)
		p.Start()
		defer p.Stop()
		re, err = runUnified(&sub, unifiedOpts{
			Profile: f.u.Check.prof, ResultDir: filepath.Join(f.u.Check.Dir, "recheck"), HostsPath: f.a.O.hosts, Ignore: f.a.Ignore,
			Collect: progStage{P: p, Idx: 1, Name: "접속·조회"}, Cancel: j.Canceled(),
		})
		return nil
	})
	if re == nil {
		f.warns = append(f.warns, "재점검 실패: "+err.Error())
		f.setNote += " · 재점검 실패: " + err.Error()
		return
	}
	if err != nil {
		f.warns = append(f.warns, "재점검 결과 파일 기록 중 오류: "+err.Error())
	}
	done := 0
	for k, i := range idx {
		if h := re.Check.Hosts[k]; h.Status != StatusCanceled && h.Status != StatusSkippedAuthStop {
			f.u.Check.Hosts[i], f.u.Snaps[i] = h, re.Snaps[k]
			delete(f.unchecked, i)
			done++
		}
	}
	f.setNote += fmt.Sprintf(" · 재점검 %d대 완료", done)
	if n := len(idx) - done; n > 0 {
		f.setNote += fmt.Sprintf(" (%d대는 재점검하지 못해 처음 결과 그대로)", n)
	}
	if done == 0 {
		return
	}
	if f.cmp != nil {
		f.cmp = nil
		ui.SwitchTab(1)
		ui.SetResults(flowTabs[1], nil)
	}
	plines := f.u.ProfileLines()
	ui.SwitchTab(0)
	ui.SetResults("BIOS 점검 결과 — 프로파일 "+f.profName, groupLines(plines))
	ui.Progress("CSV 저장 (재점검)", func(j *TUIJob) error {
		f.writeCSVIn(re.Check.Dir, "profile_merged_", plines, j.Canceled())
		return nil
	})
}

// countStatus 는 프로파일 점검에서 호스트 상태가 st 인 대수입니다.
func (f *biosFlow) countStatus(st string) int {
	n := 0
	for _, h := range f.u.Check.Hosts {
		if h != nil && h.Status == st {
			n++
		}
	}
	return n
}

// countCmp 는 전체비교에서 호스트 상태가 st 인 대수입니다.
func (f *biosFlow) countCmp(st string) int {
	n := 0
	if f.cmp != nil {
		for _, h := range f.cmp.Hosts {
			if h.Status == st {
				n++
			}
		}
	}
	return n
}

// summary 는 TUI 를 닫은 뒤 찍는 짧은 평문 요약입니다.
func (f *biosFlow) summary(w io.Writer) {
	if f.u == nil {
		if f.canceled {
			fmt.Fprintln(w, "취소했습니다 — 점검하지 않았습니다")
		}
		return
	}
	cnt := map[string]int{}
	for _, h := range f.u.Check.Hosts {
		if h != nil {
			cnt[hostProgTag(h)]++
		}
	}
	fmt.Fprintf(w, "프로파일 %s, 대상 %d대 점검 — OK %d · difference %d · N/A %d · 오류 %d\n", f.profName, len(f.u.Check.Hosts),
		cnt[TagOK], cnt[TagDiff], cnt[TagNA], cnt[progTagErr])
	if n := f.countStatus(StatusCanceled); n > 0 {
		fmt.Fprintf(w, "취소: %d대는 조회하지 않았습니다 (%s, retry.txt 로 다시 실행)\n", n, StatusCanceled)
	}
	if f.cmp != nil {
		diff := 0
		for _, h := range f.cmp.Hosts {
			if h.Status == StatusDiff {
				diff++
			}
		}
		fmt.Fprintf(w, "전체비교: 기준 %s — 차이 있는 호스트 %d대\n", f.cmp.Label, diff)
		if f.cmp.RefCanceled > 0 {
			fmt.Fprintf(w, "전체비교 취소: 비교서버 %d대 조회 안 함 — 대상 %d대 %s (N/A, 설정 제외)\n",
				f.cmp.RefCanceled, f.countCmp(StatusRefCanceled), StatusRefCanceled)
		}
	}
	if f.setNote != "" {
		fmt.Fprintf(w, "설정: %s\n", f.setNote)
	}
	dir := filepath.ToSlash(f.u.Check.Dir)
	fmt.Fprintf(w, "결과 폴더: %s/ (result.tsv, CSV %d개)\n", dir, len(f.csvs))
	for _, p := range f.csvs {
		fmt.Fprintf(w, "  %s\n", filepath.ToSlash(p))
	}
}
