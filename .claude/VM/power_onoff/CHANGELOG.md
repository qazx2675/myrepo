# CHANGELOG

버전 태그는 이 파일의 항목과 연결합니다: `git tag -a vm-power-onoff-v0.1.0 -m "<요약> (CHANGELOG 2026-10-23 항목)"`

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
