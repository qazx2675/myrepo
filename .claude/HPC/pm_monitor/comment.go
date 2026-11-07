// comment.go - 설치현황(4.3)·점검요청(4.4) 코멘트 렌더. Snapshot 만 받는 순수 함수다.
package main

import (
	"fmt"
	"strings"
)

// InstallComment 는 설치현황 코멘트를 만든다(계획서 4.3).
//
// 요약 줄 규칙: 설치완료(재부팅됨 포함)·설치중(지연 포함)은 0대여도 항상 쓰고,
// 디스크 미인식·ping X·완료후꺼짐은 0대면 뺀다. 확인전 서버는 전체 수에만 들어간다.
// 끝에 줄바꿈은 붙이지 않는다.
func InstallComment(s Snapshot) string {
	c := s.Counts
	installing := c[StatusInstalling] + c[StatusDelayed]
	var b strings.Builder
	b.WriteString("설치현황\n")
	fmt.Fprintf(&b, "%d대 중 %d대 설치완료, %d대 설치중", s.Total, c[StatusDone]+c[StatusRebooted], installing)
	if n := c[StatusDelayed]; n > 0 {
		fmt.Fprintf(&b, " (지연 %d대)", n)
	}
	if n := c[StatusDisk]; n > 0 {
		fmt.Fprintf(&b, ", %d대 디스크 미인식", n)
	}
	if n := c[StatusPingX]; n > 0 {
		fmt.Fprintf(&b, ", ping X %d대", n)
	}
	if n := c[StatusOff]; n > 0 {
		fmt.Fprintf(&b, ", 완료후꺼짐 %d대", n)
	}
	for _, g := range s.Vendors {
		disk, px, off := g.Hosts[StatusDisk], g.Hosts[StatusPingX], g.Hosts[StatusOff]
		if len(disk)+len(px)+len(off) == 0 {
			continue
		}
		b.WriteString("\n" + g.Vendor)
		writeList(&b, "디스크 미인식", disk)
		writeList(&b, "접속불가", px)
		writeList(&b, "완료후꺼짐", off)
	}
	var reb []string
	for _, r := range s.Records { // 상태·이름 순이라 이름 순이 된다
		if r.Status == StatusRebooted {
			reb = append(reb, r.Name)
		}
	}
	writeList(&b, "재부팅됨", reb)
	return b.String()
}

func writeList(b *strings.Builder, title string, hosts []string) {
	if len(hosts) == 0 {
		return
	}
	fmt.Fprintf(b, "\n%s %d대\n%s", title, len(hosts), strings.Join(hosts, " "))
}

// DelayComment 는 지연 서버의 벤더별 점검요청 코멘트를 만든다(계획서 4.4, config_check 문구 구조).
// 벤더 블록 사이는 빈 줄 하나다. 지연 서버가 없으면 안내 문구를 돌려준다.
func DelayComment(s Snapshot) string {
	var blocks []string
	for _, g := range s.Vendors {
		hosts := g.Hosts[StatusDelayed]
		if len(hosts) == 0 {
			continue
		}
		blocks = append(blocks, fmt.Sprintf("안녕하세요 %s 담당자님\n하기서버 %d대는 OS 설치가 지연되고 있습니다.\n점검부탁드립니다.\n감사합니다.\n%s",
			g.Vendor, len(hosts), strings.Join(hosts, " ")))
	}
	if len(blocks) == 0 {
		return "지연된 서버가 없습니다."
	}
	return strings.Join(blocks, "\n\n")
}
