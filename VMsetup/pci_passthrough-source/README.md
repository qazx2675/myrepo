# pci_passthrough-source

BM(ESXi 호스트)마다 `<BM짧은이름>evNN` VM 에 **PCI 패스스루 장치를 1개씩 배분해 연결하고 전원을 켜는** 도구입니다.
예전에 둘로 나뉘어 있던 "패스스루 활성화(Go, `VM/passthrough/enable-go`)"와 "VM 연결(PowerShell, `attach_pci.ps1`)"을 하나로 합쳤습니다.

⚠️ **주의사항 (Disclaimer)**
VM 전원을 끄고 켜며 호스트 패스스루 설정을 바꾸는 스크립트입니다. 실행 후 무작위로 VM 몇 대를 골라 장치가 붙어 있고 게스트에서 장치가 보이는지 직접 확인하세요.

## 1. 빌드 방법

```bash
cd V2/VMsetup/pci_passthrough-source
bash setup.sh
# 빌드 완료: .../pci_passthrough-source/pci_passthrough
```

`setup.sh`가 V2 공유 의존성(`../../govendor/govmomi-0.55.1-standard`)을 `vendor`로 링크하므로 폐쇄망에서도 빌드됩니다.

## 2. 사용 방법

```bash
export VC_PASSWORD='비밀번호'
cd V2/VMsetup
./pci_passthrough-source/pci_passthrough -vcTargetIP=192.168.0.50 -worklistFile=lsh.txt -ev01Pci=mellanox -ev02Pci=mellanox
```

- 작업 대상 파일은 `vm_create` 와 같은 BM 목록입니다(한 줄에 BM 하나, 첫 칸만 사용, `#` 주석·빈 줄·UTF-8 BOM 무시).
- VM 이름은 `vm_create` 와 같은 규칙 `<BM 이름의 첫 '.' 앞>evNN` 입니다 (예: `bm01.saccae.com` → `bm01ev01`).
- 호스트는 FQDN 이든 hostname 만이든 찾습니다 (`power_setting` 과 같은 규칙: 정확한 이름 → 짧은 이름 비교, 짧은 이름이 같은 호스트가 2대 이상이면 에러).
- **다시 실행해도 안전합니다.** 이미 장치가 붙어 켜져 있는 VM 은 건너뛰고(전원을 껐다 켜지 않음), 남은 일만 이어서 합니다.

## 3. 옵션

| 플래그 | 기본값 | 필수 | 설명 |
|---|---|---|---|
| `-vcTargetIP` | (없음) | ✅ | vCenter 주소 |
| `-worklistFile` | `worklist.txt` | | BM 목록 파일 (현재 폴더 기준) |
| `-id` | `lscsystems@vsphere.local` | | vCenter 계정 (비밀번호는 환경변수 `VC_PASSWORD`) |
| `-ev01Pci` ~ `-ev99Pci` | (없음) | 1개 이상 | 그 ev VM 에 붙일 장치의 vendor. **값이 있는 ev 만 처리**합니다 |
| `-concurrency` | `20` | | 동시에 처리할 호스트 수 (한 호스트 안의 VM 은 차례로 처리) |
| `-mmioSizeGB` | `0` (자동) | | 큰 BAR 장치(NVIDIA 등)를 붙인 VM 의 `pciPassthru.64bitMMIOSizeGB` **최솟값**. 2의 거듭제곱(64, 128, 256 …)만 받습니다. 0 이면 자동 계산(§6) |

### vendor 값 (vm-param-check 와 같은 규칙)

대소문자 무시, 위에서부터 먼저 맞는 규칙을 씁니다.

| 값 | 맞는 장치 |
|---|---|
| `mellanox` | VendorId 0x15b3 (5555) |
| `nvidia` | VendorId 0x10de (4318) |
| 10진 숫자만 (예 `5555`) | VendorId 가 그 10진수 |
| `0x` 16진수 (예 `0x15b3`), 또는 a-f 가 들어간 4자리 16진수 (예 `15b3`) | VendorId 가 그 16진수 |
| 그 밖의 값 (예 `ConnectX-6`) | 장치 이름(DeviceName)에 그 문자열이 들어감 |

판정 함수 `MatchPCIVendor` 는 `V2/vm-param-check-usability-improvement/vm-param-check/checker/pci.go` 와 **똑같은 복사본**입니다. 한쪽을 고치면 다른 쪽도 똑같이 고쳐야 합니다(이 도구가 붙인 장치를 vm-param-check 가 같은 규칙으로 OK 판정).

## 4. 동작 순서 (BM 1대 기준, BM 끼리는 병렬)

1. **수집** — 호스트의 `hardware.pciDevice`, `config.network.pnic`·`vswitch`·`proxySwitch`(스위치에 업링크로 연결된 vmnic 의 PCI 주소), `configManager.pciPassthruSystem` → `pciPassthruInfo`, 호스트 위 VM 전부와 그 VM 에 붙은 패스스루 장치.
2. **배분** — 값이 있는 ev 를 오름차순으로:
   - VM `<짧은이름>evNN` 이 이 호스트에 없으면 → **실패** `VM 없음`.
   - VM 에 vendor 에 맞는 패스스루 장치가 이미 붙어 있으면 → 그 장치를 그대로 씀(새로 붙이지 않음).
   - 아니면 vendor 에 맞는 장치 중 **PCI 주소가 가장 앞선 남는 장치**를 줌. 남는 장치 = 다른 VM 이 쓰지 않음 + 이번 실행에서 다른 ev 에 주지 않음 + 호스트 업링크(vmnic) 아님 + 패스스루 가능(`passthruCapable`).
   - 남는 장치가 없으면 → **실패** `사용 가능한 <vendor> 장치 없음 (일치 n개: 다른 VM 사용 a, 호스트 업링크(vmnic) b, 패스스루 불가 c)`.
3. **활성화** — 새로 배분한 장치 중 패스스루가 꺼진 것만 `UpdatePassthruConfig` 로 켭니다(같은 벤더라도 남는 장치는 건드리지 않음). 다시 `pciPassthruInfo` 를 읽어, 켜졌지만 `passthruActive=false` 인 장치를 쓰는 VM 은 **재부팅 필요**로 표시하고 연결·전원 단계를 건너뜁니다(같은 BM 의 다른 VM 은 계속 진행).
4. **전원 ON 전 점검** — VM 은 아직 건드리지 않고, VM 의 `EnvironmentBrowser.QueryConfigTarget(host)` 결과로 정상 부팅에 필요한 것을 모두 확인해 고칠 것을 **ReconfigVM 스펙 하나**로 모읍니다(§6). 막히는 것(BIOS 펌웨어 + 큰 BAR 장치, QueryConfigTarget 에 없는 장치)이 있으면 → **실패**, VM 은 끄지도 켜지도 않습니다.
5. **연결·설정** — VM 이 켜져 있으면 끈 뒤(`PowerOffVM_Task`), 모은 변경(새 장치 연결, backing 교정, 64bit MMIO, 메모리 예약)을 `ReconfigVM_Task` **한 번**으로 적용합니다. 바꿀 것이 없으면 ReconfigVM 하지 않습니다.
6. **전원 ON** — `PowerOnVM_Task` 로 켜고 끝날 때까지 기다린 뒤 **켜 둔 채로** 둡니다. 실패하면 vCenter 메시지 전체와 진단을 보여 주고 **실패**로 셉니다(§8).

| VM 상태 | 동작 |
|---|---|
| 장치 붙어 있음 + 켜짐 | 스킵 (전원 그대로, 아무것도 바꾸지 않음). 다음 전원 ON 때 걸릴 설정이 보이면 `[주의]` 로 알려 줌 |
| 장치 붙어 있음 + 꺼짐 | 점검 → (고칠 것 있으면) 설정 변경 → 전원 ON |
| 장치 없음 + 꺼짐 | 점검 → 연결(+고칠 것) → 전원 ON |
| 장치 없음 + 켜짐 | 점검 → 전원 끔 → 연결(+고칠 것) → 전원 ON |

켜져 있는 VM 은 이미 부팅된 것이라 전원을 껐다 켜지 않습니다(재실행해도 안전). 예를 들어 예전 스크립트가 붙인 장치의 SystemId 가 틀린 채로 켜져 있는 VM 이면 `스킵: ... — [주의] 켜진 VM 이라 그대로 둠, 다음 전원 ON 전에 확인: 0000:3b:00.0 SystemId 교정 (... → ...)` 이 나옵니다. 그 VM 을 끈 뒤 다시 실행하면 교정하고 켭니다.

## 5. backing SystemId — 예전 실패 원인

예전에 활성화와 연결을 한 스크립트로 묶었을 때 VM 전원이 켜지지 않았던 원인은 **backing `SystemId` 에 `host.hardware.systemInfo.uuid`(하드웨어/BIOS UUID)를 넣은 것**입니다(2026-10-28 사용자 확인). `SystemId` 는 ESXi 의 system uuid(`esxcli system uuid get`)여야 하고, 이 값은 `EnvironmentBrowser.QueryConfigTarget(host).PciPassthrough[].SystemId` 가 돌려줍니다. PowerCLI `Add-PassthroughDevice` 도 같은 곳에서 값을 받기 때문에 PowerShell 로 붙인 VM 은 켜졌습니다.

실장비에서 보이는 메시지는 `The systemId does not match the current system or the deviceId and vendorId no longer match the device currently at ...` 입니다. vCenter 에서 PCI 를 추가할 때 들어간 UUID 가 ESXi UUID 와 달랐고, ESXi UUID 로 다시 등록하니 정상 부팅됐습니다.

이 도구는 backing 을 전부 QueryConfigTarget 결과로 채웁니다: `Id`, `SystemId`, `VendorId`, `DeviceId`(소문자 16진 문자열, 예 `101b`), `DeviceName`. 하드웨어 UUID 는 어디에도 쓰지 않습니다. 결과 줄에 쓴 `SystemId` 를 함께 찍습니다.

**SSH·`esxcli` 는 필요 없습니다.** QueryConfigTarget 은 vCenter API 라서 vCenter 계정만으로 ESXi system uuid 를 얻습니다(호스트마다 SSH 를 열거나 root 비밀번호를 쓸 일이 없음). 호스트 쪽 `esxcli system uuid get` 값과 같은지는 실장비에서 한 번 맞춰 보면 됩니다(보류 목록).

### 이미 붙어 있는 장치 교정

예전 스크립트(하드웨어 UUID 사용)로 붙였거나, VM 을 다른 호스트로 옮겨서 backing 값이 지금 호스트와 맞지 않는 VM 도 고칩니다. 전원 ON 전에 **VM 에 붙은 패스스루 장치 전부**(이번에 배분한 장치만이 아님)를 QueryConfigTarget 의 같은 `Id` 항목과 비교합니다.

| 상황 | 동작 |
|---|---|
| `SystemId`/`VendorId`/`DeviceId` 가 다름 | 그 장치의 backing 만 고칩니다(`ReconfigVM` 의 Edit — 게스트에서 보이는 PCI 슬롯 유지). 결과 줄: `0000:3b:00.0 SystemId 교정 (<이전 값> → <ESXi uuid>)` |
| Edit 을 vCenter 가 받지 않음 | 그 장치를 빼고 같은 값으로 새로 붙입니다(Remove + Add 를 한 번에). 결과 줄 끝에 `Edit 대신 제거 후 다시 연결` (게스트에서 PCI 슬롯 번호가 바뀔 수 있음) |
| 장치 `Id` 가 QueryConfigTarget 에 없음 (패스스루 꺼짐·재부팅 대기·이 호스트에 없는 장치) | **실패**, 전원을 켜지 않습니다. 단, 이번에 배분한 장치가 "켜짐·활성 아님"이면 실패 대신 **재부팅 필요**(§4-3) |
| 켜져 있는 VM | 고치지 않고 스킵, `[주의]` 로 알려 줌(§4 표) |

## 6. 전원 ON 전 점검 (정상 부팅 보장)

`planBoot` 가 아래를 확인하고, 고칠 것을 `ReconfigVM` 한 번에 넣습니다. 전부 맞으면 ReconfigVM 을 하지 않습니다(재실행해도 설정이 바뀌지 않음).

| 항목 | 맞는 상태 | 다르면 |
|---|---|---|
| 패스스루 backing | QueryConfigTarget 값과 같음 (§5) | 교정 |
| 메모리 예약 | `memoryReservationLockedToMax=true` 또는 예약 ≥ 메모리 크기 | `memoryReservationLockedToMax=true` (예약 100%). 결과 줄: `메모리 100% 예약 (예약 0MB → 4096MB, lockedToMax)` |
| 큰 BAR 장치의 64bit MMIO | `pciPassthru.use64bitMMIO=TRUE`, `pciPassthru.64bitMMIOSizeGB` ≥ 필요 크기 | 둘 다 넣음. 결과 줄: `64bit MMIO 켬 (pciPassthru.64bitMMIOSizeGB=64, 이전 없음)` |
| 큰 BAR 장치의 펌웨어 | EFI | **실패** (자동으로 바꾸지 않음, 아래) |

패스스루 장치는 VM 메모리 전체를 고정(pin)해야 해서 메모리 예약이 100% 가 아니면 전원이 켜지지 않습니다. `vm_create` 로 만든 VM 은 이미 `lockedToMax=true` 라 바뀌지 않고, 다른 방법으로 만든 VM 만 고쳐집니다.

### 64bit MMIO 크기 규칙

- **대상**: `largeBARVendors` 표에 있는 벤더의 장치. 지금은 NVIDIA(`0x10de`) 하나입니다. AMD GPU(`0x1002`) 같은 다른 큰 BAR 장치는 `main.go` 의 이 표에 한 줄만 더하면 됩니다. Mellanox 는 BAR 가 작아서 대상이 아닙니다.
- **자동 계산(기본)**: `pciPassthru.64bitMMIOSizeGB = max(64, 장치 수 × 32 를 2의 거듭제곱으로 올린 값)`. 1~2개면 64, 3~4개면 128, 5~8개면 256.
- **근거**: VMware 가이드는 "VM 에 준 GPU 들의 BAR(메모리) 크기 합을 2의 거듭제곱으로 올린 값"을 쓰라고 합니다. vSphere API 로는 GPU 의 VRAM·BAR 크기를 알 수 없어서, 장치 1개에 32GB 를 잡았습니다 — VRAM 16GB 이하 GPU 는 Resizable BAR 를 켜도 BAR1 이 16GB(2의 거듭제곱)이고 BAR0/BAR3(수십 MB)를 더하면 16GB 를 넘으므로 32GB 창이 필요합니다. RTX 3060(12GB) 은 BAR1 16GB + 48MB → 32GB 면 충분합니다. 최소 64 는 VMware GPU 문서에서 1~2 GPU 에 흔히 쓰는 값이라 여유를 둔 것이고, 이 창은 게스트 물리 주소 공간만 차지할 뿐 호스트 메모리를 쓰지 않습니다.
- **바꾸기**: VRAM 이 16GB 보다 큰 GPU(A100 40/80GB 등)는 `-mmioSizeGB=128` 또는 `256` 처럼 직접 줍니다(VRAM 합을 2의 거듭제곱으로 올린 값 이상). 값은 **최솟값**으로 쓰여, VM 에 이미 더 큰 값이 있으면 그대로 둡니다(관리자가 손으로 키운 값을 줄이지 않음).

### 펌웨어 규칙 — BIOS 면 실패

64bit MMIO 는 EFI 펌웨어에서만 동작합니다. 큰 BAR 장치를 붙일 VM 이 BIOS 면 그 VM 은 **실패**로 남기고 아무것도 바꾸지 않습니다(켜져 있었으면 켜진 채). 펌웨어를 BIOS → EFI 로 바꾸면 이미 설치된 게스트 OS 의 부트로더가 맞지 않아 부팅되지 않기 때문에 자동으로 바꾸지 않습니다. 같은 BM 의 다른 VM 은 계속 진행합니다.

```
  -> [bm01ev01] 실패: 0000:af:00.0 전원 ON 전 점검: 펌웨어가 bios 라 큰 BAR 장치 0000:af:00.0(NVIDIA) 에 필요한 64bit MMIO 를 쓸 수 없습니다. ...
```

OS 를 아직 설치하지 않은 VM 이면 EFI 로 바꾸고(`vm_create -firmware=efi` 로 다시 만들거나 `govc vm.change -firmware efi`) 다시 실행하면 됩니다. `vm_create` 기본값은 EFI 입니다. 이때 그 GPU 는 호스트에서 패스스루가 이미 켜져 있을 수 있습니다(활성화가 VM 점검보다 먼저라서) — 다시 실행하면 그대로 이어서 씁니다.

## 7. 안전장치

- 호스트 업링크로 쓰이는 물리 NIC — 표준 vSwitch 나 DVS(proxySwitch) 에 업링크로 연결된 vmnic(관리망 포함) — 는 배분하지 않고 패스스루로 켜지 않습니다(켜면 호스트 네트워크가 끊김). 켜기 직전에도 한 번 더 막습니다. 드라이버가 잡아 vmnic 으로 보이기만 하고 어느 스위치에도 연결되지 않은 NIC(예: 쓰지 않는 Mellanox 포트)는 배분 대상입니다 — 2026-10-28 실 랩(ESXi 8.0.3)에서 스위치에 연결되지 않은 vmnic1(e1000e) 이 패스스루 가능 장치로 잡히는 것을 보고, vmnic 전체 제외에서 바꿨습니다.
- 활성화는 이번에 배분한 장치에만 합니다.
- 다른 VM(켜짐·꺼짐 무관)에 이미 붙은 장치는 배분하지 않습니다.
- 실패는 VM·BM 단위로 기록하고 나머지는 계속 진행합니다.
- 켜져 있는 VM 중 장치가 이미 붙은 것은 전원을 껐다 켜지 않습니다. 전원 ON 전 점검에서 막힌 VM 도 끄지 않습니다.

## 8. 출력과 종료코드

VM 마다 한 줄, 끝에 요약 표가 나옵니다. `설정 변경[...]` 안이 ReconfigVM 한 번에 넣은 내용입니다.

```
  -> [bm01ev01] 성공: 0000:af:00.0 설정 변경[연결 0000:af:00.0 (SystemId=a3c6...); 64bit MMIO 켬 (pciPassthru.64bitMMIOSizeGB=64, 이전 없음)] → 전원 ON
  -> [bm02ev01] 성공: 0000:3b:00.0 이미 연결됨 → 설정 변경[0000:3b:00.0 SystemId 교정 (0787...(하드웨어 UUID) → 0114...(ESXi uuid))] → 전원 ON
  -> [bm01ev02] 성공: 0000:3b:00.1 이미 연결됨 → 설정 변경[메모리 100% 예약 (예약 0MB → 4096MB, lockedToMax)] → 전원 ON
  -> [bm02ev02] 성공: 0000:3b:00.1 전원 끔 → 설정 변경[연결 0000:3b:00.1 (SystemId=0114...)] → 전원 ON
  -> [bm01ev02] 스킵: 0000:3b:00.1 이미 연결됨, 전원 켜짐
  -> [bm02ev02] 재부팅 필요: 0000:3b:00.1 패스스루 켜짐, 아직 활성 아님 — 호스트 재부팅 필요
  -> [bm01ev03] 실패: 사용 가능한 mellanox 장치 없음 (...)
 [PCI 패스스루 결과 요약]
 BM               VM        vendor    장치          결과    비고
 bm01.saccae.com  bm01ev01  mellanox  0000:3b:00.0  OK      ...
[경고] 호스트 재부팅 필요: bm02.saccae.com (장치 0000:3b:00.1) — 재부팅 후 같은 명령을 다시 실행하면 이어서 연결·전원 ON 합니다.
[INFO] PCI 패스스루 완료 — 성공 a / 스킵(이미 연결·켜짐) b / 재부팅 필요 c / 실패 d
```

요약 표의 결과 칸은 `OK` / `SKIP` / `REBOOT` / `FAIL` 입니다.

### 전원 ON 실패 진단

전원 ON 이 실패하면 vCenter 메시지 전체(fault 종류, 메시지, 세부 사유 FaultMessage)를 찍고, VM·호스트 상태를 다시 읽어 알려진 원인을 진단으로 붙입니다. 결과는 **실패**(종료코드 1)입니다.

```
  -> [bm01ev02] 전원 ON 실패 — vCenter 메시지: [GenericVmConfigFault] Module 'DevicePowerOn' power on failed. pciPassthru0(0000:3b:00.0): Device 0000:3b:00.0 is already in use by VM 'bm01ev01'
       진단: 장치 0000:3b:00.0 를 켜져 있는 다른 VM 'bm01ev01' 가 쓰고 있음
  -> [bm01ev02] 실패: 0000:3b:00.0 이미 연결됨 → 전원 ON 실패 (진단: 장치 0000:3b:00.0 를 켜져 있는 다른 VM 'bm01ev01' 가 쓰고 있음)
```

| 진단 문구 | 뜻 / 조치 |
|---|---|
| `장치 X 패스스루 켜짐·활성 아님 → 호스트 재부팅 필요` | 호스트 재부팅 후 다시 실행 |
| `장치 X 패스스루 꺼짐` / `장치 X 가 이 호스트에 없음` | 호스트 패스스루 설정 확인, 다른 호스트에서 붙인 장치면 VM 에서 제거 |
| `설정이 맞지 않음: X SystemId 교정 (a → b)` | 설정 변경 뒤에도 값이 다름 — vCenter 가 backing 변경을 반영하지 않음 |
| `설정이 맞지 않음: 메모리 100% 예약 ...` / `64bit MMIO 켬 ...` | 같은 이유 (ReconfigVM 결과 확인) |
| `펌웨어가 bios 라 ...` | §6 펌웨어 규칙 |
| `장치 X 를 켜져 있는 다른 VM 'Y' 가 쓰고 있음` | 한 장치는 켜진 VM 하나만 쓸 수 있음 — Y 에서 빼거나 Y 를 끔 |
| `사전 점검(...)으로는 원인을 찾지 못함` | 위 vCenter 메시지로 판단 (예: 호스트 IOMMU/ACS, 게스트 OS 쪽 문제) |

| 종료코드 | 뜻 |
|---|---|
| `0` | 전부 성공 또는 스킵 |
| `3` | 실패는 없고 **호스트 재부팅 필요**만 있음 — 해당 호스트를 재부팅한 뒤 같은 명령을 다시 실행하면 이어서 진행 |
| `1` | 그 밖의 실패가 하나라도 있음 (재부팅 필요가 함께 있어도 1) |

## 9. 앞으로 (메모)

- **PCI function 단위 배분** — 지금은 ev 마다 PCI 장치(function) 1개씩 줍니다(예: 듀얼 포트 카드의 `3b:00.0` 은 ev01, `3b:00.1` 은 ev02). 카드 단위(같은 슬롯 `bus:slot` 의 function 전부)를 한 VM 에 줘야 하는 경우가 생기면, 배분 단계에서 장치를 `도메인:버스:슬롯` 으로 묶어 묶음째 배분하고, 활성화·연결도 묶음의 function 전부에 하도록 바꿔야 합니다(`pciPassthruInfo.dependentDevice` 도 함께 확인).
- 실장비 확인이 필요한 것: 패스스루 backing Edit 을 vCenter 8.0.3 이 받는지(안 받으면 제거 후 다시 연결로 넘어감), QueryConfigTarget SystemId = `esxcli system uuid get` 값, GPU 64bit MMIO 크기 적정값과 BIOS 펌웨어에서의 실제 동작.
