package main

import (
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// 공용 자료
// ---------------------------------------------------------------------------

var repToday = time.Date(2026, 11, 12, 9, 30, 0, 0, time.Local)

// 계획서 3.5 의 출력 예(글자 하나까지 같아야 한다).
const sampleReport = `HPE:
    OS설치 558대 중 554대 완료, 1대 OS 설치 중 (전일대비 +5대)
    부하테스트 554대 중 500대 완료, 전일대비 +20대, 11/14 완료예정
    접속불가서버 3대 (전일대비 -2대)
        - hpe-0101 조치완료
        - hpe-0230 조치완료
        접속불가
          - hpe-0007
          - hpe-0312
          - hpe-0440

DELL:
    OS설치 120대 중 120대 완료, 0대 OS 설치 중 (전일대비 +0대)
    부하테스트 120대 중 120대 완료, 전일대비 +3대, 완료
`

func sampleCur() []repHost {
	var cur []repHost
	done := 0
	for i := 1; i <= 558; i++ {
		h := repHost{Host: fmt.Sprintf("hpe-%04d", i), Vendor: "HPE", Status: stDone}
		switch i {
		case 7, 312, 440:
			h.Status = stUnreach
		case 558:
			h.Status = stInstall
		default:
			if done < 500 {
				h.StressOK = true
			}
			done++
		}
		cur = append(cur, h)
	}
	for i := 1; i <= 120; i++ {
		cur = append(cur, repHost{Host: fmt.Sprintf("dell-%04d", i), Vendor: "DELL", Status: stDone, StressOK: true})
	}
	return cur
}

func sampleBase() *repBase {
	return &repBase{Label: "t", Vendors: map[string]*repBaseVendor{
		"hpe":  {OSDone: intp(549), LoadDone: intp(480), Unreach: intp(5), Names: []string{"hpe-0007", "hpe-0101", "hpe-0230", "hpe-0312", "hpe-0440"}, HasNames: true},
		"dell": {OSDone: intp(120), LoadDone: intp(117), Unreach: intp(0), HasNames: true},
	}}
}

func hh(v, host, status string, ok bool) repHost {
	return repHost{Host: host, Vendor: v, Status: status, StressOK: ok}
}

// ---------------------------------------------------------------------------
// buildReport
// ---------------------------------------------------------------------------

func TestBuildReportSample(t *testing.T) {
	a, b := buildReport(sampleCur(), []string{"HPE", "DELL"}, sampleBase(), repToday, 2)
	if a != sampleReport {
		t.Errorf("3.5 출력 예와 다릅니다.\n--- got ---\n%s--- want ---\n%s", a, sampleReport)
	}
	if b != "" {
		t.Errorf("본이 하나여야 함: b=%q", b)
	}
	// 같은 입력이면 같은 글
	a2, _ := buildReport(sampleCur(), []string{"HPE", "DELL"}, sampleBase(), repToday, 2)
	if a2 != a {
		t.Error("같은 입력인데 글이 다름")
	}
}

func TestBuildReportLines(t *testing.T) {
	order := []string{"A"}
	// 상태 5종과 stress 열을 섞은 호스트 표(N D I B U, 설치정지, 디스크미인식은 U 에 들어간다)
	mixed := []repHost{
		hh("A", "a1", stDone, true), hh("A", "a2", stDone, false), hh("A", "a3", stInstall, false),
		hh("A", "a4", stUnreach, false), hh("A", "a5", stNoDisk, false), hh("A", "a6", stStopped, false),
		hh("A", "a7", stStopped, true), // 완료가 아니면 StressOK 는 세지 않는다
	}
	cases := []struct {
		name string
		cur  []repHost
		base *repBase
		eta  int
		want string
	}{
		{"기준 없음(nil)·상태 5종", mixed, nil, 2, `A:
    OS설치 7대 중 2대 완료, 1대 OS 설치 중, 2대 설치정지 (전일 정보 없음)
    부하테스트 2대 중 1대 완료, 전일 정보 없음, 11/14 완료예정
    접속불가서버 2대 (전일 정보 없음)
        접속불가
          - a4
          - a5 디스크 미인식
`},
		{"기준에 벤더 없음도 같다", mixed, &repBase{Vendors: map[string]*repBaseVendor{"zzz": {}}}, 2, `A:
    OS설치 7대 중 2대 완료, 1대 OS 설치 중, 2대 설치정지 (전일 정보 없음)
    부하테스트 2대 중 1대 완료, 전일 정보 없음, 11/14 완료예정
    접속불가서버 2대 (전일 정보 없음)
        접속불가
          - a4
          - a5 디스크 미인식
`},
		{"전일대비 +0·음수·양수와 값마다 nil", []repHost{hh("A", "a1", stDone, true), hh("A", "a2", stDone, true), hh("A", "a3", stUnreach, false)},
			&repBase{Vendors: map[string]*repBaseVendor{"a": {OSDone: intp(5), LoadDone: intp(2), Unreach: nil}}}, 2, `A:
    OS설치 3대 중 2대 완료, 0대 OS 설치 중 (전일대비 -3대)
    부하테스트 2대 중 2대 완료, 전일대비 +0대, 11/14 완료예정
    접속불가서버 1대 (전일 정보 없음)
        접속불가
          - a3
`},
		{"OS·부하만 nil", []repHost{hh("A", "a1", stDone, true)},
			&repBase{Vendors: map[string]*repBaseVendor{"a": {Unreach: intp(0), HasNames: true}}}, 2, `A:
    OS설치 1대 중 1대 완료, 0대 OS 설치 중 (전일 정보 없음)
    부하테스트 1대 중 1대 완료, 전일 정보 없음, 완료
`},
		{"설치정지만", []repHost{hh("A", "a1", stDone, true), hh("A", "a2", stStopped, false)}, nil, 0, `A:
    OS설치 2대 중 1대 완료, 0대 OS 설치 중, 1대 설치정지 (전일 정보 없음)
    부하테스트 1대 중 1대 완료, 전일 정보 없음, 11/12 완료예정
`},
		{"디스크미인식만", []repHost{hh("A", "a1", stDone, true), hh("A", "a2", stNoDisk, false)}, nil, 0, `A:
    OS설치 2대 중 1대 완료, 0대 OS 설치 중 (전일 정보 없음)
    부하테스트 1대 중 1대 완료, 전일 정보 없음, 11/12 완료예정
    접속불가서버 1대 (전일 정보 없음)
        접속불가
          - a2 디스크 미인식
`},
	}
	for _, tc := range cases {
		a, b := buildReport(tc.cur, order, tc.base, repToday, tc.eta)
		if a != tc.want || b != "" {
			t.Errorf("%s:\n--- got ---\n%s--- want ---\n%s(b=%q)", tc.name, a, tc.want, b)
		}
	}
}

func TestBuildReportETA(t *testing.T) {
	cases := []struct {
		name  string
		today time.Time
		eta   int
		want  string
	}{
		{"기본 +2", time.Date(2026, 11, 12, 23, 59, 0, 0, time.Local), 2, "11/14 완료예정"},
		{"0 일이면 오늘", repToday, 0, "11/12 완료예정"},
		{"월 넘김", time.Date(2026, 11, 30, 1, 0, 0, 0, time.Local), 2, "12/2 완료예정"},
		{"해 넘김·앞에 0 을 붙이지 않음", time.Date(2026, 12, 31, 1, 0, 0, 0, time.Local), 5, "1/5 완료예정"},
		{"한 자리 월·일", time.Date(2026, 1, 3, 1, 0, 0, 0, time.Local), 3, "1/6 완료예정"},
	}
	for _, tc := range cases {
		a, _ := buildReport([]repHost{hh("A", "a1", stDone, false)}, []string{"A"}, nil, tc.today, tc.eta)
		if !strings.Contains(a, ", "+tc.want+"\n") {
			t.Errorf("%s: %q", tc.name, a)
		}
	}
}

// 완료 는 A = N 이고 B = A 일 때만(계획서 3.5 규칙·경계 4.19)
func TestBuildReportDoneWord(t *testing.T) {
	cases := []struct {
		name  string
		hosts []repHost
		want  string
	}{
		{"A=N, B=A", []repHost{hh("A", "1", stDone, true), hh("A", "2", stDone, true)}, "부하테스트 2대 중 2대 완료, 전일 정보 없음, 완료"},
		{"A=N, B<A", []repHost{hh("A", "1", stDone, true), hh("A", "2", stDone, false)}, "부하테스트 2대 중 1대 완료, 전일 정보 없음, 11/14 완료예정"},
		{"A<N, B=A 면 날짜", []repHost{hh("A", "1", stDone, true), hh("A", "2", stInstall, false)}, "부하테스트 1대 중 1대 완료, 전일 정보 없음, 11/14 완료예정"},
		{"A<N(접속불가), B=A 면 날짜", []repHost{hh("A", "1", stDone, true), hh("A", "2", stUnreach, false)}, "부하테스트 1대 중 1대 완료, 전일 정보 없음, 11/14 완료예정"},
		{"A=0, N>0 이면 날짜", []repHost{hh("A", "1", stInstall, false), hh("A", "2", stInstall, false)}, "부하테스트 0대 중 0대 완료, 전일 정보 없음, 11/14 완료예정"},
		{"A=0, B=0, 호스트 1대", []repHost{hh("A", "1", stUnreach, false)}, "부하테스트 0대 중 0대 완료, 전일 정보 없음, 11/14 완료예정"},
	}
	for _, tc := range cases {
		a, _ := buildReport(tc.hosts, []string{"A"}, nil, repToday, 2)
		if !strings.Contains(a, "    "+tc.want+"\n") {
			t.Errorf("%s: %q", tc.name, a)
		}
	}
}

func TestBuildReportVendorOrder(t *testing.T) {
	cur := []repHost{
		hh("기타", "z1", stDone, true),
		hh("B", "b1", stDone, true),
		hh("A", "a1", stDone, true),
		hh("Nope", "n1", stInstall, false), // order 에도 기타 에도 없는 벤더 이름은 기타 로 센다
	}
	a, _ := buildReport(cur, []string{"A", "B", "C"}, nil, repToday, 2)
	var heads []string
	for _, l := range strings.Split(a, "\n") {
		if strings.HasSuffix(l, ":") {
			heads = append(heads, l)
		}
	}
	// C 는 0대라 적지 않는다. 기타 는 마지막.
	if !reflect.DeepEqual(heads, []string{"A:", "B:", "기타:"}) {
		t.Errorf("벤더 순서: %v\n%s", heads, a)
	}
	if !strings.Contains(a, "기타:\n    OS설치 2대 중 1대 완료, 1대 OS 설치 중") {
		t.Errorf("기타 집계: %s", a)
	}
	// 벤더 사이 빈 줄 하나·끝 개행 하나
	if strings.Count(a, "\n\n") != 2 || strings.Contains(a, "\n\n\n") || !strings.HasSuffix(a, "\n") || strings.HasSuffix(a, "\n\n") {
		t.Errorf("빈 줄·끝 개행: %q", a)
	}
	// 기타 가 0대이면 적지 않는다
	a, _ = buildReport([]repHost{hh("A", "a1", stDone, true)}, []string{"A"}, nil, repToday, 2)
	if strings.Contains(a, "기타") {
		t.Errorf("0대 기타 가 적혔다: %s", a)
	}
	// 호스트가 없으면 빈 글
	if a, b := buildReport(nil, []string{"A"}, nil, repToday, 2); a != "" || b != "" {
		t.Errorf("빈 목록: %q %q", a, b)
	}
	// 벤더 이름 대소문자는 무시하고 묶되 글에는 order 의 이름을 적는다
	a, _ = buildReport([]repHost{hh("hpe", "h1", stDone, true)}, []string{"HPE"}, &repBase{Vendors: map[string]*repBaseVendor{"hpe": {OSDone: intp(0)}}}, repToday, 2)
	if !strings.HasPrefix(a, "HPE:\n    OS설치 1대 중 1대 완료, 0대 OS 설치 중 (전일대비 +1대)") {
		t.Errorf("대소문자: %s", a)
	}
}

func TestBuildReportUnreachBlock(t *testing.T) {
	order := []string{"A"}
	cur := []repHost{hh("A", "a1", stDone, true), hh("A", "a2", stUnreach, false), hh("A", "a3", stDone, false)}
	// 이름 명단을 모름(HasNames 거짓): 대수 전일대비는 적고 조치완료 줄은 적지 않는다
	a, b := buildReport(cur, order, &repBase{Vendors: map[string]*repBaseVendor{"a": {OSDone: intp(2), LoadDone: intp(1), Unreach: intp(3), Names: []string{"a1"}}}}, repToday, 2)
	if !strings.Contains(a, "    접속불가서버 1대 (전일대비 -2대)\n        접속불가\n          - a2\n") || strings.Contains(a, "조치완료") || b != "" {
		t.Errorf("명단 모름: %q b=%q", a, b)
	}
	// 현재 0대, 조치완료 없음, 기준에는 있음(이름 모름): 줄 3 이하를 적지 않고 글은 하나
	a, b = buildReport([]repHost{hh("A", "a1", stDone, true)}, order, &repBase{Vendors: map[string]*repBaseVendor{"a": {OSDone: intp(1), LoadDone: intp(1), Unreach: intp(2)}}}, repToday, 2)
	if strings.Contains(a, "접속불가") || b != "" {
		t.Errorf("0대·조치 없음: %q b=%q", a, b)
	}
	// 둘 다 없음
	a, b = buildReport([]repHost{hh("A", "a1", stDone, true)}, order, nil, repToday, 2)
	if strings.Contains(a, "접속불가") || b != "" {
		t.Errorf("둘 다 없음: %q", a)
	}
	// 조치완료: 현재 목록에 있고 접속불가가 아닌 것만, 기준의 이름 순서, 대소문자 무시, 중복 한 번
	cur = []repHost{hh("A", "a1", stDone, true), hh("A", "a2", stUnreach, false), hh("A", "A3", stInstall, false)}
	base := &repBase{Vendors: map[string]*repBaseVendor{"a": {Unreach: intp(5), HasNames: true, Names: []string{"a3", "gone", "a2", "a1", "A1"}}}}
	a, b = buildReport(cur, order, base, repToday, 2)
	want := "    접속불가서버 1대 (전일대비 -4대)\n        - a3 조치완료\n        - a1 조치완료\n        접속불가\n          - a2\n"
	if !strings.Contains(a, want) || b != "" {
		t.Errorf("조치완료 목록: %q", a)
	}
	// 접속불가 200대: 한 줄에 하나
	var big []repHost
	for i := 0; i < 200; i++ {
		big = append(big, hh("A", fmt.Sprintf("n%03d", i), stUnreach, false))
	}
	a, _ = buildReport(big, order, nil, repToday, 2)
	if strings.Count(a, "\n          - n") != 200 || !strings.Contains(a, "          - n000\n          - n001\n") || !strings.Contains(a, "접속불가서버 200대") {
		t.Errorf("200대: 줄 수 %d", strings.Count(a, "\n"))
	}
}

// 디스크미인식은 완료가 아니고 접속불가서버 N대에 들어간다. 명단에는 호스트파일 순서로 섞여 `- <이름> 디스크 미인식` 으로 나온다.
func TestBuildReportNoDiskInUnreach(t *testing.T) {
	order := []string{"A"}
	cur := []repHost{hh("A", "a1", stDone, true), hh("A", "a2", stUnreach, false), hh("A", "a3", stNoDisk, false),
		hh("A", "a4", stNoDisk, false), hh("A", "a5", stUnreach, false), hh("A", "a6", stDone, false)}
	// 기준의 접속불가 이름: a3(지금은 디스크미인식이라 조치완료 아님), a2(그대로), a6(지금 완료라 조치완료), a9(현재 목록에 없음)
	base := &repBase{Vendors: map[string]*repBaseVendor{"a": {OSDone: intp(2), LoadDone: intp(1), Unreach: intp(4), Names: []string{"a3", "a2", "a6", "a9"}, HasNames: true}}}
	a, b := buildReport(cur, order, base, repToday, 2)
	want := `A:
    OS설치 6대 중 2대 완료, 0대 OS 설치 중 (전일대비 +0대)
    부하테스트 2대 중 1대 완료, 전일대비 +0대, 11/14 완료예정
    접속불가서버 4대 (전일대비 +0대)
        - a6 조치완료
        접속불가
          - a2
          - a3 디스크 미인식
          - a4 디스크 미인식
          - a5
`
	if a != want || b != "" {
		t.Errorf("섞인 명단:\n--- got ---\n%s--- want ---\n%s(b=%q)", a, want, b)
	}
	// 설치정지는 그대로 줄 1 에 덧붙는다. 디스크미인식은 줄 1 에 없다
	a, _ = buildReport([]repHost{hh("A", "a1", stStopped, false), hh("A", "a2", stNoDisk, false)}, order, nil, repToday, 2)
	if !strings.Contains(a, "    OS설치 2대 중 0대 완료, 0대 OS 설치 중, 1대 설치정지 (전일 정보 없음)\n") || strings.Contains(a, "디스크미인식") ||
		!strings.Contains(a, "    접속불가서버 1대 (전일 정보 없음)\n        접속불가\n          - a2 디스크 미인식\n") {
		t.Errorf("설치정지+디스크미인식: %q", a)
	}
	// 접속불가 상태는 0대이고 디스크미인식만 있어도 U >= 1 이라 한 본이다(두 본 아님). 조치완료도 함께 나온다
	a, b = buildReport([]repHost{hh("A", "a1", stDone, true), hh("A", "a2", stNoDisk, false)}, order,
		&repBase{Vendors: map[string]*repBaseVendor{"a": {Unreach: intp(1), Names: []string{"a1"}, HasNames: true}}}, repToday, 2)
	if b != "" || !strings.Contains(a, "    접속불가서버 1대 (전일대비 +0대)\n        - a1 조치완료\n        접속불가\n          - a2 디스크 미인식\n") {
		t.Errorf("U>=1: %q b=%q", a, b)
	}
	// 접속불가였던 호스트가 디스크미인식이 되어도 조치완료가 아니다
	a, b = buildReport([]repHost{hh("A", "a1", stNoDisk, false)}, order,
		&repBase{Vendors: map[string]*repBaseVendor{"a": {Unreach: intp(1), Names: []string{"a1"}, HasNames: true}}}, repToday, 2)
	if b != "" || strings.Contains(a, "조치완료") || !strings.Contains(a, "접속불가서버 1대 (전일대비 +0대)") {
		t.Errorf("접속불가→디스크미인식: %q b=%q", a, b)
	}
}

func TestBuildReportTwoVersions(t *testing.T) {
	order := []string{"A", "B"}
	cur := []repHost{hh("A", "a1", stDone, true), hh("B", "b1", stDone, true), hh("B", "b2", stUnreach, false)}
	base := &repBase{Vendors: map[string]*repBaseVendor{
		"a": {OSDone: intp(1), LoadDone: intp(1), Unreach: intp(2), Names: []string{"a9", "a1"}, HasNames: true},
		"b": {OSDone: intp(1), LoadDone: intp(1), Unreach: intp(0), HasNames: true},
	}}
	a, b := buildReport(cur, order, base, repToday, 2)
	wantA := `A:
    OS설치 1대 중 1대 완료, 0대 OS 설치 중 (전일대비 +0대)
    부하테스트 1대 중 1대 완료, 전일대비 +0대, 완료
    접속불가서버 0대 (전일대비 -2대)
        - a1 조치완료

B:
    OS설치 2대 중 1대 완료, 0대 OS 설치 중 (전일대비 +0대)
    부하테스트 1대 중 1대 완료, 전일대비 +0대, 11/14 완료예정
    접속불가서버 1대 (전일대비 +1대)
        접속불가
          - b2
`
	wantB := `A:
    OS설치 1대 중 1대 완료, 0대 OS 설치 중 (전일대비 +0대)
    부하테스트 1대 중 1대 완료, 전일대비 +0대, 완료

B:
    OS설치 2대 중 1대 완료, 0대 OS 설치 중 (전일대비 +0대)
    부하테스트 1대 중 1대 완료, 전일대비 +0대, 11/14 완료예정
    접속불가서버 1대 (전일대비 +1대)
        접속불가
          - b2
`
	if a != wantA {
		t.Errorf("본 A:\n%s--- want ---\n%s", a, wantA)
	}
	if b != wantB {
		t.Errorf("본 B:\n%s--- want ---\n%s", b, wantB)
	}
	// 조치완료가 있어도 현재 접속불가가 1대 이상이면 두 본이 아니다
	cur2 := []repHost{hh("A", "a1", stDone, true), hh("A", "a2", stUnreach, false)}
	a, b = buildReport(cur2, []string{"A"}, base, repToday, 2)
	if b != "" || !strings.Contains(a, "        - a1 조치완료\n        접속불가\n          - a2\n") {
		t.Errorf("U>0 이면 한 본: %q %q", a, b)
	}
}

// ---------------------------------------------------------------------------
// 스냅샷
// ---------------------------------------------------------------------------

func TestSnapshotRoundTripAndNames(t *testing.T) {
	dir := t.TempDir()
	cur := sampleCur()
	snap := makeSnapshot(cur, repToday)
	if snap.Version != 1 || snap.Date != "2026-11-12" {
		t.Fatalf("snap=%+v", snap)
	}
	p, err := writeSnapshot(dir, snap, repToday)
	if err != nil || filepath.Base(p) != "report_20261112.json" {
		t.Fatalf("p=%s err=%v", p, err)
	}
	got, err := readSnapshot(p)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Hosts, cur) || got.Date != snap.Date || got.Version != 1 || !got.Made.Equal(snap.Made) {
		t.Errorf("왕복 불일치")
	}
	// 같은 날 파일은 덮어쓴다
	snap.Hosts = snap.Hosts[:3]
	if _, err := writeSnapshot(dir, snap, repToday); err != nil {
		t.Fatal(err)
	}
	if got, _ := readSnapshot(p); len(got.Hosts) != 3 {
		t.Errorf("덮어쓰기 실패: %d", len(got.Hosts))
	}
	if reportTextFileName(repToday, false) != "report_20261112.txt" || reportTextFileName(repToday, true) != "report_20261112_B.txt" {
		t.Error("txt 파일 이름")
	}
}

func TestPickSnapshot(t *testing.T) {
	today := repToday
	touch := func(dir string, names ...string) {
		for _, n := range names {
			if err := os.WriteFile(filepath.Join(dir, n), []byte("{}"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	// 파일 없음·폴더 없음(옛 상태 폴더)
	if _, _, err := pickSnapshot(t.TempDir(), today); err != errNoSnapshot {
		t.Errorf("빈 폴더: %v", err)
	}
	if _, _, err := pickSnapshot(filepath.Join(t.TempDir(), "none"), today); err != errNoSnapshot {
		t.Errorf("폴더 없음: %v", err)
	}
	// 오늘 것만 있음(경계 8): 기준으로 쓰지 않는다. 미래 날짜도 쓰지 않는다.
	d := t.TempDir()
	touch(d, "report_20261112.json", "report_20261113.json")
	if _, _, err := pickSnapshot(d, today); err != errNoSnapshot {
		t.Errorf("오늘·미래만: %v", err)
	}
	// 가장 최근 파일(어제가 아니어도). 이름 규칙에 안 맞거나 날짜가 틀린 것·txt·폴더는 건너뛴다.
	touch(d, "report_20261110.json", "report_20261105.json", "report_20261111.txt", "report_2026111.json",
		"report_20261301.json", "report_20261111.json.bak", "xreport_20261111.json", "report_20261111_B.json", "state.json")
	if err := os.Mkdir(filepath.Join(d, "report_20261111.json"), 0o755); err != nil {
		t.Fatal(err)
	}
	p, date, err := pickSnapshot(d, today)
	if err != nil || filepath.Base(p) != "report_20261110.json" || date.Format("2006-01-02") != "2026-11-10" {
		t.Errorf("최근 파일: %s %v %v", p, date, err)
	}
	// 자정을 넘기면 어제였던 오늘 파일이 어제가 된다
	p, _, err = pickSnapshot(d, today.AddDate(0, 0, 1))
	if err != nil || filepath.Base(p) != "report_20261112.json" {
		t.Errorf("다음 날: %s %v", p, err)
	}
}

func TestReadSnapshotErrors(t *testing.T) {
	d := t.TempDir()
	w := func(name, body string) string {
		p := filepath.Join(d, name)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	if _, err := readSnapshot(w("a.json", "{깨짐")); err == nil {
		t.Error("깨진 JSON 은 오류여야 함")
	}
	if _, err := readSnapshot(filepath.Join(d, "none.json")); err == nil {
		t.Error("없는 파일은 오류여야 함")
	}
	if _, err := readSnapshot(w("v2.json", `{"version":2,"hosts":[]}`)); err != errSnapshotNewer {
		t.Errorf("version 2: %v", err)
	}
	if _, err := readSnapshot(w("v0.json", `{"hosts":[]}`)); err == nil {
		t.Error("version 없음은 오류여야 함")
	}
	if s, err := readSnapshot(w("v1.json", `{"version":1,"date":"2026-11-11","hosts":[]}`)); err != nil || s.Date != "2026-11-11" {
		t.Errorf("v1: %v", err)
	}
}

func TestBaseFromSnapshotAndLoadBaseline(t *testing.T) {
	rules := parseConf("VENDOR_HPE=hpe*\nVENDOR_DELL=dell*\n").Vendors
	yesterday := []repHost{
		// 저장된 vendor 는 옛 규칙(참고용)이라 지금 규칙으로 다시 나눈다
		{Host: "hpe-1", Vendor: "OLD", Status: stDone, StressOK: true},
		{Host: "hpe-2", Vendor: "OLD", Status: stUnreach},
		{Host: "HPE-3", Vendor: "OLD", Status: stUnreach},
		{Host: "dell-1", Vendor: "OLD", Status: stDone, StressOK: false},
		{Host: "other-1", Vendor: "OLD", Status: stInstall},
	}
	cur := []repHost{ // 같은 호스트(순서·대소문자만 다름)
		{Host: "dell-1", Status: stDone}, {Host: "hpe-3", Status: stDone}, {Host: "HPE-1", Status: stDone},
		{Host: "other-1", Status: stDone}, {Host: "hpe-2", Status: stUnreach},
	}
	d := t.TempDir()
	snap := makeSnapshot(yesterday, repToday.AddDate(0, 0, -3))
	if _, err := writeSnapshot(d, snap, repToday.AddDate(0, 0, -3)); err != nil {
		t.Fatal(err)
	}
	rb, ago, err := loadBaseline(d, cur, rules, repToday)
	if err != nil || ago != 3 || rb.Label != "저장본 2026-11-09" {
		t.Fatalf("rb=%+v ago=%d err=%v", rb, ago, err)
	}
	hpe, dell, etc := rb.Vendors["hpe"], rb.Vendors["dell"], rb.Vendors["기타"]
	if hpe == nil || dell == nil || etc == nil || len(rb.Vendors) != 3 {
		t.Fatalf("벤더: %+v", rb.Vendors)
	}
	if *hpe.OSDone != 1 || *hpe.LoadDone != 1 || *hpe.Unreach != 2 || !reflect.DeepEqual(hpe.Names, []string{"hpe-2", "HPE-3"}) || !hpe.HasNames {
		t.Errorf("hpe: %+v", hpe)
	}
	if *dell.OSDone != 1 || *dell.LoadDone != 0 || *dell.Unreach != 0 || len(dell.Names) != 0 || !dell.HasNames {
		t.Errorf("dell: %+v", dell)
	}
	if *etc.OSDone != 0 || *etc.Unreach != 0 {
		t.Errorf("기타: %+v", etc)
	}

	// 어제 저장본
	d2 := t.TempDir()
	if _, err := writeSnapshot(d2, makeSnapshot(yesterday, repToday.AddDate(0, 0, -1)), repToday.AddDate(0, 0, -1)); err != nil {
		t.Fatal(err)
	}
	if _, ago, err := loadBaseline(d2, cur, rules, repToday); err != nil || ago != 1 {
		t.Errorf("어제: ago=%d err=%v", ago, err)
	}

	// 호스트 목록이 다름: 대수 문구
	_, _, err = loadBaseline(d, append(cur[:4:4], repHost{Host: "new-1"}, repHost{Host: "new-2"}), rules, repToday)
	want := "호스트 목록이 다릅니다. 저장본 2026-11-09 5대, 현재 6대. 저장본에만 있는 호스트 1대, 현재에만 있는 호스트 2대. 2(코멘트 입력)를 쓰십시오."
	if err == nil || err.Error() != want {
		t.Errorf("목록 차이: %v", err)
	}
	if a, b := diffHostLists(yesterday, cur); a != 0 || b != 0 {
		t.Errorf("diffHostLists=%d,%d", a, b)
	}

	// 저장본 없음
	if _, _, err := loadBaseline(t.TempDir(), cur, rules, repToday); err == nil || err.Error() != "전일 저장본이 없습니다. 2(코멘트 입력)를 쓰십시오." {
		t.Errorf("없음: %v", err)
	}
	// 깨진 파일: 파일 이름과 사유, 더 옛 파일로 넘어가지 않는다
	d3 := t.TempDir()
	if _, err := writeSnapshot(d3, makeSnapshot(yesterday, repToday.AddDate(0, 0, -3)), repToday.AddDate(0, 0, -3)); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(d3, "report_20261111.json"), []byte("{깨짐"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, _, err = loadBaseline(d3, cur, rules, repToday)
	if err == nil || !strings.HasPrefix(err.Error(), "저장본 report_20261111.json 을(를) 읽을 수 없습니다: ") || !strings.HasSuffix(err.Error(), ". 2(코멘트 입력)를 쓰십시오.") {
		t.Errorf("깨짐: %v", err)
	}
	// version 이 더 높음
	d4 := t.TempDir()
	if err := os.WriteFile(filepath.Join(d4, "report_20261111.json"), []byte(`{"version":9}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := loadBaseline(d4, cur, rules, repToday); err == nil || err.Error() != "이 바이너리보다 새 형식의 저장본입니다" {
		t.Errorf("새 형식: %v", err)
	}
}

// 저장본의 디스크미인식 호스트는 기준의 접속불가 대수·이름에 들어간다(오늘의 규칙과 같다).
func TestBaseFromSnapshotNoDisk(t *testing.T) {
	rules := parseConf("VENDOR_HPE=hpe*\n").Vendors
	snapHosts := []repHost{
		{Host: "hpe-1", Status: stDone, StressOK: true},
		{Host: "hpe-2", Status: stUnreach},
		{Host: "hpe-3", Status: stNoDisk},
		{Host: "hpe-4", Status: stStopped},
	}
	rb, err := baseFromSnapshot(&repSnapshot{Version: 1, Date: "2026-11-11", Hosts: snapHosts}, snapHosts, rules)
	if err != nil {
		t.Fatal(err)
	}
	v := rb.Vendors["hpe"]
	if v == nil || *v.OSDone != 1 || *v.LoadDone != 1 || *v.Unreach != 2 || !reflect.DeepEqual(v.Names, []string{"hpe-2", "hpe-3"}) || !v.HasNames {
		t.Errorf("hpe: %+v", v)
	}
	// 오늘 hpe-3 이 완료가 되면 조치완료, hpe-2 가 디스크미인식이 되면 조치완료가 아니다
	cur := []repHost{hh("HPE", "hpe-1", stDone, true), hh("HPE", "hpe-2", stNoDisk, false), hh("HPE", "hpe-3", stDone, false), hh("HPE", "hpe-4", stStopped, false)}
	a, _ := buildReport(cur, []string{"HPE"}, rb, repToday, 2)
	if !strings.Contains(a, "    접속불가서버 1대 (전일대비 -1대)\n        - hpe-3 조치완료\n        접속불가\n          - hpe-2 디스크 미인식\n") {
		t.Errorf("조치완료: %q", a)
	}
}

func TestSnapshotThenReport(t *testing.T) {
	// 어제 저장본으로 만든 기준 → 오늘 리포트(+ 접속불가 해소로 두 본)
	rules := parseConf("VENDOR_A=a*\n").Vendors
	yesterday := []repHost{hh("A", "a1", stUnreach, false), hh("A", "a2", stDone, true)}
	today := []repHost{hh("A", "a1", stDone, true), hh("A", "a2", stDone, true)}
	d := t.TempDir()
	y := repToday.AddDate(0, 0, -1)
	if _, err := writeSnapshot(d, makeSnapshot(yesterday, y), y); err != nil {
		t.Fatal(err)
	}
	rb, _, err := loadBaseline(d, today, rules, repToday)
	if err != nil {
		t.Fatal(err)
	}
	a, b := buildReport(today, vendorOrder(rules), rb, repToday, 2)
	if !strings.Contains(a, "    접속불가서버 0대 (전일대비 -1대)\n        - a1 조치완료\n") || strings.Contains(b, "접속불가") || b == "" {
		t.Errorf("a=%q\nb=%q", a, b)
	}
}

func TestNewRepHosts(t *testing.T) {
	rules := parseConf("VENDOR_HPE=hpe*\n").Vendors
	rows := []hostRow{
		{Host: "hpe-1", Status: stDone, Stress: "O"},
		{Host: "x-1", Status: stUnreach, Stress: "-"},
		{Host: "hpe-2", Status: stDone, Stress: "NOTRUN", Excluded: true},
	}
	got := newRepHosts(rows, rules)
	want := []repHost{{"hpe-1", "HPE", stDone, true}, {"x-1", "기타", stUnreach, false}, {"hpe-2", "HPE", stDone, false}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("%+v", got)
	}
}

// ---------------------------------------------------------------------------
// parseComment
// ---------------------------------------------------------------------------

var commentKnown = []string{"HPE", "DELL", "기타"}

func vsum(bv *repBaseVendor) string {
	if bv == nil {
		return "-"
	}
	f := func(p *int) string {
		if p == nil {
			return "-"
		}
		return fmt.Sprint(*p)
	}
	names := bv.Names
	if names == nil {
		names = []string{}
	}
	return fmt.Sprintf("os=%s load=%s unr=%s names=%v has=%v", f(bv.OSDone), f(bv.LoadDone), f(bv.Unreach), names, bv.HasNames)
}

func repl(s, old, new string) string { return strings.ReplaceAll(s, old, new) }

// dropLines 는 정확히 같은 줄(오른쪽 공백 무시)을 지운다.
func dropLines(s string, drop ...string) string {
	var out []string
outer:
	for _, l := range strings.Split(s, "\n") {
		for _, d := range drop {
			if strings.TrimRight(l, " ") == d {
				continue outer
			}
		}
		out = append(out, l)
	}
	return strings.Join(out, "\n")
}

func TestParseCommentTable(t *testing.T) {
	const hpe1 = "os=554 load=500 unr=3 names=[hpe-0007 hpe-0312 hpe-0440] has=true"
	const dell1 = "os=120 load=120 unr=0 names=[] has=true"
	hpeBlock := []string{"    접속불가서버 3대 (전일대비 -2대)", "        - hpe-0101 조치완료", "        - hpe-0230 조치완료",
		"        접속불가", "          - hpe-0007", "          - hpe-0312", "          - hpe-0440"}
	noIndent := func(s string) string {
		var o []string
		for _, l := range strings.Split(s, "\n") {
			o = append(o, strings.TrimLeft(l, " "))
		}
		return strings.Join(o, "\n")
	}
	tabs := func(s string) string {
		var o []string
		for _, l := range strings.Split(s, "\n") {
			n := len(l) - len(strings.TrimLeft(l, " "))
			o = append(o, strings.Repeat("\t", n/4+map[bool]int{true: 1, false: 0}[n%4 != 0])+strings.TrimLeft(l, " ")+"  ")
		}
		return strings.Join(o, "\n")
	}
	fullwidth := func(s string) string {
		var sb strings.Builder
		for _, r := range s {
			if r >= 0x21 && r <= 0x7e {
				r += 0xFEE0
			}
			sb.WriteRune(r)
		}
		return sb.String()
	}
	withoutHPEUnreach := dropLines(sampleReport, hpeBlock...)

	cases := []struct {
		no    int
		name  string
		text  string
		hpe   string
		dell  string
		other string // 기타
		notes []string
	}{
		{1, "출력 예 그대로", sampleReport, hpe1, dell1, "-", nil},
		{2, "들여쓰기 없음", noIndent(sampleReport), hpe1, dell1, "-", nil},
		{3, "탭 들여쓰기·줄 끝 공백", tabs(sampleReport), hpe1, dell1, "-", nil},
		{4, "숫자와 대 사이 공백", regexp.MustCompile(`(\d+)대`).ReplaceAllString(sampleReport, "$1 대"), hpe1, dell1, "-", nil},
		{5, "전각 숫자·영문·쌍점", fullwidth(sampleReport), hpe1, dell1, "-", nil},
		{6, "소문자 제목", repl(repl(sampleReport, "HPE:", "hpe:"), "DELL:", "Dell :"), hpe1, dell1, "-", nil},
		{7, "쌍점 없는 제목", repl(repl(sampleReport, "HPE:", "HPE"), "DELL:", "DELL"), hpe1, dell1, "-", nil},
		{8, "HPE 접속불가 블록 없음", withoutHPEUnreach, "os=554 load=500 unr=0 names=[] has=true", dell1, "-", nil},
		{9, "숫자를 고침(전일대비는 읽지 않음)", repl(sampleReport, "554대 완료, 1대", "550대 완료, 1대"), "os=550 load=500 unr=3 names=[hpe-0007 hpe-0312 hpe-0440] has=true", dell1, "-", nil},
		{10, "사이에 메모 줄", repl(sampleReport, "          - hpe-0440\n", "          - hpe-0440\n※ 내일 랙 3 작업\n담당: 홍길동\n"), hpe1, dell1, "-", nil},
		{11, "DELL 블록 없음", dropLines(sampleReport, "", "DELL:", "    OS설치 120대 중 120대 완료, 0대 OS 설치 중 (전일대비 +0대)", "    부하테스트 120대 중 120대 완료, 전일대비 +3대, 완료"), hpe1, "-", "-", nil},
		{12, "빈 글", "", "-", "-", "-", nil},
		{13, "이름 한 줄에 쉼표", repl(repl(repl(sampleReport, "          - hpe-0007\n", "          - hpe-0007, hpe-0312 hpe-0440\n"), "          - hpe-0312\n", ""), "          - hpe-0440\n", ""), hpe1, dell1, "-", nil},
		{14, "명단 제목 줄 없음", dropLines(sampleReport, "        접속불가"), hpe1, dell1, "-", nil},
		{15, "접속불가 대수 숫자 없음", repl(sampleReport, "접속불가서버 3대 (전일대비 -2대)", "접속불가서버 (전일대비 -2대)"), hpe1, dell1, "-", nil},
		{16, "숫자 5, 이름 3개", repl(sampleReport, "접속불가서버 3대", "접속불가서버 5대"), "os=554 load=500 unr=5 names=[hpe-0007 hpe-0312 hpe-0440] has=true", dell1, "-", []string{"HPE: 접속불가 숫자 5대와 이름 3개가 다릅니다"}},
		{17, "설정에 없는 벤더", sampleReport + "\nLENOVO:\n    OS설치 10대 중 10대 완료, 0대 OS 설치 중 (전일대비 +0대)\n", hpe1, dell1, "-", []string{"설정에 없는 벤더: LENOVO"}},
		{18, "조치완료 줄의 이름을 고침", repl(sampleReport, "- hpe-0101 조치완료", "- hpe-9999 조치완료"), hpe1, dell1, "-", nil},
		{19, "벤더 제목 없이 시작", dropLines(sampleReport, "HPE:", "DELL:"), "-", "-", "-", nil},
		{20, "HPE 블록이 두 번", sampleReport + "\nHPE:\n    OS설치 600대 중 560대 완료\n", "os=560 load=- unr=0 names=[] has=true", dell1, "-", []string{"HPE: 블록이 두 번 나와 뒤의 블록 값을 씁니다"}},
		{21, "명단에 한글 메모", repl(sampleReport, "          - hpe-0007\n", "          - hpe-0007 (전원 불량)\n"), hpe1, dell1, "-", nil},
		{22, "명단에 디스크 미인식 줄(이름만 읽는다)", repl(repl(sampleReport, "          - hpe-0007\n", "          - hpe-0007 디스크 미인식\n"), "          - hpe-0440\n", "          - hpe-0440 디스크 미인식\n"), hpe1, dell1, "-", nil},
		{23, "디스크 미인식 줄·전각 숫자·영문", fullwidth(repl(sampleReport, "          - hpe-0312\n", "          - hpe-0312 디스크 미인식\n")), hpe1, dell1, "-", nil},
		{24, "디스크 미인식 줄에 쉼표·들여쓰기 없음", noIndent(repl(sampleReport, "          - hpe-0312\n", "          - hpe-0312, 디스크 미인식\n")), hpe1, dell1, "-", nil},
	}
	for _, tc := range cases {
		rb, notes := parseComment(tc.text, commentKnown)
		if rb == nil || rb.Label != "코멘트" {
			t.Errorf("#%d %s: rb=%+v", tc.no, tc.name, rb)
			continue
		}
		got := [3]string{vsum(rb.Vendors["hpe"]), vsum(rb.Vendors["dell"]), vsum(rb.Vendors["기타"])}
		want := [3]string{tc.hpe, tc.dell, tc.other}
		if got != want {
			t.Errorf("#%d %s:\n got %v\nwant %v", tc.no, tc.name, got, want)
		}
		if !reflect.DeepEqual(notes, tc.notes) {
			t.Errorf("#%d %s: notes=%q want %q", tc.no, tc.name, notes, tc.notes)
		}
		for k := range rb.Vendors {
			if k != "hpe" && k != "dell" && k != "기타" {
				t.Errorf("#%d %s: 설정에 없는 벤더가 결과에 들어감: %q", tc.no, tc.name, k)
			}
		}
	}
}

func TestParseCommentMore(t *testing.T) {
	// 값 없음 규칙: 접속불가 숫자는 있고 명단만 없음 → 명단은 모름
	rb, notes := parseComment("HPE:\n    OS설치 10대 중 9대 완료\n    접속불가서버 2대 (전일대비 +1대)\n", commentKnown)
	if got := vsum(rb.Vendors["hpe"]); got != "os=9 load=- unr=2 names=[] has=false" || notes != nil {
		t.Errorf("명단만 없음: %s %v", got, notes)
	}
	// 숫자 0, 명단 없음 → 아는 값
	rb, _ = parseComment("HPE:\n    접속불가서버 0대 (전일대비 -1대)\n", commentKnown)
	if got := vsum(rb.Vendors["hpe"]); got != "os=- load=- unr=0 names=[] has=true" {
		t.Errorf("0대: %s", got)
	}
	// 음수는 부호를 읽지 않는다(경계 28)
	rb, _ = parseComment("HPE:\n OS설치 5대 중 -5대 완료\n 부하테스트 5대 중 -3대 완료\n", commentKnown)
	if got := vsum(rb.Vendors["hpe"]); !strings.HasPrefix(got, "os=5 load=3 ") {
		t.Errorf("음수: %s", got)
	}
	// 기타 블록, 설정에 없는 벤더의 값은 쓰지 않는다
	rb, notes = parseComment("기타:\n    OS설치 3대 중 3대 완료\nLENOVO:\n    OS설치 9대 중 9대 완료\nLENOVO:\n    OS설치 8대 중 8대 완료\n", commentKnown)
	if vsum(rb.Vendors["기타"]) != "os=3 load=- unr=0 names=[] has=true" || len(rb.Vendors) != 1 || !reflect.DeepEqual(notes, []string{"설정에 없는 벤더: LENOVO"}) {
		t.Errorf("기타/LENOVO: %v %v", rb.Vendors, notes)
	}
	// 불릿 종류 · CRLF · 전각 공백
	rb, _ = parseComment("• HPE:\r\n·　OS설치 5대 중 4대 완료\r\n* 접속불가서버 1대\r\n  접속불가\r\n  - HPE-1\r\n", commentKnown)
	if got := vsum(rb.Vendors["hpe"]); got != "os=4 load=- unr=1 names=[HPE-1] has=true" {
		t.Errorf("불릿: %s", got)
	}
	// 큰 숫자는 읽지 못하면 nil
	rb, _ = parseComment("HPE:\n OS설치 99999999999999999999대 중 99999999999999999999대 완료\n", commentKnown)
	if got := vsum(rb.Vendors["hpe"]); !strings.HasPrefix(got, "os=- ") {
		t.Errorf("큰 수: %s", got)
	}
	// known 이 비어 있어도 쌍점 제목은 설정에 없는 벤더로 알린다
	rb, notes = parseComment("HPE:\n OS설치 5대 중 4대 완료\n", nil)
	if len(rb.Vendors) != 0 || !reflect.DeepEqual(notes, []string{"설정에 없는 벤더: HPE"}) {
		t.Errorf("known 없음: %v %v", rb.Vendors, notes)
	}
	// 크기 검사
	if checkCommentSize("a\n") != nil || checkCommentSize(strings.Repeat("a\n", 1999)) != nil {
		t.Error("정상 크기가 오류")
	}
	if checkCommentSize(strings.Repeat("a\n", 2000)) == nil || checkCommentSize(strings.Repeat("a", 256*1024+1)) == nil {
		t.Error("큰 코멘트가 통과")
	}
	// 현재 목록에 없는 이름 수
	rb, _ = parseComment(sampleReport, commentKnown)
	if n := unknownCommentNames(rb, []repHost{{Host: "HPE-0007"}, {Host: "hpe-0312"}}); n != 1 {
		t.Errorf("목록에 없는 이름 수=%d", n)
	}
	if unknownCommentNames(nil, nil) != 0 {
		t.Error("nil")
	}
}

// 방식 2 는 어제의 리포트를 그대로 읽는다: buildReport → parseComment 왕복
func TestReportRoundTrip(t *testing.T) {
	cur := sampleCur()
	order := []string{"HPE", "DELL"}
	a, _ := buildReport(cur, order, sampleBase(), repToday, 2)
	rb, notes := parseComment(a, append(append([]string{}, order...), vendorOther))
	if len(notes) != 0 {
		t.Errorf("notes=%v", notes)
	}
	if got := vsum(rb.Vendors["hpe"]); got != "os=554 load=500 unr=3 names=[hpe-0007 hpe-0312 hpe-0440] has=true" {
		t.Errorf("hpe: %s", got)
	}
	if got := vsum(rb.Vendors["dell"]); got != "os=120 load=120 unr=0 names=[] has=true" {
		t.Errorf("dell: %s", got)
	}
	// 두 본의 본 A 도 같은 값으로 읽힌다(조치완료 줄은 버린다)
	cur2 := []repHost{hh("A", "a1", stDone, true), hh("A", "a2", stDone, false), hh("기타", "z1", stInstall, false)}
	a2, b2 := buildReport(cur2, []string{"A"}, &repBase{Vendors: map[string]*repBaseVendor{"a": {Unreach: intp(1), Names: []string{"a1"}, HasNames: true}}}, repToday, 2)
	if b2 == "" {
		t.Fatal("두 본이어야 함")
	}
	for _, text := range []string{a2, b2} {
		rb, _ = parseComment(text, []string{"A", "기타"})
		if got := vsum(rb.Vendors["a"]); !strings.HasPrefix(got, "os=2 load=1 ") {
			t.Errorf("A: %s\n%s", got, text)
		}
		if got := vsum(rb.Vendors["기타"]); got != "os=0 load=0 unr=0 names=[] has=true" {
			t.Errorf("기타: %s", got)
		}
	}
	// 본 A: 0대 줄을 읽어 접속불가 0, B: 줄 없음 → 0 (둘 다 아는 값)
	rb, _ = parseComment(a2, []string{"A", "기타"})
	if got := vsum(rb.Vendors["a"]); got != "os=2 load=1 unr=0 names=[] has=true" {
		t.Errorf("본 A 접속불가: %s", got)
	}
}

// 디스크미인식 호스트가 있는 글도 왕복한다: 이름은 호스트 이름, 대수는 `접속불가서버 N대` 의 N(디스크미인식 포함).
func TestReportRoundTripNoDisk(t *testing.T) {
	cur := []repHost{hh("HPE", "hpe-1", stDone, true), hh("HPE", "hpe-2", stUnreach, false), hh("HPE", "hpe-3", stNoDisk, false), hh("HPE", "hpe-4", stNoDisk, false)}
	order := []string{"HPE"}
	a, b := buildReport(cur, order, nil, repToday, 2)
	if b != "" || !strings.Contains(a, "접속불가서버 3대 (전일 정보 없음)\n        접속불가\n          - hpe-2\n          - hpe-3 디스크 미인식\n          - hpe-4 디스크 미인식\n") {
		t.Fatalf("글: %q b=%q", a, b)
	}
	rb, notes := parseComment(a, []string{"HPE", vendorOther})
	if len(notes) != 0 {
		t.Errorf("notes=%v", notes)
	}
	if got := vsum(rb.Vendors["hpe"]); got != "os=1 load=1 unr=3 names=[hpe-2 hpe-3 hpe-4] has=true" {
		t.Errorf("hpe: %s", got)
	}
	// 읽은 기준으로 다시 만들면 접속불가 대수의 전일대비는 +0대, 조치완료는 없다
	a2, _ := buildReport(cur, order, rb, repToday, 2)
	if !strings.Contains(a2, "접속불가서버 3대 (전일대비 +0대)\n        접속불가\n") || strings.Contains(a2, "조치완료") {
		t.Errorf("재작성: %q", a2)
	}
}

// 임의 입력에도 panic 하지 않는다(고정 seed).
func TestParseCommentNoPanic(t *testing.T) {
	rng := rand.New(rand.NewSource(20261112))
	pieces := []string{"HPE:", "DELL", "기타:", "OS설치 ", "558대 중 ", "554대 완료", "부하테스트", "접속불가서버", "접속불가", "접속불가:",
		"조치완료", "전일대비", "+5대", "(", ")", ":", "：", "- ", "• ", "\t", "\r", "\n", "\n", " ", "　", "hpe-0007", ",", "9999999999999999999999", "０１２", "대", "\u00a0", "※"}
	for i := 0; i < 3000; i++ {
		var sb strings.Builder
		if i%2 == 0 { // 순수 임의 바이트
			b := make([]byte, rng.Intn(300))
			rng.Read(b)
			sb.Write(b)
		} else { // 조각 이어 붙이기
			for n := rng.Intn(40); n > 0; n-- {
				sb.WriteString(pieces[rng.Intn(len(pieces))])
			}
		}
		rb, _ := parseComment(sb.String(), commentKnown)
		if rb == nil || rb.Vendors == nil {
			t.Fatalf("결과가 nil: %q", sb.String())
		}
	}
	for _, s := range []string{"", "\x00", "\xff\xfe", ":", "-", "- ", "•", "HPE:\n:\n::", "接続不可:", strings.Repeat("HPE:\n", 5000)} {
		parseComment(s, nil)
		parseComment(s, commentKnown)
	}
}
