package main

import (
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const fakePrimary = `<?xml version="1.0" encoding="UTF-8"?>
<metadata xmlns="http://linux.duke.edu/metadata/common" xmlns:rpm="http://linux.duke.edu/metadata/rpm" packages="4">
<package type="rpm"><name>libfoo</name><arch>x86_64</arch><version epoch="0" ver="1.2" rel="1"/>
<location href="Packages/libfoo-1.2-1.x86_64.rpm"/>
<format><rpm:provides><rpm:entry name="libfoo"/><rpm:entry name="libfoo.so.1()(64bit)"/></rpm:provides>
<rpm:requires><rpm:entry name="notme"/></rpm:requires><file>/usr/bin/foo</file></format></package>
<package type="rpm"><name>libfoo</name><arch>x86_64</arch><version epoch="0" ver="1.10" rel="1"/>
<location href="Packages/libfoo-1.10-1.x86_64.rpm"/>
<format><rpm:provides><rpm:entry name="libfoo.so.1()(64bit)"/></rpm:provides></format></package>
<package type="rpm"><name>libfoo</name><arch>src</arch><version epoch="0" ver="9.9" rel="1"/>
<location href="Packages/libfoo-9.9-1.src.rpm"/>
<format><rpm:provides><rpm:entry name="libfoo.so.1()(64bit)"/></rpm:provides></format></package>
<package type="rpm"><name>libbar</name><arch>noarch</arch><version epoch="0" ver="2" rel="1"/>
<location href="Packages/libbar-2-1.noarch.rpm"/>
<format><rpm:provides><rpm:entry name="bar"/></rpm:provides></format></package>
</metadata>
`

// mkRepodata 는 dir 아래에 가짜 repodata(repomd.xml + primary.xml.gz)를 만든다.
func mkRepodata(t *testing.T, dir string) {
	t.Helper()
	var b bytes.Buffer
	gz := gzip.NewWriter(&b)
	gz.Write([]byte(fakePrimary))
	gz.Close()
	if err := os.MkdirAll(filepath.Join(dir, "repodata"), 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(dir, "repodata", "abc-primary.xml.gz"), b.Bytes(), 0o644)
	os.WriteFile(filepath.Join(dir, "repodata", "repomd.xml"), []byte(`<?xml version="1.0"?>
<repomd xmlns="http://linux.duke.edu/metadata/repo"><data type="filelists"><location href="repodata/x-filelists.xml.gz"/></data>
<data type="primary"><location href="repodata/abc-primary.xml.gz"/></data></repomd>`), 0o644)
}

func TestParseMissingDeps(t *testing.T) {
	out := `error: Failed dependencies:
	libfoo.so.1()(64bit) is needed by datacenter-gpu-manager-4-core-4.2.3-1.x86_64
	libbar >= 1.2 is needed by datacenter-gpu-manager-4-core-4.2.3-1.x86_64
	libfoo.so.1()(64bit) is needed by datacenter-gpu-manager-4-cuda12-4.2.3-1.x86_64
	datacenter-gpu-manager-4-cuda13 conflicts with datacenter-gpu-manager-4-cuda12-4.2.3-1.x86_64
	/usr/bin/foo is needed by x
	(a or b) is needed by y`
	want := []string{"(a or b)", "/usr/bin/foo", "libbar", "libfoo.so.1()(64bit)"}
	if got := parseMissingDeps(out); !reflect.DeepEqual(got, want) {
		t.Errorf("%q", got)
	}
	if got := parseMissingDeps("package x is already installed\n"); len(got) != 0 {
		t.Errorf("%q", got)
	}
}

func TestRepoIndex(t *testing.T) {
	d := t.TempDir()
	mkRepodata(t, d)
	idx, err := LoadRepoIndex([]string{"https://x/y", "file://" + d})
	if err != nil {
		t.Fatal(err)
	}
	base := func(p string) string { return filepath.Base(idx.find(p, nil)) }
	if got := base("libfoo.so.1()(64bit)"); got != "libfoo-1.10-1.x86_64.rpm" { // 1.10 > 1.2, src 제외
		t.Errorf("provides: %s", got)
	}
	if got := base("/usr/bin/foo"); got != "libfoo-1.2-1.x86_64.rpm" { // file 요소
		t.Errorf("file: %s", got)
	}
	if got := base("bar"); got != "libbar-2-1.noarch.rpm" {
		t.Errorf("noarch: %s", got)
	}
	if idx.find("notme", nil) != "" || idx.find("nothing", nil) != "" { // requires 는 제공이 아니다
		t.Error("없는 capability 가 찾아짐")
	}
	skip := map[string]bool{filepath.Join(d, "Packages", "libfoo-1.10-1.x86_64.rpm"): true}
	if got := base2(idx, "libfoo.so.1()(64bit)", skip); got != "libfoo-1.2-1.x86_64.rpm" {
		t.Errorf("skip: %s", got)
	}
	files, missing := idx.resolveDeps([]string{"libfoo.so.1()(64bit)", "libfoo", "zzz"}, nil)
	if len(files) != 2 || !reflect.DeepEqual(missing, []string{"zzz"}) {
		t.Errorf("%q %q", files, missing)
	}
	// repodata 없음
	if _, err := LoadRepoIndex([]string{"file://" + t.TempDir()}); err == nil || err.Error() != "dcgm no repodata" {
		t.Errorf("%v", err)
	}
}

func base2(idx repoIndex, c string, skip map[string]bool) string {
	return filepath.Base(idx.find(c, skip))
}

func TestRpmTestScript(t *testing.T) {
	s := rpmTestScript([]string{"/tmp/gpu_install/a.rpm", "/tmp/gpu_install/b.rpm"})
	if s != "LC_ALL=C rpm -U --test '/tmp/gpu_install/a.rpm' '/tmp/gpu_install/b.rpm' 2>&1" {
		t.Errorf("%s", s)
	}
}

const depNeeded = "error: Failed dependencies:\n\tlibfoo.so.1()(64bit) is needed by datacenter-gpu-manager-4-core-4.10.0-1.x86_64\n" +
	"\tdatacenter-gpu-manager-4-cuda12 conflicts with x\n"

func scpTarget(t *testing.T, f *fakeHost, withRepodata bool) (*Target, string) {
	d := mkDcgmRepo(t)
	if withRepodata {
		mkRepodata(t, d)
	}
	f.installRC, f.driver, f.cuda = []int{0}, "580.65.06", "13.0"
	f.use(t)
	st := okStatus()
	st.BaseURLs = []string{"file://" + d}
	tg := newTarget(methodScp, st)
	tg.Version = "580.65.06"
	return tg, d
}

func TestInstallDcgmScpDeps(t *testing.T) {
	f := &fakeHost{depOut: []string{depNeeded, "error: Failed dependencies:\n\tbar is needed by libfoo-1.10-1.x86_64\n"}}
	tg, _ := scpTarget(t, f, true)
	installHost(&Config{AutofsPath: "/auto"}, tg)
	if tg.Fail != "" || tg.Dcgm == nil {
		t.Fatalf("%+v", tg)
	}
	k := strings.Join(f.kinds(), "|")
	if strings.Count(k, "rpm-test") != 3 || !strings.Contains(k, "rpm-test|rpm-test|rpm-test|dcgm-scp|rm -f") {
		t.Errorf("%s", k)
	}
	// 4종 + libfoo + libbar 전송, localinstall·삭제에 모두 포함
	if len(f.copied) != 7 || !strings.Contains(f.copied[5], "libfoo-1.10-1.x86_64.rpm gpu01:/tmp/gpu_install") || !strings.Contains(f.copied[6], "libbar-2-1.noarch.rpm") {
		t.Errorf("scp: %q", f.copied)
	}
	var inst, rm string
	for _, s := range f.scripts {
		if strings.Contains(s, "dnf localinstall") {
			inst = s
		}
		if strings.HasPrefix(s, "rm -f '/tmp/gpu_install/datacenter") {
			rm = s
		}
	}
	for _, s := range []string{inst, rm} {
		if !strings.Contains(s, "'/tmp/gpu_install/libfoo-1.10-1.x86_64.rpm' '/tmp/gpu_install/libbar-2-1.noarch.rpm'") {
			t.Errorf("의존성 rpm 이 빠짐: %s", s)
		}
	}
	if !strings.Contains(screen.(*bytes.Buffer).String(), "부족한 의존성 libfoo.so.1()(64bit)") {
		t.Error("전송한 의존성 로그 없음")
	}
}

func TestInstallDcgmScpDepFail(t *testing.T) {
	f := &fakeHost{depOut: []string{depNeeded}}
	tg, _ := scpTarget(t, f, false)
	installHost(&Config{AutofsPath: "/auto"}, tg)
	if tg.Fail != "dcgm no repodata" {
		t.Errorf("repodata 없음: %q", tg.Fail)
	}
	f = &fakeHost{depOut: []string{depNeeded + "\tnothing >= 1 is needed by y\n"}}
	tg, _ = scpTarget(t, f, true)
	installHost(&Config{AutofsPath: "/auto"}, tg)
	if tg.Fail != "dcgm dep not found(nothing)" {
		t.Errorf("못 찾음: %q", tg.Fail)
	}
	if k := strings.Join(f.kinds(), "|"); strings.Contains(k, "dcgm-scp") || !strings.Contains(k, "rm -f") {
		t.Errorf("설치하지 않고 보낸 파일은 지워야 함: %s", k)
	}
	// 보냈는데도 같은 부족분이 계속 나오면 상한 전에 같은 rpm 을 또 고르지 않아 not found 로 끝난다
	f = &fakeHost{depOut: []string{depNeeded, depNeeded, depNeeded}}
	tg, _ = scpTarget(t, f, true)
	installHost(&Config{AutofsPath: "/auto"}, tg)
	if !strings.HasPrefix(tg.Fail, "dcgm dep not found(libfoo.so.1()(64bit)") {
		t.Errorf("반복: %q", tg.Fail)
	}
}
