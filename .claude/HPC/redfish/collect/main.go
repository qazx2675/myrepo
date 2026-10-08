// biosdump: list.txt 의 hostname 마다 /etc/hosts 에서 "<hostname>-m" 관리망 IP 를 찾아
// Redfish 로 BIOS 전체 값을 수집해 JSON 으로 저장한다. (수집 전용, 설정 변경 없음)
package main

import (
	"bufio"
	"crypto/tls"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

var client = &http.Client{
	Timeout:   60 * time.Second,
	Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}},
}

func get(ip, path, user, pass string) (map[string]interface{}, error) {
	req, _ := http.NewRequest("GET", "https://"+ip+path, nil)
	req.SetBasicAuth(user, pass)
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("%s -> HTTP %d", path, resp.StatusCode)
	}
	var m map[string]interface{}
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, fmt.Errorf("%s JSON 오류: %v", path, err)
	}
	return m, nil
}

func link(m map[string]interface{}, key string) string {
	if o, ok := m[key].(map[string]interface{}); ok {
		s, _ := o["@odata.id"].(string)
		return s
	}
	return ""
}

// 호스트 하나 수집: Systems 의 각 시스템에서 Bios(+Bios/Settings) 를 가져온다.
func collect(ip, user, pass string) (map[string]interface{}, error) {
	root, err := get(ip, "/redfish/v1/Systems", user, pass)
	if err != nil {
		return nil, err
	}
	out := map[string]interface{}{}
	members, _ := root["Members"].([]interface{})
	for _, mm := range members {
		sp, _ := mm.(map[string]interface{})["@odata.id"].(string)
		if sp == "" {
			continue
		}
		sys, err := get(ip, sp, user, pass)
		if err != nil {
			out[sp] = map[string]string{"error": err.Error()}
			continue
		}
		entry := map[string]interface{}{
			"Model": sys["Model"], "Manufacturer": sys["Manufacturer"],
			"SerialNumber": sys["SerialNumber"], "BiosVersion": sys["BiosVersion"],
		}
		bp := link(sys, "Bios")
		if bp == "" {
			bp = strings.TrimRight(sp, "/") + "/Bios"
		}
		if bios, err := get(ip, bp, user, pass); err != nil {
			entry["Bios_error"] = err.Error()
		} else {
			entry["Bios"] = bios
			// 대기 중(pending) 설정값 — 없으면 무시
			sp2 := ""
			if lk, ok := bios["@Redfish.Settings"].(map[string]interface{}); ok {
				sp2 = link(lk, "SettingsObject")
			}
			if sp2 == "" {
				sp2 = bp + "/Settings"
			}
			if st, err := get(ip, sp2, user, pass); err == nil {
				entry["Bios_Settings"] = st
			}
		}
		out[sp] = entry
	}
	return out, nil
}

// /etc/hosts 에서 hostname-m 의 IP 를 찾는다.
func loadHosts(path string) map[string]string {
	m := map[string]string{}
	f, err := os.Open(path)
	if err != nil {
		return m
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if i := strings.Index(line, "#"); i >= 0 {
			line = line[:i]
		}
		fs := strings.Fields(line)
		for _, name := range fs[min(1, len(fs)):] {
			if _, ok := m[name]; !ok {
				m[name] = fs[0]
			}
		}
	}
	return m
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func main() {
	list := flag.String("list", "list.txt", "hostname 목록")
	hostsF := flag.String("hosts", "/etc/hosts", "hosts 파일")
	suffix := flag.String("suffix", "-m", "관리망 호스트명 접미사")
	outDir := flag.String("out", "out", "결과 폴더")
	user := flag.String("user", "", "BMC ID")
	pass := flag.String("pass", "", "BMC PW")
	par := flag.Int("p", 5, "동시 수집 수")
	flag.Parse()

	hosts := loadHosts(*hostsF)
	lf, err := os.Open(*list)
	if err != nil {
		fmt.Println("list 열기 실패:", err)
		os.Exit(1)
	}
	var names []string
	sc := bufio.NewScanner(lf)
	for sc.Scan() {
		if n := strings.TrimSpace(sc.Text()); n != "" && !strings.HasPrefix(n, "#") {
			names = append(names, n)
		}
	}
	lf.Close()
	os.MkdirAll(*outDir, 0755)

	var wg sync.WaitGroup
	var mu sync.Mutex
	sem := make(chan struct{}, *par)
	ok, fail := 0, 0
	for _, n := range names {
		wg.Add(1)
		sem <- struct{}{}
		go func(n string) {
			defer wg.Done()
			defer func() { <-sem }()
			ip := hosts[n+*suffix]
			var err error
			if ip == "" {
				err = fmt.Errorf("%s%s 를 %s 에서 찾지 못함", n, *suffix, *hostsF)
			} else {
				var data map[string]interface{}
				if data, err = collect(ip, *user, *pass); err == nil {
					res := map[string]interface{}{"hostname": n, "bmc_ip": ip, "collected_at": time.Now().Format(time.RFC3339), "systems": data}
					b, _ := json.MarshalIndent(res, "", "  ")
					err = os.WriteFile(filepath.Join(*outDir, n+".json"), b, 0644)
				}
			}
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				fail++
				fmt.Printf("[실패] %s: %v\n", n, err)
			} else {
				ok++
				fmt.Printf("[완료] %s (%s)\n", n, ip)
			}
		}(n)
	}
	wg.Wait()
	fmt.Printf("수집완료: 성공 %d / 실패 %d  -> %s/\n", ok, fail, *outDir)
}
