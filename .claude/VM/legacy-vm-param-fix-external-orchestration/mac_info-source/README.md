# mac_info-source

`ev01`/`ev02`/`ev03`... 이름 규칙으로 생성된 VM들의 **MAC 주소를 조회**해서, Kickstart
프로비저닝용 텍스트 파일(`Provisioning_List_<vCenterIP>.txt`)을 생성하는 도구입니다.
(원본 파이프라인의 "Phase 4" 단계)

> ℹ️ **읽기 전용(read-only) 도구입니다.** vCenter의 어떤 설정도 변경하지 않습니다 —
> VM 인벤토리와 하드웨어 디바이스 정보만 조회하고, 결과는 로컬 텍스트 파일로만
> 저장됩니다.

## 1. 빌드 방법

`vendor/`를 포함하고 있어 폐쇄망에서도 오프라인 빌드가 됩니다.

```bash
cd "myrepo/.claude/VM/legacy-vm-param-fix-external-orchestration/mac_info-source"
bash setup.sh
# 빌드 완료: .../mac_info-source/mac_info
```

## 2. 사용 방법

### 준비물

- 환경변수 `VC_PASSWORD`: vCenter 로그인 비밀번호 (필수)
- `worklist.txt` (기본 파일명, `-worklistFile`로 변경 가능): 대상 물리 호스트(BM) 이름을
  한 줄에 하나씩. `#` 주석/빈 줄 무시.

### 실행 예시

```bash
export VC_PASSWORD='실제_비밀번호'

./mac_info \
  -vcTargetIP=192.168.0.50 \
  -id=administrator@vsphere.local \
  -worklistFile=worklist.txt \
  -arg1=SITE01 \
  -argInt=1 \
  -argStr=KS01
```

## 3. 옵션 상세 설명

| 플래그 | 기본값 | 필수 | 설명 |
|---|---|---|---|
| `-vcTargetIP` | (없음) | ✅ | vCenter 접속 IP. |
| `-id` | `lscsystems@vsphere.local` | | vCenter 로그인 계정 ID. |
| `-worklistFile` | `worklist.txt` | | 대상 물리 호스트(BM) 목록 파일. |
| `-arg1` | (없음) | ✅ | 출력 라인의 2번째 인자(예: 사이트/그룹 식별자). 출력 포맷의 `%s` 자리에 그대로 들어갑니다. |
| `-argInt` | `0` | | 출력 라인에 들어가는 정수 인자(예: 순번). |
| `-argStr` | (없음) | ✅ | 출력 라인에 들어가는 문자열 인자(예: Kickstart 프로파일 이름). |

## 4. 동작 순서

1. `VC_PASSWORD` 로드, `worklist.txt` 읽기.
2. vCenter 접속(`insecure=true`), 기본 데이터센터 선택.
3. `worklist.txt`의 각 호스트 베이스 이름(`.`으로 자른 첫 토큰)에 대해
   `<베이스이름>ev\d+` 정규식 패턴을 만들어, 전체 VM 목록(`finder.VirtualMachineList("*")`)
   중 이름이 매칭되는 VM만 골라냅니다.
4. 매칭된 각 VM의 `config.hardware.device`를 조회해서, 첫 번째로 발견되는
   가상 이더넷 카드의 MAC 주소를 사용(없으면 `MAC_NOT_FOUND`).
5. IP는 실제로 조회하지 않고 **항상** `<VM이름>_DNS_AND_TOOLS_NOT_FOUND` 형식으로
   채웁니다(실제 IP 조회 로직은 포함되어 있지 않음 — 필요 시 VMware Tools의
   `guest.ipAddress` 속성을 추가로 조회하도록 확장해야 합니다).
6. 아래 포맷으로 한 줄씩 구성:
   ```
   VM VM <arg1> <VM이름> <VM이름>_DNS_AND_TOOLS_NOT_FOUND <MAC> eth0 sda sda5 <argInt> <argStr> uefi
   ```
7. 콘솔에 전체 리스트를 출력하는 동시에, 현재 작업 디렉터리에
   `Provisioning_List_<vcTargetIP의 .을 _로 치환>.txt` 파일로 저장.

## 5. 알려진 한계

- IP 조회 로직이 없어 IP 필드는 항상 `<VM이름>_DNS_AND_TOOLS_NOT_FOUND` 고정값입니다.
- VM 이름 매칭 패턴이 `ev\d+`라 `vmCount` 제한 없이 `ev01`~`ev99`까지 전부 매칭됩니다
  (다른 도구들의 `-vmCount`와는 무관하게 동작).
- 동일 베이스 이름에 이더넷 카드가 여러 개인 VM은 **첫 번째로 발견된 카드**의 MAC만
  사용합니다.
- 출력 파일은 실행할 때마다 같은 경로에 덮어씁니다(파일명이 vCenter IP 기준으로만
  결정되고 타임스탬프가 없음).
