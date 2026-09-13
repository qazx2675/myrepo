// Package awx는 AWX REST API(/api/v2)를 호출하는 최소 클라이언트를 제공한다.
// 외부 의존성 없이 표준 라이브러리(net/http, encoding/json)만 사용한다.
// 브릿지 용도로 Job Template의 description 필드 하나만 읽고 쓴다
// (YAML 변수(extra_vars)는 문법 오류 시 저장이 실패할 수 있어 사용하지 않는다).
package awx

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Client는 AWX 서버에 Basic 인증으로 접속하는 HTTP 클라이언트다.
type Client struct {
	BaseURL string
	http    *http.Client
}

// NewClient는 AWX 클라이언트를 생성한다.
func NewClient(baseURL, username, password string, insecureTLS bool, timeout time.Duration) *Client {
	var base http.RoundTripper = http.DefaultTransport
	if insecureTLS {
		base = &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}
	}
	return &Client{
		BaseURL: strings.TrimRight(baseURL, "/"),
		http: &http.Client{
			Timeout: timeout,
			Transport: &authTransport{
				username: username,
				password: password,
				base:     base,
			},
		},
	}
}

type authTransport struct {
	username, password string
	base                http.RoundTripper
}

func (a *authTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req.SetBasicAuth(a.username, a.password)
	req.Header.Set("Content-Type", "application/json")
	return a.base.RoundTrip(req)
}

// APIError는 AWX가 2xx 이외의 상태코드를 반환했을 때 사용된다.
type APIError struct {
	StatusCode int
	Body       string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("AWX API 오류 (HTTP %d): %s", e.StatusCode, e.Body)
}

func (c *Client) get(path string, out interface{}) error {
	return c.do(http.MethodGet, path, nil, out)
}

func (c *Client) patch(path string, body interface{}, out interface{}) error {
	return c.do(http.MethodPatch, path, body, out)
}

func (c *Client) do(method, path string, body interface{}, out interface{}) error {
	var reqBody io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reqBody = strings.NewReader(string(b))
	}

	req, err := http.NewRequest(method, c.BaseURL+path, reqBody)
	if err != nil {
		return err
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("AWX 서버 통신 실패: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &APIError{StatusCode: resp.StatusCode, Body: string(data)}
	}

	if out == nil || len(data) == 0 {
		return nil
	}
	return json.Unmarshal(data, out)
}

// JobTemplate은 job_templates 응답의 일부다.
type JobTemplate struct {
	ID          int    `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

type jobTemplateList struct {
	Count   int           `json:"count"`
	Results []JobTemplate `json:"results"`
}

// ListJobTemplates는 템플릿 목록을 조회한다(최대 200개).
func (c *Client) ListJobTemplates() ([]JobTemplate, error) {
	var out jobTemplateList
	if err := c.get("/api/v2/job_templates/?page_size=200", &out); err != nil {
		return nil, err
	}
	return out.Results, nil
}

// ResolveTemplate은 ID(숫자 문자열) 또는 이름으로 템플릿을 찾는다.
func (c *Client) ResolveTemplate(idOrName string) (*JobTemplate, error) {
	if id, err := strconv.Atoi(idOrName); err == nil {
		var out JobTemplate
		if err := c.get(fmt.Sprintf("/api/v2/job_templates/%d/", id), &out); err != nil {
			return nil, err
		}
		return &out, nil
	}

	templates, err := c.ListJobTemplates()
	if err != nil {
		return nil, err
	}
	for _, t := range templates {
		if t.Name == idOrName {
			return &t, nil
		}
	}
	return nil, fmt.Errorf("템플릿을 찾을 수 없습니다: %s", idOrName)
}

// GetDescription은 템플릿의 description 필드를 조회한다.
func (c *Client) GetDescription(id int) (string, error) {
	var out JobTemplate
	if err := c.get(fmt.Sprintf("/api/v2/job_templates/%d/", id), &out); err != nil {
		return "", err
	}
	return out.Description, nil
}

// SetDescription은 템플릿의 description 필드를 PATCH로 갱신한다.
func (c *Client) SetDescription(id int, text string) error {
	body := map[string]interface{}{"description": text}
	var out JobTemplate
	return c.patch(fmt.Sprintf("/api/v2/job_templates/%d/", id), body, &out)
}

// ClearDescription은 description 필드를 빈 값으로 초기화한다 (수신 완료 후 cleanup).
func (c *Client) ClearDescription(id int) error {
	return c.SetDescription(id, "")
}
