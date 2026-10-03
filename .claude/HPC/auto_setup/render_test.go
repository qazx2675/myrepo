// render_test.go - 렌더 순수 함수 테스트: 한글 폭, 막대, 골든(색 on/off × 폭 80/120 × 샘플), bindpw·제어문자 미노출
package main

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var updateGolden = flag.Bool("update", false, "testdata/tui_*.golden 갱신")

const tsNow int64 = 1700003600

// ---- 샘플 스냅샷 빌더 ----

func tsHost(name string, st Stage, elapsed int64, note string) SnapHost {
	h := SnapHost{Host: name, Stage: st, Label: st.Label(), Elapsed: elapsed, Note: note}
	if elapsed > 0 {
		h.StageAt = tsNow - elapsed
	}
	switch st {
	case StageDeploying, StageInstalling, StageBooting, StageStuck:
		h.DownAt = tsNow - elapsed
	case StageDone:
		h.Done = "4821"
		h.DoneSrc = "run"
	}
	return h
}

func tsGroup(yml, infra, osv, boot, splunk string, hosts ...SnapHost) SnapGroup {
	g := SnapGroup{Yml: yml, Infra: infra, OS: osv, Boot: boot, Splunk: splunk, Hosts: hosts, Complete: len(hosts) > 0}
	for _, h := range hosts {
		g.Counts.Add(h.Stage)
		if h.Stage != StageDone {
			g.Complete = false
			if h.Stage != StageFailed && h.Elapsed > g.MaxElapsed {
				g.MaxElapsed = h.Elapsed
			}
		}
	}
	return g
}

func tsJob(id, user string, closed bool, groups ...SnapGroup) SnapJob {
	j := SnapJob{ID: id, User: user, Submitted: tsNow - 7200, AllYml: "(all).yaml", Closed: closed,
		Groups: groups, Runs: []Run{}, Manual: []SnapManual{}}
	for _, g := range groups {
		j.Counts.merge(g.Counts)
	}
	return j
}

func tsSnap(daemon bool, jobs ...SnapJob) Snapshot {
	s := Snapshot{Version: 1, Now: tsNow, NowText: time.Unix(tsNow, 0).UTC().Format(snapTimeLayout), Jobs: jobs}
	if s.Jobs == nil {
		s.Jobs = []SnapJob{}
	}
	if daemon {
		s.Daemon = SnapDaemon{Running: true, PID: 4242, Started: tsNow - 86400}
	}
	for _, j := range jobs {
		if !j.Closed {
			s.Totals.merge(j.Counts)
		}
	}
	return s
}

func sampleNormal() Snapshot {
	return tsSnap(true, tsJob("J1", "user1", false,
		tsGroup("web.yml", "ib", "rhel8", "pxe", "y",
			tsHost("web001", StageDone, 100, ""), tsHost("web002", StageDone, 90, ""),
			tsHost("web003", StageInstalling, 1500, ""), tsHost("web004", StageBooting, 240, ""),
			tsHost("web005", StageDeploying, 600, ""), tsHost("web006", StageQueued, 0, "")),
		tsGroup("gpu.yml", "eth", "rhel7", "pxe", "n",
			tsHost("gpu001", StageReady, 30, ""), tsHost("gpu002", StageLdap, 20, "LDAP 복원"),
			tsHost("gpu003", StageChecking, 120, "LDAP 동일"))))
}

func sampleStuck() Snapshot {
	g := tsGroup("web.yml", "ib", "rhel8", "pxe", "y",
		tsHost("web001", StageDone, 100, "code 4821"),
		tsHost("web002", StageStuck, 4000, "이름 미해결"),
		tsHost("web003", StageFailed, 300, "LDAP 적용실패, 실패 3회"),
		tsHost("web004", StageInstalling, 900, ""))
	g.Hosts[2].Ldap = &LdapState{Backup: "ok", Bindpw: "diff", Reason: "bindpw=SECRET123 복원 실패"}
	return tsSnap(true, tsJob("J1", "user1", false, g))
}

func sampleAllDone() Snapshot {
	j := tsJob("J1", "user1", false,
		tsGroup("web.yml", "ib", "rhel8", "pxe", "y", tsHost("web001", StageDone, 100, "code 4821"), tsHost("web002", StageDone, 90, "타경로 완료기록")),
		tsGroup("gpu.yml", "eth", "rhel7", "pxe", "n", tsHost("gpu001", StageDone, 80, "code 4821, 2차 OK")))
	j.Runs = []Run{{Code: "4821", Hosts: []string{"web001"}, At: tsNow - 600}, {Code: "5012", Hosts: []string{"web001", "web002"}, At: tsNow - 120, Manual: true, Yml: "web.yml"}}
	closed := tsJob("J0", "user2", true, tsGroup("old.yml", "ib", "rhel8", "pxe", "y", tsHost("old001", StageDone, 9000, "")))
	return tsSnap(true, j, closed)
}

func sampleNoJob() Snapshot { return tsSnap(false) }

func sampleKorean() Snapshot {
	return tsSnap(true, tsJob("J1", "김철수", false,
		tsGroup("한글그룹.yml", "인피니", "rhel8", "pxe", "y",
			tsHost("서버-가나다-01", StageDone, 100, "code 4821"),
			tsHost("서버-가나다-02", StageInstalling, 700, "비고 한글 설명이 아주 길게 이어지는 경우에도 화면이 깨지지 않아야 합니다 끝"),
			tsHost("server-abcdefghijklmnopqrstuvwxyz-0123456789", StageStuck, 4500, ""))))
}

// ---- 폭 계산 ----

func TestRuneWidth(t *testing.T) {
	cases := []struct {
		r    rune
		want int
	}{{'a', 1}, {'0', 1}, {' ', 1}, {'한', 2}, {'가', 2}, {'힣', 2}, {'漢', 2}, {'ｗ', 2}, {'ㄱ', 2}, {'…', 1}, {'\x1b', 0}, {'\t', 0}, {0x0301, 0}, {'●', 1}}
	for _, c := range cases {
		if got := runeWidth(c.r); got != c.want {
			t.Errorf("runeWidth(%q)=%d want %d", c.r, got, c.want)
		}
	}
	if strWidth("서버01") != 6 || strWidth("abc") != 3 || strWidth("") != 0 {
		t.Error("strWidth")
	}
}

func TestTruncPad(t *testing.T) {
	cases := []struct {
		s    string
		w    int
		want string
	}{{"abc", 3, "abc"}, {"abcd", 3, "ab…"}, {"서버서버", 5, "서버…"}, {"서버서버", 7, "서버서…"}, {"서버", 4, "서버"}, {"abc", 0, ""}, {"abc", 1, "…"}}
	for _, c := range cases {
		if got := truncW(c.s, c.w); got != c.want {
			t.Errorf("truncW(%q,%d)=%q want %q", c.s, c.w, got, c.want)
		}
		if strWidth(truncW(c.s, c.w)) > c.w {
			t.Errorf("truncW 폭 초과 %q", c.s)
		}
	}
	if padR("한", 4) != "한  " || padL("한", 4) != "  한" || padR("abcdef", 4) != "abc…" {
		t.Errorf("pad: %q %q %q", padR("한", 4), padL("한", 4), padR("abcdef", 4))
	}
	if strWidth(padR("서버-가나다", 9)) != 9 {
		t.Error("padR 한글 폭")
	}
}

func TestFmtDur(t *testing.T) {
	cases := map[int64]string{-5: "0s", 0: "0s", 59: "59s", 60: "1m00s", 725: "12m05s", 3600: "1h00m", 3720: "1h02m", 86400: "1d00h", 90000: "1d01h"}
	for in, want := range cases {
		if got := fmtDur(in); got != want {
			t.Errorf("fmtDur(%d)=%q want %q", in, got, want)
		}
	}
}

func TestFmtClockUsesSnapshotZone(t *testing.T) {
	s := Snapshot{Now: 1700000000, NowText: "2023-11-15 07:13:20"} // UTC 22:13:20 → +9시간대
	if got := fmtClock(s, 1700000000-60, "15:04:05"); got != "07:12:20" {
		t.Errorf("fmtClock=%q", got)
	}
}

func TestBarSegsWidth(t *testing.T) {
	for _, c := range []StageCounts{
		{Total: 1, Done: 1}, {Total: 3, Done: 1, Queued: 1, Installing: 1}, {Total: 100, Done: 1, Stuck: 1, Queued: 98},
		{Total: 7, Done: 2, Deploying: 2, Failed: 1, Queued: 2}, {Total: 200, Done: 199, Failed: 1}, {Total: 0},
	} {
		for _, w := range []int{1, 4, 12, 20} {
			if c.Total > 0 && c.Total < 4 && w < 4 {
				continue
			}
			segs := barSegs(c, w)
			if got := strWidth(segsText(segs)); got != w {
				t.Errorf("barSegs(%+v,%d) 폭 %d", c, w, got)
			}
		}
	}
	// 0 보다 큰 구간은 최소 1칸
	txt := segsText(barSegs(StageCounts{Total: 100, Done: 99, Failed: 1}, 12))
	if !strings.Contains(txt, "!") || !strings.Contains(txt, "#") {
		t.Errorf("최소 1칸: %q", txt)
	}
}

// ---- 골든 ----

func goldenText(s string) string { return strings.ReplaceAll(s, "\x1b", "\\e") }

func renderAll(s Snapshot, width int, color bool) string {
	var sb strings.Builder
	sel := selection{}
	sb.WriteString("== overview ==\n" + renderOverview(s, sel, width, 24, color) + "\n")
	if refs := flatGroups(s); len(refs) > 0 {
		j := s.Jobs[refs[0].Job]
		sel = selection{Detail: true, JobID: j.ID, Yml: j.Groups[refs[0].Grp].Yml, Host: 1}
	} else {
		sel = selection{Detail: true, JobID: "none", Yml: "none"}
	}
	sb.WriteString("== detail ==\n" + renderDetail(s, sel, width, 24, color) + "\n")
	sel.Filter = true
	sb.WriteString("== detail filter ==\n" + renderDetail(s, sel, width, 24, color) + "\n")
	return sb.String()
}

func TestRenderGolden(t *testing.T) {
	samples := []struct {
		name string
		s    Snapshot
	}{{"normal", sampleNormal()}, {"stuck", sampleStuck()}, {"alldone", sampleAllDone()}, {"nojob", sampleNoJob()}, {"korean", sampleKorean()}}
	for _, sm := range samples {
		for _, color := range []bool{false, true} {
			for _, w := range []int{80, 120} {
				mode := "plain"
				if color {
					mode = "color"
				}
				name := "tui_" + sm.name + "_" + mode + "_" + map[int]string{80: "80", 120: "120"}[w] + ".golden"
				got := goldenText(renderAll(sm.s, w, color))
				path := filepath.Join("testdata", name)
				if *updateGolden {
					if err := os.WriteFile(path, []byte(got), 0644); err != nil {
						t.Fatal(err)
					}
					continue
				}
				want, err := os.ReadFile(path)
				if err != nil {
					t.Fatalf("골든 없음 %s (go test -run Golden -update): %v", name, err)
				}
				if string(want) != got {
					t.Errorf("%s 불일치\n--- got ---\n%s", name, got)
				}
			}
		}
	}
}

// ---- 속성: 폭·색·비밀·제어문자 ----

func allRenders(s Snapshot, width int, color bool) []string {
	out := []string{renderOverview(s, selection{}, width, 24, color), renderOverview(s, selection{}, width, 0, color),
		renderHelp(width, 24, color), renderPlain(s, width)}
	for _, j := range s.Jobs {
		for _, g := range j.Groups {
			for _, f := range []bool{false, true} {
				sel := selection{Detail: true, JobID: j.ID, Yml: g.Yml, Filter: f, Confirm: g.Complete, Msg: "요청됨 — 테스트"}
				out = append(out, renderDetail(s, sel, width, 24, color), renderDetail(s, sel, width, 0, color))
			}
		}
	}
	return out
}

func TestRenderWidthNeverExceeded(t *testing.T) {
	for _, s := range []Snapshot{sampleNormal(), sampleStuck(), sampleAllDone(), sampleNoJob(), sampleKorean()} {
		for _, w := range []int{40, 60, 80, 120, 200} {
			for _, color := range []bool{false, true} {
				for _, r := range allRenders(s, w, color) {
					for _, line := range strings.Split(r, "\n") {
						if got := strWidth(stripANSI(line)); got > w {
							t.Fatalf("폭 %d 초과(%d) color=%v: %q", w, got, color, stripANSI(line))
						}
					}
				}
			}
		}
	}
}

func TestRenderColorOff(t *testing.T) {
	for _, s := range []Snapshot{sampleNormal(), sampleStuck(), sampleKorean()} {
		for _, r := range allRenders(s, 80, false) {
			if strings.Contains(r, "\x1b") {
				t.Fatalf("color=false 인데 ANSI 포함: %q", r)
			}
		}
	}
	on := renderOverview(sampleStuck(), selection{}, 80, 24, true)
	if !strings.Contains(on, "\x1b[1;31m") || !strings.Contains(on, "\x1b[32m") || !strings.Contains(on, "\x1b[0m") {
		t.Errorf("색 on 인데 빨강/초록 없음: %q", on)
	}
}

func TestRenderNoBindpwNoControl(t *testing.T) {
	s := sampleStuck()
	s.Jobs[0].Groups[0].Hosts[0].Host = "evil\x1b[2Jhost"
	s.Jobs[0].Groups[0].Hosts[0].Note = "x\x1b]0;title\x07y"
	for _, color := range []bool{false, true} {
		for _, r := range allRenders(s, 120, color) {
			if strings.Contains(r, "SECRET123") {
				t.Fatalf("bindpw 값 노출: %q", r)
			}
			plain := stripANSI(r)
			if strings.Contains(plain, "\x1b") || strings.Contains(plain, "\x07") {
				t.Fatalf("제어문자 주입: %q", plain)
			}
		}
	}
}

func TestRenderContent(t *testing.T) {
	s := sampleStuck()
	ov := renderOverview(s, selection{}, 120, 24, false)
	for _, want := range []string{"전체 4", "완료 1", "진행 1", "정체 1", "실패 1", "● 데몬 실행", "web.yml", "ib/rhel8/pxe/y", "25%", "1h06m"} {
		if !strings.Contains(ov, want) {
			t.Errorf("화면 1 에 %q 없음:\n%s", want, ov)
		}
	}
	if !strings.Contains(renderOverview(sampleNoJob(), selection{}, 80, 24, false), "○ 데몬 중지") {
		t.Error("데몬 정지 표시")
	}
	// 화면 2: 미완료 그룹은 회색 비활성 안내, 완료 그룹은 활성
	d := renderDetail(s, selection{Detail: true, JobID: "J1", Yml: "web.yml"}, 120, 24, false)
	if !strings.Contains(d, "비활성") || !strings.Contains(d, "LDAP 적용실패") || !strings.Contains(d, "정체") {
		t.Errorf("화면 2:\n%s", d)
	}
	a := sampleAllDone()
	d = renderDetail(a, selection{Detail: true, JobID: "J1", Yml: "gpu.yml"}, 120, 24, false)
	if strings.Contains(d, "비활성") || !strings.Contains(d, "[c] OS 체크 수동 실행(이중체크)") {
		t.Errorf("완료 그룹 활성:\n%s", d)
	}
	d = renderDetail(a, selection{Detail: true, JobID: "J1", Yml: "web.yml", Confirm: true}, 120, 24, false)
	if !strings.Contains(d, "[y/n]") || !strings.Contains(d, "code 5012") {
		t.Errorf("확인/수동 code:\n%s", d)
	}
	// 필터: 정체·실패만
	f := renderDetail(s, selection{Detail: true, JobID: "J1", Yml: "web.yml", Filter: true}, 120, 24, false)
	if strings.Contains(f, "web001") || strings.Contains(f, "web004") || !strings.Contains(f, "web002") || !strings.Contains(f, "web003") {
		t.Errorf("필터:\n%s", f)
	}
	// 수동 run 진행 중 표시
	a.Jobs[0].Manual = []SnapManual{{Yml: "web.yml", State: "running", Requested: tsNow - 30}}
	if d = renderDetail(a, selection{Detail: true, JobID: "J1", Yml: "web.yml"}, 120, 24, false); !strings.Contains(d, "수동 run 진행 중") {
		t.Errorf("수동 진행:\n%s", d)
	}
}

func TestRenderScrollKeepsCursorVisible(t *testing.T) {
	var hosts []SnapHost
	for i := 0; i < 60; i++ {
		hosts = append(hosts, tsHost("h"+strings.Repeat("0", 2)+string(rune('a'+i%26))+string(rune('a'+i/26)), StageInstalling, 100, ""))
	}
	s := tsSnap(true, tsJob("J1", "u", false, tsGroup("big.yml", "", "", "", "", hosts...)))
	for _, row := range []int{0, 10, 59} {
		out := renderDetail(s, selection{Detail: true, JobID: "J1", Yml: "big.yml", Host: row}, 80, 15, false)
		lines := strings.Split(out, "\n")
		if len(lines) > 15 {
			t.Fatalf("높이 초과 %d", len(lines))
		}
		if !strings.Contains(out, "> "+padR(hosts[row].Host, 8)) {
			t.Errorf("row %d 커서가 안 보임:\n%s", row, out)
		}
	}
	// 화면 1 도 마찬가지
	var gs []SnapGroup
	for i := 0; i < 40; i++ {
		gs = append(gs, tsGroup("g"+string(rune('a'+i%26))+string(rune('a'+i/26))+".yml", "", "", "", "", tsHost("x", StageQueued, 0, "")))
	}
	s = tsSnap(true, tsJob("J1", "u", false, gs...))
	out := renderOverview(s, selection{Row: 39}, 80, 12, false)
	if len(strings.Split(out, "\n")) > 12 || !strings.Contains(out, "> "+gs[39].Yml) {
		t.Errorf("화면 1 스크롤:\n%s", out)
	}
}
