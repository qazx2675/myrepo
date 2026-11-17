package main

// yum update 모드의 입력·기록(계획서 4.8, 6.3 의 19단계) 단위 테스트.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestYumStatusLabelsAndNumbers(t *testing.T) {
	// 기존 번호 1~7 은 그대로, 새 상태는 뒤에 붙는다
	nums := map[Status]int{StatusInstalling: 1, StatusDelayed: 2, StatusDisk: 3, StatusDone: 4,
		StatusPingX: 5, StatusRebooted: 6, StatusOff: 7, StatusAlive: 8, StatusYumDone: 9}
	for s, n := range nums {
		if int(s) != n {
			t.Errorf("%v 의 번호=%d, want %d", s, int(s), n)
		}
	}
	if statusCount != 10 {
		t.Errorf("statusCount=%d, want 10", statusCount)
	}
	if StatusAlive.String() != "접속가능" || StatusYumDone.String() != "yum_update/접속가능" {
		t.Errorf("라벨: %q %q", StatusAlive, StatusYumDone)
	}
	// 새 상태가 json 에서 한글 이름으로 왕복한다
	for _, st := range []Status{StatusAlive, StatusYumDone} {
		b, err := st.MarshalText()
		var got Status
		if err != nil || got.UnmarshalText(b) != nil || got != st {
			t.Errorf("%v 왕복 실패: %q -> %v", st, b, got)
		}
	}
	// 새 상태는 19단계에서 완료 계열·설치완료 집계에 넣지 않는다(20·21단계에서 모드로 구분)
	for _, st := range []Status{StatusAlive, StatusYumDone} {
		if st.IsCompleted() || st.CountsAsDone() {
			t.Errorf("%v 는 아직 완료 계열이 아님", st)
		}
	}
}

func TestYumRecordJSONRoundTrip(t *testing.T) {
	since := time.Date(2026, 11, 17, 9, 0, 0, 0, time.UTC)
	done := since.Add(95 * time.Minute)
	for _, st := range []Status{StatusAlive, StatusYumDone} {
		in := Record{Name: "y1", Vendor: "D", Status: st, Reason: "r", Mode: ModeYum,
			YumKernel: "4.18.0-513.el8.x86_64", YumBoot: "2026-11-10 03:04:05", YumSince: since,
			YumDoneAt: done, YumNow: "4.18.0-553.el8.x86_64"}
		b, err := json.Marshal(in)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(b), `"status":"`+st.String()+`"`) {
			t.Errorf("상태가 한글 이름으로 저장되지 않음: %s", b)
		}
		var out Record
		if err := json.Unmarshal(b, &out); err != nil {
			t.Fatal(err)
		}
		if out.Status != st || out.Mode != ModeYum || !out.IsYum() || out.YumKernel != in.YumKernel ||
			out.YumBoot != in.YumBoot || !out.YumSince.Equal(since) || !out.YumDoneAt.Equal(done) || out.YumNow != in.YumNow {
			t.Errorf("왕복 불일치: %+v", out)
		}
	}
	if (Record{}).IsYum() || (Record{Mode: "x"}).IsYum() {
		t.Error("IsYum 은 Mode==yum 일 때만 참")
	}
}

// OS 서버의 json 에는 새 필드가 하나도 나오지 않아 v0.3.0 의 출력과 같다(zero 시각 포함).
func TestOSRecordJSONHasNoYumKeys(t *testing.T) {
	r := Record{Name: "a", Vendor: "D", Status: StatusDone, FirstInstallingAt: t0, DoneAt: t0}
	for _, marshal := range []func() ([]byte, error){
		func() ([]byte, error) { return json.Marshal(r) },
		func() ([]byte, error) { return json.MarshalIndent(stateFile{Version: 1, Hosts: []Record{r}}, "", " ") },
	} {
		b, err := marshal()
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(b), "yum") || strings.Contains(string(b), `"mode"`) {
			t.Errorf("OS 서버 json 에 yum 필드가 나옴: %s", b)
		}
	}
	// yum 서버라도 기준 시각이 없으면(zero) 시각 키는 빠진다
	b, _ := json.Marshal(Record{Name: "y", Mode: ModeYum})
	if strings.Contains(string(b), "yum_since") || strings.Contains(string(b), "yum_done_at") || !strings.Contains(string(b), `"mode":"yum"`) {
		t.Errorf("yum 서버 json: %s", b)
	}
}

func writeState(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func recOf(c *Collector, h string) Record {
	for _, r := range c.Snapshot().Records {
		if r.Name == h {
			return r
		}
	}
	return Record{}
}

// 모드 필드가 없는 v0.3.0 의 state.json 이 그대로 읽힌다.
func TestOldStateWithoutModeLoads(t *testing.T) {
	path := filepath.Join(t.TempDir(), "h.pm_state.json")
	writeState(t, path, `{
 "version": 1,
 "hosts": [
  {"name":"a","vendor":"D","status":"설치완료","first_installing_at":"2026-11-08T09:00:00Z","done_at":"2026-11-08T10:00:00Z","off_at":"0001-01-01T00:00:00Z","rebooted_at":"0001-01-01T00:00:00Z","reason":"","check":{}},
  {"name":"b","vendor":"D","status":"설치중","first_installing_at":"2026-11-08T09:30:00Z","done_at":"0001-01-01T00:00:00Z","off_at":"0001-01-01T00:00:00Z","rebooted_at":"0001-01-01T00:00:00Z","reason":"OS 설치중","check":{}}
 ]
}
`)
	col, err := NewCollector(testCfg(), []string{"a", "b"}, path, newFake().deps())
	if err != nil {
		t.Fatal(err)
	}
	a, b := recOf(col, "a"), recOf(col, "b")
	if a.Status != StatusDone || !a.DoneAt.Equal(t0.Add(time.Hour)) || a.Mode != "" || a.IsYum() ||
		a.YumKernel != "" || !a.YumSince.IsZero() {
		t.Errorf("a: %+v", a)
	}
	if b.Status != StatusInstalling || b.Reason != "OS 설치중" || b.Mode != "" {
		t.Errorf("b: %+v", b)
	}
	// 같은 모드(OS)로 열었으므로 기록이 지워지지 않는다
	if col.Snapshot().Done() != 1 {
		t.Errorf("Done=%d", col.Snapshot().Done())
	}
}

func TestNewCollectorSetsModeFromOption(t *testing.T) {
	path := filepath.Join(t.TempDir(), "h.pm_state.json")
	col, err := NewCollector(testCfg(), []string{"os1", "y1", "y2"}, path, newFake().deps(),
		WithYum(map[string]bool{"y1": true, "y2": true, "없는서버": true}))
	if err != nil {
		t.Fatal(err)
	}
	if recOf(col, "os1").Mode != "" || recOf(col, "os1").IsYum() {
		t.Errorf("os1: %+v", recOf(col, "os1"))
	}
	for _, h := range []string{"y1", "y2"} {
		if r := recOf(col, h); r.Mode != ModeYum || !r.IsYum() || r.Name != h || r.Status != StatusNone {
			t.Errorf("%s: %+v", h, r)
		}
	}
	// 옵션이 없으면(nil) 모두 OS 설치
	c2, _ := NewCollector(testCfg(), []string{"y1"}, filepath.Join(t.TempDir(), "s.json"), newFake().deps(), WithYum(nil))
	if recOf(c2, "y1").IsYum() {
		t.Error("WithYum(nil) 은 모두 OS 설치")
	}
}

// 모드가 바뀐 서버의 기록은 초기화하고, 모드가 같은 서버와 yum 필드는 유지한다(계획서 4.8).
func TestModeChangeResetsRecord(t *testing.T) {
	path := filepath.Join(t.TempDir(), "h.pm_state.json")
	writeState(t, path, `{
 "version": 1,
 "hosts": [
  {"name":"os_to_yum","vendor":"D","status":"설치완료","first_installing_at":"2026-11-08T09:00:00Z","done_at":"2026-11-08T10:00:00Z","reason":"x","check":{"ldap":"zxcv"}},
  {"name":"os_same","vendor":"D","status":"설치완료","first_installing_at":"2026-11-08T09:00:00Z","done_at":"2026-11-08T10:00:00Z","reason":"","check":{}},
  {"name":"yum_to_os","vendor":"D","mode":"yum","status":"접속가능","reason":"기준","yum_kernel":"4.18.0-513","yum_boot":"2026-11-10 03:04:05","yum_since":"2026-11-17T09:00:00Z","yum_now":"4.18.0-513","check":{}},
  {"name":"yum_same","vendor":"D","mode":"yum","status":"yum_update/접속가능","reason":"4.18.0-513 -> 4.18.0-553","yum_kernel":"4.18.0-513","yum_boot":"2026-11-10 03:04:05","yum_since":"2026-11-17T09:00:00Z","yum_done_at":"2026-11-17T10:30:00Z","yum_now":"4.18.0-553","check":{}}
 ]
}
`)
	hosts := []string{"os_to_yum", "os_same", "yum_to_os", "yum_same"}
	yum := map[string]bool{"os_to_yum": true, "yum_same": true}
	col, err := NewCollector(testCfg(), hosts, path, newFake().deps(), WithYum(yum))
	if err != nil {
		t.Fatal(err)
	}
	// OS -> yum: 초기화(상태·이력·점검 결과 모두), 새 모드만 남는다
	if r := recOf(col, "os_to_yum"); r.Status != StatusNone || !r.DoneAt.IsZero() || !r.FirstInstallingAt.IsZero() ||
		r.Reason != "" || r.Check != (Check{}) || r.Mode != ModeYum || r.InstallDone {
		t.Errorf("os_to_yum: %+v", r)
	}
	// 계획서 4.9: v0.4.0 state(install_done 없음)는 읽을 때 상태로 정한다. 모드가 바뀐 서버는 꺼진다
	if !recOf(col, "os_same").InstallDone || !recOf(col, "yum_same").InstallDone || recOf(col, "yum_to_os").InstallDone {
		t.Errorf("install_done 이행: os_same=%v yum_same=%v yum_to_os=%v",
			recOf(col, "os_same").InstallDone, recOf(col, "yum_same").InstallDone, recOf(col, "yum_to_os").InstallDone)
	}
	// yum -> OS: yum 필드까지 모두 초기화
	if r := recOf(col, "yum_to_os"); r.Status != StatusNone || r.Mode != "" || r.YumKernel != "" || r.YumBoot != "" ||
		!r.YumSince.IsZero() || r.YumNow != "" || r.Reason != "" {
		t.Errorf("yum_to_os: %+v", r)
	}
	// 같은 모드: 그대로(기준 커널·완료 기록 유지)
	if r := recOf(col, "os_same"); r.Status != StatusDone || !r.DoneAt.Equal(t0.Add(time.Hour)) || r.Mode != "" {
		t.Errorf("os_same: %+v", r)
	}
	r := recOf(col, "yum_same")
	if r.Status != StatusYumDone || r.Mode != ModeYum || r.YumKernel != "4.18.0-513" || r.YumBoot != "2026-11-10 03:04:05" ||
		!r.YumSince.Equal(time.Date(2026, 11, 17, 9, 0, 0, 0, time.UTC)) ||
		!r.YumDoneAt.Equal(time.Date(2026, 11, 17, 10, 30, 0, 0, time.UTC)) || r.YumNow != "4.18.0-553" {
		t.Errorf("yum_same: %+v", r)
	}
	// 초기화가 저장되면 다음에 같은 모드로 열어도 그대로다(되살아나지 않음)
	if err := col.save(); err != nil {
		t.Fatal(err)
	}
	col2, err := NewCollector(testCfg(), hosts, path, newFake().deps(), WithYum(yum))
	if err != nil {
		t.Fatal(err)
	}
	if recOf(col2, "os_to_yum").Status != StatusNone || recOf(col2, "yum_same").YumKernel != "4.18.0-513" ||
		recOf(col2, "yum_to_os").YumKernel != "" {
		t.Errorf("재시작 뒤: %+v %+v %+v", recOf(col2, "os_to_yum"), recOf(col2, "yum_same"), recOf(col2, "yum_to_os"))
	}
}

// 새 상태가 있어도 스냅샷 정렬·집계는 번호 순서를 따르고 확인전은 맨 끝이다.
// 화면 머리줄은 OS 서버만 있으면 8·9 항목을 내지 않아 v0.3.0 화면 그대로다.
func TestSnapshotOrderWithYumStatuses(t *testing.T) {
	recs := []Record{
		{Name: "n", Status: StatusNone}, {Name: "y", Status: StatusYumDone, Mode: ModeYum},
		{Name: "a", Status: StatusAlive, Mode: ModeYum}, {Name: "o", Status: StatusOff}, {Name: "i", Status: StatusInstalling},
	}
	s := buildSnapshot(defaultConfig(), recs, 1, false, time.Time{}, "")
	var order []string
	for _, r := range s.Records {
		order = append(order, r.Name)
	}
	if strings.Join(order, ",") != "i,o,a,y,n" {
		t.Errorf("정렬=%v", order)
	}
	if s.Counts[StatusAlive] != 1 || s.Counts[StatusYumDone] != 1 || s.Done() != 0 {
		t.Errorf("집계=%v done=%d", s.Counts, s.Done())
	}
	osOnly := buildSnapshot(defaultConfig(), []Record{{Name: "i", Status: StatusInstalling}}, 1, false, time.Time{}, "")
	v := &ViewState{W: 120, H: 30}
	if h := strings.Join(v.headerLines(*osOnly, 120), "\n"); strings.Contains(h, "접속가능") {
		t.Errorf("OS 전용 머리줄에 yum 상태가 나옴: %s", h)
	}
	if h := strings.Join(v.headerLines(*s, 120), "\n"); !strings.Contains(h, "8 접속가능 1") || !strings.Contains(h, "9 yum_update/접속가능 1") {
		t.Errorf("yum 서버가 있는 머리줄: %s", h)
	}
}
