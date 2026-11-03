// paths_test.go - paths.go 단위 테스트
package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
)

func setPathVars(t *testing.T, mgmt, osc, os6osc, awx string) {
	t.Helper()
	a, b, c, d := os6_mgmt, os_check_sh, os6_os_check_sh, awx_dir
	t.Cleanup(func() { os6_mgmt, os_check_sh, os6_os_check_sh, awx_dir = a, b, c, d })
	os6_mgmt, os_check_sh, os6_os_check_sh, awx_dir = mgmt, osc, os6osc, awx
}

func TestOS6OSCheckPath(t *testing.T) {
	cases := []struct{ mgmt, osc, os6osc, want string }{
		{"m", "/a/oc.sh", "/b/oc.sh", "/b/oc.sh"}, // 명시값 우선
		{"m", "/a/oc.sh", "", "/a/oc.sh"},         // autofs 동일 경로 폴백
		{"", "/a/oc.sh", "", ""},                  // os6_mgmt 없음
		{"m", "", "", ""},                         // 둘 다 비면 2차 생략
		{"m", "/a/oc.sh", "-", ""},                // "-" 는 2차 체크 끄기
	}
	for _, c := range cases {
		setPathVars(t, c.mgmt, c.osc, c.os6osc, "")
		if got := os6OSCheckPath(); got != c.want {
			t.Errorf("%+v: got %q want %q", c, got, c.want)
		}
	}
}

func TestDhcpCandidates(t *testing.T) {
	setPathVars(t, "", "", "", "/awx/script")
	want := []string{"/awx/script/awxkit/dhcp.sh", "/awx/script/dhcp.sh", "/o/dhcp.sh"}
	if got := dhcpCandidates("/o/os_check.sh"); !reflect.DeepEqual(got, want) {
		t.Errorf("got %v", got)
	}
	setPathVars(t, "", "", "", "")
	if got := dhcpCandidates("/o/os_check.sh"); !reflect.DeepEqual(got, []string{"/o/dhcp.sh"}) {
		t.Errorf("awx_dir 빈 값: got %v", got)
	}
	if got := dhcpCandidates(""); len(got) != 0 {
		t.Errorf("모두 빔: got %v", got)
	}
}

func TestFirstExisting(t *testing.T) {
	tmp := t.TempDir()
	b := filepath.Join(tmp, "b.sh")
	c := filepath.Join(tmp, "c.sh")
	os.WriteFile(b, nil, 0644)
	os.WriteFile(c, nil, 0644)
	if got := firstExisting([]string{filepath.Join(tmp, "a.sh"), b, c}); got != b {
		t.Errorf("got %q", got)
	}
	if got := firstExisting([]string{filepath.Join(tmp, "a.sh")}); got != "" {
		t.Errorf("got %q", got)
	}
}

func TestDhcpLinkSh(t *testing.T) {
	needBash(t)
	if got := dhcpLinkSh(nil); got != "{ true; }" {
		t.Errorf("빈 후보: %q", got)
	}
	tmp := t.TempDir()
	odd := filepath.Join(tmp, "it's dir") // 공백·따옴표
	os.MkdirAll(odd, 0755)
	b := filepath.Join(odd, "dhcp.sh")
	os.WriteFile(b, []byte("#b\n"), 0644)
	cands := []string{filepath.Join(tmp, "none", "dhcp.sh"), b, filepath.Join(tmp, "z.sh")}
	work := t.TempDir()
	cmd := exec.Command("sh", "-c", dhcpLinkSh(cands)+" && echo ok")
	cmd.Dir = work
	if out, err := cmd.CombinedOutput(); err != nil || string(out) != "ok\n" {
		t.Fatalf("err=%v out=%q", err, out)
	}
	if dst, err := os.Readlink(filepath.Join(work, "dhcp.sh")); err != nil || dst != b {
		t.Errorf("링크 대상 %q err=%v", dst, err)
	}
	// 후보가 하나도 없어도 성공 종료, 링크 없음
	work2 := t.TempDir()
	cmd = exec.Command("sh", "-c", dhcpLinkSh(cands[:1])+" && echo ok")
	cmd.Dir = work2
	if out, err := cmd.CombinedOutput(); err != nil || string(out) != "ok\n" {
		t.Fatalf("err=%v out=%q", err, out)
	}
	if _, err := os.Lstat(filepath.Join(work2, "dhcp.sh")); err == nil {
		t.Error("링크가 생기면 안 됨")
	}
}
