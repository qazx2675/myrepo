# vm_power_onoff — vCenter VM 안전 Power Off / Power On / 재부팅

vCenter 에 연결된 VM(`<BM이름>ev01`, `<BM이름>ev02` …)을 **휴먼 에러가 나지 않도록 다단계 안전장치**를 거쳐 끄고·켜고·재부팅하는 Go 도구입니다.
user 를 번호로 고르고 기능을 고르면, (VWP 해당 시 담당자 확인 문구 입력) → 안전장치 5단계를 통과한 뒤에만 실제 전원 작업을 합니다.
**0.3.0 부터 교차검증(`cross_check`)이 기본 on** 입니다: 안전장치 3단계(VM 이름 직접 입력) 대신 다른 사람(확인자)이 자기 터미널(`cross_check --user <확인자>`)에서 **작업자에게 전달받은 대상 목록을 붙여넣고 본인 패스워드로 승인**합니다(확인자 화면에는 대상이 보이지 않고, 붙여넣은 목록이 다르면 작업이 중단됩니다). 끄려면 conf 에 `cross_check=off`.

| 기능 | 동작 |
|---|---|
| 1) Power Off | Guest OS 정상 종료 요청을 **대상 전체에 한꺼번에** 보냄 → 제한 시간 안에 안 꺼진 VM 은 목록을 보여주고 `YeS` 확인 후 강제 PowerOff |
| 2) Power On (순차) | lpage 사용중 BM 의 VM 은 **ev 단계별**로: 모든 ev01 에 동시에 PowerOn → 전부 부팅 완료될 때까지 대기 → 모든 ev02 에 동시에 PowerOn → … . lpage 미사용 BM 은 안내 후 한 번에 부팅 |
| 3) 일괄 Power On | OS 설치용. 순서·부팅 확인 없이 **전 대상 병렬**로 한 번에 켬 |
| 4) 재부팅 | Power Off 를 대상 전체에 한꺼번에 → 전부 꺼진 것을 확인 → Power On 을 대상 전체에 한꺼번에 (순차 부팅은 2번 기능에만 있음) |

> 작업 흐름도: [WORKFLOW.md](WORKFLOW.md) / 폴더·파일 역할: [ARCHITECTURE.md](ARCHITECTURE.md) / 설계 결정 전체: [계획서.md](계획서.md)

## 1. 빌드 및 설치 방법

이 폴더만 통째로 받으면 **인터넷 없이** 빌드됩니다(의존성 govmomi 가 `vendor/` 에 들어 있고, `setup.sh` 가 `GOPROXY=off`, `-mod=vendor` 로 빌드).

```bash
# 요구사항: Go 1.26.5 이상, Linux (Rocky Linux 8 에서 검증). OS6 는 Go 없이 미리 빌드된 파일 사용
cd power_onoff
bash setup.sh                 # → ./vm_power_onoff  (OS6 서버에서는 자동으로 bin_os6/vm_power_onoff.gz 를 풀어 둠)
sudo bash install.sh          # /usr/local/bin/vm_power_onoff 와 /etc/vcetner_pwon_off/pwonoff.conf 설치
sudo vi /etc/vcetner_pwon_off/pwonoff.conf   # users / vcenters / (필요 시) secret_lib, os6_host 수정
```

- `install.sh [허용디렉터리]`: 허용디렉터리 기본값은 이 폴더 절대경로(`allowed_dir`). 이미 `pwonoff.conf` 가 있으면 덮어쓰지 않습니다. 제거는 `sudo bash install.sh -u`.
- `setup.sh` / `install.sh` 는 교차검증 데몬 `cross_check`([cross_check/README.md](cross_check/README.md))도 함께 빌드·설치하고 데몬까지 띄웁니다. `pwonoff.conf` 의 `cross_check` 는 **기본 `on`**(새로 만드는 conf 도 `cross_check=on`)이고, 끄려면 `sudo bash install.sh --disable-cross-check`(또는 `bash setup.sh --disable-cross-check`, 기존 conf 의 `cross_check=` 줄을 `off` 로 바꿈)를 줍니다. `--enable-cross-check` 는 이제 효과가 없고 안내만 출력합니다(예전 스크립트 호환). 이미 `cross_check=off` 가 적힌 기존 conf 는 설치로 바뀌지 않으므로 켜려면 직접 `on` 으로 고치세요. 처음 설치할 때 **관리자(admin) 패스워드가 화면에 한 번만 출력**됩니다.
- `cross_check=on` 인데 `cross_check_bin`(기본 `/usr/local/bin/cross_check`)이 없거나 실행할 수 없으면 vm_power_onoff 는 vCenter 를 묻기 전에 `cross_check 가 설치되어 있지 않습니다: <경로> — bash setup.sh 로 설치하거나 pwonoff.conf 에 cross_check=off 를 지정하세요` 로 멈춥니다(`-n`, `--demo` 는 cross_check 없이 동작).
- **`os6_host` 직접 채우기 (선택)**: 부팅 완료 확인 때 vm_power_onoff 가 VM 의 22 포트에 직접 접속할 수 없는 대역(예: 관리망 분리)이 있을 때만 필요합니다. 설치된 conf 에서 두 줄을 채웁니다 — `sudo vi /etc/vcetner_pwon_off/pwonoff.conf` 후 `os6_host=<경유할 서버 IP 또는 호스트명>`, `os6_user=root`(기본값). 경유 서버에는 이 서버에서 **비밀번호 없이(키 인증) `ssh <os6_user>@<os6_host>` 로 접속**되어야 하므로 먼저 `ssh-copy-id <os6_user>@<os6_host>` 후 `ssh <os6_user>@<os6_host> true` 로 확인하세요. 두 값 모두 채워야 동작하고, 비워 두면(기본) 직접 접속만 확인합니다. 저장 후 별도 재시작은 필요 없고 다음 실행부터 적용됩니다. 이 값은 스크립트로 채워 주지 않으며(가이드·패치 스크립트 없음) 직접 수정합니다.
- OS6(RHEL/CentOS 6, 커널 2.6.32)용 실행파일은 Go 1.20 빌드 서버에서 `bash build_os6.sh` 로 `bin_os6/vm_power_onoff.gz` 를 만듭니다(저장소에 포함됨).
- 설정 디렉터리 이름은 요구사항 그대로 **`/etc/vcetner_pwon_off/`** 입니다(철자 `vcetner`).

## 2. 사용 방법

대상 파일을 **허용 디렉터리(`allowed_dir`)** 에 `on_off_<user>.txt` 이름으로 만듭니다. **VM 이름만** 한 줄에 하나(`#` 주석 가능). BM 이름은 적지 않습니다.

```text
# on_off_hong.txt  — BM svr1 의 VM 3대, BM svr2 의 VM 1대
svr1ev01
svr1ev02
svr1ev03
svr2ev01
```

```bash
cd /home/power_onoff            # conf 의 allowed_dir. 이 디렉터리 밖에서는 실행 불가
vm_power_onoff                  # 1) user 번호 선택 → 2) vCenter 선택 → 3) 비밀번호 → 4) 기능 선택 → 5) 안전장치 → 실행
vm_power_onoff -u hong          # user 지정 (Enter = 이전 실행 vCenter)
vm_power_onoff -u hong -v 192.168.0.50 -n   # 계획(대상·상태·VWP)만 확인, 전원 작업 없음
vm_power_onoff -u hong -c kim   # 교차검증 확인자를 kim 으로 미리 지정 (2/5 번호 선택 생략)
vm_power_onoff --demo           # vCenter 없이 샘플 3대로 안전장치만 체험 (기존 방식)
```

진행 순서:

1. **허용 경로 확인** — 현재 디렉터리가 `allowed_dir` 이 아니면 즉시 종료. conf 가 root 소유가 아니거나 group/other 쓰기 가능이어도 종료.
2. **user 선택**(숫자 1~) → `on_off_<user>.txt` 만 읽습니다(다른 이름·심볼릭 링크 불가).
3. **vCenter 선택**(번호, `0`=직접 입력, Enter=이 user 의 이전 vCenter) → **비밀번호**(`secret_lib` 암호 파일 → 없으면 직접 입력, 화면 표시 안 함).
4. **기능 선택**(1~4) → 대상 VM 을 vCenter 에서 조회해 **계획표**(BM별 전원 상태, lpage 사용/미사용 → 부팅 방식)를 출력. 대상 VM 이 vCenter 에 없거나 같은 이름이 2대 이상이면 **아무 작업도 하지 않고** 목록을 보여준 뒤 종료합니다.
5. **VWP 확인** — 대상 VM 의 상위 폴더 경로 또는 포트그룹 이름에 `vwp`(대소문자 무시)가 있으면 빨간 경고 배너가 뜨고, 담당자 확인 후 `담당자 확인이 완료되었다면 VWPekaekdwkghkrdls(VWP담당자확인)을 그대로 입력하세요.` 에 따라 **`VWPekaekdwkghkrdls`** 를 입력해야 다음으로 갑니다.
6. **안전장치 5단계**(아래) 통과.
7. **실행** → 결과 표(성공/스킵/실패, 소요 시간) → 하나라도 실패하면 종료코드 1. 실행 기록은 `allowed_dir/run_<user>/` 에 남습니다.

### 안전장치 5단계

conf 의 `cross_check` 에 따라 2·3단계가 다릅니다. **기본은 `on`(교차검증 방식)** 입니다.

| 단계 | `cross_check=on` (기본, 교차검증 방식) | `cross_check=off` (기존 방식) | 틀리면 |
|---|---|---|---|
| 1/5 | `woqnxldwkrdjq(재부팅작업)을 그대로 입력하세요.` — 괄호 앞 영문 `woqnxldwkrdjq`(한글 '재부팅작업'을 영문 자판으로 친 것)를 입력. Power Off/On/재부팅 모두 같은 문구, 대소문자까지 정확히(앞뒤 공백만 무시) → 통과하면 작업 대상 호스트 목록(BM별)과 `총 N대` 출력 | (같음) | 즉시 취소 `[취소] 작업 확인 문구 불일치`(exit 1) |
| 2/5 | `확인자를 선택하세요` — conf `users` 에서 **작업자 본인을 뺀** 번호 목록 중 번호 입력. 틀리면 다시 묻고 **3번 틀리면 취소**. `-c <checker>` 를 주면 `[2/5] 확인자: kim (-c)` 로 메뉴 생략 | "다음 단계는 1분(60초)의 입력 제한 시간이 있습니다…" 안내 후 Enter 로 진행 | 3회 오입력·입력 끝 → 취소 |
| 3/5 | **VM 이름 입력 대신** `cross_check --run` 으로 확인자 승인 대기. 화면에 출력되는 **대상 목록을 작업자가 확인자에게 전달**하면, 확인자가 자기 터미널에서 `cross_check --user <확인자>`(전체 화면) → 작업 선택 → **전달받은 대상 붙여넣기 → 본인 패스워드**로 승인. 붙여넣은 목록이 다르면 작업이 중단됨(확인자 화면에는 대상이 보이지 않음) | 대상 VM 이름을 **직접 입력**(복사·붙여넣기 가능, 공백·탭·줄바꿈 구분, 순서 무관). 60초 이내. 대상과 정확히 같아야 하며 **중복이 하나라도 있거나 다르면** 취소 | on: 대상 불일치·확인자 취소·데몬 오류·Ctrl+C → `[취소] 교차검증 미승인` / off: 누락·대상 아님·중복 표시 후 취소 |
| 4/5 | `정말 작업을 진행하시겠습니까? (YeS/No, ls=대상 보기):` — 대소문자까지 정확히 `YeS` 만 통과. **`ls`**(대소문자·앞뒤 공백 무시)를 입력하면 1단계와 같은 대상 VM 목록과 `총 N대` 를 보여 주고 **취소 없이 다시 묻습니다**(몇 번이든) | (같음) | `yes`,`YES`,`y`,`No`, 빈 입력 등 모두 취소 |
| 5/5 | `10 .. 9 .. 8 .. 7 .. 6 .. 5 .. 4 .. 3 .. 2 .. 1` 한 줄 카운트다운 (Ctrl+C 로 취소) → 끝나자마자 `cross_check --start` 로 승인 유효시간 안인지 확인(→ 전원 작업 → 끝나면 `cross_check --finish` 로 결과 보고) | 카운트다운만 | on: 승인 유효시간 초과·거부 → `[취소] 교차검증 승인 유효시간 초과 또는 거부` |

- 교차검증 방식에서 **승인 유효시간**(`cross_check.conf` 의 `approval_ttl_sec`, 기본 2분)은 승인 순간부터 4/5(`YeS`)와 5/5(카운트다운 10초)를 지나 `--start` 까지를 덮습니다. 승인 직후 화면에 `승인 유효시간: 2분 — HH:MM:SS 까지 …` 가 나오므로 그 안에 `YeS` 를 입력하세요. 넘기면 전원 작업 없이 exit 1.
- 모든 단계에서 Ctrl+C(SIGINT)는 즉시 중단(exit 1)이며, 취소 사유가 화면에 `[취소] …` 로 표시됩니다. `--demo` 는 데몬 없이 기존 방식으로 돌고(`ls` 는 지원), `-n` 은 안전장치·교차검증 없이 계획만 봅니다.

화면 예(교차검증 방식, vcsim 시험에서 캡처 — 작업자 tester, 확인자 other):

```text
[1/5] woqnxldwkrdjq(재부팅작업)을 그대로 입력하세요.
 > [INFO] 작업 확인 문구 일치
작업 대상 호스트 목록:
svr1ev01
총 1대

[2/5] 확인자를 선택하세요 (교차검증 승인자, 작업자 tester 제외):
  1) other
번호 입력: 확인자: other

[3/5] 교차검증 — 확인자 other 의 승인을 받습니다 (VM 이름 입력 대신)
※ cross_check 정책: 확인자 승인 후 설정된 시간(cross_check.conf 의 approval_ttl_sec, 기본 2분) 내에 본 작업을 진행하지 않으면 승인이 자동 취소됩니다.
[INFO] 확인자 other 에게 교차검증을 요청합니다 (대상 1대). 승인을 기다리는 중... (Ctrl+C 로 취소)
[대기] 작업 tnflu2d7c9 — 확인자 other 의 승인을 기다립니다 (Ctrl+C 로 취소)
  ...
[승인] 작업 tnflu2d7c9 — other 가 승인했습니다 (17:46:51)
  ...
  승인 유효시간: 2분 — 17:48:51 까지 아래 --start 를 호출해야 합니다
[INFO] 교차검증 승인됨 (확인자 other, 작업 ID tnflu2d7c9)
[INFO] 승인 유효시간 안에 4/5 YeS 입력과 5/5 카운트다운을 마쳐야 전원 작업이 시작됩니다 (cross_check --start 는 카운트다운 직후 자동 호출, 넘기면 승인 자동 취소)
[4/5] 정말 작업을 진행하시겠습니까? (YeS/No, ls=대상 보기): ls
작업 대상 호스트 목록:
svr1ev01
총 1대
[4/5] 정말 작업을 진행하시겠습니까? (YeS/No, ls=대상 보기): YeS
[5/5] (Ctrl+C 로 취소 가능)
10 .. 9 .. 8 .. 7 .. 6 .. 5 .. 4 .. 3 .. 2 .. 1
[INFO] 교차검증 통과 — 전원 작업을 시작합니다
```

## 3. 옵션별 상세 설명

### 명령행 옵션

| 옵션 | 설명 |
|---|---|
| `-u <user>` | user 를 번호 선택 없이 지정(conf `users` 에 있어야 함) |
| `-v <vCenter>` | vCenter 주소 직접 지정(`host` 또는 `host:port`) |
| `-id <계정>` | vCenter 계정(기본: conf `vc_id`, 보통 `lscsystems@vsphere.local`) |
| `-n` | 계획까지만 확인하고 종료(전원 작업 없음) |
| `-c <checker>` | 교차검증 확인자 지정(conf 의 `users` 에 있고 본인이 아니어야 함, 틀리면 vCenter 접속 전에 오류). `cross_check=on`(기본)일 때 안전장치 2/5 의 번호 메뉴를 건너뜀(`[2/5] 확인자: <checker> (-c)`). 생략하면 2/5 에서 본인을 뺀 번호 목록에서 고름. `cross_check=off` 에서 주면 경고 후 무시 |
| `--demo` | 샘플 3대(`web-prd-01`, `web-prd-02`, `was-prd-01`)로 안전장치 1~5단계만 실행(데몬이 없으므로 **기존 방식**: 2/5 Enter, 3/5 VM 이름 입력, 4/5 `ls` 지원). vCenter·conf 불필요, 전원 작업 없음 |
| `--version`, `-h` | 버전 / 도움말 |

### 설정 파일 `/etc/vcetner_pwon_off/pwonoff.conf`

`키=값` 형식, `#` 주석. 알 수 없는 키·중복 키·잘못된 값은 시작할 때 오류로 종료합니다. 예시: [pwonoff.conf.example](pwonoff.conf.example)

| 키 | 기본값 | 설명 |
|---|---|---|
| `allowed_dir` | (필수) | 이 디렉터리에서만 실행 가능. `on_off_<user>.txt`, `run_<user>/` 가 여기에 위치 |
| `users` | (필수) | 선택 가능한 user 목록(콤마). 영문 소문자/숫자/`_`. 적은 순서가 번호 |
| `vcenters` | — | vCenter 목록(콤마), 번호 선택용 |
| `vc_id` | `lscsystems@vsphere.local` | vCenter 계정 |
| `secret_lib` | (빈 값) | VMsetup 의 `secret_lib.sh` 경로. 채우면 암호 파일에서 비밀번호를 읽고, 비우면 매번 입력 |
| `os6_host`, `os6_user` | (빈 값), `root` | 부팅 완료 확인 시 직접 접속이 안 되는 대역을 `ssh os6_user@os6_host` 로 경유(키 인증). 비우면 직접 확인만 |
| `cross_check` | **`on`** (키가 없어도 on) | `on` 이면 안전장치 2/5 확인자 번호 선택, **3/5 VM 이름 입력 대신 확인자(다른 user) 승인**, 5/5 카운트다운 직후 `--start`. `off` 면 기존 방식(2/5 Enter, 3/5 VM 이름 입력). `-n`, `--demo` 에서는 호출하지 않음 |
| `cross_check_bin` | `/usr/local/bin/cross_check` | 교차검증 실행 파일 절대경로(비우면 기본값). `cross_check=on` 이면 시작할 때(vCenter 접속 전) 실행 가능한지 확인하고, 없으면 `cross_check=off` 로 끄는 방법을 안내하며 종료 |
| `concurrency` | 200 | 한 번에 vCenter 로 보내는 최대 동시 작업 수. 대상이 이보다 많을 때만 나머지를 이어서 보냄 (보통 대상 전체가 한꺼번에 나감) |
| `shutdown_timeout` | 300 | Guest 종료 대기(초) |
| `boot_timeout` | 600 | 순차 부팅에서 한 ev 단계의 VM 부팅 완료 대기(초) |
| `boot_poll_interval` | 10 | 상태 확인 간격(초) |
| `port_check_timeout` | 3 | 22 포트 연결 제한(초) |

### 순차 부팅(2번)의 판정 규칙

- **lpage 사용중**: 그 BM 의 대상 VM 중 하나라도 `sched.mem.lpage.enable1GPage=TRUE` → 그 BM 의 VM 은 **ev 단계별** 순차 대상.
- **ev 단계(wave)**: 순차 대상 BM 전체에서 같은 ev 번호끼리 한 단계입니다. 예) 대상이 svr1ev01~02, svr2ev01~02 이면 ① `svr1ev01`, `svr2ev01` 에 **동시에** PowerOn → 둘 다 **부팅 완료될 때까지 대기** → ② `svr1ev02`, `svr2ev02` 에 동시에 PowerOn → … . VM 하나씩 켜는 것이 아니라, 단계 안은 병렬이고 단계 사이만 순차입니다.
- **부팅 완료** = ① VMware Tools 실행 + heartbeat green + IPv4 확보 → ② 22 포트 응답(직접, 막힌 대역은 `os6_host` 경유). 화면에 `22 직접` / `22 os6_mgmt 경유` 로 구분 표시.
- **lpage 미사용**: `※ <BM>: lpage 미사용 — 한 번에 부팅해도 됩니다.` 안내 후 순차 단계를 기다리지 않고 한 번에 켭니다(부팅 완료 대기 없음).
- 한 단계에서 제한 시간(`boot_timeout`) 안에 부팅이 안 된 VM 이 있으면 **그 VM 목록을 보여주고 한 번만** `계속 대기(c) / 이 VM 건너뛰고 다음 ev 단계(s) / 순차 부팅 중단(q)` 를 묻습니다. `c` 는 못 켜진 VM 만 다시 대기, `s` 는 그 VM 을 실패로 두고 다음 ev 단계, `q` 는 남은 ev 단계를 실행하지 않습니다. 같은 단계의 다른 VM 은 정상 처리됩니다.
- BM 은 이름이 아니라 vCenter 에서 조회한 **실제 ESXi 호스트** 기준으로 묶습니다(이름상 BM 과 다르면 계획표에 `[주의]` 표시).

### VWP 검사

VM 이 속한 **모든 상위 폴더 이름**과 **모든 NIC 포트그룹 이름**에 `vwp`(대소문자 무시)가 들어 있으면 `vwp대상 장비가 포함되어있습니다. vwp장비의 경우 vwp담당자에게 확인이 필요합니다.` 경고와 해당 VM·사유가 표시되고, 담당자 확인 후 고정 문구 `VWPekaekdwkghkrdls`(한글 'VWP담당자확인'을 영문 자판으로 친 것, 대소문자까지 정확히)를 입력해야 합니다(안전장치 1/5 문구와는 별개). 틀리면 `[취소] VWP 담당자 확인 문구 불일치`(exit 1).

### 종료 코드와 실행 기록

- 정상 완료 `0`. 안전장치 취소·Ctrl+C·오류·실패 VM 이 하나라도 있으면 `1`.
- `allowed_dir/run_<user>/YYYYmmdd_HHMMSS_<기능>.log`(디렉터리 700, 파일 600): 실행 시각·OS 계정·user·vCenter·기능·대상·VWP 여부·안전장치 통과/취소 사유·4/5 `ls` 사용 횟수·VM별 결과, 교차검증 방식이면 고른 확인자(번호/`-c`)·승인/미승인 분류·`--start` 허용/거부. **비밀번호, 확인 문구 입력값, cross_check token 은 기록하지 않습니다.**

### 교차검증(cross_check) 연동

> **[필수 정책]** `cross_check` 데몬을 사용하는 작업의 경우, 확인자의 **승인 완료 후 2분 내에 본 작업을 진행하지 않으면 해당 작업 승인은 자동으로 취소(중단)** 처리됩니다.
>
> 위 **2분은 기본값**입니다. `/etc/cross_check/cross_check.conf` 의 `approval_ttl_sec`(초, 10~3600, 기본 120)로 바꿀 수 있습니다. 새로 설치할 때는 `sudo bash install.sh --approval-ttl 300`(새 conf 에만 적용), 이미 운영 중이면 conf 수정 후 `sudo pkill -HUP -f 'cross_check --daemon'` 으로 재시작 없이 반영합니다. 자세한 방법: [cross_check/README.md](cross_check/README.md#승인-유효시간-바꾸기).

2인 1조 교차검증을 원격으로 하는 `cross_check` 데몬([cross_check/README.md](cross_check/README.md))과 연동합니다(**0.3.0 부터 기본 on**). 원래 cross_check 는 안전장치 3단계(VM 이름 입력)를 **대신하려고** 만든 것이므로, 교차검증 방식에서는 3/5 에서 VM 이름을 묻지 않고 확인자 승인을 기다립니다. 작업자는 자기 터미널에서 대기하며 화면에 나온 **대상 목록을 확인자에게 전달**하고, 확인자는 자기 터미널에서 `cross_check --user <확인자>` 로 작업을 골라 **전달받은 대상을 붙여넣은 뒤 본인 패스워드로 승인**합니다. 확인자 화면에는 대상이 보이지 않고, 붙여넣은 목록이 작업 대상과 다르면(순서는 무관) 작업이 **중단**됩니다.

| 설정 | 설명 |
|---|---|
| conf `cross_check=on` | 교차검증 사용(**기본**, 키가 없어도 on). 끄려면 `cross_check=off` 또는 `sudo bash install.sh --disable-cross-check` |
| conf `cross_check_bin=` | `cross_check` 실행 파일 절대경로(기본 `/usr/local/bin/cross_check`). 없으면 시작하자마자 안내 후 종료 |
| `-c <checker>` | 확인자 미리 지정(2/5 메뉴 생략). 생략하면 2/5 에서 번호로 선택(본인 제외, 3회 오입력 시 취소) |

```bash
vm_power_onoff -u hong             # 1/5 작업 확인 문구 → 2/5 확인자 번호 → 3/5 확인자 승인 대기 → 4/5 YeS(ls=대상 보기) → 5/5 카운트다운 → 전원 작업
vm_power_onoff -u hong -c kim      # 2/5 를 kim 으로 미리 선택
```

- 순서: **1/5 작업 확인 문구 → 2/5 확인자 선택 → 3/5 승인 대기(`cross_check --run`, 대상 VM 이름은 stdin) → 4/5 `YeS` → 5/5 카운트다운 → `cross_check --start`(token 은 자식 프로세스 환경변수로만) → 전원 작업 → `cross_check --finish`**. 작업자가 1·2단계에서 취소하면 확인자를 부르지 않습니다. 전체 그림은 [WORKFLOW.md](WORKFLOW.md).
- 3/5 에서 대상 불일치·확인자 취소·데몬 오류·Ctrl+C 면 `[취소] 교차검증 미승인`, 승인 후 4/5 에서 `YeS` 가 아니면 `[취소] YeS 미입력 …`(`--finish --result aborted` 로 `승인/작업중단` 기록, 알리지 못하면 승인 유효시간이 지나 데몬이 자동 중단), 카운트다운 뒤 `--start` 가 거부되면(승인 유효시간 초과 등) `[취소] 교차검증 승인 유효시간 초과 또는 거부` — 모두 **전원 작업 없이 종료 코드 1** 이고 감사 로그에 남습니다.
- 승인 유효시간(`approval_ttl_sec`, 기본 2분)은 **승인 → 4/5 → 카운트다운(10초) → `--start`** 를 덮습니다. 4/5 에서 오래 머물면 만료됩니다. `--start` 이후의 본 작업은 `work_timeout_sec`(기본 24시간) 안에 `--finish` 로 마쳐야 합니다.
- **작업 상태**: `대기` → `승인` → `승인/작업중`(`--start`) → `승인/작업완료`(전원 작업 스크립트가 취소 없이 끝까지 실행됨) 또는 `승인/작업중단`(Ctrl+C·4/5 에서 No·시간 초과). **`승인/작업완료` 는 일부 VM 의 실패 여부를 검사하지 않습니다** — 결과 표와 실제 전원 상태를 확인하세요. 호출 스크립트 이름(`vm_power_onoff`)은 cross_check 목록에 표시되며 정보용입니다.
- user(`-u`)와 확인자는 `cross_check` 쪽 설정(`/etc/cross_check/cross_check.conf` 의 `users=`)에도 있고 `cross_check --user <이름>` 으로 **처음 한 번 패스워드를 등록**해야 합니다. `pwonoff.conf` 와 `cross_check.conf` 의 `users` 는 서로 다른 목록입니다.
- 데몬은 올린 대상이 `<target_dir>/on_off_<user>.txt` 의 부분집합인지 검사하므로 `cross_check.conf` 의 `target_dir` 은 `allowed_dir` 과 같아야 합니다(`install.sh` 가 같은 값으로 설정).
- 보안 모델·알려진 한계(모두 root 로 쓰므로 root 우회는 막지 못함, 패스워드 평문 저장, 호출 스크립트 이름은 위조 가능 등)는 [cross_check/README.md](cross_check/README.md#보안-모델과-한계)를 읽어 주세요.

## 4. 문서별 설명

| 문서/폴더 | 내용 |
|---|---|
| [README.md](README.md) | 이 문서: 설치, 사용법, 옵션, 주의사항 |
| [사용법.txt](사용법.txt) | 수동 시험에 쓸 명령어 모음(각 명령 위에 설명 주석, cross_check 설치·등록·승인·`--finish`·놀이터 포함) |
| [계획서.md](계획서.md) | 요구사항·인터뷰 결정 사항·설계 전체, 구현 후 변경 사항 |
| [ARCHITECTURE.md](ARCHITECTURE.md) | 폴더·파일별 역할 표(cross_check 포함) — 어디를 고치면 되는지 |
| [WORKFLOW.md](WORKFLOW.md), `workflow.svg` | 실행 흐름도 (mermaid / 렌더한 SVG, vm_power_onoff + cross_check 교차검증 방식) |
| [CHANGELOG.md](CHANGELOG.md) | 변경 이력과 배포 태그 규칙 |
| [cross_check/README.md](cross_check/README.md) | 교차검증 데몬: 2분(기본) 필수 정책과 승인 유효시간 설정, 설치·등록, 명령, 보안 모델·한계, 문제 해결 (계획서 [PLAN.md](cross_check/PLAN.md), 결정 기록 [INTERVIEW.md](cross_check/INTERVIEW.md)) |
| [PR_CHECKLIST.md](PR_CHECKLIST.md) | 수정·배포 전 체크리스트(cross_check 항목 포함) |
| `test/` | `run_tests.sh`(전체 시험), `guard_scenarios.sh`(안전장치 시나리오, `--demo`), `e2e_vcsim.sh`+`e2e_driver.py`(vcsim 위 전체 흐름, 기존 방식 `cross_check=off`), `e2e_vcsim_crosscheck.sh`+`e2e_vcsim_crosscheck.py`(vcsim + 격리 cross_check 데몬으로 교차검증 방식: 승인·확인자 취소·`yes`·승인 유효시간 초과·데몬 없음), `vcsim_server/`(시험용 vCenter 시뮬레이터) |
| `dev/sync_test.sh` | 로컬에서 쓴 코드를 빌드 서버(192.168.0.58)로 보내 go vet/test/build 를 돌리는 도우미 |
| `.github/workflows/ci.yml` | 커밋마다 두 작업: vm_power_onoff(vendor, `-mod=vendor`) 빌드·vet·테스트, cross_check(별도 모듈, 표준 라이브러리만) 빌드·vet·테스트 |

시험 실행(빌드 서버, root): `bash test/run_tests.sh` → 마지막 줄 `PASS=N FAIL=M`. 실제 vCenter 없이 vcsim 으로 모든 기능·안전장치·취소 경로를 확인합니다.

## 주의사항 (Disclaimer)

> 본 로그 분석 관련 스크립트 및 툴은 100% 신뢰하기보다는 참고용(보조 도구)으로 사용하는 것을 권장합니다.
> **설정 변경·전원 작업 스크립트의 경우, 작업 후 무작위로 서버 몇 대를 골라 실제로 변경(전원 상태·부팅 완료)되었는지 직접 확인하는 절차가 반드시 필요합니다.**

- 이 도구는 **전원을 직접 끄고 켭니다.** 반드시 `-n` 으로 대상·계획을 먼저 확인하고, 안전장치에서 대상 목록을 눈으로 대조하세요(교차검증 방식은 확인자가 3/5 에서 대조, 작업자는 4/5 에서 `ls` 로 다시 볼 수 있음).
- 강제 PowerOff 는 Guest OS 가 정상 종료되지 못한 VM 에만 묻고, `YeS` 가 아니면 하지 않습니다. 데이터 손상 위험을 이해한 뒤에만 승인하세요.
- cross_check(교차검증)는 **서버 설정을 바꾸지 않고 승인을 통제·기록**할 뿐 100% 신뢰할 수 있는 장치가 아닙니다(모두 root 로 쓰므로 의도적 우회는 막지 못하고, 호출 스크립트 이름은 위조할 수 있으며, `승인/작업완료` 는 스크립트가 끝까지 실행됐다는 뜻일 뿐 모든 VM 의 성공을 뜻하지 않습니다). 보조 장치로 쓰고, **전원 작업 뒤에는 반드시 무작위 몇 대의 전원 상태·부팅을 직접 확인**하세요.
- Ctrl+C 로 중단해도 **이미 vCenter 에 요청한 작업은 vCenter 에서 계속 진행**될 수 있습니다. 중단 후 전원 상태를 `-n` 으로 다시 확인하세요.
- Power Off·일괄 Power On·재부팅은 대상 전체에 명령을 한꺼번에 보냅니다. lpage 사용중 BM 에서 한꺼번에 켜면 메모리 할당 실패가 날 수 있으므로(계획표에 경고 표시) 그런 BM 은 2번(ev 단계별 순차)을 쓰세요.
- vCenter 비밀번호는 환경변수(`VC_PASSWORD`/`VC_PASS`)에서 읽지 않습니다. `secret_lib` 암호 파일 또는 직접 입력만 사용하며, conf·소스·로그에 비밀번호를 적지 마세요.
- `pwonoff.conf` 는 root 소유, group/other 쓰기 금지여야 동작합니다(허용 경로·user 우회 방지).

## 5. 전역 명령어로 사용하기 (선택 사항)

빌드된 실행 파일을 PATH에 포함된 디렉터리로 복사하거나, 실행 파일이 있는 경로를 PATH에 추가하면 어디서든 명령어처럼 쓸 수 있습니다.

```bash
sudo cp /home/power_onoff/vm_power_onoff /usr/local/bin/
```

(`sudo bash install.sh` 가 같은 일을 하면서 설정 파일까지 설치합니다. 전역 명령으로 등록해도 **`allowed_dir` 에서만 실행**됩니다.)
