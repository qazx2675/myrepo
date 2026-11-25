# HPE 서버 관리망(iLO) IP 및 VLAN 일괄 변경 도구

HPE ProLiant 서버의 원격 관리 칩셋(iLO 4 / iLO 5 / iLO 6)에 설정된 관리망 IP, 서브넷 마스크, 게이트웨이 및 VLAN을 `ilorest` Redfish CLI를 이용해 일괄 조회 및 변경하는 순수 Bash 기반 운영 스크립트입니다.

> [!WARNING]
> **사용 전 필수 확인 사항 (권장 지침)**
> 본 설정 변경 스크립트 및 툴은 100% 신뢰하기보다는 참고용(보조 도구)으로 사용하는 것을 권장합니다. 실제 설정 변경 적용 후에는 반드시 대상 장비 중 일부를 무작위로 표본 확인(웹 콘솔 접속 또는 호스트 OS 내 ipmitool/ilorest 조회)하여 변경 사항이 정상 반영되었는지 교차 검증하시기 바랍니다.

---

## 1. 빌드 및 설치 방법

본 프로젝트는 컴파일이나 외부 패키지(`jq`, `python3`, `pip` 등) 설치가 필요 없는 순수 Bash 스크립트(RHEL 6 기본 Bash 4.1 이상 호환)로 구성되어 있습니다. 폐쇄망 환경의 관리서버에는 HPE 공식 `ilorest` 패키지만 설치되어 있으면 즉시 실행할 수 있습니다.

### 1.1 폐쇄망 반입 절차

1. **인터넷 연결 환경(또는 사내 미러 저장소)에서 `ilo` 브랜치만 내려받기**
   ```bash
   git clone -b ilo --single-branch https://github.com/qazx2675/myrepo.git ilo-ip-changer
   cd ilo-ip-changer
   ```
   - 폐쇄망 내부 Git 서버가 없는 경우, 외부 망에서 위 명령으로 내려받은 폴더를 압축(`tar czf ilo-ip-changer.tar.gz ilo-ip-changer`)하여 망연계/반입 절차로 관리서버에 복사한 뒤 압축을 해제합니다.
   ```bash
   tar xzf ilo-ip-changer.tar.gz
   cd ilo-ip-changer
   ```

2. **실행 권한 부여 및 확인**
   ```bash
   chmod +x change_ilo_ip.sh
   ```

3. **`ilorest` CLI 설치 및 정상 동작 확인**
   ```bash
   which ilorest
   /usr/sbin/ilorest --nologo --version
   ```
   - `ilorest`가 기본 경로(`/usr/sbin/ilorest` 또는 `PATH` 내 경로)에 설치되어 있지 않으면 `--ilorest-bin` 옵션 또는 `ILOREST_BIN` 환경 변수로 경로를 지정할 수 있습니다.

### 1.2 RHEL 6 `prelink` 바이너리 손상 오류 예방 및 조치

RHEL 6 환경에서는 OS의 주기적인 `prelink` 작업이 `/usr/sbin/ilorest` 바이너리를 변조하여 실행 시 **`Cannot open self /usr/sbin/ilorest`** 오류가 발생할 수 있습니다. `change_ilo_ip.sh`는 실행 초기 단계에서 이를 자동 감지하여 안내하며, 아래 절차로 예방 또는 복구할 수 있습니다.

```bash
# 1) /etc/prelink.conf 파일에 ilorest 예외 경로 추가 (재발 방지)
echo "-b /usr/sbin/ilorest" >> /etc/prelink.conf

# 2) 이미 손상된 바이너리의 prelink 해제
prelink -ua

# 3) ilorest 정상 실행 재확인 (여전히 오류 시 ilorest RPM 재설치 필요)
/usr/sbin/ilorest --nologo --version
```

---

## 2. 사용 방법

### 2.1 작업 전 필수 준비 (네트워크/스위치)

- 대상 서버의 iLO 칩을 재시작(`iloreset`)하는 순간 기존 관리망 IP 세션은 즉시 종료되며, 약 30~60초 후 신규 IP와 신규 VLAN 태그로 재기동합니다.
- 따라서 **작업 전 관리망 스위치 포트에 변경될 VLAN 허용(Trunk 허용 또는 Access VLAN 전환) 및 라우팅 개통**이 반드시 준비되어 있어야 합니다.

### 2.2 대상 목록 파일(`list.txt`) 작성

샘플 파일(`list.txt.example`)을 복사하여 대상 서버 목록을 작성합니다. 각 줄은 공백(스페이스 또는 탭)으로 구분된 3개의 컬럼으로 구성됩니다.

```bash
cp list.txt.example list.txt
vim list.txt
```

```text
# [현재 관리망 IP]   [변경될 관리망 IP]   [변경될 VLAN]
# VLAN 해제(비활성화)가 필요하면 0 또는 off 입력
10.10.10.101        10.20.30.101        30
10.10.10.102        10.20.30.102        30
10.10.10.103        10.20.40.15         0
```

- 빈 줄과 `#`으로 시작하는 주석 줄은 자동으로 무시됩니다.
- VLAN 컬럼은 `1`~`4094` 사이의 정수(VLAN 활성화) 또는 `0` / `off` / `disable` / `none`(VLAN 비활성화)을 입력할 수 있습니다.
- 스크립트는 변경 시작 전 전체 줄의 IPv4 형식 및 VLAN 유효성을 일괄 검증하므로, 오타가 있으면 즉시 중단됩니다.

### 2.3 사전 점검 실행 (`--check`)

실제 설정을 변경하기 전에 `--check` 옵션으로 모든 대상 장비의 로그인 가능 여부, iLO 세대(iLO 4/5/6), 활성 관리 포트(Dedicated/Shared), 현재 IP·서브넷·게이트웨이·VLAN 상태 및 변경 예정값을 조회합니다.

```bash
bash change_ilo_ip.sh --check
```

### 2.4 대화형 일괄 변경 실행

옵션 없이 실행하면 필요한 항목을 대화형으로 순서대로 입력받습니다.

```bash
bash change_ilo_ip.sh
```

대화형 입력 흐름 예시:
1. `관리망(iLO) 계정 ID [기본값: Administrator]: ` → 엔터 시 `Administrator` 적용
2. `관리망(iLO) 비밀번호: ` → 화면에 표시되지 않는 숨김 입력(`read -r -s`)
3. `신규 서브넷 마스크 [Enter=255.255.255.0]: ` → 엔터 시 `255.255.255.0` 적용
4. `신규 게이트웨이 [Enter=변경될 IP 대역 .1 자동 / 직접 입력 시 고정 IP]: ` → 엔터 시 각 대상 장비의 변경될 IP 기준 `.1` 자동 계산 (예: `10.20.30.101` → `10.20.30.1`)
5. 대상 대수·목록 파일·서브넷·게이트웨이 규칙 요약 출력 후:
   `작업 모드를 선택하세요 [y: 실제 변경 진행 / c: 사전 점검(조회만) / n: 취소]: `
   - `y`: 순차 변경 진행 (변경 전 원본 JSON을 `logs/backup_<타임스탬프>/<현재IP>.json`에 백업 후 패치 및 `iloreset`)
   - `c`: 사전 점검 모드로 전환 (변경 없이 조회만 수행)
   - `n` (또는 기타 입력): 변경 없이 즉시 종료

### 2.5 실행 결과 및 백업 확인

- 전체 작업 결과 요약표는 화면 출력과 동시에 `logs/result_<YYYYMMDD_HHMMSS>.log`에 저장됩니다.
- 개별 장비에서 로그인 실패, 통신 불가, 패치 실패가 발생하더라도 전체 배치를 중단하지 않고 실패 사유를 기록한 뒤 다음 장비를 계속 처리합니다. (패치 실패 시 해당 장비에는 `iloreset`을 실행하지 않습니다.)

---

## 3. 옵션별 상세 설명

### 3.1 CLI 옵션

| 옵션 | 약어 | 기본값 | 설명 |
| :--- | :--- | :--- | :--- |
| `--file <파일>` | `-f` | `list.txt` | 대상 장비 목록 파일 경로를 지정합니다. |
| `--netmask <넷마스크>` | `-m` | `255.255.255.0` | 적용할 신규 서브넷 마스크를 지정합니다. 지정 시 대화형 서브넷 질문을 건너뜁니다. |
| `--gw <게이트웨이IP>` | `-g` | 변경 IP 기준 `.1` 자동 계산 | 고정 게이트웨이 IP를 지정합니다. 지정 시 대화형 게이트웨이 질문을 건너뜁니다. |
| `--check` | `-c` | 비활성화 | 설정 변경 없이 현재 iLO 상태 조회 및 변경 예정값 점검(Dry-run)만 수행합니다. |
| `--yes` | `-y` | 비활성화 | 서브넷 마스크·게이트웨이·작업 모드 확인 프롬프트를 건너뛰고 기본값 또는 CLI 지정값으로 즉시 실행합니다. (계정/비밀번호 환경변수가 없으면 입력 프롬프트는 표시됩니다.) |
| `--ilorest-bin <경로>` | - | 자동 탐색 | 사용할 `ilorest` 바이너리 경로를 직접 지정합니다. |
| `--help` | `-h` | - | 한국어 도움말 및 사용 예시를 출력합니다. |

### 3.2 환경 변수

| 환경 변수 | 우선순위 / 기본값 | 설명 |
| :--- | :--- | :--- |
| `ILO_USER` | 미설정 시 대화형 입력 (기본 `Administrator`) | 대상 iLO 로그인 계정 ID를 비대화형으로 전달합니다. |
| `ILO_PASS` | 미설정 시 대화형 숨김 입력 | 대상 iLO 로그인 비밀번호를 비대화형으로 전달합니다. |
| `ILOREST_BIN` | `--ilorest-bin` > `ILOREST_BIN` > `command -v ilorest` > `/usr/sbin/ilorest` | `ilorest` 실행 파일 경로를 지정합니다. |

---

## 4. 문서별 설명

| 파일 / 디렉토리 | 구분 | 설명 |
| :--- | :--- | :--- |
| `change_ilo_ip.sh` | 실행 스크립트 | RHEL 6(Bash 4.1) 호환 메인 iLO IP/VLAN 일괄 조회·변경 스크립트 |
| `list.txt.example` | 샘플 파일 | `list.txt` 작성을 위한 3컬럼 입력 양식 예시 파일 |
| `.gitignore` | Git 설정 | 실제 작업 목록(`list.txt`), 백업/결과 로그(`logs/`), 임시 파일(`*.tmp`, `*.bak`) 추적 제외 |
| `README.md` | 운영 매뉴얼 | 설치, 사용법, 옵션, 문서 안내 및 호스트 OS 비상 복구 가이드 |
| `ARCHITECTURE.md` | 구조 문서 | 디렉토리 구성과 `rawget`/`rawpatch`/`iloreset` 내부 처리 흐름 설명 |
| `CHANGELOG.md` | 변경 이력 | 버전별 기능 추가 및 수정 내역 (`v0.1.0` 최초 릴리즈) |
| `PR_CHECKLIST.md` | 검증 체크리스트 | 문법, Mock E2E 테스트, 실행 권한, 문서 정합성 점검 항목 |
| `사용법.txt` | 명령어 모음 | 관리서버에서 복사-붙여넣기로 바로 실행할 수 있는 단계별 명령어 모음 |
| `계획서.md` | 설계 계획서 | 프로젝트 배경, 설계 기준 및 검증 계획 요약 문서 |
| `tests/mock_ilorest` | 테스트 Mock | iLO 4/5/6 및 Dedicated/Shared 포트, 오류 시나리오를 모사하는 테스트용 `ilorest` |
| `tests/run_tests.sh` | 자동화 테스트 | `mock_ilorest` 기반 E2E 검증 스크립트 |
| `.github/workflows/ci.yml` | CI 워크플로우 | Push/PR 시 `bash -n` 및 `tests/run_tests.sh` 자동 실행 |
| `logs/` | 런타임 산출물 | 실행 시 생성되는 원본 백업 JSON(`backup_*`) 및 결과 로그(`result_*.log`) 저장 폴더 (Git 제외) |

---

## 5. 부록: 통신 단절 영향도 및 호스트 OS 상에서의 확인·비상 복구 방법

### 5.1 통신 단절 영향도

1. **호스트 OS 및 업무망(서비스 네트워크) 무중단**
   - `ilorest iloreset` 명령은 서버 메인보드의 **iLO 관리 칩셋(BMC)만 단독 재부팅**합니다.
   - 서버에서 운영 중인 Linux/Windows 호스트 OS, 실행 중인 서비스 프로세스, 업무용 NIC(서비스 네트워크)는 **절대 재부팅되거나 통신이 끊기지 않습니다.**
2. **관리망(iLO) 전환 특성**
   - `iloreset` 실행 직후 **기존 관리망 IP를 통한 접속(웹 콘솔, SSH, Redfish)은 즉시 끊어집니다.**
   - 약 30~60초 후 iLO가 재부팅을 완료하면 **신규 관리망 IP + 신규 VLAN**으로 응답을 시작합니다. 스위치 포트에 신규 VLAN이 허용되어 있지 않으면 관리망 원격 접속만 불통 상태가 되며, 아래 방법으로 호스트 OS 내부에서 즉시 확인 및 원복할 수 있습니다.

### 5.2 호스트 OS(`root`)에서의 상태 확인 방법

관리망 통신이 안 되더라도 업무망 SSH 등으로 대상 서버 호스트 OS에 접속할 수 있다면, 내부 시스템 버스(CHIF/KCS)를 통해 iLO 상태를 직접 조회할 수 있습니다.

```bash
# 1) ipmitool을 이용한 현재 iLO LAN 설정 확인
ipmitool lan print 1

# 2) 호스트 OS에 설치된 로컬 ilorest로 네트워크 인터페이스 조회 (로그인 IP 불필요)
ilorest --nologo get --select EthernetInterface.

# 3) HPE hponcfg 유틸리티로 현재 iLO 전체 네트워크 XML 추출 확인
hponcfg -w /tmp/ilo.xml
cat /tmp/ilo.xml | grep -E "IP_ADDRESS|SUBNET_MASK|GATEWAY_IP_ADDRESS|VLAN"
```

### 5.3 호스트 OS(`root`)에서의 비상 원복 방법

IP, 서브넷, 게이트웨이 또는 VLAN ID를 잘못 입력하여 관리망 접속이 두절된 경우, 호스트 OS 내부에서 아래 도구 중 사용 가능한 것으로 즉시 복구할 수 있습니다.

#### 방법 A: `ipmitool`을 이용한 즉시 원복 (가장 범용적)

```bash
# 정적 IP 모드 설정 및 IP / 서브넷 / 게이트웨이 / VLAN 복구 예시
ipmitool lan set 1 ipsrc static
ipmitool lan set 1 ipaddr 10.10.10.101
ipmitool lan set 1 netmask 255.255.255.0
ipmitool lan set 1 defgw ipaddr 10.10.10.1

# VLAN 복구 (예: VLAN 30 적용 시 / VLAN 해제 시 off 입력)
ipmitool lan set 1 vlan id 30
# ipmitool lan set 1 vlan id off

# 필요 시 BMC(iLO) 콜드 리셋
ipmitool mc reset cold
```

#### 방법 B: 호스트 OS 로컬 `ilorest`를 이용한 원복

```bash
# 로컬 로그인(원격 IP 지정 없이 실행) 후 백업 JSON 또는 수정 JSON 적용
ilorest --nologo login
ilorest --nologo rawget /redfish/v1/Managers/1/EthernetInterfaces/1/ > /tmp/ilo_cur.json
# /tmp/ilo_cur.json에서 Address, SubnetMask, Gateway, VLAN 수정 후 패치
ilorest --nologo rawpatch /tmp/ilo_restore.json
ilorest --nologo iloreset
```

#### 방법 C: `hponcfg`를 이용한 XML 기반 원복

```bash
# /tmp/restore_ilo.xml 작성 예시:
# <RIBCL VERSION="2.0">
#   <LOGIN USER_LOGIN="Administrator" PASSWORD="dummy">
#     <RIB_INFO MODE="write">
#       <MOD_NETWORK_SETTINGS>
#         <IP_ADDRESS VALUE="10.10.10.101"/>
#         <SUBNET_MASK VALUE="255.255.255.0"/>
#         <GATEWAY_IP_ADDRESS VALUE="10.10.10.1"/>
#         <VLAN_ENABLED VALUE="Y"/>
#         <VLAN_ID VALUE="30"/>
#       </MOD_NETWORK_SETTINGS>
#     </RIB_INFO>
#   </LOGIN>
# </RIBCL>

hponcfg -f /tmp/restore_ilo.xml
```
