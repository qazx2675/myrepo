// kbgen 테스트 — 가짜 Jev 서버(httptest)로 weights·validate·캐시·재시도·키 탐색 검증
package main

import (
	"bytes"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// fakeJev 는 state 가 설명에 들어 있는 선택지에 9, 나머지에 1을 주고 정규화한다.
func fakeJev(t *testing.T, calls *int32, wantKey string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(calls, 1)
		if r.Header.Get("Authorization") != "Bearer "+wantKey {
			http.Error(w, "unauthorized", 401)
			return
		}
		var req jevRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Model != "jev-latest" {
			http.Error(w, "bad", 400)
			return
		}
		q := req.Questions[qName]
		raw := map[string]float64{}
		sum := 0.0
		for id, d := range q.Criteria {
			v := 1.0
			if strings.Contains(strings.ToLower(d), strings.ToLower(req.State)) {
				v = 9
			}
			raw[id] = v
			sum += v
		}
		pr := map[string]float64{}
		for id, v := range raw {
			pr[id] = v / sum
		}
		json.NewEncoder(w).Encode(map[string]any{"answers": map[string]any{qName: map[string]any{"type": "choice", "probabilities": pr}}})
	}))
}

const tinyManual = "# 지침\n\n## 1. 용어\n\n### 6.1 GPU 드라이버\n본문 secret\n\n### 6.2 OS 설치\n본문\n"

const tinySeed = `{"version":1,"thresholds":{"min_score":0.5},"stopwords":["좀"],
"groups":{"g1":"GPU: gpu driver; gpu mode; zz","g2":"OS: os install; os config","g3":"Term: x term"},
"intents":[
{"id":"gpu_driver","title":"gpu driver","group":"g1","anchor":{"h":"6.1"},"keywords":["GPU Driver","gpudriver","zz"]},
{"id":"gpu_mode","title":"gpu mode","group":"g1","anchor":{"h":"6.1"},"keywords":["gpu"]},
{"id":"os_install","title":"os install","group":"g2","anchor":{"h":"6.2"},"keywords":["os"]},
{"id":"os_cfg","title":"os config zz","group":"g2","anchor":{"h":"6.2"},"keywords":["tuning"]},
{"id":"term_x","title":"x term","group":"g3","anchor":{"table":"1","row":"X"},"keywords":["x"]}],
"overrides":[{"kw":"Z Z","id":"os_install","add":0.1,"reason":"t"}]}`

func init() { defaultGap = 0 }

type env struct {
	dir, seed, manual, out, cache string
}

func setup(t *testing.T) env {
	d := t.TempDir()
	e := env{dir: d, seed: filepath.Join(d, "seed.json"), manual: filepath.Join(d, "m.md"),
		out: filepath.Join(d, "kb.json"), cache: filepath.Join(d, "cache.json")}
	os.WriteFile(e.seed, []byte(tinySeed), 0o644)
	os.WriteFile(e.manual, []byte(tinyManual), 0o644)
	return e
}

func getenvFor(ep, key string) func(string) string {
	return func(k string) string {
		switch k {
		case "JEV_ENDPOINT":
			return ep
		case "JEV_API_KEY":
			return key
		}
		return ""
	}
}

func near(a, b float64) bool { return math.Abs(a-b) < 2e-4 }

func TestWeightsEndToEndAndCache(t *testing.T) {
	var calls int32
	srv := fakeJev(t, &calls, "k123")
	defer srv.Close()
	e := setup(t)
	now = func() time.Time { return time.Date(2026, 10, 4, 1, 2, 3, 0, time.UTC) }
	args := []string{"-seed", e.seed, "-manual", e.manual, "-out", e.out, "-cache", e.cache}
	var errb bytes.Buffer
	if rc := run(append([]string{"weights"}, args...), nil, &errb, getenvFor(srv.URL, "k123"), e.dir); rc != 0 {
		t.Fatalf("rc=%d %s", rc, errb.String())
	}
	b, _ := os.ReadFile(e.out)
	var kb struct {
		Keywords  map[string]map[string]float64 `json:"keywords"`
		Generated string                        `json:"generated"`
		Intents   []Intent                      `json:"intents"`
		Groups    map[string]string             `json:"groups"`
	}
	if err := json.Unmarshal(b, &kb); err != nil {
		t.Fatal(err, string(b))
	}
	if kb.Generated != "2026-10-04T01:02:03Z mode=two-stage" || len(kb.Intents) != 5 || len(kb.Groups) != 3 {
		t.Fatalf("seed 내용/generated 이상: %q", kb.Generated)
	}
	// 정규화: "GPU Driver"/"gpudriver" 는 하나
	if _, ok := kb.Keywords["gpudriver"]; !ok || len(kb.Keywords) != 6 {
		t.Fatalf("키워드 수/정규화 이상: %v", kb.Keywords)
	}
	// "gpu driver": p(g1)=9/11, g2 0.0909(2단계 0.5씩), g3 0.0909(선택지 1개)
	k := kb.Keywords["gpudriver"]
	if !near(k["gpu_driver"], 9.0/11*0.9) || !near(k["gpu_mode"], 9.0/11*0.1) ||
		!near(k["os_install"], 1.0/11*0.5) || !near(k["term_x"], 1.0/11) {
		t.Fatalf("gpudriver 가중치: %v", k)
	}
	// "zz": g2 의 os_install 은 1/11*0.1=0.009 → 버림, override +0.1 로 다시 생김
	z := kb.Keywords["zz"]
	if !near(z["os_cfg"], 1.0/11*0.9) || !near(z["gpu_driver"], 9.0/11*0.5) || !near(z["os_install"], 0.1) {
		t.Fatalf("zz 가중치/override: %v", z)
	}
	for kw, m := range kb.Keywords {
		for id, w := range m {
			if w < 0.02 && !(kw == "zz" && id == "os_install") {
				t.Fatalf("0.02 미만이 남음 %s/%s=%v", kw, id, w)
			}
		}
	}
	if !strings.Contains(string(b), "\n  \"zz\": {") {
		t.Fatalf("키워드 한 줄 형식 아님:\n%s", b)
	}
	n := atomic.LoadInt32(&calls)
	if n == 0 {
		t.Fatal("호출 없음")
	}
	// 2회차: 캐시로 HTTP 0회, 결과 동일
	before := string(b)
	errb.Reset()
	if rc := run(append([]string{"weights", "-only-new"}, args...), nil, &errb, getenvFor(srv.URL, "k123"), e.dir); rc != 0 {
		t.Fatalf("rc=%d %s", rc, errb.String())
	}
	if atomic.LoadInt32(&calls) != n {
		t.Fatalf("캐시 적중해야 하는데 호출됨: %d → %d", n, calls)
	}
	if !strings.Contains(errb.String(), "호출 0") {
		t.Fatalf("보고 이상: %s", errb.String())
	}
	after, _ := os.ReadFile(e.out)
	if string(after) != before {
		t.Fatal("재실행 결과가 다름")
	}
	if strings.Contains(errb.String(), "k123") {
		t.Fatal("키 노출")
	}
}

func TestWeightsSingleMode(t *testing.T) {
	var calls int32
	srv := fakeJev(t, &calls, "k")
	defer srv.Close()
	e := setup(t)
	rc := run([]string{"weights", "-single", "-seed", e.seed, "-manual", e.manual, "-out", e.out, "-cache", e.cache},
		nil, &bytes.Buffer{}, getenvFor(srv.URL, "k"), e.dir)
	b, _ := os.ReadFile(e.out)
	if rc != 0 || !strings.Contains(string(b), "mode=single") {
		t.Fatalf("rc=%d %s", rc, b)
	}
}

func TestRetryThenSuccess(t *testing.T) {
	var n int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&n, 1) <= 2 {
			http.Error(w, "boom", 500)
			return
		}
		w.Write([]byte(`{"answers":{"intent":{"probabilities":{"a":0.7,"b":0.3}}}}`))
	}))
	defer srv.Close()
	c, _ := newClient(srv.URL, "k", "")
	var slept []time.Duration
	c.Sleep = func(d time.Duration) { slept = append(slept, d) }
	p, err := c.Choose("s", "i", map[string]string{"a": "A", "b": "B"})
	if err != nil || p["a"] != 0.7 || n != 3 {
		t.Fatalf("err=%v p=%v n=%d", err, p, n)
	}
	if len(slept) != 2 || slept[0] != time.Second || slept[1] != 2*time.Second {
		t.Fatalf("백오프: %v", slept)
	}
	// 4xx 는 재시도하지 않고 실패
	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&n, 100)
		http.Error(w, "bad", 400)
	}))
	defer srv2.Close()
	c2, _ := newClient(srv2.URL, "k", "")
	c2.Sleep = func(time.Duration) {}
	n = 0
	if _, err := c2.Choose("s", "i", map[string]string{"a": "A"}); err == nil || n != 100 || c2.Failures != 1 {
		t.Fatalf("4xx: err=%v n=%d", err, n)
	}
}

func TestWeightsFailureExit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "no", 400) }))
	defer srv.Close()
	e := setup(t)
	rc := run([]string{"weights", "-seed", e.seed, "-manual", e.manual, "-out", e.out, "-cache", e.cache},
		nil, &bytes.Buffer{}, getenvFor(srv.URL, "k"), e.dir)
	if rc != 1 {
		t.Fatalf("rc=%d", rc)
	}
	if _, err := os.Stat(e.out); err == nil {
		t.Fatal("실패 시 kb.json 을 쓰면 안 됨")
	}
}

func TestKeyLoading(t *testing.T) {
	home := t.TempDir()
	none := func(string) string { return "" }
	if _, err := loadKey(none, home); err != errNoKey {
		t.Fatalf("키 없음 오류여야 함: %v", err)
	}
	os.WriteFile(filepath.Join(home, ".jev-router.env"), []byte("# c\nexport JEV_API_KEY='router-key'\n"), 0o600)
	if k, _ := loadKey(none, home); k != "router-key" {
		t.Fatalf("router: %q", k)
	}
	os.WriteFile(filepath.Join(home, ".jev-claude.env"), []byte("OTHER=1\nJEV_API_KEY=\"claude-key\"\n"), 0o600)
	if k, _ := loadKey(none, home); k != "claude-key" {
		t.Fatalf("claude 가 우선: %q", k)
	}
	if k, _ := loadKey(func(s string) string { return map[string]string{"JEV_API_KEY": "envkey"}[s] }, home); k != "envkey" {
		t.Fatalf("env 우선: %q", k)
	}
	if parseEnvKey("JEV_API_KEYX=1\nJEV_API_KEY = plain \n") != "plain" {
		t.Fatal("parseEnvKey")
	}
}

func TestMissingKeyExit2(t *testing.T) {
	e := setup(t)
	var errb bytes.Buffer
	rc := run([]string{"weights", "-seed", e.seed, "-manual", e.manual, "-out", e.out, "-cache", e.cache},
		nil, &errb, func(string) string { return "" }, e.dir)
	if rc != 2 || !strings.Contains(errb.String(), "JEV_API_KEY") {
		t.Fatalf("rc=%d %s", rc, errb.String())
	}
	if rc := run(nil, nil, &errb, func(string) string { return "" }, e.dir); rc != 2 {
		t.Fatal("인자 없음은 2")
	}
}

func TestValidate(t *testing.T) {
	var calls int32
	srv := fakeJev(t, &calls, "k")
	defer srv.Close()
	e := setup(t)
	qs := `[
{"id":"q1","type":"normal","q":"gpu driver","expect":"gpu_driver"},
{"id":"q2","type":"normal","q":"os install","expect":"os_install_type"},
{"id":"q3","type":"h","q":"/h rmfovlr","converted_hint":"gpu mode","expect":"gpu_driver"},
{"id":"q4","type":"ambiguous","q":"os install","expect_mode":"candidates","expect_any":["os_install","gpu_driver"]},
{"id":"q5","type":"ambiguous","q":"x term","expect_mode":"candidates","expect_any":["os_install","gpu_driver"]},
{"id":"q6","type":"nomatch","q":"점심","expect_mode":"nomatch"}]`
	qp := filepath.Join(e.dir, "q.json")
	os.WriteFile(qp, []byte(qs), 0o644)
	lp := filepath.Join(e.dir, "labels.json")
	var out, errb bytes.Buffer
	rc := run([]string{"validate", "-questions", qp, "-out", lp, "-seed", e.seed, "-manual", e.manual, "-cache", e.cache},
		&out, &errb, getenvFor(srv.URL, "k"), e.dir)
	if rc != 0 {
		t.Fatalf("rc=%d %s", rc, errb.String())
	}
	var ls []label
	b, _ := os.ReadFile(lp)
	if err := json.Unmarshal(b, &ls); err != nil {
		t.Fatal(err)
	}
	if len(ls) != 5 {
		t.Fatalf("nomatch 는 제외되어야 함: %d", len(ls))
	}
	byID := map[string]label{}
	for _, l := range ls {
		byID[l.ID] = l
	}
	if l := byID["q1"]; l.Jev != "gpu_driver" || !l.Match || l.JevProb <= 0 {
		t.Fatalf("q1 %+v", l)
	}
	// os install → os_install, expect os_install_type: accept 쌍이라 일치
	if l := byID["q2"]; l.Jev != "os_install" || !l.Match {
		t.Fatalf("q2 %+v", l)
	}
	// hint 사용: "gpu mode" → gpu_mode, expect gpu_driver 불일치
	if l := byID["q3"]; l.Jev != "gpu_mode" || l.Match {
		t.Fatalf("q3 %+v", l)
	}
	if l := byID["q4"]; !l.Match {
		t.Fatalf("q4 %+v", l)
	}
	if l := byID["q5"]; l.Match || l.Jev != "term_x" {
		t.Fatalf("q5 %+v", l)
	}
	o := out.String()
	if !strings.Contains(o, "q3") || !strings.Contains(o, "q5") || strings.Contains(o, "q1\t") || !strings.Contains(o, "불일치 2 / 5") {
		t.Fatalf("불일치 목록:\n%s", o)
	}
}
