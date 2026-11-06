package main

// new_setup -run 진입: 상태 폴더(./<호스트파일>_run/) 유무와 워커 생존에 따라 새로 시작 / 다시 붙기 / 이어서 진행 / 결과 보기를 고른다.
// 사용자 선택(USER_ROUTE 메뉴)은 TUI 에 들어가기 전에 일반 터미널 모드에서 한다.
// 외부 동작은 runDeps 로 주입해 사례 선택을 tty·워커 없이 시험한다.

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// runDeps 는 -run 진입이 쓰는 외부 동작.
type runDeps struct {
	alive    func(dir string) bool                              // 워커 생존
	start    func(dir string) (int, error)                      // 워커 기동
	pickUser func(conf config, flagUser string) (string, error) // 사용자 선택
	confirm  func(prompt string) bool                           // y/N 질문
	show     func(dir string)                                   // 화면(TUI 또는 글) 보이기
	tty      bool                                               // 대화형 터미널인지(안내 문구를 낼지)
	fresh    bool                                               // -new: 이전 상태 폴더가 있으면 보관(이름 바꿈)하고 처음부터
	out, err io.Writer                                          // 안내(표준 출력, 표준 오류)
}

// runCommand 는 `new_setup -run` 의 실제 진입점이다. 종료 코드를 돌려준다.
func runCommand(hostArg, confFlag, errFlag, userFlag string, fresh bool) int {
	tty := isTerminal(int(os.Stdin.Fd())) && isTerminal(int(os.Stdout.Fd()))
	d := runDeps{
		alive:    workerAlive,
		start:    startWorkerWait,
		pickUser: selectUser,
		confirm:  askYesNo,
		tty:      tty,
		fresh:    fresh,
		out:      os.Stdout,
		err:      os.Stderr,
	}
	d.show = func(dir string) {
		if tty {
			err := runTUI(dir)
			if err == nil {
				return
			}
			fmt.Fprintf(os.Stderr, "경고: TUI 를 시작할 수 없어 현재 상태를 글로 냅니다: %v\n", err)
		}
		printPlain(os.Stdout, dir, workerAlive)
	}
	return runEntry(hostArg, confFlag, errFlag, userFlag, d)
}

// startWorkerWait 는 워커를 띄우고 lock 을 잡을 때까지 잠깐(최대 3초) 기다린다(TUI 가 곧바로 "중단됨"으로 보이지 않게).
func startWorkerWait(dir string) (int, error) {
	pid, err := startWorker(dir)
	if err != nil {
		return 0, err
	}
	for i := 0; i < 30 && !workerAlive(dir); i++ {
		time.Sleep(100 * time.Millisecond)
	}
	return pid, nil
}

// printPlain 은 tty 가 아닐 때 현재 상태를 한 번 글로 낸다.
func printPlain(w io.Writer, dir string, alive func(string) bool) {
	st, err := loadState(dir)
	if err != nil {
		fmt.Fprintf(w, "상태를 읽을 수 없습니다: %v\n", err)
		return
	}
	fmt.Fprint(w, renderPlain(st, alive(dir), time.Now()))
}

// runEntry 는 사례를 고른다.
//
//	(a) 상태 폴더 없음            → 사용자 선택 → 상태 만들기 → 워커 기동 → 화면
//	(b) 있고 워커 살아 있음        → 화면만 붙음(-user 는 무시)
//	(c) 있고 워커 없음, 미완료      → "이어서 진행할까요? (y/N)" y: 워커 재기동 + 화면 / N: 마지막 상태 화면
//	(d) 완료(또는 중지됨)          → 최종 표 화면과 결과 파일 경로
func runEntry(hostArg, confFlag, errFlag, userFlag string, d runDeps) int {
	dir := stateDirFor(hostArg)
	if d.fresh {
		if code := archiveOldState(dir, d); code != 0 {
			return code
		}
	}
	st, err := loadState(dir)
	switch {
	case err == nil:
		return runExisting(dir, hostArg, st, userFlag, d)
	case os.IsNotExist(err):
		return runNew(dir, hostArg, confFlag, errFlag, userFlag, d)
	}
	fmt.Fprintf(d.err, "오류: %v\n처음부터 다시 하려면 -new 를 붙여 실행하세요(이전 상태는 .old-<시각> 으로 보관): new_setup -run -new %s  (상태 폴더 %s)\n", err, hostArg, dir)
	return 2
}

// archiveOldState 는 -new 처리: 이전 상태 폴더가 있으면 워커가 살아 있을 때는 거절(종료코드 2)하고,
// 아니면 지우지 않고 <폴더>.old-<YYYYMMDD-HHMMSS> 로 이름을 바꿔 보관한다(같은 이름이 있으면 -2, -3 …). 폴더가 없으면 아무것도 안 한다.
func archiveOldState(dir string, d runDeps) int {
	if _, err := os.Lstat(dir); err != nil {
		if os.IsNotExist(err) {
			return 0
		}
		fmt.Fprintf(d.err, "오류: 상태 폴더를 확인할 수 없습니다: %v\n", err)
		return 2
	}
	if d.alive(dir) {
		fmt.Fprintf(d.err, "오류: -new 를 쓸 수 없습니다. 이 호스트파일의 작업이 아직 진행 중입니다(워커 실행 중): %s\n"+
			"먼저 같은 명령(-new 없이)으로 화면에 붙어 S 로 작업을 중지하고 워커가 끝난 뒤 다시 -new 로 실행하세요.\n", dir)
		return 2
	}
	dst, err := archiveDir(dir, time.Now())
	if err != nil {
		fmt.Fprintf(d.err, "오류: 이전 상태 폴더를 보관할 수 없습니다: %v\n", err)
		return 2
	}
	fmt.Fprintf(d.err, "알림: 이전 상태를 지우지 않고 보관했습니다: %s\n처음부터 새로 시작합니다.\n", dst)
	return 0
}

// archiveDir 는 dir 을 <dir>.old-<YYYYMMDD-HHMMSS> 로 이름을 바꾸고 새 이름을 돌려준다.
func archiveDir(dir string, now time.Time) (string, error) {
	base := dir + ".old-" + now.Format("20060102-150405")
	dst := base
	for i := 2; ; i++ {
		if _, err := os.Lstat(dst); os.IsNotExist(err) {
			break
		}
		dst = fmt.Sprintf("%s-%d", base, i)
	}
	if err := os.Rename(dir, dst); err != nil {
		return "", err
	}
	return dst, nil
}

func runNew(dir, hostArg, confFlag, errFlag, userFlag string, d runDeps) int {
	clean := strings.TrimPrefix(hostArg, "^")
	raw, err := os.ReadFile(clean)
	if err != nil {
		fmt.Fprintf(d.err, "오류: 호스트 파일을 읽을 수 없습니다: %v\n", err)
		return 2
	}
	if len(parseHostLines(string(raw))) == 0 {
		fmt.Fprintf(d.err, "오류: %s 에 등록된 호스트가 없습니다.\n", clean)
		return 2
	}
	conf, confPath, err := loadConf(confFlag)
	if err != nil {
		fmt.Fprintf(d.err, "오류: %v\n", err)
		return 2
	}
	for _, m := range conf.Warnings {
		fmt.Fprintf(d.err, "경고: %s\n", m)
	}
	_, errPath, err := loadErrList(errFlag)
	if err != nil {
		fmt.Fprintf(d.err, "오류: %v\n", err)
		return 2
	}
	user, err := d.pickUser(conf, userFlag)
	if err != nil {
		fmt.Fprintf(d.err, "오류: %v\n", err)
		return 2
	}
	st, err := newRunState(hostArg, confPath, errPath, user)
	if err != nil {
		fmt.Fprintf(d.err, "오류: %v\n", err)
		return 2
	}
	st.Detail = "워커 시작 중"
	if err := saveState(dir, st); err != nil {
		fmt.Fprintf(d.err, "오류: 상태 파일을 쓸 수 없습니다: %v\n", err)
		return 2
	}
	pid, err := d.start(dir)
	if err != nil {
		fmt.Fprintf(d.err, "오류: 워커를 시작할 수 없습니다: %v\n", err)
		return 2
	}
	fmt.Fprintf(d.err, "워커 시작: pid %d, 상태 폴더 %s\n", pid, dir)
	d.show(dir)
	afterShow(dir, hostArg, d)
	return 0
}

func runExisting(dir, hostArg string, st *runState, userFlag string, d runDeps) int {
	switch {
	case d.alive(dir):
		if userFlag != "" && userFlag != st.User {
			fmt.Fprintf(d.err, "알림: 이미 진행 중인 작업이라 -user 는 무시합니다(사용자: %s)\n", st.User)
		}
	case st.Finished():
		fmt.Fprintf(d.err, "알림: 끝난 작업입니다(%s). 처음부터 다시 하려면 -new 를 붙여 실행하세요(이전 상태는 .old-<시각> 으로 보관): new_setup -run -new %s\n", st.Detail, hostArg)
	default:
		fmt.Fprintf(d.err, "알림: 작업이 중단되어 있습니다(%s 단계). 처음부터 다시 하려면 -new 를 붙여 실행하세요(이전 상태는 .old-<시각> 으로 보관): new_setup -run -new %s\n", stageName(st.Stage), hostArg)
		if d.confirm("작업이 중단되어 있습니다. 이어서 진행할까요? (y/N) ") {
			pid, err := d.start(dir)
			if err != nil {
				fmt.Fprintf(d.err, "오류: 워커를 시작할 수 없습니다: %v\n", err)
				return 2
			}
			fmt.Fprintf(d.err, "워커 재기동: pid %d\n", pid)
		} else {
			fmt.Fprintln(d.err, "이어서 진행하지 않고 마지막 상태만 보여 줍니다.")
		}
	}
	d.show(dir)
	afterShow(dir, hostArg, d)
	return 0
}

// afterShow 는 화면을 나온 뒤 안내(다시 붙는 법·결과 파일)를 낸다. tty 가 아니면 생략한다.
func afterShow(dir, hostArg string, d runDeps) {
	if !d.tty {
		return
	}
	st, err := loadState(dir)
	if err != nil {
		return
	}
	switch {
	case st.Finished():
		fmt.Fprintf(d.err, "작업이 끝났습니다. 결과 파일: %s\n", filepath.Join(dir, resultFile))
	case d.alive(dir):
		fmt.Fprintf(d.err, "화면만 나왔습니다. 작업은 백그라운드에서 계속됩니다. 다시 붙으려면 같은 경로에서 같은 명령을 실행하세요: new_setup -run %s\n", hostArg)
	default:
		fmt.Fprintf(d.err, "워커가 실행 중이 아닙니다. 이어서 하려면 같은 경로에서 같은 명령을 실행하세요: new_setup -run %s\n", hostArg)
	}
}

// askYesNo 는 표준 입력에서 y/N 을 읽는다(입력이 없으면 N).
func askYesNo(prompt string) bool {
	fmt.Fprint(os.Stderr, prompt)
	line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	a := strings.ToLower(strings.TrimSpace(line))
	return a == "y" || a == "yes"
}

// selectUser 는 stress-test 의 stress_run.sh 와 같은 방식으로 사용자를 정한다.
// 설정 USER_ROUTE 가 있으면 info_mn.sh 메뉴를 보여 주고 "Input Number: " 로 번호를 받아 info.sh <번호> 출력을 쓴다(비면 오류).
// 없으면 -user, 그것도 없으면 환경변수 USER.
func selectUser(conf config, flagUser string) (string, error) {
	if conf.UserRoute != "" {
		return userFromRoute(conf.UserRoute, bufio.NewReader(os.Stdin), os.Stdout, os.Stderr)
	}
	if flagUser != "" {
		return flagUser, nil
	}
	if u := os.Getenv("USER"); u != "" {
		return u, nil
	}
	return "", fmt.Errorf("사용자를 알 수 없습니다. -user 로 지정하세요")
}

// userFromRoute 는 route 폴더의 info_mn.sh(메뉴 출력) → 번호 입력 → info.sh <번호>(사용자 출력) 를 실행한다.
func userFromRoute(route string, in *bufio.Reader, out, errw io.Writer) (string, error) {
	menu := exec.Command("bash", filepath.Join(route, "info_mn.sh"))
	menu.Stdout, menu.Stderr = out, errw
	if err := menu.Run(); err != nil {
		return "", fmt.Errorf("%s 실행 실패: %v", filepath.Join(route, "info_mn.sh"), err)
	}
	fmt.Fprint(out, "Input Number: ")
	line, _ := in.ReadString('\n')
	num := strings.TrimSpace(line)
	var buf strings.Builder
	cmd := exec.Command("bash", filepath.Join(route, "info.sh"), num)
	cmd.Stdout, cmd.Stderr = &buf, errw
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%s 실행 실패: %v", filepath.Join(route, "info.sh"), err)
	}
	user := strings.TrimSpace(buf.String())
	if user == "" {
		return "", fmt.Errorf("user 값이 없습니다 (USER_ROUTE 스크립트 확인)")
	}
	return user, nil
}
