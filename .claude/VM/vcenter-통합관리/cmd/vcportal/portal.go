package main

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/cdproto/target"
	"github.com/chromedp/chromedp"

	"vcportal/internal/conf"
)

// 포털 모드: vcportal.exe 를 인자 없이 실행하면 전용 브라우저에 포털(index.html)을 띄우고
// 브라우저가 닫힐 때까지 대기한다. 전용 브라우저의 file:// 탭마다 window.vcpOpen(딥링크) 함수를
// 연결해 두므로, 포털의 [vCenter에서 열기] 가 vcportal:// 프로토콜(Edge 확인 창)을 거치지 않고
// 바로 이 프로세스로 전달된다. 열 수 있는 주소는 checkTarget 규칙(conf 에 등록된 vCenter 의 /ui/)과 같다.
const bindingName = "vcpOpen"

// portalMode 는 포털을 띄우고, 이미 다른 포털 모드 런처가 떠 있으면 새 포털 탭만 열고 끝낸다.
func portalMode(cfg *conf.Config, index string) error {
	if _, err := os.Stat(index); err != nil {
		return fmt.Errorf("포털 파일을 찾을 수 없습니다: %s", index)
	}
	page := fileURL(index)

	owner := acquireSingleton("vcportal-portal-agent")
	if !owner && debuggerAlive() {
		logf("포털 모드가 이미 실행 중 - 새 포털 탭만 연다")
		return newTab(page)
	}

	var first target.ID
	if !debuggerAlive() {
		id, err := startBrowser(cfg.Browser)
		if err != nil {
			return err
		}
		first = id
	}
	actx, _ := chromedp.NewRemoteAllocator(context.Background(), debugBase)
	if first != "" {
		tab, _ := chromedp.NewContext(actx, chromedp.WithTargetID(first))
		if err := chromedp.Run(tab, chromedp.Navigate(page)); err != nil {
			return fmt.Errorf("포털을 열지 못했습니다: %v", err)
		}
	} else if err := newTab(page); err != nil {
		return err
	}
	logf("포털 모드 시작: %s", page)

	// 전용 브라우저가 떠 있는 동안 file:// 탭을 찾아 연결 함수를 붙인다.
	attached := map[string]bool{}
	for debuggerAlive() {
		ts, err := listTargets()
		if err == nil {
			live := map[string]bool{}
			for _, t := range ts {
				if t.Type != "page" || !strings.HasPrefix(strings.ToLower(t.URL), "file:") {
					continue
				}
				live[t.ID] = true
				if !attached[t.ID] {
					if err := attachBinding(actx, cfg, target.ID(t.ID)); err != nil {
						logf("탭 연결 실패(%s): %v", t.URL, err)
						continue
					}
					attached[t.ID] = true
				}
			}
			for id := range attached {
				if !live[id] {
					delete(attached, id) // 닫힌 탭
				}
			}
		}
		time.Sleep(time.Second)
	}
	logf("전용 브라우저가 닫혀 포털 모드 종료")
	return nil
}

// attachBinding 은 탭에 window.vcpOpen 을 연결한다. 연결은 탭을 새로고침해도 유지된다.
// 탭 컨텍스트를 취소하면 chromedp 가 탭을 닫으므로 취소하지 않는다.
func attachBinding(actx context.Context, cfg *conf.Config, id target.ID) error {
	tab, _ := chromedp.NewContext(actx, chromedp.WithTargetID(id))
	chromedp.ListenTarget(tab, func(ev any) {
		e, ok := ev.(*runtime.EventBindingCalled)
		if !ok || e.Name != bindingName {
			return
		}
		go func(payload string) {
			deep, err := checkTarget(cfg, payload)
			if err == nil {
				logf("요청(포털): %s", deep)
				err = open(cfg, deep)
			}
			if err != nil {
				logf("오류: %v", err)
				showMessage("vCenter 포털 런처", err.Error(), true)
			}
		}(e.Payload)
	})
	done := make(chan error, 1)
	go func() { done <- chromedp.Run(tab, runtime.AddBinding(bindingName)) }()
	select {
	case err := <-done:
		return err
	case <-time.After(startTimeout):
		return fmt.Errorf("시간 초과")
	}
}

// newTab 은 전용 브라우저에 새 탭을 연다.
func newTab(page string) error {
	req, _ := http.NewRequest(http.MethodPut, debugBase+"/json/new?"+url.QueryEscape(page), nil)
	c := http.Client{Timeout: 5 * time.Second}
	resp, err := c.Do(req)
	if err != nil {
		return fmt.Errorf("브라우저에 새 탭을 열지 못했습니다: %v", err)
	}
	resp.Body.Close()
	return nil
}

// fileURL 은 로컬(C:\...) 또는 UNC(\\서버\공유\...) 경로를 file:// URL 로 바꾼다.
func fileURL(p string) string {
	p = filepath.ToSlash(p)
	u := url.URL{Scheme: "file"}
	if strings.HasPrefix(p, "//") { // UNC
		rest := strings.TrimPrefix(p, "//")
		host, path, _ := strings.Cut(rest, "/")
		u.Host, u.Path = host, "/"+path
	} else {
		u.Path = "/" + strings.TrimPrefix(p, "/")
	}
	return u.String()
}
