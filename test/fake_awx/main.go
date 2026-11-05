// fake_awx: stress-test 랩 검증용 AWX 대역 (표준 라이브러리만).
// 사용: go run . -addr :18080 [-skipfile f] [-skiponce h1,h2] [-failtmpl stress-ng] [-loops 8] [-key target_hosts]
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
)

var (
	addr     = flag.String("addr", "127.0.0.1:18080", "listen")
	skipFile = flag.String("skipfile", "", "이 파일(줄마다 hostname)에 있는 호스트는 부하를 시작하지 않음 (launch 때마다 다시 읽음)")
	skipOnce = flag.String("skiponce", "", "쉼표 구분 hostname: 첫 launch 에서만 부하를 시작하지 않음")
	failTmpl = flag.String("failtmpl", "", "이 템플릿 이름의 job 은 failed")
	loops    = flag.Int("loops", 8, "호스트당 busy loop 수")
	hostKey  = flag.String("key", "target_hosts", "survey 변수명")
	gossh    = flag.String("gossh", "/usr/local/bin/gossh", "gossh")
)

var (
	mu       sync.Mutex
	tmpls    = map[int]string{1: "cpu-intel", 2: "cpu-amd", 3: "stress-ng"}
	jobs     = map[int]string{} // job id -> status
	jobOut   = map[int]string{}
	nextJob  = 100
	nextUpd  = 500
	once     = map[string]bool{}
	srcField = map[string]interface{}{}
)

func js(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
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
	mux.HandleFunc("/api/v2/job_templates/", func(w http.ResponseWriter, r *http.Request) {
		p := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/v2/job_templates/"), "/")
		if p == "" {
			var res []map[string]interface{}
			for id, n := range tmpls {
				res = append(res, map[string]interface{}{"id": id, "name": n})
			}
			js(w, map[string]interface{}{"results": res})
			return
		}
		parts := strings.Split(p, "/")
		id, _ := strconv.Atoi(parts[0])
		if len(parts) == 1 {
			n, ok := tmpls[id]
			if !ok {
				http.NotFound(w, r)
				return
			}
			js(w, map[string]interface{}{"id": id, "name": n})
			return
		}
		if parts[1] == "launch" && r.Method == http.MethodPost {
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
			mu.Unlock()
			log.Printf("launch tmpl=%s job=%d hosts=%q start=%v status=%s", tmpls[id], j, hv, start, st)
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
		js(w, map[string]interface{}{"results": []map[string]interface{}{{"id": 7, "name": "src1"}}})
	})
	mux.HandleFunc("/api/v2/inventory_sources/", func(w http.ResponseWriter, r *http.Request) {
		p := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/v2/inventory_sources/"), "/")
		if strings.HasSuffix(p, "/update") && r.Method == http.MethodPost {
			mu.Lock()
			nextUpd++
			mu.Unlock()
			w.WriteHeader(202)
			js(w, map[string]interface{}{"inventory_update": nextUpd})
			return
		}
		if r.Method == http.MethodPatch {
			var b map[string]interface{}
			json.NewDecoder(r.Body).Decode(&b)
			log.Printf("PATCH %s %v", p, b)
			js(w, b)
			return
		}
		js(w, map[string]interface{}{"id": 7})
	})
	mux.HandleFunc("/api/v2/inventory_updates/", func(w http.ResponseWriter, r *http.Request) {
		js(w, map[string]interface{}{"id": 1, "status": "successful"})
	})
	log.Printf("fake_awx listening %s", *addr)
	log.Fatal(http.ListenAndServe(*addr, mux))
}
