# ARCHITECTURE.md

> 업무에 필요한 서버 설정값 비교를 위해 사용되는 툴로, 업무 시 텍스트 기록을 위한 목적으로 가져오는 데만 사용됩니다.

| 폴더/파일 | 역할 |
|---|---|
| main.go | 진입점. 옵션 파싱, 업로드/수신/기록/복원 분기 |
| config.go, userconf.go | conf 읽기(권한·소유자 검사), conf 목록, `/etc/awx_copy` 와 `conf\` 경로 |
| awx.go | AWX REST 클라이언트 (inventory variables GET/PATCH) |
| proto.go, textproto.go | v2 평문 YAML 프로토콜: 조각, eol/final_newline, SHA-256, 키 검증 |
| send.go | 업로더: 조각 전송, ack 대기, 결과 읽기, 원복 |
| fetch.go | 수신기: 조각 수신, 해시 검증, 파일 저장, 결과 기록 |
| restore.go | 남은 블록 복구(`--restore`), 백업 |
| history.go, tui.go | 업로드 기록(`/tmp/.awx_copy`), root 조회, 선택/기록/진행 화면 |
| measure.go | 본문 한도 측정, 템플릿·소스 스냅샷 |
| gui.go, gui_windows.go, gui_other.go | 윈도우 창 앱 서버, 윈도우 API(DPAPI, 뮤텍스), 비윈도우 대체 |
| console_*.go | 콘솔 붙이기(윈도우) |
| webui/ | 창 앱 화면 (HTML/JS/CSS, go:embed) |
| internal/fakeawx, cmd/fakeawx | 테스트용 가짜 AWX |
| *_test.go, test/ | 단위·통합 테스트, E2E 스크립트 |
| install.sh, setup.sh, build.sh, build.bat | 설치·빌드 스크립트 |
| bin/, dist/ | 빌드된 실행파일 |
| docs/ | 계획서, 한도 측정, 스크린샷 |

수정 요청이 "OO 추가"면:
- 전송 형식 변경 → proto.go, textproto.go, send.go, fetch.go
- 옵션 추가 → main.go
- 윈도우 화면 변경 → gui.go, webui/
- 설정 키 추가 → config.go, userconf.go, conf.example/

작업 흐름도는 [WORKFLOW.md](WORKFLOW.md) 참고.
