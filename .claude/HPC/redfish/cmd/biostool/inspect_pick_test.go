package main

import (
	"errors"
	"strings"
	"testing"
)

// fakeGet 은 경로 → JSON 맵으로 getFunc 을 만듭니다 (읽은 경로를 기록).
func fakeGet(docs map[string]string, seen *[]string) getFunc {
	return func(p string) ([]byte, error) {
		*seen = append(*seen, p)
		if d, ok := docs[p]; ok {
			return []byte(d), nil
		}
		return nil, errors.New("404")
	}
}

// Dell XE9680 모양: Systems 의 첫 멤버가 GPU 베이스보드(NVIDIA, Bios 없음), 둘째가 본체(Dell, Bios 있음).
func TestDetectSystemSkipsBaseboardWithoutBios(t *testing.T) {
	docs := map[string]string{
		"/redfish/v1":                                `{"Vendor":"Dell","Systems":{"@odata.id":"/redfish/v1/Systems"}}`,
		"/redfish/v1/Systems":                        `{"Members":[{"@odata.id":"/redfish/v1/Systems/HGX_Baseboard_0"},{"@odata.id":"/redfish/v1/Systems/System.Embedded.1"}]}`,
		"/redfish/v1/Systems/HGX_Baseboard_0":        `{"Manufacturer":"NVIDIA","Model":"HGX H100 8-GPU"}`,
		"/redfish/v1/Systems/System.Embedded.1":      `{"Manufacturer":"Dell Inc.","Model":"PowerEdge XE9680","Bios":{"@odata.id":"/redfish/v1/Systems/System.Embedded.1/Bios"}}`,
		"/redfish/v1/Systems/System.Embedded.1/Bios": `{"Attributes":{"LogicalProc":"Enabled"}}`,
	}
	var seen []string
	info, err := detectSystemWith(fakeGet(docs, &seen))
	if err != nil {
		t.Fatalf("detectSystem: %v", err)
	}
	if info.Vendor != "Dell" || !strings.Contains(info.Model, "XE9680") || info.BiosPath == "" {
		t.Errorf("본체 System 을 고르지 못함: vendor=%q model=%q bios=%q", info.Vendor, info.Model, info.BiosPath)
	}
}

// 첫 멤버에 Bios 가 있으면 다른 System 을 읽지 않는다 (기존 동작·GET 수 유지).
func TestPickSystemFirstHasBiosNoExtraGets(t *testing.T) {
	docs := map[string]string{
		"/redfish/v1/Systems/1": `{"Bios":{"@odata.id":"/redfish/v1/Systems/1/Bios"}}`,
		"/redfish/v1/Systems/2": `{"Manufacturer":"X"}`,
	}
	var seen []string
	got := pickSystem(fakeGet(docs, &seen), []string{"/redfish/v1/Systems/1", "/redfish/v1/Systems/2"})
	if got != "/redfish/v1/Systems/1" || len(seen) != 1 {
		t.Errorf("got=%q seen=%v", got, seen)
	}
	// 멤버가 하나면 읽지도 않는다.
	seen = nil
	if pickSystem(fakeGet(docs, &seen), []string{"/redfish/v1/Systems/9"}) != "/redfish/v1/Systems/9" || len(seen) != 0 {
		t.Errorf("단일 멤버는 GET 없이 그대로: %v", seen)
	}
	// 모두 Bios 가 없으면 첫 멤버.
	seen = nil
	if got := pickSystem(fakeGet(map[string]string{"/a": `{}`, "/b": `{}`}, &seen), []string{"/a", "/b"}); got != "/a" {
		t.Errorf("Bios 가 없으면 첫 멤버: %q", got)
	}
}
