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
