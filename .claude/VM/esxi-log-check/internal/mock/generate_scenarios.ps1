$scenarios = @()
$idCounter = 1

function Add-Scenario {
    param([string]$desc, [array]$logs)
    
    $obj = @{
        id = "SCENARIO_$idCounter"
        desc = $desc
        logs = $logs
    }
    $script:scenarios += $obj
    $script:idCounter++
}

# 1. Storage APD / PDL (20 cases)
for ($i=0; $i -lt 10; $i++) {
    $dev = "naa.600000000000000000000000000$(Get-Random -Minimum 1000 -Maximum 9999)"
    $cpu = Get-Random -Minimum 0 -Maximum 16
    $m1 = "cpu{0}:2049)WARNING: NMP: nmp_DeviceStartLoop:721: NMP Device `"{1}`" is blocked. Not starting I/O from device." -f $cpu, $dev
    $m2 = "[APDCorrelator] 1234567890us: [esx.problem.storage.apd.start] Device or filesystem with identifier [{0}] has entered the All Paths Down state." -f $dev
    $m3 = "cpu0:2049)StorageApdHandler: 1204: APD Handle Created with lock [{0}]" -f $dev
    $m4 = "[APDCorrelator] 1234707890us: [esx.problem.storage.apd.timeout] Device or filesystem with identifier [{0}] has entered the All Paths Down Timeout state after being in the All Paths Down state for 140 seconds." -f $dev
    Add-Scenario -desc "스토리지 APD 타임아웃 발생 (디바이스: $dev)" -logs @(
        @{file="vmkernel.log"; delay_ms=0; msg=$m1},
        @{file="vobd.log"; delay_ms=100; msg=$m2},
        @{file="vmkernel.log"; delay_ms=200; msg=$m3},
        @{file="vobd.log"; delay_ms=140000; msg=$m4}
    )

    $devPdl = "naa.600000000000000000000000000$(Get-Random -Minimum 10000 -Maximum 19999)"
    $p1 = "cpu{0}:2050)WARNING: NMP: nmp_IssueCommandToDevice:2954: I/O could not be issued to device `"{1}`" due to Not found" -f $cpu, $devPdl
    $p2 = "[scsiCorrelator] 2234567890us: [esx.problem.scsi.device.state.permanentloss] The path to device {0} has been permanently lost." -f $devPdl
    Add-Scenario -desc "스토리지 영구 손실 PDL (디바이스: $devPdl)" -logs @(
        @{file="vmkernel.log"; delay_ms=0; msg=$p1},
        @{file="vobd.log"; delay_ms=100; msg=$p2}
    )
}

# 2. Storage SCSI Sense / Medium Error (10 cases)
for ($i=0; $i -lt 10; $i++) {
    $dev = "naa.500000000000000000000000000$(Get-Random -Minimum 1000 -Maximum 9999)"
    $lba = Get-Random -Minimum 100000 -Maximum 9999999
    $cpu = Get-Random -Minimum 0 -Maximum 32
    $e1 = "cpu{0}:2097669)ScsiDeviceIO: 4181: Cmd(0x459a40b3c080) 0x28, CmdSN 0x1234 from world 1000 to dev `"{1}`" failed H:0x0 D:0x2 P:0x0 Valid sense data: 0x3 0x11 0x0." -f $cpu, $dev
    $e2 = "cpu4:2097669)ScsiDeviceIO: 4182: Medium Error, LBA: {0}" -f $lba
    Add-Scenario -desc "디스크 물리 베드섹터 Medium Error (디바이스: $dev, LBA: $lba)" -logs @(
        @{file="vmkernel.log"; delay_ms=0; msg=$e1},
        @{file="vmkernel.log"; delay_ms=100; msg=$e2}
    )
}

# 3. Network NIC Flapping (10 cases)
for ($i=0; $i -lt 10; $i++) {
    $nic = "vmnic$(Get-Random -Minimum 0 -Maximum 8)"
    $n1 = "cpu1:2097)Uplink: 339: {0}: link down" -f $nic
    $n2 = "[netCorrelator] 3234567890us: [esx.problem.net.vmnic.linkstate.down] Physical NIC {0} link state is down." -f $nic
    $n3 = "[netCorrelator] 3234567990us: [esx.problem.net.redundancy.lost] Network connectivity issues on virtual switch `"vSwitch0`". Redundancy lost."
    $n4 = "[netCorrelator] 3234568090us: [vob.net.pg.uplink.transition.down] Uplink {0} is down. Failed criteria: 128" -f $nic
    Add-Scenario -desc "네트워크 $nic 링크 단절 및 리던던시 상실" -logs @(
        @{file="vmkernel.log"; delay_ms=0; msg=$n1},
        @{file="vobd.log"; delay_ms=100; msg=$n2},
        @{file="vobd.log"; delay_ms=200; msg=$n3},
        @{file="vobd.log"; delay_ms=300; msg=$n4}
    )
}

# 4. CPU MCE & PSOD (15 cases)
for ($i=0; $i -lt 15; $i++) {
    $pcpu = Get-Random -Minimum 0 -Maximum 64
    $c1 = "cpu{0}:2097)MCA: 4: UE Excp" -f $pcpu
    $c2 = "cpu{0}:2097)System has encountered a Hardware Error" -f $pcpu
    $c3 = "cpu{0}:2097)@BlueScreen: Machine Check Exception on PCPU {1}" -f $pcpu, $pcpu
    $c4 = "cpu0:2049)Starting coredump to disk..."
    Add-Scenario -desc "CPU 하드웨어 결함 (MCE) 및 PSOD 발생 (PCPU: $pcpu)" -logs @(
        @{file="vmkernel.log"; delay_ms=0; msg=$c1},
        @{file="vmkernel.log"; delay_ms=10; msg=$c2},
        @{file="vmkernel.log"; delay_ms=50; msg=$c3},
        @{file="vmkernel.log"; delay_ms=60; msg=$c4}
    )
}

# 5. CPU Lockup (10 cases)
for ($i=0; $i -lt 10; $i++) {
    $pcpu = Get-Random -Minimum 0 -Maximum 64
    $l1 = "cpu{0}:2097)PCPU {1} locked up." -f $pcpu, $pcpu
    $l2 = "cpu{0}:2097)Failed to ack TLB invalidate." -f $pcpu
    $l3 = "cpu{0}:2097)Spin count exceeded - possible deadlock" -f $pcpu
    Add-Scenario -desc "물리 CPU 락업 및 스핀락 데드락 (PCPU: $pcpu)" -logs @(
        @{file="vmkernel.log"; delay_ms=0; msg=$l1},
        @{file="vmkernel.log"; delay_ms=100; msg=$l2},
        @{file="vmkernel.log"; delay_ms=200; msg=$l3}
    )
}

# 6. IPMI Hardware (15 cases)
for ($i=0; $i -lt 5; $i++) {
    $h1 = "2026-08-17 12:00:00 | Power Supply 2 | Power Supply Fault | Asserted"
    $h2 = "2026-08-17 12:00:01 | Power Unit | Power Input Lost | Asserted"
    $h3 = "[auditCorrelator] 4234567890us: [esx.audit.host.poweroff.reason.unavailable] Host powered off unexpectedly"
    Add-Scenario -desc "하드웨어 전원 단절 및 호스트 비정상 셧다운" -logs @(
        @{file="ipmi_sel"; delay_ms=0; msg=$h1},
        @{file="ipmi_sel"; delay_ms=10; msg=$h2},
        @{file="vobd.log"; delay_ms=5000; msg=$h3}
    )
    $t1 = "2026-08-17 13:00:00 | Temperature | Upper Critical going high | Asserted"
    $t2 = "cpu0:2049)ALERT: Hardware temperature threshold exceeded"
    Add-Scenario -desc "하드웨어 과열 (Thermal Critical) 경보" -logs @(
        @{file="ipmi_sel"; delay_ms=0; msg=$t1},
        @{file="vmkernel.log"; delay_ms=100; msg=$t2}
    )
    $fan = Get-Random -Minimum 1 -Maximum 5
    $f1 = "2026-08-17 14:00:00 | Fan Block {0} | Transition to Critical | Asserted" -f $fan
    $f2 = "2026-08-17 14:00:01 | Fan | Redundancy Lost | Asserted"
    Add-Scenario -desc "팬 고장 및 리던던시 상실" -logs @(
        @{file="ipmi_sel"; delay_ms=0; msg=$f1},
        @{file="ipmi_sel"; delay_ms=10; msg=$f2}
    )
}

# 7. Memory Uncorrectable ECC (10 cases)
for ($i=0; $i -lt 10; $i++) {
    $ch = Get-Random -Minimum 0 -Maximum 4
    $cpu = Get-Random -Minimum 0 -Maximum 32
    $mem1 = "2026-08-17 12:10:00 | Memory | Uncorrectable ECC | Asserted"
    $mem2 = "cpu{0}:2097)Memory Controller Read Error on Channel {1}" -f $cpu, $ch
    $mem3 = "cpu{0}:2097)ScsiDeviceIO: performance has deteriorated" -f $cpu
    $mem4 = "cpu0:2049)ALERT: hostd detected to be non-responsive"
    Add-Scenario -desc "Uncorrectable ECC 메모리 에러 및 서비스 Hang (채널: $ch)" -logs @(
        @{file="ipmi_sel"; delay_ms=0; msg=$mem1},
        @{file="vmkernel.log"; delay_ms=100; msg=$mem2},
        @{file="vmkernel.log"; delay_ms=200; msg=$mem3},
        @{file="vmkernel.log"; delay_ms=10000; msg=$mem4}
    )
}

# 8. vSAN PDL (5 cases)
for ($i=0; $i -lt 5; $i++) {
    $dev = "naa.600000000000000000000000000$(Get-Random -Minimum 1000 -Maximum 9999)"
    $v1 = "[vsanCorrelator] 5234567890us: [esx.problem.vob.vsan.lsom.diskerror] vSAN disk error detected."
    $v2 = "[vsanCorrelator] 5234567990us: [esx.problem.vob.vsan.pdl.offline] vSAN device {0} has gone offline." -f $dev
    Add-Scenario -desc "vSAN 디스크 오프라인 및 에러 (디바이스: $dev)" -logs @(
        @{file="vobd.log"; delay_ms=0; msg=$v1},
        @{file="vobd.log"; delay_ms=100; msg=$v2}
    )
}

# 9. Silent Reboot (5 cases)
for ($i=0; $i -lt 5; $i++) {
    $s1 = "2026-08-17 12:20:00 | Processor | Processor Transition to Non-recoverable | Asserted"
    Add-Scenario -desc "PSOD 없는 하드웨어 즉시 재부팅 (Silent Reboot)" -logs @(
        @{file="ipmi_sel"; delay_ms=0; msg=$s1}
    )
}

# 10. VMFS Lock Corrupt (5 cases)
for ($i=0; $i -lt 5; $i++) {
    $fs1 = "[vmfsCorrelator] 6234567890us: [esx.problem.vmfs.heartbeat.timedout] VMFS heartbeat timed out."
    $fs2 = "[vmfsCorrelator] 6234567990us: [esx.problem.vmfs.lock.corruptondisk] VMFS lock corrupt on disk."
    Add-Scenario -desc "VMFS Lock Corrupt 및 데이터스토어 타임아웃" -logs @(
        @{file="vobd.log"; delay_ms=0; msg=$fs1},
        @{file="vobd.log"; delay_ms=100; msg=$fs2}
    )
}

$json = ConvertTo-Json $scenarios -Depth 4
Set-Content -Path "c:\Users\qazx2\myrepo\.git\myrepo\.claude\esxi-log-check\internal\mock\scenarios.json" -Value $json -Encoding UTF8
Write-Host "Generated $($scenarios.Count) scenarios."
