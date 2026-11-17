package main

// 업로드 기록: /tmp/.awx_copy/{user}.history.jsonl (디렉터리 1777, 파일 600).
// root 기록 조회: /etc/awx_copy/root.conf 의 SHA-256 해시와 비교한다.

import (
	"bufio"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	resultSuccess = "성공"
	resultFailed  = "실패"
	resultAborted = "중단"
)

// histRec 는 기록 한 줄이다. 비밀번호는 담지 않는다.
type histRec struct {
	TS      string  `json:"ts"`
	User    string  `json:"user"`
	File    string  `json:"file"`
	Type    string  `json:"type"`
	Size    int64   `json:"size"`
	Chunks  int     `json:"chunks"`
	Seconds float64 `json:"seconds"`
	Result  string  `json:"result"`
	Message string  `json:"message"`
}

func histFile(dir, user string) string {
	return filepath.Join(dir, sanitizeFileName(user)+".history.jsonl")
}

// ensureHistDir 는 공용 기록 폴더를 만든다. 내가 만든(또는 소유한) 경우 1777 로 맞춘다.
func ensureHistDir(dir string) error {
	fi, err := os.Lstat(dir)
	if os.IsNotExist(err) {
		if err := os.MkdirAll(dir, 0o777|os.ModeSticky); err != nil {
			return fmt.Errorf("기록 폴더 생성 실패: %v", err)
		}
		if fi, err = os.Lstat(dir); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	if !fi.IsDir() || fi.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("기록 폴더 %s 가 디렉터리가 아닙니다", dir)
	}
	if uid, ok := fileUID(fi); ok && uid == euid() {
		_ = os.Chmod(dir, 0o777|os.ModeSticky)
	}
	return nil
}

// appendHistory 는 기록 한 줄을 덧붙인다. 심볼릭 링크와 남의 파일에는 쓰지 않는다.
func appendHistory(dir string, r histRec) error {
	if err := ensureHistDir(dir); err != nil {
		return err
	}
	p := histFile(dir, r.User)
	if fi, err := os.Lstat(p); err == nil && fi.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("기록 파일이 심볼릭 링크입니다: %s", p)
	}
	f, err := os.OpenFile(p, os.O_WRONLY|os.O_APPEND|os.O_CREATE|oNoFollow, 0o600)
	if err != nil {
		return fmt.Errorf("기록 파일 열기 실패: %v", err)
	}
	defer f.Close()
	if fi, err := f.Stat(); err == nil {
		if uid, ok := fileUID(fi); ok && uid != euid() && euid() != 0 {
			return fmt.Errorf("기록 파일 %s 의 소유자가 다릅니다", p)
		}
		if !fi.Mode().IsRegular() {
			return fmt.Errorf("기록 파일이 일반 파일이 아닙니다: %s", p)
		}
	}
	_ = f.Chmod(0o600)
	line, err := json.Marshal(r)
	if err != nil {
		return err
	}
	_, err = f.Write(append(line, '\n'))
	return err
}

// readHistoryFile 은 기록 파일 하나를 읽는다. 깨진 줄은 건너뛴다.
func readHistoryFile(p string) ([]histRec, error) {
	f, err := os.Open(p)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []histRec
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 1<<20)
	for sc.Scan() {
		var r histRec
		if json.Unmarshal(sc.Bytes(), &r) == nil && r.TS != "" {
			out = append(out, r)
		}
	}
	return out, sc.Err()
}

func sortRecent(rs []histRec) {
	sort.SliceStable(rs, func(i, j int) bool { return rs[i].TS > rs[j].TS })
}

// loadHistory 는 한 사용자의 기록이다 (최근 순).
func loadHistory(dir, user string) ([]histRec, error) {
	rs, err := readHistoryFile(histFile(dir, user))
	if os.IsNotExist(err) {
		return nil, nil
	}
	sortRecent(rs)
	return rs, err
}

// loadAllHistory 는 모든 사용자 기록을 모은다 (최근 순). 읽지 못한 파일 수도 돌려준다.
func loadAllHistory(dir string) ([]histRec, int) {
	m, _ := filepath.Glob(filepath.Join(dir, "*.history.jsonl"))
	var all []histRec
	bad := 0
	for _, p := range m {
		rs, err := readHistoryFile(p)
		if err != nil {
			bad++
		}
		all = append(all, rs...)
	}
	sortRecent(all)
	return all, bad
}

func humanSize(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1fMiB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1fKiB", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%dB", n)
}

// historyRows 는 표 머리와 줄을 만든다. withUser 면 사용자 열을 넣는다.
func historyRows(rs []histRec, withUser bool) (string, []string) {
	cols := func(ts, user, file, typ, size, chunks, sec, res, msg string) string {
		s := fit(ts, 19) + " "
		if withUser {
			s += fit(user, 14) + " "
		}
		return s + fit(file, 24) + " " + fit(typ, 4) + " " + fit(size, 9) + " " + fit(chunks, 5) + " " + fit(sec, 7) + " " + fit(res, 4) + " " + msg
	}
	header := cols("시각", "사용자", "파일", "형식", "크기", "조각", "초", "결과", "메시지")
	rows := make([]string, 0, len(rs))
	for _, r := range rs {
		ts := r.TS
		if t, err := time.Parse(time.RFC3339, r.TS); err == nil {
			ts = t.Local().Format("2006-01-02 15:04:05")
		}
		rows = append(rows, cols(ts, r.User, filepath.Base(r.File), r.Type, humanSize(r.Size),
			fmt.Sprint(r.Chunks), fmt.Sprintf("%.1f", r.Seconds), r.Result, r.Message))
	}
	return header, rows
}

// --- root 기록 조회 ---

// rootHashPrefix 는 root.conf 의 키다: password_sha256 = <hex>
const rootHashKey = "password_sha256"

func hashPassword(pw string) string {
	s := sha256.Sum256([]byte(pw))
	return hex.EncodeToString(s[:])
}

// checkRootPassword 는 SHA-256 을 상수 시간으로 비교한다.
func checkRootPassword(pw, wantHex string) bool {
	want, err := hex.DecodeString(strings.TrimSpace(wantHex))
	if err != nil || len(want) != sha256.Size {
		return false
	}
	got := sha256.Sum256([]byte(pw))
	return subtle.ConstantTimeCompare(got[:], want) == 1
}

// readRootHash 는 root.conf 에서 해시를 읽는다. 파일은 root 소유 600 이어야 한다.
func readRootHash(path string) (string, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("%s 를 읽을 수 없습니다 (root 권한 필요, install.sh 로 만듭니다)", path)
	}
	if err := checkOwnerPerm(path, fi, true); err != nil {
		return "", fmt.Errorf("%s: %v", path, err)
	}
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("%s 를 읽을 수 없습니다 (root 권한 필요)", path)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if i := strings.IndexAny(line, "=:"); i > 0 && strings.TrimSpace(line[:i]) == rootHashKey {
			return strings.TrimSpace(line[i+1:]), nil
		}
	}
	return "", fmt.Errorf("%s 에 %s 가 없습니다", path, rootHashKey)
}

var errRootAuth = errors.New("root 비밀번호가 3회 틀렸습니다")

// rootLogin 은 비밀번호를 최대 3회 받는다. 비밀번호는 어디에도 출력하지 않는다.
func rootLogin(in *bufio.Reader, out io.Writer, readPw func(*bufio.Reader, io.Writer) (string, error), wantHex string) error {
	for try := 1; try <= 3; try++ {
		fmt.Fprint(out, "root 비밀번호 (기록 조회용, 입력이 보이지 않습니다): ")
		pw, err := readPw(in, out)
		fmt.Fprintln(out)
		if err != nil {
			return errors.New("입력이 없어 종료합니다")
		}
		if checkRootPassword(pw, wantHex) {
			return nil
		}
		fmt.Fprintf(out, "비밀번호가 틀렸습니다 (%d/3)\n", try)
	}
	return errRootAuth
}

// writeRootHash 는 root.conf 를 새 해시로 바꾼다 (root 만).
func writeRootHash(path, pw string) error {
	body := "# awx_copy 기록 조회용 root 비밀번호 SHA-256 (평문 저장 금지)\n" + rootHashKey + " = " + hashPassword(pw) + "\n"
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(body), 0o600); err != nil {
		return err
	}
	_ = os.Chmod(tmp, 0o600)
	return os.Rename(tmp, path)
}
