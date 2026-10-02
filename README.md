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

### 최상단 변수 6개 채우기

`01.AWX_nodeinfo_V2.sh` 최상단의 빈 변수들을 현장 값으로 채웁니다.

```bash
#!/bin/bash
# AWX nodeinfo V2 - HPC 서버 등록 전처리 및 인벤토리/DHCP/PXE 등록

repohost=""                  # ← custom_inventory.sh 가 있는 서버. 분할/전체 입력 파일을 scp 로 /root/Inventory/ 에 보내고 ssh 로 실행
svr_dir=""                   # ← custom_inventory.sh 가 yml 을 쓰는 경로(이 서버에서 보이는 공유 경로)
ai_server_list=""            # ← AI 서버 호스트명 "host01|host02" (| 구분, 정확 일치). 비워 두면 안내를 건너뜀
ldap_check_script=""         # ← 대상 호스트에서 실행할 LDAP 점검 스크립트 경로
lacp_comment=""              # ← LACP(802.3ad) 호스트가 있을 때 출력할 안내 문구
inventory_delete_host=""     # ← 작업진행 Y 후 `ssh <host> "bash /root/server/delhost_${user}"` 를 실행할 서버
```

`ai_server_list` 외 5개는 필수이며, 작업진행 Y 직후(원격 삭제 전)에 비어 있으면 `[X] <변수> 가 비어 있습니다` 로 종료합니다. 값은 현장 값으로 직접 채우십시오(저장소에는 모두 빈 값으로 둡니다).

### user() 함수에 현장 코드 붙이기

`user()` 함수는 빈 함수로 두고, 현장 환경에 맞는 코드를 직접 삽입합니다. 이 함수는 변수 `$user` 를 설정해야 합니다.

```bash
user() {
	# 현장 코드로 교체: 이 함수가 $user 를 설정한다
	:
}
```

현장 코드는 어떤 방식이든 마지막에 `user` 변수만 채우면 됩니다. 호출 후 `$user` 가 비어 있으면 `[X] user 값이 없습니다 (user 함수 확인)` 로 종료하며, 별도 입력 프롬프트는 없습니다.

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
| os | 운영체제 | `RHEL8`, `RHEL9` |
| boot | 부트 방식 | `UEFI`, `legacy`, `BIOS` |
| splunk | Splunk 설치 여부 선택값 | `On-premise`, `Cloud`, `no` |


- **infra 는 소문자로 변환해 전달**합니다(`INFRA-A` → `infra-a`). awxkit 은 conf 의 `*_choices` 와 **대소문자까지 정확히 일치**해야 하고(번호 또는 값 그대로 사용 가능), os/boot/splunk 는 변환하지 않습니다. 확인표에서 `N` 으로 직접 입력한 값도 그대로 전달됩니다.
- **실행 순서**: yml 마다 `invsync` 를 먼저 실행한 뒤 `dhcp` 와 `pxe` 를 **동시에** 실행하고, 둘 다 끝나야 다음 yml 로 넘어갑니다. 동시 실행이라 프롬프트를 받을 수 없어 stdin 을 닫으며(옵션 값이 비면 대화형 대신 오류), 출력은 둘 다 끝난 뒤 `--- dhcp ---`, `--- pxe ---` 순으로 보여 줍니다. 한쪽만 실패해도 다른 쪽은 끝까지 실행됩니다.
#### 옵션 확인표 및 수동 수정

```
번호 | yml | infra | os | boot | splunk | 호스트 수
1 | INFRA-A_inventory-1234_5ea.yml | INFRA-A | RHEL8 | UEFI | On-premise | 5
2 | INFRA-B_inventory-1234_3ea.yml | INFRA-B | RHEL9 | legacy | Cloud | 3
옵션이 맞습니까 (Y|N) : n
```

**N 선택 시 값 재입력** (Enter = 현재값 유지):

```
[1/2] INFRA-A_inventory-1234_5ea.yml
  infra [INFRA-A] : INFRA-X
  os [RHEL8] :              (Enter = RHEL8 유지)
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
| os | RHEL8, RHEL9 |
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
- `ldap_check_script`
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
