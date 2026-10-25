# CHANGELOG

버전 태그는 이 파일의 항목과 연결합니다. 배포 시점마다 태그를 남기고 태그 메시지에 CHANGELOG 의 어느 항목까지 포함하는지 적습니다(태그는 만들거나 푸시하기 전에 사용자 확인을 받습니다).

```bash
# 이 저장소에는 다른 프로젝트 태그(v0.1.0 등)가 있어 접두어를 붙입니다. 폴더가 저장소 루트인 경우의 기본 형태는 `git tag -a v0.3.0 -m ...` 입니다.
git tag -a vm-power-onoff-v0.3.0 -m "교차검증(cross_check) 기본 on·3단계 대체, 확인자 붙여넣기 승인, 전체 화면 TUI, 작업 단계·--finish (CHANGELOG 2026-10-25 항목)"
git push origin vm-power-onoff-v0.3.0
```

## 0.3.0 — 2026-10-25

교차검증(cross_check)이 원래 목적대로 **안전장치 3단계(VM 이름 입력)를 대신**합니다. vm_power_onoff 버전 0.3.0, cross_check 버전 0.3.0(데몬·화면·프로토콜도 이 릴리스에서 크게 바뀜 — 아래 "cross_check 0.3.0").

- **BREAKING — `cross_check` 기본 on**: `pwonoff.conf` 에 키가 없어도 `on`, 새로 만드는 conf(`pwonoff.conf.example`)도 `cross_check=on`. 끄려면 `cross_check=off`. `on` 인데 `cross_check_bin` 이 없거나 실행할 수 없으면 vCenter 를 묻기 전에 `cross_check 가 설치되어 있지 않습니다: <경로> — bash setup.sh 로 설치하거나 pwonoff.conf 에 cross_check=off 를 지정하세요` 로 종료(exit 1). conf `users` 에 작업자 외 user 가 없을 때도 시작 시 안내 후 종료. `-n`·`--demo` 는 cross_check 없이 동작. 이미 `cross_check=off` 가 적힌 기존 conf 는 그대로 off 입니다.
- **BREAKING — 3/5 대체**: `cross_check=on` 이면 3/5 에서 VM 이름을 묻지 않고 `cross_check --run` 으로 확인자 승인을 기다립니다(대상 VM 이름은 stdin). 거부·확인자 취소·데몬 오류·Ctrl+C → `[취소] 교차검증 미승인`, 전원 작업 없이 exit 1. 이전(0.2.x)의 "1~5단계를 모두 마친 뒤 승인" 순서는 없어졌습니다.
- **2/5 확인자 번호 선택**: 60초 안내 + Enter 대신 `확인자를 선택하세요` 메뉴(conf `users` 에서 작업자 본인 제외, conf 순서). 잘못된 번호는 다시 묻고 3번 틀리면 취소, Ctrl+C 취소, `-c <checker>` 면 `[2/5] 확인자: <checker> (-c)` 로 메뉴 생략.
- **4/5 `ls`**: 질문이 `정말 작업을 진행하시겠습니까? (YeS/No, ls=대상 보기):` 로 바뀌고, `ls`(대소문자·앞뒤 공백 무시)를 입력하면 1단계와 같은 대상 VM 목록(BM별)과 `총 N대` 를 보여 준 뒤 **취소 없이 다시 묻습니다**(횟수 제한 없음). `YeS` 만 진행, 그 밖(`yes`, `y`, `No`, 빈 입력, `ls -l` …)은 지금처럼 취소. 기존 방식(`cross_check=off`)과 `--demo` 에도 적용.
- **5/5 뒤 `--start`**: 10초 카운트다운이 끝나자마자 `cross_check --start --job <ID>`(token 은 자식 환경변수로만, 관문 안에서 1회용). 거부(승인 유효시간 초과 등) → 사유 한 줄 + `[취소] 교차검증 승인 유효시간 초과 또는 거부`, 전원 작업 없음. 승인 유효시간(`approval_ttl_sec`, 기본 2분)은 승인 → 4/5 → 카운트다운 → `--start` 를 덮으며, 승인 직후 안내에 마감 시각이 나옵니다.
- **`cross_check=off`**: 기존 1~5단계(2/5 Enter, 3/5 VM 이름 입력) 그대로 + 4/5 `ls`. **`--demo`**: 데몬이 없으므로 기존 방식 + `ls`, 교차검증 방식에서 2/3단계가 다르다는 안내 한 줄.
- **감사 로그**: 확인자 선택(번호/`-c`), 요청·승인/미승인 분류, 4/5 `ls` 사용 횟수, `--start` 허용/거부를 기록(token 은 기록하지 않음).
- **설치**: `install.sh`/`setup.sh` 에 `--disable-cross-check`(`pwonoff.conf` 의 `cross_check=` 를 `off` 로, 없으면 줄 추가). `--enable-cross-check` 는 효과 없이 안내만(이미 off 인 conf 면 고치는 방법 안내). 마지막 안내 문구를 "기본 on / 끄는 방법" 으로 변경. cross_check 실행 파일을 설치하지 못했는데 conf 가 on 이면 경고.
- **코드**: `GuardOpts` 에 `Checker`·`CrossCheck`·`AfterCountdown`·`OnList` 훅(둘 다 nil 이면 기존 방식), `CrossCheckGate` 를 `SelectChecker`(2/5)·`Approve`(3/5)·`Start`(카운트다운 뒤)로 나누고 `Hook` 으로 연결.
- **시험**: 단위(번호 선택 정상·3회 오입력·Ctrl+C·EOF·`-c`·본인 제외·1명·0명, 승인/거부, `ls` 반복 후 `YeS`·`yes` 취소, `--start` 거부, 기존 방식 유지, 데모, conf 기본 on·예시 conf on), `guard_scenarios.sh` 에 `ls` 4건, `e2e_driver.py` 는 conf 에 `cross_check=off` 를 명시해 기존 방식을 시험(+`ls` 2회, 미설치 시 조기 종료), 새 `test/e2e_vcsim_crosscheck.sh`+`.py`(vcsim + 격리 cross_check 데몬: 번호 선택→TUI 승인→`ls`→`YeS`→카운트다운→Power Off, 확인자 취소, 4/5 `yes`, 틀린 패스워드, 4/5 에서 승인 유효시간 3초 초과→`--start` 거부, 데몬 없음). `e2e_vcsim.sh`·`run_tests.sh` 는 `GOFLAGS` 환경변수를 따름.

### cross_check 0.3.0 (같은 날짜 항목에 포함)

- **BREAKING — 승인 방식 변경(패스워드만 → 붙여넣기 + 패스워드)**: 확인자는 작업자에게 전달받은 대상을 자기 화면에 **붙여넣고**(줄바꿈·탭·공백 구분, 순서 무관) **본인 패스워드**를 입력해 승인합니다. 확인자 화면에는 **대상이 보이지 않습니다**(개수도 안 줌). 붙여넣은 목록이 작업 대상과 다르면(빠짐·남음·중복·빈 입력) 작업이 **중단**(`붙여넣은 대상 불일치(빠짐 N·남음 N·중복 N)`, 개수만 기록)되고 작업자 스크립트도 중단됩니다. 데몬 `approve` 요청에 `pasted` 필드가 추가되었고 `internal/verify/` 가 파싱·대조를 맡습니다. 작업자의 `--run` 화면에는 확인자에게 전달할 대상 목록이 출력됩니다.
- **전체 화면 TUI**: `--user`/`--admin` 이 터미널이면 `auto_setup` 상태 리포트와 같은 전체 화면(raw 모드, 2초 자동 갱신, 머리말에 데몬 pid·카운터, `←→` 구역 전환, 핫키, 붙여넣기 구간 처리, 종료 시 터미널 복원)입니다. 파이프·`--plain`·`TERM=dumb`·raw 전환 실패면 같은 동작의 줄 입력 화면으로 대체합니다. 표준 라이브러리만 사용(go 1.20 유지).
- **승인 이후 단계**: `승인/작업중`(`--start`), `승인/작업완료`(`--finish --result done`: 작업 스크립트가 취소 없이 끝까지 실행됨 — **일부 VM 의 실패는 검사하지 않음**), `승인/작업중단`(`--finish --result aborted`: Ctrl+C·최종 확인 No·시작 전 포기, 작업 제한 시간 초과, 데몬 재시작, 관리자 변경). 새 명령 **`cross_check --finish --job <ID> --result done|aborted`**(token 은 `--start` 와 같은 `CROSS_CHECK_TOKEN`, 끝나면 폐기). 관리자 화면의 상태 변경도 단계를 다룹니다.
- **`work_timeout_sec`**(cross_check.conf, 기본 86400초, 600~604800): `작업중` 인 채 `--finish` 가 없으면 데몬이 `승인/작업중단`(`작업자 응답 없음`)으로 바꿉니다. SIGHUP(`pkill -HUP -f 'cross_check --daemon'`)으로 `approval_ttl_sec` 와 함께 다시 읽습니다. `install.sh` 가 만드는 conf 에는 이 줄이 없어 기본값을 씁니다.
- **호출 스크립트(caller) 기록**: `--run` 이 `--caller NAME` → `CROSS_CHECK_CALLER` → 부모 프로세스 감지 순으로 호출 스크립트 이름을 정해 데몬에 보냅니다(정보용·위조 가능, 제어문자 제거 64자). 사용자·관리자 화면의 목록은 사유 대신 **호출 스크립트** 칸을 보이고 사유는 상세·실시간 이벤트에 나옵니다.
- **vm_power_onoff 연동**: 전원 작업이 끝나면 `cross_check --finish`(done/aborted)로 결과를 알립니다(4/5 에서 No·Ctrl+C 도 aborted).
- **문서**: `cross_check/README.md` 를 git-upload-sk 순서(빌드·설치 → 사용 → 옵션 → 문서별 → 주의사항 → 전역 명령어)로 재구성하고 승인 방식·상태·단계·키를 최신으로 고침. `cross_check/INTERVIEW.md` 의 Q3(TUI 조사)·Q8·Q23(패스워드만 승인)은 정정·철회하고 Q33~Q40 과 계획서 지시사항 점검표 추가. `ARCHITECTURE.md`(verify·화면 파일·문서 행), `WORKFLOW.md`+`workflow.svg`(교차검증 방식 그림 새로 작성), `PR_CHECKLIST.md`, `사용법.txt`(cross_check 명령), `.github/workflows/ci.yml`(vm_power_onoff 와 cross_check 별도 작업) 갱신.
- **시험**: `go test ./...`(cross_check)에 단계·붙여넣기·caller·화면 시험 추가, `test/e2e.sh`(붙여넣기 불일치·`--finish`·작업 제한 시간·caller 감지), 새 `test/pty_e2e.sh`(실제 pty 에서 전체 화면 붙여넣기 승인과 터미널 복원).

## 0.2.1 — 2026-10-25

cross_check 승인 유효시간 설정화와 손으로 해 보는 시험 환경 추가. 필수 정책 문구(아래 0.2.0 항목)는 그대로이고, **2분은 기본값**이며 `cross_check.conf` 의 `approval_ttl_sec` 로 바꿀 수 있습니다.

- **`approval_ttl_sec` 정식 설정**: 정수 **10~3600초**(기본 120). 범위 밖·숫자 아님이면 데몬이 시작하지 않고 한국어 안내(`approval_ttl_sec 는 10~3600 사이의 정수(초)여야 함`)를 냅니다.
- **하드코딩된 "2분" 제거**: 중단 사유 `승인 후 {TTL} 내 미진행`(기본 설정이면 이전과 같은 `승인 후 2분 내 미진행`), `--run` 승인 출력·정책 안내, `--start` 거부 문구, 확인자/작업자 TUI 의 승인 안내와 `남은 시간`, 관리자 화면이 모두 데몬의 실제 값으로 표기됩니다(`FormatTTL`: `2분` · `45초` · `1분 30초` · `1시간`). vm_power_onoff 의 교차검증 관문 안내는 "설정된 시간(cross_check.conf 의 approval_ttl_sec, 기본 2분)" 으로 일반화했고, 시작 거부 문구는 `[취소] 교차검증 승인 유효시간 초과 또는 거부` 입니다.
- **통신(하위 호환)**: `ping` 응답에 `ttl_sec`, `run` 응답에 `ttl_sec`, 승인 이벤트에 `ttl_sec`·`expires_at`, `list`/`get` 의 작업에 `ttl_sec`(+승인 후 미시작이면 `expires_at`)를 **선택 필드**로 추가했습니다. 필드가 없는 구버전과도 동작합니다.
- **재시작 없이 반영(SIGHUP)**: `pkill -HUP -f 'cross_check --daemon'` 이 `approval_ttl_sec` **만** 다시 읽습니다(잘못된 값이면 이전 값 유지 + 로그, 이미 승인된 작업에도 새 값 적용). 이전에는 SIGHUP 을 무시했습니다.
- **화면**: 사용자·관리자 화면 머리말에 `승인 유효시간: … (approval_ttl_sec=N)` 표시(데몬에 물어 표시).
- **설치**: `install.sh`·`setup.sh` 에 `--approval-ttl <초>` — **새로 만드는** `cross_check.conf` 에만 반영하고 기존 파일은 덮어쓰지 않으며 고치는 방법을 안내합니다. 기본 conf 템플릿에 키 설명 주석이 들어갑니다.
- **놀이터**: `cross_check/test/playground.sh`·`playground_worker.sh` — `/etc`·`/var`·crontab 을 건드리지 않는 격리 폴더(`$CC_PLAY_DIR`, 기본 `$HOME/cc_playground`)에 데몬·시험 계정(alice/bob/carol/admin)·대상 파일을 만들고, 작업자 시뮬레이터와 함께 터미널 두 개로 승인·취소·시간 초과·관리자 모드를 직접 시험합니다(`bash playground.sh help` 에 따라 하기 안내).
- **시험**: 단위(`FormatTTL`·`LoadConf` 범위·핑/이벤트/뷰의 ttl·`ReloadApprovalTTL`·화면 표기), e2e 에 설정 3초 표시·SIGHUP 변경·범위 검증(5·99999 거부, 10 허용) 추가. e2e 는 시험 전용 환경변수 `CROSS_CHECK_TEST_MIN_TTL_SEC=3` 으로 최소 10초 검증만 낮춥니다(운영 설정 아님).

## 0.2.0 — 2026-10-24

원격 2인 1조 교차검증 데몬 `cross_check` 추가와 vm_power_onoff 연동. 문서: [cross_check/README.md](cross_check/README.md), 계획서 [cross_check/PLAN.md](cross_check/PLAN.md), 결정 기록 [cross_check/INTERVIEW.md](cross_check/INTERVIEW.md).

> **[필수 정책]** `cross_check` 데몬을 사용하는 작업의 경우, 확인자의 **승인 완료 후 2분 내에 본 작업을 진행하지 않으면 해당 작업 승인은 자동으로 취소(중단)** 처리됩니다.

- **cross_check 데몬**(`cross_check/`, 별도 Go 모듈·표준 라이브러리만): Unix 소켓 `/var/run/cross_check/cross_check.sock`, root crontab `* * * * * cross_check --ensure` 로 상시 구동. 작업 상태 `대기`/`승인`/`중단`과 사유를 `/var/lib/cross_check/jobs.json` 에 기록(재시작 시 열린 작업은 `데몬 재시작` 으로 중단).
- **CLI**: `--user`(사용자 화면) · `--run --user --checker`(`--cechker` 오타 허용, 승인 시 stdout 에 `CROSS_CHECK_JOB`/`CROSS_CHECK_TOKEN` 두 줄) · `--start --job`(승인 후 2분 이내 1회) · `--admin` · `--daemon` · `--ensure`. 종료 코드 0/1/2/3/130, 경로 환경변수 `CROSS_CHECK_*`.
- **승인 방식**: 확인자는 대상을 붙여넣지 않고 화면에 표시된 대상 목록을 확인한 뒤 **본인 패스워드**로 승인/취소. 작업자는 대기 중 모든 상태 전이(승인·취소·2분 만료·종료 등)를 사유와 함께 받고, 승인 직후 내 작업 리스트를 봄. 대기 중 작업자 스크립트가 끝나면(Ctrl+C·세션 끊김·kill -9) 즉시 `중단`. 작업자 = 확인자면 즉시 `중단`.
- **인증·보안 강화**: `cross_check.conf` 의 `users=`(관리자가 관리하는 허용 이름, `auth=required` 면 필수)만 등록·작업·승인 가능, 작업자·확인자 모두 등록 필수, `admin` 은 소켓으로 등록 불가(`install.sh` 가 `auth.conf` 에 `admin=<무작위>` 를 쓰고 한 번만 출력). 패스워드 6~128자, 오답 5회 연속 후 지연(잠금 아님, 올바른 패스워드는 통과), `--start` token 은 1회용, 중단된 작업은 관리자도 되살릴 수 없음. 작업자는 `target_dir/on_off_<user>.txt` 의 부분집합만 요청 가능. 동시 연결 256 · 유휴 읽기 60초 · 사용자당 대기 50건 · 저장 20000건 · 30일 지난 중단 작업 자동 삭제.
- **vm_power_onoff 연동**: conf `cross_check=off|on`(기본 off), `cross_check_bin`, 옵션 `-c <checker>`. `on` 이면 안전장치 1~5단계 통과 직후·전원 작업 직전에 `cross_check --run` → 승인 → 곧바로 `--start`. 거부·중단·2분 초과·Ctrl+C 면 전원 작업 없이 exit 1. `-n`/`--demo` 는 호출하지 않음. 기본 off 라 기존 동작은 그대로.
- **설치**: `setup.sh`/`install.sh` 가 cross_check 빌드·설치, `/etc/cross_check/`(conf·auth), `/var/lib/cross_check`(0700), tmpfiles.d, crontab 등록, 데몬 기동까지 구성(`--enable-cross-check`, `--skip-vm-power-onoff`). `build_os6.sh` 는 `cross_check` 도 Go 1.20 으로 정적 빌드해 `bin_os6/cross_check.gz` 를 만든다(`--cross-check-only`).
- **시험**: `cross_check/` 안의 `go test ./...`(단위·임시 소켓 통합), 격리 E2E `cross_check/test/e2e.sh`, 루트의 `crosscheck_test.go`.
- **한계**: 모든 사용자가 root 이고 패스워드는 평문 저장(설계)이라 root 가 `auth.conf`·기록·데몬을 직접 고치는 우회는 막지 못한다. `run`/`list` 는 패스워드가 없어 사용자 이름을 알면 그 사람의 작업·대상이 보인다. 등록은 `users` 목록 안의 선착순. 자세한 내용은 [cross_check/README.md](cross_check/README.md) 6장. `bin_os6/cross_check.gz` 는 Go 1.20 빌드 서버에서 만들어 추가해야 한다(아직 없음).

## 0.1.0 — 2026-10-23

최초 구현. 설계 결정 전체는 [계획서.md](계획서.md).

- 기능 4종: Power Off(Guest 종료 요청을 대상 전체에 한꺼번에 → 미종료 VM 은 `YeS` 확인 후 강제), Power On(순차: lpage BM 의 VM 은 **ev 단계별** — 모든 ev01 동시 PowerOn → 전부 부팅 대기 → 모든 ev02 동시 PowerOn …, 미사용 BM 은 안내 후 한 번에), 일괄 Power On(OS 설치용), 재부팅(Off 전체 동시 → 완료 확인 → On 전체 동시).
- **병렬 규칙**: 작업 집합 단위로 명령을 한꺼번에 보낸다(VM 하나씩 보내고 기다리지 않음). Power Off · 일괄 Power On · 재부팅은 대상 전체 동시(`concurrency` 기본 200은 상한). 순차 부팅은 2번 기능에만 있고, 그 안에서도 VM 단위가 아니라 **ev 단계(같은 ev 번호 전체) 단위**로 순차이며 단계 안은 병렬이다. 부팅 지연 질문은 단계당 한 번(c/s/q).
- 안전장치 5단계: 6자리 코드 → 대상 목록·총 대수 → Enter 대기 → 60초 VM 이름 입력(구분자 무관·순서 무관·중복/불일치 즉시 취소) → `YeS` → 10초 가로 카운트다운. 모든 단계 Ctrl+C 즉시 중단, 취소 사유 메시지, exit 1.
- VWP 검사: VM 의 모든 상위 폴더 경로·NIC 포트그룹 이름에 `vwp`(대소문자 무시) 포함 시 경고 배너 + 별도 6자리 확인 코드.
- 실행 제한: `/etc/vcetner_pwon_off/pwonoff.conf`(root 소유·group/other 쓰기 금지)의 `allowed_dir` 에서만 실행, `on_off_<user>.txt` 이외 파일 불가(심볼릭 링크 거부).
- vCenter 지정·인증은 VMsetup 방식: 번호 선택(0=직접, Enter=이전), `secret_lib.sh` 암호 파일 → 직접 입력, `VC_PASSWORD` 환경변수는 읽지 않음.
- 부팅 완료 판정: Tools 실행 + heartbeat green + IPv4 → 22 포트 직접 → 막힌 대역은 `os6_host` 경유 ssh(`/dev/tcp`).
- 감사 로그 `allowed_dir/run_<user>/`(비밀번호·확인 코드 미기록).
- 빌드: vendor 포함 오프라인 빌드(`setup.sh`), OS6(Go 1.20) 정적 빌드 `bin_os6/vm_power_onoff.gz`, `install.sh`.
- 수정: 비밀번호를 공용 stdin 리더로 읽도록 해 비밀번호 줄이 다음 메뉴 입력으로 새는 문제를 방지(E2E `user/vCenter/기능 번호 선택 + 비밀번호 유출 없음`).
- 검증: 단위 테스트(race 포함), vcsim 통합 테스트, 안전장치 시나리오(`--demo` 파이프 입력), vcsim 위 E2E 14 시나리오, OS6 바이너리 `--demo` 실행. 빌드 서버 192.168.0.58(Rocky 8, Go 1.26.5), OS6 빌드 192.168.0.60(Go 1.20.14).
- 한계: 실제 vCenter 에서는 아직 확인하지 못했다(vCenter 사용 불가). vcsim 은 Tools/heartbeat/IP 를 채우지 않아 부팅 완료 판정의 vCenter 쪽 값과 22 포트 경유는 가짜 구현·단위 테스트로만 검증했다.
