package main

import (
	"fmt"
	"math/rand"
)

func getExtraTemplates() []ScenarioTemplate {
	var scenarios []ScenarioTemplate
	idCounter := 1

	add := func(desc string, logs []LogEntry) {
		scenarios = append(scenarios, ScenarioTemplate{
			ID:   fmt.Sprintf("SCENARIO_%d", idCounter),
			Desc: desc,
			Logs: logs,
		})
		idCounter++
	}

	// 1. Storage APD / PDL Variations (20 cases)
	for i := 0; i < 10; i++ {
		dev := fmt.Sprintf("naa.600000000000000000000000000%04d", rand.Intn(9999))
		add(fmt.Sprintf("스토리지 APD 타임아웃 발생 (디바이스: %s)", dev), []LogEntry{
			{"vmkernel.log", 0, fmt.Sprintf("{TIME} In(182) vmkernel: cpu{CPU}:2049)WARNING: NMP: nmp_DeviceStartLoop:721: NMP Device \"%s\" is blocked. Not starting I/O from device.", dev)},
			{"vobd.log", 100, fmt.Sprintf("{TIME} In(14) vobd[2098148]: [APDCorrelator] 1234567890us: [esx.problem.storage.apd.start] Device or filesystem with identifier [%s] has entered the All Paths Down state.", dev)},
			{"vmkernel.log", 200, fmt.Sprintf("{TIME} In(182) vmkernel: cpu0:2049)StorageApdHandler: 1204: APD Handle Created with lock [%s]", dev)},
			{"vobd.log", 140000, fmt.Sprintf("{TIME} In(14) vobd[2098148]: [APDCorrelator] 1234707890us: [vob.storage.apd.timeout] Device or filesystem with identifier [%s] has entered the All Paths Down Timeout state after being in the All Paths Down state for 140 seconds.", dev)},
		})

		devPdl := fmt.Sprintf("naa.600000000000000000000000000%04d", rand.Intn(9999)+10000)
		add(fmt.Sprintf("스토리지 영구 손실 PDL (디바이스: %s)", devPdl), []LogEntry{
			{"vmkernel.log", 0, fmt.Sprintf("{TIME} In(182) vmkernel: cpu{CPU}:2050)WARNING: NMP: nmp_IssueCommandToDevice:2954: I/O could not be issued to device \"%s\" due to Not found", devPdl)},
			{"vobd.log", 100, fmt.Sprintf("{TIME} In(14) vobd[2098148]: [scsiCorrelator] 2234567890us: [esx.problem.scsi.device.state.permanentloss] The path to device %s has been permanently lost.", devPdl)},
		})
	}

	// 2. Storage SCSI Sense / Medium Error Variations (10 cases)
	for i := 0; i < 10; i++ {
		dev := fmt.Sprintf("naa.500000000000000000000000000%04d", rand.Intn(9999))
		lba := rand.Intn(9000000) + 100000
		add(fmt.Sprintf("디스크 물리 베드섹터 Medium Error (디바이스: %s, LBA: %d)", dev, lba), []LogEntry{
			{"vmkernel.log", 0, fmt.Sprintf("{TIME} In(182) vmkernel: cpu{CPU}:2097669)ScsiDeviceIO: 4181: Cmd(0x459a40b3c080) 0x28, CmdSN 0x1234 from world 1000 to dev \"%s\" failed H:0x0 D:0x2 P:0x0 Valid sense data: 0x3 0x11 0x0.", dev)},
			{"vmkernel.log", 100, fmt.Sprintf("{TIME} In(182) vmkernel: cpu{CPU}:2097669)ScsiDeviceIO: 4182: Medium Error, LBA: %d", lba)},
		})
	}

	// 3. Network NIC Flapping (10 cases)
	for i := 0; i < 10; i++ {
		nic := fmt.Sprintf("vmnic%d", rand.Intn(8))
		add(fmt.Sprintf("네트워크 %s 링크 단절 및 리던던시 상실", nic), []LogEntry{
			{"vmkernel.log", 0, fmt.Sprintf("{TIME} In(182) vmkernel: cpu{CPU}:2097)Uplink: 339: %s: link down", nic)},
			{"vobd.log", 100, fmt.Sprintf("{TIME} In(14) vobd[2098148]: [netCorrelator] 3234567890us: [esx.problem.net.vmnic.linkstate.down] Physical NIC %s link state is down.", nic)},
			{"vobd.log", 200, "{TIME} In(14) vobd[2098148]: [netCorrelator] 3234567990us: [esx.problem.net.redundancy.lost] Network connectivity issues on virtual switch \"vSwitch0\". Redundancy lost."},
			{"vobd.log", 300, fmt.Sprintf("{TIME} In(14) vobd[2098148]: [netCorrelator] 3234568090us: [vob.net.pg.uplink.transition.down] Uplink %s is down. Failed criteria: 128", nic)},
		})
	}

	// 4. CPU MCE & PSOD Variations (15 cases)
	for i := 0; i < 15; i++ {
		pcpu := rand.Intn(64)
		add(fmt.Sprintf("CPU 하드웨어 결함 (MCE) 및 PSOD 발생 (PCPU: %d)", pcpu), []LogEntry{
			{"vmkernel.log", 0, fmt.Sprintf("{TIME} In(182) vmkernel: cpu%d:2097)MCA: 4: UE Excp", pcpu)},
			{"vmkernel.log", 10, fmt.Sprintf("{TIME} In(182) vmkernel: cpu%d:2097)System has encountered a Hardware Error", pcpu)},
			{"vmkernel.log", 50, fmt.Sprintf("{TIME} In(182) vmkernel: cpu%d:2097)@BlueScreen: Machine Check Exception on PCPU %d", pcpu, pcpu)},
			{"vmkernel.log", 60, "{TIME} In(182) vmkernel: cpu0:2049)Starting coredump to disk..."},
		})
	}

	// 5. CPU Lockup / Spin Count (10 cases)
	for i := 0; i < 10; i++ {
		pcpu := rand.Intn(64)
		add(fmt.Sprintf("물리 CPU 락업 및 스핀락 데드락 (PCPU: %d)", pcpu), []LogEntry{
			{"vmkernel.log", 0, fmt.Sprintf("{TIME} In(182) vmkernel: cpu%d:2097)PCPU %d locked up.", pcpu, pcpu)},
			{"vmkernel.log", 100, fmt.Sprintf("{TIME} In(182) vmkernel: cpu%d:2097)Failed to ack TLB invalidate.", pcpu)},
			{"vmkernel.log", 200, fmt.Sprintf("{TIME} In(182) vmkernel: cpu%d:2097)Spin count exceeded - possible deadlock", pcpu)},
		})
	}

	// 6. IPMI Hardware Power/Thermal/Fan (15 cases)
	for i := 0; i < 5; i++ {
		add("하드웨어 전원 단절 및 호스트 비정상 셧다운", []LogEntry{
			{"ipmi_sel", 0, "{YMD} {HMS} | Power Supply 2 | Power Supply Fault | Asserted"},
			{"ipmi_sel", 10, "{YMD} {HMS} | Power Unit | Power Input Lost | Asserted"},
			{"vobd.log", 5000, "{TIME} In(14) vobd[2098148]: [auditCorrelator] 4234567890us: [esx.audit.host.poweroff.reason.unavailable] Host powered off unexpectedly"},
		})
	}
	for i := 0; i < 5; i++ {
		add("하드웨어 과열 (Thermal Critical) 경보", []LogEntry{
			{"ipmi_sel", 0, "{YMD} {HMS} | Temperature | Upper Critical going high | Asserted"},
			{"vmkernel.log", 100, "{TIME} In(182) vmkernel: cpu0:2049)ALERT: Hardware temperature threshold exceeded"},
		})
	}
	for i := 0; i < 5; i++ {
		add("팬 고장 및 리던던시 상실", []LogEntry{
			{"ipmi_sel", 0, fmt.Sprintf("{YMD} {HMS} | Fan Block %d | Transition to Critical | Asserted", rand.Intn(4)+1)},
			{"ipmi_sel", 10, "{YMD} {HMS} | Fan | Redundancy Lost | Asserted"},
		})
	}

	// 7. Memory Uncorrectable ECC & Hostd Hang (10 cases)
	for i := 0; i < 10; i++ {
		ch := rand.Intn(4)
		add(fmt.Sprintf("Uncorrectable ECC 메모리 에러 및 서비스 Hang (채널: %d)", ch), []LogEntry{
			{"ipmi_sel", 0, "{YMD} {HMS} | Memory | Uncorrectable ECC | Asserted"},
			{"vmkernel.log", 100, fmt.Sprintf("{TIME} In(182) vmkernel: cpu{CPU}:2097)Memory Controller Read Error on Channel %d", ch)},
			{"vmkernel.log", 200, "{TIME} In(182) vmkernel: cpu{CPU}:2097)ScsiDeviceIO: performance has deteriorated"},
			{"vmkernel.log", 10000, "{TIME} In(182) vmkernel: cpu0:2049)ALERT: hostd detected to be non-responsive"},
		})
	}

	// 8. vSAN PDL / Disk Error (5 cases)
	for i := 0; i < 5; i++ {
		dev := fmt.Sprintf("naa.600000000000000000000000000%04d", rand.Intn(9999))
		add(fmt.Sprintf("vSAN 디스크 오프라인 및 에러 (디바이스: %s)", dev), []LogEntry{
			{"vobd.log", 0, "{TIME} In(14) vobd[2098148]: [vsanCorrelator] 5234567890us: [esx.problem.vob.vsan.lsom.diskerror] vSAN disk error detected."},
			{"vobd.log", 100, fmt.Sprintf("{TIME} In(14) vobd[2098148]: [vsanCorrelator] 5234567990us: [esx.problem.vob.vsan.pdl.offline] vSAN device %s has gone offline.", dev)},
		})
	}

	// 9. Silent Reboot / IPMI Non-recoverable (5 cases)
	for i := 0; i < 5; i++ {
		add("PSOD 없는 하드웨어 즉시 재부팅 (Silent Reboot)", []LogEntry{
			{"ipmi_sel", 0, "{YMD} {HMS} | Processor | Processor Transition to Non-recoverable | Asserted"},
		})
	}
	
	// 10. VMFS Heartbeat / Lock Corrupt (5 cases)
	for i := 0; i < 5; i++ {
		add("VMFS Lock Corrupt 및 데이터스토어 타임아웃", []LogEntry{
			{"vobd.log", 0, "{TIME} In(14) vobd[2098148]: [vmfsCorrelator] 6234567890us: [esx.problem.vmfs.heartbeat.timedout] VMFS heartbeat timed out."},
			{"vobd.log", 100, "{TIME} In(14) vobd[2098148]: [vmfsCorrelator] 6234567990us: [esx.problem.vmfs.lock.corruptondisk] VMFS lock corrupt on disk."},
		})
	}

	return scenarios
}
