// vm-network-migration: vCenter 상의 여러 VM 을 지정한 포트그룹으로 일괄 이관하는 도구.
//
// 포트그룹 자체는 삭제하지 않으며, VM 의 가상 NIC 백킹(backing)만 교체합니다.
// 포트그룹 "생성"은 이 도구의 역할이 아닙니다. 이미 만들어진 포트그룹을 전제로 하며,
// 필요하면 -pg-cmd 로 외부 생성 도구를 실행 전에 한 번 호출할 수 있습니다.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/vmware/govmomi/find"
	"github.com/vmware/govmomi/vim25/types"
)

const version = "0.1.0"

func main() {
	os.Exit(run())
}

func run() int {
	var (
		vcenterFile = flag.String("vcenter-file", "vcenter.txt", "vCenter 접속 설정 파일")
		vmFile      = flag.String("vm-file", "vmlist.txt", "대상 VM 이름 목록 파일")
		toPG        = flag.String("to-portgroup", "", "이관할 신규 포트그룹 이름 (롤백 모드가 아니면 필수)")
		fromPG      = flag.String("from-portgroup", "", "이 포트그룹에 연결된 NIC 만 대상으로 변경")
		nicIndex    = flag.Int("nic-index", 0, "-from-portgroup 미지정 시 변경할 NIC 순번(0=첫 번째)")
		concurrency = flag.Int("concurrency", 8, "동시에 처리할 VM 수")
		dryRun      = flag.Bool("dry-run", false, "실제 변경 없이 무엇이 바뀔지만 출력")
		rollbackCSV = flag.String("rollback", "", "롤백 CSV 경로. 지정하면 롤백 모드로 동작")
		pgCmd       = flag.String("pg-cmd", "", "마이그레이션 전 1회 실행할 포트그룹 생성 명령. {{PG}} 가 포트그룹 이름으로 치환됨")
		vmTimeout   = flag.Duration("vm-timeout", 3*time.Minute, "VM 1대당 최대 처리 시간")
		outDir      = flag.String("out-dir", ".", "리포트/롤백 CSV 를 저장할 디렉터리")
		showVersion = flag.Bool("version", false, "버전 출력 후 종료")
	)
	flag.Parse()

	if *showVersion {
		fmt.Println("vm-network-migration", version)
		return 0
	}

	rollbackMode := *rollbackCSV != ""
	if !rollbackMode && *toPG == "" {
		fmt.Fprintln(os.Stderr, "오류: -to-portgroup 은 필수입니다. (-rollback 사용 시 제외)")
		flag.Usage()
		return 2
	}
	if *concurrency < 1 {
		*concurrency = 1
	}

	cfg, err := loadVCenterConfig(*vcenterFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "오류: vCenter 설정을 읽을 수 없습니다: %v\n", err)
		return 2
	}

	// 처리 대상 결정: 일반 모드는 VM 목록 파일, 롤백 모드는 롤백 CSV.
	type target struct {
		name   string
		nicKey int32
		useKey bool
		toPG   string
	}
	var targets []target

	if rollbackMode {
		entries, err := readRollbackCSV(*rollbackCSV)
		if err != nil {
			fmt.Fprintf(os.Stderr, "오류: 롤백 파일을 읽을 수 없습니다: %v\n", err)
			return 2
		}
		for _, e := range entries {
			targets = append(targets, target{name: e.VMName, nicKey: e.NicKey, useKey: true, toPG: e.OldPG})
		}
		fmt.Printf("롤백 모드: %s 기준 %d건을 되돌립니다.\n", *rollbackCSV, len(targets))
	} else {
		names, dup, err := readVMList(*vmFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "오류: VM 목록을 읽을 수 없습니다: %v\n", err)
			return 2
		}
		if len(names) == 0 {
			fmt.Fprintln(os.Stderr, "오류: 대상 VM 이 한 대도 없습니다.")
			return 2
		}
		if dup > 0 {
			fmt.Printf("알림: 중복된 VM 이름 %d건을 제거했습니다.\n", dup)
		}
		for _, n := range names {
			targets = append(targets, target{name: n, toPG: *toPG})
		}
		fmt.Printf("대상 VM %d대를 %s 포트그룹으로 이관합니다.\n", len(targets), *toPG)
	}

	ctx := context.Background()

	// 포트그룹 생성은 외부 도구에 위임합니다. 전체 작업 전에 딱 한 번만 실행합니다.
	if *pgCmd != "" && !rollbackMode {
		if *dryRun {
			fmt.Printf("[dry-run] 포트그룹 생성 명령 생략: %s\n", strings.ReplaceAll(*pgCmd, "{{PG}}", *toPG))
		} else {
			cmdline := strings.ReplaceAll(*pgCmd, "{{PG}}", *toPG)
			fmt.Printf("포트그룹 생성 명령 실행: %s\n", cmdline)
			out, err := exec.CommandContext(ctx, "/bin/bash", "-c", cmdline).CombinedOutput()
			if len(out) > 0 {
				fmt.Printf("--- 명령 출력 ---\n%s\n-----------------\n", strings.TrimRight(string(out), "\n"))
			}
			if err != nil {
				fmt.Fprintf(os.Stderr, "오류: 포트그룹 생성 명령 실패: %v\n", err)
				return 1
			}
		}
	}

	client, err := connectVCenter(ctx, cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "오류: vCenter(%s) 접속 실패: %v\n", cfg.Host, err)
		return 1
	}
	defer client.Logout(ctx)

	finder := find.NewFinder(client.Client, true)
	dc, err := finder.DatacenterOrDefault(ctx, cfg.Datacenter)
	if err != nil {
		fmt.Fprintf(os.Stderr, "오류: 데이터센터(%s) 조회 실패: %v\n", cfg.Datacenter, err)
		return 1
	}
	finder.SetDatacenter(dc)

	vmIndex, dupNames, err := buildVMIndex(ctx, client, dc)
	if err != nil {
		fmt.Fprintf(os.Stderr, "오류: VM 인벤토리 조회 실패: %v\n", err)
		return 1
	}
	if len(dupNames) > 0 {
		fmt.Printf("경고: 인벤토리에 같은 이름의 VM 이 여러 대 있습니다: %s\n", strings.Join(dupNames, ", "))
	}

	netIndex, err := buildNetworkIndex(ctx, client, dc)
	if err != nil {
		fmt.Fprintf(os.Stderr, "오류: 네트워크 색인 생성 실패: %v\n", err)
		return 1
	}

	// 필요한 포트그룹의 백킹 정보를 미리 전부 확인합니다.
	// 여기서 실패하면 VM 은 한 대도 건드리지 않은 상태이므로 안전합니다.
	backings := map[string]types.BaseVirtualDeviceBackingInfo{}
	for _, t := range targets {
		if _, ok := backings[t.toPG]; ok {
			continue
		}
		b, err := resolveBacking(ctx, finder, t.toPG)
		if err != nil {
			fmt.Fprintf(os.Stderr, "오류: %v\n", err)
			fmt.Fprintln(os.Stderr, "      포트그룹이 먼저 생성되어 있어야 합니다. VM 은 변경되지 않았습니다.")
			return 1
		}
		backings[t.toPG] = b
	}

	// 병렬 처리(동시 실행 수 제한). 전 VM 동시 변경은 장애 시 피해가 커서 기본 8로 제한합니다.
	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		results = make([]Result, 0, len(targets))
		sem     = make(chan struct{}, *concurrency)
	)

	for _, t := range targets {
		wg.Add(1)
		go func(t target) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			entry, ok := vmIndex[t.name]
			if !ok {
				mu.Lock()
				results = append(results, Result{
					VMName: t.name, ToPG: t.toPG, Status: StatusFailed,
					Message: "인벤토리에서 VM 을 찾을 수 없습니다",
				})
				mu.Unlock()
				return
			}

			vmCtx, cancel := context.WithTimeout(ctx, *vmTimeout)
			defer cancel()

			res := migrateVM(vmCtx, client, netIndex, entry.Ref, t.name, migrateOptions{
				ToPortgroup: t.toPG,
				FromFilter:  *fromPG,
				NicIndex:    *nicIndex,
				NicKey:      t.nicKey,
				UseNicKey:   t.useKey,
				Backing:     backings[t.toPG],
				DryRun:      *dryRun,
			})

			mu.Lock()
			results = append(results, res)
			mu.Unlock()
		}(t)
	}
	wg.Wait()

	failed := printSummary(results)

	// 리포트와 롤백 파일 저장
	stamp := time.Now().Format("20060102_150405")
	reportPath := filepath.Join(*outDir, fmt.Sprintf("report_%s.csv", stamp))
	if err := writeReportCSV(reportPath, results); err != nil {
		fmt.Fprintf(os.Stderr, "경고: 리포트 저장 실패: %v\n", err)
	} else {
		fmt.Printf("리포트: %s\n", reportPath)
	}

	if !*dryRun && !rollbackMode {
		rbPath := filepath.Join(*outDir, fmt.Sprintf("rollback_%s.csv", stamp))
		n, err := writeRollbackCSV(rbPath, results)
		if err != nil {
			fmt.Fprintf(os.Stderr, "경고: 롤백 파일 저장 실패: %v\n", err)
		} else if n > 0 {
			fmt.Printf("롤백 파일: %s (%d건)\n", rbPath, n)
			fmt.Printf("되돌리려면: ./vm-network-migration -rollback=%s\n", rbPath)
		}
	}

	if failed > 0 {
		return 1
	}
	return 0
}
