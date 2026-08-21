package dhcp

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "10.10.10")
	content := `
subnet 10.10.10.0 netmask 255.255.255.0 {
    host svr01ev01 {
        hardware ethernet 00:11:22:33:44:01;
        fixed-address 10.10.10.15;
    }
    host svr01ev02 {
        hardware ethernet 00:11:22:33:44:02;
        fixed-address 10.10.10.89;
    }
    host svr01ev03 {
        hardware ethernet 00:11:22:33:44:03;
        fixed-address 10.10.10.201;
    }
}
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	records, err := ParseFile(path)
	if err != nil {
		t.Fatalf("ParseFile 실패: %v", err)
	}
	if len(records) != 3 {
		t.Fatalf("호스트 3개가 파싱돼야 하는데 %d개", len(records))
	}

	got := records["svr01ev02"]
	if got.MAC != "00:11:22:33:44:02" || got.IP != "10.10.10.89" {
		t.Fatalf("svr01ev02 불일치: %+v", got)
	}
}

func TestParseFile_NotFound(t *testing.T) {
	if _, err := ParseFile("/no/such/file"); err == nil {
		t.Fatal("파일 없을 때 에러가 나야 함 (우회 통과 금지)")
	}
}

func TestSubnetPrefix(t *testing.T) {
	got, err := SubnetPrefix("10.10.10.15")
	if err != nil {
		t.Fatalf("SubnetPrefix 실패: %v", err)
	}
	if got != "10.10.10" {
		t.Fatalf("기대값 10.10.10, 실제값 %s", got)
	}

	got, err = SubnetPrefix("1.1.1.1")
	if err != nil {
		t.Fatalf("SubnetPrefix 실패: %v", err)
	}
	if got != "1.1.1" {
		t.Fatalf("기대값 1.1.1, 실제값 %s", got)
	}
}

func TestSubnetPrefix_Invalid(t *testing.T) {
	if _, err := SubnetPrefix("not-an-ip"); err == nil {
		t.Fatal("IPv4 형식이 아니면 에러가 나야 함")
	}
}

func TestResolve_DNSFailure(t *testing.T) {
	if _, err := Resolve(t.TempDir(), "no-such-host.invalid."); err == nil {
		t.Fatal("존재하지 않는 hostname은 DNS 조회 실패로 에러가 나야 함")
	}
}
