package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestParseListTagDedupe(t *testing.T) {
	text := "host1 A\nhost1 B\nhost2\t\tTAG(1/2)\nhost2\nhost3\n host3   X  # 주석\nhost4  \t t4\nhost5 a b\nhost5\n"
	got := parseList(text)
	want := []string{"host1 A", "host2 TAG(1/2)", "host3", "host4 t4", "host5 a b"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got=%q want=%q", got, want)
	}
}

func TestResolveTargetsTag(t *testing.T) {
	hosts := writeHosts(t, "192.0.2.10 host0001-m\n192.0.2.11 host0002-m\n")
	in := parseList("192.0.2.5 ipgrp\nhost0001 rhel810(TEST/SR)\nhost0002-m\n127.0.0.1:8443\tp\nhost0001-m:9443 q\n")
	got, err := resolveTargets(in, hosts)
	if err != nil {
		t.Fatal(err)
	}
	type row struct{ In, Host, IP, Port, Tag string }
	var rows []row
	for _, g := range got {
		rows = append(rows, row{g.Input, g.Hostname, g.IP, g.Port, g.Tag})
	}
	want := []row{
		{"192.0.2.5", "192.0.2.5", "192.0.2.5", "", "ipgrp"},
		{"host0001", "host0001", "192.0.2.10", "", "rhel810(TEST/SR)"},
		{"host0002-m", "host0002", "192.0.2.11", "", ""},
		{"127.0.0.1:8443", "127.0.0.1:8443", "127.0.0.1", "8443", "p"},
		{"host0001-m:9443", "host0001:9443", "192.0.2.10", "9443", "q"},
	}
	if !reflect.DeepEqual(rows, want) {
		t.Errorf("got=%v\nwant=%v", rows, want)
	}
}

func TestResolveTargetsThreeFields(t *testing.T) {
	_, err := resolveTargets(parseList("host1 a b\n"), "/없음")
	if err == nil || !strings.Contains(err.Error(), "3개 이상") || !strings.Contains(err.Error(), "host1 a b") {
		t.Errorf("3칸 줄은 오류여야 함: %v", err)
	}
}

func TestSplitTargetsNoTag(t *testing.T) {
	got := splitTargets("a b,c\td")
	if !reflect.DeepEqual(got, []string{"a", "b", "c", "d"}) {
		t.Errorf("got=%q", got)
	}
}

func TestRetryLineKeepsTag(t *testing.T) {
	ts, err := resolveTargets(parseList("192.0.2.5 g1\n192.0.2.6\n"), "/없음")
	if err != nil {
		t.Fatal(err)
	}
	var lines []string
	for _, x := range ts {
		lines = append(lines, x.ListLine())
	}
	if !reflect.DeepEqual(lines, []string{"192.0.2.5 g1", "192.0.2.6"}) {
		t.Fatalf("lines=%q", lines)
	}
	dir := t.TempDir()
	if err := writeLines(filepath.Join(dir, "retry.txt"), lines); err != nil {
		t.Fatal(err)
	}
	back, err := loadRetryList(dir)
	if err != nil || !reflect.DeepEqual(back, lines) {
		t.Fatalf("back=%q err=%v", back, err)
	}
	// merged_retry.txt 가 있으면 그쪽을 읽는다.
	if err := os.WriteFile(filepath.Join(dir, "merged_retry.txt"), []byte("192.0.2.7\tg2\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if back, _ = loadRetryList(dir); !reflect.DeepEqual(back, []string{"192.0.2.7 g2"}) {
		t.Errorf("merged back=%q", back)
	}
}
