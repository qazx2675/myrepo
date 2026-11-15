package main

import "testing"

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
