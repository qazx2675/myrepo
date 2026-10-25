package daemon

// 데몬 설정 파일과 사용자별 대상 파일.
//
// 설정 파일(기본 /etc/cross_check/cross_check.conf):
//
//	# 주석
//	target_dir=/etc/vcetner_pwon_off/targets   ← 사용자별 on_off_<user>.txt 가 있는 절대 경로(auth=required 면 필수)
//	auth=required                              ← required(기본) | off
//	users=hong,kim,lee                         ← 작업자·확인자로 쓸 수 있는 이름(쉼표 구분, 영소문자·숫자·_,
//	                                             admin 불가). auth=required 면 필수. 목록 밖 이름은 등록·작업·승인 불가
//	approval_ttl_sec=120                       ← 승인 후 start 허용 시간(초, 10~3600, 기본 120 = 2분). 바꾸면 데몬에 SIGHUP
//	                                             (pkill -HUP -f 'cross_check --daemon') 또는 재시작으로 반영
//	work_timeout_sec=86400                     ← 본 작업(작업중) 제한 시간(초, 600~604800, 기본 86400 = 24시간). 이보다 오래
//	                                             --finish 가 없으면 승인/작업중단("작업자 응답 없음"). SIGHUP 으로도 반영
//
// 대상 파일(<target_dir>/on_off_<user>.txt, vm_power_onoff 와 같은 형식): 한 줄에 이름 하나, '#' 주석·빈 줄 무시.
// run 의 대상은 이 파일에 있는 이름의 부분집합이어야 한다(작업자는 자기에게 등록된 대상만 작업).

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// DefaultConfPath 는 데몬 설정 파일 기본 경로.
const DefaultConfPath = "/etc/cross_check/cross_check.conf"

// Conf 는 설정 파일 내용.
type Conf struct {
	TargetDir   string        // target_dir
	AuthMode    string        // auth: AuthRequired | AuthOff
	Users       []string      // users: 허용 사용자 이름(파일에 적힌 순서)
	ApprovalTTL time.Duration // approval_ttl_sec
	WorkTimeout time.Duration // work_timeout_sec
}

// ParseUsers 는 users= 값(쉼표 구분)을 이름 목록으로 바꾼다. 각 이름은 ValidName 이고 admin 이 아니며 중복이 없어야 한다.
// 빈 항목(예: "a,,b")도 오류다.
func ParseUsers(v string) ([]string, error) {
	if strings.TrimSpace(v) == "" {
		return nil, errors.New("users 가 비어 있음")
	}
	var out []string
	seen := map[string]bool{}
	for _, f := range strings.Split(v, ",") {
		name := strings.TrimSpace(f)
		if err := checkAllowedName(name); err != nil {
			return nil, err
		}
		if seen[name] {
			return nil, fmt.Errorf("users 에 %s 가 두 번 나옴", name)
		}
		seen[name] = true
		out = append(out, name)
	}
	return out, nil
}

// checkAllowedName 은 users 목록에 넣을 수 있는 이름인지 검사한다.
func checkAllowedName(name string) error {
	if !ValidName(name) {
		return fmt.Errorf("users 의 이름이 올바르지 않음(영소문자·숫자·_ 만, %d자 이하): %q", MaxNameLen, clip(name))
	}
	if name == AdminUser {
		return errors.New("users 에 " + AdminUser + " 를 넣을 수 없음(관리자용 예약 이름)")
	}
	return nil
}

// LoadConf 는 설정 파일을 읽는다. 파일이 없으면 기본값(auth=required, 120초)으로 보지만,
// auth=required 인데 target_dir 나 users 가 없으면 오류다(fail closed). 모르는 키·중복 키·형식 오류도 오류.
func LoadConf(path string) (Conf, error) {
	c := Conf{AuthMode: AuthRequired, ApprovalTTL: DefaultApprovalTTL, WorkTimeout: DefaultWorkTimeout}
	b, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return c, fmt.Errorf("설정 파일 %s 읽기 실패: %v", path, err)
	}
	seen := map[string]int{}
	sc := bufio.NewScanner(bytes.NewReader(b))
	ln := 0
	for sc.Scan() {
		ln++
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		i := strings.IndexByte(line, '=')
		if i <= 0 {
			return c, fmt.Errorf("설정 파일 %s %d번째 줄: <키>=<값> 형식이 아님", path, ln)
		}
		k, v := strings.TrimSpace(line[:i]), strings.TrimSpace(line[i+1:])
		if prev, dup := seen[k]; dup {
			return c, fmt.Errorf("설정 파일 %s %d번째 줄: %s 가 중복됨(처음 %d번째 줄)", path, ln, k, prev)
		}
		seen[k] = ln
		switch k {
		case "target_dir":
			if !filepath.IsAbs(v) && !strings.HasPrefix(v, "/") {
				return c, fmt.Errorf("설정 파일 %s %d번째 줄: target_dir 는 절대 경로여야 함: %q", path, ln, v)
			}
			c.TargetDir = filepath.Clean(v)
		case "auth":
			if v != AuthRequired && v != AuthOff {
				return c, fmt.Errorf("설정 파일 %s %d번째 줄: auth 는 %s 또는 %s: %q", path, ln, AuthRequired, AuthOff, v)
			}
			c.AuthMode = v
		case "users":
			users, err := ParseUsers(v)
			if err != nil {
				return c, fmt.Errorf("설정 파일 %s %d번째 줄: %v", path, ln, err)
			}
			c.Users = users
		case "approval_ttl_sec":
			n, err := strconv.Atoi(v)
			if min := minTTLSec(); err != nil || n < min || n > MaxApprovalTTLSec {
				return c, fmt.Errorf("설정 파일 %s %d번째 줄: approval_ttl_sec 는 %d~%d 사이의 정수(초)여야 함(기본 %d = %s): %q",
					path, ln, min, MaxApprovalTTLSec, int(DefaultApprovalTTL/time.Second), FormatTTLDuration(DefaultApprovalTTL), clip(v))
			}
			c.ApprovalTTL = time.Duration(n) * time.Second
		case "work_timeout_sec":
			n, err := strconv.Atoi(v)
			if min := minWorkTimeoutSec(); err != nil || n < min || n > MaxWorkTimeoutSec {
				return c, fmt.Errorf("설정 파일 %s %d번째 줄: work_timeout_sec 는 %d~%d 사이의 정수(초)여야 함(기본 %d = %s): %q",
					path, ln, min, MaxWorkTimeoutSec, int(DefaultWorkTimeout/time.Second), FormatTTLDuration(DefaultWorkTimeout), clip(v))
			}
			c.WorkTimeout = time.Duration(n) * time.Second
		default:
			return c, fmt.Errorf("설정 파일 %s %d번째 줄: 알 수 없는 키 %q", path, ln, k)
		}
	}
	if err := sc.Err(); err != nil {
		return c, fmt.Errorf("설정 파일 %s 읽기 실패: %v", path, err)
	}
	if c.AuthMode == AuthRequired && c.TargetDir == "" {
		return c, fmt.Errorf("설정 파일 %s 에 target_dir 가 없음 — auth=required 에서는 사용자별 대상 파일 위치가 필수", path)
	}
	if c.AuthMode == AuthRequired && len(c.Users) == 0 {
		return c, fmt.Errorf("설정 파일 %s 에 users 가 없음 — auth=required 에서는 users=<이름,이름,...> 로 작업자·확인자 이름 목록이 필수", path)
	}
	return c, nil
}

// Apply 는 설정 파일 값을 데몬 Config 에 옮긴다(소켓·저장소 경로 등 나머지는 그대로).
func (c Conf) Apply(cfg *Config) {
	cfg.TargetDir = c.TargetDir
	cfg.AuthMode = c.AuthMode
	cfg.AllowedUsers = append([]string(nil), c.Users...)
	cfg.ApprovalTTL = c.ApprovalTTL
	cfg.WorkTimeout = c.WorkTimeout
}

// TargetFilePath 는 user 의 대상 파일 경로 <dir>/on_off_<user>.txt.
func TargetFilePath(dir, user string) string {
	return filepath.Join(dir, "on_off_"+user+".txt")
}

// LoadUserTargets 는 user 의 대상 파일을 읽어 이름 집합을 돌려준다(매번 새로 읽음, 캐시 없음).
// 심볼릭 링크·일반 파일 아님·없음·비어 있음·이름 규칙 위반은 오류.
func LoadUserTargets(dir, user string) (map[string]bool, error) {
	if !ValidName(user) {
		return nil, fmt.Errorf("사용자 이름이 올바르지 않음: %q", clip(user))
	}
	path := TargetFilePath(dir, user)
	fi, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("작업자 %s 의 대상 파일이 없음: %s", user, path)
		}
		return nil, fmt.Errorf("대상 파일 확인 실패: %v", err)
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("대상 파일이 심볼릭 링크라 거부: %s", path)
	}
	if !fi.Mode().IsRegular() {
		return nil, fmt.Errorf("대상 파일이 일반 파일이 아님: %s", path)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("대상 파일 읽기 실패: %v", err)
	}
	set := map[string]bool{}
	var bad []string
	sc := bufio.NewScanner(bytes.NewReader(b))
	sc.Buffer(make([]byte, 4096), 1<<20)
	ln := 0
	for sc.Scan() {
		ln++
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if !ValidTarget(line) {
			bad = append(bad, fmt.Sprintf("%d번째 줄 %q", ln, clip(line)))
			continue
		}
		set[line] = true
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("대상 파일 읽기 실패: %v", err)
	}
	if len(bad) > 0 {
		return nil, fmt.Errorf("대상 파일 %s 에 이름 규칙(영문·숫자·. _ -) 위반: %s", path, joinCapped(bad, 10))
	}
	if len(set) == 0 {
		return nil, fmt.Errorf("작업자 %s 의 대상 파일이 비어 있음: %s", user, path)
	}
	return set, nil
}

// checkOwnTargets 는 targets 가 user 대상 파일의 부분집합인지 확인한다. 아니면 벗어난 이름을 담은 오류.
func checkOwnTargets(dir, user string, targets []string) error {
	if dir == "" {
		return errors.New("target_dir 가 설정되지 않아 작업 대상을 확인할 수 없음")
	}
	allowed, err := LoadUserTargets(dir, user)
	if err != nil {
		return err
	}
	var out []string
	for _, t := range targets {
		if !allowed[t] {
			out = append(out, t)
		}
	}
	if len(out) > 0 {
		return fmt.Errorf("작업자 %s 에게 등록되지 않은 대상 %d개(%s): %s",
			user, len(out), filepath.Base(TargetFilePath(dir, user)), joinCapped(out, 20))
	}
	return nil
}

// joinCapped 는 처음 max 개만 ", " 로 잇고 나머지는 "외 N개" 로 줄인다.
func joinCapped(list []string, max int) string {
	if len(list) <= max {
		return strings.Join(list, ", ")
	}
	return strings.Join(list[:max], ", ") + fmt.Sprintf(" 외 %d개", len(list)-max)
}
