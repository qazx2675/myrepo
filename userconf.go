package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
)

// confDir 은 사용자별 설정파일 폴더다. 리눅스 /etc/awx_copy/, 윈도우 실행파일 옆 conf\.
func confDir() string {
	if runtime.GOOS == "windows" {
		return filepath.Join(exeDir(), "conf")
	}
	return etcDir
}

// rootConfName 은 root 기록 조회용 해시 파일이다. AWX 설정 목록에서 뺀다.
const rootConfName = "root.conf"

// osUserName 은 현재 OS 로그인 사용자 이름이다.
func osUserName() string {
	if runtime.GOOS != "windows" {
		if u := os.Getenv("USER"); u != "" {
			return u
		}
	}
	if u, err := user.Current(); err == nil && u.Username != "" {
		n := u.Username
		if i := strings.LastIndexAny(n, `\/`); i >= 0 { // 윈도우 DOMAIN\user
			n = n[i+1:]
		}
		return n
	}
	return os.Getenv("USERNAME")
}

// confCandidate 는 발견된 설정파일 한 개다. 비밀번호는 읽지 않는다.
type confCandidate struct {
	Path        string
	Name        string // 파일명
	Username    string
	URL         string
	InventoryID string
}

// User 는 설정 이름(파일명에서 .conf 를 뺀 것)이다.
func (c confCandidate) User() string { return strings.TrimSuffix(c.Name, ".conf") }

// peekConfig 는 username, url 만 느슨하게 읽는다 (비밀번호는 읽지 않는다).
func peekConfig(path string) (username, url string, err error) {
	u, l, _, err := peekConfigInv(path)
	return u, l, err
}

// peekConfigInv 는 username, url, inventory_id 를 읽는다. 읽을 수 없으면 오류다.
func peekConfigInv(path string) (username, url, inv string, err error) {
	f, err := os.Open(path)
	if err != nil {
		return "", "", "", err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(strings.TrimPrefix(sc.Text(), "\uFEFF"))
		if line == "" || line[0] == '#' || line[0] == ';' {
			continue
		}
		i := strings.IndexAny(line, "=:")
		if i < 0 {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(line[:i]))
		val := strings.TrimSpace(line[i+1:])
		if len(val) >= 2 && (val[0] == '"' && val[len(val)-1] == '"' || val[0] == '\'' && val[len(val)-1] == '\'') {
			val = val[1 : len(val)-1]
		}
		switch key {
		case "username":
			username = val
		case "url":
			url = strings.TrimRight(val, "/")
		case "inventory_id":
			inv = val
		}
	}
	return username, url, inv, sc.Err()
}

// listCandidates 는 dir 의 *.conf 와 호환용 legacy 파일 중 읽을 수 있는 것을 모은다. root.conf 는 뺀다.
func listCandidates(dir, legacy string) []confCandidate {
	var out []confCandidate
	seen := map[string]bool{}
	add := func(p string) {
		if abs, err := filepath.Abs(p); err == nil {
			p = abs
		}
		if seen[p] {
			return
		}
		st, err := os.Stat(p)
		if err != nil || st.IsDir() || filepath.Base(p) == rootConfName {
			return
		}
		seen[p] = true
		u, url, inv, err := peekConfigInv(p)
		if err != nil { // 읽을 수 없는 파일(다른 사용자 소유 등)은 목록에 넣지 않는다
			return
		}
		out = append(out, confCandidate{Path: p, Name: filepath.Base(p), Username: u, URL: url, InventoryID: inv})
	}
	if m, err := filepath.Glob(filepath.Join(dir, "*.conf")); err == nil {
		sort.Strings(m)
		for _, p := range m {
			add(p)
		}
	}
	if legacy != "" {
		add(legacy)
	}
	return out
}

// selector 는 설정파일 자동 선택에 필요한 환경이다 (테스트에서 바꿀 수 있다).
type selector struct {
	Dir          string
	Legacy       string
	OSUser       string
	AutoOSUser   bool // OS 사용자 이름과 같은 설정을 바로 쓴다 (윈도우만)
	AllowCreate  bool // 메뉴에서 새 설정을 만든다 (윈도우만. 리눅스는 관리자가 배포)
	IsTTY        bool
	In           *bufio.Reader
	Out          io.Writer
	Err          io.Writer
	ReadPassword func(in *bufio.Reader, out io.Writer) (string, error)
	// Choose 가 있으면 번호 입력 대신 이것으로 고른다 (리눅스 TUI). -1 이면 취소.
	Choose func(title string, items []string) (int, error)
}

func newSelector() *selector {
	win := runtime.GOOS == "windows"
	s := &selector{
		Dir: confDir(), Legacy: defaultConfigPath(), OSUser: osUserName(), AutoOSUser: win, AllowCreate: win,
		IsTTY: stdinIsTerminal(), In: stdinR, Out: os.Stdout, Err: os.Stderr, ReadPassword: readPassword,
	}
	if !win && s.IsTTY && stdoutIsTerminal() {
		s.Choose = func(title string, items []string) (int, error) { return ttyChoose(stdinR, os.Stdout, title, items) }
	}
	return s
}

func (s *selector) load(path string) (*Config, int, error) {
	cfg, err := loadConfig(path)
	return cfg, 2, err
}

// resolveConfig 는 사용할 설정을 정한다. 종료 코드는 오류일 때만 의미가 있다.
// 우선순위: --config > --user > OS 사용자 파일 > username 일치 > 선택 메뉴.
func (s *selector) resolveConfig(cfgPath, userName string) (*Config, int, error) {
	if cfgPath != "" {
		return s.load(cfgPath)
	}
	if userName != "" {
		if !validConfName(userName) {
			return nil, 2, fmt.Errorf("--user 이름이 올바르지 않습니다: %q", userName)
		}
		p := filepath.Join(s.Dir, userName+".conf")
		if _, err := os.Stat(p); err == nil {
			return s.load(p)
		}
		cands := listCandidates(s.Dir, s.Legacy)
		for _, c := range cands {
			if c.Username == userName {
				return s.load(c.Path)
			}
		}
		s.printCandidates(s.Err, cands)
		return nil, 2, fmt.Errorf("사용자 %q 의 설정파일이 없습니다 (%s)", userName, p)
	}
	cands := listCandidates(s.Dir, s.Legacy)
	if s.AutoOSUser && s.OSUser != "" && validConfName(s.OSUser) {
		p, _ := filepath.Abs(filepath.Join(s.Dir, s.OSUser+".conf"))
		for _, c := range cands {
			if c.Path == p && c.Username != "" {
				return s.load(c.Path)
			}
		}
		for _, c := range cands {
			if c.Username == s.OSUser {
				return s.load(c.Path)
			}
		}
	}
	if !s.IsTTY {
		fmt.Fprintln(s.Err, "설정파일을 자동으로 고를 수 없고 터미널이 아니라 메뉴를 띄울 수 없습니다.")
		s.printCandidates(s.Err, cands)
		fmt.Fprintf(s.Err, "-u <이름>, --config <경로> 또는 --user <이름> 으로 지정하세요. (설정 폴더: %s)\n", s.Dir)
		return nil, 2, errors.New("설정파일 선택 불가")
	}
	if s.Choose != nil {
		if len(cands) == 0 {
			return nil, 2, fmt.Errorf("읽을 수 있는 설정파일이 없습니다 (설정 폴더: %s). 관리자에게 {사용자}.conf 배포를 요청하세요", s.Dir)
		}
		items := make([]string, len(cands))
		for i, c := range cands {
			items[i] = tuiCandLine(c)
		}
		n, err := s.Choose("사용할 설정을 고르세요 ("+s.Dir+")", items)
		if err != nil {
			return nil, 2, err
		}
		if n < 0 || n >= len(cands) {
			return nil, 2, errors.New("선택을 취소했습니다")
		}
		return s.load(cands[n].Path)
	}
	return s.menu(cands)
}

// tuiCandLine 은 TUI 목록 한 줄이다: user, url, inventory_id. 비밀번호는 읽지 않는다.
func tuiCandLine(c confCandidate) string {
	return fmt.Sprintf("%-20s %-36s inventory_id=%s", c.User(), c.URL, c.InventoryID)
}

// confUser 는 기록에 쓰는 사용자 이름이다 (설정 파일명에서 .conf 를 뺀 것).
func confUser(cfg *Config) string {
	if cfg.Path != "" {
		if n := strings.TrimSuffix(filepath.Base(cfg.Path), ".conf"); n != "" {
			return n
		}
	}
	return cfg.Username
}

func (s *selector) printCandidates(w io.Writer, cands []confCandidate) {
	if len(cands) == 0 {
		fmt.Fprintf(w, "설정파일이 없습니다 (설정 폴더: %s)\n", s.Dir)
		return
	}
	fmt.Fprintln(w, "발견된 설정파일:")
	for i, c := range cands {
		fmt.Fprintln(w, candLine(i+1, c))
	}
}

func candLine(n int, c confCandidate) string {
	u := c.Username
	if u == "" {
		u = "(username 없음)"
	}
	return fmt.Sprintf("  %d) %-24s %-16s %s", n, c.Name, u, c.URL)
}

func (s *selector) ask(prompt string) (string, error) {
	fmt.Fprint(s.Out, prompt)
	line, err := s.In.ReadString('\n')
	if err != nil && line == "" {
		return "", err
	}
	return strings.TrimSpace(line), nil
}

func (s *selector) menu(cands []confCandidate) (*Config, int, error) {
	fmt.Fprintln(s.Out, "사용할 설정을 고르세요.")
	for i, c := range cands {
		fmt.Fprintln(s.Out, candLine(i+1, c))
	}
	newN := len(cands) + 1
	if s.AllowCreate {
		fmt.Fprintf(s.Out, "  %d) 새 설정 만들기\n", newN)
	} else {
		if len(cands) == 0 {
			return nil, 2, fmt.Errorf("읽을 수 있는 설정파일이 없습니다 (설정 폴더: %s)", s.Dir)
		}
		newN = len(cands)
	}
	for try := 0; try < 5; try++ {
		in, err := s.ask("번호: ")
		if err != nil {
			return nil, 2, errors.New("입력이 없어 종료합니다")
		}
		n, err := strconv.Atoi(in)
		if err != nil || n < 1 || n > newN {
			fmt.Fprintf(s.Out, "1 ~ %d 사이의 번호를 입력하세요.\n", newN)
			continue
		}
		if s.AllowCreate && n == newN {
			return s.create()
		}
		return s.load(cands[n-1].Path)
	}
	return nil, 2, errors.New("선택이 올바르지 않아 종료합니다")
}

func validConfName(n string) bool {
	return n != "" && n != "." && n != ".." && !strings.ContainsAny(n, "/\\:*?\"<>|")
}

func (s *selector) create() (*Config, int, error) {
	url, err := s.ask("AWX 주소 (예: http://192.168.0.58:30080): ")
	if err != nil || url == "" {
		return nil, 2, errors.New("url 이 필요합니다")
	}
	name, err := s.ask("username: ")
	if err != nil || !validConfName(name) {
		return nil, 2, errors.New("username 이 올바르지 않습니다")
	}
	fmt.Fprint(s.Out, "password (입력이 보이지 않습니다): ")
	pw, err := s.ReadPassword(s.In, s.Out)
	fmt.Fprintln(s.Out)
	if err != nil || pw == "" {
		return nil, 2, errors.New("password 가 필요합니다")
	}
	idS, err := s.ask("inventory_id: ")
	id, e2 := strconv.Atoi(idS)
	if err != nil || e2 != nil || id <= 0 {
		return nil, 2, errors.New("inventory_id 는 양의 정수여야 합니다")
	}
	path := filepath.Join(s.Dir, name+".conf")
	if _, err := os.Stat(path); err == nil {
		a, _ := s.ask(path + " 가 이미 있습니다. 덮어쓸까요? (y/N): ")
		if a != "y" && a != "Y" {
			return nil, 2, errors.New("저장하지 않고 종료합니다")
		}
	}
	if err := os.MkdirAll(s.Dir, 0o700); err != nil {
		return nil, 2, err
	}
	body := fmt.Sprintf("url = %s\nusername = %s\npassword = %s\ninventory_id = %d\nchunk_size = %d\ntimeout = %d\n",
		url, name, pw, id, defaultChunkSize, defaultTimeout)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		return nil, 2, err
	}
	_ = os.Chmod(path, 0o600)
	fmt.Fprintf(s.Out, "저장했습니다: %s\n", path)
	return s.load(path)
}

// listUsers 는 --list-users 출력이다.
func (s *selector) listUsers(w io.Writer) {
	cands := listCandidates(s.Dir, s.Legacy)
	if len(cands) == 0 {
		fmt.Fprintf(w, "설정파일이 없습니다 (설정 폴더: %s)\n", s.Dir)
		return
	}
	fmt.Fprintf(w, "설정 폴더: %s (현재 OS 사용자: %s, 읽을 수 있는 파일만 표시)\n", s.Dir, s.OSUser)
	s.printCandidates(w, cands)
}
