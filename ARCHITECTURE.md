# ARCHITECTURE.md

| 폴더/파일 | 역할 |
|---|---|
| `main.go` | gossh v2 전체: `-w` 파싱·범위 확장, 워커풀, DNS 선행 조회, SSH(키 우선), stdout/stderr 분리, 진행률, `-b` 그룹, 위험 명령 가드, autofs 가드, 재검증, 결과 파일 |
| `term_linux.go` | 터미널 크기 조회·키 입력 모드(`-m`) — Linux 전용 |
| `term_other.go` | Linux 외 OS용 대체 구현(`-m` 미지원, 빌드만 가능) |
| `setup.sh` | 오프라인 빌드 (`vendor/`) |
| `update.sh` | 대상 서버 배포 스크립트 |
| `gossh_os6` | RHEL 6 / CentOS 6용 사전 정적 빌드 바이너리 |
| `old/` | 구버전(v1) 소스 아카이브 |
| `PLAN.md` / `CHANGELOG.md` | 개선 계획 / 버전별 변경 이력 |

수정 요청별로 볼 곳 (모두 `main.go`):

- **"옵션 추가"** → flag 정의 + README 4장 옵션 표
- **"접속불가 판정·재검증 조정"** → DNS 선행 조회 / 재검증 부분
- **"결과 파일 추가"** → 결과 파일 기록·시작 시 정리 부분 (README 5장 표도 갱신)
- gossh를 부르는 도구(`esxi-log-check`, `OS 환경설정 체크`, `ip_change`, `ldap_setting`, `조사`)가 출력 형식(`호스트: 줄`)에 의존하므로, 출력 형식을 바꾸면 그 도구들도 함께 확인하세요.

## 작업 흐름도

단계별 설명은 [WORKFLOW.md](WORKFLOW.md)를 참고하세요.

```mermaid
flowchart TD
    A["gossh -w hosts/범위식 [옵션] '명령'"] --> B["-w 파싱 · 호스트명 범위 확장<br/>(pdsh / clush NodeSet 문법)"]
    B --> C["이전 결과 파일 중 이번 실행 이름과 같은 것만 삭제"]
    C --> D{"위험 명령? (재부팅 등)"}
    D -- 예 --> D1["대상 출력 후 y/N 최종 확인"]
    D1 -- N --> Z["중단"]
    D1 -- y --> E
    D -- 아니오 --> E{"/user/ 경로 사용? (autofs)"}
    E -- 예 --> E1["동시성 제한 (안전가드)"] --> F
    E -- 아니오 --> F["워커풀 (-c 동시성)"]
    F --> G["DNS 조회 선행 (순수 Go 리졸버, 최대 2회 짧은 재시도)"]
    G --> H["SSH 접속 (키 우선 인증, -t 제한시간)"]
    H --> I["명령 실행 → stdout / stderr 분리 수집<br/>이스케이프 코드 제거"]
    I --> J["진행률 실시간 표시 (-m: 오래 걸리는 호스트 보기)"]
    J --> K{"1차 접속불가 호스트?"}
    K -- 예 --> L["낮은 동시성(≤50)으로 재검증<br/>로그인 제한 8초, 명령까지 재실행"]
    K -- 아니오 --> M
    L --> M["출력<br/>호스트명: 출력줄 (-b 면 같은 결과끼리 그룹)"]
    M --> N["요약 + 특이 호스트 결과 파일<br/>_res_off · _res_refsed · _os_install · _nosvrauto · _res_cancel"]
    N --> O["결과 파일을 -w 로 넘겨 재실행 가능"]
    F -.->|Ctrl+C 1·2·3회| P["pdsh 방식 중단 처리"]
```
