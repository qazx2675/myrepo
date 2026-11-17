package main

import (
	"path/filepath"
	"reflect"
	"testing"
)

func TestParseArgs(t *testing.T) {
	cases := []struct {
		args []string
		want options
		ok   bool
	}{
		{[]string{"hosts"}, options{hostfile: "hosts"}, true},
		{[]string{"-c", "x.conf", "hosts"}, options{conf: "x.conf", hostfile: "hosts"}, true},
		{[]string{"hosts", "-c", "x.conf"}, options{conf: "x.conf", hostfile: "hosts"}, true},
		{[]string{"-f", "hosts"}, options{mode: modeFG, hostfile: "hosts"}, true},
		{[]string{"--daemon", "-c", "/a/b.conf", "/h"}, options{mode: modeDaemon, conf: "/a/b.conf", hostfile: "/h"}, true},
		{[]string{"--stop", "hosts"}, options{mode: modeStop, hostfile: "hosts"}, true},
		{[]string{"--status"}, options{mode: modeStatus}, true},
		{[]string{"--status", "hosts"}, options{mode: modeStatus, hostfile: "hosts"}, true},
		{[]string{}, options{}, false},
		{[]string{"--stop"}, options{}, false},            // stop 은 호스트파일이 필요
		{[]string{"-f", "--stop", "h"}, options{}, false}, // 방식은 하나만
		{[]string{"-c"}, options{}, false},
		{[]string{"-x", "hosts"}, options{}, false},
		{[]string{"a", "b"}, options{}, false},
		{[]string{"--status", "-c", "x"}, options{}, false},
	}
	for _, c := range cases {
		got, ok := parseArgs(c.args)
		if ok != c.ok || (ok && got != c.want) {
			t.Errorf("parseArgs(%v) = %+v, %v; 기대 %+v, %v", c.args, got, ok, c.want, c.ok)
		}
	}
}

// 계획서 4.8 "호스트파일 형식" 의 예시 줄 그대로.
func TestReadHostSpecsPlanExamples(t *testing.T) {
	p := filepath.Join(t.TempDir(), "hosts")
	writeFile(t, p, "svr001            # 두 번째 칸 없음 -> OS 설치(기존 동작 그대로)\n"+
		"svr002 yum        # yum update 모드\n"+
		"svr003  YUM       # 대소문자 무시, 공백·탭 여러 개 허용\n"+
		"svr004 kickstart  # yum 이 아닌 값 -> OS 설치로 간주(두 번째 칸 무시)\n")
	hosts, yum, err := readHostSpecs(p)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"svr001", "svr002", "svr003", "svr004"}; !reflect.DeepEqual(hosts, want) {
		t.Errorf("hosts=%v, want %v", hosts, want)
	}
	if want := map[string]bool{"svr002": true, "svr003": true}; !reflect.DeepEqual(yum, want) {
		t.Errorf("yum=%v, want %v", yum, want)
	}
}

func TestReadHostSpecsRules(t *testing.T) {
	cases := []struct {
		name  string
		in    string
		hosts []string
		yum   []string // yum 모드인 hostname
	}{
		{"OS 한 칸", "a\n", []string{"a"}, nil},
		{"주석이 붙은 OS 줄", "svr001   # note\n", []string{"svr001"}, nil},
		{"yum 에 주석", "svr002 yum # note\n", []string{"svr002"}, []string{"svr002"}},
		{"주석이 두 번째 칸에 붙은 yum 은 OS", "svr002 #yum\n", []string{"svr002"}, nil},
		{"탭", "a\tyum\nb\t\tYum\n", []string{"a", "b"}, []string{"a", "b"}},
		{"공백 여러 개와 앞뒤 공백", "   a     yum   \n\t b \t YUM\t\n", []string{"a", "b"}, []string{"a", "b"}},
		{"대소문자", "a yum\nb Yum\nc YUM\nd yUm\n", []string{"a", "b", "c", "d"}, []string{"a", "b", "c", "d"}},
		{"세 번째 칸은 무시", "a yum extra stuff\nb kickstart yum\nc os yum\n", []string{"a", "b", "c"}, []string{"a"}},
		{"yum 비슷한 글자는 OS", "a yums\nb yu\nc yum2\nd -yum\n", []string{"a", "b", "c", "d"}, nil},
		{"빈 줄과 # 줄", "\n# x yum\n   \n  # y yum\na yum\n\n", []string{"a"}, []string{"a"}},
		{"CRLF", "a yum\r\nb\r\nc YUM\r\n", []string{"a", "b", "c"}, []string{"a", "c"}},
		{"마지막 줄 개행 없음", "a\nb yum", []string{"a", "b"}, []string{"b"}},
		{"중복은 처음 줄만(yum 이 먼저)", "a yum\nb\na\n", []string{"a", "b"}, []string{"a"}},
		{"중복은 처음 줄만(OS 가 먼저)", "a\nb\na yum\n", []string{"a", "b"}, nil},
		{"빈 파일", "", nil, nil},
	}
	for _, c := range cases {
		p := filepath.Join(t.TempDir(), "hosts")
		writeFile(t, p, c.in)
		hosts, yum, err := readHostSpecs(p)
		if err != nil {
			t.Errorf("%s: %v", c.name, err)
			continue
		}
		if !reflect.DeepEqual(hosts, c.hosts) {
			t.Errorf("%s: hosts=%v, want %v", c.name, hosts, c.hosts)
		}
		want := map[string]bool{}
		for _, h := range c.yum {
			want[h] = true
		}
		if !reflect.DeepEqual(yum, want) {
			t.Errorf("%s: yum=%v, want %v", c.name, yum, want)
		}
	}
	if _, _, err := readHostSpecs(filepath.Join(t.TempDir(), "없음")); err == nil {
		t.Error("없는 파일은 오류여야 함")
	}
}

// readHosts 는 모드를 버리고 이름만 돌려주며, 두 번째 칸이 있어도 hostname 은 첫 칸이다.
func TestReadHostsWrapperIgnoresMode(t *testing.T) {
	p := filepath.Join(t.TempDir(), "hosts")
	writeFile(t, p, "a yum\nb kickstart\nc\n")
	got, err := readHosts(p)
	if err != nil || !reflect.DeepEqual(got, []string{"a", "b", "c"}) {
		t.Errorf("readHosts: %v %v", got, err)
	}
}
