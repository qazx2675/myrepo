# CHANGELOG

## [Unreleased]

### Added
- `run_survey.sh --debug` (또는 `DEBUG=1`): 읽어들인 conf 값·임시 경로·A 실행 명령을
  `[debug]` 로 출력하고, A 용 임시 conf 의 `[gossh].bin`/`[input].asset_file` 실제 값을
  보여주며, 임시 작업 디렉토리와 A 실행폴더를 지우지 않고 남긴다.
- A 실행 직전 A 서버에 gossh 가 있는지(`test -x`) 점검해 없으면 경고.
- B 조사 후 **특이사항 분포**를 항상 출력. 재조사 대상이 0대일 때 그 이유가 바로 보인다
  (`gossh 실행 실패` 는 타임아웃/접속불가가 아니므로 A 재조사 대상이 아니다).
- `[server_a].gossh_bin` — A 서버의 `gossh` 경로. `run_survey.sh` 가 A 전용 conf 사본을
  만들 때 `[gossh].bin` 을 이 값으로 바꾼다. 비우면 B 의 `[gossh].bin` 을 그대로 쓴다.
  (`survey` 는 조사를 직접 하지 않고 `gossh` 를 exec 하므로 A 에도 gossh 가 있어야 한다)
- `update.sh` 보존 목록에 `survey-rhel6` 추가 (A용 바이너리가 덮이지 않도록).

### Fixed
- **gossh `-script` 인자가 따옴표 문자(`'`/`"`)로 끝나면** 원격 bash 가
  `unexpected EOF while looking for matching '` 로 죽던 문제. 기본 `config_value`
  (`... | grep -w '/appl'`)가 `'` 로 끝나 **모든 호스트가 `gossh 실행 실패`/설정값 공란**이
  됐다. `runGossh` 가 스크립트 문자열 끝에 공백 한 칸을 붙이도록 수정
  (`sh -c "cmd " == sh -c "cmd"` 이라 정상 명령엔 영향 없음). RHEL6·RHEL9 gossh 공통 버그.
- `run_survey.sh` 가 A 재조사 목록을 만들 때 자산 파일을 **탭 구분으로만** 파싱해,
  공백 구분 자산 파일에서는 목록이 비어 A 조사가 조용히 생략되던 문제.
  이제 `asset.go` 와 동일하게 "탭이 있으면 탭, 없으면 공백" 으로 판단한다.
  대상 수보다 적게 매칭되면 경고를, 0행이면 에러 메시지를 낸다.
- A 조사 생략 사유를 구분해서 출력: 바이너리 없음 / 실행권한 없음(`chmod +x`) / conf 없음.
  (기존엔 셋 다 `A 바이너리/conf 없음` 한 줄이라 원인 파악이 안 됐음)
- ESXi 인데도 `ldap.conf`(인프라 fallback)를 조사하던 경로 제거. 1차 조사에서
  `타임아웃`/`DNS 미등록` 이었던 호스트는 `DetectESXi` 대상(`Reached && 설정값 없음`)에
  들지 못해 ESXi 판별이 안 된 채 재조사 `CollectInfra` 로 들어갔다. 이제 재조사 후
  응답한 호스트에 대해 ESXi 를 **다시 판별**하고, 재조사 인프라 조사에서도 ESXi 를 제외한다.
  (해당 호스트도 이제 정상적으로 `특이사항 = esxi` 행 + VM 조사 대상이 된다)
- VM 2차 조사: 존재하지 않는 VM(`ev03` 등)이 gossh 에러 파싱 실패로 `접속불가` 행으로
  잘못 기록되던 문제. 이제 `survey` 가 VM 이름을 **직접 `net.LookupHost` 로 조회**해
  해석되지 않으면 조사 대상에서 빼고 어떤 행도 남기지 않는다. (`ev01`,`ev02` 만 있으면
  `ev03` 은 파일에 없음). `detectError` 에 Go 스타일 `no such host` 도 DNS 미등록으로 추가.
- VM 결과파일(B): 인프라망이 비어 있어도 사유가 안 보이던 문제. `SurveyVMs` 가 이제
  파일 A 와 동일하게 `인프라 스크립트 없음` 등 인프라 Note 를 `특이사항` 에 표기한다.
- 인프라 출력이 여러 줄일 때(`FAIL ldap_site` + `INFO ldap infra`) 첫 줄만 보고 매칭
  실패하던 문제. 이제 각 줄에 `infra_regex` 를 적용해 **처음 매칭되는 줄**을 쓴다.
- `infra_regex` 매칭 실패 시 fallback(binddn) 결과 처리:
  - binddn 출력이 있으면 `infra_fallback_regex` 로 값을 뽑고, 못 뽑으면 **binddn 원본 줄**을 인프라망에 기록.
  - binddn 응답이 아예 없을 때만 `특이사항` 에 `인프라 확인필요(<원본>)` / `인프라 스크립트 없음`.
- 인프라 스크립트가 대상 호스트에 없을 때(`no such file`)도 `infra_fallback_cmd`(binddn)
  로 재조사하도록 변경 (기존: 바로 포기). VM 에 infra 스크립트를 안 깔아도 ldap.conf 로 대체 가능.

### Changed
- `infra_fallback_regex` 기본값을 `uid=([^,\s]+)` 로 변경 (기존 `(?:uid|ou)=...` 는
  문자열에서 먼저 나오는 쪽을 잡아 `ou` 가 나오곤 했음). 이제 binddn 의 `uid=` 만 뽑는다.
  `uid=` 없는 binddn 은 매칭 실패 → 원본 줄 기록(SDC 자동 분리 안 됨).
- `update.sh`: `conf/conf.toml` 은 계속 보존하되, 새 배포본과 다르면 `diff` 를 출력하고
  "직접 반영 필요" 경고를 낸다 (기존엔 조용히 건너뛰어 conf 변경이 반영 안 된 걸 모름).
- ESXi 물리장비로 판별된 호스트는 인프라·설정값 조사를 **아예 하지 않는다**.
  `설정값`/`인프라망`/`appl설정유무` 모두 `없음`, `특이사항` `esxi` 로만 기록.
  (기존: ESXi 에도 infra gossh 를 날려 `인프라 확인필요(...)` 같은 값이 들어가던 것 제거)

### Added
- **A·B 서버 연동 + 자산 필터링 (`run_survey.sh`, Go 바이너리 불변)**
  - `[asset_filter]` (source/include/exclude): 원본 표1 을 `egrep -E` 로 걸러 `[input].asset_file` 생성.
  - `[server_a]` (enabled/host/user/dir/bin): B 결과에서 `타임아웃`/`접속불가` 호스트만
    (DNS 미등록 제외) A 서버(RHEL6 빌드)에서 재조사 → B·A 결과를 hostname 기준 병합.
    재조사 성공분은 A 행으로 교체. A 실행 실패 시 경고 후 B 결과만으로 진행.
  - 전제: A·B 가 `dir` 을 auto mount 로 공유. B 가 `dir/.resurvey.XXXXXX/` 에 재조사 목록 +
    `[input].asset_file` 만 바꾼 conf 사본을 만들고 `ssh` 로 실행만 A 에서 한다(scp 안 씀).
    A 바이너리 파일명은 `[server_a].bin` 으로 지정(B 것과 겹치지 않게, 예 `survey-rhel6`).
  - 병합 후에도 `result_*.tsv` / `_sdc_` / `_vm_` 간 hostname 중복 없음.
  - 두 섹션 모두 conf.toml 에 있어도 `survey` 바이너리는 무시(미지정 키). 없으면 기존 동작 그대로.
- RHEL 6 지원: `go.mod` 를 `go 1.20` 으로 낮춤. Go 1.21+ 는 런타임이 커널 3.2+ 를
  요구해 RHEL 6(커널 2.6.32)에서 즉시 죽는다. Go 1.20 이 2.6.32 지원 마지막 릴리스.
  코드는 표준 라이브러리만 쓰므로 로직 변경 없음.
  배포용 정적 바이너리: `CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "-s -w"`
  (glibc 의존 없음 → RHEL 6 glibc 2.12 에서도 동작). `examples/` 참고.
- `[gossh].retry_timeout` — 일반서버(표1, VM 제외) 재조사 시 쓸 긴 타임아웃.
  `DNS 미등록`/`타임아웃` 호스트를 이 타임아웃으로 **1회 더** 조사(답을 더 기다림).
- 인프라망 값 치환: `VIP` → `SLSI_VIP` (`mapInfraValue`).
- `infra_fallback_regex` 기본값을 `(?:uid|ou)=([^,\s]+)` 로 변경 — binddn 의 `uid=` 값도 추출.
- `[gossh].timeout` — gossh 타임아웃 초(`-t`). 설정값·인프라망 두 gossh 호출 모두에 적용.
  비우면 gossh 기본값 사용.
- **ESXi 판별 + VM 2차 조사** (`cmd/survey/vm.go`)
  - `설정값 = 없음` 인 호스트에 `uname` 실행 → `VMkernel` 이면 ESXi. 1차 결과 `특이사항` 에 `esxi`.
  - ESXi 가 1대 이상이면 `<esxi_hostname>ev01~ev03` (규칙 고정)을 만들어 별도 파일로 조사.
    VM 은 1차와 같은 `config_value`/`infra_net`/O·X 로직. 위치·상태·O·X 기준은 소속 ESXi(표1)를 상속.
  - DNS 미등록 VM 은 행 제외. 어떤 ESXi 의 VM 이 모두 실패면 그 ESXi 이름으로 `접속불가` 1행.
  - 타임아웃 VM 만 1회 재조사(무한 루프 방지).
  - 출력 파일: `result_YYYYMMDD_HHMM.tsv` + (ESXi 있을 때만) `result_vm_YYYYMMDD_HHMM.tsv`.
- `detectError` 가 이름 해석 실패를 `DNS 미등록` 으로 별도 분류(기존: `접속불가`).
- **인프라망 fallback**: `infra_regex` 매칭 실패 호스트에 `[scripts].infra_fallback_cmd`
  (기본 `cat /etc/openldap/ldap.conf | grep -i binddn`)를 gossh 로 재조사하고
  `infra_fallback_regex` 로 값 추출. 정상 출력 `INFO⇥LDAP⇥[infra]` → `infra`,
  실패 출력 `FAIL⇥LDAP⇥확인필요`/`undefined` → fallback.
- **SDC 분리 파일**: 인프라망 값이 `SDC` 인 호스트는 A·B 에서 빠지고 `result_sdc_YYYYMMDD_HHMM.tsv`
  로만 기록. A·B·SDC 간 호스트 중복 없음. (출력 파일 최대 3개)
- **`update.sh`**: 폐쇄망 증분 업데이트. 변경/신규 파일만 복사, `conf/conf.toml`·`result_*.tsv`·
  `asset_list.txt` 보존, 대상에만 있는 오래된 `cmd/**/*.go` 제거, 멱등.

## [v0.3.0] - 2026-08-27

### Changed
- **표1 파싱을 탭(`\t`) 구분으로 변경.** 상태·위치에 공백이 들어가도 정상 파싱
  (탭이 있으면 탭으로만 분리, 없으면 공백 폴백). 샘플도 탭 구분으로 교체.
- **인프라망 조사를 gossh 배치로 변경.** 기존: 로컬에서 `bash <script> <hostname>` 을
  호스트마다 실행. 변경: `gossh -w <hosts> -script "bash <infra_net>"` 1회 실행 후
  각 호스트 출력값(`hostname: 뒤`)에 `infra_regex` 적용. `-c` 는 붙지 않음.
  → `infra_net` 은 이제 **조사 대상 호스트에 배포된** 스크립트/명령이다.
- **"설정값 없음" 판정 정정.** 해당 호스트의 gossh 출력이 아예 없으면(또는 `/appl`
  매칭 행이 없으면) 설정값·appl설정유무 = `없음`. 접속 실패(ssh 에러)일 때만 공란 + `접속불가`.
- `extractConfigValue` 는 `/` 로 시작하는 줄만 후보로 삼는다
  (gossh/grep 에러 라인이 `ERROR: ...` 형태로 설정값에 섞여 `X` 로 나오던 문제 해결).

## [v0.2.0] - 2026-08-27

### Changed
- 설정값이 없을 때(응답은 받았으나 auto.appl 매칭 행 없음): 설정값 = `없음`,
  appl설정유무 = `없음` (기존: 공란 + `X` + 특이사항 "설정값 없음"). 특이사항에는 표기하지 않음.
- 접속불가 등으로 조사 자체가 안 된 경우: 설정값·appl설정유무 공란, 사유는 특이사항.
- 인프라망 조사는 gossh 접속 여부와 무관하게 항상 수행됨을 문서에 명시.

### Added
- `[scripts].infra_regex` — 인프라망 스크립트 출력에서 값을 뽑는 정규식(캡처 그룹 1).
  예: `'^\S+:\s+INFO\s+(.+)$'` → `web01: INFO ldap infra site` 에서 `ldap infra site`.
  비우면 첫 줄 전체 사용, 매칭 실패 시 공란(미조사).
- conf 파서가 작은따옴표(`'...'`) 리터럴 문자열도 허용(정규식 입력 편의).
- 인프라 스크립트가 0이 아닌 종료코드여도 stdout 이 있으면 사용.
- 인프라 스크립트는 `bash <경로> <hostname>` 으로 실행 (실행권한/셔뱅 불필요).

## [v0.1.0] - 2026-08-27

### Added
- 초기 구현 (계획서: `.claude/HPC/조사/계획서.md`)
  - `cmd/survey` Go 도구: 표1 파싱 → gossh 병렬 수집 → TSV 결과 파일
  - 표1 hostname 열 전체를 조사 대상으로 자동 사용 (중간 목록 파일 없음)
  - 설정값(auto.appl 마지막 필드) 추출, mountpoint→정상위치→표1위치 비교로 appl설정유무 O/X
  - 설정값 조사: `gossh -c <concurrency> -w file -script` 1회 배치 (기본 동시 4000, `conf` 조정 가능)
  - 인프라망 조사: gossh 와 별개로 로컬 스크립트를 호스트마다 실행
  - 인프라망은 conf 지정 스크립트(`scripts/infra_survey.sh` 샘플)에 위임
  - pdsh 형식(`hostname: 결과`) 출력 파싱 및 접속 실패 감지 → 특이사항
  - 탭 구분 결과 파일 `result_YYYYMMDD_HHMM.tsv` (필드 내 탭·개행 스페이스 치환)
- `run_survey.sh` 실행 래퍼 (인자 없음)
- 설정은 실행 파일 옆 `conf/conf.toml` 하나만 사용 (옵션 지정 없음)
- 문서: `README.md`, `ARCHITECTURE.md`, `PR_CHECKLIST.md`
- `.github/workflows/ci.yml` (build/vet/test)
- 단위 테스트 `cmd/survey/survey_test.go`

### Verified
- Rocky Linux (go 1.26.5) 에서 `go build ./...` / `go vet ./...` / `go test ./...` 통과.
- 가짜 gossh(pdsh 형식 출력) + 접속불가 host 1대 섞은 스모크 테스트:
  3행 모두 출력, 탭 구분, 접속불가 host 는 특이사항만 채워짐, O/X 정상.

### Notes
- 외부 모듈 의존성 없음(표준 라이브러리만) → 폐쇄망에서 `go build` 만으로 빌드.
  이 때문에 계획서의 "TOML 라이브러리 + go mod vendor" 대신 conf 하위집합 자체 파서를 사용.
- 미확정 항목(리스크): 실제 `gossh` 출력 형식(pdsh 가정), 인프라망 판별 실제 규칙(샘플 제공).
