package main

// tui_flow.go — `biostool bios` 대화형 흐름 (계획서_v0.2_TUI.md "흐름" 1~6, 단계 6). TUI 세션 1개 안에서 제자리 갱신만 한다.
//
//	대상 입력(파일 / 붙여넣기, IP 가 있으면 Cisco(XML) 대상도) → 프로파일 선택 → [2/4 접속·조회] → [3/4 CSV 저장] → 결과 화면 [프로파일 점검]
//	  Tab → [전체 설정 비교] 화면 (처음이면 기준 선택 → 비교서버 조회 → 결과), 다시 Tab 이면 [프로파일 점검] (화면마다 위치 유지)
//	  r (전체 설정 비교 화면) → 기준 다시 선택 → 다시 비교
//	  -ref/-ref-json/-diff 를 줬으면 수집 직후 바로 전체 설정 비교까지 (Tab 을 기다리지 않음)
//	  q 는 한 단계 뒤로 (관리망 그룹 줄 → 벤더 줄, 미리보기·설정 결과 → 결과 보기, 프로파일 고르기 → 대상 입력)
//	  벤더 줄에서 q 두 번 → 묻지 않고 끝. e → 어디서든 수정 단계 (수정 방법: 대표서버 기준으로 맞추기 / 점검 결과 기준 수정 / BIOS 값 직접 입력)
//	→ (대표서버 기준으로 맞추기면 전체비교(안 했으면 대표서버 자동 지정)의 대표서버와 다른 속성을 묶음(태그·모델)별로 체크 → 아래 확인 1·2부터 같음, setalign.go)
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
	// alignBack 은 대표서버 기준 맞추기 체크 목록에서 Esc 를 눌렀음을 editStep 에 알립니다 (수정 방법 선택으로 한 단계 뒤).
	alignBack bool
	// 대상 입력에서 마지막으로 친 내용 (오류·뒤로 가기 뒤 다시 열 때 미리 채워 둔다).
	lastPaste, lastXMLPaste []string
	lastFile                string
	// 전체비교 뒤 모델이 다른 대상의 참고용 비교 계획 (x 키로 CSV 를 만들면 만든 계획은 뺀다).
	xplans []*xmodelPlan
	xdone  bool // x 로 한 번이라도 만들었음 (남은 계획이 없을 때 "이미 만들었습니다" 안내용)
	// autoPick 은 대표서버 자동 지정 뒤 c 키로 직접 고른 대표입니다 (묶음 ID → 호스트 targetID). 같은 비교 세션 동안 유지하고,
	// 기준 메뉴에서 "대표서버 자동 지정"을 다시 고르면 비웁니다 (처음부터 자동 선정).
	autoPick map[string]string
	// profNote 는 p 키로 프로파일에 행을 더한 기록입니다 (요약의 "프로파일 변경:" 줄, tui_profadd.go).
	profNote []string
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
	ui.SetProfileKey(true) // p: 프로파일 점검 화면에서 프로파일에 태그·모델별 설정 행 추가 (tui_profadd.go)
	ui.SetResults("BIOS 점검 결과 — 프로파일 "+f.profName, groupLines(plines))
	ui.SetStage(4)
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
		case TUIXModel:
			f.xmodelKey()
		case TUIPick:
			if !f.repickKey() && ui.TakeTabOut() { // 고르다 Tab 이면 프로파일 화면으로 (앞의 비교 결과는 그대로)
				ui.SwitchTab(0)
			}
		case TUIProfAdd:
			f.profAddKey()
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
		if isIPInput(listHost(in)) { // 줄이 "호스트 태그" 일 수 있다
			return true
		}
	}
	return false
}

// askTargets 는 대상을 정합니다. 돌려준 문자열이 비면 대상 목록 파일(o.user)을 쓴다. ok=false 는 취소.
// 대상에 IP 가 있으면(xml_host_prefix 가 비면 늘) Cisco(XML) 대상을 따로 묻는다: 파일은 o.userXML, 붙여넣기는 o.userXMLArg
// (hostname 은 xml_host_prefix 로 자동으로 나뉘므로 묻지 않음).
func (f *biosFlow) askTargets() (string, bool) {
	f.ui.SetStage(1)
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
	xmlFileTitle, xmlPasteHelp := "Cisco(XML) 대상 파일 (IP 로 적은 Cisco 장비, 비우면 없음)",
		"IP 로 적은 Cisco 장비만, 없으면 바로 Enter"
	if len(pfx) == 0 {
		xmlFileTitle, xmlPasteHelp = "Cisco(XML) 대상 파일 (Cisco 장비, 비우면 없음)",
			"Cisco 장비만, 없으면 바로 Enter"
	}
	pasteHelp := []string{"한 줄에 하나: hostname 또는 hostname 태그 (예: 10.0.0.1 web)",
		"엑셀에서 복사한 IP·태그 칸을 그대로 붙여넣으면 됩니다. 다 넣었으면 빈 줄에서 Enter"}
	o.targetsPaste = false
	o.userXML, o.userXMLArg = "", "" // 대상 입력으로 돌아오면 앞에서 넣은 Cisco(XML) 대상은 버린다
	// 친 내용(f.lastPaste 등)은 오류·뒤로 가기(프로파일 고르기 취소 포함) 뒤에도 남겨 다시 열 때 미리 채운다.
menu:
	for {
		f.wait("targets")
		i, ok := ui.Choose("대상 입력", []string{"대상 파일 지정", "직접 입력 (붙여넣기: hostname / hostname-m / IP)"})
		if !ok {
			return "", false
		}
		if i == 0 {
			initial := f.lastFile
			if initial == "" {
				initial = f.a.O.user
			}
			for {
				f.wait("targets-file")
				p, ok := ui.InputLine("대상 파일", initial)
				if !ok {
					if ui.Quit() {
						return "", false
					}
					continue menu
				}
				initial, f.lastFile = p, p
				if _, err := os.Stat(p); p == "" || err != nil {
					ui.SetMessage("대상 파일을 찾을 수 없습니다: " + p)
					continue // 친 경로를 둔 채 다시
				}
				f.a.O.user = p
				if in, _ := loadList(p); askXML(in) { // 읽기 오류는 prepare 가 알린다
					x, ok := f.askXMLFile(xmlFileTitle)
					if !ok {
						if ui.Quit() {
							return "", false
						}
						continue menu
					}
					o.userXML = x
				}
				return "", true
			}
		}
		initial := f.lastPaste
	paste:
		for {
			f.wait("targets-paste")
			// Excel 에서 복사한 "IP<Tab>태그" 가 들어오므로 Tab 은 화면 전환이 아니라 공백으로 받는다.
			lines, ok := ui.InputLinesOpt("대상 붙여넣기", TUILinesOpt{TabText: true, Help: pasteHelp, Initial: initial})
			if !ok {
				if ui.Quit() {
					return "", false
				}
				continue menu
			}
			initial, f.lastPaste = lines, lines
			in, err := pasteTargetLines(lines)
			if err != nil {
				ui.SetMessage(err.Error())
				continue // 친 줄을 둔 채 상자에서 바로 고친다
			}
			if len(in) == 0 {
				ui.SetMessage("입력한 대상이 없습니다")
				continue
			}
			xmlArg := ""
			if askXML(in) {
				xinit := f.lastXMLPaste
				for {
					f.wait("targets-xml-paste")
					xl, ok := ui.InputLinesOpt("Cisco(XML) 대상 붙여넣기", TUILinesOpt{TabText: true, Help: []string{xmlPasteHelp}, Initial: xinit})
					if !ok {
						if ui.Quit() {
							return "", false
						}
						continue paste // 한 단계 뒤(대상 붙여넣기)로, 앞에서 친 줄은 그대로
					}
					xinit, f.lastXMLPaste = xl, xl
					xin, err := pasteTargetLines(xl)
					if err != nil {
						ui.SetMessage("Cisco(XML) " + err.Error())
						continue
					}
					xmlArg = strings.Join(xin, "\n")
					break
				}
			}
			o.userXMLArg = xmlArg
			o.targetsPaste = true
			return strings.Join(in, "\n"), true
		}
	}
}

// pasteTargetLines 는 TUI 에 붙여넣은 대상 줄을 user.txt 형식의 목록으로 바꿉니다: 한 줄 = "hostname" 또는 "hostname 태그"
// (Excel 복사의 Tab 은 입력 단계에서 공백이 됨). 쉼표·세미콜론은 줄바꿈처럼 나누고, 빈 줄·# 주석은 건너뛰며 같은 호스트는 처음 것만 남깁니다.
// 칸이 3개 이상인 줄이 있으면 오류입니다 (resolveTargets 와 같은 규칙 — 미리 알리고 다시 묻는다).
func pasteTargetLines(lines []string) ([]string, error) {
	in := parseList(strings.NewReplacer(",", "\n", ";", "\n").Replace(strings.Join(lines, "\n")))
	for _, l := range in {
		if len(strings.Fields(l)) > 2 {
			return nil, fmt.Errorf("대상 줄 %q: 칸이 3개 이상입니다 (hostname 또는 \"hostname 태그\" 만 허용)", l)
		}
	}
	return in, nil
}

// pasteRefHosts 는 태그 모드가 아닐 때의 비교서버 붙여넣기를 hostname 목록으로 바꿉니다: 줄마다 첫 칸이 hostname 이고
// 나머지(Excel 의 메모·태그 칸)는 무시합니다. 쉼표·세미콜론은 줄바꿈처럼 나눕니다 (공백은 구분자가 아님 — 메모가 호스트가 되지 않게).
func pasteRefHosts(lines []string) []string {
	var out []string
	for _, l := range parseList(strings.NewReplacer(",", "\n", ";", "\n").Replace(strings.Join(lines, "\n"))) {
		out = append(out, listHost(l))
	}
	return out
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
	f.ui.SetStage(2)
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
	f.ui.SetStage(3)
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
	f.autoPick = nil // 기준을 (다시) 골랐으면 앞의 대표 직접 고르기는 버린다 — c 키 재비교는 compareWith 를 바로 부른다
	return f.compareWith(ref)
}

// compareWith 는 정해진 기준으로 비교 → CSV → 전체 설정 비교 화면에 결과를 넣습니다 (compareStep 의 뒷부분, c 키 재비교도 씀).
// 실패면 안내 줄만 남기고 false (앞 결과는 그대로).
func (f *biosFlow) compareWith(ref compareRef) bool {
	ui := f.ui
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
	xmsg := f.xmodelNote(cmp)
	if cmp.Auto != nil { // 대표서버 자동 지정: 선정 결과를 compare_refs.tsv 에 남기고 대표를 안내 (c 키로 묶음별 재선택)
		if path, err := writeAutoRefs(f.u.Check.Dir, cmp.Auto); err != nil {
			f.warns = append(f.warns, "compare_refs.tsv 기록 오류: "+err.Error())
		} else if !inList(f.csvs, path) {
			f.csvs = append(f.csvs, path)
		}
		ui.SetPickKey(cmp.Auto.Choosable())
		msg := cmp.Auto.Announce()
		if cmp.Auto.Choosable() {
			msg += " (c: 묶음별 대표 바꾸기)"
		}
		if xmsg != "" {
			msg += " / " + xmsg
		}
		ui.SetMessage(msg)
	} else if cmp.RefCanceled > 0 {
		msg := fmt.Sprintf("취소했습니다 — 비교서버 %d대 조회 안 함 (비교하지 못한 대상 %d대는 %s, 설정 제외)",
			cmp.RefCanceled, f.countCmp(StatusRefCanceled), StatusRefCanceled)
		if xmsg != "" {
			msg += " / " + xmsg
		}
		ui.SetMessage(msg)
	} else if xmsg != "" {
		ui.SetMessage(xmsg)
	}
	return true
}

// repickKey 는 c 키입니다 (대표서버 자동 지정으로 비교한 결과 화면): 묶음을 고르고 그 묶음의 다른 서버를 대표로 골라
// 같은 스냅샷으로 다시 비교합니다 (BMC 에 다시 접속하지 않음). 고른 대표는 f.autoPick 에 남아 다음 c 재비교에도 유지됩니다.
// 고르기를 끝까지 마치고 다시 비교했으면 true, 취소·할 일 없음·비교 실패면 false (Tab 으로 나왔으면 부르는 쪽이 TakeTabOut).
func (f *biosFlow) repickKey() bool {
	ui := f.ui
	if f.cmp == nil || f.cmp.Auto == nil {
		ui.SetMessage("대표서버 자동 지정으로 비교한 결과가 아닙니다 (r 로 \"" + flowAutoRefItem + "\" 를 고르십시오)")
		return false
	}
	p := f.cmp.Auto
	var bs []*autoBundle
	var items []string
	for _, b := range p.Bundles {
		if len(b.Idx) < 2 { // 1대뿐인 묶음은 바꿀 서버가 없다
			continue
		}
		bs = append(bs, b)
		mark := ""
		if b.Manual {
			mark = " (직접 지정)"
		}
		items = append(items, fmt.Sprintf("%s %d대 — 대표 %s%s · %s", b.Name, len(b.Idx), p.RepSnap(b).Target.Hostname, mark, b.Reason))
	}
	if len(bs) == 0 {
		ui.SetMessage("대표서버를 바꿀 수 있는 묶음이 없습니다 (모든 묶음이 1대뿐)")
		return false
	}
	f.wait("repick-bundle")
	i, ok := ui.Choose("대표서버를 바꿀 묶음 (태그·모델)", items)
	if !ok {
		return false
	}
	b := bs[i]
	hosts := make([]string, len(b.Idx))
	for k, si := range b.Idx {
		s := f.u.Snaps[si]
		hosts[k] = fmt.Sprintf("%s  BIOS %s · 다수결 일치 %d/%d", s.Target.Hostname, dash(strings.TrimSpace(s.BiosVersion)), b.Scores[k], b.Total)
		switch {
		case si == b.Rep:
			hosts[k] += "  ← 지금 대표"
		case si == b.AutoRep:
			hosts[k] += "  (자동 선정)"
		}
	}
	f.wait("repick-host")
	j, ok := ui.Choose(fmt.Sprintf("%s 대표서버 선택 — 지금 %s: %s", b.Name, p.RepSnap(b).Target.Hostname, b.Reason), hosts)
	if !ok {
		return false
	}
	si := b.Idx[j]
	if si == b.Rep {
		ui.SetMessage(f.u.Snaps[si].Target.Hostname + " 이(가) 이미 " + b.Name + " 의 대표서버입니다")
		return false
	}
	pick := map[string]string{}
	for k, v := range f.autoPick {
		pick[k] = v
	}
	if id := b.ID(p.TagMode); si == b.AutoRep {
		delete(pick, id) // 자동 선정으로 되돌림
	} else {
		pick[id] = targetID(f.u.Snaps[si].Target)
	}
	f.autoPick = pick
	return f.compareWith(compareRef{Auto: true, AutoPick: pick})
}

// xmodelNote 는 전체비교 직후 모델이 다른 대상 계획을 잡아 두고(f.xplans) x 키를 켭니다. 묻지 않는다 — 안내 문구만 돌려줍니다 (계획이 없으면 "").
// CSV 는 x 키(xmodelKey)를 눌렀을 때만 씁니다. 전체 비교 결과(f.cmp)·설정 계획은 건드리지 않는다 (그 대상은 계속 NO_REFERENCE).
// r 로 기준을 다시 고를 때마다 계획을 다시 잡는다. -auto/-auto-check 는 이 흐름을 지나지 않는다.
func (f *biosFlow) xmodelNote(cmp *compareResult) string {
	f.xplans, f.xdone = xmodelPlansFor(cmp, f.u.Snaps), false
	f.ui.SetXKey(len(f.xplans) > 0)
	n := 0
	for _, p := range f.xplans {
		for _, g := range p.Groups {
			n += len(g)
		}
	}
	if n == 0 {
		return ""
	}
	return fmt.Sprintf("모델이 다른 대상이 있습니다 (%d대) — x: 참고용 비교 CSV 만들기", n)
}

// xmodelKey 는 x 키입니다: 남은 계획마다 참고용 CSV(compare_xmodel_*)를 쓰고 안내를 바꿉니다. 만든 계획은 뺀다 (다시 누르면 "이미 만들었습니다").
// Ctrl+X 로 취소하면 쓰지 못한 계획은 남겨 두어 x 로 다시 만들 수 있습니다.
func (f *biosFlow) xmodelKey() {
	ui := f.ui
	if len(f.xplans) == 0 {
		if f.xdone {
			ui.SetMessage("이미 만들었습니다")
		} else {
			ui.SetMessage("모델이 다른 대상이 없습니다")
		}
		return
	}
	plans := f.xplans
	var parts []string
	ui.Progress("CSV 저장 (모델이 다른 비교)", func(j *TUIJob) error {
		for k, p := range plans {
			s, done := f.writeXModel(p, j.Canceled())
			if s != "" {
				parts = append(parts, s)
			}
			if !done {
				f.xplans = plans[k:]
				return nil
			}
			f.xplans = plans[k+1:]
		}
		return nil
	})
	f.xdone = true
	msg := strings.Join(parts, " / ")
	if msg == "" {
		msg = "참고용 비교 CSV 를 만들지 못했습니다 (결과 폴더 쓰기 경고 확인)"
	}
	if len(f.xplans) > 0 {
		msg += " / 취소했습니다 — 남은 계획은 x 로 다시 만들 수 있습니다"
	}
	ui.SetMessage(msg)
}

// writeXModel 은 모델이 다른 비교 계획 1건의 CSV 를 결과 폴더에 쓰고 안내 문구(파일마다 " / " 로 이은 것)를 돌려줍니다.
// cancel 이 닫히면 남은 파일은 쓰지 않고 done=false 입니다. 쓰기 오류는 f.warns 에 남긴다.
func (f *biosFlow) writeXModel(p *xmodelPlan, cancel <-chan struct{}) (string, bool) {
	prot := protectFor(f.rc.Conf)
	var parts []string
	for _, g := range p.Groups {
		select {
		case <-cancel: // Ctrl+X: 남은 파일은 쓰지 않음
			return strings.Join(parts, " / "), false
		default:
		}
		c := buildXModel(p.B, g, f.u.Ignore, prot)
		c.Tag = p.Tag
		path := filepath.Join(f.u.Check.Dir, c.FileName())
		if err := writeCSVFile(path, c.Rows(p.BHost)); err != nil {
			f.warns = append(f.warns, err.Error())
			continue
		}
		if !inList(f.csvs, path) {
			f.csvs = append(f.csvs, path)
		}
		pre := ""
		if p.Tag != "" {
			pre = "[태그 " + p.Tag + "] "
		}
		parts = append(parts, fmt.Sprintf("%s모델이 다른 비교 (%s %d대 ↔ %s): %s → %s", pre, c.AName, len(g), c.BName, c.Summary(), path))
	}
	return strings.Join(parts, " / "), true
}

// flowAutoRefItem 은 기준 메뉴 맨 위 항목입니다.
const flowAutoRefItem = "대표서버 자동 지정 (태그·모델별 1대)"

// chooseRef 는 전체비교 기준입니다: -ref/-ref-json/-diff 를 줬으면 그것(again 이면 무시하고 메뉴), 아니면 메뉴
// (1 대표서버 자동 지정 / 2 비교서버 hostname 입력 / refs_dir 의 JSON / 마지막 diff.txt). hostname 을 비워 두면 diff.txt.
func (f *biosFlow) chooseRef(again bool) (compareRef, bool) {
	if f.a.Ref.count() > 0 && !again {
		return f.a.Ref, true
	}
	refs, err := listRefJSON(f.rc.Conf.RefsDir)
	if err != nil {
		f.ui.SetMessage(err.Error())
	}
	items := []string{flowAutoRefItem, "비교서버 hostname 입력"}
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
		return compareRef{Auto: true}, true
	case i == 1:
		f.wait("ref-host")
		// 태그 모드(대상에 태그가 있음)면 한 줄이 "hostname 태그" 라 공백으로 쪼개지 않는다 (compareRefs 가 줄마다 parseRefList).
		tagMode := f.u.tagMode()
		help := []string{"한 줄에 hostname 하나 (뒤 칸은 무시)", "바로 Enter 면 " + flowDiffFile + " 를 씁니다"}
		if tagMode {
			help[0] = "한 줄에 hostname 태그 하나 — 대상과 같은 태그끼리만 비교"
		}
		// Excel 복사("hostname<Tab>태그/메모") 의 Tab 은 화면 전환이 아니라 공백으로 받는다.
		hs, ok := f.ui.InputLinesOpt("비교서버 입력", TUILinesOpt{TabText: true, Help: help})
		if !ok {
			return compareRef{}, false
		}
		var h []string
		if tagMode {
			h = parseList(strings.NewReplacer(",", "\n", ";", "\n").Replace(strings.Join(hs, "\n")))
		} else {
			h = pasteRefHosts(hs) // 첫 칸만 hostname (뒤 칸 무시)
		}
		if len(h) > 0 {
			return compareRef{Hosts: h}, true
		}
		return compareRef{DiffFile: flowDiffFile}, true
	case i <= len(good)+1:
		return compareRef{JSONPath: good[i-2].Path}, true
	}
	return compareRef{DiffFile: flowDiffFile}, true
}

// flowEditMenu 는 수정 단계(e)의 수정 방법입니다: 0 대표서버 기준으로 맞추기(alignStep), 1 점검 결과 기준 수정(setStep),
// 2 BIOS 값 직접 입력(directStep).
var flowEditMenu = []string{"대표서버 기준으로 맞추기", "점검 결과 기준 수정", "BIOS 값 직접 입력"}

// editStep 은 수정 단계 입구입니다: 수정 방법을 고른다. 덤프 읽기면 고르지 않고 false, 고르기 취소면 false (결과 보기로).
func (f *biosFlow) editStep() bool {
	f.ui.SetStage(5)
	defer f.ui.SetStage(4) // 수정 단계에서 돌아오면 결과 보기
	if f.a.O.fromDump != "" {
		f.setNote = "덤프 읽기 모드라 설정하지 않았습니다"
		return false
	}
	for {
		f.wait("edit-menu")
		i, ok := f.ui.Choose("수정 방법 선택", flowEditMenu)
		if !ok {
			f.ui.TakeTabOut() // Tab 으로 접었어도 결과 보기로 (화면 탭은 그대로)
			return false
		}
		switch i {
		case 0:
			tried := f.alignStep()
			if f.alignBack { // 체크 목록에서 Esc: 한 단계 뒤인 수정 방법 선택으로
				f.alignBack = false
				continue
			}
			return tried
		case 2:
			return f.directStep()
		}
		return f.setStep()
	}
}

// flowAlignHelp 는 대표서버 기준 맞추기 체크 목록 위의 안내입니다.
var flowAlignHelp = []string{
	"한 줄 = 그 묶음에서 대표서버와 값이 다른 서버 전부에 그 속성을 대표서버 값으로 맞춤. 처음엔 아무것도 체크되어 있지 않습니다.",
	alignNote,
	"[-] 줄은 자동 제외 (보호 속성·고유값·한쪽에만 있는 속성 등). BMC Pending 까지만 설정, 재부팅 없음.",
}

// alignStep 은 "대표서버 기준으로 맞추기" 입니다: (전체비교를 아직 안 했으면 대표서버 자동 지정으로 먼저 비교) → 묶음별 체크 목록 →
// 확인 1·2 → applyStep(Pending 질문·적용·결과·재점검) → 고른 속성이 대표서버 값이 됐는지 확인(alignCheck).
// true 면 적용을 시도함, false 면 취소·거절·고른 항목 없음·할 일 없음. 취소는 어느 단계에서든 아무것도 쓰지 않는다.
func (f *biosFlow) alignStep() bool {
	ui := f.ui
	switch {
	case f.a.O.fromDump != "":
		f.setNote = "덤프 읽기 모드라 설정하지 않았습니다"
		return false
	case len(f.unchecked) > 0:
		f.setNote = fmt.Sprintf("설정을 보낸 %d대를 재점검하지 못했습니다 — 결과가 적용 전 값이라 다시 점검한 뒤 설정하십시오", len(f.unchecked))
		return false
	}
	prev := ui.Tab()
	defer func() { // 비교를 새로 했어도 수정 단계에서 돌아오면 있던 화면으로 (재점검이 비교를 비웠으면 그 화면 그대로)
		if f.cmp != nil && ui.Tab() != prev {
			ui.SwitchTab(prev)
		}
	}()
	if f.cmp == nil { // 전체비교를 아직 안 했으면 대표서버 자동 지정으로 먼저 비교 (읽은 스냅샷만 씀, 접속 없음)
		ui.SwitchTab(1)
		f.autoPick = nil
		if !f.compareWith(compareRef{Auto: true}) {
			f.setNote = "대표서버 자동 지정 비교를 하지 못해 맞추지 않았습니다"
			return false
		}
	}
	cands := buildAlignCands(f.rc.Conf, f.u, f.cmp)
	if cands.Msg != "" {
		f.setNote = cands.Msg
		ui.SetText("대표서버 기준으로 맞추기", append([]string{cands.Msg}, cands.base.notes...))
		ui.SetMessage(cands.Msg + " — q 를 누르면 결과 보기로 돌아갑니다")
		f.wait("align-empty")
		ui.Browse()
		return false
	}
	f.wait("align-pick")
	picked, ok := ui.ChooseMulti("대표서버 기준으로 맞추기 — 바꿀 항목 선택", cands.Rows(), TUIMultiOpt{Help: flowAlignHelp})
	if !ok { // Esc 는 한 단계 뒤(수정 방법 선택), Ctrl+X 는 전체 취소, Tab 은 접기 — 모두 아무것도 쓰지 않음
		abort, tabOut := ui.TakeAbort(), ui.TakeTabOut()
		if !abort && !tabOut {
			f.alignBack = true
			return false
		}
		f.setNote = "설정하지 않았습니다"
		return false
	}
	if len(picked) == 0 {
		f.setNote = "고른 항목이 없습니다 — 설정하지 않았습니다"
		return false
	}
	plan := alignPlan(cands, picked)
	if plan.Empty() { // 고른 줄이 호스트 관문에서 모두 빠짐 — 사유를 보여 준다
		f.setNote = "설정할 항목이 없습니다"
		if cl := plan.ConfirmLines(); len(cl) > 0 {
			ui.SetText("수정 미리보기", cl)
			ui.SetMessage("설정할 항목이 없습니다 — q 를 누르면 결과 보기로 돌아갑니다")
			f.wait("preview-empty")
			ui.Browse()
		}
		return false
	}
	ui.SetText("수정 미리보기", plan.ConfirmLines())
	f.wait("ask-set")
	if ui.Ask("위 수정대상을 적용할까요?") != TUIYes {
		f.setNote = "설정하지 않았습니다"
		return false
	}
	if ex := plan.ExcludedLines(); len(ex) > 0 {
		ui.SetText("수정제외 확인", ex)
		f.wait("ask-excluded")
		if ui.Ask("수정제외 항목을 확인했습니다. 진행할까요?") != TUIYes {
			f.setNote = "설정하지 않았습니다 (수정제외 확인에서 멈춤)"
			return false
		}
	}
	res, ok := f.applyStep(plan)
	if !ok {
		return false
	}
	f.alignCheck(plan, res)
	return true
}

// alignCheck 는 재점검을 마친 호스트의 고른 속성 현재값·Pending 이 대표서버 값인지 보입니다 (directCheck 와 같은 모양).
// 재점검하지 못한 호스트(f.unchecked)는 처음 값이라 빼고, 보일 줄이 없으면 아무것도 하지 않습니다.
// 비교 결과는 적용 전 값이라 비웠으므로 다시 맞추려면 전체 설정 비교 화면에서 r 로 다시 비교해야 합니다.
func (f *biosFlow) alignCheck(plan *setPlan, res *setOutcome) {
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
		tag := "대표서버 값 아님"
		switch {
		case hasP && directSame(pend, it.To):
			tag, good = "Pending 이 대표서버 값, 재부팅 대기", good+1
		case !hasP && directSame(cur, it.To):
			tag, good = "현재값이 대표서버 값", good+1
		}
		if !hasP {
			pend = "-"
		}
		lines = append(lines, fmt.Sprintf("%s  %s: 현재값 %s · Pending %s · 대표서버 %s (%s)", it.Host, sanitizeDetail(it.Attr), fullVal(cur), fullVal(pend), fullVal(it.To), tag))
	}
	if n == 0 {
		return
	}
	f.setNote += fmt.Sprintf(" · 맞추기 재점검 대표서버 값 %d/%d건", good, n)
	head := fmt.Sprintf("맞추기 확인 (재점검): 대표서버 값이 된 항목 %d/%d건 — %s", good, n, flowNoReboot)
	f.ui.SetText("맞추기 확인", append([]string{head, "※ 비교 결과는 적용 전 값이라 비웠습니다 — 다시 맞추려면 전체 설정 비교 화면에서 r 로 다시 비교하십시오"}, lines...))
	f.wait("align-check")
	f.ui.Browse()
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
	var typed []string // 입력 오류 뒤 다시 열 때 미리 채울 줄
	help := []string{"한 줄에 하나: 속성이름 설정값 (예: SysProfile PerfOptimized)", "다 넣었으면 빈 줄에서 Enter"}
	for len(reqs) == 0 {
		f.wait("direct-input")
		lines, ok := ui.InputLinesOpt("BIOS 값 입력", TUILinesOpt{TabText: true, Help: help, Initial: typed})
		if !ok {
			ui.TakeTabOut()
			f.setNote = "설정하지 않았습니다"
			return false
		}
		typed = lines
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
func (f *biosFlow) recheckHosts(idx []int) { f.recheckHostsFor(idx, false) }

// recheckHostsFor 는 recheckHosts 의 본체입니다. profileOnly 는 프로파일 행만 바뀐 뒤의 재점검입니다 (profAdd): 설정을 보내지
// 않았으므로 f.unchecked 에 올리지 않고(취소해도 e 가 막히지 않음), BIOS 전체 값은 그대로라 전체 설정 비교(f.cmp)도 비우지 않습니다.
func (f *biosFlow) recheckHostsFor(idx []int, profileOnly bool) {
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
		if !profileOnly {
			f.unchecked[i] = true
		}
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
	if f.cmp != nil && !profileOnly {
		f.cmp = nil
		f.xplans, f.xdone = nil, false
		ui.SwitchTab(1)
		ui.SetResults(flowTabs[1], nil) // r·x 키도 꺼진다
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
		if f.cmp.Auto != nil {
			fmt.Fprintf(w, "%s (선정 사유: %s)\n", f.cmp.Auto.Announce(), autoRefsFile)
		}
		if f.cmp.RefCanceled > 0 {
			fmt.Fprintf(w, "전체비교 취소: 비교서버 %d대 조회 안 함 — 대상 %d대 %s (N/A, 설정 제외)\n",
				f.cmp.RefCanceled, f.countCmp(StatusRefCanceled), StatusRefCanceled)
		}
	}
	if f.setNote != "" {
		fmt.Fprintf(w, "설정: %s\n", f.setNote)
	}
	for _, n := range f.profNote {
		fmt.Fprintf(w, "프로파일 변경: %s\n", n)
	}
	dir := filepath.ToSlash(f.u.Check.Dir)
	fmt.Fprintf(w, "결과 폴더: %s/ (result.tsv, CSV %d개)\n", dir, len(f.csvs))
	for _, p := range f.csvs {
		fmt.Fprintf(w, "  %s\n", filepath.ToSlash(p))
	}
}
