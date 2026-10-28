# pcisim — PCI 패스스루 흉내 vcsim (검증 전용)

랩 vCenter 가 꺼져 있어서 `pci_passthrough`·`vm-param-check` 를 검증하려고 만든 서버다. govmomi v0.55.1 simulator(VPX 모델)에 vcsim 에 없는 PCI 패스스루 기능을 더했다. **배포하지 않는다**(V2 밖에 둔 이유).

## 실행 (.58 Rocky)

```bash
export PATH=/opt/go1.26.5/bin:/root/go/bin:$PATH
unset VCENTER_URL VCENTER_USER VCENTER_PASS          # ~/.bashrc 의 실 vCenter 값 무시
cd /root/pciwork/VM-PCI-패스스루-통합/검증/pcisim
bash pcisim.sh start                                  # 처음이거나 main.go 가 바뀌었으면 빌드부터
bash pcisim.sh start -rebootDevs=0000:3b:00.1         # 재부팅 필요 장치를 흉내 낼 때
eval "$(bash pcisim.sh env)"                          # GOVC_URL/GOVC_USERNAME/GOVC_PASSWORD/GOVC_INSECURE, VC_PASSWORD
bash pcisim.sh status                                 # 호스트별 장치 enabled/active
bash pcisim.sh reboot bm01.saccae.com                 # 호스트 재부팅 흉내 (짧은 이름 bm01 도 됨)
bash pcisim.sh stop
bash selftest.sh                                      # 자체 점검 (새로 띄워 점검 후 끔)
```

| 항목 | 기본값 | 바꾸는 법 |
|---|---|---|
| 접속 URL | `https://127.0.0.1:8989/sdk` (자체 서명 인증서) | `PCISIM_LISTEN` |
| 계정 / 비밀번호 | `lscsystems@vsphere.local` / `pcisim-pass` | `PCISIM_USER` / `PCISIM_PASS` |
| 호스트 | `bm01.saccae.com`, `bm02.saccae.com` (클러스터 `DC0_C0`) | `PCISIM_HOSTS` (쉼표 구분) |
| 재부팅 필요 장치 | 없음 | `start -rebootDevs=0000:3b:00.1,bm02.saccae.com/0000:3b:00.0` |
| 패스스루 backing Edit 거부 | 받음 | `start -rejectPciEdit` |

V2 도구는 `-vcTargetIP=127.0.0.1:8989 -id=lscsystems@vsphere.local` 과 `VC_PASSWORD=pcisim-pass` 로 붙는다. 로그는 `pcisim.log`.

> 비밀번호를 `pcisim` 으로 하면 안 된다: govc 는 `GOVC_PASSWORD` 값과 같은 이름의 파일이 있으면 그 파일 내용을 비밀번호로 읽는다(실행파일 `pcisim` 이 읽혀 로그인 실패).

## 인벤토리

- `/DC0/host/DC0_C0/<호스트>` — VM 기본 생성은 없다(`Machine=0`). 데이터스토어 `LocalDS_0`(모든 호스트), 네트워크 `VM Network`·`DC0_DVPG0`. `vm_create` 가 그대로 동작한다(worklist 에 호스트 이름, hostgroup 에 `DC0_DVPG0`).
- 호스트마다 같은 PCI 장치 4개 (`hardware.pciDevice`, PCI 주소 순):

| Id | VendorId (10진) | DeviceId | 장치 | 비고 |
|---|---|---|---|---|
| 0000:18:00.0 | -32634 (0x8086) | 0x1572 | Intel X710 | **관리망 vmnic0** (`config.network.pnic` 의 vmnic0 `Pci`) — 패스스루 금지 대상 |
| 0000:3b:00.0 | 5555 (0x15b3) | 0x101b | Mellanox MT28908 [ConnectX-6] | |
| 0000:3b:00.1 | 5555 (0x15b3) | 0x101b | Mellanox MT28908 [ConnectX-6] | |
| 0000:af:00.0 | 4318 (0x10de) | 0x2503 | NVIDIA GA106 [GeForce RTX 3060] | 64bit MMIO 필요 |

  govmomi 의 `VendorId`/`DeviceId` 는 int16 이다. 0x8000 이상 값은 실 vCenter 처럼 2의 보수 음수로 들어간다(0x8086 → -32634). 비교할 때는 `uint16(v)` 로 바꿔서 본다.

## 흉내 내는 동작

| 대상 | 경로 / 메서드 | 동작 |
|---|---|---|
| 패스스루 시스템 | `configManager.pciPassthruSystem` → `HostPciPassthruSystem:pciPassthruSystem-<host moref>` | `pciPassthruInfo` (Id, DependentDevice=Id, PassthruCapable=true, 처음엔 Enabled/Active=false). 호스트 `config.pciPassthruInfo` 도 같은 값 |
| 활성화 | `UpdatePassthruConfig` | Enabled 를 바꾸고 Active 도 바로 같게 한다. 단 `-rebootDevs` 장치는 Active 가 그대로(재부팅 때까지). 없는 Id 는 InvalidArgument. 관리망 NIC 를 켜면 로그에 `MGMT_NIC_PASSTHRU` 경고 |
| 재부팅 | `GET /pcisim/reboot?host=<이름>` (`pcisim.sh reboot`) | 그 호스트의 Active = Enabled. 로그 `[REBOOT]` |
| 상태 | `GET /pcisim/state` (`pcisim.sh status`) | 호스트별 moref, ESXi system uuid, 장치 상태 |
| 장치 목록 | `EnvironmentBrowser.QueryConfigTarget` | vcsim 기본 결과에 `PciPassthrough[]`(Active 장치만, `SystemId` = 호스트별 ESXi system uuid) 를 더한다. host 를 주면 그 호스트만, 안 주면 클러스터 호스트 전부(govc `device.pci.ls` 가 이 경우) |
| 전원 ON | `PowerOnVM_Task` | 아래 검증을 통과해야 켠다 |
| 전원 정책 | `configManager.powerSystem` → `HostPowerSystem:powerSystem-<host moref>` | vm_setup 종단 실행의 `power_setting` 단계가 막히지 않게 하는 최소 흉내(vcsim 은 참조만 있고 객체가 없음). 정책 4개(`static`=고성능 key 1, `dynamic`=Balanced 2, `low` 3, `custom` 4), 처음엔 Balanced. `ConfigurePowerPolicy` 만 받아 `config.powerSystemInfo` 를 바꾼다. 로그 `[POWER]` |

ESXi system uuid 는 호스트 이름으로 정해지는 값(재시작해도 같음)이고 `hardware.systemInfo.uuid` 와 **다르다**. backing `SystemId` 에 hardware uuid 를 넣던 예전 버그를 재현하려는 것이다. 값은 `pcisim.sh status` 나 시작 로그 `[INIT]` 에서 본다.

### PowerOnVM_Task 검증

VM 에 `VirtualPCIPassthrough`(backing `VirtualPCIPassthroughDeviceBackingInfo`) 가 있으면 아래를 모두 본다. 하나라도 걸리면 Task 가 `GenericVmConfigFault` 로 실패하고, 메시지(LocalizedMessage)는 `Module 'DevicePowerOn' power on failed. ` 뒤에 사유를 `; ` 로 이어 붙인다. 사유는 `FaultMessage[]` 에도 하나씩 들어간다.

| # | 조건 | 사유 메시지 |
|---|---|---|
| a | backing.SystemId ≠ 그 호스트 ESXi system uuid | `pciPassthru0(<Id>): The systemId does not match the current system (backing systemId=..., host <호스트> ESXi system uuid=...)` |
| b | 장치가 Active 아님 | `...: Failed to register the device <Id> due to unavailable hardware or software support (passthruEnabled=true but passthruActive=false — host reboot required)` (꺼져 있으면 `(passthrough is not enabled)`) |
| c | VendorId / DeviceId(16진 문자열, 예 `101b`) 가 호스트 장치와 다름 | `...: The deviceId and vendorId do not match the device currently at <Id> (...)` |
| d | 메모리 예약 < 메모리 크기, `memoryReservationLockedToMax` 아님 | `Invalid memory setting: memory reservation (sched.mem.min) should be equal to memsize(<MB>). (...)` |
| e | 같은 호스트의 같은 장치를 다른 VM 이 쓰고 있음(켜짐 또는 켜는 중) | `...: Device <Id> is already in use by VM '<VM>'` |
| f | NVIDIA 장치인데 extraConfig `pciPassthru.use64bitMMIO=TRUE` 없음 | `...: PCI passthrough device <Id> has large BARs and needs 64-bit MMIO (set pciPassthru.use64bitMMIO=TRUE)` |
| g | NVIDIA + 64bit MMIO 켬, 펌웨어가 EFI 아님 | `64-bit MMIO for PCI passthrough requires EFI firmware (firmware="bios")` |
| h | NVIDIA + 64bit MMIO 켬, `pciPassthru.64bitMMIOSizeGB`×1024 < GPU 수 × 16432MB (없으면 0) | `64-bit MMIO window too small: pciPassthru.64bitMMIOSizeGB=16, the BARs of 1 GPU(s) need 16432 MB` |

g·h 의 근거: 64bit MMIO 는 EFI 에서만 동작하고, 창은 GPU BAR 합보다 커야 한다. GPU 1개 BAR 합은 RTX 3060(12GB) 에 Resizable BAR 가 켜진 경우로 잡았다(BAR0 16MB + BAR1 16GB + BAR3 32MB = 16432MB). 그래서 16GB 창은 거부, 32GB 이상은 통과한다. 실 ESXi 의 정확한 거부 조건·메시지는 실장비 확인 전까지 흉내다.

판정은 모두 로그에 남는다: `[POWERON] 허용 vm=... host=... 장치=[...]`, `[POWERON] 거부 vm=... 사유=...`, 패스스루가 없는 VM 은 `[POWERON] 허용 ... (패스스루 장치 없음)`.

`ReconfigVM_Task` 로 `VirtualPCIPassthrough` 를 붙이거나 backing 을 고치는(Edit) 것은 vcsim 이 그대로 받는다. `start -rejectPciEdit` 로 띄우면 패스스루 장치의 Edit 을 `NotSupported` 로 거부한다(로그 `[RECONFIG] 거부`) — 실 vCenter 가 Edit 을 받지 않을 때 `pci_passthrough` 가 "제거 후 다시 연결"로 넘어가는지 보는 용도.

## 시나리오 예

- 정상: `UpdatePassthruConfig` → Active=true → `QueryConfigTarget(host)` 의 SystemId 로 backing → 메모리 100% 예약 → 전원 ON 성공.
- 재부팅 필요: `start -rebootDevs=0000:3b:00.1` → 켜도 Active=false, QueryConfigTarget 에 안 나옴 → `pcisim.sh reboot bm01` → 나옴.
- 실패 재현: SystemId 에 hardware uuid, 예약 없음, 이미 쓰는 장치, NVIDIA 64bit MMIO 없음, DeviceId 틀림.

`selftest.sh` 가 위 시나리오 전부를 `selftest/main.go` 로 확인한다(30개 항목 PASS/FAIL. NVIDIA 는 MMIO 없음 → BIOS·16GB 창 → EFI·64GB 순서).

## vcsim 제약과 우회

- vcsim 은 로그인한 호출이면 `Registry.Handler` 결과를 `session.Get(ref)` 로 덮어쓴다. 그래서 감싼 객체(전원 ON·QueryConfigTarget)를 세션 레지스트리에 잠깐 넣고, 감싼 메서드 첫 줄에서 뺀다. 감싼 객체는 `Get()` 으로 원래 객체를 돌려줘 PropertyCollector 는 원래 객체를 읽는다.
- vcsim Task 는 실패 메시지를 `*types.XXX` 로만 채운다. 실패 Task 는 끝난 뒤 메시지를 바꾸고 나서 참조를 돌려준다(클라이언트에는 바뀐 메시지만 보임).
- `PowerOnMultiVM_Task`(Datacenter) 와 `ResetVM_Task` 는 검증을 거치지 않는다. VM 별 `PowerOnVM_Task` 를 쓸 것.
- 재부팅은 패스스루 Active 만 바꾼다. 호스트 위 VM 전원·유지보수 모드는 흉내 내지 않는다.
- 호스트는 모두 클러스터 `DC0_C0` 안에 있다(단독 호스트 없음). vcsim 은 VM 과 클러스터가 같은 EnvironmentBrowser 를 쓴다.
