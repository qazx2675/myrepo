package main

import (
	"fmt"
	"os"
	"strconv"
)

// runInvSync는 [S2] 인벤토리 소스 동기화를 트리거하고, 완료되면 등록된 호스트 목록을 보여준다.
func runInvSync(confPath, user string) {
	cfg, client, err := loadConfigAndClient(confPath)
	if err != nil {
		fmt.Printf("[X] 설정 오류: %v\n", err)
		os.Exit(1)
	}
	if cfg.S2InventorySource == "" {
		fmt.Println("[X] conf의 s2_inventory_source가 설정되지 않았습니다.")
		os.Exit(1)
	}
	sourceID, err := strconv.Atoi(cfg.S2InventorySource)
	if err != nil {
		fmt.Printf("[X] s2_inventory_source(%s)는 숫자 ID여야 합니다.\n", cfg.S2InventorySource)
		os.Exit(1)
	}

	fmt.Printf("[i] 인벤토리 소스(%d) 동기화 시작...\n", sourceID)
	updateID, err := client.SyncInventorySource(sourceID)
	if err != nil {
		fmt.Printf("[X] 동기화 요청 실패: %v\n", err)
		appendHistory(cfg, fmt.Sprintf("user=%s action=invsync source=%d status=sync_error error=%q", user, sourceID, err.Error()))
		os.Exit(1)
	}

	upd, err := pollInventoryUpdate(client, updateID, cfg.PollIntervalSec)
	if err != nil {
		fmt.Printf("[X] 상태 조회 실패: %v\n", err)
		appendHistory(cfg, fmt.Sprintf("user=%s action=invsync source=%d update=%d status=poll_error error=%q", user, sourceID, updateID, err.Error()))
		os.Exit(1)
	}
	if upd.Status != "successful" {
		fmt.Printf("[X] 동기화 실패 (status=%s)\n", upd.Status)
		appendHistory(cfg, fmt.Sprintf("user=%s action=invsync source=%d update=%d status=%s", user, sourceID, updateID, upd.Status))
		os.Exit(1)
	}
	fmt.Printf("[✔] 동기화 완료 (inventory_update %d)\n", updateID)
	appendHistory(cfg, fmt.Sprintf("user=%s action=invsync source=%d update=%d status=successful", user, sourceID, updateID))

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
