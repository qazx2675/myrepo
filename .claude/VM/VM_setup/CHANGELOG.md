# CHANGELOG

`VM_setup` 아래 도구들에 기능이 추가·수정될 때마다 이 파일에 날짜순(최신이 위)으로 기록합니다.

---

## 2026-08-25 — `vm_create-source` 속도 개선 (동작 변경 없음)

대상 호스트가 많을수록 vCenter 왕복(round-trip) 횟수가 선형으로 늘어나던 구간들을 전부 배치 조회로 바꾸고, VM당 Task 수를 줄였습니다. **생성되는 VM의 최종 설정값은 이전과 완전히 동일합니다.**

- **VM당 Task 2회 → 1회 (`main.go`)**: 예전에는 `CreateVM`으로 만든 뒤 별도 `Reconfigure` Task로 메모리 예약·CPU/메모리 Shares·`sched.mem.*` extraConfig·Secure Boot 해제를 넣었다. 이 값들은 생성 시점에 이미 확정돼 있고 device key에 의존하지 않으므로 **생성 스펙에 함께 담아** Task 1회로 처리하도록 바꿨다.
  - 단, **부팅 순서(BootOrder)만은 생성 이후에 남겨뒀다** — 실제 device key는 VM이 만들어진 뒤에야 확정되므로, 생성 스펙에 임시 음수 key로 넣으면 동작이 달라질 위험이 있어 의도적으로 합치지 않았다.
- **인벤토리 재귀 탐색 제거 (`main.go`)**: 설정 단계에서 VM마다 `finder.VirtualMachine()`으로 인벤토리를 다시 뒤지던 것을, 생성 Task가 돌려주는 MoRef(`task.WaitForResult()`)를 그대로 쓰도록 바꿨다. VM 대수가 많을수록 이 탐색이 급격히 느려지던 구간이 통째로 사라진다.
- **디바이스 목록 배치 조회 (`main.go`)**: 부팅 순서를 정하려고 VM마다 `Properties()`를 호출하던 것을, 생성된 VM 전체에 대해 1회 배치 조회로 바꿨다.
- **데이터스토어 배치 조회 (`main.go`)**: 사전조사 goroutine 안에서 호스트마다 `pc.Retrieve(datastore)`를 부르던 것을, 전체 호스트의 데이터스토어를 중복 제거해 1회 배치 조회하도록 바꿨다. 선택 로직은 그대로 각 호스트 자신의 데이터스토어 안에서만 최대 여유공간을 고르므로 결과는 동일하다.
- **리소스풀 배치 조회 (`main.go`)**: `HostSystem.ResourcePool()`은 내부적으로 `parent` 조회 + `ComputeResource` 조회로 **호스트당 2회** 왕복이 발생한다. 여러 호스트가 같은 클러스터를 공유하므로 부모를 중복 제거한 뒤 `ComputeResource`/`ClusterComputeResource` 타입별로 한 번씩만 조회하도록 바꿨다(govmomi 원본과 동일한 타입 분기).
  - 그 결과 **사전조사 goroutine 안에서는 vCenter 왕복이 아예 발생하지 않는다**(전부 맵 조회).
- **검증**: 변경 전(git HEAD) 바이너리와 변경 후 바이너리를 각각 별도 vcsim 인스턴스에 돌려 결과를 비교함. 독립 호스트(`ComputeResource`)와 클러스터 호스트(`ClusterComputeResource`)를 모두 포함한 4개 호스트 × `-vmCount=3` = **12대 생성**, 커스텀 ratio(`4000`)와 `nomal` 두 Share 모드를 모두 사용.
  - VM별 `numCPU`/`memoryMB`/`firmware`/`guestId`/`memoryReservationLockedToMax`/메모리 예약/CPU·메모리 Shares(level+ratio)/`sched.*` extraConfig/Secure Boot/부트 순서를 덤프해 비교 — **차이 0건**.
  - 디바이스(ParaVirtual SCSI, 디스크 용량, vmxnet3 NIC 포트그룹)도 12대 전부 비교 — **차이 0건**.
  - 재실행 시 이미 존재하는 VM을 건너뛰는 멱등성도 그대로 유지됨을 확인.
- **출력 문구 변경**: 2단계 진행 메시지가 `리소스 설정 대상 VM N대` → `부팅 순서 설정 대상 VM N대`로 바뀌었다(리소스 설정이 생성 단계로 옮겨갔으므로). 그 외 출력은 동일.
