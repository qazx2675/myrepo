# vswitch_setting-source

> ⚠️ **이 도구는 실제로 ESXi 호스트의 네트워크 설정(vSwitch 포트그룹)을 변경(write)합니다.**

`worklist.txt`에 나열된 (호스트, 포트그룹, VLAN) 조합을 읽어, 각 호스트의
표준 가상 스위치(`vSwitch0`)에 포트그룹을 일괄 생성하는 도구입니다.
(`HostNetworkSystem.AddPortGroup`)

## 1. 빌드 방법

`vendor/`를 포함하고 있어 폐쇄망에서도 오프라인 빌드가 됩니다.

```bash
cd "myrepo/.claude/VM/VM_setup/vswitch_setting-source"
bash setup.sh
# 빌드 완료: .../vswitch_setting-source/vswitch_setting
```

## 2. 사용 방법

```bash
export VC_PASSWORD='비밀번호'
./vswitch_setting \
  -vcTargetIP="vcenter.example.local" \
  -id="lscsystems@vsphere.local" \
  -worklistFile="worklist.txt" \
  -targetVSwitch="vSwitch0"
```

`worklist.txt` 형식 (공백 구분, 빈 줄과 `#` 주석 무시):

```
# <호스트명>  <포트그룹명>  <VLAN ID>
esxi-node-001.local  PG-APP-100  100
esxi-node-001.local  PG-DB-200   200
esxi-node-002.local  PG-APP-100  100
```

## 3. 옵션 상세 설명

| 플래그 | 기본값 | 설명 |
|---|---|---|
| `-vcTargetIP` | (필수) | vCenter 접속 IP/호스트명. 누락 시 실행 중단. |
| `-id` | `lscsystems@vsphere.local` | vCenter 로그인 계정 ID. |
| `-worklistFile` | `worklist.txt` | vSwitch/포트그룹 설정 파일. 실행 디렉토리 기준 상대 경로. |
| `-targetVSwitch` | `vSwitch0` | 포트그룹을 생성할 대상 표준 가상 스위치. |

비밀번호는 플래그가 아니라 환경변수 `VC_PASSWORD`로 전달합니다. 미설정 시 실행 중단.

## 4. 동작 순서

1. `-worklistFile` 파싱 → `(호스트, 포트그룹, VLAN)` 목록과 고유 호스트 목록 생성.
2. `-vcTargetIP`로 vCenter 접속.
3. `HostSystem` 컨테이너 뷰로 전체 호스트의 `name`, `configManager.networkSystem`을
   1회 일괄 수집.
4. 고유 호스트별로 `HostNetworkSystem.AddPortGroup`을 호출해 해당 호스트의 항목을
   순차 생성. 이미 존재하면(`AlreadyExists`) 스킵, 그 외 에러는 출력 후 계속 진행.
5. 완료 메시지 출력.

## 5. 알려진 한계

- 포트그룹은 `-targetVSwitch` 하나에만 생성합니다. 호스트별로 다른 vSwitch에
  생성하려면 파일 포맷/코드 수정이 필요합니다.
- 호스트를 vCenter 인벤토리에서 찾지 못하거나 `networkSystem`이 없으면 해당 호스트를
  건너뜁니다(전체 실행은 중단되지 않음).
- 포트그룹 정책(`HostNetworkPolicy`)은 빈 값으로 생성되어 vSwitch 기본 정책을
  상속합니다.

## 6. 디렉토리 구조

```
vswitch_setting-source/
├── README.md         # 이 문서
├── main.go           # vSwitch 포트그룹 일괄 생성 로직 전체
├── go.mod / go.sum     # Go 모듈 정의 파일
├── setup.sh          # vendor 패키지로 폐쇄망에서도 빌드하는 스크립트
├── vswitch_setting   # setup.sh로 빌드된 실행 바이너리 (빌드 후 생성)
└── vendor/           # 빌드에 필요한 Go 의존성 패키지 모음 (서드파티, 문서화 대상 제외)
```
