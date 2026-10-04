// hpcbot — 렌더 규칙 테스트
package main

import (
	"strings"
	"testing"
)

func TestRenderRealBlocks(t *testing.T) {
	m := ParseManual(embeddedManual)

	b, _ := m.ResolveAnchor(Intent{ID: "g", Anchor: Anchor{H: "6.1"}})
	out := RenderBlock(b)
	if !strings.HasPrefix(out, "[§6.1 GPU 드라이버 설치]\n") {
		t.Errorf("제목 줄 변환 실패: %q", strings.SplitN(out, "\n", 2)[0])
	}
	if strings.Contains(out, "**") || strings.Contains(out, "```") || strings.Contains(out, "### ") {
		t.Errorf("마크업 잔존:\n%s", out)
	}
	if !strings.Contains(out, "\n    ./NVIDIA-Linux-x86_64-xxx.xx.run -s") {
		t.Errorf("코드 4칸 들여쓰기 실패:\n%s", out)
	}

	// 2.1 mermaid
	b, _ = m.ResolveAnchor(Intent{ID: "s", Anchor: Anchor{H: "2.1"}})
	out = RenderBlock(b)
	for _, bad := range []string{"flowchart", "mermaid", "-->", `["`, `{"`, "<br/>"} {
		if strings.Contains(out, bad) {
			t.Errorf("mermaid 변환 잔존 %q:\n%s", bad, out)
		}
	}
	if !strings.Contains(out, "─(아니오)→") || !strings.Contains(out, "─(예)→") {
		t.Errorf("엣지 라벨 변환 실패:\n%s", out)
	}
	if !strings.Contains(out, "    작업 요청 접수 → HPC 팀 관리 서버인가?") {
		t.Errorf("노드 라벨/화살표 변환 실패:\n%s", out)
	}

	// 2.3 이후 콜아웃: 2.2~2.4 중 [!CAUTION] 이 있는 절(2.4 이전)과 3.2
	for _, h := range []string{"3.1", "3.5", "6.3"} {
		b, _ = m.ResolveAnchor(Intent{ID: "c", Anchor: Anchor{H: h}})
		out = RenderBlock(b)
		if strings.Contains(out, "[!") || strings.Contains(out, "\n> ") {
			t.Errorf("%s 콜아웃/인용 잔존:\n%s", h, out)
		}
	}
	b, _ = m.ResolveAnchor(Intent{ID: "c", Anchor: Anchor{H: "3.5"}})
	if out = RenderBlock(b); !strings.Contains(out, "[중요]") || !strings.Contains(out, "[경고]") {
		t.Errorf("3.5 콜아웃 변환 실패:\n%s", out)
	}

	// 표 행 블록: 라벨 + 표 그대로(굵게만 제거)
	b, _ = m.ResolveAnchor(Intent{ID: "d", Title: "Docker 설치", Anchor: Anchor{Table: "8.1", Row: "Docker 설치"}})
	lines := strings.Split(RenderBlock(b), "\n")
	if len(lines) != 4 || lines[0] != "[§8.1 Docker 설치]" || !strings.HasPrefix(lines[1], "| 요청 유형") ||
		!strings.HasPrefix(lines[2], "|---") || !strings.HasPrefix(lines[3], "| Docker 설치 |") {
		t.Errorf("표 렌더 오류:\n%s", strings.Join(lines, "\n"))
	}
}

func TestRenderRules(t *testing.T) {
	b := Block{Label: "§9.9 시험", Text: "### 9.9 시험\n\n" +
		"> [!CAUTION]\n> **굵게** 주의\n>\n> [!NOTE]\n> 참고글\n\n" +
		"> [!WARNING]\n> w\n> [!IMPORTANT]\n> i\n\n" +
		"```bash\necho hi\n```\n\n" +
		"```mermaid\nflowchart TD\n    A[\"시작<br/>점\"] --> B{\"맞나?\"}\n    B -- 예 --> C[\"끝\"]\n```\n\n---\n"}
	got := RenderBlock(b)
	want := "[§9.9 시험]\n\n" +
		"[주의]\n굵게 주의\n\n[참고]\n참고글\n\n" +
		"[경고]\nw\n[중요]\ni\n\n" +
		"    echo hi\n\n" +
		"    시작 점 → 맞나?\n    B ─(예)→ 끝\n\n---"
	if got != want {
		t.Errorf("got:\n%s\n\nwant:\n%s", got, want)
	}
}
