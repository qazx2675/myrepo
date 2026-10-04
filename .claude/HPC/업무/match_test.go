// hpcbot — 점수 엔진 고정 회귀 테스트 (계획서 §8-2)
package main

import (
	"strings"
	"sync"
	"testing"
)

var (
	testMatcherOnce sync.Once
	testMatcher     *Matcher
)

// loadTestMatcher 는 내장 데이터로 엔진을 한 번만 만든다.
func loadTestMatcher(t testing.TB) *Matcher {
	t.Helper()
	testMatcherOnce.Do(func() {
		d, err := LoadData()
		if err != nil {
			t.Fatal(err)
		}
		if len(d.UnresolvedIDs()) > 0 {
			t.Fatalf("앵커 없음: %v", d.UnresolvedIDs())
		}
		testMatcher = NewMatcher(d)
	})
	if testMatcher == nil {
		t.Fatal("matcher 없음")
	}
	return testMatcher
}

// acceptPairs 는 서로 정답으로 인정하는 비슷한 블록 쌍이다 (§6-3).
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

func TestRegressionFixed(t *testing.T) {
	m := loadTestMatcher(t)
	answers := []struct{ q, want string }{
		{"GPU 설치", "gpu_driver"},
		{"gpu 드라이버 설치", "gpu_driver"},
		{"그래픽카드 드라이버 교체", "gpu_driver"},
		{"nvidia 드라이버 재설치", "gpu_driver"},
		{"/h rmfovlr emfkdlqj tjfcl", "gpu_driver"},
		{"OS 설치", "os_install"},
		{"os 재설치", "os_install"},
		{"PXE로 OS 깔기", "os_install"},
		{"fabric manager 설치", "fabric_manager"},
		{"nvswitch", "fabric_manager"},
		{"cuda 설치", "cuda"},
		{"docker 설치", "docker"},
	}
	for _, c := range answers {
		v := m.Query(c.q)
		if v.Mode != ModeAnswer || !accepted(c.want, v.ID()) {
			t.Errorf("%q → mode=%s id=%s top=%v, want %s", c.q, v.Mode, v.ID(), v.Top, c.want)
		}
	}
	if v := m.Query("설치"); v.Mode != ModeCandidates {
		t.Errorf("설치 → %s %v, want candidates", v.Mode, v.Top)
	}
	if v := m.Query("점심 메뉴"); v.Mode != ModeNoMatch {
		t.Errorf("점심 메뉴 → %s %v, want nomatch", v.Mode, v.Top)
	}
}

func TestSelfSearch(t *testing.T) {
	m := loadTestMatcher(t)
	for _, it := range m.d.ActiveIntents() {
		q := it.Title
		if it.Group == "term" {
			q += " 뜻"
		}
		v := m.Query(q)
		got := ""
		if len(v.Top) > 0 {
			got = v.Top[0].ID
		}
		if !accepted(it.ID, got) {
			t.Errorf("자기 검색 %q: 1위 %s (%v), want %s", q, got, v.Top, it.ID)
		}
	}
}

func TestAutoDetect(t *testing.T) {
	m := loadTestMatcher(t)
	v := m.Query("emfkdlqj tjfcl")
	if !v.Auto || !strings.Contains(v.Converted, "드라이버") || v.ID() != "gpu_driver" {
		t.Errorf("자동 영타: %+v", v)
	}
	v = m.Query("gpu 드라이버 설치")
	if v.Auto || v.Converted != "" {
		t.Errorf("변환하면 안 됨: %+v", v)
	}
	// 문장부호가 붙은 영타 토큰도 변환 후보
	v = m.Query("emfkdlqj tjfcl?")
	if !v.Auto || !strings.HasSuffix(v.Converted, "?") {
		t.Errorf("문장부호 영타: %+v", v)
	}
}

func TestMatchBoundaryAndDeterminism(t *testing.T) {
	m := loadTestMatcher(t)
	// 영문 키워드는 단어 경계: 'os' 가 'hosts' 안에서 잡히면 안 된다
	for _, e := range m.found("hosts") {
		if string(e.runes) == "os" {
			t.Error("hosts 에서 os 매칭")
		}
	}
	a, b := m.Score("gpu 드라이버 설치"), m.Score("gpu 드라이버 설치")
	if len(a) != len(b) {
		t.Fatal("비결정적")
	}
	for i := range a {
		if a[i] != b[i] {
			t.Fatal("비결정적")
		}
	}
}
