# vcsim 연동 가이드 (Phase 4 산출물)

`vc-test-env`(`../`)로 기동한 vcsim 위에서 본 프로젝트의 자동완성/권고 모듈을 스모크테스트하는 절차.

## 1. 실습용 vcsim 기동

```bash
cd ../                     # vcenter-test-env-vcsim/
export VC_USER='administrator@vsphere.local'
export VC_PASS='...'
./vc-test-env -vc=192.168.0.50
```

- 콘솔에 재생성된 인벤토리 트리와 접속 정보(`127.0.0.1:54321`)가 출력되면 대기 상태로 유지 (Ctrl+C 전까지 유지해야 함)

## 2. 다른 터미널에서 PowerCLI 접속 + 모듈 로드

```powershell
pwsh
Connect-VIServer -Server 127.0.0.1:54321 -User administrator@vsphere.local -Password 아무값 -Force
# setup.sh로 설치했다면 $PROFILE에서 이미 자동 로드됨. 수동 테스트 시:
Import-Module ./profile/VCenterAdvisory.psm1
Import-Module ./profile/VCenterCompleters.psm1
```

## 3. 확인 항목 체크리스트

| 항목 | 확인 방법 | 기대 결과 |
|---|---|---|
| 권고 메시지 출력 | `Get-VM` 실행 | 노란색 `[성능 안내]` 메시지 + `Get-View -ViewType VirtualMachine` 권장 문구 출력, 이어서 원래 결과도 정상 출력 |
| bypass 옵션 | `Get-VM -SkipAdvisory` | 안내 메시지 없이 결과만 출력 |
| 전역 disable | `$Global:VCAdvisory_Disabled = $true` 후 `Get-VM` | 안내 메시지 없음 |
| 이름 자동완성 | `Get-VM -Name <Tab>` | vcsim에 재생성된 실제 VM 이름 목록이 후보로 뜸 |
| 다른 대상 완성 | `Get-VMHost -Name <Tab>`, `Get-Cluster -Name <Tab>` | 각각 vcsim의 호스트/클러스터 이름 후보로 뜸 |
| 캐시 동작 | 15초 이내 연속 Tab | vCenter/vcsim 재조회 없이 즉시 후보 표시 (반응 속도로 간접 확인) |
| 네이티브 멤버 완성 | `(Get-View -ViewType VirtualMachine)[0].<Tab>` | `VMware.Vim.VirtualMachine` 타입의 속성/메서드 목록이 뜸 — 이 항목은 본 프로젝트가 만든 게 아니라 PowerShell 자체 기능임을 재확인하는 용도 |
| 대안 명령 동작 | `Get-View -ViewType VirtualMachine \| Select Name` | vcsim의 VM 이름 목록 정상 조회 |

## 4. 알려진 제약 (vc-test-env README 계승)

- 씨드 호스트가 하나 더 보일 수 있음 — 실제 VM 동작에는 영향 없으므로 무시
- 다중 데이터센터 레시피는 아직 미지원 — 단일 데이터센터 시나리오로만 실습 진행
- 네트워크 어댑터는 포트그룹 이름/커넥트 상태만 재현 — VLAN ID 관련 실습은 vcsim으로 불가

## 5. 미실행 사항 (실제 인프라 필요)

이 문서는 설계/절차 문서이며, 위 체크리스트는 **실제 Linux 환경(pwsh + PowerCLI + vcsim)에서 아직 실행 검증되지 않았다.** 현재 작업 환경(Windows, pwsh 미설치)에서는 실행할 수 없으므로, Rocky Linux/RHEL 서버에서 위 절차를 그대로 실행해 결과를 기록해야 Phase 4가 완료된다.
