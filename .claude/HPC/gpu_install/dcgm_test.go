package main

import (
	"bytes"
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "골든 파일 갱신")

func TestDcgmPackages(t *testing.T) {
	if cudaMajor("12.4") != 12 || cudaMajor("13.0") != 13 || cudaMajor("11") != 11 || cudaMajor("") != 0 || cudaMajor("x") != 0 {
		t.Error("cudaMajor")
	}
	want := []string{"datacenter-gpu-manager-4-core", "datacenter-gpu-manager-4-proprietary",
		"datacenter-gpu-manager-4-cuda13", "datacenter-gpu-manager-4-proprietary-cuda13"}
	if got := dcgmPackages(13); !reflect.DeepEqual(got, want) {
		t.Errorf("%q", got)
	}
	if got := dcgmOpposite(13); !reflect.DeepEqual(got, []string{"datacenter-gpu-manager-4-cuda12", "datacenter-gpu-manager-4-proprietary-cuda12"}) {
		t.Errorf("%q", got)
	}
	if got := dcgmOpposite(12); got[0] != "datacenter-gpu-manager-4-cuda13" {
		t.Errorf("%q", got)
	}
}

func TestParseDcgmState(t *testing.T) {
	out := "GI_DCGM_ACTIVE=active\nGI_DCGM_ENABLED=enabled\nGI_DCGM_FOUND=2 GPUs found.\n" +
		"GI_DCGM_PKG=datacenter-gpu-manager-4-core 4.2.3\nGI_DCGM_PKG=datacenter-gpu-manager-4-cuda12 4.2.3\n관계없는 줄\n"
	d := parseDcgmState(out)
	if d.Active != "active" || d.Enabled != "enabled" || d.GPUs != 2 || len(d.Pkgs) != 2 {
		t.Fatalf("%+v", d)
	}
	if d.summary() != "4.2.3 cuda12" || d.state() != "active/enabled/GPU 2" {
		t.Errorf("%q %q", d.summary(), d.state())
	}
	for s, n := range map[string]int{"1 GPU found.": 1, "| 8 GPUs found.  |": 8, "0 GPUs found.": 0, "Error: unable to connect": 0, "": 0} {
		if dcgmiGPUs(s) != n {
			t.Errorf("dcgmiGPUs(%q)=%d", s, dcgmiGPUs(s))
		}
	}
}

func TestDcgmProblems(t *testing.T) {
	if p := dcgmProblems(parseDcgmState(goodDcgm(12)), 12); len(p) != 0 {
		t.Errorf("정상이어야 함: %q", p)
	}
	// cuda12 → cuda13 으로 바뀌었는데 DCGM 은 그대로인 상태
	p := dcgmProblems(parseDcgmState(goodDcgm(12)), 13)
	want := []string{"dcgm missing(cuda13,proprietary-cuda13)", "dcgm cuda mismatch(cuda12,proprietary-cuda12)"}
	if !reflect.DeepEqual(p, want) {
		t.Errorf("%q", p)
	}
	p = dcgmProblems(DcgmState{Active: "failed", Enabled: "disabled"}, 12)
	want = []string{"dcgm missing(core,proprietary,cuda12,proprietary-cuda12)", "dcgm inactive", "dcgm not enabled", "dcgm no gpu"}
	if !reflect.DeepEqual(p, want) {
		t.Errorf("%q", p)
	}
}

func TestRpmVerCmp(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"4.2.3-1", "4.2.3-1", 0},
		{"4.10.0-1", "4.9.9-1", 1},
		{"4.2.3-2", "4.2.3-10", -1},
		{"4.2.3-1", "4.2.3.1-1", -1},
		{"1.0a-1", "1.0-1", 1},
		{"1.0a-1", "1.01-1", -1},
		{"007-1", "7-1", 0},
	}
	for _, c := range cases {
		if got := rpmEVRCmp(c.a, c.b); got != c.want {
			t.Errorf("%s vs %s = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

func touch(t *testing.T, p string) {
	t.Helper()
	os.MkdirAll(filepath.Dir(p), 0o755)
	if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// mkDcgmRepo 는 관리 서버의 repo 디렉토리(file:// 경로)를 흉내 낸다.
func mkDcgmRepo(t *testing.T) string {
	d := t.TempDir()
	for _, f := range []string{
		"Packages/datacenter-gpu-manager-4-core-4.2.3-1.x86_64.rpm",
		"Packages/datacenter-gpu-manager-4-core-4.10.0-1.x86_64.rpm",
		"Packages/datacenter-gpu-manager-4-core-4.11.0-1.src.rpm",
		"datacenter-gpu-manager-4-proprietary-4.10.0-1.x86_64.rpm",
		"datacenter-gpu-manager-4-proprietary-cuda12-4.10.0-1.x86_64.rpm",
		"datacenter-gpu-manager-4-proprietary-cuda13-4.10.0-1.x86_64.rpm",
		"datacenter-gpu-manager-4-cuda12-4.10.0-1.x86_64.rpm",
		"datacenter-gpu-manager-4-cuda12-4.2.3-1.x86_64.rpm",
		"datacenter-gpu-manager-4-cuda13-4.10.0-1.x86_64.rpm",
		"datacenter-gpu-manager-4-devel-4.10.0-1.x86_64.rpm",
		"other-1.0-1.x86_64.rpm",
	} {
		touch(t, filepath.Join(d, f))
	}
	return d
}

func TestFindDcgmRPMs(t *testing.T) {
	d := mkDcgmRepo(t)
	got, err := FindDcgmRPMs([]string{"http://mirror/x", "file://" + d}, dcgmPackages(12))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, p := range got {
		names = append(names, filepath.Base(p))
	}
	want := []string{"datacenter-gpu-manager-4-core-4.10.0-1.x86_64.rpm", "datacenter-gpu-manager-4-proprietary-4.10.0-1.x86_64.rpm",
		"datacenter-gpu-manager-4-cuda12-4.10.0-1.x86_64.rpm", "datacenter-gpu-manager-4-proprietary-cuda12-4.10.0-1.x86_64.rpm"}
	if !reflect.DeepEqual(names, want) {
		t.Errorf("%q", names)
	}
	os.Remove(filepath.Join(d, "datacenter-gpu-manager-4-cuda13-4.10.0-1.x86_64.rpm"))
	if _, err := FindDcgmRPMs([]string{"file://" + d}, dcgmPackages(13)); err == nil || err.Error() != "dcgm rpm not found(cuda13)" {
		t.Errorf("%v", err)
	}
	if _, err := FindDcgmRPMs([]string{"https://cdn/x"}, dcgmPackages(12)); err == nil || err.Error() != "dcgm baseurl not file://(https://cdn/x)" {
		t.Errorf("%v", err)
	}
	if _, err := FindDcgmRPMs(nil, dcgmPackages(12)); err == nil || err.Error() != "dcgm no baseurl in rhel.repo" {
		t.Errorf("%v", err)
	}
}

func TestDcgmScripts(t *testing.T) {
	m := dcgmMountScript(rhelRepo, 13)
	for _, s := range []string{"trap restore EXIT", "trap 'exit 130' INT TERM HUP PIPE", "'/etc/yum.repos.d/rhel.repo'",
		"datacenter-gpu-manager-4-proprietary-cuda13", "for p in datacenter-gpu-manager-4-cuda12 datacenter-gpu-manager-4-proprietary-cuda12",
		"dnf remove -y --disablerepo='*'"} {
		if !strings.Contains(m, s) {
			t.Errorf("mount 스크립트에 %q 없음", s)
		}
	}
	s := dcgmScpScript([]string{"/tmp/gpu_install/a.rpm", "/tmp/gpu_install/b.rpm"}, 12)
	if !strings.Contains(s, "dnf localinstall -y --disablerepo='*' '/tmp/gpu_install/a.rpm' '/tmp/gpu_install/b.rpm'") ||
		!strings.Contains(s, "datacenter-gpu-manager-4-cuda13 datacenter-gpu-manager-4-proprietary-cuda13") {
		t.Errorf("scp 스크립트: %s", s)
	}
	for _, sc := range []string{m, s, dcgmStartScript} {
		if strings.Contains(sc, "http") || strings.Contains(sc, "curl") || strings.Contains(sc, "wget") {
			t.Error("외부 다운로드 금지")
		}
	}
}

// bashRun 은 가짜 dnf/rpm/systemctl 을 PATH 앞에 두고 스크립트를 실제 bash 로 돌린다.
func bashRun(t *testing.T, script string, env ...string) (string, int) {
	t.Helper()
	bash, err := exec.LookPath("bash")
	if err != nil || (runtime.GOOS == "windows" && strings.Contains(strings.ToLower(bash), "\\system32\\")) {
		t.Skip("bash 없음")
	}
	bin := t.TempDir()
	fakes := map[string]string{
		"dnf": "#!/bin/bash\necho \"dnf $*\" >> \"$GI_LOG\"\ngrep enabled \"$GI_REPO\" | sed 's/^/during: /' >> \"$GI_LOG\"\n" +
			"if [ \"$1\" = install ]; then [ -n \"$DNF_KILL\" ] && kill -TERM $PPID; exit ${DNF_RC:-0}; fi\nexit 0\n",
		"rpm":       "#!/bin/bash\ncase \" $RPM_HAS \" in *\" $2 \"*) exit 0;; esac\nexit 1\n",
		"systemctl": "#!/bin/bash\necho \"systemctl $*\" >> \"$GI_LOG\"\n",
	}
	for n, body := range fakes {
		if err := os.WriteFile(filepath.Join(bin, n), []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	c := exec.Command(bash, "-c", script)
	c.Env = append(os.Environ(), append([]string{"PATH=" + bin + string(os.PathListSeparator) + os.Getenv("PATH")}, env...)...)
	out, _ := c.CombinedOutput()
	return string(out), c.ProcessState.ExitCode()
}

func TestDcgmMountScriptRun(t *testing.T) {
	const orig = "[baseos]\nname=a\nbaseurl=file:///repo/a\nenabled=0\n\n[dcgm]\nbaseurl=file:///repo/d\nenabled = 0\ngpgcheck=0\n"
	cases := []struct {
		name   string
		env    []string
		stale  bool // 앞선 실행이 강제 종료돼 원본 사본이 남고 repo 는 켜진 채인 상태
		rc     int
		inLog  []string
		output string
	}{
		{"성공", nil, false, 0, []string{"dnf install -y --disablerepo=* --enablerepo=baseos,dcgm datacenter-gpu-manager-4-core", "during: enabled=1", "during: enabled = 1"}, "GI_REPO_RESTORED=1"},
		{"dnf 실패", []string{"DNF_RC=1"}, false, 1, []string{"dnf install"}, "GI_FAIL=dcgm install failed"},
		{"중단(TERM)", []string{"DNF_KILL=1"}, false, 130, []string{"dnf install"}, "GI_REPO_RESTORED=1"},
		{"반대 cuda 제거", []string{"RPM_HAS=datacenter-gpu-manager-4-cuda12 datacenter-gpu-manager-4-proprietary-cuda12"}, false, 0,
			[]string{"systemctl stop nvidia-dcgm", "dnf remove -y --disablerepo=* datacenter-gpu-manager-4-cuda12 datacenter-gpu-manager-4-proprietary-cuda12"}, "GI_REPO_RESTORED=1"},
		{"남은 원본 사본 사용", nil, true, 0, []string{"during: enabled=1"}, "GI_REPO_RESTORED=1"},
	}
	// 저장소 id 가 없는 rhel.repo 는 dnf 를 돌리지 않고 fail
	t.Run("저장소 id 없음", func(t *testing.T) {
		d := t.TempDir()
		repo := filepath.ToSlash(filepath.Join(d, "rhel.repo"))
		log := filepath.ToSlash(filepath.Join(d, "log"))
		const noID = "baseurl=file:///repo/d\nenabled=0\n"
		os.WriteFile(repo, []byte(noID), 0o644)
		out, rc := bashRun(t, dcgmMountScript(repo, 12), "GI_LOG="+log, "GI_REPO="+repo)
		lb, _ := os.ReadFile(log)
		if rc != 1 || !strings.Contains(out, "GI_FAIL=dcgm no repo id in rhel.repo") || strings.Contains(string(lb), "dnf install") {
			t.Errorf("rc=%d out=%s\nlog=%s", rc, out, lb)
		}
		if b, _ := os.ReadFile(repo); string(b) != noID {
			t.Errorf("rhel.repo 가 복구되지 않음:\n%s", b)
		}
	})
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d := t.TempDir()
			repo := filepath.ToSlash(filepath.Join(d, "rhel.repo"))
			log := filepath.ToSlash(filepath.Join(d, "log"))
			cur := orig
			if c.stale {
				cur = strings.ReplaceAll(orig, "= 0", "= 1")
				os.WriteFile(repo+".gpu_install.orig", []byte(orig), 0o644)
			}
			os.WriteFile(repo, []byte(cur), 0o644)
			out, rc := bashRun(t, dcgmMountScript(repo, 13), append(c.env, "GI_LOG="+log, "GI_REPO="+repo)...)
			lb, _ := os.ReadFile(log)
			if rc != c.rc || !strings.Contains(out, c.output) {
				t.Errorf("rc=%d out=%s\nlog=%s", rc, out, lb)
			}
			for _, s := range c.inLog {
				if !strings.Contains(string(lb), s) {
					t.Errorf("로그에 %q 없음:\n%s", s, lb)
				}
			}
			if b, _ := os.ReadFile(repo); string(b) != orig {
				t.Errorf("rhel.repo 가 복구되지 않음:\n%s", b)
			}
			if _, err := os.Stat(repo + ".gpu_install.orig"); err == nil {
				t.Error("원본 사본이 남아 있음")
			}
		})
	}
}

func TestInstallDcgmScp(t *testing.T) {
	d := mkDcgmRepo(t)
	f := &fakeHost{installRC: []int{0}, driver: "580.65.06", cuda: "13.0"}
	f.use(t)
	st := okStatus()
	st.BaseURLs = []string{"file://" + d}
	tg := newTarget(methodScp, st)
	tg.Version = "580.65.06"
	installHost(&Config{AutofsPath: "/auto"}, tg)
	if tg.Fail != "" || tg.Dcgm == nil || tg.Dcgm.summary() != "4.2.3 cuda13" {
		t.Fatalf("%+v", tg)
	}
	k := f.kinds()
	tail := k[len(k)-4:]
	if tail[0] != "mkdir" || tail[1] != "dcgm-scp" || !strings.HasPrefix(tail[2], "rm -f '/tmp/gpu_install/datacenter-gpu-manager-4-core-4.10.0-1") || tail[3] != "dcgm-start" {
		t.Errorf("%q", k)
	}
	if len(f.copied) != 5 || !strings.Contains(f.copied[4], "datacenter-gpu-manager-4-proprietary-cuda13-4.10.0-1.x86_64.rpm gpu01:/tmp/gpu_install") {
		t.Errorf("scp: %q", f.copied)
	}
	// baseurl 이 file:// 이 아니면 fail (드라이버는 설치됨)
	f = &fakeHost{installRC: []int{0}, driver: "550.54.14"}
	f.use(t)
	st = okStatus()
	st.BaseURLs = []string{"https://repo/x"}
	tg = newTarget(methodScp, st)
	installHost(&Config{AutofsPath: "/auto"}, tg)
	if tg.Fail != "dcgm baseurl not file://(https://repo/x)" || tg.NewDriver != "550.54.14" {
		t.Errorf("%+v", tg)
	}
}

func TestInstallDcgmFail(t *testing.T) {
	notActive := "GI_DCGM_ACTIVE=failed\nGI_DCGM_ENABLED=enabled\n" + strings.Join(strings.Split(goodDcgm(12), "\n")[3:], "\n")
	cases := []struct {
		name string
		f    *fakeHost
		fail string
	}{
		{"dnf 실패", &fakeHost{dcgmRC: 1, dcgmOut: "GI_FAIL=dcgm install failed\nGI_REPO_RESTORED=1"}, "dcgm install failed"},
		{"서비스 비정상", &fakeHost{startOut: notActive}, "dcgm inactive, dcgm no gpu"},
		{"repo 복구 미확인", &fakeHost{dcgmOut: "설치됨"}, "dcgm repo restore failed"},
	}
	for _, c := range cases {
		c.f.installRC, c.f.driver = []int{0}, "550.54.14"
		c.f.use(t)
		tg := newTarget(methodMount, okStatus())
		installHost(&Config{AutofsPath: "/auto"}, tg)
		if tg.Fail != c.fail {
			t.Errorf("%s: %q", c.name, tg.Fail)
		}
	}
	// CUDA 11: 드라이버만 성공, DCGM 건너뛰고 경고
	f := &fakeHost{installRC: []int{0}, driver: "470.1", cuda: "11.4"}
	f.use(t)
	tg := newTarget(methodMount, okStatus())
	tg.Version = "470.1"
	installHost(&Config{AutofsPath: "/auto"}, tg)
	if tg.Fail != "" || tg.DcgmNote != "skip(CUDA 11.4)" || !reflect.DeepEqual(f.kinds(), []string{"sh '/auto/NVIDIA-Linux-x86_64-470.1.run' -s", "verify"}) {
		t.Errorf("%+v %q", tg, f.kinds())
	}
	if !strings.Contains(screen.(*bytes.Buffer).String(), "DCGM 을 건너뜁니다") {
		t.Error("경고 없음")
	}
}

func TestMarkDcgmRepair(t *testing.T) {
	good := okStatus()
	good.Driver, good.CUDA, good.DCGM = "550.54.14", "12.4", parseDcgmState(goodDcgm(12))
	bad := okStatus()
	bad.Driver, bad.CUDA, bad.DCGM = "550.54.14", "12.4", DcgmState{Active: "inactive"}
	old := okStatus()
	old.Driver, old.CUDA = "470.1", "11.4"
	ts := []*Target{
		{Entry: Entry{"a", "550.54.14"}, Status: good, Method: methodMount, Skip: skipSameVersion},
		{Entry: Entry{"b", "550.54.14"}, Status: bad, Method: methodMount, Skip: skipSameVersion},
		{Entry: Entry{"c", "580.65.06"}, Status: bad, Method: methodMount, Skip: "재설치 안 함"},
		{Entry: Entry{"d", "470.1"}, Status: old, Method: methodMount, Skip: skipSameVersion},
	}
	var out bytes.Buffer
	MarkDcgmRepair(&out, ts)
	if ts[0].Skip == "" || ts[1].Skip != "" || !ts[1].DcgmOnly || ts[2].DcgmOnly || ts[3].DcgmOnly {
		t.Fatalf("%+v %+v %+v %+v", ts[0], ts[1], ts[2], ts[3])
	}
	if !strings.Contains(out.String(), "b: 드라이버 550.54.14 는 같은 버전이라") {
		t.Errorf("%s", out.String())
	}
	// DCGM 만 수행: 사전점검·드라이버 설치 없이 DCGM 단계만
	f := &fakeHost{}
	f.use(t)
	installHost(&Config{AutofsPath: "/auto"}, ts[1])
	if ts[1].Fail != "" || ts[1].NewDriver != "550.54.14" || !reflect.DeepEqual(f.kinds(), []string{"dcgm-mount", "dcgm-start"}) {
		t.Errorf("%+v %q", ts[1], f.kinds())
	}
}

func TestPrintResults(t *testing.T) {
	ok12 := parseDcgmState(goodDcgm(12))
	skipSt := okStatus()
	skipSt.Driver, skipSt.CUDA, skipSt.DCGM = "550.54.14", "12.4", ok12
	ts := []*Target{
		{Entry: Entry{"gpu01", "550.54.14"}, Status: &HostStatus{}, Method: methodMount, NewDriver: "550.54.14", NewCUDA: "12.4", Dcgm: &ok12},
		{Entry: Entry{"gpu02", "580.65.06"}, Status: &HostStatus{Driver: "550.54.14", CUDA: "12.4"}, Method: methodScp,
			NewDriver: "580.65.06", NewCUDA: "13.0", Dcgm: &DcgmState{Active: "failed", Enabled: "enabled"}, Fail: "dcgm inactive, dcgm no gpu"},
		{Entry: Entry{"gpu03", "470.1"}, Status: &HostStatus{}, Method: methodScp, NewDriver: "470.1", NewCUDA: "11.4", DcgmNote: "skip(CUDA 11.4)"},
		{Entry: Entry{"gpu04", "550.54.14"}, Status: skipSt, Method: methodMount, Skip: skipSameVersion},
		{Entry: Entry{"gpu05", "550.54.14"}, Status: &HostStatus{Err: "응답 없음"}, Method: methodScp, Fail: "응답 없음"},
		{Entry: Entry{"gpu06", "550.54.14"}, Status: &HostStatus{}, Method: methodMount, Fail: "install failed(retry after unload)"},
	}
	var b bytes.Buffer
	PrintResults(&b, ts)
	golden := filepath.Join("testdata", "result.golden")
	if *update {
		os.MkdirAll("testdata", 0o755)
		os.WriteFile(golden, b.Bytes(), 0o644)
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatal(err)
	}
	if b.String() != string(want) {
		t.Errorf("결과 표가 다름:\n%s\n--- want\n%s", b.String(), want)
	}
}

func TestParseSurveyDcgm(t *testing.T) {
	so := "gpu01: GI_BEGIN\ngpu01: GI_DCGM_ACTIVE=active\ngpu01: GI_DCGM_ENABLED=enabled\ngpu01: GI_DCGM_FOUND=8 GPUs found.\n" +
		"gpu01: GI_DCGM_PKG=datacenter-gpu-manager-4-core 4.2.3\ngpu01: GI_END\n"
	r := parseSurvey(so, "", []string{"gpu01"})
	d := r["gpu01"].DCGM
	if d.Active != "active" || d.Enabled != "enabled" || d.GPUs != 8 || d.Pkgs["datacenter-gpu-manager-4-core"] != "4.2.3" {
		t.Errorf("%+v", d)
	}
	if !strings.Contains(buildSurveyScript("/a", "", nil), "GI_DCGM_ACTIVE") {
		t.Error("현황 스크립트에 DCGM 상태 없음")
	}
}
