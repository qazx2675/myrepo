package config

import (
	"os"
	"path/filepath"
	"testing"
)

func writeTemp(t *testing.T, name, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadLinesSkipsBlankAndComments(t *testing.T) {
	p := writeTemp(t, "list.txt", "# 주석\n\n  vm-a  \nvm-b\n   # 들여쓴 주석\n")
	got, err := LoadLines(p)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"vm-a", "vm-b"}
	if len(got) != len(want) {
		t.Fatalf("줄 수 불일치: got %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("[%d] got %q want %q", i, got[i], want[i])
		}
	}
}

func TestLoadLinesStripsBOM(t *testing.T) {
	// 메모장 등에서 "UTF-8"로 저장하면 파일 맨 앞에 BOM(U+FEFF)이 붙는다.
	// 첫 줄 첫 항목만 이름이 달라져 조회에 실패하던 문제의 회귀 테스트.
	p := writeTemp(t, "list.txt", "\ufeffvm-a\nvm-b\n")
	got, err := LoadLines(p)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"vm-a", "vm-b"}
	if len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("BOM 제거 실패: got %q want %q", got, want)
	}
}

func TestLoadWorklist(t *testing.T) {
	p := writeTemp(t, "w.txt", "# c\nhost-a  PG_A  100\nhost-b  PG_B  0\n")
	got, err := LoadWorklist(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("항목 수 %d, want 2", len(got))
	}
	if got[0].BMHost != "host-a" || got[0].PGName != "PG_A" || got[0].VlanID != 100 {
		t.Errorf("첫 항목 파싱 오류: %+v", got[0])
	}
	if got[1].VlanID != 0 {
		t.Errorf("VLAN 0 처리 오류: %+v", got[1])
	}
}

func TestLoadWorklistRejectsBadInput(t *testing.T) {
	cases := map[string]string{
		"컬럼 부족":      "host-a  PG_A\n",
		"VLAN 비숫자":   "host-a  PG_A  abc\n",
		"VLAN 범위 초과": "host-a  PG_A  5000\n",
		"VLAN 음수":    "host-a  PG_A  -1\n",
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := LoadWorklist(writeTemp(t, "w.txt", body)); err == nil {
				t.Fatal("에러를 기대했지만 nil")
			}
		})
	}
}

func TestTargetForHost(t *testing.T) {
	entries := []WorkEntry{
		{BMHost: "host-a", PGName: "PG_A", VlanID: 100},
		{BMHost: "host-b", PGName: "PG_B1", VlanID: 200},
		{BMHost: "host-b", PGName: "PG_B2", VlanID: 201},
	}

	got, err := TargetForHost(entries, "HOST-A") // 대소문자 무시
	if err != nil {
		t.Fatalf("host-a 조회 실패: %v", err)
	}
	if got.PGName != "PG_A" {
		t.Errorf("got %q want PG_A", got.PGName)
	}

	// 한 호스트에 여러 줄이면 이관 대상을 특정할 수 없어야 합니다.
	if _, err := TargetForHost(entries, "host-b"); err == nil {
		t.Error("중복 항목인데 에러가 나지 않았습니다")
	}
	if _, err := TargetForHost(entries, "host-z"); err == nil {
		t.Error("없는 호스트인데 에러가 나지 않았습니다")
	}
}

// vswitch 는 FQDN, vCenter 는 short name 인 경우(또는 그 반대)에도 매칭돼야 합니다.
func TestTargetForHostFQDNvsShort(t *testing.T) {
	entries := []WorkEntry{{BMHost: "esxi01.seccae.com", PGName: "PG_A", VlanID: 100}}

	got, err := TargetForHost(entries, "esxi01") // vCenter 가 short name 으로 보고
	if err != nil {
		t.Fatalf("short name 매칭 실패: %v", err)
	}
	if got.PGName != "PG_A" {
		t.Errorf("got %q want PG_A", got.PGName)
	}

	entries2 := []WorkEntry{{BMHost: "esxi01", PGName: "PG_B", VlanID: 200}}
	if _, err := TargetForHost(entries2, "esxi01.seccae.com"); err != nil {
		t.Fatalf("FQDN 매칭 실패: %v", err)
	}

	// 첫 마디가 다르면 매칭되면 안 됩니다.
	if _, err := TargetForHost(entries, "esxi02.seccae.com"); err == nil {
		t.Error("다른 호스트인데 매칭됐습니다")
	}
}

func TestLoadVMListTakesFirstField(t *testing.T) {
	// {user}.txt 가 "VM이름 변경될IP" 2열이어도 VM 이름만 읽어야 합니다.
	p := writeTemp(t, "u.txt", "vm-a 10.1.2.3\nvm-b\n# 주석\nvm-a 10.1.2.9\n")
	got, err := LoadVMList(p)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"vm-a", "vm-b"} // 중복 제거됨
	if len(got) != len(want) {
		t.Fatalf("got %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("[%d] got %q want %q", i, got[i], want[i])
		}
	}
}

func TestPassword(t *testing.T) {
	for _, k := range []string{"VC_PASSWORD", "VC_PASS", "VCENTER_PASS"} {
		t.Setenv(k, "")
	}
	if _, err := Password(); err == nil {
		t.Fatal("비밀번호가 없는데 에러가 나지 않았습니다")
	}

	t.Setenv("VC_PASSWORD", "p")
	p, err := Password()
	if err != nil || p != "p" {
		t.Fatalf("got (%q,%v)", p, err)
	}

	// 대체 변수명도 인식해야 합니다.
	t.Setenv("VC_PASSWORD", "")
	t.Setenv("VCENTER_PASS", "p2")
	p, err = Password()
	if err != nil || p != "p2" {
		t.Fatalf("대체 변수명 처리 실패: (%q,%v)", p, err)
	}
}
