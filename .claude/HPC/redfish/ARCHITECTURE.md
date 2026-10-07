# ARCHITECTURE.md

BMC(Redfish)에 접속해 BIOS 표준값을 점검하고 승인된 항목만 Pending 으로 설정하는 도구입니다. 전체를 다 읽지 않고도 어디를 고쳐야 할지 찾을 수 있게 파일별 역할을 정리했습니다. 사용법은 [README.md](README.md), 첫 실장비에서 확인할 것은 [FIRST_RUN.md](FIRST_RUN.md) 를 보십시오.

## 폴더·파일별 역할

### Go 소스 — `cmd/biostool/` (package main, 표준 라이브러리만, Go 1.20 호환)

| 파일 | 역할 |
|---|---|
| `main.go` | CLI 진입점. 서브커맨드(`check`·`allcheck`·`dump`·`encrypt`)·공통 플래그·`usageText`, `prepare`(conf → 대상 해석 → 비밀번호 복호화), 실행 요약 출력 |
| `conf.go` | `bios.conf` 파서와 기본값(`Config`). 알 수 없는 키·비밀번호 키는 오류 |
| `profile.go` | `profiles/<이름>.tsv` 로드·검증(`*` 행의 `verified=Y` 거부, 중복 검사), `rowsFor`(모델 정확 일치 우선 → `*`) |
| `modelnorm.go` | 모델명 정규화 `normalizeModel`(프로파일의 model 열과 BMC 가 보고한 Model 을 같은 키로 맞추는 **한곳의 규칙**, allcheck 도 사용) |
| `resolve.go` | 대상 목록 해석: `user.txt` 파싱, `/etc/hosts` 에서 `이름-m` 조회, `:포트`, 재시도 목록(`loadRetryList`), `NO_HOSTS_ENTRY` |
| `cryptutil.go` | AES-256-GCM 비밀번호 암복호화(`pass.enc`+`key.bin`, 0600), `encrypt` 서브커맨드의 본체 |
| `redfish.go` | **BMC 와 통신하는 유일한 HTTP 클라이언트.** 세션 1회·`checkAllowed`(호출 허용목록)·`checkBody`(본문 검사)·타임아웃·재시도·백오프·401 즉시 중단·오류 상태 분류(`UNREACHABLE`·`TIMEOUT`·`BMC_ERROR`·`AUTH_FAIL`·`UNSUPPORTED`·`HTTP_ERROR`)·비밀번호 마스킹 |
| `inspect.go` | 시스템 판별 `detectSystem`(ServiceRoot → Systems → Bios → Settings 경로 → 속성 레지스트리 위치), Bios 속성·레지스트리 해석, 벤더 정규화 `normalizeVendor`, **표준 4항목 이름 후보 키워드 `stdKeywords`** |
| `sweep.go` | 호스트 순회 워커풀과 **AUTH_FAIL 차단기**(웜업 직렬 → 병렬, `SKIPPED_AUTH_STOP`). check·dump·allcheck·set 이 공유 |
| `check.go` | `check` 본체: 호스트별 판정(`judge`), 상태 상수(OK·FAIL·UNVERIFIED·PENDING_*·MAPPING_MISSING·NO_DUMP), Dell BIOS Job 판별, `-from-dump` 읽기, `result.tsv`·`ok.txt`·`fail.tsv`·`retry.txt`·`run_info.txt` 기록 |
| `report.go` | `check` 의 터미널 리포트(`[OK]`·`[FAIL 상세]`·`[설정 불가]`·`[기타 오류]`)와 **Y/N 확인**(`confirmApply`, 비대화형 입력은 N, `-stdin-ok`) |
| `set.go` | **BMC 에 쓰는 유일한 경로.** 입력 재검증·벤더 제한·재조회·레지스트리 사전검증·시스템 프로파일 종속 처리·PATCH 1회·Dell Job POST·반영 재조회, `-dry-run`(`buildPlan`/`writePlan` 공유), `apply_*` 결과 파일과 설정 리포트 |
| `dump.go` | `dump`(`xml.sh`) 본체: GET 전용 좁은 순회, 호스트당 경로 상한, JSON 저장(`safeSeg`/`dirPart`), `index.tsv`, 사람이 읽는 요약 |
| `allcheck.go` | `allcheck`(`all_bios_check.sh`) 본체: 기준(`diff.txt`)·제외 속성(`ignore_attrs.txt`)·모델별 분류·Attributes 전체 비교·`all_diff.tsv`·`summary.tsv` |
| `allreport.go` | `allcheck` 의 터미널 리포트(모델 그룹·속성별 집계·특이사항·분류 불가) |
| `retry.go` | `-retry-from`: 이전 결과 읽기·검증, 재시도 대상만 다시 실행한 결과를 이전 결과와 병합(`merged_*`) |
| `*_test.go` | 단위·통합 테스트. 보안 규칙은 `redfish_security_test.go`·`set_security_test.go`, 수천 대 시험은 `scale_test.go`, mock 연동은 `mock_integration_test.go` |

### mock BMC (개발·검증 전용, 배포 대상 아님)

| 경로 | 역할 |
|---|---|
| `internal/mockbmc/mockbmc.go` | Redfish BMC 를 흉내 내는 mock 서버: 트리 파일 서빙, 세션, Pending 병합, 레지스트리 검사, 요청 기록(`Calls`·`Writes`·`LogHits`·`Sessions`), 장애 주입 |
| `internal/mockbmc/farm.go` | `Farm`: 한 TLS 리스너로 가상 BMC 수천 대(127.0.x.y)를 흉내 냄(트리 공유, 호스트별 상태만 개별). 리눅스 전용 |
| `cmd/mockbmc/main.go` | `mockbmc` 실행 파일(`mockbmc.sh` 가 빌드·실행). 종료 시 `MOCKBMC_SUMMARY` 집계 출력 |
| `testdata/` | mock 용 **합성** Redfish 트리 6종(Dell·HPE×2·Lenovo·Cisco·Supermicro)과 테스트용 프로파일 `testdata/profiles-test/VM.tsv`. 실제 덤프가 아님 → [testdata/README.md](testdata/README.md) |

### 데이터·설정·스크립트·산출물

| 경로 | 역할 |
|---|---|
| `profiles/VM.tsv` | 표준값(프로파일). 엑셀 표 시작값, 전부 `verified=N`. 프로파일 추가 = 파일 추가 |
| `bios.conf.example` · `user.txt.example` · `diff.txt.example` · `ignore_attrs.txt.example` | 설정·입력 파일 예시(실제 값은 `.gitignore` 로 커밋 금지) |
| `bios_check.sh` | 점검·설정 래퍼(프로파일 메뉴, 파일 확인) → `biostool check` |
| `xml.sh` | 사전조사 래퍼 → `biostool dump -targets …` |
| `all_bios_check.sh` | 전체 비교 래퍼 → `biostool allcheck` |
| `encrypt.sh` | 비밀번호 암호화 래퍼(비밀번호는 파이프로만 전달) → `biostool encrypt` |
| `lib.sh` | 래퍼 공용 함수: OS(커널 메이저)별 바이너리 선택 `select_bin`, `conf_val`. bash 4.1 호환 |
| `mockbmc.sh` | mock BMC 기동 래퍼(필요하면 `bin/mockbmc` 빌드, 저장소에는 커밋하지 않음) |
| `setup.sh` · `build.sh` · `build_os6.sh` | 오프라인 빌드 / os8 빌드 / os6(RHEL6, Go 1.20.x) 빌드 |
| `bin/biostool` · `bin/biostool_os6` | **빌드 완료 바이너리(커밋 대상).** 회사 환경에 Go 가 없어 이것이 곧 배포본. 소스를 고치면 둘 다 재빌드해 함께 커밋 |
| `go.mod` | `module biostool`, `go 1.20`, 외부 의존성 없음(→ `vendor/` 불필요) |
| `.gitignore` · `.gitattributes` | 실제 호스트·계정·결과물 커밋 차단 / `.sh`·`.tsv`·`.example`·`.json` 은 LF, `bin/*` 는 바이너리 |
| `README.md` · `WORKFLOW.md` · `workflow*.svg` · `FIRST_RUN.md` · `PR_CHECKLIST.md` · `CHANGELOG.md` · `사용법.txt` · `계획서.md` | 문서(README 4절 참고) |
| 저장소 루트 `.github/workflows/redfish-ci.yml` | 이 폴더가 바뀔 때 build/vet/test 자동 실행 |

## 수정 요청이 “OO 추가”면 볼 곳

| 하고 싶은 일 | 볼 곳 |
|---|---|
| **새 벤더·모델 추가**(점검 대상) | `profiles/*.tsv` 에 행 추가(`verified=N` 로 시작) → 속성 이름이 후보 키워드로 안 잡히면 `inspect.go` 의 `stdKeywords`, 모델명 표기가 맞지 않으면 `modelnorm.go` 의 `modelPrefixes`, 벤더 문자열은 `inspect.go` 의 `normalizeVendor`. 절차는 [FIRST_RUN.md](FIRST_RUN.md) |
| 새 모델에 **설정(쓰기) 허용** | 코드 수정 없이 프로파일 행을 `verified=Y`(모델 지정 행)로 승격. 쓰기 벤더 자체를 늘리는 것은 `set.go` 의 `setVendors` + 보안 테스트(`set_security_test.go`) |
| **새 표준 항목** 추가 | 표준 4항목을 늘리려면 `inspect.go` 의 `stdKeywords`(이름 후보 키워드)와 `profile.go` 가 쓰는 항목 목록, 프로파일 TSV 행. 표준이 아닌 **추가 항목**은 프로파일에 새 `std_name` 행만 추가하면 됨(코드 수정 불필요) |
| **상태(결과 코드) 추가·변경** | 점검: `check.go` 상수와 `judge`, 터미널 표시는 `report.go`(`reportOrder`·`otherErrOrder`), 설정: `set.go` 상수·`applyOrder`·`applyFailedStatus`, 재시도 여부는 `check.go` 의 `isRetryStatus`, 병합은 `retry.go`. 바뀌면 README 3.6절 표 갱신 |
| **안전 규칙 변경**(허용 호출·본문·위험 속성) | `redfish.go` 의 `checkAllowed`·`checkBody`·`reDangerAttr` + **보안 테스트**(`redfish_security_test.go`, `set_security_test.go`)를 먼저 고치고 통과시킬 것. 허용목록을 넓히면 PR_CHECKLIST 의 보안 항목을 반드시 확인 |
| 로그인·재시도·타임아웃·AUTH_FAIL 동작 | `redfish.go`(`Client.do`·`login`), 차단기는 `sweep.go`, conf 키는 `conf.go` |
| **bios.conf 키 추가** | `conf.go`(`Config`·`defaultConfig`·`parseConf` 의 switch), `bios.conf.example`, README 3.3절 표, `lib.sh` 가 읽는 키라면 `conf_val` 호출부 |
| 터미널 리포트 모양 | 점검 `report.go`, 설정 결과 `set.go`(`writeApplyReport`), 전체 비교 `allreport.go` |
| 결과 파일 열·이름 변경 | `check.go`(`resultHeader`·`writeResultFiles`), `set.go`(`applyHeader`·`writeApplyFiles`), `allcheck.go`(`allDiffHeader`·`summaryHeader`), `retry.go`(병합이 헤더를 검증하므로 함께) |
| 이름 해석(`user.txt` 표기) | `resolve.go` |
| 덤프 범위·요약 | `dump.go`(순회 범위·상한 `maxDumpResources`·요약), 저장 폴더 규칙 `safeSeg`/`dirPart`(mock·`-from-dump` 와 같은 규칙) |
| 새 mock 시나리오·벤더 응답 | `testdata/<트리>/…` JSON(`Oem.MockData` 표식·링크 무결성 유지) 또는 테스트 코드의 `Server.SetBiosAttr`/`Modify`/`Fault` |
| 래퍼 스크립트 | `bios_check.sh`·`xml.sh`·`all_bios_check.sh`·`encrypt.sh`·`lib.sh`(bash 4.1 에서 동작해야 함). Go 와 쌍이므로 플래그가 바뀌면 usage 도 함께 |
| 빌드 방법 | `setup.sh`·`build.sh`·`build_os6.sh`, CI 는 저장소 루트 `.github/workflows/redfish-ci.yml` |

## 안전 설계 요약

BMC 에 남는 흔적을 줄이고 운영자의 실수를 코드로 막는 것이 설계의 중심입니다.

1. **허용목록(`redfish.go` 의 `checkAllowed`)** — 모든 HTTP 요청은 `Client.do` 한 경로만 지나고 전송 직전에 다시 검사합니다. 목록 밖 요청은 네트워크로 나가지 않고 `ErrNotAllowed`.

   | 메서드 | 허용 | 모드 |
   |---|---|---|
   | GET | `/redfish/v1` 아래. **로그·Actions·SSE 세그먼트(LogServices, SEL, Entries, `*Log`, `*Logs`, Actions, sse 등)는 거부** | 전 모드 |
   | POST | `SessionService/Sessions`(로그인) | 전 모드 |
   | POST | Dell `Managers/<id>/Jobs`(BIOS 설정 Job, 본문은 `TargetSettingsURI`(정규형 `Systems/<id>/Bios/Settings`)만 + 시작시각류) | 설정(Y) 모드만 |
   | PATCH | `Systems/<id>/Bios/Settings` 또는 `Bios/Pending`, 본문은 `Attributes` 와 `ApplyTime=OnReset` 뿐(`Immediate` 금지, 위험 속성명·객체·빈값 금지) | 설정(Y) 모드만 |
   | DELETE | **자기가 만든 세션 URI 1개** | 전 모드 |

   → `ClearLog`, 로그·Job 삭제, 리셋(`ComputerSystem.Reset`), 즉시 적용은 경로 자체가 목록에 없어 **구조적으로 불가능**합니다.
2. **세션 1회** — 호스트당 로그인 1회, 모든 요청에 토큰, 끝나면 자기 세션만 삭제. 세션을 지원하지 않는 BMC 만 Basic 으로 폴백(요청마다 인증 기록이 남을 수 있음). 401 은 재시도하지 않고 이후 호출도 보내지 않습니다.
3. **쓰기 경로는 `set.go` 뿐** — 점검·덤프·전체 비교는 쓰기가 불가능한 읽기 전용 클라이언트(`ModeReadOnly`)이고, `-dry-run` 도 같은 읽기 전용 클라이언트라 쓰기가 구조적으로 불가능합니다. `ModeSet` 은 Y 응답 뒤 `set.go` 에서만 만듭니다.
4. **여러 겹의 입력 재검증(`set.go`)** — 점검 결과를 그대로 믿지 않고 프로파일에서 *모델을 지정한 `verified=Y` 행*을 다시 확인, Dell·HPE·Lenovo 만, 재조회로 점검 이후 변화(`CHANGED`·`ALREADY_OK`·`PENDING_EXISTS`) 확인, 레지스트리 ReadOnly·허용값·형식 확인, 시스템 프로파일 종속은 프로파일 항목만 먼저. 호스트당 PATCH 1회·재시도 없음, Dell Job 은 자동 생성되지 않았을 때만 최대 1회.
5. **계정 잠금 방지** — `sweep.go` 의 AUTH_FAIL 차단기(처음 `auth_fail_stop` 대 직렬 접속 → 성공하면 병렬, 누적되면 중단 + `SKIPPED_AUTH_STOP`).
6. **비밀 취급** — 비밀번호는 `pass.enc`(AES-256-GCM)+`key.bin`(0600)으로만 보관, CLI 인자·로그·TSV 에 남기지 않으며 오류 상세에서도 비밀번호·토큰·Basic 헤더를 지웁니다. 결과 파일은 0600.

작업 흐름도는 [WORKFLOW.md](WORKFLOW.md) 참고.
