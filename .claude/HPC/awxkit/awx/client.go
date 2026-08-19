// Package awx는 AWX REST API(/api/v2)를 호출하는 최소 클라이언트를 제공한다.
// 외부 의존성 없이 표준 라이브러리(net/http, encoding/json)만 사용한다.
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

// authTransport는 모든 요청에 Basic 인증 헤더를 자동으로 붙이는 http.RoundTripper 래퍼다.
type authTransport struct {
	username, password string
	base               http.RoundTripper
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

func (c *Client) post(path string, body interface{}, out interface{}) error {
	return c.do(http.MethodPost, path, body, out)
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

	if out == nil {
		return nil
	}
	if len(data) == 0 {
		return nil
	}
	return json.Unmarshal(data, out)
}

// PingResult는 GET /api/v2/ping/ 응답이다.
type PingResult struct {
	Version    string `json:"version"`
	ActiveNode string `json:"active_node"`
}

// Ping은 AWX 서버 연결 및 버전을 확인한다.
func (c *Client) Ping() (*PingResult, error) {
	var out PingResult
	if err := c.get("/api/v2/ping/", &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// JobTemplate은 job_templates 목록/조회 응답의 일부다.
type JobTemplate struct {
	ID                   int    `json:"id"`
	Name                 string `json:"name"`
	AskVariablesOnLaunch bool   `json:"ask_variables_on_launch"`
	SurveyEnabled        bool   `json:"survey_enabled"`
	SummaryFields        struct {
		UserCapabilities struct {
			Start bool `json:"start"`
		} `json:"user_capabilities"`
	} `json:"summary_fields"`
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

// SurveySpec은 job_templates/{id}/survey_spec/ 응답이다.
type SurveySpec struct {
	Name        string           `json:"name"`
	Description string           `json:"description"`
	Spec        []SurveyQuestion `json:"spec"`
}

// SurveyQuestion은 survey_spec.spec 배열의 항목 하나다.
type SurveyQuestion struct {
	QuestionName string      `json:"question_name"`
	Variable     string      `json:"variable"`
	Type         string      `json:"type"`
	Choices      interface{} `json:"choices"` // 문자열(줄바꿈 구분) 또는 배열로 올 수 있음
	Default      interface{} `json:"default"`
	Required     bool        `json:"required"`
}

// GetSurveySpec은 템플릿의 survey 정의를 조회한다.
func (c *Client) GetSurveySpec(templateID int) (*SurveySpec, error) {
	var out SurveySpec
	if err := c.get(fmt.Sprintf("/api/v2/job_templates/%d/survey_spec/", templateID), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// LaunchResult는 launch/ 호출 응답의 일부다.
type LaunchResult struct {
	Job           int                    `json:"job"`
	IgnoredFields map[string]interface{} `json:"ignored_fields"`
}

// Launch는 템플릿을 extra_vars와 함께 실행한다.
func (c *Client) Launch(templateID int, extraVars map[string]interface{}) (*LaunchResult, error) {
	body := map[string]interface{}{}
	if len(extraVars) > 0 {
		body["extra_vars"] = extraVars
	}
	var out LaunchResult
	if err := c.post(fmt.Sprintf("/api/v2/job_templates/%d/launch/", templateID), body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Job은 jobs/{id}/ 응답의 일부다.
type Job struct {
	ID        int                    `json:"id"`
	Status    string                 `json:"status"` // pending/waiting/running/successful/failed/error/canceled
	Failed    bool                   `json:"failed"`
	Artifacts map[string]interface{} `json:"artifacts"`
}

// GetJob은 Job 상태를 조회한다.
func (c *Client) GetJob(jobID int) (*Job, error) {
	var out Job
	if err := c.get(fmt.Sprintf("/api/v2/jobs/%d/", jobID), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetJobStdout은 Job의 표준출력 텍스트를 조회한다.
func (c *Client) GetJobStdout(jobID int) (string, error) {
	req, err := http.NewRequest(http.MethodGet, c.BaseURL+fmt.Sprintf("/api/v2/jobs/%d/stdout/?format=txt", jobID), nil)
	if err != nil {
		return "", err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", &APIError{StatusCode: resp.StatusCode, Body: string(data)}
	}
	return string(data), nil
}

type inventorySourceUpdateResult struct {
	InventoryUpdate int `json:"inventory_update"`
}

// SyncInventorySource는 인벤토리 소스 동기화를 트리거하고 inventory_update ID를 반환한다.
func (c *Client) SyncInventorySource(sourceID int) (int, error) {
	var out inventorySourceUpdateResult
	if err := c.post(fmt.Sprintf("/api/v2/inventory_sources/%d/update/", sourceID), nil, &out); err != nil {
		return 0, err
	}
	return out.InventoryUpdate, nil
}

// InventoryUpdate는 inventory_updates/{id}/ 응답의 일부다.
type InventoryUpdate struct {
	ID     int    `json:"id"`
	Status string `json:"status"`
	Failed bool   `json:"failed"`
}

// GetInventoryUpdate는 인벤토리 동기화 작업 상태를 조회한다.
func (c *Client) GetInventoryUpdate(id int) (*InventoryUpdate, error) {
	var out InventoryUpdate
	if err := c.get(fmt.Sprintf("/api/v2/inventory_updates/%d/", id), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

type hostListResult struct {
	Count int `json:"count"`
}

// CountInventoryHosts는 인벤토리에 등록된 호스트 수를 조회한다.
func (c *Client) CountInventoryHosts(inventoryID int) (int, error) {
	var out hostListResult
	if err := c.get(fmt.Sprintf("/api/v2/inventories/%d/hosts/?page_size=1", inventoryID), &out); err != nil {
		return 0, err
	}
	return out.Count, nil
}
