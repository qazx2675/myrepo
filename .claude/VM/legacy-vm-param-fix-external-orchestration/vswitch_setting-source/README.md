# vswitch_setting-source

> ℹ️ **이름 주의**: 폴더명은 `vswitch_setting-source`이지만, 실제 코드는 vSwitch(가상
> 스위치) 설정과는 무관합니다. vCenter 클러스터의 **HostGroup(DRS 그룹)** 에 호스트를
> 일괄 매핑하고 `ReconfigureComputeResource_Task`로 클러스터 설정에 반영하는 도구입니다.

> ⚠️ **이 도구는 실제로 클러스터 설정(HostGroup)을 변경(write)합니다.**

## 1. 빌드 방법

`vendor/`를 포함하고 있어 폐쇄망에서도 오프라인 빌드가 됩니다.

```bash
cd "myrepo/.claude/VM/legacy-vm-param-fix-external-orchestration/vswitch_setting-source"
bash setup.sh
# 빌드 완료: .../vswitch_setting-source/vswitch_setting
```

## 2. 사용 방법

```bash
./vswitch_setting \
  -url="https://administrator@vsphere.local:비밀번호@vcenter.example.local/sdk" \
  -cluster="Production-Cluster" \
  -concurrency=24
```

대상 호스트 목록(`hostList`)과 각 호스트가 속할 HostGroup 이름은 **소스코드에
하드코딩**되어 있습니다. 실제 대상이 바뀌면 `main.go`의 아래 부분을 수정한 뒤
재빌드해야 합니다.

```go
hostList := []HostTarget{
    {HostName: "esxi-node-001.local", GroupName: "Compute-HG-A"},
    {HostName: "esxi-node-002.local", GroupName: "Compute-HG-A"},
    {HostName: "esxi-node-003.local", GroupName: "Compute-HG-B"},
    // ... 500대 노드 데이터
}
```

## 3. 옵션 상세 설명

| 플래그 | 기본값 | 설명 |
|---|---|---|
| `-url` | `https://administrator@vsphere.local:Password!@vcenter.example.local/sdk` | vCenter SDK 접속 URL. `soap.ParseURL`이 파싱하는 표준 `https://id:pw@host/sdk` 형식이어야 합니다. |
| `-cluster` | `Production-Cluster` | HostGroup을 적용할 대상 클러스터 이름. |
| `-concurrency` | `24` | 호스트 인벤토리 조회(`finder.HostSystem`) 시 동시 처리 수. 500대 규모 기준 16~32 권장. |

## 4. 동작 순서

1. `-url`로 vCenter 접속 → 기본 데이터센터 조회 → `-cluster`로 대상 클러스터 조회.
2. `hostList`의 각 항목을 goroutine으로 동시에 처리(`-concurrency`로 세마포어 제한)하며
   `finder.HostSystem()`으로 실제 호스트 객체(ManagedObjectReference)를 찾고,
   `GroupName`별로 분류(뮤텍스로 보호되는 공유 맵에 누적).
3. 모든 조회가 끝나면(`wg.Wait()`), 분류된 그룹별로 `ClusterGroupSpec`
   (`Operation: Add`)을 구성해서 `ReconfigureComputeResource_Task` **1회 호출**로
   클러스터에 일괄 적용.
4. 완료 메시지 출력.

## 5. 알려진 한계

- 호스트↔그룹 매핑이 소스코드에 하드코딩되어 있어, 실제 운영에서는 파일 기반 입력으로
  바꾸는 리팩터링이 필요합니다.
- `Operation: Add`만 사용하므로 그룹을 **새로 생성**하는 상황을 가정합니다. 이미 같은
  이름의 HostGroup이 존재하는 상태에서 호스트를 추가/치환하려는 경우 별도 처리가
  필요할 수 있습니다.
- 특정 호스트가 `finder.HostSystem()`에서 조회되지 않으면 해당 goroutine이
  `fmt.Printf`로 에러만 출력하고 조용히 건너뜁니다(전체 실행은 중단되지 않음).
