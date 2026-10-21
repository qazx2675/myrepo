package main

import (
	"fmt"
	"testing"
)

// tuiBigVGs 는 전체 설정 비교 규모의 결과입니다 (호스트 hosts대 × 속성 attrs개, 한 관리망 그룹).
func tuiBigVGs(hosts, attrs int) []VendorGroups {
	g := LineGroup{Name: "iLO 6"}
	for h := 0; h < hosts; h++ {
		host := fmt.Sprintf("node%03d", h)
		g.Hosts = append(g.Hosts, host)
		for a := 0; a < attrs; a++ {
			tag := TagOK
			if a%7 == 0 {
				tag = TagDiff
			}
			g.Lines = append(g.Lines, ResultLine{Kind: KindCompare, Tag: tag, Host: host, IP: fmt.Sprintf("10.0.%d.%d", h/250, h%250),
				Vendor: "HPE", Model: "ProLiant DL360 Gen11", BiosVersion: "U54 v2.16", MgmtModel: "iLO 6",
				Attr: fmt.Sprintf("Attribute%04d", a), Expected: "Enabled", Current: "Disabled", Note: "-"})
		}
	}
	return []VendorGroups{{Vendor: "HPE", Groups: []LineGroup{g}}}
}

// BenchmarkTUIFrameBig 는 50대 × 1500 속성 화면에서 한 줄 내리고 다시 그리는 시간입니다 (키 1번 = 1 op).
func BenchmarkTUIFrameBig(b *testing.B) {
	u := &TUI{env: tuiEnv{Color: true}}
	u.SetResults("전체 설정 비교", tuiBigVGs(50, 1500))
	u.frame(199, 50)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		u.navKey(tuiKey{k: tkDown})
		u.frame(199, 50)
	}
}

// BenchmarkTUIFilterSwitch 는 같은 화면에서 필터를 a ↔ f 로 바꿀 때(표 캐시를 다시 만듦) 한 번의 시간입니다.
func BenchmarkTUIFilterSwitch(b *testing.B) {
	u := &TUI{env: tuiEnv{Color: true}}
	u.SetResults("전체 설정 비교", tuiBigVGs(50, 1500))
	u.frame(199, 50)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		f := rune(tuiFilterDiff)
		if i%2 == 1 {
			f = tuiFilterAll
		}
		u.navKey(tuiKey{k: tkRune, r: f})
		u.frame(199, 50)
	}
}
