// hpcbot — 운영 --jev 옵션: 로컬 판정이 애매할 때만 Jev choice 1회 호출
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	jevEndpoint  = "https://api.typesafe.ai/v1/systemone"
	jevTimeout   = 5 * time.Second
	jevMinChoice = 0.6 // Jev 1위 확률이 이 이상일 때만 채택
)

// JEV Request types
type Question struct {
	Type         string            `json:"type"`
	Instructions string            `json:"instructions"`
	Criteria     map[string]string `json:"criteria"`
}

type SystemOneRequest struct {
	State     string              `json:"state"`
	Model     string              `json:"model"`
	Questions map[string]Question `json:"questions"`
}

// JEV Response types
type Answer struct {
	Type          string             `json:"type"`
	Choice        string             `json:"choice"`
	Probabilities map[string]float64 `json:"probabilities"`
	Confidence    float64            `json:"confidence"`
}

type SystemOneResponse struct {
	Model   string            `json:"model"`
	Answers map[string]Answer `json:"answers"`
}

// parseEnvKey 는 env 파일 내용에서 JEV_API_KEY 값을 꺼낸다 (export·따옴표 허용).
func parseEnvKey(content string) string {
	for _, ln := range strings.Split(content, "\n") {
		ln = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(ln), "export "))
		if v, ok := strings.CutPrefix(ln, "JEV_API_KEY="); ok {
			return strings.Trim(strings.TrimSpace(v), "\"'")
		}
	}
	return ""
}

// LoadJevKey 는 JEV_API_KEY 환경변수 → ~/.jev-claude.env → ~/.jev-router.env 순으로 키를 찾는다.
func LoadJevKey() string {
	if k := strings.TrimSpace(os.Getenv("JEV_API_KEY")); k != "" {
		return k
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	for _, n := range []string{".jev-claude.env", ".jev-router.env"} {
		if b, err := os.ReadFile(filepath.Join(home, n)); err == nil {
			if k := parseEnvKey(string(b)); k != "" {
				return k
			}
		}
	}
	return ""
}

// JevPick 은 활성 블록 전체를 선택지로 Jev 에 한 번 묻고 1위 id 와 확률을 돌려준다.
func JevPick(d *Data, key, input, endpoint string) (string, float64, error) {
	if endpoint == "" {
		endpoint = jevEndpoint
	}
	crit := map[string]string{}
	for _, it := range d.ActiveIntents() {
		desc := d.Blocks[it.ID].Label
		if g := d.KB.Groups[it.Group]; g != "" {
			desc += " (" + g + ")"
		}
		crit[it.ID] = desc
	}
	body, err := json.Marshal(SystemOneRequest{State: input, Model: "jev-latest", Questions: map[string]Question{
		"intent": {Type: "choice", Criteria: crit,
			Instructions: "사용자의 질문이 서버 운영 매뉴얼의 어느 항목에 대한 것인지 고르세요."}}})
	if err != nil {
		return "", 0, err
	}
	req, err := http.NewRequest("POST", endpoint, bytes.NewReader(body))
	if err != nil {
		return "", 0, err
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")
	resp, err := (&http.Client{Timeout: jevTimeout}).Do(req)
	if err != nil {
		return "", 0, errors.New("네트워크 오류")
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if resp.StatusCode >= 400 {
		return "", 0, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	var res SystemOneResponse
	if err := json.Unmarshal(b, &res); err != nil {
		return "", 0, err
	}
	a, ok := res.Answers["intent"]
	if !ok || len(a.Probabilities) == 0 {
		return "", 0, errors.New("응답에 probabilities 없음")
	}
	best, bp := "", -1.0
	for _, it := range d.ActiveIntents() { // kb 순서로 순회해 동점도 결정적으로
		if p := a.Probabilities[it.ID]; p > bp {
			best, bp = it.ID, p
		}
	}
	return best, bp, nil
}
