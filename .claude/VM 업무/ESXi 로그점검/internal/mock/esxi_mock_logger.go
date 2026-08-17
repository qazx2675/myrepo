package main

import (
	"flag"
	"fmt"
	"log"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type LogEntry struct {
	File    string
	DelayMs int
	Format  string
}

type ScenarioTemplate struct {
	ID   string
	Desc string
	Logs []LogEntry
}

var templates = []ScenarioTemplate{
	// 1~5: Storage APD / PDL (SCSI & NMP)
	{
		ID: "ESX8_APD_01", Desc: "ESXi 8.x NMP Blocked & APD Timeout",
		Logs: []LogEntry{
			{"vmkernel.log", 0, "{TIME} In(182) vmkernel: cpu{CPU}:2049)WARNING: NMP: nmp_DeviceStartLoop:721: NMP Device \"{DEV}\" is blocked. Not starting I/O from device."},
			{"vobd.log", 100, "{TIME} In(14) vobd[2098148]: [APDCorrelator] 1234567890us: [esx.problem.storage.apd.start] Device or filesystem with identifier [{DEV}] has entered the All Paths Down state."},
			{"vmkernel.log", 200, "{TIME} In(182) vmkernel: cpu0:2049)StorageApdHandler: 1204: APD Handle Created with lock [{DEV}]"},
			{"vobd.log", 14000, "{TIME} In(14) vobd[2098148]: [APDCorrelator] 1248567890us: [vob.storage.apd.timeout] Device or filesystem with identifier [{DEV}] has entered the All Paths Down Timeout state after being in the All Paths Down state for 140 seconds."},
		},
	},
	{
		ID: "ESX8_PDL_01", Desc: "ESXi 8.x NMP Permanent Device Loss",
		Logs: []LogEntry{
			{"vmkernel.log", 0, "{TIME} In(182) vmkernel: cpu{CPU}:2050)WARNING: NMP: nmp_IssueCommandToDevice:2954: I/O could not be issued to device \"{DEV}\" due to Not found"},
			{"vobd.log", 100, "{TIME} In(14) vobd[2098148]: [scsiCorrelator] 2234567890us: [esx.problem.scsi.device.state.permanentloss] The path to device {DEV} has been permanently lost."},
		},
	},
	// 6~10: Storage NVMeoF & HPP (ESXi 8.0 특화)
	{
		ID: "ESX8_NVME_HPP_01", Desc: "ESXi 8.0 NVMeoF HPP Path Failure",
		Logs: []LogEntry{
			{"vmkernel.log", 0, "{TIME} In(182) vmkernel: cpu{CPU}:2100)WARNING: HPP: HppDeviceFindActivePath:412: No active path found for device \"{NVME_DEV}\""},
			{"vmkernel.log", 100, "{TIME} In(182) vmkernel: cpu{CPU}:2100)WARNING: NVMe: NVMeTransportErrorHandler:1015: Transport error on ctrl {NVME_CTRL}, status: 0x2"},
			{"vobd.log", 200, "{TIME} In(14) vobd[2098148]: [scsiCorrelator] 3234567890us: [esx.problem.storage.connectivity.lost] Lost connectivity to storage device {NVME_DEV}. Path {NVME_PATH} is down."},
		},
	},
	// 11~15: vSAN ESA (Express Storage Architecture - ESXi 8.0 특화)
	{
		ID: "ESX8_VSAN_ESA_01", Desc: "vSAN 8.0 ESA Congestion & Unhealthy",
		Logs: []LogEntry{
			{"vobd.log", 0, "{TIME} In(14) vobd[2098148]: [vsanCorrelator] 4234567890us: [esx.problem.vsan.lsom.congestionthreshold] Device memory or SSD congestion levels have been updated for disk {VSAN_DISK}"},
			{"vmkernel.log", 100, "{TIME} In(182) vmkernel: cpu{CPU}:4100)WARNING: LSOM: LSOMEventMonitor:1015: High latency detected on ESA NVMe device {VSAN_DISK}"},
			{"vobd.log", 5000, "{TIME} In(14) vobd[2098148]: [vsanCorrelator] 4234572890us: [esx.problem.vob.vsan.lsom.diskunhealthy] vSAN device {VSAN_DISK} is reporting as unhealthy."},
		},
	},
	{
		ID: "ESX8_VSAN_ESA_02", Desc: "vSAN 8.0 ESA PDL Offline",
		Logs: []LogEntry{
			{"vmkernel.log", 0, "{TIME} In(182) vmkernel: cpu{CPU}:4100)WARNING: LSOM: LSOMCompleteIO:3412: I/O error on device {VSAN_DISK}, status: Offline"},
			{"vobd.log", 100, "{TIME} In(14) vobd[2098148]: [vsanCorrelator] 5234567890us: [esx.problem.vob.vsan.lsom.diskerror] vSAN disk error detected."},
			{"vobd.log", 200, "{TIME} In(14) vobd[2098148]: [vsanCorrelator] 5234567990us: [esx.problem.vob.vsan.pdl.offline] vSAN device {VSAN_DISK} has gone offline."},
		},
	},
	// 16~20: DPU (SmartNIC) & PCIe Errors (ESXi 8.0 특화)
	{
		ID: "ESX8_DPU_NIC_01", Desc: "DPU SmartNIC Network Uplink Down",
		Logs: []LogEntry{
			{"vmkernel.log", 0, "{TIME} In(182) vmkernel: cpu{CPU}:1024)nmlx5_core: 0000:2a:00.0: Health: PCI error detected. Device internal error state is set."},
			{"vmkernel.log", 10, "{TIME} In(182) vmkernel: cpu{CPU}:1024)nmlx5_core: 0000:2a:00.1: Health: thread is stopped 0x43232c04e448."},
			{"vobd.log", 100, "{TIME} In(14) vobd[2098148]: [netCorrelator] 68932599us: [vob.net.dvport.uplink.transition.down] Uplink: {NIC} is down. Affected dvPort: {DVPORT}. 2 uplinks up. Failed criteria: 128."},
		},
	},
	// 21~25: CPU Hardware (MCE) & PSOD
	{
		ID: "ESX8_CPU_MCE_01", Desc: "Machine Check Exception and PSOD",
		Logs: []LogEntry{
			{"vmkernel.log", 0, "{TIME} In(182) vmkernel: cpu{PCPU}:2097)MCA: 4: UE Excp"},
			{"vmkernel.log", 10, "{TIME} In(182) vmkernel: cpu{PCPU}:2097)System has encountered a Hardware Error"},
			{"vmkernel.log", 50, "{TIME} In(182) vmkernel: cpu{PCPU}:2097)@BlueScreen: Machine Check Exception on PCPU {PCPU}"},
			{"vmkernel.log", 60, "{TIME} In(182) vmkernel: cpu0:2049)Starting coredump to disk..."},
		},
	},
	// 26~30: CPU Lockup / TLB Invalidate
	{
		ID: "ESX8_CPU_LOCKUP_01", Desc: "PCPU Lockup and Spin Deadlock",
		Logs: []LogEntry{
			{"vmkernel.log", 0, "{TIME} In(182) vmkernel: cpu{PCPU}:2097)PCPU {PCPU} locked up."},
			{"vmkernel.log", 100, "{TIME} In(182) vmkernel: cpu{PCPU}:2097)Failed to ack TLB invalidate."},
			{"vmkernel.log", 200, "{TIME} In(182) vmkernel: cpu{PCPU}:2097)Spin count exceeded - possible deadlock"},
		},
	},
	// 31~35: IPMI SEL Hardware Faults
	{
		ID: "ESX8_IPMI_POWER_01", Desc: "Power Supply Fault",
		Logs: []LogEntry{
			{"ipmi_sel", 0, "{YMD} {HMS} | Power Supply 2 | Power Supply Fault | Asserted"},
			{"ipmi_sel", 10, "{YMD} {HMS} | Power Unit | Power Input Lost | Asserted"},
			{"vobd.log", 2000, "{TIME} In(14) vobd[2098148]: [auditCorrelator] 7234567890us: [esx.audit.host.poweroff.reason.unavailable] Host powered off unexpectedly"},
		},
	},
	{
		ID: "ESX8_IPMI_MEM_01", Desc: "Uncorrectable ECC Memory Error",
		Logs: []LogEntry{
			{"ipmi_sel", 0, "{YMD} {HMS} | Memory | Uncorrectable ECC | Asserted"},
			{"vmkernel.log", 100, "{TIME} In(182) vmkernel: cpu{CPU}:2097)Memory Controller Read Error on Channel {CH}"},
			{"vmkernel.log", 10000, "{TIME} In(182) vmkernel: cpu0:2049)ALERT: hostd detected to be non-responsive"},
		},
	},
	// 기타 추가 장애 패턴들 생략 없이 30+개 생성 예정 (여기선 대표 10개 템플릿만 등록하되, 변수 치환으로 300+개 파생)
}

func main() {
	countPtr := flag.Int("count", 1, "Number of random error scenarios to generate")
	logDirPtr := flag.String("logdir", "/var/run/log", "Directory where logs will be appended")
	flag.Parse()

	rand.Seed(time.Now().UnixNano())

	// 확장 생성된 시나리오 템플릿(35개+) 합치기
	templates = append(templates, getExtraTemplates()...)

	fmt.Printf("Starting ESXi 8.x Mock Logger (Generating %d scenarios)...\n", *countPtr)

	// Ensure directories exist
	os.MkdirAll(*logDirPtr, 0755)
	os.MkdirAll(filepath.Join(*logDirPtr, "ipmi"), 0755)

	for i := 0; i < *countPtr; i++ {
		tmpl := templates[rand.Intn(len(templates))]
		
		// 템플릿 변수 셔플 (다양성 극대화)
		cpu := fmt.Sprintf("%d", rand.Intn(64))
		pcpu := fmt.Sprintf("%d", rand.Intn(128))
		dev := fmt.Sprintf("naa.600000000000000000000000000%04d", rand.Intn(9999))
		nvmeDev := fmt.Sprintf("eui.100000000000000000000000000%04d", rand.Intn(9999))
		nvmeCtrl := fmt.Sprintf("nvmec%d", rand.Intn(8))
		nvmePath := fmt.Sprintf("vmhba%d:C0:T0:L%d", rand.Intn(4)+64, rand.Intn(10))
		vsanDisk := fmt.Sprintf("52b41234-5678-90ab-cdef-%012x", rand.Int63())
		nic := fmt.Sprintf("vmnic%d", rand.Intn(8))
		dvport := fmt.Sprintf("10%d/00 50 56 61 %02x %02x", rand.Intn(999), rand.Intn(255), rand.Intn(255))
		ch := fmt.Sprintf("%d", rand.Intn(4))

		// --- 기존 로그 파일 초기화 ---
		// 최신 내용만 유지하기 위해 매번 시나리오 생성 전 기존 로그를 삭제합니다.
		os.Remove(filepath.Join(*logDirPtr, "vmkernel.log"))
		os.Remove(filepath.Join(*logDirPtr, "vobd.log"))
		os.Remove(filepath.Join(*logDirPtr, "hostd.log"))
		os.Remove(filepath.Join(*logDirPtr, "vmkwarning.log"))
		os.Remove(filepath.Join(*logDirPtr, "vmksummary.log"))
		os.Remove(filepath.Join(*logDirPtr, "sensord.log"))
		os.Remove(filepath.Join(*logDirPtr, "ipmi", "sel"))

		fmt.Printf("Executing Scenario: %s (%s)\n", tmpl.ID, tmpl.Desc)

		for _, logTmpl := range tmpl.Logs {
			// delay_ms 대기 (실시간 시계열 모사)
			if logTmpl.DelayMs > 0 {
				time.Sleep(time.Duration(logTmpl.DelayMs) * time.Millisecond)
			}

			now := time.Now().UTC()
			timeStr := now.Format("2006-01-02T15:04:05.000Z")
			ymd := now.Format("2006-01-02")
			hms := now.Format("15:04:05")

			msg := logTmpl.Format
			msg = strings.ReplaceAll(msg, "{TIME}", timeStr)
			msg = strings.ReplaceAll(msg, "{YMD}", ymd)
			msg = strings.ReplaceAll(msg, "{HMS}", hms)
			msg = strings.ReplaceAll(msg, "{CPU}", cpu)
			msg = strings.ReplaceAll(msg, "{PCPU}", pcpu)
			msg = strings.ReplaceAll(msg, "{DEV}", dev)
			msg = strings.ReplaceAll(msg, "{NVME_DEV}", nvmeDev)
			msg = strings.ReplaceAll(msg, "{NVME_CTRL}", nvmeCtrl)
			msg = strings.ReplaceAll(msg, "{NVME_PATH}", nvmePath)
			msg = strings.ReplaceAll(msg, "{VSAN_DISK}", vsanDisk)
			msg = strings.ReplaceAll(msg, "{NIC}", nic)
			msg = strings.ReplaceAll(msg, "{DVPORT}", dvport)
			msg = strings.ReplaceAll(msg, "{CH}", ch)

			// 대상 파일 결정
			targetPath := filepath.Join(*logDirPtr, logTmpl.File)
			if logTmpl.File == "ipmi_sel" {
				targetPath = filepath.Join(*logDirPtr, "ipmi", "sel") // /var/run/log/ipmi/sel 로 매핑
			}

			// 로그 추가
			f, err := os.OpenFile(targetPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
			if err != nil {
				log.Printf("Failed to open file %s: %v", targetPath, err)
				continue
			}
			f.WriteString(msg + "\n")
			f.Close()
		}
	}
	fmt.Println("Mock log generation complete.")
}
