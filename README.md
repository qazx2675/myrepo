# AWX_copy

AWX 인벤토리 `variables` 를 중계로 써서 레드햇(ETX) 터미널의 **텍스트 파일**을 윈도우로 가져온다.

- 업로더(리눅스): `/usr/local/bin/awx_copy` (Go 단일 바이너리. bash 래퍼 없음)
- 다운로더(윈도우): `dist/awx_fetch.exe` (같은 Go 코드. 창 앱은 Step 4)
- 인벤토리 `variables` 만 PATCH 한다. 작업 템플릿, 인벤토리 소스는 호출·변경하지 않는다.
- 표준 라이브러리만 쓴다. go 1.20 이상에서 빌드된다.

## 사용법 (리눅스)

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

## 설치 (리눅스, root)

```bash
./build.sh            # bin/awx_copy (CGO_ENABLED=0 정적 빌드. RHEL6 은 go 1.20 으로 빌드)
sudo ./install.sh     # 또는: ./install.sh <바이너리 경로>
```

`install.sh` 가 하는 일:

| 대상 | 권한 | 내용 |
|---|---|---|
| `/usr/local/bin/awx_copy` | root:root 755 | 바이너리 |
| `/etc/awx_copy/` | root:root 700 | 설정 폴더 |
| `/etc/awx_copy/root.conf` | root:root 600 | 없을 때만 생성. 기본 root 기록 조회 비밀번호의 **SHA-256 해시만** 저장 (`password_sha256 = ...`). 평문은 스크립트·바이너리·파일 어디에도 없다. 설치 뒤 `awx_copy --root-passwd` 로 바꾼다. |
| `/tmp/.awx_copy/` | 1777 | 업로드 기록 폴더 |

사용자 설정은 관리자가 배포한다:

```bash
cp conf.example/user.conf.example /etc/awx_copy/test.kim.conf
vi /etc/awx_copy/test.kim.conf               # url, username, password, inventory_id
chown test.kim /etc/awx_copy/test.kim.conf   # 소유자 = 해당 OS 사용자
chmod 600 /etc/awx_copy/test.kim.conf
```

주의: 요구사항대로 `/etc/awx_copy` 는 700(root) 이다. 이 상태에서는 root 만 안의 설정을 읽는다.
일반 사용자가 직접 실행하려면 관리자가 폴더를 711(목록 숨김, 자기 파일만 접근) 또는 755 로 바꿔야 한다.

## /etc 구조와 설정파일

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
- 윈도우 다운로더의 conf 규칙(exe 옆 `conf\`)은 Step 4 전까지 v1 그대로다.

## 업로드 기록

- 위치: `/tmp/.awx_copy/{user}.history.jsonl` (폴더 1777, 파일 600). `/tmp` 는 재부팅 때 지워질 수 있다.
- 한 줄(JSON): `ts, user, file, type, size, chunks, seconds, result(성공|실패|중단), message`.
- 심볼릭 링크나 다른 사용자 소유 파일에는 쓰지 않는다(root 제외).
- `-u root`: 비밀번호를 숨김 입력으로 받아 SHA-256 을 `root.conf` 해시와 상수 시간 비교한다.
  3회 틀리면 종료 코드 1. 맞으면 `/tmp/.awx_copy/*.history.jsonl` 전체를 사용자 열과 함께 보여 준다.
  `root.conf` 와 다른 사용자 기록 파일을 읽어야 하므로 root 권한이 필요하다. 비밀번호는 로그·오류·기록에 남지 않는다.

## 전송 프로토콜 (v2, 평문 텍스트)

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

### 원복 책임

| 상황 | 원복하는 쪽 | 업로더 결과 |
|---|---|---|
| 정상 수신 (`done/ok`) | 업로더 (결과를 읽은 뒤) | 성공 |
| 수신 실패 (`done/fail`, 해시 불일치 등) | 업로더 (결과를 읽은 뒤) | 실패 (message 표시) |
| `done` 을 남겼는데 업로더가 timeout 안에 원복하지 않음 | 다운로더 | (업로더 없음) |
| 업로더 Ctrl+C, 오류, ack 시간 초과 | 업로더 (자기 세션 블록일 때만) | 중단/실패 |
| 다운로더 Ctrl+C 등 결과 없이 종료 | 다운로더 | 실패 ("수신 결과 없이 원복") |
| kill -9, 전원 차단 | 없음 → `awx_copy -u {user} --restore` | - |

원복은 현재 블록이 자기 세션일 때만 한다. 결과(`done`) 없이 원본으로 돌아온 경우 업로더는 성공으로 보지 않는다.

## 윈도우 (다운로더, Step 4 전)

```bat
build.bat             :: dist\awx_fetch.exe
awx_fetch.exe --config D:\conf\test.kim.conf --out D:\받은파일
```

## 한도

자세한 표는 `docs/limits.md`. 실제 AWX(v24.6.1) 측정.

- 최대 성공 본문 4.94 MiB. 5 MiB 부터 nginx 413.
- AWX 는 저장할 때 variables 끝의 공백/줄바꿈을 제거한다. `chunk_text` 뒤에 항상 다른 키를 두어 영향이 없다.

## 테스트

```bash
go vet ./... && go test ./...                 # 단위·통합 테스트 (가짜 AWX)
GOOS=windows go vet ./...                     # 윈도우 빌드 확인
test/make_files.sh /root/awx_e2e/files 1048576
test/e2e_send.sh <conf> /root/awx_e2e/files   # 설치본 awx_copy 사용
```

## 파일

| 파일 | 내용 |
|---|---|
| `main.go` 외 `*.go` | Go 본체 (업로더/다운로더/기록/TUI/복원/측정) |
| `build.sh`, `build.bat` | 리눅스/윈도우 빌드 |
| `install.sh` | 리눅스 설치 (root) |
| `conf.example/user.conf.example`, `awx_copy.conf.example` | 설정 예시 (비밀번호 비움) |
| `bin/awx_copy` | 리눅스 amd64 정적 실행파일 (go 1.20.14) |
| `dist/awx_fetch.exe` | 윈도우 amd64 실행파일 |
| `docs/PLAN_v2.md`, `docs/limits.md` | 계획서, 한도 측정 |
| `test/` | E2E 스크립트, PyYAML 검증 |
| `internal/fakeawx`, `cmd/fakeawx` | 테스트용 가짜 AWX |

### 정리한 잔여물

- bash 래퍼 `awx_copy` 를 삭제했다.
- VM 의 테스트 잔여물(`/root/AWX_copy/bin/awx_copy_new`, `/tmp/awx_copy_v2` 등)을 지웠다.
