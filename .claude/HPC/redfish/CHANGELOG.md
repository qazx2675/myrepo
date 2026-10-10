# CHANGELOG.md — redfish (biostool)

## [Unreleased] (v0.2.0 후보) - 2026-10-10

Cisco UCSM 읽기 전용 점검·연구용 프로파일과 2순위(예비) 계정을 추가했습니다. **실제 BMC·UCSM 에 접속해 본 적은 없습니다**(아래 “알려진 한계”, [FIRST_RUN.md](FIRST_RUN.md) 18~25).

### 추가

- **1순위 계정 생성·비밀번호 변경 `biostool account create|change`** + 래퍼 `account_create.sh` (확정 사양 [ACCOUNT_SPEC.md](ACCOUNT_SPEC.md), 사용법·실제 출력 README 2.12절). 1순위 계정(`bios.conf` user + `pass.enc`)이 BMC 에 없으면 rf.conf 의 벤더 admin(2순위)으로 로그인해 **같은 id/pw 의 관리자 계정을 만들고**(같은 id 가 있으면 덮어쓰지 않고 건너뜀), 새 계정으로 1회 로그인해 검증하며, **인증이 거부된 서버만 이번 실행이 만든 계정을 되돌립니다**(롤백). 점검 끝(`bios_check.sh`)에서도 같은 질문을 하며 기본 응답은 Y(빈 입력)입니다. 터미널이 아니면 `-stdin-ok` 없이는 묻지 않습니다. `change` 는 바이너리를 직접 쓰고(래퍼 없음) 새 비밀번호를 터미널에서 `*` 로 2번 입력받으며 붙여넣기를 거부합니다. `pass.enc` 는 자동 갱신하지 않고 `cd <폴더>; bash encrypt.sh` 를 csh 에서 그대로 복사할 수 있게 안내합니다. 결과 `account_result.tsv`(0600, 비밀번호·해시 없음). 구현 `cmd/biostool/account*.go`.
- **쓰기 허용목록 `ModeAccount`** (`redfish.go`): 계정 컬렉션 POST·계정 멤버 PATCH/DELETE 만 열고, PATCH/DELETE 는 이번 실행이 등록한(빈 슬롯·만든 계정·대상 계정) 멤버에만 나갑니다. 본문은 고정 형태만 허용(생성 키 집합, 비밀번호 변경, 슬롯 롤백 2단계). BIOS 점검·설정 모드에서는 계정 경로가 계속 거부됩니다.
- **Cisco UCSM 사용자 생성·변경** (`internal/ucsmxml` 계정 모드): `configConfMo` 로 `sys/user-ext/user-<id>` 의 aaaUser 하나만 생성(`created`, 역할 admin)·비밀번호 변경(`modified`, 허용 등록된 이름)·삭제(`deleted`, 이번에 만든 이름) 합니다. 그 밖의 `configConfMo`·다른 객체·`status="created,modified"` 는 전송 전에 거부됩니다. 읽기 전용 점검기(`biostool ucsm`)는 그대로 쓰기 불가입니다.
- **rf.conf 에 `cray` 벤더 추가**(`cray_user`/`cray_pass`): 비인증 ServiceRoot 의 `Vendor`/`Oem` 에 `cray` 가 들어 있는 BMC 가 대상.
- **mock 계정 서비스**: `mockbmc -accounts [-account-slots N] [-account-slot-style] [-account-deny-new-login]` (AccountService/Accounts/Roles 동적 응답, 슬롯 방식·검증 실패 시험).
- **테스트케이스 시나리오 7~11**: 계정 생성(기본 Y)·중복 건너뜀·슬롯 방식·검증 실패 롤백·변경 dry-run.
- 보안 리뷰(Jev 분류 `security_review`)에서 확신도 7 이하 지적 3건을 반영: 콘솔 출력의 BMC 값 정리, 기본 Y 질문 전에 터미널 입력 버퍼 비움, `change` 를 1순위 로그인 호스트·정확한 이름 일치로 한정(빈 슬롯은 Id 2 이상).
- **Cisco UCSM 읽기 전용 점검**: `biostool ucsm` + 래퍼 `ucsm_check.sh`. 대상은 블레이드 BMC 가 아니라 UCSM(Fabric Interconnect VIP) 주소입니다. 호출은 `POST https://<UCSM>/nuova` 의 `aaaLogin` → `configResolveClass` → `aaaLogout` 뿐이며 전송 직전 관문(`internal/ucsmxml`)이 그 밖의 요청을 막습니다. 기대값은 `bios_json/standard_map/standard_bios_map_xml.tsv`(`-xml-map` 으로 변경). 결과는 `results/<일시>/ucsm_result.tsv`. 판정 OK·FAIL·UNVERIFIED·MAPPING_MISSING·UNSUPPORTED, 호스트 오류 AUTH_FAIL·UNREACHABLE·TIMEOUT·XML_ERROR. 쓰기·재부팅·`-retry-from`·`retry.txt` 는 없습니다. 테스트: `cmd/biostool/ucsm_test.go`, `internal/ucsmxml/ucsmxml_test.go`.
- **연구용 프로파일 `profiles/VM_research.tsv`**: `bios_json/` 조사 데이터에서 만든 Redfish(JSON) 표준값입니다. 모든 행 `verified=N` 이라 점검만 되고 설정은 되지 않습니다. `bash bios_check.sh --profile VM_research`.
- **프로파일 오프라인 검증**: `bios_json/standard_map/validate_profile.ps1` → `profile_validation.md` (120행: OK 89, 허용값 확인 불가 29, Supermicro UI 이름만 2, 오류 0). 데이터와의 대조일 뿐 실장비 확인이 아닙니다.

- **2순위(예비) 계정 `/etc/rf_tool/rf.conf`**: 평문 `key=value`(`<vendor>_user`·`<vendor>_pass`, vendor = dell hpe lenovo supermicro cisco), 경로는 `-rf-conf <경로>` 또는 환경변수 `RF_TOOL_CONF`, 파일이 없으면 기능 꺼짐, 그룹/기타 권한이 열려 있으면 경고. **1순위(`bios.conf` user + `pass.enc`) 로그인이 401 일 때만** 비인증 ServiceRoot 로 벤더를 식별해 그 벤더 예비 계정으로 **1회** 다시 시도하며, 벤더 식별 불가·예비 계정 없음·인증 외 실패(연결 거부·타임아웃·5xx)에는 시도하지 않습니다. 둘 다 실패하면 최종 `AUTH_FAIL`. 결과는 리포트 `[2순위 계정(rf.conf)으로 접속] N대` 구역, 결과 폴더 `fallback_login.txt`(호스트 이름만, 0600), `run_info.txt` 건수 줄. `result.tsv` 열(14열)은 그대로이고 비밀번호는 어디에도 출력되지 않습니다. 한 번 실행(점검+Y 적용)에 같은 BMC 로 인증 시도가 최대 4회(단계마다 2회)입니다. 구현 `cmd/biostool/fallback.go`.
- **테스트케이스 `testcase/rf_fallback_case.sh`**: mock BMC(127.0.0.1:19101~19106)로 1순위 정상·예비 계정 접속·둘 다 실패·접속불가·값 불일치·표기 선택 6개 시나리오를 PASS/FAIL 로 판정합니다. Go 불필요, 실제 BMC 접속 없음, `/etc/rf_tool/rf.conf` 를 건드리지 않습니다(`-rf-conf` 로 임시 파일 지정). `RF_FORCE_OS6=1` 로 `*_os6` 바이너리 강제. 설명 `testcase/README.md`. 시나리오 2 는 mockbmc 에 `-open-serviceroot` 플래그가 있을 때만 활성(없으면 SKIP).
- **mock 서버 바이너리 커밋**: `bin/mockbmc`·`bin/mockbmc_os6`(정적 빌드)를 저장소에 포함해 폐쇄망에서 Go 없이 시험할 수 있습니다(`.gitignore` 의 `/bin/mockbmc` 제외 삭제). `mockbmc.sh` 는 바이너리가 있으면 빌드 없이 그대로 쓰고 없을 때만 go 로 빌드합니다.

### 변경

- **설정 시 허용값 표기 선택**: 프로파일 `value` 가 `Enabled|Enable` 처럼 여러 표기면 점검은 어느 쪽이든 OK 이고, 설정(Y)·`-dry-run` 은 BMC 레지스트리가 받는 표기를 골라 보냅니다(정확히 있는 첫 표기, 없으면 대소문자만 다른 레지스트리 표기). 예: `Enabled|Enable` + Lenovo 레지스트리 `Enable|Disable` → `Enable`.
- **리포트의 오류 구역 분리**: 호스트 오류가 `[접속불가]`(UNREACHABLE·TIMEOUT·BMC_ERROR·HTTP_ERROR, 계정 문제 아님) / `[계정 오류 AUTH_FAIL]` / `[기타 오류]` 로 나뉩니다. 상태 값·`result.tsv`·`retry.txt` 규칙은 그대로입니다.
- `VM_research.tsv` 수동 보정 2건: Lenovo 값 `Enabled|Enable`·`Disabled|Disable` 둘 다 허용, SD650 V3 hyper_threading 대체 후보 `Processors_HyperThreading`. `map_bios_attrs.ps1` 로 재생성하면 덮어써집니다.
- FIRST_RUN 1번(Dell `Performance` ↔ `PerfOptimized`): 조사 데이터의 레지스트리 값이 `PerfOptimized` 이므로 데이터 기준으로 해소했습니다. 실장비 값 대조는 여전히 권장합니다.
- **Redfish `result.tsv`·`fail.tsv`·`merged_result.tsv` 에 `protocol`(항상 `json-redfish`)·`mgmt_version` 열 추가** (`bios_version` 바로 뒤, 12열 → 14열). 점검이 끝난 호스트마다 Manager 리소스를 1회 더 GET 합니다(읽기 전용 허용목록 안). 값은 Manager 의 Model·FirmwareVersion 이며 못 읽으면 `-`. v0.1.0 의 12열 결과는 `-retry-from` 이 읽어 새 형식으로 병합합니다(호환 테스트 포함). **열 위치로 `result.tsv` 를 읽는 외부 스크립트·엑셀은 수정해야 합니다.**
- UCSM system_profile 은 후보 2개(`vpCPUPerformance=hpc`, `vpEnergyPerformance=performance`)를 각각 별도 항목으로 점검합니다(매핑 TSV 에 행 추가, 한 항목에 여러 행 허용).
- 문서: README(VM_research·UCSM 사용법 2.9절, 2순위 계정 2.10절, 테스트케이스 2.11절, 서브커맨드·플래그·래퍼 표), `bios_json/standard_map/README.md`(XML 점검기 존재 반영), FIRST_RUN(확인 항목 18~25, 26~30 추가), ARCHITECTURE(`ucsm` 위치, `fallback.go`, “추후 확장”: 계정 생성 설계 메모), 사용법.txt(UCSM 절, 2순위 계정·테스트케이스 절), PR_CHECKLIST(테스트케이스 항목).

### 검증 (2026-10-10, 랩)

| 항목 | 결과 |
|---|---|
| 랩 .58 (Go 1.26.5) | `go vet ./...`·`go test ./...` 통과. `bash testcase/rf_fallback_case.sh` 통과(6개 시나리오 모두 PASS, SKIP 0) |
| 랩 .60 (Go 1.20.14) | build·vet·test 통과 (Go 1.20 호환 확인). `testcase/rf_fallback_case.sh` 를 기본 바이너리와 `RF_FORCE_OS6=1`(`*_os6`) 로 각각 통과 |
| 바이너리 재빌드 | `bin/biostool` (Go 1.26.5), `bin/biostool_os6` (Go 1.20.14), `bin/mockbmc`, `bin/mockbmc_os6` 를 다시 빌드해 함께 커밋 |
| 실제 UCSM 장비 | 조회하지 않음 (미확인) |

랩으로 소스를 보낼 때 Windows 작업 트리의 Go 파일은 CRLF 이므로 LF 로 바꿔 보내야 `TestModeSetOnlyInSetPath` 가 통과합니다(저장소 blob 은 LF).

빌드 완료 바이너리 (v0.2.0 후보, v0.1.0 항목의 해시와 별도):

| 파일 | 빌드 | 크기(바이트) | sha256 |
|---|---|---|---|
| `bin/biostool` | Go 1.26.5 | 7475362 | `1f9d439970ec34578b88cdce66746b5b80a9cd459405da07bac9438e5c23e915` |
| `bin/biostool_os6` | Go 1.20.14 | 6107136 | `39a895e198248d8ac4b262cfa89d265fa78cd95354726b642db2221a39697220` |
| `bin/mockbmc` | Go 1.26.5 | 7237794 | `41b6b9a69fbcce12e49ddf09d7823a31cfdcf55f26eeb06ee2988693dab45c97` |
| `bin/mockbmc_os6` | Go 1.20.14 | 5689344 | `8f5e997045fee74b55d71ab88bfa00b2985dcb79561a5235710caacb12c5fc49` |

### 알려진 한계 (2순위 계정)

- **실제 BMC 에서 확인하지 않았습니다.** 비인증 ServiceRoot 가 벤더(`Vendor`·`Oem`)를 돌려주는지, 보안 강화로 ServiceRoot 가 막힌 BMC, 로그인 실패 횟수와 잠금 정책, Basic 인증 BMC 에서의 동작은 [FIRST_RUN.md](FIRST_RUN.md) 26~29 항목입니다. 레지스트리 허용값의 실제 표기(`Enable` 대 `Enabled`)는 30 항목입니다.
- 비인증 ServiceRoot 가 막힌 BMC 는 벤더를 식별하지 못해 예비 계정을 시도하지 않고 `AUTH_FAIL` 로 남습니다(다른 벤더 계정을 돌려가며 넣지 않음).
- 계정 생성·변경의 한계는 아래 “알려진 한계 (계정 생성·변경)” 입니다.
- `mockbmc` 에 `-open-serviceroot` 플래그를 추가했습니다(비인증 ServiceRoot 에 응답, 벤더 식별·2순위 계정 시험용). 기본은 꺼짐(401)이며 실제 BMC 의 설정과는 무관한 mock 전용 옵션입니다.

### 알려진 한계 (계정 생성·변경)

- **실제 BMC·UCSM 에서 확인하지 않았습니다.** 벤더별 생성·변경 방식은 `id_json` 의 문서·공개 스크립트 근거입니다(사양상 미확인 방식도 Y 로 적용되며 결과 `warn` 열에 경고). 첫 실장비는 1대부터 `-dry-run` 으로 본문을 확인하십시오. [FIRST_RUN.md](FIRST_RUN.md) 31~37.
- 관리자 역할 이름은 BMC 의 Roles 컬렉션에서 `Administrator`→`Admin`→`admin` 순으로 고르고, 읽지 못하면 후보를 순서대로 보냅니다. Cray 의 `PasswordChangeRequired` 는 `false` 를 보내지만 실제 동작은 미확인입니다.
- 비밀번호 정책(길이·복잡도)은 사전 검사하지 않고 BMC 응답(`REJECTED`)으로만 압니다. `$ ? =` 처럼 UCSM 이 금지하는 문자도 마찬가지입니다.
- 생성 검증은 로그인 1회입니다. BMC 가 새 계정을 잠시 뒤에 활성화하면 거짓 `VERIFY_FAIL_ROLLED_BACK` 이 날 수 있습니다(다시 실행하면 됨).
- 붙여넣기 거부는 완전하지 않습니다(터미널이 키 입력과 붙여넣기를 구분하지 못하는 환경에서는 안내 문구만 있음).
- 동시 처리 100(최대 300)은 실측 값이 아니며 첫 실행 로그로 조정합니다. RHEL6 커널(2.6.32)·bash 4.1 에서의 동작은 확인하지 못했습니다.
### 알려진 한계 (UCSM 및 프로파일)
- **실 UCSM 미확인.** 응답 형태(outCookie·errorCode), 블레이드 dn·PID 매칭(`ucsmModelKey`), 실효 설정 위치(`<blade dn>/bios/bios-settings/…`)는 [FIRST_RUN.md](FIRST_RUN.md) 18~20 항목입니다.
- BIOS 정책(`bios-prof-*`)은 보지 않고 서버에 실제 적용된 값만 봅니다.
- `biosPlatformDefaults` 를 조회하지 않습니다. 현재값이 `platform-default` 이면 OK 로 추측하지 않고 사유에 확인 안내를 붙입니다.
- 세션 갱신이 없습니다. 긴 실행 중 세션 만료(errorCode 552 등)는 `UNSUPPORTED` 로 보일 수 있습니다.
- 재시도 기능이 없습니다(실패한 UCSM 은 `-user` 로 다시 실행). 랙 서버(computeRackUnit)는 대상이 아닙니다.
- 매핑 TSV 의 모든 행이 `verified=N` 이라 불일치는 `FAIL` 이 아니라 `UNVERIFIED` 로 보입니다.
- `VM_research.tsv` 의 값(Lenovo `Enabled`/`Enable`, HPE iLO6 허용값, SR630/650/SD530 `llc_prefetch`, SR645/675 V3 `sub_numa_cluster`, XE9780 `LogicalProc`, Supermicro 속성명)은 실장비에서 확인하지 않았습니다. FIRST_RUN 19~25 항목입니다.

## [0.1.0] - 2026-10-08

BMC(Redfish)의 BIOS 표준값을 점검하고 승인된 항목만 Pending 으로 설정하는 도구의 첫 릴리스입니다. **실제 BMC 에 접속해 본 적이 없고** 모든 검증은 합성 mock 과 `-from-dump` 로 했습니다(아래 “알려진 제한”, [FIRST_RUN.md](FIRST_RUN.md)).

### 추가 — 단계별 기능

- **골격(단계 1)**: `bios.conf`(알 수 없는 키·conf 안의 비밀번호는 오류)·`user.txt` 파싱, `/etc/hosts` 의 `이름-m` 해석(`IP`/`이름-m`/`이름`, `:포트`), AES-256-GCM 비밀번호 암복호화(`pass.enc`+`key.bin`, 패스워드변경자동화 방식 재사용), bash 래퍼(`bios_check.sh`·`xml.sh`·`all_bios_check.sh`·`encrypt.sh`·`lib.sh`)와 빌드 스크립트. 래퍼는 커널 메이저 버전으로 `bin/biostool`(os8)/`bin/biostool_os6`(RHEL6)을 자동 선택하고 RHEL6 의 bash 4.1 에서 동작.
- **Redfish 클라이언트(단계 2)**: 호스트당 세션 1회·자기 세션만 삭제, **호출 허용목록**(`checkAllowed`/`checkBody`: GET 은 로그·Actions 제외, POST 는 세션과 Dell 설정 Job 만, PATCH 는 `Bios/Settings|Pending` 만, DELETE 는 자기 세션만), 타임아웃·재시도(GET·세션 연결 실패만)·지수 백오프, 401 즉시 중단·재시도 금지, 오류 상태 분류(`UNREACHABLE`·`TIMEOUT`·`BMC_ERROR`·`AUTH_FAIL`·`UNSUPPORTED`·`HTTP_ERROR`), 오류 상세의 비밀번호·토큰 마스킹. 보안 리뷰 지적(F1~F9) 반영.
- **mock BMC(단계 3)**: `internal/mockbmc` + `cmd/mockbmc`(`mockbmc.sh`), `testdata/` 합성 트리 6종(Dell R660, HPE DL360 Gen11/Gen10, Lenovo SR650 V3, Cisco C220 M7, Supermicro X12), 요청 기록(`Writes`·`LogHits`·`Sessions`)·장애 주입.
- **사전조사(단계 4)**: `biostool dump`/`xml.sh` — GET 전용 좁은 순회(호스트당 120경로 상한, LogServices 제외), `dumps/<vendor>/<model>/<biosver>/<host>/…` JSON 저장, `index.tsv`, 사람이 읽고 전달할 짧은 요약(`--compact` 지원).
- **점검(단계 5)**: `biostool check`/`bios_check.sh` — 프로파일 TSV(표준 4항목+추가 항목, `A|B` 허용값, 모델 정규화, `*` 폴백), 병렬 점검, 상태 분류(`OK`·`PENDING_OK`·`FAIL`·`UNVERIFIED`·`PENDING_EXISTS`·`MAPPING_MISSING`·`PENDING_NO_JOB`), 결과 TSV(`result.tsv`·`ok.txt`·`fail.tsv`·`retry.txt`·`run_info.txt`), 터미널 리포트, `-from-dump`, MAPPING_MISSING 모델의 덤프·요약 자동 저장.
- **수천 대 규모(단계 5 확장)**: `mockbmc.Farm`(가상 BMC 수천 대를 한 리스너에), 동시 접속 제어, **AUTH_FAIL 차단기**(`auth_fail_stop`, 기본 3: 처음 N 대 직렬 웜업 → 성공하면 병렬, 누적되면 중단하고 `SKIPPED_AUTH_STOP`).
- **전체 비교(단계 5b)**: `biostool allcheck`/`all_bios_check.sh` — `diff.txt` 기준 호스트와 `user.txt` 대상을 BMC 모델로 자동 분류해 같은 모델끼리 Bios Attributes 전체 비교(`ignore_attrs.txt`·내장 제외 목록), `DIFF`/`ONLY_REF`/`ONLY_HOST`, `NO_REFERENCE`·`REF_UNREACHABLE`·`REFERENCE`, 특이사항(`BIOS_VER_DIFF`·`MULTI_REF`·`DIFF_TXT_MODEL_MISMATCH`), 읽기 전용.
- **설정(단계 6)**: 점검 후 `Y` 일 때만 Pending 설정, `-dry-run`(보낼 PATCH·Job 본문 출력, 읽기 전용 클라이언트), 재조회 후 쓰기(`ALREADY_OK`·`CHANGED`·`PENDING_EXISTS`), 레지스트리 사전검증(`SKIPPED_READONLY`·`INVALID_VALUE`), 시스템 프로파일 종속 처리(`DEPENDS_ON_PROFILE`), Dell 은 BIOS 설정 Job 판별(`PENDING_NO_JOB`)·자동 생성 안 됐을 때만 Job 최대 1회, 반영 재조회(`APPLIED`/`APPLY_UNCONFIRMED`), 같은 BMC 중복 대상 방지(`DUPLICATE_TARGET`), `apply_result.tsv`·`applied.txt`·`apply_failed.txt`·`apply_requests.txt`. 보안 리뷰 지적 반영(`*` 행의 `verified=Y` 금지 등).
- **재시도(단계 7)**: `-retry-from` — 이전 결과의 `retry.txt` 대상만 다시 실행하고 `merged_*` 로 병합(이전 폴더는 읽기만, 재시도 여러 번 연쇄 가능, check·allcheck 공통).
- **빌드·배포(단계 8)**: `bin/biostool`(os8)·`bin/biostool_os6`(RHEL6, Go 1.20.x 정적)를 **저장소에 커밋**(회사 환경에 Go 없음), 두 바이너리의 mock 대상 결과 비교.
- **문서(단계 9)**: README(빌드·설치·사용·옵션·상태 코드·Disclaimer·전역 명령어), ARCHITECTURE, WORKFLOW+`workflow*.svg`, FIRST_RUN, PR_CHECKLIST, CHANGELOG, 사용법.txt, `setup.sh`(오프라인 빌드), 저장소 루트 `.github/workflows/redfish-ci.yml`.

### 안전장치

- 재부팅·즉시 적용(`ApplyTime=Immediate`)·Reset·ClearLog·로그 조회·남의 세션/Job 삭제는 **경로 자체가 허용목록에 없어** 구조적으로 불가능(전송 전 차단).
- 설정은 `set.go` 한 곳뿐이며 프로파일에서 **모델을 지정한 `verified=Y` 행**·Dell/HPE/Lenovo 만, 호스트당 PATCH 1회·재시도 없음. 남의 Pending/Job 은 건드리지 않음(`PENDING_EXISTS`).
- 비대화형 표준입력의 Y 는 N 으로 처리(`-stdin-ok` 로만 예외, 경고 출력).
- 계정 잠금 방지: AUTH_FAIL 재시도 금지 + 차단기. 비밀번호는 인자·로그·TSV 에 남기지 않고 결과 파일은 0600.

### 문서 단계에서 고친 것

- `allcheck.go` 의 `diff.txt`·`ignore_attrs.txt` BOM 제거가 `"\ufeff"` 대신 문자열 `Feff` 로 들어가 있던 버그 수정(윈도우 메모장 BOM 이 붙은 파일의 첫 줄 인식 실패, `Feff` 로 시작하는 호스트명 잘림). 테스트도 같은 오류로 통과하고 있어 함께 바로잡음.
- 내부 버전 문자열 `toolVersion` 을 `0.4.0-dev` → `0.1.0` 으로 정리(`run_info.txt`·`dump_meta.json` 에 기록되는 값).

### 알려진 제한

- **실장비 미검증.** 속성 이름·허용값·Settings 경로·Dell Job 동작은 모두 추정이며 `testdata/` 는 합성 자료입니다. 확인 항목 17건은 [FIRST_RUN.md](FIRST_RUN.md) 에 정리했고, `profiles/VM.tsv` 는 엑셀 표의 시작값(전부 `verified=N`)입니다.
- 엑셀 값 3건(Dell `Performance`↔`PerfOptimized`, HPE `ProHyperthreading` 철자, `HighPerformanceCompute(HPC)` 표기)과 Cisco CIMC/UCSM 구분, Supermicro Settings 경로, Lenovo `Bios/Pending` 은 미확정.
- 쓰기는 Dell·HPE·Lenovo 만. Cisco·Supermicro·Cisco UCSM 관리형·Supermicro X11 이하는 점검만(안 되면 `UNSUPPORTED`).
- 구형 BMC(TLS 1.0/1.1)에는 연결할 수 없습니다(TLS 1.2 이상). `UNREACHABLE` 의 원인(연결 거부·DNS·TLS 실패)은 화면·결과 파일에 나오지 않습니다.
- 실제 RHEL6 커널(2.6.32)에서의 `bin/biostool_os6` 기동은 확인하지 못했습니다(랩 커널 4.18, CentOS 6 userland 컨테이너까지 확인).
- 실제 BMC 응답 지연에서의 소요 시간은 미측정(mock 은 3,000대를 3초대에 처리).

### 검증 현황 (2026-10-08, 랩에서 실행)

| 항목 | 결과 |
|---|---|
| 랩 .58 (Go 1.25.10, Rocky/RHEL8) | `go vet ./...` 통과, `go test ./... -count=1` 통과 (최상위 테스트 210 PASS / 0 FAIL / 0 SKIP, 서브테스트 89 PASS) |
| 랩 .60 (Go 1.20.14) | `go vet ./...`·`go build ./cmd/biostool` 통과, `go test ./... -count=1` 통과 (210 PASS / 0 FAIL / 0 SKIP) → Go 1.20 호환 확인 |
| `bash setup.sh` | .58(Go 1.25.10)·.60(Go 1.20.14) 모두 성공, `vendor/` 없이 `-mod=vendor` 동작 확인. **재빌드 결과가 커밋된 바이너리와 sha256 이 같음**(.58 의 `setup.sh` = `bin/biostool`, .60 의 `build_os6.sh` = `bin/biostool_os6`; .60 에서 `setup.sh` 를 돌린 결과는 `bin/biostool_os6` 와 같음 → os6 바이너리는 Go 1.20.14 산출물). go 가 없으면 안내 후 종료코드 2 |
| 수천 대 규모 | `TestScaleCheckThousands`: mock 3,000대(미등록 30·5xx 30·FAIL 주입 90), 동시 100 — doCheck 3.16초, 힙 최대 36.2 MiB, 고루틴 최대 462 → 종료 후 3, FD 최대 212, 결과 행 수 = 호스트×항목 |
| 허용목록·로그 안전 | 보안 테스트(`redfish_security_test.go`·`set_security_test.go`) 통과, mock 의 `LogHits`=0, 호스트당 세션 생성 1·삭제 1 |
| 바이너리 e2e | 번들된 `bin/biostool` 로 mock 에 대해 점검 → dry-run → Y 적용(`APPLIED`) → 재점검(`PENDING_OK`) → 사전조사 → 전체 비교 → `-from-dump` → `-retry-from` → AUTH_FAIL 차단기를 랩에서 실행, `사용법.txt` 의 mock 연습 블록도 그대로 실행해 성공. os8 ↔ os6 바이너리의 결과 파일 일치 비교는 단계 8 구현 보고 기준이며 이번에 다시 실행하지는 않음 |
| RHEL6 userland | CentOS 6.10 컨테이너(bash 4.1.2)에서 래퍼 5종 `bash -n` 통과, `bin/biostool_os6 help` 동작(커널은 호스트 4.18) |
| **실장비** | **없음 (미검증)** |

빌드 완료 바이너리 (문서 단계에서 발견한 BOM 처리 버그·내부 버전 문자열 수정 후 재빌드한 값; `bin/biostool` = Go 1.25.10 / `bin/biostool_os6` = Go 1.20.14, 둘 다 정적):

| 파일 | 크기(바이트) | sha256 |
|---|---|---|
| `bin/biostool` | 6811832 | `fc7c54e699878f2b0ea322d27077a953396bfd1fb6a904a2e8fbde3f99d0ae67` |
| `bin/biostool_os6` | 5697536 | `258897877b97b78c21ca1e466e9bf883f1847704ea3a2ac8db8deae4d8263546` |

## 버전 태그

배포 시점마다 태그를 남기고, 태그 메시지에 CHANGELOG 의 어느 항목까지 포함하는지 적습니다. (태그는 아직 만들지 않았습니다. 커밋·푸시 후 아래 명령을 실행하십시오.)

```bash
git tag -a v0.1.0 -m "redfish BIOS 표준값 점검·설정 도구 첫 릴리스 (CHANGELOG 2026-10-08 항목)"
git push origin v0.1.0
```
