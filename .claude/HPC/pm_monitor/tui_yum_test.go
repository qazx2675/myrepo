package main

// yum update 모드의 화면·데몬(계획서 4.8, 6.3 의 22단계)과 21단계 이월 수정(재부팅 중 구분) 테스트.
// 기존 tui_test.go 의 골든·표본은 그대로 두고, 여기서 yum 서버가 섞인 새 표본·새 골든을 쓴다.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// yrebooting 은 재부팅됨이지만 재부팅 뒤 아직 접속되지 않는 yum 서버다(이유 `재부팅 중`).
func yrebooting(name, vendor string, sinceMin int) Record {
	r := yrec(name, vendor, StatusRebooted, sinceMin)
	r.Reason = "재부팅 중(ping 응답 없음)"
	return r
}

// ---- 21단계 이월: 재부팅됨 중 커널 동일 확인과 재부팅 중을 가른다 ----

func TestYumKernelSame(t *testing.T) {
	same := yrec("a", "D", StatusRebooted, 10)
	down := yrebooting("b", "D", 10)
	alive := yrec("c", "D", StatusAlive, 10)
	alive.Reason = "재부팅됨, 커널 동일 x" // 상태가 재부팅됨이 아니면 해당 없음
	for _, tc := range []struct {
		r    Record
		want bool
	}{{same, true}, {down, false}, {alive, false}, {Record{Status: StatusRebooted}, false}} {
		if got := yumKernelSame(tc.r); got != tc.want {
			t.Errorf("yumKernelSame(%s %q)=%v, want %v", tc.r.Status, tc.r.Reason, got, tc.want)
		}
	}
}

func TestInstallCommentYumRebootingSplit(t *testing.T) {
	// 재부팅됨 3대: 커널 동일 확인 1대, 재부팅 중 2대
	s := snapOf(yrec("a", "D", StatusYumDone, 200), yrec("same1", "D", StatusRebooted, 10),
		yrebooting("down1", "D", 10), yrebooting("down2", "sds", 10))
	if y := s.Yum; y.Rebooted != 3 || y.SameKernel != 1 || strings.Join(y.SameKernelHosts, ",") != "same1" ||
		strings.Join(y.RebootingHosts, ",") != "down1,down2" {
		t.Fatalf("집계: %+v", y)
	}
	want := strings.Join([]string{
		"설치현황",
		"yum update 4대 중 1대 커널 변경 완료(yum_update/접속가능), 재부팅됨 3대(커널 동일 1대)",
		"yum 재부팅 중 2대",
		"down1 down2",
		"yum 재부팅됨 커널 동일 1대",
		"same1",
	}, "\n")
	if got := InstallComment(s); got != want {
		t.Errorf("재부팅 중 분리\n--- want\n%s\n--- got\n%s", want, got)
	}
	// 커널 동일이 하나도 없으면 괄호를 쓰지 않는다
	got := InstallComment(snapOf(yrebooting("down1", "D", 10)))
	if want := "설치현황\nyum update 1대 중 0대 커널 변경 완료(yum_update/접속가능), 재부팅됨 1대\nyum 재부팅 중 1대\ndown1"; got != want {
		t.Errorf("커널 동일 없음: %q", got)
	}
	// 재부팅 중은 OS 요약의 재부팅됨 목록·완료 수에 들어가지 않는다
	s = snapOf(yrebooting("down1", "D", 10), orec("os1", "D", StatusDone))
	if s.Done() != 1 || strings.Contains(InstallComment(s), "\n재부팅됨 ") {
		t.Errorf("OS 집계 오염: done=%d\n%s", s.Done(), InstallComment(s))
	}
}

func TestDelayCommentYumRebootingClassC(t *testing.T) {
	// 지연 기준(60분)을 넘긴 재부팅됨: 벤더 D 는 A(대기)·B(커널 동일)·C(재부팅 뒤 접속 안 됨) 모두, sds 는 C 만
	s := snapOf(orec("o1", "D", StatusDelayed),
		yrec("a1", "D", StatusAlive, 90), yrec("b1", "D", StatusRebooted, 90), yrebooting("c2", "D", 90), yrebooting("c1", "D", 120),
		yrebooting("s1", "sds", 70), yrebooting("c3", "D", 5)) // c3 는 아직 지연 아님
	block := func(vendor, text string, hosts string, n int) string {
		return fmt.Sprintf("안녕하세요 %s 담당자님\n하기서버 %d대는 %s\n점검부탁드립니다.\n감사합니다.\n%s", vendor, n, text, hosts)
	}
	want := strings.Join([]string{
		block("D", "OS 설치가 지연되고 있습니다.", "o1", 1),
		block("D", "yum update 후 재부팅이 확인되지 않고 있습니다.", "a1", 1),
		block("D", "yum update 후 재부팅은 되었으나 커널이 변경되지 않았습니다.", "b1", 1),
		block("D", "yum update 후 재부팅한 뒤 접속되지 않고 있습니다.", "c1 c2", 2),
		block("sds", "yum update 후 재부팅한 뒤 접속되지 않고 있습니다.", "s1", 1),
	}, "\n\n")
	if got := DelayComment(s); got != want {
		t.Errorf("점검요청 C 블록\n--- want\n%s\n--- got\n%s", want, got)
	}
	if s.Yum.Delayed != 5 {
		t.Errorf("지연 %d대", s.Yum.Delayed)
	}
}

// 새 필드가 늘었어도 소켓(json) 왕복에서 출력이 그대로여야 한다.
func TestYumSummaryJSONRoundTrip(t *testing.T) {
	s := snapOf(yrec("a", "D", StatusRebooted, 90), yrebooting("b", "D", 90), yrec("c", "D", StatusAlive, 90))
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	var back Snapshot
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatal(err)
	}
	if InstallComment(back) != InstallComment(s) || DelayComment(back) != DelayComment(s) {
		t.Errorf("json 왕복 뒤 출력이 달라짐")
	}
}

// ---- 22단계: 화면 ----

// yumTuiSnap 은 tuiSnap(OS 40대)에 yum 서버 9대를 더한 표본이다.
// yum: 완료 3(yd), 접속가능 2(ya, 하나는 지연), 재부팅됨 2(yr: 커널 동일 지연 1, 재부팅 중 1), ping X 1, 확인전 1.
func yumTuiSnap() Snapshot {
	recs := append([]Record(nil), tuiSnap().Records...)
	since := func(min int) time.Time { return tNow.Add(-time.Duration(min) * time.Minute) }
	y := func(name, vendor string, st Status, min int, reason string) Record {
		r := Record{Name: name, Vendor: vendor, Status: st, Mode: ModeYum, Reason: reason}
		if st != StatusPingX && st != StatusNone {
			r.YumKernel, r.YumNow, r.YumSince = "4.18.0-513.el8", "4.18.0-513.el8", since(min)
		}
		return r
	}
	for i := 1; i <= 3; i++ {
		r := y(fmt.Sprintf("yd%03d", i), "D", StatusYumDone, 300, "커널 4.18.0-513.el8 → 4.18.0-553.el8")
		r.YumNow = "4.18.0-553.el8"
		r.RebootedAt = since(100 - i)
		recs = append(recs, r)
	}
	recs = append(recs,
		y("ya001", "D", StatusAlive, 90, "기준 커널 4.18.0-513.el8"),
		y("ya002", "sds", StatusAlive, 10, "기준 커널 4.18.0-513.el8"),
		y("yr001", "D", StatusRebooted, 120, "재부팅됨, 커널 동일 4.18.0-513.el8"),
		y("yr002", "sds", StatusRebooted, 30, "재부팅 중(ping 응답 없음)"),
		y("yx001", "D", StatusPingX, 0, "ping 응답 없음"),
		y("yn001", "D", StatusNone, 0, "ssh 확인 실패: 응답 없음"))
	return *buildSnapshot(defaultConfig(), recs, 3, true, tNow.Add(-95*time.Second), "", withYumNow(tNow))
}

func TestYumKeyPanel(t *testing.T) {
	snap := yumTuiSnap()
	v := newTestView(120, 30)
	if act := press(v, snap, "y"); act != actNone || v.Panel != panelYum || v.reportReq != 0 || v.Busy {
		t.Fatalf("y 열기: act=%v panel=%c req=%c busy=%v", act, v.Panel, v.reportReq, v.Busy)
	}
	got := render(*v, snap, 120, 30)
	for _, want := range []string{"[y] yum 커널", "hostname", "기준 커널", "yd001", "yum_update/접속가능", "지연 90분", "재부팅 중(ping 응답 없음)"} {
		if !strings.Contains(got, want) {
			t.Errorf("y 패널에 %q 없음\n%s", want, got)
		}
	}
	// 본문은 YumTable 과 같다(수집기·데몬 화면 모두 스냅샷만 쓴다)
	v.prepare(snap, 120)
	if body := v.panelBody(snap); body != YumTable(snap, tNow) {
		t.Errorf("패널 본문이 YumTable 과 다름")
	}
	if press(v, snap, "y"); v.Panel != 0 { // 같은 키로 닫기
		t.Fatal("y 로 닫기 실패")
	}
	press(v, snap, "y")
	if act := press(v, snap, "q"); act != actNone || v.Panel != 0 { // q 는 뒤로가기이고 화면을 나가지 않는다
		t.Fatalf("q 로 닫기: act=%v panel=%c", act, v.Panel)
	}
	if act := press(v, snap, "q"); act != actQuit {
		t.Fatal("기본 화면의 q 는 나가기")
	}
	// 다른 패널과 전환, 숫자 키·Esc 로도 닫힌다
	press(v, snap, "y", "c")
	if v.Panel != 'c' {
		t.Fatalf("y -> c 전환: %c", v.Panel)
	}
	press(v, snap, "y", "8")
	if v.Panel != 0 || v.Filter != StatusAlive {
		t.Fatalf("패널 중 필터: panel=%c filter=%v", v.Panel, v.Filter)
	}
	press(v, snap, "y", "\x1b")
	if v.Panel != 0 {
		t.Fatal("Esc 로 닫기 실패")
	}
	// 패널 본문 제공자(PanelText)가 있어도 y 는 YumTable 이다
	v2 := newTestView(120, 30)
	v2.PanelText = func(kind rune, s Snapshot) string { return "다른 본문" }
	press(v2, snap, "y")
	if got := render(*v2, snap, 120, 30); !strings.Contains(got, "yd001") || strings.Contains(got, "다른 본문") {
		t.Errorf("PanelText 가 y 를 가림\n%s", got)
	}
}

func TestYumPanelNoYumServers(t *testing.T) {
	snap := tuiSnap()
	v := newTestView(80, 24)
	press(v, snap, "y")
	if got := render(*v, snap, 80, 24); !strings.Contains(got, "yum 서버가 없습니다.") || !strings.Contains(got, "[y] yum 커널") {
		t.Errorf("yum 없음 안내\n%s", got)
	}
}

func TestYumFilters8And9(t *testing.T) {
	snap := yumTuiSnap()
	v := newTestView(80, 30)
	names := func() []string {
		v.prepare(snap, 80)
		var out []string
		for _, i := range v.rows {
			out = append(out, snap.Records[i].Name)
		}
		return out
	}
	press(v, snap, "8")
	if v.Filter != StatusAlive || strings.Join(names(), ",") != "ya001,ya002" {
		t.Fatalf("8 필터: %v %v", v.Filter, names())
	}
	if got := render(*v, snap, 80, 30); !strings.Contains(got, "필터: 접속가능") || !strings.Contains(got, "[8 접속가능 2]") {
		t.Errorf("8 필터 표시\n%s", got)
	}
	press(v, snap, "9")
	if v.Filter != StatusYumDone || strings.Join(names(), ",") != "yd001,yd002,yd003" {
		t.Fatalf("9 필터: %v %v", v.Filter, names())
	}
	if got := render(*v, snap, 80, 30); !strings.Contains(got, "필터: yum_update/접속가능") || !strings.Contains(got, "[9 yum_update/접속가능 3]") {
		t.Errorf("9 필터 표시\n%s", got)
	}
	// 6(재부팅됨)은 OS 서버와 yum 서버를 함께 보인다
	press(v, snap, "6")
	if got := strings.Join(names(), ","); got != "d001,d002,yr001,yr002" {
		t.Fatalf("6 필터: %s", got)
	}
	press(v, snap, "0")
	if v.Filter != 0 || len(names()) != len(snap.Records) {
		t.Fatalf("0 해제: %v %d", v.Filter, len(names()))
	}
	// 검색과 같이 쓴다
	press(v, snap, "9", "/", "yd002", "\r")
	if got := strings.Join(names(), ","); got != "yd002" {
		t.Fatalf("필터+검색: %s", got)
	}
}

func TestYumFooterAndHeader(t *testing.T) {
	osSnap, ySnap := tuiSnap(), yumTuiSnap()
	// OS 서버만이면 푸터·머리줄은 v0.3.0 그대로
	v := newTestView(120, 24)
	v.prepare(osSnap, 120)
	foot := strings.Join(v.footerLines(120), "\n")
	if !strings.Contains(foot, "1~7 필터") || strings.Contains(foot, "y yum") || strings.Contains(foot, "1~9") {
		t.Errorf("OS 전용 푸터: %q", foot)
	}
	if h := strings.Join(v.headerLines(osSnap, 120), "\n"); strings.Contains(h, "yum") {
		t.Errorf("OS 전용 머리줄: %q", h)
	}
	// yum 서버가 있으면 y 키와 1~9 를 안내한다
	v = newTestView(120, 24)
	v.prepare(ySnap, 120)
	foot = strings.Join(v.footerLines(120), "\n")
	if !strings.Contains(foot, "y yum") || !strings.Contains(foot, "1~9 필터") || !strings.Contains(foot, "q 종료") {
		t.Errorf("yum 푸터: %q", foot)
	}
	press(v, ySnap, "y")
	v.prepare(ySnap, 120)
	if foot = strings.Join(v.footerLines(120), "\n"); !strings.Contains(foot, "c r d l y 전환") || !strings.Contains(foot, "y 닫기") {
		t.Errorf("yum 패널 푸터: %q", foot)
	}
	// 백그라운드 화면에서도 q/Q 안내는 그대로 뒤에 붙는다
	dv := newDetachView()
	dv.prepare(ySnap, 120)
	if foot = strings.Join(dv.footerLines(120), "\n"); !strings.Contains(foot, "y yum") || !strings.Contains(foot, "q 나가기(수집 계속)") || !strings.Contains(foot, "Q 수집 종료") {
		t.Errorf("yum 백그라운드 푸터: %q", foot)
	}
	// 머리줄: 전체 수는 OS+yum, yum 대수를 밝히고 상태별 개수에 8·9 가 나온다
	h := strings.Join(v.headerLines(ySnap, 120), "\n")
	for _, want := range []string{"전체 49대 (yum 9대)", "6 재부팅됨 4", "8 접속가능 2", "9 yum_update/접속가능 3"} {
		if !strings.Contains(h, want) {
			t.Errorf("머리줄에 %q 없음\n%s", want, h)
		}
	}
	if ySnap.Done() != osSnap.Done() { // 설치완료는 OS 만(yum 재부팅됨 2대·yum 완료 3대는 세지 않음)
		t.Errorf("Done()=%d, OS 만이면 %d", ySnap.Done(), osSnap.Done())
	}
}

// 긴 라벨 yum_update/접속가능 이 폭 60/80/120 에서 잘리지 않고 열이 맞는다.
func TestYumStatusColumnFits(t *testing.T) {
	snap := yumTuiSnap()
	for _, w := range []int{60, 80, 120} {
		v := newTestView(w, 80)
		got := render(*v, snap, w, 80)
		var hdr, row string
		for _, l := range strings.Split(got, "\n") {
			if strings.HasPrefix(strings.TrimSpace(l), "호스트명") {
				hdr = l
			}
			if strings.Contains(l, "yd001") {
				row = l
			}
		}
		if row == "" || !strings.Contains(row, "yum_update/접속가능 ") { // 라벨 뒤는 칸 사이 공백(… 로 잘리지 않음; 비고는 잘릴 수 있다)
			t.Errorf("w=%d: 완료 행이 잘림: %q", w, row)
		}
		// 상태 칸 시작과 경과 칸 시작이 머리줄과 같은 열에 있다
		col := func(line, sub string) int { return strWidth(line[:strings.Index(line, sub)]) }
		if col(hdr, "상태") != col(row, "yum_update") {
			t.Errorf("w=%d: 상태 열이 어긋남\n%s\n%s", w, hdr, row)
		}
		for i, l := range strings.Split(got, "\n") {
			if sw := strWidth(l); sw > w {
				t.Errorf("w=%d %d번째 줄 폭 %d: %q", w, i+1, sw, l)
			}
		}
	}
}

// 새 골든: yum 서버가 섞인 화면(기존 OS 전용 골든은 건드리지 않는다).
func TestTUIGoldenYum(t *testing.T) {
	snap := yumTuiSnap()
	type tc struct {
		name string
		prep func(v *ViewState, snap Snapshot)
	}
	cases := []tc{
		{"default", func(v *ViewState, s Snapshot) {}},
		{"filter9", func(v *ViewState, s Snapshot) { press(v, s, "9") }},
		{"filter8", func(v *ViewState, s Snapshot) { press(v, s, "8") }},
		{"y_panel", func(v *ViewState, s Snapshot) { press(v, s, "y") }},
		{"c_panel", func(v *ViewState, s Snapshot) { press(v, s, "c"); v.checkDone(nil) }},
		{"r_panel", func(v *ViewState, s Snapshot) { press(v, s, "r") }},
	}
	for _, w := range []int{60, 80, 120} {
		for _, c := range cases {
			t.Run(fmt.Sprintf("%s_%d", c.name, w), func(t *testing.T) {
				v := newTestView(w, 24)
				c.prep(v, snap)
				got := render(*v, snap, w, 24)
				if n := strings.Count(got, "\n") + 1; n != 24 {
					t.Errorf("줄 수 %d, 24 여야 함", n)
				}
				for i, l := range strings.Split(got, "\n") {
					if sw := strWidth(l); sw > w {
						t.Errorf("%d번째 줄 폭 %d > %d: %q", i+1, sw, w, l)
					}
				}
				golden(t, fmt.Sprintf("tui_yum_%s_%d", c.name, w), got+"\n")
			})
		}
	}
}

// y 는 조회·ping 을 시작하지 않고(actNone) 루프에서도 수집기에 아무것도 요청하지 않는다.
func TestLoopYumPanelNoRemoteCalls(t *testing.T) {
	src := newFakeSrc()
	s := yumTuiSnap()
	src.snap = &s
	le := newLoopEnv()
	res := runLoopAsync(src, le)
	defer le.in.Close()
	waitFor(t, "첫 화면", func() bool { return strings.Contains(le.out.String(), "install_mn") })
	le.in.Write([]byte("y"))
	waitFor(t, "yum 표", func() bool {
		return strings.Contains(le.out.String(), "[y] yum 커널") && strings.Contains(le.out.String(), "yd001")
	})
	le.in.Write([]byte("qq"))
	if code := wait(t, res); code != 0 {
		t.Fatalf("종료 코드 %d", code)
	}
	if c := atomic.LoadInt32(&src.checks); c != 0 {
		t.Fatalf("y 가 ping 확인(CheckCompleted)을 %d 번 시작함", c)
	}
}

// 5천 대 중 3천 대가 yum 서버여도 y 패널·8/9 필터·긴 상태 열의 키 반응이 50ms 안이다.
func TestRenderYumUnder50ms(t *testing.T) {
	recs := make([]Record, 5000)
	for i := range recs {
		recs[i] = Record{Name: fmt.Sprintf("node%05d", i), Vendor: "D", Status: Status(1 + i%7), Reason: "OS 설치중"}
		if i%5 >= 2 { // 60%
			recs[i].Mode = ModeYum
			recs[i].Status = Status(6 + i%4) // 6 재부팅됨, 7 완료후꺼짐, 8 접속가능, 9 완료
			recs[i].YumKernel, recs[i].YumNow, recs[i].YumSince = "k1", "k2", tNow.Add(-time.Duration(i)*time.Minute)
		}
	}
	snap := *buildSnapshot(defaultConfig(), recs, 1, false, tNow, "", withYumNow(tNow))
	if snap.Yum.Total != 3000 {
		t.Fatalf("yum %d대", snap.Yum.Total)
	}
	v := newTestView(80, 40)
	v.prepare(snap, 80)
	steps := []struct {
		name string
		fn   func()
	}{
		{"y 패널+렌더", func() { press(v, snap, "y"); v.prepare(snap, 80); render(*v, snap, 80, 40) }},
		{"y 스크롤+렌더", func() { press(v, snap, "\x1b[6~"); render(*v, snap, 80, 40) }},
		{"y 닫기+9 필터+렌더", func() { press(v, snap, "y", "9"); v.prepare(snap, 80); render(*v, snap, 80, 40) }},
		{"8 필터+렌더", func() { press(v, snap, "8"); v.prepare(snap, 80); render(*v, snap, 80, 40) }},
		{"6 필터+스크롤+렌더", func() { press(v, snap, "6", "\x1b[F"); v.prepare(snap, 80); render(*v, snap, 80, 40) }},
	}
	for _, st := range steps {
		start := time.Now()
		st.fn()
		if d := time.Since(start); d > 50*time.Millisecond {
			t.Errorf("%s: %v (50ms 초과)", st.name, d)
		}
	}
}

// ---- 22단계: 데몬 ----

// 프로그램 버전이 지문에 들어가 바이너리를 바꾸면 옛 데몬이 새 데몬으로 교체된다.
func TestFingerprintIncludesProgramVersion(t *testing.T) {
	dir := t.TempDir()
	h, c := filepath.Join(dir, "hosts"), filepath.Join(dir, "conf")
	os.WriteFile(h, []byte("a\nb yum\n"), 0o644)
	os.WriteFile(c, []byte("interval_sec=30\n"), 0o644)
	old := programVersion
	defer func() { programVersion = old }()
	if programVersion != "0.4.0" {
		t.Errorf("programVersion=%q", programVersion)
	}
	programVersion = "0.4.0"
	base := fingerprint(h, c)
	programVersion = "0.4.1"
	if fingerprint(h, c) == base {
		t.Fatal("버전이 달라졌는데 지문이 같음")
	}
	programVersion = "0.4.0"
	if fingerprint(h, c) != base {
		t.Fatal("같은 버전·내용인데 지문이 다름")
	}
	// 버전 문자열과 호스트파일 내용이 경계에서 섞이지 않는다
	programVersion = "0.4.0"
	os.WriteFile(h, []byte("0\na\nb yum\n"), 0o644)
	if fingerprint(h, c) == base {
		t.Fatal("내용이 달라졌는데 지문이 같음")
	}
}

// --status 줄: yum 서버가 없으면 v0.3.0 과 글자까지 같고, 있으면 OS 수와 yum 수를 따로 보인다.
func TestDescribeDaemonYum(t *testing.T) {
	started := time.Date(2026, 11, 17, 10, 0, 0, 0, time.Local)
	in := daemonInfo{PID: 4242, Hostfile: "hosts.txt", Started: started, Expires: started.Add(12 * time.Hour), Total: 300, Done: 30, Round: 7}
	now := started.Add(time.Hour)
	want := "hosts.txt\n  pid 4242 · 서버 300대 중 설치완료 30 · 회차 7\n  시작 11-17 10:00 · 자동 종료 11-17 22:00 (남은 11h00m)"
	if got := describeDaemon(in, now); got != want {
		t.Errorf("OS 전용 --status\n--- want\n%s\n--- got\n%s", want, got)
	}
	in.YumTotal, in.YumDone = 12, 8
	want = "hosts.txt\n  pid 4242 · 서버 300대 중 설치완료 30 · yum 8/12 · 회차 7\n  시작 11-17 10:00 · 자동 종료 11-17 22:00 (남은 11h00m)"
	if got := describeDaemon(in, now); got != want {
		t.Errorf("yum --status\n--- want\n%s\n--- got\n%s", want, got)
	}
}

// liveInfo: Total·Done 은 OS 서버만, yum 서버는 YumTotal·YumDone 에 따로 담긴다.
func TestLiveInfoSeparatesYum(t *testing.T) {
	osSnap := tuiSnap()
	osDone := osSnap.Done()
	snap := yumTuiSnap()
	col := &Collector{}
	col.snap.Store(&snap)
	s := &daemonServer{col: col}
	in := s.liveInfo()
	if in.Total != 40 || in.Done != osDone || in.YumTotal != 9 || in.YumDone != 3 || in.Round != 3 {
		t.Errorf("liveInfo=%+v", in)
	}
}
