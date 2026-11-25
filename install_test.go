package main

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// fakeHost 는 원격 실행·scp 를 흉내 낸다. installRC 는 설치 시도마다 돌려줄 종료코드다.
type fakeHost struct {
	scripts   []string
	copied    []string
	installRC []int
	nouveauRC int
	scpErr    error
	driver    string
	cuda      string   // nvidia-smi CUDA (비우면 12.4)
	dcgmRC    int      // DCGM 설치 스크립트 종료코드
	dcgmOut   string   // DCGM 설치 스크립트 출력 (비우면 repo 복구 표식만)
	startOut  string   // nvidia-dcgm 기동·확인 출력 (비우면 정상 상태)
	depOut    []string // rpm -U --test 출력, 호출마다 하나씩 (다 쓰면 부족분 없음)
	rcLocal   int      // /etc/rc.local 종료코드
}

// goodDcgm 은 major 기준 정상인 dcgmStateScript 출력이다.
func goodDcgm(major int) string {
	s := "GI_DCGM_ACTIVE=active\nGI_DCGM_ENABLED=enabled\nGI_DCGM_FOUND=8 GPUs found.\n"
	for _, p := range dcgmPackages(major) {
		s += "GI_DCGM_PKG=" + p + " 4.2.3\n"
	}
	return s
}

func (f *fakeHost) use(t *testing.T) {
	r, c, s := remoteRun, copyFile, screen
	t.Cleanup(func() { remoteRun, copyFile, screen = r, c, s })
	screen = &bytes.Buffer{}
	remoteRun = func(cfg *Config, host, script string, timeout time.Duration) (string, int, error) {
		f.scripts = append(f.scripts, script)
		switch {
		case script == rcLocalScript:
			return "rc.local out", f.rcLocal, nil
		case script == dcgmStartScript:
			if f.startOut != "" {
				return f.startOut, 0, nil
			}
			return goodDcgm(cudaMajor(f.cudaVer())), 0, nil
		case strings.HasPrefix(script, "LC_ALL=C rpm -U --test"):
			if len(f.depOut) == 0 {
				return "", 0, nil
			}
			out := f.depOut[0]
			f.depOut = f.depOut[1:]
			return out, 1, nil
		case strings.Contains(script, "dnf install") || strings.Contains(script, "dnf localinstall"):
			if f.dcgmOut != "" {
				return f.dcgmOut, f.dcgmRC, nil
			}
			return "GI_REPO_RESTORED=1", f.dcgmRC, nil
		case strings.HasPrefix(script, "sh "):
			rc := f.installRC[0]
			f.installRC = f.installRC[1:]
			return "installer out", rc, nil
		case strings.Contains(script, "rmmod nouveau"):
			return "", f.nouveauRC, nil
		case strings.Contains(script, "lsof"):
			return "--- lsof /dev/nvidia*\nnv-hostengine 123 root", 0, nil
		case strings.Contains(script, "GI_DRIVER"):
			return "GI_DRIVER=" + f.driver + "\nGI_CUDA=" + f.cudaVer(), 0, nil
		}
		return "", 0, nil
	}
	copyFile = func(local, host, dir string, timeout time.Duration) error {
		f.copied = append(f.copied, filepath.ToSlash(local)+" "+host+":"+dir)
		return f.scpErr
	}
}

func (f *fakeHost) cudaVer() string {
	if f.cuda == "" {
		return "12.4"
	}
	return f.cuda
}

// kinds 는 실행된 원격 스크립트를 짧은 이름으로 바꾼다.
func (f *fakeHost) kinds() []string {
	var k []string
	for _, s := range f.scripts {
		switch {
		case strings.HasPrefix(s, "sh "):
			k = append(k, s)
		case strings.Contains(s, "rmmod nouveau"):
			k = append(k, "nouveau")
		case strings.HasPrefix(s, "mkdir"):
			k = append(k, "mkdir")
		case strings.HasPrefix(s, "LC_ALL=C rpm -U --test"):
			k = append(k, "rpm-test")
		case strings.HasPrefix(s, "rm -f"):
			k = append(k, s)
		case s == unloadScript:
			k = append(k, "unload")
		case s == diagScript:
			k = append(k, "diag")
		case s == verifyScript:
			k = append(k, "verify")
		case s == dcgmStartScript:
			k = append(k, "dcgm-start")
		case s == rcLocalScript:
			k = append(k, "rc.local")
		case strings.Contains(s, "dnf install"):
			k = append(k, "dcgm-mount")
		case strings.Contains(s, "dnf localinstall"):
			k = append(k, "dcgm-scp")
		default:
			k = append(k, "?")
		}
	}
	return k
}

func okStatus() *HostStatus {
	return &HostStatus{GPUs: 8, KernelDevel: true, GCC: true, Kernel: "4.18.0"}
}

func newTarget(method string, st *HostStatus) *Target {
	return &Target{Entry: Entry{"gpu01", "550.54.14"}, Status: st, Method: method}
}

const mountRun = "sh '/auto/NVIDIA-Linux-x86_64-550.54.14.run' -s"

func TestInstallMountOK(t *testing.T) {
	f := &fakeHost{installRC: []int{0}, driver: "550.54.14"}
	f.use(t)
	cfg := &Config{AutofsPath: "/auto", LogDir: t.TempDir()}
	tg := newTarget(methodMount, okStatus())
	installHost(cfg, tg)
	if tg.Fail != "" || tg.NewDriver != "550.54.14" || tg.NewCUDA != "12.4" {
		t.Fatalf("%+v", tg)
	}
	if got := f.kinds(); !reflect.DeepEqual(got, []string{mountRun, "verify", "dcgm-mount", "dcgm-start", "rc.local"}) {
		t.Errorf("%q", got)
	}
	b, _ := os.ReadFile(filepath.Join(cfg.LogDir, "gpu01.log"))
	if !strings.Contains(string(b), "installer out") || !strings.Contains(string(b), "설치 완료") {
		t.Errorf("로그: %s", b)
	}
}

func TestInstallScpRetry(t *testing.T) {
	// busy_once: 첫 설치 실패 → 언로드 → 재시도 성공, 설치파일은 끝나면 지움
	f := &fakeHost{installRC: []int{1, 0}, driver: "550.54.14", cuda: "11.4"} // DCGM 은 건너뜀
	f.use(t)
	cfg := &Config{AutofsPath: "/auto", LogDir: t.TempDir()}
	tg := newTarget(methodScp, okStatus())
	installHost(cfg, tg)
	if tg.Fail != "" {
		t.Fatalf("%+v", tg)
	}
	tmpRun := "sh '/tmp/gpu_install/NVIDIA-Linux-x86_64-550.54.14.run' -s"
	want := []string{"mkdir", tmpRun, "unload", tmpRun, "rm -f '/tmp/gpu_install/NVIDIA-Linux-x86_64-550.54.14.run'", "verify", "rc.local"}
	if got := f.kinds(); !reflect.DeepEqual(got, want) {
		t.Errorf("%q", got)
	}
	if !reflect.DeepEqual(f.copied, []string{"/auto/NVIDIA-Linux-x86_64-550.54.14.run gpu01:/tmp/gpu_install"}) {
		t.Errorf("scp: %q", f.copied)
	}
}

func TestInstallRetryFailed(t *testing.T) {
	// busy_always: 재시도도 실패 → 진단을 로그에 남기고 fail, 설치 확인은 안 함
	f := &fakeHost{installRC: []int{1, 1}}
	f.use(t)
	cfg := &Config{AutofsPath: "/auto", LogDir: t.TempDir()}
	tg := newTarget(methodMount, okStatus())
	installHost(cfg, tg)
	if tg.Fail != "install failed(retry after unload)" {
		t.Fatalf("%+v", tg)
	}
	if got := f.kinds(); !reflect.DeepEqual(got, []string{mountRun, "unload", mountRun, "diag"}) {
		t.Errorf("%q", got)
	}
	b, _ := os.ReadFile(filepath.Join(cfg.LogDir, "gpu01.log"))
	if !strings.Contains(string(b), "nv-hostengine 123") {
		t.Errorf("로그에 lsof 없음: %s", b)
	}
	if strings.Contains(unloadScript, "kill") || strings.Contains(diagScript, "kill") {
		t.Error("프로세스 kill 금지")
	}
}

func TestInstallPrecheck(t *testing.T) {
	cases := []struct {
		name      string
		mod       func(*HostStatus)
		nouveauRC int
		skip      string
		fail      string
		kinds     []string
	}{
		{"GPU 없음", func(s *HostStatus) { s.GPUs = 0 }, 0, "GPU 없음", "", nil},
		{"kernel-devel 없음", func(s *HostStatus) { s.KernelDevel = false }, 0, "", "no kernel-devel", nil},
		{"둘 다 없음", func(s *HostStatus) { s.KernelDevel, s.GCC = false, false }, 0, "", "no kernel-devel, no gcc", nil},
		{"nouveau 언로드 실패", func(s *HostStatus) { s.Nouveau = true }, 1, "", "nouveau unload failed", []string{"nouveau"}},
		{"nouveau 언로드 후 진행", func(s *HostStatus) { s.Nouveau = true }, 0, "", "", []string{"nouveau", mountRun, "verify", "dcgm-mount", "dcgm-start", "rc.local"}},
	}
	for _, c := range cases {
		f := &fakeHost{installRC: []int{0}, driver: "550.54.14", nouveauRC: c.nouveauRC}
		f.use(t)
		st := okStatus()
		c.mod(st)
		tg := newTarget(methodMount, st)
		installHost(&Config{AutofsPath: "/auto"}, tg)
		if tg.Skip != c.skip || tg.Fail != c.fail || !reflect.DeepEqual(f.kinds(), c.kinds) {
			t.Errorf("%s: skip=%q fail=%q %q", c.name, tg.Skip, tg.Fail, f.kinds())
		}
	}
}

func TestInstallWarnAndMismatch(t *testing.T) {
	f := &fakeHost{installRC: []int{0}, driver: "535.1"}
	f.use(t)
	st := okStatus()
	st.FabricManager = "nvidia-fabricmanager-550"
	tg := newTarget(methodMount, st)
	installHost(&Config{AutofsPath: "/auto"}, tg)
	if tg.Fail != "version mismatch(535.1)" {
		t.Errorf("%+v", tg)
	}
	if !strings.Contains(screen.(*bytes.Buffer).String(), "fabricmanager") {
		t.Errorf("경고 없음: %s", screen)
	}
	// scp 실패
	f = &fakeHost{scpErr: os.ErrNotExist}
	f.use(t)
	tg = newTarget(methodScp, okStatus())
	installHost(&Config{AutofsPath: "/auto"}, tg)
	if tg.Fail != "scp failed" {
		t.Errorf("%+v", tg)
	}
}

func TestParseRemote(t *testing.T) {
	so := "gpu01: a\ngpu02: x\ngpu01: ERROR: b\ngpu01: GI_RC=3\n"
	out, rc, err := parseRemote(so, "", "gpu01")
	if out != "a\nERROR: b" || rc != 3 || err != nil {
		t.Errorf("%q %d %v", out, rc, err)
	}
	// 본문의 ERROR 줄은 남고, gossh 의 "ERROR: 종료코드 N" 줄은 판정·본문에 쓰지 않는다
	so = "gpu01: ERROR: An NVIDIA kernel module 'nvidia' appears to already be loaded\ngpu01: GI_RC=0\ngpu01: ERROR: 종료코드 3\n"
	out, rc, err = parseRemote(so, "", "gpu01")
	if out != "ERROR: An NVIDIA kernel module 'nvidia' appears to already be loaded" || rc != 0 || err != nil {
		t.Errorf("%q %d %v", out, rc, err)
	}
	if _, rc, err = parseRemote("gpu01: ERROR: 종료코드 3\n", "", "gpu01"); rc != -1 || err == nil {
		t.Errorf("%d %v", rc, err)
	}
	_, rc, err = parseRemote("", "gpu01: ERROR: SSH 접속 실패\n", "gpu01")
	if rc != -1 || err == nil || !strings.Contains(err.Error(), "SSH 접속 실패") {
		t.Errorf("%d %v", rc, err)
	}
}

func TestParseVerify(t *testing.T) {
	d, c := parseVerify("GI_DRIVER=580.65.06\nGI_CUDA=13.0\n")
	if d != "580.65.06" || c != "13.0" {
		t.Errorf("%q %q", d, c)
	}
	if d, _ := parseVerify("GI_DRIVER=NVIDIA-SMI has failed\n"); d != "" {
		t.Errorf("%q", d)
	}
}

func TestWriteFailLists(t *testing.T) {
	dir := t.TempDir()
	wd, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(wd)
	ts := []*Target{
		{Entry: Entry{"gpu01", "550.54.14"}, Fail: "install failed(retry after unload)"},
		{Entry: Entry{"gpu02", "550.54.14"}, Skip: "GPU 없음"},
		{Entry: Entry{"gpu03", "580.65.06"}, Fail: "no kernel-devel"},
	}
	var out bytes.Buffer
	if err := WriteFailLists(&out, Failed(ts)); err != nil {
		t.Fatal(err)
	}
	fl, _ := os.ReadFile(failListFile)
	rl, _ := os.ReadFile(retryListFile)
	if string(fl) != "gpu01 550.54.14 install failed(retry after unload)\ngpu03 580.65.06 no kernel-devel\n" {
		t.Errorf("fail_list: %q", fl)
	}
	if string(rl) != "gpu01 550.54.14\ngpu03 580.65.06\n" {
		t.Errorf("retry: %q", rl)
	}
	if es, err := ParseList(bytes.NewReader(rl)); err != nil || len(es) != 2 {
		t.Errorf("retry 는 list 로 읽혀야 함: %v %v", es, err)
	}
	if !strings.Contains(out.String(), "gpu03 580.65.06 no kernel-devel") {
		t.Errorf("화면: %s", out.String())
	}
	// 실패 없으면 파일을 만들지 않음
	os.Remove(failListFile)
	if err := WriteFailLists(&out, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(failListFile); err == nil {
		t.Error("fail_list 가 생기면 안 됨")
	}
}

func TestInstallRcLocal(t *testing.T) {
	cases := []struct {
		name string
		rc   int
		fail string
	}{
		{"성공", 0, ""},
		{"실패", 3, "rc.local failed(rc=3)"},
		{"10분 초과", 124, "rc.local failed(rc=124)"},
	}
	for _, c := range cases {
		f := &fakeHost{installRC: []int{0}, driver: "550.54.14", rcLocal: c.rc}
		f.use(t)
		tg := newTarget(methodMount, okStatus())
		installHost(&Config{AutofsPath: "/auto"}, tg)
		if tg.Fail != c.fail {
			t.Errorf("%s: fail=%q, want %q", c.name, tg.Fail, c.fail)
		}
	}
	// DCGM 이 실패하면 rc.local 은 실행하지 않는다
	f := &fakeHost{installRC: []int{0}, driver: "550.54.14", dcgmRC: 1}
	f.use(t)
	tg := newTarget(methodMount, okStatus())
	installHost(&Config{AutofsPath: "/auto"}, tg)
	for _, k := range f.kinds() {
		if k == "rc.local" {
			t.Errorf("DCGM 실패인데 rc.local 실행: %q", f.kinds())
		}
	}
}
