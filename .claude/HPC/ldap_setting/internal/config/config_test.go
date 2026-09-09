package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "c.conf")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

const good = `
s4.enabled  = true
s4.prefix   = s4
s4.services = nslcd,ntp

infra.zxcv.dns    = 10.10.1.10, 10.10.1.11
infra.zxcv.ntp    = 10.20.1.10
infra.zxcv.uri1   = ldap://a/
infra.zxcv.uri2   = ldap://b/
infra.zxcv.uri3   = NONE
infra.zxcv.binddn = uid=x,dc=e,dc=c
infra.zxcv.bindpw = pw

infra.zxcv.site.a1.uri_order  = uri1,uri2,uri3
infra.zxcv.site.a1.storage    = s1
infra.zxcv.site.a1.mountpoint = /appl1
infra.zxcv.site.a2.uri_order  = uri2,uri1,uri3
infra.zxcv.site.a2.storage    = s2
infra.zxcv.site.a2.mountpoint = /appl2
`

func TestLoadGood(t *testing.T) {
	c, err := Load(write(t, good))
	if err != nil {
		t.Fatal(err)
	}
	in := c.Infras["zxcv"]
	if len(in.DNS) != 2 || in.DNS[1] != "10.10.1.11" {
		t.Errorf("DNS 파싱 실패: %v", in.DNS)
	}
	if !c.S4.Enabled || c.S4.Prefix != "s4" {
		t.Errorf("s4 규칙 파싱 실패: %+v", c.S4)
	}
	if len(in.Sites) != 2 {
		t.Errorf("사이트 개수 %d", len(in.Sites))
	}
}

// uri3 = NONE 이면 URI 목록에서 빠져야 합니다.
func TestURIListSkipsNONE(t *testing.T) {
	c, err := Load(write(t, good))
	if err != nil {
		t.Fatal(err)
	}
	in := c.Infras["zxcv"]
	got := strings.Join(in.URIList("a1"), " ")
	if got != "ldap://a/ ldap://b/" {
		t.Errorf("a1 URI = %q", got)
	}
	got = strings.Join(in.URIList("a2"), " ")
	if got != "ldap://b/ ldap://a/" {
		t.Errorf("a2 URI = %q", got)
	}
}

// storage 는 사이트 판별 키라서 인프라 안에서 중복되면 안 됩니다.
func TestDuplicateStorageRejected(t *testing.T) {
	body := strings.Replace(good, "infra.zxcv.site.a2.storage    = s2",
		"infra.zxcv.site.a2.storage    = s1", 1)
	_, err := Load(write(t, body))
	if err == nil {
		t.Fatal("중복 storage 인데 오류가 나지 않았습니다")
	}
	if !strings.Contains(err.Error(), "중복") {
		t.Errorf("예상과 다른 오류: %v", err)
	}
}

func TestMissingKeyRejected(t *testing.T) {
	for _, drop := range []string{
		"infra.zxcv.bindpw = pw",
		"infra.zxcv.dns    = 10.10.1.10, 10.10.1.11",
		"infra.zxcv.site.a1.mountpoint = /appl1",
	} {
		body := strings.Replace(good, drop, "", 1)
		if _, err := Load(write(t, body)); err == nil {
			t.Errorf("%q 가 없는데 오류가 나지 않았습니다", drop)
		}
	}
}

// uri_order 가 정의되지 않은 URI 키를 가리키면 잡아야 합니다.
func TestUnknownURIKeyRejected(t *testing.T) {
	body := strings.Replace(good, "infra.zxcv.site.a1.uri_order  = uri1,uri2,uri3",
		"infra.zxcv.site.a1.uri_order  = uri1,uri9", 1)
	if _, err := Load(write(t, body)); err == nil {
		t.Fatal("정의되지 않은 uri9 인데 오류가 나지 않았습니다")
	}
}

func TestBadLineRejected(t *testing.T) {
	if _, err := Load(write(t, good+"\n이건등호가없는줄\n")); err == nil {
		t.Fatal("'=' 없는 줄인데 오류가 나지 않았습니다")
	}
}
