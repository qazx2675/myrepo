package main

import (
	"bytes"
	"os"
	"testing"
)

func sampleLine(tag string) ResultLine {
	return ResultLine{Kind: KindCompare, Tag: tag, Host: "hpe-node01", IP: "10.0.0.11", Vendor: "HPE", Model: "DL380 Gen11",
		BiosVersion: "U54 v2.30", Attr: "AutoEnrollPolicy", Expected: noneValue, Current: "auto", Note: "ONLY_HOST"}
}

func TestFormatLineNoColor(t *testing.T) {
	got := formatLine(sampleLine(TagDiff), false)
	want := "[difference] hpe-node01\t10.0.0.11\tHPE\tDL380 Gen11\tU54 v2.30\tAutoEnrollPolicy\t(없음)\tauto\tONLY_HOST"
	if got != want {
		t.Fatalf("got %q\nwant %q", got, want)
	}
	l := sampleLine(TagNA)
	l.Expected, l.Current = "Enabled", "-"
	got = formatLine(l, false)
	want = "[N/A] hpe-node01\t10.0.0.11\tHPE\tDL380 Gen11\tU54 v2.30\tAutoEnrollPolicy\tEnabled\t(N/A)\tONLY_HOST"
	if got != want {
		t.Fatalf("got %q\nwant %q", got, want)
	}
}

func TestFormatLineColor(t *testing.T) {
	cases := []struct {
		name string
		l    ResultLine
		c    string
	}{
		{"OK", sampleLine(TagOK), lnGreen},
		{"difference", sampleLine(TagDiff), lnYellow},
		{"N/A", sampleLine(TagNA), lnGray},
	}
	for _, c := range cases {
		got := formatLine(c.l, true)
		want := c.c + "[" + c.l.Tag + "]" + lnReset + " hpe-node01\t10.0.0.11\tHPE\tDL380 Gen11\tU54 v2.30\tAutoEnrollPolicy\t" +
			lnGreen + "(없음)" + lnReset + "\t" + c.c + "auto" + lnReset + "\tONLY_HOST"
		if got != want {
			t.Errorf("%s: got %q\nwant %q", c.name, got, want)
		}
	}
	// 호스트 오류 줄은 빨강, 기준값은 그대로 초록
	e := ResultLine{Tag: TagNA, Host: "h3", IP: "10.0.0.13", Vendor: "HPE", Attr: "-", Expected: "-", Current: "-", Note: "UNREACHABLE", HostError: true}
	got := formatLine(e, true)
	want := lnRed + "[N/A]" + lnReset + " h3\t10.0.0.13\tHPE\t\t\t-\t" + lnGreen + "(N/A)" + lnReset + "\t" + lnRed + "(N/A)" + lnReset + "\tUNREACHABLE"
	if got != want {
		t.Errorf("host error: got %q\nwant %q", got, want)
	}
}

func TestWriteResultLines(t *testing.T) {
	var b bytes.Buffer
	writeResultLines(&b, []ResultLine{sampleLine(TagOK), sampleLine(TagDiff)}, false)
	want := formatLine(sampleLine(TagOK), false) + "\n" + formatLine(sampleLine(TagDiff), false) + "\n"
	if b.String() != want {
		t.Fatalf("got %q want %q", b.String(), want)
	}
}

func TestColorEnabled(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	if colorEnabled(os.Stdout) {
		t.Fatal("NO_COLOR 인데 색을 켰다")
	}
	t.Setenv("NO_COLOR", "")
	if colorEnabled(nil) {
		t.Fatal("nil 파일에 색을 켰다")
	}
	// 파일(터미널 아님)이면 꺼진다
	f, err := os.CreateTemp(t.TempDir(), "x")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if colorEnabled(f) {
		t.Fatal("일반 파일에 색을 켰다")
	}
}
