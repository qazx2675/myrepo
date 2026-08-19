# awxkit 작업 기록

## 2026-08-19

- 기존 스텁 확인: `README.md`(골격만 존재, 2~4장 TODO), `go.mod`(module awxkit, go 1.21), `main.go`(10줄 placeholder), `setup.sh`(`GOFLAGS=-mod=vendor go build`), `vendor/modules.txt`(빈 상태).
- 사용자와 설계 결정 확정:
  - 인증: `${user}_setting.conf`에 ID/PW 평문 저장, `chmod 600` 안내 + 권한 경고 출력
  - 조작 방식: 대화형 메뉴 + 플래그 병행 지원
  - [S1] 결과 파일: 취득 경로(artifacts/stdout/remote) 3종 모두 지원, 로컬 저장 경로는 conf(`s1_output_dir`)에서 지정
  - 설정 파일: `${user}_setting.conf` 단일 파일 (공통 파일 분리 없음)
  - 사용자 식별: `config.CurrentUser()` 함수 껍데기만 제공, 실제 판별 로직은 사용자가 직접 채움
- `PLAN.md` 작성: 목적/범위/API 매핑/conf 포맷/단계별 마일스톤/리스크 정리.
- 작업 저장소를 `C:\Users\qazx2\AndroidStudioProjects\myrepo`에 별도 클론하여 진행 (기존 `clipSend` 로컬 저장소와 `origin/master`가 당시 이력이 갈라져 있어 그쪽에서는 작업하지 않음).

### 다음 단계
- 1단계: `config` 패키지(conf 로더 + `CurrentUser()` 훅) + `cmd doctor` 구현

## 2026-08-19 (계속) — 1단계 구현

- `config/config.go`: `key = value` 평문 conf 파서(`#` 주석 허용), `-conf` → `./conf/${user}_setting.conf` → `~/.awxkit/${user}_setting.conf` → `<실행파일>/conf/${user}_setting.conf` 순 탐색.
- `config/user.go`: `CurrentUser()` 훅(빈 문자열 반환) + `ResolveUser()`(CurrentUser → `-user` → `AWXKIT_USER` → `$USER`/`$USERNAME` 폴백).
- `awx/client.go`: Basic 인증 HTTP 클라이언트. `Ping`, `ListJobTemplates`, `ResolveTemplate`(ID 또는 이름), `GetSurveySpec`, `Launch`, `GetJob`, `GetJobStdout`, `SyncInventorySource`, `GetInventoryUpdate`, `CountInventoryHosts` — 2~6단계에서 바로 재사용할 수 있도록 API 매핑 전체를 미리 구현.
- `main.go` / `doctor.go`: 전역 플래그(`-conf`, `-user`), 인자 없을 때 번호 선택 메뉴(스피너/화면 재그리기 없음, ETX 환경 고려), `doctor` 명령 — conf 파일 권한 경고(`chmod 600` 안내, Windows는 건너뜀), AWX 연결/버전, 템플릿 개수, S1/S3/S4 설정 템플릿의 존재·실행 권한·`ask_variables_on_launch` 점검.
- `conf/sample_setting.conf` 추가.
- **검증**: Rocky Linux(192.168.0.58)로 소스 전송 후 `go build`/`go vet` 통과. Python `http.server`로 `/api/v2/ping/`, `/api/v2/job_templates/` 스텁을 띄워 `doctor`의 정상 경로(연결 성공, 템플릿 권한/`ask_variables_on_launch` 경고 출력)와 오류 경로(서버 연결 실패, conf 파일 없음, 사용자 미상)를 모두 실행 확인. 검증 후 VM의 임시 파일은 정리함.

### 다음 단계
- 2단계: `ls`(템플릿 목록) / `survey`(survey_spec → conf 스니펫 출력) 명령 구현. `awx/client.go`에 이미 필요한 메서드는 준비되어 있어 CLI 배선만 남음.

## 2026-08-19 (계속) — 2단계 구현

- `common.go` 신설: conf 로딩 + AWX 클라이언트 생성을 `loadConfigAndClient()`로 공용화. `doctor.go`가 이 헬퍼를 쓰도록 리팩터링(동작 변화 없음).
- `catalog.go`: `runLs`(템플릿 ID·이름·extra_vars 허용 여부·survey 유무 표 출력), `runSurvey`(survey_spec 조회 → 질문명·변수명·선택지·기본값·필수여부 출력, 비-survey/미존재 템플릿 처리, 인자 생략 시 표준입력으로 프롬프트). AWX가 `choices`를 줄바꿈 문자열 또는 배열 어느 쪽으로 주더라도 처리하도록 `formatChoices` 구현.
- `main.go`: `ls`/`survey` 명령 배선, `survey`용 `promptLine` 헬퍼 추가.
- **검증**: Rocky Linux(192.168.0.58)로 소스 전송 후 `go build`/`go vet` 통과. 스텁 서버에 `/api/v2/job_templates/{id}/`, `/survey_spec/` 엔드포인트를 보강해 이름/ID 조회, choices가 문자열·배열인 경우, survey 비활성 템플릿, 존재하지 않는 템플릿 4가지 경로를 모두 실행 확인. 검증 후 VM 임시 파일 정리함.

### 다음 단계
- 3단계: [S1] `nodeinfo -host <hostname>` 구현 — 템플릿 실행, Job 상태 폴링, 결과 취득(`s1_fetch`: artifacts/stdout/remote)과 `s1_output_dir` 저장까지.

## 2026-08-19 (계속) — 3단계 구현

- **설계 변경**: hostname을 `-host` 단일 플래그가 아니라 `${user}.txt`(conf와 동일한 탐색 규칙) 목록 파일로 받도록 사용자 요청 반영. hostname은 NodeInfo 단계에서만 필요하고 이후는 인벤토리 등록 상태를 기준으로 동작하기 때문. 또한 Go 바이너리를 bash로 쉽게 실행할 수 있는 환경이 필요하다는 요청에 따라 `run.sh` 래퍼 스크립트 추가.
- `config/config.go`: `ResolvePath`를 `ResolveNamedPath(explicit, filename)`로 일반화(conf 탐색 로직 재사용), `ReadHostList()` 추가(`#` 주석/빈 줄 무시), conf에 `s1_artifact_key`(artifacts에서 결과가 담긴 키, 미설정 시 전체 저장) 필드 추가.
- `common.go`: `pollJob`(상태 바뀔 때만 한 줄 출력, ETX 환경 고려), `printStdoutTail`(실패 시 마지막 N줄), `appendHistory`(`history_file`에 실행 이력 기록) 공용 헬퍼 추가 — 4~6단계(S2~S4)에서도 재사용 예정.
- `nodeinfo.go`: `${user}.txt`(또는 `-hosts`로 지정한 파일)의 hostname마다 `s1_template`을 개별 실행 → 폴링 → `s1_fetch`(artifacts/stdout/remote)에 따라 결과를 `s1_output_dir/{hostname}.yaml`에 저장. 실패 hostname은 stdout 마지막 30줄 출력 + 종료코드 1. 매 실행을 `history_file`에 기록.
- `main.go`: `-hosts` 플래그, `nodeinfo` 명령 배선.
- `conf/sample_setting.conf`(s1_artifact_key 추가), `conf/sample.txt`(hostname 목록 샘플) 추가.
- **버그 수정 2건** (검증 중 발견):
  1. `setup.sh`가 `go build -o awxkit main.go`로 **main.go 한 파일만** 빌드하고 있어 doctor.go/catalog.go/common.go/nodeinfo.go가 누락되어 있었음(원격 스텁 상태부터 있던 문제, 지금까지는 `go build .`로 직접 빌드해 검증해왔기 때문에 발견 못함). `go build -o awxkit .`로 수정.
  2. `setup.sh`/`go.mod`가 CRLF 줄바꿈으로 체크아웃되어 있어(Windows `core.autocrlf=true` 환경 특성) 리눅스에서 `setup.sh` 실행 시 `$'\r': command not found` 등으로 깨짐. LF로 정규화하고, 앞으로도 깨지지 않도록 `.gitattributes`(`*.sh`, `*.go`, `go.mod`를 `eol=lf`로 강제) 추가.
- **검증**: Rocky Linux(192.168.0.58)로 소스 전송 후 `go build`/`go vet` 통과. launch/poll/stdout/artifacts를 흉내내는 상태 저장형 스텁 서버로 성공 2건 + 실패 1건(stdout tail 확인) + `ignored_fields` 경고 + `history_file` 기록을 한 번에 검증. 이어서 `s1_artifact_key` 미설정 폴백, `-hosts` 오버라이드, 그리고 위 버그 수정 후 `bash run.sh doctor`(바이너리 없는 상태 → 자동 빌드 → 실행)까지 별도로 재확인. 검증 후 VM 임시 파일 정리함.

### 다음 단계
- 4단계: [S2] `invsync` — 인벤토리 소스 동기화 트리거 + 등록된 호스트 리스트 확인.

## 2026-08-19 (계속) — nodeinfo 실행 방식 수정 (hostname별 개별 실행 → 전체 일괄 실행)

- **사용자 정정**: NodeInfo 템플릿은 hostname을 텍스트로 한 번에 받아 처리하는 구조. `${user}.txt`의 hostname마다 launch를 반복하는 게 아니라, 전체 hostname을 한 번에 넣고 템플릿을 1회만 실행해야 함.
- `nodeinfo.go` 재작성: hostname 목록을 줄바꿈으로 이어붙여(`strings.Join(hosts, "\n")`) 하나의 extra_vars 값으로 전달, launch/poll/fetch를 모두 1회만 수행. 결과 저장 파일도 hostname별(`{hostname}.yaml`)이 아니라 실행 1회당 하나(`s1_output_dir/${user}_nodeinfo.yaml`)로 변경. `history_file` 기록도 hostname 개수(`hosts=N`)만 남기도록 조정.
- PLAN.md/README.md의 [S1] 관련 서술과 예시 출력을 새 동작에 맞게 갱신.
- **검증**: Rocky Linux(192.168.0.58)에서 `gofmt`/빌드/`go vet` 통과. extra_vars로 받은 hostname 텍스트를 그대로 파싱해 결과 YAML을 만드는 상태 저장형 스텁 서버로, 3개 hostname이 한 번의 launch·한 개의 결과 파일로 처리되는 것을 확인. 검증 후 VM 임시 파일 정리함.

## 2026-08-19 (계속) — 다운로드 후 양식 변환 확인(Y/N) 게이트 추가

- **요구사항**: 다운로드된 결과를 정해진 양식으로 바꾸는 스크립트가 별도로 있음. 사용자가 다른 터미널에서 그 스크립트를 직접 실행하고, 완료되면 awxkit에서 Y를 눌러 진행하는 방식. awxkit은 스크립트를 직접 실행하지 않고, 사람의 확인(Y/N)만으로 완료 여부를 판단(종료코드는 awxkit이 직접 실행하지 않으므로 볼 수 없음).
- `main.go`에 `promptYesNo()` 헬퍼 추가.
- `nodeinfo.go`: 결과 파일 저장 후 "[✔] 다운로드 완료"를 먼저 출력하고, 양식 변환 스크립트를 다른 터미널에서 실행하라는 안내 + `Y/N` 확인을 받도록 변경. `N`(또는 그 외 입력)이면 `history_file`에 `status=downloaded_unconfirmed`로 기록하고 종료 코드 1. `Y`면 기존과 같이 `status=successful`로 기록(`format_confirmed=true` 추가).
- README/PLAN 갱신: 실행 예시에 확인 프롬프트 추가, 설계 결정 표에 "양식 변환 확인" 항목 추가.
- **검증**: Rocky Linux(192.168.0.58)에서 `gofmt`/빌드/`go vet` 통과. 표준입력으로 `y`/`n`을 각각 넣어 두 경로(성공 확정 / downloaded_unconfirmed) 모두 확인. 검증 후 VM 임시 파일 정리함.

## 2026-08-19 (계속) — 4단계 구현

- `awx/client.go`: `Host` 구조체와 `ListInventoryHosts()`(이름·활성 여부 포함 전체 목록, 최대 500개) 추가. 기존 `CountInventoryHosts`는 유지(4단계에서는 미사용, S4에서 재사용 예정).
- `common.go`: `pollInventoryUpdate()` 추가 — `pollJob`과 같은 패턴(상태 바뀔 때만 출력)으로 인벤토리 동기화 상태를 폴링.
- `invsync.go`: `s2_inventory_source` 동기화 트리거 → `pollInventoryUpdate`로 완료 대기 → 실패면 종료 코드 1 → 성공이면 `s2_inventory`(설정된 경우)의 등록 호스트 전체를 이름+활성 여부로 나열. `s2_inventory` 미설정 시 호스트 목록 조회는 건너뜀. 모든 단계를 `history_file`에 기록.
- `main.go`: `invsync` 명령 배선.
- **미확정 리스크 명시**: [S1]이 만든 결과 파일을 AWX가 실제로 인벤토리 소스로 인식하게 만드는 절차(수동 배치/Git 프로젝트 커밋/SCP 등)는 여전히 현장 확인이 필요함. 현재 `invsync`는 이미 등록된(또는 이미 다른 경로로 채워질) 소스의 동기화 트리거·결과 확인만 구현하고, 이 부분은 PLAN.md 리스크 항목에 남겨둠.
- **검증**: Rocky Linux(192.168.0.58)에서 `gofmt`/빌드/`go vet` 통과. inventory_source별로 다른 결과(성공/실패)를 흉내내는 상태 저장형 스텁 서버로 성공+호스트 목록, 실패, `s2_inventory` 미설정 3가지 경로 모두 확인. 검증 후 VM 임시 파일 정리함.

### 다음 단계
- 5단계: [S3] `dhcp` — 인프라 선택지를 번호/이름으로 받아 DHCP 템플릿 실행, 최종 상태 즉시 출력, 설정 변경 검증 권고 문구 출력.

## 2026-08-19 (계속) — 5단계 구현

- `dhcp.go`: `-infra`(번호 또는 값, 생략 가능) → `s3_infra_choices`가 있으면 번호/값 검증 또는 번호 선택 메뉴, 없으면 자유 입력 → `s3_template` 실행 → 폴링 → 성공/실패 즉시 출력. 실패 시 stdout 마지막 30줄, 성공/실패와 무관하게 항상 설정 변경 검증 권고 문구(`settingChangeWarning`) 출력.
- `main.go`: `-infra` 플래그, `dhcp` 명령 배선. 플래그가 명령보다 반드시 앞에 와야 한다는 사용법 안내를 usage 상단에 명시(Go `flag` 패키지가 첫 비플래그 인자에서 파싱을 멈추는 특성 때문 — 검증 중 `awxkit dhcp -infra 1`로 테스트하다가 발견).
- **검증**: Rocky Linux(192.168.0.58)에서 `gofmt`/빌드/`go vet` 통과. 인프라 값에 따라 성공/실패를 다르게 응답하는 스텁 서버로 번호 지정(성공)·값 지정(성공)·값 지정(실패, stdout tail 확인)·목록에 없는 값(오류)·범위 밖 번호(오류)·대화형 메뉴 선택 6가지 경로 모두 확인. 검증 후 VM 임시 파일 정리함.

### 다음 단계
- 6단계: [S4] `pxe` — 인프라·OS 버전·Boot Mode·Splunk 설치 여부 4개 옵션 조합 실행, 완료 후 대상 인벤토리 호스트 수 리포트("총 N대의 호스트가 등록 완료되었습니다").

## 2026-08-19 (계속) — 6단계 구현 (4대 시나리오 전부 완료)

- `config/config.go`: `s4_infra_choices`/`s4_osver_choices`/`s4_bootmode_choices`/`s4_splunk_choices`(전부 선택 사항) 필드 추가.
- `pxe.go`: `dhcp.go`의 `resolveChoice`/`parseChoices`를 그대로 재사용해 4개 옵션(인프라/OS 버전/Boot Mode/Splunk 설치 여부)을 각각 번호·값·생략(선택지 있으면 메뉴, 없으면 자유입력)으로 받음 → `s4_template` 실행 → 폴링 → 실패 시 stdout tail + 종료 코드 1 → 성공 시 `s4_inventory`가 설정되어 있으면 `CountInventoryHosts`로 "총 N대의 호스트가 등록 완료되었습니다." 출력.
- `main.go`: `-os`/`-boot`/`-splunk` 플래그(‑infra는 dhcp/pxe 공용) 추가, `pxe` 명령 배선.
- **검증**: Rocky Linux(192.168.0.58)에서 `gofmt`/빌드/`go vet` 통과. 인프라 값에 따라 성공/실패를 다르게 응답하는 스텁 서버로 4개 플래그 전부 지정한 성공+호스트 수 집계, 실패(stdout tail), `s4_inventory` 미설정(집계 건너뜀) 3가지 경로 확인. 검증 후 VM 임시 파일 정리함.
- 이로써 계획서의 4대 핵심 시나리오(NodeInfo/인벤토리 동기화/DHCP/PXE)가 모두 구현됨. 남은 것은 7단계(폐쇄망 패키징 정비 — vendor 실채움, 크로스컴파일 확인)뿐.

### 다음 단계
- 7단계: 폐쇄망 패키징 정비 — 실제로는 외부 의존성이 없어 `vendor/`가 비어 있어도 되는 상태이므로, README에 이 점을 명확히 하고 크로스컴파일(`GOOS=linux GOARCH=amd64`) 절차를 재확인.
