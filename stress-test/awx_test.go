package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// fakeAWX 는 launch/poll/invsync·템플릿 인벤토리 변경에 필요한 엔드포인트만 흉내 낸다.
// 인벤토리: 1=DEFAULT, 2=admin(username), 9=kim(다른 사용자)
type fakeAWX struct {
	mu       sync.Mutex
	calls    map[string]int
	patch    map[string]interface{}
	launches map[string]map[string]interface{}

	tmplInv     map[int]int // 템플릿 → 현재 인벤토리
	busyGets    int         // 남은 횟수만큼 템플릿 GET 이 kim(9) 인벤토리를 보여 준다
	stealPatch  bool        // PATCH 직후 다른 사람이 kim 으로 바꾼 것처럼
	failLaunch  bool        // launch 가 500
	failRestore bool        // DEFAULT 로 되돌리는 PATCH 가 500
	failCPUJob  bool        // cpu 템플릿(6) job 을 failed(32)로
	events      []string    // "patch 5→2", "launch 5" 순서 기록

	srcPath   string   // 소스 11 의 source_path (-yaml-state 시험)
	hosts     []string // 인벤토리 7 의 호스트 이름 (nil 이면 repoDir 의 가장 최근 yaml 4번째 필드 = 실제 등록처럼)
	repoDir   string
	hostPages bool // true 면 호스트 목록을 2쪽(next)으로 나눠 준다
	failSrc   bool // 소스 11 조회가 500
}

var fakeInvNames = map[int]string{1: "DEFAULT", 2: "admin", 9: "kim"}

func (f *fakeAWX) tmplJSON(id int, name string) map[string]interface{} {
	inv := f.tmplInv[id]
	if f.busyGets > 0 {
		f.busyGets--
		inv = 9
	}
	return map[string]interface{}{"id": id, "name": name, "inventory": inv,
		"summary_fields": map[string]interface{}{"inventory": map[string]interface{}{"id": inv, "name": fakeInvNames[inv]}}}
}

func (f *fakeAWX) handler(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := r.Method + " " + r.URL.Path
	f.calls[key]++
	n := f.calls[key]
	body, _ := io.ReadAll(r.Body)
	if u, p, ok := r.BasicAuth(); !ok || u != "admin" || p != "pw" {
		w.WriteHeader(401)
		return
	}
	write := func(v interface{}) { json.NewEncoder(w).Encode(v) }
	status := func(final string) string {
		if n == 1 {
			return "running"
		}
		return final
	}
	tmplNames := map[string]string{"5": "stress-ng", "6": "cpu-test"}
	if p := strings.Split(strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/v2/job_templates/"), "/"), "/"); strings.HasPrefix(r.URL.Path, "/api/v2/job_templates/") && len(p) == 1 && tmplNames[p[0]] != "" {
		id := 5
		if p[0] == "6" {
			id = 6
		}
		switch r.Method {
		case http.MethodGet:
			write(f.tmplJSON(id, tmplNames[p[0]]))
		case http.MethodPatch:
			var b struct {
				Inventory int `json:"inventory"`
			}
			json.Unmarshal(body, &b)
			f.events = append(f.events, fmt.Sprintf("patch %d→%d", id, b.Inventory))
			if b.Inventory == 1 && f.failRestore {
				w.WriteHeader(500)
				return
			}
			f.tmplInv[id] = b.Inventory
			if f.stealPatch {
				f.tmplInv[id] = 9
			}
			write(map[string]interface{}{"id": id})
		}
		return
	}
	switch key {
	case "GET /api/v2/inventories/":
		name := r.URL.Query().Get("name")
		var res []map[string]interface{}
		for id, n := range fakeInvNames {
			if name == "" || n == name {
				res = append(res, map[string]interface{}{"id": id, "name": n})
			}
		}
		write(map[string]interface{}{"count": len(res), "results": res})
	case "GET /api/v2/job_templates/5/survey_spec/":
		write(map[string]interface{}{"spec": []map[string]interface{}{
			{"variable": "end_year", "type": "integer"}, {"variable": "end_month", "type": "integer"},
			{"variable": "end_day", "type": "text"}, {"variable": "end_hour", "type": "float"}}})
	case "GET /api/v2/inventories/7/inventory_sources/":
		write(map[string]interface{}{"results": []map[string]interface{}{{"id": 11, "name": "src"}, {"id": 12, "name": "src2"}}})
	case "GET /api/v2/inventory_sources/11/":
		if f.failSrc {
			w.WriteHeader(500)
			return
		}
		write(map[string]interface{}{"id": 11, "inventory": 7, "source": "scm", "source_path": f.srcPath})
	case "GET /api/v2/inventories/7/hosts/":
		var res []map[string]interface{}
		hosts := f.hosts
		if hosts == nil && f.repoDir != "" {
			hosts = lastYamlHosts(f.repoDir)
		}
		for _, h := range hosts {
			res = append(res, map[string]interface{}{"name": h})
		}
		if f.hostPages && len(res) > 1 {
			if r.URL.Query().Get("page") == "2" {
				write(map[string]interface{}{"next": nil, "results": res[1:]})
			} else {
				write(map[string]interface{}{"next": "/api/v2/inventories/7/hosts/?page=2&page_size=200", "results": res[:1]})
			}
			return
		}
		write(map[string]interface{}{"next": nil, "results": res})
	case "PATCH /api/v2/inventory_sources/11/":
		json.Unmarshal(body, &f.patch)
		write(map[string]interface{}{"id": 11})
	case "POST /api/v2/inventory_sources/11/update/":
		write(map[string]interface{}{"inventory_update": 21})
	case "GET /api/v2/inventory_updates/21/":
		write(map[string]interface{}{"id": 21, "status": status("successful")})
	case "GET /api/v2/job_templates/":
		write(map[string]interface{}{"results": []map[string]interface{}{{"id": 5, "name": "stress-ng"}, {"id": 6, "name": "cpu-test"}}})
	case "POST /api/v2/job_templates/5/launch/", "POST /api/v2/job_templates/6/launch/":
		f.events = append(f.events, "launch "+strings.Split(r.URL.Path, "/")[4])
		if f.failLaunch {
			w.WriteHeader(500)
			io.WriteString(w, "launch 거부")
			return
		}
		var m map[string]map[string]interface{}
		json.Unmarshal(body, &m)
		f.launches[r.URL.Path] = m["extra_vars"]
		if !(strings.Contains(r.URL.Path, "/6/") && f.failCPUJob) {
			write(map[string]interface{}{"job": 31})
		} else {
			write(map[string]interface{}{"job": 32})
		}
	case "GET /api/v2/jobs/31/":
		write(map[string]interface{}{"id": 31, "status": status("successful")})
	case "GET /api/v2/jobs/32/":
		write(map[string]interface{}{"id": 32, "status": status("failed")})
	case "GET /api/v2/jobs/32/stdout/":
		io.WriteString(w, "line1\nfatal: 실패\n")
	default:
		w.WriteHeader(404)
		io.WriteString(w, "not found: "+key)
	}
}

func writeScript(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("#!/bin/bash\n"+body), 0o755); err != nil {
		t.Fatal(err)
	}
}

// lastYamlHosts 는 dir 에서 가장 최근에 바뀐 *.yaml 의 줄마다 4번째 필드(hostname)를 돌려준다.
func lastYamlHosts(dir string) []string {
	m, _ := filepath.Glob(filepath.Join(dir, "*.yaml"))
	var best string
	var bt time.Time
	for _, p := range m {
		if fi, err := os.Stat(p); err == nil && !fi.ModTime().Before(bt) {
			best, bt = p, fi.ModTime()
		}
	}
	if best == "" {
		return nil
	}
	b, _ := os.ReadFile(best)
	var out []string
	for _, l := range strings.Split(string(b), "\n") {
		if f := strings.Fields(l); len(f) >= 4 {
			out = append(out, f[3])
		}
	}
	return out
}

// newTestApp 은 가짜 AWX·가짜 ssh/scp 로 등록·launch 를 시험할 app 을 만든다.
func newTestApp(t *testing.T) (*app, *fakeAWX, string) {
	f := &fakeAWX{calls: map[string]int{}, launches: map[string]map[string]interface{}{}, tmplInv: map[int]int{5: 1, 6: 1}}
	srv := httptest.NewServer(http.HandlerFunc(f.handler))
	t.Cleanup(srv.Close)

	dir := t.TempDir()
	repo, svr, bin := filepath.Join(dir, "repo"), filepath.Join(dir, "svr"), filepath.Join(dir, "bin")
	for _, d := range []string{repo, svr, bin} {
		os.MkdirAll(d, 0o755)
	}
	os.WriteFile(filepath.Join(svr, "old_1ea.yml"), nil, 0o644)
	f.repoDir = repo
	t.Setenv("FAKE_REPO", repo)
	t.Setenv("FAKE_SVR", svr)
	// scp -o BatchMode=yes <local> <host>:<dir>/
	writeScript(t, filepath.Join(bin, "scp"), `cp "$3" "$FAKE_REPO/"`+"\n")
	// ssh -o BatchMode=yes <host> "bash <dir>/custom_inventory.sh <dir>/<yaml>"
	writeScript(t, filepath.Join(bin, "ssh"), `f=$(basename "${4##* }"); n=$(wc -l < "$FAKE_REPO/$f"); touch "$FAKE_SVR/${f%.yaml}_${n}ea.yml"`+"\n")
	oldSSH, oldSCP, oldUnit := sshBin, scpBin, pollUnit
	sshBin, scpBin, pollUnit = filepath.Join(bin, "ssh"), filepath.Join(bin, "scp"), time.Millisecond
	t.Cleanup(func() { sshBin, scpBin, pollUnit = oldSSH, oldSCP, oldUnit })

	cfg, err := parseConfig("", "test.conf")
	if err != nil {
		t.Fatal(err)
	}
	cfg.Repohost, cfg.SvrDir, cfg.Inventory = "repohost", svr, "7"
	cfg.HistoryFile = filepath.Join(dir, "history.log")
	cfg.Username = "admin"
	cfg.Stress = TemplateConf{Template: "stress-ng", HostnameKey: "target_hosts", ExtraVars: "duration=60, mode=all"}
	cfg.CPU = TemplateConf{Template: "6", HostnameKey: "hosts"}
	a := &app{
		cfg:     cfg,
		client:  newAWXClient(srv.URL, "admin", "pw", false, 5*time.Second),
		user:    "hong",
		workDir: dir,
		out:     io.Discard,
		in:      bufio.NewReader(strings.NewReader("")),
		end:     &endTime{Vals: [5]string{"2099", "07", "08", "09", "05"}, T: time.Date(2099, 7, 8, 9, 5, 0, 0, time.Local)},
	}
	if err := a.prepareAWX([]string{stageCPU, stageStress}); err != nil {
		t.Fatal(err)
	}
	if a.userInvID != 2 || a.defInvID != 1 || a.tmpl[stageStress].ID != 5 || a.tmpl[stageCPU].ID != 6 {
		t.Fatalf("prepareAWX: user=%d def=%d tmpl=%v", a.userInvID, a.defInvID, a.tmpl)
	}
	return a, f, repo
}

func twoTargets() []*target {
	return []*target{
		{Host: "n1", Fields: strings.Fields("tmp tmp tmp n1 10.0.0.1 02:00:00:00:00:01 eth0 sda sda5 100 8.10 uefi"), Res: map[string]string{}},
		{Host: "n2", Fields: strings.Fields("tmp tmp tmp n2 10.0.0.2 02:00:00:00:00:02 eth0 sda sda5 100 8.10 uefi"), Res: map[string]string{}},
	}
}

func TestLaunchBatchSuccess(t *testing.T) {
	a, f, repo := newTestApp(t)
	ts := []*target{
		{Host: "n1", Fields: strings.Fields("tmp tmp tmp n1 10.0.0.1 02:00:00:00:00:01 eth0 sda sda5 100 8.10 uefi"), Res: map[string]string{}},
		{Host: "n2", Fields: strings.Fields("tmp tmp tmp n2 10.0.0.2 02:00:00:00:00:02 eth0 sda sda5 100 8.10 uefi"), Res: map[string]string{}},
	}
	if !a.launchBatch(stageStress, batch{name: "stress", tc: a.cfg.Stress, list: ts}, 0) {
		t.Fatal("성공이어야 함")
	}
	b, err := os.ReadFile(filepath.Join(repo, "hong_stress.yaml"))
	if err != nil || strings.Count(string(b), "\n") != 2 || !strings.HasPrefix(string(b), "tmp tmp tmp n1 ") {
		t.Errorf("yaml = %q %v", b, err)
	}
	if f.patch["source_path"] != "hong_stress_2ea.yml" {
		t.Errorf("PATCH = %v", f.patch)
	}
	ev := f.launches["/api/v2/job_templates/5/launch/"]
	if ev["target_hosts"] != "n1\nn2" || ev["duration"] != "60" || ev["mode"] != "all" {
		t.Errorf("extra_vars = %v", ev)
	}
	// survey type 변환: integer/float → 숫자(JSON 에서 float64), text·survey 에 없음 → 입력 글자 그대로
	if ev["end_year"] != float64(2099) || ev["end_month"] != float64(7) || ev["end_day"] != "08" || ev["end_hour"] != float64(9) || ev["end_min"] != "05" {
		t.Errorf("종료 시각 extra_vars = %v", ev)
	}
	// stress-ng 템플릿은 개인 인벤토리를 그대로 쓰므로 인벤토리를 바꾸지 않는다
	if strings.Join(f.events, ",") != "launch 5" || f.tmplInv[5] != 1 {
		t.Errorf("events = %v inv=%d", f.events, f.tmplInv[5])
	}
	if f.calls["GET /api/v2/inventory_updates/21/"] != 2 || f.calls["GET /api/v2/jobs/31/"] != 2 {
		t.Errorf("poll 횟수 = %v", f.calls)
	}
}

func TestLaunchBatchJobFail(t *testing.T) {
	a, f, _ := newTestApp(t)
	f.failCPUJob = true
	ts := []*target{{Host: "n1", Fields: strings.Fields("a b c n1 e f g h i j k l"), Res: map[string]string{}}}
	if a.launchBatch(stageCPU, batch{name: "cpu", tc: a.cfg.CPU, list: ts}, 1) {
		t.Fatal("실패여야 함")
	}
	if ts[0].Res[stageCPU] != resFail || len(ts[0].Notes) != 1 || !strings.Contains(ts[0].Notes[0], "실패(job 32)") {
		t.Errorf("결과 = %v %v", ts[0].Res, ts[0].Notes)
	}
	if _, err := os.Stat(filepath.Join(os.Getenv("FAKE_REPO"), "hong_retry1_cpu.yaml")); err != nil {
		t.Errorf("재실행 yaml 이름: %v", err)
	}
}

// hostname_key 를 비우면 hostname 목록을 넘기지 않는다. cpu 는 종료 시각·survey 조회 없음.
func TestLaunchBatchNoHostnameKey(t *testing.T) {
	a, f, _ := newTestApp(t)
	a.cfg.CPU = TemplateConf{Template: "6"}
	ts := []*target{{Host: "n1", Fields: strings.Fields("a b c n1 e f g h i j k l"), Res: map[string]string{}}}
	a.launchBatch(stageCPU, batch{name: "cpu", tc: a.cfg.CPU, list: ts}, 0)
	ev := f.launches["/api/v2/job_templates/6/launch/"]
	if ev != nil || f.calls["GET /api/v2/job_templates/6/survey_spec/"] != 0 {
		t.Errorf("extra_vars 가 없어야 함: %v calls=%v", ev, f.calls)
	}
}

// launch 가 실패해도 인벤토리는 되돌린다.
func TestLaunchFailRestores(t *testing.T) {
	a, f, _ := newTestApp(t)
	f.failLaunch = true
	ts := twoTargets()
	if a.launchBatch(stageCPU, batch{name: "cpu", tc: a.cfg.CPU, list: ts}, 0) {
		t.Fatal("실패여야 함")
	}
	if strings.Join(f.events, ",") != "patch 6→2,launch 6,patch 6→1" || f.tmplInv[6] != 1 {
		t.Errorf("events = %v", f.events)
	}
	if ts[0].Res[stageCPU] != resFail || !strings.Contains(strings.Join(ts[0].Notes, ","), "launch 실패") {
		t.Errorf("결과 = %v %v", ts[0].Res, ts[0].Notes)
	}
}

// 되돌리기 실패: 비고에 남기고 job 은 계속 감시 대상.
func TestRestoreFail(t *testing.T) {
	a, f, _ := newTestApp(t)
	f.failRestore = true
	var out strings.Builder
	a.out = &out
	ts := twoTargets()
	if !a.launchBatch(stageCPU, batch{name: "cpu", tc: a.cfg.CPU, list: ts}, 0) {
		t.Fatal("job 은 성공이어야 함")
	}
	if len(ts[0].Notes) != 1 || ts[0].Notes[0] != "인벤토리 복원 실패 — 수동으로 DEFAULT 로" {
		t.Errorf("비고 = %v", ts[0].Notes)
	}
	if !strings.Contains(out.String(), "✗✗✗") {
		t.Errorf("경고 없음:\n%s", out.String())
	}
	b, _ := os.ReadFile(a.cfg.HistoryFile)
	if !strings.Contains(string(b), "action=restore") {
		t.Errorf("이력 = %s", b)
	}
}

// 사용 중(batch): [인벤토리사용중] 줄 → stdin "r" → 다시 확인 후 실행. 등록은 한 번만.
func TestBusyThenRetry(t *testing.T) {
	a, f, _ := newTestApp(t)
	f.busyGets = 1
	var out strings.Builder
	a.out = &out
	a.in = bufio.NewReader(strings.NewReader("r\n"))
	ts := twoTargets()
	if !a.launchBatch(stageCPU, batch{name: "cpu", tc: a.cfg.CPU, list: ts}, 0) {
		t.Fatalf("성공이어야 함:\n%s", out.String())
	}
	want := busyPrefix + "템플릿 cpu-test(6) 인벤토리가 kim 입니다(DEFAULT 아님) — 그 인벤토리 사용자에게 DEFAULT 로 바꿔 달라고 요청하세요\n"
	if strings.Count(out.String(), busyPrefix) != 1 || !strings.Contains(out.String(), "\n"+want) {
		t.Errorf("출력:\n%s", out.String())
	}
	if f.calls["POST /api/v2/inventory_sources/11/update/"] != 1 || strings.Join(f.events, ",") != "patch 6→2,launch 6,patch 6→1" {
		t.Errorf("calls=%v events=%v", f.calls, f.events)
	}
}

// 사용 중 + stdin EOF → 그 묶음 중단, 인벤토리는 건드리지 않음.
func TestBusyThenEOF(t *testing.T) {
	a, f, _ := newTestApp(t)
	f.busyGets = 100
	ts := twoTargets()
	if a.launchBatch(stageCPU, batch{name: "cpu", tc: a.cfg.CPU, list: ts}, 0) {
		t.Fatal("실패여야 함")
	}
	if len(f.events) != 0 || ts[1].Res[stageCPU] != resFail || ts[1].Notes[0] != "템플릿 인벤토리 사용 중" {
		t.Errorf("events=%v res=%v notes=%v", f.events, ts[1].Res, ts[1].Notes)
	}
}

// 대화형: 질문 문구, x 면 중단.
func TestBusyInteractiveX(t *testing.T) {
	a, f, _ := newTestApp(t)
	f.busyGets = 100
	a.interactive = true
	var out strings.Builder
	a.out = &out
	a.in = bufio.NewReader(strings.NewReader("x\n"))
	if a.launchBatch(stageCPU, batch{name: "cpu", tc: a.cfg.CPU, list: twoTargets()}, 0) {
		t.Fatal("실패여야 함")
	}
	if !strings.Contains(out.String(), "r=다시 확인, x=이 묶음 중단: ") || strings.Contains(out.String(), busyPrefix) {
		t.Errorf("출력:\n%s", out.String())
	}
}

// PATCH 직후 다른 사람이 바꿨으면 되돌리지 않고 사용 중으로 처리.
func TestSwitchStolen(t *testing.T) {
	a, f, _ := newTestApp(t)
	f.stealPatch = true
	ts := twoTargets()
	if a.launchBatch(stageCPU, batch{name: "cpu", tc: a.cfg.CPU, list: ts}, 0) {
		t.Fatal("실패여야 함")
	}
	if strings.Join(f.events, ",") != "patch 6→2" || ts[0].Notes[0] != "템플릿 인벤토리 사용 중" {
		t.Errorf("events=%v notes=%v", f.events, ts[0].Notes)
	}
}

// batch 에서 종료 시각이 지났으면 등록 없이 묶음 실패.
func TestEndPassedBatch(t *testing.T) {
	a, f, _ := newTestApp(t)
	a.end.T = time.Now().Add(-time.Minute)
	ts := twoTargets()
	if a.launchBatch(stageStress, batch{name: "stress", tc: a.cfg.Stress, list: ts}, 1) {
		t.Fatal("실패여야 함")
	}
	if ts[0].Notes[0] != "종료 시각이 지남" || f.calls["POST /api/v2/inventory_sources/11/update/"] != 0 {
		t.Errorf("notes=%v calls=%v", ts[0].Notes, f.calls)
	}
}

// 신호를 받으면 바꿔 둔 인벤토리를 되돌리고 128+신호번호로 끝낸다.
func TestOnSignalRestores(t *testing.T) {
	a, f, _ := newTestApp(t)
	f.tmplInv[5] = 2
	s := &invSwitch{}
	s.begin(a.client, 5, 1)
	code := -1
	old := exitFn
	exitFn = func(c int) { code = c }
	defer func() { exitFn = old }()
	var out strings.Builder
	s.onSignal(syscall.SIGTERM, &out)
	s.mu.Unlock() // onSignal 은 종료 전까지 잠금을 쥔다
	if code != 143 || f.tmplInv[5] != 1 || !strings.Contains(out.String(), "되돌렸습니다") {
		t.Errorf("code=%d inv=%d out=%s", code, f.tmplInv[5], out.String())
	}
	s.onSignal(syscall.SIGINT, io.Discard) // 바꾼 게 없으면 PATCH 없이 종료
	if code != 130 || len(f.events) != 1 {
		t.Errorf("code=%d events=%v", code, f.events)
	}
}

func TestPrepareAWXMissingInventory(t *testing.T) {
	a, _, _ := newTestApp(t)
	a.cfg.Username = "nobody"
	if err := a.prepareAWX([]string{stageCPU}); err == nil || !strings.Contains(err.Error(), "nobody") {
		t.Errorf("err = %v", err)
	}
	a.cfg.Username, a.cfg.DefaultInventory = "admin", "DEF2"
	if err := a.prepareAWX([]string{stageCPU}); err == nil || !strings.Contains(err.Error(), "default_inventory") {
		t.Errorf("err = %v", err)
	}
	a.cfg.DefaultInventory, a.cfg.CPU.Template = "DEFAULT", "nope"
	if err := a.prepareAWX([]string{stageCPU}); err == nil || !strings.Contains(err.Error(), "cpu 템플릿") {
		t.Errorf("err = %v", err)
	}
}

func TestRegisterNoNewYML(t *testing.T) {
	a, _, _ := newTestApp(t)
	writeScript(t, sshBin, "exit 0\n") // yml 을 만들지 않음
	_, err := a.register("hong_cpu.yaml", []*target{{Host: "n1", Fields: strings.Fields("a b c n1 e f g h i j k l")}})
	if err == nil || !strings.Contains(err.Error(), "0개") {
		t.Errorf("err = %v", err)
	}
}

func TestInvsyncFixedSource(t *testing.T) {
	a, f, _ := newTestApp(t)
	a.cfg.InventorySource = "11"
	if err := a.client.invsync(a.cfg, "x.yml", time.Millisecond, a.logf); err != nil {
		t.Fatal(err)
	}
	if f.calls["GET /api/v2/inventories/7/inventory_sources/"] != 0 || f.patch["source_path"] != "x.yml" {
		t.Errorf("calls=%v patch=%v", f.calls, f.patch)
	}
	a.cfg.InventorySource = "99" // 404
	if err := a.client.invsync(a.cfg, "x.yml", time.Millisecond, a.logf); err == nil || !strings.Contains(err.Error(), "404") {
		t.Errorf("err = %v", err)
	}
}

// 응답은 했지만 vendor_id 가 없어 원격 명령이 실패한 호스트(gossh 는 stderr 로만 알림)는 접속불가가 아니라 unknown.
func TestGosshRunStderrCountsAsResponse(t *testing.T) {
	dir := t.TempDir()
	g := filepath.Join(dir, "gossh")
	writeScript(t, g, `hf=$2
echo "h1: vendor_id	: GenuineIntel"
echo "h2: ERROR: 종료코드 1" >&2
echo "h3: ERROR: SSH 접속 실패" >&2
echo h3 > "${hf}_res_off"
`)
	r := &gosshRunner{path: g, workDir: dir, log: io.Discard}
	hosts := []string{"h1", "h2", "h3"}
	out, off, err := r.run(hosts, vendorCmd)
	if err != nil {
		t.Fatal(err)
	}
	v := parseVendors(out, off, hosts)
	if v["h1"] != vendorIntel || v["h2"] != vendorUnknown || v["h3"] != "" {
		t.Fatalf("vendors = %v", v)
	}
	if m, _ := filepath.Glob(filepath.Join(dir, "hosts_*")); len(m) != 0 {
		t.Fatalf("임시 파일이 남음: %v", m)
	}
}
