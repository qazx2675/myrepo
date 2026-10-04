// hpcbot — 블록 원문 터미널 렌더링
package main

import (
	"regexp"
	"strings"
)

var (
	calloutMap = map[string]string{
		"[!CAUTION]":   "[주의]",
		"[!WARNING]":   "[경고]",
		"[!IMPORTANT]": "[중요]",
		"[!NOTE]":      "[참고]",
	}
	mmSquareRe = regexp.MustCompile(`\w+\["([^"]*)"\]`)
	mmCurlyRe  = regexp.MustCompile(`\w+\{"([^"]*)"\}`)
	mmEdgeRe   = regexp.MustCompile(`--\s+([^-<>]+?)\s+-->`)
	multiSpace = regexp.MustCompile(` {2,}`)
)

func renderMermaidLine(s string) string {
	s = strings.TrimSpace(s)
	s = mmSquareRe.ReplaceAllString(s, "$1")
	s = mmCurlyRe.ReplaceAllString(s, "$1")
	s = mmEdgeRe.ReplaceAllString(s, " ─($1)→ ")
	s = strings.ReplaceAll(s, "-->", " → ")
	s = strings.ReplaceAll(s, "<br/>", " ")
	return multiSpace.ReplaceAllString(strings.TrimSpace(s), " ")
}

// RenderBlock 은 블록 원문 마크다운을 터미널 텍스트로 바꾼다 (§6-6).
// 첫 줄은 항상 "[라벨]" 이며, 원문 첫 줄이 # 제목이면 그 줄을 대신한다.
func RenderBlock(b Block) string {
	lines := strings.Split(strings.ReplaceAll(b.Text, "\r\n", "\n"), "\n")
	if len(lines) > 0 && strings.HasPrefix(lines[0], "#") {
		lines = lines[1:]
	}
	out := []string{"[" + b.Label + "]"}
	inFence, mermaid := false, false
	fenceIndent := ""
	for _, ln := range lines {
		ln = strings.ReplaceAll(ln, "**", "")
		// 인용: 앞의 "> " 제거
		if t := strings.TrimLeft(ln, " "); strings.HasPrefix(t, ">") {
			ln = strings.TrimPrefix(strings.TrimPrefix(t, ">"), " ")
		}
		t := strings.TrimSpace(ln)
		if strings.HasPrefix(t, "```") {
			if !inFence {
				inFence = true
				mermaid = strings.EqualFold(strings.TrimSpace(strings.TrimPrefix(t, "```")), "mermaid")
				fenceIndent = ln[:len(ln)-len(strings.TrimLeft(ln, " "))]
			} else {
				inFence, mermaid = false, false
			}
			continue
		}
		switch {
		case inFence && mermaid:
			if strings.HasPrefix(t, "flowchart") || strings.HasPrefix(t, "graph") {
				continue
			}
			out = append(out, "    "+renderMermaidLine(t))
		case inFence:
			out = append(out, "    "+strings.TrimPrefix(ln, fenceIndent))
		default:
			if c, ok := calloutMap[t]; ok {
				ln = c
			}
			out = append(out, ln)
		}
	}
	for len(out) > 1 && strings.TrimSpace(out[len(out)-1]) == "" {
		out = out[:len(out)-1]
	}
	return strings.Join(out, "\n")
}
