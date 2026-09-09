package main

import "testing"

func TestFormatResultLineOK(t *testing.T) {
	got, ok := formatResultLine("host1", "RESULT|OK|host1|192.168.1.50|3.3.3.3|3.3.3.1")
	if !ok {
		t.Fatalf("want ok=true, got %v (%q)", ok, got)
	}
	want := "host1                192.168.1.50 -> 3.3.3.3   (GW 3.3.3.1)"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestFormatResultLineFail(t *testing.T) {
	got, ok := formatResultLine("host1", "RESULT|FAIL|host1|ifcfg 파일을 찾지 못했습니다")
	if ok {
		t.Fatalf("want ok=false, got %v (%q)", ok, got)
	}
	want := "host1                FAIL: ifcfg 파일을 찾지 못했습니다"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestFormatResultLineMalformed(t *testing.T) {
	got, ok := formatResultLine("host1", "garbage output")
	if ok {
		t.Fatalf("want ok=false for malformed line")
	}
	if got == "" {
		t.Fatalf("want non-empty fallback message")
	}
}
