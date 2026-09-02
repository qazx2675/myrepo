# CHANGELOG

`VM_setup` 아래 도구들에 기능이 추가·수정될 때마다 이 파일에 날짜순(최신이 위)으로 기록합니다.

---

## 2026-09-02 — `vswitch_setting-source` 코드 교체 (HostGroup 매핑 → vSwitch 포트그룹 생성)

- 메일(`go lang 모음 V2`)의 `main_vs.txt`로 `main.go` 전체를 교체했다. 기존 코드는 폴더명과 달리 클러스터 HostGroup(DRS 그룹)을 매핑하는 도구였는데, 새 코드는 폴더명 그대로 **`worklist.txt`의 (호스트, 포트그룹, VLAN)을 읽어 각 호스트 `vSwitch0`에 포트그룹을 일괄 생성**한다(`HostNetworkSystem.AddPortGroup`).
- 플래그 변경: `-url`/`-cluster`/`-concurrency` → `-vcTargetIP`(필수), `-id`, `-worklistFile`, `-targetVSwitch`. 비밀번호는 환경변수 `VC_PASSWORD`로 받는다.
- 오래된 빌드 산출물 `vswitch_setting`(옛 로직 바이너리)을 삭제했다. 폐쇄망에서 `bash setup.sh`로 재빌드해야 한다.
- `README.md`를 새 동작/플래그에 맞게 다시 작성했다.
- `go.mod`/`go.sum`/`vendor/`는 그대로 두었다. 새 코드가 쓰는 패키지(`object`, `view`, `mo`, `types`)는 모두 기존 `vendor/`에 포함돼 있다.
- **검증**: 이 환경에 Go 툴체인이 없어 빌드/실행 검증은 하지 못했다. 소스 교체와 import 대상이 vendor에 존재하는지만 확인.

## 2026-08-25 — `vm_create-source` 게스트 OS 지정 옵션(`-guestId`) 추가

- **`-guestId` 플래그 신설 (`main.go`)**: 기존에는 `rhel8_64Guest`가 소스에 하드코딩돼 있어 다른 OS로 만들려면 소스를 고쳐야 했다. 이제 `-guestId=rhel9_64Guest`처럼 인자로 지정할 수 있고, **아무것도 주지 않으면 종전과 동일하게 `rhel8_64Guest`가 기본값**으로 쓰인다.
  - 게스트 OS 식별자 목록은 vSphere 버전마다 달라지므로 도구에서 화이트리스트로 막지 않는다(막으면 새 OS가 나올 때마다 소스를 고쳐야 함). 빈 값만 즉시 거르고, 실제 유효성은 vCenter가 `CreateVM` 단계에서 판정한다.
  - 시작 시 `[INFO] 게스트 OS: <값> / 펌웨어: <값>`을 출력해서 어떤 값으로 만들어지는지 바로 확인할 수 있게 했다.
- **VM 생성 실패가 조용히 무시되던 문제 보완 (`main.go`)**: 예전에는 `CreateVM`/Task 실패 시 아무 출력 없이 넘어가서, 예컨대 `-guestId`에 오타가 있으면 `생성 대상 VM 12대`라고 찍은 뒤 아무것도 만들어지지 않은 채 `새로 생성할 VM이 없거나...`로 끝나 원인을 알 수 없었다. 이제 실패 사유를 출력하고(대수가 많을 때 로그가 넘치지 않도록 **처음 5건까지만** 상세 출력), 마지막에 **실패 총 건수**와 `-guestId` 확인 안내를 요약해 준다. 성공 경로의 동작·출력은 이전과 동일하다.
- **검증**: vcsim 기준 ① 옵션 미지정 → `[INFO] 게스트 OS: rhel8_64Guest`, 생성된 VM의 실제 `guestId`도 `rhel8_64Guest` ② `-guestId rhel9_64Guest` → 실제 `guestId`가 `rhel9_64Guest`로 반영됨(독립 호스트/클러스터 호스트 양쪽에서 확인) ③ 빈 값/공백만 준 경우 즉시 종료 — 3가지 모두 확인.
  - 잘못된 식별자에 대한 실패 처리는 vcsim이 `guestId`를 검증하지 않아 시뮬레이터로는 재현되지 않으므로, 소스 사본에 생성 실패를 강제 주입해 별도로 검증함(12건 실패 시 상세 5건 + `실패 12건 (전체 12건 중)` 요약이 정상 출력됨을 확인).

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
