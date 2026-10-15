package main

// unified_cmd.go — `biostool bios`: 수집 통합 엔진(unified.go) 명령 (단계 2). 표준출력이 터미널이고 -plain·-auto 가
// 아니면 TUI 흐름(tui_flow.go, 단계 6 — 마지막에 설정 여부를 묻는다)으로 가고, 그 밖에는 평문 출력이며 읽기 전용입니다
// — 설정(Pending 쓰기)은 하지 않습니다 (-auto 도 점검만).
//
//	biostool bios -profile VM [-user user.txt] [-ref host[,host] | -ref-json refs/x.json | -diff diff.txt] [-ignore ignore_attrs.txt] [-plain] [공통 옵션]
//	biostool bios -auto-check <user> <targets.txt> [-profile VM] [공통 옵션]   비대화형: 프로파일 점검만 → ./check.res_<user>
//	biostool bios -auto       <user> <targets.txt> [-profile VM] [공통 옵션]   위와 같음 + ./check.res_<user>_postapply (같은 내용)
//
// check.res_<user> 는 기본으로 현재 폴더에 쓰며, -res-dir <폴더> 로 바꿀 수 있습니다 (bios.sh 가 호출한 쪽 작업 폴더를 넘김).
// 비대화형은 묻지 않고, 점검을 끝냈으면 FAIL 이 있어도 종료코드 0 (auto_setup 이 결과 파일을 읽음).
// conf 오류·대상 없음·프로파일 없음 같은 치명적 오류만 0 이 아닌 종료코드입니다. 시작할 때 이전 check.res_<user>·
// _postapply 를 지우므로, 치명적 오류로 끝나면 결과 파일이 남지 않습니다 (파일은 임시 파일 + rename 으로 원자적으로 씀).

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// checkResPrefix 는 비대화형 결과 파일 이름 앞부분입니다 (현재 폴더에 check.res_<user>).
const checkResPrefix = "check.res_"

// validAutoUser 는 check.res_<user> 의 user 가 파일 이름으로 안전한지 봅니다.
func validAutoUser(u string) error {
	if u == "" || u == "." || u == ".." || unsafeFileChars.MatchString(u) {
		return fmt.Errorf("사용자 이름 %q 은(는) 결과 파일 이름(check.res_<user>)에 쓸 수 없습니다 (영문·숫자·._- 만)", u)
	}
	return nil
}

// resolveRefJSONPath 는 -ref-json 값을 경로로 바꿉니다: 그대로 없고 파일 이름만 줬으면 refs_dir 아래에서 찾습니다.
func resolveRefJSONPath(p, refsDir string) string {
	if _, err := os.Stat(p); err != nil && filepath.Base(p) == p {
		alt := filepath.Join(refsDir, p)
		if _, aerr := os.Stat(alt); aerr == nil {
			return alt
		}
	}
	return p
}

func cmdBios(args []string) error {
	fs, o := newFlagSet("bios")
	ref := fs.String("ref", "", "전체비교 비교서버 hostname (쉼표로 여러 개). 대상 목록에 있으면 다시 접속하지 않음")
	refJSON := fs.String("ref-json", "", "전체비교 기준 속성 JSON (bios_export / allcheck -export-attrs 결과, 파일 이름만 주면 refs_dir 에서 찾음)")
	diff := fs.String("diff", "", "전체비교 기준 목록 diff.txt (모델별 기준 hostname)")
	ignore := fs.String("ignore", "ignore_attrs.txt", "전체비교에서 제외할 속성 목록 (없으면 내장 기본 목록, bios.conf exclude_attrs 와 합침)")
	resDir := fs.String("res-dir", ".", "-auto / -auto-check 의 check.res_<user> 를 쓸 폴더 (bios.sh 래퍼가 호출한 쪽 작업 폴더를 넘김)")
	plain := fs.Bool("plain", false, "TUI 없이 색 없는 평문으로 출력 (설정 질문 없음)")
	autoCheck := fs.Bool("auto-check", false, "비대화형 점검: -auto-check <user> <targets.txt> → ./check.res_<user> (설정 없음)")
	auto := fs.Bool("auto", false, "비대화형 점검: -auto <user> <targets.txt> → ./check.res_<user> + _postapply (설정 없음, 점검만)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	autoUser := ""
	var resFiles []string // 비대화형이 쓸 check.res_<user> (+ _postapply)
	if *autoCheck || *auto {
		if *autoCheck && *auto {
			return fmt.Errorf("-auto 와 -auto-check 는 함께 쓸 수 없습니다")
		}
		rest := fs.Args()
		if len(rest) < 2 {
			return fmt.Errorf("사용법: biostool bios -auto-check|-auto <user> <targets.txt> [옵션]")
		}
		autoUser, o.user = rest[0], rest[1]
		if err := fs.Parse(rest[2:]); err != nil { // 위치 인자 뒤의 옵션도 받는다
			return err
		}
		if err := validAutoUser(autoUser); err != nil {
			return err
		}
		// 이후 치명적 오류로 끝나도 auto_setup 이 옛 OK 줄을 읽지 않게, 무엇보다 먼저 이전 결과 파일 둘 다 지운다.
		res := filepath.Join(*resDir, checkResPrefix+autoUser)
		if err := removeCheckRes([]string{res, res + "_postapply"}); err != nil {
			return err
		}
		resFiles = []string{res}
		if *auto {
			resFiles = append(resFiles, res+"_postapply")
		}
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("알 수 없는 인자: %s", strings.Join(fs.Args(), " "))
	}
	if o.dryRun || o.retryFrom != "" {
		return fmt.Errorf("bios 는 -dry-run, -retry-from 을 지원하지 않습니다 (check 를 쓰십시오)")
	}
	cref := compareRef{JSONPath: *refJSON, DiffFile: *diff}
	if *ref != "" {
		cref.Hosts = []string{*ref}
	}
	if cref.count() > 1 {
		return fmt.Errorf("-ref, -ref-json, -diff 는 하나만 쓸 수 있습니다")
	}
	if autoUser != "" && cref.count() > 0 {
		return fmt.Errorf("-auto / -auto-check 는 프로파일 점검만 합니다 (-ref, -ref-json, -diff 를 쓰지 마십시오)")
	}
	// 대화형(표준출력이 터미널, -plain 아님, 비대화형 아님)은 TUI 흐름(tui_flow.go): 프로파일은 그 안에서 고를 수 있다.
	interactive := autoUser == "" && !*plain && ttyIsTerminal(int(os.Stdout.Fd()))
	if autoUser == "" && (!interactive || o.profile != "") {
		if err := validProfileName(o.profile); err != nil {
			return err
		}
	}
	explicitIgnore, explicitUser := false, false
	fs.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "ignore":
			explicitIgnore = true
		case "user":
			explicitUser = true
		}
	})
	ign, err := loadIgnore(*ignore, explicitIgnore)
	if err != nil {
		return err
	}
	if interactive {
		if _, err := loadConf(o.conf); err != nil { // 대상을 입력받기 전에 conf 오류를 알린다
			return err
		}
		if ui, err := tuiOpenTTY(); err == nil {
			return runBiosTUI(ui, biosTUIArgs{O: o, UserSet: explicitUser, Ref: cref, Ignore: ign}, os.Stdout)
		}
		if err := validProfileName(o.profile); err != nil { // TUI 를 못 열면 아래 평문 흐름 (프로파일 필요)
			return err
		}
	}

	stages := 3 // 설정 확인 · 접속·조회 · CSV 저장
	if cref.count() > 0 {
		stages = 4 // + 기준 조회
	}
	var p *progress
	if autoUser != "" {
		p = newProgress(os.Stderr, false, stages) // 비대화형은 10초마다 한 줄
	} else {
		p = newStderrProgress(stages)
	}
	p.Start()
	defer p.Stop()
	p.Stage(1, "설정 확인", 0)

	rc, err := prepare(o, "", "")
	if err != nil {
		return err
	}
	profName := o.profile
	if profName == "" {
		profName = rc.Conf.AutoProfile
	}
	if err := validProfileName(profName); err != nil {
		return fmt.Errorf("%v (비대화형은 -profile 또는 bios.conf auto_profile)", err)
	}
	profilePath := filepath.Join(rc.Conf.ProfileDir, profName+".tsv")
	prof, err := loadProfile(profilePath, profName)
	if err != nil {
		return err
	}
	// 기준 파일은 접속 전에 확인한다 (오래 수집한 뒤에 경로 오류로 끝나지 않게).
	if cref.JSONPath != "" {
		cref.JSONPath = resolveRefJSONPath(cref.JSONPath, rc.Conf.RefsDir)
		if _, err := readRefJSON(cref.JSONPath); err != nil {
			return err
		}
	}
	if cref.DiffFile != "" {
		if _, err := loadRefList(cref.DiffFile); err != nil {
			return err
		}
	}

	var warns []string
	u, err := runUnified(rc, unifiedOpts{
		Profile: prof, FromDump: o.fromDump, ResultDir: rc.Conf.ResultDir, HostsPath: o.hosts, Ignore: ign,
		Collect: progStage{P: p, Idx: 2, Name: "접속·조회"},
	})
	if u == nil {
		return err
	}
	if err != nil {
		warns = append(warns, "결과 파일 기록 중 오류: "+err.Error())
	}
	plines := u.ProfileLines()
	var cmp *compareResult
	var cmpErr error // 전체비교 실패는 프로파일 결과를 보여 준 뒤 오류로 끝낸다
	if cref.count() > 0 {
		if cmp, cmpErr = u.Compare(cref, progStage{P: p, Idx: 3, Name: "기준 조회"}); cmpErr != nil {
			cmpErr = fmt.Errorf("전체비교 실패: %w", cmpErr)
		}
	}

	p.Stage(stages, "CSV 저장", 0)
	var csvs []string
	paths, werr := u.WriteOutputs("profile_", plines)
	csvs = append(csvs, paths...)
	if werr != nil {
		warns = append(warns, werr.Error())
	}
	if cmp != nil {
		paths, werr = u.WriteOutputs("compare_", cmp.Lines)
		csvs = append(csvs, paths...)
		if werr != nil {
			warns = append(warns, werr.Error())
		}
	}
	for _, f := range resFiles {
		if err := writeCheckRes(f, plines, u.HostOrder()); err != nil {
			p.Stop()
			return fmt.Errorf("%s 기록 실패: %w", f, err)
		}
	}
	p.Stop()

	for _, w := range warns {
		fmt.Fprintln(os.Stderr, "경고: "+w)
	}
	dir := filepath.ToSlash(u.Check.Dir)
	if autoUser != "" {
		fmt.Printf("프로파일 %s, 대상 %d대 점검 완료 (설정 없음)\n", profName, len(rc.Targets))
		for _, f := range resFiles {
			fmt.Printf("결과: %s\n", f)
		}
		fmt.Printf("결과 폴더: %s/ (result.tsv, CSV %d개)\n", dir, len(csvs))
		return nil
	}

	color := !*plain && colorEnabled(os.Stdout)
	fmt.Printf("== 프로파일 %s 점검 (대상 %d대) ==\n", profName, len(rc.Targets))
	writeResultLines(os.Stdout, plines, color)
	if cmp != nil {
		fmt.Printf("\n== 전체비교 (기준: %s, 제외 속성: %s) ==\n", cmp.Label, cmp.Ignore.Source)
		writeResultLines(os.Stdout, cmp.Lines, color)
		for _, h := range cmp.Hosts {
			var notes []string
			for _, n := range h.Notes {
				notes = append(notes, n.String())
			}
			if h.compared() && !h.sameAttrNames() {
				notes = append(notes, "속성 이름 목록이 기준과 다름 (설정 불가)")
			}
			if !h.compared() && h.Detail != "" {
				notes = append(notes, h.Detail)
			}
			if len(notes) > 0 {
				fmt.Printf("특이사항 %s: %s\n", lineSafe(h.Target.Hostname), lineSafe(strings.Join(notes, "; "))) // Detail 은 BMC 응답에서 옴
			}
		}
	}
	fmt.Printf("\n결과 폴더: %s/ (result.tsv, CSV %d개)\n", dir, len(csvs))
	return cmpErr
}
