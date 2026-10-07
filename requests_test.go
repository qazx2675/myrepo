// requests_test.go - 요청 채널 단위 테스트: writeRequest 원자·이름, manual-run 수락/거부(완료 전·중복·알 수 없는 종류), 종료된 job 수동 run, cancel
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const groupJobBody = "user=u1\ntime=%d\nh1\nh2\n" +
	"yml=a.yml infra=i1 os=rhel8 boot=uefi splunk=y hosts=h1\n" +
	"yml=b.yml infra=i2 os=rhel7 boot=legacy splunk=n hosts=h2\n" +
	"all=all.yml\n"

func submitBody(t *testing.T, o int64, body string) string {
	t.Helper()
	p := filepath.Join(queueDir(), fmt.Sprintf("%d_u1_1.job", base+o))
	if err := os.WriteFile(p, []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
	return jobIDAt("u1", o)
}

func req(t *testing.T, kind string, pl map[string]string) string {
	t.Helper()
	p, err := writeRequest(kind, pl)
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Base(p)
}

func rejectedReason(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(reqRejectedDir(), name))
	if err != nil {
		t.Fatalf("rejected/%s 없음", name)
	}
	return parseRequest(b)["reason"]
}

func TestWriteRequest(t *testing.T) {
	setupDir(t)
	a := req(t, ReqManualRun, map[string]string{"yml": "a.yml", "jobid": "j1"})
	b := req(t, ReqManualRun, map[string]string{"yml": "a.yml", "jobid": "j1"})
	if a == b || !reqFileRe.MatchString(a) || !reqFileRe.MatchString(b) || !strings.HasSuffix(a, "_manual-run.req") {
		t.Fatalf("이름 이상: %s %s", a, b)
	}
	got, _ := os.ReadFile(filepath.Join(requestsDir(), a))
	if string(got) != "jobid=j1\nyml=a.yml\n" {
		t.Fatalf("내용 이상(키 정렬): %q", got)
	}
	ents, _ := os.ReadDir(requestsDir())
	for _, e := range ents {
		if strings.HasPrefix(e.Name(), ".tmp") {
			t.Fatalf("임시파일 잔존: %s", e.Name())
		}
	}
	if _, err := writeRequest("Bad_Kind", nil); err == nil {
		t.Fatal("잘못된 kind 는 에러")
	}
	if _, err := writeRequest(ReqCancel, map[string]string{"jobid": "a\nb"}); err == nil {
		t.Fatal("줄바꿈 값은 에러")
	}
}

// manual-run: 완료 여부와 상관없이 수락·수동 run(그룹 호스트만, runs 에 manual 기록, wall), 미완료 그룹(b.yml)은 접속불가로 알림, 중복·알 수 없는 종류 거부
func TestManualRunRequest(t *testing.T) {
	setupDir(t)
	x := newX(map[string]*simHost{"h1": install(60, 125, 400, 600), "h2": oldOS()})
	id := submitBody(t, 0, fmt.Sprintf(groupJobBody, base))
	d := drive(t, x.world, newXDaemon(x, false, false), 0, 95)
	d = drive(t, x.world, d, 100, 1025) // 1차 run 1020 → h1 처리
	ok1 := req(t, ReqManualRun, map[string]string{"jobid": id, "yml": "a.yml"})
	dup := req(t, ReqManualRun, map[string]string{"jobid": id, "yml": "a.yml"})
	other := req(t, ReqManualRun, map[string]string{"jobid": id, "yml": "b.yml"})
	unk := req(t, "reboot", map[string]string{"jobid": id})
	d = drive(t, x.world, d, 1030, 1070)
	if len(x.runs) != 3 || !reflect.DeepEqual(x.runs[1].Hosts, []string{"h1"}) || !reflect.DeepEqual(x.runs[2].Hosts, []string{"h2"}) {
		t.Fatalf("run 호출 이상 (h2 미완료 그룹도 수동 run 되어야 함): %+v", x.runs)
	}
	if r := rejectedReason(t, dup); !strings.Contains(r, "이미 대기") {
		t.Fatalf("중복 거부 이상: %q", r)
	}
	if _, err := os.Stat(filepath.Join(reqRejectedDir(), other)); !os.IsNotExist(err) {
		t.Fatal("b.yml(미완료 그룹) 수동 run 이 거부됨")
	}
	if r := rejectedReason(t, unk); r != "알 수 없는 요청 종류" {
		t.Fatalf("알 수 없는 종류 거부 이상: %q", r)
	}
	if _, err := os.Stat(filepath.Join(reqActiveDir(), ok1)); !os.IsNotExist(err) {
		t.Fatal("끝난 수동 run 요청이 active 에 남음")
	}
	j := d.jobs[id]
	want := Run{Code: "0002", Hosts: []string{"h1"}, At: base + 1030, Manual: true, Yml: "a.yml"}
	if len(j.Runs) != 3 || !reflect.DeepEqual(j.Runs[1], want) || j.Hosts["h1"].Processed != "0001" || j.Hosts["h2"].Processed != "" {
		t.Fatalf("runs/processed 이상: %+v %+v", j.Runs, j.Hosts["h1"])
	}
	if len(x.walls) != 3 || !strings.Contains(x.walls[1], "\nh1 (1대)\ncode : 0002") || strings.Contains(x.walls[1], "접속불가") {
		t.Fatalf("수동 run wall 이상: %q", x.walls)
	}
	if !strings.Contains(x.walls[2], "\n(0대)\ncode : 0003") || !strings.HasSuffix(x.walls[2], "[!] 접속불가·미응답 1대: h2\n") {
		t.Fatalf("접속불가 알림 이상: %q", x.walls[2])
	}
	if ents, _ := os.ReadDir(requestsDir()); len(ents) != 2 { // active/, rejected/ 만
		t.Fatalf("requests/ 에 처리 안 된 파일: %v", ents)
	}
}

// 종료된(jobs/done) job 의 수동 run: 수락 → run → jobs/done/<id>.json 에 manual run 기록, 스냅샷에 closed·complete
func TestManualRunClosedJob(t *testing.T) {
	setupDir(t)
	x := newX(map[string]*simHost{"h1": install(60, 125, 400, 600)})
	id := submit(t, "u1", 0, "h1")
	d := drive(t, x.world, newXDaemon(x, false, false), 0, 610)
	doneJob(t, id)
	s := BuildSnapshot(dataDir(), x.clock.Now())
	if len(s.Jobs) != 1 || !s.Jobs[0].Closed || len(s.Jobs[0].Groups) != 1 || s.Jobs[0].Groups[0].Yml != allGroup || !s.Jobs[0].Groups[0].Complete {
		t.Fatalf("종료 job 스냅샷 이상: %+v", s.Jobs)
	}
	name := req(t, ReqManualRun, map[string]string{"jobid": id, "yml": allGroup})
	d = drive(t, x.world, d, 615, 615)
	if s := BuildSnapshot(dataDir(), x.clock.Now()); len(s.Jobs[0].Manual) != 1 || s.Jobs[0].Manual[0].State != "running" || s.Jobs[0].Manual[0].File != name {
		t.Fatalf("진행 중 수동 run 스냅샷 이상: %+v", s.Jobs[0].Manual)
	}
	drive(t, x.world, d, 620, 625)
	wantRuns(t, x.world, []runCall{{At: 600, Hosts: []string{"h1"}}, {At: 615, Hosts: []string{"h1"}}})
	j := doneJob(t, id)
	if len(j.Runs) != 2 || !j.Runs[1].Manual || j.Runs[1].Code != "0002" {
		t.Fatalf("종료 job runs 이상: %+v", j.Runs)
	}
}

func TestCancelRequest(t *testing.T) {
	setupDir(t)
	x := newX(map[string]*simHost{"h1": oldOS()})
	id := submit(t, "u1", 0, "h1")
	d := drive(t, x.world, newXDaemon(x, false, false), 0, 5)
	bad := req(t, ReqCancel, map[string]string{"jobid": "nope"})
	req(t, ReqCancel, map[string]string{"jobid": id})
	d = drive(t, x.world, d, 10, 15)
	if len(d.jobs) != 0 {
		t.Fatalf("cancel 후 메모리에 남음: %v", d.jobs)
	}
	doneJob(t, id)
	if r := rejectedReason(t, bad); !strings.Contains(r, "job 없음") {
		t.Fatalf("없는 job cancel 거부 이상: %q", r)
	}
}
