package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func writeTestJob(t *testing.T) string {
	t.Helper()
	id := "20261007-100000-u1"
	j := &Job{ID: id, User: "u1", Submitted: base,
		Hosts: map[string]*Host{
			"a1": {IP: "x", Processed: "0001"}, "a2": {IP: "x"}, "b1": {IP: "x"}, "b2": {IP: "x"},
		},
		Groups: map[string]*Group{"a.yml": {Hosts: []string{"a1", "a2"}}, "b.yml": {Hosts: []string{"b1", "b2"}}}}
	if _, err := writeJobFile(jobFile(id), j); err != nil {
		t.Fatal(err)
	}
	return id
}

// done --job: 미완료 호스트 전부 (완료된 a1 제외), --yml 이면 그 그룹만
func TestJobPendingHosts(t *testing.T) {
	setupDir(t)
	id := writeTestJob(t)
	got, err := jobPendingHosts([]string{id})
	if err != nil || !reflect.DeepEqual(got, []string{"a2", "b1", "b2"}) {
		t.Fatalf("job 전체: %v %v", got, err)
	}
	got, err = jobPendingHosts([]string{id, "--yml", "b.yml"})
	if err != nil || !reflect.DeepEqual(got, []string{"b1", "b2"}) {
		t.Fatalf("그룹 b.yml: %v %v", got, err)
	}
	for name, a := range map[string][]string{
		"인자 없음":    {},
		"없는 job":   {"no-such"},
		"없는 그룹":    {id, "--yml", "zz.yml"},
		"잘못된 옵션":   {id, "--group", "a.yml"},
		"경로 문자":    {"../x"},
		"인자 개수 초과": {id, "--yml", "a.yml", "x"},
	} {
		if h, err := jobPendingHosts(a); err == nil {
			t.Errorf("%s: 오류여야 함: %v", name, h)
		}
	}
	if _, err := jobPendingHosts([]string{id, "--yml", "a.yml"}); err != nil { // a2 남음
		t.Fatalf("a.yml: %v", err)
	}
}

func TestJobPendingHostsAllDone(t *testing.T) {
	setupDir(t)
	j := &Job{ID: "j1", Hosts: map[string]*Host{"h": {Processed: "0001"}}}
	if _, err := writeJobFile(jobFile("j1"), j); err != nil {
		t.Fatal(err)
	}
	if _, err := jobPendingHosts([]string{"j1"}); err == nil || !strings.Contains(err.Error(), "미완료 호스트가 없습니다") {
		t.Fatalf("모두 완료: %v", err)
	}
}

// cmdDone --job: done/<호스트> 기록이 미완료 호스트에만 생김 (job 파일은 변하지 않음)
func TestCmdDoneByJob(t *testing.T) {
	setupDir(t)
	id := writeTestJob(t)
	before, _ := os.ReadFile(jobFile(id))
	if rc := cmdDone([]string{"--job", id, "--yml", "b.yml"}); rc != 0 {
		t.Fatalf("rc=%d", rc)
	}
	for _, h := range []string{"b1", "b2"} {
		if b, err := os.ReadFile(filepath.Join(doneRecDir(), h)); err != nil || !strings.Contains(string(b), " manual") {
			t.Fatalf("%s 기록: %q %v", h, b, err)
		}
	}
	for _, h := range []string{"a1", "a2"} {
		if _, err := os.Stat(filepath.Join(doneRecDir(), h)); !os.IsNotExist(err) {
			t.Fatalf("%s 는 기록되면 안 됨", h)
		}
	}
	if after, _ := os.ReadFile(jobFile(id)); string(after) != string(before) {
		t.Fatal("job 파일이 바뀜")
	}
	if rc := cmdDone([]string{"--job", "no-such"}); rc != 1 {
		t.Fatalf("없는 job rc=%d", rc)
	}
	if !relayArgsOK([]string{"done", "--job", id}) {
		t.Fatal("원격 중계 인자 검증")
	}
}

// 전체 보기(a): 모든 그룹 호스트를 한 표로, 그룹(yml) 열 표시, 카운트 합산
func TestMergedGroup(t *testing.T) {
	s := twoGroupSnap()
	_, g := findGroup(s, "J1", allView)
	if g == nil || len(g.Hosts) != 4 || g.Counts.Total != 4 || g.Counts.Done != 3 || g.Complete {
		t.Fatalf("합친 그룹: %+v", g)
	}
	if g.Hosts[0].Yml != "a.yml" || g.Hosts[3].Yml != "b.yml" {
		t.Fatalf("yml 채움: %+v", g.Hosts)
	}
	if _, g := findGroup(s, "J1", "a.yml"); g.Hosts[0].Yml != "" {
		t.Fatal("그룹별 화면의 호스트에는 yml 열이 없어야 함")
	}
	d := renderDetail(s, selection{Detail: true, JobID: "J1", Yml: allView}, 120, 24, false)
	for _, want := range []string{"전체 호스트 (그룹 2개)", "그룹(yml)", "a1", "a.yml", "b2", "b.yml", "a 그룹별 보기"} {
		if !strings.Contains(d, want) {
			t.Errorf("전체 보기에 %q 없음:\n%s", want, d)
		}
	}
	if strings.Contains(d, "[c] OS 체크 수동 실행(이중체크) - 미완료") {
		t.Error("전체 보기에서는 수동 실행 활성 안내가 없어야 함")
	}
	if _, g := findGroup(s, "J1", "no.yml"); g != nil {
		t.Error("없는 그룹")
	}
}

func TestTUIAllViewKey(t *testing.T) {
	src := &fakeSrc{snap: twoGroupSnap(), local: true}
	r := runKeys(t, src, "ac") // 전체 보기 → c (수동 실행 불가 안내, 요청 없음)
	o := r.out.String()
	if !strings.Contains(o, "전체 호스트 (그룹 2개)") || !strings.Contains(o, "전체 보기에서는 수동 실행을 할 수 없습니다") {
		t.Errorf("전체 보기/안내 없음")
	}
	if len(src.reqs) != 0 {
		t.Fatalf("전체 보기에서 요청됨: %+v", src.reqs)
	}
	assertRestored(t, r)
	// 그룹 화면에서 a → 전체 보기, 다시 a → 화면 1
	r2 := runKeys(t, &fakeSrc{snap: twoGroupSnap(), local: true}, "\raa")
	if !strings.Contains(lastFrame(r2), "Enter 상세") {
		t.Errorf("a 두 번이면 화면 1 로 돌아가야 함:\n%s", lastFrame(r2))
	}
}
