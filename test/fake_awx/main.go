// fake_awx: stress-test 랩 검증용 AWX 대역 (표준 라이브러리만).
// 사용: go run . -addr :18080 [-svrdir d] [-skipfile f] [-skiponce h1,h2] [-failtmpl stress-ng] [-busyfile f] [-launchdelay 5s] [-loops 8] [-key target_hosts]
// 인벤토리: 1=DEFAULT, 2=admin(tester conf username), 3=src-inv(소스 7), 4=I_dh.kim(username 포함 판정용), 9=다른 사용자(-busyfile)
// -inspect 시험용: 템플릿 survey(stress-ng: end_* 5개+호스트+mode, cpu-test: 호스트+intensity), 소스 상세 필드
// -yaml-state 시험용: 소스 7 의 source_path 는 PATCH 로 바뀌고, 소스 동기화(update) 때 -svrdir 의 그 yml(줄마다 4번째 필드 = 호스트)로 인벤토리 3 호스트가 채워진다. POST /api/v2/inventories/3/hosts/ 로 호스트 추가(다른 사용자 흉내)
package main

import (
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

var (
	addr     = flag.String("addr", "127.0.0.1:18080", "listen")
	skipFile = flag.String("skipfile", "", "이 파일(줄마다 hostname)에 있는 호스트는 부하를 시작하지 않음 (launch 때마다 다시 읽음)")
	skipOnce = flag.String("skiponce", "", "쉼표 구분 hostname: 첫 launch 에서만 부하를 시작하지 않음")
	failTmpl = flag.String("failtmpl", "", "이 템플릿 이름의 job 은 failed")
	busyFile = flag.String("busyfile", "", "이 파일에 이름이 적혀 있으면 템플릿 조회 때 인벤토리가 그 이름(id 9, 다른 사용자)인 것처럼 보임 (조회 때마다 다시 읽음)")
	loops    = flag.Int("loops", 8, "호스트당 busy loop 수")
	delay    = flag.Duration("launchdelay", 0, "launch 응답을 이만큼 늦춤 (인벤토리를 바꿔 둔 사이 신호 시험용)")
	hostKey  = flag.String("key", "target_hosts", "survey 변수명")
	gossh    = flag.String("gossh", "/usr/local/bin/gossh", "gossh")
	svrDir   = flag.String("svrdir", "/root/work/st-svr", "소스 동기화(update) 때 source_path 의 yml 을 읽는 폴더(줄마다 4번째 필드 = 호스트 이름)")
)

var (
	mu       sync.Mutex
	tmpls    = map[int]string{1: "cpu-test", 3: "stress-ng"}
	tmplInv  = map[int]int{1: 1, 3: 2} // 템플릿 → 현재 인벤토리 (stress-ng 은 개인 인벤토리 admin)
	invs     = map[int]string{1: "DEFAULT", 2: "admin", 3: "src-inv", 4: "I_dh.kim"}
	jobs     = map[int]string{} // job id -> status
	jobOut   = map[int]string{}
	nextJob  = 100
	nextUpd  = 500
	once     = map[string]bool{}
	srcField = map[string]interface{}{}
	srcPath  = "tester_stress_2ea.yml" // 소스 7 의 source_path (PATCH 로 바뀜, 다른 사용자가 바꾼 것도 흉내)
	invHosts []string                  // 인벤토리 3 의 호스트 이름 (소스 동기화 때 yml 로 다시 채움, POST .../3/hosts/ 로 추가)
)

// syncHosts 는 소스 동기화(update)를 흉내 낸다: svrdir 의 srcPath yml 을 읽을 수 있으면 호스트를 그 파일의 4번째 필드로 바꾼다.
// 읽을 수 없으면(다른 사용자가 만든 yml 등) 호스트는 그대로 둔다. mu 를 잡고 부른다.
func syncHosts() {
	b, err := os.ReadFile(*svrDir + "/" + srcPath)
	if err != nil {
		return
	}
	invHosts = nil
	for _, l := range strings.Split(string(b), "\n") {
		if f := strings.Fields(l); len(f) >= 4 {
			invHosts = append(invHosts, f[3])
		}
	}
}

func js(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

// busyName 은 -busyfile 내용(다른 사용자 인벤토리 이름)이다. 비었으면 "".
func busyName() string {
	if *busyFile == "" {
		return ""
	}
	b, _ := os.ReadFile(*busyFile)
	return strings.TrimSpace(string(b))
}

func tmplJSON(id int) map[string]interface{} {
	inv := tmplInv[id]
	name := invs[inv]
	if n := busyName(); n != "" {
		inv, name = 9, n
	}
	return map[string]interface{}{"id": id, "name": tmpls[id], "inventory": inv,
		"ask_inventory_on_launch": false, "survey_enabled": true, "extra_vars": "",
		"summary_fields": map[string]interface{}{"inventory": map[string]interface{}{"id": inv, "name": name}}}
}

// srcJSON 은 인벤토리 소스 7 (인벤토리 3) 상세다.
func srcJSON() map[string]interface{} {
	return map[string]interface{}{"id": 7, "name": "src1", "inventory": 3, "source": "scm", "source_path": srcPath, "overwrite": true, "overwrite_vars": true}
}

func skipped(h string) bool {
	if *skipFile != "" {
		if b, err := os.ReadFile(*skipFile); err == nil {
			for _, l := range strings.Fields(string(b)) {
				if l == h {
					return true
				}
			}
		}
	}
	for _, s := range strings.Split(*skipOnce, ",") {
		if s != "" && s == h && !once[h] {
			once[h] = true
			return true
		}
	}
	return false
}

func startLoad(hosts []string) {
	if len(hosts) == 0 {
		return
	}
	f, _ := os.CreateTemp("", "fakeawx_hosts")
	f.WriteString(strings.Join(hosts, "\n") + "\n")
	f.Close()
	defer os.Remove(f.Name())
	// 대상 로그인 셸이 csh/tcsh 일 수 있어 sh 스크립트를 base64 로 넘겨 sh 에서 실행한다
	script := fmt.Sprintf("nohup sh -c 'for i in $(seq 1 %d); do timeout 200 sh -c \"while :; do :; done\" & done' >/dev/null 2>&1 &\n", *loops)
	cmd := "echo " + base64.StdEncoding.EncodeToString([]byte(script)) + " | base64 -d | sh"
	out, err := exec.Command(*gossh, "-w", f.Name(), "-script", cmd).CombinedOutput()
	log.Printf("load start %v err=%v out=%q", hosts, err, string(out))
	for _, s := range []string{"_res_off", "_res_refsed", "_res_cancel", "_os_install", "_nosvrauto"} {
		os.Remove(f.Name() + s)
	}
}

func main() {
	flag.Parse()
	mux := http.NewServeMux()
	// setup.sh 접속 점검용: 비밀번호가 test 일 때만 200
	mux.HandleFunc("/api/v2/me/", func(w http.ResponseWriter, r *http.Request) {
		if u, p, ok := r.BasicAuth(); !ok || u == "" || p != "test" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Write([]byte(`{"count":1}`))
	})
	mux.HandleFunc("/api/v2/job_templates/", func(w http.ResponseWriter, r *http.Request) {
		p := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/v2/job_templates/"), "/")
		if p == "" {
			var res []map[string]interface{}
			mu.Lock()
			for id := range tmpls {
				res = append(res, tmplJSON(id))
			}
			mu.Unlock()
			js(w, map[string]interface{}{"results": res})
			return
		}
		parts := strings.Split(p, "/")
		id, _ := strconv.Atoi(parts[0])
		if _, ok := tmpls[id]; !ok {
			http.NotFound(w, r)
			return
		}
		if len(parts) == 1 {
			mu.Lock()
			defer mu.Unlock()
			if r.Method == http.MethodPatch {
				var b struct {
					Inventory int `json:"inventory"`
				}
				json.NewDecoder(r.Body).Decode(&b)
				if _, ok := invs[b.Inventory]; !ok {
					w.WriteHeader(400)
					js(w, map[string]interface{}{"inventory": []string{"없는 인벤토리"}})
					return
				}
				log.Printf("PATCH template=%s inventory=%d(%s)", tmpls[id], b.Inventory, invs[b.Inventory])
				tmplInv[id] = b.Inventory
			}
			js(w, tmplJSON(id))
			return
		}
		if parts[1] == "survey_spec" {
			// stress-ng: end_* 중 day/min 은 text, 나머지는 integer (type 변환 확인용). cpu-test: 호스트 목록 + 선택 항목
			var spec []map[string]interface{}
			q := func(v, t string, req bool, extra map[string]interface{}) {
				m := map[string]interface{}{"variable": v, "type": t, "question_name": v + " 질문", "required": req}
				for k, x := range extra {
					m[k] = x
				}
				spec = append(spec, m)
			}
			switch tmpls[id] {
			case "stress-ng":
				q("end_year", "integer", true, map[string]interface{}{"min": 2020, "max": 2100})
				q("end_month", "integer", true, map[string]interface{}{"min": 1, "max": 12})
				q("end_day", "text", true, nil)
				q("end_hour", "integer", true, map[string]interface{}{"min": 0, "max": 23})
				q("end_min", "text", true, nil)
				q(*hostKey, "textarea", true, nil)
				q("mode", "multiplechoice", false, map[string]interface{}{"choices": "all\ncpu\nvm", "default": "all"})
			case "cpu-test":
				q(*hostKey, "textarea", true, nil)
				q("intensity", "integer", false, map[string]interface{}{"default": 80, "min": 1, "max": 100})
			}
			js(w, map[string]interface{}{"name": "", "description": "", "spec": spec})
			return
		}
		if parts[1] == "launch" && r.Method == http.MethodPost {
			time.Sleep(*delay)
			var body struct {
				ExtraVars map[string]interface{} `json:"extra_vars"`
			}
			json.NewDecoder(r.Body).Decode(&body)
			hv, _ := body.ExtraVars[*hostKey].(string)
			var start []string
			mu.Lock()
			for _, h := range strings.Fields(hv) {
				if !skipped(h) {
					start = append(start, h)
				}
			}
			nextJob++
			j := nextJob
			st := "successful"
			if tmpls[id] == *failTmpl {
				st = "failed"
				jobOut[j] = "fake failure line 1\nfake failure: playbook error\n"
			}
			jobs[j] = st
			inv := tmplInv[id]
			mu.Unlock()
			ev, _ := json.Marshal(body.ExtraVars)
			log.Printf("launch tmpl=%s inventory=%d job=%d hosts=%q start=%v status=%s extra_vars=%s", tmpls[id], inv, j, hv, start, st, ev)
			if st == "successful" {
				startLoad(start)
			}
			w.WriteHeader(201)
			js(w, map[string]interface{}{"job": j})
			return
		}
		http.NotFound(w, r)
	})
	mux.HandleFunc("/api/v2/jobs/", func(w http.ResponseWriter, r *http.Request) {
		parts := strings.Split(strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/v2/jobs/"), "/"), "/")
		id, _ := strconv.Atoi(parts[0])
		mu.Lock()
		defer mu.Unlock()
		if len(parts) > 1 && parts[1] == "stdout" {
			w.Write([]byte(jobOut[id]))
			return
		}
		js(w, map[string]interface{}{"id": id, "status": jobs[id]})
	})
	mux.HandleFunc("/api/v2/inventories/", func(w http.ResponseWriter, r *http.Request) {
		p := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/v2/inventories/"), "/")
		if p == "" { // 목록 (?name= 정확히 일치 필터)
			name := r.URL.Query().Get("name")
			var res []map[string]interface{}
			for id, n := range invs {
				if name == "" || n == name {
					res = append(res, map[string]interface{}{"id": id, "name": n})
				}
			}
			js(w, map[string]interface{}{"count": len(res), "results": res})
			return
		}
		// 3/hosts/ : 인벤토리 3 의 호스트 (GET 목록, POST {"name":..} 추가 = 다른 사용자가 호스트를 넣은 것 흉내)
		if p == "3/hosts" {
			mu.Lock()
			defer mu.Unlock()
			if r.Method == http.MethodPost {
				var b struct {
					Name string `json:"name"`
				}
				json.NewDecoder(r.Body).Decode(&b)
				log.Printf("POST host %s", b.Name)
				invHosts = append(invHosts, b.Name)
				w.WriteHeader(201)
				js(w, map[string]interface{}{"name": b.Name})
				return
			}
			res := []map[string]interface{}{}
			for _, h := range invHosts {
				res = append(res, map[string]interface{}{"name": h})
			}
			js(w, map[string]interface{}{"count": len(res), "next": nil, "results": res})
			return
		}
		// <id>/inventory_sources/ (소스 7 은 인벤토리 3 에만 있음)
		var res []map[string]interface{}
		if strings.HasPrefix(p, "3/") {
			mu.Lock()
			res = append(res, srcJSON())
			mu.Unlock()
		}
		js(w, map[string]interface{}{"count": len(res), "results": res})
	})
	mux.HandleFunc("/api/v2/inventory_sources/", func(w http.ResponseWriter, r *http.Request) {
		p := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/v2/inventory_sources/"), "/")
		if strings.HasSuffix(p, "/update") && r.Method == http.MethodPost {
			mu.Lock()
			nextUpd++
			syncHosts()
			mu.Unlock()
			w.WriteHeader(202)
			js(w, map[string]interface{}{"inventory_update": nextUpd})
			return
		}
		if r.Method == http.MethodPatch {
			var b map[string]interface{}
			json.NewDecoder(r.Body).Decode(&b)
			log.Printf("PATCH %s %v", p, b)
			if s, ok := b["source_path"].(string); ok {
				mu.Lock()
				srcPath = s
				mu.Unlock()
			}
			js(w, b)
			return
		}
		mu.Lock()
		defer mu.Unlock()
		js(w, srcJSON())
	})
	mux.HandleFunc("/api/v2/inventory_updates/", func(w http.ResponseWriter, r *http.Request) {
		js(w, map[string]interface{}{"id": 1, "status": "successful"})
	})
	log.Printf("fake_awx listening %s", *addr)
	log.Fatal(http.ListenAndServe(*addr, mux))
}
