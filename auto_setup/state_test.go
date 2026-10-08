// state_test.go - state.go 단위 테스트: 저장/복원 왕복, .job 파싱, 원자 저장, code 조회, queue 수거
package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func setupDir(t *testing.T) {
	t.Helper()
	t.Setenv("AUTO_SETUP_DIR", t.TempDir())
	if err := ensureDirs(); err != nil {
		t.Fatal(err)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	setupDir(t)
	j := &Job{ID: "20261002-153000-user1", User: "user1", Submitted: 1759386600,
		Hosts: map[string]*Host{"host01": {IP: "10.0.0.1", Route: "local", SeenDown: true, Miss: 1}},
		Runs:  []Run{{Code: "4821", Hosts: []string{"host01"}, At: 1759387000}}}
	if ok, err := saveJob(j); err != nil || !ok {
		t.Fatalf("save: %v %v", ok, err)
	}
	jobs, err := loadJobs()
	if err != nil || len(jobs) != 1 {
		t.Fatalf("load: %v %d", err, len(jobs))
	}
	if !reflect.DeepEqual(jobs[0], j) {
		t.Fatalf("왕복 불일치: %+v vs %+v", jobs[0], j)
	}
}

func TestSaveAtomicAndOnlyOnChange(t *testing.T) {
	setupDir(t)
	j := &Job{ID: "a-u", User: "u", Hosts: map[string]*Host{"h": {}}}
	if ok, _ := saveJob(j); !ok {
		t.Fatal("첫 저장은 true 여야 함")
	}
	if ok, _ := saveJob(j); ok {
		t.Fatal("변화 없으면 false 여야 함")
	}
	j.Hosts["h"].SeenDown = true
	if ok, _ := saveJob(j); !ok {
		t.Fatal("변화 시 true 여야 함")
	}
	ents, _ := os.ReadDir(jobsDir())
	for _, e := range ents {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Fatalf("tmp 파일 잔존: %s", e.Name())
		}
	}
}

func TestParseJobFile(t *testing.T) {
	in := "user=user1\ntime=1759386600\nhost01\n  host02 extra\n\nhost01\n"
	u, tm, hs, err := parseJobFile([]byte(in))
	if err != nil || u != "user1" || tm != 1759386600 || !reflect.DeepEqual(hs, []string{"host01", "host02"}) {
		t.Fatalf("파싱 결과 이상: %q %d %v %v", u, tm, hs, err)
	}
	if _, _, _, err := parseJobFile([]byte("time=1\nhost01\n")); err == nil {
		t.Fatal("user 없으면 에러여야 함")
	}
	if _, _, _, err := parseJobFile([]byte("user=u\n")); err == nil {
		t.Fatal("호스트 없으면 에러여야 함")
	}
}

func TestCodeLookup(t *testing.T) {
	setupDir(t)
	if _, err := readCode("0000"); err == nil {
		t.Fatal("없는 code 는 에러여야 함")
	}
	if _, err := readCode("../x"); err == nil {
		t.Fatal("형식 오류는 에러여야 함")
	}
	if err := os.WriteFile(codePath("4821"), []byte("hello\n"), 0644); err != nil {
		t.Fatal(err)
	}
	b, err := readCode("4821")
	if err != nil || string(b) != "hello\n" {
		t.Fatalf("code 조회 실패: %q %v", b, err)
	}
	c, err := newCode()
	if err != nil || !validCode(c) || c == "4821" {
		t.Fatalf("newCode 이상: %q %v", c, err)
	}
}

func TestCollectQueue(t *testing.T) {
	setupDir(t)
	d := &Daemon{Clock: realClock{}}
	q := filepath.Join(queueDir(), "1759386600_user1_123.job")
	if err := os.WriteFile(q, []byte("user=user1\ntime=1759386600\nhost01\nhost02\n"), 0644); err != nil {
		t.Fatal(err)
	}
	n, err := d.collectQueue()
	if err != nil || n != 1 {
		t.Fatalf("수거 이상: %d %v", n, err)
	}
	if _, err := os.Stat(q); !os.IsNotExist(err) {
		t.Fatal(".job 이 삭제되지 않음")
	}
	jobs, _ := loadJobs()
	if len(jobs) != 1 || jobs[0].User != "user1" || len(jobs[0].Hosts) != 2 ||
		jobs[0].ID != time.Unix(1759386600, 0).Format("20060102-150405")+"-user1" {
		t.Fatalf("job 이상: %+v", jobs)
	}
	if err := cancelJob(jobs[0].ID); err != nil {
		t.Fatal(err)
	}
	if jobs, _ = loadJobs(); len(jobs) != 0 {
		t.Fatal("cancel 후 jobs 에 남아 있음")
	}
}
