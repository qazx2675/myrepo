# PCI 패스스루 (임시 보관)

> **이 폴더는 기록용으로만 남겨 둔 것입니다.** 통합·관리되는 버전은 [`V2/VMsetup/pci_passthrough-source`](../V2/VMsetup/pci_passthrough-source/README.md) 입니다(활성화 + VM 연결 + 전원 ON 을 Go 하나로, `vm_setup.sh` 가 스펙의 `pci-evNN` 키로 실행). 아래 두 스크립트는 더 고치지 않습니다. 예전 부팅 실패 원인(backing `SystemId` 에 하드웨어 UUID 사용)도 통합 버전에서 고쳤습니다.

Gmail(aisoki2675@gmail.com)에서 찾은 스크립트를 일단 그대로 보관한 것. 실장비 검증 전.

| 기능 | 파일 | 출처 메일 |
|---|---|---|
| 패스스루 **활성화** (Go, govmomi) | `enable-go/main.go` | 2026-08-10 "패스스루 활성화 및 장착 go lang" 첨부 `main_passthrough_enable.go` |
| 활성화된 PCI **VM 연결** (PowerShell, PowerCLI) | `attach_pci.ps1` | 2026-07-23 23:46 UTC 본문 (제목이 코드 첫 줄인 메일) |

## 사용

- 두 스크립트 모두 같은 디렉터리의 `worklist_pci.txt`(VM 이름 한 줄씩)를 읽는다.
- 활성화(Go): `VCENTER_USER`/`VCENTER_PASS` 환경변수 필요.

```bash
cd enable-go
go build -o pci-passthrough-enable .
./pci-passthrough-enable -vcTargetIP <vCenter> -pciVendor Mellanox -worklistFile worklist_pci.txt
```

- 연결(PowerShell): `Create_VM_pwsh_dir/password_dir/secret_VMw.key`, `password_VMw.enc` 필요.

```powershell
./attach_pci.ps1 -vcTargetIP <vCenter> -pciVendor Mellanox
```

## 미해결

- Go 활성화 후 호스트 재부팅이 필요할 수 있다.
- 메일 마지막 본문의 `SystemId`(esxcli system uuid get vs `host.Summary.Hardware.Uuid`) 확인은 회사 환경 검증 대기. 이 PowerShell 연결본은 `Add-PassthroughDevice`를 쓰므로 해당 없음, Go 연결본(`main_passthrough_attach.go`)은 이 저장소에 넣지 않았다.
- `attach_pci.ps1` 은 UTF-8(BOM 없음). Windows PowerShell 5.1 에서 한글이 깨지면 BOM 을 붙일 것(RHEL pwsh 7 은 문제 없음).
