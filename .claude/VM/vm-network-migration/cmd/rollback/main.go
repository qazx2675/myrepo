// nm-rollback — 롤백 (역순 실행)
//
// state_{user}.json 에 백업해 둔 작업 전 상태로 되돌립니다.
// 계획서의 역순 실행 순서를 그대로 따릅니다.
//
//	(Step 3-Undo) 신규 포트그룹 연결을 해제한다
//	(Step 1-Undo) 백업에 기록된 원본 포트그룹으로 다시 연결한다
//
// 기본은 상태 파일의 전체 VM 이지만, -vm 또는 -only-file 로 실패한 VM 만 골라
// 선택적으로 되돌릴 수 있습니다. run.sh 는 실패 목록 파일을 -only-file 로 넘깁니다.
//
// 생성한 포트그룹은 지우지 않습니다. 다른 VM 이 이미 그 포트그룹을 쓰고 있을 수
// 있고, 비어 있는 포트그룹이 남아 있는 것은 무해하기 때문입니다.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

	"vm-network-migration/internal/cli"
	"vm-network-migration/internal/state"
	"vm-network-migration/internal/steps"
	"vm-network-migration/internal/vsphere"
)

// nameList 는 -vm 을 여러 번 줄 수 있게 하는 플래그 타입입니다.
type nameList []string

func (n *nameList) String() string     { return strings.Join(*n, ",") }
func (n *nameList) Set(v string) error { *n = append(*n, v); return nil }

func main() { os.Exit(run()) }

func run() int {
	fs := flag.NewFlagSet("nm-rollback", flag.ExitOnError)
	f := cli.Register(fs)
	var only nameList
	fs.Var(&only, "vm", "이 VM 만 롤백 (여러 번 지정 가능). 미지정 시 상태 파일 전체")
	onlyFile := fs.String("only-file", "", "롤백할 VM 이름 목록 파일 (보통 failed_{user}.txt)")
	prune := fs.Bool("prune", false, "롤백에 성공한 VM 을 상태 파일에서 제거해 이후 단계의 대상에서 뺍니다")
	_ = fs.Parse(os.Args[1:])

	if f.ShowVersion {
		fmt.Println("nm-rollback", cli.Version)
		return cli.ExitOK
	}
	if err := f.Resolve(); err != nil {
		return cli.Usage("%v", err)
	}

	fromFile, err := steps.ReadNameList(*onlyFile)
	if err != nil {
		return cli.Usage("%v", err)
	}
	targets := append([]string(only), fromFile...)

	ctx, cancel := context.WithTimeout(context.Background(), f.Timeout)
	defer cancel()

	fleet, sf, err := steps.Prepare(ctx, f)
	if err != nil {
		return cli.Usage("%v", err)
	}
	defer fleet.Close(context.Background())

	records, err := sf.Filter(targets)
	if err != nil {
		return cli.Usage("%v", err)
	}
	if len(records) == 0 {
		fmt.Println("[INFO] 롤백할 대상이 없습니다.")
		return cli.ExitOK
	}

	scope := "전체"
	if len(targets) > 0 {
		scope = "선택"
	}
	fmt.Printf("[롤백] 작업 전 상태로 원복 — 대상 %d대 (%s, 동시 %d)\n",
		len(records), scope, f.Concurrency)

	rep := steps.Run(ctx, "롤백", fleet, records, f.Concurrency,
		func(ctx context.Context, s *vsphere.Session, info *vsphere.VMInfo, rec state.Record) (string, string, error) {
			if rec.OrigPG == "" {
				return "", "", fmt.Errorf("백업에 원본 포트그룹이 없어 자동 원복할 수 없습니다(수동 확인 필요)")
			}
			if f.DryRun {
				return cli.StatusDryRun,
					fmt.Sprintf("%s -> %s 원복 예정", rec.TargetPG, rec.OrigPG), nil
			}

			// (Step 3-Undo) 먼저 끊는다. 끊지 않고 백킹만 바꾸면 원본 포트그룹으로
			// 돌아가는 순간 게스트가 잘못된 상태로 트래픽을 흘릴 수 있습니다.
			if _, err := s.SetConnected(ctx, info, sf.NicIndex, rec.NicKey, false); err != nil {
				return "", "", fmt.Errorf("연결 해제(3-Undo) 실패: %w", err)
			}

			// (Step 1-Undo) 원본 포트그룹으로 되돌리고 원래 연결 상태를 복원한다.
			changed, err := s.SetPortgroup(ctx, info, sf.NicIndex, rec.NicKey,
				rec.OrigPG, rec.OrigConnected, rec.OrigStartConnected)
			if err != nil {
				return "", "", fmt.Errorf("원본 포트그룹 재연결(1-Undo) 실패: %w", err)
			}
			if !changed {
				return cli.StatusSkipped, fmt.Sprintf("이미 %s 상태", rec.OrigPG), nil
			}
			return cli.StatusOK, fmt.Sprintf("%s -> %s 원복", rec.TargetPG, rec.OrigPG), nil
		})

	rep.Print()
	// 롤백 결과의 실패 목록은 원래 실패 목록을 덮어쓰면 안 되므로 별도 파일에 남깁니다.
	code := rep.Finish("rollback_failed_" + f.User + ".txt")

	// 원본으로 되돌린 VM 은 이번 작업의 대상이 아니므로 상태 파일에서 뺍니다.
	// 그래야 남은 VM 만으로 뒤 단계를 이어서 진행할 수 있습니다.
	if *prune && !f.DryRun {
		var done []string
		for _, r := range rep.Results {
			if r.Status == cli.StatusOK || r.Status == cli.StatusSkipped {
				done = append(done, r.Name)
			}
		}
		if n := sf.Remove(done); n > 0 {
			if err := state.Save(f.StateFile, sf); err != nil {
				fmt.Fprintf(os.Stderr, "[경고] 상태 파일 갱신 실패: %v\n", err)
			} else {
				fmt.Printf("[INFO] 원복 완료한 %d대를 상태 파일에서 제외했습니다. 남은 대상 %d대.\n",
					n, len(sf.Records))
			}
		}
	}

	if code != cli.ExitOK {
		fmt.Fprintln(os.Stderr, "\n[경고] 자동 롤백에 실패한 VM 이 있습니다. 수동 확인이 필요합니다.")
	}
	return code
}
