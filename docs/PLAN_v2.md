# AWX_copy v2 수정 계획서

상태: 인터뷰 완료, 구현 전. 대상 브랜치: AWX_copy.

## 1. 변경 요약
| 항목 | 현재(v1) | v2 |
|---|---|---|
| 실행 방식 | bash 래퍼 + Go 바이너리 | **Go 바이너리 단독**. bash 래퍼 삭제. |
| 설치 위치 | /root/AWX_copy/bin | **/usr/local/bin/awx_copy**. 어느 경로에서든 실행. |
| 설정파일 | ~/.awx_copy/{이름}.conf | **/etc/awx_copy/{user}.conf**. |
| 전송 형식 | gzip + base64 | **평문 텍스트**. YAML 블록(`|`)에 그대로 넣는다. |
| 기록 | 없음 | **TUI에 업로드 기록 표시**. |
| 윈도우 | CLI 다운로더 | **host-cross-checker 방식의 창 앱**. |

## 2. 레드햇 (awx_copy)
### 2.1 실행 형식
```
awx_copy -u {user} [filename] [-csv]
awx_copy                      # -u 없음: 선택 TUI
```
- 두 번째 인자 `-csv`가 없으면 type은 txt이다.
- `filename`이 없으면 기록 TUI만 연다.
- `filename`이 있으면 진행 TUI(`[k/N]`)로 업로드하고, 끝나면 기록 TUI로 넘어간다.

### 2.2 설정파일
- 경로: `/etc/awx_copy/{user}.conf`. 소유자는 해당 사용자. 권한 600. 관리자가 배포한다.
- 키: url, username, password, inventory_id, chunk_size, timeout.
- 사용자마다 inventory_id가 다르다. 같은 사용자 conf를 쓰는 업로더와 다운로더가 서로 만난다.
- `-u` 없이 실행하면 읽을 수 있는 conf 목록을 TUI로 보여 주고 고르게 한다.
- 비밀번호는 화면과 로그에 출력하지 않는다.

### 2.3 기록
- 저장 위치: `/tmp/.awx_copy/`. 사용자별 파일 `{user}.history.jsonl`.
- `-u {user}` 실행과 선택 TUI 실행 모두 기록 TUI를 보여 준다.
- 한 줄 기록: 시각, user, 파일명, type, 크기, 조각 수, 소요 시간, 결과(성공/실패/중단).
- `-u root`: root 비밀번호를 입력받는다. 일치하면 모든 사용자의 기록을 본다.
- root 비밀번호는 `/etc/awx_copy/root.conf`에 SHA-256 해시로 저장한다. 평문과 바이너리 내장은 하지 않는다. 설치 시 기본값은 사용자가 정한 값의 해시이다. root가 바꿀 수 있다.
- 주의: /tmp는 재부팅 때 지워질 수 있다. 기록 파일 권한은 600, 디렉터리 권한은 1733이 아닌 사용자별 파일 권한으로 보호한다. root 조회는 root 권한 또는 파일 읽기 권한이 필요하다.

## 3. 텍스트 전용 전송
- 사용자 지시: 거부 방식(확장자 목록, 바이너리 검사)은 넣지 않는다.
- 파일 내용을 UTF-8 문자열 그대로 YAML 리터럴 블록(`|`)에 넣는다. 조각마다 블록 하나.
- 줄바꿈, 탭, 앞쪽 공백을 보존한다. 첫 줄이 공백으로 시작하면 들여쓰기 지시자를 쓴다.
- 압축과 base64는 쓰지 않는다. 텍스트만 전송한다. 바이트를 16진수 등으로 따로 보내는 경로(예: `chunk_raw`)는 두지 않는다. 바이너리 반출 경로가 되기 때문이다.
- v2 수신기는 정의되지 않은 키(`chunk_raw`, `chunk_data` 포함)를 만나면 "지원하지 않는 키"로 거부한다.
- 블록에 넣을 수 없는 바이트(제어문자, DEL, 잘못된 UTF-8, NEL, U+2028/2029, BOM)는 U+FFFD로 바꿔 보낸다. 원문으로 복원되지 않는다. 조각 SHA-256이 달라 수신 쪽이 실패 처리하고 파일을 저장하지 않는다. 업로더는 경고만 출력한다(거부 기능 없음).
- 줄바꿈(`eol`): `lf` | `crlf` | `cr`은 블록 안에서 LF로 쓰고 받을 때 되돌린다(바이트 단위 복원). `mixed`(LF와 CR이 섞임)는 모든 CR을 LF로 정규화해 보낸다. 복원되지 않으므로 해시 불일치로 실패한다. 업로더는 경고를 출력한다.
- 조각 크기는 문자 수 기준으로 1 MiB 이하. 본문 추정 3 MiB 상한도 함께 적용한다. AWX 한도 4.94 MiB 안이다. 멀티바이트 문자와 CRLF 중간에서 자르지 않는다.
- 조각마다 원본 바이트의 SHA-256을 메타데이터에 넣고, 합친 뒤 전체 해시를 확인한다.
- 알려진 제약: AWX는 variables 끝의 공백/줄바꿈을 제거한다. `chunk_text` 블록 뒤에 항상 다른 키 줄을 두어 조각 끝 공백이 잘리지 않게 한다. 파일 끝 줄바꿈 유무는 `final_newline`으로도 기록한다. 블록은 들여쓰기 지시자 2와 chomp(`|2-`/`|2+`)를 써서 PyYAML 값도 원문과 같다.
- 기존 base64+gzip 구현은 커밋 11a4960에 태그 `base64-final`을 붙여 보존한다. 소스를 지우지 않고 별도 브랜치 `AWX_copy-base64`에도 둔다.

## 4. 윈도우 (awx_fetch.exe → 창 앱)
참고 구현: `C:\Users\qazx2\.gemini\antigravity\scratch\host-cross-checker` (README, WORK_LOG, agent/, internal/launcher, internal/webui).

### 4.1 구조
- Go 단일 exe. `go:embed`로 HTML/JS를 내장한다.
- Edge 또는 Chrome을 `--app=http://127.0.0.1:{포트}/` 창으로 실행한다. 주소창과 탭이 없다.
- 빌드 플래그 `-ldflags "-H windowsgui"`로 cmd 창이 뜨지 않게 한다.
- 창을 닫으면 `/api/bye` 비콘과 `/api/ping` 감시로 프로세스가 1~2초 안에 종료된다.
- 같은 폰트(Pretendard, 맑은 고딕, Consolas)와 같은 고대비 엔터프라이즈 스타일을 쓴다. 이모티콘은 쓰지 않는다.

### 4.2 host-cross-checker 제작 중 발생한 문제와 대책
| 문제 | 대책 |
|---|---|
| cmd 창 표시 | `-H windowsgui` 빌드. 자식 프로세스에도 `HideWindow`/`CREATE_NO_WINDOW`. |
| 창 닫은 뒤 프로세스 잔존 | `/api/bye` 비콘 + `/api/ping` 타임아웃 종료. 종료 후 100 ms 안에 회수. |
| HTTP 응답 Body 미닫힘(소켓 누수) | 모든 요청에 `defer resp.Body.Close()`. 요청 헬퍼 한 곳에 모은다. |
| WebSocket/요청 무기한 블록 | 쓰기와 요청에 타임아웃(3초/설정값)을 둔다. |
| 이벤트 리스너 중복 등록 | 초기화 가드. 드래그 리스너는 mousedown에서만 등록. |
| 파일 목록 폴링 부하 | 최신 100건만 감시. 맵 1,000건 초과 시 정리. |
| AWX 401 | 인증 방식(Basic/Session)을 기억하고 실패 시 재시도. 비밀번호는 DPAPI로 암호화해 저장. |
| 폰트 깨짐 | 폰트 스택 지정. UTF-8 BOM 없는 HTML에 `<meta charset>` 명시. |
| 포트 충돌 | 기본 포트 사용 불가 시 빈 포트를 찾는다. 이미 실행 중이면 기존 창을 앞으로 가져온다. |
| 브라우저 프로필 충돌 | `--user-data-dir=%TEMP%\awx-copy-profile` 독립 프로필 사용. |
| 한글 로그 깨짐 | 로그 파일은 UTF-8로 쓴다. 콘솔 출력에 의존하지 않는다. |

### 4.3 화면과 동작
- 사용자 선택: 윈도우 conf(`conf\{user}.conf`, 비밀번호는 DPAPI 암호화) 목록에서 고른다. conf가 없으면 입력 화면에서 만든다.
- 수신: `[지금 가져오기]` 버튼 + `[자동 감시]` 토글(2초 간격, 새 업로드 시작을 감지하면 자동 수신).
- 관리 공간:
  - 저장 폴더: exe 옆 `received\`. 설정으로 변경한다.
  - 기록 표: 시각, 원본 파일명, 크기, 조각 수, SHA-256 일치 여부(정상/실패), 저장 경로.
  - 기록 파일: `received\history.jsonl`에 영구 저장.
  - 버튼: 열기, 폴더 열기, 경로 복사, 삭제.
- 수신이 끝나면 variables를 시작 전 값으로 복원한다. 템플릿과 인벤토리 소스는 건드리지 않는다.

## 5. 작업 단계와 모델
| # | 작업 | 모델 |
|---|---|---|
| 1 | 태그 `base64-final`, 브랜치 `AWX_copy-base64` 생성 | haiku |
| 2 | 평문 YAML 프로토콜 구현(조각, 해시, eol/final_newline), 단위 테스트 | opus |
| 3 | 리눅스: /etc conf, `-u`, 선택 TUI, 기록 TUI, root 인증, 래퍼 삭제, install.sh | sonnet |
| 4 | 윈도우: 창 앱(launcher, embed UI, 수명 관리, 관리 공간) | opus |
| 5 | 통합 테스트: 실제 AWX, 텍스트 11종, 한도, 원복, 안전성 diff | sonnet |
| 6 | 문서와 설치: README, limits.md, /usr/local/bin 설치, 빌드(go 1.20) | haiku |

## 6. 완성 조건
- `awx_copy -u {user} file [-csv]`가 어느 경로에서든 동작한다. bash 래퍼가 없다.
- 평문 텍스트가 줄바꿈, 탭, 공백, 한글까지 바이트 단위로 같게 복원된다(해시 일치).
- 기록 TUI가 두 실행 방식 모두에서 보인다. `-u root`와 비밀번호로 전체 기록을 본다.
- 윈도우 exe 실행 시 cmd 창이 뜨지 않고, 창을 닫으면 프로세스가 종료된다.
- 수신 기록에 정상/실패와 해시 일치 여부가 남는다.
- 작업 템플릿과 인벤토리 소스 변경 0건.
