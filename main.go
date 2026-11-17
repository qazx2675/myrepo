// awx_copy: AWX 인벤토리 variables 를 중계로 써서 파일을 옮기는 도구.
// 업로더(리눅스): awx_copy [옵션] <파일> [csv|txt]
// 다운로더(윈도우): awx_fetch.exe [옵션]   (또는 awx_copy --fetch)
package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
)

const version = "1.0.0"

type options struct {
	cfgPath       string
	outDir        string
	fetch         bool
	restore       bool
	measure       bool
	measureMaxMiB int
	snapshot      bool
	status        bool
	noPause      bool
	version       bool
	help          bool
	positional    []string
}

func usage() {
	fmt.Print(`awx_copy ` + version + ` - AWX 인벤토리 variables 를 중계로 파일 전송

사용법:
  업로더  : awx_copy [--config 경로] <파일> [csv|txt]      (type 기본 txt)
  다운로더: awx_fetch.exe [--config 경로] [--out 폴더]     (또는 awx_copy --fetch)
  복구    : awx_copy --restore [--config 경로]
  한도측정: awx_copy --measure-limits [--measure-max-mib 64]
  안전확인: awx_copy --snapshot   (작업 템플릿/인벤토리 소스 modified 출력, GET 만 함)
  상태확인: awx_copy --status     (variables 길이/SHA-256/블록 여부, GET 만 함)

설정파일 기본 경로: 리눅스 ~/.awx_copy.conf (권한 600), 윈도우 실행파일 옆 awx_copy.conf
`)
}

func parseArgs(args []string) (options, error) {
	o := options{measureMaxMiB: 64}
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "-h" || a == "--help" {
			o.help = true
			continue
		}
		if !strings.HasPrefix(a, "--") || a == "--" {
			o.positional = append(o.positional, a)
			continue
		}
		name, val, hasVal := strings.Cut(a[2:], "=")
		need := func() (string, error) {
			if hasVal {
				return val, nil
			}
			if i+1 >= len(args) {
				return "", fmt.Errorf("--%s 에 값이 필요합니다", name)
			}
			i++
			return args[i], nil
		}
		var err error
		switch name {
		case "config":
			o.cfgPath, err = need()
		case "out":
			o.outDir, err = need()
		case "fetch":
			o.fetch = true
		case "restore":
			o.restore = true
		case "measure-limits":
			o.measure = true
		case "measure-max-mib":
			var s string
			if s, err = need(); err == nil {
				o.measureMaxMiB, err = strconv.Atoi(s)
			}
		case "snapshot":
			o.snapshot = true
		case "status":
			o.status = true
		case "no-pause":
			o.noPause = true
		case "version":
			o.version = true
		default:
			return o, fmt.Errorf("알 수 없는 옵션 --%s", name)
		}
		if err != nil {
			return o, err
		}
	}
	return o, nil
}

func main() {
	code := run(os.Args)
	os.Exit(code)
}

func run(argv []string) int {
	setupConsole()
	o, err := parseArgs(argv[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, "오류:", err)
		usage()
		return 2
	}
	if o.help {
		usage()
		return 0
	}
	if o.version {
		fmt.Println("awx_copy", version, runtime.GOOS+"/"+runtime.GOARCH, runtime.Version())
		return 0
	}
	exe := strings.ToLower(filepath.Base(argv[0]))
	fetchMode := o.fetch || strings.Contains(exe, "fetch")
	pause := runtime.GOOS == "windows" && fetchMode && len(argv) == 1 && !o.noPause
	if pause {
		defer func() {
			fmt.Print("\nEnter 키를 누르면 창을 닫습니다...")
			_, _ = bufio.NewReader(os.Stdin).ReadString('\n')
		}()
	}

	cfg, err := loadConfig(o.cfgPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "오류:", err)
		return 2
	}
	c := newClient(cfg)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	switch {
	case o.restore:
		err = runRestore(ctx, c, cfg, os.Stdout)
	case o.measure:
		err = measureLimits(ctx, c, o.measureMaxMiB, os.Stdout)
	case o.snapshot:
		err = snapshot(ctx, c, os.Stdout)
	case o.status:
		var v string
		if v, err = c.GetVars(ctx); err == nil {
			b, _ := parseBlock(v)
			fmt.Printf("variables len=%d sha256=%s block=%v session=%s state=%s\n", len(v), sha256Hex([]byte(v)), hasBlock(v), b["session"], b["state"])
		}
	case fetchMode:
		_, err = fetchFile(ctx, c, cfg, o.outDir, os.Stdout)
	default:
		if len(o.positional) < 1 || len(o.positional) > 2 {
			usage()
			return 2
		}
		typ := "txt"
		if len(o.positional) == 2 {
			typ = strings.ToLower(o.positional[1])
		}
		err = sendFile(ctx, c, cfg, o.positional[0], typ, os.Stdout)
	}
	if err != nil {
		if errors.Is(err, context.Canceled) {
			fmt.Fprintln(os.Stderr, "중단됨 (Ctrl+C). variables 원복을 시도했습니다. 남아 있으면 --restore 를 실행하세요.")
			return 130
		}
		fmt.Fprintln(os.Stderr, "오류:", err)
		return 1
	}
	return 0
}
