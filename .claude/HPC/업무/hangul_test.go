// hpcbot — 영타→한글(2벌식) 변환 테스트
package main

import "testing"

func TestHangulConvert(t *testing.T) {
	cases := []struct{ in, want string }{
		{"xptmxm", "테스트"},
		{"dlrpehlfRk?", "이게될까?"},
		{"rmfovlr", "그래픽"},
		{"emfkdlqj", "드라이버"},
		{"tjfcl", "설치"},
		{"dkssudgktpdy", "안녕하세요"},
		{"s", "ㄴ"},
		{"r", "ㄱ"},
		{"k", "ㅏ"},
		{"A", "ㅁ"},
		{"dlfrdj", "읽어"},
		{"hk", "ㅘ"},
		{"Rk", "까"},
	}
	for _, c := range cases {
		if got := ConvertEngToHangul(c.in); got != c.want {
			t.Errorf("ConvertEngToHangul(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestHangulTokens(t *testing.T) {
	prot := map[string]bool{"gpu": true, "nvidia": true, "sssd": true}
	if got := ConvertTokens("gpu emfkdlqj tjfcl", prot); got != "gpu 드라이버 설치" {
		t.Errorf("ConvertTokens = %q", got)
	}
	if got := ConvertTokens("GPU  tjfcl", prot); got != "GPU 설치" {
		t.Errorf("ConvertTokens case = %q", got)
	}
	if got, ch := AutoConvert("nvidia sssd", prot); got != "nvidia sssd" || ch {
		t.Errorf("AutoConvert protected = %q,%v", got, ch)
	}
	if got, ch := AutoConvert("gpu emfkdlqj tjfcl", prot); got != "gpu 드라이버 설치" || !ch {
		t.Errorf("AutoConvert = %q,%v", got, ch)
	}
	if got, ch := AutoConvert("그래픽 드라이버 설치", prot); got != "그래픽 드라이버 설치" || ch {
		t.Errorf("AutoConvert korean = %q,%v", got, ch)
	}
	if got, ch := AutoConvert("그래픽 tjfcl", prot); got != "그래픽 설치" || !ch {
		t.Errorf("AutoConvert mixed = %q,%v", got, ch)
	}
	for _, tok := range []string{"s", "k", "gpu", "a1b", "nvidia"} {
		if IsConvertibleToken(tok, prot) {
			t.Errorf("IsConvertibleToken(%q) should be false", tok)
		}
	}
	if !IsConvertibleToken("tjfcl", prot) {
		t.Error("tjfcl should be convertible")
	}
	// sssd is not protected here: check its behavior
	t.Logf("sssd -> %q convertible=%v", ConvertEngToHangul("sssd"), IsConvertibleToken("sssd", nil))
}
