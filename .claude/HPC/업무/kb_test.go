// hpcbot — kb 로드·앵커 해석 테스트
package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testManual(t *testing.T) *Manual {
	t.Helper()
	return ParseManual(embeddedManual)
}

func TestResolveAnchors(t *testing.T) {
	m := testManual(t)
	cases := []struct {
		id    string
		a     Anchor
		title string
		label string
		has   string
	}{
		{"gpu_driver", Anchor{H: "6.1"}, "", "§6.1 GPU 드라이버 설치", "nvidia-uninstall"},
		{"os_caution", Anchor{H: "3.5"}, "", "§3.5 OS 설치 주의사항", "psky"},
		{"usb_block", Anchor{H4: "USB 차단 블록"}, "", "§4.2 USB 차단 블록", "MR1동"},
		{"account_login", Anchor{H4: "계정으로 서버 접속 불가"}, "", "§7.1 계정으로 서버 접속 불가", ""},
		{"docker", Anchor{Table: "8.1", Row: "Docker 설치"}, "Docker 설치", "§8.1 Docker 설치", "Docker 설치"},
		{"so_link", Anchor{Table: "8.1", Row: "*.so 파일"}, "*.so 파일 및 link 설정 요청", "§8.1 *.so 파일 및 link 설정 요청", "*.so"},
		{"passwd", Anchor{Table: "13.3", Row: "current password"}, "current password", "§13.3 current password", "Authentication token"},
		{"ssh_kex", Anchor{Table: "13.3", Row: "SSH key exchange failed"}, "SSH key exchange failed", "§13.3 SSH key exchange failed", "key exchange failed"},
		{"term_itom", Anchor{Table: "1", Row: "ITOM"}, "ITOM", "§1 ITOM", "자산관리시스템"},
		{"term_svr", Anchor{Table: "1", Row: "svr 계정"}, "svr 계정", "§1 svr 계정", "서버 관리 계정"},
		{"non_redhat", Anchor{Table: "8.2", Row: "Redhat 제품군 외"}, "Redhat 제품군 외 패키지 설치", "§8.2 Redhat 제품군 외 패키지 설치", "yum install"},
	}
	for _, c := range cases {
		b, ok := m.ResolveAnchor(Intent{ID: c.id, Title: c.title, Anchor: c.a})
		if !ok {
			t.Errorf("%s: 앵커 해석 실패", c.id)
			continue
		}
		if b.Label != c.label {
			t.Errorf("%s: label=%q want %q", c.id, b.Label, c.label)
		}
		if !strings.Contains(b.Text, c.has) {
			t.Errorf("%s: 본문에 %q 없음", c.id, c.has)
		}
	}
}

func TestBlockBoundaries(t *testing.T) {
	m := testManual(t)
	b, _ := m.ResolveAnchor(Intent{ID: "gpu_driver", Anchor: Anchor{H: "6.1"}})
	if !strings.Contains(b.Text, "nvidia-uninstall") || strings.Contains(b.Text, "Fabric") {
		t.Errorf("6.1 경계 오류:\n%s", b.Text)
	}
	if strings.HasSuffix(strings.TrimSpace(b.Text), "---") || strings.HasSuffix(b.Text, "\n") {
		t.Errorf("끝의 빈 줄/--- 가 남음: %q", b.Text[max(0, len(b.Text)-20):])
	}
	// 3.5 는 다음 절(3.6) 제목을 포함하지 않고, --- 로 끝나는 절(4.1 앞 등)도 깔끔해야 함
	b, _ = m.ResolveAnchor(Intent{ID: "x", Anchor: Anchor{H: "3.6"}})
	if strings.Contains(b.Text, "## 4.") || strings.HasSuffix(b.Text, "---") {
		t.Errorf("3.6 끝 처리 오류")
	}
	// h4 는 다음 #### 직전까지
	b, _ = m.ResolveAnchor(Intent{ID: "usb", Anchor: Anchor{H4: "USB 차단 블록"}})
	if strings.Contains(b.Text, "update-ca-trust") {
		t.Errorf("h4 블록이 다음 h4 를 포함함")
	}
	// h 7.1 은 하위 #### 를 포함
	b, _ = m.ResolveAnchor(Intent{ID: "x", Anchor: Anchor{H: "7.1"}})
	if !strings.Contains(b.Text, "#### sudo 권한 부여") {
		t.Errorf("7.1 이 하위 h4 를 포함하지 않음")
	}
	// 표 행 블록은 머리행 + 구분행 + 행 3줄
	b, _ = m.ResolveAnchor(Intent{ID: "docker", Title: "Docker 설치", Anchor: Anchor{Table: "8.1", Row: "Docker 설치"}})
	if n := len(strings.Split(b.Text, "\n")); n != 3 {
		t.Errorf("표 행 블록 줄 수=%d want 3", n)
	}
	if strings.Contains(b.Text, "CUDA") {
		t.Errorf("다른 행이 섞임")
	}
}

func TestUnresolvedAnchor(t *testing.T) {
	var buf bytes.Buffer
	old := warnOut
	warnOut = &buf
	defer func() { warnOut = old }()

	kb := KB{Intents: []Intent{
		{ID: "ok", Title: "GPU", Anchor: Anchor{H: "6.1"}},
		{ID: "bad_h", Anchor: Anchor{H: "99.9"}},
		{ID: "bad_row", Anchor: Anchor{Table: "8.1", Row: "없는 행"}},
	}}
	d := NewData(kb, testManual(t))
	got := d.UnresolvedIDs()
	if len(got) != 2 || got[0] != "bad_h" || got[1] != "bad_row" {
		t.Fatalf("unresolved=%v", got)
	}
	if !strings.Contains(buf.String(), "[경고] 앵커 없음: bad_h") {
		t.Errorf("경고 출력 없음: %q", buf.String())
	}
	if _, ok := d.Block("bad_h"); ok {
		t.Errorf("비활성 intent 의 블록이 있음")
	}
	if len(d.ActiveIntents()) != 1 {
		t.Errorf("활성 intent 수 오류")
	}
}

func TestLoadEmbedded(t *testing.T) {
	t.Setenv("HPCBOT_DATA", "")
	d, err := LoadData()
	if err != nil {
		t.Fatal(err)
	}
	if d.Sources[fileManual] != "내장" || d.Sources[fileKB] != "내장" {
		// 테스트 바이너리 폴더에 파일이 있을 일은 없음
		t.Errorf("sources=%v", d.Sources)
	}
	if d.KB.Version != 1 || len(d.Manual.Heads) < 50 {
		t.Errorf("내장 데이터 이상: ver=%d heads=%d", d.KB.Version, len(d.Manual.Heads))
	}
}

func TestExternalPriority(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HPCBOT_DATA", dir)

	// kb.json 만 외부 파일 → manual 은 내장, kb 는 외부 (파일별 독립)
	kbJSON := `{"version":7,"intents":[{"id":"t1","title":"테스트","group":"g","anchor":{"h":"1.1"}}]}`
	if err := os.WriteFile(filepath.Join(dir, "kb.json"), []byte(kbJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	old := warnOut
	warnOut = &buf
	defer func() { warnOut = old }()

	d, err := LoadData()
	if err != nil {
		t.Fatal(err)
	}
	if d.KB.Version != 7 || d.Sources[fileKB] != filepath.Join(dir, "kb.json") || d.Sources[fileManual] != "내장" {
		t.Errorf("kb 외부 우선 실패: ver=%d src=%v", d.KB.Version, d.Sources)
	}
	if !strings.Contains(d.SourceReport(), dir) {
		t.Errorf("SourceReport 에 경로 없음: %s", d.SourceReport())
	}

	// manual.md 도 외부로 → 외부 매뉴얼 사용 (1.1 절이 있는 작은 매뉴얼)
	ext := "# T\n\n### 1.1 외부 절\n\n외부본문\n\n---\n"
	if err := os.WriteFile(filepath.Join(dir, "manual.md"), []byte(ext), 0o644); err != nil {
		t.Fatal(err)
	}
	d, err = LoadData()
	if err != nil {
		t.Fatal(err)
	}
	b, ok := d.Block("t1")
	if !ok || !strings.Contains(b.Text, "외부본문") || b.Label != "§1.1 외부 절" || strings.Contains(b.Text, "---") {
		t.Errorf("외부 매뉴얼 미적용: %+v ok=%v", b, ok)
	}
	if d.Sources[fileManual] != filepath.Join(dir, "manual.md") {
		t.Errorf("sources=%v", d.Sources)
	}

	// 잘못된 외부 kb.json 은 오류
	os.WriteFile(filepath.Join(dir, "kb.json"), []byte("{"), 0o644)
	if _, err := LoadData(); err == nil {
		t.Errorf("깨진 kb.json 인데 오류 없음")
	}
}

func TestFencedHashNotHeading(t *testing.T) {
	m := ParseManual("## 1. A\n\n### 1.1 B\n\n```bash\n## not heading\n### 9.9 x\n```\n\n### 1.2 C\n")
	if len(m.Heads) != 3 {
		t.Errorf("heads=%d want 3", len(m.Heads))
	}
}

func TestProtectedWords(t *testing.T) {
	kb := &KB{
		Intents:  []Intent{{ID: "a", Keywords: []string{"GPU", "그래픽", "Fabric Manager", "nvidia-uninstall"}}},
		Keywords: map[string]map[string]float64{"nvswitch": {"a": 0.5}},
	}
	p := ProtectedWords(kb, testManual(t))
	for _, w := range []string{"gpu", "fabric manager", "fabric", "manager", "nvswitch", "nvidia-uninstall", "itom", "update-ca-trust"} {
		if !p[w] {
			t.Errorf("보호 단어 누락: %s", w)
		}
	}
	if p["그래픽"] {
		t.Errorf("한글 키워드가 보호 단어에 들어감")
	}
	for w := range p {
		if w != strings.ToLower(w) || !isASCII(w) {
			t.Errorf("소문자 ASCII 아님: %q", w)
		}
	}
}
