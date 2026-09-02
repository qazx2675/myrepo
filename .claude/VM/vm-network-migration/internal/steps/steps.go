// Package steps 는 "상태 파일의 레코드마다 VM 을 찾아 무언가를 한다"는,
// 해제/연결/검증/롤백 네 단계가 공통으로 쓰는 뼈대입니다.
package steps

import (
	"context"
	"fmt"
	"os"

	"vm-network-migration/internal/cli"
	"vm-network-migration/internal/config"
	"vm-network-migration/internal/pool"
	"vm-network-migration/internal/state"
	"vm-network-migration/internal/vsphere"
)

// Action 은 VM 한 대에 대해 수행할 작업입니다.
// 돌려주는 status 는 cli.StatusOK / StatusSkipped / StatusDryRun 중 하나이고,
// 에러를 돌려주면 호출 측이 StatusFailed 로 기록합니다.
type Action func(ctx context.Context, s *vsphere.Session, info *vsphere.VMInfo,
	rec state.Record) (status, message string, err error)

// Prepare 는 자격증명/vcenter 목록/상태 파일을 읽고 vCenter 에 접속합니다.
// 반환된 Fleet 은 호출 측이 Close 해야 합니다.
func Prepare(ctx context.Context, f *cli.Flags) (*vsphere.Fleet, *state.File, error) {
	user, pass, err := config.Credentials()
	if err != nil {
		return nil, nil, err
	}
	vcenters, err := config.LoadVCenters(f.VCenterFile)
	if err != nil {
		return nil, nil, err
	}
	sf, err := state.Load(f.StateFile)
	if err != nil {
		return nil, nil, fmt.Errorf("상태 파일을 읽을 수 없습니다(먼저 nm-backup 을 실행하세요): %w", err)
	}
	fleet, err := vsphere.ConnectFleet(ctx, vcenters, user, pass, f.Concurrency)
	if err != nil {
		return nil, nil, err
	}
	return fleet, sf, nil
}

// Run 은 레코드들을 워커 풀로 병렬 처리하고 리포트를 만듭니다.
//
// 실패는 그 VM 한 대에 갇힙니다. 한 대가 실패해도 나머지는 계속 진행하고,
// 어떤 VM 이 실패했는지는 리포트에 남아 선택적 롤백의 입력이 됩니다.
func Run(ctx context.Context, step string, fleet *vsphere.Fleet, records []state.Record,
	concurrency int, act Action) *cli.Report {

	results := make([]cli.Result, len(records))

	pool.Run(concurrency, records, func(i int, rec state.Record) {
		sess, info, err := fleet.LookupVMByUUID(rec.VMUUID, rec.VMName)
		if err != nil {
			results[i] = cli.Result{Name: rec.VMName, Status: cli.StatusFailed, Message: err.Error()}
			return
		}
		status, msg, err := act(ctx, sess, info, rec)
		if err != nil {
			results[i] = cli.Result{Name: rec.VMName, Status: cli.StatusFailed, Message: err.Error()}
			return
		}
		results[i] = cli.Result{Name: rec.VMName, Status: status, Message: msg}
	})

	return &cli.Report{Step: step, Results: results}
}

// ReadNameList 는 실패 목록 파일처럼 "이름만 한 줄에 하나" 인 파일을 읽습니다.
// 파일이 없으면 빈 목록을 돌려줍니다(실패가 없었다는 뜻).
func ReadNameList(path string) ([]string, error) {
	if path == "" {
		return nil, nil
	}
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil, nil
	}
	return config.LoadLines(path)
}
