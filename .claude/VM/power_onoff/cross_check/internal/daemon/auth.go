package daemon

// 사용자별 비밀번호 파일.
//
// 이 서버들은 모두 root 로 로그인하므로 OS 계정(uid)으로는 사람을 구분할 수 없다.
// 그래서 "지정된 확인자만 승인·취소" 는 사용자별 비밀번호로 지킨다.
//
// 파일 형식(기본 /etc/cross_check/auth.conf, root 소유 0600, 관리자가 손으로 편집):
//
//	# 주석, 빈 줄 허용
//	<사용자>=<비밀번호>        ← 첫 번째 '=' 에서 나눈다(비밀번호 안의 '=' 는 그대로)
//	admin=<관리자 비밀번호>    ← 의사 사용자 admin 은 관리자 op 를 지킨다. register 로는 만들 수 없고
//	                             install.sh(또는 root 가 직접)가 쓴다. 없으면 관리자 op 는 모두 거부된다
//
// 파일이 없거나, 그룹/기타 사용자가 읽을 수 있거나(mode & 077 != 0), 형식이 틀리면
// 아무도 인증하지 않는다(fail closed). 파일의 수정 시각·크기가 바뀌면 다음 인증 때 다시 읽는다.
// 비밀번호는 오류 문구·로그·응답 어디에도 넣지 않는다(오류에는 줄 번호와 사용자 이름만).

import (
	"bufio"
	"bytes"
	"crypto/subtle"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// DefaultAuthPath 는 비밀번호 파일 기본 경로.
const DefaultAuthPath = "/etc/cross_check/auth.conf"

// AdminUser 는 관리자 op 를 지키는 의사 사용자 이름.
const AdminUser = "admin"

// AuthStore 는 비밀번호 파일 하나를 읽어 들고 있는 저장소. 모든 메서드는 동시 호출에 안전하다.
// 관리자가 파일을 직접 편집해도 되고, Register/SetPassword 로 데몬이 쓸 수도 있다.
type AuthStore struct {
	// SkipPermCheck 가 true 면 파일 권한(mode & 077) 검사를 건너뛴다.
	// Windows 처럼 Unix 권한이 없는 환경이나 테스트용. Load 전에 정한다.
	SkipPermCheck bool

	mu      sync.Mutex
	path    string
	entries map[string]string
	modTime time.Time // 마지막으로 본 파일의 수정 시각·크기·권한(바뀌었는지 판단용)
	size    int64
	mode    os.FileMode
	loadErr error
	missing bool // loadErr 가 "파일 없음" 때문인지(등록은 새 파일을 만들 수 있음)
	loaded  bool
}

// ErrAlreadyRegistered 는 이미 항목이 있는 사용자를 Register 하려 할 때.
var ErrAlreadyRegistered = errors.New("이미 등록됨")

// 비밀번호 길이 한도(문자 수).
const (
	MinPasswordLen = 6
	MaxPasswordLen = 128
)

// CheckPassword 는 등록·변경할 비밀번호를 검사한다: 6~128자, 줄바꿈·NUL 금지, 앞뒤 공백 금지
// (파일에서 앞뒤 공백을 잘라 읽기 때문). 오류 문구에 비밀번호 내용은 넣지 않는다.
// (관리자가 auth.conf 를 손으로 고친 짧은 비밀번호는 읽을 때 거부하지 않는다. 이 검사는 데몬이 쓸 때만 한다.)
func CheckPassword(pw string) error {
	n := utf8.RuneCountInString(pw)
	if n < MinPasswordLen || n > MaxPasswordLen || !utf8.ValidString(pw) {
		return fmt.Errorf("비밀번호는 %d~%d자여야 함", MinPasswordLen, MaxPasswordLen)
	}
	if strings.ContainsAny(pw, "\r\n\x00") {
		return errors.New("비밀번호에 줄바꿈·NUL 문자를 쓸 수 없음")
	}
	if strings.TrimSpace(pw) != pw {
		return errors.New("비밀번호 앞뒤에 공백을 둘 수 없음")
	}
	return nil
}

// Register 는 user 항목이 아직 없을 때만 user=password 한 줄을 파일 끝에 붙인다(임시파일 → rename, 0600).
// 파일이 없으면 만들고(디렉터리는 0700), 이미 있으면 ErrAlreadyRegistered.
// 파일이 손상(권한 느슨함·형식 오류)되어 있으면 덮어쓰지 않고 거부한다. 같은 사용자를 동시에 등록하면 하나만 성공한다.
func (a *AuthStore) Register(user, password string) error {
	return a.write(user, password, false)
}

// SetPassword 는 user 의 비밀번호를 바꾼다(항목이 없으면 끝에 붙인다). 다른 줄과 주석은 그대로 둔다.
func (a *AuthStore) SetPassword(user, password string) error {
	return a.write(user, password, true)
}

// write 는 Register/SetPassword 공통. mu 를 잡은 채 다시 읽고 → 검사 → 쓰기까지 한 번에 한다.
func (a *AuthStore) write(user, password string, replace bool) error {
	if !ValidName(user) {
		return fmt.Errorf("사용자 이름이 올바르지 않음(영소문자·숫자·_ 만, %d자 이하)", MaxNameLen)
	}
	if err := CheckPassword(password); err != nil {
		return err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.loaded {
		return errors.New("비밀번호 파일을 읽지 않음(Load 필요)")
	}
	a.refreshLocked()
	if a.loadErr != nil && !a.missing {
		return fmt.Errorf("기존 파일을 쓸 수 없어 변경하지 않음: %v", a.loadErr)
	}
	_, exists := a.entries[user]
	if exists && !replace {
		return ErrAlreadyRegistered
	}

	var old []byte
	if !a.missing {
		b, err := os.ReadFile(a.path)
		if err != nil {
			return fmt.Errorf("비밀번호 파일 읽기 실패: %v", err)
		}
		old = b
	}
	newLine := user + "=" + password
	var lines []string
	if len(old) > 0 {
		lines = strings.Split(strings.TrimRight(string(old), "\r\n"), "\n")
	} else {
		lines = []string{"# cross_check 사용자 비밀번호(<사용자>=<비밀번호>). root 소유 0600 유지"}
	}
	done := false
	if exists {
		for i, l := range lines {
			t := strings.TrimSpace(l)
			if strings.HasPrefix(t, "#") {
				continue
			}
			if k := strings.IndexByte(t, '='); k >= 0 && strings.TrimSpace(t[:k]) == user {
				lines[i] = newLine
				done = true
				break
			}
		}
	}
	if !done {
		lines = append(lines, newLine)
	}
	if err := writeFileAtomic(a.path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		return err
	}
	return a.loadLocked()
}

// writeFileAtomic 은 같은 디렉터리에 임시파일(0600)을 쓰고 fsync 후 rename 한다. 디렉터리가 없으면 0700 으로 만든다.
func writeFileAtomic(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("디렉터리 생성 실패: %v", err)
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("임시파일 생성 실패: %v", err)
	}
	name := tmp.Name()
	fail := func(e error) error {
		tmp.Close()
		os.Remove(name)
		return e
	}
	if err := tmp.Chmod(mode); err != nil {
		return fail(fmt.Errorf("임시파일 권한 설정 실패: %v", err))
	}
	if _, err := tmp.Write(data); err != nil {
		return fail(fmt.Errorf("임시파일 쓰기 실패: %v", err))
	}
	if err := tmp.Sync(); err != nil {
		return fail(fmt.Errorf("임시파일 fsync 실패: %v", err))
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return fmt.Errorf("임시파일 닫기 실패: %v", err)
	}
	if err := os.Rename(name, path); err != nil {
		os.Remove(name)
		return fmt.Errorf("파일 교체 실패: %v", err)
	}
	if d, err := os.Open(dir); err == nil {
		d.Sync()
		d.Close()
	}
	return nil
}

// Load 는 path 의 비밀번호 파일을 읽는다. 실패(파일 없음·권한 느슨함·형식 오류)하면
// 그 이유를 돌려주고 저장소는 비운다(이후 Verify 는 모두 false). 이후 파일이 바뀌면 자동으로 다시 읽는다.
func (a *AuthStore) Load(path string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.path = path
	a.loaded = true
	return a.loadLocked()
}

// loadLocked 는 파일을 다시 읽는다. 호출 전에 mu 를 잡아야 한다.
func (a *AuthStore) loadLocked() error {
	a.entries = map[string]string{}
	a.modTime, a.size, a.mode = time.Time{}, -1, 0
	a.loadErr, a.missing = nil, false
	fi, err := os.Stat(a.path)
	if err != nil {
		if os.IsNotExist(err) {
			a.missing = true
			a.loadErr = fmt.Errorf("비밀번호 파일 %s 이 없음 — 먼저 사용자 등록(cross_check --user <이름>)이 필요", a.path)
		} else {
			a.loadErr = fmt.Errorf("비밀번호 파일 %s 확인 실패: %v", a.path, err)
		}
		return a.loadErr
	}
	a.modTime, a.size, a.mode = fi.ModTime(), fi.Size(), fi.Mode()
	if !fi.Mode().IsRegular() {
		a.loadErr = fmt.Errorf("비밀번호 파일 %s 이 일반 파일이 아님", a.path)
		return a.loadErr
	}
	if !a.SkipPermCheck && fi.Mode().Perm()&0o077 != 0 {
		a.loadErr = fmt.Errorf("비밀번호 파일 %s 권한이 %04o 라 다른 사용자가 읽을 수 있음 — chmod 600 후 사용", a.path, fi.Mode().Perm())
		return a.loadErr
	}
	b, err := os.ReadFile(a.path)
	if err != nil {
		a.loadErr = fmt.Errorf("비밀번호 파일 %s 읽기 실패: %v", a.path, err)
		return a.loadErr
	}
	entries, err := parseAuthFile(b)
	if err != nil {
		a.loadErr = fmt.Errorf("비밀번호 파일 %s %v", a.path, err)
		return a.loadErr
	}
	a.entries = entries
	return nil
}

// parseAuthFile 은 파일 내용을 해석한다. 오류 문구에는 줄 번호·사용자 이름만 넣고 비밀번호는 넣지 않는다.
func parseAuthFile(b []byte) (map[string]string, error) {
	out := map[string]string{}
	sc := bufio.NewScanner(bytes.NewReader(b))
	sc.Buffer(make([]byte, 4096), 1<<20)
	ln := 0
	for sc.Scan() {
		ln++
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		i := strings.IndexByte(line, '=')
		if i < 0 {
			return nil, fmt.Errorf("%d번째 줄: <사용자>=<비밀번호> 형식이 아님", ln)
		}
		user, pw := strings.TrimSpace(line[:i]), strings.TrimSpace(line[i+1:])
		if !ValidName(user) {
			return nil, fmt.Errorf("%d번째 줄: 사용자 이름이 올바르지 않음(영소문자·숫자·_ 만)", ln)
		}
		if pw == "" {
			return nil, fmt.Errorf("%d번째 줄(%s): 비밀번호가 비어 있음", ln, user)
		}
		if _, dup := out[user]; dup {
			return nil, fmt.Errorf("%d번째 줄: 사용자 %s 가 두 번 나옴", ln, user)
		}
		out[user] = pw
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("읽기 오류: %v", err)
	}
	return out, nil
}

// refreshLocked 는 파일의 수정 시각·크기·권한이 바뀌었거나 지난번에 실패했으면 다시 읽는다. mu 를 잡고 부른다.
func (a *AuthStore) refreshLocked() {
	if !a.loaded {
		return
	}
	fi, err := os.Stat(a.path)
	if err != nil || a.loadErr != nil || !fi.ModTime().Equal(a.modTime) || fi.Size() != a.size || fi.Mode() != a.mode {
		a.loadLocked()
	}
}

// Err 는 지금 파일 상태로 인증할 수 없는 이유(없으면 nil). 비밀번호는 담지 않는다.
func (a *AuthStore) Err() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.loaded {
		return fmt.Errorf("비밀번호 파일을 읽지 않음(Load 필요)")
	}
	a.refreshLocked()
	return a.loadErr
}

// Has 는 user 항목이 있는지 알려준다(파일을 쓸 수 없는 상태면 false).
func (a *AuthStore) Has(user string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.refreshLocked()
	_, ok := a.entries[user]
	return ok
}

// Registered 는 user 항목이 있는지 알려준다. 파일이 없으면 (false, nil),
// 파일이 손상(권한 느슨함·형식 오류)되어 판단할 수 없으면 (false, 그 이유).
func (a *AuthStore) Registered(user string) (bool, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.loaded {
		return false, errors.New("비밀번호 파일을 읽지 않음(Load 필요)")
	}
	a.refreshLocked()
	if a.loadErr != nil && !a.missing {
		return false, a.loadErr
	}
	_, ok := a.entries[user]
	return ok, nil
}

// Users 는 등록된 사용자 이름을 정렬해 돌려준다(--admin 화면용, 비밀번호는 주지 않음).
func (a *AuthStore) Users() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.refreshLocked()
	out := make([]string, 0, len(a.entries))
	for u := range a.entries {
		out = append(out, u)
	}
	sort.Strings(out)
	return out
}

// Verify 는 user 의 비밀번호가 password 와 같은지 상수 시간 비교로 확인한다.
// 항목이 없거나 password 가 비었거나 파일을 쓸 수 없는 상태면 false.
func (a *AuthStore) Verify(user, password string) bool {
	a.mu.Lock()
	a.refreshLocked()
	want, ok := a.entries[user]
	a.mu.Unlock()
	if !ok || password == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(want), []byte(password)) == 1
}

// throttleKeep 은 마지막 실패 뒤 이만큼 실패가 없으면 그 이름의 연속 실패 기록을 지운다.
const throttleKeep = 10 * time.Minute

// throttle 은 비밀번호 추측을 늦춘다. 잠그지 않는다(잠그면 남이 일부러 틀려 정당한 사용자를 막을 수 있음).
//
//   - 이름별: 연속 실패가 max 번 이상이면 그 이름의 다음 시도들은 이름별 관문(gate)을 하나씩 통과하며
//     delay 만큼 기다린 뒤 검사한다(초당 1번 꼴로만 추측 가능). 올바른 비밀번호는 기다린 뒤 통과하고 횟수를 지운다.
//   - 전체: window 안의 실패가 globalLimit 번을 넘으면 실패 응답마다 전체 관문을 하나씩 통과하며 delay 만큼 늦춘다.
//
// 기다림(sleep)은 t.mu 도 서버 mu 도 잡지 않은 채로 한다. 이름별 기록은 호출자가 track=true 로 넘긴
// 이름(users= 목록과 admin)만 남기므로 아무 이름이나 보내도 맵이 커지지 않는다.
type throttle struct {
	max         int
	delay       time.Duration
	globalLimit int
	window      time.Duration
	sleep       func(time.Duration)

	mu     sync.Mutex
	users  map[string]*userThrottle
	recent []time.Time   // window 안의 전체 실패 시각(오래된 것부터)
	global chan struct{} // 전체 지연 관문(크기 1)
}

type userThrottle struct {
	count int           // 연속 실패 횟수
	last  time.Time     // 마지막 실패 시각
	gate  chan struct{} // 이름별 지연 관문(크기 1)
	refs  int           // 관문을 쓰는 중이거나 기다리는 시도 수(0 일 때만 지울 수 있음)
}

func newThrottle(max int, delay time.Duration, globalLimit int, window time.Duration, sleep func(time.Duration)) *throttle {
	return &throttle{
		max: max, delay: delay, globalLimit: globalLimit, window: window, sleep: sleep,
		users: map[string]*userThrottle{}, global: make(chan struct{}, 1),
	}
}

func (t *throttle) entryLocked(user string) *userThrottle {
	u, ok := t.users[user]
	if !ok {
		u = &userThrottle{gate: make(chan struct{}, 1)}
		t.users[user] = u
	}
	return u
}

// begin 은 비밀번호 검사 직전에 부른다. user 가 연속 실패 max 번 이상이면 이름별 관문을 잡고 delay 만큼 기다린다.
// 돌려준 함수(관문 놓기)는 검사가 끝나면 반드시 부른다. track=false 이면 아무것도 하지 않는다.
func (t *throttle) begin(user string, track bool) (release func()) {
	if !track {
		return func() {}
	}
	t.mu.Lock()
	u, ok := t.users[user]
	if !ok || u.count < t.max {
		t.mu.Unlock()
		return func() {}
	}
	u.refs++
	t.mu.Unlock()

	u.gate <- struct{}{} // 같은 이름의 지연 시도는 한 번에 하나씩
	t.sleep(t.delay)
	return func() {
		<-u.gate
		t.mu.Lock()
		u.refs--
		t.mu.Unlock()
	}
}

// failed 는 실패를 기록한다(track 이면 이름별 연속 횟수도). 전체 실패가 한도를 넘었으면 true
// (호출자는 응답 전에 globalDelay 를 부른다).
func (t *throttle) failed(user string, now time.Time, track bool) (overGlobal bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if track {
		u := t.entryLocked(user)
		u.count++
		u.last = now
	}
	t.trimLocked(now)
	t.recent = append(t.recent, now)
	return len(t.recent) > t.globalLimit
}

// trimLocked 는 window 보다 오래된 전체 실패 기록을 버린다.
func (t *throttle) trimLocked(now time.Time) {
	i := 0
	for i < len(t.recent) && now.Sub(t.recent[i]) >= t.window {
		i++
	}
	if i > 0 {
		t.recent = append(t.recent[:0], t.recent[i:]...)
	}
}

// globalDelay 는 전체 관문을 하나씩 통과하며 delay 만큼 기다린다(실패 응답을 늦춤).
func (t *throttle) globalDelay() {
	t.global <- struct{}{}
	t.sleep(t.delay)
	<-t.global
}

// ok 는 성공(또는 관리자 재설정) 시 그 이름의 연속 실패 횟수를 지운다.
func (t *throttle) ok(user string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if u, found := t.users[user]; found {
		if u.refs == 0 {
			delete(t.users, user)
		} else {
			u.count = 0
		}
	}
}

// failures 는 user 의 연속 실패 횟수(테스트·로그용).
func (t *throttle) failures(user string) int {
	t.mu.Lock()
	defer t.mu.Unlock()
	if u, found := t.users[user]; found {
		return u.count
	}
	return 0
}

// prune 은 마지막 실패가 throttleKeep 보다 오래됐고 아무도 쓰지 않는 이름별 기록과 오래된 전체 기록을 지운다.
func (t *throttle) prune(now time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for name, u := range t.users {
		if u.refs == 0 && now.Sub(u.last) > throttleKeep {
			delete(t.users, name)
		}
	}
	t.trimLocked(now)
}
