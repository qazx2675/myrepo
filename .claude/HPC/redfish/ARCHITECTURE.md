# ARCHITECTURE.md

BMC(Redfish)에 접속해 BIOS 표준값을 점검하고 승인된 항목만 Pending 으로 설정하는 도구입니다. 전체를 다 읽지 않고도 어디를 고쳐야 할지 찾을 수 있게 파일별 역할을 정리했습니다. 사용법은 [README.md](README.md), 첫 실장비에서 확인할 것은 [FIRST_RUN.md](FIRST_RUN.md) 를 보십시오.

## 폴더·파일별 역할

### Go 소스 — `cmd/biostool/` (package main, 표준 라이브러리만, Go 1.20 호환)

| 파일 | 역할 |
|---|---|
| `main.go` | CLI 진입점. 서브커맨드(`bios`·`check`·`allcheck`·`dump`·`encrypt`·`ucsm`·`account`)·공통 플래그·`usageText`, `prepare`(conf → 대상 해석 → 비밀번호 복호화), 실행 요약 출력 |
| `conf.go` | `bios.conf` 파서와 기본값(`Config`). 알 수 없는 키·비밀번호 키는 오류. 통합 점검 키 `exclude_attrs`·`allcheck_set`·`protect_attrs`·`refs_dir`·`auto_profile` 포함 |
| `account.go` | `biostool account create|change` 본체: 점검(`probeCreate`)·생성(`createOnHost`)·검증·롤백(`rollbackCreate`)·변경(`changeOnHost`), 벤더별 생성 단계(`planCreate`), 워커풀+웜업(`runAcctPool`), 결과 `account_result.tsv`·요약·Y/n |
| `account_gate.go` | 계정 쓰기 본문 검사(`checkAccountCreateBody`·`checkAccountPatchBody`): 허용 키·값·형태를 고정 (`redfish.go` `checkBody` 가 호출) |
| `account_input.go` · `account_tty_linux.go` · `account_tty_other.go` | 새 비밀번호 터미널 입력(`*` 표시·2번 입력·붙여넣기 거부), csh 명령 인용(`cshQuote`), 입력 버퍼 비우기. raw 입력은 리눅스 전용(`syscall` ioctl) |
| `account_ucsm.go` | Cisco UCSM 사용자 생성·변경(`-ucsm`), `internal/ucsmxml` 계정 모드 사용 |
| `account_check.go` | `check`·`bios`(TUI·`-plain`) 끝에서 2순위로 접속한 호스트에 계정 생성 제안(`offerAccountCreate`, 기본 Y, `user2` 도 함께) |
| `profile.go` | `profiles/<이름>.tsv` 로드·검증(`*` 행의 `verified=Y` 거부, 중복 검사), `rowsFor`(모델 정확 일치 우선 → `*`). 주석 줄 `# [redfish]` / `# [xml]` 로 구역을 나눠 `[xml]` 행(attribute = `클래스.속성`, 벤더 CISCO, `*` 모델 불가)은 `Profile.XML`(UCSM 기대값), `# UNMAPPED` 주석은 `MAPPING_MISSING` 사유로 읽음 |
| `modelnorm.go` | 모델명 정규화 `normalizeModel`(프로파일의 model 열과 BMC 가 보고한 Model 을 같은 키로 맞추는 **한곳의 규칙**, allcheck 도 사용) |
| `resolve.go` | 대상 목록 해석: `user.txt` 파싱, `/etc/hosts` 에서 `이름-m` 조회, `:포트`, 재시도 목록(`loadRetryList`), `NO_HOSTS_ENTRY`. `classifyTargets` 가 대상을 Redfish / XML 로 나눔(`xml_host_prefix` 로 시작하는 이름과 `-user-xml` 목록 = XML, IP 는 `-user-xml` 에 있을 때만) |
| `cryptutil.go` | AES-256-GCM 비밀번호 암복호화(`pass.enc`+`key.bin`, 0600), `encrypt` 서브커맨드의 본체 |
| `redfish.go` | **BMC 와 통신하는 유일한 HTTP 클라이언트.** 세션 1회·`checkAllowed`(호출 허용목록)·`checkBody`(본문 검사)·타임아웃·재시도·백오프·401 즉시 중단·오류 상태 분류(`UNREACHABLE`·`TIMEOUT`·`BMC_ERROR`·`AUTH_FAIL`·`UNSUPPORTED`·`HTTP_ERROR`)·비밀번호 마스킹. PATCH 본문 검사 `checkAttributes` 가 위험 속성명 외에 내장 보호 속성(`protect.go`)도 거부 |
| `inspect.go` | 시스템 판별 `detectSystem`(ServiceRoot → Systems → Bios → Settings 경로 → 속성 레지스트리 위치), Bios 속성·레지스트리 해석, 벤더 정규화 `normalizeVendor`, **표준 4항목 이름 후보 키워드 `stdKeywords`** |
| `sweep.go` | 호스트 순회 워커풀과 **AUTH_FAIL 차단기**(웜업 직렬 → 병렬, `SKIPPED_AUTH_STOP`). check·dump·allcheck·set·ucsm 이 공유. 호스트 처리 중 패닉은 `onPanic` 으로 그 호스트만 오류(`BMC_ERROR`, `panicDetail`)로 남기고 나머지를 계속함(콜백이 없으면 모두 끝낸 뒤 호출한 goroutine 에서 다시 패닉) |
| `check.go` | `check` 본체: 호스트별 판정(`judge`), 상태 상수(OK·FAIL·UNVERIFIED·PENDING_*·MAPPING_MISSING·NO_DUMP), Dell BIOS Job 판별, `-from-dump` 읽기, `result.tsv`·`ok.txt`·`fail.tsv`·`retry.txt`·`run_info.txt` 기록 |
| `report.go` | `check` 의 터미널 리포트(`[OK]`·`[FAIL 상세]`·`[설정 불가]`·`[기타 오류]`)와 **Y/N 확인**(`confirmApply`, 비대화형 입력은 N, `-stdin-ok`) |
| `set.go` | **BMC 에 쓰는 유일한 경로.** 입력 재검증·벤더 제한·**보호 속성 검사(걸리면 호스트 통째로 `PROTECTED_ATTR`, PATCH 직전 본문 재검사)**·재조회·레지스트리 사전검증(전체비교 항목은 Enumeration·Integer·Boolean 만)·시스템 프로파일 종속 처리·PATCH 1회·Dell Job POST·반영 재조회, `-dry-run`(`buildPlan`/`writePlan` 공유), `apply_*` 결과 파일과 설정 리포트 |
| `dump.go` | `dump`(`xml.sh`) 본체: GET 전용 좁은 순회, 호스트당 경로 상한, JSON 저장(`safeSeg`/`dirPart`), `index.tsv`, 사람이 읽는 요약 |
| `trs_cisco.go` | `trs-cisco`(`trs_cisco.sh`) 본체: Cisco 장비 UNSUPPORTED·XML 실패 원인 진단 — trs 의 네트워크·인증 단계를 재사용(`trsMain`·`newTrsHost`)하고 Cisco Redfish 구조(System Bios 링크·Oem·Chassis·Manager·레지스트리·펌웨어 목록·하위 리소스 탐색)와 XML API(쿠키 없는 확인 요청 → 있을 때만 aaaLogin 1회)를 보고 `ciscoDiagnose` 가 종합. XML 진단 요청은 `ciscoRaw` 허용 3종만 |
| `trs.go` | `trs`(`trs.sh`) 본체: BMC 접속 장애(403 등) 단계별 진단 — TCP/TLS → 비인증 ServiceRoot(헤더 변형) → 인증(Basic 1회+세션 1회, 401 중단) → 인증 후 읽기 → 403 경로 변형 비교. `trsDiagnose` 가 원인 추정 규칙(새 장애 유형은 여기에 추가), `trs4xx` 표가 4xx 코드별 설명(코드 추가는 이 표). GET/HEAD/OPTIONS·세션 생성/삭제만 허용 |
| `allcheck.go` | `allcheck`(`all_bios_check.sh`) 본체: 기준(`diff.txt`)·제외 속성(`ignore_attrs.txt`)·모델별 분류·Attributes 전체 비교·`all_diff.tsv`·`summary.tsv` |
| `allreport.go` | `allcheck` 의 터미널 리포트(모델 그룹·속성별 집계·특이사항·분류 불가) |
| `allexport.go` | `allcheck -export-attrs`: 읽은 BIOS 속성을 호스트별 JSON(`attrs/<호스트>.json`)으로 저장 |
| `unified_cmd.go` | **`biostool bios` 서브커맨드**(`bios.sh` 의 본체): 플래그(`-profile`·`-user`·`-ref`·`-ref-json`·`-diff`·`-ignore`·`-plain`·`-auto`/`-auto-check <user> <targets>`·`-res-dir`), 대화형이면 TUI 흐름으로, 아니면 평문 출력(읽기 전용), `-auto` 는 `check.res_<user>` 기록. 비대화형에는 설정 경로가 없음 |
| `unified.go` | **수집 통합 엔진** `runUnified`: 호스트당 1회 접속(읽기 전용)으로 프로파일 판정 + BIOS 전체 속성 스냅샷을 함께 얻고, `Compare` 로 대상에 다시 접속하지 않고 전체비교(기준 = 비교서버 hostname / refs JSON / `diff.txt`, 같은 모델끼리만, 속성 이름 목록 검사 `sameAttrNames`), `WriteOutputs` 로 CSV 저장, `listRefJSON` 으로 refs 폴더 목록 |
| `xmlhost.go` | **Cisco XML 혼합 대상 점검**(`biostool bios`·`check`, 분류는 `addXMLRouting`(main.go)이 켬): XML 대상 1대를 `ucsmCheckHost`(ucsm.go)로 읽어 프로파일 `# [xml]` 구역으로 판정(`checkXML`). 서버가 여럿이면 서비스 프로파일(`lsServer`) 이름 = 호스트 이름인 서버를 고르고 없으면 모두 `<호스트>/<서버 dn>` 으로 보고. 로그인 전 실패(AUTH_FAIL 제외)면 그 호스트만 Redfish 로 1번 폴백(비고 `XML 실패 → Redfish`). 전체비교·설정에서는 `StatusXMLReadOnly`(`XML_READONLY`)로 제외. `allcheck` 는 `xmlProbe`(aaaLogin·aaaLogout 만)로 XML 장비인지만 보고 `XML_READONLY`, 로그인 전 실패면 Redfish 로 비교 |
| `resultline.go` | 화면·CSV 공통 결과 줄 `ResultLine`, 태그 3종(`tagFor`: OK / difference / N/A)·`showValue`(`-`→`(N/A)`), 벤더 → 관리 컨트롤러 그룹 나누기 `groupLines`(BIOS 속성 이름 목록이 같으면 한 그룹으로 합침) |
| `resultconv.go` | 프로파일 점검 결과(`hostCheck`)와 전체비교 결과(`allHost`)를 `ResultLine` 으로 바꿈 (비고 = 원래 상태명) |
| `lineout.go` | 색상 줄 렌더러 `formatLine`(`[태그] host↹ip↹…↹기준값↹현재값↹비고`, 기준값은 항상 초록, 호스트 오류는 빨강), `NO_COLOR`·tty 판단 `colorEnabled`. `lineSafe` 가 BMC·호스트에서 온 글자의 제어 문자를 공백으로 바꿈(터미널 이스케이프 주입 방지) |
| `csvout.go` | CSV 4종 저장(UTF-8 BOM, 0600): `summary.csv`·`wide_hosts_<벤더>_<그룹>.csv`·`wide_attrs_<벤더>_<그룹>.csv`·`long.csv` (`profile_`/`compare_` 접두어). 모든 칸은 `csvSafe` 를 거쳐 수식 주입(`=` `+` `@` TAB CR, 음수가 아닌 `-` 시작)을 `'` 로 막음 |
| `checkres.go` | auto_setup 이 읽는 `check.res_<user>` 한 줄 결과(`host: OK` / `host: FAIL difference N건 (...)` / `host: FAIL N/A 접속실패(...)`) 생성·기록. `removeCheckRes`(시작 때 이전 결과 삭제)·`writeFileAtomic`(임시 파일 + rename)·`lineSafe` 로 옛 결과를 읽거나 반쯤 쓴 파일을 보지 않게 함 |
| `progress.go` | 진행 표시 `[1/4 설정 확인] [2/4 접속·조회 123/1000 · OK … difference … N/A … 오류 …]`: 터미널은 한 줄 제자리 갱신, 비대화형은 10초마다 한 줄, TUI 안에서는 `TUIJob` 으로 전달 |
| `tui_keys.go` | TUI 키 파서(방향키·PgUp/PgDn·Home/End·Shift+Tab·Ctrl 키·UTF-8 문자) |
| `tui_tty_linux.go` · `tui_tty_other.go` | raw 터미널·창 크기·SIGWINCH(리눅스, `syscall` 만 사용) / 리눅스가 아닌 개발 빌드용 자리 채움(평문 출력으로 대신) |
| `tui_render.go` | 화면 조각을 만드는 순수 함수: 한글 표시 폭, 색, 결과 줄·요약 표, 탭 줄, 키 안내 줄바꿈, 도움말 텍스트(`tuiHelpLines`), 비 tty 평문 출력 |
| `tui_view.go` | TUI 본체: 대체 화면·제자리 갱신, `Browse`(결과 보기, ←→ 벤더 탭·Tab 그룹 탭·필터 a/o/f/n/s·`e`·`?`·`q`)·`Ask`·`InputLine(s)`·`Choose`·`Progress`, Ctrl+C/Ctrl+Z 무시(“종료는 q 를 두 번 누르세요”)·Ctrl+X 작업 취소. 벤더 줄 `q` 두 번 종료(다른 키는 확인 취소), `Ask` 는 질문 전에 쌓인 키를 버리고, 취소 대기 중 두 번째 시그널은 터미널을 되돌리고 바로 종료(`tuiExit`), `Progress` 의 패닉은 호출 goroutine 에서 다시 패닉 |
| `tui_flow.go` | `biostool bios` 대화형 흐름: 대상 입력 → 프로파일 선택 → 접속·조회 → 결과 화면(Tab 으로 [프로파일 점검] ↔ [전체 설정 비교], 처음 넘어갈 때 기준 선택·비교, r 로 기준 다시 선택) → 벤더 줄에서 q 두 번(또는 e) → 수정 확인 1(차이 항목 전부 `현재값 -> 변경값 (수정대상)`/`(수정제외: 사유)`, “위 수정대상을 적용할까요?”) → 수정 확인 2(수정제외가 있을 때만, “수정제외 항목을 확인했습니다. 진행할까요?”) → 남의 Pending 질문 → 설정·재조회 → 요약. `setStep` 이 이 순서를 맡고 n/Esc/Ctrl+X 면 아무것도 쓰지 않고 결과 보기로 돌아옴. `declined`/`applied` 로 종료 때 재질문·재설정을 막고, 흐름 중 패닉이어도 터미널을 되돌림 |
| `setplan.go` | **설정 계획·적용**: `buildSetPlan`(BMC 접속 없음. 프로파일 `verified=Y` difference + `allcheck_set=on` 일 때만 전체비교 difference, 충돌 `[충돌]`, **보호 속성(`protect.go`)은 항목이 되지 않고 수정제외**, 제외 목록·위험 속성·N/A·쓰기 불가 벤더·BIOS 버전이 다른 비교서버(`BIOS_VER_DIFF`) 제외, 시스템 프로파일 성격 속성이 여럿이면 프로파일 행만, 남의 Pending 목록(Dell 은 남의 Pending 이 있으면 호스트 제외). 확인 화면 줄 `ConfirmLines`/`ExcludedLines`) / `applySetPlan`(`set.go` 의 `runHost` 를 그대로 타서 재조회·레지스트리 검사·PATCH 1회·Pending 재조회. 재부팅 없음, TUI 흐름에서만 호출) |
| `protect.go` | **절대 바꾸지 않는 속성(보호 속성)**: 내장 패턴 `builtinProtect`(iSCSI/IQN·IP/네트워크·이름/식별·부팅(BootMode·PXE·URL/HTTP 부팅 포함)·보안(SecureBoot·TPM·비밀번호)·스토리지(RAID·SATA·NVMe·VMD)·NIC/LOM/OCP/WoL/OptionRom/Slot)은 코드에 고정이라 conf 로 끌 수 없고, `bios.conf` `protect_attrs` 는 `newProtectSet` 으로 더하기만 함. `match`/`why` 가 `setplan.go`·`set.go`·`redfish.go` 세 곳에서 쓰임 |
| `fallback.go` | **2순위(예비) 계정.** `/etc/rf_tool/rf.conf`(`-rf-conf`·`RF_TOOL_CONF`) 로드·검증(`loadRFConf`·`parseRFConf`), 비인증 ServiceRoot 로 벤더 식별(`vendorFromServiceRoot`), 1순위가 401 일 때만 1회 재로그인하는 `loginWithFallback`. 비밀번호는 클로저 안에만 두고 출력하지 않음 |
| `retry.go` | `-retry-from`: 이전 결과 읽기·검증, 재시도 대상만 다시 실행한 결과를 이전 결과와 병합(`merged_*`) |
| `*_test.go` | 단위·통합 테스트. 보안 규칙은 `redfish_security_test.go`·`set_security_test.go`·`protect_test.go`(보호 패턴이 `profiles/*.tsv` 속성을 막지 않는지 포함), 수정 확인 흐름은 `setstep_test.go`·`setplan_test.go`·`tui_flow_test.go`, 수천 대 시험은 `scale_test.go`, mock 연동은 `mock_integration_test.go` |

**Cisco UCSM(XML API) 점검 — `ucsm.go` + `internal/ucsmxml/`** (`biostool ucsm`, 래퍼 `ucsm_check.sh`, 읽기 전용·설정 기능 없음). 대상은 블레이드 BMC 가 아니라 UCSM(FI VIP) 주소이고, 기대값은 프로파일의 `# [xml]` 구역(`-profile <이름>`, 기본 VM)입니다. `ucsm.go` 는 `# [xml]` 구역 해석(`ucsmMap`·`addProfileRow`, `# UNMAPPED` 줄 포함), 모델 키 `ucsmModelKey`(TSV `UCS B200 M5` ↔ PID `UCSB-B200-M5` 를 맞추는 한곳의 규칙), 서버별 실효값 수집(dn 이 `<서버 dn>/bios/bios-settings/…` 인 인스턴스만, BIOS 정책 `bios-prof-*` 는 무시), 판정(OK·FAIL·UNVERIFIED·MAPPING_MISSING·UNSUPPORTED, 호스트 단위 AUTH_FAIL·UNREACHABLE·TIMEOUT·XML_ERROR)과 터미널 요약을 맡고, 호스트 순회·AUTH_FAIL 차단기는 `sweep.go`, 결과 폴더·0600 쓰기는 `check.go` 의 `newRunDir`·`writeTSV` 를 그대로 씁니다. UCSM 과의 통신은 **`internal/ucsmxml` 패키지만** 하며, 공개된 호출은 `Login`·`ResolveClass`·`Logout` 뿐이고 모든 요청은 전송 직전 단일 관문 `gate` 를 지납니다: 루트가 `aaaLogin`·`configResolveClass`·`aaaLogout` 이 아니거나, 자식 요소·추가 속성·남의 쿠키·`inHierarchical≠false`·허용목록(`computeBlade`·`computeRackUnit`·`lsServer` + 프로파일 `# [xml]` 의 `bios*` 클래스) 밖 classId 면 보내지 않고 `ErrNotAllowed`(`configConfMo`·`configResolveDn` 등 쓰기·임의 조회는 만들 수 없음). 별도 패키지로 둔 이유는 `redfish_test.go` 의 구조 가드(“`cmd/biostool` 에서 HTTP 는 `redfish.go` 만”)를 유지하면서 UCSM 전송 경로를 패키지 경계로 막기 위해서입니다. `biostool bios` 의 XML 대상(`xmlhost.go`)도 같은 `ucsmCheckHost`·`internal/ucsmxml` 을 재사용하므로 읽기 전용 관문이 그대로 적용됩니다. `ucsm` 서브커맨드의 결과는 Redfish 의 `result.tsv` 와 섞지 않고 `results/<일시>/ucsm_result.tsv` 에 씁니다 — 열: `ucsm_host ip protocol(xml) mgmt_version(UCSM) server_dn vendor model std_name xml_class attribute expected current result` (호스트 단위 오류는 `server_dn`·`std_name` 이 `-` 인 1행, serial 등 식별정보 없음). 테스트는 `ucsm_test.go`(가짜 UCSM 판정·AUTH_FAIL 1회·허용목록·비밀값 노출)와 `internal/ucsmxml/ucsmxml_test.go`(관문·마스킹).

### mock BMC (개발·검증 전용, 배포 대상 아님)

| 경로 | 역할 |
|---|---|
| `internal/mockbmc/mockbmc.go` | Redfish BMC 를 흉내 내는 mock 서버: 트리 파일 서빙, 세션, Pending 병합, 레지스트리 검사, 요청 기록(`Calls`·`Writes`·`LogHits`·`Sessions`), 장애 주입 |
| `internal/mockbmc/farm.go` | `Farm`: 한 TLS 리스너로 가상 BMC 수천 대(127.0.x.y)를 흉내 냄(트리 공유, 호스트별 상태만 개별). 리눅스 전용 |
| `cmd/mockbmc/main.go` | `mockbmc` 실행 파일(`mockbmc.sh` 가 실행, 바이너리가 없을 때만 빌드). 종료 시 `MOCKBMC_SUMMARY` 집계 출력 |
| `internal/mockucsm/` | Cisco UCSM/CIMC XML API 를 흉내 내는 mock 서버: `/nuova` 에서 `aaaLogin`·`configResolveClass`·`aaaLogout` 만 응답(그 밖은 errorCode 로 거절), JSON 으로 서버(dn·class·model·sp_name·bios) 정의. 합성 값 |
| `cmd/mockucsm/main.go` | `mockucsm` 실행 파일(`testcase/demo_env.sh` 가 `bin/mockucsm`(`_os6`)을 기동. 소스를 고치면 `go build -o bin/mockucsm ./cmd/mockucsm` 후 `_os6`(Go 1.20)도 재빌드) |
| `testdata/` | mock 용 **합성** Redfish 트리 6종(Dell·HPE×2·Lenovo·Cisco·Supermicro)과 테스트용 프로파일 `testdata/profiles-test/VM.tsv`. 실제 덤프가 아님 → [testdata/README.md](testdata/README.md) |

### 데이터·설정·스크립트·산출물

| 경로 | 역할 |
|---|---|
| `profiles/VM.tsv` | 표준값(프로파일). 엑셀 표 시작값, 전부 `verified=N`. 프로파일 추가 = 파일 추가 |
| `profiles/VM_research.tsv` | 표준값(프로파일, 연구용). `bios_json/` 조사 데이터에서 생성(`map_bios_attrs.ps1`), 전부 `verified=N` |
| `bios.conf.example` · `user.txt.example` · `diff.txt.example` · `ignore_attrs.txt.example` | 설정·입력 파일 예시(실제 값은 `.gitignore` 로 커밋 금지) |
| `refs/` | 전체비교 기준으로 고를 속성 JSON 폴더(`bios.conf` 의 `refs_dir`). `bios_export.sh` 로 뽑은 `attrs/<호스트>.json` 을 복사해 둠(실제 값이라 커밋 금지) |
| `bios.sh` | **통합 진입점**(TUI·`-plain`·`-auto`/`-auto-check`): 폴더 이동 → 바이너리 선택 → 필요 파일 확인 → `biostool bios`. `-auto` 는 호출한 폴더 기준으로 targets 경로·`-res-dir` 를 넘김. 첫 인자가 기능 이름(`account` `passwd` `trs` `trs-cisco` `dump` `export` `ucsm` `check` `allcheck` `encrypt` `update`)이면 해당 래퍼로 넘김(새 기능 래퍼를 만들면 여기 `case` 에도 추가) |
| `bios_check.sh` | 옛 이름 래퍼(프로파일 메뉴, 파일 확인) → `biostool check`. 옛 전용 옵션(`-dry-run`·`-no-prompt`·`-stdin-ok`·`-retry-from`·`-list-max`·`-fail-max`)이 없고 프로파일을 줬거나 터미널이면 `bios.sh` 로 넘김 |
| `xml.sh` | 사전조사 래퍼 → `biostool dump -targets …` |
| `ucsm_check.sh` | Cisco UCSM 읽기 전용 점검 래퍼 → `biostool ucsm` (위 “Cisco UCSM” 단락 참고) |
| `all_bios_check.sh` | 전체 비교 래퍼 → `biostool allcheck`. 인자 없이 터미널에서 실행하면 `bios.sh` TUI 로 넘김(옛 1.체크/2.전체비교 메뉴는 `-diff-max`·`-list-max`·`-export-attrs` 를 줄 때만) |
| `bios_export.sh` | 비교서버·대상 BIOS 속성 JSON 추출 래퍼 → `biostool allcheck -ref -export-attrs` |
| `encrypt.sh` | 비밀번호 암호화 래퍼(비밀번호는 파이프로만 전달) → `biostool encrypt` |
| `lib.sh` | 래퍼 공용 함수: OS(커널 메이저)별 바이너리 선택 `select_bin`, `conf_val`. bash 4.1 호환 |
| `mockbmc.sh` | mock BMC 기동 래퍼(`bin/mockbmc`(`_os6`) 가 있으면 그대로 사용, 없을 때만 go 로 빌드) |
| `testcase/rf_fallback_case.sh` | 2순위 계정·접속불가 구분·설정 표기 선택을 mock 으로 직접 시험하는 케이스(Go 불필요, `bin/mockbmc` 사용). 설명 `testcase/README.md` |
| `setup.sh` · `build.sh` · `build_os6.sh` | 오프라인 빌드 / os8 빌드 / os6(RHEL6, Go 1.20.x) 빌드 |
| `trs.sh` | `biostool trs` 래퍼: user.txt(관리망 IP)로 접속 장애 진단, 결과 `results/<일시>_trs/` |
| `update.sh` | 설치 폴더(`TARGET`, 고정 `/root/server_vm_python/redhfish/V2`) 갱신: ZIP 풀기 → 백업 → `KEEP` 외 항목 교체 → `profiles/` 파일별 병합(`.update_profiles.sha`) |
| `bin/biostool` · `bin/biostool_os6` | **빌드 완료 바이너리(커밋 대상).** 회사 환경에 Go 가 없어 이것이 곧 배포본. 소스를 고치면 둘 다 재빌드해 함께 커밋 |
| `bin/mockbmc` · `bin/mockbmc_os6` · `bin/mockucsm` · `bin/mockucsm_os6` | mock 서버 빌드 완료 바이너리(폐쇄망 시험용으로 커밋). mock 소스를 고치면 해당 둘 다 재빌드 |
| `go.mod` | `module biostool`, `go 1.20`, 외부 의존성 없음(→ `vendor/` 불필요) |
| `.gitignore` · `.gitattributes` | 실제 호스트·계정·결과물 커밋 차단 / `.sh`·`.tsv`·`.example`·`.json` 은 LF, `bin/*` 는 바이너리 |
| `README.md` · `WORKFLOW.md` · `workflow*.svg`(`workflow_bios.svg`·`workflow_auto.svg` 포함) · `FIRST_RUN.md` · `PR_CHECKLIST.md` · `CHANGELOG.md` · `사용법.txt` · `계획서.md` · `계획서_v0.2_TUI.md` | 문서(README 4절 참고). `계획서_v0.2_TUI.md` 는 통합 점검(TUI) 확정 사양 |
| 저장소 루트 `.github/workflows/redfish-ci.yml` | 이 폴더가 바뀔 때 build/vet/test 자동 실행 |

## 수정 요청이 “OO 추가”면 볼 곳

| 하고 싶은 일 | 볼 곳 |
|---|---|
| **새 벤더·모델 추가**(점검 대상) | `profiles/*.tsv` 에 행 추가(`verified=N` 로 시작) → 속성 이름이 후보 키워드로 안 잡히면 `inspect.go` 의 `stdKeywords`, 모델명 표기가 맞지 않으면 `modelnorm.go` 의 `modelPrefixes`, 벤더 문자열은 `inspect.go` 의 `normalizeVendor`. 절차는 [FIRST_RUN.md](FIRST_RUN.md) |
| 새 모델에 **설정(쓰기) 허용** | 코드 수정 없이 프로파일 행을 `verified=Y`(모델 지정 행)로 승격. 쓰기 벤더 자체를 늘리는 것은 `set.go` 의 `setVendors` + 보안 테스트(`set_security_test.go`) |
| **새 표준 항목** 추가 | 표준 4항목을 늘리려면 `inspect.go` 의 `stdKeywords`(이름 후보 키워드)와 `profile.go` 가 쓰는 항목 목록, 프로파일 TSV 행. 표준이 아닌 **추가 항목**은 프로파일에 새 `std_name` 행만 추가하면 됨(코드 수정 불필요) |
| **상태(결과 코드) 추가·변경** | 점검: `check.go` 상수와 `judge`, 터미널 표시는 `report.go`(`reportOrder`·`otherErrOrder`), 설정: `set.go` 상수·`applyOrder`·`applyFailedStatus`, 재시도 여부는 `check.go` 의 `isRetryStatus`, 병합은 `retry.go`. 바뀌면 README 3.6절 표 갱신 |
| **안전 규칙 변경**(허용 호출·본문·위험 속성) | `redfish.go` 의 `checkAllowed`·`checkBody`·`reDangerAttr` + **보안 테스트**(`redfish_security_test.go`, `set_security_test.go`)를 먼저 고치고 통과시킬 것. 허용목록을 넓히면 PR_CHECKLIST 의 보안 항목을 반드시 확인 |
| **2순위 계정** 동작(벤더 식별 규칙·rf.conf 키·시도 조건) | `fallback.go`, 호출 지점은 `check.go`·`dump.go`·`allcheck.go`, 리포트 구역은 `report.go`. 시험은 `testcase/rf_fallback_case.sh` |
| 로그인·재시도·타임아웃·AUTH_FAIL 동작 | `redfish.go`(`Client.do`·`login`), 차단기는 `sweep.go`, conf 키는 `conf.go` |
| **bios.conf 키 추가** | `conf.go`(`Config`·`defaultConfig`·`parseConf` 의 switch), `bios.conf.example`, README 3.3절 표, `lib.sh` 가 읽는 키라면 `conf_val` 호출부 |
| 터미널 리포트 모양 | 점검 `report.go`, 설정 결과 `set.go`(`writeApplyReport`), 전체 비교 `allreport.go` |
| **통합 점검(`bios.sh`) 화면·키** 수정 | 키·탭·필터·도움말 `tui_view.go`(`navKey`·`hintItems`)·`tui_keys.go`·`tui_render.go`(`tuiHelpLines`), 질문 순서·메뉴 문구·수정 확인 1·2(`setStep`) `tui_flow.go`, 확인 화면 줄 `setplan.go`(`ConfirmLines`·`ExcludedLines`) |
| **색상 줄·태그 3종·`(N/A)` 표기** 수정 | `resultline.go`(`tagFor`·`showValue`), 색·열 순서 `lineout.go`, 상태→줄 변환 `resultconv.go` |
| **CSV 열·파일 이름·그룹 합치기** 수정 | `csvout.go`(`summaryRows`·`wideHostsRows`·`wideAttrsRows`·`longRows`), 그룹 규칙 `resultline.go`(`groupLines`) |
| **전체비교 기준**(비교서버 / refs JSON / `diff.txt`)·제외 속성 | `unified.go`(`Compare`·`compareRefs`·`readRefJSON`), conf 키 `conf.go`(`exclude_attrs`·`refs_dir`), 기준 선택 화면 `tui_flow.go`(`chooseRef`) |
| **보호 속성(절대 바꾸지 않는 속성)** 패턴 추가·조정 | `protect.go`(`builtinProtect`), 막는 곳 `setplan.go`(`protect`)·`set.go`(`runHost`·`send`)·`redfish.go`(`checkAttributes`), 시험 `protect_test.go`(패턴을 넓히면 `profiles/*.tsv` 속성이 막히지 않는지 확인). 사용자가 더하는 쪽은 conf 키 `protect_attrs` |
| **수정(설정) 규칙**: 대상 선정·충돌·Pending 질문·안전 | `setplan.go`(`buildSetPlan`·`applySetPlan`), 실제 쓰기는 `set.go`(코드 수정 시 `setplan_test.go`·`set_security_test.go`), 설정 관문 conf 키 `allcheck_set` |
| **auto_setup 연동 결과(`check.res_<user>`)** 형식·`-auto` 동작 | `checkres.go`, `unified_cmd.go`(`-auto`·`-auto-check`·`-res-dir`), 래퍼 `bios.sh`(호출 폴더 처리) |
| 진행 표시 문구·주기 | `progress.go`, TUI 안의 진행 줄 `tui_view.go`(`Progress`·`TUIJob`) |
| 결과 파일 열·이름 변경 | `check.go`(`resultHeader`·`writeResultFiles`), `set.go`(`applyHeader`·`writeApplyFiles`), `allcheck.go`(`allDiffHeader`·`summaryHeader`), `retry.go`(병합이 헤더를 검증하므로 함께) |
| 이름 해석(`user.txt` 표기) | `resolve.go` |
| 덤프 범위·요약 | `dump.go`(순회 범위·상한 `maxDumpResources`·요약), 저장 폴더 규칙 `safeSeg`/`dirPart`(mock·`-from-dump` 와 같은 규칙) |
| 새 mock 시나리오·벤더 응답 | `testdata/<트리>/…` JSON(`Oem.MockData` 표식·링크 무결성 유지) 또는 테스트 코드의 `Server.SetBiosAttr`/`Modify`/`Fault` |
| 래퍼 스크립트 | `bios.sh`·`bios_check.sh`·`xml.sh`·`all_bios_check.sh`·`bios_export.sh`·`encrypt.sh`·`lib.sh`(bash 4.1 에서 동작해야 함). Go 와 쌍이므로 플래그가 바뀌면 usage 도 함께. 옛 래퍼의 `bios.sh` 위임 조건은 각 스크립트 위쪽 주석 |
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
3. **쓰기 경로는 `set.go` 뿐** — 점검·덤프·전체 비교는 쓰기가 불가능한 읽기 전용 클라이언트(`ModeReadOnly`)이고, `-dry-run` 도 같은 읽기 전용 클라이언트라 쓰기가 구조적으로 불가능합니다. `ModeSet` 은 Y 응답 뒤 `set.go` 에서만 만듭니다. 통합 점검(`bios.sh`)의 수정 단계도 같은 경로(`setplan.go` 가 `set.go` 의 `runHost` 를 호출)이고 TUI 에서 수정 확인(“위 수정대상을 적용할까요?”, 수정제외가 있으면 “수정제외 항목을 확인했습니다. 진행할까요?” 까지)에 y 를 눌렀을 때만 실행되며, 비대화형 `-auto`/`-auto-check`·`-plain`·`-from-dump` 에는 설정 경로가 없습니다.
4. **여러 겹의 입력 재검증(`set.go`)** — 점검 결과를 그대로 믿지 않고 프로파일에서 *모델을 지정한 `verified=Y` 행*을 다시 확인, Dell·HPE·Lenovo 만, 재조회로 점검 이후 변화(`CHANGED`·`ALREADY_OK`·`PENDING_EXISTS`) 확인, 레지스트리 ReadOnly·허용값·형식 확인, 시스템 프로파일 종속은 프로파일 항목만 먼저. 호스트당 PATCH 1회·재시도 없음, Dell Job 은 자동 생성되지 않았을 때만 최대 1회.
5. **계정 잠금 방지** — `sweep.go` 의 AUTH_FAIL 차단기(처음 `auth_fail_stop` 대 직렬 접속 → 성공하면 병렬, 누적되면 중단 + `SKIPPED_AUTH_STOP`).
6. **비밀 취급** — 비밀번호는 `pass.enc`(AES-256-GCM)+`key.bin`(0600)으로만 보관, CLI 인자·로그·TSV 에 남기지 않으며 오류 상세에서도 비밀번호·토큰·Basic 헤더를 지웁니다. 결과 파일은 0600.

7. **보호 속성(`protect.go`)** — 서버 고유값·부팅·보안·RAID·PXE·NIC 속성은 프로파일이든 전체비교 값이든 설정하지 않습니다. 내장 목록은 conf 로 끌 수 없고 `protect_attrs` 는 더하기만 합니다. 막는 곳은 겹겹입니다: ① `setplan.go` 가 설정 항목에서 빼고 수정제외로 표시 ② `set.go` `runHost` 가 항목 중 하나라도 걸리면 그 호스트를 접속 없이 통째로 `PROTECTED_ATTR` ③ PATCH 직전(`send`)에 실제 본문 속성 재검사 ④ `redfish.go` `checkAttributes` 가 본문 전체 거부. 패턴을 고칠 때는 `protect_test.go`(프로파일 속성이 막히지 않는지)를 함께 통과시킬 것.
8. **확인 단계와 출력 안전** — 수정 확인 1·2 와 Pending 질문을 모두 통과해야 쓰며, 질문 전에 쌓인 키는 버립니다. 비교서버 값은 레지스트리 형식이 Enumeration·Integer·Boolean 일 때만, BIOS 버전이 같은 호스트에만 보냅니다. BMC·호스트에서 온 글자는 `lineSafe`(제어 문자 제거)와 `csvSafe`(엑셀 수식 주입 방지)를 거쳐 출력하고, `check.res_<user>` 는 시작 때 옛 파일을 지우고 원자적으로 씁니다.

작업 흐름도는 [WORKFLOW.md](WORKFLOW.md) 참고.

## 계정 생성·비밀번호 변경 (구현됨) — 쓰기 경로 규칙

확정 사양은 [ACCOUNT_SPEC.md](ACCOUNT_SPEC.md), 사용법은 README 2.12절, 흐름도는 [WORKFLOW.md](WORKFLOW.md) 6번입니다.

- **쓰기는 `account*.go` 만 합니다.** `redfish.go` 의 `ModeAccount` 에서만 계정 컬렉션 POST·계정 멤버 PATCH/DELETE 가 열리고(`reAcctColl`·`reAcctMember`), PATCH/DELETE 는 이번 실행이 `AllowAccountTarget` 으로 등록했거나(빈 슬롯·대상 계정) 자기가 POST 로 만든 멤버에만 나갑니다. BIOS 점검·설정 모드에서는 계정 경로가 계속 거부됩니다(`TestCheckAllowedAccountPaths`).
- **두 번째 계정**(`bios.conf` `user2`·`user2_role`·`pass2_file`): `account.go` 의 `acctSpec`(계정 id·비밀번호·권한) 목록을 `newAcctEnv` 가 만들고, 호스트마다 계정 칸(`newCreateHosts`)을 두어 `createGroup` 이 admin 로그인 1회로 차례로 만듭니다. 권한 → RoleId 후보는 `account_gate.go` `acctRoleIDs`, UCSM 은 `account_ucsm.go` `ucsmRole`.
- **본문은 고정 형태만**: 생성(`UserName`·`Password`·`RoleId`(`allowedRoleIDs`: Administrator/Operator/ReadOnly 계열)·`Enabled:true`·HPE `Oem` 권한), PATCH 는 비밀번호 / 빈 슬롯 생성 / 롤백 비활성 / 이름 비우기 4가지(`account_gate.go`).
- **UCSM**: `internal/ucsmxml` 의 계정 모드(`Options.Account`)에서만 `configConfMo` 가 열리고 `aaaUser` 하나의 생성(admin 역할)·비밀번호 변경(등록된 이름)·삭제(이번에 만든 이름)로 제한됩니다(`gateConf`).
- **비밀번호**: 인자·환경변수·파일로 받지 않고 출력·결과 파일·오류 메시지(`Client.AddSecret` 마스킹)에 싣지 않습니다.
- **롤백**: 이번 실행이 만든 계정만 지웁니다(원래 있던 계정은 건드리지 않음). 변경 롤백은 열려 있는 1순위 세션으로 이전 비밀번호를 되돌립니다.
- **수정 가이드**: 새 벤더 생성 방식 → `account.go` 의 `planCreate`(단계 목록)와 `account_gate.go`(허용 형태가 늘면 여기도) → `account_test.go` 에 mock 시험(`mockbmc` 계정 서비스 `internal/mockbmc/accounts.go`).

## 추후 확장 (아직 구현되지 않음)

- **rf.conf 에 admin 도 없는 BMC 에 admin 계정을 만드는 기능**: 로그인 가능한 계정이 하나도 없으면 Redfish 로는 만들 수 없으므로(IPMI·로컬 콘솔 필요) 이 도구의 범위가 아닙니다.
- **change 의 호스트별 y/n·부분 재실행**: 지금은 `account_result.tsv` 의 실패 호스트만 모아 `-user` 로 다시 실행합니다.
