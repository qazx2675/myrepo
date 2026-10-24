package tui

import (
	"context"
	"fmt"
	"sort"
	"strconv"

	"cross_check/internal/daemon"
	"cross_check/internal/store"
)

// ClearConfirmText 는 전체 정리 전에 그대로 입력해야 하는 확인 문구.
const ClearConfirmText = "전체 정리"

const adminMenu = `
[관리자 메뉴]
  1) 전체 목록(대상 포함)
  2) 상태 변경(승인/중단, 사유)
  3) 작업 삭제
  4) 전체 정리
  5) 사용자 패스워드 재설정
  6) 등록된 사용자 보기
  q) 종료`

// AdminMissingText 는 auth.conf 에 admin 항목이 없을 때 관리자 화면이 보여주는 안내.
// admin 은 화면에서 만들 수 없다(먼저 연 사람이 관리자를 차지하지 못하게): install.sh 가 만들거나 root 가 직접 추가한다.
const AdminMissingText = `관리자(admin) 항목이 auth.conf 에 없어 관리자 화면을 열 수 없습니다.
  관리자 항목은 이 화면에서 만들 수 없습니다. 다음 중 하나를 root 로 하세요:
    1) install.sh 를 실행(관리자 패스워드를 물어 auth.conf 에 admin 줄을 만듭니다)
    2) auth.conf(기본 /etc/cross_check/auth.conf, 권한 0600)에 'admin=<패스워드>' 한 줄을 직접 추가(6자 이상)`

// RunAdmin 은 cross_check --admin 화면을 돌리고 종료 코드를 돌려준다.
// admin 항목이 없으면 안내만 하고 종료 1, 있으면 세션마다 한 번 패스워드를 묻는다(최대 3번).
func (s *Session) RunAdmin(ctx context.Context) int {
	reg, err := s.c.UserRegistered(daemon.AdminUser)
	if err != nil {
		if msg, ok := isDaemonErr(err); ok {
			s.errorf("관리자 등록 여부를 확인할 수 없습니다: %s", msg)
			return ExitFail
		}
		s.errorf("%v", err)
		return ExitError
	}
	if !reg {
		s.errorf("%s", AdminMissingText)
		return ExitFail
	}
	pw, err := s.adminLogin(ctx)
	if err != nil {
		return quitCode(err, ExitFail)
	}
	s.okf("[관리자] 로그인했습니다.")

	var last []store.Job // 마지막 1번 목록(번호로 고를 수 있게)
	for {
		s.println(adminMenu)
		choice, err := s.ask(ctx, "선택> ")
		if err != nil {
			return quitCode(err, ExitOK)
		}
		switch choice {
		case "1":
			if jobs, ok := s.adminList(pw); ok {
				last = jobs
			}
		case "2":
			err = s.adminSet(ctx, pw, last)
		case "3":
			err = s.adminDelete(ctx, pw, last)
		case "4":
			err = s.adminClear(ctx, pw)
		case "5":
			err = s.adminResetPassword(ctx, pw)
		case "6":
			s.adminUsers(pw)
		case "q", "Q", "quit", "exit":
			s.println("종료합니다.")
			return ExitOK
		case "":
		default:
			s.warnf("1~6 또는 q 를 입력하세요.")
		}
		if err != nil {
			return quitCode(err, ExitOK)
		}
	}
}

// adminLogin 은 관리자 패스워드를 받아 AdminList 로 확인한다(3번까지).
func (s *Session) adminLogin(ctx context.Context) (string, error) {
	for attempt := 1; attempt <= 3; attempt++ {
		pw, err := s.askSecret(ctx, "관리자 패스워드: ")
		if err != nil {
			return "", err
		}
		if pw == "" {
			s.warnf("입력이 없습니다. (%d/3)", attempt)
			continue
		}
		_, err = s.c.AdminList(pw)
		if err == nil {
			return pw, nil
		}
		msg, ok := isDaemonErr(err)
		if !ok {
			s.errorf("데몬 요청 실패: %v", err)
			return "", errQuit{ExitError}
		}
		if msg == daemon.MsgNoAdmin {
			s.errorf("%s", AdminMissingText) // 그사이 admin 줄이 지워짐
			return "", errQuit{ExitFail}
		}
		s.errorf("%s (%d/3)", msg, attempt)
	}
	s.errorf("관리자 인증을 3번 실패해 종료합니다.")
	return "", errQuit{ExitFail}
}

// adminErr 는 관리자 요청 실패를 보여준다.
func (s *Session) adminErr(what string, err error) {
	if msg, ok := isDaemonErr(err); ok {
		s.errorf("%s 거부: %s", what, msg)
		return
	}
	s.errorf("%s 요청 실패: %v", what, err)
}

// adminList 는 1번: 모든 작업 표(감사 uid 포함)와 작업별 대상 목록.
func (s *Session) adminList(pw string) ([]store.Job, bool) {
	jobs, err := s.c.AdminList(pw)
	if err != nil {
		s.adminErr("목록 조회", err)
		return nil, false
	}
	s.println()
	s.println(s.p.Bold(fmt.Sprintf("[전체 작업] %d건 — %s", len(jobs), s.opt.Now().Format("2006-01-02 15:04:05"))))
	if len(jobs) == 0 {
		s.println("  (없음)")
		return jobs, true
	}
	rows := make([][]string, 0, len(jobs))
	for i, j := range jobs {
		reason := j.Reason
		if j.Status == daemon.StatusApproved && j.StartedAt != nil {
			if reason != "" {
				reason += " / "
			}
			reason += "본 작업 시작 " + fmtTime(*j.StartedAt)
		}
		if reason == "" {
			reason = "-"
		}
		rows = append(rows, []string{
			fmt.Sprintf("[%d]", i+1), j.ID, j.User, j.Checker, string(j.Status),
			fmtTime(j.CreatedAt), fmtTimePtr(j.ApprovedAt), strconv.Itoa(len(j.Targets)),
			fmt.Sprintf("%d/%d", j.WorkerUID, j.CheckerUID), reason,
		})
	}
	table(s.out, "  ", []string{"번호", "ID", "작업자", "확인자", "상태", "생성", "승인", "대상", "uid(작업/확인)", "사유"}, rows,
		func(row, col int, cell string) string {
			if col == 4 {
				return s.p.Status(jobs[row].Status, cell)
			}
			return cell
		})
	s.println()
	s.println(s.p.Bold("[작업별 대상]"))
	for i, j := range jobs {
		s.printf("  [%d] %s (%d개): %s\n", i+1, j.ID, len(j.Targets), joinTargets(j.Targets, 0))
	}
	return jobs, true
}

// pickJobID 는 번호(마지막 목록 기준) 또는 ID 를 받는다. 빈 입력이면 "".
func (s *Session) pickJobID(ctx context.Context, last []store.Job) (string, error) {
	in, err := s.ask(ctx, "작업 번호(1번 목록 기준) 또는 ID (Enter 취소): ")
	if err != nil || in == "" {
		return "", err
	}
	if n, e := strconv.Atoi(in); e == nil && n >= 1 && n <= len(last) {
		return last[n-1].ID, nil
	}
	return in, nil
}

// adminSet 은 2번: 상태를 승인 또는 중단으로 바꾼다(사유는 "관리자 변경: " 뒤에 붙는다).
func (s *Session) adminSet(ctx context.Context, pw string, last []store.Job) error {
	id, err := s.pickJobID(ctx, last)
	if err != nil || id == "" {
		return err
	}
	c, err := s.ask(ctx, "바꿀 상태 — 1) 승인  2) 중단 : ")
	if err != nil {
		return err
	}
	var st store.Status
	switch c {
	case "1", string(daemon.StatusApproved):
		st = daemon.StatusApproved
	case "2", string(daemon.StatusAborted):
		st = daemon.StatusAborted
	default:
		s.warnf("1 또는 2 를 입력하세요. 변경하지 않았습니다.")
		return nil
	}
	reason, err := s.ask(ctx, "사유(선택): ")
	if err != nil {
		return err
	}
	if err := s.c.AdminSet(pw, id, st, reason); err != nil {
		s.adminErr("상태 변경", err)
		return nil
	}
	s.okf("[변경 완료] 작업 %s → %s", id, st)
	if st == daemon.StatusApproved {
		s.println(s.p.Bold(PolicyText(s.opt.ApprovalTTL)))
	}
	return nil
}

// adminDelete 는 3번: 작업 하나를 지운다(y 확인).
func (s *Session) adminDelete(ctx context.Context, pw string, last []store.Job) error {
	id, err := s.pickJobID(ctx, last)
	if err != nil || id == "" {
		return err
	}
	yes, err := s.ask(ctx, fmt.Sprintf("작업 %s 를 삭제합니다. 계속하려면 y: ", id))
	if err != nil {
		return err
	}
	if yes != "y" && yes != "Y" {
		s.println("삭제하지 않았습니다.")
		return nil
	}
	if err := s.c.AdminDelete(pw, id); err != nil {
		s.adminErr("삭제", err)
		return nil
	}
	s.okf("[삭제 완료] 작업 %s", id)
	return nil
}

// adminClear 는 4번: 확인 문구를 그대로 입력해야 모든 작업을 지운다.
func (s *Session) adminClear(ctx context.Context, pw string) error {
	s.println(s.p.Red("모든 작업 기록을 지웁니다. 기다리던 작업자 스크립트는 모두 중단됩니다."))
	in, err := s.ask(ctx, fmt.Sprintf("계속하려면 %q 를 그대로 입력하세요: ", ClearConfirmText))
	if err != nil {
		return err
	}
	if in != ClearConfirmText {
		s.println("확인 문구가 달라 정리하지 않았습니다.")
		return nil
	}
	n, err := s.c.AdminClear(pw)
	if err != nil {
		s.adminErr("전체 정리", err)
		return nil
	}
	s.okf("[정리 완료] %d건 삭제", n)
	return nil
}

// adminResetPassword 는 5번: users= 목록 사용자의 패스워드 재설정(없으면 새로 등록, 두 번 입력).
func (s *Session) adminResetPassword(ctx context.Context, pw string) error {
	user, err := s.ask(ctx, "재설정할 사용자 이름(users= 목록에 있는 이름) (Enter 취소): ")
	if err != nil || user == "" {
		return err
	}
	if !daemon.ValidName(user) {
		s.warnf("사용자 이름이 올바르지 않습니다(영소문자·숫자·_ 만, %d자 이하).", daemon.MaxNameLen)
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
		s.warnf("두 입력이 서로 다릅니다. 재설정하지 않았습니다.")
		return nil
	}
	if err := daemon.CheckPassword(n1); err != nil {
		s.warnf("%v — 재설정하지 않았습니다.", err)
		return nil
	}
	if err := s.c.AdminSetPassword(pw, user, n1); err != nil {
		s.adminErr("패스워드 재설정", err)
		return nil
	}
	s.okf("[재설정 완료] %s 의 패스워드를 바꿨습니다(연속 실패 지연도 풀림).", user)
	return nil
}

// adminUsers 는 6번: 데몬에 등록 사용자 목록 op 가 없으므로 그렇다고 알리고, 참고로 작업 기록에 나온 이름만 보여준다.
func (s *Session) adminUsers(pw string) {
	s.warnf("데몬에 등록 사용자 목록 조회 기능이 없어 등록 명단은 표시할 수 없습니다(명단은 root 소유 auth.conf 에만 있음).")
	jobs, err := s.c.AdminList(pw)
	if err != nil {
		s.adminErr("목록 조회", err)
		return
	}
	seen := map[string]bool{}
	for _, j := range jobs {
		seen[j.User] = true
		seen[j.Checker] = true
	}
	names := make([]string, 0, len(seen))
	for n := range seen {
		if n != "" {
			names = append(names, n)
		}
	}
	sort.Strings(names)
	if len(names) == 0 {
		s.println("  (작업 기록에 나온 이름 없음)")
		return
	}
	s.printf("  참고 — 작업 기록에 나온 이름(등록 여부와 무관): %s\n", joinTargets(names, 0))
}
