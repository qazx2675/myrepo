# AWX 템플릿 실행 CLI (awxkit) - 계획서

> **진행 상태 (2026-08-19 기준)**: 0~3단계 완료 (config 로더 + `CurrentUser()` 훅 + `doctor` + `ls`/`survey` + `nodeinfo`).
> Rocky Linux(192.168.0.58)에서 빌드/`go vet` 통과, 로컬 HTTP 스텁 서버로 정상·오류 경로 검증 완료. 4단계([S2] 인벤토리 동기화)부터 이어서 진행 예정.
> hostname은 `-host` 단일 플래그가 아니라 `${user}.txt`(conf와 동일한 탐색 규칙) 목록 파일로 받도록 설계 변경됨 — hostname은 NodeInfo 단계에서만 필요하고, 이후 단계는 인벤토리에 등록된 상태를 기준으로 동작하기 때문.
> 작업 이력은 [`WORKLOG.md`](./WORKLOG.md), 사용법은 [`README.md`](./README.md) 참고.

## 목적

ETX 원격 터미널 환경에서 AWX 웹 GUI를 띄우면 화면 지연(Lag)이 심해 실질적으로 사용이 어렵다.
또한 AWX 관리 권한이 없어 템플릿 탐색·가시성이 부족하다.
이 도구는 **웹 콘솔 접속 없이 터미널 안에서 AWX 템플릿을 실행·추적·검증**하기 위한 Go 기반 단일 바이너리 CLI다.

## 배경 및 제약

- **폐쇄망**: 외부 인터넷 차단. `vendor/`를 포함해 폴더를 통째로 옮기면 빌드 가능해야 한다.
- **현장 정보 미확정**: 인터넷 환경(집)에서는 AWX URL·템플릿 ID·파라미터 키값을 알 수 없다.
  → **설정 파일(`${user}_setting.conf`)만 수정하면 즉시 동작**하는 구조가 필수 전제다.
- **다중 사용자**: 여러 사용자가 같은 서버에서 사용한다. 설정은 사용자별로 분리한다.
- **Golang 채택 배경**: 외부 의존성 없이 단일 바이너리로 빌드 가능해 폐쇄망 배포에 최적.

## 범위

**포함**
- `${user}_setting.conf` 기반 설정 로딩 및 사용자 식별 훅
- 연결/권한/파라미터 사전 진단 (`doctor`)
- 템플릿·인벤토리 탐색 및 survey 정의 자동 조회 (`ls`, `survey`)
- 4대 시나리오 자동화 (NodeInfo / 인벤토리 동기화 / DHCP / PXE)
- Job 상태 추적 및 결과 리포트
- 폐쇄망 빌드 패키징 및 문서화

**제외**
- AWX 관리 기능 (템플릿 생성·수정·삭제, 사용자/권한 관리)
- 웹 GUI 전면 대체, 워크플로/스케줄 편집
- TUI 프레임워크 도입 — ETX 저대역 환경에서 화면 재그리기는 오히려 체감 저하를 유발한다.

---

## 확정된 설계 결정

| 항목 | 결정 | 근거 |
|---|---|---|
| 인증 | Basic 인증, ID/PW를 `${user}_setting.conf`에 평문 저장 | 폐쇄망 운용 편의 우선. 대신 `chmod 600` 안내 및 실행 시 권한 경고 출력 |
| 설정 파일 | `${user}_setting.conf` **단일 파일**만 사용 (공통 파일 없음) | 사용자 요청 |
| 조작 방식 | **대화형 메뉴 + 플래그 병행** | 인자 없이 실행 → 번호 선택 메뉴 / 인자 지정 → 즉시 실행 (스크립트화 가능) |
| 결과 파일 | 취득 경로 3종(`artifacts`/`stdout`/원격파일) 모두 지원, **로컬 저장 경로는 conf에 지정** | 현장에서 어느 경로로 나오는지 미확정이므로 전부 대비 |
| 설정 포맷 | `key = value` 평문 (`#` 주석 허용) | 의존성 제로 원칙상 YAML/TOML 파서 사용 불가. 현장에서 vi로 여는 파일이라 오히려 적합 |
| 사용자 식별 | `config.CurrentUser()` 함수 **껍데기만 제공** | 사용자가 현장 로직을 직접 채워 넣음 |
| hostname 입력 | `-host` 단일 플래그 대신 **`${user}.txt`** 목록 파일 (conf와 동일한 탐색 규칙) | hostname은 [S1] NodeInfo에서만 필요. 이후 [S2]~[S4]는 인벤토리 등록 상태를 기준으로 동작 |
| NodeInfo 실행 방식 | hostname마다 개별 launch가 아니라, **`${user}.txt`의 전체 hostname을 줄바꿈으로 이어붙여 템플릿을 한 번만 실행** | NodeInfo 템플릿 자체가 hostname을 텍스트로 한 번에 받아 처리하는 구조. 결과도 파일 하나로 나옴 |
| 양식 변환 확인 | 다운로드 결과를 정해진 양식으로 바꾸는 스크립트는 **awxkit이 실행하지 않음**. 사용자가 별도 터미널에서 직접 실행한 뒤, awxkit이 물어보는 `Y/N`에 답해야 완료 처리됨 | 변환 스크립트는 사용자가 이미 보유한 별도 자산이며 자동 호출 대상이 아님. 종료코드는 awxkit이 볼 수 없으므로(직접 실행하지 않으므로) 사람의 확인으로 판단 |
| 실행 환경 | 핵심 로직은 Go 바이너리, `run.sh`로 감싸 바이너리 없으면 자동 빌드 후 실행 | Go로 만든 걸 bash로 쉽게 실행할 수 있어야 한다는 요청 반영 |

### 설정 파일 예시 (`conf/sample_setting.conf`)

```ini
# ── AWX 접속 (여기만 채우면 doctor 까지 동작) ──────────────
awx_url        = http://10.20.30.40
username       = admin
password       = changeme
insecure_tls   = true

# ── [S1] NodeInfo ──────────────────────────────────────
s1_template        = nodeinfo      # ID(숫자) 또는 템플릿 이름 둘 다 허용
s1_hostname_key    = target_host
s1_fetch           = artifacts     # artifacts | stdout | remote
s1_artifact_key    =               # s1_fetch=artifacts 일 때 결과가 담긴 artifacts 키. 비우면 전체 저장
s1_remote_path     =               # s1_fetch=remote 일 때 AWX 실행노드상의 경로
s1_output_dir      = ./output      # 로컬 저장 경로 (실행 1회당 ${user}_nodeinfo.yaml 하나)

# ── [S2] 인벤토리 동기화 ────────────────────────────────
s2_inventory_source = 5
s2_inventory        = 3

# ── [S3] DHCP ──────────────────────────────────────────
s3_template        = dhcp-register
s3_infra_key       = infra_type
s3_infra_choices   = seoul, daejeon, busan   # survey_spec 조회 가능하면 생략

# ── [S4] PXE ───────────────────────────────────────────
s4_template        = pxe-register
s4_infra_key       = pxe_infra
s4_osver_key       = os_version
s4_bootmode_key    = boot_mode
s4_splunk_key      = install_splunk
s4_inventory       = 3           # 등록 완료 호스트 수를 셀 인벤토리

# ── 공통 동작 ──────────────────────────────────────────
poll_interval  = 3               # Job 상태 폴링 간격(초)
history_file   = ./awxkit_history.log
```

### 설정 파일 / 호스트 목록 탐색 순서

`${user}_setting.conf`와 `${user}.txt`(nodeinfo용 hostname 목록) 모두 동일한 규칙을 따른다.

1. `-conf <경로>` (또는 `-hosts <경로>`) 플래그
2. `./conf/<파일명>`
3. `~/.awxkit/<파일명>`
4. `<바이너리 위치>/conf/<파일명>`

`${user}`는 `config.CurrentUser()` → `-user` 플래그 → `AWXKIT_USER` 환경변수 → `$USER` 순으로 결정한다.
`CurrentUser()`가 빈 문자열을 반환해도 나머지 폴백으로 도구는 정상 동작한다.

---

## AWX API 매핑

| 기능 | 엔드포인트 |
|---|---|
| 연결/버전 확인 | `GET /api/v2/ping/` |
| 템플릿 목록 | `GET /api/v2/job_templates/?page_size=200` |
| 실행 전 조사 | `GET /api/v2/job_templates/{id}/launch/` → `ask_variables_on_launch`, `survey_enabled` |
| 셀렉트박스 정의 | `GET /api/v2/job_templates/{id}/survey_spec/` |
| 템플릿 실행 | `POST /api/v2/job_templates/{id}/launch/` body `{"extra_vars":{...}}` |
| Job 상태 폴링 | `GET /api/v2/jobs/{id}/` → `status`, `failed` |
| Job 로그 | `GET /api/v2/jobs/{id}/stdout/?format=txt` |
| Job 산출물 | `GET /api/v2/jobs/{id}/` 의 `artifacts` 필드 (플레이북이 `set_stats` 사용 시) |
| 인벤토리 소스 동기화 | `POST /api/v2/inventory_sources/{id}/update/` → `GET /api/v2/inventory_updates/{id}/` |
| 호스트 수 | `GET /api/v2/inventories/{id}/hosts/?page_size=1` → `count` |

---

## 진행 단계

| 단계 | 내용 | 완료 기준 (검증 가능) |
|---|---|---|
| **0** | 저장소 클론, 폴더 구조·`PLAN.md`·`WORKLOG.md` 정비 | ✅ 완료 (2026-08-19) — 원격 파일 유실 없이 커밋 |
| **1** | config 로더 + `CurrentUser()` 훅 + `awxkit doctor` | ✅ 완료 (2026-08-19) — conf에 URL/ID/PW만 넣고 `doctor` 실행 → 버전·인증·템플릿 개수·권한·`ask_variables_on_launch` 점검까지 출력. Rocky Linux(192.168.0.58) 빌드/`go vet` 통과, 스텁 서버로 정상/오류 경로 검증 |
| **2** | 공통 AWX 클라이언트(launch/poll/stdout) + `ls` / `survey` | ✅ 완료 (2026-08-19) — 템플릿 목록과 survey 정의(변수명·선택지·기본값)를 조회해 출력. 스텁 서버로 정상/오류(비-survey 템플릿, 존재하지 않는 템플릿) 경로 검증 |
| **3** | [S1] `nodeinfo` (`${user}.txt`의 hostname 전체를 한 번에 실행) + 결과 파일 저장 | ✅ 완료 (2026-08-19) — 전체 hostname을 하나의 extra_vars로 묶어 1회 실행·폴링·결과 저장(`${user}_nodeinfo.yaml`), 실패 시 stdout 마지막 30줄, `history_file` 기록. `setup.sh`가 `main.go`만 빌드하던 버그와 CRLF 줄바꿈 문제(`.gitattributes` 추가)도 함께 수정 |
| **4** | [S2] `invsync` 인벤토리 동기화 + 등록 결과 조회 | sync 성공 상태 + 호스트 리스트 출력 |
| **5** | [S3] `dhcp -infra <n\|이름>` | `successful`/`failed` 판정 즉시 출력, 실패 시 stdout 마지막 30줄 |
| **6** | [S4] `pxe` 4개 옵션 조합 + 호스트 수 리포트 | "총 N대의 호스트가 등록 완료되었습니다." 출력 |
| **7** | 폐쇄망 패키징(실 vendor, 크로스컴파일) + README 2~4장 완성 | 인터넷 차단 상태에서 `bash setup.sh` 빌드 성공 |

**1단계가 끝나는 시점부터 현장 검증이 가능**하도록 순서를 배치했다.
집에서는 AWX URL을 알 수 없으므로, 1단계 산출물을 현장에서 먼저 돌려 접속·권한을 확인한 뒤 나머지를 채우는 것이 리스크가 가장 적다.

---

## 시나리오별 상세

### [S1] NodeInfo 템플릿 실행 및 결과 파일 저장
`${user}.txt`에 나열된 hostname 전체를 줄바꿈으로 이어붙여 `s1_hostname_key`에 담아 템플릿을 한 번 실행 → Job 상태 실시간 추적 → 성공 시 결과 취득.
취득 경로는 `s1_fetch`로 선택하며, 저장 위치는 `s1_output_dir/${user}_nodeinfo.yaml`이다.
다운로드 후에는 결과를 정해진 양식으로 바꾸는 (사용자가 이미 보유한) 별도 스크립트를 다른 터미널에서 실행하도록 안내하고,
완료 여부를 `Y/N`으로 확인받는다. `Y`가 아니면 `downloaded_unconfirmed` 상태로 종료 코드 1을 반환한다.

### [S2] 인벤토리 동적 동기화
[S1]에서 받은 YAML을 AWX가 참조하는 경로에 등록하거나 인벤토리 소스로 주입한 뒤,
`inventory_sources/{id}/update/`로 sync를 트리거하고 등록 결과를 리스트/JSON으로 확인한다.

### [S3] DHCP 등록 및 결과 검증
인프라 선택지를 번호로 고르거나 `-infra` 플래그로 지정 → 템플릿 실행 → 최종 상태를 즉시 출력.
**설정 변경 계열이므로 완료 후 "랜덤 서버 몇 대를 직접 확인하라"는 경고를 화면에 출력한다.**

### [S4] PXE 등록 및 호스트 리포팅
`인프라`·`OS 버전`·`Boot Mode`·`Splunk 설치 여부` 4개 파라미터를 조합해 실행 →
완료 후 `s4_inventory`의 전체 호스트 수를 조회해 요약 리포트를 출력한다.

---

## 추가 기능 (합의됨)

1. **`awxkit doctor`** — conf 검증 + 연결 + 권한 + `ask_variables_on_launch` 점검 (1단계 포함)
2. **`awxkit survey <템플릿>`** — 템플릿의 survey 정의를 조회해 conf에 붙여넣을 스니펫으로 출력
3. **실행 이력 로그** — `history_file`에 누가·언제·어떤 템플릿을 무슨 파라미터로 실행했는지 기록
4. **`-dry-run`** — 실제 launch 없이 전송될 extra_vars만 출력
5. **`awxkit logs <job_id>`** — 지난 Job stdout 재조회
6. **`-nowait`** — 오래 걸리는 Job은 job id만 받고 즉시 반환
7. **설정 변경 경고 출력** — DHCP/PXE 실행 결과 화면에 검증 권고 문구 출력

---

## 리스크 및 미확인 사항

| 리스크 | 영향 | 대응 |
|---|---|---|
| 결과 파일 취득 경로 미확정 | [S1]이 막힘 | `s1_fetch`로 3가지 경로 모두 지원, 현장에서 선택 |
| `ask_variables_on_launch` 미허용 | extra_vars가 **무시된 채 성공**으로 보임 (에러 없음) | `doctor`가 사전 경고 + launch 응답의 `ignored_fields` 검사 |
| 계정 권한 부족(템플릿 실행 불가) | 403 | `doctor`에서 실행 권한 사전 확인 |
| survey 문항의 변수명 불일치 | 파라미터가 전달되지 않음 | `survey` 명령으로 실제 `variable` 값을 조회해 conf에 반영 |
| 폐쇄망 서버에 Go 미설치 | 빌드 불가 | Rocky Linux(192.168.0.58)에서 `GOOS=linux GOARCH=amd64` 크로스빌드 후 바이너리도 함께 배포 |
| conf 평문 비밀번호 | 다중 사용자 환경에서 유출 가능 | `chmod 600` 안내, 실행 시 권한이 느슨하면 경고 출력 |
| 이름 충돌 | AWX 공식 Python CLI 이름도 `awxkit` | 폴더명은 유지. 바이너리명 변경 여부는 확인 필요 |

---

## 개발/검증 환경

- Go 빌드 및 테스트: **Rocky Linux 192.168.0.58** (`root@192.168.0.58`, Go 설치됨)
- 실제 AWX 서버는 폐쇄망에만 존재하므로, 집에서는 **HTTP 스텁 서버 기반 테스트**로 대체한다.
