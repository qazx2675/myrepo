// render_test.go - 렌더 순수 함수 테스트: 한글 폭, 막대, 골든(색 on/off × 폭 40/60/80/120 × 샘플), bindpw·제어문자 미노출, 좁은 화면 줄바꿈
package main

import (
	"flag"
	"os"
	"path/filepath"
	"strconv"
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

// sampleLong: 좁은 화면 줄바꿈 확인용 — 긴 job 이름·all yml·영문/한글 yml 이름·긴 호스트명·긴 비고, 수동 run 기록
func sampleLong() Snapshot {
	j := tsJob("20261008-103043-alice-infra-team-long-job", "alice", false,
		tsGroup("infra_inventory-20261008103043_4ea.yml", "인피니밴드", "rhel8.10", "pxe", "y",
			tsHost("node-a01", StageDone, 100, "code 4821"),
			tsHost("server-abcdefghijklmnopqrstuvwxyz-0123456789", StageInstalling, 700, "비고 한글 설명이 아주 길게 이어지는 경우에도 줄바꿈되어 모두 보여야 합니다 끝"),
			tsHost("node-a03", StageStuck, 4500, "LDAP 적용실패, 이름 미해결 - 관리자에게 문의 (재시도 3회 모두 실패) 마지막")),
		tsGroup("한글로_아주_긴_그룹_이름을_가진_인벤토리_파일-20261008103043.yml", "eth", "rhel7", "pxe", "n",
			tsHost("gpu001", StageReady, 30, "")))
	j.AllYml = "infra_inventory-20261008103043_all_hosts.yml"
	j.Runs = []Run{{Code: "5012", Hosts: []string{"node-a01"}, At: tsNow - 120, Manual: true, Yml: j.Groups[0].Yml}}
	j.Manual = []SnapManual{{Yml: j.Groups[1].Yml, State: "waiting", Requested: tsNow - 30}}
	return tsSnap(true, j)
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
	}{{"normal", sampleNormal()}, {"stuck", sampleStuck()}, {"alldone", sampleAllDone()}, {"nojob", sampleNoJob()}, {"korean", sampleKorean()},
		{"long", sampleLong()}}
	for _, sm := range samples {
		for _, color := range []bool{false, true} {
			for _, w := range []int{40, 60, 80, 120} {
				mode := "plain"
				if color {
					mode = "color"
				}
				name := "tui_" + sm.name + "_" + mode + "_" + strconv.Itoa(w) + ".golden"
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
	for _, s := range []Snapshot{sampleNormal(), sampleStuck(), sampleAllDone(), sampleNoJob(), sampleKorean(), sampleLong()} {
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
	// 화면 2: 완료 여부와 상관없이 수동 실행 활성 (미완료 그룹도 비활성 안내 없음)
	d := renderDetail(s, selection{Detail: true, JobID: "J1", Yml: "web.yml"}, 120, 24, false)
	if strings.Contains(d, "비활성") || !strings.Contains(d, "[c] 설정체크 + 설정수정") || !strings.Contains(d, "LDAP 적용실패") || !strings.Contains(d, "정체") {
		t.Errorf("화면 2:\n%s", d)
	}
	a := sampleAllDone()
	d = renderDetail(a, selection{Detail: true, JobID: "J1", Yml: "gpu.yml"}, 120, 24, false)
	if strings.Contains(d, "비활성") || !strings.Contains(d, "[c] 설정체크 + 설정수정") {
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

// hintLines: 한 줄에 들어가면 합치고, 아니면 묶음별로 나누며, 어떤 폭에서도 폭을 넘지 않고 항목을 잃지 않는다
func TestHintLines(t *testing.T) {
	hint := " ↑↓ 이동  ← 목록  q 복귀\n c 체크+수정  t 체크만  w AWX"
	wide := hintLines(false, 120, hint)
	if len(wide) != 1 || !strings.Contains(wide[0], "q 복귀  c 체크+수정") {
		t.Fatalf("넓으면 한 줄: %q", wide)
	}
	mid := hintLines(false, 40, hint)
	if len(mid) != 2 || !strings.HasPrefix(mid[1], " c 체크+수정") {
		t.Fatalf("묶음별 두 줄: %q", mid)
	}
	for w := 12; w <= 130; w++ {
		for _, color := range []bool{false, true} {
			lines := hintLines(color, w, hint)
			var plain string
			for _, l := range lines {
				p := stripANSI(l)
				if strWidth(p) > w {
					t.Fatalf("폭 %d 초과: %q", w, p)
				}
				plain += p + " "
			}
			if w >= 30 { // 항목이 잘리지 않는 폭에서는 모든 항목이 남는다
				for _, it := range []string{"↑↓ 이동", "← 목록", "q 복귀", "c 체크+수정", "t 체크만", "w AWX"} {
					if !strings.Contains(plain, it) {
						t.Fatalf("폭 %d 에서 %q 사라짐: %q", w, it, plain)
					}
				}
			}
		}
	}
}

// ---- 좁은 화면 줄바꿈 (S5) ----

const longMsg = "[X] 상태 갱신 실패: open /var/lib/auto_setup/state/jobs/20261008-103043-alice.json: permission denied (권한 확인 필요)"

// wideRenders: allRenders + 날짜 줄·그룹 선택·전체 보기·확인 질문·긴 안내·결과 창
func wideRenders(s Snapshot, width int, color bool) []string {
	out := allRenders(s, width, color)
	n := len(flatGroups(s))
	for row := -1; row < n; row++ {
		out = append(out, renderOverview(s, selection{Row: row, DateFilter: true, Msg: longMsg}, width, 24, color))
	}
	for _, j := range s.Jobs {
		sel := selection{Detail: true, JobID: j.ID, Yml: allView, Host: 1, Msg: longMsg}
		out = append(out, renderDetail(s, sel, width, 24, color), renderDetail(s, sel, width, 0, color))
		for _, g := range j.Groups {
			for _, mode := range []string{"", ModeCheck} {
				sel := selection{Detail: true, JobID: j.ID, Yml: g.Yml, Host: 2, Confirm: true, Mode: mode, Msg: longMsg}
				out = append(out, renderDetail(s, sel, width, 30, color))
			}
		}
	}
	v := &viewState{kind: viewRun, done: true, title: "수동 실행 [설정체크 + 설정수정] - infra_inventory-20261008103043_4ea.yml",
		status: "완료 - code 5012  (auto_setup code 5012) - 결과 파일 /var/lib/auto_setup/codes/5012.txt",
		lines: []string{"작업 : 설정체크 + 설정수정", "server-abcdefghijklmnopqrstuvwxyz-0123456789-and-a-very-long-suffix-without-spaces-at-all",
			"127.0.0.14  접속불가  os8 무응답 → os6 무응답 → 접속불가로 판정되어 다음 주기에 다시 시도합니다 (최대 3회)"}}
	out = append(out, renderView(v, width, 24, color), renderView(v, width, 0, color))
	v.done, v.kind = false, viewRecheck
	out = append(out, renderView(v, width, 24, color))
	return out
}

func wrapSamples() []Snapshot {
	return []Snapshot{sampleNormal(), sampleStuck(), sampleAllDone(), sampleNoJob(), sampleKorean(), sampleLong()}
}

// 폭 ≥ 60 이면 어떤 줄에도 '…' 가 없고(잘림 없이 줄바꿈) 어떤 줄도 폭을 넘지 않는다
func TestRenderWrapNoEllipsis(t *testing.T) {
	for si, s := range wrapSamples() {
		for _, w := range []int{60, 61, 63, 67, 72, 79, 80, 81, 99, 100, 119, 120, 150} {
			for _, color := range []bool{false, true} {
				for ri, r := range wideRenders(s, w, color) {
					for _, line := range strings.Split(r, "\n") {
						p := stripANSI(line)
						if strings.Contains(p, "…") {
							t.Fatalf("sample %d render %d 폭 %d color=%v: … 로 잘림: %q\n%s", si, ri, w, color, p, stripANSI(r))
						}
						if strWidth(p) > w {
							t.Fatalf("sample %d render %d 폭 %d 초과(%d): %q", si, ri, w, strWidth(p), p)
						}
					}
				}
			}
		}
	}
}

// 줄바꿈해도 내용이 사라지지 않는다 (긴 이름·비고·안내가 공백을 빼면 그대로 화면에 있다)
func TestRenderWrapKeepsContent(t *testing.T) {
	s := sampleLong()
	j := s.Jobs[0]
	squash := func(x string) string { return strings.Join(strings.Fields(stripANSI(x)), "") }
	for _, w := range []int{60, 80} {
		ov := squash(renderOverview(s, selection{Row: 0, Msg: longMsg}, w, 40, true))
		for _, want := range []string{j.ID, j.AllYml, j.Groups[0].Yml, j.Groups[1].Yml, "인피니밴드/rhel8.10/pxe/y", "33%", longMsg} {
			if !strings.Contains(ov, squash(want)) {
				t.Errorf("폭 %d 화면 1 에 %q 없음:\n%s", w, want, renderOverview(s, selection{Row: 0}, w, 40, false))
			}
		}
		// 화면 2: 표 칸 안 줄바꿈은 옆 칸과 섞이므로 단어 단위로 확인 (긴 호스트명은 앞·뒤 조각)
		d := renderDetail(s, selection{Detail: true, JobID: j.ID, Yml: j.Groups[0].Yml, Host: 1, Confirm: true}, w, 40, false)
		words := append(strings.Fields(j.Groups[0].Hosts[1].Note), strings.Fields(j.Groups[0].Hosts[2].Note)...)
		words = append(words, "server-abcdefghij", "0123456789", "[y/n]", "code 5012")
		for _, want := range words {
			if !strings.Contains(d, want) {
				t.Errorf("폭 %d 화면 2 에 %q 없음:\n%s", w, want, d)
			}
		}
	}
}

// 넓은 폭에서는 종전 한 줄 형태 유지 (job 헤더·그룹 행이 한 줄), 좁으면 그룹 행은 2줄 카드형
func TestRenderWideKeepsOneLine(t *testing.T) {
	s := sampleLong()
	ov := renderOverview(s, selection{Row: -5}, 120, 0, false)
	if !strings.Contains(ov, " job "+s.Jobs[0].ID+"  alice  전달 ") || !strings.Contains(ov, "  all="+s.Jobs[0].AllYml) {
		t.Errorf("job 헤더 한 줄:\n%s", ov)
	}
	for _, l := range strings.Split(ov, "\n") {
		if strings.HasPrefix(l, "  infra_inventory-") && !strings.HasSuffix(l, "33%") {
			t.Errorf("120칼 그룹 행은 한 줄이어야 함: %q", l)
		}
	}
	// 80칼: 이름이 칸보다 길면 2줄 카드형 (1줄 전체 이름, 2줄 구성·대수·막대·경과·%)
	lines := strings.Split(renderOverview(s, selection{Row: 0}, 80, 0, false), "\n")
	for i, l := range lines {
		if l == "> "+s.Jobs[0].Groups[0].Yml {
			if i+1 >= len(lines) || !strings.HasPrefix(lines[i+1], "  인피니밴드/rhel8.10/pxe/y ") || !strings.HasSuffix(lines[i+1], "33%") {
				t.Fatalf("카드 2줄:\n%s", strings.Join(lines, "\n"))
			}
			return
		}
	}
	t.Fatalf("카드 1줄(전체 이름) 없음:\n%s", strings.Join(lines, "\n"))
}

// 선택 행 반전(\x1b[7m)은 선택 행(줄바꿈된 줄 전부)에만 — 안내 줄·다른 행에는 없다
func TestRenderReverseOnlySelected(t *testing.T) {
	revLines := func(r string) (idx []int, lines []string) {
		lines = strings.Split(r, "\n")
		for i, l := range lines {
			if strings.Contains(l, "\x1b[7m") {
				idx = append(idx, i)
			}
		}
		return
	}
	contiguous := func(idx []int) bool {
		for i := 1; i < len(idx); i++ {
			if idx[i] != idx[i-1]+1 {
				return false
			}
		}
		return true
	}
	for _, s := range wrapSamples() {
		for _, w := range []int{60, 80, 120} {
			for _, r := range wideRenders(s, w, true) {
				if idx, _ := revLines(r); !contiguous(idx) {
					t.Fatalf("폭 %d: 반전 줄이 한 덩어리가 아님 %v:\n%s", w, idx, goldenText(r))
				}
			}
		}
	}
	s := sampleLong()
	j := s.Jobs[0]
	// 화면 1: 카드형 그룹 행 2줄 모두 반전, 안내 줄 반전 없음
	idx, lines := revLines(renderOverview(s, selection{Row: 0}, 60, 24, true))
	if len(idx) < 2 || !strings.HasPrefix(stripANSI(lines[idx[0]]), "> infra_inventory-") || !strings.Contains(stripANSI(lines[idx[len(idx)-1]]), "33%") {
		t.Fatalf("카드 행 반전 %v:\n%s", idx, goldenText(strings.Join(lines, "\n")))
	}
	// 화면 2: 줄바꿈된 호스트 행 전체가 반전 (호스트명 첫 줄 ~ 비고 끝)
	idx, lines = revLines(renderDetail(s, selection{Detail: true, JobID: j.ID, Yml: j.Groups[0].Yml, Host: 1}, 60, 24, true))
	var sb strings.Builder
	for _, i := range idx {
		sb.WriteString(stripANSI(lines[i]))
	}
	if len(idx) < 2 || !strings.HasPrefix(stripANSI(lines[idx[0]]), "> server-") || !strings.Contains(sb.String(), "끝") {
		t.Fatalf("호스트 행 반전 %v:\n%s", idx, goldenText(strings.Join(lines, "\n")))
	}
}

// 색이 켜진 모든 줄은 마지막 SGR 이 리셋(\x1b[0m) — 지우고 다시 그릴 때 색·반전이 줄 끝(\x1b[K)으로 새지 않는다
func TestRenderLinesEndReset(t *testing.T) {
	for _, s := range wrapSamples() {
		for _, w := range []int{40, 60, 80, 120} {
			for _, r := range wideRenders(s, w, true) {
				for _, l := range strings.Split(r, "\n") {
					if i := strings.LastIndex(l, "\x1b["); i >= 0 && !strings.HasPrefix(l[i:], "\x1b[0m") {
						t.Fatalf("폭 %d: 줄 끝 리셋 없음: %q", w, l)
					}
				}
			}
		}
	}
}

// 스크롤: 여러 줄로 줄바꿈된 행도 커서 행 전체가 보이고, 본문이 행 중간에서 시작하지 않는다
func TestRenderScrollWrappedRows(t *testing.T) {
	var hosts []SnapHost
	for i := 0; i < 30; i++ {
		name := "h" + string(rune('a'+i%26)) + string(rune('a'+i/26))
		hosts = append(hosts, tsHost(name, StageInstalling, 100, "비고 "+strings.Repeat("길게 ", 12)+"끝-"+name))
	}
	s := tsSnap(true, tsJob("J1", "u", false, tsGroup("big.yml", "", "", "", "", hosts...)))
	for _, w := range []int{60, 80} {
		for _, row := range []int{0, 1, 10, 28, 29} {
			out := renderDetail(s, selection{Detail: true, JobID: "J1", Yml: "big.yml", Host: row}, w, 22, false)
			lines := strings.Split(out, "\n")
			if len(lines) > 22 {
				t.Fatalf("높이 초과 %d", len(lines))
			}
			if !strings.Contains(out, "> "+padR(hosts[row].Host, 8)) || !strings.Contains(out, "끝-"+hosts[row].Host) {
				t.Errorf("폭 %d row %d 커서 행 전체가 안 보임:\n%s", w, row, out)
			}
			for i, l := range lines {
				if strings.HasPrefix(l, "  호스트") && i+1 < len(lines) {
					if n := lines[i+1]; !strings.HasPrefix(n, "  h") && !strings.HasPrefix(n, "> h") {
						t.Errorf("폭 %d row %d: 본문이 행 중간에서 시작: %q\n%s", w, row, n, out)
					}
				}
			}
		}
	}
}
