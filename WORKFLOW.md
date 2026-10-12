# WORKFLOW

그림 모음: 전체 [`workflow.svg`](workflow.svg), 명령·키별 31개 [`docs/svg/`](docs/svg/README.md). 사용법은 [README.md](README.md), 코드 구조는 [ARCHITECTURE.md](ARCHITECTURE.md).

## auto_setup 실행 흐름 (Mermaid, v0.8.0 반영)

```mermaid
flowchart TD
    A6["[os6_mgmt] 01 실행<br/>auto_setup_host 채움"] -->|"gossh 원샷(base64)"| Q
    A8["[os8_mgmt] 01 실행"] -->|"로컬 기록"| Q
    Q["os8_mgmt queue/<epoch>_<user>_<pid>.job<br/>user=, time=, 호스트줄, 그룹 줄(yml=…/all=…)"]
    Q --> C["[cron 매분] auto_setup ensure<br/>(os8_mgmt 에서만)"]
    C --> D{"데몬 실행중?"}
    D -->|Yes| D1["flock 실패, 즉시 종료"]
    D -->|No| D2["setsid daemon 백그라운드 기동"]
    D1 --> E
    D2 --> E["[데몬] queue 수거 5초 주기"]

    E --> F["호스트명 → IP (DNS, 동시 32)<br/>경로 판별: 로컬 ping 실패 → route=os6<br/>(설치 중 os8 무응답·os6 응답 → os6 전환)"]
    F --> I["jobs/<jobid>.json 저장"]
    I --> BK["전달 시점 LDAP 백업<br/>ping O 인 호스트만, ldapbak/<jobid>/<host>/"]

    BK --> J["[루프 ①] ping 감시 10초<br/>무응답 2회 → seen_down (설치 시작)"]
    J --> L["[루프 ②] 준비확인 30초 (후보만)<br/>gossh -pm cat /proc/uptime<br/>호출당 60초 제한, 원격은 bash -c"]
    L --> N{"READY?<br/>응답 · anaconda 아님 ·<br/>uptime < 경과시간"}
    N -->|No| L
    N -->|Yes| LD["LDAP 비교 (stage=ldap)<br/>새 OS ldap.conf binddn uid vs 백업"]
    LD --> LDQ{"같은가?"}
    LDQ -->|"same / 백업 없음"| O
    LDQ -->|diff| LR["공유경로(ldap_share_dir) 경유로<br/>백업 파일 세트 복원 + 서비스 재시작<br/>(변수 비면 수동 복원 필요)"]
    LR --> O["[루프 ③] 스케줄 7분/3분<br/>전부 READY 면 즉시"]

    O --> R["[run] os_check -auto user targets.txt<br/>동시 1개, runs/<code>/"]
    R --> S2{"2차 대상 있음?<br/>route=local 의 접속불가·FAIL<br/>(os6_mgmt·os6_os_check_sh 설정 시)"}
    S2 -->|Yes| S3["os6_mgmt 에서 os_check -auto 1회<br/>결과 tar 회수 → runs/<code>/second/"]
    S2 -->|No| V
    S3 --> V["code 파일 생성<br/>① 결과 리포트 ② FAIL/NO FAIL ③ LDAP·Splunk·커널<br/>④ 원본 경로 ⑤ ### 2차 체크 (os6_mgmt)"]
    V --> Y["wall 발송 (기본 꺼짐): 완료 호스트 + code"]
    Y --> AA["processed 표시 (최종 = 1차 OK 또는 2차 OK)"]
    AA --> AB{"모든 호스트 완료?"}
    AB -->|Yes| AB1["작업 종료 jobs → done"]
    AB -->|No| J

    DN["os_check 정상 종료 시 완료기록<br/>done/<host> : epoch user sha256 source"] -.->|"전달·부팅 이후만 인정"| AA
    RQ["TUI h→c·t·g / auto_setup request / done<br/>requests/*.req"] -.->|"5초 수거, 수동 run"| R
```

## 상태 리포트 흐름 (TUI, 양방향 동일)

```mermaid
flowchart LR
    T8["[os8_mgmt] auto_setup"] -->|"BuildSnapshot (로컬 파일)"| S["Snapshot"]
    T6["[os6_mgmt] auto_setup (os8_mgmt 변수 채운 빌드)"] -->|"gossh 원샷 snapshot (10초 폴링)"| S
    S --> V1["화면 1: 작업·그룹 yml 집합<br/>단계별 대수, 막대, 최장 경과"]
    V1 -->|Enter| V2["화면 2: 호스트표 (단계·경과·비고)"]
    V2 -->|"h → c · t, y"| RQ["requests/*.req 기록<br/>(원격이면 gossh 로 request)"]
    RQ --> D["데몬 수거 → run 큐에서 수동 OS 체크"]
```

## 키 · 메뉴 구조 (v0.8.0)

기능이 늘어 안내 줄이 길어졌으므로 `m` 과 같은 **인라인 메뉴**(화면을 바꾸지 않고 안내 줄 아래에 한 줄로 열림)를 분야별로 둡니다. 메뉴 글자는 예전 단축키와 같아서 `c` `t` `g` `v` `d` `x` 는 **숨은 단축키로 그대로 동작**합니다. 그림 목록: [docs/svg/README.md](docs/svg/README.md)

```mermaid
flowchart TD
    TUI["상태 TUI<br/>화면 1 (그룹 목록) · 화면 2 (호스트표)"]
    TUI --> NAV["이동·보기: ↑↓ ←→ Enter a f r ? PgUp/PgDn q"]
    TUI -->|"h (화면 2 에서만)"| H["호스트 작업 메뉴"]
    TUI -->|"w"| W["AWX 메뉴"]
    TUI -->|"m (켜졌을 때)"| M["변경 작업 메뉴"]
    TUI -->|"s"| S["gossh 작업 화면 (메뉴 없음)"]
    TUI -->|"x"| X["체크스크립트 단독 실행"]
    H --> Hc["c 체크+수정"]
    H --> Ht["t 체크만"]
    H --> Hg["g 재확인"]
    H --> Hd["d 수동 완료"]
    H --> Hv["v 결과 보기"]
    W --> W1["1 AWX 실행 (ww 또는 w Enter)"]
    W --> W2["2 신규 설치 양식"]
    Hc --> RQ["요청 .req → 데몬 수락 → os_check -auto"]
    Ht --> RQ2["요청 .req mode=check → os_check -auto-check"]
    Hg --> RC["요청 recheck → os8 → os6_mgmt → 접속불가"]
    Hd --> MD["markDone (manual) → 데몬이 5초 안에 반영"]
    W1 --> AX["01.AWX_nodeinfo_V2.sh 를 터미널에서 실행"]
    W2 --> NH["신규 설치 양식 5단계"]
    NH -.->|"저장 후 y"| AX
    M --> MV["IP · LDAP 변경 · 원복 (엔진 호출)"]
    S --> WK["gossh 실행 · 기록 (work/user)"]
```

- `w` 는 메뉴이며 `ww`·`w` `Enter`·`w` `1` 이 예전의 AWX 실행입니다. 화면 1 에서는 `h` 가 없습니다 (호스트 표가 없음). 전체 보기(`a`)에서는 `h` 메뉴에 `g`·`d` 만 나옵니다.
- 안내 줄 둘째 줄: ` h 호스트 작업  w AWX·신규설치  m 변경  s gossh 작업  x 단독 체크` (`m 변경` 은 켜졌을 때만).
- **세션 user**: `-user` 로 실행했으면 그 user. 아니면 `s`·신규 설치 양식을 처음 열 때 한 번 고르고 TUI 를 닫을 때까지 기억합니다 (`x`·`w`→1·`m` 은 기존처럼 매번 묻습니다).

## 실행 흐름 상세 설명

### 1단계: 전달 (01.AWX_nodeinfo_V2.sh)

```bash
# [14-1] 02 성공 직후 (auto_setup_host 가 비어 있으면 로컬, 채워져 있으면 gossh 로 os8_mgmt 에 전송)
{ echo "user=${user}"; echo "time=$(date +%s)"; cat "$hostfile"; as_group_lines; } > job
```

- 항상 전달(프롬프트 없음), 02 실패 시 전달 없음, 실패해도 경고 1줄만
- `as_group_lines`: 그룹 yml 이 2개 이상일 때 `yml=<파일명> infra= os= boot= splunk= hosts=<h1,h2,…>` 그룹별 한 줄 + `all=<전체 yml>`
- 원격 전송은 base64 를 두 글자마다 `.` 로 나눠 gossh 위험어를 우연히 만드는 것을 피함

### 2단계: 감시 시작 (데몬 큐)

```bash
# 01 이 넘긴 job 파일을 로드
jobs/<jobid>.json 생성
- id / user / submitted
- hosts: host → {ip, route, seen_down, ready_at, processed, stage, ldap, second, done_src, ...}
- groups: yml → {infra, os, boot, splunk, hosts}, all_yml
- runs: [{code, hosts, at}, ...]
```

### 3단계: 경로 판별 · 전달 시점 LDAP 백업

```bash
# 로컬 ICMP ping 응답 없음 → route=os6 (os6_mgmt 비어있으면 모두 local)
# ssh os6_mgmt "<os6_autosetup>/auto_setup probe -i 10s" 장기 세션 유지
# ping O 인 호스트: gossh 로 LDAP 설정 파일 수집 → ldapbak/<jobid>/<host>/ (0700/0600)
```

### 4단계: 감시 루프 (ping)

```bash
# 10초마다: local 은 raw ICMP, os6 는 probe 세션에서 변화 수신
# route=local 인데 로컬 무응답 → 다음 주기는 로컬+os6 probe(both), os6 가 응답하면 route=os6 로 전환 (0.4.0)
# 무응답 2회 연속 → seen_down=true (설치 시작 간주)
# stage: queued → deploying → installing → booting; seen_down 후 60분 넘기면 stuck(화면 표시만)
```

### 5단계: 준비확인 (30초 주기)

```bash
# 후보: (미처리 && ping up && seen_down) || (기동 후 1회)
# gossh -pm -script -w <list> "cat /proc/uptime"
# READY = 응답 있음 && anaconda 아님 && uptime < (지금 − submitted)
# route=local 무응답 호스트는 같은 주기에 os6_mgmt 경유 재확인 → 응답하면 route=os6 (os6_gossh 필요, 0.4.0)
# 호출 1회마다 전체 60초 제한(초과 시 프로세스 그룹 종료, 사유 "시간 초과(60초)"), os6 ssh 는 bash -c 로 감싸 csh/tcsh 로그인 셸에서도 동작 (0.8.0)
# gossh 출력은 parseGossh 하나로 해석 (FQDN ↔ 짧은 이름, 해석 실패 시 로그 "[!] gossh 출력 해석 실패" + 사유 "출력 형식 불일치")
```

### 6단계: LDAP 비교·복원 (READY 직후, os_check 전)

```bash
# 백업 보유 호스트: 새 OS 의 ldap.conf binddn 줄의 uid=<값> 의 값만 백업과 비교 (bindpw 는 읽지 않음)
# same → 생략 / diff → 공유경로(ldap_share_dir) 경유로 백업 파일 세트 복원(명령줄엔 경로만, 권한·소유, restorecon, sssd|nslcd·chronyd|ntpd 재시작, 끝나면 공유 임시 디렉터리 삭제) / ldap_share_dir 비면 "수동 복원 필요" 표시
# 실패해도 run 진행 (os_check 가 FAIL 로 보고). os_check·설정 스크립트는 변경 없음
```

### 7단계: 스케줄 (7분/3분)

```bash
# 1차: 전부 READY → 즉시 / 아니면 첫 READY + 7분
# 늦은: 첫 늦은 READY + 3분 모아서
# 각 run 시 targets.txt (1차=전체, 재실행=미처리만, 완료기록·수동 완료 호스트 제외)
```

### 8단계: os_check 실행

```bash
# cd /tmp/auto_setup/runs/<code>
# os6 호스트 포함 시: /tmp/auto_setup/bin/gossh 래퍼 PATH 앞에 삽입
# bash os_check_sh -auto <user> targets.txt > os_check.log 2>&1
# os_check 는 정상 종료 시 (빈 변수 auto_done_* 가 채워진 경우) 완료기록을 남김
```

### 9단계: 2차(이중) 체크

```bash
# 대상: route=local 중 접속불가 또는 postapply FAIL (route=os6 제외)
# os6_mgmt 에서 os_check -auto <user> <목록> 1회 → tar 회수 → runs/<code>/second/
# 최종 판정 = 1차 OK 또는 2차 OK. os6_os_check_sh="-" 면 끔
# 부작용: 설정(set)이 같은 호스트에 한 번 더 적용될 수 있음
```

### 10단계: code 파일 생성

```
codes/<code>.txt
├── 결과 리포트 (담당자 문구)
├── 설정체크 (FAIL/NO FAIL)
├── LDAP/Splunk/커널 요약
├── 원본 경로
└── ### 2차 체크 (os6_mgmt)  (2차 체크를 한 경우)
```

### 11단계: wall 발송 (v0.4.3 부터 기본 꺼짐 — 결과는 TUI·code·로그로 확인)

```bash
# wall "[auto_setup] OS 설치 + 설정체크 완료 (user=<user>)"
#      "host01 host02 host03 (3대)"
#      "code : 4821   →  auto_setup code 4821"
```

### 12단계: 감시 계속 또는 종료

```bash
# processed 된 호스트 제외
# 남은 호스트 있음 → 무기한 감시 (cancel 까지)
# 모두 처리 → 작업 종료 (jobs → done)
```

### 완료기록 (타 경로에서 먼저 끝난 호스트)

```bash
# os_check(어느 서버든) 정상 종료 → done/<host> : "epoch user sha256 source"
# 데몬: 전달 이후 · 마지막 부팅 이후 기록만 인정 (미래 +5분 초과 거부) → processed(done_src=external)
# 수동: auto_setup done <host...> (부팅 시각 검증 없음)
```

### 요청 (TUI h→c·t·g · request)

```bash
# requests/<epoch>_<kind>.req (manual-run | cancel) → 데몬 5초 주기 수거
# manual-run 은 완료 여부와 상관없이 수락(접속불가는 결과에 표시) → 데몬 run 큐에서 실행(동시 1개), code 동일
# TUI 에서는 h → c (설정체크 + 설정수정) / h → t (설정체크만, mode=check) / h → g (recheck)
```

## 변경 작업 흐름 (m 메뉴, v0.7.0)

화면 1·2 어디서나 `m` → 메뉴 번호. 엔진(`ip-change-engine`, `ldap-config-engine`)을 os8_mgmt 에서 호출하고, 응답 없는 호스트만 os6_mgmt 경유로 다시 시도합니다. 미리보기에서 `yes` 를 정확히 입력해야 실행됩니다.

```mermaid
flowchart TD
    M["[화면 1·2] m 키<br/>켜진 항목만 표시 (conf 또는 --admin)"] --> K{"메뉴 번호"}
    K -->|"1 · 2 · 3"| U["user 선택<br/>(x 와 같은 메뉴)"]
    K -->|"4 원복"| RB["원복 종류 선택<br/>IP / LDAP / 둘 다"]
    K -->|"5 auto.appl 원복"| U
    RB --> U
    U --> IF{"infra 필요?<br/>(2 · 3)"}
    IF -->|Yes| INF["infra 메뉴 선택<br/>(ldap_config.conf)"]
    IF -->|No| IN
    INF --> IN["대상 입력 → Ctrl+D<br/>원복·5 는 user 기록으로 자동 채움"]
    IN --> DR{"LDAP 포함?<br/>(2 · 3)"}
    DR -->|Yes| DRY["LDAP dry-run 자동 조회<br/>(-no-restart, 노드는 바뀌지 않음)"]
    DR -->|No| PV
    DRY --> PV["미리보기<br/>진행 중 job 경고 · 재시작 안 함 안내"]
    PV --> YES{"yes 를 정확히 입력?"}
    YES -->|No| CAN["취소 — 아무것도 바꾸지 않음"]
    YES -->|Yes| REC["기록 파일 먼저 생성 (실패하면 실행 안 함)<br/>changes/사용자명/시각_종류.json"]
    REC --> BR{"실행 종류"}
    BR -->|"1 · 3 · 4 (IP 포함)"| IPS["IP 단계 (ifcfg, 재시작 없음)<br/>os8 실행 → 응답 없는 호스트만 os6_mgmt 경유"]
    BR -->|"2 · 4 (LDAP 만)"| LDS["LDAP 단계 (-no-restart)<br/>os8 실행 → 응답 없는 호스트만 os6 경유"]
    BR -->|"5"| APS["auto.appl 단계<br/>노드 /root/auto_appl_restore.sh 만 실행"]
    IPS -->|"1 · 4 (IP 만)"| RES
    IPS -->|"3 · 4 (둘 다): IP 성공 호스트"| LDS
    IPS -->|"3: IP 실패 호스트"| SKP["건너뜀 (IP 실패)"]
    LDS --> RES
    APS --> RES
    SKP --> RES
    RES["결과 화면<br/>호스트별 IP / LDAP / auto.appl 칸"] --> LOG["auto_setup.log 요약 한 줄<br/>원복 성공 → 이전 기록 갱신"]
```

- 취소: 실행 중 `Ctrl+X` → `y` (프로세스 그룹 SIGTERM, 3초 뒤 SIGKILL). 중단되면 호스트별 결과를 그대로 기록하고 "일부 호스트만 변경됐을 수 있음" 을 표시합니다.
- 경유 래퍼: os6 경유가 필요한 호스트가 있을 때만 임시 래퍼(0700)를 만들어 엔진 `-gossh` 로 넘기고, 끝나면 지웁니다.
- 기록 위치와 형식은 README 「변경 작업」 참고. 변경 흐름의 화면 구성은 `change_render.go` 에 있습니다.
- `workflow.svg` 는 전체 흐름(v0.8.0 신규 기능 열 포함)이며, 변경 작업 흐름의 그림은 [docs/svg/tui_m.svg](docs/svg/tui_m.svg) 에도 있습니다.

## 신규 설치 양식 흐름 (w→2 / auto_setup newhost, v0.8.0)

nodeinfo 를 쓸 수 없는 신규 설치 서버용입니다. hostname ip mac 만으로 01 의 12필드 양식(`awx_dir/<user>.txt`)을 만들고 AWX 로 이어집니다 (nodeinfo 는 항상 N). os8_mgmt 에서만 열립니다. 그림: [tui_w_2.svg](docs/svg/tui_w_2.svg) · [cli_newhost.svg](docs/svg/cli_newhost.svg)

```mermaid
flowchart TD
    A["w → 2 또는 auto_setup newhost"] --> B{"os8_mgmt 이고 awx_dir 가 있는가?"}
    B -->|"아니오"| B1["안내만 표시"]
    B -->|"예"| U["user 정하기<br/>-user 또는 세션 user (처음 한 번 선택)"]
    U --> P["1/5 붙여넣기 hostname ip mac<br/>MAC 소문자, 형식 오류·중복은 고칠 때까지 못 넘어감"]
    P --> T["2/5 템플릿 지정<br/>범위·패턴·열 수정, 지정 없는 행은 이력 추천"]
    T --> D["3/5 DNS 점검 (백그라운드)"]
    D --> DQ{"불일치가 있는가?"}
    DQ -->|"아니오 또는 점검 불가"| V
    DQ -->|"예"| DA["a 모두 DNS 로 / n 그대로 / s 하나씩 / q 중단"]
    DA --> V["4/5 미리보기 (12필드 줄을 템플릿별로)"]
    V -->|"y"| SV["awx_dir/<user>.txt 저장<br/>기존은 newhost/<user>/backup/, history/ 에 사본"]
    SV --> AQ{"5/5 AWX 실행할까요?"}
    AQ -->|"n"| E["저장만 완료 (나중에 w → 1)"]
    AQ -->|"y"| AX["01 실행: nodeinfo N 강제, 대상 목록 자동 입력<br/>DNS 를 점검했으면 AWX_DNS_CHECKED=1"]
```

- 각 단계에서 `Esc` = 이전 단계, `Ctrl+X` → `y` = 취소(저장 안 함). 저장 전에는 어떤 파일도 바꾸지 않습니다.
- 프로파일(`awx_profile_N`)이 있으면 5/5 의 `y` 뒤에 프로파일을 고르고, 없으면 `AWX_AUTO` 없이 `AWX_AUTO_NODEINFO=n` 만 넘깁니다.

## DNS 점검 흐름 (auto_setup 양식 · awx_script 01 [4-1], v0.8.0)

같은 규칙을 Go(`dnscheck.go`)와 bash(01 `dns_check`)가 씁니다. 그림: awx_script 의 `workflow.svg` 에도 [4-1] 단계가 있습니다.

```mermaid
flowchart TD
    A["resolv.conf 의 nameserver 최대 3 읽기"] --> B{"DNS 서버가 있고 질의 도구를 쓸 수 있는가?<br/>(01: dig → nslookup)"}
    B -->|"아니오"| B1["DNS 조회 불가 / DNS 도구 없음 — 점검 생략<br/>기존대로 진행"]
    B -->|"예"| C["호스트마다 모든 서버에 A 레코드 직접 질의<br/>동시 32, 질의당 2초, 재시도 1회"]
    C --> D{"1대라도 A 를 받았는가?"}
    D -->|"아니오"| B1
    D -->|"예"| E["호스트 판정<br/>일치 · 불일치 · 판단 불가 · 미등록 · 조회 실패"]
    E --> F{"불일치가 있는가?"}
    F -->|"아니오"| G["일치 N대 요약 후 진행"]
    F -->|"예"| H["표 출력 + 중복 경고 (목록 · DNS · PTR)"]
    H --> I{"누가 묻는가?"}
    I -->|"auto_setup 양식 · 01 대화형"| J["a 모두 DNS 로 / n 그대로 / s 하나씩 / q 중단"]
    I -->|"01 auto 모드 (AWX_AUTO=1)"| K["/dev/tty 로 등록값 그대로 계속? y/N<br/>y 외 또는 터미널 없음은 중단"]
    I -->|"AWX_DNS_CHECKED=1"| L["표만 출력하고 묻지 않고 진행"]
    J --> M["교체는 IP(5번째 필드)만 바꿈<br/>01 은 LOG 에 [DNS] host 옛IP -> 새IP"]
```

## gossh 작업 흐름 (s / auto_setup s, v0.8.0)

호스트 목록 파일(`/tmp/<3자리>`)별로 gossh 실행 기록을 user 별로 남깁니다. 그림: [tui_s.svg](docs/svg/tui_s.svg) · [cli_s.svg](docs/svg/cli_s.svg)

```mermaid
flowchart TD
    S["s 또는 auto_setup s"] --> U["user 정하기 (-user 또는 세션 user)"]
    U --> L["작업 목록 work/<user>/index.jsonl<br/>열 때 정리: 출력 7일 · 200MB · /tmp 파일 7일"]
    L -->|"n"| PA["호스트 붙여넣기"]
    PA --> RU{"같은 목록이 있는가?"}
    RU -->|"예 y"| CMD["명령 한 줄 입력"]
    RU -->|"아니오 n"| NEW["/tmp/<소문자 3자리> 이름 정함"]
    NEW --> CMD
    L -->|"Enter"| ITEM["항목 메뉴: 새 명령 · 재실행 · 실패만 재실행 · 결과 보기 · 메모 · 호스트 목록"]
    ITEM --> CMD
    L -->|"/"| SR["호스트 검색 → 작업·명령 시간순"]
    CMD --> DG{"위험어(reboot·halt 등)?"}
    DG -->|"예"| DC["화면 확인 + gossh 위험 작업 확인 y/N"]
    DG -->|"아니오"| PL
    DC --> PL{"어디서 실행?"}
    PL -->|"os6_mgmt 에서 열림"| O6["이 서버 gossh 로 바로 실행<br/>기록은 os8_mgmt 로 중계"]
    PL -->|"os8_mgmt, 대수 ≤ work_os6_over"| O8["os8 gossh 실행<br/>접속불가 호스트만 os6_mgmt 경유로 자동 재실행"]
    PL -->|"os8_mgmt, 대수 초과"| OA["전량 os6_mgmt 경유 (ssh -t, bash -c)"]
    O6 --> RUN["TUI 를 떠나 터미널에서 script -q -f -c 로 실행<br/>출력·색·위험어 확인 그대로"]
    O8 --> RUN
    OA --> RUN
    RUN --> REC["기록: 요약(영구) + 전체 출력(압축, 7일)<br/>성공·실패·접속불가를 ERROR/SSH 접속 실패 줄로 판정"]
    REC --> L
    DM["데몬: 하루 한 번 workCleanAll"] -.->|"출력 7일 · user 당 200MB · /tmp 7일"| REC
```

## 흐름도 보조 자료

- **계획서 §14**: 2차 설계
- **workflow.svg**: 정적 SVG 흐름도 (v0.8.0 반영: 항목별 메뉴·신규 설치 양식·DNS 점검·gossh 작업 열 추가)
- **docs/svg/**: CLI 명령·TUI 키별 흐름도 31개 — [목록](docs/svg/README.md)
- **README.md**: 사용법·옵션·키 표 (키 행마다 해당 SVG 링크)
- **ARCHITECTURE.md**: 함수 및 단계 매핑
