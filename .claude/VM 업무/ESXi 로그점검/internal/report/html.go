package report

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"esxi-log-check/internal/match"
)

// WriteHTML은 브라우저에서 보기 좋은 HTML 형식의 리포트를 출력한다.
func WriteHTML(w io.Writer, r Result, opts TextOptions) {
	fmt.Fprintf(w, `<!DOCTYPE html>
<html lang="ko">
<head>
<meta charset="UTF-8">
<title>ESXi Critical Log Check Report</title>
<style>
	body { font-family: 'Segoe UI', Tahoma, Geneva, Verdana, sans-serif; background-color: #f4f7f6; color: #333; margin: 0; padding: 20px; }
	h1, h2, h3 { color: #2c3e50; }
	.container { max-width: 1200px; margin: 0 auto; background: #fff; padding: 20px; box-shadow: 0 4px 6px rgba(0,0,0,0.1); border-radius: 8px; }
	.summary-box { display: flex; flex-wrap: wrap; gap: 15px; margin-bottom: 30px; }
	.card { background: #ecf0f1; padding: 15px; border-radius: 6px; flex: 1; min-width: 150px; text-align: center; }
	.card h3 { margin: 0 0 10px 0; font-size: 1.1em; color: #7f8c8d; }
	.card p { margin: 0; font-size: 1.8em; font-weight: bold; }
	.sev-CRITICAL { border-left: 5px solid #e74c3c; }
	.sev-HIGH { border-left: 5px solid #9b59b6; }
	.sev-MEDIUM { border-left: 5px solid #f1c40f; }
	.sev-LOW { border-left: 5px solid #3498db; }
	.sev-CLEAR { border-left: 5px solid #2ecc71; }
	table { width: 100%%; border-collapse: collapse; margin-bottom: 30px; font-size: 0.95em; }
	th, td { padding: 12px 15px; text-align: left; border-bottom: 1px solid #ddd; }
	th { background-color: #2c3e50; color: #fff; }
	tr:hover { background-color: #f1f1f1; }
	.badge { padding: 4px 8px; border-radius: 4px; color: #fff; font-size: 0.85em; font-weight: bold; }
	.bg-CRITICAL { background-color: #e74c3c; }
	.bg-HIGH { background-color: #9b59b6; }
	.bg-MEDIUM { background-color: #f1c40f; color: #333; }
	.bg-LOW { background-color: #3498db; }
	.bg-CLEAR { background-color: #2ecc71; }
	.bg-NOISE { background-color: #95a5a6; }
	.bg-SUSPECTED { background-color: #34495e; }
	.finding-details { background: #f9f9f9; padding: 10px; border: 1px solid #e0e0e0; border-radius: 4px; margin-top: 5px; font-family: monospace; font-size: 0.9em; white-space: pre-wrap; word-wrap: break-word;}
	.recommendation { margin-top: 10px; color: #0056b3; font-weight: bold; }
</style>
</head>
<body>
<div class="container">
	<h1>ESXi 크리티컬 로그 점검 리포트</h1>
	<p><strong>생성 시간:</strong> %s</p>
	<p><strong>대상 호스트:</strong> %d대 (Findings 있음: %d / 수집실패: %d / 무응답: %d)</p>

	<h2>1. 전체 집계</h2>
	<div class="summary-box">
		<div class="card sev-CRITICAL"><h3>CRITICAL</h3><p>%d</p></div>
		<div class="card sev-HIGH"><h3>HIGH</h3><p>%d</p></div>
		<div class="card sev-MEDIUM"><h3>MEDIUM</h3><p>%d</p></div>
		<div class="card sev-LOW"><h3>LOW</h3><p>%d</p></div>
		<div class="card sev-CLEAR"><h3>CLEAR</h3><p>%d</p></div>
	</div>

	<h2>2. 호스트별 요약 (심각도 순)</h2>
	<table>
		<thead>
			<tr>
				<th>호스트</th>
				<th>CRITICAL</th>
				<th>HIGH</th>
				<th>MEDIUM</th>
				<th>LOW</th>
				<th>CLEAR</th>
				<th>NOISE</th>
				<th>SUSPECTED</th>
			</tr>
		</thead>
		<tbody>
`, r.GeneratedAt, r.TargetHostCount, len(r.HostSummary), r.FailedHostCount, len(r.SilentHosts),
		r.Total.Critical, r.Total.High, r.Total.Medium, r.Total.Low, r.Total.Clear)

	hosts := make([]string, 0, len(r.HostSummary))
	for h := range r.HostSummary {
		hosts = append(hosts, h)
	}
	sort.Slice(hosts, func(i, j int) bool {
		si, sj := r.HostSummary[hosts[i]], r.HostSummary[hosts[j]]
		scoreI, scoreJ := hostSeverityScore(si), hostSeverityScore(sj)
		if scoreI != scoreJ {
			return scoreI > scoreJ
		}
		return hosts[i] < hosts[j]
	})

	hiddenClean := 0
	for _, h := range hosts {
		s := r.HostSummary[h]
		if opts.OnlyProblems && hostSeverityScore(s) == 0 {
			hiddenClean++
			continue
		}
		fmt.Fprintf(w, "<tr><td>%s</td><td>%d</td><td>%d</td><td>%d</td><td>%d</td><td>%d</td><td>%d</td><td>%d</td></tr>\n",
			h, s.Critical, s.High, s.Medium, s.Low, s.Clear, s.Noise, s.Suspected)
	}

	fmt.Fprintf(w, `		</tbody>
	</table>
`)
	if hiddenClean > 0 {
		fmt.Fprintf(w, "<p><em>* 정상 호스트 %d대는 생략되었습니다.</em></p>", hiddenClean)
	}

	fmt.Fprintf(w, `<h2>3. CRITICAL / HIGH 로그 상세</h2>`)
	highlighted := filterAndSort(r.Findings, "CRITICAL", "HIGH")
	if len(highlighted) == 0 {
		fmt.Fprintln(w, "<p>해당 항목 없음</p>")
	} else {
		for _, f := range highlighted {
			renderFindingHTML(w, f)
		}
	}

	fmt.Fprintf(w, `<h2>4. 수집 실패 / 무응답 호스트</h2>`)
	if len(r.FailedHosts) == 0 && len(r.SilentHosts) == 0 {
		fmt.Fprintln(w, "<p>해당 항목 없음</p>")
	} else {
		if len(r.FailedHosts) > 0 {
			fmt.Fprintln(w, "<h3>수집 실패 (명령어/SSH 에러)</h3><ul>")
			for _, fh := range r.FailedHosts {
				fmt.Fprintf(w, "<li><strong>%s</strong>: %s <br><small>%s</small></li>\n", fh.Host, fh.Reason, htmlEscape(fh.Evidence))
			}
			fmt.Fprintln(w, "</ul>")
		}
		if len(r.SilentHosts) > 0 {
			fmt.Fprintln(w, "<h3>무응답 (목록에 있으나 결과 없음)</h3><ul>")
			for _, h := range r.SilentHosts {
				fmt.Fprintf(w, "<li>%s</li>\n", h)
			}
			fmt.Fprintln(w, "</ul>")
		}
	}

	fmt.Fprintf(w, `<h2>5. 기타 항목 (MEDIUM / LOW / CLEAR)</h2>`)
	other := filterAndSort(r.Findings, "MEDIUM", "LOW", "CLEAR")
	if len(other) == 0 {
		fmt.Fprintln(w, "<p>해당 항목 없음</p>")
	} else {
		for _, f := range other {
			renderFindingHTML(w, f)
		}
	}

	fmt.Fprintln(w, "</div></body></html>")
}

func renderFindingHTML(w io.Writer, f match.Finding) {
	fmt.Fprintf(w, `<div style="margin-bottom: 20px; padding: 15px; border-left: 4px solid #333; background: #fff; box-shadow: 0 1px 3px rgba(0,0,0,0.05);">`)
	fmt.Fprintf(w, `<div><span class="badge bg-%s">%s</span> <strong>%s</strong> (%s) @ %s</div>`, f.Severity, f.Severity, f.PatternID, f.Category, f.Host)
	fmt.Fprintf(w, `<div style="margin-top: 8px; font-size: 0.85em; color: #555;">소스: %s (라인 %d)</div>`, f.Source, f.LineNo)
	fmt.Fprintf(w, `<div class="finding-details">%s</div>`, htmlEscape(f.Line))
	if f.Recommendation != "" {
		fmt.Fprintf(w, `<div class="recommendation">조치 권고: %s</div>`, htmlEscape(f.Recommendation))
	}
	if f.DynamicNote != "" {
		fmt.Fprintf(w, `<div style="color:#d35400; font-weight:bold; margin-top:5px;">Dynamic: %s</div>`, htmlEscape(f.DynamicNote))
	}
	if f.AggregateNote != "" {
		fmt.Fprintf(w, `<div style="color:#8e44ad; font-weight:bold; margin-top:5px;">Aggregate: %s</div>`, htmlEscape(f.AggregateNote))
	}
	fmt.Fprintln(w, `</div>`)
}

func htmlEscape(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	return s
}
