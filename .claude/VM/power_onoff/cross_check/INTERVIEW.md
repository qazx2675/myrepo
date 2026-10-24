# cross_check 인터뷰 결정 기록 (자체 인터뷰 — 사용자 개입 없이 저장소 근거로 결정)

계획서 원문: [PLAN.md](PLAN.md). 아래 결정은 `vm-power-onoff` 브랜치의 코드(`main.go`, `guard.go`, `setup.sh`, `install.sh`, `config.go`)를 읽고 내렸다.
사용자가 뒤집고 싶은 항목은 이 표만 고치면 된다.

| # | 질문 | 결정 | 근거 |
|---|---|---|---|
| Q1 | 어디에 둘까? (계획서: `safety_guard` 브랜치 / 요청: vm-power-onoff 와 연동) | `power_onoff/cross_check/` 하위 폴더, 브랜치 `cross-check-daemon`(vm-power-onoff 에서 분기) | 연동 + 폴더만 받아 `setup.sh` 한 번으로 구성되려면 같은 트리여야 함 |
| Q2 | Go 모듈/의존성? | `cross_check/go.mod` 별도 모듈, **표준 라이브러리만** | vendor 불필요 → 폐쇄망 빌드 단순, OS6(Go 1.20) 호환 가능한 코드만 사용 |
| Q3 | TUI 방식? | 줄 입력 + ANSI 색상 (`vm_power_onoff` 의 `ui.go` 와 같은 스타일). 번호/명령 선택 | `auto_setup` 의 `change_tui.go` 도 터미널 직접 제어 없이 메뉴 방식, 의존성 추가 회피 |
| Q4 | 데몬↔클라이언트 통신? | Unix 도메인 소켓 `/var/run/cross_check/cross_check.sock`(0666), 요청마다 JSON 한 줄 | 작업자 프로세스가 소켓을 **연결 유지**하면 Ctrl+C·세션 끊김·kill -9 모두 커널이 EOF 로 알려줌 → `중단` 즉시 처리 |
| Q5 | 같은 사람이 두 이름으로 승인하는 것을 막나? | `user==checker` 는 즉시 취소. 추가로 Linux `SO_PEERCRED` 로 OS uid 를 기록하고 **작업자 uid == 승인자 uid 이면 승인 거부** | 계획서 "동일인 검증"의 취지. 비 Linux 빌드에서는 uid 검사 생략 |
| Q6 | "승인 후 2분" 시작 신호는? | `--run` 은 승인되면 종료코드 0 으로 반환(job id 출력). 본 작업 직전에 `cross_check --start --job <id>` 호출 → 데몬이 승인 후 2분 이내인지 검사. 초과 시 `중단` + 종료코드 1. 데몬은 시작 신호 없는 승인 건도 2분 경과 시 스스로 `중단` | 승인과 실제 작업 사이에 안전장치 5단계(60초 입력 등)가 끼므로 "작업 직전 재확인"이 필요 |
| Q7 | 상태 값? | 계획서 3가지(`대기`/`승인`/`중단`)만. 시작 여부는 `started_at` 필드로 기록(상태는 `승인` 유지). 중단 사유는 `reason` 필드 | 계획서 상태 정의를 늘리지 않음 |
| Q8 | 대조 규칙? | **폐지(Q23)**: 붙여넣기 대조 대신 패스워드 승인 | 사용자 지시 |
| Q9 | 저장소? | `/var/lib/cross_check/jobs.json` (임시파일→rename 원자적 저장, 데몬만 씀). 재기동 시 `대기`/`승인` 건은 연결이 없으므로 `중단`(사유: 데몬 재시작) | 단일 쓰기 주체라 잠금 단순 |
| Q10 | crontab? | `* * * * * /usr/local/bin/cross_check --ensure` — flock(`/var/run/cross_check/daemon.lock`)으로 이미 떠 있으면 즉시 종료, 없으면 데몬 기동. root crontab 에 멱등 등록 | 계획서 "종료되어 있으면 자동 기동" |
| Q11 | `--admin` 권한? | root(uid 0)만. 목록 조회·상태 변경·삭제·전체 정리 | 잘못된 기록 수동 정리 용도 |
| Q12 | 플래그 철자? | `--checker` 와 `--cechker` 모두 허용 | 계획서 명시 |
| Q13 | 기본 `--user {user}` 필수? | 필수. 생략 시 사용법 출력 후 exit 2 | 계획서 0항 |
| Q14 | vm_power_onoff 연동 방식? | conf 키 `cross_check=off`(기본)/`on`, `cross_check_bin=`, 옵션 `-c <checker>`. `on` 이면 **안전장치 1~5단계를 통과한 직후, 실제 전원 작업 직전**에 `cross_check --run`(대상 목록은 stdin) → 확인자 승인 → 곧바로 `cross_check --start` 를 호출한 뒤 실행. (작업자의 사람 실수 방지 단계가 모두 끝난 뒤 확인자가 최종 승인하므로 승인~실행 간격이 짧아 2분 규칙에 안 걸리고, 작업자가 안전장치에서 중단하면 확인자를 부르지 않음) `-n`/`--demo` 는 호출 안 함 | 기존 동작·테스트 무영향(기본 off) |
| Q15 | checker 선택? | `-c` 없으면 conf `users` 에서 번호 선택(본인 제외) | `pickUser` 와 같은 방식 |
| Q16 | `setup.sh` 가 구성할 범위? | ① `vm_power_onoff`, `cross_check` 오프라인 빌드(OS6 은 `bin_os6/*.gz`) ② root(또는 `sudo -n` 가능)이면 `install.sh` 까지 자동 실행(바이너리·conf·`/var/lib`·`/var/run` 디렉터리·crontab·데몬 기동) ③ 아니면 sudo 안내 출력 | "setup.sh 실행 시 필요한 환경이 모두 구성" 요구 |
| Q17 | 테스트 방법? | 순수 로직(파서·상태기계·저장소)은 단위 테스트, 데몬 통합은 임시 소켓 경로로 고루틴 데몬 기동 테스트. 개발기는 Windows → `GOOS=linux go vet`/빌드 확인 | WSL 없음, Linux 실기 검증은 못 함 → 최종 보고에 명시 |
| Q18 | 문서? | `cross_check/README.md` 최상단에 **2분 필수 정책** 인용구, `power_onoff/README.md`·`ARCHITECTURE.md`·`CHANGELOG.md` 갱신 | 계획서 4-2항 |
| Q19 | Git 반영? | 로컬 커밋만(브랜치 `cross-check-daemon`). **push 는 사용자 확인 후** | 외부 반영은 되돌리기 어려움. vendor 는 Windows 긴 경로 때문에 체크아웃이 불완전하므로 `git add -A` 금지, 경로 지정 add 만 |
| Q20 | OS6 용 `cross_check` 바이너리? | `build_os6.sh` 를 확장해 같은 방식으로 빌드하도록 하되, 이 환경에는 Go 1.20 이 없어 `bin_os6/cross_check.gz` 는 **생성하지 못함**(최종 보고에 명시) | 검증 불가 항목은 숨기지 않음 |

## 추가 요구 (사용자 후속 지시) — Q21~Q25

| # | 질문 | 결정 | 근거 |
|---|---|---|---|
| Q21 | 승인되면 작업자에게 무엇을 보이나? | 확인자 승인 직후 작업자 화면(`--run` 출력과 `--user` TUI)에 **내 작업 리스트**(ID, 확인자, 상태, 시각, 대상 VM)를 출력 | 사용자 요구 |
| Q22 | 취소·불일치·2분 만료 등 기타 상태는? | 데몬이 대기 중인 `--run` 연결로 **모든 상태 전이를 이벤트로 푸시**(사유 포함). TUI 목록에도 상태·사유 표시 | 사용자 요구 |
| Q23 | 지정된 확인자 외에는 절대 승인 불가? / 승인 방식? | **모두가 root 로 접속**하므로 OS 계정 바인딩은 제외(uid 는 감사 기록용). 사용자 지시에 따라 **`/etc/cross_check/auth.conf`(0600, root 소유)에 `user=패스워드` 평문 저장**, 확인자는 **작업 대상 붙여넣기 대신 본인 패스워드를 입력하면 승인이 전송**된다(대상 대조 `verify` 단계와 "불일치" 중단 사유는 삭제). 패스워드는 승인·취소·관리자 기능에만 요구하고, 파일이 없거나 group/other 가 읽을 수 있으면 거부(fail closed, `auth=off` 로만 해제). 연속 5회 실패하면 그 사용자의 이후 시도가 1초씩 지연된다(올바른 패스워드는 항상 통과, 잠금으로 확인자를 막는 DoS 방지). 확인자 화면에는 승인 판단을 위해 작업 대상 목록을 **보여준다**. `start` 는 `run` 이 작업자에게 준 일회용 토큰으로만 가능 | 사용자 지시(모두 root, 평문, 패스워드 승인) |
| Q24 | 검증은 어떻게? | 192.168.0.58 에서 root 한 계정으로 **서로 다른 비밀값**을 써서 실제 시나리오를 돌린다: 정상 승인, 패스워드 틀림/미등록/타인 패스워드로 사칭 거부, 취소·만료·끊김 이벤트, admin 패스워드 없는 관리 거부 | 단위 테스트만으로는 부족 |
| Q25 | 완료 보고? | 모든 단계 완료 후 `vm_onoff` 세션(local_2b421fdc…)에 SendMessage 로 완료 내용 통지 | 사용자 요구 |
| Q26 | 패스워드는 어떻게 등록하나? | `cross_check --user X` 를 **처음** 실행하면 TUI 가 패스워드 입력을 받아(확인 재입력) 데몬의 `register` 로 `/etc/cross_check/auth.conf` 에 `X=패스워드` 를 추가한다. 이미 등록된 user 는 덮어쓸 수 없다(변경은 현재 패스워드 필요, 분실 시 admin 이 재설정). 선착순 등록이므로 **모든 user 는 가능한 한 빨리 한 번씩 등록**해야 한다. 등록·작업은 `cross_check.conf` 의 `users=` 목록(관리자가 정함)에 있는 이름만 가능하고, `admin` 은 소켓으로 등록할 수 없으며 install.sh 가 만든 무작위 패스워드를 쓴다(보안 점검 반영). 패스워드는 6~128자 | 사용자 지시 |
| Q27 | 작업자는 어떤 대상을 요청할 수 있나? | **`--user` 본인의 대상(`<target_dir>/on_off_<user>.txt`)의 부분집합만** 요청 가능. 데몬이 `run` 때마다 파일을 직접 읽어 검사하고, 벗어나면 작업 생성 없이 거부. `target_dir` 은 `/etc/cross_check/cross_check.conf` 에 두며 install.sh 가 vm_power_onoff 의 `allowed_dir` 과 같은 값으로 설정 | 사용자 지시 |
| Q28 | 작업자·확인자 등록이 필요한가? | **둘 다 등록되어 있어야** `--run` 이 동작한다(`auth=off` 제외). 미등록이면 등록 방법을 안내하며 거부 | 사용자 지시 |

주의: 모든 사용자가 root 이고 패스워드가 평문이므로, root 로 auth.conf 를 읽거나 데몬·DB 파일을 직접 고치는 의도적 우회는 막을 수 없다. 이 인증은 사칭·실수·무단 승인을 막는 수준이다.

## 보안 점검 반영 (Q29)

읽기 전용 보안 점검에서 나온 지적을 반영했다: 가짜 확인자 이름으로 자기 승인(→ `users=` 목록 제한), `admin` 선점(→ 등록 금지, install.sh 가 생성), 잠금 DoS(→ 지연 방식), 연결/작업 수 상한, 시작 토큰 일회성, 중단 작업 부활 금지. 의도적으로 남긴 것: `run`/`list` 무인증, 평문 패스워드, root 공유 환경의 우회 가능성.

## 후속 지시: 승인 유효시간 설정화와 놀이터 (Q30~Q31)

| # | 질문 | 결정 | 근거 |
|---|---|---|---|
| Q30 | "승인 후 2분 안에 진행하지 않으면 취소" 규칙을 설정으로 바꿀 수 있게? | 2분은 **기본값**으로 두고(문서의 필수 정책 문장은 그대로, 바로 아래에 "기본값이며 `approval_ttl_sec` 로 변경 가능" 추가) `cross_check.conf` 의 `approval_ttl_sec` 를 정식 설정으로: 정수 **10~3600초**(그 밖이면 데몬이 시작하지 않음). 데몬이 실제 값을 `ping`·`run` 응답, 승인 이벤트(`ttl_sec`,`expires_at`), `list`/`get`(`ttl_sec`, 승인 후 미시작이면 `expires_at`)에 **선택 필드**로 알리고, 모든 화면·메시지·중단 사유(`승인 후 {TTL} 내 미진행`)는 `FormatTTL`(2분/45초/1분 30초/1시간)로 그 값을 표기(하드코딩 "2분" 제거). 클라이언트는 데몬 값 → 설정 파일 → 기본 2분 순으로 표시. 반영은 **SIGHUP**(`pkill -HUP -f 'cross_check --daemon'`)로 `approval_ttl_sec` **만** 서버 mutex 안에서 다시 읽음(잘못된 값·깨진 파일이면 이전 값 유지 + 로그, 이미 승인된 작업에도 새 값 적용) 또는 재시작. `install.sh --approval-ttl <초>` 는 새로 만드는 conf 에만 반영(기존 conf 는 덮어쓰지 않고 수정 방법만 안내) | 사용자 지시. 하한 10초는 확인자 승인과 작업자 `--start` 사이의 최소 여유, 상한 1시간은 "승인 직후 바로 작업" 정책의 취지. 재시작은 열린 작업을 중단시키므로 SIGHUP 으로 값만 바꾸게 함. e2e 의 3초 만료 시험은 시험 전용 환경변수 `CROSS_CHECK_TEST_MIN_TTL_SEC` 로 하한만 낮춤(운영 설정 아님) |
| Q31 | 손으로 시험해 볼 환경? | `test/playground.sh`(start/stop/restart/reload/ttl/status/reset/env/help) + `test/playground_worker.sh`(작업자 시뮬레이터). `$CC_PLAY_DIR`(기본 `$HOME/cc_playground`) 한 폴더 안에서만 데몬·conf·auth.conf(0600)·상태·로그·소켓을 쓰고 `/etc`·`/var`·`/usr/local`·crontab 은 건드리지 않음(위험 경로는 거부). 시험 계정 alice/bob/carol/admin 을 고정 시험 패스워드로 미리 등록(파일·도움말에 "시험 전용" 명시). `PATH` 에 두는 래퍼 `bin/cross_check` 와 `eval "$(bash playground.sh env)"` 로 어느 터미널에서나 그냥 `cross_check` 사용 | 사용자 지시("직접 시험해 볼 수 있는 환경") |
