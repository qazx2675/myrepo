# ARCHITECTURE.md — copy (closed-network-copy)

| 폴더/파일 | 역할 |
|---|---|
| `setup.sh` | ETX(Linux) 오프라인 빌드. `GOPROXY=off`, `CGO_ENABLED=0` 정적 빌드로 `bin/copy-send` 생성 |
| `send.sh` | `copy-send` 실행 래퍼. 바이너리가 없으면 `setup.sh` 를 먼저 실행 |
| `build.bat` | Windows 빌드. `dist\copy-widget.exe` 생성 |
| `cmd/copy-send/main.go` | ETX 송신 CLI. 설정 로드 → 파일 읽기(원문 그대로) → 줄 수 검사 → AWX 템플릿 조회 → description PATCH |
| `cmd/copy-widget/main_windows.go` | Windows 위젯 본체 (`//go:build windows`). Win32 API를 `syscall`로 직접 호출해 항상 위 창을 그리고, 클릭 시 조회→클립보드 복사→표시→초기화를 수행 |
| `cmd/copy-widget/main_other.go` | Windows 이외 OS용 스텁 (`//go:build !windows`). CI가 Linux에서도 `go build ./...` 를 돌릴 수 있게 함 |
| `internal/config/config.go` | `copy_setting.conf`(평문 key=value) 파싱. `copy-send`/`copy-widget` 공용 |
| `internal/awx/client.go` | AWX REST API 최소 클라이언트. Job Template의 `description` 필드 조회(`GetDescription`)/갱신(`SetDescription`)/초기화(`ClearDescription`)만 제공 |
| `conf/copy_setting.conf.sample` | 설정 예시. 실제 값 파일은 `.gitignore` 로 커밋 차단 |

## 수정 요청별로 볼 곳

| 요청 | 볼 파일 |
|---|---|
| "저장 위치를 description 대신 extra_vars(YAML)로 바꿔줘" | `internal/awx/client.go`(Get/SetDescription을 extra_vars PATCH로 교체), `cmd/copy-send/main.go`, `cmd/copy-widget/main_windows.go` 양쪽의 호출부, 그리고 YAML 유효성 검증 로직 추가 필요 (README 4번 위험요인 참고) |
| "conf 스키마에 키 추가" | `internal/config/config.go`, `conf/copy_setting.conf.sample`, README 3.3절 |
| "위젯 UI/동작 변경" | `cmd/copy-widget/main_windows.go` 의 `wndProc`(입력 처리), `onPaint`(그리기), `handleFetch`(조회 로직) |
| "위젯이 자동으로 주기 조회하게 해줘" | `cmd/copy-widget/main_windows.go` `main()`의 메시지 루프 — Win32 `SetTimer`로 `WM_TIMER` 추가 후 `handleFetch` 호출 |
| "copy-send CLI 옵션 추가" | `cmd/copy-send/main.go` |
| "AWX 호출 방식 변경 (인증/타임아웃 등)" | `internal/awx/client.go` |

## 반드시 지킬 것

1. **description 필드는 이 브릿지가 유일한 쓰기 주체라고 가정합니다.** 같은 Job
   Template의 description을 다른 자동화가 동시에 쓰면 경합이 생깁니다 — 브릿지
   전용 템플릿을 따로 만들어 쓰십시오 (README 5번 주의사항).
2. **`copy-send`는 기본적으로 이전 미수신 데이터를 덮어쓰지 않습니다** (`-force`
   없이는 description이 비어있지 않으면 오류로 중단). 이 가드를 지우면 Windows가
   미처 읽지 못한 데이터가 조용히 유실될 수 있습니다.
3. **`copy-widget`은 클립보드 복사가 성공한 뒤에만 AWX를 초기화합니다.** 순서를
   바꿔 초기화를 먼저 하면, 클립보드 복사가 실패했을 때 원본 데이터를 이미
   잃어버리게 됩니다 (`handleFetch` 함수의 호출 순서 참고).
4. **텍스트는 원문 그대로 보존해야 합니다.** `copy-send`는 파일을 바이트 그대로
   읽어 JSON 문자열로 감싸 전송하고(공백/개행이 JSON 인코딩으로 보존됨), `copy-widget`은
   받은 문자열을 가공 없이 그대로 클립보드에 씁니다. 중간에 `strings.TrimSpace`
   등으로 원문을 건드리지 마십시오 (조건 판단에만 트림된 사본을 쓰고, 실제
   전송/복사에는 항상 원본 변수를 쓰는 이유입니다).
5. **`go vet`을 Windows에서 전체 실행하면 `cmd/copy-widget`의 클립보드 코드에서
   `possible misuse of unsafe.Pointer` 경고가 뜹니다 — 알려진 오탐입니다.**
   `GlobalAlloc`/`GlobalLock`이 반환하는 주소는 Win32가 관리하는 OS 힙 메모리이지
   Go GC가 추적하는 객체가 아니라서 실제로는 안전하지만, `go vet`의 정적 분석은
   이를 구분하지 못합니다. `syscall.Syscall`을 직접 호출해 변환 지점을 명확히
   해뒀지만 경고 자체는 남습니다. 이 경고를 없애려고 `unsafe` 사용을 억지로
   우회하지 마십시오 — Win32 클립보드 API를 표준 라이브러리만으로 쓰려면 불가피한
   패턴입니다. CI는 Linux에서 돌기 때문에 이 파일 자체가 빌드 대상에서 빠져
   경고가 나타나지 않습니다 (`main_windows.go`의 `//go:build windows` 태그).
6. **`internal/awx`는 이 프로젝트 전용입니다.** `.claude/HPC/awxkit/awx`(다른
   프로젝트)와 코드가 비슷해 보여도 공유 패키지가 아닙니다 — 각 HPC 프로젝트가
   자기 완결적으로 빌드되어야 한다는 저장소 관례(예: `ldap_setting`, `ip_change`가
   각자 `internal/`을 갖는 것)를 따른 것입니다. 한쪽만 고치고 다른 쪽은 그대로
   둬도 안전합니다.
