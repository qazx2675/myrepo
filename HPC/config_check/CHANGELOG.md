# CHANGELOG

## 2026-10-28 — LDAP SLSI 규칙 추가, update.sh, 브랜치를 HPC/config_check 단독 구성으로

- **LDAP(`config_check.sh` `run_check`)**: `INFO<TAB>SLSI<TAB>VIP<TAB>1.1.1.1<TAB>(10.225.130|10.225.184)` 줄을 LDAP 값으로 읽는다. `SLSI` 다음 토큰이 `VIP` 이면 `SLSI VIP`, 아니면 `SLSI`. 기존 `ldap` 키워드 규칙은 그대로이며 호스트당 처음 찾은 값 하나를 쓴다. 출력 형식은 그대로다 (한 종류 `LDAP : SLSI VIP`, 2종류 이상이면 `LDAP : SLSI VIP(5ea) / SLSI(3ea)` + 값별 호스트, 20대 이상은 `check.info_${user}`).
- **update.sh(신규)**: 실사용 사본에 SLSI LDAP 줄(+설명 주석 1줄)만 내용 기준으로 끼워 넣는다. CRLF 유지, 이미 적용된 사본은 무변경, 앵커가 정확히 1곳이 아니면 거부, 추가 1~2줄·삭제 0줄 검사, `bash -n` 검사, `<대상>.bak.<YYYYmmddHHMMSS>` 백업. 검사가 하나라도 실패하면 대상은 바뀌지 않는다.
- **브랜치 구성**: `os-check-script` 브랜치에는 `HPC/config_check` 만 남긴다 (이전에는 `.claude/HPC/config_check` 경로). 이전 이력은 백업 태그 `os-check-script-backup-20261028` 로 보존한다.
- **영향 범위**: `config_check.sh`, `update.sh`(신규), `README.md`, `ARCHITECTURE.md`, `CHANGELOG.md`.
- **검증**: .58(Rocky 8.10). update.sh — 원본·CRLF(추가 줄도 CRLF, 나머지 바이트 동일)·탭 들여쓰기·`for(j=2;…){` 변형 적용(추가 2줄·삭제 0), 재적용 무변경, LDAP 규칙 없는 파일 거부, 인자 없음 사용법 출력, `.bak` 백업 생성. 기능 — 가짜 gossh 9대(`-auto-check`)에서 `LDAP : SLSI VIP(5ea) / SLSI(3ea) / infra(1ea)`·값별 호스트·조합 줄 확인, 5대 단일 값은 `LDAP : SLSI VIP` 한 줄, 수정 전 사본은 SLSI 8대가 `LDAP 미확인`. 실제 운영 run.sh 로는 확인하지 못했다.

## 2026-10-21 — 작업 디렉터리를 /tmp 바로 아래에 만들지 않음

- **작업 디렉터리 위치**: `/tmp/config_check.XXXXXX` → `$TMPDIR/config_check.XXXXXX`. TMPDIR 이 없으면 `${AUTO_SETUP_DIR:-/tmp/auto_setup}/tmp`(0777+sticky, 만들 수 없거나 쓸 수 없으면 `/tmp`). auto_setup 이 실행하면 auto_setup 이 TMPDIR 을 준다(os8_mgmt 는 run 별 `runs/<code>/tmp`, os6_mgmt 2차 체크는 원격 작업 디렉터리 안). os8_mgmt `/tmp` 에 `config_check.*` 가 보이던 것은 이 스크립트의 작업 디렉터리였다
- **끊김 정리 명시**: `trap 'exit 129' HUP` 추가 — 터미널·ssh 가 끊겨도 EXIT 트랩으로 작업 디렉터리를 지운다 (bash 4.4 는 원래도 지웠음, 셸 버전과 무관하게 명시)
- **현장 반영**: `bash apply_tmpdir.sh <config_check.sh>` (바뀌는 기존 줄은 `TMP=$(mktemp …)` 한 줄, 주석·코멘트 불변, 백업 `.bak`, 이미 적용된 사본은 그대로)

## 2026-10-14 — 상세 흐름도를 실제 동작에 맞게 수정

- **문서(`workflow-detail.svg`)**: 환경설정 y/set 실행 스크립트(`setting_insert.sh; appl_change.sh` [; `setting.sh`])·대상(OK 호스트만), 임시 파일명(`first.*`/`recheck.*`), `-auto`/`-auto-check` 분기, 마무리 출력 순서를 실제 스크립트에 맞게 정정하고 `nosvrauto` 표기를 `no_mgmt-autofs` 로 바꿨다. 스크립트 코드는 변경하지 않았다.

## 2026-10-14 — 내부 흐름 상세 설명도 추가

- **문서(`workflow-detail.svg`)**: 실사용용 `workflow.svg` 와 별개로, 내부 처리 단계·변수·상태 코드(OK/OFF/REF/INST/NOSV/NONE)·임시 파일 구조를 설명하는 상세 흐름도를 추가했다. 스크립트 코드는 변경하지 않았다.
- **영향 범위**: `workflow-detail.svg`(신규), `README.md` 문서별 설명 표 1줄, `CHANGELOG.md`.

## 2026-10-13 — `-auto-check` 추가 (auto_setup 설정체크만 t)

- **`-auto-check <user> <호스트목록파일>`**: `-auto` 와 같이 user 선택·질문 없이 진행하되 환경설정 수정 질문에 `n` 으로 답해 설정 적용·재점검·`check.res_<user>_postapply`·완료기록을 하지 않는다. 점검 결과 `check.res_<user>` 와 결과 리포트만 만든다 (질문 에코는 `(-auto-check)`). `-auto` 만 아는 사본에 auto_setup 의 설정체크만(t)을 실행하면 user 선택 메뉴에서 멈추던 문제 해결
- **현장 반영**: `-auto` 가 이미 적용된 사본은 `bash apply_auto_check.sh <config_check.sh>` (증분 패치 `auto_check_mode.patch`), 원본은 `bash apply_auto_mode.sh` (누적 패치 `auto_mode.patch` 에 포함). 백업(.bak)·주석 변경 검사·`bash -n` 은 기존 스크립트와 같다. 이미 `-auto-check` 가 있는 사본에 다시 실행하면 적용 실패로 멈추며 파일은 바뀌지 않는다
- **테스트**: `test/run_auto_test.sh` 7번 추가 (PASS 25 / FAIL 0)

## 2026-10-07 — 처리 흐름도 추가

- **문서(`workflow.svg`)**: user 선택 → 리스트 확인 → 작업 진행(y/n) → OS 체크(gossh 1회) → 체크 리포트 → 환경설정(y/n/set) → 재체크 → 마무리 출력 흐름도를 추가했다. 스크립트 코드는 변경하지 않았다.
- **영향 범위**: `workflow.svg`(신규), `README.md` 문서별 설명 표 1줄, `CHANGELOG.md`.
- **검증**: 브라우저로 렌더링해 글자 넘침을 확인하고 박스 4 폭을 넓혀 수정했다.

## 2026-10-06 — `-auto` 비대화형 모드와 auto_setup 완료기록 추가

- **`-auto <user> <호스트목록파일>`**: user 선택·y/n 질문 없이 진행. 작업 y, 환경설정은 목록에 p/d 로 시작하는 호스트가 있으면 set, 없으면 y. 결과 리포트 블록(시작/끝 마커)과 `check.res_<user>_postapply` 출력. 인자 없는 실행은 기존과 동일
- **완료기록(`auto_record_done`)**: 설정 `auto_done_dir`/`auto_done_host`(빈 값 기본)가 채워지면 재체크 결과에 에러 없이 나온 호스트마다 `epoch user sha256 config_check` 기록(로컬 원자 기록 또는 gossh 원샷, 500대 단위). 둘 다 비면 동작 없음
- **패치**: `auto_mode.patch` + `apply_auto_mode.sh` (회사 사본 반영용. 주석·코멘트 줄은 수정하지 않으며, 바뀌게 되면 중단)
- **영향 범위**: `config_check.sh`(설정 블록, 1·3·5·6단계, 완료기록 함수), 테스트 `test/run_auto_test.sh`(18건)
- **알아둘 점**: 이 스크립트에는 os_check 의 `===== LDAP 정보` 형태 요약 블록이 없어 auto_setup code 파일에는 리포트 블록과 설정체크 섹션만 실립니다. 실제 운영 run.sh·gossh·auto_setup 데몬 연동은 스텁 테스트만 했습니다

## 2026-10-02 — 버그 수정(전부 접속불가 시 NO FAIL, INFO 줄바꿈) 및 Splunk·LDAP·OS 조합 요약 추가

- **NO FAIL(`config_check.sh` `report_fail`)**: 모든 호스트가 접속불가일 때도 `NO FAIL`이 출력되던 문제 수정. 체크된(OK) 호스트가 1대 이상일 때만 `NO FAIL`·LDAP·정보 요약을 출력한다.
- **INFO 줄바꿈(`report_status`/`report_values`)**: INFO 값을 상태줄 끝에 붙이던 방식을 없애고(`total=13ea OK=13ea OS ... / OS ...`처럼 한 줄에 섞이던 문제), 2종류 이상이면 경고·대수·값별 호스트를 줄을 나눠 출력한다.
- **정보 요약 추가(`report_combo`)**: 상태줄 아래에 Splunk·LDAP·대수·OS를 조합이 같은 호스트끼리 한 줄로 출력한다(값 정규화, 없으면 `-`).
  - Splunk: `INFO` 줄에서 `Splunk` 키워드 다음 토큰 / LDAP: `LDAP` 키워드 다음 토큰(`FAIL ldap Undefined configuration`도 `undefined`로 집계) / OS: `INFO` 줄 중 `std` 포함 줄의 `INFO` 뒤 문자열.
  - 줄은 공백·탭 모두 구분자로 토큰화한다. 값은 연속 공백을 1개로 줄이고 `Undefined...`는 `undefined`로 통일한다.
- **LDAP 요약(`report_values`)**: 2종류 이상이면 값별 호스트를 모두 출력하고, 값별 호스트가 20대 이상이면 `check.info_${user}`에 저장한다(화면에는 대수와 파일 안내). `LDAP 미확인`도 20대 이상이면 파일로 저장.
- **영향 범위**: `config_check.sh`(`run_check` 파서, `report_fail`, `report_values`(구 `report_multi`), `report_combo`(신규), `report_status`, `do_check`, `INFO_FILE` 변수), `README.md`(상태줄·LDAP·정보 요약 섹션), `ARCHITECTURE.md`(함수/임시 파일 표).
- **알아둘 점**: Splunk은 요청대로 `INFO` 줄만 읽는다(`FAIL Splunk ...` 줄은 읽지 않음). OS는 `KERNEL`이 아니라 `std` 포함 여부로 찾는다(이전 `INFO`+`KERNEL` 규칙 대체).
- **검증**: .58(Rocky 8.10) + 최신 `gossh-standalone` 빌드, 가짜 run.sh(`pd` 처리)로 5개 시나리오 확인. 전부 접속불가(NO FAIL·LDAP 미출력), Splunk 3종·LDAP 2종·OS 2종 혼합 28대(조합 5줄, 대수 합 27, LDAP infra 23대·OS 25대는 요약 파일 저장, 50줄), 모두 1종류, INFO 없음(`LDAP : 정보 없음`, 조합 없음), 설정 적용(y) 후 재체크 경로. 종료 후 임시 디렉터리·랩 임시 파일 모두 삭제 확인. 실제 운영 run.sh로는 검증하지 못했다.

## 2026-09-21 (추가 변경)

- 대상 리스트 출력: 탭 대신 공백으로 열을 맞춤(대수가 많을 때 탭 정렬이 깨져 보이던 문제).
- S 벤더 추가: D 규칙(`^c ^h ^sh ^s2h ^s3h ^s4h`)에 해당하지 않는 `^s` 호스트는 S 벤더로 분류하고 L과 같은 코멘트(접속불가 목록)를 출력.
- 검증: .58(Rocky 8.10)에서 스텁 gossh로 68대 리스트(3열, 탭 0개), `sh1`·`s2h1`=D / `s9x1`·`sabc1`=S / `p1`=L 분류 확인.

## 2026-09-21 (변경 요청 반영)

- run.sh 실행 인자 변경: `bash run.sh` → `bash run.sh pd` (FAIL이 없으면 `OK`만 출력하고, `pd`를 주면 OK가 아닌 결과값을 출력하는 방식으로 바뀜).
- LDAP: 정상 `INFO<TAB>ldap<TAB>infra`, 미정의 `FAIL<TAB>ldap<TAB>Undefined configuration` 모두 값으로 집계(`ldap` 대소문자 무관). 2종류 이상이면 `infra(N ea) / Undefined configuration(N ea)` 형식.
- INFO 요약 추가: `INFO`와 `KERNEL`이 함께 있는 결과 줄의 탭 뒤 문자열을 상태줄 끝에 표시. 2종류 이상이면 LDAP과 같은 규칙(값별 대수 + 소수 값 호스트).
- 상태줄: 항목 사이 구분을 탭으로 변경, `total`/`OK`를 제외하고 0인 항목은 숨김, `total`이 각 항목의 합과 같으면 초록·다르면 빨간색 깜빡임.
- 체크 결과가 한 줄도 없는 호스트를 `no_output`으로 분리(OK로 세지 않음, 설정 적용 대상 제외, 기타 상태 서버에 표시). `total` 불일치를 실제로 감지하기 위함.
- 검증: .58(Rocky 8.10) + gossh v2, 가짜 run.sh(`pd` 인자 처리)로 LDAP Undefined 혼재, INFO 2종, 무응답 호스트(total 빨간 깜빡임), 전체 정상(total 초록)을 확인. 시험 환경은 원복.

## 2026-09-21

### 추가
- `config_check.sh` 최초 작성: user 선택 → 리스트 확인 → OS 체크 → 환경설정(y/n/set) → 재체크 → 벤더 코멘트.
- 체크 결과의 LDAP 요약(상태줄 위): 동일하면 `LDAP : infra`, 2종류 이상이면 노란색 경고와 값별 대수, 소수 값 호스트, 미확인 호스트.
- 기타 상태 서버(pingO_sshx / nosvrauto / os_install) 요약.

### 속도
- 기존 `os_check_final_annotated.sh`는 전체 대상에 gossh를 4회 실행(분류, 커널, run.sh, LDAP 조사). 이번 스크립트는 `gossh -pm`으로 run.sh를 **1회**만 실행하고 분류는 gossh 결과 파일에서 얻습니다.
- 설정 스크립트는 `;`로 묶어 gossh 1회로 실행, 재체크는 OK 호스트만 대상.
- DHCP 조회는 scp+ssh 2회 → ssh 1회(파일 미잔류).
- 호스트 파싱·집계·분류를 awk로 처리. 5,000대 기준 스크립트 자체 오버헤드 0.4초 미만(스텁 gossh로 측정, 실제 gossh 시간 제외).

### 검증 중 발견해 반영
- run.sh가 0이 아닌 값으로 끝나면 gossh가 결과 전체를 stderr(`ERROR:`)로 보내 FAIL/LDAP이 비어 나오던 문제 → 명령을 `bash run.sh; :`로 실행.
- gossh는 Ctrl+C를 자체 처리하므로 gossh 실행 구간에서만 스크립트의 INT를 무시(`gsh`).
- 입력이 EOF일 때 y/n 재질문 루프가 무한 반복되던 문제 → `read` 실패 시 종료.
- 병렬 출력 순서가 섞여 FAIL/LDAP 목록이 뒤죽박죽이던 문제 → 호스트명 기준 안정 정렬.

### 검증 환경
- .58(Rocky 8.10) + gossh v2(`gossh-standalone` 브랜치 빌드). 고정 경로에 가짜 스크립트를 두고 호스트 별칭(127.0.0.x)으로 시나리오 재현. 실제 nosvrauto는 .59(ESXi)로, 타임아웃/refused는 iptables DROP/REJECT로 재현. 시험 후 환경은 모두 원복.
- 실제 운영 환경의 `run.sh` / 설정 스크립트 / DHCP `a.sh`로는 검증하지 못했습니다(가짜 스크립트 사용).
