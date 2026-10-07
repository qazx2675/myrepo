package main

import "strings"

// modelPrefixes 는 모델명 앞에서 떼어 내는 벤더명·제품군 접두입니다 (공백·하이픈·밑줄을 지운 소문자 기준).
// 겹치는 것은 긴 쪽이 앞에 옵니다 (dellemc → dell, hpe → hp).
var modelPrefixes = []string{
	"hewlettpackardenterprise", "thinksystem", "thinkserver", "supermicro", "poweredge", "proliant",
	"dellemc", "lenovo", "cisco", "dell", "hpe", "hp",
}

// normalizeModel 은 모델명 비교용 키를 만듭니다. 프로파일의 model 열과 BMC 가 보고한 Model 을
// 같은 키로 맞추는 한곳의 규칙이며, all_bios_check(단계 5b)도 이 함수를 씁니다.
//
//	"ProLiant DL360 Gen11" / "DL360 Gen11" / "DL360Gen11" → "dl360gen11"
//	"PowerEdge R660" → "r660",  "ThinkSystem SR650 V3" → "sr650v3"
//
// 소문자로 바꾸고 공백·하이픈·밑줄을 지운 뒤, 앞의 벤더명/제품군 접두를 (남는 글자가 있을 때만) 반복해서 뗍니다.
func normalizeModel(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		switch r {
		case ' ', '\t', '-', '_', '\u00a0':
			continue
		}
		b.WriteRune(r)
	}
	out := b.String()
	for stripped := true; stripped; {
		stripped = false
		for _, p := range modelPrefixes {
			if len(out) > len(p) && strings.HasPrefix(out, p) {
				out = out[len(p):]
				stripped = true
				break
			}
		}
	}
	return out
}
