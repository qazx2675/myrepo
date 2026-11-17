package main

// yum update 모드의 상태기계·커널 조회(계획서 4.8, 6.3 의 20단계) 단위 테스트.

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	kOld  = "4.18.0-513.el8.x86_64"
	kNew  = "4.18.0-553.el8.x86_64"
	boot0 = "2026-11-17 09:00:00"
)

func yumRec() Record { return Record{Name: "y", Vendor: "D", Mode: ModeYum} }

func okObs(kernel, boot string) *YumObs { return &YumObs{Kernel: kernel, Boot: boot} }

// 4.8 전이표의 주 경로: 첫 접속 -> 접속가능 -> (유지) -> ping X -> 재부팅됨 -> 커널 동일 -> 커널 변경 -> 완료
func TestApplyYumMainPath(t *testing.T) {
	r := yumRec()
	now := t0

	// 첫 접속: 기준 기록
	r = applyYum(r, true, PortOpen, okObs(kOld, boot0), now)
	if r.Status != StatusAlive || r.YumKernel != kOld || r.YumBoot != boot0 || !r.YumSince.Equal(now) ||
		r.YumNow != kOld || r.Reason != "기준 커널 "+kOld {
		t.Fatalf("첫 접속: %+v", r)
	}

	// 같은 커널·같은 부팅 시각: 접속가능 유지, 기준은 바뀌지 않음
	now = now.Add(time.Minute)
	r = applyYum(r, true, PortOpen, okObs(kOld, "2026-11-17 09:00:01"), now)
	if r.Status != StatusAlive || !r.YumSince.Equal(t0) || r.YumBoot != boot0 || !r.RebootedAt.IsZero() {
		t.Fatalf("유지: %+v", r)
	}

	// ping X: 재부팅 중
	t1 := now.Add(time.Minute)
	r = applyYum(r, false, PortTimeout, nil, t1)
	if r.Status != StatusRebooted || r.Reason != "재부팅 중(ping 응답 없음)" || !r.RebootedAt.Equal(t1) {
		t.Fatalf("ping X: %+v", r)
	}
	// 계속 ping X: RebootedAt 은 처음 시각 유지
	r = applyYum(r, false, PortTimeout, nil, t1.Add(time.Minute))
	if r.Status != StatusRebooted || !r.RebootedAt.Equal(t1) {
		t.Fatalf("ping X 계속: %+v", r)
	}

	// ping O, 커널 동일, 새 부팅 시각: 재부팅됨 유지(완료 아님)
	r = applyYum(r, true, PortOpen, okObs(kOld, "2026-11-17 09:05:00"), t1.Add(2*time.Minute))
	if r.Status != StatusRebooted || r.Reason != "재부팅됨, 커널 동일 "+kOld || !r.RebootedAt.Equal(t1) || r.Completed() {
		t.Fatalf("커널 동일: %+v", r)
	}
	// 부팅 시각이 기준과 같아도(재부팅 흔적이 이미 있으므로) 재부팅됨 유지
	r = applyYum(r, true, PortOpen, okObs(kOld, boot0), t1.Add(3*time.Minute))
	if r.Status != StatusRebooted {
		t.Fatalf("재부팅됨은 유지: %+v", r)
	}

	// 커널 변경: 완료
	t2 := t1.Add(10 * time.Minute)
	r = applyYum(r, true, PortOpen, okObs(kNew, "2026-11-17 09:15:00"), t2)
	if r.Status != StatusYumDone || !r.YumDoneAt.Equal(t2) || r.YumNow != kNew || r.YumKernel != kOld ||
		r.Reason != "커널 "+kOld+" → "+kNew || !r.Completed() {
		t.Fatalf("커널 변경: %+v", r)
	}

	// 완료 뒤에는 평소 회차에서 바뀌지 않는다(ping X 여도)
	if got := applyYum(r, false, PortTimeout, nil, t2.Add(time.Minute)); got.Status != StatusYumDone || !got.YumDoneAt.Equal(t2) {
		t.Fatalf("완료 유지: %+v", got)
	}
}

// 접속가능에서 ping X 없이 바로 커널이 바뀐 경우(회차 사이 재부팅)도 완료다.
func TestApplyYumKernelChangeWithoutPingX(t *testing.T) {
	r := applyYum(yumRec(), true, PortOpen, okObs(kOld, boot0), t0)
	r = applyYum(r, true, PortOpen, okObs(kNew, "2026-11-17 10:00:00"), t0.Add(time.Hour))
	if r.Status != StatusYumDone || !r.YumDoneAt.Equal(t0.Add(time.Hour)) {
		t.Fatalf("%+v", r)
	}
}

// 빠른 재부팅: ping X 를 보지 못했어도 부팅 시각이 60초 넘게 바뀌면 재부팅됨(1초·60초 오차는 허용).
func TestApplyYumFastRebootByBootTime(t *testing.T) {
	base := applyYum(yumRec(), true, PortOpen, okObs(kOld, boot0), t0)
	tests := []struct {
		boot string
		want Status
	}{
		{"2026-11-17 09:00:01", StatusAlive},    // 1초 오차
		{"2026-11-17 09:01:00", StatusAlive},    // 정확히 60초: 넘지 않음
		{"2026-11-17 09:01:01", StatusRebooted}, // 61초
		{"2026-11-17 08:58:59", StatusRebooted}, // 거꾸로 61초
		{"2026-11-18 03:00:00", StatusRebooted},
		{"", StatusAlive},        // 읽지 못함 = 다르지 않음
		{"garbage", StatusAlive}, // 읽지 못함
		{"2026/11/17 10", StatusAlive},
	}
	now := t0.Add(5 * time.Minute)
	for _, tc := range tests {
		got := applyYum(base, true, PortOpen, okObs(kOld, tc.boot), now)
		if got.Status != tc.want {
			t.Errorf("boot %q: %v, want %v", tc.boot, got.Status, tc.want)
		}
		if tc.want == StatusRebooted && (!got.RebootedAt.Equal(now) || got.Reason != "재부팅됨, 커널 동일 "+kOld) {
			t.Errorf("boot %q: %+v", tc.boot, got)
		}
		if got.YumBoot != boot0 {
			t.Errorf("기준 부팅 시각이 바뀜: %q", got.YumBoot)
		}
	}
	// 기준 부팅 시각을 읽지 못했으면 부팅 시각으로는 판단하지 않는다
	nob := applyYum(yumRec(), true, PortOpen, okObs(kOld, ""), t0)
	if got := applyYum(nob, true, PortOpen, okObs(kOld, "2026-11-18 03:00:00"), now); got.Status != StatusAlive {
		t.Errorf("기준 부팅 시각 없음: %v", got.Status)
	}
}

func TestBootTimeDiffers(t *testing.T) {
	tests := []struct {
		a, b string
		want bool
	}{
		{boot0, boot0, false},
		{boot0, "2026-11-17 09:01:00", false},
		{boot0, "2026-11-17 09:01:01", true},
		{"2026-11-17 09:01:01", boot0, true},
		{"", boot0, false},
		{boot0, "x", false},
	}
	for _, tc := range tests {
		if got := bootTimeDiffers(tc.a, tc.b); got != tc.want {
			t.Errorf("bootTimeDiffers(%q,%q)=%v", tc.a, tc.b, got)
		}
	}
}

// ssh 실패(22번 거부·무응답·조회 오류)는 상태를 유지하고 이유만 남긴다. 기준이 없으면 확인전.
func TestApplyYumSSHFailureKeepsState(t *testing.T) {
	alive := applyYum(yumRec(), true, PortOpen, okObs(kOld, boot0), t0)
	rebooted := applyYum(alive, false, PortTimeout, nil, t0.Add(time.Minute))
	pingx := applyYum(yumRec(), false, PortTimeout, nil, t0)
	if pingx.Status != StatusPingX || pingx.Reason != "ping 응답 없음" || pingx.YumKernel != "" {
		t.Fatalf("기준 전 ping X: %+v", pingx)
	}
	fails := []struct {
		port PortState
		obs  *YumObs
		why  string
	}{
		{PortRefused, nil, "22번 포트 거부"},
		{PortTimeout, nil, "22번 포트 응답 없음"},
		{PortOpen, &YumObs{Err: "SSH 접속 실패: x"}, "ssh 확인 실패: SSH 접속 실패: x"},
		{PortOpen, &YumObs{}, "ssh 확인 실패: 응답 없음"},
		{PortOpen, nil, "ssh 확인 실패: 응답 없음"},
	}
	now := t0.Add(10 * time.Minute)
	for _, f := range fails {
		for _, prev := range []Record{yumRec(), pingx, alive, rebooted} {
			got := applyYum(prev, true, f.port, f.obs, now)
			want := prev.Status
			if prev.YumKernel == "" {
				want = StatusNone // 기준 전에는 확인전(ping O 이므로 ping X 아님)
			}
			if got.Status != want || got.Reason != f.why {
				t.Errorf("%v + %q: %v %q, want %v", prev.Status, f.why, got.Status, got.Reason, want)
			}
			if got.YumKernel != prev.YumKernel || got.YumBoot != prev.YumBoot || !got.YumSince.Equal(prev.YumSince) ||
				!got.RebootedAt.Equal(prev.RebootedAt) || got.YumNow != prev.YumNow {
				t.Errorf("%v + %q: 기준·이력이 바뀜 %+v", prev.Status, f.why, got)
			}
		}
	}
	// ssh 가 다시 되면 기준이 그때 기록된다
	r := applyYum(applyYum(yumRec(), true, PortRefused, nil, t0), true, PortOpen, okObs(kOld, boot0), now)
	if r.Status != StatusAlive || !r.YumSince.Equal(now) {
		t.Errorf("ssh 회복: %+v", r)
	}
}

func TestRecordCompletedAndDelay(t *testing.T) {
	for _, st := range []Status{StatusNone, StatusPingX, StatusAlive, StatusRebooted} {
		if (Record{Mode: ModeYum, Status: st}).Completed() {
			t.Errorf("yum %v 는 완료가 아님", st)
		}
	}
	for _, st := range []Status{StatusYumDone, StatusOff} {
		if !(Record{Mode: ModeYum, Status: st}).Completed() {
			t.Errorf("yum %v 는 완료", st)
		}
	}
	for s := Status(0); s < statusCount; s++ { // OS 서버는 Status.IsCompleted 와 같다
		if (Record{Status: s}).Completed() != s.IsCompleted() {
			t.Errorf("OS %v", s)
		}
	}

	delay := 60 * time.Minute
	alive := Record{Mode: ModeYum, Status: StatusAlive, YumSince: t0, YumKernel: kOld}
	if yumDelayed(alive, t0.Add(59*time.Minute), delay) || !yumDelayed(alive, t0.Add(delay), delay) {
		t.Error("접속가능 지연 경계")
	}
	reb := alive
	reb.Status = StatusRebooted
	if !yumDelayed(reb, t0.Add(2*time.Hour), delay) {
		t.Error("재부팅됨(커널 동일)도 지연")
	}
	done := alive
	done.Status = StatusYumDone
	off := alive
	off.Status = StatusOff
	none := Record{Mode: ModeYum, Status: StatusPingX}
	osr := Record{Status: StatusInstalling, YumSince: t0}
	for _, r := range []Record{done, off, none, osr} {
		if yumDelayed(r, t0.Add(10*time.Hour), delay) {
			t.Errorf("지연 아님: %+v", r)
		}
	}
	// 상태는 지연으로 바뀌지 않는다
	if got := applyYum(alive, true, PortOpen, okObs("", ""), t0.Add(10*time.Hour)); got.Status != StatusAlive {
		t.Errorf("지연이 상태를 바꿈: %v", got.Status)
	}
}

func TestApplyCompletedPingYum(t *testing.T) {
	later := t0.Add(3 * time.Hour)
	done := Record{Name: "y", Mode: ModeYum, Status: StatusYumDone, YumDoneAt: t0}
	off := ApplyCompletedPing(done, false, later)
	if off.Status != StatusOff || !off.OffAt.Equal(later) || off.Reason != "완료 후 ping 응답 없음" || !off.YumDoneAt.Equal(t0) {
		t.Fatalf("완료후꺼짐: %+v", off)
	}
	back := ApplyCompletedPing(off, true, later.Add(time.Hour))
	if back.Status != StatusYumDone || back.Reason != "완료 후 꺼졌다가 다시 켜짐" || !back.RebootedAt.Equal(later.Add(time.Hour)) {
		t.Fatalf("복귀: %+v", back)
	}
	if got := ApplyCompletedPing(done, true, later); got.Status != StatusYumDone {
		t.Errorf("ping O 는 유지: %v", got.Status)
	}
	if got := ApplyCompletedPing(off, false, later); got.Status != StatusOff {
		t.Errorf("꺼짐 유지: %v", got.Status)
	}
	// yum 서버의 재부팅됨·접속가능은 c 의 대상이 아니며, 넘겨도 바뀌지 않는다(OS 의 재부팅됨 -> 완료후꺼짐 규칙을 타지 않음)
	for _, st := range []Status{StatusRebooted, StatusAlive} {
		if got := ApplyCompletedPing(Record{Mode: ModeYum, Status: st}, false, later); got.Status != st {
			t.Errorf("yum %v: %v", st, got.Status)
		}
	}
}

func TestParseYumOutput(t *testing.T) {
	stdout := "a: NS_YUM\na: " + kOld + "\na: " + boot0 + "\n" +
		"b: motd 인사말\nb: NS_YUM\nb: Last login: x\nb: " + kNew + "\r\nb: 2026-11-16 23:59:59\r\n" +
		"c: NS_YUM\n" + // 커널 줄 없음
		"e: ERROR: NS_YUM\ne: ERROR: " + kOld + "\n" + // uptime -s 실패(옛 procps), ERROR 접두사
		"f: " + kOld + "\nf: " + boot0 + "\n" + // 표식 없음
		"g: NS_YUM\ng: " + kOld + "\ng: not a time\n" +
		"garbage line\n"
	stderr := "c: 경고 줄\nc: 둘째 줄\nd: SSH 접속 실패: dial tcp d:22: i/o timeout\ne: uptime: invalid option -- 's'\n"
	hosts := []string{"a", "b", "c", "d", "e", "f", "g", "h"}
	got := parseYumOutput(stdout, stderr, hosts)
	want := map[string]YumObs{
		"a": {Kernel: kOld, Boot: boot0},
		"b": {Kernel: kNew, Boot: "2026-11-16 23:59:59"},
		"c": {Err: "경고 줄"},
		"d": {Err: "SSH 접속 실패: dial tcp d:22: i/o timeout"},
		"e": {Kernel: kOld},
		"f": {Err: "응답 없음"},
		"g": {Kernel: kOld},
		"h": {Err: "응답 없음"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got  %+v\nwant %+v", got, want)
	}
	if got := parseYumOutput("", "", nil); len(got) != 0 {
		t.Errorf("빈 목록: %v", got)
	}
}

// 원격 명령은 csh/tcsh 에서도 돌아야 하고(변수·중괄호·$()·따옴표 없음), 랩 gossh 가 끝 따옴표를 자르므로 따옴표로 끝나지 않는다.
func TestYumCmdCshSafe(t *testing.T) {
	if yumCmd != "echo NS_YUM; uname -r; uptime -s" {
		t.Errorf("명령: %q", yumCmd)
	}
	if strings.ContainsAny(yumCmd, "${}`'\"") || strings.HasSuffix(yumCmd, "'") || strings.HasSuffix(yumCmd, `"`) {
		t.Errorf("csh 에서 위험한 문자: %q", yumCmd)
	}
}

func TestYumCheckStub(t *testing.T) {
	cfg, dir := stubEnv(t, "a done\nb installing 4.18.0-553.el8.x86_64 2026-11-17 10:30:00\nc down\n")
	log := filepath.Join(dir, "log")
	t.Setenv("STUB_GOSSH_LOG", log)
	got, err := YumCheck(cfg, []string{"a", "b", "c", "z"})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]YumObs{
		"a": {Kernel: kOld, Boot: boot0},
		"b": {Kernel: kNew, Boot: "2026-11-17 10:30:00"},
		"c": {Err: "SSH 접속 실패: dial tcp c:22: i/o timeout"},
		"z": {Err: "SSH 접속 실패: dial tcp z:22: i/o timeout"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got  %+v\nwant %+v", got, want)
	}
	// 빈 목록은 gossh 를 실행하지 않는다
	cfg.Gossh = "/nonexistent/gossh"
	if m, err := YumCheck(cfg, nil); err != nil || len(m) != 0 {
		t.Errorf("YumCheck(nil) = %v %v", m, err)
	}
}

// yumFake 는 fakeEnv 의 Deps 에 yum 조회를 붙인다.
type yumFake struct {
	*fakeEnv
	ymu     sync.Mutex
	kernel  map[string]string
	boot    map[string]string
	fail    error
	yumCall [][]string
}

func newYumFake() *yumFake {
	return &yumFake{fakeEnv: newFake(), kernel: map[string]string{}, boot: map[string]string{}}
}

func (f *yumFake) deps() Deps {
	d := f.fakeEnv.deps()
	d.Yum = func(_ *Config, hosts []string) (map[string]YumObs, error) {
		f.ymu.Lock()
		defer f.ymu.Unlock()
		f.yumCall = append(f.yumCall, append([]string{}, hosts...))
		if f.fail != nil {
			return nil, f.fail
		}
		m := map[string]YumObs{}
		for _, h := range hosts {
			if k := f.kernel[h]; k != "" {
				m[h] = YumObs{Kernel: k, Boot: f.boot[h]}
			} else {
				m[h] = YumObs{Err: "SSH 접속 실패"}
			}
		}
		return m, nil
	}
	return d
}

func (f *yumFake) calls() [][]string {
	f.ymu.Lock()
	defer f.ymu.Unlock()
	return append([][]string{}, f.yumCall...)
}

// OS·yum 이 섞여도 OS 서버는 이전과 똑같이 ping·22번·gossh -pm·디스크 확인을 하고, yum 서버는 -pm·디스크 확인 없이
// 회차당 yum 조회 한 번만 한다. 완료된 yum 서버는 평소 회차에서 ping 도 하지 않는다.
func TestRoundMixedOSAndYum(t *testing.T) {
	f := newYumFake()
	hosts := []string{"c1", "s1", "y1", "y2", "y3", "y4", "y5"}
	f.pmDone["c1"] = true
	f.pmInst["s1"] = true
	f.pmInst["y1"], f.pmDone["y4"], f.diskBad["y1"] = true, true, true // yum 서버에 -pm 이 가면 드러나도록
	f.kernel["y1"], f.boot["y1"] = kOld, boot0
	f.pingDown["y2"] = true
	f.refused["y3"] = true
	f.kernel["y4"], f.boot["y4"] = kOld, boot0
	// y5: 22번은 열렸지만 조회 실패
	path := filepath.Join(t.TempDir(), "st.json")
	yset := map[string]bool{"y1": true, "y2": true, "y3": true, "y4": true, "y5": true}
	col, err := NewCollector(testCfg(), hosts, path, f.deps(), WithYum(yset))
	if err != nil {
		t.Fatal(err)
	}
	if err := col.Round(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(f.pinged, [][]string{hosts}) {
		t.Errorf("ping 한 번에 모두: %v", f.pinged)
	}
	if !reflect.DeepEqual(f.pmCalls, [][]string{{"c1", "s1"}}) || !reflect.DeepEqual(f.diskCall, [][]string{{"s1"}}) {
		t.Errorf("OS 만 -pm·디스크: pm=%v disk=%v", f.pmCalls, f.diskCall)
	}
	if got := f.calls(); !reflect.DeepEqual(got, [][]string{{"y1", "y4", "y5"}}) {
		t.Errorf("yum 조회: %v", got)
	}
	want := map[string]Status{"c1": StatusDone, "s1": StatusInstalling, "y1": StatusAlive, "y2": StatusPingX,
		"y3": StatusNone, "y4": StatusAlive, "y5": StatusNone}
	for h, w := range want {
		if got := status(col, h); got != w {
			t.Errorf("%s: %v, want %v (%q)", h, got, w, recOf(col, h).Reason)
		}
	}
	if r := recOf(col, "y3"); r.Reason != "22번 포트 거부" {
		t.Errorf("y3 이유: %q", r.Reason)
	}
	if r := recOf(col, "y5"); r.Reason != "ssh 확인 실패: SSH 접속 실패" {
		t.Errorf("y5 이유: %q", r.Reason)
	}

	// 2회차: y4 커널 변경 -> 완료, y1 ping X -> 재부팅됨
	f.advance(time.Minute)
	f.kernel["y4"] = kNew
	f.pingDown["y1"] = true
	if err := col.Round(context.Background()); err != nil {
		t.Fatal(err)
	}
	if status(col, "y4") != StatusYumDone || status(col, "y1") != StatusRebooted {
		t.Errorf("2회차: y4=%v y1=%v", status(col, "y4"), status(col, "y1"))
	}
	if got := f.calls(); !reflect.DeepEqual(got[1], []string{"y4", "y5"}) {
		t.Errorf("2회차 yum 조회: %v", got[1])
	}

	// 3회차: c1(설치완료)·y4(yum 완료)는 ping 하지 않고, yum 재부팅됨 y1 은 계속 확인한다
	f.advance(time.Minute)
	delete(f.pingDown, "y1")
	if err := col.Round(context.Background()); err != nil {
		t.Fatal(err)
	}
	if last := f.pinged[len(f.pinged)-1]; !reflect.DeepEqual(last, []string{"s1", "y1", "y2", "y3", "y5"}) {
		t.Errorf("3회차 ping 대상: %v", last)
	}
	if got := f.calls(); !reflect.DeepEqual(got[2], []string{"y1", "y5"}) {
		t.Errorf("3회차 yum 조회: %v", got[2])
	}
	if r := recOf(col, "y1"); r.Status != StatusRebooted || r.Reason != "재부팅됨, 커널 동일 "+kOld {
		t.Errorf("y1 커널 동일: %+v", r)
	}
	if len(f.pmCalls) != 3 || !reflect.DeepEqual(f.pmCalls[2], []string{"s1"}) {
		t.Errorf("OS -pm 호출: %v", f.pmCalls)
	}
	// OS 집계는 그대로(yum 서버 재부팅됨 수는 21단계에서 분리)
	if s := col.Snapshot(); s.Counts[StatusYumDone] != 1 || s.Counts[StatusDone] != 1 {
		t.Errorf("counts: %v", s.Counts)
	}

	// c: 완료 계열만 ping. y4 꺼짐 -> 완료후꺼짐, y1(재부팅됨, yum) 은 대상 아님
	f.pingDown["y4"] = true
	n := len(f.pinged)
	if err := col.CheckCompleted(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(f.pinged[n], []string{"c1", "y4"}) {
		t.Errorf("c 대상: %v", f.pinged[n])
	}
	if r := recOf(col, "y4"); r.Status != StatusOff || r.Reason != "완료 후 ping 응답 없음" {
		t.Errorf("y4 완료후꺼짐: %+v", r)
	}
	if status(col, "c1") != StatusDone || status(col, "y1") != StatusRebooted {
		t.Errorf("c 뒤: c1=%v y1=%v", status(col, "c1"), status(col, "y1"))
	}
	// 다시 켜지면 yum_update/접속가능 으로 복귀(커널 비교는 다시 하지 않음)
	delete(f.pingDown, "y4")
	f.kernel["y4"] = kOld
	if err := col.CheckCompleted(context.Background()); err != nil {
		t.Fatal(err)
	}
	if r := recOf(col, "y4"); r.Status != StatusYumDone || r.YumNow != kNew || r.Reason != "완료 후 꺼졌다가 다시 켜짐" {
		t.Errorf("y4 복귀: %+v", r)
	}
}

// yum 조회가 실패하면 오류를 남기고 그 서버들의 상태는 유지한다. OS 서버는 그대로 진행한다.
func TestRoundYumErrorKeepsState(t *testing.T) {
	f := newYumFake()
	f.pmDone["c1"] = true
	f.kernel["y1"], f.boot["y1"] = kOld, boot0
	col, err := NewCollector(testCfg(), []string{"c1", "y1", "y2"}, filepath.Join(t.TempDir(), "s.json"), f.deps(),
		WithYum(map[string]bool{"y1": true, "y2": true}))
	if err != nil {
		t.Fatal(err)
	}
	if err := col.Round(context.Background()); err != nil {
		t.Fatal(err)
	}
	f.fail = errors.New("gossh 없음")
	f.advance(time.Minute)
	err = col.Round(context.Background())
	if err == nil || !strings.Contains(err.Error(), "yum 커널 조회: gossh 없음") {
		t.Fatalf("err = %v", err)
	}
	if r := recOf(col, "y1"); r.Status != StatusAlive || r.YumKernel != kOld || r.Reason != "ssh 확인 실패: gossh 실패" {
		t.Errorf("y1: %+v", r)
	}
	if status(col, "y2") != StatusNone || status(col, "c1") != StatusDone {
		t.Errorf("y2=%v c1=%v", status(col, "y2"), status(col, "c1"))
	}
}

// yum 서버가 없거나 모두 완료면 yum 조회 gossh 를 실행하지 않는다(nil Yum 이어도 부르지 않음).
func TestRoundNoYumCallWhenNoTargets(t *testing.T) {
	f := newFake() // Yum 이 nil 인 기존 Deps
	f.pmDone["c1"] = true
	col, err := NewCollector(testCfg(), []string{"c1"}, filepath.Join(t.TempDir(), "s.json"), f.deps())
	if err != nil {
		t.Fatal(err)
	}
	if err := col.Round(context.Background()); err != nil {
		t.Fatal(err)
	}
	yf := newYumFake()
	path := filepath.Join(t.TempDir(), "s.json")
	writeState(t, path, `{"version":1,"hosts":[{"name":"y1","vendor":"D","status":"yum_update/접속가능","first_installing_at":"0001-01-01T00:00:00Z","done_at":"0001-01-01T00:00:00Z","off_at":"0001-01-01T00:00:00Z","rebooted_at":"0001-01-01T00:00:00Z","reason":"","check":{},"mode":"yum","yum_kernel":"`+kOld+`"}]}`)
	col2, err := NewCollector(testCfg(), []string{"y1"}, path, yf.deps(), WithYum(map[string]bool{"y1": true}))
	if err != nil {
		t.Fatal(err)
	}
	if err := col2.Round(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(yf.calls()) != 0 || len(yf.pinged) != 0 {
		t.Errorf("완료 yum 서버에 조회: yum=%v ping=%v", yf.calls(), yf.pinged)
	}
}

// 기준 커널·부팅 시각은 state.json 에 남아 다시 실행해도 유지되고, 다시 기록되지 않는다.
func TestYumBaselinePersistsAcrossReload(t *testing.T) {
	f := newYumFake()
	f.kernel["y1"], f.boot["y1"] = kOld, boot0
	path := filepath.Join(t.TempDir(), "h.pm_state.json")
	opt := WithYum(map[string]bool{"y1": true})
	col, err := NewCollector(testCfg(), []string{"y1"}, path, f.deps(), opt)
	if err != nil {
		t.Fatal(err)
	}
	if err := col.Round(context.Background()); err != nil {
		t.Fatal(err)
	}
	since := f.now

	// 다시 실행: 커널이 이미 바뀐 것처럼 보이지 않도록 같은 값, 시각은 뒤로
	f.advance(2 * time.Hour)
	f.boot["y1"] = "2026-11-17 10:30:00" // 실행 사이에 재부팅됨
	col2, err := NewCollector(testCfg(), []string{"y1"}, path, f.deps(), opt)
	if err != nil {
		t.Fatal(err)
	}
	r := recOf(col2, "y1")
	if r.Status != StatusAlive || r.YumKernel != kOld || r.YumBoot != boot0 || !r.YumSince.Equal(since) || r.YumNow != kOld {
		t.Fatalf("다시 읽은 기준: %+v", r)
	}
	if err := col2.Round(context.Background()); err != nil {
		t.Fatal(err)
	}
	r = recOf(col2, "y1")
	if r.Status != StatusRebooted || r.YumBoot != boot0 || !r.YumSince.Equal(since) {
		t.Errorf("다시 실행 후 재부팅 감지: %+v", r)
	}
	// 커널이 바뀌면 다시 읽은 기준과 비교해 완료
	f.advance(time.Minute)
	f.kernel["y1"] = kNew
	if err := col2.Round(context.Background()); err != nil {
		t.Fatal(err)
	}
	col3, err := NewCollector(testCfg(), []string{"y1"}, path, f.deps(), opt)
	if err != nil {
		t.Fatal(err)
	}
	if r := recOf(col3, "y1"); r.Status != StatusYumDone || r.YumKernel != kOld || r.YumNow != kNew || !r.YumDoneAt.Equal(f.now) {
		t.Errorf("완료 기록 유지: %+v", r)
	}
}
