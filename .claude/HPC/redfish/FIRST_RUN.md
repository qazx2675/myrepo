# FIRST_RUN.md — 첫 실장비 절차와 사람이 확인할 것

이 도구(v0.1.0)는 **실제 BMC 에 접속해 본 적이 없습니다.** 지금까지의 검증은 합성 데이터로 만든 mock BMC 와 `-from-dump` 뿐이라([README.md](README.md) 의 주의사항), 속성 이름·허용값·Settings 경로·Dell Job 동작은 모두 **추정**입니다. 새 모델 장비를 처음 만날 때마다 아래 절차로 **1대씩** 확인하고, 확인된 것만 프로파일에 `verified=Y` 로 올립니다. 모델·BIOS 버전마다 다를 수 있으므로 한 모델에서 확인했다고 다른 모델로 일반화하지 마십시오.

## A. 모델 1대씩 처음 만났을 때의 절차

실제 OS 설치 요청이 들어온 장비에서 **모델별로 한 대씩** 진행합니다. 한 번에 완성하려 하지 마십시오.

1. **준비**: 공통 계정으로 로그인할 수 있는지 확인합니다(`bios.conf` 의 `user`, `bash encrypt.sh` 로 만든 비밀번호). 비밀번호가 틀리면 `AUTH_FAIL` 로 끝나고, 같은 계정이 잠길 수 있으니 틀린 채 반복하지 마십시오. 같은 장비에 짧은 간격으로 반복 실행하지 마십시오(BMC 로그에 로그인 기록이 쌓입니다).
2. **사전조사**: `bash xml.sh <BMC 주소 또는 hostname-m>` — GET 만 보내며 덤프를 `dumps/` 에 저장하고 요약을 출력합니다.
3. **요약 전달**: 폐쇄망이라 파일을 반출할 수 없으므로 터미널 요약을 사람이 옮겨 적어 전달합니다. 전달할 것:
   - 헤더 줄의 **Vendor Model, BIOS 버전**과 `auth=` 값(`session` 이면 정상, `basic` 이면 세션 미지원 → 요청마다 로그인 기록이 남음)
   - `settings=` 의 **Settings 경로와 괄호 안 출처**(`SettingsObject` 는 BMC 가 알려 준 것, `추정` 이면 확인 필요)
   - 표준 4항목(`system_profile`, `hyper_threading`, `llc_prefetch`, `sub_numa_cluster`)의 **후보 속성 이름·현재값·`[허용값: …]`·`[ReadOnly]`**(`후보 없음` 이면 그대로 `후보 없음`)
   - `pending=` 줄(`settings_diff`·`jobs` 숫자: 정상이면 `없음`) 과 `registry=`(없으면 사전검증이 안 됨)
   - `note(N): …` 가 있으면 그 내용
4. **프로파일에 행 추가**(`profiles/VM.tsv`, 탭 구분): 요약에서 확인한 **실제 속성 이름·허용값 그대로** `verified=N` 으로 추가합니다. 허용값에 없는 값은 설정 단계에서 `INVALID_VALUE` 가 됩니다. 값은 대소문자까지 같아야 합니다. 후보 이름이 둘 이상이면 같은 `std_name` 행을 여러 줄(우선순위 순)로, 허용값이 여럿이면 `A|B` 로 적습니다(설정할 때는 첫 값을 보냅니다).
   ```text
   vendor	model	std_name	attribute	value	verified
   DELL	R660	system_profile	SysProfile	PerfOptimized	N
   ```
   모델 칸은 `xml.sh` 헤더의 Model 을 그대로 써도 됩니다(공백·하이픈·벤더 접두를 지워 비교). `*`(벤더 공통) 행은 점검 비교용이라 `verified=Y` 를 쓸 수 없습니다.
5. **점검과 dry-run**: `bash bios_check.sh --profile VM -dry-run` 으로 그 1대(`user.txt` 를 1대로)를 점검하고, FAIL 이면 출력되는 **PATCH 경로·본문·Job** 을 사람이 눈으로 확인합니다. 볼 것: 경로가 `Bios/Settings`(또는 Lenovo `Bios/Pending`)인지, 속성 이름·값이 맞는지, `ApplyTime=OnReset` 이 들어가는지, Dell 이면 Job 줄이 `TargetSettingsURI` 하나뿐인지, `DEPENDS_ON_PROFILE`·`SKIPPED_READONLY`·`INVALID_VALUE` 로 제외된 항목이 없는지, `참고: … 레지스트리 사전검증 없이` 가 없는지.
6. **1대 수동 적용 → 재부팅 → 재점검 → 직접 확인**: dry-run 본문과 같은 값을 **사람이 그 1대에 직접 적용**(BMC 웹 화면 등)하고 재부팅한 뒤 `bash bios_check.sh --profile VM -no-prompt` 로 `OK` 가 되는지 확인합니다. 그리고 BMC 화면/BIOS Setup 에서 실제 값을 **직접 확인**합니다. 이것이 속성 이름·값이 정말 유효한지 확인하는 단계입니다.
7. **`verified=Y` 승격**: 6번까지 확인된 **그 모델 행만**(모델을 지정한 행) `verified=Y` 로 바꿉니다. 승격은 반드시 사람이 합니다.
8. **도구로 같은 모델 1대 적용**: 같은 모델의 다른 한 대로 `user.txt` 를 줄여 `bash bios_check.sh --profile VM` → Y → **Pending 설정 확인(`APPLIED`) → 재부팅 → 재점검 `OK` → 직접 확인**. Dell 이면 iDRAC Job Queue 에 BIOS 설정 Job 이 있었는지(재부팅 후 완료됐는지)도 봅니다. `REJECTED`·`SET_ERROR`·`APPLY_UNCONFIRMED`·`PENDING_NO_JOB` 가 나오면 **확대하지 말고** 원인을 확인합니다.
9. **단계적 확대**: 1대 → 소수 대 → 수십 대 → 수천 대. 각 단계가 끝날 때마다 랜덤한 서버 몇 대를 직접 확인합니다.

중단 기준: `AUTH_FAIL`(계정부터 확인), 예상 밖의 `REJECTED`/`SET_ERROR`/`APPLY_UNCONFIRMED`, `PENDING_EXISTS` 가 많이 나올 때(남의 Pending 을 건드리지 않고 멈춘 것이므로 원인 확인), 한 모델에서 `MAPPING_MISSING` 이 나올 때(`results/<일시>/mapping_missing/summary.txt` 를 보고 프로파일에 행 추가).

## B. 지금까지 모인 “확인 필요” 항목

각 항목은 **무엇을 보면 되는지**와 **결과를 어디에 반영하는지**입니다. 확인이 끝나면 이 표의 상태 칸을 채우십시오.

| # | 항목 (왜 불확실한가) | 무엇을 보면 되는지 | 결과를 어디에 반영하는지 | 상태 |
|---|---|---|---|---|
| 1 | **Dell `Performance` ↔ `PerfOptimized`** — 엑셀 표의 값 표기가 둘 중 어느 쪽인지 모름 | `xml.sh` 요약의 `SysProfile` `[허용값: …]` 에 정확히 어떤 문자열이 있는지 | `profiles/VM.tsv` 의 DELL `system_profile` 행 `value`(실제 허용값만 유효, 모르면 `A\|B` 로 둘 다) | 데이터 기준 해소(레지스트리 값이 `PerfOptimized`). 실장비의 실제 값 대조는 권장 |
| 2 | **HPE `ProHyperthreading` 철자** — 엑셀의 `ProHyperthreading` 은 `ProcHyperthreading` 의 오타일 가능성 | 요약의 `hyper_threading` 후보 속성 이름 | HPE `hyper_threading` 행 `attribute`(두 철자를 후보 행 두 줄로 두면 실제 있는 쪽을 사용) | 미확인 |
| 3 | **`HighPerformanceCompute(HPC)` 표기** — HPE WorkloadProfile 허용값의 정확한 문자열(공백·하이픈·괄호·대소문자) | 요약의 `WorkloadProfile` `[허용값: …]` | HPE `system_profile` 행 `value`(값 비교는 대소문자까지 정확히) | 미확인 |
| 4 | **Cisco CIMC 단독 vs UCSM 관리형 구분** — Redfish 응답만으로 구분되는지 모름 | `xml.sh` 가 `UNSUPPORTED`/오류로 끝나는지, 시스템·매니저 응답의 모델·링크, 관리 방식을 담당자에게 확인 | 구분 가능하면 이 문서·README 에 기준 기록. 구분이 안 되면 Cisco 전체를 점검만(현재 쓰기는 이미 Dell·HPE·Lenovo 로 제한)으로 유지. 계획서 7장 갱신 | 미확인 |
| 5 | **Settings 응답 모양** — HPE 등이 `Bios/Settings` GET 에 변경분이 아니라 **전체 속성을 되풀이**하는지 | 요약의 `pending=` 의 `settings_diff` 가 변경이 없는데 0 이 아닌지, 점검에서 `PENDING_EXISTS` 가 오탐으로 많이 나오는지(현재값과 같은 Pending 값은 변경으로 보지 않음) | 오탐이면 `check.go` 의 Pending 판정(`judge`)·`set.go` 의 남의 Pending 판정 수정 + 시험 추가 | 미확인 |
| 6 | **Dell Job / ApplyTime / PATCH 응답** — iDRAC 이 BIOS 설정 Job 을 PATCH 로 자동 생성하는지, `@Redfish.SettingsApplyTime` 지원 여부, PATCH 응답 코드(200/202/204), `Location` 헤더, Job 의 `JobType`(`BIOSConfiguration`)·`JobState`(`Scheduled`/`Scheduling`/`New` 등) 문자열 | 1대 적용 후 `apply_requests.txt` 의 `결과` 줄(`Dell: PATCH 응답 HTTP 202 …` / `BIOS 설정 Job 생성 (HTTP …)`), iDRAC 의 Job Queue 화면, 재점검에서 `PENDING_OK`(Job 확인) vs `PENDING_NO_JOB` | `check.go` 의 `pendingJobStates`·`isBiosJob`, `set.go` 의 `dellJob`·`reJobURI`. **`PENDING_NO_JOB` 이 오탐이면 Y 를 누르지 말 것** | 미확인 |
| 7 | **프로파일 종속과 ReadOnly** — Dell `SysProfile`/HPE `WorkloadProfile` 이 `Custom` 이 아니면 하위 항목(`LogicalProc` 등)이 ReadOnly 가 되거나 덮어써지는지 | 요약의 `[ReadOnly]` 표시, dry-run 의 `DEPENDS_ON_PROFILE`, 프로파일을 바꾼 뒤 재부팅·재점검에서 하위 항목이 어떻게 되는지 | 표준값에서 `system_profile` 을 어떻게 둘지, `set.go` 의 `buildPlan` 의 보수적 정책(프로파일 항목 먼저, 나머지는 재부팅 후 재점검) 유지/완화 | 미확인 |
| 8 | **Lenovo `Bios/Pending`** — Lenovo 의 SettingsObject 가 `Bios/Pending` 이 맞는지 | 요약 `settings=` 줄이 `Bios/Pending (SettingsObject)` 인지(`추정` 이면 미확정) | `inspect.go` 의 Settings 경로 추정, `redfish.go` 의 PATCH 허용 경로(`Bios/Settings`·`Bios/Pending`) | 미확인 |
| 9 | **Supermicro Settings 경로·Pending 표시** (X12+) — 쓰기는 하지 않지만 Pending 을 어떻게 보여 주는지 | 요약 `settings=` 가 `없음` 인지, `pending=확인불가` 인지 | README 의 벤더별 한계 기록, Supermicro 점검 판정(`UNSUPPORTED`/`pending` 처리) | 미확인 |
| 10 | **구형 BMC TLS 1.0/1.1 불가** — 이 도구의 TLS 클라이언트는 TLS 1.2 이상만 협상(Go 기본) → 구형 BMC 는 연결 단계에서 실패해 `UNREACHABLE` 로 보임 | 같은 주소에서 `openssl s_client -connect <BMC>:443 -tls1_2` 가 되는지(도구는 `UNREACHABLE` 의 원인 — 연결 거부·DNS·TLS 실패 — 을 화면·결과 파일에 보여 주지 않으므로 직접 확인) | 지원 불가 모델 목록을 README·계획서에 기록(BMC 펌웨어 업데이트 또는 TLS 1.2 활성화가 필요). 필요하면 `redfish.go` 의 TLS 설정 | 미확인 |
| 11 | **Model 문자열 형태와 `modelnorm` 보정** — 실제 BMC 의 Model 문자열이 프로파일의 model 열과 정규화 후 같아지는지(`UCSC-C220-M7S`, `PowerEdge R660`, `ProLiant DL360 Gen11` 등) | 요약 헤더의 Model, 점검에서 `MAPPING_MISSING`(행이 없음) 이 나오는지, 전체 비교의 모델 그룹 라벨 | 프로파일 `model` 열 수정, 접두 규칙이 부족하면 `modelnorm.go` 의 `modelPrefixes`(+ 시험) | 미확인 |
| 12 | **`ignore_attrs` 보정** — 호스트마다 값이 달라야 하는 속성이 내장 기본 목록에 다 들어 있는지 | 첫 `all_bios_check.sh` 결과에서 거의 모든 호스트가 다르게 나오는 속성(`[속성별 집계]` 상위, 서비스태그·부팅순서·MAC 류) | `ignore_attrs.txt`(그리고 반복되면 `ignore_attrs.txt.example`·`allcheck.go` 의 `defaultIgnore`). `*Mac*` 처럼 넓은 패턴은 MachineCheck 같은 진짜 설정까지 가리니 주의 | 미확인 |
| 13 | **`ulimit -n` ≥ 동시 수 + 수십** — 동시 접속마다 연결(FD)이 열림 | 실행 서버의 `ulimit -n`, 수천 대에서 정상 BMC 가 갑자기 `UNREACHABLE`·`TIMEOUT` 으로 늘어나는지(원인 문구는 화면에 안 나옴) | `ulimit -n` 상향 또는 `bios.conf` 의 `concurrency` 하향, README 3.5절 | 미확인 |
| 14 | **실제 RHEL6 커널(2.6.32)에서 `bin/biostool_os6` 기동** — 랩은 커널 4.18 이라 확인하지 못함(CentOS 6 userland 컨테이너까지만) | 회사 RHEL6 에서 `bin/biostool_os6 help`, 이어 `bash bios_check.sh -h` 와 mock 없이 `-from-dump` 점검 | 안 뜨면 빌드 환경(Go 1.20.x, `CGO_ENABLED=0`) 재확인. README Disclaimer·계획서 갱신 | 미확인 |
| 15 | **실제 BMC 응답 지연 기반 소요 시간** — mock 은 즉시 응답(3,000대 3초)이라 실제 시간을 알 수 없음. 호스트당 GET 이 십수 번이고 같은 BMC 로 가는 요청 사이에 100ms 간격을 둠 | 수십 대 단계에서 `time bash bios_check.sh --profile VM -no-prompt` 로 소요 시간 측정 후 수천 대로 환산 | `bios.conf` 의 `concurrency`·`timeout`·`retries` 조정, README 에 실측 기록 | 미확인 |
| 16 | **`ApplyTime=OnReset` 지원** — Bios 가 `@Redfish.Settings.SupportedApplyTimes` 에 `OnReset` 을 알릴 때만 본문에 넣음. 알리지 않으면 본문에 없음(BMC 기본 동작에 맡김) | dry-run 본문에 `@Redfish.SettingsApplyTime` 이 있는지, 없다면 BMC 가 기본으로 재부팅 때 반영하는지 | `set.go` 의 `planHost`(`onReset`) | 미확인 |
| 17 | **속성 레지스트리 접근** — 레지스트리를 못 읽으면 ReadOnly·허용값 사전검증 없이 보냄 | dry-run 의 `참고: 레지스트리 … 사전검증 없이 계산했습니다`, 요약의 `registry=없음` | `inspect.go` 의 레지스트리 위치 탐색 | 미확인 |
| 18 | **UCSM 응답 형태** — `aaaLogin` 응답의 outCookie·errorCode 와 세션 만료 응답(errorCode 552)이 가정과 같은지 모름. 만료는 현재 `UNSUPPORTED` 로 보일 수 있음 | `bash ucsm_check.sh` 1대 실행 후 `ucsm_result.tsv` 의 호스트 단위 행 상태와 터미널 `[UCSM 오류]`·`예:` 줄. 긴 실행에서 만료가 나는지 | `internal/ucsmxml` 의 응답 파싱·오류 분류, 세션 갱신 여부(현재 없음) | 미확인 |
| 19 | **블레이드 dn·PID 매칭** — X210c M7 등이 `computeBlade` 로 보이는지, dn 이 `sys/chassis-N/blade-M` 인지, PID(예 `UCSB-B200-M5`)가 매핑 TSV 모델(`UCS B200 M5`)과 `ucsmModelKey` 로 맞는지 모름 | ucsm 요약의 서버 dn·모델, `MAPPING_MISSING` 이 나오는 모델 | `standard_bios_map_xml.tsv` 의 `model` 열, `ucsm.go` 의 `ucsmModelKey` | 미확인 |
| 20 | **실효 설정 위치** — 서버에 적용된 BIOS 값이 `<blade dn>/bios/bios-settings/…` 아래에 있는지 모름(BIOS 정책 `bios-prof-*` 는 무시함) | `ucsm_result.tsv` 에서 `current` 가 `-` 인 `UNSUPPORTED` 행, UCSM 화면의 서버 BIOS 값과 대조 | `ucsm.go` 의 `ucsmServerOf`·`ucsmSettingsSeg`, 매핑 TSV 의 `xml_class`·`xml_attribute` | 미확인 |
| 21 | **system_profile 값 선택** — `vpCPUPerformance=hpc` 와 `vpEnergyPerformance=performance` 중 어느 쪽이 UCSM 의 고성능 프로파일인지 모름. 현재는 **두 속성을 각각 별도 항목으로 모두 점검**함(`ucsm_result.tsv` 에 `system_profile` 행 2개) | UCSM 화면의 BIOS 프로파일 값과 두 속성의 현재값 | 맞지 않는 쪽 행을 매핑 TSV 에서 삭제하고 남은 행의 `verified` 판단 | 미확인 |
| 22 | **Lenovo `Enabled` 대 `Enable`** — XCC 가 실제로 어느 쪽 값을 주는지 모름(프로파일은 둘 다 허용) | 요약의 `[허용값: …]`, dry-run 본문의 값 | `profiles/VM_research.tsv` 의 LENOVO 행 `value`(수동 보정 항목이라 `map_bios_attrs.ps1` 재생성 시 다시 적용) | 미확인 |
| 23 | **매핑에 없는 항목의 노출 여부** — SR630/SR650/SD530 `llc_prefetch`, SR645/SR675 V3 `sub_numa_cluster`, XE9780 `LogicalProc` 가 실제 BMC 에 있는지 모름 | 점검 결과의 `MAPPING_MISSING`·`UNSUPPORTED`, `mapping_missing/summary.txt`, 속성 이름 검색 | `profiles/VM_research.tsv` 해당 행(없으면 그 항목은 `MAPPING_MISSING` 으로 남김) | 미확인 |
| 24 | **HPE iLO6 Gen11 허용값** — 문서의 AMD 샘플 기반이라 값이 `mapped_value_assumed`(가정)임 | iLO 요약의 `[허용값: …]`, hyper_threading·system_profile 값 | `profiles/VM_research.tsv` 의 HPE iLO6 행 `value`([bios_json/HPE/iLO6/README.md](bios_json/HPE/iLO6/README.md) 참고) | 미확인 |
| 25 | **Supermicro 속성 이름** — 조사 데이터의 이름은 BIOS Setup UI 이름(`UI_NAME_ONLY`)이라 Redfish AttributeName 과 다를 수 있음 | Supermicro BMC 의 Bios Attributes 실제 키 | `profiles/VM_research.tsv` 의 SUPERMICRO 행 `attribute` | 미확인 |
| 26 | **비인증 ServiceRoot 의 벤더 표시** — 2순위 계정은 비인증 `GET /redfish/v1` 의 `Vendor` → `Oem` 키 → `Product` → `Name` 으로 벤더를 식별함. 실제 BMC 가 이 값을 돌려주는지(벤더별: Dell `Vendor: Dell`, HPE `Oem.Hpe`/`Oem.Hp`, Lenovo, Supermicro, Cisco)와 Oem 키 이름을 모름 | 벤더별 BMC 1대에서 `curl -k https://<BMC>/redfish/v1` (인증 없이)의 `Vendor`·`Oem` 필드 | `fallback.go` 의 `vendorFromServiceRoot`·`fallbackVendorKey`(키 이름·별칭 추가) + 시험. 식별 못 하는 벤더는 예비 계정이 시도되지 않음 | 미확인 |
| 27 | **비인증 ServiceRoot 가 막힌 BMC** — 보안 강화 설정으로 `/redfish/v1` 도 401 을 주는 BMC 는 벤더를 식별하지 못해 예비 계정을 시도하지 않고 `AUTH_FAIL` 이 됨 | 해당 BMC 에서 비인증 `/redfish/v1` 이 401 인지, 그런 BMC 가 얼마나 있는지(리포트의 `BMC 벤더를 식별하지 못해 2순위 계정은 시도하지 않음` 사유 건수) | 많다면 식별 방법 보강(예: 1순위가 아닌 다른 비인증 경로, 대상 목록에 벤더 표기) 설계 변경. 적다면 해당 호스트는 수동 처리 | 미확인 |
| 28 | **예비 계정 로그인 실패 횟수와 BMC 잠금 정책** — 도구는 1순위 거부 후 예비 계정을 1회 시도하고, 한 번 실행(점검+Y 적용)에 같은 BMC 에 인증 시도가 최대 4회(단계마다 2회)임. BMC 의 잠금 임계값(예: 3~5회 실패)과 잠금 해제 시간을 모름 | 잠금 정책을 확인한 BMC 1대(또는 시험용)에서 실패 횟수·`FailedLoginAttempts`·잠금 여부, `AccountService` 의 `AccountLockoutThreshold`·`AccountLockoutDuration`(읽기만) | 임계값이 작으면 `auth_fail_stop` 낮추기·2순위 시도 정책(1회 유지/끄기) 조정, README 2.10절 주의 기록 | 미확인 |
| 29 | **Basic 인증 BMC 에서 예비 계정이 동작하지 않음** — 세션을 지원하지 않아 Basic 으로 폴백하는 BMC 는 로그인 단계가 없어 1순위 거부(401)를 요청 시점에야 알게 됨. 현재 구현은 로그인(`Login`) 실패에서만 예비 계정으로 바꾸므로 이런 BMC 에서는 예비 계정이 시도되지 않음 | 세션 없는 BMC(`-no-sessions` mock 으로 재현 가능)에서 1순위가 틀릴 때 결과가 `AUTH_FAIL` 로만 나오는지 | 필요하면 Basic 폴백 경로에도 예비 계정을 연결(+ 시험), 아니면 README 한계로 유지 | 미확인 |
| 30 | **레지스트리 허용값의 실제 표기(`Enable` 대 `Enabled`)** — 프로파일 `Enabled\|Enable` 에서 설정(Y)·dry-run 은 BMC 레지스트리가 받는 표기를 골라 보냄. 실제 BMC 레지스트리가 어느 쪽을 쓰는지, 대소문자만 다른 표기(`enabled`)인지, 레지스트리를 못 읽을 때 첫 값으로 가는 것이 맞는지 모름 | dry-run 본문의 값과 요약의 `[허용값: …]`, 레지스트리를 못 읽은 호스트의 `참고: 레지스트리 … 사전검증 없이` 줄 | 프로파일 `value` 의 `A\|B` 순서(레지스트리를 못 읽을 때는 첫 값이 전송됨), `set.go` 의 `pickAllowedValue` | 미확인 |

| 31 | **계정 생성 방식(벤더별)** — 문서 근거만 있음: Dell iDRAC8/9 빈 슬롯(3~16) PATCH, iDRAC10 POST, HPE `RoleId` POST(iLO4 는 Oem 권한), Lenovo POST(Purley 는 슬롯 PATCH), Supermicro POST(X10 `Admin`) | 벤더별 1대에서 `bash account_create.sh -dry-run` 의 본문·방식 → 실제 생성 후 `account_result.tsv` 의 result, BMC 웹 화면의 계정 목록 | `account.go` 의 `planCreate`(단계·역할 후보), `account_gate.go`(허용 형태) | 미확인 |
| 32 | **새 계정 즉시 로그인 가능 여부** — 검증 로그인 1회가 거짓 실패(`VERIFY_FAIL_ROLLED_BACK`)로 끝나는 BMC 가 있는지(생성 직후 활성화 지연, 첫 로그인 시 비밀번호 변경 요구 `PasswordChangeRequired`) | 생성 직후 같은 계정으로 수동 로그인, 결과의 `VERIFY_FAIL_ROLLED_BACK` 비율 | 지연이 있으면 검증 전 대기 또는 재시도 1회 추가, Cray 는 `PasswordChangeRequired:false` 동작 확인 | 미확인 |
| 33 | **롤백(삭제) 동작** — 슬롯 방식(Dell)은 비활성→이름 비우기 2단계, 그 밖은 멤버 DELETE. 실제 BMC 가 이를 받아들이는지, 삭제 후 슬롯이 비는지 | 일부러 검증 실패를 만들 수 없으므로 시험용 BMC 에서 수동으로 PATCH/DELETE 확인 | `rollbackCreate` | 미확인 |
| 34 | **UCSM XML 쓰기** — `configConfMo`(aaaUser created/modified/deleted, 역할 admin) 응답, `errorCode`, 비밀번호 정책(`$ ? =` 금지), `historyCount`/변경 간격에 의한 거부 | UCSM 1대(또는 시험 도메인)에서 `account_create.sh -ucsm -dry-run` 후 실행 | `ucsmxml.go` `gateConf`·`CreateUser`·`SetUserPassword` | 미확인 |
| 35 | **비밀번호 변경 후 세션** — 변경 직후 기존 세션이 유지되는지(롤백이 같은 세션을 쓰므로 중요), Basic 인증 BMC 에서의 롤백 | 시험용 BMC 에서 변경 후 같은 세션으로 GET 가능한지 | `changeOnHost` 의 롤백 경로(필요하면 새 로그인 후 복원) | 미확인 |
| 36 | **빈 슬롯 판별** — 빈 슬롯을 `UserName==""` · `Enabled=false` · 숫자 Id ≥ 2(Dell ≥ 3) 로 판별함. 실제 BMC 가 예약 슬롯(익명 IPMI, HostBootstrap 등)을 다르게 표시하는지 | 계정 컬렉션의 각 멤버(Id·UserName·Enabled·RoleId) 전체 | `account.go` 의 `isEmptySlot` | 미확인 |
| 37 | **동시 처리 수·잠금 정책** — 기본 100(최대 300), 웜업 10대. 계정 쓰기에서 BMC 지연·세션 한도에 걸리는지, 잠금 정책과의 상호작용 | 첫 실행 로그의 `TIMEOUT`·`HTTP 429/503` 비율, 호스트당 소요 시간 | `acctDefaultWorkers`·`acctMaxWorkers`·`acctWarmup` 조정 | 미확인 |
## C. 확인 결과를 반영한 뒤 할 일

- [ ] `profiles/VM.tsv` 를 확정값으로 갱신(`verified=Y` 는 모델을 지정한 행만, 사람이 확인한 것만)
- [ ] 표준값이 바뀌었거나 새 동작을 알았다면 테스트·`testdata/` 에 반영하고(합성 데이터임을 유지) `bash build.sh`·`bash build_os6.sh` 로 바이너리를 다시 빌드해 **두 바이너리를 함께 커밋**([PR_CHECKLIST.md](PR_CHECKLIST.md))
- [ ] [CHANGELOG.md](CHANGELOG.md) 의 알려진 제한·검증 현황, [계획서.md](계획서.md) 의 “실장비 미검증” 표를 갱신
- [ ] 이 표의 상태 칸 갱신
