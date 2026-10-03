# AWX 노드정보 V2 스크립트

HPC 서버 노드정보를 자동 수집·가공하여 AWX 인벤토리/DHCP/PXE를 일괄 등록하는 배시 스크립트 세트.

## 빌드 및 설치 방법

본 프로젝트는 bash 전용이므로 빌드 단계는 없습니다. 다음과 같이 파일을 배치하고 최상단 변수를 설정합니다.

### 파일 배치

```
awx_script/
├── 01.AWX_nodeinfo_V2.sh       ← 메인 스크립트
├── 02.source_dhcp_pxe.sh       ← 보조 스크립트
├── awxkit/                      ← 저장소의 .claude/HPC/awxkit/ 를 통째로 복사 (vendor 포함, 폐쇄망 빌드: bash awxkit/setup.sh)
│   ├── nodeinfo.sh / invsync.sh / dhcp.sh / pxe.sh   ← 01·02 가 호출하는 래퍼
│   └── output/                  ← (자동 생성) ${user}_nodeinfo.yaml
├── .power_limit_setting.txt     ← AI 서버 전력 제한 설정 가이드
├── LOG/                         ← (자동 생성) 사용자별 실행 로그
├── tmp/                         ← (자동 생성) 임시 결과물
├── dhcp_pool/                   ← (자동 생성) DHCP 삭제 정보
└── test/                        ← 테스트 하네스
    └── run_tests.sh
```

### 의존성

| 항목 | 요구사항 | 설명 |
|---|---|---|
| 운영체제 | RHEL 7+ | bash 4.0+, gawk/awk |
| 셸 | bash 4.x | bash -n 검사, 배열 문법 |
| 유틸 | gawk/awk | 다단 출력, MAC 보정, 분할 |
| 유틸 | sed | 파일 내용 치환 (sed -i) |
| 유틸 | ssh, scp | 원격 명령 실행, 파일 전송 |
| 유틸 | gossh | 병렬 호스트 확인 (PATH 에 있어야 함) |
| 외부 | awxkit | AWX 인벤토리 등록 래퍼 |

### 최상단 변수 7개 채우기

`01.AWX_nodeinfo_V2.sh` 최상단의 빈 변수들을 현장 값으로 채웁니다.

```bash
#!/bin/bash
# AWX nodeinfo V2 - HPC 서버 등록 전처리 및 인벤토리/DHCP/PXE 등록

repohost=""                  # ← custom_inventory.sh 가 있는 서버. 분할/전체 입력 파일을 scp 로 /root/Inventory/ 에 보내고 ssh 로 실행
svr_dir=""                   # ← custom_inventory.sh 가 yml 을 쓰는 경로(이 서버에서 보이는 공유 경로)
ai_server_list=""            # ← AI 서버 호스트명 "host01|host02" (| 구분, 정확 일치). 비워 두면 안내를 건너뜀
day_print=""                  # ← (선택) 이 변수에 user 가 들어 있으면(공백/쉼표/| 구분) 작업 대상을 "날짜 tmp tmp infra hostname ip mac ..." 형식으로 출력
lacp_comment=""              # ← LACP(802.3ad) 호스트가 있을 때 출력할 안내 문구
inventory_delete_host=""     # ← 작업진행 Y 후 `ssh <host> "bash /root/server/delhost_${user}"` 를 실행할 서버
infra_alias=""               # ← (선택) 등록되지 않은 infra 이름 치환 "adjfg:infra1 foo:infra2" (공백/쉼표 구분, `이름:바꿀infra`). 비우면 치환 없음
```

(참고) 이 7개 외에 auto_setup 연계용 빈 변수 `auto_setup_host=""` 가 하나 더 있습니다(선택, 아래 "auto_setup 전달" 참고 — 비워 두면 기존과 동일, 필수 검증 대상 아님).

`ai_server_list`·`infra_alias`·`day_print` 외는 필수이며, 작업진행 Y 직후(원격 삭제 전)에 비어 있으면 `[X] <변수> 가 비어 있습니다` 로 종료합니다. 값은 현장 값으로 직접 채우십시오(저장소에는 모두 빈 값으로 둡니다).

### user() 함수 (사용자 선택)

`user()` 는 아래 현장 코드를 그대로 사용합니다. `user_route` 는 `info_mn.sh`·`info.sh` 가 있는 경로로 현장에서 채우십시오(저장소에는 빈 값).

```bash
user(){
user_route=""
bash $user_route/info_mn.sh
read -p "Input Number: " user_choice
user=`bash $user_route/info.sh $user_choice`
}
```

이 함수가 `user` 변수를 설정합니다. 호출 후 `$user` 가 비어 있으면 `[X] user 값이 없습니다 (user 함수 확인)` 로 종료하며, 별도 입력 프롬프트는 없습니다.

## 사용 방법

### Step 1: 기본 검사

```bash
cd /path/to/awx_script

# 문법 검사
bash -n 01.AWX_nodeinfo_V2.sh
bash -n 02.source_dhcp_pxe.sh
shellcheck -S warning 01.AWX_nodeinfo_V2.sh 02.source_dhcp_pxe.sh
```

### Step 2: 호스트 목록 준비

`${user}.txt` 를 스크립트와 같은 디렉터리에 준비합니다. 형식은 프롬프트 선택에 따라 다릅니다.

- **Y (nodeinfo 사용)**: `${user}.txt` 에 호스트명을 한 줄에 하나씩 적습니다. `awxkit/nodeinfo.sh -hosts` 의 입력으로 쓰이고, 수집 결과(`awxkit/output/${user}_nodeinfo.yaml`)가 `${user}.txt` 를 덮어씁니다(nodeinfo 가 실패하면 덮어쓰지 않음).
- **N (nodeinfo 미사용)**: `${user}.txt` 가 이미 `"msg": "…"` 가 들어 있는 nodeinfo 출력이거나 12필드 줄(`vendor model infra hostname ip mac nic disk part 용량 os boot`)이어야 합니다. 호스트명만 있으면 12필드 검사에서 종료합니다.

### Step 3: 테스트 실행

```bash
bash test/run_tests.sh
```

모든 케이스가 통과해야 프로덕션 실행 가능합니다.

### Step 4: 실제 실행

```bash
bash 01.AWX_nodeinfo_V2.sh
```

다음 프롬프트들에 응답합니다:

```
awx nodeinfo 사용여부 Y|N : Y        # Y: nodeinfo 수집, N: 기존 ${user}.txt(12필드/msg) 사용
[등록 대상 20개씩 세로 다단 출력]
작업진행여부 (Y|N) : y               # y: 계속 진행, n: 취소
[작업 진행...]
AWX 인벤토리 소스 : su exit : 종료 ls : yaml 파일출력 : su  # su: 02 진입
[02 자동 실행]
옵션이 맞습니까 (Y|N) : y            # 02 옵션 확인표 확인
[인벤토리/DHCP/PXE 등록]
완료
```

## 옵션별 상세 설명

### 01.AWX_nodeinfo_V2.sh 프롬프트 및 메뉴

#### [2] download_txt 프롬프트

```
awx nodeinfo 사용여부 Y|N : Y
```

- **Y**: AWX 노드정보 자동 수집 (awxkit/nodeinfo.sh 호출)
- **N**: 기존 `${user}.txt` 파일 사용 (파일이 있어야 함)

#### [5] 등록 대상 다단 출력

호스트명을 20개씩 세로 정렬하고, 가로로 나열합니다.

```
host01 host21 host41
host02 host22 host42
…
총 45대
```

#### [6] 작업진행여부

```
작업진행여부 (Y|N) : y
```

- **y**: 다음 단계(inventory_delete) 진행
- **n**: 작업 취소 후 종료

#### [13] 메뉴 명령어

```
AWX 인벤토리 소스 : su exit : 종료 ls : yaml 파일출력 : <명령>
```

| 명령 | 설명 |
|---|---|
| `su` | 02 진입 (yml별 인벤토리 등록) |
| `exit` | 종료 (exit 0) |
| `ls` | 생성된 yml 파일 + boot 옵션 출력 |
| `<파일명>` | yml 파일 내용 출력 (예: `INFRA-A_inventory-1234_3ea.yml`) |
| (Enter) | 무시 (다시 프롬프트) |

### 02.source_dhcp_pxe.sh 옵션

#### 배치 모드 (01에서 자동 호출)

```bash
bash 02.source_dhcp_pxe.sh <user> <yml1>=<opts1> [<yml2>=<opts2> ...]
```

예:

```bash
bash 02.source_dhcp_pxe.sh testuser \
  INFRA-A_inventory-1234_5ea.yml=INFRA-A,RHEL8,UEFI,On-premise \
  INFRA-B_inventory-1234_3ea.yml=INFRA-B,RHEL9,legacy,Cloud
```

#### 옵션 형식: `<yml>=<infra>,<os>,<boot>,<splunk>`

| 필드 | 설명 | 예 |
|---|---|---|
| yml | 파일명 (필수) | `INFRA-A_inventory-1234_5ea.yml` |
| infra | 인프라 코드 | `INFRA-A`, `INFRA-B` |
| os | 인자로 받지만 **02 가 무시하고** 아래 "OS 버전 선택"으로 정함 | (아무 값) |
| boot | 부트 방식 | `UEFI`, `legacy`, `BIOS` |
| splunk | Splunk 설치 여부 선택값 | `On-premise`, `Cloud`, `no` |


- **infra 는 소문자로 변환해 전달**합니다(`INFRA-A` → `infra-a`). awxkit 은 conf 의 `*_choices` 와 **대소문자까지 정확히 일치**해야 하고(번호 또는 값 그대로 사용 가능), os/boot/splunk 는 변환하지 않습니다. 확인표에서 `N` 으로 직접 입력한 infra 도 소문자로 변환됩니다.
- **실행 순서**: yml 마다 `invsync` 를 먼저 실행한 뒤 `dhcp` 와 `pxe` 를 **동시에** 실행하고, 둘 다 끝나야 다음 yml 로 넘어갑니다. 동시 실행이라 프롬프트를 받을 수 없어 stdin 을 닫으며(옵션 값이 비면 대화형 대신 오류), 출력은 둘 다 끝난 뒤 `--- dhcp ---`, `--- pxe ---` 순으로 보여 줍니다. 한쪽만 실패해도 다른 쪽은 끝까지 실행됩니다.
#### 색상 출력터미널에서 실행할 때만 색을 입힙니다(리다이렉트·파이프·`NO_COLOR=1` 이면 꺼짐, `AWX_COLOR=1` 로 강제 가능). 로그(`LOG/${user}.log`)에는 색 코드가 들어가지 않습니다.| 대상 | 색 ||---|---|| 오류 `[X]`, 응답 없음, 메뉴 `exit`, 02 실패 항목 | 빨강 || LDAP 동일 요약의 LDAP 값, 메뉴 `su`, 완료·옵션 확정·02 성공 항목 | 초록 || 경고 `[!]`, 작업진행·재시도·옵션 확인 프롬프트, AI power limit·LACP 안내, LDAP 상이 값 | 노랑 || 타임스탬프, 메뉴 `ls`·yml 파일명, 02 단계 구분선 | 청록 || 등록 대상 총 대수, 작업 리스트, 02 확인표 헤더·요약 | 굵게 |
#### infra 이름 치환 (`infra_alias`)nodeinfo 결과에 등록되지 않은 infra 이름(예: `adjfg`)이 나오면 `infra_alias="adjfg:infra1 foo:infra2"` 처럼 `이름:바꿀infra` 쌍으로 지정합니다(공백/쉼표 구분). 3번째 필드를 msg 파싱 직후, 분할·yml 생성 전에 치환하므로 이후 모든 단계(yml 이름, dhcp/pxe)에 치환된 값이 쓰이며 `[infra 치환] adjfg -> infra1 : N건` 으로 알려 줍니다. 비워 두면 치환하지 않습니다.
#### dhcp / pxe 별 infra 치환 (`02` 상단 변수)dhcp 와 pxe 가 같은 infra 를 다른 이름으로 요구하면(예: dhcp 는 `asdf`, pxe 는 `asdfl`) 02 최상단의 두 변수에 `표시infra:넘길값` 을 적습니다(공백/쉼표 구분, 표시infra 는 확인표에 보이는 소문자 값이며 대소문자 무시).```bashdhcp_infra_alias="infra1:asdf"pxe_infra_alias="infra1:asdfl,infra2:xyz"```적용된 건은 `[infra 치환] pxe -infra infra1 -> asdfl` 로 알려 줍니다. 비워 두면 치환하지 않습니다. 01 의 `infra_alias`(nodeinfo 이름 → 등록 infra, yml 이름까지 반영)와 별개로, 이 값은 dhcp/pxe 호출 인자에만 적용됩니다.
#### 작업 대상 날짜 출력 (`day_print`), LDAP 점검- `day_print="user1,user2"` 처럼 user 를 적어 두면, 해당 user 로 실행할 때 작업 대상을 호스트명 다단 대신 `${user}.txt` 내용으로 출력합니다: `2026-10-02 tmp tmp infra hostname ip mac nic disk part 용량 os boot` (vendor·model 자리는 `tmp tmp`, 앞에 오늘 날짜). 끝에 `총 N대`. 비워 두거나 user 가 없으면 기존 출력입니다.- LDAP 점검은 별도 스크립트 대신 각 서버에서 `cat /etc/openldap/ldap.conf |grep -v '#' |grep -i uri |awk -F= '{print $2}' |awk -F',' '{print $1}'` 결과(와 bonding mode)를 모아 비교합니다. `ldap_check_script` 변수는 없어졌습니다.#### 파티션 표준 확인 (02, 마지막 단계)02 가 끝나면 `${user}.txt` 의 모든 호스트에서 `lsblk -nl -o NAME,TYPE,SIZE,MOUNTPOINT` 를 읽어(gossh) 표준 여부를 보고합니다(정보 출력만, 종료코드에는 영향 없음). 용량은 lsblk 표시값 기준입니다.| 항목 | 표준 ||---|---|| 구성 | LVM 이 아닌 물리 파티션 || OS 설치 디스크 | `/` 가 있는 디스크가 `sda` 또는 `nvme0n1` || `/boot` 또는 `/boot/efi` | 500M ~ 512M || `/` | 30G || `/var` | 20G || swap | 존재 || `/tmp` | 존재(나머지 용량) |이 외의 마운트·마운트 없는 파티션·LVM 이 있으면 `표준과 다른 파티션이 있습니다` 와 호스트별 사유를 빨강으로 출력하고, 응답 없는 호스트는 별도로 알립니다. OS 디스크 이외 디스크의 마운트는 판정 대상이 아닙니다(단 LVM 은 어디에 있든 표준 외).#### OS 번호 변환 (pxe)awxkit 은 `2024`·`2025` 처럼 숫자만 있는 값을 '선택지 번호'로 해석해 `OS 버전 번호가 범위를 벗어났습니다` 오류가 납니다. 02 는 conf(`awxkit/conf/${user}_setting.conf` 또는 `~/.awxkit/`)의 `s4_osver_choices` 에서 해당 값의 순번을 찾아 번호로 넘기고(예: 2025 → 2) `[os 번호 변환]` 으로 알려 줍니다. conf 나 선택지가 없으면 값 그대로 넘깁니다.
#### OS 버전 선택 (02, 옵션 확인표 출력 전)02 는 확인표보다 먼저 OS 버전을 묻습니다. 선택지는 `1) 2024  2) 2025  3) 2026  4) 2026-OPC_MDP  5) 2026-ECAD_TCAD` 입니다.```OS 버전 선택  1) 기본값 (2026-ECAD_TCAD) — 모든 yml 동일  2) yml 별로 직접 선택번호 : 2  1) 2024  2) 2025  3) 2026  4) 2026-OPC_MDP  5) 2026-ECAD_TCAD   (실제로는 한 줄씩 출력)a.yml에 사용할 버전 : 3        → pxe -os 2026b.yml에 사용할 버전 : 4        → pxe -os 2026-OPC_MDP```- 1번: 모든 yml 이 `2026-ECAD_TCAD`. 2번: yml 마다 번호(1~5)를 입력하며, 범위 밖이면 다시 묻습니다.- 01 이 넘기는 nodeinfo 의 os 값은 사용하지 않습니다. 선택값은 conf 의 `s4_osver_choices` 와 **대소문자까지 같아야** 하며(변환 없음), 확인표에서 `N` 으로 한 번 더 고칠 수 있습니다.
#### 전체 yml 갱신 · 최종 수량 · 등록 후 확인- **전체 yml(`_all`) 갱신 (02)**: 그룹 yml 이 **2개 이상**이고 **모두 성공**하면, 마지막에 전체 yml 로 `invsync`(AWX 인벤토리 소스 1단계)만 실행해 인벤토리 호스트를 전체 대상으로 갱신합니다(dhcp/pxe 는 실행하지 않음). yml 이 1개이거나 실패한 yml 이 있으면 생략합니다. 01 이 `--all=<yml>` 인자로 전달합니다.- **최종 수량 (02)**: 성공한 yml 기준으로 `구분 infra OS버전 : N대` 로 합산해 출력합니다. `On-premise`=`HPC`, `Cloud`=`SDS`, 그 외(`no`)는 그대로 표시하며 OS 버전은 위에서 고른 값입니다. 끝에 `합계` 를 출력합니다.- **등록 후 확인 (01 마지막)**: 작업 대상 서버를 붙여넣고(공백/쉼표/| 구분, 빈 줄로 종료, 바로 빈 줄이면 생략) 이번 등록 대상과 비교해 `붙여넣은 대상 서버 N대가 모두 존재함` 또는 `등록 대상에 없는 서버`(exit 1)를 보고합니다. 붙여넣지 않은 등록 대상은 경고로 알려 줍니다.
#### auto_setup 전달 (01 [14-1], 02 성공 직후)
02 가 성공하면 01 은 `${AUTO_SETUP_DIR:-/tmp/auto_setup}/queue/<epoch>_<user>_<pid>.job` 에 작업 파일(`user=`, `time=`, 호스트명 줄들)을 항상 남기고 `auto_setup 전달 : N대` 한 줄만 로그로 출력합니다(프롬프트·옵션 없음). 02 실패(재시도 N → exit 1) 시에는 전달하지 않으며, 디렉터리를 만들 수 없거나 쓰기에 실패하면 경고 한 줄만 출력하고 등록 진행·종료코드에는 영향을 주지 않습니다.

2차: `.job` 에는 호스트 줄 뒤에 그룹 줄이 추가됩니다(그룹 yml 이 2개 이상일 때만) — `yml=<파일명> infra=<> os=<> boot=<> splunk=<> hosts=<h1,h2,…>` 그룹별 한 줄과 전체 yml 의 `all=<파일명>`. 최상단 빈 변수 `auto_setup_host`(os8_mgmt 호스트명, os6_mgmt 등에서 채움)가 채워져 있으면 로컬 queue 대신 `gossh` 로 그 서버의 `${AUTO_SETUP_DIR:-/tmp/auto_setup}/queue/` 에 같은 내용을 원자 전송(원격에서 tmp 후 mv)하고 `auto_setup 전달 : N대 → <host>` 를 출력하며, 실패하면 경고 한 줄만 냅니다. 비어 있으면 기존과 동일합니다.

#### 옵션 확인표 및 수동 수정

```
번호 | yml | infra | os | boot | splunk | 호스트 수
1 | INFRA-A_inventory-1234_5ea.yml | INFRA-A | 2026-ECAD_TCAD | UEFI | On-premise | 5
2 | INFRA-B_inventory-1234_3ea.yml | INFRA-B | 2026-OPC_MDP | legacy | Cloud | 3
옵션이 맞습니까 (Y|N) : n
```

**N 선택 시 값 재입력** (Enter = 현재값 유지):

```
[1/2] INFRA-A_inventory-1234_5ea.yml
  infra [INFRA-A] : INFRA-X
  os [2026-ECAD_TCAD] :     (Enter = 현재값 유지)
  boot [UEFI] :
  splunk [On-premise] :
```

### 분할 키 (infra + nic + disk + 용량 + os + boot + splunk)

yml 파일은 다음 조합으로 분할됩니다. 같은 조합의 호스트는 같은 파일에 묶입니다.

| 구성 | 예 |
|---|---|
| infra | INFRA-A, INFRA-B |
| nic | eth0, eth1, bond0 |
| disk | sda, nvme0n1 |
| 용량 | 1200, 7600 (nodeinfo 의 1.1T/7T 는 sed 로 치환된 뒤의 값) |
| os | 분할 키에는 포함되나, 02 의 OS 버전은 별도 선택 |
| boot | UEFI, legacy, BIOS |
| splunk | On-premise, Cloud, no |

part(9번째 필드)는 분할 키에서 제외됩니다. boot 의 `레거시` 는 `legacy` 로 바꿔 전달합니다. 분할 파일 외에 모든 줄을 담은 전체(`_all`) 파일도 하나 만들어 yml 로 등록·git 업로드하지만, 02 의 등록 대상(DHCP/PXE)에서는 제외됩니다.

### splunk 값 자동 결정 규칙

호스트명 기준:

1. **ev 포함** → `no` (가장 우선)
2. **s 로 시작** → `Cloud`
3. **기타** → `On-premise`

예: `spice01ev02` → `no`, `spice01` → `Cloud`, `node01` → `On-premise`

### 01 최상단 변수 검증 및 자동 종료

작업진행(Y) 직후, 다음 변수들이 비어 있으면 오류 메시지 후 종료합니다:

- `inventory_delete_host`
- `repohost`
- `svr_dir`
- `lacp_comment`

## 문서별 설명

| 파일 | 설명 |
|---|---|
| `01.AWX_nodeinfo_V2.sh` | 메인 스크립트 — 노드정보 수집, 가공, yml 생성, git 업로드 |
| `02.source_dhcp_pxe.sh` | 보조 스크립트 — yml별 인벤토리/DHCP/PXE 등록 (01 내에서 자동 호출) |
| `test/run_tests.sh` | 테스트 하네스 — 계획서 §9 검증 기준. 01/02 를 실제로 실행하는 목업 E2E(스텁 awxkit/ssh/scp/gossh), 랩(bash 4.4)에서 실행 |
| `계획서.md` | 설계 근거와 결정 사항(§8) |
| `.github/workflows/ci.yml` | 단독 브랜치용 CI (bash -n / shellcheck / 테스트) |
| `README.md` | 이 파일 — 설치, 사용, 옵션 가이드 |
| `CHANGELOG.md` | 변경 이력 |
| `ARCHITECTURE.md` | 함수 및 단계 매핑, 수정 가이드 |
| `WORKFLOW.md` | 실행 흐름도 (텍스트 mermaid) |
| `workflow.svg` | 실행 흐름도 (SVG 이미지) |
| `PR_CHECKLIST.md` | 풀리퀘스트 검증 항목 |
| `사용법.txt` | 명령어 중심 빠른 참고 |

## 전역 명령어로 사용하기 (선택)

01 은 `awxkit/`, `LOG/`, `tmp/`, `${user}.txt` 를 **현재 디렉터리 기준**으로 사용하므로 심볼릭 링크로 PATH 에 넣으면 동작하지 않습니다. 배치 디렉터리로 이동해 실행하는 alias 를 쓰십시오.

```bash
# ~/.bashrc
alias awx-nodeinfo='cd /path/to/awx_script && bash 01.AWX_nodeinfo_V2.sh'
```

## 주의사항 (Disclaimer)

본 로그 분석 관련 스크립트 및 툴은 **100% 신뢰하기보다는 참고용(보조 도구)으로 사용하는 것을 권장합니다.**

**설정 변경 후 반드시 다음을 확인하세요:**

1. 설정 변경 스크립트이므로, 설정 변경 후 랜덤한 서버 몇 대를 선택하여 **실제로 변경되었는지 확인**하세요.
2. `inventory_delete` 는 원격 호스트에서 호스트 레코드를 삭제하는 기능입니다. **'작업진행 Y' 이후에만 실행되며, 삭제는 되돌릴 수 없으므로 신중하게 진행하세요.**
3. 테스트 값은 스크립트 본체에 들어 있지 않으며, `test/` 하네스가 스크래치 복사본에만 sed 로 주입합니다.

### 알려진 한계 및 위험

| 항목 | 설명 | 대응 |
|---|---|---|
| custom_inventory.sh 덮어쓰기 | 같은 초에 같은 infra 그룹이 생성되면 덮어쓸 수 있음 | `sleep 1` 로 완화, 전후 비교로 검출 |
| infra/os 문자열 불일치 | AWX survey 선택지와 다르면 dhcp/pxe 오류 | 02 확인표에서 수동 수정 |
| gossh 색상/진행률 꺼짐 | tee 로깅으로 인해 stdout이 TTY가 아님 | 결과 영향 없음 |
| sed 7T 부작용 | `sed 's/7T/7600/g'` 는 17T도 변경 | 원문 유지 결정 |

## 버전 및 태그

```bash
# 버전 태그 지정 예
git tag -a awx-script-v0.1.0 -m "Initial 01/02 scripts and documentation (CHANGELOG 2026-10-02)"
git push origin awx-script-v0.1.0

# 향후 버전
git tag -a awx-script-v0.2.0 -m "..."
```

태그 규칙: `awx-script-v<major>.<minor>.<patch>`
