# 명령어 권고 규칙 정의서 (Phase 1 산출물)

프록시 함수가 감지·경고할 순차 조회 cmdlet과, 각각에 대응하는 고성능 대안(`Get-View -ViewType ...`)의 매핑 정의. `internal` 모듈의 프록시 함수 구현 시 이 표를 그대로 규칙 테이블로 사용한다.

## 1. 감지 대상 cmdlet ↔ 대안 매핑

| 감지 대상 cmdlet | 근거 (순차 조회 문제) | 권장 대안 | ViewType |
|---|---|---|---|
| `Get-VM` | 대상마다 개별 API 호출 반복 — VM 수가 많을수록 선형으로 느려짐 | `Get-View -ViewType VirtualMachine [-Filter @{...}]` | VirtualMachine |
| `Get-VMHost` | 위와 동일 | `Get-View -ViewType HostSystem` | HostSystem |
| `Get-Datastore` | 위와 동일 | `Get-View -ViewType Datastore` | Datastore |
| `Get-Cluster` | 위와 동일 | `Get-View -ViewType ClusterComputeResource` | ClusterComputeResource |
| `Get-ResourcePool` | 위와 동일 | `Get-View -ViewType ResourcePool` | ResourcePool |
| `Get-Datacenter` | 위와 동일 | `Get-View -ViewType Datacenter` | Datacenter |
| `Get-VirtualPortGroup` | 위와 동일 | `Get-View -ViewType Network` 또는 `-ViewType DistributedVirtualPortgroup` | Network / DistributedVirtualPortgroup |
| `<Get-VM 등> \| Get-View` (파이프라인 형태) | `Get-VM`으로 이미 개별 조회한 뒤 다시 `Get-View`를 호출 — 앞단 비용이 그대로 남음 | `Get-View -ViewType ...` 단독 호출로 대체 | 위 표와 동일 |

**공통 근거:** `Get-VM -Name X` 같은 단건 조회도 내부적으로 전체 목록을 순회한 뒤 필터링하는 경우가 많아, 대상 수가 많은 환경(수천 VM 규모)에서는 `Get-View -ViewType`을 한 번 호출해 서버 측에서 필요한 뷰만 받아오는 방식이 왕복 횟수와 응답 시간 모두에서 유리하다. (참고: [vmdev.info 벤치마크](https://www.vmdev.info/?p=125) — 동일 조건에서 `Get-VM -Name` 약 2초 vs `Get-View -ViewType VirtualMachine -Filter` 약 0.45초)

## 2. 안내 메시지 템플릿

```
[성능 안내] '<cmdlet>'은 대규모 인프라에서 순차 조회를 수행하여 성능이 저하될 수 있습니다.
  권장: Get-View -ViewType <ViewType> [-Filter @{'name'='<value>'}]
  이 메시지를 끄려면: -SkipAdvisory 스위치를 추가하세요.
```

- 색상: 경고 수준 텍스트(노란색)로 출력, 실행 자체는 막지 않음(안내만)
- 단일 객체를 이름으로 지정해 조회하는 경우(`-Name` 파라미터 사용)에도 동일하게 안내는 출력하되, "소규모/디버깅 목적이면 무시해도 됨"을 메시지에 명시

## 3. 병렬 처리 권고 대상

| 상황 | 권고 |
|---|---|
| 다수 VM 대상 설정 변경 (`Set-VM`, `New-AdvancedSetting` 등을 다건 반복) | `$vms \| ForEach-Object -Parallel { ... } -ThrottleLimit <N>` |
| 다수 대상에 대한 조회 후 후처리(가공/집계) | `Get-View -ViewType ...`로 일괄 조회 후 PowerShell 파이프라인에서 후처리 (조회 자체를 병렬화할 필요 없음 — 이미 단일 호출) |

## 4. 억제(Bypass) 옵션 설계

- 모든 프록시 함수에 공통 스위치 파라미터 `-SkipAdvisory` 추가
- 세션 전체에 대해 끄고 싶은 경우: 환경변수 또는 `$Global:VCAdvisory_Disabled = $true` 방식의 전역 스위치도 함께 제공 (교육 후반부에 "이미 배웠으니 계속 안내받고 싶지 않다"는 요구 대응)

## 5. 검증 필요 사항 (Phase 1 후속)

- 위 매핑은 문서/커뮤니티 자료 기반 — 실제 `vc-test-env`로 재현한 vcsim 환경에서 `Get-VM` vs `Get-View -ViewType VirtualMachine`의 응답 시간을 직접 측정해 표에 실측치를 추가해야 함
- PowerCLI 버전에 따라 `Get-View`의 `-Filter` 문법이 달라질 수 있어, 목표 PowerCLI 버전 고정 후 재확인 필요
