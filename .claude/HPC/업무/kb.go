// hpcbot — 매뉴얼 로드·앵커 해석
package main

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

//go:embed data/manual.md
var embeddedManual string

//go:embed data/kb.json
var embeddedKB []byte

// 데이터 파일 이름 (HPCBOT_DATA·실행 파일 폴더에서 찾는 이름)
const (
	fileManual = "manual.md"
	fileKB     = "kb.json"
)

// warnOut 은 [경고] 메시지 출력 대상이다 (테스트에서 교체).
var warnOut io.Writer = os.Stderr

// ---- kb.json / kb_seed.json 공용 구조 (계획서 §6-8) ----

type Thresholds struct {
	MinScore   float64 `json:"min_score"`
	AmbigRatio float64 `json:"ambig_ratio"`
	CandRatio  float64 `json:"cand_ratio"`
}

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

	// Disabled 는 앵커를 찾지 못해 로드 시 비활성화된 블록 (JSON 에는 없음)
	Disabled bool `json:"-"`
}

type Override struct {
	KW     string  `json:"kw"`
	ID     string  `json:"id"`
	Add    float64 `json:"add"`
	Reason string  `json:"reason"`
}

type KB struct {
	Version    int                           `json:"version"`
	Thresholds Thresholds                    `json:"thresholds"`
	Stopwords  []string                      `json:"stopwords,omitempty"`
	Groups     map[string]string             `json:"groups,omitempty"`
	Intents    []Intent                      `json:"intents"`
	Overrides  []Override                    `json:"overrides,omitempty"`
	Keywords   map[string]map[string]float64 `json:"keywords,omitempty"`
	Generated  string                        `json:"generated,omitempty"`
}

// ---- 매뉴얼 파서 ----

type heading struct {
	Level int    // # 개수
	Line  int    // 0부터 시작하는 줄 번호
	Text  string // # 와 공백을 뺀 제목 전체 ("6.1 GPU 드라이버 설치")
	Num   string // 절 번호 ("6.1", "1"), 없으면 ""
	Title string // 번호를 뺀 제목
}

type Manual struct {
	Lines []string
	Heads []heading
}

var numRe = regexp.MustCompile(`^\d+(\.\d+)*\.?$`)

// ParseManual 은 매뉴얼 마크다운을 줄과 제목 목록으로 나눈다 (코드펜스 안의 # 는 제목이 아님).
func ParseManual(src string) *Manual {
	src = strings.ReplaceAll(src, "\r\n", "\n")
	m := &Manual{Lines: strings.Split(src, "\n")}
	inFence := false
	for i, ln := range m.Lines {
		t := strings.TrimSpace(ln)
		if strings.HasPrefix(t, "```") {
			inFence = !inFence
			continue
		}
		if inFence || !strings.HasPrefix(ln, "#") {
			continue
		}
		lv := 0
		for lv < len(ln) && ln[lv] == '#' {
			lv++
		}
		if lv > 6 || lv >= len(ln) || ln[lv] != ' ' {
			continue
		}
		h := heading{Level: lv, Line: i, Text: strings.TrimSpace(ln[lv:])}
		h.Title = h.Text
		if f := strings.Fields(h.Text); len(f) > 0 && numRe.MatchString(f[0]) {
			h.Num = strings.TrimSuffix(f[0], ".")
			h.Title = strings.TrimSpace(strings.TrimPrefix(h.Text, f[0]))
		}
		m.Heads = append(m.Heads, h)
	}
	return m
}

// span 은 제목 i 의 구간 [start,end) 줄 범위를 돌려준다.
// 같은 레벨 이상의 다음 제목 직전까지이며, 끝의 빈 줄과 --- 는 잘라낸다.
func (m *Manual) span(i int) (int, int) {
	h := m.Heads[i]
	end := len(m.Lines)
	for j := i + 1; j < len(m.Heads); j++ {
		if m.Heads[j].Level <= h.Level {
			end = m.Heads[j].Line
			break
		}
	}
	for end > h.Line+1 {
		t := strings.TrimSpace(m.Lines[end-1])
		if t == "" || t == "---" {
			end--
			continue
		}
		break
	}
	return h.Line, end
}

// findSection 은 번호가 num 인 ## 또는 ### 제목의 인덱스를 찾는다.
func (m *Manual) findSection(num string) int {
	for i, h := range m.Heads {
		if (h.Level == 2 || h.Level == 3) && h.Num == num {
			return i
		}
	}
	return -1
}

// parentNum 은 제목 i 앞쪽에서 가장 가까운 ### 제목의 번호를 돌려준다.
func (m *Manual) parentNum(i int) string {
	for j := i - 1; j >= 0; j-- {
		if m.Heads[j].Level <= 3 {
			return m.Heads[j].Num
		}
	}
	return ""
}

// ---- 블록과 앵커 해석 ----

// Block 은 답변 단위 하나다.
type Block struct {
	ID    string // intent id
	Label string // 표시용 라벨 (예: "§6.1 GPU 드라이버 설치")
	Text  string // 매뉴얼 원문 마크다운 (h/h4 는 제목 줄 포함, 표 행은 머리행+구분행+행)
}

func cleanCell(s string) string {
	s = strings.ReplaceAll(s, "**", "")
	s = strings.ReplaceAll(s, "`", "")
	return strings.TrimSpace(s)
}

func firstCell(line string) string {
	t := strings.TrimSpace(line)
	t = strings.TrimPrefix(t, "|")
	if k := strings.Index(t, "|"); k >= 0 {
		t = t[:k]
	}
	return cleanCell(t)
}

// ResolveAnchor 는 intent 의 앵커를 매뉴얼에서 찾아 블록으로 만든다.
func (m *Manual) ResolveAnchor(it Intent) (Block, bool) {
	a := it.Anchor
	switch {
	case a.H != "":
		i := m.findSection(a.H)
		if i < 0 {
			return Block{}, false
		}
		s, e := m.span(i)
		h := m.Heads[i]
		return Block{ID: it.ID, Label: "§" + h.Num + " " + h.Title, Text: strings.Join(m.Lines[s:e], "\n")}, true
	case a.H4 != "":
		want := strings.TrimSpace(a.H4)
		for i, h := range m.Heads {
			if h.Level == 4 && h.Text == want {
				s, e := m.span(i)
				label := "§" + m.parentNum(i) + " " + h.Text
				return Block{ID: it.ID, Label: label, Text: strings.Join(m.Lines[s:e], "\n")}, true
			}
		}
		return Block{}, false
	case a.Table != "":
		i := m.findSection(a.Table)
		if i < 0 {
			return Block{}, false
		}
		s, e := m.span(i)
		var tbl []string
		for _, ln := range m.Lines[s:e] {
			if strings.HasPrefix(strings.TrimSpace(ln), "|") {
				tbl = append(tbl, ln)
			} else if len(tbl) > 0 {
				break
			}
		}
		if len(tbl) < 3 {
			return Block{}, false
		}
		want := cleanCell(a.Row)
		for _, ln := range tbl[2:] {
			cell := firstCell(ln)
			if want != "" && strings.HasPrefix(cell, want) {
				name := it.Title
				if name == "" {
					name = cell
				}
				return Block{ID: it.ID, Label: "§" + m.Heads[i].Num + " " + name,
					Text: tbl[0] + "\n" + tbl[1] + "\n" + ln}, true
			}
		}
	}
	return Block{}, false
}

// ---- 데이터 로드 (외부 파일 우선) ----

// Data 는 로드된 KB + 매뉴얼 + 해석된 블록이다.
type Data struct {
	KB      KB
	Manual  *Manual
	Blocks  map[string]Block  // intent id → 블록 (해석된 것만)
	Sources map[string]string // "manual.md"/"kb.json" → "내장" 또는 파일 경로

	unresolved []string
}

// readData 는 $HPCBOT_DATA/<name> → 실행 파일 폴더/<name> → 내장본 순으로 읽는다.
func readData(name string, embedded []byte) ([]byte, string) {
	var dirs []string
	if d := os.Getenv("HPCBOT_DATA"); d != "" {
		dirs = append(dirs, d)
	}
	if exe, err := os.Executable(); err == nil {
		dirs = append(dirs, filepath.Dir(exe))
	}
	for _, d := range dirs {
		p := filepath.Join(d, name)
		if b, err := os.ReadFile(p); err == nil {
			return b, p
		}
	}
	return embedded, "내장"
}

// LoadData 는 데이터를 읽어 앵커를 모두 해석한다. 실패한 앵커는 경고 후 비활성화한다.
func LoadData() (*Data, error) {
	mb, msrc := readData(fileManual, []byte(embeddedManual))
	kbb, ksrc := readData(fileKB, embeddedKB)
	var kb KB
	if err := json.Unmarshal(kbb, &kb); err != nil {
		return nil, fmt.Errorf("kb.json(%s) 파싱 실패: %w", ksrc, err)
	}
	d := NewData(kb, ParseManual(string(mb)))
	d.Sources = map[string]string{fileManual: msrc, fileKB: ksrc}
	return d, nil
}

// NewData 는 이미 읽은 KB·매뉴얼로 Data 를 만들고 모든 앵커를 해석한다.
func NewData(kb KB, m *Manual) *Data {
	d := &Data{KB: kb, Manual: m, Blocks: map[string]Block{}, Sources: map[string]string{}}
	for i := range d.KB.Intents {
		it := &d.KB.Intents[i]
		b, ok := m.ResolveAnchor(*it)
		if !ok {
			fmt.Fprintf(warnOut, "[경고] 앵커 없음: %s\n", it.ID)
			it.Disabled = true
			d.unresolved = append(d.unresolved, it.ID)
			continue
		}
		d.Blocks[it.ID] = b
	}
	return d
}

// UnresolvedIDs 는 앵커를 찾지 못해 비활성화된 intent id 목록이다.
func (d *Data) UnresolvedIDs() []string { return slices.Clone(d.unresolved) }

// Block 은 id 의 블록을 돌려준다 (비활성이면 false).
func (d *Data) Block(id string) (Block, bool) {
	b, ok := d.Blocks[id]
	return b, ok
}

// ActiveIntents 는 앵커가 해석된 intent 만 kb 순서대로 돌려준다.
func (d *Data) ActiveIntents() []Intent {
	var out []Intent
	for _, it := range d.KB.Intents {
		if !it.Disabled {
			out = append(out, it)
		}
	}
	return out
}

// SourceReport 는 -v 용 데이터 출처 문자열이다.
func (d *Data) SourceReport() string {
	return fmt.Sprintf("[데이터] manual.md: %s / kb.json: %s", d.Sources[fileManual], d.Sources[fileKB])
}

// ---- 보호 단어 (§6-4) ----

var protTokenRe = regexp.MustCompile(`[A-Za-z][A-Za-z0-9_.-]{1,}`)

func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			return false
		}
	}
	return true
}

// ProtectedWords 는 영타 변환에서 제외할 소문자 단어 집합이다:
// kb 의 ASCII 키워드(공백 포함 키워드는 통째와 각 단어) + 매뉴얼의 영문 토큰.
func ProtectedWords(kb *KB, m *Manual) map[string]bool {
	set := map[string]bool{}
	add := func(w string) {
		w = strings.ToLower(strings.TrimSpace(w))
		if len(w) >= 2 && isASCII(w) {
			set[w] = true
		}
	}
	addKW := func(kw string) {
		if !isASCII(kw) {
			return
		}
		add(kw)
		for _, p := range strings.Fields(kw) {
			add(p)
		}
	}
	for _, it := range kb.Intents {
		for _, k := range it.Keywords {
			addKW(k)
		}
	}
	for k := range kb.Keywords {
		addKW(k)
	}
	for _, o := range kb.Overrides {
		addKW(o.KW)
	}
	if m != nil {
		for _, tok := range protTokenRe.FindAllString(strings.Join(m.Lines, "\n"), -1) {
			add(tok)
			// 문장 끝 마침표 등을 뗀 형태도 보호
			add(strings.TrimRight(tok, ".-_"))
		}
	}
	return set
}
