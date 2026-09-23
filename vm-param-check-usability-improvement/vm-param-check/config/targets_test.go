package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// vcenter.txt 는 vm_setup.sh 와 같이 쓰므로 줄 끝 주석이 있어도 주소만 읽어야 한다.
func TestLoadLinesInlineComment(t *testing.T) {
	p := filepath.Join(t.TempDir(), "vcenter.txt")
	body := "# 목록\n\nvcsim.saccae.com      # 가상 vCenter\n  vcenter.saccae.com\t# 실제\n192.168.0.50\n"
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := LoadLines(p)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"vcsim.saccae.com", "vcenter.saccae.com", "192.168.0.50"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("LoadLines = %q, want %q", got, want)
	}
}
