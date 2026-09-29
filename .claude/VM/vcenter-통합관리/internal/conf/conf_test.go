package conf

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/text/encoding/korean"
)

func TestCleanPath(t *testing.T) {
	cases := map[string]string{
		`\\fileserver\share\vc-portal`:  `\\fileserver\share\vc-portal`,
		`\\fileserver\share\vc-portal\`: `\\fileserver\share\vc-portal`,
		`"D:\my folder\"`:               `D:\my folder`,
		`  'D:\포털 폴더\로그\'  `:            `D:\포털 폴더\로그`,
		`D:\`:                           `D:\`,
		`" \\srv\공유\a b "`:              `\\srv\공유\a b`,
		`/var/log/vc/`:                  `/var/log/vc`,
	}
	for in, want := range cases {
		if got := CleanPath(in); got != want {
			t.Errorf("CleanPath(%q)=%q want %q", in, got, want)
		}
	}
}

const base = `[paths]
output_dir = "\\fileserver\share\포털 폴더\"
work_dir   = D:\vcportal\work
log_dir    = D:\vcportal\logs\

; 주석
[collect]
parallel = 3
timeout  = 60

[account]
user     = svc-portal@vsphere.local
password = P@ss#w=rd!

[browser]
Type = Chrome

[vcenters]
vc01 = vc01.corp.local
VC-02 = https://VC02.corp.local:8443/
`

func write(t *testing.T, b []byte) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "vcportal.conf")
	if err := os.WriteFile(p, b, 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadFull(t *testing.T) {
	c, err := Load(write(t, []byte(strings.ReplaceAll(base, "\n", "\r\n"))))
	if err != nil {
		t.Fatal(err)
	}
	if c.OutputDir != `\\fileserver\share\포털 폴더` || c.WorkDir != `D:\vcportal\work` || c.LogDir != `D:\vcportal\logs` {
		t.Errorf("paths: %+v", c)
	}
	if c.Password != "P@ss#w=rd!" || c.User != "svc-portal@vsphere.local" {
		t.Errorf("account: %q %q", c.User, c.Password)
	}
	if c.Parallel != 3 || c.Timeout.Seconds() != 60 || c.Browser != "chrome" {
		t.Errorf("collect/browser: %+v", c)
	}
	if len(c.VCenters) != 2 || c.VCenters[0].URL != "https://vc01.corp.local" || c.VCenters[0].Host != "vc01.corp.local" ||
		c.VCenters[1].URL != "https://VC02.corp.local:8443" || c.VCenters[1].Host != "VC02.corp.local:8443" {
		t.Errorf("vcenters: %+v", c.VCenters)
	}
	if v := c.VCenterByHost("vc02.CORP.local:8443"); v == nil || v.ID != "VC-02" {
		t.Errorf("VCenterByHost: %v", v)
	}
	if c.VCenterByHost("nope") != nil {
		t.Error("expected nil")
	}
}

func TestBOM(t *testing.T) {
	c, err := Load(write(t, append([]byte{0xEF, 0xBB, 0xBF}, base...)))
	if err != nil || c.OutputDir == "" {
		t.Fatalf("%v %+v", err, c)
	}
}

func TestCP949(t *testing.T) {
	b, err := korean.EUCKR.NewEncoder().Bytes([]byte(base))
	if err != nil {
		t.Fatal(err)
	}
	c, err := Load(write(t, b))
	if err != nil {
		t.Fatal(err)
	}
	if c.OutputDir != `\\fileserver\share\포털 폴더` {
		t.Errorf("got %q", c.OutputDir)
	}
}

func TestDefaults(t *testing.T) {
	c, err := Load(write(t, []byte("[paths]\noutput_dir=D:\\o\n[account]\nuser=u\npassword=p\n[vcenters]\na=b.local\n")))
	if err != nil {
		t.Fatal(err)
	}
	if c.Parallel != 5 || c.Timeout.Seconds() != 300 || c.Browser != "edge" || c.WorkDir == "" || c.LogDir != "" {
		t.Errorf("%+v", c)
	}
}

func TestErrors(t *testing.T) {
	head := "[paths]\noutput_dir=x\n[account]\nuser=u\npassword=p\n"
	cases := map[string]struct{ text, want string }{
		"no output": {"[account]\nuser=u\npassword=p\n[vcenters]\na=b\n", "output_dir"},
		"no user":   {"[paths]\noutput_dir=x\n[account]\npassword=p\n[vcenters]\na=b\n", "user"},
		"no pw":     {"[paths]\noutput_dir=x\n[account]\nuser=u\n[vcenters]\na=b\n", "password"},
		"no vc":     {head, "vCenter"},
		"bad id":    {head + "[vcenters]\nvc 01=b\n", "7번째 줄"},
		"dup id":    {head + "[vcenters]\na=b\na=c\n", "중복"},
		"dup host":  {head + "[vcenters]\na=B\nb=https://b/\n", "중복"},
		"bad line":  {"[paths]\nfoo\n", "2번째 줄"},
		"bad num":   {"[collect]\nparallel=x\n", "2번째 줄"},
	}
	for name, tc := range cases {
		_, err := Load(write(t, []byte(tc.text)))
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err=%v want contains %q", name, err, tc.want)
		}
	}
}
