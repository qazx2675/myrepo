// vcportal-collector 는 모든 vCenter 를 병렬로 조회해 공유폴더 data\ 에 .js 파일을 만든다.
package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"vcportal/internal/collect"
	"vcportal/internal/conf"
)

// 종료 코드: 0 전부 성공, 1 conf/경로 오류, 2 일부 실패, 3 전부 실패.
func main() { os.Exit(run()) }

func run() int {
	exe, _ := os.Executable()
	confPath := flag.String("conf", filepath.Join(filepath.Dir(exe), "vcportal.conf"), "vcportal.conf 경로")
	check := flag.Bool("check", false, "설정/경로/vCenter 접속만 점검하고 종료")
	flag.Parse()

	cfg, err := conf.Load(*confPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "설정 오류:", err)
		return 1
	}
	if *check {
		return runCheck(cfg)
	}
	for _, p := range []string{cfg.OutputDir, cfg.WorkDir, cfg.LogDir} {
		if p == "" {
			continue
		}
		if err := conf.CheckNotMappedDrive(p); err != nil {
			fmt.Fprintln(os.Stderr, "경로 오류:", err)
			return 1
		}
	}

	logw := io.Writer(os.Stdout)
	if cfg.LogDir != "" {
		if err := os.MkdirAll(cfg.LogDir, 0o755); err != nil {
			fmt.Fprintln(os.Stderr, "log_dir 를 만들 수 없습니다:", err)
			return 1
		}
		f, err := os.OpenFile(filepath.Join(cfg.LogDir, "collector-"+time.Now().Format("20060102")+".log"),
			os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			fmt.Fprintln(os.Stderr, "로그 파일을 열 수 없습니다:", err)
			return 1
		}
		defer f.Close()
		logw = io.MultiWriter(os.Stdout, f)
	}
	lg := log.New(logw, "", log.LstdFlags)
	return collectAll(cfg, lg)
}

type outcome struct {
	res     *collect.Result
	err     error
	elapsed time.Duration
}

// hostname 은 URL 에서 포트를 뺀 호스트명(트리 최상위 표시명).
func hostname(vc conf.VCenter) string {
	if u, err := url.Parse(vc.URL); err == nil && u.Hostname() != "" {
		return u.Hostname()
	}
	return vc.Host
}

func collectAll(cfg *conf.Config, lg *log.Logger) int {
	start := time.Now()
	lg.Printf("수집 시작: vCenter %d대, 동시 %d, 제한시간 %s (conf: %s)", len(cfg.VCenters), cfg.Parallel, cfg.Timeout, cfg.Path)

	outDir := filepath.Join(cfg.WorkDir, "out")
	cacheDir := filepath.Join(cfg.WorkDir, "cache")
	_ = os.RemoveAll(outDir)
	for _, d := range []string{outDir, cacheDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			lg.Printf("work_dir 를 만들 수 없습니다: %v", err)
			return 1
		}
	}

	outs := make([]outcome, len(cfg.VCenters))
	sem := make(chan struct{}, cfg.Parallel)
	var wg sync.WaitGroup
	for i, vc := range cfg.VCenters {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			t0 := time.Now()
			ctx, cancel := ctxTimeout(cfg.Timeout)
			defer cancel()
			res, err := collect.Collect(ctx, vc, cfg.User, cfg.Password, hostname(vc))
			outs[i] = outcome{res, err, time.Since(t0).Round(time.Millisecond)}
		}()
	}
	wg.Wait()

	var files []string // out 폴더에 만든 파일 이름 (data/<id>.js 먼저)
	var index [][]string
	var entries []manifestVC
	failed := 0
	for i, vc := range cfg.VCenters {
		o := outs[i]
		e := manifestVC{ID: vc.ID, Name: hostname(vc), URL: vc.URL}
		var cached *collect.Result
		if o.err == nil {
			cached = o.res
			e.Status = "ok"
			if err := saveCache(cacheDir, vc.ID, o.res); err != nil {
				lg.Printf("[%s] 캐시 저장 실패(수집 결과는 정상 사용): %v", vc.ID, err)
			}
			if err := writeDetail(outDir, vc.ID, o.res); err != nil {
				lg.Printf("work_dir 에 쓸 수 없습니다: %v", err)
				return 1
			}
			files = append(files, vc.ID+".js")
			lg.Printf("[%s] 성공 hosts=%d vms=%d clusters=%d (%s)", vc.ID, o.res.Counts.Hosts, o.res.Counts.VMs, o.res.Counts.Clusters, o.elapsed)
		} else {
			failed++
			e.Status = "fail"
			e.Error = oneLine(o.err.Error())
			cached = loadCache(cacheDir, vc.ID)
			if cached != nil {
				lg.Printf("[%s] 실패 (%s): %s - 직전 성공(%s) 데이터를 유지합니다", vc.ID, o.elapsed, e.Error, cached.CollectedAt)
			} else {
				lg.Printf("[%s] 실패 (%s): %s - 직전 성공 이력이 없습니다", vc.ID, o.elapsed, e.Error)
			}
		}
		if cached != nil {
			e.InstanceUUID, e.Version, e.Build = cached.InstanceUUID, cached.Version, cached.Build
			e.CollectedAt = cached.CollectedAt
			e.Counts = cached.Counts
			index = append(index, cached.Index...)
		}
		entries = append(entries, e)
	}
	if err := writeIndexAndManifest(outDir, index, entries); err != nil {
		lg.Printf("work_dir 에 쓸 수 없습니다: %v", err)
		return 1
	}
	files = append(files, "index.js", "manifest.js")

	if err := publish(outDir, cfg.OutputDir, files); err != nil {
		lg.Printf("output_dir 배포 실패: %v", err)
		return 1
	}
	lg.Printf("배포 완료: %s", filepath.Join(cfg.OutputDir, "data"))

	lg.Printf("전체: 성공 %d / 실패 %d (%s)", len(cfg.VCenters)-failed, failed, time.Since(start).Round(time.Millisecond))
	switch {
	case failed == 0:
		return 0
	case failed == len(cfg.VCenters):
		return 3
	}
	return 2
}

func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
