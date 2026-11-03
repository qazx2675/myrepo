// model_test.go - stage 라벨·그룹 줄 파싱(구버전 호환)·Job JSON 하위 호환·스냅샷 결정성/정체 경계/집계 단위 테스트
package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestStageLabels(t *testing.T) {
	for _, s := range StageOrder {
		if s.Label() == string(s) {
			t.Fatalf("라벨 없음: %s", s)
		}
	}
	if StageStuck.Label() != "정체" || stageLabel(StageInstalling) != "설치중" || Stage("x").Label() != "x" {
		t.Fatal("라벨 이상")
	}
}

func TestParseJobSpecGroups(t *testing.T) {
	in := "user=u1\ntime=100\nh1\nh2 extra\n" +
		"yml=a.yml infra=i1 os=rhel8 boot=uefi splunk=y hosts=h1,h3\n" +
		"yml=b.yml infra= os=rhel7 boot= splunk= hosts=h2\n" +
		"yml=a.yml hosts=h1,h4\n" +
		"all=all.yml\n"
	s, err := parseJobSpec([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	if s.User != "u1" || s.Time != 100 || s.AllYml != "all.yml" || !reflect.DeepEqual(s.Hosts, []string{"h1", "h2", "h3", "h4"}) {
		t.Fatalf("기본 필드 이상: %+v", s)
	}
	a, b := s.Groups["a.yml"], s.Groups["b.yml"]
	if len(s.Groups) != 2 || a == nil || b == nil || !reflect.DeepEqual(a.Hosts, []string{"h1", "h3", "h4"}) ||
		b.OS != "rhel7" || b.Infra != "" || !reflect.DeepEqual(b.Hosts, []string{"h2"}) {
		t.Fatalf("그룹 이상: %+v %+v", a, b)
	}
	// 1차 함수(parseJobFile) 는 그룹 줄을 호스트로 오인하지 않는다
	if _, _, hs, err := parseJobFile([]byte(in)); err != nil || !reflect.DeepEqual(hs, s.Hosts) {
		t.Fatalf("parseJobFile 이상: %v %v", hs, err)
	}
	// 구버전: 그룹 줄 없음 → 단일 "(all)"
	old, err := parseJobSpec([]byte("user=u1\ntime=1\nh2\nh1\n"))
	if err != nil || len(old.Groups) != 1 || !reflect.DeepEqual(old.Groups[allGroup].Hosts, []string{"h2", "h1"}) || old.AllYml != "" {
		t.Fatalf("구버전 이상: %+v %v", old, err)
	}
}

func TestCollectQueueGroupsAndStage(t *testing.T) {
	setupDir(t)
	x := newX(map[string]*simHost{"h1": oldOS(), "h2": oldOS()})
	id := submitBody(t, 0, strings.Replace(groupJobBody, "%d", "1759386600", 1))
	drive(t, x.world, newXDaemon(x, false, false), 0, 0)
	j := readJobFile(t, jobFile(id))
	if j.AllYml != "all.yml" || len(j.Groups) != 2 || j.Groups["a.yml"].Infra != "i1" || j.Hosts["h1"].Stage != string(StageQueued) || j.Hosts["h1"].StageAt != base {
		t.Fatalf("수거 job 이상: %+v", j)
	}
}

// 1차(v0.1.0) JSON 은 그대로 읽히고, 다시 저장해도 2차 키가 생기지 않는다(omitempty)
func TestJobJSONBackwardCompat(t *testing.T) {
	setupDir(t)
	v1 := `{"id":"a-u","user":"u","submitted":1,"first_ready":0,"late_first_ready":0,"first_run_done":false,` +
		`"hosts":{"h":{"ip":"1.2.3.4","route":"local","seen_down":true,"ready_at":0,"processed":"","miss":0,"fails":0}},` +
		`"runs":[{"code":"1234","hosts":["h"],"at":5}]}`
	p := filepath.Join(jobsDir(), "a-u.json")
	if err := os.WriteFile(p, []byte(v1), 0644); err != nil {
		t.Fatal(err)
	}
	j, err := loadJobFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := saveJob(j); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(p)
	for _, k := range []string{"stage", "groups", "all_yml", "ldap", "second", "done_src", "down_at", "boot_at", "manual", "yml"} {
		if bytes.Contains(b, []byte(`"`+k+`"`)) {
			t.Fatalf("2차 키 %q 가 생김:\n%s", k, b)
		}
	}
	var a, c map[string]interface{}
	_ = json.Unmarshal([]byte(v1), &a)
	_ = json.Unmarshal(b, &c)
	if !reflect.DeepEqual(a, c) {
		t.Fatalf("왕복 내용 변경:\n%s", b)
	}
	if s := BuildSnapshot(dataDir(), time.Unix(100, 0)); len(s.Jobs) != 1 || s.Jobs[0].Groups[0].Yml != allGroup || s.Jobs[0].Groups[0].Hosts[0].Stage != StageDeploying {
		t.Fatalf("구버전 스냅샷 이상: %+v", s.Jobs)
	}
}

func snapFixture(t *testing.T, dir string) {
	t.Helper()
	t.Setenv("AUTO_SETUP_DIR", dir)
	if err := ensureDirs(); err != nil {
		t.Fatal(err)
	}
	j1 := &Job{ID: "20251002-153000-u1", User: "u1", Submitted: 1000, AllYml: "all.yml", Runs: []Run{{Code: "0001", Hosts: []string{"a1"}, At: 1500}},
		Groups: map[string]*Group{
			"b.yml": {Infra: "i2", OS: "rhel7", Hosts: []string{"b1", "b2"}},
			"a.yml": {Infra: "i1", OS: "rhel8", Boot: "uefi", Splunk: "y", Hosts: []string{"a1", "gone"}},
		},
		Hosts: map[string]*Host{
			"a1": {IP: "x", Route: "local", Processed: "0001", DoneSrc: DoneSrcRun, Stage: string(StageDone), StageAt: 1600,
				Ldap: &LdapState{Backup: LdapBackupOK, Bindpw: BindpwDiff, Applied: true}},
			"b1": {IP: "x", Route: "os6", SeenDown: true, DownAt: 1100, Stage: string(StageInstalling), StageAt: 1200},
			"b2": {IP: "", Stage: string(StageQueued), StageAt: 1000},
			"c1": {IP: "x", Route: "local", Processed: DoneSrcExternal, DoneSrc: DoneSrcExternal},
		}}
	j2 := &Job{ID: "20251002-150000-u2", User: "u2", Submitted: 900, Hosts: map[string]*Host{"z1": {Fails: 3}}, Runs: []Run{}}
	for _, j := range []*Job{j1, j2} {
		if _, err := saveJob(j); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(reqActiveDir(), 0755); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(reqActiveDir(), "1700_manual-run.req"), []byte("jobid=20251002-153000-u1\nyml=a.yml\n"), 0644)
}

// 같은 입력 → 같은 JSON 바이트 (다른 디렉터리 복사본에서도), 정렬·집계·정체 경계
func TestSnapshotDeterministic(t *testing.T) {
	d1, d2 := t.TempDir(), t.TempDir()
	snapFixture(t, d2)
	snapFixture(t, d1)
	now := time.Unix(1100+3599, 0)
	b1, _ := snapshotJSON(BuildSnapshot(d1, now))
	b1b, _ := snapshotJSON(BuildSnapshot(d1, now))
	b2, _ := snapshotJSON(BuildSnapshot(d2, now))
	if !bytes.Equal(b1, b1b) || !bytes.Equal(b1, b2) {
		t.Fatalf("스냅샷이 결정적이지 않음\n%s\n---\n%s", b1, b2)
	}
	s := BuildSnapshot(d1, now)
	if len(s.Jobs) != 2 || s.Jobs[0].ID != "20251002-150000-u2" || s.Jobs[1].ID != "20251002-153000-u1" {
		t.Fatalf("job 순서 이상: %+v", s.Jobs)
	}
	j := s.Jobs[1]
	var names []string
	for _, g := range j.Groups {
		names = append(names, g.Yml)
	}
	if !reflect.DeepEqual(names, []string{"a.yml", "b.yml", etcGroup}) {
		t.Fatalf("그룹 순서 이상: %v", names)
	}
	a, b := j.Groups[0], j.Groups[1]
	if len(a.Hosts) != 1 || !a.Complete || a.Hosts[0].Note != "code 0001, LDAP 복원" || a.Hosts[0].Ldap == nil {
		t.Fatalf("a.yml 이상: %+v", a)
	}
	if b.Complete || b.Hosts[0].Stage != StageInstalling || b.Hosts[0].Note != "os6경유" || b.Hosts[1].Note != "이름 미해결" || b.MaxElapsed != now.Unix()-1000 {
		t.Fatalf("b.yml 이상: %+v", b)
	}
	if j.Counts.Total != 4 || j.Counts.Done != 2 || j.Counts.Installing != 1 || j.Counts.Queued != 1 || s.Totals.Failed != 1 || s.Totals.Total != 5 {
		t.Fatalf("집계 이상: %+v / %+v", j.Counts, s.Totals)
	}
	if len(j.Manual) != 1 || j.Manual[0].State != "queued" || j.Manual[0].Requested != 1700 || j.Manual[0].Yml != "a.yml" {
		t.Fatalf("수동 요청 이상: %+v", j.Manual)
	}
	// 정체 경계: down_at=1100 → +3601 에서 정체
	s2 := BuildSnapshot(d1, time.Unix(1100+3601, 0))
	if h := s2.Jobs[1].Groups[1].Hosts[0]; h.Stage != StageStuck || h.Label != "정체" || s2.Totals.Stuck != 1 {
		t.Fatalf("정체 경계 이상: %+v", h)
	}
	if bytes.Contains(b1, []byte("bindpw\": \"secret")) {
		t.Fatal("bindpw 값 노출")
	}
}
