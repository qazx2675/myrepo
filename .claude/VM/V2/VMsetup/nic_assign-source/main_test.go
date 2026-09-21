package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeMap(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "nic_map.txt")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadAssignments(t *testing.T) {
	p := writeMap(t, bomPrefix+"# 주석\nbm001ev01 PG-A\n\nbm001ev02,PG-B\n")
	got, err := loadAssignments(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != (assignment{"bm001ev01", "PG-A"}) || got[1] != (assignment{"bm001ev02", "PG-B"}) {
		t.Errorf("결과가 이상합니다: %+v", got)
	}
}

func TestLoadAssignmentsRejectsDuplicateVM(t *testing.T) {
	_, err := loadAssignments(writeMap(t, "bm001ev01 PG-A\nbm001ev01 PG-B\n"))
	if err == nil || !strings.Contains(err.Error(), "한 줄만") {
		t.Fatalf("같은 VM 두 줄이면 에러여야 합니다: %v", err)
	}
}

func TestLoadAssignmentsRejectsBadLine(t *testing.T) {
	if _, err := loadAssignments(writeMap(t, "bm001ev01\n")); err == nil {
		t.Fatal("칸이 모자란 줄인데 에러가 나지 않았습니다")
	}
}
