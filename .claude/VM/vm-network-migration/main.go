// vm-network-migration: 여러 vCenter 에 걸친 VM 들의 가상 NIC 를
// 지정한 포트그룹으로 일괄 이관하는 도구.
//
// 포트그룹 자체는 삭제하지 않으며, VM 의 가상 NIC 백킹(backing)만 교체합니다.
// 포트그룹 "생성"은 이 도구의 역할이 아닙니다. 이미 만들어진 포트그룹을 전제로 하며,
// 필요하면 -pg-cmd 로 외부 생성 도구를 실행 전에 한 번 호출할 수 있습니다.
//
// vCenter 주소는 vcenter.txt 에 한 줄에 하나씩 적고, 계정은 VC_USER / VC_PASS
// 환경 변수로 받습니다(vm-param-check 와 동일한 규약).
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/vmware/govmomi/vim25/types"
)

const version = "0.2.0"

// vmLocation 은 어떤 VM 이 어느 vCenter 의 어느 데이터센터에 있는지를 가리킵니다.
type vmLocation struct {
	vcIdx int
	dcIdx int
	ref   types.ManagedObjectReference
}

// task 는 처리할 VM 한 건입니다.
type task struct {
	vcenter string // 롤백 모드에서 대상 vCenter 를 고정할 때 사용(빈 값이면 전체에서 탐색)
	name    string
	nicKey  int32
	useKey  bool
	toPG    string
}

func main() {
	os.Exit(run())
}

func run() int {
	var (
		vcenterFile = flag.String("vcenter-file", "vcenter.txt", "vCenter 주소 목록 파일 (한 줄에 하나)")
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

	// 계정은 파일이 아니라 환경 변수에서 읽습니다.
	vcUser, vcPass, err := loadCredentials()
	if err != nil {
		fmt.Fprintf(os.Stderr, "오류: 인증 정보 로드 실패: %v\n", err)
		fmt.Fprintln(os.Stderr, "      예) export VC_USER='administrator@vsphere.local'; read -rs VC_PASS; export VC_PASS")
		return 2
	}

	vcenters, dupVC, err := loadVCenterList(*vcenterFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "오류: vCenter 목록을 읽을 수 없습니다: %v\n", err)
		return 2
	}
	if dupVC > 0 {
		fmt.Printf("알림: 중복된 vCenter 주소 %d건을 제거했습니다.\n", dupVC)
	}
	multiVC := len(vcenters) > 1

	// 처리 대상 결정: 일반 모드는 VM 목록 파일, 롤백 모드는 롤백 CSV.
	var tasks []task
	if rollbackMode {
		entries, err := readRollbackCSV(*rollbackCSV)
		if err != nil {
			fmt.Fprintf(os.Stderr, "오류: 롤백 파일을 읽을 수 없습니다: %v\n", err)
			return 2
		}
		for _, e := range entries {
			tasks = append(tasks, task{vcenter: e.VCenter, name: e.VMName, nicKey: e.NicKey, useKey: true, toPG: e.OldPG})
		}
		fmt.Printf("롤백 모드: %s 기준 %d건을 되돌립니다.\n", *rollbackCSV, len(tasks))
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
			tasks = append(tasks, task{name: n, toPG: *toPG})
		}
		fmt.Printf("vCenter %d대 / 대상 VM %d대를 %s 포트그룹으로 이관합니다.\n", len(vcenters), len(tasks), *toPG)
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

	// ---- 1단계: 모든 vCenter 를 동시에 접속·조사 ----
	// vCenter 끼리, 그리고 각 vCenter 안의 데이터센터끼리도 동시에 조회합니다.
	fmt.Printf("vCenter %d대를 동시에 조회합니다...\n", len(vcenters))
	conns := make([]*vcConn, len(vcenters))
	var surveyWG sync.WaitGroup
	for i, addr := range vcenters {
		surveyWG.Add(1)
		go func(i int, addr string) {
			defer surveyWG.Done()
			conns[i] = surveyVCenter(ctx, addr, vcUser, vcPass)
		}(i, addr)
	}
	surveyWG.Wait()

	defer func() {
		for _, c := range conns {
			c.Close(ctx)
		}
	}()

	// 한 대라도 조회에 실패하면 중단합니다.
	// 그대로 진행하면 그 vCenter 의 VM 이 "찾을 수 없음"으로 잘못 보고되기 때문입니다.
	failedVC := false
	for _, c := range conns {
		if c.Err != nil {
			fmt.Fprintf(os.Stderr, "오류: vCenter %s %v\n", c.Addr, c.Err)
			failedVC = true
			continue
		}
		names := make([]string, len(c.DCs))
		for i, d := range c.DCs {
			names[i] = d.Name
			if len(d.Dups) > 0 {
				fmt.Printf("경고: %s / %s 에 이름이 겹치는 VM 이 있습니다: %s\n",
					c.Addr, d.Name, strings.Join(d.Dups, ", "))
			}
		}
		fmt.Printf("  %s : 데이터센터 %d개 (%s)\n", c.Addr, len(c.DCs), strings.Join(names, ", "))
	}
	if failedVC {
		fmt.Fprintln(os.Stderr, "      VM 을 잘못 판정할 수 있어 중단합니다. VM 은 변경되지 않았습니다.")
		return 1
	}

	// ---- 2단계: VM 이름 -> 위치(vCenter/데이터센터) 해석 ----
	locations := map[string][]vmLocation{}
	for vi, c := range conns {
		for di, d := range c.DCs {
			for name, ref := range d.VMs {
				locations[name] = append(locations[name], vmLocation{vcIdx: vi, dcIdx: di, ref: ref})
			}
		}
	}

	type resolved struct {
		task task
		loc  vmLocation
	}
	var (
		ready    []resolved
		earlyRes []Result // 위치를 못 정한 대상은 여기서 바로 실패 처리
	)
	for _, t := range tasks {
		cands := locations[t.name]
		// 롤백 모드는 CSV 에 적힌 vCenter 로 후보를 좁힙니다.
		if t.vcenter != "" {
			var filtered []vmLocation
			for _, l := range cands {
				if conns[l.vcIdx].Addr == t.vcenter {
					filtered = append(filtered, l)
				}
			}
			cands = filtered
		}

		switch {
		case len(cands) == 0:
			where := "전체 vCenter"
			if t.vcenter != "" {
				where = t.vcenter
			}
			earlyRes = append(earlyRes, Result{
				VCenter: t.vcenter, VMName: t.name, ToPG: t.toPG, Status: StatusFailed,
				Message: fmt.Sprintf("%s 인벤토리에서 VM 을 찾을 수 없습니다", where),
			})
		case len(cands) > 1:
			var at []string
			for _, l := range cands {
				at = append(at, fmt.Sprintf("%s/%s", conns[l.vcIdx].Addr, conns[l.vcIdx].DCs[l.dcIdx].Name))
			}
			sort.Strings(at)
			earlyRes = append(earlyRes, Result{
				VMName: t.name, ToPG: t.toPG, Status: StatusFailed,
				Message: fmt.Sprintf("이름이 여러 곳에 있어 대상을 특정할 수 없습니다: %s", strings.Join(at, ", ")),
			})
		default:
			ready = append(ready, resolved{task: t, loc: cands[0]})
		}
	}

	// ---- 3단계: 필요한 포트그룹의 백킹 정보를 미리 전부 확인 (동시 실행) ----
	// 여기서 실패하면 VM 은 한 대도 건드리지 않은 상태이므로 안전합니다.
	type pgKey struct {
		vcIdx int
		dcIdx int
		pg    string
	}
	needed := map[pgKey]bool{}
	for _, r := range ready {
		needed[pgKey{r.loc.vcIdx, r.loc.dcIdx, r.task.toPG}] = true
	}

	var (
		keys     []pgKey
		backings = map[pgKey]types.BaseVirtualDeviceBackingInfo{}
		pgErrs   = map[pgKey]error{}
		pgMu     sync.Mutex
		pgWG     sync.WaitGroup
	)
	for k := range needed {
		keys = append(keys, k)
	}
	for _, k := range keys {
		pgWG.Add(1)
		go func(k pgKey) {
			defer pgWG.Done()
			c := conns[k.vcIdx]
			b, err := resolveBacking(ctx, c.Client, c.DCs[k.dcIdx].DC, k.pg)
			pgMu.Lock()
			defer pgMu.Unlock()
			if err != nil {
				pgErrs[k] = err
			} else {
				backings[k] = b
			}
		}(k)
	}
	pgWG.Wait()

	if len(pgErrs) > 0 {
		for k, err := range pgErrs {
			fmt.Fprintf(os.Stderr, "오류: %s / %s : %v\n", conns[k.vcIdx].Addr, conns[k.vcIdx].DCs[k.dcIdx].Name, err)
		}
		fmt.Fprintln(os.Stderr, "      포트그룹이 먼저 생성되어 있어야 합니다. VM 은 변경되지 않았습니다.")
		return 1
	}

	// ---- 4단계: VM 별 처리 (동시 실행 수 제한) ----
	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		results = append([]Result{}, earlyRes...)
		sem     = make(chan struct{}, *concurrency)
	)

	for _, r := range ready {
		wg.Add(1)
		go func(r resolved) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			c := conns[r.loc.vcIdx]
			d := c.DCs[r.loc.dcIdx]

			vmCtx, cancel := context.WithTimeout(ctx, *vmTimeout)
			defer cancel()

			res := migrateVM(vmCtx, c.Client, d.Net, r.loc.ref, r.task.name, migrateOptions{
				ToPortgroup: r.task.toPG,
				FromFilter:  *fromPG,
				NicIndex:    *nicIndex,
				NicKey:      r.task.nicKey,
				UseNicKey:   r.task.useKey,
				Backing:     backings[pgKey{r.loc.vcIdx, r.loc.dcIdx, r.task.toPG}],
				DryRun:      *dryRun,
			})
			res.VCenter = c.Addr
			res.Datacenter = d.Name

			mu.Lock()
			results = append(results, res)
			mu.Unlock()
		}(r)
	}
	wg.Wait()

	failed := printSummary(results, multiVC)

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
