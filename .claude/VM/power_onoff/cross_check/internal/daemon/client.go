package daemon

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"cross_check/internal/store"
)

// 클라이언트 기본 제한 시간.
const (
	DefaultDialTimeout    = 3 * time.Second
	DefaultRequestTimeout = 30 * time.Second
)

// idleReconnect: 데몬은 run 이 아닌 연결을 요청 사이 ReadTimeout(기본 60초)이 지나면 닫으므로,
// 이보다 오래 쉬었던 연결은 보내기 전에 새로 연다(TUI 에서 대상 목록을 오래 읽은 뒤 승인해도 실패하지 않게).
// 테스트에서만 바꾼다.
var idleReconnect = 30 * time.Second

// ErrClientClosed 는 Close 한 Client 를 다시 쓸 때.
var ErrClientClosed = errors.New("클라이언트가 이미 닫힘")

// DaemonError 는 데몬이 {"ok":false} 로 거절한 요청. Msg 는 데몬의 한국어 오류 문구 그대로다.
// (연결 끊김 같은 통신 오류는 DaemonError 가 아니라 일반 error 로 온다.)
type DaemonError struct {
	Op  string // 거절된 op
	Msg string // 데몬 오류 문구(비밀번호는 절대 들어가지 않음)
	ID  string // run 이 동일인으로 중단 기록만 남긴 경우 그 작업 ID(그 밖에는 빈 값)
}

func (e *DaemonError) Error() string { return e.Msg }

// AbortedError 는 RunHandle.Wait 가 승인 전에 작업이 중단됐을 때 돌려준다.
type AbortedError struct {
	Reason string // 중단 사유(예: "확인자 bob 가 취소")
	By     string // 누가: 확인자 이름, BySystem, ByAdmin
}

func (e *AbortedError) Error() string { return "작업 중단: " + e.Reason }

// Client 는 데몬 연결 하나. run 을 뺀 요청은 이 연결 하나로 차례대로 주고받는다(동시 호출 안전, 내부에서 순서대로).
// 통신 오류가 나면 그 연결은 버리고, 다음 호출 때 다시 연결한다(실패한 요청을 자동으로 다시 보내지는 않음).
type Client struct {
	// Timeout 은 요청 하나(쓰기+응답 읽기)의 제한 시간. 0 이면 DefaultRequestTimeout.
	Timeout time.Duration

	socket  string
	mu      sync.Mutex
	conn    net.Conn
	r       *bufio.Reader
	lastUse time.Time // 마지막으로 응답을 받은(또는 연결한) 시각
	closed  bool
}

// Dial 은 socket(보통 SocketFromEnv())의 데몬에 연결한다. 데몬이 떠 있지 않으면 바로 오류.
func Dial(socket string) (*Client, error) {
	c := &Client{socket: socket}
	if err := c.connect(); err != nil {
		return nil, err
	}
	return c, nil
}

// connect 는 새 연결을 연다. mu 를 잡고 부르거나 생성 직후에 부른다.
func (c *Client) connect() error {
	conn, err := net.DialTimeout("unix", c.socket, DefaultDialTimeout)
	if err != nil {
		return fmt.Errorf("cross_check 데몬 연결 실패(%s): %w", c.socket, err)
	}
	c.conn, c.r = conn, bufio.NewReaderSize(conn, 64<<10)
	c.lastUse = time.Now()
	return nil
}

// Close 는 연결을 닫는다. 이미 만든 RunHandle 은 영향받지 않는다(각자 연결을 가짐).
func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
	if c.conn == nil {
		return nil
	}
	err := c.conn.Close()
	c.conn, c.r = nil, nil
	return err
}

func (c *Client) timeout() time.Duration {
	if c.Timeout > 0 {
		return c.Timeout
	}
	return DefaultRequestTimeout
}

// call 은 요청 한 줄을 보내고 응답 한 줄을 받는다. ok=false 면 *DaemonError.
func (c *Client) call(req Request) (Response, error) {
	b, err := encodeLine(req)
	if err != nil {
		return Response{}, err
	}
	if len(b)-1 > MaxLineSize {
		return Response{}, errors.New(msgTooLarge)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return Response{}, ErrClientClosed
	}
	if c.conn != nil && time.Since(c.lastUse) > idleReconnect {
		c.conn.Close() // 데몬이 곧(또는 이미) 닫을 연결
		c.conn, c.r = nil, nil
	}
	if c.conn == nil {
		if err := c.connect(); err != nil {
			return Response{}, err
		}
	}
	drop := func(e error) (Response, error) {
		c.conn.Close()
		c.conn, c.r = nil, nil
		return Response{}, fmt.Errorf("cross_check 데몬 통신 실패(%s): %w", req.Op, e)
	}
	c.conn.SetDeadline(time.Now().Add(c.timeout()))
	if _, err := c.conn.Write(b); err != nil {
		return drop(err)
	}
	line, err := readLine(c.r, maxResponseLine)
	if err != nil {
		return drop(err)
	}
	c.conn.SetDeadline(time.Time{})
	c.lastUse = time.Now()
	var resp Response
	if err := json.Unmarshal(line, &resp); err != nil {
		return drop(errors.New("응답 형식 오류"))
	}
	if !resp.OK {
		return resp, &DaemonError{Op: req.Op, Msg: resp.Error, ID: resp.ID}
	}
	return resp, nil
}

// Ping 은 데몬이 응답하는지 확인한다(인증 없음).
func (c *Client) Ping() error {
	_, err := c.call(Request{Op: OpPing})
	return err
}

// ApprovalTTL 은 데몬이 지금 적용 중인 승인 유효시간을 ping 으로 알아낸다.
// 구버전 데몬처럼 값을 주지 않으면 (0, nil) 이므로 호출자는 설정 파일·기본값으로 대신한다.
func (c *Client) ApprovalTTL() (time.Duration, error) {
	resp, err := c.call(Request{Op: OpPing})
	if err != nil {
		return 0, err
	}
	return time.Duration(resp.TTLSec) * time.Second, nil
}

// UserRegistered 는 user 가 auth.conf 에 등록되어 있는지 알려준다(인증 없음, 첫 실행 등록 안내용).
// 비밀번호 파일이 손상되어 판단할 수 없으면 *DaemonError.
func (c *Client) UserRegistered(user string) (bool, error) {
	resp, err := c.call(Request{Op: OpStatusUser, User: user})
	if err != nil {
		return false, err
	}
	return resp.Registered != nil && *resp.Registered, nil
}

// Register 는 아직 등록되지 않은 user 를 password 로 등록한다. 이미 있으면 Msg "이미 등록됨" 인 *DaemonError.
// user 는 cross_check.conf 의 users= 목록에 있어야 하고, admin 은 언제나 거부된다(관리자 항목은 install.sh 가 만든다).
// password 규칙: 6~128자, 줄바꿈·NUL·앞뒤 공백 금지(CheckPassword 로 미리 검사 가능).
func (c *Client) Register(user, password string) error {
	_, err := c.call(Request{Op: OpRegister, User: user, Password: password})
	return err
}

// ChangePassword 는 현재 비밀번호(oldPassword)를 확인한 뒤 newPassword 로 바꾼다(실패는 연속 실패 횟수에 셈).
func (c *Client) ChangePassword(user, oldPassword, newPassword string) error {
	_, err := c.call(Request{Op: OpChangePassword, User: user, Password: oldPassword, NewPassword: newPassword})
	return err
}

// List 는 user 가 작업자 또는 확인자인 작업 목록(생성 순, 대상 목록 포함)을 돌려준다(비밀번호 없음).
func (c *Client) List(user string) ([]JobView, error) {
	resp, err := c.call(Request{Op: OpList, User: user})
	if err != nil {
		return nil, err
	}
	return resp.Jobs, nil
}

// Get 은 작업 하나를 돌려준다. user 가 그 작업의 작업자·확인자여야 한다.
// password 는 선택: 비우면 검사하지 않고, 주면 그 user 의 비밀번호와 맞아야 한다(승인 전에 미리 확인하는 용도).
func (c *Client) Get(id, user, password string) (JobView, error) {
	resp, err := c.call(Request{Op: OpGet, ID: id, User: user, Password: password})
	if err != nil {
		return JobView{}, err
	}
	if resp.Job == nil {
		return JobView{}, errors.New("응답에 작업이 없음")
	}
	return *resp.Job, nil
}

// Approve 는 확인자 user 가 자기 비밀번호로 대기 작업 id 를 승인한다. 승인되면 작업자에게 승인 이벤트가 간다.
// 비밀번호가 틀리면 상태는 그대로이고 *DaemonError("인증 실패…").
func (c *Client) Approve(id, user, password string) error {
	_, err := c.call(Request{Op: OpApprove, ID: id, User: user, Password: password})
	return err
}

// Cancel 은 확인자 user 가 자기 비밀번호로 대기 작업 id 를 취소(중단)한다. 작업자에게 중단 이벤트가 간다.
func (c *Client) Cancel(id, user, password string) error {
	_, err := c.call(Request{Op: OpCancel, ID: id, User: user, Password: password})
	return err
}

// Start 는 본 작업 직전에 부른다. token 은 Run 이 준 RunHandle.Token.
// 승인 상태이고, 승인 후 유효시간(approval_ttl_sec) 이내이며, 처음일 때만 nil. 유효시간이 지났으면 작업은 중단되고
// Msg 가 "승인 후 {TTL} 내 미진행"(ReasonExpired, IsExpiredReason 으로 알아봄) 인 *DaemonError 가 온다.
func (c *Client) Start(id, token string) error {
	_, err := c.call(Request{Op: OpStart, ID: id, Token: token})
	return err
}

// AdminList 는 모든 작업(대상·감사 uid 포함)을 돌려준다. adminPassword 는 auth.conf 의 admin 비밀번호.
func (c *Client) AdminList(adminPassword string) ([]store.Job, error) {
	resp, err := c.call(Request{Op: OpAdminList, Password: adminPassword})
	if err != nil {
		return nil, err
	}
	return resp.AllJobs, nil
}

// AdminSet 은 작업 상태를 바꾼다: 대기→승인, 대기→중단, 승인→중단 만 된다(대기로 못 되돌리고, 중단된 작업은
// 되살릴 수 없음). reason 은 "관리자 변경: " 뒤에 붙는다. 승인으로 바꾸면 그때부터 승인 유효시간 규칙이 적용된다.
func (c *Client) AdminSet(adminPassword, id string, status store.Status, reason string) error {
	_, err := c.call(Request{Op: OpAdminSet, Password: adminPassword, ID: id, Status: status, Reason: reason})
	return err
}

// AdminDelete 는 작업 하나를 지운다(기다리던 작업자에게는 중단 이벤트).
func (c *Client) AdminDelete(adminPassword, id string) error {
	_, err := c.call(Request{Op: OpAdminDelete, Password: adminPassword, ID: id})
	return err
}

// AdminClear 는 모든 작업을 지우고 지운 개수를 돌려준다.
func (c *Client) AdminClear(adminPassword string) (int, error) {
	resp, err := c.call(Request{Op: OpAdminClear, Password: adminPassword})
	if err != nil {
		return 0, err
	}
	return resp.Count, nil
}

// AdminSetPassword 는 관리자가 users= 목록에 있는 user 의 비밀번호를 newPassword 로 재설정한다(없으면 새로 등록).
func (c *Client) AdminSetPassword(adminPassword, user, newPassword string) error {
	_, err := c.call(Request{Op: OpAdminSetPassword, Password: adminPassword, User: user, NewPassword: newPassword})
	return err
}

// RunHandle 은 run 으로 등록한 작업 하나와 그 전용 연결. 이 연결이 열려 있는 동안 데몬은 작업자가 살아 있다고 본다:
// 대기 중에 Close(또는 프로세스 종료)하면 데몬이 즉시 그 작업을 중단한다. 승인 후에 닫는 것은 괜찮다.
type RunHandle struct {
	ID     string // 작업 ID
	Token  string // Start 에 넘길 1회용 token(화면·로그에 출력하지 말 것)
	TTLSec int    // run 시점에 데몬이 알려 준 승인 유효시간(초). 구버전 데몬이면 0

	conn    net.Conn
	events  chan Event
	decided chan struct{} // 첫 승인/중단 또는 연결 끊김 때 닫힘
	once    sync.Once     // decided 한 번만
	result  error         // decided 뒤에만 읽음
	closeMu sync.Once
}

// Run 은 작업을 등록한다(비밀번호 없음, 작업자·확인자는 등록되어 있어야 함).
// targets 는 작업자 대상 파일(on_off_<user>.txt)의 부분집합이어야 한다.
// 동일인(user==checker)이면 데몬이 중단 기록만 남기고 *DaemonError(ID 채움)를 돌려준다.
// 성공하면 새 연결을 쥔 RunHandle 을 돌려준다(Client 의 연결과 별개). 다 쓰면 Close 한다.
func (c *Client) Run(user, checker string, targets []string) (*RunHandle, error) {
	b, err := encodeLine(Request{Op: OpRun, User: user, Checker: checker, Targets: targets})
	if err != nil {
		return nil, err
	}
	if len(b)-1 > MaxLineSize {
		return nil, fmt.Errorf("%s(대상 %d개) — 대상을 나눠서 요청하세요", msgTooLarge, len(targets))
	}
	conn, err := net.DialTimeout("unix", c.socket, DefaultDialTimeout)
	if err != nil {
		return nil, fmt.Errorf("cross_check 데몬 연결 실패(%s): %w", c.socket, err)
	}
	r := bufio.NewReaderSize(conn, 64<<10)
	conn.SetDeadline(time.Now().Add(c.timeout()))
	if _, err := conn.Write(b); err != nil {
		conn.Close()
		return nil, fmt.Errorf("cross_check 데몬 통신 실패(run): %w", err)
	}
	line, err := readLine(r, maxResponseLine)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("cross_check 데몬 통신 실패(run): %w", err)
	}
	conn.SetDeadline(time.Time{})
	var resp Response
	if err := json.Unmarshal(line, &resp); err != nil {
		conn.Close()
		return nil, errors.New("cross_check 데몬 응답 형식 오류(run)")
	}
	if !resp.OK {
		conn.Close()
		return nil, &DaemonError{Op: OpRun, Msg: resp.Error, ID: resp.ID}
	}
	h := &RunHandle{
		ID: resp.ID, Token: resp.Token, TTLSec: resp.TTLSec, conn: conn,
		events: make(chan Event, 64), decided: make(chan struct{}),
	}
	go h.readLoop(r)
	return h, nil
}

// readLoop 는 이벤트 줄을 읽어 Events 로 넘기고, 첫 승인/중단을 Wait 결과로 정한다. 연결이 끝나면 Events 를 닫는다.
func (h *RunHandle) readLoop(r *bufio.Reader) {
	defer close(h.events)
	for {
		line, err := readLine(r, maxResponseLine)
		if err != nil {
			h.decide(fmt.Errorf("cross_check 데몬 연결이 끊겨 승인 여부를 알 수 없음: %w", err))
			return
		}
		var ev Event
		if json.Unmarshal(line, &ev) != nil || ev.Event != EventStatus {
			continue
		}
		select {
		case h.events <- ev:
		default: // 아무도 Events 를 읽지 않아 버퍼(64)가 차면 버린다(Wait 는 영향 없음)
		}
		switch ev.Status {
		case StatusApproved:
			h.decide(nil)
		case StatusAborted:
			h.decide(&AbortedError{Reason: ev.Reason, By: ev.By})
		}
	}
}

func (h *RunHandle) decide(err error) {
	h.once.Do(func() {
		h.result = err
		close(h.decided)
	})
}

// Events 는 이 작업의 상태 변화 이벤트(대기 → 승인 → 승인(본 작업 시작) / 중단)를 받는 채널.
// 연결이 끝나면 닫힌다. 버퍼는 64개이고 읽지 않아 가득 차면 새 이벤트는 버려진다. Wait 와 함께 써도 된다.
func (h *RunHandle) Events() <-chan Event { return h.events }

// Wait 는 승인되거나 중단될 때까지 기다린다(대기에는 시간 제한이 없다).
// 승인이면 nil, 중단이면 *AbortedError, 연결이 끊기면 그 밖의 error.
// ctx 가 먼저 끝나면 연결을 닫고 ctx.Err() 를 돌려준다(데몬은 대기 중이던 작업을 "작업자 스크립트 종료" 로 중단).
// 승인된 뒤에도 연결은 열려 있어 Events 로 이후 이벤트(승인 유효시간 만료·관리자 변경·본 작업 시작)를 받을 수 있다.
func (h *RunHandle) Wait(ctx context.Context) error {
	select {
	case <-h.decided:
		return h.result
	case <-ctx.Done():
		select {
		case <-h.decided: // 동시에 결정됐으면 결과를 우선
			return h.result
		default:
		}
		h.Close()
		return ctx.Err()
	}
}

// Close 는 연결을 닫는다. 여러 번 불러도 안전하다.
func (h *RunHandle) Close() error {
	var err error
	h.closeMu.Do(func() { err = h.conn.Close() })
	return err
}
