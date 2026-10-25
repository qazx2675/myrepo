package tui

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"cross_check/internal/daemon"
	"cross_check/internal/verify"
)

const userHelp = "명령: <번호> 또는 o <번호|ID> 작업 열기(확인자는 승인·취소) · r/Enter 새로고침 · w 감시(2초 자동 갱신) · p 패스워드 변경 · q 종료"

// RunUser 는 cross_check --user <user> 의 줄 입력 화면(터미널이 아닐 때·--plain)을 돌리고 종료 코드를 돌려준다.
// 처음이면 패스워드를 등록하고(INTERVIEW Q26), 내 작업(작업자)과 내가 확인자인 작업 두 표를 보여준다.
// 확인자 화면에는 입력 시간 제한이 없다(계획서 3-2). 전체 화면(RunUserFull)과 같은 동작이다.
func (s *Session) RunUser(ctx context.Context, user string) int {
	reg, err := s.c.UserRegistered(user)
	if err != nil {
		if msg, ok := isDaemonErr(err); ok {
			s.errorf("등록 여부를 확인할 수 없습니다: %s", msg)
			return ExitFail
		}
		s.errorf("%v", err)
		return ExitError
	}
	if !reg {
		if _, err := s.registerFlow(ctx, user, "사용자"); err != nil {
			return quitCode(err, ExitFail)
		}
	}

	s.renderUser(user, s.loadUser(user))
	pending := ""
	for {
		line := pending
		pending = ""
		if line == "" {
			l, err := s.ask(ctx, "\n명령> ")
			if err != nil {
				return quitCode(err, ExitOK)
			}
			line = l
		}
		cmd, arg := splitCmd(line)
		switch cmd {
		case "", "r":
		case "q", "quit", "exit":
			s.println("종료합니다.")
			return ExitOK
		case "h", "?", "help":
			s.println(userHelp)
			continue
		case "w":
			typed, err := s.watchUser(ctx, user)
			if err != nil {
				return quitCode(err, ExitOK)
			}
			pending = typed
			continue
		case "p":
			if err := s.changePassword(ctx, user); err != nil {
				return quitCode(err, ExitOK)
			}
			continue
		case "o":
			if arg == "" {
				s.warnf("열 작업 번호를 같이 입력하세요 (예: o 1)")
				continue
			}
			if err := s.openJob(ctx, user, s.resolve(arg)); err != nil {
				return quitCode(err, ExitOK)
			}
		default:
			if _, e := strconv.Atoi(cmd); e == nil && arg == "" {
				if err := s.openJob(ctx, user, s.resolve(cmd)); err != nil {
					return quitCode(err, ExitOK)
				}
				break
			}
			s.warnf("알 수 없는 명령: %q", line)
			s.println(userHelp)
			continue
		}
		s.renderUser(user, s.loadUser(user))
	}
}

// splitCmd 는 "o 3" → ("o","3"). 명령은 소문자로 바꾼다.
func splitCmd(line string) (string, string) {
	f := strings.Fields(line)
	if len(f) == 0 {
		return "", ""
	}
	cmd := strings.ToLower(f[0])
	return cmd, strings.Join(f[1:], " ")
}

// resolve 는 화면의 [번호] 를 작업 ID 로 바꾼다. 번호가 아니면 ID 로 본다.
func (s *Session) resolve(arg string) string {
	if n, err := strconv.Atoi(arg); err == nil && n >= 1 && n <= len(s.shown) {
		return s.shown[n-1]
	}
	return arg
}

// userView 는 한 번 새로고침한 결과.
type userView struct {
	jobs    []daemon.JobView
	notices []string
	err     error
}

// loadUser 는 List 로 새로 읽고, 지난번과 비교해 상태가 바뀐 작업을 알림으로 만든다.
func (s *Session) loadUser(user string) userView {
	jobs, err := s.c.List(user)
	if err != nil {
		return userView{err: err}
	}
	var notes []string
	now := s.opt.Now()
	for _, j := range jobs {
		label := daemon.StatusLabel(j.Status, j.Phase)
		old, seen := s.prev[j.ID]
		changed := s.prevInit && (!seen || old != label)
		if j.User == user {
			switch {
			case j.Status == daemon.StatusApproved && j.Phase == daemon.PhaseNone && j.StartedAt == nil && remainText(j, s.opt.ApprovalTTL, now) != "" && (changed || !s.prevInit):
				notes = append(notes, s.p.Green(fmt.Sprintf("★ [승인] 내 작업 %s — 확인자 %s 가 승인했습니다. %s", j.ID, j.Checker, remainText(j, s.opt.ApprovalTTL, now))))
			case j.Status == daemon.StatusAborted && changed && seen:
				notes = append(notes, s.p.Red(fmt.Sprintf("[중단] 내 작업 %s — %s", j.ID, j.Reason)))
			case j.Status == daemon.StatusApproved && j.Phase != daemon.PhaseNone && changed && seen:
				notes = append(notes, s.p.Label(j.Status, j.Phase, fmt.Sprintf("[%s] 내 작업 %s", label, j.ID)))
			}
		}
		if j.Checker == user && j.Status == daemon.StatusWaiting && changed && !seen {
			notes = append(notes, s.p.Yellow(fmt.Sprintf("[새 요청] 작업자 %s 의 작업 %s 확인 요청 — 번호로 열어 전달받은 대상을 붙여넣고 승인", j.User, j.ID)))
		}
	}
	s.prev = map[string]string{}
	for _, j := range jobs {
		s.prev[j.ID] = daemon.StatusLabel(j.Status, j.Phase)
	}
	s.prevInit = true
	return userView{jobs: jobs, notices: notes}
}

// renderUser 는 메인 화면: 머리말, 알림, 두 표, 명령 안내.
func (s *Session) renderUser(user string, v userView) {
	now := s.opt.Now()
	s.println()
	s.println(s.p.Cyan(fmt.Sprintf("==== cross_check — 사용자 %s — %s ====", user, now.Format("2006-01-02 15:04:05"))))
	s.println(ttlHeader(s.opt.ApprovalTTL))
	if v.err != nil {
		if msg, ok := isDaemonErr(v.err); ok {
			s.errorf("목록 조회 거부: %s", msg)
		} else {
			s.errorf("목록 조회 실패: %v (데몬 상태 확인: cross_check --ensure)", v.err)
		}
		s.println(userHelp)
		return
	}
	for _, n := range v.notices {
		s.println(n)
	}
	var mine, checking []daemon.JobView
	for _, j := range v.jobs {
		if j.User == user {
			mine = append(mine, j)
		}
		if j.Checker == user {
			checking = append(checking, j)
		}
	}
	s.shown = s.shown[:0]
	s.jobTable(fmt.Sprintf("내 작업 (작업자) — %d건", len(mine)), "확인자", mine, true)
	s.jobTable(fmt.Sprintf("내가 확인자인 작업 — %d건", len(checking)), "작업자", checking, false)
	waiting := 0
	for _, j := range checking {
		if j.Status == daemon.StatusWaiting {
			waiting++
		}
	}
	if waiting > 0 {
		s.println(s.p.Yellow(fmt.Sprintf("※ 내 승인을 기다리는 작업 %d건 — 번호로 열어 a 승인(전달받은 대상 붙여넣기 → 패스워드) / c 취소", waiting)))
	}
	s.println(userHelp)
}

// jobTable 은 표 하나. asWorker 면 상대 칸은 확인자, 아니면 작업자. 최근 MaxRows 건만 보여준다.
// 열: 번호, ID, 상대, 상태(승인/작업중 등), 생성, 승인, [대상 수 — 작업자 표에만], 호출 스크립트.
// (중단 사유는 표에 넣지 않고 작업 상세와 알림 줄에 보인다. 확인자 표에는 대상 수도 없다 — 확인자는 대상을 볼 수 없다.)
func (s *Session) jobTable(title, otherCol string, jobs []daemon.JobView, asWorker bool) {
	s.println()
	s.println(s.p.Bold("[" + title + "]"))
	if len(jobs) == 0 {
		s.println("  (없음)")
		return
	}
	skipped := 0
	if len(jobs) > s.opt.MaxRows {
		skipped = len(jobs) - s.opt.MaxRows
		jobs = jobs[skipped:]
	}
	now := s.opt.Now()
	rows := make([][]string, 0, len(jobs))
	for _, j := range jobs {
		s.shown = append(s.shown, j.ID)
		other := j.Checker
		if !asWorker {
			other = j.User
		}
		st := daemon.StatusLabel(j.Status, j.Phase)
		if asWorker && remainText(j, s.opt.ApprovalTTL, now) != "" {
			st += " ★"
		}
		row := []string{fmt.Sprintf("[%d]", len(s.shown)), j.ID, other, st, fmtTime(j.CreatedAt), fmtTimePtr(j.ApprovedAt)}
		if asWorker {
			row = append(row, strconv.Itoa(j.TargetCount))
		}
		rows = append(rows, append(row, callerText(j.Caller)))
	}
	headers := []string{"번호", "ID", otherCol, "상태", "생성", "승인"}
	if asWorker {
		headers = append(headers, "대상")
	}
	table(s.out, "  ", append(headers, "호출 스크립트"), rows,
		func(row, col int, cell string) string {
			if col == 3 {
				return s.p.Label(jobs[row].Status, jobs[row].Phase, cell)
			}
			return cell
		})
	if skipped > 0 {
		s.printf("  (이전 작업 %d건 생략 — o <ID> 로 열 수 있음)\n", skipped)
	}
}

// watchUser 는 감시 모드: WatchInterval 마다 다시 그리다가 한 줄이 들어오면 끝내고 그 줄을 돌려준다
// (Enter 면 "", 명령을 쳤으면 그 명령을 메인 화면이 이어서 처리).
func (s *Session) watchUser(ctx context.Context, user string) (string, error) {
	for {
		s.renderUser(user, s.loadUser(user))
		s.printf("[감시 모드] %g초마다 자동 갱신 — Enter 를 누르면 끝냅니다(명령을 치면 바로 실행)\n", s.opt.WatchInterval.Seconds())
		line, err := s.in.ReadLineTimeout(ctx, s.opt.WatchInterval)
		if errors.Is(err, ErrTimeout) {
			continue
		}
		if err != nil {
			return "", s.readErr(ctx, err)
		}
		return strings.TrimSpace(line), nil
	}
}

// openJob 은 작업 하나를 자세히 보여준다. 내가 확인자이고 대기면 승인/취소를 받는다.
func (s *Session) openJob(ctx context.Context, user, id string) error {
	j, err := s.c.Get(id, user, "")
	if err != nil {
		if msg, ok := isDaemonErr(err); ok {
			s.errorf("작업 %s 를 열 수 없습니다: %s", id, msg)
		} else {
			s.errorf("작업 조회 실패: %v", err)
		}
		return nil
	}
	s.printDetail(user, j)
	if j.Checker == user && j.Status == daemon.StatusWaiting {
		return s.checkerActions(ctx, user, j)
	}
	if j.User == user && remainText(j, s.opt.ApprovalTTL, s.opt.Now()) != "" {
		s.println(s.p.Bold(PolicyText(jobTTL(j, s.opt.ApprovalTTL))))
	}
	_, err = s.ask(ctx, "Enter 를 누르면 목록으로 돌아갑니다: ")
	return err
}

// printDetail 은 작업 상세. 대상 목록은 작업자에게만 보인다(확인자는 작업자에게 전달받은 대상을 붙여넣어 대조한다).
func (s *Session) printDetail(user string, j daemon.JobView) {
	s.println()
	s.println(s.p.Cyan(fmt.Sprintf("---- 작업 %s ----", j.ID)))
	role := "작업자"
	if j.Checker == user && j.User != user {
		role = "확인자"
	}
	now := s.opt.Now()
	s.printf("  내 역할   : %s\n", role)
	s.printf("  작업자    : %s\n", j.User)
	s.printf("  확인자    : %s\n", j.Checker)
	s.printf("  상태      : %s\n", s.p.Label(j.Status, j.Phase, daemon.StatusLabel(j.Status, j.Phase)))
	s.printf("  사유      : %s\n", reasonText(j, s.opt.ApprovalTTL, now))
	s.printf("  호출      : %s\n", callerText(j.Caller))
	s.printf("  생성      : %s\n", fmtTime(j.CreatedAt))
	s.printf("  승인      : %s\n", fmtTimePtr(j.ApprovedAt))
	s.printf("  본 작업   : %s\n", fmtTimePtr(j.StartedAt))
	s.printf("  작업 종료 : %s\n", fmtTimePtr(j.FinishedAt))
	s.printf("  갱신      : %s\n", fmtTime(j.UpdatedAt))
	if role == "작업자" {
		s.printf("  대상 VM   : %d개\n", len(j.Targets))
		writeTargets(s, j.Targets)
	} else {
		s.printf("  대상 VM   : (확인자에게는 표시되지 않음 — 작업자에게 전달받은 대상을 붙여넣어 승인)\n")
	}
}

// writeTargets 는 대상 이름을 한 줄에 4개씩 번호와 함께 쓴다.
func writeTargets(s *Session, ts []string) {
	w := 0
	for _, t := range ts {
		if len(t) > w {
			w = len(t)
		}
	}
	for i := 0; i < len(ts); i += 4 {
		var sb strings.Builder
		sb.WriteString("     ")
		for k := i; k < i+4 && k < len(ts); k++ {
			sb.WriteString(pad(fmt.Sprintf("%3d) %s", k+1, ts[k]), w+7))
		}
		s.println(strings.TrimRight(sb.String(), " "))
	}
}

// readPasted 는 확인자가 붙여넣는 대상 목록을 받는다: 빈 줄 또는 입력 끝(EOF)까지. 인식한 이름 수만 알려 주고 판정은 하지 않는다.
// EOF 로 끝나면 eof=true (이후 패스워드를 읽을 수 없으므로 호출자는 종료한다).
func (s *Session) readPasted(ctx context.Context, worker string) (text string, n int, eof bool, err error) {
	s.println(s.p.Bold(fmt.Sprintf("작업자 %s 에게 전달받은 작업 대상을 붙여넣으세요(줄바꿈·탭·공백 구분). 빈 줄(Enter) 또는 Ctrl+D 로 끝냅니다.", worker)))
	s.println("  대상 목록은 이 화면에 표시되지 않습니다. 대조는 데몬이 하며 결과는 패스워드 확인 뒤에 나옵니다.")
	var lines []string
	for {
		line, rerr := s.in.ReadLine(ctx)
		if rerr != nil {
			if ctx.Err() == nil && errors.Is(rerr, io.EOF) {
				eof = true
				break
			}
			return "", 0, false, s.readErr(ctx, rerr)
		}
		if strings.TrimSpace(line) == "" {
			break
		}
		lines = append(lines, line)
	}
	text = strings.Join(lines, "\n")
	return text, len(verify.Parse(text)), eof, nil
}

// checkerActions 는 확인자의 승인(a: 대상 붙여넣기 → 패스워드)/취소(c: 패스워드). 시간 제한 없이 기다린다.
// 비밀번호가 틀리면 다시 고를 수 있고, 대상이 달라 중단되거나 상태가 바뀌어 거절되면(이미 중단 등) 목록으로 돌아간다.
func (s *Session) checkerActions(ctx context.Context, user string, j daemon.JobView) error {
	s.println(s.p.Yellow("이 작업의 확인자입니다. a 를 누르고 작업자에게 전달받은 대상을 붙여넣은 뒤 패스워드로 승인하거나, c 로 취소하세요."))
	for {
		line, err := s.ask(ctx, "a 승인 · c 취소 · Enter 목록으로 > ")
		if err != nil {
			return err
		}
		switch strings.ToLower(line) {
		case "":
			return nil
		case "a":
			pasted, n, eof, err := s.readPasted(ctx, j.User)
			if err != nil {
				return err
			}
			if n == 0 {
				s.warnf("붙여넣은 대상이 없어 승인하지 않았습니다.")
				if eof {
					return errQuit{ExitOK}
				}
				continue
			}
			s.printf("  인식한 이름: %d개\n", n)
			pw, err := s.askSecret(ctx, "승인하려면 패스워드를 입력하세요: ")
			if err != nil {
				return err
			}
			if pw == "" {
				s.warnf("입력이 없어 승인하지 않았습니다.")
				continue
			}
			err = s.c.Approve(j.ID, user, pw, pasted)
			if err == nil {
				s.okf("[승인 완료] 작업 %s (작업자 %s, 붙여넣은 대상 %d개 일치)를 승인했습니다. 작업자 스크립트가 다음 단계로 진행합니다.", j.ID, j.User, n)
				s.println(s.p.Bold(PolicyText(jobTTL(j, s.opt.ApprovalTTL))))
				return nil
			}
			if !s.actionFailed("승인", err) {
				return nil
			}
		case "c":
			pw, err := s.askSecret(ctx, "취소하려면 패스워드를 입력하세요: ")
			if err != nil {
				return err
			}
			if pw == "" {
				s.warnf("입력이 없어 취소하지 않았습니다.")
				continue
			}
			err = s.c.Cancel(j.ID, user, pw)
			if err == nil {
				s.okf("[취소 완료] 작업 %s 를 취소(중단)했습니다. 작업자 %s 의 스크립트도 중단됩니다.", j.ID, j.User)
				return nil
			}
			if !s.actionFailed("취소", err) {
				return nil
			}
		default:
			s.warnf("a(승인), c(취소) 또는 Enter(목록으로)를 입력하세요.")
		}
	}
}

// actionFailed 는 승인/취소 실패를 보여주고, 다시 시도할 수 있으면(비밀번호 문제) true.
func (s *Session) actionFailed(what string, err error) bool {
	msg, ok := isDaemonErr(err)
	if !ok {
		s.errorf("%s 요청 실패: %v", what, err)
		return false
	}
	if daemon.IsMismatchReason(msg) {
		s.errorf("%s 실패: %s", what, msg)
		return false
	}
	s.errorf("%s 실패: %s", what, msg)
	if isAuthMsg(msg) {
		if msg == daemon.MsgAuthFailed {
			s.println("  패스워드를 확인하세요(같은 이름으로 5회 연속 실패하면 이후 시도는 1초씩 늦게 처리됩니다).")
		}
		return true
	}
	return false
}

// changePassword 는 p 명령: 현재 패스워드 확인 후 새 패스워드로 바꾼다.
func (s *Session) changePassword(ctx context.Context, user string) error {
	s.println(s.p.Bold("[패스워드 변경] 빈 입력이면 취소합니다."))
	cur, err := s.askSecret(ctx, "현재 패스워드: ")
	if err != nil {
		return err
	}
	if cur == "" {
		s.println("취소했습니다.")
		return nil
	}
	n1, err := s.askSecret(ctx, "새 패스워드: ")
	if err != nil {
		return err
	}
	n2, err := s.askSecret(ctx, "새 패스워드 확인: ")
	if err != nil {
		return err
	}
	if n1 != n2 {
		s.warnf("두 입력이 서로 다릅니다. 변경하지 않았습니다.")
		return nil
	}
	if err := daemon.CheckPassword(n1); err != nil {
		s.warnf("%v — 변경하지 않았습니다.", err)
		return nil
	}
	if err := s.c.ChangePassword(user, cur, n1); err != nil {
		if msg, ok := isDaemonErr(err); ok {
			s.errorf("변경 실패: %s", msg)
		} else {
			s.errorf("변경 요청 실패: %v", err)
		}
		return nil
	}
	s.okf("[변경 완료] %s 의 패스워드를 바꿨습니다.", user)
	return nil
}
