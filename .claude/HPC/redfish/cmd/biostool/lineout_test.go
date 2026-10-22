package main

import (
	"bytes"
	"os"
	"strings"
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
		c, n string // c = 태그·현재값 색, n = 비고 색
	}{
		{"OK", sampleLine(TagOK), lnGreen, lnGreen},
		{"difference", sampleLine(TagDiff), lnYellow, lnYellow},
		{"N/A", sampleLine(TagNA), lnGray, lnYellow},
	}
	for _, c := range cases {
		got := formatLine(c.l, true)
		want := c.c + "[" + c.l.Tag + "]" + lnReset + " hpe-node01\t10.0.0.11\tHPE\tDL380 Gen11\tU54 v2.30\tAutoEnrollPolicy\t" +
			lnGreen + "(없음)" + lnReset + "\t" + c.c + "auto" + lnReset + "\t" + c.n + "ONLY_HOST" + lnReset
		if got != want {
			t.Errorf("%s: got %q\nwant %q", c.name, got, want)
		}
	}
	// 호스트 오류 줄은 태그·현재값·비고만 빨강, 기준값은 그대로 초록
	e := ResultLine{Tag: TagNA, Host: "h3", IP: "10.0.0.13", Vendor: "HPE", Attr: "-", Expected: "-", Current: "-", Note: "UNREACHABLE", HostError: true}
	got := formatLine(e, true)
	want := lnRed + "[N/A]" + lnReset + " h3\t10.0.0.13\tHPE\t\t\t-\t" + lnGreen + "(N/A)" + lnReset + "\t" + lnRed + "(N/A)" + lnReset + "\t" + lnRed + "UNREACHABLE" + lnReset
	if got != want {
		t.Errorf("host error: got %q\nwant %q", got, want)
	}
}

// BMC 에서 온 제어 문자는 모든 칸에서 공백이 되고, formatLine 이 붙인 색 코드만 남는다.
func TestFormatLineStripsControl(t *testing.T) {
	l := ResultLine{Tag: TagDiff, Host: "h\x1b[2J", IP: "1\n2", Vendor: "V\x07", Model: "M\r", BiosVersion: "B\u009b1m",
		Attr: "A\tB", Expected: "e\x1b]0;t\x07", Current: "c\x7f", Note: "n\x1b[31m"}
	got := formatLine(l, false)
	want := "[difference] h [2J\t1 2\tV \tM \tB 1m\tA B\te ]0;t \tc \tn [31m"
	if got != want {
		t.Fatalf("got %q\nwant %q", got, want)
	}
	got = formatLine(l, true)
	if n := strings.Count(got, "\x1b"); n != 8 { // 태그·기준값·현재값·비고의 색 시작+끝
		t.Fatalf("ESC %d개 (8 기대): %q", n, got)
	}
	if strings.Count(got, "\t") != 8 || strings.ContainsAny(got, "\r\n\a\x7f\u009b") {
		t.Fatalf("칸 구분이 깨짐: %q", got)
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

// 비고 칸 색: OK 줄은 초록, 그 밖(difference·N/A)은 노랑, 호스트 오류는 빨강 (평문 출력과 TUI 가 같다).
func TestNoteColor(t *testing.T) {
	ok := ResultLine{Tag: TagOK, Host: "h", Attr: "A", Note: "REFERENCE"}
	df := ResultLine{Tag: TagDiff, Host: "h", Attr: "A", Note: "DIFF"}
	na := ResultLine{Tag: TagNA, Host: "h", Attr: "A", Note: "ONLY_REF"}
	er := ResultLine{Tag: TagNA, Host: "h", Attr: "-", Note: "UNREACHABLE", HostError: true}
	for _, c := range []struct {
		l          ResultLine
		plain, tui string
	}{{ok, lnGreen, tuiStGreen}, {df, lnYellow, tuiStYellow}, {na, lnYellow, tuiStYellow}, {er, lnRed, tuiStRed}} {
		if got := formatLine(c.l, true); !strings.HasSuffix(got, c.plain+c.l.Note+lnReset) {
			t.Errorf("%s 평문 비고 색: %q", c.l.Note, got)
		}
		if row := tuiLineRow(c.l); row[len(row)-1].st != c.tui {
			t.Errorf("%s TUI 비고 색 %q, 기대 %q", c.l.Note, row[len(row)-1].st, c.tui)
		}
	}
}
