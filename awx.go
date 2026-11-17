package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

// Client 는 AWX REST API 클라이언트다. 인벤토리 variables 만 수정한다.
type Client struct {
	base string
	user string
	pass string
	inv  int
	hc   *http.Client
}

func newClient(cfg *Config) *Client {
	tr := &http.Transport{
		Proxy:               http.ProxyFromEnvironment,
		DialContext:         (&net.Dialer{Timeout: 15 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		MaxIdleConns:        4,
		MaxIdleConnsPerHost: 4,
		IdleConnTimeout:     90 * time.Second,
	}
	return &Client{
		base: cfg.URL, user: cfg.Username, pass: cfg.Password, inv: cfg.InventoryID,
		hc: &http.Client{Transport: tr, Timeout: cfg.Timeout},
	}
}

// HTTPError 는 2xx 가 아닌 응답이다. 비밀번호는 담지 않는다.
type HTTPError struct {
	Method, Path string
	Code         int
	Body         string
}

func (e *HTTPError) Error() string {
	b := strings.TrimSpace(e.Body)
	if len(b) > 300 {
		b = b[:300] + "..."
	}
	return fmt.Sprintf("AWX %s %s 실패: HTTP %d %s", e.Method, e.Path, e.Code, b)
}

func (c *Client) do(ctx context.Context, method, path string, body []byte) ([]byte, int, error) {
	var rd io.Reader
	if body != nil {
		rd = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, rd)
	if err != nil {
		return nil, 0, err
	}
	req.SetBasicAuth(c.user, c.pass)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, 0, scrubErr(err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, scrubErr(err)
	}
	if resp.StatusCode/100 != 2 {
		return data, resp.StatusCode, &HTTPError{Method: method, Path: path, Code: resp.StatusCode, Body: string(data)}
	}
	return data, resp.StatusCode, nil
}

// scrubErr 는 오류 문자열에 혹시 비밀번호가 섞이지 않게 한다 (URL 에 자격증명을 넣지 않으므로 보통 해당 없음).
func scrubErr(err error) error {
	if err == nil {
		return nil
	}
	var ue interface{ Timeout() bool }
	if errors.As(err, &ue) && ue.Timeout() {
		return fmt.Errorf("응답 시간 초과(timeout): %v", err)
	}
	return err
}

func (c *Client) invPath() string { return fmt.Sprintf("/api/v2/inventories/%d/", c.inv) }

// GetVars 는 인벤토리 variables 원문 문자열을 돌려준다.
func (c *Client) GetVars(ctx context.Context) (string, error) {
	data, _, err := c.do(ctx, http.MethodGet, c.invPath(), nil)
	if err != nil {
		return "", err
	}
	var v struct {
		Variables *string `json:"variables"`
	}
	if err := json.Unmarshal(data, &v); err != nil {
		return "", fmt.Errorf("인벤토리 응답 해석 실패: %v", err)
	}
	if v.Variables == nil {
		return "", errors.New("인벤토리 응답에 variables 가 없습니다")
	}
	return *v.Variables, nil
}

// PatchVars 는 인벤토리 variables 만 PATCH 한다.
func (c *Client) PatchVars(ctx context.Context, vars string) error {
	_, err := c.PatchVarsCode(ctx, vars)
	return err
}

// PatchVarsCode 는 HTTP 코드도 돌려준다 (한도 측정용).
func (c *Client) PatchVarsCode(ctx context.Context, vars string) (int, error) {
	body, err := json.Marshal(map[string]string{"variables": vars})
	if err != nil {
		return 0, err
	}
	_, code, err := c.do(ctx, http.MethodPatch, c.invPath(), body)
	return code, err
}

// listModified 는 목록 API 를 끝까지 읽어 "id modified" 줄 목록을 만든다 (GET 만 한다).
func (c *Client) listModified(ctx context.Context, path string) ([]string, error) {
	var out []string
	next := path + "?page_size=200&order_by=id"
	for next != "" {
		data, _, err := c.do(ctx, http.MethodGet, next, nil)
		if err != nil {
			return nil, err
		}
		var page struct {
			Next    *string `json:"next"`
			Results []struct {
				ID       int    `json:"id"`
				Name     string `json:"name"`
				Modified string `json:"modified"`
			} `json:"results"`
		}
		if err := json.Unmarshal(data, &page); err != nil {
			return nil, fmt.Errorf("%s 응답 해석 실패: %v", path, err)
		}
		for _, r := range page.Results {
			out = append(out, fmt.Sprintf("%d %s %q", r.ID, r.Modified, r.Name))
		}
		next = ""
		if page.Next != nil && *page.Next != "" {
			next = *page.Next
			if i := strings.Index(next, "/api/"); i > 0 {
				next = next[i:]
			}
		}
	}
	return out, nil
}
