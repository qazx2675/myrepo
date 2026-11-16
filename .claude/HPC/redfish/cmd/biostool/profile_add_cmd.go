package main

// profile_add_cmd.go — `biostool profile add` (bash bios.sh profile add ...): 프로파일에 태그·모델별 설정 행을 더하는 명령.
// 핵심 로직은 profile_add.go 와 같고, 화면(p 키)은 tui_profadd.go 입니다.
//
//	biostool profile add -profile VM -tag gold  -set "LogicalProc=Enabled" [-set "hyper_threading:LogicalProc=Enabled" ...] [-user user.txt]
//	biostool profile add -profile VM -model "DELL/R660" -set "LogicalProc=Enabled"
//
// -model 만 주면 BMC 에 접속하지 않습니다 (속성 이름은 입력 그대로). -tag 는 그 태그 서버들의 모델을 알아야 하므로 대상에 읽기 전용으로
// 1회 접속합니다 (-from-dump 면 덤프에서 읽음). 충돌(같은 행이 다른 값으로 있음)이 있으면 -force 없이는 아무것도 쓰지 않고 오류로 끝납니다.

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const profileUsageText = `사용법:
  biostool profile add -profile <이름> (-tag <태그> | -model "벤더/모델") -set "속성=값" [-set "std_name:속성=값" ...]
                       [-user user.txt] [-conf bios.conf] [-hosts /etc/hosts] [-from-dump 디렉터리] [-force] [-dry-run]

  프로파일 profile_dir/<이름>.tsv 의 [redfish] 구역 끝에 모델 지정 행을 더합니다 (verified 는 항상 N, 보호 속성은 거절).
  -tag    그 태그(user.txt 둘째 칸)가 붙은 서버가 보고한 모델마다 같은 행을 만듭니다. 대상에 읽기 전용으로 1회 접속합니다.
  -model  그 모델 1종에만 행을 만듭니다 (접속하지 않음, 속성 이름은 입력 그대로). 예) -model "DELL/R660"
  -set    "속성=값" 또는 "std_name:속성=값" (여러 번). 값에는 A|B (허용값 여러 개) 를 쓸 수 있고, std_name 을 생략하면 표준 항목 이름
          (hyper_threading 등)이나 속성 이름의 소문자·밑줄 변환으로 정합니다.
  -force  같은 행이 다른 값으로 이미 있으면 새 값으로 덮어씀 (verified 는 N 으로). 없으면 충돌 시 아무것도 쓰지 않고 오류로 끝납니다.
  -dry-run 미리보기만 하고 쓰지 않습니다.
  쓰기 전에 <이름>.tsv.bak.<일시> 로 백업하고, 결과를 다시 읽어 검증한 뒤 임시 파일 + rename 으로 바꿉니다.
`

// profAddFlags 는 반복해서 쓰는 -set 플래그입니다.
type profAddFlags []string

func (f *profAddFlags) String() string     { return strings.Join(*f, ", ") }
func (f *profAddFlags) Set(v string) error { *f = append(*f, v); return nil }

// cmdProfile 은 `biostool profile <하위 명령>` 입니다 (지금은 add 만).
func cmdProfile(args []string) error {
	if len(args) == 0 {
		fmt.Fprint(os.Stderr, profileUsageText)
		return fmt.Errorf("profile 하위 명령이 필요합니다 (add)")
	}
	switch args[0] {
	case "add":
		return cmdProfileAdd(args[1:], os.Stdout)
	case "-h", "--help", "help":
		fmt.Print(profileUsageText)
		return nil
	}
	return fmt.Errorf("알 수 없는 profile 하위 명령 %q (add)", args[0])
}

// cmdProfileAdd 는 `profile add` 입니다. 출력은 out 으로 합니다 (시험이 읽음).
func cmdProfileAdd(args []string, out io.Writer) error {
	fs, o := newFlagSet("profile add")
	tag := fs.String("tag", "", "행을 만들 태그 (그 태그 서버들의 모델마다)")
	model := fs.String("model", "", `행을 만들 모델 "벤더/모델"`)
	var setFlags profAddFlags
	fs.Var(&setFlags, "set", `설정 "속성=값" 또는 "std_name:속성=값" (여러 번)`)
	force := fs.Bool("force", false, "같은 행이 다른 값으로 있으면 덮어씀")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("알 수 없는 인자 %q — 옵션은 -tag/-model/-set 처럼 - 로 시작합니다 (biostool profile add -h)", fs.Arg(0))
	}
	if err := validProfileName(o.profile); err != nil {
		return err
	}
	if o.retryFrom != "" {
		return fmt.Errorf("-retry-from 은 profile add 에서 쓸 수 없습니다")
	}
	if (*tag == "") == (*model == "") {
		return fmt.Errorf("-tag 와 -model 중 정확히 하나를 주십시오")
	}
	if len(setFlags) == 0 {
		return fmt.Errorf(`-set "속성=값" 이 하나 이상 필요합니다`)
	}
	var sets []profAddSet
	var bad []string
	for _, s := range setFlags {
		set, err := parseProfAddFlag(s)
		if err != nil {
			bad = append(bad, err.Error())
			continue
		}
		sets = append(sets, set)
	}
	if len(bad) > 0 {
		return fmt.Errorf("%s", strings.Join(bad, "\n"))
	}

	conf, err := loadConf(o.conf)
	if err != nil {
		return err
	}
	if fi, err := os.Stat(conf.ProfileDir); err != nil || !fi.IsDir() {
		return fmt.Errorf("프로파일 폴더 %s 을(를) 찾을 수 없습니다 (bios.conf profile_dir) — 프로파일에 추가할 수 없습니다", conf.ProfileDir)
	}
	path := filepath.Join(conf.ProfileDir, o.profile+".tsv")
	prof, err := loadProfile(path, o.profile)
	if err != nil {
		return err
	}

	var models []*profAddModel
	var scope string
	if *model != "" {
		i := strings.Index(*model, "/")
		if i <= 0 || strings.TrimSpace((*model)[i+1:]) == "" {
			return fmt.Errorf(`-model 은 "벤더/모델" 형식이어야 합니다 (예: "DELL/R660")`)
		}
		m := profAddModelFromSpec((*model)[:i], (*model)[i+1:], nil)
		models, scope = []*profAddModel{m}, "모델 "+m.label()+" (접속하지 않음 — 속성 이름은 입력 그대로)"
	} else {
		snaps, err := profAddReadSnaps(o, prof, out)
		if err != nil {
			return err
		}
		ms, matched, unreadable := profAddModelsForTag(snaps, *tag)
		switch {
		case matched == 0:
			return fmt.Errorf("태그 %q 인 대상이 없습니다%s — 아무것도 쓰지 않았습니다", *tag, profAddTagList(snaps))
		case len(ms) == 0:
			return fmt.Errorf("태그 %q 서버 %d대를 모두 읽지 못해 모델을 알 수 없습니다 — 아무것도 쓰지 않았습니다", *tag, matched)
		}
		models = ms
		scope = fmt.Sprintf("태그 %s (서버 %d대, 모델 %d종", *tag, matched, len(ms))
		if unreadable > 0 {
			scope += fmt.Sprintf(", 읽지 못한 %d대 제외", unreadable)
		}
		scope += ")"
	}

	plan := buildProfileAdd(prof, models, sets, protectFor(conf))
	fmt.Fprintf(out, "범위: %s\n", scope)
	for _, l := range plan.Lines() {
		fmt.Fprintln(out, l)
	}
	fmt.Fprintln(out)

	switch {
	case len(plan.Rejected) > 0:
		return fmt.Errorf("받을 수 없는 설정이 %d건 있어 아무것도 쓰지 않았습니다 (위 [입력 거절])", len(plan.Rejected))
	case len(plan.Conflicts) > 0 && !*force:
		return fmt.Errorf("같은 행이 다른 값으로 이미 있는 충돌이 %d건 있어 아무것도 쓰지 않았습니다 — 새 값으로 덮어쓰려면 -force", len(plan.Conflicts))
	case len(plan.Add) == 0 && len(plan.Conflicts) == 0:
		if len(plan.Same) > 0 {
			fmt.Fprintln(out, "추가할 행이 없습니다 — 모두 이미 있는 행입니다")
			return nil
		}
		return fmt.Errorf("추가할 수 있는 행이 없습니다 (위 [건너뜀] 사유 확인) — 아무것도 쓰지 않았습니다")
	case o.dryRun:
		fmt.Fprintf(out, "-dry-run: 프로파일 %s 을(를) 바꾸지 않았습니다\n", path)
		return nil
	}

	res, err := profAddWrite(path, plan, *force, time.Now())
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "프로파일 %s 에 %d행 추가", path, res.Added)
	if res.Replaced > 0 {
		fmt.Fprintf(out, ", %d행 덮어씀", res.Replaced)
	}
	fmt.Fprintf(out, " (verified=N, 백업 %s)\n", res.Backup)
	return nil
}

// profAddReadSnaps 는 -tag 로 모델을 알기 위해 대상을 읽기 전용으로 1회 조회합니다 (check 와 같은 경로, -from-dump 면 덤프).
// 결과 파일은 임시 폴더에 두었다가 지웁니다.
func profAddReadSnaps(o *commonOpts, prof *Profile, out io.Writer) ([]*biosSnap, error) {
	rc, err := prepare(o, "", "")
	if err != nil {
		return nil, err
	}
	tmp, err := os.MkdirTemp("", "biostool-profadd-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)
	if o.fromDump != "" {
		fmt.Fprintf(out, "대상 %d대를 덤프 %s 에서 읽습니다 (접속 안 함)\n", len(rc.Targets), o.fromDump)
	} else {
		fmt.Fprintf(out, "대상 %d대에 읽기 전용으로 접속해 모델을 확인합니다 (설정은 하지 않음)\n", len(rc.Targets))
	}
	u, err := runUnified(rc, unifiedOpts{Profile: prof, FromDump: o.fromDump, ResultDir: tmp, HostsPath: o.hosts})
	if u == nil {
		return nil, err
	}
	return u.Snaps, nil
}

// profAddTagList 는 대상의 태그 목록을 " (있는 태그: a, b)" 로 돌려줍니다 (없으면 "").
func profAddTagList(snaps []*biosSnap) string {
	set := map[string]bool{}
	for _, s := range snaps {
		if s != nil && s.Target.Tag != "" {
			set[s.Target.Tag] = true
		}
	}
	if len(set) == 0 {
		return " (대상에 태그가 하나도 없음 — user.txt 둘째 칸)"
	}
	tags := make([]string, 0, len(set))
	for t := range set {
		tags = append(tags, t)
	}
	sort.Strings(tags)
	return " (있는 태그: " + strings.Join(tags, ", ") + ")"
}
