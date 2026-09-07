package asset

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "a.txt")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadTabSeparated(t *testing.T) {
	e, err := Load(write(t, "# 주석\n\nsvr001\ta1\nsvr002\ta3\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(e) != 2 || e[0].Host != "svr001" || e[1].Site != "a3" {
		t.Fatalf("파싱 결과: %+v", e)
	}
}

// 구분자가 탭이므로 공백으로만 나뉜 줄은 오류여야 합니다.
func TestSpaceSeparatedRejected(t *testing.T) {
	if _, err := Load(write(t, "svr001 a1\n")); err == nil {
		t.Fatal("공백 구분인데 통과했습니다")
	}
}

func TestDuplicateHostRejected(t *testing.T) {
	_, err := Load(write(t, "svr001\ta1\nsvr001\ta2\n"))
	if err == nil || !strings.Contains(err.Error(), "중복") {
		t.Fatalf("중복 호스트를 잡지 못했습니다: %v", err)
	}
}

func TestGroupBySite(t *testing.T) {
	e, err := Load(write(t, "s1\ta1\ns2\ta1\ns3\ta2\n"))
	if err != nil {
		t.Fatal(err)
	}
	g := GroupBySite(e)
	if len(g["a1"]) != 2 || len(g["a2"]) != 1 {
		t.Fatalf("그룹 결과: %v", g)
	}
}
