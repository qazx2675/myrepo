// awxkit-invsync는 [S2] 인벤토리 소스 동기화를 트리거하고, 완료되면 등록된 호스트 목록을 보여준다.
package main

import (
	"flag"
	"fmt"
	"os"
	"strconv"

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
		fmt.Fprintf(os.Stderr, "[X] 설정 파일을 찾을 수 없습니다: %v\n", err)
		os.Exit(1)
	}

	cfg, client, err := cli.LoadConfigAndClient(confPath)
	if err != nil {
		fmt.Printf("[X] 설정 오류: %v\n", err)
		os.Exit(1)
	}
	if *fileFlag == "" {
		fmt.Println("[X] -file로 git에 업로드된 인벤토리 yaml 파일명을 지정해야 합니다.")
		os.Exit(1)
	}

	sourceID, err := resolveSourceID(client, cfg)
	if err != nil {
		fmt.Printf("[X] %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("[i] 인벤토리 소스(%d)의 %s를 %q로 저장 중...\n", sourceID, cfg.S2SourceField, *fileFlag)
	if err := client.UpdateInventorySourceField(sourceID, cfg.S2SourceField, *fileFlag); err != nil {
		fmt.Printf("[X] 소스 필드 저장 실패: %v\n", err)
		cli.AppendHistory(cfg, fmt.Sprintf("user=%s action=invsync source=%d file=%s status=patch_error error=%q", user, sourceID, *fileFlag, err.Error()))
		os.Exit(1)
	}

	fmt.Printf("[i] 인벤토리 소스(%d) 동기화 시작...\n", sourceID)
	updateID, err := client.SyncInventorySource(sourceID)
	if err != nil {
		fmt.Printf("[X] 동기화 요청 실패: %v\n", err)
		cli.AppendHistory(cfg, fmt.Sprintf("user=%s action=invsync source=%d status=sync_error error=%q", user, sourceID, err.Error()))
		os.Exit(1)
	}

	upd, err := cli.PollInventoryUpdate(client, updateID, cfg.PollIntervalSec)
	if err != nil {
		fmt.Printf("[X] 상태 조회 실패: %v\n", err)
		cli.AppendHistory(cfg, fmt.Sprintf("user=%s action=invsync source=%d update=%d status=poll_error error=%q", user, sourceID, updateID, err.Error()))
		os.Exit(1)
	}
	if upd.Status != "successful" {
		fmt.Printf("[X] 동기화 실패 (status=%s)\n", upd.Status)
		cli.AppendHistory(cfg, fmt.Sprintf("user=%s action=invsync source=%d update=%d status=%s", user, sourceID, updateID, upd.Status))
		os.Exit(1)
	}
	fmt.Printf("[✔] 동기화 완료 (inventory_update %d)\n", updateID)
	cli.AppendHistory(cfg, fmt.Sprintf("user=%s action=invsync source=%d update=%d status=successful", user, sourceID, updateID))

	if cfg.S2Inventory == "" {
		fmt.Println("[i] conf의 s2_inventory가 설정되지 않아 등록된 호스트 목록 조회는 건너뜁니다.")
		return
	}
	inventoryID, err := strconv.Atoi(cfg.S2Inventory)
	if err != nil {
		fmt.Printf("[X] s2_inventory(%s)는 숫자 ID여야 합니다.\n", cfg.S2Inventory)
		os.Exit(1)
	}
	hosts, err := client.ListInventoryHosts(inventoryID)
	if err != nil {
		fmt.Printf("[X] 인벤토리(%d) 호스트 조회 실패: %v\n", inventoryID, err)
		os.Exit(1)
	}

	fmt.Printf("[✔] 인벤토리(%d)에 총 %d대 등록됨\n", inventoryID, len(hosts))
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
