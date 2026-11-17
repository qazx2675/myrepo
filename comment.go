// comment.go - 설치현황(4.3)·점검요청(4.4)·yum 표(4.8) 렌더. Snapshot 만 받는 순수 함수다.
package main

import (
	"fmt"
	"strings"
	"time"
)

// InstallComment 는 설치현황 코멘트를 만든다(계획서 4.3, yum 서버가 있으면 4.8).
//
// 요약 줄 규칙: 설치완료(재부팅됨 포함)·설치중(지연 포함)은 0대여도 항상 쓰고,
// 디스크 미인식·ping X·완료후꺼짐은 0대면 뺀다. 확인전 서버는 전체 수에만 들어간다.
// yum 서버가 있으면 위 줄은 OS 서버만 세고(N대 중 의 N 도 OS 서버 수), 그 아래 yum 요약 줄과 목록이 붙는다.
// yum 서버가 없으면 출력은 v0.3.0 과 바이트 단위로 같다. 끝에 줄바꿈은 붙이지 않는다.
func InstallComment(s Snapshot) string {
	c := s.osCounts()
	hasYum := s.Yum.Total > 0
	osTotal := s.Total - s.Yum.Total
	installing := c[StatusInstalling] + c[StatusDelayed]
	var b strings.Builder
	b.WriteString("설치현황")
	if !hasYum || osTotal > 0 { // yum 서버만 있으면 OS 요약 줄은 쓰지 않는다
		fmt.Fprintf(&b, "\n%d대 중 %d대 설치완료, %d대 설치중", osTotal, c[StatusDone]+c[StatusRebooted], installing)
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
	}
	if hasYum {
		y := s.Yum
		fmt.Fprintf(&b, "\nyum update %d대 중 %d대 커널 변경 완료(%s)", y.Total, y.Done, StatusYumDone)
		if y.Alive > 0 {
			fmt.Fprintf(&b, ", %s %d대", StatusAlive, y.Alive)
		}
		if y.Rebooted > 0 {
			fmt.Fprintf(&b, ", %s %d대", StatusRebooted, y.Rebooted)
			if y.SameKernel > 0 {
				fmt.Fprintf(&b, "(커널 동일 %d대)", y.SameKernel)
			}
		}
		if y.PingX > 0 {
			fmt.Fprintf(&b, ", ping X %d대", y.PingX)
		}
		if y.Off > 0 {
			fmt.Fprintf(&b, ", 완료후꺼짐 %d대", y.Off)
		}
		if y.Delayed > 0 {
			fmt.Fprintf(&b, " (지연 %d대)", y.Delayed)
		}
		writeList(&b, "yum 접속불가", y.PingXHosts)
		writeList(&b, "yum 재부팅 대기", y.WaitingHosts)
		writeList(&b, "yum 재부팅 중", y.RebootingHosts)
		writeList(&b, "yum 재부팅됨 커널 동일", y.SameKernelHosts)
	}
	for _, g := range s.Vendors { // OS 서버만 담겨 있다
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
		if r.Status == StatusRebooted && !r.IsYum() { // yum 서버의 재부팅됨은 위 yum 목록에 있다
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

// delayBlock 은 점검요청 블록 하나(담당자 인사, 문장, 맺음, 서버 목록)다.
func delayBlock(vendor, what string, hosts []string) string {
	return fmt.Sprintf("안녕하세요 %s 담당자님\n하기서버 %d대는 %s\n점검부탁드립니다.\n감사합니다.\n%s",
		vendor, len(hosts), what, strings.Join(hosts, " "))
}

const (
	osDelayText      = "OS 설치가 지연되고 있습니다."
	yumWaitingText   = "yum update 후 재부팅이 확인되지 않고 있습니다."
	yumSameKernelTxt = "yum update 후 재부팅은 되었으나 커널이 변경되지 않았습니다."
	yumRebootingText = "yum update 후 재부팅한 뒤 접속되지 않고 있습니다."
)

// DelayComment 는 지연 서버의 벤더별 점검요청 코멘트를 만든다(계획서 4.4, config_check 문구 구조).
// 벤더마다 OS 설치 지연 블록, yum 재부팅 미확인(A), 커널 동일(B), 재부팅 뒤 접속 안 됨(C) 블록 순이다(계획서 4.8).
// 블록 사이는 빈 줄 하나다. OS·yum 지연 서버가 모두 없으면 안내 문구를 돌려준다.
func DelayComment(s Snapshot) string {
	yum := make(map[string]YumVendorGroup, len(s.Yum.Vendors))
	for _, g := range s.Yum.Vendors {
		yum[g.Vendor] = g
	}
	var blocks []string
	yumBlocks := func(g YumVendorGroup) {
		if len(g.Waiting) > 0 {
			blocks = append(blocks, delayBlock(g.Vendor, yumWaitingText, g.Waiting))
		}
		if len(g.SameKernel) > 0 {
			blocks = append(blocks, delayBlock(g.Vendor, yumSameKernelTxt, g.SameKernel))
		}
		if len(g.Rebooting) > 0 {
			blocks = append(blocks, delayBlock(g.Vendor, yumRebootingText, g.Rebooting))
		}
	}
	for _, g := range s.Vendors {
		if hosts := g.Hosts[StatusDelayed]; len(hosts) > 0 {
			blocks = append(blocks, delayBlock(g.Vendor, osDelayText, hosts))
		}
		if yg, ok := yum[g.Vendor]; ok {
			yumBlocks(yg)
			delete(yum, g.Vendor)
		}
	}
	for _, g := range s.Yum.Vendors { // OS 벤더 목록에 없던 벤더(드묾)
		if yg, ok := yum[g.Vendor]; ok {
			yumBlocks(yg)
		}
	}
	if len(blocks) == 0 {
		return "지연된 서버가 없습니다."
	}
	return strings.Join(blocks, "\n\n")
}

// YumTable 은 `y` 패널 본문이다(계획서 4.8). yum 서버의 탭 구분 표이며 ssh·ping 없이 스냅샷만 쓴다.
// 줄 순서는 스냅샷 순서(상태 번호 -> hostname)이고, 비고는 상태 이유에 지연이 있으면 `지연 N분`(기준 시각부터 now 까지)을 잇는다.
// 머리줄에 비고 칸이 항상 있고, 비고가 없는 줄은 빈 칸으로 둔다. now 는 지연 판정 기준 시각이다.
func YumTable(s Snapshot, now time.Time) string {
	delay := time.Duration(s.Yum.DelayMin) * time.Minute
	var sb strings.Builder
	sb.WriteString("hostname\t벤더\t상태\t기준 커널\t현재 커널\t재부팅\t비고\n")
	n := 0
	for _, r := range s.Records {
		if !r.IsYum() {
			continue
		}
		n++
		note := r.Reason
		if yumDelayed(r, now, delay) {
			if note != "" {
				note += "; "
			}
			note += fmt.Sprintf("지연 %d분", int(now.Sub(r.YumSince)/time.Minute))
		}
		reboot := "-"
		if !r.RebootedAt.IsZero() {
			reboot = r.RebootedAt.Format("01-02 15:04")
		}
		fmt.Fprintf(&sb, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n", r.Name, r.Vendor, r.Status,
			dashIfEmpty(r.YumKernel), dashIfEmpty(r.YumNow), reboot, note)
	}
	if n == 0 {
		return "yum 서버가 없습니다."
	}
	return strings.TrimRight(sb.String(), "\n")
}

func dashIfEmpty(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
