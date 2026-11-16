package main

import (
	"reflect"
	"testing"
)

func TestVendorDefaults(t *testing.T) {
	cfg := defaultConfig()
	cases := map[string]string{
		"s001": "sds", "sh01": "D", "h01": "D", "c01": "D", "s2h01": "D", "s4h1": "D",
		"p1": "L", "l1": "j", "d1": "t", "m1": "W", "x1": "기타", "s3x": "sds",
	}
	for h, want := range cases {
		if got := cfg.Vendor(h); got != want {
			t.Errorf("Vendor(%q)=%q want %q", h, got, want)
		}
	}
}

func TestVendorCustom(t *testing.T) {
	cfg := defaultConfig()
	var d, v bool
	for _, kv := range [][2]string{
		{"vendor", "^svr=AAA"}, {"vendor", "^s=BBB"}, {"vendor_other", "etc"},
	} {
		if err := cfg.set(kv[0], kv[1], &d, &v); err != nil {
			t.Fatal(err)
		}
	}
	cases := map[string]string{"svr1": "AAA", "s1": "BBB", "h01": "etc", "p1": "etc"}
	for h, want := range cases {
		if got := cfg.Vendor(h); got != want {
			t.Errorf("Vendor(%q)=%q want %q", h, got, want)
		}
	}
	if got, want := cfg.VendorOrder(), []string{"AAA", "BBB", "etc"}; !reflect.DeepEqual(got, want) {
		t.Errorf("VendorOrder=%v want %v", got, want)
	}
}

func TestVendorOrderDefault(t *testing.T) {
	want := []string{"D", "sds", "L", "j", "t", "W", "기타"}
	if got := defaultConfig().VendorOrder(); !reflect.DeepEqual(got, want) {
		t.Errorf("VendorOrder=%v want %v", got, want)
	}
}
