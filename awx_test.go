package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeAWX 는 launch/poll/invsync 에 필요한 엔드포인트만 흉내 낸다.
type fakeAWX struct {
	mu       sync.Mutex
	calls    map[string]int
	patch    map[string]interface{}
	launches map[string]map[string]interface{}
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
	switch key {
	case "GET /api/v2/inventories/7/inventory_sources/":
		write(map[string]interface{}{"results": []map[string]interface{}{{"id": 11, "name": "src"}, {"id": 12, "name": "src2"}}})
	case "PATCH /api/v2/inventory_sources/11/":
		json.Unmarshal(body, &f.patch)
		write(map[string]interface{}{"id": 11})
	case "POST /api/v2/inventory_sources/11/update/":
		write(map[string]interface{}{"inventory_update": 21})
	case "GET /api/v2/inventory_updates/21/":
		write(map[string]interface{}{"id": 21, "status": status("successful")})
	case "GET /api/v2/job_templates/":
		write(map[string]interface{}{"results": []map[string]interface{}{{"id": 5, "name": "stress-ng"}, {"id": 6, "name": "cpu-intel"}}})
	case "GET /api/v2/job_templates/6/":
		write(map[string]interface{}{"id": 6, "name": "cpu-intel"})
	case "POST /api/v2/job_templates/5/launch/", "POST /api/v2/job_templates/6/launch/":
		var m map[string]map[string]interface{}
		json.Unmarshal(body, &m)
		f.launches[r.URL.Path] = m["extra_vars"]
		if strings.Contains(r.URL.Path, "/5/") {
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

// newTestApp 은 가짜 AWX·가짜 ssh/scp 로 등록·launch 를 시험할 app 을 만든다.
func newTestApp(t *testing.T) (*app, *fakeAWX, string) {
	f := &fakeAWX{calls: map[string]int{}, launches: map[string]map[string]interface{}{}}
	srv := httptest.NewServer(http.HandlerFunc(f.handler))
	t.Cleanup(srv.Close)

	dir := t.TempDir()
	repo, svr, bin := filepath.Join(dir, "repo"), filepath.Join(dir, "svr"), filepath.Join(dir, "bin")
	for _, d := range []string{repo, svr, bin} {
		os.MkdirAll(d, 0o755)
	}
	os.WriteFile(filepath.Join(svr, "old_1ea.yml"), nil, 0o644)
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
	cfg.Stress = TemplateConf{Template: "stress-ng", HostnameKey: "target_hosts", ExtraVars: "duration=60, mode=all"}
	cfg.CPUIntel = TemplateConf{Template: "6", HostnameKey: "hosts"}
	a := &app{
		cfg:     cfg,
		client:  newAWXClient(srv.URL, "admin", "pw", false, 5*time.Second),
		user:    "hong",
		workDir: dir,
		out:     io.Discard,
	}
	return a, f, repo
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
	if f.calls["GET /api/v2/inventory_updates/21/"] != 2 || f.calls["GET /api/v2/jobs/31/"] != 2 {
		t.Errorf("poll 횟수 = %v", f.calls)
	}
}

func TestLaunchBatchJobFail(t *testing.T) {
	a, _, _ := newTestApp(t)
	ts := []*target{{Host: "n1", Fields: strings.Fields("a b c n1 e f g h i j k l"), Res: map[string]string{}}}
	if a.launchBatch(stageCPU, batch{name: "intel", tc: a.cfg.CPUIntel, list: ts}, 1) {
		t.Fatal("실패여야 함")
	}
	if ts[0].Res[stageCPU] != resFail || len(ts[0].Notes) != 1 || !strings.Contains(ts[0].Notes[0], "실패(job 32)") {
		t.Errorf("결과 = %v %v", ts[0].Res, ts[0].Notes)
	}
	if _, err := os.Stat(filepath.Join(os.Getenv("FAKE_REPO"), "hong_retry1_intel.yaml")); err != nil {
		t.Errorf("재실행 yaml 이름: %v", err)
	}
}

func TestRegisterNoNewYML(t *testing.T) {
	a, _, _ := newTestApp(t)
	writeScript(t, sshBin, "exit 0\n") // yml 을 만들지 않음
	_, err := a.register("hong_amd.yaml", []*target{{Host: "n1", Fields: strings.Fields("a b c n1 e f g h i j k l")}})
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
