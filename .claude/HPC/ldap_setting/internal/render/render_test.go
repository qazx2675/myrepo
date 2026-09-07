package render

import (
	"strings"
	"testing"

	"ldap-automation/internal/config"
)

func infra() config.Infra {
	return config.Infra{
		Name: "zxcv",
		DNS:  []string{"10.10.1.10"},
		NTP:  []string{"10.20.1.10"},
		URI: map[string]string{
			"uri1": "ldap://a/", "uri2": "ldap://b/", "uri3": "NONE",
		},
		BindDN: "uid=x,dc=e",
		BindPW: "pw",
		Sites: map[string]config.Site{
			"a1": {Name: "a1", URIOrder: []string{"uri1", "uri2", "uri3"}, Storage: "s1", Mountpoint: "/appl1"},
		},
	}
}

func TestApplyScriptEmbedsValues(t *testing.T) {
	s, err := ApplyScript(infra(), config.S4Rule{Enabled: true, Prefix: "s4", Services: []string{"nslcd", "ntp"}}, "a1")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"URI_LINE='ldap://a/ ldap://b/'", // uri3=NONE 은 빠져야 함
		"APPL_STORAGE='s1'",
		"S4_NSLCD='1'",
		"S4_NTP='1'",
		"#!/bin/bash",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("생성된 스크립트에 %q 가 없습니다", want)
		}
	}
	if strings.Contains(s, "NONE") {
		t.Error("NONE 이 스크립트에 새어 나갔습니다")
	}
}

// 홑따옴표가 들어간 값이 bash 대입문을 깨뜨리면 안 됩니다.
func TestQuoteEscaping(t *testing.T) {
	in := infra()
	in.BindPW = "pw'with'quote"
	s, err := ApplyScript(in, config.S4Rule{}, "a1")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(s, `BINDPW='pw'\''with'\''quote'`) {
		t.Errorf("홑따옴표 탈출 실패")
	}
}

func TestUnknownSite(t *testing.T) {
	if _, err := ApplyScript(infra(), config.S4Rule{}, "a9"); err == nil {
		t.Fatal("없는 사이트인데 오류가 나지 않았습니다")
	}
}

func TestRollbackScriptModes(t *testing.T) {
	for _, tc := range []struct {
		mode  RollbackMode
		stamp string
		want  string
	}{
		{RollbackList, "", "MODE='list'"},
		{RollbackLatest, "", "MODE='latest'"},
		{RollbackStamp, "20260907120000", "STAMP='20260907120000'"},
	} {
		s, err := RollbackScript(tc.mode, tc.stamp)
		if err != nil {
			t.Fatalf("%s: %v", tc.mode, err)
		}
		if !strings.Contains(s, tc.want) {
			t.Errorf("%s: %q 가 없습니다", tc.mode, tc.want)
		}
		// 공용 라이브러리가 붙어 있어야 restart_for_changed 를 쓸 수 있습니다.
		if !strings.Contains(s, "restart_for_changed()") {
			t.Errorf("%s: lib_common.sh 가 붙지 않았습니다", tc.mode)
		}
	}
}

// list/latest 에는 STAMP 가 들어가면 안 됩니다(오해 방지).
func TestRollbackStampClearedForNonStampModes(t *testing.T) {
	s, err := RollbackScript(RollbackLatest, "20260907120000")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(s, "STAMP=''") {
		t.Error("latest 모드인데 STAMP 가 비워지지 않았습니다")
	}
}

// STAMP 는 원격 셸로 나가는 값이라 숫자 14자리만 허용해야 합니다.
func TestRollbackStampValidation(t *testing.T) {
	for _, bad := range []string{"", "abc", "2026090712000", "202609071200000", "20260907; rm -rf /", "2026-09-07"} {
		if _, err := RollbackScript(RollbackStamp, bad); err == nil {
			t.Errorf("잘못된 STAMP %q 가 통과했습니다", bad)
		}
	}
}

func TestRollbackUnknownMode(t *testing.T) {
	if _, err := RollbackScript(RollbackMode("wipe"), ""); err == nil {
		t.Fatal("알 수 없는 모드인데 오류가 나지 않았습니다")
	}
}

// apply 스크립트도 공용 라이브러리를 포함해야 합니다.
func TestApplyScriptIncludesLib(t *testing.T) {
	s, err := ApplyScript(infra(), config.S4Rule{}, "a1")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(s, "restart_for_changed()") || !strings.Contains(s, "restart_for_changed\n") {
		t.Error("apply 스크립트에 공용 라이브러리 또는 호출이 없습니다")
	}
}
