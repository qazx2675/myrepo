// Package tui 는 cross_check 의 사용자(--user)·관리자(--admin) 화면이다. 두 가지 방식이 있다.
//
//   - 전체 화면(screen*.go): stdin·stdout 이 모두 터미널이고 linux 이면 auto_setup 상태 리포트와 같은 방식 —
//     raw 모드(syscall ioctl), 대체 화면, 커서 숨김, 프레임마다 한 번의 write, 2초 자동 갱신, 창 크기 변경 처리,
//     한글 2칸 폭 배치, 키 하나로 바로 동작, 모든 종료 경로에서 터미널 복원.
//   - 줄 입력 화면(Session, 이 파일·user.go·admin.go): 파이프·테스트·e2e·비 linux 일 때(또는 --plain).
//     같은 동작(등록·목록·승인(대상 붙여넣기 → 패스워드)·취소·패스워드 변경·관리자 기능)을 한 줄씩 명령으로 한다.
//
// 모든 입출력은 주입한 io.Reader/io.Writer(전체 화면은 Env)와 Client 인터페이스로만 한다(테스트에서 가짜 클라이언트와 각본 입력을 쓴다).
// 비밀번호는 화면에 쓰지 않는다: 전체 화면은 '*' 만, 줄 입력 화면은 HideInput(보통 EchoOff: 터미널이면 stty -echo)을 켠 채 읽는다.
// 확인자는 작업 대상을 볼 수 없다(데몬이 확인자에게 대상 목록을 주지 않음). 작업자에게 전달받은 대상을 붙여넣으면 데몬이 대조한다.
package tui

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"cross_check/internal/daemon"
	"cross_check/internal/store"
)

// 종료 코드.
const (
	ExitOK        = 0   // 정상 종료(q, 입력 끝)
	ExitFail      = 1   // 인증·등록 실패 등 거부
	ExitError     = 3   // 데몬 연결·통신 오류
	ExitInterrupt = 130 // Ctrl+C
)

// Client 는 TUI 가 쓰는 데몬 요청. *daemon.Client 가 그대로 만족한다.
type Client interface {
	Info() (daemon.Info, error)
	UserRegistered(user string) (bool, error)
	Register(user, password string) error
	ChangePassword(user, oldPassword, newPassword string) error
	List(user string) ([]daemon.JobView, error)
	Get(id, user, password string) (daemon.JobView, error)
	Approve(id, user, password, pasted string) error
	Cancel(id, user, password string) error
	AdminList(adminPassword string) ([]store.Job, error)
	AdminSet(adminPassword, id string, status store.Status, reason string) error
	AdminSetPhase(adminPassword, id string, phase store.Phase, reason string) error
	AdminDelete(adminPassword, id string) error
	AdminClear(adminPassword string) (int, error)
	AdminSetPassword(adminPassword, user, newPassword string) error
}

var _ Client = (*daemon.Client)(nil)

// Options 는 Session 설정. 0 값은 기본값.
type Options struct {
	Color         bool                    // ANSI 색상
	HideInput     func() (restore func()) // 비밀번호 입력 동안 에코 끄기(nil 이면 안 함). 보통 EchoOff
	Now           func() time.Time        // 시계(기본 time.Now)
	WatchInterval time.Duration           // w 감시 모드 갱신 주기(기본 2초)
	ApprovalTTL   time.Duration           // 데몬 승인 유효시간(표시용, 기본 2분)
	MaxRows       int                     // 표 하나에 보여줄 최근 작업 수(기본 20)
}

// Session 은 화면 하나(사용자 또는 관리자).
type Session struct {
	in  *LineReader
	out io.Writer
	c   Client
	p   Painter
	opt Options

	prev     map[string]string // 지난 새로고침 때의 상태 표기(상태/단계, 전이 알림용)
	prevInit bool
	shown    []string // 마지막 화면의 [번호] → 작업 ID
}

// New 는 in/out/c 로 Session 을 만든다.
func New(in io.Reader, out io.Writer, c Client, opt Options) *Session {
	if opt.Now == nil {
		opt.Now = time.Now
	}
	if opt.WatchInterval <= 0 {
		opt.WatchInterval = 2 * time.Second
	}
	if opt.ApprovalTTL <= 0 {
		opt.ApprovalTTL = daemon.DefaultApprovalTTL
	}
	if opt.MaxRows <= 0 {
		opt.MaxRows = 20
	}
	return &Session{in: NewLineReader(in), out: out, c: c, p: Painter{On: opt.Color}, opt: opt, prev: map[string]string{}}
}

// errQuit 은 입력이 끝났거나(EOF) Ctrl+C 로 화면을 끝내야 할 때. code 는 종료 코드.
type errQuit struct{ code int }

func (e errQuit) Error() string { return fmt.Sprintf("종료(%d)", e.code) }

// quitCode 는 errQuit 이면 그 코드, 아니면 def.
func quitCode(err error, def int) int {
	var q errQuit
	if errors.As(err, &q) {
		return q.code
	}
	return def
}

func (s *Session) printf(format string, a ...any) { fmt.Fprintf(s.out, format, a...) }

func (s *Session) println(a ...any) { fmt.Fprintln(s.out, a...) }

func (s *Session) errorf(format string, a ...any) {
	fmt.Fprintln(s.out, s.p.Red("[오류] "+fmt.Sprintf(format, a...)))
}

func (s *Session) okf(format string, a ...any) {
	fmt.Fprintln(s.out, s.p.Green(fmt.Sprintf(format, a...)))
}

func (s *Session) warnf(format string, a ...any) {
	fmt.Fprintln(s.out, s.p.Yellow(fmt.Sprintf(format, a...)))
}

// readErr 는 입력 오류를 errQuit 로 바꾼다(EOF → 정상 종료, Ctrl+C → 130).
func (s *Session) readErr(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		s.println()
		s.warnf("Ctrl+C — 종료합니다")
		return errQuit{ExitInterrupt}
	}
	if errors.Is(err, io.EOF) {
		s.println()
		return errQuit{ExitOK}
	}
	s.errorf("입력을 읽지 못했습니다: %v", err)
	return errQuit{ExitError}
}

// ask 는 prompt 를 쓰고 한 줄(앞뒤 공백 제거)을 읽는다. 시간 제한 없음.
func (s *Session) ask(ctx context.Context, prompt string) (string, error) {
	s.printf("%s", prompt)
	line, err := s.in.ReadLine(ctx)
	if err != nil {
		return "", s.readErr(ctx, err)
	}
	return strings.TrimSpace(line), nil
}

// askSecret 는 에코를 끈 채 비밀번호 한 줄을 읽는다(앞뒤 공백을 자르지 않는다: 규칙 검사는 데몬·CheckPassword 가 함).
// 줄 끝 \r 만 떼고, 입력 내용은 절대 출력하지 않는다.
func (s *Session) askSecret(ctx context.Context, prompt string) (string, error) {
	s.printf("%s", prompt)
	var restore func()
	if s.opt.HideInput != nil {
		restore = s.opt.HideInput()
	}
	line, err := s.in.ReadLine(ctx)
	if restore != nil {
		restore()
		s.println() // 에코가 꺼져 Enter 줄바꿈이 보이지 않았으므로
	}
	if err != nil {
		return "", s.readErr(ctx, err)
	}
	return strings.TrimRight(line, "\r"), nil
}

// isDaemonErr 는 데몬이 거절한 응답인지(연결 오류가 아닌지) 알려주고 그 문구를 돌려준다.
func isDaemonErr(err error) (string, bool) {
	var de *daemon.DaemonError
	if errors.As(err, &de) {
		return de.Msg, true
	}
	return "", false
}

// isAuthMsg 는 비밀번호 문제(틀림·잠김·인증 불가)로 거절된 문구인지.
func isAuthMsg(msg string) bool {
	return strings.HasPrefix(msg, daemon.MsgAuthFailed) || strings.HasPrefix(msg, "인증 불가")
}

// registerFlow 는 첫 실행 등록: 비밀번호를 두 번 받아 Register 한다(최대 3번 시도).
// 성공하면 (비밀번호, nil). 이미 다른 곳에서 등록됐으면 ("", nil) — 호출자가 기존 비밀번호로 진행한다.
func (s *Session) registerFlow(ctx context.Context, name, what string) (string, error) {
	s.println()
	s.println(s.p.Bold(fmt.Sprintf("[첫 실행] %s %s 은(는) 아직 cross_check 에 등록되지 않았습니다.", what, name)))
	s.println("  사용할 패스워드를 두 번 입력하세요(화면에 표시되지 않습니다). 등록은 선착순이며 이후에는 덮어쓸 수 없습니다.")
	s.println("  변경: 이 화면의 p 명령(현재 패스워드 필요) / 분실: 관리자(cross_check --admin)에게 재설정 요청")
	s.println(fmt.Sprintf("  규칙: %d~%d자, 앞뒤 공백·줄바꿈 금지", daemon.MinPasswordLen, daemon.MaxPasswordLen))
	for attempt := 1; attempt <= 3; attempt++ {
		p1, err := s.askSecret(ctx, "새 패스워드: ")
		if err != nil {
			return "", err
		}
		p2, err := s.askSecret(ctx, "새 패스워드 확인: ")
		if err != nil {
			return "", err
		}
		if p1 != p2 {
			s.warnf("두 입력이 서로 다릅니다. 다시 입력하세요. (%d/3)", attempt)
			continue
		}
		if err := daemon.CheckPassword(p1); err != nil {
			s.warnf("%v — 다시 입력하세요. (%d/3)", err, attempt)
			continue
		}
		err = s.c.Register(name, p1)
		if err == nil {
			s.okf("[등록 완료] %s 의 패스워드가 등록되었습니다.", name)
			return p1, nil
		}
		msg, ok := isDaemonErr(err)
		if !ok {
			s.errorf("등록 요청 실패: %v", err)
			return "", errQuit{ExitError}
		}
		if msg == daemon.ErrAlreadyRegistered.Error() {
			s.warnf("[등록 충돌] %s 은(는) 방금 다른 곳에서 먼저 등록되었습니다. 그 패스워드를 쓰거나 관리자에게 재설정을 요청하세요.", name)
			return "", nil
		}
		s.errorf("등록 거부: %s", msg)
		if strings.Contains(msg, "비밀번호") {
			continue // 규칙 위반은 다시 입력
		}
		return "", errQuit{ExitFail}
	}
	s.errorf("등록을 3번 실패해 종료합니다.")
	return "", errQuit{ExitFail}
}
