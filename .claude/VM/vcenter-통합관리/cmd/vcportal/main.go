// vcportal 은 포털의 "vCenter에서 열기" 링크(vcportal://open?url=<딥링크>)를 받아
// 전용 프로필 Edge/Chrome 에서 vCenter 에 자동 로그인한 뒤 딥링크 화면을 연다.
//
// 사용법:
//
//	vcportal.exe "vcportal://open?url=<인코딩된 딥링크>"
//	vcportal.exe --url <딥링크> [--conf <vcportal.conf 경로>]
//
// conf 기본 위치: <exe 폴더>\..\config\vcportal.conf
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/chromedp/cdproto/target"
	"github.com/chromedp/chromedp"

	"vcportal/internal/conf"
)

// vSphere Client 8.0.3 SSO 로그인 화면 선택자 (업그레이드로 화면이 바뀌면 여기만 수정)
const (
	ssoPathMark = "/websso/"          // SSO 로그인 페이지 URL 표식
	selUsername = "#username"         // 사용자 이름 입력칸
	selPassword = "#password"         // 비밀번호 입력칸
	selSubmit   = "#submit"           // 로그인 버튼
	selLoginErr = "#response"         // 로그인 실패 메시지 영역 (실패 시 표시됨)
	selLoggedIn = "header .user-menu" // 로그인된 vSphere Client 상단 헤더의 사용자 메뉴
)

const (
	debugPort    = 9333             // 전용 브라우저 원격 디버깅 포트 (127.0.0.1 전용)
	totalTimeout = 60 * time.Second // 딥링크 이동 + 로그인 전체 제한 시간
	startTimeout = 20 * time.Second // 브라우저 기동 대기 시간
)

var debugBase = fmt.Sprintf("http://127.0.0.1:%d", debugPort)

func main() {
	if err := run(os.Args[1:]); err != nil {
		logf("오류: %v", err)
		showMessage("vCenter 포털 런처", err.Error(), true)
		os.Exit(1)
	}
}

func run(args []string) error {
	fs := flag.NewFlagSet("vcportal", flag.ContinueOnError)
	fs.SetOutput(new(strings.Builder))
	deep := fs.String("url", "", "열 vCenter 딥링크 (https://...)")
	confPath := fs.String("conf", "", "vcportal.conf 경로 (기본: <exe 폴더>\\..\\config\\vcportal.conf)")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("잘못된 실행 인자입니다: %v", err)
	}
	if *deep == "" && fs.NArg() > 0 {
		v, err := fromProtocolURL(fs.Arg(0))
		if err != nil {
			return err
		}
		*deep = v
	}
	if *deep == "" {
		return errors.New("열 주소가 없습니다.\n사용법: vcportal.exe \"vcportal://open?url=<딥링크>\" 또는 vcportal.exe --url <딥링크>")
	}
	if *confPath == "" {
		exe, err := os.Executable()
		if err != nil {
			return fmt.Errorf("실행 파일 위치를 알 수 없습니다: %v", err)
		}
		*confPath = filepath.Join(filepath.Dir(exe), "..", "config", "vcportal.conf")
	}
	cfg, err := conf.Load(*confPath)
	if err != nil {
		return fmt.Errorf("%v\n(conf: %s)", err, *confPath)
	}
	target, err := checkTarget(cfg, *deep)
	if err != nil {
		return err
	}
	logf("요청: %s", target)
	return open(cfg, target)
}

// fromProtocolURL 은 vcportal://open?url=<인코딩된 딥링크> 에서 딥링크를 꺼낸다.
func fromProtocolURL(s string) (string, error) {
	u, err := url.Parse(s)
	if err != nil || !strings.EqualFold(u.Scheme, "vcportal") {
		return "", fmt.Errorf("vcportal:// 링크가 아닙니다: %s", s)
	}
	v := u.Query().Get("url")
	if v == "" {
		return "", fmt.Errorf("링크에 url 값이 없습니다: %s", s)
	}
	return v, nil
}

// checkTarget 은 https + conf 에 등록된 vCenter 호스트 + /ui/ 경로인 주소만 허용한다.
func checkTarget(cfg *conf.Config, s string) (string, error) {
	u, err := url.Parse(s)
	if err != nil {
		return "", fmt.Errorf("주소를 해석할 수 없습니다: %s", s)
	}
	if u.Scheme != "https" {
		return "", fmt.Errorf("https 주소만 열 수 있습니다: %s", s)
	}
	if u.User != nil {
		return "", fmt.Errorf("사용자 정보(@)가 포함된 주소는 열 수 없습니다: %s", s)
	}
	if cfg.VCenterByHost(u.Host) == nil {
		return "", fmt.Errorf("conf [vcenters] 에 등록되지 않은 호스트라 열지 않습니다: %s", u.Host)
	}
	if !strings.HasPrefix(u.EscapedPath(), "/ui/") {
		return "", fmt.Errorf("vSphere Client(/ui/) 주소만 열 수 있습니다: %s", s)
	}
	return u.String(), nil
}

// open 은 전용 브라우저(없으면 기동)에 탭을 열고 필요하면 로그인한 뒤 딥링크로 이동한다.
// 탭/브라우저는 런처 종료 후에도 남아야 하므로 chromedp 컨텍스트를 취소하지 않고 그대로 종료한다.
func open(cfg *conf.Config, deep string) error {
	var reuse target.ID
	if !debuggerAlive() {
		id, err := startBrowser(cfg.Browser)
		if err != nil {
			return err
		}
		reuse = id
	}

	actx, _ := chromedp.NewRemoteAllocator(context.Background(), debugBase)
	var opts []chromedp.ContextOption
	if reuse != "" {
		opts = append(opts, chromedp.WithTargetID(reuse)) // 새로 띄운 브라우저의 첫 빈 탭을 사용
	}
	tab, _ := chromedp.NewContext(actx, opts...)
	// 첫 Run(브라우저 연결 + 탭 생성)은 취소하지 않는 탭 컨텍스트로 해야 한다.
	// 제한 시간 컨텍스트로 연결하면 그 컨텍스트가 끝날 때 chromedp 가 탭을 닫는다
	// (마지막 탭이면 브라우저도 종료되어, 오류 메시지 창을 띄운 사이 창이 사라짐).
	attached := make(chan error, 1)
	go func() { attached <- chromedp.Run(tab) }()
	select {
	case err := <-attached:
		if err != nil {
			return fmt.Errorf("브라우저에 연결하지 못했습니다: %v", err)
		}
	case <-time.After(startTimeout):
		return errors.New("브라우저에 연결하지 못했습니다(시간 초과).")
	}
	// 제한 시간은 이후 동작용 자식 컨텍스트에만 건다(취소돼도 탭은 유지됨).
	ctx, cancel := context.WithTimeout(tab, totalTimeout)
	defer cancel()

	if err := chromedp.Run(ctx, chromedp.Navigate(deep)); err != nil {
		return fmt.Errorf("브라우저에서 주소를 열지 못했습니다: %v", err)
	}

	loginTried := false
	last := ""
	for {
		var st struct {
			Href     string `json:"href"`
			LoggedIn bool   `json:"loggedIn"`
			Err      string `json:"err"`
		}
		js := `({href: location.href, loggedIn: !!document.querySelector('` + selLoggedIn + `'),
			err: (function(){var e=document.querySelector('` + selLoginErr + `');
			return e && e.offsetParent !== null ? e.innerText.trim() : ''})()})`
		if err := chromedp.Run(ctx, chromedp.Evaluate(js, &st)); err != nil && ctx.Err() != nil {
			break
		}
		if st.Href != "" && st.Href != last {
			logf("현재 주소: %s", st.Href)
			last = st.Href
		}
		switch {
		case strings.Contains(st.Href, ssoPathMark) && st.Err != "" && loginTried:
			return fmt.Errorf("자동 로그인에 실패했습니다: %s\n열린 브라우저 창에서 직접 로그인하세요.", st.Err)
		case strings.Contains(st.Href, ssoPathMark) && !loginTried:
			loginTried = true
			logf("SSO 로그인 화면 - 자동 로그인 (%s)", cfg.User)
			if err := login(ctx, cfg.User, cfg.Password); err != nil {
				return fmt.Errorf("로그인 화면 입력에 실패했습니다: %v\n열린 브라우저 창에서 직접 로그인하세요.", err)
			}
		case strings.Contains(st.Href, "/ui/") && st.LoggedIn:
			if st.Href != deep && loginTried {
				logf("로그인 완료 - 딥링크로 다시 이동")
				loginTried = false // 한 번만 재이동
				if err := chromedp.Run(ctx, chromedp.Navigate(deep)); err != nil {
					return fmt.Errorf("딥링크로 이동하지 못했습니다: %v", err)
				}
				continue
			}
			logf("완료: %s", st.Href)
			return nil
		}
		select {
		case <-ctx.Done():
		case <-time.After(500 * time.Millisecond):
			continue
		}
		break
	}
	if strings.Contains(last, ssoPathMark) {
		return errors.New("제한 시간 안에 로그인하지 못했습니다.\n열린 브라우저 창에서 직접 로그인하세요.")
	}
	return fmt.Errorf("제한 시간(%s) 안에 화면을 열지 못했습니다. 브라우저 창을 확인하세요.\n마지막 주소: %s", totalTimeout, last)
}

func login(ctx context.Context, user, pass string) error {
	c, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	return chromedp.Run(c,
		chromedp.WaitVisible(selUsername, chromedp.ByQuery),
		chromedp.SendKeys(selUsername, user, chromedp.ByQuery),
		chromedp.SendKeys(selPassword, pass, chromedp.ByQuery),
		chromedp.WaitEnabled(selSubmit, chromedp.ByQuery),
		chromedp.Click(selSubmit, chromedp.ByQuery),
	)
}

type devTarget struct {
	ID   string `json:"id"`
	Type string `json:"type"`
	URL  string `json:"url"`
}

func debuggerAlive() bool {
	c := http.Client{Timeout: 2 * time.Second}
	resp, err := c.Get(debugBase + "/json/version")
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// startBrowser 는 전용 프로필로 브라우저를 띄우고 첫 빈 탭의 id 를 돌려준다.
func startBrowser(kind string) (target.ID, error) {
	exe, err := findBrowser(kind)
	if err != nil {
		return "", err
	}
	profile := filepath.Join(appDataDir(), "browser-profile")
	if err := os.MkdirAll(profile, 0o755); err != nil {
		return "", fmt.Errorf("브라우저 프로필 폴더를 만들 수 없습니다: %v", err)
	}
	args := []string{
		fmt.Sprintf("--remote-debugging-port=%d", debugPort),
		"--remote-debugging-address=127.0.0.1",
		"--user-data-dir=" + profile,
		"--no-first-run",
		"--no-default-browser-check",
		"--disable-sync", // 새 프로필에서 Edge 동기화 확인 창이 뜨지 않도록
		// 사내 vCenter 는 자체 서명 인증서라 전용 프로필에서만 인증서 오류를 무시한다.
		"--ignore-certificate-errors",
		"about:blank",
	}
	logf("브라우저 기동: %s", exe)
	if err := startDetached(exe, args); err != nil {
		return "", fmt.Errorf("브라우저를 실행하지 못했습니다(%s): %v", exe, err)
	}
	deadline := time.Now().Add(startTimeout)
	for time.Now().Before(deadline) {
		if ts, err := listTargets(); err == nil {
			for _, t := range ts {
				if t.Type == "page" && t.URL == "about:blank" {
					return target.ID(t.ID), nil
				}
			}
		}
		time.Sleep(300 * time.Millisecond)
	}
	if debuggerAlive() {
		return "", nil // 빈 탭을 못 찾으면 새 탭을 연다
	}
	return "", fmt.Errorf("브라우저가 %s 안에 준비되지 않았습니다.\n같은 프로필의 브라우저가 디버깅 포트 없이 이미 떠 있다면 모두 닫고 다시 시도하세요.\n(프로필: %s)", startTimeout, profile)
}

func listTargets() ([]devTarget, error) {
	c := http.Client{Timeout: 2 * time.Second}
	resp, err := c.Get(debugBase + "/json/list")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var ts []devTarget
	return ts, json.NewDecoder(resp.Body).Decode(&ts)
}

// appDataDir 은 %LOCALAPPDATA%\vcportal (없으면 사용자 캐시 폴더) 이다.
func appDataDir() string {
	base := os.Getenv("LOCALAPPDATA")
	if base == "" {
		base, _ = os.UserCacheDir()
	}
	return filepath.Join(base, "vcportal")
}

// logf 는 %LOCALAPPDATA%\vcportal\launcher.log 에 한 줄 추가한다(비밀번호는 기록하지 않음).
func logf(format string, a ...any) {
	dir := appDataDir()
	_ = os.MkdirAll(dir, 0o755)
	f, err := os.OpenFile(filepath.Join(dir, "launcher.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "%s [%d] %s\n", time.Now().Format("2006-01-02 15:04:05"), os.Getpid(), fmt.Sprintf(format, a...))
}
