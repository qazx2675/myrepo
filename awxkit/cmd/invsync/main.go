// awxkit-invsync는 [S2] 인벤토리 소스 동기화를 트리거하고, 완료되면 등록된 호스트 목록을 보여준다.
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"

	"awxkit/awx"
	"awxkit/cli"
	"awxkit/config"
)

func main() {
	confFlag := flag.String("conf", "", "설정 파일 경로를 직접 지정합니다")
	userFlag := flag.String("user", "", "사용자 식별자를 직접 지정합니다 (AWXKIT_USER 환경변수보다 낮은 우선순위)")
	fileFlag := flag.String("file", "", "git에 이미 업로드된 인벤토리 yaml 파일명 (필수) — 소스의 s2_source_field에 저장됨")
	flag.Parse()

	user := config.ResolveUser(*userFlag)
	confPath, err := config.ResolvePath(*confFlag, user)
	if err != nil {
		fmt.Fprintf(os.Stderr, cli.MarkFail+" 설정 파일을 찾을 수 없습니다: %v\n", err)
		os.Exit(1)
	}

	cfg, client, err := cli.LoadConfigAndClient(confPath)
	if err != nil {
		fmt.Printf(cli.MarkFail+" 설정 오류: %v\n", err)
		os.Exit(1)
	}
	if *fileFlag == "" {
		fmt.Println(cli.MarkFail+" -file로 git에 업로드된 인벤토리 yaml 파일명을 지정해야 합니다.")
		os.Exit(1)
	}

	sourceID, err := resolveSourceID(client, cfg)
	if err != nil {
		fmt.Printf(cli.MarkFail+" %v\n", err)
		os.Exit(1)
	}

	updateID, err := patchAndSync(client, cfg, sourceID, *fileFlag)
	if err != nil {
		// conf에 고정한 소스 ID가 AWX에서 바뀐 경우(404)에만 새 ID를 받아 한 번 재시도한다.
		if newID, ok := recoverSourceID(cfg, confPath, user, err); ok {
			sourceID = newID
			updateID, err = patchAndSync(client, cfg, sourceID, *fileFlag)
		}
	}
	if err != nil {
		reportSyncFail(cfg, user, sourceID, *fileFlag, err)
		os.Exit(1)
	}

	upd, err := cli.PollInventoryUpdate(client, updateID, cfg.PollIntervalSec)
	if err != nil {
		fmt.Printf(cli.MarkFail+" 상태 조회 실패: %v\n", err)
		cli.AppendHistory(cfg, fmt.Sprintf("user=%s action=invsync source=%d update=%d status=poll_error error=%q", user, sourceID, updateID, err.Error()))
		os.Exit(1)
	}
	if upd.Status != "successful" {
		fmt.Printf(cli.MarkFail+" 동기화 실패 (status=%s)\n", upd.Status)
		cli.AppendHistory(cfg, fmt.Sprintf("user=%s action=invsync source=%d update=%d status=%s", user, sourceID, updateID, upd.Status))
		os.Exit(1)
	}
	fmt.Printf(cli.MarkOK+" 동기화 완료 (inventory_update %d)\n", updateID)
	cli.AppendHistory(cfg, fmt.Sprintf("user=%s action=invsync source=%d update=%d status=successful", user, sourceID, updateID))

	if cfg.S2Inventory == "" {
		fmt.Println("[i] conf의 s2_inventory가 설정되지 않아 등록된 호스트 목록 조회는 건너뜁니다.")
		return
	}
	inventoryID, err := strconv.Atoi(cfg.S2Inventory)
	if err != nil {
		fmt.Printf(cli.MarkFail+" s2_inventory(%s)는 숫자 ID여야 합니다.\n", cfg.S2Inventory)
		os.Exit(1)
	}
	hosts, err := client.ListInventoryHosts(inventoryID)
	if err != nil {
		fmt.Printf(cli.MarkFail+" 인벤토리(%d) 호스트 조회 실패: %v\n", inventoryID, err)
		os.Exit(1)
	}

	fmt.Printf(cli.MarkOK+" 인벤토리(%d)에 총 %d대 등록됨\n", inventoryID, len(hosts))
	for _, h := range hosts {
		status := "enabled"
		if !h.Enabled {
			status = "disabled"
		}
		fmt.Printf("  - %s (%s)\n", h.Name, status)
	}
}

// resolveSourceID는 사용할 인벤토리 소스 ID를 정한다.
// cfg.S2InventorySource가 채워져 있으면 그 값을 고정으로 사용하고,
// 비어 있으면 cfg.S2Inventory 아래의 소스 목록 중 첫 번째(id 오름차순)를 자동 선택한다.
func resolveSourceID(client *awx.Client, cfg *config.Config) (int, error) {
	if cfg.S2InventorySource != "" {
		id, err := strconv.Atoi(cfg.S2InventorySource)
		if err != nil {
			return 0, fmt.Errorf("s2_inventory_source(%s)는 숫자 ID여야 합니다", cfg.S2InventorySource)
		}
		return id, nil
	}

	if cfg.S2Inventory == "" {
		return 0, fmt.Errorf("s2_inventory_source와 s2_inventory가 둘 다 비어 있어 소스를 찾을 수 없습니다")
	}
	inventoryID, err := strconv.Atoi(cfg.S2Inventory)
	if err != nil {
		return 0, fmt.Errorf("s2_inventory(%s)는 숫자 ID여야 합니다", cfg.S2Inventory)
	}

	sources, err := client.ListInventorySources(inventoryID)
	if err != nil {
		return 0, fmt.Errorf("인벤토리(%d)의 소스 목록 조회 실패: %w", inventoryID, err)
	}
	if len(sources) == 0 {
		return 0, fmt.Errorf("인벤토리(%d)에 등록된 소스가 없습니다", inventoryID)
	}
	if len(sources) > 1 {
		fmt.Printf("[i] 소스가 %d개 있어 첫 번째(%s, ID=%d)를 사용합니다.\n", len(sources), sources[0].Name, sources[0].ID)
	}
	return sources[0].ID, nil
}

// 사용자 입력 함수. 테스트에서 대체한다.
var (
	promptLine  = cli.PromptLine
	promptYesNo = cli.PromptYesNo
)

// syncStageError는 PATCH/동기화 요청 중 어느 단계에서 난 오류인지 함께 담는다.
type syncStageError struct {
	stage string // "patch" | "sync"
	err   error
}

func (e *syncStageError) Error() string { return e.err.Error() }
func (e *syncStageError) Unwrap() error { return e.err }

// patchAndSync는 소스 필드를 저장(PATCH)하고 동기화를 요청해 inventory_update ID를 반환한다.
// 실패 출력·이력 기록은 하지 않고 단계 정보가 담긴 오류만 반환한다 (reportSyncFail이 처리).
func patchAndSync(client *awx.Client, cfg *config.Config, sourceID int, file string) (int, error) {
	fmt.Printf("[i] 인벤토리 소스(%d)의 %s를 %q로 저장 중...\n", sourceID, cfg.S2SourceField, file)
	if err := client.UpdateInventorySourceField(sourceID, cfg.S2SourceField, file); err != nil {
		return 0, &syncStageError{stage: "patch", err: err}
	}

	fmt.Printf("[i] 인벤토리 소스(%d) 동기화 시작...\n", sourceID)
	updateID, err := client.SyncInventorySource(sourceID)
	if err != nil {
		return 0, &syncStageError{stage: "sync", err: err}
	}
	return updateID, nil
}

// reportSyncFail은 patchAndSync 실패를 종전 형식으로 출력하고 이력에 남긴다.
func reportSyncFail(cfg *config.Config, user string, sourceID int, file string, err error) {
	var se *syncStageError
	if errors.As(err, &se) && se.stage == "sync" {
		fmt.Printf(cli.MarkFail+" 동기화 요청 실패: %v\n", err)
		cli.AppendHistory(cfg, fmt.Sprintf("user=%s action=invsync source=%d status=sync_error error=%q", user, sourceID, err.Error()))
		return
	}
	fmt.Printf(cli.MarkFail+" 소스 필드 저장 실패: %v\n", err)
	cli.AppendHistory(cfg, fmt.Sprintf("user=%s action=invsync source=%d file=%s status=patch_error error=%q", user, sourceID, file, err.Error()))
}

// recoverSourceID는 conf에 고정된 s2_inventory_source가 AWX에 없을 때(404) 새 ID를 입력받아
// conf를 갱신하고 새 ID를 반환한다. 해당 상황이 아니거나 입력·확인·갱신이 안 되면 ok=false다.
func recoverSourceID(cfg *config.Config, confPath, user string, err error) (newID int, ok bool) {
	var apiErr *awx.APIError
	if cfg.S2InventorySource == "" || !errors.As(err, &apiErr) || apiErr.StatusCode != 404 {
		return 0, false
	}

	invName := cfg.S2Inventory
	if invName == "" {
		invName = "[s2_inventory]"
	}
	num := cli.Highlight("숫자")
	fmt.Printf(cli.MarkWarn+" 인벤토리 소스에서 URI확인, inventories/inventory/%s/%s/details %s를 입력하세요.\n", invName, num, num)

	for try := 0; try < 3 && newID == 0; try++ {
		line := promptLine("새 인벤토리 소스 ID: ")
		if line == "" {
			return 0, false
		}
		n, convErr := strconv.Atoi(strings.TrimSpace(line))
		if convErr != nil || n <= 0 {
			fmt.Println(cli.MarkFail + " 1 이상의 숫자만 입력하세요.")
			continue
		}
		newID = n
	}
	if newID == 0 {
		return 0, false
	}

	oldID := cfg.S2InventorySource
	if !promptYesNo(fmt.Sprintf("s2_inventory_source 를 %s → %d 으로 업데이트할까요? (y/n): ", oldID, newID)) {
		return 0, false
	}
	if setErr := config.SetKey(confPath, "s2_inventory_source", strconv.Itoa(newID)); setErr != nil {
		fmt.Printf(cli.MarkFail+" conf 갱신 실패: %v\n", setErr)
		return 0, false
	}
	fmt.Printf(cli.MarkOK+" s2_inventory_source 를 %s → %d 으로 업데이트했습니다 (%s)\n", oldID, newID, confPath)
	cli.AppendHistory(cfg, fmt.Sprintf("user=%s action=invsync status=source_updated old=%s new=%d", user, oldID, newID))
	cfg.S2InventorySource = strconv.Itoa(newID)
	return newID, true
}
