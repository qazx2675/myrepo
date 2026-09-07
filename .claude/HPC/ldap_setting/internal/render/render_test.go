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
