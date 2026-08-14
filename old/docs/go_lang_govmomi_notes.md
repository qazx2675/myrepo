# govmomi 뷰 타입 및 활용 노트

1. 자원 관리 계층 (Compute & Hardware)
- HostSystem : 물리 ESXi 호스트 서버
- VirtualMachine : VM (가상머신)
- ClusterComputeResource : 여러 호스트를 묶어둔 ESXi 클러스터
- ResourcePool : CPU/메모리 자원을 할당받은 리소스 풀

실무 활용 예시: "특정 클러스터(ClusterComputeResource)에 속한 전체 호스트(HostSystem)들의 CPU 사용률 집계"

2. 저장소 계층 (Storage)
- Datastore : ESXi에 연결된 데이터스토어 (VMFS, NFS, vSAN 등)
- StoragePod : 데이터스토어 여러 개를 묶은 '데이터스토어 클러스터'

실무 활용 예시: "잔여 용량이 10% 미만인 Datastore 전체 목록 추출 및 알람"

3. 네트워크 계층 (Networking)
- DistributedVirtualPortgroup : 분산 가상 스위치(dVS)의 포트그룹 (가장 흔함)
- Network : 표준 가상 스위치(vSS)의 네트워크 또는 일반 포트그룹
- VmwareDistributedVirtualSwitch : 분산 가상 스위치 본체

4. 조직 및 관리 계층 (Logical Organization)
- Folder : VM이나 호스트가 담긴 vCenter 내부 폴더
- Datacenter : 최상위 데이터센터 단위 (예: 서울_DC, 대전_DC)

5. vCenter 시스템 및 서비스 계층 (Management & Services)
- Task : 현재 진행 중인 작업 (이동, 생성, 스냅샷 생성 등)
- Alarm / AlarmManager : vCenter 알람 설정 및 모니터링
- OptionManager : vCenter Advanced Setting(고급 설정값) 조작

```
# 1. 모든 VM 가져오기 (가장 흔함)
Get-View -ViewType VirtualMachine -Property Name, Runtime.PowerState

# 2. 모든 호스트 서버 가져오기
Get-View -ViewType HostSystem -Property Name, OverallStatus

# 3. 모든 데이터스토어 가져오기
Get-View -ViewType Datastore -Property Name, Summary.FreeSpace
```
