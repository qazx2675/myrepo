package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"
)

const marker = "_DNS_AND_TOOLS_NOT_FOUND"

var hostTokenRe = regexp.MustCompile(`\S+` + regexp.QuoteMeta(marker))

type lookupResult struct {
	IP  string
	Err error
}

// resolveHost: nslookup "$HOSTNAME" 2>/dev/null | awk '/^Address: / {print $2}' | head -n1 과 동일 로직
func resolveHost(ctx context.Context, hostname string) lookupResult {
	cmd := exec.CommandContext(ctx, "nslookup", hostname)
	out, _ := cmd.Output()
	sc := bufio.NewScanner(strings.NewReader(string(out)))
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "Address: ") {
			if fields := strings.Fields(line); len(fields) >= 2 {
				return lookupResult{IP: fields[1]}
			}
		}
	}
	return lookupResult{Err: fmt.Errorf("주소를 찾을 수 없음")}
}

func writeLines(path string, lines []string) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}
	defer f.Close()
	w := bufio.NewWriter(f)
	for _, l := range lines {
		if _, err := w.WriteString(l + "\n"); err != nil {
			return err
		}
	}
	if err := w.Flush(); err != nil {
		return err
	}
	return f.Sync()
}

// ip_change.go - Phase4-6 킥스타트 배포 리스트의 _DNS_AND_TOOLS_NOT_FOUND 마커를 nslookup으로 실 IP로 치환
func main() {
	concurrency := flag.Int("concurrency", 32, "동시 nslookup 처리 수")
	timeout := flag.Duration("timeout", 5*time.Second, "nslookup 1건당 타임아웃")
	flag.Parse()

	args := flag.Args()
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "사용법: ip_change [-concurrency N] [-timeout 5s] <파일경로>")
		os.Exit(1)
	}
	outPath := args[0]
	if fi, err := os.Stat(outPath); err != nil || fi.IsDir() {
		fmt.Fprintf(os.Stderr, "오류: 지정한 파일을 찾을 수 없습니다: %s\n", outPath)
		os.Exit(1)
	}

	inFile, err := os.Open(outPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "오류: 파일을 열 수 없습니다: %v\n", err)
		os.Exit(1)
	}
	var lines []string
	sc := bufio.NewScanner(inFile)
	sc.Buffer(make([]byte, 1024*1024), 1024*1024)
	for sc.Scan() {
		lines = append(lines, sc.Text())
	}
	inFile.Close()

	originalLines := make([]string, len(lines))
	copy(originalLines, lines)

	type job struct {
		lineIdx  int
		hostname string
	}
	var jobs []job
	for i, line := range lines {
		if !strings.Contains(line, marker) {
			continue
		}
		token := hostTokenRe.FindString(line)
		if token == "" {
			continue
		}
		if hostname := strings.TrimSuffix(token, marker); hostname != "" {
			jobs = append(jobs, job{lineIdx: i, hostname: hostname})
		}
	}
	if len(jobs) == 0 {
		fmt.Println("치환 대상 라인이 없습니다. (원본 그대로 유지)")
		return
	}

	uniqueHosts := make(map[string]struct{})
	for _, j := range jobs {
		uniqueHosts[j.hostname] = struct{}{}
	}
	resultMu := sync.Mutex{}
	results := make(map[string]lookupResult, len(uniqueHosts))
	var wg sync.WaitGroup
	sem := make(chan struct{}, *concurrency)
	for hostname := range uniqueHosts {
		wg.Add(1)
		sem <- struct{}{}
		go func(hostname string) {
			defer wg.Done()
			defer func() { <-sem }()
			ctx, cancel := context.WithTimeout(context.Background(), *timeout)
			defer cancel()
			r := resolveHost(ctx, hostname)
			resultMu.Lock()
			results[hostname] = r
			resultMu.Unlock()
		}(hostname)
	}
	wg.Wait()

	for _, j := range jobs {
		r := results[j.hostname]
		ip := r.IP
		if ip == "" {
			ip = "NSLOOKUP_FAILED"
		}
		lines[j.lineIdx] = strings.ReplaceAll(lines[j.lineIdx], j.hostname+marker, j.hostname+" "+ip)
	}

	// 원본을 .bak로 백업 후 직접 덮어쓰기 (rename 방식이 일부 네트워크 마운트에서 실패하는 문제 회피)
	backupPath := outPath + ".bak"
	if err := writeLines(backupPath, originalLines); err != nil {
		fmt.Fprintf(os.Stderr, "오류: 백업 파일 생성 실패(%s): %v\n", backupPath, err)
		os.Exit(1)
	}
	if err := writeLines(outPath, lines); err != nil {
		fmt.Fprintf(os.Stderr, "오류: 원본 파일 쓰기 실패: %v\n", err)
		os.Exit(1)
	}
	os.Remove(backupPath)
	fmt.Println("동적 변환 완료! 파일이 성공적으로 업데이트되었습니다.")
}
