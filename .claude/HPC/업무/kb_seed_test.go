// hpcbot — kb_seed.json 구조·앵커 검증 테스트
package main

import (
	"encoding/json"
	"io"
	"os"
	"slices"
	"testing"
)

// 계획서 §6-3 블록 목록 (순서 고정)
var seedIDs = []string{
	"term_itom", "term_cf", "term_vwp", "term_ssi", "term_awx", "term_mdp",
	"term_lsc", "term_dcm", "term_edm", "term_sds", "term_svr", "term_dreami",
	"support_scope", "approval", "server_usage", "requester",
	"os_install", "os_config_check", "usb0_cdc", "os_install_type", "os_install_caution", "os_patch",
	"ssi", "usb_block", "cert",
	"network_change", "asset_disposal", "asset_register", "hostname_change",
	"gpu_driver", "gpu_driver_incident", "fabric_manager", "gpu_mode",
	"splunk_install", "sw_support_scope",
	"account_login", "login_users", "sudo", "zombie", "cgroup", "crontab",
	"ht", "limit", "timezone", "tmp_clean", "server_inspect", "rsh", "telnet", "ib", "dscloud",
	"so_link", "cuda", "docker", "physical_work", "jump_server", "os_downgrade",
	"repo", "smart_de", "bu_disposal", "server_move", "os_std_change", "non_redhat_pkg",
	"emulator", "maint_expired", "partition",
	"password_quarterly", "worklog", "dns_check", "led",
	"splunk_pw_expire", "splunk_long_down", "splunk_long_incident", "usb_monthly", "dr_appl",
	"dongbo_splunk_na", "dongbo_itom",
	"server_intake", "dc_survey",
	"dhcp_fail", "pxe_fail", "passwd_disk_full", "ssh_kex", "dracut", "dhcpd_fail",
	"parts_intake", "parts_guide",
}

func TestKBSeed(t *testing.T) {
	b, err := os.ReadFile("data/kb_seed.json")
	if err != nil {
		t.Fatal(err)
	}
	var kb KB
	if err := json.Unmarshal(b, &kb); err != nil {
		t.Fatalf("kb_seed.json 파싱 실패: %v", err)
	}
	mb, err := os.ReadFile("data/manual.md")
	if err != nil {
		t.Fatal(err)
	}

	if len(kb.Intents) != 86 {
		t.Errorf("intent 수 %d, 86 이어야 함", len(kb.Intents))
	}
	var ids []string
	for _, it := range kb.Intents {
		ids = append(ids, it.ID)
	}
	if !slices.Equal(ids, seedIDs) {
		t.Errorf("intent id 목록/순서가 계획서 §6-3 과 다름:\n%v", ids)
	}

	seen := map[string]bool{}
	for _, it := range kb.Intents {
		if seen[it.ID] {
			t.Errorf("id 중복: %s", it.ID)
		}
		seen[it.ID] = true
		if it.Title == "" {
			t.Errorf("%s: title 없음", it.ID)
		}
		if _, ok := kb.Groups[it.Group]; !ok {
			t.Errorf("%s: groups 에 없는 그룹 %q", it.ID, it.Group)
		}
		if len(it.Keywords) < 5 {
			t.Errorf("%s: 키워드 %d개 (5개 이상 필요)", it.ID, len(it.Keywords))
		}
		if it.Group == "term" && len(it.RequireAny) == 0 {
			t.Errorf("%s: term 블록에 require_any 없음", it.ID)
		}
	}

	old := warnOut
	warnOut = io.Discard
	defer func() { warnOut = old }()
	d := NewData(kb, ParseManual(string(mb)))
	if u := d.UnresolvedIDs(); len(u) > 0 {
		t.Errorf("앵커 해석 실패: %v", u)
	}
}
