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

// confDir 은 사용자별 설정파일 폴더다. 리눅스 ~/.awx_copy/, 윈도우 실행파일 옆 conf\.
func confDir() string {
	if runtime.GOOS == "windows" {
		return filepath.Join(exeDir(), "conf")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ".awx_copy"
	}
	return filepath.Join(home, ".awx_copy")
}

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
	Path     string
	Name     string // 파일명
	Username string
	URL      string
}

// peekConfig 는 username, url 만 느슨하게 읽는다 (비밀번호는 읽지 않는다).
func peekConfig(path string) (username, url string, err error) {
	f, err := os.Open(path)
	if err != nil {
		return "", "", err
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
		}
	}
	return username, url, sc.Err()
}

// listCandidates 는 dir 의 *.conf 와 호환용 legacy 파일을 모은다.
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
		if err != nil || st.IsDir() {
			return
		}
		seen[p] = true
		u, url, _ := peekConfig(p)
		out = append(out, confCandidate{Path: p, Name: filepath.Base(p), Username: u, URL: url})
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
	IsTTY        bool
	In           *bufio.Reader
	Out          io.Writer
	Err          io.Writer
	ReadPassword func(in *bufio.Reader, out io.Writer) (string, error)
}

func newSelector() *selector {
	return &selector{
		Dir: confDir(), Legacy: defaultConfigPath(), OSUser: osUserName(),
		IsTTY: stdinIsTerminal(), In: bufio.NewReader(os.Stdin), Out: os.Stdout, Err: os.Stderr, ReadPassword: readPassword,
	}
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
	if s.OSUser != "" && validConfName(s.OSUser) {
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
		fmt.Fprintf(s.Err, "--config <경로> 또는 --user <이름> 으로 지정하세요. (설정 폴더: %s)\n", s.Dir)
		return nil, 2, errors.New("설정파일 선택 불가")
	}
	return s.menu(cands)
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
	fmt.Fprintf(s.Out, "  %d) 새 설정 만들기\n", newN)
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
		if n == newN {
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
	fmt.Fprintf(w, "설정 폴더: %s (현재 OS 사용자: %s)\n", s.Dir, s.OSUser)
	s.printCandidates(w, cands)
}
