// vcenter_vm_inventory: 다수 vCenter를 순회하며 VM 인벤토리(BM호스트/VM/vCenter/상태)를 수집하고 이전 결과와 변동분을 비교
package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/vmware/govmomi"
	"github.com/vmware/govmomi/view"
	"github.com/vmware/govmomi/vim25/mo"
	"github.com/vmware/govmomi/vim25/soap"
)

const (
	vcListName    = "vcenter.txt"
	defaultOutput = "vcenter_vm_inventory_status.txt"
	defaultVCUser = "lscsystems@vsphere.local"
	timeMarker    = "업데이트 시간:"
)

var (
	flagOutput      = flag.String("o", defaultOutput, "결과 파일 경로")
	flagDir         = flag.String("dir", "", "작업 디렉토리")
	flagUser        = flag.String("id", defaultVCUser, "vCenter 접속 ID")
	flagConcurrency = flag.Int("c", 4, "vCenter 동시 처리 개수")
	flagTimeout     = flag.Duration("timeout", 120*time.Second, "vCenter 1대당 수집 타임아웃")
	flagPortTimeout = flag.Duration("porttimeout", 2*time.Second, "TCP 443 접속 확인 타임아웃")
)

type vcResult struct {
	index int
	lines []string
}

func resolveBaseDir() (string, error) {
	if *flagDir != "" {
		return *flagDir, nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		cwd = "."
	}
	if _, statErr := os.Stat(filepath.Join(cwd, vcListName)); statErr == nil {
		return cwd, nil
	}
	return "", fmt.Errorf("%s 파일을 찾을 수 없습니다", vcListName)
}

func readVCenterList(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var list []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		list = append(list, line)
	}
	return list, sc.Err()
}

func hostPort(target string, defaultPort string) string {
	t := target
	if strings.Contains(t, "://") {
		if u, err := url.Parse(t); err == nil && u.Host != "" {
			t = u.Host
		}
	}
	t = strings.TrimSuffix(t, "/")
	if h, p, err := net.SplitHostPort(t); err == nil {
		return net.JoinHostPort(h, p)
	}
	return net.JoinHostPort(t, defaultPort)
}

func testVCPort(target string, timeout time.Duration) bool {
	conn, err := net.DialTimeout("tcp", hostPort(target, "443"), timeout)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

func collectFromVCenter(ctx context.Context, target, vcUser, password string) ([]string, error) {
	u, err := soap.ParseURL(target)
	if err != nil {
		return nil, fmt.Errorf("URL 파싱 실패: %w", err)
	}
	u.User = url.UserPassword(vcUser, password)
	client, err := govmomi.NewClient(ctx, u, true)
	if err != nil {
		return nil, fmt.Errorf("SDK 연결 실패: %w", err)
	}
	defer func() {
		logoutCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = client.Logout(logoutCtx)
	}()

	viewMgr := view.NewManager(client.Client)
	root := client.Client.ServiceContent.RootFolder

	hostView, err := viewMgr.CreateContainerView(ctx, root, []string{"HostSystem"}, true)
	if err != nil {
		return nil, err
	}
	var hosts []mo.HostSystem
	_ = hostView.Retrieve(ctx, []string{"HostSystem"}, []string{"name"}, &hosts)
	_ = hostView.Destroy(ctx)
	hostMap := make(map[string]string, len(hosts))
	for i := range hosts {
		hostMap[hosts[i].Self.Value] = hosts[i].Name
	}

	vmView, err := viewMgr.CreateContainerView(ctx, root, []string{"VirtualMachine"}, true)
	if err != nil {
		return nil, err
	}
	var vms []mo.VirtualMachine
	_ = vmView.Retrieve(ctx, []string{"VirtualMachine"}, []string{"name", "runtime.host", "runtime.connectionState"}, &vms)
	_ = vmView.Destroy(ctx)

	lines := make([]string, 0, len(vms))
	for i := range vms {
		hostName := "UNKNOWN_HOST"
		if vms[i].Runtime.Host != nil {
			if n, ok := hostMap[vms[i].Runtime.Host.Value]; ok && n != "" {
				hostName = n
			}
		}
		state := strings.ToLower(string(vms[i].Runtime.ConnectionState))
		lines = append(lines, fmt.Sprintf("%s / %s / %s / %s", hostName, vms[i].Name, target, state))
	}
	return lines, nil
}

func main() {
	flag.Parse()
	password := os.Getenv("VC_PASSWORD")
	if password == "" {
		os.Exit(1)
	}
	vcUser := os.Getenv("VC_USER")
	if vcUser == "" {
		vcUser = *flagUser
	}
	baseDir, err := resolveBaseDir()
	if err != nil {
		os.Exit(1)
	}
	vcList, err := readVCenterList(filepath.Join(baseDir, vcListName))
	if err != nil || len(vcList) == 0 {
		os.Exit(1)
	}
	concurrency := *flagConcurrency
	if concurrency < 1 {
		concurrency = 1
	}

	var wg sync.WaitGroup
	sem := make(chan struct{}, concurrency)
	results := make([]vcResult, len(vcList))
	for i, target := range vcList {
		wg.Add(1)
		sem <- struct{}{}
		go func(idx int, vcTarget string) {
			defer wg.Done()
			defer func() { <-sem }()
			results[idx] = vcResult{index: idx}
			if !testVCPort(vcTarget, *flagPortTimeout) {
				return
			}
			ctx, cancel := context.WithTimeout(context.Background(), *flagTimeout)
			defer cancel()
			lines, err := collectFromVCenter(ctx, vcTarget, vcUser, password)
			if err != nil {
				return
			}
			results[idx].lines = lines
		}(i, target)
	}
	wg.Wait()

	var currentResults []string
	for _, r := range results {
		currentResults = append(currentResults, r.lines...)
	}
	outputPath := *flagOutput
	if !filepath.IsAbs(outputPath) {
		outputPath = filepath.Join(baseDir, outputPath)
	}
	timestamp := time.Now().Format("2006-01-02 15:04:05")
	var sb strings.Builder
	for _, l := range currentResults {
		sb.WriteString(l + "\n")
	}
	sb.WriteString(fmt.Sprintf("%s %s\n", timeMarker, timestamp))
	_ = os.WriteFile(outputPath, []byte(sb.String()), 0o644)
	fmt.Printf("작업이 완료되었습니다. 결과 파일: %s (총 VM %d대)\n", outputPath, len(currentResults))
}
