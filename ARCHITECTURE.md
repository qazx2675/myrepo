# 아키텍처 및 내부 동작 구조 (`ARCHITECTURE.md`)

본 문서는 `hpe-ilo-ipchanger` 프로젝트의 파일 구조와 `change_ilo_ip.sh`의 내부 처리 흐름을 설명합니다.

---

## 1. 디렉토리 및 파일 역할

| 경로 | 역할 | 비고 |
| :--- | :--- | :--- |
| `change_ilo_ip.sh` | 메인 실행 스크립트 | Bash 4.1+ 호환, CLI 파싱·사전 검증·대화형 입력·순차 Redfish 패치·요약 로그 생성 |
| `list.txt.example` | 입력 목록 샘플 | `[현재 IP] [변경 IP] [변경 VLAN]` 3컬럼 표준 양식 제공 |
| `.gitignore` | Git 제외 규칙 | `list.txt`, `logs/`, `*.tmp`, `*.bak` 제외 |
| `README.md` | 운영 매뉴얼 | 설치, 사용법, 옵션, 문서 안내, 호스트 OS 확인·비상 복구 가이드 |
| `ARCHITECTURE.md` | 아키텍처 문서 | 구성 요소 역할 및 내부 처리 흐름 |
| `CHANGELOG.md` | 변경 이력 | 버전별 릴리즈 내역 |
| `PR_CHECKLIST.md` | 검증 체크리스트 | PR/배포 전 필수 확인 항목 |
| `사용법.txt` | 명령어 안내서 | 관리서버 복사용 1줄 주석 + 명령어 모음 |
| `계획서.md` | 프로젝트 계획서 | 요구사항·설계 근거·검증 계획 |
| `tests/mock_ilorest` | 상태 기반 Mock CLI | iLO 4/5/6, Dedicated/Shared 포트, 인증/통신/패치/prelink 오류 시뮬레이션 |
| `tests/run_tests.sh` | E2E 테스트 스위트 | `bash -n` 및 7가지 시나리오 자동 검증 |
| `.github/workflows/ci.yml` | GitHub Actions CI | Push/PR(`ilo` 브랜치 포함) 시 자동 문법 및 E2E 테스트 실행 |
| `logs/backup_<타임스탬프>/` | 원본 JSON 백업 | 실제 변경 전 조회한 인터페이스 원본 JSON 저장 (`chmod 600`) |
| `logs/result_<타임스탬프>.log` | 실행 결과 로그 | 전체 요약 표 및 성공/실패 집계 저장 |

---

## 2. 내부 처리 흐름

```text
[1. CLI 인자 파싱 및 Preflight 검증]
   │  - ilorest 경로 탐색 (--ilorest-bin -> ILOREST_BIN -> PATH -> /usr/sbin/ilorest)
   │  - ilorest --nologo --version 실행 및 RHEL 6 prelink 손상("Cannot open self") 감지
   │  - list.txt 존재 여부 및 전수 구문 검증 (IPv4 4옥텟 범위, VLAN 1~4094 또는 0/off/disable/none)
   ▼
[2. 대화형 입력 수집] (CLI 옵션/-y/환경변수 미지정 항목만 질의)
   │  - ILO_USER (기본: Administrator)
   │  - ILO_PASS (숨김 입력 read -r -s)
   │  - SubnetMask (기본: 255.255.255.0)
   │  - Gateway (기본: 변경될 IP의 <A.B.C>.1 자동 계산)
   │  - 실행 모드 선택 (y: 변경 / c: 사전 점검 / n: 취소)
   ▼
[3. 대상 서버별 순차 실행 루프] (개별 장비 실패 시 오류 기록 후 다음 장비 계속 진행)
   ├─ 3-1. 세션 초기화 및 로그인
   │       ilorest --nologo logout
   │       ilorest --nologo login <현재IP> -u <ILO_USER> -p <ILO_PASS>
   ├─ 3-2. iLO 세대 및 활성 관리 포트 자동 탐색
   │       rawget /redfish/v1/Managers/1                -> FirmwareVersion (iLO 4/5/6) 판별
   │       rawget /redfish/v1/Managers/1/EthernetInterfaces -> 모든 포트 @odata.id 수집
   │       각 포트 URI rawget -> 현재 IP 경계 일치 매칭(예: 10.0.0.1 vs 10.0.0.10 오탐 방지)
   │                          -> Dedicated(/1/) vs Shared(/2/) 및 OEM 키("Hpe" vs "Hp") 식별
   ├─ 3-3. 모드 분기
   │   ├─ [--check 모드] 현재 설정 및 변경 예정값 출력 -> logout -> "점검완료" 기록
   │   └─ [실제 변경 모드]
   │        1) logs/backup_<timestamp>/<현재IP>.json 백업 저장 (chmod 600)
   │        2) 임시 패치 JSON 생성 (mktemp, chmod 600, EXIT/INT/TERM trap으로 자동 삭제)
   │           - DHCPv4.DHCPEnabled: false
   │           - Oem.<Hpe|Hp>.DHCPv4.Enabled: false (원본 JSON에 해당 OEM DHCPv4 존재 시)
   │           - IPv4StaticAddresses: [{Address, SubnetMask, Gateway}]
   │           - VLAN: {VLANEnable: true, VLANId: N} 또는 {VLANEnable: false}
   │        3) rawpatch <임시JSON> 실행
   │           - 실패 시: iloreset 실행 금지 -> 실패 기록 후 다음 장비로 이동
   │        4) iloreset 실행 -> iLO 재부팅 트리거 -> 성공 기록 및 logout
   ▼
[4. 최종 요약 리포트 출력 및 종료 코드 반환]
   - 화면 및 logs/result_<timestamp>.log에 표 형식 결과·전체/성공/실패 건수 출력
   - 실패 건수가 1건 이상이면 exit 1, 모두 성공(또는 점검 완료)이면 exit 0
```

---

## 3. 호환성 및 안전 설계 포인트

1. **RHEL 6 Bash 4.1 순수 호환**: `jq`, `python3`, Bash 4.3+ 전용 문법(`local -n` 등)을 배제하고 POSIX 기본 유틸리티(`awk`, `sed`, `grep`, `mktemp`, `date`, `chmod`, `tr`)만 사용합니다.
2. **왜 `set` 대신 `rawget` + `rawpatch`를 사용하는가?**: iLO 4/5/6의 `IPv4StaticAddresses`는 배열 구조여서 구버전 `ilorest set`에서 파싱 오류가 발생할 수 있으며, 관리 포트가 Dedicated(`/1/`)인지 Shared(`/2/`)인지 장비마다 다를 수 있습니다. 현재 IP가 실제로 매핑된 `@odata.id`를 직접 탐색하고 OEM 키(`Hp` vs `Hpe`)까지 판별한 뒤 패치함으로써 세대별 차이를 흡수합니다.
3. **임시 파일 및 크리덴셜 보호**: 패치용 임시 JSON은 `chmod 600`으로 생성하며 `trap`으로 비정상 종료(`Ctrl+C` 등) 시에도 즉시 삭제하고 `ilorest logout`을 수행합니다.
