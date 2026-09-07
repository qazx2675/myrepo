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
	_, err := Load(write(t, "svr001 a1\n"))
	if err == nil {
		t.Fatal("공백 구분인데 통과했습니다")
	}
	// "탭인 줄 알았는데 사실 스페이스" 를 사용자가 에러 메시지만 보고 알 수 있어야 합니다.
	if !strings.Contains(err.Error(), "svr001·a1") {
		t.Fatalf("스페이스를 눈에 보이게 표시하지 않았습니다: %v", err)
	}
}

func TestDuplicateHostRejected(t *testing.T) {
	_, err := Load(write(t, "svr001\ta1\nsvr001\ta2\n"))
	if err == nil || !strings.Contains(err.Error(), "중복") {
		t.Fatalf("중복 호스트를 잡지 못했습니다: %v", err)
	}
}

func TestLoadHostList(t *testing.T) {
	list, err := LoadHostList(write(t, "# 주석\n\nsvr001\nsvr002\n  svr003  \n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 3 || list[0] != "svr001" || list[2] != "svr003" {
		t.Fatalf("호스트 목록 파싱 결과: %v", list)
	}
}

// 같은 호스트가 여러 번 있어도 한 번만 남아야 합니다 (중복 SSH 방지).
func TestLoadHostListDedup(t *testing.T) {
	list, err := LoadHostList(write(t, "svr001\nsvr001\nsvr002\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("중복 제거 실패: %v", list)
	}
}

func TestLoadHostListEmpty(t *testing.T) {
	if _, err := LoadHostList(write(t, "\n# 주석뿐\n")); err == nil {
		t.Fatal("빈 목록인데 오류가 나지 않았습니다")
	}
}

// 탭(hostname\tsite)이 들어와도 LoadHostList 는 site 를 잘라내지 않고
// 줄 전체를 하나의 '호스트'로 취급합니다 — 이 파일에 site 를 넣는 것 자체가
// 잘못된 사용이므로, 조용히 파싱해주지 않고 명백히 이상한 값으로 남겨
// 사용자가 바로 알아채게 합니다.
func TestLoadHostListDoesNotSplitTabs(t *testing.T) {
	list, err := LoadHostList(write(t, "svr001\ta1\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || !strings.Contains(list[0], "\t") {
		t.Fatalf("탭을 분리해버렸습니다(자산현황 형식과 혼동 방지 실패): %v", list)
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
