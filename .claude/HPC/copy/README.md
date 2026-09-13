# copy (closed-network-copy)

**폐쇄망 단방향 텍스트 전송(복사) 브릿지.** ETX(RedHat, 폐쇄망)에서 Windows(개인 PC)로는
클립보드 복사가 안 되는 환경에서, 양쪽 모두 접속 가능한 **AWX**를 중계소로 써서
텍스트를 옮깁니다.

```
[ETX]  {user}_copy.txt --(copy-send)--> AWX Job Template.description --(copy-widget)--> [Windows 클립보드]
```

- ETX 쪽(`copy-send`)과 Windows 쪽(`copy-widget`) 모두 **Go 표준 라이브러리만 사용**합니다.
  외부 의존성이 없어 `go.mod`에 `require` 항목이 없고, 저장소를 통째로 내려받으면
  **폐쇄망에서 그대로 빌드**됩니다.
- 저장 위치는 Job Template의 **description 필드**만 사용합니다. YAML 변수(extra_vars)는
  문법이 틀리면 저장 자체가 실패할 수 있어 쓰지 않습니다.
- Windows 쪽은 전용 GUI 프레임워크 없이, 표준 라이브러리 `syscall`로 Win32 API를
  직접 호출하는 항상 위(always-on-top) 위젯입니다.

---

## 1. 빌드 및 설치 방법

### 1.1 사전 준비

| 항목 | 필요 위치 | 비고 |
|---|---|---|
| Go 툴체인 | ETX(빌드용), Windows | `go.mod` 기준 1.27.0. 빌드할 때만 필요, 실행에는 불필요 |
| AWX Job Template | AWX 서버 | 이 브릿지 전용으로 하나 만들어 두는 것을 권장 (아무 동작도 실행하지 않는 더미 템플릿이어도 무방) |

### 1.2 내려받기

```bash
git clone https://github.com/qazx2675/myrepo.git
cd "myrepo/.claude/HPC/copy"
```

### 1.3 ETX(송신측) 빌드 — `setup.sh`

```bash
./setup.sh
```

`GOPROXY=off` 로 인터넷 접속을 시도하지 않고 `bin/copy-send` 를 만듭니다.
수동 빌드:

```bash
CGO_ENABLED=0 go build -o bin/copy-send ./cmd/copy-send
```

### 1.4 Windows(수신측) 빌드 — `build.bat`

Windows PC에 Go가 없다면 https://go.dev/dl/ 에서 windows/amd64 설치본을 받아 설치하십시오
(또는 `winget install GoLang.Go`). 그다음 이 폴더에서:

```bat
build.bat
```

`dist\copy-widget.exe` 가 만들어집니다. 수동 빌드:

```bat
go build -o dist\copy-widget.exe .\cmd\copy-widget
```

### 1.5 설정 파일 채우기 (양쪽 모두)

```bash
cp conf/copy_setting.conf.sample conf/copy_setting.conf
```

- `conf/copy_setting.conf` — AWX 접속 정보와 대상 템플릿, 줄 수 제한.
  **password가 평문이므로 커밋 금지** (`.gitignore` 등록됨).
  **ETX와 Windows 양쪽의 이 파일 내용은 반드시 동일해야 합니다** (같은 AWX, 같은 템플릿을 봐야 하므로).

### 1.6 설치 확인

```bash
go test ./...     # 단위 테스트 (config 패키지)
go vet ./cmd/copy-send/... ./internal/...
```

> `go vet ./...` 를 **Windows에서** 전체 실행하면 `cmd/copy-widget`의 클립보드 코드에서
> `possible misuse of unsafe.Pointer` 경고가 하나 뜹니다. `GlobalLock`이 돌려주는 주소는
> Go GC가 관리하지 않는 OS 메모리를 가리키는 것이라 안전하지만, `go vet`은 이를 알 방법이
> 없어 관례적으로 뜨는 오탐입니다 (ARCHITECTURE.md 참고). CI는 Linux에서 돌기 때문에
> `cmd/copy-widget`의 Windows 전용 파일 자체가 빌드 대상에서 빠져 이 경고가 나타나지 않습니다.

---

## 2. 사용 방법

### 2.1 표준 절차

```bash
# 1) ETX: 보낼 내용을 파일로 준비
vi hong_copy.txt

# 2) ETX: AWX로 전송
./send.sh -user hong
# 전송 완료: hong_copy.txt (12줄, 340바이트) -> template="closed-network-copy"(id=7)
# Windows에서 오버레이 위젯의 불러오기를 눌러 확인하십시오.

# 3) Windows: dist\copy-widget.exe 를 실행해 둔 상태에서 위젯을 왼쪽 클릭
#    -> 클립보드에 자동 복사 + 내용이 메시지창으로 표시됨 + AWX 쪽 값 자동 초기화
```

### 2.2 위젯 조작

| 동작 | 결과 |
|---|---|
| 왼쪽 클릭 | AWX에서 조회 → 클립보드 복사 → 내용을 메시지창으로 표시 → AWX 값 초기화(cleanup) |
| 드래그 | 창 위치 이동 (항상 위 상태 유지) |
| 오른쪽 클릭 | 위젯 종료 |

위젯은 자동 폴링을 하지 않습니다. **클릭할 때만** AWX를 조회합니다 — 계획 단계에서
자동 주기 폴링과 수동 클릭 중 수동 클릭 방식으로 정했습니다 (AWX 부하 및 불필요한
API 호출 방지).

### 2.3 `copy-send` 직접 실행

```bash
./bin/copy-send -user hong                       # ./hong_copy.txt 를 전송
./bin/copy-send -file /tmp/my.txt                # 파일 경로를 직접 지정
./bin/copy-send -user hong -force                # 이전 미수신 데이터가 있어도 덮어쓰기
./bin/copy-send -conf /etc/copy-bridge/copy_setting.conf -user hong
```

기본적으로 AWX에 **아직 Windows가 읽지 않은 이전 데이터**가 description에 남아 있으면
전송을 거부합니다 (잔여 데이터 덮어쓰기로 인한 유실 방지). 정말 덮어쓰려면 `-force`.

---

## 3. 옵션별 상세 설명

### 3.1 `copy-send`

| 옵션 | 기본값 | 설명 |
|---|---|---|
| `-conf` | 자동 탐색 | 설정 파일 경로. 생략 시 `./conf/copy_setting.conf` → 실행파일 옆 `conf/` → `~/.copy-bridge/` 순으로 탐색 |
| `-file` | *(없음)* | 전송할 텍스트 파일 경로. 생략하면 `-user` 로 `{user}_copy.txt` 를 찾음 |
| `-user` | `$USER` | `-file` 을 생략했을 때 `{user}_copy.txt` 파일명을 만드는 데 사용 |
| `-force` | `false` | AWX에 아직 수신되지 않은 이전 데이터가 있어도 덮어씀 |

종료 코드: `0` 성공 / `1` 설정·파일·AWX 오류 (줄 수 초과, 이전 데이터 잔존 등 포함).

### 3.2 `copy-widget`

| 옵션 | 기본값 | 설명 |
|---|---|---|
| `-conf` | 자동 탐색 | 설정 파일 경로. 생략 시 `copy-send`와 동일한 순서로 탐색 |

CLI 인자가 사실상 이것뿐이라, 보통은 `dist\copy-widget.exe` 를 바로가기로 만들어 두고
더블클릭해서 씁니다.

### 3.3 `copy_setting.conf` 키

| 키 | 설명 |
|---|---|
| `awx_url` | AWX 서버 주소 |
| `username` / `password` | AWX Basic 인증 계정 |
| `insecure_tls` | `true`면 TLS 인증서 검증을 건너뜀 (사내 자체서명 인증서 환경용) |
| `template` | 브릿지로 쓸 Job Template. ID(숫자) 또는 이름 |
| `max_lines` | `copy-send`가 이 줄 수를 넘는 파일을 거부하는 임계값 (기본 200) |

---

## 4. 문서별 설명

| 파일 | 내용 |
|---|---|
| `README.md` | 이 문서 |
| `ARCHITECTURE.md` | 폴더/파일별 역할. 어디를 고쳐야 하는지 찾을 때 |
| `CHANGELOG.md` | 날짜순 변경 이력 |
| `PR_CHECKLIST.md` | 배포·수정 전 확인 목록 |
| `setup.sh` | ETX(Linux) 쪽 오프라인 빌드 — `bin/copy-send` 생성 |
| `build.bat` | Windows 쪽 빌드 — `dist\copy-widget.exe` 생성 |
| `send.sh` | `copy-send` 실행 래퍼. 바이너리가 없으면 자동으로 `setup.sh` 를 먼저 돌림 |
| `cmd/copy-send/main.go` | ETX 송신 CLI |
| `cmd/copy-widget/main_windows.go` | Windows 위젯 본체 (`//go:build windows`) |
| `cmd/copy-widget/main_other.go` | Windows가 아닌 OS에서 `go build ./...` 가 깨지지 않도록 하는 스텁 |
| `internal/config/` | `copy_setting.conf` 파싱. 송/수신 양쪽이 공유 |
| `internal/awx/` | AWX REST API 최소 클라이언트 (description 조회/갱신) |
| `conf/copy_setting.conf.sample` | 설정 예시 |

---

## 5. 주의사항 (Disclaimer)

본 도구는 **참고용(보조 도구)** 입니다. AWX 및 대상 서버 설정을 100% 신뢰하기보다,
아래 사항을 함께 확인하는 것을 권장합니다.

1. **AWX Job Template의 description은 이 브릿지 외 다른 용도로 쓰지 마십시오.**
   `copy-send`가 실행될 때마다 그 값을 덮어쓰고, `copy-widget`이 조회 후 곧바로
   빈 값으로 초기화합니다. 같은 템플릿을 다른 목적(실제 실행 이력 설명 등)으로
   쓰고 있다면 그 내용이 사라집니다.
2. **`copy_setting.conf`에는 AWX 비밀번호가 평문으로 들어갑니다.** 커밋하지 말고,
   파일 권한(chmod 600 / Windows는 해당 사용자만 읽기)을 제한하십시오.
3. **description은 AWX에 접속 가능한 누구나 볼 수 있습니다.** 같은 AWX를 쓰는
   다른 사용자가 그 템플릿을 열람할 권한이 있다면 전송 중인 텍스트가 노출될 수
   있습니다 — 민감 정보(비밀번호, 개인정보 등)는 이 경로로 옮기지 마십시오.
4. **`copy-widget`을 처음 실행한 뒤에는 실제로 클립보드에 원하는 내용이 복사됐는지
   `Ctrl+V`로 아무 곳에나 붙여넣어 눈으로 확인**하십시오. 특히 특수문자·탭·여러 줄이
   섞인 텍스트는 최초 1회 반드시 육안으로 검증할 것을 권장합니다.
5. `max_lines` 제한에 걸리면 `copy-send`가 전송 자체를 거부합니다. 큰 텍스트는
   여러 번에 나눠 보내십시오.
