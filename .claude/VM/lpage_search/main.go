package main

import (
	"flag"
	"fmt"
	"math"
	"os"
)

// ESXi Large Page (2MB) 및 메모리 상태 상수 (단위: MB)
const (
	LargePageSizeMB  = 2    // 2MB Large Page
	PagePerGB        = 512  // 1GB당 2MB Page 개수 (511-512 mapped)
	DefaultMinFreePct = 0.02 // 호스트 기본 minFree 비율 (약 1.5~2%)
)

type LPageCalculator struct {
	HostName        string
	HostTotalMemMB  int64
	Ev01MemMB       int64
	VcpuCountEv01   int
	VcpuCountEv02   int
}

// VM 기본 오버헤드 계산 (vCPU 및 할당 메모리 기반 추정)
func calculateVmOverheadMB(memMB int64, vcpu int) int64 {
	// Base VM kernel overhead + Page table overhead (약 1~1.5% + vCPU당 오버헤드)
	baseOverhead := int64(math.Ceil(float64(memMB) * 0.012))
	vcpuOverhead := int64(vcpu * 64) // vCPU당 약 64MB 스케줄링/MMU 오버헤드
	return baseOverhead + vcpuOverhead
}

// ESXi High State 유지를 위한 최소 여유 메모리 (3 * minFree)
func calculateHostReservedMB(hostTotalMB int64) int64 {
	minFree := float64(hostTotalMB) * DefaultMinFreePct
	highThreshold := minFree * 3.0 // Large Page가 깨지지 않는 High State 임계치
	systemBase := 4096.0           // ESXi 시스템 기본 상주 영역 (약 4GB)
	return int64(math.Ceil(highThreshold + systemBase))
}

func main() {
	hostFlag := flag.String("h", "", "ESXi Host IP/FQDN or Total Memory in GB (e.g. esxi01 or 512)")
	ev01Flag := flag.Int64("v1", 0, "ev01 node allocated memory size in GB (e.g. 240)")
	hostMemGB := flag.Int64("hm", 512, "Host Total Memory in GB (if -h is hostname)")
	vcpu01 := flag.Int("vcpu1", 32, "ev01 vCPU count")
	vcpu02 := flag.Int("vcpu2", 32, "ev02 vCPU count")

	flag.Parse()

	if *hostFlag == "" || *ev01Flag <= 0 {
		fmt.Println("Usage: go run main.go -h <Host> -v1 <ev01_Memory_GB> [options]")
		fmt.Println("Example: go run main.go -h 192.168.10.50 -v1 240")
		os.Exit(1)
	}

	ev01MemMB := *ev01Flag * 1024
	hostTotalMB := *hostMemGB * 1024

	// 1. ESXi 시스템 예약 및 Large Page 보장 버퍼 계산
	hostReservedMB := calculateHostReservedMB(hostTotalMB)

	// 2. ev01 오버헤드 산출
	ev01OverheadMB := calculateVmOverheadMB(ev01MemMB, *vcpu01)

	// 3. ev02 가용 메모리 풀 산출
	// HostTotal - HostReserved - (ev01 + ev01_Overhead) - ev02_Overhead_Buffer
	availableForEv02 := hostTotalMB - hostReservedMB - (ev01MemMB + ev01OverheadMB)

	// ev02 자체 오버헤드를 제외한 실제 할당 가능 용량 역산
	ev02OverheadEstimated := calculateVmOverheadMB(availableForEv02, *vcpu02)
	rawEv02MB := availableForEv02 - ev02OverheadEstimated

	// 4. 2MB Large Page 경계 및 1GB 단위 정렬 (Safe Truncation)
	alignedEv02GB := rawEv02MB / 1024
	if alignedEv02GB%2 != 0 {
		alignedEv02GB -= 1 // 짝수 GB 단위 정렬 권장
	}
	finalEv02MB := alignedEv02GB * 1024

	// 5. 결과 출력
	fmt.Println("==================================================")
	fmt.Printf("[ESXi Large Page (2MB) Memory Sizing Tool]\n")
	fmt.Printf("Target Host           : %s (%d GB)\n", *hostFlag, *hostMemGB)
	fmt.Printf("Configured ev01       : %d GB (%d MB)\n", *ev01Flag, ev01MemMB)
	fmt.Println("--------------------------------------------------")
	fmt.Printf("ESXi High-State Buffer: %d MB (3x minFree + Base)\n", hostReservedMB)
	fmt.Printf("ev01 VM Overhead      : %d MB\n", ev01OverheadMB)
	fmt.Printf("ev02 Est. Overhead    : %d MB\n", ev02OverheadEstimated)
	fmt.Println("--------------------------------------------------")
	fmt.Printf(">> Recommended ev02 Size: %d GB (%d MB)\n", alignedEv02GB, finalEv02MB)
	fmt.Printf("   (Total 2MB LPage Mappings: %d pages)\n", (finalEv02MB / LargePageSizeMB))
	fmt.Println("==================================================")
}
