// biostool 은 관리망 BMC 에 Redfish 로 접속해 BIOS 표준값(프로파일별)을 점검하고,
// 승인된 FAIL 항목만 Pending 으로 설정하는 도구입니다. 재부팅은 하지 않습니다.
//
// 사용법:
//
//	biostool check   -profile VM [-conf bios.conf] [-user user.txt] [-from-dump 디렉터리] [-no-prompt] [-stdin-ok] [-list-max N] [-fail-max N] [-dry-run] [-hosts /etc/hosts] [-retry-from 이전결과폴더]
//	biostool allcheck [-diff diff.txt] [-ignore ignore_attrs.txt] [-conf bios.conf] [-user user.txt] [-from-dump 디렉터리] [-list-max N] [-diff-max N] [-hosts /etc/hosts] [-retry-from 이전결과폴더]
//	biostool bios    -profile VM [-ref 호스트 | -ref-json refs/x.json | -diff diff.txt] [-ignore ignore_attrs.txt] [-plain] [공통 옵션]
//	biostool bios    -auto-check|-auto <user> <targets.txt> [-profile VM] [공통 옵션]
//	biostool dump    [-targets 대상,대상...] [-conf bios.conf] [-user user.txt] [-hosts /etc/hosts] [-out dumps] [-compact]
//	printf '%s' "$pw" | biostool encrypt [-key key.bin] [-out pass.enc]
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// notImplementedError 는 아직 구현되지 않은 서브커맨드를 나타냅니다 (종료코드 2).
type notImplementedError struct {
	cmd   string
	stage int
}

func (e notImplementedError) Error() string {
	return fmt.Sprintf("%s 은(는) 아직 구현되지 않았습니다 (단계 %d)", e.cmd, e.stage)
}

const usageText = `사용법:
  biostool check   -profile <이름> [공통 옵션]    BIOS 표준값 점검
  biostool allcheck [-diff diff.txt] [옵션]       모델별 BIOS 전체 Attribute 비교 (읽기 전용)
  biostool bios    -profile <이름> [-ref 호스트 | -ref-json 파일 | -diff diff.txt] [-user-xml 파일] [-plain] [옵션]
                                                 호스트당 1회 조회로 프로파일 점검 + 전체비교 (읽기 전용)
                                                 xml_host_prefix(기본 c) 이름·-user-xml 대상은 XML API 점검만 (전체비교·설정 제외)
  biostool bios    -auto-check|-auto <user> <targets.txt> [-profile <이름>]
                                                 비대화형 점검만 → ./check.res_<user> (-auto 는 _postapply 도, 설정 없음)
  biostool dump    [-targets 대상,...] [-out 디렉터리] [-compact] [공통 옵션]  사전조사 덤프 (GET 전용)
  biostool ucsm    [-profile <이름>] [-list-max N] [-conf] [-user] [-hosts]  Cisco UCSM XML API BIOS 점검 (읽기 전용, 대상=UCSM 주소, 기대값=프로파일 # [xml] 구역, 기본 VM)
  biostool encrypt [-key key.bin] [-out pass.enc]  표준입력의 비밀번호를 암호화
  biostool account create|change [옵션]          1순위 계정 생성 / 비밀번호 변경 (biostool account -h)
  biostool trs     [-targets 대상,...] [-path 경로,...] [공통 옵션]  BMC 접속 장애(403 등) 단계별 진단 (읽기 전용, 인증 시도 최소)

공통 옵션:
  -conf <파일>        설정 파일 (기본 bios.conf)
  -user <파일>        대상 목록 파일 (기본 user.txt). BMC 계정 ID 는 conf 의 user
  -user-xml <파일>    (bios·check·allcheck) Cisco(XML) 대상 목록. hostname 이 xml_host_prefix(기본 c)로 시작하는 대상과 함께
                      XML API 로 점검만 (설정·전체비교 제외, allcheck 는 접속하지 않고 XML_READONLY)
  -profile <이름>     프로파일 이름 (profile_dir/<이름>.tsv)
  -from-dump <디렉터리> 접속 대신 덤프에서 읽기
  -retry-from <폴더경로> 이전 결과 폴더 경로 또는 그 안의 retry.txt 경로 (재시도 모드, -user 와 함께 쓸 수 없음)
  -dry-run            (check) 설정 없이 호스트별로 보낼 PATCH 경로·본문만 출력 (미검증 모델은 시험 출력, Y/N 묻지 않음)
  -no-prompt          (check) FAIL 이 있어도 설정 여부를 묻지 않고 종료
  -stdin-ok           (check) 자동화용: 표준입력이 터미널이 아니어도(파이프·파일) Y/N 을 읽음. 없으면 비대화형 입력은 N 처리
  -list-max <N>       (check) [OK] 호스트 이름을 나열할 최대 대수 (기본 100, 넘으면 개수와 ok.txt 경로만)
  -fail-max <N>       (check) [FAIL 상세]·[설정 불가] 에 보여 줄 최대 줄 수 (기본 200, 넘으면 fail.tsv 안내)
  -diff <파일>        (allcheck) 기준(정상 설정값) 호스트 목록 (기본 diff.txt, 한 줄에 hostname)
  -ignore <파일>      (allcheck) 비교에서 제외할 속성 목록 (기본 ignore_attrs.txt, 없으면 내장 기본 목록)
  -diff-max <N>       (allcheck) 호스트당 터미널에 보일 차이 속성 수 (기본 30, 넘으면 all_diff.tsv 안내)
  -list-max <N>       (allcheck) 차이 상세·특이사항·오류에 나열할 호스트 수 (기본 20)
  -hosts <파일>       이름 해석에 쓸 hosts 파일 (기본 /etc/hosts)
  -rf-conf <파일>     2순위(예비) 계정 파일 (기본: 환경변수 RF_TOOL_CONF, 없으면 /etc/rf_tool/rf.conf; 파일이 없으면 기능 꺼짐)
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usageText)
		os.Exit(1)
	}
	var err error
	switch os.Args[1] {
	case "check":
		err = cmdCheck(os.Args[2:])
	case "allcheck":
		err = cmdAllCheck(os.Args[2:])
	case "bios":
		err = cmdBios(os.Args[2:])
	case "dump":
		err = cmdDump(os.Args[2:])
	case "encrypt":
		err = cmdEncrypt(os.Args[2:])
	case "ucsm":
		err = cmdUcsm(os.Args[2:])
	case "account":
		err = cmdAccount(os.Args[2:])
	case "trs":
		err = cmdTrs(os.Args[2:])
	case "-h", "--help", "help":
		fmt.Print(usageText)
		return
	default:
		fmt.Fprintf(os.Stderr, "알 수 없는 서브커맨드: %s\n\n%s", os.Args[1], usageText)
		os.Exit(1)
	}
	if err == nil || errors.Is(err, flag.ErrHelp) {
		return
	}
	var ni notImplementedError
	if errors.As(err, &ni) {
		fmt.Fprintln(os.Stderr, ni.Error())
		os.Exit(2)
	}
	fmt.Fprintln(os.Stderr, "오류: "+err.Error())
	os.Exit(1)
}

// commonOpts 는 check/dump 가 공유하는 플래그입니다.
type commonOpts struct {
	conf      string
	user      string // 대상 목록 파일 (BMC 계정 ID 가 아님)
	profile   string
	fromDump  string
	retryFrom string // 이전 결과 폴더 (단계 7 재시도)
	hosts     string
	dryRun    bool
	rfConf    string // 2순위 계정 파일 (비면 RF_TOOL_CONF → /etc/rf_tool/rf.conf)
	// routeXML 이 true 면 prepare 가 classifyTargets 로 XML 대상(xml_host_prefix, -user-xml)을 나눈다 (bios·check·allcheck 가 켬).
	routeXML bool
	userXML  string // -user-xml: 모두 XML 로 점검할 대상 목록 파일 (bios 만, 비면 없음)
	// userXMLArg 는 TUI 에서 붙여넣은 Cisco(XML) 대상입니다 (비어 있지 않으면 userXML 파일 대신 씀).
	userXMLArg string
}

func newFlagSet(name string) (*flag.FlagSet, *commonOpts) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	o := &commonOpts{}
	fs.StringVar(&o.conf, "conf", "bios.conf", "설정 파일")
	fs.StringVar(&o.user, "user", "user.txt", "대상 목록 파일 (BMC 계정 ID 는 conf 의 user)")
	fs.StringVar(&o.profile, "profile", "", "프로파일 이름 (profile_dir/<이름>.tsv)")
	fs.StringVar(&o.fromDump, "from-dump", "", "접속 대신 읽을 덤프 디렉터리")
	fs.StringVar(&o.retryFrom, "retry-from", "", "이전 결과 폴더 경로 또는 retry.txt 경로 (재시도 모드)")
	fs.StringVar(&o.hosts, "hosts", "/etc/hosts", "이름 해석에 쓸 hosts 파일")
	fs.BoolVar(&o.dryRun, "dry-run", false, "설정하지 않고 보낼 요청만 출력")
	fs.StringVar(&o.rfConf, "rf-conf", "", "2순위(예비) 계정 파일 (기본: 환경변수 "+envRFConf+", 없으면 "+defaultRFConf+"; 파일이 없으면 기능 꺼짐)")
	return fs, o
}

// userXMLUsage 는 -user-xml 옵션 설명입니다 (bios·check·allcheck 공통).
const userXMLUsage = "XML API(UCSM/CIMC, 읽기 전용)로만 점검할 Cisco 대상 목록 파일 (hostname·IP, 전체비교·설정 제외). Cisco 장비는 XML 선택: hostname 이 xml_host_prefix(기본 c)로 시작하면 자동, IP 는 이 파일에"

// addXMLRouting 은 -user-xml 을 붙이고 대상을 Redfish / XML 로 나누게 합니다 (classifyTargets, bios 와 같은 규칙).
func addXMLRouting(fs *flag.FlagSet, o *commonOpts) {
	fs.StringVar(&o.userXML, "user-xml", "", userXMLUsage)
	o.routeXML = true
}

// userXMLOnly 는 -user-xml 만 주고 -user·-retry-from 은 주지 않았으면 기본 user.txt 를 읽지 않게 합니다 (Parse 뒤에 부름).
func userXMLOnly(fs *flag.FlagSet, o *commonOpts) {
	if o.userXML == "" || o.retryFrom != "" {
		return
	}
	explicit := false
	fs.Visit(func(f *flag.Flag) { explicit = explicit || f.Name == "user" })
	if !explicit {
		o.user = ""
	}
}

func cmdCheck(args []string) error {
	fs, o := newFlagSet("check")
	noPrompt := fs.Bool("no-prompt", false, "FAIL 이 있어도 설정 여부를 묻지 않고 종료")
	stdinOK := fs.Bool("stdin-ok", false, "자동화용: 표준입력이 터미널이 아니어도 Y/N 을 읽음")
	listMax := fs.Int("list-max", 100, "[OK] 호스트 이름을 나열할 최대 대수")
	failMax := fs.Int("fail-max", 200, "[FAIL 상세]·[설정 불가] 에 보여 줄 최대 줄 수")
	addXMLRouting(fs, o) // xml_host_prefix 이름·-user-xml 대상은 XML API 로 점검만 (설정 제외)
	if err := fs.Parse(args); err != nil {
		return err
	}
	userXMLOnly(fs, o)
	if err := validProfileName(o.profile); err != nil {
		return err
	}
	if *listMax < 0 || *failMax < 0 {
		return fmt.Errorf("-list-max, -fail-max 는 0 이상이어야 합니다")
	}

	if *stdinOK {
		fmt.Fprintln(os.Stderr, "경고: -stdin-ok — 표준입력이 터미널이 아니어도 Y/N 을 읽습니다 (자동화 전용, 미리 넣은 Y 로도 실제 설정됨)")
	}

	if proceed, err := retryPreflight(fs, o, os.Stdout); err != nil || !proceed {
		return err
	}

	rc, err := prepare(o, "", o.retryFrom)
	if err != nil {
		return err
	}
	profilePath := filepath.Join(rc.Conf.ProfileDir, o.profile+".tsv")
	prof, err := loadProfile(profilePath, o.profile)
	if err != nil {
		return err
	}
	printSummary(rc, o, profilePath)

	run, err := doCheck(rc, checkOpts{Profile: prof, FromDump: o.fromDump, ResultDir: rc.Conf.ResultDir, RetryFromDir: o.retryFrom})
	if run == nil {
		return err
	}
	ropts := reportOpts{ListMax: *listMax, FailMax: *failMax, Color: useColor()}
	fmt.Println()
	writeReport(os.Stdout, run, ropts)
	if run.Retry != nil {
		// 재시도 병합 블록은 dry-run·설정(Y/N) 출력까지 끝난 터미널 맨 끝에 둔다.
		defer writeRetryMergeSection(os.Stdout, run.Retry, run.Dir, painter{ropts.Color})
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "경고: 결과 파일 기록 중 오류: "+err.Error())
	}
	// 1순위 계정이 없어 2순위로 접속한 호스트가 있으면 계정 생성을 제안한다 (기본 Y, 터미널일 때만 묻는다).
	if aerr := offerAccountCreate(rc, run, o.dryRun, *noPrompt, *stdinOK, os.Stdin, os.Stdout, ropts); aerr != nil {
		fmt.Fprintln(os.Stderr, "경고: 계정 생성 단계 오류: "+aerr.Error())
	}
	if o.dryRun {
		// dry-run: 보낼 요청만 출력하고 Y/N 을 묻지 않는다 (쓰기 없음).
		if derr := doDryRun(rc, run, os.Stdout); derr != nil {
			return derr
		}
		return err
	}
	if aerr := confirmApplyStdin(rc, run, os.Stdin, *stdinOK, os.Stdout, *noPrompt, ropts); aerr != nil {
		return aerr
	}
	return err
}

func cmdEncrypt(args []string) error {
	fs := flag.NewFlagSet("encrypt", flag.ContinueOnError)
	key := fs.String("key", "key.bin", "AES-256 키 파일 (없으면 생성)")
	out := fs.String("out", "pass.enc", "암호문 출력 경로")
	if err := fs.Parse(args); err != nil {
		return err
	}
	// 비밀번호는 인자로 받지 않고 표준입력으로만 받는다 (ps/히스토리 노출 방지).
	if fi, err := os.Stdin.Stat(); err == nil && fi.Mode()&os.ModeCharDevice != 0 {
		return fmt.Errorf("표준입력이 터미널/장치입니다. 비밀번호는 파이프로 전달해야 합니다 (encrypt.sh 사용)")
	}
	if err := encryptFromReader(os.Stdin, *out, *key); err != nil {
		return err
	}
	fmt.Printf("암호화 완료: %s (키: %s)\n", *out, *key)
	return nil
}

// runContext 는 check/dump 가 공통으로 준비하는 실행 상태입니다.
type runContext struct {
	Conf     *Config
	Targets  []Target
	Password string         // 메모리에서만 사용. 출력 금지.
	Fallback *fallbackCreds // rf.conf 의 2순위 계정 (nil 이면 기능 꺼짐). 출력 금지.
}

// prepareWarnOut 은 prepare 의 경고·알림(rf.conf) 출력 곳입니다. TUI 가 열려 있는 동안에는 흐름(tui_flow.go)이
// 버퍼로 바꿔 두었다가 TUI 를 닫은 뒤 표준에러에 찍는다.
var prepareWarnOut io.Writer = os.Stderr

// prepare 는 conf 로드 → 대상 해석 → 비밀번호 복호화를 수행합니다.
// targetsArg 가 비어 있지 않으면 대상 목록 파일 대신 그 값을 씁니다.
// retryFromPath 가 비어 있지 않으면 그 폴더의 retry.txt 에서 대상을 읽습니다.
// -from-dump 일 때는 접속하지 않으므로 비밀번호를 복호화하지 않습니다.
func prepare(o *commonOpts, targetsArg string, retryFromPath string) (*runContext, error) {
	conf, err := loadConf(o.conf)
	if err != nil {
		return nil, err
	}
	var inputs []string
	if targetsArg != "" {
		inputs = splitTargets(targetsArg)
	} else if retryFromPath != "" {
		inputs, err = loadRetryList(retryFromPath)
		if err != nil {
			return nil, err
		}
	} else if o.user != "" || o.userXML == "" {
		if inputs, err = loadList(o.user); err != nil {
			return nil, err
		}
	}
	var xmlInputs []string
	if o.routeXML && o.userXMLArg != "" {
		xmlInputs = splitTargets(o.userXMLArg)
	} else if o.routeXML && o.userXML != "" {
		if xmlInputs, err = loadList(o.userXML); err != nil {
			return nil, err
		}
	}
	if len(inputs)+len(xmlInputs) == 0 {
		return nil, fmt.Errorf("대상이 비어 있습니다")
	}
	var targets []Target
	if o.routeXML {
		targets, err = classifyTargets(conf, inputs, xmlInputs, o.hosts)
	} else {
		targets, err = resolveTargets(inputs, o.hosts)
	}
	if err != nil {
		return nil, err
	}
	rc := &runContext{Conf: conf, Targets: targets}
	if o.fromDump == "" {
		if rc.Password, err = readPassword(conf.PassFile, conf.KeyFile); err != nil {
			return nil, err
		}
		path, explicit := rfConfPath(o.rfConf)
		fb, warn, err := loadRFConf(path)
		if err != nil {
			return nil, err
		}
		if warn != "" {
			fmt.Fprintln(prepareWarnOut, "경고: "+warn)
		}
		if fb == nil && explicit {
			fmt.Fprintf(prepareWarnOut, "알림: 2순위 계정 파일 %s 이(가) 없어 2순위 계정 기능을 쓰지 않습니다\n", path)
		}
		rc.Fallback = fb
	}
	return rc, nil
}

// printSummary 는 해석된 설정·대상 요약을 출력합니다 (비밀번호는 출력하지 않는다).
func printSummary(rc *runContext, o *commonOpts, profilePath string) {
	c := rc.Conf
	fmt.Printf("설정: %s (user=%s, concurrency=%d, timeout=%s, insecure=%t, retries=%d, auth_fail_stop=%d)\n",
		o.conf, c.User, c.Concurrency, c.Timeout, c.Insecure, c.Retries, c.AuthFailStop)

	var noEntry []string
	for _, t := range rc.Targets {
		if t.Err == StatusNoHostsEntry {
			noEntry = append(noEntry, t.Input)
		}
	}
	fmt.Printf("대상: %d개 중 해석 %d개, %s %d개\n",
		len(rc.Targets), len(rc.Targets)-len(noEntry), StatusNoHostsEntry, len(noEntry))
	for i, in := range noEntry {
		if i == 10 {
			fmt.Printf("  ... 외 %d개\n", len(noEntry)-10)
			break
		}
		fmt.Printf("  %s\n", in)
	}

	if profilePath != "" {
		fmt.Printf("프로파일: %s (%s)\n", o.profile, profilePath)
	}
	switch {
	case o.fromDump != "":
		fmt.Printf("모드: 덤프 읽기 (%s), 접속 안 함\n", o.fromDump)
	case o.dryRun:
		fmt.Printf("모드: dry-run (비밀번호 복호화 성공: %s)\n", c.PassFile)
	default:
		fmt.Printf("모드: 실제 접속 (비밀번호 복호화 성공: %s)\n", c.PassFile)
	}
}
