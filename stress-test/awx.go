package main

// AWX REST API(/api/v2) 최소 클라이언트 (awxkit awx/client.go·cli/poll.go 에서 필요한 부분만 복사)

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type awxClient struct {
	BaseURL string
	http    *http.Client
}

func newAWXClient(baseURL, username, password string, insecureTLS bool, timeout time.Duration) *awxClient {
	var base http.RoundTripper = http.DefaultTransport
	if insecureTLS {
		base = &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}
	}
	return &awxClient{
		BaseURL: strings.TrimRight(baseURL, "/"),
		http: &http.Client{
			Timeout:   timeout,
			Transport: &authTransport{username: username, password: password, base: base},
		},
	}
}

// authTransport 는 모든 요청에 Basic 인증 헤더를 붙인다.
type authTransport struct {
	username, password string
	base               http.RoundTripper
}

func (a *authTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req.SetBasicAuth(a.username, a.password)
	req.Header.Set("Content-Type", "application/json")
	return a.base.RoundTrip(req)
}

// apiError 는 AWX 가 2xx 이외의 상태코드를 돌려줬을 때 쓴다.
type apiError struct {
	StatusCode int
	Body       string
}

func (e *apiError) Error() string {
	return fmt.Sprintf("AWX API 오류 (HTTP %d): %s", e.StatusCode, e.Body)
}

func (c *awxClient) raw(method, path string, body interface{}) ([]byte, error) {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		rd = strings.NewReader(string(b))
	}
	req, err := http.NewRequest(method, c.BaseURL+path, rd)
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("AWX 서버 통신 실패: %w", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &apiError{StatusCode: resp.StatusCode, Body: string(data)}
	}
	return data, nil
}

func (c *awxClient) do(method, path string, body, out interface{}) error {
	data, err := c.raw(method, path, body)
	if err != nil || out == nil || len(data) == 0 {
		return err
	}
	return json.Unmarshal(data, out)
}

type jobTemplate struct {
	ID        int    `json:"id"`
	Name      string `json:"name"`
	Inventory int    `json:"inventory"` // 현재 인벤토리 ID (없으면 0)
	// -inspect 에서만 쓰는 필드
	AskInventory  bool   `json:"ask_inventory_on_launch"`
	SurveyEnabled bool   `json:"survey_enabled"`
	ExtraVars     string `json:"extra_vars"`
	SummaryFields struct {
		Inventory struct {
			ID   int    `json:"id"`
			Name string `json:"name"`
		} `json:"inventory"`
	} `json:"summary_fields"`
}

// invName 은 템플릿의 현재 인벤토리 이름이다 (없으면 "(없음)").
func (t *jobTemplate) invName() string {
	switch {
	case t.SummaryFields.Inventory.Name != "":
		return t.SummaryFields.Inventory.Name
	case t.Inventory != 0:
		return fmt.Sprintf("(id %d)", t.Inventory)
	}
	return "(없음)"
}

// getTemplate 은 템플릿 하나를 ID 로 다시 조회한다 (현재 인벤토리 확인용).
func (c *awxClient) getTemplate(id int) (*jobTemplate, error) {
	var t jobTemplate
	if err := c.do(http.MethodGet, fmt.Sprintf("/api/v2/job_templates/%d/", id), nil, &t); err != nil {
		return nil, err
	}
	return &t, nil
}

// setTemplateInventory 는 템플릿의 인벤토리를 invID 로 바꾼다(PATCH).
func (c *awxClient) setTemplateInventory(tmplID, invID int) error {
	return c.do(http.MethodPatch, fmt.Sprintf("/api/v2/job_templates/%d/", tmplID), map[string]interface{}{"inventory": invID}, nil)
}

// findInventory 는 이름이 정확히 같은 인벤토리의 ID 를 찾는다.
func (c *awxClient) findInventory(name string) (int, error) {
	var list struct {
		Results []struct {
			ID   int    `json:"id"`
			Name string `json:"name"`
		} `json:"results"`
	}
	if err := c.do(http.MethodGet, "/api/v2/inventories/?page_size=200&name="+url.QueryEscape(name), nil, &list); err != nil {
		return 0, fmt.Errorf("인벤토리 %q 조회 실패: %w", name, err)
	}
	for _, r := range list.Results {
		if r.Name == name {
			return r.ID, nil
		}
	}
	return 0, fmt.Errorf("AWX 에 이름이 %q 인 인벤토리가 없습니다", name)
}

// matchUserInventory 는 username 인벤토리를 고른다: 이름이 username 과 같으면 그것(exact=true),
// 없으면 이름에 username 이 들어간(대소문자 무시) 인벤토리가 하나뿐일 때 그것(exact=false, 예: I_donghyun97.kim).
// 못 고르면 ID 0, cands 는 이름에 username 이 들어간 후보들.
func matchUserInventory(invs []invInfo, username string) (inv invInfo, exact bool, cands []string) {
	u := strings.ToLower(username)
	var hit []invInfo
	for _, v := range invs {
		if v.Name == username {
			return v, true, nil
		}
		if u != "" && strings.Contains(strings.ToLower(v.Name), u) {
			hit = append(hit, v)
		}
	}
	for _, h := range hit {
		cands = append(cands, h.Name)
	}
	if len(hit) == 1 {
		return hit[0], false, cands
	}
	return invInfo{}, false, cands
}

// findUserInventory 는 matchUserInventory 로 username 인벤토리를 찾는다.
func (c *awxClient) findUserInventory(username string) (invInfo, bool, error) {
	invs, err := c.listInventories()
	if err != nil {
		return invInfo{}, false, fmt.Errorf("인벤토리 목록 조회 실패: %w", err)
	}
	inv, exact, cands := matchUserInventory(invs, username)
	switch {
	case inv.ID != 0:
		return inv, exact, nil
	case len(cands) > 1:
		return invInfo{}, false, fmt.Errorf("이름에 %q 가 들어간 인벤토리가 여러 개라 고를 수 없습니다: %s", username, strings.Join(cands, ", "))
	}
	return invInfo{}, false, fmt.Errorf("AWX 에 이름이 %q 이거나 %q 가 들어간 인벤토리가 없습니다", username, username)
}

// surveyTypes 는 템플릿 survey 의 변수명 → type(text/integer/float …) 이다. survey 가 없으면 빈 맵.
func (c *awxClient) surveyTypes(tmplID int) (map[string]string, error) {
	var s struct {
		Spec []struct {
			Variable string `json:"variable"`
			Type     string `json:"type"`
		} `json:"spec"`
	}
	if err := c.do(http.MethodGet, fmt.Sprintf("/api/v2/job_templates/%d/survey_spec/", tmplID), nil, &s); err != nil {
		return nil, err
	}
	res := map[string]string{}
	for _, q := range s.Spec {
		res[q.Variable] = q.Type
	}
	return res, nil
}

// resolveTemplate 는 ID(숫자) 또는 이름으로 job template 을 찾는다.
func (c *awxClient) resolveTemplate(idOrName string) (*jobTemplate, error) {
	if id, err := strconv.Atoi(idOrName); err == nil {
		var t jobTemplate
		if err := c.do(http.MethodGet, fmt.Sprintf("/api/v2/job_templates/%d/", id), nil, &t); err != nil {
			return nil, err
		}
		return &t, nil
	}
	var list struct {
		Results []jobTemplate `json:"results"`
	}
	if err := c.do(http.MethodGet, "/api/v2/job_templates/?page_size=200", nil, &list); err != nil {
		return nil, err
	}
	for _, t := range list.Results {
		if t.Name == idOrName {
			t := t
			return &t, nil
		}
	}
	return nil, fmt.Errorf("템플릿을 찾을 수 없습니다: %s", idOrName)
}

// launch 는 템플릿을 extra_vars 와 함께 실행하고 job ID 를 돌려준다.
func (c *awxClient) launch(templateID int, extraVars map[string]interface{}) (int, error) {
	body := map[string]interface{}{}
	if len(extraVars) > 0 {
		body["extra_vars"] = extraVars
	}
	var out struct {
		Job int `json:"job"`
	}
	if err := c.do(http.MethodPost, fmt.Sprintf("/api/v2/job_templates/%d/launch/", templateID), body, &out); err != nil {
		return 0, err
	}
	return out.Job, nil
}

// awxStatus 는 job / inventory_update 조회 응답의 공통 부분이다.
type awxStatus struct {
	ID     int    `json:"id"`
	Status string `json:"status"`
}

func (c *awxClient) getStatus(path string) (*awxStatus, error) {
	var s awxStatus
	if err := c.do(http.MethodGet, path, nil, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

func (c *awxClient) jobStdout(jobID int) (string, error) {
	b, err := c.raw(http.MethodGet, fmt.Sprintf("/api/v2/jobs/%d/stdout/?format=txt", jobID), nil)
	return string(b), err
}

// inventorySourceID 는 inventory_source 가 있으면 그 값을, 없으면 inventory 의 첫 번째 소스(id 오름차순)를 쓴다.
func (c *awxClient) inventorySourceID(cfg *Config) (int, error) {
	if cfg.InventorySource != "" {
		id, err := strconv.Atoi(cfg.InventorySource)
		if err != nil {
			return 0, fmt.Errorf("inventory_source(%s)는 숫자 ID여야 합니다", cfg.InventorySource)
		}
		return id, nil
	}
	invID, err := strconv.Atoi(cfg.Inventory)
	if err != nil {
		return 0, fmt.Errorf("inventory(%s)는 숫자 ID여야 합니다", cfg.Inventory)
	}
	var list struct {
		Results []struct {
			ID   int    `json:"id"`
			Name string `json:"name"`
		} `json:"results"`
	}
	if err := c.do(http.MethodGet, fmt.Sprintf("/api/v2/inventories/%d/inventory_sources/?page_size=200&order_by=id", invID), nil, &list); err != nil {
		return 0, fmt.Errorf("인벤토리(%d)의 소스 목록 조회 실패: %w", invID, err)
	}
	if len(list.Results) == 0 {
		return 0, fmt.Errorf("인벤토리(%d)에 등록된 소스가 없습니다", invID)
	}
	return list.Results[0].ID, nil
}

// invsync 는 인벤토리 소스의 source_field 에 yml 이름을 저장(PATCH)하고 동기화(update)한 뒤 완료까지 기다린다.
func (c *awxClient) invsync(cfg *Config, yml string, interval time.Duration, logf func(string, ...interface{})) error {
	srcID, err := c.inventorySourceID(cfg)
	if err != nil {
		return err
	}
	logf("인벤토리 소스(%d)의 %s 를 %q 로 저장 중...", srcID, cfg.SourceField, yml)
	if err := c.do(http.MethodPatch, fmt.Sprintf("/api/v2/inventory_sources/%d/", srcID), map[string]interface{}{cfg.SourceField: yml}, nil); err != nil {
		return fmt.Errorf("소스 필드 저장 실패: %w", err)
	}
	var upd struct {
		InventoryUpdate int `json:"inventory_update"`
	}
	if err := c.do(http.MethodPost, fmt.Sprintf("/api/v2/inventory_sources/%d/update/", srcID), nil, &upd); err != nil {
		return fmt.Errorf("동기화 요청 실패: %w", err)
	}
	st, err := c.poll(fmt.Sprintf("/api/v2/inventory_updates/%d/", upd.InventoryUpdate), "inventory_update", upd.InventoryUpdate, interval, logf)
	if err != nil {
		return fmt.Errorf("동기화 상태 조회 실패: %w", err)
	}
	if st != "successful" {
		return fmt.Errorf("동기화 실패 (inventory_update %d, status=%s)", upd.InventoryUpdate, st)
	}
	return nil
}

func isTerminalStatus(s string) bool {
	switch s {
	case "successful", "failed", "error", "canceled":
		return true
	}
	return false
}

// poll 은 상태가 최종 상태가 될 때까지 interval 간격으로 조회하고, 상태가 바뀔 때만 한 줄 출력한다.
func (c *awxClient) poll(path, kind string, id int, interval time.Duration, logf func(string, ...interface{})) (string, error) {
	last := ""
	for {
		s, err := c.getStatus(path)
		if err != nil {
			return "", err
		}
		if s.Status != last {
			logf("    [%s %d] %s", kind, id, s.Status)
			last = s.Status
		}
		if isTerminalStatus(s.Status) {
			return s.Status, nil
		}
		time.Sleep(interval)
	}
}

// pollJob 은 job 이 끝날 때까지 기다리고 최종 상태를 돌려준다.
func (c *awxClient) pollJob(jobID int, interval time.Duration, logf func(string, ...interface{})) (string, error) {
	return c.poll(fmt.Sprintf("/api/v2/jobs/%d/", jobID), "job", jobID, interval, logf)
}

// stdoutTail 은 job stdout 의 마지막 n 줄을 돌려준다 (실패 원인 확인용).
func (c *awxClient) stdoutTail(jobID, n int) string {
	out, err := c.jobStdout(jobID)
	if err != nil {
		return fmt.Sprintf("(stdout 조회 실패: %v)", err)
	}
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}
