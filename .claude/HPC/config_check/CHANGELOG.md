# CHANGELOG

## 2026-10-06 — `-auto` 비대화형 모드와 auto_setup 완료기록 추가

- **`-auto <user> <호스트목록파일>`**: user 선택·y/n 질문 없이 진행. 작업 y, 환경설정은 목록에 p/d 로 시작하는 호스트가 있으면 set, 없으면 y. 결과 리포트 블록(시작/끝 마커)과 `check.res_<user>_postapply` 출력. 인자 없는 실행은 기존과 동일
- **완료기록(`auto_record_done`)**: 설정 `auto_done_dir`/`auto_done_host`(빈 값 기본)가 채워지면 재체크 결과에 에러 없이 나온 호스트마다 `epoch user sha256 config_check` 기록(로컬 원자 기록 또는 gossh 원샷, 500대 단위). 둘 다 비면 동작 없음
- **패치**: `auto_mode.patch` (회사 사본 반영용)
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
