// kbgen — Jev 키워드 가중치 생성(weights)·질문셋 라벨 교차검증(validate) 도구 (계획서 §6-8, §6-9)
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

const (
	defaultEndpoint = "https://api.typesafe.ai/v1/systemone"
	groupMinP       = 0.05 // 2단계로 넘기는 그룹 확률 하한
	weightMin       = 0.02 // 이보다 작은 w 는 버린다
	saveEvery       = 20
	qName           = "intent"
)

// defaultGap 은 실제 호출 사이의 간격이다 (테스트에서 0으로 바꿈).
var defaultGap = 200 * time.Millisecond

// ---- kb.json / kb_seed.json 최소 구조 (루트 package main 과 JSON 형식 동일) ----

type Anchor struct {
	H     string `json:"h,omitempty"`
	H4    string `json:"h4,omitempty"`
	Table string `json:"table,omitempty"`
	Row   string `json:"row,omitempty"`
}

type Intent struct {
	ID         string   `json:"id"`
	Title      string   `json:"title"`
	Group      string   `json:"group"`
	Anchor     Anchor   `json:"anchor"`
	RequireAny []string `json:"require_any,omitempty"`
	Keywords   []string `json:"keywords,omitempty"`
}

type Override struct {
	KW     string  `json:"kw"`
	ID     string  `json:"id"`
	Add    float64 `json:"add"`
	Reason string  `json:"reason"`
}

type Seed struct {
	Version    int               `json:"version"`
	Thresholds json.RawMessage   `json:"thresholds"`
	Stopwords  []string          `json:"stopwords,omitempty"`
	Groups     map[string]string `json:"groups"`
	Intents    []Intent          `json:"intents"`
	Overrides  []Override        `json:"overrides,omitempty"`
}

// ---- 매뉴얼 제목 조회 (criteria 설명용, 본문은 쓰지 않는다) ----

var numRe = regexp.MustCompile(`^\d+(\.\d+)*\.?$`)

type head struct {
	Level     int
	Text, Num string
	Title     string
}

func parseHeads(src string) []head {
	var hs []head
	fence := false
	for _, ln := range strings.Split(strings.ReplaceAll(src, "\r\n", "\n"), "\n") {
		if strings.HasPrefix(strings.TrimSpace(ln), "```") {
			fence = !fence
			continue
		}
		if fence || !strings.HasPrefix(ln, "#") {
			continue
		}
		lv := 0
		for lv < len(ln) && ln[lv] == '#' {
			lv++
		}
		if lv > 6 || lv >= len(ln) || ln[lv] != ' ' {
			continue
		}
		h := head{Level: lv, Text: strings.TrimSpace(ln[lv:])}
		h.Title = h.Text
		if f := strings.Fields(h.Text); len(f) > 0 && numRe.MatchString(f[0]) {
			h.Num = strings.TrimSuffix(f[0], ".")
			h.Title = strings.TrimSpace(strings.TrimPrefix(h.Text, f[0]))
		}
		hs = append(hs, h)
	}
	return hs
}

// headingLabel 은 intent 앵커가 가리키는 매뉴얼 제목줄 라벨이다 (못 찾으면 "").
func headingLabel(hs []head, a Anchor) string {
	sec := func(num string) string {
		for _, h := range hs {
			if (h.Level == 2 || h.Level == 3) && h.Num == num {
				return "§" + h.Num + " " + h.Title
			}
		}
		return ""
	}
	switch {
	case a.H != "":
		return sec(a.H)
	case a.Table != "":
		return sec(a.Table)
	case a.H4 != "":
		for _, h := range hs {
			if h.Level == 4 && h.Text == strings.TrimSpace(a.H4) {
				return h.Text
			}
		}
	}
	return ""
}

func intentDesc(hs []head, it Intent) string {
	if l := headingLabel(hs, it.Anchor); l != "" && l != it.Title {
		return it.Title + " — " + l
	}
	return it.Title
}

// ---- 키 ----

var errNoKey = errors.New("Jev API 키가 없습니다: JEV_API_KEY 환경변수 또는 ~/.jev-claude.env, ~/.jev-router.env 의 JEV_API_KEY 를 설정하세요")

// parseEnvKey 는 env 파일 내용에서 JEV_API_KEY 값을 꺼낸다 (export 접두·따옴표 허용).
func parseEnvKey(content string) string {
	for _, ln := range strings.Split(content, "\n") {
		ln = strings.TrimSpace(ln)
		ln = strings.TrimSpace(strings.TrimPrefix(ln, "export "))
		if !strings.HasPrefix(ln, "JEV_API_KEY") {
			continue
		}
		v := strings.TrimSpace(strings.TrimPrefix(ln, "JEV_API_KEY"))
		if !strings.HasPrefix(v, "=") {
			continue
		}
		v = strings.TrimSpace(v[1:])
		if len(v) >= 2 && (v[0] == '"' || v[0] == '\'') && v[len(v)-1] == v[0] {
			v = v[1 : len(v)-1]
		}
		if v = strings.TrimSpace(v); v != "" {
			return v
		}
	}
	return ""
}

// loadKey 는 환경변수 → ~/.jev-claude.env → ~/.jev-router.env 순으로 키를 찾는다.
func loadKey(getenv func(string) string, home string) (string, error) {
	if k := strings.TrimSpace(getenv("JEV_API_KEY")); k != "" {
		return k, nil
	}
	for _, n := range []string{".jev-claude.env", ".jev-router.env"} {
		if home == "" {
			break
		}
		if b, err := os.ReadFile(filepath.Join(home, n)); err == nil {
			if k := parseEnvKey(string(b)); k != "" {
				return k, nil
			}
		}
	}
	return "", errNoKey
}

// ---- Jev 클라이언트 (캐시·재시도·간격) ----

type jevQuestion struct {
	Type         string            `json:"type"`
	Instructions string            `json:"instructions"`
	Criteria     map[string]string `json:"criteria"`
}

type jevRequest struct {
	State     string                 `json:"state"`
	Model     string                 `json:"model"`
	Questions map[string]jevQuestion `json:"questions"`
}

type jevResponse struct {
	Answers map[string]struct {
		Probabilities map[string]float64 `json:"probabilities"`
	} `json:"answers"`
}

type Client struct {
	Endpoint  string
	Key       string
	HTTP      *http.Client
	Cache     map[string]map[string]float64
	CachePath string
	Gap       time.Duration
	Backoffs  []time.Duration
	Sleep     func(time.Duration)

	Calls, Hits, Failures int
	unsaved               int
	called                bool
}

func newClient(endpoint, key, cachePath string) (*Client, error) {
	c := &Client{Endpoint: endpoint, Key: key, CachePath: cachePath,
		HTTP:     &http.Client{Timeout: 30 * time.Second},
		Cache:    map[string]map[string]float64{},
		Gap:      defaultGap,
		Backoffs: []time.Duration{time.Second, 2 * time.Second, 4 * time.Second},
		Sleep:    time.Sleep}
	if cachePath != "" {
		if b, err := os.ReadFile(cachePath); err == nil {
			if err := json.Unmarshal(b, &c.Cache); err != nil {
				return nil, fmt.Errorf("캐시 파싱 실패 %s: %w", cachePath, err)
			}
		}
	}
	return c, nil
}

// SaveCache 는 임시 파일에 쓴 뒤 rename 으로 원자적으로 저장한다.
func (c *Client) SaveCache() error {
	c.unsaved = 0
	if c.CachePath == "" {
		return nil
	}
	b, err := json.MarshalIndent(c.Cache, "", " ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(c.CachePath), 0o755); err != nil {
		return err
	}
	tmp := c.CachePath + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, c.CachePath)
}

type fatalErr struct{ error }

// Choose 는 state 를 criteria(id→설명) 중에서 고른 확률 분포를 돌려준다.
func (c *Client) Choose(state, instructions string, criteria map[string]string) (map[string]float64, error) {
	body, err := json.Marshal(jevRequest{State: state, Model: "jev-latest", Questions: map[string]jevQuestion{
		qName: {Type: "choice", Instructions: instructions, Criteria: criteria}}})
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(body)
	hk := hex.EncodeToString(sum[:])
	if p, ok := c.Cache[hk]; ok {
		c.Hits++
		return p, nil
	}
	var lastErr error
	for attempt := 0; attempt <= len(c.Backoffs); attempt++ {
		if attempt > 0 {
			c.Sleep(c.Backoffs[attempt-1])
		} else if c.called {
			c.Sleep(c.Gap)
		}
		c.called = true
		p, retry, err := c.post(body)
		if err == nil {
			c.Calls++
			c.Cache[hk] = p
			if c.unsaved++; c.unsaved >= saveEvery {
				if err := c.SaveCache(); err != nil {
					return p, fmt.Errorf("캐시 저장 실패: %w", err)
				}
			}
			return p, nil
		}
		lastErr = err
		if !retry {
			break
		}
	}
	c.Failures++
	return nil, lastErr
}

// post 는 한 번 요청한다. retry=true 면 네트워크 오류·5xx·429 라서 재시도 대상이다.
func (c *Client) post(body []byte) (map[string]float64, bool, error) {
	req, err := http.NewRequest("POST", c.Endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, false, err
	}
	req.Header.Set("Authorization", "Bearer "+c.Key)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, true, errors.New("네트워크 오류: " + strings.ReplaceAll(err.Error(), c.Key, "***"))
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if resp.StatusCode >= 500 || resp.StatusCode == 429 {
		return nil, true, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	if resp.StatusCode >= 400 {
		return nil, false, fmt.Errorf("HTTP %d: %s", resp.StatusCode, truncate(string(b), 200))
	}
	var jr jevResponse
	if err := json.Unmarshal(b, &jr); err != nil {
		return nil, false, fmt.Errorf("응답 파싱 실패: %w", err)
	}
	a, ok := jr.Answers[qName]
	if !ok || len(a.Probabilities) == 0 {
		return nil, false, errors.New("응답에 probabilities 가 없음")
	}
	return a.Probabilities, false, nil
}

func truncate(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}

// ---- 분류 (1단계 그룹 → 2단계 블록, 또는 -single) ----

type Classifier struct {
	C       *Client
	Groups  map[string]string
	Intents []Intent
	Descs   map[string]string // intent id → 설명
	Single  bool
}

func newClassifier(c *Client, seed *Seed, manual string, single bool) *Classifier {
	hs := parseHeads(manual)
	cl := &Classifier{C: c, Groups: seed.Groups, Intents: seed.Intents, Descs: map[string]string{}, Single: single}
	for _, it := range seed.Intents {
		cl.Descs[it.ID] = intentDesc(hs, it)
	}
	return cl
}

func (cl *Classifier) Mode() string {
	if cl.Single {
		return "single"
	}
	return "two-stage"
}

// Classify 는 intent id → w(그룹확률×블록확률) 를 돌려준다 (임계 미적용).
func (cl *Classifier) Classify(state string) (map[string]float64, error) {
	if cl.Single {
		crit := map[string]string{}
		for _, it := range cl.Intents {
			crit[it.ID] = cl.Descs[it.ID]
		}
		return cl.C.Choose(state, "다음 질문이 어느 업무 항목에 해당하는지 고르세요.", crit)
	}
	pg, err := cl.C.Choose(state, "다음 질문이 어느 업무 그룹에 해당하는지 고르세요.", cl.Groups)
	if err != nil {
		return nil, err
	}
	out := map[string]float64{}
	for g, p := range pg {
		if p < groupMinP {
			continue
		}
		var members []Intent
		for _, it := range cl.Intents {
			if it.Group == g {
				members = append(members, it)
			}
		}
		switch len(members) {
		case 0:
		case 1: // 선택지가 하나면 호출하지 않는다
			out[members[0].ID] += p
		default:
			crit := map[string]string{}
			for _, it := range members {
				crit[it.ID] = cl.Descs[it.ID]
			}
			pi, err := cl.C.Choose(state, "이 그룹 안에서 질문이 어느 업무 항목에 해당하는지 고르세요.", crit)
			if err != nil {
				return nil, err
			}
			for id, q := range pi {
				out[id] += p * q
			}
		}
	}
	return out, nil
}

// ---- weights ----

func canon(kw string) string {
	return strings.ReplaceAll(strings.ToLower(strings.TrimSpace(kw)), " ", "")
}

func round4(f float64) float64 { return math.Round(f*1e4) / 1e4 }

var now = func() time.Time { return time.Now().UTC() }

func readSeed(path string) (*Seed, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var s Seed
	if err := json.Unmarshal(b, &s); err != nil {
		return nil, fmt.Errorf("seed 파싱 실패: %w", err)
	}
	return &s, nil
}

func runWeights(args []string, stderr io.Writer, getenv func(string) string, home string) int {
	fs := flag.NewFlagSet("weights", flag.ContinueOnError)
	fs.SetOutput(stderr)
	seedP := fs.String("seed", "data/kb_seed.json", "seed 파일")
	manualP := fs.String("manual", "data/manual.md", "매뉴얼 파일")
	outP := fs.String("out", "data/kb.json", "출력 kb.json")
	cacheP := fs.String("cache", "tools/kbgen/cache.json", "요청 캐시 파일")
	onlyNew := fs.Bool("only-new", false, "캐시에 없는 키워드 수만 따로 보고 (캐시는 항상 사용)")
	single := fs.Bool("single", false, "1단계(전체 블록 한 번에 선택)로 처리")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	seed, err := readSeed(*seedP)
	if err != nil {
		fmt.Fprintln(stderr, "오류:", err)
		return 1
	}
	mb, err := os.ReadFile(*manualP)
	if err != nil {
		fmt.Fprintln(stderr, "오류:", err)
		return 1
	}
	key, err := loadKey(getenv, home)
	if err != nil {
		fmt.Fprintln(stderr, "오류:", err)
		return 2
	}
	ep := getenv("JEV_ENDPOINT")
	if ep == "" {
		ep = defaultEndpoint
	}
	c, err := newClient(ep, key, *cacheP)
	if err != nil {
		fmt.Fprintln(stderr, "오류:", err)
		return 1
	}
	cl := newClassifier(c, seed, string(mb), *single)

	// 키워드 합집합: 정규키 → 원문(처음 본 것)
	orig := map[string]string{}
	for _, it := range seed.Intents {
		for _, k := range it.Keywords {
			if ck := canon(k); ck != "" {
				if _, ok := orig[ck]; !ok {
					orig[ck] = strings.TrimSpace(k)
				}
			}
		}
	}
	keys := make([]string, 0, len(orig))
	for k := range orig {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	fmt.Fprintf(stderr, "키워드 %d개 (모드 %s)\n", len(keys), cl.Mode())
	if *onlyNew {
		// 정확한 신규 수는 호출 후 Calls 로 보고된다
		fmt.Fprintln(stderr, "-only-new: 캐시에 있는 요청은 다시 보내지 않습니다")
	}

	kws := map[string]map[string]float64{}
	failed := 0
	for i, k := range keys {
		ws, err := cl.Classify(orig[k])
		if err != nil {
			failed++
			fmt.Fprintf(stderr, "실패 %q: %v\n", orig[k], err)
			continue
		}
		m := map[string]float64{}
		for id, w := range ws {
			if w >= weightMin {
				m[id] = round4(w)
			}
		}
		kws[k] = m
		if (i+1)%50 == 0 {
			fmt.Fprintf(stderr, "  %d/%d (호출 %d, 캐시 %d)\n", i+1, len(keys), c.Calls, c.Hits)
		}
	}
	if err := c.SaveCache(); err != nil {
		fmt.Fprintln(stderr, "오류:", err)
		return 1
	}
	fmt.Fprintf(stderr, "키워드 %d개, 호출 %d, 캐시 적중 %d, 실패 %d\n", len(keys), c.Calls, c.Hits, failed)
	if failed > 0 {
		fmt.Fprintln(stderr, "실패가 있어 kb.json 을 쓰지 않았습니다 (캐시는 저장됨, 다시 실행하면 이어서 진행)")
		return 1
	}
	for _, o := range seed.Overrides {
		ck := canon(o.KW)
		if kws[ck] == nil {
			kws[ck] = map[string]float64{}
		}
		kws[ck][o.ID] = round4(kws[ck][o.ID] + o.Add)
	}
	gen := now().Format("2006-01-02T15:04:05Z") + " mode=" + cl.Mode()
	if err := writeKB(*outP, seed, kws, gen); err != nil {
		fmt.Fprintln(stderr, "오류:", err)
		return 1
	}
	fmt.Fprintf(stderr, "작성: %s\n", *outP)
	return 0
}

// writeKB 는 seed 내용 + keywords + generated 를 쓴다. 키워드는 한 줄에 하나.
func writeKB(path string, seed *Seed, kws map[string]map[string]float64, gen string) error {
	var buf bytes.Buffer
	field := func(name string, v any, first bool) error {
		b, err := json.MarshalIndent(v, " ", " ")
		if err != nil {
			return err
		}
		if !first {
			buf.WriteString(",\n")
		}
		fmt.Fprintf(&buf, " %q: %s", name, b)
		return nil
	}
	buf.WriteString("{\n")
	if err := field("version", seed.Version, true); err != nil {
		return err
	}
	th := seed.Thresholds
	if len(th) == 0 {
		th = json.RawMessage("{}")
	}
	if err := field("thresholds", th, false); err != nil {
		return err
	}
	if len(seed.Stopwords) > 0 {
		if err := field("stopwords", seed.Stopwords, false); err != nil {
			return err
		}
	}
	if err := field("groups", seed.Groups, false); err != nil {
		return err
	}
	if err := field("intents", seed.Intents, false); err != nil {
		return err
	}
	if len(seed.Overrides) > 0 {
		if err := field("overrides", seed.Overrides, false); err != nil {
			return err
		}
	}
	ks := make([]string, 0, len(kws))
	for k := range kws {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	buf.WriteString(",\n \"keywords\": {")
	for i, k := range ks {
		kb, _ := json.Marshal(k)
		vb, _ := json.Marshal(kws[k]) // 맵 키 정렬됨
		if i > 0 {
			buf.WriteString(",")
		}
		fmt.Fprintf(&buf, "\n  %s: %s", kb, vb)
	}
	if len(ks) > 0 {
		buf.WriteString("\n ")
	}
	buf.WriteString("}")
	if err := field("generated", gen, false); err != nil {
		return err
	}
	buf.WriteString("\n}\n")
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, buf.Bytes(), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// ---- validate ----

type question struct {
	ID            string   `json:"id"`
	Type          string   `json:"type"`
	Q             string   `json:"q"`
	ConvertedHint string   `json:"converted_hint"`
	Expect        string   `json:"expect"`
	ExpectAny     []string `json:"expect_any"`
	ExpectMode    string   `json:"expect_mode"`
}

type label struct {
	ID        string   `json:"id"`
	Q         string   `json:"q"`
	Expect    string   `json:"expect,omitempty"`
	ExpectAny []string `json:"expect_any,omitempty"`
	Jev       string   `json:"jev"`
	JevProb   float64  `json:"jev_prob"`
	Match     bool     `json:"match"`
}

var acceptPairs = [][2]string{
	{"gpu_driver", "gpu_driver_incident"},
	{"usb_block", "usb_monthly"},
	{"os_install", "os_install_type"},
}

func accepted(expect, got string) bool {
	if expect == got {
		return true
	}
	for _, p := range acceptPairs {
		if (expect == p[0] && got == p[1]) || (expect == p[1] && got == p[0]) {
			return true
		}
	}
	return false
}

func top(ws map[string]float64) (string, float64) {
	best, bp := "", -1.0
	for id, w := range ws {
		if w > bp || (w == bp && id < best) {
			best, bp = id, w
		}
	}
	if best == "" {
		return "", 0
	}
	return best, bp
}

func runValidate(args []string, stdout, stderr io.Writer, getenv func(string) string, home string) int {
	fs := flag.NewFlagSet("validate", flag.ContinueOnError)
	fs.SetOutput(stderr)
	qP := fs.String("questions", "testdata/questions.json", "질문셋")
	outP := fs.String("out", "testdata/jev_labels.json", "출력 라벨 파일")
	seedP := fs.String("seed", "data/kb_seed.json", "seed 파일")
	manualP := fs.String("manual", "data/manual.md", "매뉴얼 파일")
	cacheP := fs.String("cache", "tools/kbgen/cache.json", "요청 캐시 파일")
	single := fs.Bool("single", false, "1단계(전체 블록 한 번에 선택)로 처리")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	seed, err := readSeed(*seedP)
	if err != nil {
		fmt.Fprintln(stderr, "오류:", err)
		return 1
	}
	mb, err := os.ReadFile(*manualP)
	if err != nil {
		fmt.Fprintln(stderr, "오류:", err)
		return 1
	}
	qb, err := os.ReadFile(*qP)
	if err != nil {
		fmt.Fprintln(stderr, "오류:", err)
		return 1
	}
	var qs []question
	if err := json.Unmarshal(qb, &qs); err != nil {
		fmt.Fprintln(stderr, "질문셋 파싱 실패:", err)
		return 1
	}
	key, err := loadKey(getenv, home)
	if err != nil {
		fmt.Fprintln(stderr, "오류:", err)
		return 2
	}
	ep := getenv("JEV_ENDPOINT")
	if ep == "" {
		ep = defaultEndpoint
	}
	c, err := newClient(ep, key, *cacheP)
	if err != nil {
		fmt.Fprintln(stderr, "오류:", err)
		return 1
	}
	cl := newClassifier(c, seed, string(mb), *single)

	labels := []label{}
	var mism []label
	failed := 0
	for _, q := range qs {
		if q.Expect == "" && len(q.ExpectAny) == 0 {
			continue // nomatch 등 정답 블록이 없는 질문
		}
		text := q.Q
		if (q.Type == "h" || q.Type == "auto_eng") && q.ConvertedHint != "" {
			text = q.ConvertedHint
		}
		l := label{ID: q.ID, Q: q.Q, Expect: q.Expect, ExpectAny: q.ExpectAny}
		ws, err := cl.Classify(text)
		if err != nil {
			failed++
			fmt.Fprintf(stderr, "실패 %s: %v\n", q.ID, err)
		} else {
			l.Jev, l.JevProb = top(ws)
			l.JevProb = round4(l.JevProb)
			if q.Expect != "" {
				l.Match = accepted(q.Expect, l.Jev)
			} else {
				for _, e := range q.ExpectAny {
					if e == l.Jev {
						l.Match = true
					}
				}
			}
		}
		labels = append(labels, l)
		if !l.Match {
			mism = append(mism, l)
		}
	}
	if err := c.SaveCache(); err != nil {
		fmt.Fprintln(stderr, "오류:", err)
		return 1
	}
	b, _ := json.MarshalIndent(labels, "", " ")
	if err := os.WriteFile(*outP, append(b, '\n'), 0o644); err != nil {
		fmt.Fprintln(stderr, "오류:", err)
		return 1
	}
	fmt.Fprintf(stderr, "질문 %d개, 호출 %d, 캐시 적중 %d, 실패 %d\n", len(labels), c.Calls, c.Hits, failed)
	fmt.Fprintf(stdout, "라벨 재검토 목록 (불일치 %d / %d)\n", len(mism), len(labels))
	for _, l := range mism {
		exp := l.Expect
		if exp == "" {
			exp = "any(" + strings.Join(l.ExpectAny, ",") + ")"
		}
		fmt.Fprintf(stdout, "%s\t%s\t정답=%s\tJev=%s(%.3f)\n", l.ID, l.Q, exp, l.Jev, l.JevProb)
	}
	if failed > 0 {
		return 1
	}
	return 0
}

// ---- main ----

func run(args []string, stdout, stderr io.Writer, getenv func(string) string, home string) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "사용법: kbgen weights [-seed ..] [-manual ..] [-out ..] [-cache ..] [-only-new] [-single]\n        kbgen validate [-questions ..] [-out ..] [-single]")
		return 2
	}
	switch args[0] {
	case "weights":
		return runWeights(args[1:], stderr, getenv, home)
	case "validate":
		return runValidate(args[1:], stdout, stderr, getenv, home)
	}
	fmt.Fprintf(stderr, "알 수 없는 명령: %s\n", args[0])
	return 2
}

func main() {
	home, _ := os.UserHomeDir()
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr, os.Getenv, home))
}
