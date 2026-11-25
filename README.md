# HPE iLO 관리망 다중 서버 RAID 구성 도구 (`raid0.sh`)

HPE ProLiant 서버(**Gen10 / Gen10 Plus / Gen11**)의 iLO 관리망(Redfish API)에 `pdsh` + `ilorest`로 병렬 접속하여, OS 진입 전에도 여러 대의 서버에 RAID 볼륨을 안전하게 일괄 구성할 수 있는 운영 도구입니다.

---

## 1. 사전 요구사항 (RHEL 6 관리 서버 기준)

폐쇄망 RHEL 6 관리 서버에서 실행하기 위해 아래 패키지가 설치되어 있어야 합니다.

| 구성 요소 | 요구 버전 / 패키지 | 확인 명령 |
|---|---|---|
| Bash | RHEL 6 기본 `bash 4.1` 이상 | `bash --version` |
| HPE iLOrest | `ilorest` CLI (v3.x / v4.x 권장) | `ilorest --version` |
| pdsh + exec 모듈 | `pdsh`, `pdsh-rcmd-exec` | `pdsh -L \| grep -i exec` |
| Python | RHEL 6 기본 `/usr/bin/python` (2.6.6) 또는 `python3` | `python --version` |
| 네트워크 | 관리 서버 -> 대상 서버 iLO `TCP 443 (HTTPS)` 통신 허용 | `nc -zv <iLO_IP> 443` |

> **참고**: `pdsh -L` 출력에 `exec` 모듈이 보이지 않으면 `pdsh-rcmd-exec` RPM 패키지를 추가로 설치해야 합니다.

---

## 2. 디렉토리 구성

```text
ilo/
├── raid0.sh               # 메인 대화형 실행 스크립트
├── worklist.txt.example   # 대상 iLO IP 목록 예시 파일
├── README.md              # 운영 매뉴얼 (본 문서)
└── lib/
    ├── ilo_worker.sh      # pdsh가 서버별로 호출하는 iLOrest 워커 (IP별 캐시 격리)
    └── raid_parser.py     # Redfish JSON 파싱 및 페이로드 생성기 (Python 2.6.6+ 호환)
```

---

## 3. 대상 서버 목록(`worklist.txt`) 준비

예시 파일을 복사한 뒤 작업 대상 서버의 iLO 관리 IP를 한 줄에 하나씩 입력합니다.

```bash
cp worklist.txt.example worklist.txt
vi worklist.txt
```

- `#`으로 시작하는 주석, 빈 줄, 공백 및 중복 IP는 자동 정리됩니다.
- 서로 다른 세대나 디스크 구성이 섞여 있어도 조회 및 경고를 지원하지만, 운영 안정성을 위해 **동일 기종/동일 디스크 구성끼리 묶어 실행**하는 것을 권장합니다.

---

## 4. 실행 방법 및 CLI 옵션

```bash
bash raid0.sh [옵션]
```

> **권한 안내**: 내부 워커(`lib/ilo_worker.sh`)도 `bash`로 직접 실행되므로, 파일을 복사하면서 실행 권한(`+x`)이 빠진 상태에서도 `bash raid0.sh`로 바로 실행할 수 있습니다. (`chmod +x raid0.sh lib/ilo_worker.sh`는 선택 사항입니다.)

| 옵션 | 설명 | 기본값 |
|---|---|---|
| `-w, --worklist FILE` | 대상 iLO IP 목록 파일 경로 | `./worklist.txt` |
| `-f, --fanout N` | `pdsh` 동시 접속 서버 수 | `10` |
| `--dry-run` | 실제 변경 없이 조회 및 생성 예정 Redfish 페이로드만 미리보기 | 비활성 |
| `--force-delete` | 선택한 디스크가 기존 RAID 볼륨에 할당되어 있을 때 기존 볼륨 삭제 후 재생성 | 비활성 (안전 보호) |
| `--name PREFIX` | 생성할 볼륨 이름 접두사 지정 (예: `DATA`) | 컨트롤러 기본값 |
| `--stripe BYTES` | 스트라이프 크기(바이트 단위, 예: `262144`) | 컨트롤러 기본값 |
| `-h, --help` | 도움말 출력 | - |

### 권장 사전 검증 실행 (`--dry-run`)

실제 반영 전 `--dry-run`으로 서버 목록, 디스크 매핑, 충돌 여부 및 Redfish 요청 내용을 먼저 확인하세요.

```bash
bash raid0.sh -w ./worklist.txt --dry-run
```

---

## 5. 단계별 대화형 진행 절차

1. **iLO 계정 입력**
   - `iLO ID`와 `iLO Password`를 입력합니다.
   - 비밀번호는 화면에 표시되지 않으며, 어떤 로그 파일이나 표준 출력에도 기록되지 않습니다.
2. **하드웨어/컨트롤러 병렬 조회 (`[1/4]`)**
   - `pdsh -R exec`로 모든 대상 서버의 모델명, 세대(Gen10/Gen10+/Gen11), 전원 상태, 컨트롤러 목록을 병렬 조회합니다.
   - 이기종(세대/컨트롤러/디스크 수 불일치)이 섞여 있으면 경고 메시지가 표시됩니다.
   - 부팅 전용 컨트롤러(`NS204i-p`, `BOSS` 등)는 `[BOOT - 제외 권장]`으로 표시되며, 기본값 `auto` 선택 시 데이터용 메인 RAID 컨트롤러를 자동 선택합니다.
3. **물리 디스크 목록 확인 및 범위/RAID 입력 (`[2/4]`)**
   - 선택한 컨트롤러의 물리 디스크 테이블(`#`, Location, Protocol, Media, Size, Status, Assigned Volume)을 확인합니다.
   - 대상 디스크 범위와 RAID 레벨을 입력합니다.
   - RAID `0`을 선택한 경우 다음 두 가지 모드 중 선택할 수 있습니다.
     - `1`: **전체 통합 RAID 0** — 선택한 디스크 전체를 1개의 RAID 0 볼륨으로 구성
     - `2`: **디스크별 개별 RAID 0** — 선택한 디스크 1개당 RAID 0 볼륨 1개씩 개별 생성 (`1디스크 = 1볼륨`)
4. **계획 검토 및 최종 승인 (`[3/4]`)**
   - 서버별 삭제/생성 예정 볼륨 표를 출력합니다.
   - 기존 볼륨 충돌이 있으면 기본적으로 작업을 보호하며, `--force-delete` 사용 안내를 출력합니다.
   - `--force-delete`로 기존 볼륨이 삭제되는 경우 실수 방지를 위해 정확히 **`DELETE`** 를 입력해야만 진행됩니다.
5. **결과 요약 및 선택적 일괄 재부팅 (`[4/4]`)**
   - 서버별 적용 결과(`APPLIED`, `REBOOT_REQUIRED`, `SKIPPED`, `FAILED`)를 표로 출력합니다.
   - 재부팅이 필요한 서버가 있으면 **재부팅 필요 장비 목록(IP, 모델명, 사유)을 먼저 출력한 뒤** 일괄 재부팅 여부를 확인합니다(`y/N`).
   - `y`를 입력하면 재부팅이 필요한 서버만 선별하여 `ilorest reboot`(실패 시 Redfish `GracefulRestart` 자동 폴백)를 병렬 실행합니다.

---

## 6. 디스크 범위 지정 예시

콤마(`,`)로 여러 범위를 조합할 수 있습니다.

| 입력 예시 | 해석 기준 | 활용 시나리오 |
|---|---|---|
| `sda-sdh` | 선택 컨트롤러의 **SAS/SATA** 디스크만 베이(Bay) 순 정렬 후 1~8번째 선택 | SAS+NVMe 혼재 서버에서 SAS HDD 8개만 선택 |
| `nvme0n1-nvme7n1` (또는 `nvme0-nvme7`) | 선택 컨트롤러의 **NVMe** 디스크만 베이 순 정렬 후 1~8번째 선택 | SAS+NVMe 혼재 서버에서 NVMe SSD 8개만 선택 |
| `1-8` 또는 `1,3,5,7` | 출력된 디스크 테이블의 `#` 번호 기준 선택 | 프로토콜 무관하게 테이블 번호로 직접 선택 |
| `1I:1:1-1I:1:8` 또는 `Bay1-Bay8` | 물리 Location 문자열 직접 매칭 | 특정 포트/박스/베이 위치를 정확히 지정 |

### 지원 RAID 레벨 및 최소 디스크 수

| RAID 레벨 | 최소 디스크 수 및 조건 |
|---|---|
| `0` (통합 / 개별) | 1개 이상 |
| `1` | 2개 이상, 짝수 |
| `5` | 3개 이상 |
| `6` | 4개 이상 |
| `10` | 4개 이상, 짝수 |
| `50` | 6개 이상 |
| `60` | 8개 이상 |

---

## 7. 기존 볼륨 보호 및 `--force-delete` 사용법

- 선택한 디스크 중 이미 기존 RAID 볼륨(`Assigned Volume`)에 속해 있는 디스크가 있으면, 기본적으로 충돌(`CONFLICT`)로 차단되며 데이터 덮어쓰기를 방지합니다.
- **기존 OS 볼륨 보호**: Gen10 `SmartStorageConfig` 방식에서도 기존에 설정된 다른 볼륨(예: OS용 RAID 1)을 그대로 유지한 채 새 볼륨만 추가합니다.
- 충돌된 기존 볼륨을 삭제하고 다시 구성해야 하는 경우에만 명시적으로 `--force-delete` 옵션을 붙여 실행하세요.

```bash
bash raid0.sh -w ./worklist.txt --force-delete
```

- `--force-delete` 실행 시에도 마지막 단계에서 삭제 대상 볼륨 수를 경고하며 **`DELETE`** 를 정확히 입력해야만 삭제가 수행됩니다.

---

## 8. 세대별(Gen10 / Gen10 Plus / Gen11) 동작 차이

| 서버 세대 | 대표 컨트롤러 | 사용 Redfish 백엔드 | 반영 시점 |
|---|---|---|---|
| **Gen10** (iLO 5) | Smart Array `P408i-a`, `P816i-a`, `E208i-a` 등 | `SmartStorageConfig` (`rawput` + `DataGuard: "Disabled"`) | **재부팅 필요 (`REBOOT_REQUIRED`)** — 다음 POST 단계에서 반영 |
| **Gen10 Plus** (iLO 5) | Smart Array SR (P/E 시리즈) | `SmartStorageConfig` | **재부팅 필요 (`REBOOT_REQUIRED`)** |
| **Gen10 Plus** (iLO 5) | MegaRAID `MR416i-a`, `MR216i-a` / `SR932i-p` 등 RDE | 표준 Redfish `/Storage/{id}/Volumes` (`rawdelete` / `rawpost`) | 보통 **즉시 반영 (`APPLIED`)** (컨트롤러가 재부팅을 요구하면 `REBOOT_REQUIRED`) |
| **Gen11** (iLO 6) | `MR408i-o`, `MR416i-o`, `SR932i-p` 등 | 표준 Redfish `/Storage/{id}/Volumes` (`rawdelete` / `rawpost`) | 보통 **즉시 반영 (`APPLIED`)** (재부팅 메시지 감지 시 `REBOOT_REQUIRED`) |

---

## 9. 로그 확인 및 트러블슈팅

### 로그 위치

실행할 때마다 현재 경로 아래 `./logs/raid_YYYYMMDD_HHMMSS/` 디렉토리에 모든 이력이 자동 저장됩니다.

- `discovery_summary.txt`: 조회된 서버 요약 및 컨트롤러 목록
- `drive_inventory.txt`: 선택한 컨트롤러의 물리 디스크 테이블
- `plan_preview.txt`: 적용 전 계획 미리보기
- `apply_summary.txt`: 서버별 최종 적용 결과 및 재부팅 필요 목록
- `reboot_summary.txt`: 일괄 재부팅 실행 결과 (재부팅 수행 시)
- `discover_<IP>.log`, `apply_<IP>.log`, `reboot_<IP>.log`: 서버별 상세 `ilorest` 실행 로그

> **보안**: 모든 워커 로그는 비밀번호 문자열을 자동 마스킹(`***`)하며, 종료 시 임시 캐시 디렉토리와 `ILO_PASS` 변수를 정리합니다.

### 자주 발생하는 문제 해결

1. **`pdsh에 'exec' 모듈이 없습니다` 오류**
   - `pdsh-rcmd-exec` 패키지가 설치되어 있지 않은 경우입니다. `yum install pdsh-rcmd-exec` (폐쇄망은 로컬 RPM 설치) 후 `pdsh -L | grep exec`로 확인하세요.
2. **특정 서버 `iLO login failed` 오류**
   - 대상 iLO IP 방화벽(TCP 443), 계정 권한(Configure iLO / Storage 권한), 비밀번호를 확인하세요.
   - 다른 정상 서버는 영향을 받지 않고 계속 조회되며, 실패 원인은 `logs/raid_.../discover_<IP>.log`에 기록됩니다.
3. **병렬 실행 중 `ilorest` 캐시 충돌 우려**
   - 워커(`lib/ilo_worker.sh`)가 IP별 독립 디렉토리(`--cache-dir "$RUN_TMP_DIR/cache_<IP>"`)를 사용하고 종료 시 `trap`으로 항상 `ilorest logout`을 호출하므로, 동시 실행(`-f 10~20`) 시에도 캐시나 iLO 세션이 충돌하지 않습니다.
4. **Gen10 적용 후 OS에서 디스크가 바로 보이지 않는 경우**
   - Gen10 `SmartStorageConfig` 변경 사항은 서버 재부팅(POST) 과정에서 컨트롤러에 커밋됩니다. 4단계에서 재부팅을 건너뛴 경우 수동으로 서버를 재부팅한 뒤 확인하세요.
