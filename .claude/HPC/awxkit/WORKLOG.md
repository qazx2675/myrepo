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
