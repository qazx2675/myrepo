package main

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestRemoteBash(t *testing.T) {
	script := buildSurveyScript("/a b/x", "u1", []string{"550.54.14", "580.65.06"})
	cmd := remoteBash(script)
	b64 := strings.TrimSuffix(strings.TrimPrefix(cmd, "echo "), " | base64 -d | bash")
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil || !strings.HasSuffix(string(raw), script) {
		t.Fatalf("디코드 불일치: %v", err)
	}
	for _, s := range []string{"'/user/u1'", "'/a b/x/NVIDIA-Linux-x86_64-580.65.06.run'", "GI_RUN=550.54.14:1"} {
		if !strings.Contains(script, s) {
			t.Errorf("스크립트에 %q 없음", s)
		}
	}
	// 위험 단어가 생기는 입력이라도 결과에는 없어야 한다
	for i := 0; i < 300; i++ {
		low := strings.ToLower(remoteBash(strings.Repeat("x", i) + "halt ddc reboot"))
		for _, w := range gosshDanger {
			if strings.Contains(low, w) {
				t.Fatalf("위험 단어 %q 포함 (i=%d)", w, i)
			}
		}
	}
	if strings.Contains(buildSurveyScript("/a", "", nil), "/user/") {
		t.Error("MOUNT_USER 없으면 /user 확인 안 함")
	}
}

func TestParseSurvey(t *testing.T) {
	so := `gpu01: GI_BEGIN
gpu01: GI_HOSTNAME=gpu01.local
gpu01: GI_KERNEL=4.18.0-553.el8_10.x86_64
gpu01: GI_DRIVER=550.54.14
gpu01: GI_CUDA=12.4
gpu01: GI_GPU=8
gpu01: GI_NVSWITCH=1
gpu01: GI_KDEVEL=1
gpu01: GI_GCC=1
gpu01: GI_FM=nvidia-fabricmanager-550-550.54.14-1.x86_64
gpu01: GI_USERMNT=1
gpu01: GI_RUN=580.65.06:1
gpu01: GI_RUN=550.54.14:0
gpu01: GI_BASEURL=file:///repo/BaseOS
gpu01: GI_BASEURL=file:///repo/AppStream
gpu01: GI_END
gpu02: GI_BEGIN
gpu02: GI_HOSTNAME=gpu02
gpu02: GI_DRIVER=NVIDIA-SMI has failed because it couldn't communicate
gpu02: GI_GPU=0
gpu02: GI_NOUVEAU=1
gpu02: GI_FM=
gpu02: ERROR: GI_END
gpu03: GI_BEGIN
gpu03: GI_HOSTNAME=gpu03
gpu05: 관계없는 줄
`
	se := "gpu04: ERROR: SSH 접속 실패: dial tcp: i/o timeout\ngpu04: 두번째 줄\n"
	hosts := []string{"gpu01", "gpu02", "gpu03", "gpu04", "gpu05"}
	r := parseSurvey(so, se, hosts)
	want1 := &HostStatus{Hostname: "gpu01.local", Kernel: "4.18.0-553.el8_10.x86_64", Driver: "550.54.14", CUDA: "12.4",
		GPUs: 8, NVSwitch: true, KernelDevel: true, GCC: true, FabricManager: "nvidia-fabricmanager-550-550.54.14-1.x86_64",
		UserMount: true, RunReadable: map[string]bool{"580.65.06": true, "550.54.14": false},
		BaseURLs: []string{"file:///repo/BaseOS", "file:///repo/AppStream"}}
	if !reflect.DeepEqual(r["gpu01"], want1) {
		t.Errorf("gpu01:\n%+v\n%+v", r["gpu01"], want1)
	}
	if g := r["gpu02"]; g.Err != "" || g.Driver != "" || !g.Nouveau || g.GPUs != 0 || g.Hostname != "gpu02" {
		t.Errorf("gpu02: %+v", g)
	}
	if r["gpu03"].Err != "현황 출력이 중간에 끊김" {
		t.Errorf("gpu03: %+v", r["gpu03"])
	}
	if r["gpu04"].Err != "ERROR: SSH 접속 실패: dial tcp: i/o timeout" {
		t.Errorf("gpu04: %+v", r["gpu04"])
	}
	if r["gpu05"].Err != "응답 없음" {
		t.Errorf("gpu05: %+v", r["gpu05"])
	}
}

func TestBuildTargets(t *testing.T) {
	es := []Entry{{"a", "550"}, {"b", "550"}, {"c", "580"}, {"d", "550"}}
	sts := map[string]*HostStatus{
		"a": {UserMount: true, RunReadable: map[string]bool{"550": true}},
		"b": {UserMount: false, RunReadable: map[string]bool{"550": true}},
		"c": {UserMount: true, RunReadable: map[string]bool{"550": true, "580": false}},
		"d": {Err: "응답 없음"},
	}
	ts := BuildTargets(es, sts)
	got := []string{ts[0].Method, ts[1].Method, ts[2].Method, ts[3].Fail}
	if !reflect.DeepEqual(got, []string{"mount", "scp", "scp", "응답 없음"}) {
		t.Errorf("%v", got)
	}
	var out bytes.Buffer
	PrintStatus(&out, ts)
	if !strings.Contains(out.String(), "fail: 응답 없음") || !strings.Contains(out.String(), "GPU 없음") {
		t.Errorf("표: %s", out.String())
	}
}

func TestConfirmStart(t *testing.T) {
	es := []Entry{{"gpu01", "550.54.14"}}
	var out bytes.Buffer
	if !ConfirmStart(bufio.NewReader(strings.NewReader("y\n")), &out, es) {
		t.Error("y 면 진행")
	}
	s := out.String()
	if !strings.Contains(s, red) || !strings.Contains(s, "gpu01") || !strings.Contains(s, "550.54.14") || !strings.Contains(s, "드레인") {
		t.Errorf("경고 출력: %q", s)
	}
	for _, in := range []string{"n\n", "yes\n", ""} {
		if ConfirmStart(bufio.NewReader(strings.NewReader(in)), &out, es) {
			t.Errorf("%q 는 취소", in)
		}
	}
}

func reinstallTargets() []*Target {
	return []*Target{
		{Entry: Entry{"a", "580"}, Status: &HostStatus{Driver: "550"}},
		{Entry: Entry{"b", "580"}, Status: &HostStatus{Driver: "580"}},
		{Entry: Entry{"c", "580"}, Status: &HostStatus{}},                // 드라이버 없음 → 질문 대상 아님
		{Entry: Entry{"d", "580"}, Status: &HostStatus{}, Fail: "응답 없음"}, // fail 은 대상 아님
		{Entry: Entry{"e", "580"}, Status: &HostStatus{Driver: "470"}},
	}
}

func skips(ts []*Target) []string {
	var s []string
	for _, t := range ts {
		s = append(s, t.Skip)
	}
	return s
}

func TestAskReinstall(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"a\n", []string{"", "같은 버전 설치됨", "", "", ""}},
		{"n\n", []string{"재설치 안 함", "같은 버전 설치됨", "", "", "재설치 안 함"}},
		{"x\ns\n9\n2, 3\n", []string{"재설치 안 함", "", "", "", ""}},
	}
	for _, c := range cases {
		ts := reinstallTargets()
		var out bytes.Buffer
		if err := AskReinstall(bufio.NewReader(strings.NewReader(c.in)), &out, ts); err != nil {
			t.Fatalf("%q: %v", c.in, err)
		}
		if got := skips(ts); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%q: %q", c.in, got)
		}
		if !strings.Contains(out.String(), "2) b 580 → 580  (같은 버전, 기본 건너뜀)") || strings.Contains(out.String(), ") c ") {
			t.Errorf("목록: %s", out.String())
		}
	}
	if err := AskReinstall(bufio.NewReader(strings.NewReader("")), &bytes.Buffer{}, reinstallTargets()); err == nil {
		t.Error("EOF 오류 기대")
	}
	// 기존 드라이버가 없으면 묻지 않음
	var out bytes.Buffer
	ts := []*Target{{Entry: Entry{"c", "580"}, Status: &HostStatus{}}}
	if err := AskReinstall(bufio.NewReader(strings.NewReader("")), &out, ts); err != nil || out.Len() != 0 {
		t.Errorf("질문하면 안 됨: %v %q", err, out.String())
	}
}

func TestRunParallel(t *testing.T) {
	var ts []*Target
	for i := 0; i < 20; i++ {
		ts = append(ts, &Target{})
	}
	ts[3].Skip, ts[4].Fail = "x", "y"
	var cur, max, n int32
	var mu sync.Mutex
	RunParallel(ts, 3, func(tt *Target) {
		c := atomic.AddInt32(&cur, 1)
		mu.Lock()
		if c > max {
			max = c
		}
		mu.Unlock()
		time.Sleep(5 * time.Millisecond)
		atomic.AddInt32(&n, 1)
		tt.Method = "done"
		atomic.AddInt32(&cur, -1)
	})
	if n != 18 || max > 3 || max < 2 || ts[3].Method != "" || ts[0].Method != "done" {
		t.Errorf("n=%d max=%d", n, max)
	}
}
