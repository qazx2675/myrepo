// Package daemon 은 cross_check 상시 거주 데몬(서버)과 그 데몬에 붙는 클라이언트를 함께 담는다.
//
// 통신(INTERVIEW Q4): Unix 도메인 소켓(0666), 요청·응답 모두 JSON 객체 한 줄(\n 으로 끝남, 요청 최대 1 MiB).
//
//	요청: {"op":"<op>", ...}           응답: {"ok":true|false, "error":"...", ...}
//
// 신원 확인: 모든 사람이 root 로 로그인하므로 OS uid 로는 사람을 구분할 수 없다. 그래서 상태를 바꾸는
// 확인자·관리자 op(approve/cancel/admin_*)는 auth.conf 의 비밀번호(password)로 확인한다(fail closed).
// 실패는 "인증 실패" 로만 답한다. 같은 이름으로 5번 연속 실패하면 그 이름의 이후 시도는 한 번에 하나씩
// 1초 지연 뒤에 처리한다(잠그지 않음: 올바른 비밀번호는 지연 중에도 통과하고 횟수를 지운다).
// 전체 실패가 1분에 100번을 넘으면 실패 응답마다 1초씩(전체에서 한 번에 하나씩) 늦춘다.
// 이름은 cross_check.conf 의 users= 목록에 있어야 한다(auth=required). admin 은 목록 밖의 관리자 전용 이름이며
// register 로 만들 수 없고 install.sh 가 auth.conf 에 직접 쓴다.
// start 는 run 이 작업자에게만 준 1회용 token 으로 확인한다. run/list/get 은 상태를 바꾸지 않거나
// 작업자 자신의 등록이라 비밀번호가 없다. SO_PEERCRED uid 는 감사 기록(Job.WorkerUID/CheckerUID)용일 뿐이다.
//
// 자원 한도: 동시 연결 256개(넘으면 오류 한 줄 후 닫음), run 이 아닌 연결은 요청마다 60초 안에 다음 줄이
// 와야 함(넘으면 닫음), 작업자 한 명의 대기 작업 50건, 저장 작업 전체 20000건. 30일 지난 중단 작업은 매시간 지운다.
//
// op 목록과 규칙:
//
//	ping                            → {"ok":true,"ttl_sec":120}  (ttl_sec: 이 데몬의 승인 유효시간. 구버전 데몬은 필드 없음)
//	status_user {user}              → {"ok":true,"registered":bool}  (첫 실행 등록 안내용, 인증 없음)
//	register {user,password}        → users= 목록에 있고 auth.conf 에 아직 없는 user 만 등록(admin 은 항상 거부)
//	change_password {user,password,new_password} → 현재 비밀번호 확인 후 변경
//	run     {user,checker,targets}  → 작업자·확인자 모두 users= 목록에 있고 등록되어 있으며, targets 가
//	                                  <target_dir>/on_off_<user>.txt 의 부분집합일 때만 등록.
//	                                  {"ok":true,"id":"...","token":"...","ttl_sec":120} 후 연결 유지. 이후 상태가 바뀔 때마다
//	                                  {"event":"status","status":"대기|승인|중단","reason":"...","by":"..."} 한 줄씩
//	                                  (승인 이벤트에는 선택 필드 ttl_sec, expires_at(RFC3339) 가 붙는다).
//	                                  중단 또는 본 작업 시작 이벤트 뒤 데몬이 연결을 닫는다.
//	                                  대기 중 작업자 연결이 끊기면(Ctrl+C·세션 끊김·kill -9) 즉시 중단.
//	list    {user}                  → user 가 작업자/확인자인 작업(대상 목록 포함). 각 작업에 ttl_sec, 승인 후 미시작이면 expires_at
//	get     {id,user[,password]}    → 그 작업 하나(user 가 작업자/확인자일 때). password 를 주면 그것도 확인
//	approve {id,user,password}      → 확인자 본인 비밀번호로 대기 작업 승인
//	cancel  {id,user,password}      → 확인자 본인 비밀번호로 대기 작업 취소
//	start   {id,token}              → 본 작업 직전 신호. 승인 후 ApprovalTTL(approval_ttl_sec, 기본 120초) 이내 1회(성공하면 token 폐기)
//	admin_list / admin_set {id,status,reason} / admin_delete {id} / admin_clear /
//	admin_set_password {user,new_password}                       ← password 는 admin 의 것
//	                                  (auth.conf 에 admin 항목이 없으면 관리자 op 는 모두 거부)
//	                                  admin_set: 대기→승인/중단, 승인→중단 만(중단된 작업은 되살릴 수 없음)
//	                                  admin_set_password: users= 목록의 사용자만(항목이 없으면 새로 만듦)
//
// run 연결 하나가 작업 하나에 대응하고, 그 밖의 op 는 한 연결에서 여러 번 주고받을 수 있다.
// 비밀번호와 token 은 로그·오류 문구·list/get 결과에 절대 넣지 않는다.
package daemon

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"cross_check/internal/store"
)

// 기본 경로(INTERVIEW Q4·Q9·Q10).
const (
	DefaultSocketPath = "/var/run/cross_check/cross_check.sock"
	DefaultStorePath  = "/var/lib/cross_check/jobs.json"
	DefaultLockPath   = "/var/run/cross_check/daemon.lock"

	// EnvSocket 은 소켓 경로를 바꾸는 환경변수 이름(테스트·비표준 설치용).
	EnvSocket = "CROSS_CHECK_SOCKET"
)

// 기본값과 한도.
const (
	DefaultApprovalTTL   = 2 * time.Minute // 승인 후 start 까지 허용 시간 기본값(계획서 4-2 필수 정책). cross_check.conf 의 approval_ttl_sec 로 바꾼다
	DefaultSweepInterval = time.Second     // 승인 만료 검사 주기

	MaxLineSize  = 1 << 20 // 요청 한 줄 최대 크기(줄바꿈 제외). 넘으면 오류 응답 후 연결 종료
	MaxTargets   = 100000  // run 대상 최대 개수(실제로는 MaxLineSize 가 먼저 걸릴 수 있음)
	MaxNameLen   = 64      // user/checker 이름 최대 길이
	MaxTargetLen = 255     // 대상 이름 하나 최대 길이(호스트명 한도)
)

// maxResponseLine 은 클라이언트가 받아들이는 응답 한 줄 최대 크기.
// list/admin_list 는 대상 목록을 담으므로 요청 한도보다 훨씬 크게 둔다.
const maxResponseLine = 512 << 20

// SocketFromEnv 는 환경변수 CROSS_CHECK_SOCKET 이 있으면 그 값을, 없으면 DefaultSocketPath 를 돌려준다.
func SocketFromEnv() string {
	if v := os.Getenv(EnvSocket); v != "" {
		return v
	}
	return DefaultSocketPath
}

// 요청 op 이름.
const (
	OpPing        = "ping"
	OpRun         = "run"
	OpList        = "list"
	OpGet         = "get"
	OpApprove     = "approve"
	OpCancel      = "cancel"
	OpStart       = "start"
	OpAdminList   = "admin_list"
	OpAdminSet    = "admin_set"
	OpAdminDelete = "admin_delete"
	OpAdminClear  = "admin_clear"

	OpRegister         = "register"
	OpStatusUser       = "status_user"
	OpChangePassword   = "change_password"
	OpAdminSetPassword = "admin_set_password"
)

// 인증 방식(Config.AuthMode).
const (
	AuthRequired = "required" // 기본: 비밀번호·token 검사(fail closed)
	AuthOff      = "off"      // 검사 안 함(시험용). 관리자 op 도 열린다
)

// 인증 실패 지연(throttle) 기본값: 같은 이름으로 5번 연속 실패하면 그 이름의 이후 시도는 하나씩 1초 지연.
// 전체 실패가 1분에 100번을 넘으면 실패 응답마다 1초 지연.
const (
	DefaultAuthMaxFailures  = 5
	DefaultAuthDelay        = time.Second
	DefaultGlobalFailLimit  = 100
	DefaultGlobalFailWindow = time.Minute
)

// 자원 한도 기본값.
const (
	DefaultMaxConns          = 256                 // 동시 연결 수(작업자 run 연결 포함)
	DefaultReadTimeout       = 60 * time.Second    // run 이 아닌 연결에서 다음 요청 줄을 기다리는 시간
	DefaultMaxWaitingPerUser = 50                  // 작업자 한 명이 동시에 가질 수 있는 대기 작업 수
	DefaultMaxJobs           = 20000               // 저장하는 작업 전체 수
	DefaultPruneInterval     = time.Hour           // 오래된 중단 작업 정리 주기
	DefaultPruneAge          = 30 * 24 * time.Hour // 이보다 오래된 중단 작업은 지움
)

// 작업 상태 값. store 의 한글 상수(대기/승인/중단)는 한글이 대문자가 아니라서 패키지 밖으로 export 되지 않으므로
// 같은 값을 여기서 export 한다(CLI/TUI 는 이것을 쓴다).
const (
	StatusWaiting  store.Status = "대기"
	StatusApproved store.Status = "승인"
	StatusAborted  store.Status = "중단"
)

// EventStatus 는 run 연결로 밀어주는 이벤트 이름(현재는 이것 하나).
const EventStatus = "status"

// Event.By 에 들어가는 고정 주체(그 밖에는 확인자/작업자 이름이 들어간다).
const (
	BySystem = "system"
	ByAdmin  = "admin"
)

// 중단 사유·이벤트 사유(Job.Reason 과 Event.Reason). 이름이 들어가는 것은 함수로 만든다.
const (
	ReasonSameUser    = "작업자와 확인자가 동일"
	ReasonWorkerGone  = "작업자 스크립트 종료"
	ReasonRestart     = "데몬 재시작"
	ReasonShutdown    = "데몬 종료"
	ReasonAdminSet    = "관리자 변경"
	ReasonAdminDelete = "관리자 삭제"
	ReasonAdminClear  = "관리자 전체 정리"
	ReasonStarted     = "본 작업 시작" // 이벤트 전용(상태는 승인 유지, StartedAt 기록)
)

// (승인 만료 사유 "승인 후 {TTL} 내 미진행" 은 ttl.go 의 ReasonExpired(ttl) 함수로 만든다.)

// ReasonCancel 은 확인자 취소 사유 "확인자 {checker} 가 취소".
func ReasonCancel(checker string) string { return "확인자 " + checker + " 가 취소" }

// reasonWaiting 은 등록 직후 대기 이벤트 사유.
func reasonWaiting(checker string) string { return "확인자 " + checker + " 의 승인 대기" }

// reasonApproved 는 승인 이벤트 사유.
func reasonApproved(checker string) string { return "확인자 " + checker + " 가 승인" }

// 오류 메시지. 인증에 실패한 상대에게는 사용자·작업의 존재 여부를 알 수 없는 문구만 준다.
const (
	MsgAuthFailed = "인증 실패"
	// MsgNoAdmin 은 auth.conf 에 admin 항목이 없어 관리자 op 를 모두 거부할 때.
	MsgNoAdmin      = "관리자(admin) 항목이 auth.conf 에 없어 관리자 기능을 쓸 수 없음 — install.sh 를 실행하거나 root 로 auth.conf 에 'admin=<패스워드>' 줄(6자 이상)을 추가하세요(파일 권한 0600)"
	MsgNoResurrect  = "중단된 작업은 되살릴 수 없습니다"
	msgNoJob        = "작업이 없거나 이 작업의 확인자가 아님"
	msgNotMember    = "작업이 없거나 이 작업의 작업자·확인자가 아님"
	msgTooLarge     = "요청 한 줄이 1 MiB 를 넘음"
	msgTooManyConns = "동시 연결이 너무 많음 — 잠시 후 다시 시도"
)

// ErrLocked 는 AcquireLock 이 이미 다른 프로세스가 잠금을 쥐고 있을 때 돌려준다(--ensure 는 정상 종료하면 된다).
var ErrLocked = errors.New("이미 다른 cross_check 데몬이 실행 중(잠금 사용 중)")

// errLineTooLong 은 한 줄이 한도를 넘었을 때.
var errLineTooLong = errors.New(msgTooLarge)

// Request 는 요청 한 줄. op 마다 쓰는 필드만 채운다.
type Request struct {
	Op          string       `json:"op"`
	ID          string       `json:"id,omitempty"`           // get/approve/cancel/start/admin_set/admin_delete
	User        string       `json:"user,omitempty"`         // run(작업자)/list/get/approve/cancel(호출자)
	Password    string       `json:"password,omitempty"`     // approve/cancel/change_password: User 의 (현재) 비밀번호, register: 새 비밀번호, admin_*: admin 의 비밀번호, get: 선택
	NewPassword string       `json:"new_password,omitempty"` // change_password/admin_set_password: 바꿀 비밀번호
	Token       string       `json:"token,omitempty"`        // start: run 응답으로 받은 1회용 token
	Checker     string       `json:"checker,omitempty"`      // run
	Targets     []string     `json:"targets,omitempty"`      // run
	Status      store.Status `json:"status,omitempty"`       // admin_set
	Reason      string       `json:"reason,omitempty"`       // admin_set
}

// Response 는 응답 한 줄. ok/error 는 항상 들어가고 나머지는 op 에 따라 채운다.
type Response struct {
	OK      bool        `json:"ok"`
	Error   string      `json:"error"`
	TTLSec  int         `json:"ttl_sec,omitempty"`  // ping/run: 이 데몬의 승인 유효시간(초). 구버전 데몬은 안 줌
	ID      string      `json:"id,omitempty"`       // run: 등록된 작업 ID(동일인 중단 때도 기록용으로 채움)
	Token   string      `json:"token,omitempty"`    // run: start 에 쓸 1회용 token(128비트 hex). 작업자에게만 한 번 준다
	Jobs    []JobView   `json:"jobs,omitempty"`     // list
	Job     *JobView    `json:"job,omitempty"`      // get
	AllJobs []store.Job `json:"all_jobs,omitempty"` // admin_list
	Count   int         `json:"count,omitempty"`    // admin_clear: 지운 개수
	// status_user: auth.conf 에 항목이 있는지(다른 op 에서는 빠짐)
	Registered *bool `json:"registered,omitempty"`
}

// Event 는 run 연결로 밀어주는 상태 변화 한 줄.
//
//	{"event":"status","status":"승인","reason":"확인자 bob 가 승인","by":"bob"}
//
// 작업자가 받는 순서: 대기(등록 직후) → 승인 → 승인(본 작업 시작), 또는 어느 시점이든 중단.
type Event struct {
	Event  string       `json:"event"`            // 항상 EventStatus
	Status store.Status `json:"status"`           // 바뀐 뒤의 상태
	Reason string       `json:"reason,omitempty"` // 사람이 읽는 한국어 사유
	By     string       `json:"by,omitempty"`     // 확인자/작업자 이름, BySystem, ByAdmin
	// 승인 이벤트에만: 승인 유효시간(초)과 그 만료 시각(이 시각까지 --start 해야 함). 구버전 클라이언트는 무시한다.
	TTLSec    int        `json:"ttl_sec,omitempty"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
}

// JobView 는 list/get 응답의 작업 한 건. 작업자·확인자(그리고 관리자)만 받으므로 대상 목록을 담는다
// (확인자는 붙여넣기 대신 대상을 보고 비밀번호로 승인한다).
type JobView struct {
	ID          string       `json:"id"`
	User        string       `json:"user"`    // 작업자
	Checker     string       `json:"checker"` // 확인자
	Status      store.Status `json:"status"`
	Reason      string       `json:"reason,omitempty"`
	TargetCount int          `json:"target_count"`
	Targets     []string     `json:"targets"`
	CreatedAt   time.Time    `json:"created_at"`
	ApprovedAt  *time.Time   `json:"approved_at,omitempty"`
	StartedAt   *time.Time   `json:"started_at,omitempty"`
	UpdatedAt   time.Time    `json:"updated_at"`
	// TTLSec 은 이 데몬의 승인 유효시간(초), ExpiresAt 은 승인되었고 아직 시작하지 않은 작업의 만료 시각(승인 시각 + 유효시간).
	// 둘 다 선택 필드(구버전 데몬은 안 줌). 유효시간을 바꾸면(SIGHUP) 이미 승인된 작업에도 새 값이 적용된다.
	TTLSec    int        `json:"ttl_sec,omitempty"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
}

// viewOf 는 저장된 작업에서 JobView 를 만든다(uid 등 감사 필드는 admin_list 에서만 보인다).
// ttl 은 이 데몬의 승인 유효시간(0 이하면 TTLSec/ExpiresAt 을 채우지 않음).
func viewOf(j store.Job, ttl time.Duration) JobView {
	v := JobView{
		ID: j.ID, User: j.User, Checker: j.Checker, Status: j.Status, Reason: j.Reason,
		TargetCount: len(j.Targets), Targets: j.Targets,
		CreatedAt: j.CreatedAt, ApprovedAt: j.ApprovedAt, StartedAt: j.StartedAt, UpdatedAt: j.UpdatedAt,
	}
	if ttl > 0 {
		v.TTLSec = int(ttl / time.Second)
		if j.Status == StatusApproved && j.ApprovedAt != nil && j.StartedAt == nil {
			exp := j.ApprovedAt.Add(ttl)
			v.ExpiresAt = &exp
		}
	}
	return v
}

// ValidName 은 user/checker 이름이 ^[a-z0-9_]+$ 이고 MaxNameLen 이하인지 검사한다.
func ValidName(s string) bool {
	if len(s) == 0 || len(s) > MaxNameLen {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '_') {
			return false
		}
	}
	return true
}

// ValidTarget 은 대상 이름이 ^[A-Za-z0-9._-]+$ 이고 MaxTargetLen 이하인지 검사한다.
func ValidTarget(s string) bool {
	if len(s) == 0 || len(s) > MaxTargetLen {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '.' || c == '_' || c == '-') {
			return false
		}
	}
	return true
}

// validateRun 은 run 요청의 이름·대상을 검사한다. 대상 중복도 거부한다(같은 대상을 두 번 작업하는 실수 방지).
func validateRun(req Request) error {
	if !ValidName(req.User) {
		return fmt.Errorf("작업자 이름이 올바르지 않음(영소문자·숫자·_ 만, %d자 이하): %q", MaxNameLen, clip(req.User))
	}
	if !ValidName(req.Checker) {
		return fmt.Errorf("확인자 이름이 올바르지 않음(영소문자·숫자·_ 만, %d자 이하): %q", MaxNameLen, clip(req.Checker))
	}
	if req.User == AdminUser || req.Checker == AdminUser {
		return errors.New(AdminUser + " 은 관리자용 예약 이름이라 작업자·확인자로 쓸 수 없음")
	}
	if len(req.Targets) == 0 {
		return errors.New("작업 대상이 비어 있음")
	}
	if len(req.Targets) > MaxTargets {
		return fmt.Errorf("작업 대상이 너무 많음(%d개, 최대 %d개)", len(req.Targets), MaxTargets)
	}
	seen := make(map[string]struct{}, len(req.Targets))
	for i, t := range req.Targets {
		if !ValidTarget(t) {
			return fmt.Errorf("%d번째 대상 이름이 올바르지 않음(영문·숫자·. _ - 만): %q", i+1, clip(t))
		}
		if _, dup := seen[t]; dup {
			return fmt.Errorf("작업 대상 중복: %s", t)
		}
		seen[t] = struct{}{}
	}
	return nil
}

// clip 은 오류 메시지에 넣을 사용자 입력을 32바이트로 자른다(1 MiB 짜리 이름을 그대로 돌려주지 않게).
func clip(s string) string {
	if len(s) > 32 {
		return s[:32] + "..."
	}
	return s
}

// readLine 은 \n 까지 한 줄을 읽어 줄바꿈(과 앞의 \r)을 뗀 내용을 돌려준다.
// 내용이 max 바이트를 넘으면 errLineTooLong. 줄바꿈 없이 EOF 를 만나면 그 조각은 버리고 오류를 돌려준다.
func readLine(r *bufio.Reader, max int) ([]byte, error) {
	var buf []byte
	for {
		frag, err := r.ReadSlice('\n')
		if len(buf)+len(frag) > max+1 { // +1 은 끝의 \n
			return nil, errLineTooLong
		}
		buf = append(buf, frag...)
		if err == nil {
			buf = buf[:len(buf)-1]
			if n := len(buf); n > 0 && buf[n-1] == '\r' {
				buf = buf[:n-1]
			}
			return buf, nil
		}
		if err != bufio.ErrBufferFull {
			if err == io.EOF && len(buf) > 0 {
				return nil, io.ErrUnexpectedEOF
			}
			return nil, err
		}
	}
}

// encodeLine 은 v 를 JSON 한 줄(\n 포함)로 만든다.
func encodeLine(v any) ([]byte, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}
