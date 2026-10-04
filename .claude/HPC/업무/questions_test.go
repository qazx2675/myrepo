// hpcbot — 질문셋(testdata/questions.json) 구조·분할·커버리지·영타 변환 검증 테스트
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"unicode"
)

type question struct {
	ID            string   `json:"id"`
	Set           string   `json:"set"`
	Type          string   `json:"type"`
	Q             string   `json:"q"`
	ConvertedHint string   `json:"converted_hint,omitempty"`
	Expect        string   `json:"expect,omitempty"`
	ExpectMode    string   `json:"expect_mode,omitempty"`
	ExpectAny     []string `json:"expect_any,omitempty"`
}

// 계획서 §6-9 유형별 개수
var questionTypeCounts = map[string]int{
	"normal": 45, "colloquial": 20, "mixed": 11, "auto_eng": 8, "h": 6, "ambiguous": 5, "nomatch": 5,
}

// holdout 유형별 허용 범위 [min, max]
var holdoutTypeRange = map[string][2]int{
	"normal": {13, 14}, "colloquial": {6, 6}, "mixed": {3, 3}, "auto_eng": {2, 3}, "h": {2, 2},
	"ambiguous": {1, 2}, "nomatch": {1, 2},
}

func loadQuestions(t *testing.T) []question {
	t.Helper()
	b, err := os.ReadFile("testdata/questions.json")
	if err != nil {
		t.Fatal(err)
	}
	dec := json.NewDecoder(strings.NewReader(string(b)))
	dec.DisallowUnknownFields()
	var qs []question
	if err := dec.Decode(&qs); err != nil {
		t.Fatalf("questions.json 파싱 실패: %v", err)
	}
	return qs
}

func loadSeedKB(t *testing.T) KB {
	t.Helper()
	b, err := os.ReadFile("data/kb_seed.json")
	if err != nil {
		t.Fatal(err)
	}
	var kb KB
	if err := json.Unmarshal(b, &kb); err != nil {
		t.Fatalf("kb_seed.json 파싱 실패: %v", err)
	}
	return kb
}

func hasHangulSyllable(s string) bool {
	for _, c := range s {
		if c >= 0xAC00 && c <= 0xD7A3 {
			return true
		}
	}
	return false
}

func TestQuestions(t *testing.T) {
	qs := loadQuestions(t)
	kb := loadSeedKB(t)
	group := map[string]string{}
	for _, it := range kb.Intents {
		group[it.ID] = it.Group
	}

	if len(qs) != 100 {
		t.Fatalf("질문 수 %d, 100 이어야 함", len(qs))
	}

	typeN := map[string]int{}
	holdN := map[string]int{}
	setN := map[string]int{}
	seenQ := map[string]bool{}
	covered := map[string]bool{}
	termN, stressN := 0, 0
	isStress := func(id string) bool { return group[id] == "os" || group[id] == "gpu" || id == "cuda" }

	for i, q := range qs {
		if want := fmt.Sprintf("q%03d", i+1); q.ID != want {
			t.Errorf("%d번째 id %q, %q 이어야 함 (순서·중복)", i+1, q.ID, want)
		}
		if strings.TrimSpace(q.Q) == "" {
			t.Errorf("%s: q 비어 있음", q.ID)
		}
		if seenQ[q.Q] {
			t.Errorf("%s: 질문 문장 중복 %q", q.ID, q.Q)
		}
		seenQ[q.Q] = true
		if q.Set != "train" && q.Set != "holdout" {
			t.Errorf("%s: set %q", q.ID, q.Set)
		}
		if _, ok := questionTypeCounts[q.Type]; !ok {
			t.Errorf("%s: type %q", q.ID, q.Type)
		}
		typeN[q.Type]++
		setN[q.Set]++
		if q.Set == "holdout" {
			holdN[q.Type]++
		}

		switch q.Type {
		case "ambiguous":
			if q.Expect != "" || q.ExpectMode != "candidates" || len(q.ExpectAny) < 2 || len(q.ExpectAny) > 4 {
				t.Errorf("%s: ambiguous 는 expect 없이 expect_mode=candidates, expect_any 2~4개", q.ID)
			}
		case "nomatch":
			if q.Expect != "" || q.ExpectMode != "nomatch" || len(q.ExpectAny) != 0 {
				t.Errorf("%s: nomatch 는 expect_mode=nomatch 만", q.ID)
			}
		default:
			if q.Expect == "" || q.ExpectMode != "" || len(q.ExpectAny) != 0 {
				t.Errorf("%s: %s 유형은 expect 하나만", q.ID, q.Type)
			}
		}
		if (q.Type == "auto_eng" || q.Type == "h") != (q.ConvertedHint != "") {
			t.Errorf("%s: converted_hint 는 auto_eng·h 유형에만 있어야 함", q.ID)
		}

		ids := q.ExpectAny
		if q.Expect != "" {
			ids = []string{q.Expect}
			covered[q.Expect] = true
			if group[q.Expect] == "term" {
				termN++
			}
		}
		stress := false
		for _, id := range ids {
			if _, ok := group[id]; !ok {
				t.Errorf("%s: kb_seed.json 에 없는 id %q", q.ID, id)
			}
			stress = stress || isStress(id)
		}
		if stress {
			stressN++
		}
	}

	for ty, n := range questionTypeCounts {
		if typeN[ty] != n {
			t.Errorf("type %s: %d개, %d개 이어야 함", ty, typeN[ty], n)
		}
		r := holdoutTypeRange[ty]
		if holdN[ty] < r[0] || holdN[ty] > r[1] {
			t.Errorf("holdout %s: %d개, %d~%d개 이어야 함", ty, holdN[ty], r[0], r[1])
		}
	}
	if setN["train"] != 70 || setN["holdout"] != 30 {
		t.Errorf("분할 train %d / holdout %d, 70/30 이어야 함", setN["train"], setN["holdout"])
	}

	for _, it := range kb.Intents {
		if it.Group != "term" && !covered[it.ID] {
			t.Errorf("커버리지: %s 를 expect 로 하는 질문 없음", it.ID)
		}
	}
	if termN < 6 {
		t.Errorf("term 질문 %d개, 6개 이상 필요", termN)
	}
	if stressN < 20 {
		t.Errorf("os·gpu·cuda 질문 %d개, 20개 이상 필요", stressN)
	}
	t.Logf("term %d개, os·gpu·cuda %d개", termN, stressN)
}

// auto_eng·h 질문은 실제 변환 함수로 converted_hint 가 나와야 한다.
func TestQuestionsConvert(t *testing.T) {
	qs := loadQuestions(t)
	kb := loadSeedKB(t)
	mb, err := os.ReadFile("data/manual.md")
	if err != nil {
		t.Fatal(err)
	}
	prot := ProtectedWords(&kb, ParseManual(string(mb)))

	for _, q := range qs {
		switch q.Type {
		case "auto_eng":
			for _, c := range q.Q {
				if c > unicode.MaxASCII {
					t.Errorf("%s: auto_eng 질문에 비ASCII 문자 %q", q.ID, q.Q)
					break
				}
			}
			if strings.HasPrefix(q.Q, "/h") {
				t.Errorf("%s: auto_eng 는 /h 없이", q.ID)
			}
			got, changed := AutoConvert(q.Q, prot)
			if !changed || got != q.ConvertedHint {
				t.Errorf("%s: AutoConvert(%q) = %q,%v, want %q", q.ID, q.Q, got, changed, q.ConvertedHint)
			}
		case "h":
			rest, ok := strings.CutPrefix(q.Q, "/h ")
			if !ok {
				t.Errorf("%s: h 유형은 \"/h \" 로 시작해야 함", q.ID)
				continue
			}
			if got := ConvertTokens(rest, prot); got != q.ConvertedHint {
				t.Errorf("%s: ConvertTokens(%q) = %q, want %q", q.ID, rest, got, q.ConvertedHint)
			}
		default:
			continue
		}
		if !hasHangulSyllable(q.ConvertedHint) {
			t.Errorf("%s: converted_hint 에 한글 음절 없음", q.ID)
		}
	}
}
