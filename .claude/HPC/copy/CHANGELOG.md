# CHANGELOG — copy (closed-network-copy)

## 2026-09-13

### 신규
- 최초 구현. ETX(폐쇄망) → AWX → Windows 단방향 텍스트 복사 브릿지.
- `copy-send` (ETX/Linux CLI): `{user}_copy.txt` 를 읽어 AWX Job Template의
  `description` 필드에 그대로 등록. YAML 변수(extra_vars) 대신 description을 쓴
  이유: YAML 문법 오류 시 저장 자체가 실패할 위험이 있어, 서식 없는 필드가 더 안전함.
- `copy-send`는 AWX에 아직 수신되지 않은 이전 데이터가 남아 있으면 기본적으로
  전송을 거부(`-force`로 강제 가능) — 잔여 데이터를 조용히 덮어써 유실시키는 사고 방지.
- `max_lines` 설정으로 과도하게 긴 텍스트의 AWX 웹페이지 부하를 사전 차단.
- `copy-widget` (Windows GUI): 처음에는 Fyne/Walk 같은 외부 GUI 라이브러리를
  검토했으나, "이모지처럼 생기기만 하면 되고 복사 기능만 있으면 된다"는 요청에 따라
  외부 의존성 없이 표준 라이브러리 `syscall`로 Win32 API를 직접 호출하는 방식으로 결정.
  덕분에 `go.mod`에 `require`가 하나도 없어 폐쇄망 오프라인 빌드가 더 단순해짐.
- 위젯은 항상 위(always-on-top) 소형 창(📋 불러오기) 하나로 구성:
  - 왼쪽 클릭 → AWX 조회 → 클립보드 복사 → 내용을 메시지창으로 표시 → AWX 값 초기화(cleanup)
  - 드래그로 위치 이동, 오른쪽 클릭으로 종료
  - 자동 폴링 없음 — 클릭할 때만 조회 (AWX 부하 및 불필요한 API 호출 방지, 사용자 확정 사항)
- `internal/config`: 두 실행 파일이 공유하는 `copy_setting.conf`(평문 key=value) 파서.
- `internal/awx`: description 조회/갱신/초기화만 제공하는 최소 AWX REST 클라이언트.
- `setup.sh`(ETX 빌드) / `build.bat`(Windows 빌드) / `send.sh`(실행 래퍼) 추가.
- `internal/config`에 단위 테스트 추가 (정상 파싱, 기본값, 필수 키 누락, 문법 오류, 경로 탐색).

### 구현 중 확인한 사항
- `go vet ./...` 를 **Windows에서** 돌리면 `copy-widget`의 클립보드 코드
  (`GlobalAlloc`/`GlobalLock`으로 얻은 주소를 `unsafe.Pointer`로 변환하는 부분)에서
  `possible misuse of unsafe.Pointer` 경고가 남는다. `syscall.Syscall`을 직접 호출해
  변환 지점을 명확히 했지만 경고 자체는 사라지지 않는 것을 확인 — Win32 클립보드를
  표준 라이브러리만으로 다룰 때 흔히 발생하는 알려진 오탐으로 판단하고 그대로 둠
  (ARCHITECTURE.md 5번). CI는 Linux에서 돌아 `main_windows.go` 자체가 빌드 대상에서
  빠지므로 이 경고와 무관하다.
- Windows에 Go 툴체인이 없었으나(`go.dev` 다운로드 링크 안내 후 자동 설치하는 `.bat`을
  계획서에서는 검토했음), 이번 작업에서는 `winget install GoLang.Go`로 사용자 PC에
  Go 1.27.0을 직접 설치해 실제로 `go build`/`go vet`/`go test`/`build.bat`을 그
  자리에서 실행·검증했다 (위젯 프로세스를 실제로 띄워 즉시 종료되지 않는지도 확인).

### 수정한 버그 — 폐쇄망에서 조용히 인터넷을 요구하던 문제
- `go.mod`의 `go 1.27.0`이 실제 ETX 참조 환경(VM warestation의 Rocky Linux,
  192.168.0.58)에 설치된 Go 1.26.5보다 높아, `go build`가 매번 필요한 툴체인을
  인터넷에서 자동 다운로드하려 시도했다(Go 1.21+ 기본 동작인 `GOTOOLCHAIN=auto`).
  실제로 192.168.0.58(인터넷 되는 랩 환경)에 코드를 옮겨 처음 빌드했을 때
  `go: downloading go1.27.0 (linux/amd64)` 로 재현을 확인 — 진짜 폐쇄망이었다면
  이 시점에서 빌드가 실패했을 것이다.
- 수정: `go.mod`를 `go 1.26.5`(대상 host의 실제 설치 버전)로 낮추고, `setup.sh`/
  `build.bat` 양쪽에 `GOTOOLCHAIN=local`을 명시해 향후 go.mod 버전이 다시 오르더라도
  자동 다운로드를 시도하지 않도록 이중으로 막았다.
- 검증: 192.168.0.58에서 `GOTOOLCHAIN=local GOPROXY=off` 로 빌드/vet/테스트/`setup.sh`
  전체를 재실행해 네트워크 접근 없이 성공하는 것을 확인.

### 알려진 제약
- `copy_setting.conf`의 `password`는 평문. 커밋 금지(`.gitignore` 등록), 파일 권한 제한 필요.
- Job Template의 description은 이 브릿지 전용으로 써야 함 — 다른 자동화가 같은
  필드를 쓰면 값이 서로 덮어써진다.
- description은 해당 AWX Job Template을 열람할 수 있는 모든 사용자에게 노출된다.
  민감 정보 전송에는 쓰지 말 것.
- 위젯의 이모지(📋) 렌더링은 순수 GDI `DrawTextW` 의존이라, Windows 구성에 따라
  컬러 이모지 대신 흑백 윤곽선으로 보일 수 있다 — 기능에는 영향 없는 미관상 제약.
