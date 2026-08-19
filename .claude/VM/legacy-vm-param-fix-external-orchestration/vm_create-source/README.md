# vm_create-source

물리 호스트(BM) 목록을 읽어, 호스트마다 `ev01`/`ev02`/`ev03` 이름 규칙으로 VM을
**동적으로 생성**하고, 생성된 VM에 CPU/메모리 예약·Shares·부트옵션·`sched.mem.*`
extraConfig까지 **일괄 설정**하는 도구입니다. (원본 파이프라인의 "Phase 3" 단계)

> ⚠️ **이 도구는 실제로 vCenter에 VM을 생성하고 리소스를 설정(write)합니다.**

## 1. 빌드 방법

`vendor/`를 포함하고 있어 폐쇄망에서도 오프라인 빌드가 됩니다.

```bash
cd "myrepo/.claude/VM/legacy-vm-param-fix-external-orchestration/vm_create-source"
bash setup.sh
# 빌드 완료: .../vm_create-source/vm_create
```

## 2. 사용 방법

### 준비물

- 환경변수 `VC_PASSWORD`: vCenter 로그인 비밀번호 (필수)
- `worklist.txt` (기본 파일명, `-worklistFile`로 변경 가능): 대상 물리 호스트 이름을
  한 줄에 하나씩. `#`으로 시작하는 줄은 주석, 빈 줄은 무시. UTF-8 BOM 자동 제거.
- `hostgroup.txt` (기본 파일명, `-mapFile`로 변경 가능): `"BM호스트이름 hostgroup이름"`
  형식(콤마 또는 공백 구분)의 네트워크(포트그룹) 매핑 파일. 매핑이 없는 호스트는
  네트워크 어댑터 없이 VM이 생성됩니다.

### 실행 예시

```bash
export VC_PASSWORD='실제_비밀번호'

./vm_create \
  -vcTargetIP=192.168.0.50 \
  -id=administrator@vsphere.local \
  -worklistFile=worklist.txt \
  -mapFile=hostgroup.txt \
  -vmCount=2 \
  -ev01Cpu=4 -ev01Mem=8 -ev01Disk=100 -ev01Share=nomal \
  -ev02Cpu=2 -ev02Mem=4 -ev02Disk=50  -ev02Share=1000 \
  -firmware=efi \
  -prepConcurrency=16 -taskConcurrency=24
```

## 3. 옵션 상세 설명

| 플래그 | 기본값 | 필수 | 설명 |
|---|---|---|---|
| `-vcTargetIP` | (없음) | ✅ | vCenter 접속 IP. |
| `-id` | `lscsystems@vsphere.local` | | vCenter 로그인 계정 ID. |
| `-worklistFile` | `worklist.txt` | | 대상 물리 호스트 목록 파일. |
| `-mapFile` | `hostgroup.txt` | | `"BM호스트 hostgroup이름"` 네트워크 매핑 파일. |
| `-vmCount` | `2` | | 호스트 1대당 생성할 VM 수. **1~3만 지원**(그 외 값은 즉시 종료). |
| `-firmware` | `efi` | | `bios` 또는 `efi`만 허용. 정상 부팅이 확인된 값은 `efi`(권장). |
| `-datacenter` | (없음) | 조건부 | 데이터센터가 여러 개일 때만 필수. 1개뿐이면 자동 선택되어 생략 가능. |
| `-prepConcurrency` | `16` | | 호스트 사전 조사(데이터스토어/리소스풀 조회) 동시 처리 수. 500대 규모 기준 12~24 권장. |
| `-taskConcurrency` | `24` | | VM 생성/리소스 설정 Task 동시 실행 수. 500대 규모 기준 16~32 권장. |
| `-ev01Cpu` / `-ev02Cpu` | `0` / `0` | ✅(0이면 안 됨) | ev01/ev02 VM의 vCPU 수. |
| `-ev01Mem` / `-ev02Mem` | `0` / `0` | | ev01/ev02 VM의 메모리(GB). |
| `-ev01Disk` / `-ev02Disk` | `0` / `0` | | ev01/ev02 VM의 디스크(GB). |
| `-ev01Share` / `-ev02Share` | `"0"` / `"0"` | | ev01/ev02 VM의 CPU·메모리 Shares. **숫자** 또는 `nomal`(오탈자 허용, `normal`도 가능·대소문자 무시). 아래 "Share 값 상세" 참고. |
| `-ev03Cpu` | `1` | | ev03 VM의 vCPU 수 (vmCount=3일 때만 사용). |
| `-ev03Mem` | `1` | | ev03 VM의 메모리(GB). |
| `-ev03Disk` | `20` | | ev03 VM의 디스크(GB). |
| `-ev03Share` | `"1000"` | | ev03 VM의 Shares. 숫자 또는 `nomal`. |

### Share 값 상세 (`-ev01Share` / `-ev02Share` / `-ev03Share`)

- **숫자**(예: `1000`)를 넣으면 vCenter의 `SharesLevelCustom`으로 해당 값이 그대로
  CPU/메모리 Shares에 적용됩니다.
- **`nomal`**(또는 `normal`, 대소문자 무시)을 넣으면 vCenter의 `SharesLevelNormal`
  (자동 계산되는 표준 공유값)로 설정됩니다. 이 경우 숫자값은 무시됩니다.
- 숫자도 아니고 `nomal`/`normal`도 아닌 값을 넣으면 vCenter에 연결하기 **전에** 바로
  에러를 내고 종료합니다. (예: `-ev01Share=abc` → `-ev01Share 값이 올바르지 않습니다: "abc" (숫자 또는 'nomal'만 허용)`)

## 4. 동작 순서

1. 플래그 검증(`-vcTargetIP`, ev01/ev02 CPU, `-vmCount` 범위, `-firmware`, Share 값 파싱).
2. `VC_PASSWORD` 로드, `worklist.txt`/`hostgroup.txt` 읽기.
3. vCenter 접속, 데이터센터 선택(단일 자동 선택 또는 `-datacenter` 지정).
4. `ContainerView`로 **VM 전체 목록을 1회 배치 조회**해서 이미 존재하는 VM 이름을
   미리 인덱싱 (중복 생성 방지).
5. `ContainerView`로 **호스트 전체 목록을 1회 배치 조회**해서 FQDN/짧은 이름 둘 다로
   조회 가능하게 맵 구성(호스트마다 개별 조회하지 않음 → 대규모에서 빠름).
6. **[사전조사 단계]** `worklist.txt`의 각 호스트를 goroutine으로 동시 조사
   (`-prepConcurrency`로 제한): 데이터스토어 중 여유공간이 가장 큰 곳 선택, 리소스풀
   조회, hostgroup.txt 매핑으로 네트워크 포트그룹 결정. 진행 상황은 완료되는 즉시
   `[n/총계] 조사 완료`로 출력.
7. **[생성 단계]** 사전조사에 성공하고 아직 존재하지 않는 VM만 골라, `-taskConcurrency`
   로 동시 생성(ParaVirtual SCSI 컨트롤러 + Thin 아닌 디스크 + 매핑된 네트워크가
   있으면 vmxnet3 NIC 추가).
8. **[설정 단계]** 생성에 성공한 VM만 골라 `-taskConcurrency`로 동시에 Reconfigure:
   - `MemoryReservationLockedToMax=true`, 메모리 전체 예약
   - 부트옵션: EFI Secure Boot 비활성화, 디스크→NIC 순서로 부트 순서 지정
   - CPU/메모리 Shares (숫자 또는 Normal — 위 "Share 값 상세" 참고)
   - `sched.mem.pin` / `sched.mem.prealloc` / `sched.mem.prealloc.pinnedMainMem` = TRUE,
     `sched.swap.vmxSwapEnabled` = FALSE (extraConfig)
9. 전체 완료 메시지 출력.

## 5. 알려진 한계

- `-vmCount`는 1~3만 지원하며, ev04 이상은 지원하지 않습니다.
- `hostgroup.txt`에 매핑이 없는 호스트는 네트워크 어댑터 없이 VM이 생성됩니다
  (경고만 출력, 중단되지 않음).
- 게스트 OS는 `rhel8_64Guest`로 고정되어 있습니다(플래그로 변경 불가, 소스 수정 필요).
- 전체 타임아웃이 60분(`context.WithTimeout`)으로 고정되어 있습니다.
