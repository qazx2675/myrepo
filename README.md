# AWX_copy

AWX 인벤토리 `variables` 를 중계로 써서 레드햇(ETX) 터미널의 **텍스트 파일**을 윈도우로 가져온다.

- 업로더(리눅스): `/usr/local/bin/awx_copy` (Go 단일 바이너리. bash 래퍼 없음)
- 다운로더(윈도우): `dist/awx_fetch.exe` (같은 Go 코드. Edge/Chrome 앱 창, cmd 창 없음)
- 인벤토리 `variables` 만 PATCH 한다. 작업 템플릿, 인벤토리 소스는 호출·변경하지 않는다.
- 표준 라이브러리만 쓴다. go 1.20 이상에서 빌드된다.


> **용도**: 업무에 필요한 서버 설정값 비교를 위해 사용되는 툴로, 업무 시 텍스트 기록을 위한 목적으로 가져오는 데만 사용됩니다.

## 1) 빌드 및 설치

### 폐쇄망(오프라인) 빌드

이 저장소를 **통째로 내려받아(ZIP 또는 `git clone`)** 폐쇄망 PC로 옮긴다. 외부 의존성이 없다(Go 표준 라이브러리만 사용, `go.mod` 에 `require` 없음).
그래서 `vendor/` 폴더가 비어 있어도 되고, 인터넷 접속 없이 **Go 1.20 이상 툴체인만 있으면** 빌드된다.

```bash
bash setup.sh          # 리눅스: bin/awx_copy 빌드 (GOPROXY=off, -mod=vendor)
```

```bat
build.bat              :: 윈도우: distwx_fetch.exe 빌드 (-H windowsgui)
```

빌드된 실행파일이 이미 들어 있다. Go 가 없는 PC 는 그대로 쓰면 된다.

| 실행파일 | 대상 |
|---|---|
| `bin/awx_copy` | 리눅스 amd64 정적 바이너리 (go 1.20.14) |
| `dist/awx_fetch.exe` | 윈도우 amd64 창 앱 |

### 리눅스 설치 (root)


```bash
./build.sh            # bin/awx_copy (CGO_ENABLED=0 정적 빌드. RHEL6 은 go 1.20 으로 빌드)
sudo ./install.sh     # 또는: ./install.sh <바이너리 경로>
```

`install.sh` 가 하는 일:

| 대상 | 권한 | 내용 |
|---|---|---|
| `/usr/local/bin/awx_copy` | root:root 755 | 바이너리 |
| `/etc/awx_copy/` | root:root 755 | 설정 폴더 |
| `/etc/awx_copy/root.conf` | root:root 600 | 없을 때만 생성. 기본 root 기록 조회 비밀번호의 **SHA-256 해시만** 저장 (`password_sha256 = ...`). 평문은 스크립트·바이너리·파일 어디에도 없다. 설치 뒤 `awx_copy --root-passwd` 로 바꾼다. |
| `/tmp/.awx_copy/` | 1777 | 업로드 기록 폴더 |

사용자 설정은 관리자가 배포한다:

```bash
cp conf.example/user.conf.example /etc/awx_copy/test.kim.conf
vi /etc/awx_copy/test.kim.conf               # url, username, password, inventory_id
chown test.kim /etc/awx_copy/test.kim.conf   # 소유자 = 해당 OS 사용자
chmod 600 /etc/awx_copy/test.kim.conf
```

`/etc/awx_copy` 는 755 다. 일반 사용자가 자기 `{user}.conf` 를 읽고 선택 목록을 보려면 폴더 탐색·목록 권한이 필요하기 때문이다.
conf 파일은 600(소유자=해당 사용자)이라 다른 사용자는 내용을 읽지 못한다.
대신 폴더 목록으로 **사용자 이름(파일명)은 모든 사용자에게 보인다**. 선택 목록에는 읽을 수 있는 파일만 나온다.
`root.conf` 는 root 소유 600 그대로다.

## 2) 사용 방법


```bash
awx_copy -u {user} {filename}        # 업로드 (type txt). 끝나면 기록 화면
awx_copy -u {user} {filename} -csv   # 업로드 (type csv)
awx_copy -u {user}                   # 내 업로드 기록 화면
awx_copy                             # 설정 선택 화면(읽을 수 있는 /etc/awx_copy/*.conf) → 기록 화면
awx_copy -u root                     # root 비밀번호 확인 뒤 전체 사용자 기록 (AWX 계정 아님, 조회 전용)
awx_copy --root-passwd               # root 기록 조회 비밀번호 변경 (root 만)
awx_copy --help
```

- 어느 경로에서든 실행한다. 파일 경로는 상대/절대 모두 된다.
- 두 번째 위치 인자 `-csv` 가 없으면 type 은 txt 다. (호환: `csv`, `txt` 도 받는다)
- 업로드 중에는 한 줄 진행 화면을 갱신한다: `[k/N] 전송 중 [####----] 45.0%  12.3초  120.4 KiB/s  파일명`.
  표준 출력이 터미널이 아니면 `[k/N] 전송 중 (n bytes)` 한 줄 로그로 대신한다.
- Ctrl+C 는 variables 를 원복하고 종료한다(종료 코드 130). 기록에 "중단"이 남는다.
- 기록 화면: 최근 순 표. 위/아래, PgUp/PgDn, Home/End 로 이동, q 로 닫는다. 터미널이 아니면 표를 그대로 출력한다.

유지한 옵션: `--user`(= `-u`), `--config <경로>`, `--list-users`, `--status`, `--snapshot`, `--measure-limits`, `--restore`, `--fetch`, `--out`, `--no-pause`.


윈도우: `awx_fetch.exe` 를 더블클릭해 창에서 사용자 선택 → `[지금 가져오기]` 또는 `[자동 감시]`. 자세한 내용은 아래 "윈도우" 절.

## 3) 옵션별 상세 설명

| 옵션 | 뜻 |
|---|---|
| `-u, --user <이름>` | `/etc/awx_copy/<이름>.conf` 사용. 없으면 선택 화면 |
| `--config <경로>` | 그 설정파일만 사용 |
| `--list-users` | 읽을 수 있는 설정파일 목록 |
| `-csv` | 두 번째 위치 인자. type 을 csv 로 기록 (기본 txt). 전송 방식은 같다 |
| `--restore` | 남은 awx_copy 블록을 원본 variables 로 복구 |
| `--status` | variables 길이/SHA-256/블록 여부 (GET 만) |
| `--snapshot` | 작업 템플릿/인벤토리 소스 modified 출력 (GET 만, 변경 0건 확인용) |
| `--measure-limits [--measure-max-mib N]` | variables 본문 한도 측정 (끝나면 원복) |
| `--root-passwd` | root 기록 조회 비밀번호 변경 (root 만) |
| `--fetch`, `--out`, `--no-pause` | 리눅스 바이너리로 수신할 때의 옵션 |
| `--cli`, `--no-browser` | 윈도우 창 앱: 창 없이 1건 수신, 브라우저를 열지 않음 |
| `--version`, `-h` | 버전, 도움말 |

### /etc 구조와 설정파일

```
/etc/awx_copy/
  ├─ root.conf        root 600   password_sha256 = <hex>   (기록 조회용, AWX 계정 아님)
  ├─ test.kim.conf    test.kim 600
  └─ kim.conf         kim 600
```

| 키 | 뜻 | 기본값 |
|---|---|---|
| url | AWX 주소 | (필수) |
| username | AWX 사용자 | (필수) |
| password | 비밀번호 | (필수) |
| inventory_id | 중계 인벤토리 ID (사용자마다 다름) | (필수) |
| chunk_size | 조각 하나의 최대 문자 수 (본문 추정 3 MiB 상한도 적용) | 1048576 |
| timeout | 상대측 응답 대기와 HTTP 요청의 최대 시간(초) | 300 |

- 권한이 600 이 아니거나 소유자가 실행 사용자와 다르면 거부한다. root 는 소유자 검사를 건너뛴다(600 은 필요).
- `-u {user}` 는 `/etc/awx_copy/{user}.conf` 를 쓴다. `~/.awx_copy/` 탐색과 "OS 사용자 이름이면 바로 실행" 동작은 없앴다.
- `-u` 없이 실행하면 읽을 수 있는 `*.conf`(root.conf 제외) 목록을 user, url, inventory_id 로 보여 주고 화살표/숫자로 고른다. 비밀번호는 읽거나 표시하지 않는다.
- 같은 사용자 conf(같은 inventory_id)를 쓰는 업로더와 다운로더가 서로 만난다.
- 윈도우 다운로더는 exe 옆 `conf\{user}.conf` 를 쓴다 (아래 "윈도우" 절).

### 업로드 기록

- 위치: `/tmp/.awx_copy/{user}.history.jsonl` (폴더 1777, 파일 600). `/tmp` 는 재부팅 때 지워질 수 있다.
- 한 줄(JSON): `ts, user, file, type, size, chunks, seconds, result(성공|실패|중단), message`.
- 심볼릭 링크나 다른 사용자 소유 파일에는 쓰지 않는다(root 제외).
- `-u root`: 비밀번호를 숨김 입력으로 받아 SHA-256 을 `root.conf` 해시와 상수 시간 비교한다.
  3회 틀리면 종료 코드 1. 맞으면 `/tmp/.awx_copy/*.history.jsonl` 전체를 사용자 열과 함께 보여 준다.
  `root.conf` 와 다른 사용자 기록 파일을 읽어야 하므로 root 권한이 필요하다. 비밀번호는 로그·오류·기록에 남지 않는다.

### 전송 프로토콜 (v2, 평문 텍스트)

1. 업로더가 파일을 읽어 문자 수 기준으로 나눈다(멀티바이트·CRLF 중간에서 자르지 않음).
2. 시작 전 `variables` 원문을 `~/.awx_copy/backup/`(실행 사용자 홈)과 블록의 `backup_b64` 에 둔다.
3. 조각 k 를 `chunk_text: |2-`(또는 `|2+`) YAML 리터럴 블록으로 PATCH 한다. 다운로더가 받아 `state: ack`, `ack: k` 를 PATCH 하면 다음 조각을 보낸다.
4. 다운로더는 조각마다 SHA-256 을, 끝에 전체 SHA-256 을 검증하고 저장한다.
5. **결과 전달**: 다운로더가 마지막 조각(또는 실패한 조각)에서 `state: done`, `result: ok|fail`, `message`, `ack` 를 남긴다.
   업로더는 그것을 읽어 성공/실패를 정하고 기록에 남긴 뒤 `variables` 를 원복한다. 조각이 1개여도 같다.

```yaml
awx_copy:
  version: "2"
  session: "20261117T025748-4ceb84cc9f1fadad"
  state: "sending"          # sending | ack | done
  file_name: "data.csv"
  type: "csv"
  encoding: "text"
  eol: "crlf"               # lf | crlf | cr | mixed
  final_newline: "true"
  total_bytes: "93"
  sha256: "..."
  chunk_total: "1"
  chunk_index: "1"
  chunk_sha256: "..."       # 조각 원본 바이트의 SHA-256
  chunk_text: |2+
    id,이름
    1,홍길동
  ack: "0"
  result: ""                # done 일 때 ok | fail
  message: ""               # 실패 이유
  backup_b64: ""
  updated_at: "..."
```

- 텍스트만 보낸다. 제어문자·잘못된 UTF-8·NEL·U+2028/2029·BOM 은 U+FFFD 로 바뀌고, LF 와 CR 이 섞인 파일(mixed)은 CR 이 LF 로 바뀐다. 둘 다 해시 불일치로 실패하고 저장하지 않는다. 확장자·바이너리 검사로 거부하지는 않는다.
- v2 수신기는 정의되지 않은 키(`chunk_raw` 등)가 있으면 거부한다.
- `variables` 에 이미 `awx_copy:` 블록이 있으면 다른 세션으로 보고 거부한다.

#### 원복 책임

| 상황 | 원복하는 쪽 | 업로더 결과 |
|---|---|---|
| 정상 수신 (`done/ok`) | 업로더 (결과를 읽은 뒤) | 성공 |
| 수신 실패 (`done/fail`, 해시 불일치 등) | 업로더 (결과를 읽은 뒤) | 실패 (message 표시) |
| `done` 을 남겼는데 업로더가 timeout 안에 원복하지 않음 | 다운로더 | (업로더 없음) |
| 업로더 Ctrl+C, 오류, ack 시간 초과 | 업로더 (자기 세션 블록일 때만) | 중단/실패 |
| 다운로더 Ctrl+C 등 결과 없이 종료 | 다운로더 | 실패 ("수신 결과 없이 원복") |
| kill -9, 전원 차단 | 없음 → `awx_copy -u {user} --restore` | - |

원복은 현재 블록이 자기 세션일 때만 한다. 결과(`done`) 없이 원본으로 돌아온 경우 업로더는 성공으로 보지 않는다.

### 윈도우 (다운로더 창 앱 `awx_fetch.exe`)

`awx_fetch.exe` 를 더블클릭하면 Edge(없으면 Chrome)의 `--app` 창이 열린다. 주소창과 탭이 없다. cmd 창은 뜨지 않는다(`-H windowsgui` 빌드).
Go 단일 exe 이며 화면(HTML/JS/CSS)은 `go:embed` 로 들어 있다. 서버는 `127.0.0.1` 에만 바인딩한다.

```bat
build.bat                                   :: dist\awx_fetch.exe (-H windowsgui)
awx_fetch.exe                               :: 창 앱
awx_fetch.exe --cli --user test.kim [--out D:\받은파일]   :: 창 없이 1건 수신 (시험용)
awx_fetch.exe --status --user test.kim      :: variables 상태 (GET 만)
awx_fetch.exe --restore --user test.kim     :: 남은 블록 복구
```

`--cli`, `--status`, `--restore` 의 출력: 콘솔이 없는 빌드라서 **exe 옆 `awx_fetch.log`(UTF-8)에 항상 남기고**, 부모 콘솔이 있으면 `AttachConsole` 로 거기에도 쓴다.
cmd 는 GUI exe 를 기다리지 않으므로 종료 코드가 필요하면 `start /wait` 또는 `Start-Process -Wait -PassThru` 로 실행한다.

#### 폴더

```
awx_fetch.exe
conf\                 {user}.conf (여러 개). url, username, password_enc(또는 password), inventory_id, chunk_size, timeout
received\             받은 파일 (기본 저장 폴더, 화면에서 바꿀 수 있음)
received\history.jsonl  받은 파일 기록 (UTF-8, 영구). 저장 폴더를 바꿔도 기록은 여기에 남는다
awx_fetch.json        화면 설정 (저장 폴더)
awx_fetch.log         동작 로그 (UTF-8, 5 MiB 넘으면 .1 로 돌림). 비밀번호는 쓰지 않는다
```

- 비밀번호는 Windows DPAPI(현재 사용자) 암호문 `password_enc` 로 저장한다. 다른 PC/사용자에서는 풀리지 않는다.
- 평문 `password` 도 읽는다. 화면에서 그 사용자를 고르면(연결 확인 성공 시) `password_enc` 로 바꿔 저장한다.
- 실제 conf 와 받은 파일, 로그는 `.gitignore` 에 있다.

#### 화면

| 화면 | 내용 |
|---|---|
| 사용자 선택 | `conf\*.conf` 목록(user, url, inventory_id, 암호화 여부). 비밀번호는 표시하지 않는다. 설정이 1개면 바로 선택한다. 선택할 때 인벤토리 GET 으로 연결을 확인한다. |
| 새 설정 만들기 | url, username, password, inventory_id 입력 → [연결 테스트] → [연결 테스트 후 저장] (`conf\{username}.conf`, password_enc). 같은 이름은 "덮어쓰기"를 체크해야 한다. |
| 수신 | [지금 가져오기]: 업로드를 timeout 까지 기다려 1건 받는다. [수신 중단]: 멈추고 자기 세션이면 variables 원복. [자동 감시]: 2초마다 GET 해서 새 업로드(1번 조각)를 보면 자동으로 받는다. 진행 막대에 `[k/N]`, 바이트, 경과 초, KiB/s 를 보여 준다. |
| 받은 파일 (관리 공간) | 저장 폴더(변경, 폴더 열기). 기록 표: 시각, 원본 파일명, 크기, 조각 수, SHA-256(정상/실패), 저장 경로, 사용자. 버튼: 열기, 폴더 열기, 경로 복사, 삭제(확인 창 뒤 파일만 삭제, 기록은 "삭제됨"). 최근 200건만 표시한다. |

- 같은 이름은 `_1`, `_2` 를 붙인다. 윈도우에서 쓸 수 없는 문자(`<>:"/\|?*`, 제어문자)는 `_` 로, 예약 이름(CON 등)은 앞에 `_` 를 붙인다.
- 해시 불일치(텍스트가 아닌 바이트, 섞인 줄바꿈 등)면 저장하지 않고 기록에 "실패"로 남긴다. 업로더에도 `result: fail` 이 전달된다.
- 변경 대상은 인벤토리 variables 뿐이다. 작업 템플릿·인벤토리 소스는 호출하지 않는다.

#### 수명 관리

- 창은 2초마다 `/api/ping` 을 보내고, 닫힐 때 `/api/bye` 비콘을 보낸다. 마지막 창이 bye 를 보내고 1초 안에 새 ping 이 없으면 종료한다(새로고침은 종료 아님).
- 비콘 없이 창이 죽으면 ping 이 15초 끊긴 뒤 종료한다. 창이 60초 안에 한 번도 붙지 않으면 종료한다.
- 종료할 때 진행 중 수신을 멈추고, variables 가 자기 세션 블록이면 원복한다.
- 이미 실행 중이면(이름 있는 뮤텍스 `Local\AWX_copy_awx_fetch`) 새 실행은 기존 창을 앞으로 가져오고 바로 끝난다.

#### host-cross-checker 제작 중 문제와 대책

| 문제 | 대책 (이 앱) |
|---|---|
| cmd 창 표시 | `-H windowsgui` 빌드(PE subsystem 2). 브라우저는 `CREATE_NO_WINDOW` 로 실행. 파일/폴더 열기는 `ShellExecuteW`(자식 콘솔 없음). |
| 창 닫은 뒤 프로세스 잔존 | `/api/bye` 비콘 + `/api/ping` 감시. 실측 창 닫기 후 1.1초에 종료, 잔존 0. |
| HTTP 응답 Body 미닫힘 | 모든 AWX 요청은 한 헬퍼(`Client.do`)에서 `defer resp.Body.Close()`. 앞으로 가져오기 요청도 Body 를 비우고 닫는다. |
| 요청 무기한 블록 | AWX HTTP 클라이언트 timeout, 연결 확인 15초, 감시 GET 10초. 서버 `ReadHeaderTimeout/ReadTimeout/WriteTimeout/IdleTimeout`. 화면 fetch 는 `AbortController` 타임아웃. |
| 이벤트 리스너 중복 등록 | `window.__awxFetchInit` 초기화 가드. 버튼은 문서 하나의 click 위임 리스너. 폴링은 `setTimeout` 사슬(겹치지 않음). |
| 폴링 부하 | 상태는 수신 중 0.5초, 평소 2초. 기록은 `hist_rev` 가 바뀔 때만, 최근 200건만 읽는다. 자동 감시는 2초 간격 GET 1회. |
| AWX 401 | 선택·저장 때 인벤토리 GET 으로 인증을 먼저 확인하고 오류를 화면에 보여 준다. 비밀번호는 DPAPI 로 저장. |
| 한글 깨짐 | `<meta charset="utf-8">`, 폰트 스택 Pretendard / Malgun Gothic / Consolas. 로그·기록 파일은 UTF-8. |
| 포트 충돌 | 기본 18780 이 쓰이면 빈 포트에 바인딩. 포트는 `%TEMP%\awx_fetch.port` 에 쓴다. |
| 브라우저 프로필 충돌 | 독립 프로필 `%TEMP%\awx-copy-profile`. 20초 안에 창이 붙지 않으면 새 프로필로 한 번 더 연다. |
| 다른 사이트의 요청(CSRF) | Host 는 127.0.0.1/localhost:{포트} 만, Origin 은 같은 출처만, 상태 변경 POST 는 `X-AWX-Fetch` 헤더 필수. |

#### 자원 실측 (2026-11-17, Windows 11, Edge)

| 항목 | 값 |
|---|---|
| 유휴 CPU (30초) | 0.007% |
| 3.7 MiB 파일 수신 뒤 | WS 61.1 MB, Private 72.5 MB |
| 수신 25회 뒤 / 50회 뒤 / 15초 뒤 | WS 38.6 / 39.5 / 39.5 MB, 핸들 423 / 440 / 438, 스레드 14 / 19 / 16 |
| 수신 50회 결과 | 50/50 정상 (자동 감시) |
| 창 닫기 → 종료 | 1.1초 (수신 중 닫기 1.6초, variables 원복) |

화면: `docs/screens/fetch_main.png`, `fetch_users.png`, `fetch_new.png`.

### 한도

자세한 표는 `docs/limits.md`. 실제 AWX(v24.6.1) 측정.

- 최대 성공 본문 4.94 MiB. 5 MiB 부터 nginx 413.
- AWX 는 저장할 때 variables 끝의 공백/줄바꿈을 제거한다. `chunk_text` 뒤에 항상 다른 키를 두어 영향이 없다.

### 테스트

```bash
go vet ./... && go test ./...                 # 단위·통합 테스트 (가짜 AWX)
GOOS=windows go vet ./...                     # 윈도우 빌드 확인
test/make_files.sh /root/awx_e2e/files 1048576
test/e2e_send.sh <conf> /root/awx_e2e/files   # 설치본 awx_copy 사용
```

## 4) 문서별 설명


| 파일 | 내용 |
|---|---|
| `main.go` 외 `*.go` | Go 본체 (업로더/다운로더/기록/TUI/복원/측정) |
| `build.sh`, `build.bat` | 리눅스/윈도우 빌드 |
| `install.sh` | 리눅스 설치 (root) |
| `conf.example/user.conf.example`, `awx_copy.conf.example` | 설정 예시 (비밀번호 비움) |
| `bin/awx_copy` | 리눅스 amd64 정적 실행파일 (go 1.20.14) |
| `dist/awx_fetch.exe` | 윈도우 amd64 창 앱 (-H windowsgui) |
| `gui.go`, `gui_windows.go`, `webui/` | 창 앱 서버, 윈도우 API(DPAPI, 뮤텍스, ShellExecute), 화면 |
| `docs/screens/` | 창 앱 스크린샷 |
| `docs/PLAN_v2.md`, `docs/limits.md` | 계획서, 한도 측정 |
| `test/` | E2E 스크립트, PyYAML 검증 |
| `internal/fakeawx`, `cmd/fakeawx` | 테스트용 가짜 AWX |


| 문서 | 내용 |
|---|---|
| `README.md` | 이 문서 |
| `ARCHITECTURE.md` | 폴더/파일별 역할 표 |
| `WORKFLOW.md`, `workflow.svg` | 업로드·수신 흐름도 |
| `CHANGELOG.md` | 변경 이력 |
| `PR_CHECKLIST.md` | 배포·수정 전 점검표 |
| `사용법.txt` | 수동 테스트용 명령어 모음 |
| `setup.sh` | 폐쇄망 빌드 스크립트 |
| `.github/workflows/ci.yml` | 커밋마다 빌드/정적분석/테스트 |

## 주의사항 (Disclaimer)

- 본 로그 분석 관련 스크립트 및 툴은 100% 신뢰하기보다는 **참고용(보조 도구)** 으로 사용하는 것을 권장합니다.
- 설정 변경 스크립트를 쓴 뒤에는 **랜덤한 서버 몇 대를 직접 확인해서 실제로 변경되었는지** 확인하십시오.
- 이 도구는 **텍스트 파일 전용**입니다. 제어문자, 잘못된 UTF-8, BOM, CRLF 와 LF 가 섞인 파일은 복원되지 않고 수신이 실패합니다(저장되지 않고 variables 는 원복).
- 이 도구는 AWX 인벤토리 `variables` 만 바꿉니다. 비정상 종료가 겹치면 `awx_copy` 블록이 남을 수 있으니 `--status` 로 확인하고 `--restore` 로 복구하십시오.
- 설정파일(`*.conf`)에는 비밀번호가 들어 있습니다. 저장소에 커밋하지 마십시오. (`.gitignore` 로 제외됨)

## 5) 전역 명령어로 사용하기 (선택 사항)

어느 경로에서든 `awx_copy` 로 실행하려면 실행파일을 `PATH` 에 있는 폴더에 둔다. `install.sh` 가 `/usr/local/bin/awx_copy` 로 설치하므로 별도 작업이 필요 없다.

```bash
sudo ./install.sh
awx_copy --version      # 어느 경로에서든 실행되는지 확인
```

root 권한이 없으면 개인 폴더에 두고 `PATH` 에 추가한다.

```bash
mkdir -p ~/bin && cp bin/awx_copy ~/bin/ && echo 'export PATH=$HOME/bin:$PATH' >> ~/.bashrc
```
