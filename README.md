# AWX_copy

AWX 인벤토리 `variables` 를 중계로 써서 레드햇(ETX) 터미널의 파일을 윈도우로 가져온다.

- 업로더(리눅스): `awx_copy` (bash 래퍼) → `bin/awx_copy` (Go)
- 다운로더(윈도우): `dist/awx_fetch.exe` (같은 Go 코드)
- 인벤토리 `variables` 만 PATCH 한다. 작업 템플릿, 인벤토리 소스는 호출·변경하지 않는다.
- 표준 라이브러리만 쓴다. go 1.20 이상에서 빌드된다.

## 동작 방식

1. 업로더가 파일을 읽는다. gzip 이 이득이면 압축한다. base64 로 바꾸고 `chunk_size` 문자씩 나눈다.
2. 시작 전 `variables` 원문을 로컬 `~/.awx_copy/backup/` 와 블록의 `backup_b64` 에 둔다.
3. 조각 k 를 `variables` 에 PATCH 한다. 다운로더가 받아 `ack: "k"` 를 PATCH 하면 다음 조각을 보낸다.
4. 다운로더는 조각을 합치고 SHA-256 을 검증해 `downloads\<원래 파일명>` 에 저장한다. 같은 이름이 있으면 `_1`, `_2` 를 붙인다.
5. 다운로더가 `variables` 를 시작 전 원문으로 복원한다.
6. SIGINT(Ctrl+C), 오류, 시간 초과 때도 원복한다. 강제 종료(kill -9) 뒤에는 `--restore` 로 원복한다.

양식 (YAML 로 읽힌다. 값은 모두 큰따옴표 문자열):

```yaml
awx_copy:
  version: "1"
  session: "20261117T025748-4ceb84cc9f1fadad"
  state: "sending"          # sending | ack
  file_name: "data.csv"
  type: "csv"               # csv | txt (메타데이터. 바이트는 그대로 보존)
  encoding: "gzip+base64"   # gzip+base64 | base64
  total_bytes: "93"
  sha256: "..."
  chunk_total: "1"
  chunk_index: "1"
  chunk_sha256: "..."       # chunk_data 문자열의 SHA-256
  chunk_data: "H4sI..."     # 한 줄 base64
  ack: "0"
  backup_b64: ""            # 시작 전 variables 원문의 base64
  updated_at: "2026-11-17T02:57:48Z"
```

`variables` 에 이미 `awx_copy:` 블록이 있으면 다른 세션으로 보고 거부한다.
폴링은 200ms 에서 시작해 2배씩 늘려 최대 2초다. HTTP keep-alive 를 쓴다. 병렬 PATCH 는 쓰지 않는다.

## 설치

### 리눅스 (업로더)

```bash
# 소스 폴더에서
./build.sh            # bin/awx_copy 생성 (RHEL6 은 go 1.20 으로 빌드)
cp awx_copy.conf.example ~/.awx_copy.conf
vi ~/.awx_copy.conf   # password 입력
chmod 600 ~/.awx_copy.conf   # 600 이 아니면 실행을 거부한다
```

빌드 없이 쓰려면 `awx_copy` 와 `bin/awx_copy` 를 같은 구조로 복사한다.
`bin/awx_copy` 는 go 1.20.14 정적 빌드라 RHEL6(커널 2.6.32)에서도 돈다.

### 윈도우 (다운로더)

```bat
build.bat             :: dist\awx_fetch.exe 생성
copy awx_copy.conf.example dist\awx_copy.conf
notepad dist\awx_copy.conf
```

설정파일 위치는 위 "사용자별 설정파일" 을 본다. (호환: exe 옆 `awx_copy.conf`)

## 설정파일

| 키 | 뜻 | 기본값 |
|---|---|---|
| url | AWX 주소 | (필수) |
| username | AWX 사용자 | (필수) |
| password | 비밀번호 (직접 적음, 토큰 아님) | (필수) |
| inventory_id | 중계 인벤토리 ID | (필수) |
| chunk_size | 조각 하나의 base64 문자 수 (4의 배수로 내림) | 1048576 |
| timeout | 상대측 응답 대기와 HTTP 요청의 최대 시간(초) | 300 |

비밀번호는 로그와 오류 메시지에 나오지 않는다. 실제 설정파일은 `.gitignore` 에 있다.

## 사용자별 설정파일

사용자마다 `<이름>.conf` 를 따로 둔다. 키는 위 표와 같다. `username` 값이 그 파일의 user 레코드다.
예시: `conf.example/user.conf.example`.

```
리눅스: ~/.awx_copy/
          ├─ kim.conf        (chmod 600)
          └─ test.kim.conf
윈도우: awx_fetch.exe 옆 conf\
          ├─ kim.conf
          └─ test.kim.conf
```

설정을 고르는 순서 (업로더와 다운로더 공통):

1. `--config <경로>` 가 있으면 그 파일만 쓴다.
2. `--user <이름>` 이 있으면 `<설정폴더>/<이름>.conf` 를 쓴다. 파일이 없으면 username 이 같은 파일을 찾는다.
3. 아무 옵션이 없으면 OS 로그인 사용자명으로 `<사용자명>.conf` 를 찾는다.
   파일이 있고 username 이 비어 있지 않으면 **묻지 않고 바로 실행**한다.
   파일명이 달라도 username 이 OS 사용자명과 같은 파일이 있으면 그 파일을 쓴다.
4. 해당 파일이 없으면 선택 메뉴를 보여준다. 호환용으로 `~/.awx_copy.conf`(리눅스), exe 옆 `awx_copy.conf`(윈도우)도 목록에 들어간다.
5. 터미널이 아니면(표준 입력이 TTY 가 아님) 메뉴를 띄우지 않는다. 후보 목록과 사용법을 알리고 종료 코드 2 로 끝난다.

```
사용할 설정을 고르세요.
  1) kim.conf                 kim              http://192.168.0.58:30080
  2) test.kim.conf            test.kim         http://192.168.0.58:30080
  3) 새 설정 만들기
번호:
```

비밀번호는 메뉴에 나오지 않는다. "새 설정 만들기" 는 url, username, password(화면에 보이지 않음), inventory_id 를 묻고
`<username>.conf` 로 저장한다(리눅스 권한 600). 같은 이름이 있으면 덮어쓰기 전에 묻는다. 저장한 뒤 바로 실행한다.

```bash
./awx_copy /data/a.csv csv                  # OS 사용자 파일이 있으면 즉시 실행
./awx_copy --user test.kim /data/a.csv csv  # ~/.awx_copy/test.kim.conf
./awx_copy --list-users                     # 목록만 출력하고 종료
```

업로더와 다운로더는 **같은 inventory_id** 를 써야 서로 만난다. (같은 사용자 conf 를 양쪽에서 써도 된다.)
리눅스 conf 의 권한이 600 이 아니면 거부한다.

## 사용

```bash
# 리눅스: 보내기 (type 기본 txt)
./awx_copy /data/report.csv csv
./awx_copy /var/log/app.log
./awx_copy /data/x.txt txt --config /root/other.conf
```

```
[1/3] 전송 중 (1048576자)
[2/3] 전송 중 (1048576자)
[3/3] 전송 중 (1048576자)
완료: 2359296 bytes, 조각 3개, 1.90초 ...
```

윈도우: `awx_fetch.exe` 를 더블클릭하거나 명령창에서 실행한다. 순서는 상관없다.
먼저 실행한 쪽이 상대를 `timeout` 초까지 기다린다.

```bat
awx_fetch.exe
awx_fetch.exe --config D:\conf\awx_copy.conf --out D:\받은파일
```

기타 명령 (리눅스, 윈도우 공통):

| 명령 | 하는 일 |
|---|---|
| `--restore` | 남은 블록을 지우고 시작 전 원문으로 복원. 블록의 backup_b64 우선, 깨졌으면 최신 로컬 백업 |
| `--status` | variables 길이, SHA-256, 블록 여부 (GET 만) |
| `--snapshot` | 작업 템플릿/인벤토리 소스의 id, modified (GET 만) |
| `--measure-limits` | variables 최대 크기 측정 후 원복 |
| `--user <이름>` / `--list-users` | 사용자 설정 지정 / 목록 출력 |
| `--no-pause` | 윈도우에서 끝난 뒤 Enter 대기를 하지 않음 |

## 복구

| 상황 | 결과 / 할 일 |
|---|---|
| 업로더 Ctrl+C | 업로더가 원복. 다운로더는 "업로더가 세션을 중단" 오류로 끝남 |
| 다운로더 Ctrl+C, SHA 불일치 | 다운로더가 원복. 업로더는 "다운로더가 세션을 중단" 오류로 끝남 |
| ack 시간 초과 | 업로더가 원복 |
| kill -9, 전원 차단 | 블록이 남음 → `./awx_copy --restore` |
| "다른 세션이 사용 중" | 진짜 다른 사용자면 기다린다. 남은 블록이면 `--restore` |

원복은 현재 블록이 자기 세션일 때만 한다. 다른 세션 블록은 건드리지 않는다.

## 한도

자세한 표는 `docs/limits.md`. 실제 AWX(v24.6.1)에서 인증 후 PATCH 로 측정했다.

- 최대 성공: **4.94 MiB (5,177,344자)**. 5 MiB 부터 nginx 413 (`client_max_body_size 5m`). timeout 없음.
- 1 MiB 조각 PATCH 는 0.42초, 4.94 MiB 는 1.64초.
- AWX 는 저장할 때 variables 끝의 공백/줄바꿈을 제거한다 (내용과 탭은 그대로).
- `chunk_size` 기본값 1 MiB 는 최대 성공 크기의 약 0.2배다.
- 실측 전송 속도: 1 MiB 파일 약 4초, 10 MiB 파일 약 19초 (업로더 기준, 조각 14개). 약 0.5 MiB/s.
- 실제 AWX E2E 11개 파일(0B ~ 10MiB, 조각 경계, 탭/CRLF/한글/CSV) 모두 SHA-256 과 바이트 일치.

## 테스트

```bash
go test ./...                                   # 단위·통합 테스트 (가짜 AWX)
test/make_files.sh /root/awx_e2e/files 1048576  # E2E 파일 생성
test/e2e_send.sh <conf> /root/awx_e2e/files     # VM 업로더
```

```powershell
test\e2e_fetch.ps1 -Exe dist\awx_fetch.exe -Config <conf> -Out <폴더> -Count 11   # 윈도우 다운로더
test\compare.ps1 -Orig <원본폴더> -Got <받은폴더>                                 # SHA-256 + 바이트 비교
```

`cmd/fakeawx` 는 테스트용 가짜 AWX 서버다 (인벤토리 GET/PATCH 만 흉내).

## 파일

| 파일 | 내용 |
|---|---|
| `awx_copy` | bash 래퍼 (업로더 실행용) |
| `main.go` 외 `*.go` | Go 본체 (업로더/다운로더/복원/측정) |
| `build.sh`, `build.bat` | 리눅스/윈도우 빌드 |
| `awx_copy.conf.example`, `conf.example/user.conf.example` | 설정 예시 (비밀번호 비움) |
| `bin/awx_copy` | 리눅스 amd64 실행파일 (go 1.20.14) |
| `dist/awx_fetch.exe` | 윈도우 amd64 실행파일 |
| `docs/limits.md` | 한도 측정 결과 |
| `test/` | E2E 스크립트 |
| `internal/fakeawx`, `cmd/fakeawx` | 테스트용 가짜 AWX |
