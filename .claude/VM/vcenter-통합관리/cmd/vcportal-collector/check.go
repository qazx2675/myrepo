package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"vcportal/internal/collect"
	"vcportal/internal/conf"
)

// runCheck 는 데이터를 쓰지 않고 설정/경로/vCenter 로그인만 점검한다.
func runCheck(cfg *conf.Config) int {
	fmt.Println("== 설정 ==")
	fmt.Printf("conf         : %s\n", cfg.Path)
	fmt.Printf("output_dir   : %s\n", cfg.OutputDir)
	fmt.Printf("work_dir     : %s\n", cfg.WorkDir)
	fmt.Printf("log_dir      : %s\n", cfg.LogDir)
	fmt.Printf("parallel     : %d\n", cfg.Parallel)
	fmt.Printf("timeout      : %s\n", cfg.Timeout)
	fmt.Printf("user         : %s\n", cfg.User)
	fmt.Printf("password     : %s\n", "********")
	fmt.Printf("browser      : %s\n", cfg.Browser)
	for _, vc := range cfg.VCenters {
		fmt.Printf("vcenter      : %s = %s\n", vc.ID, vc.URL)
	}

	fmt.Println("\n== 점검 ==")
	bad := 0
	report := func(name string, err error, okMsg string) {
		if err != nil {
			bad++
			fmt.Printf("FAIL %s: %v\n", name, err)
			return
		}
		fmt.Printf("OK   %s%s\n", name, okMsg)
	}

	for _, p := range []string{cfg.OutputDir, cfg.WorkDir, cfg.LogDir} {
		if p != "" {
			report("드라이브 경로 "+p, conf.CheckNotMappedDrive(p), "")
		}
	}
	report("output_dir 쓰기", checkWritable(cfg.OutputDir), "")
	report("work_dir 생성", os.MkdirAll(cfg.WorkDir, 0o755), "")
	if cfg.LogDir != "" {
		report("log_dir 생성", os.MkdirAll(cfg.LogDir, 0o755), "")
	}

	for _, vc := range cfg.VCenters {
		ctx, cancel := ctxTimeout(cfg.Timeout)
		c, err := collect.Connect(ctx, vc, cfg.User, cfg.Password)
		if err != nil {
			cancel()
			report("vCenter "+vc.ID+" 로그인", err, "")
			continue
		}
		a := c.ServiceContent.About
		collect.Logout(c)
		cancel()
		report("vCenter "+vc.ID+" 로그인", nil, fmt.Sprintf(" (%s, version %s, build %s)", vc.URL, a.Version, a.Build))
	}

	if bad > 0 {
		fmt.Printf("\n점검 결과: 실패 %d건\n", bad)
		return 1
	}
	fmt.Println("\n점검 결과: 모두 정상")
	return 0
}

// checkWritable 은 폴더가 있고 임시 파일을 만들고 지울 수 있는지 확인한다.
func checkWritable(dir string) error {
	st, err := os.Stat(dir)
	if err != nil {
		return err
	}
	if !st.IsDir() {
		return fmt.Errorf("폴더가 아닙니다: %s", dir)
	}
	p := filepath.Join(dir, fmt.Sprintf(".vcportal-check-%d-%d", os.Getpid(), time.Now().UnixNano()))
	f, err := os.Create(p)
	if err != nil {
		return err
	}
	f.Close()
	return os.Remove(p)
}
