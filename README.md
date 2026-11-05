# stress-test — HPC 부하테스트 자동화 도구 (AWX + gossh)

HPC 서버 대상 부하테스트(`a) cpu`, `b) stress-ng`)를 AWX 로 실행하고, 대상 서버의 1분 로드(`/proc/loadavg`)를 `gossh` 로 감시해 **실제로 부하가 걸렸는지**(실행/미실행)를 판정하는 Go 기반 도구입니다. 대상 호스트를 gossh 로 접속체크하며 CPU 벤더(Intel/AMD)를 구분하고, AWX 01 스크립트와 같은 방식(yaml → scp → `custom_inventory.sh` → 새 yml → git 업로드 → 인벤토리 소스 동기화)으로 인벤토리를 등록한 뒤 job template 을 launch 합니다. 감시 화면은 `top` 처럼 갱신되고, 끝나면 호스트별 결과표를 `|` 구분으로 출력·저장합니다. Go 바이너리 + bash 래퍼(`stress_run.sh`) 한 쌍이며, 표준 라이브러리만 사용하므로 `vendor/` 가 필요 없습니다.

> 폴더·파일별 역할과 실행 흐름은 [ARCHITECTURE.md](ARCHITECTURE.md), 변경 시 점검 목록은 [PR_CHECKLIST.md](PR_CHECKLIST.md), 설계 결정은 [계획서.md](계획서.md)를 참고하세요.

⚠️ **주의사항 (Disclaimer)**
본 로그 분석 관련 스크립트 및 툴은 100% 신뢰하기보다는 참고용(보조 도구)으로 사용하는 것을 권장합니다. 이 도구는 AWX 템플릿을 실행해 **대상 서버에 실제 부하를 걸고**, 인벤토리 소스를 변경하는 설정 변경 작업을 포함합니다. `실행`/`미실행` 판정은 1분 로드 값(기준 `load_threshold`)에 기댄 추정이므로, 실행 후 무작위로 몇 대를 골라 직접 접속해 `uptime`/`top` 및 stress-ng·부하 프로세스가 실제로 떠 있는지 확인하는 절차를 권장합니다. 스크립트가 보여 주는 성공 출력만 믿지 마세요.

## 1. 빌드 및 설치 방법

### 1.1 폐쇄망(회사) 환경 — 빌드 불필요

회사 환경에는 Go 가 없으므로 **빌드 완료된 바이너리(`stress-test`)를 저장소에 함께 커밋**해 두었습니다. 이 바이너리는 Rocky 8(os8) 호스트에서 `CGO_ENABLED=0` 정적 빌드한 linux/amd64 파일입니다(os6 빌드는 하지 않습니다). 폴더를 통째로 내려받아 옮기면 바로 실행됩니다. `vendor/` 는 없으며 표준 라이브러리만 사용합니다.

```bash
# 인터넷이 되는 환경에서 저장소 다운로드 후 압축
tar czf stress-test.tar.gz ".claude/HPC/stress-test"

# 폐쇄망 서버에서
tar xzf stress-test.tar.gz
cd ".claude/HPC/stress-test"
chmod +x stress_run.sh build.sh stress-test   # 실행 권한이 빠졌을 때만
```

필요 조건

- `gossh` — 실행 서버에 설치(PATH, `stress_run.sh` 의 `GOSSH`, 또는 conf 의 `gossh` 키로 지정)
- 실행 서버에서 `repohost`(custom_inventory.sh 가 있는 서버)로 **비밀번호 없이** `ssh`/`scp` 가능해야 함 (`BatchMode=yes` 로 호출)
- AWX 접속 정보와 job template(`cpu-intel`, `cpu-amd`, `stress-ng` 에 해당하는 것) — 설정 파일에 기입
- `git_dir` 을 쓰면 그 폴더에 `.git_upload.sh` 가 있어야 함 (AWX 01 스크립트의 git_upload 와 동일)

### 1.2 소스를 고쳐서 다시 빌드할 때 (Go 가 있는 호스트)

```bash
bash build.sh      # CGO_ENABLED=0 go build -o stress-test .
```

- `go.mod` 는 `go 1.20` 기준입니다. 그보다 새로운 문법·표준 라이브러리 API 를 쓰지 마세요.
- **소스를 바꿨다면 바이너리를 os8(랩 .58) 에서 재빌드해 함께 커밋**해야 합니다(회사에 Go 가 없으므로). `.gitignore` 로 `stress-test` 를 제외하지 마세요.
- `stress_run.sh` 는 `stress-test` 바이너리가 없으면 `build.sh` 로 자동 빌드를 시도합니다(Go 가 있는 환경 한정).

### 1.3 설정 파일 준비

`conf/sample_setting.conf` 를 `conf/${user}_setting.conf` 로 복사해 값을 채웁니다(예: user 가 `hong` 이면 `conf/hong_setting.conf`). 키 설명은 3.3 을 참고하세요.

## 2. 사용 방법

### 2.1 대화형 (사람이 실행)

```bash
bash stress_run.sh user.txt                # 입력 파일 = user.txt
bash stress_run.sh user.txt -retry 2       # 뒤 인자는 stress-test 에 그대로 전달
```

`stress_run.sh` 상단의 변수를 먼저 확인합니다.

```bash
GOSSH=""        # gossh 경로 (폴더 또는 파일). 비우면 설정 파일 gossh 키 → PATH 순
USER_ROUTE=""   # user 선택 스크립트 폴더 (info_mn.sh, info.sh). 비우면 STRESS_USER → $USER
```

진행 순서

1. **user 선택** — `USER_ROUTE` 가 있으면 AWX 01 스크립트처럼 `bash $USER_ROUTE/info_mn.sh` 메뉴를 보여 주고 `Input Number:` 로 번호를 받아 `bash $USER_ROUTE/info.sh <번호>` 출력을 user 로 사용합니다(빈 값이면 오류 종료, 종료코드 2). `USER_ROUTE` 가 비어 있으면 `STRESS_USER` 환경변수 → `$USER` 순입니다.
2. **단계 선택** — 아래처럼 묻고, 입력한 **글자 순서대로** 실행합니다. `a`, `b`, `ab`, `ba` 만 허용(같은 글자 중복·다른 글자는 다시 묻기).
   ```
   a) cpu
   b) stress
   어떤 부하테스트를 실행할지 고르세요: ba
   ```
3. 바이너리 실행 — `stress-test -user <선택값> -hosts user.txt -steps ba <추가 인자>`.

실행 중에는 gossh 접속체크 → (단계별) 등록 → launch → 감시 화면이 이어지고, 미실행 호스트가 남으면 `미실행 N대를 재실행할까요? (y/N):` 를 묻습니다. 마지막에 결과표가 출력됩니다.

### 2.2 비대화형 (setup_check 등이 Go 에서 직접 호출)

```bash
stress-test -user <u> -hosts <파일> -steps <ab> [-retry N] [-er <장애목록>] [-conf <설정파일>] -batch
```

- `-batch` 이면 어떤 프롬프트도 띄우지 않고 stdin 도 쓰지 않습니다. 터미널이 아닌 환경(파이프·cron·다른 프로그램이 호출)에서는 `-batch` 가 없어도 자동으로 비대화형이 됩니다.
- 비대화형 감시는 화면 갱신 대신 회차마다 한 줄 요약을 출력하고, 재실행은 `-retry N` 횟수만큼 자동으로 합니다.
- 이번 작업에서 setup_check 코드는 수정하지 않았습니다(호출 규약만 맞춰 둠).

### 2.3 입력 파일 형식 (`user.txt`)

줄 단위로 형식을 판정하므로 한 파일에 두 형식을 섞어도 됩니다. 빈 줄과 `#` 시작 줄은 무시하고, 같은 hostname 은(대소문자 무시) **첫 줄만** 씁니다.

| 필드 수 | 형식 | 처리 |
|---|---|---|
| 1개 | hostname 한 줄 | `tmp tmp tmp <host> <IP> <MAC> eth0 sda sda5 100 8.10 uefi` 로 변환 |
| 12개 | AWX 설치양식 레코드 | hostname = **4번째 필드**. 12필드 그대로 yaml 에 기록 |
| 13개 | 설치양식 + 13번째 분류 필드 | hostname = 4번째 필드. yaml 에는 **앞 12필드만** 기록 |
| 그 외 | 오류 | 줄 번호와 함께 모아서 출력하고 종료코드 2 |

hostname 한 줄 양식의 변환 규칙

- **IP**: DNS 조회(`net.LookupHost`)로 얻은 **첫 번째 IPv4**. 조회에 실패하면 그 호스트는 접속체크·실행에서 제외하고 결과에 `DNS실패` 로 표시합니다.
- **MAC**: `02:00:00:xx:xx:xx` 대역에서 **입력 파일 줄 번호**를 순번으로 씁니다(호스트마다 다름). 예) 3번째 줄 → `02:00:00:00:00:03`.

입력 예시

```
# hostname 한 줄
node0001
node0002
# 설치양식 12필드 (4번째가 hostname)
a b c node0003 10.0.0.3 aa:bb:cc:dd:ee:03 eth0 sda sda5 100 8.10 uefi
```

### 2.4 실행 흐름 요약

1. **접속체크 + 벤더 확인** — DNS실패를 뺀 호스트에 `gossh -script 'grep -m1 vendor_id /proc/cpuinfo'` 실행. `GenuineIntel`→`Intel`, `AuthenticAMD`→`AMD`, 그 외 응답→`unknown`(cpu 단계에서 제외, 결과 `-`), 무응답·gossh 접속불가 결과 파일(`_res_off`/`_res_refsed`/`_res_cancel`)→`접속불가`(전 단계 제외). uptime 대신 이 명령으로 접속 여부를 판정합니다.
2. **단계 실행** — 선택한 순서대로, **앞 단계의 감시가 끝난 뒤** 다음 단계를 시작합니다.
   - `b) stress`: 대상 전체 → yaml 1개 → 등록 → stress-ng 템플릿 launch.
   - `a) cpu`: Intel 묶음 yaml → 등록 → Intel 템플릿 launch, 이어서 AMD 묶음 yaml → 등록 → AMD 템플릿 launch(대상이 없는 벤더는 건너뜀). 감시는 둘 다 launch 한 뒤 **한 번**만 합니다.
3. **등록**(묶음마다, AWX 01 스크립트 방식, 인벤토리 삭제는 하지 않음)
   ```
   yaml 생성(12필드) → scp <yaml> <repohost>:<repo_dir>/
   → ssh <repohost> "bash <repo_dir>/custom_inventory.sh <repo_dir>/<yaml>"
   → <svr_dir> 에 새로 생긴(또는 수정 시각이 바뀐) *.yml 정확히 1개 확인
   → (git_dir 설정 시) yml 을 git_dir 로 복사 후 그 안에서 bash .git_upload.sh <user> <yml>
   → invsync: 인벤토리 소스의 source_field(기본 source_path)에 yml 이름 PATCH → 소스 update → 완료까지 대기
   ```
   - yaml 이름: `${user}_stress.yaml`, `${user}_intel.yaml`, `${user}_amd.yaml`. 재실행은 `${user}_retryN_<stress|intel|amd>.yaml`.
   - 새 yml 이름이 `_<N>ea.yml` 형식이면 N 이 입력 대수와 다를 때 경고만 출력합니다.
   - 새 yml 이 0개 또는 2개 이상이면 그 묶음은 `실패` 입니다.
   - `git_dir` 의 `${user}` 는 user 로 치환됩니다. 비우면 git 업로드를 건너뜁니다.
4. **템플릿 launch** — 묶음의 hostname 들을 **줄바꿈으로 이어** conf 의 `<tmpl>_hostname_key` survey 변수 하나로 넘기고, `<tmpl>_extra_vars`(`k=v, k2=v2`)를 함께 보냅니다. 템플릿은 ID(숫자) 또는 이름으로 지정합니다. job 은 부하를 백그라운드로 띄우고 바로 끝나므로 `successful` 이 될 때까지 `poll_interval` 간격으로 조회합니다. job 이 실패하면 stdout 마지막 20줄을 보여 주고 그 묶음 전체를 `실패`(비고 `... 실패(job N)`)로 표시한 뒤 감시에서 제외합니다.
5. **감시** — job 완료 직후 시작. 최대 `monitor_sec` 초 동안 `monitor_interval` 초마다(첫 확인은 launch 후 interval 뒤) gossh 로 `cat /proc/loadavg` 를 실행해 **1분 로드 < `load_threshold`** 인 호스트 목록을 `top` 처럼 같은 화면에서 갱신합니다(ANSI 화면 지움, 프롬프트가 밀려 내려가지 않음).
   - 화면 열: `hostname | CPU벤더 | load | 비고`. 응답이 없는 호스트는 load 열에 `응답없음` 으로 남깁니다.
   - 로드가 기준 이상이 된 호스트는 목록에서 빠지고 `실행` 으로 확정됩니다. **목록이 비면 `monitor_sec` 가 안 됐어도 즉시 완료**합니다(조기 종료).
   - 감시 종료(시간 만료) 시 목록에 남은 호스트(로드 < 기준) = `미실행`.
   - 비대화형(`-batch` 또는 터미널 아님)은 화면 갱신 대신 회차마다 한 줄 요약을 출력합니다.
6. **재실행** — 대화형은 `미실행 N대를 재실행할까요? (y/N):`. `y` 면 남은 호스트만으로 새 yaml → 등록 → 같은 템플릿 launch → 감시(cpu 단계는 벤더별로 다시 나눔)를 반복하고 `미실행` 이 남는 동안 계속 묻습니다. 비대화형은 `-retry N`(기본 0) 횟수만큼 자동 재실행합니다. `접속불가`·`DNS실패`·`실패`(job 실패) 호스트는 재실행 대상이 아닙니다.
7. **결과** — 결과표를 화면에 출력하고 파일로 저장합니다. 마지막 줄에 결과 파일 경로를 안내합니다.

### 2.5 장애 목록 `error_list.txt` (`-er`)

줄마다 `hostname 장애사유` 형식입니다. `-er <파일>` 로 지정하고, 생략하면 **현재 경로의 `error_list.txt`** 가 있으면 자동으로 씁니다(없으면 무시). 지정한 파일이 없으면 종료코드 2. 규칙은 setup_check `-er` 와 같습니다.

- 첫 공백(스페이스/탭) 앞 = 호스트(대소문자 무시, 정확 일치), 뒤 전체 = 사유
- 같은 호스트가 여러 줄이면 `, ` 로 이어 붙임
- 사유 안의 `|` 는 `/` 로 바꿈 (결과표 구분자와 충돌 방지)
- 빈 줄, `#` 주석, 사유 없는 줄은 무시
- **장애 호스트도 실행 대상**입니다. 감시 화면과 결과표의 마지막 `비고` 열에 사유가 붙습니다.

### 2.6 결과표

화면 출력과 `<output_dir>/${user}_stress_result.txt` 저장 내용이 같습니다. 열은 `|` 로 구분하고 **첫 줄은 제목 줄**, 이후 **입력 파일 순서**대로 호스트당 한 줄입니다.

```
hostname|CPU벤더|cpu|stress-ng|비고
node0001|Intel|실행|실행|
node0002|AMD|미실행|실행|메모리 불량
node0003|-|접속불가|접속불가|전원 케이블 불량
node0004|-|DNS실패|DNS실패|
node0005|unknown|-|실행|
```

(위 값은 예시입니다. 선택하지 않은 단계 열은 `-` 입니다.)

| 열 | 값 |
|---|---|
| hostname | 입력 파일의 hostname |
| CPU벤더 | `Intel` / `AMD` / `unknown` / `-`(접속불가·DNS실패) |
| cpu, stress-ng | `실행` / `미실행` / `실패` / `접속불가` / `DNS실패` / `-` |
| 비고 | `error_list.txt` 사유 + 실행 중 생긴 사유(예: `cpu(intel) 등록 실패`). 없으면 빈칸 |

단계 값의 의미

| 값 | 의미 |
|---|---|
| `실행` | 감시 중 1분 로드가 `load_threshold` 이상이 됨 |
| `미실행` | 감시 시간 안에 로드가 기준 미만으로 남음(재실행을 거절했거나 횟수 소진 포함) |
| `실패` | 등록·템플릿 조회·launch·job 중 하나가 실패(비고에 사유) |
| `접속불가` | 접속체크에서 응답 없음 |
| `DNS실패` | hostname 한 줄 양식에서 IP 조회 실패 |
| `-` | 선택하지 않은 단계, 또는 대상이 아님(`unknown` 벤더의 cpu 단계) |

열 수 검증: `awk -F'|' 'NF!=5' <결과파일>` 결과가 비어 있어야 합니다.

### 2.7 종료코드

| 코드 | 의미 |
|---|---|
| 0 | `미실행`/`실패` 없음 (`접속불가`·`DNS실패` 만 있어도 0) |
| 1 | `미실행` 또는 `실패` 가 하나라도 있음 |
| 2 | 설정·입력·인자 오류 (설정 파일 없음/키 누락, 입력 파일 형식 오류, gossh 없음, 접속체크 gossh 실행 실패 등) |

## 3. 옵션 및 설정 상세 설명

### 3.1 `stress_run.sh`

| 항목 | 설명 |
|---|---|
| `<입력파일>` | 필수 첫 인자. 없거나 파일이 없으면 사용법 출력 후 종료코드 2 |
| 두 번째 이후 인자 | `stress-test` 에 그대로 전달 (`-retry N`, `-er 파일`, `-conf 파일` 등) |
| `GOSSH` | gossh 경로(폴더 또는 파일). 환경변수 `GOSSH` 로 Go 에 전달 |
| `USER_ROUTE` | user 선택 스크립트 폴더(`info_mn.sh`, `info.sh`) |

### 3.2 `stress-test` 옵션

| 옵션 | 설명 |
|---|---|
| `-user <u>` | 사용자 식별자. 생략 시 `STRESS_USER` 환경변수 → `$USER`. 영문/숫자/`.`/`_`/`-` 만 허용 |
| `-hosts <파일>` | **필수.** 입력 파일 |
| `-steps <ab>` | **필수.** `a`=cpu, `b`=stress. 적은 순서대로 실행(`a`,`b`,`ab`,`ba`). 같은 글자 중복·다른 글자는 오류 |
| `-retry N` | 비대화형에서 `미실행` 호스트 자동 재실행 횟수 (기본 0). 대화형에서는 y/N 으로 묻기 때문에 무시 |
| `-er <파일>` | 장애 목록 (2.5). 생략 시 현재 경로 `error_list.txt` |
| `-conf <파일>` | 설정 파일 직접 지정. 생략 시 `./conf` → `~/.stress-test` → `<바이너리 위치>/conf` 의 `${user}_setting.conf` 를 순서대로 탐색 |
| `-batch` | 비대화형 실행 (프롬프트 없음, 감시는 한 줄 요약) |

대화형 판정: `-batch` 가 없고 stdin·stdout 이 모두 터미널일 때만 대화형입니다. gossh 는 conf `gossh` 키 → 환경변수 `GOSSH` → PATH 순으로 찾고, 폴더를 주면 그 안의 `gossh` 를 씁니다(`~/` 확장, 양끝 따옴표 제거).

### 3.3 설정 파일 키 (`conf/${user}_setting.conf`)

`key = value` 형식, `#` 이후는 주석(값 안에 `#` 사용 불가). 모르는 키는 무시하고, **값이 빈 키는 기본값을 유지**합니다. 필수 키가 비어 있으면 종료코드 2 입니다.

| 키 | 기본값 | 의미 |
|---|---|---|
| `awx_url` | (필수) | AWX 주소 |
| `username` / `password` | (필수) | AWX Basic 인증 계정 |
| `insecure_tls` | false | `true`/`yes`/`y`/`1`/`on` 이면 https 인증서 검증 생략 |
| `gossh` | (빈 값) | gossh 경로(폴더 또는 파일). 비우면 환경변수 `GOSSH` → PATH |
| `repohost` | (필수) | `custom_inventory.sh` 가 있는 서버(ssh/scp 대상, 비밀번호 없이 접속) |
| `repo_dir` | `/root/Inventory` | repohost 의 yaml 업로드·`custom_inventory.sh` 폴더 |
| `svr_dir` | (필수) | **이 서버**에서 보이는, 새 yml 이 생기는 폴더 |
| `git_dir` | (빈 값) | 01 의 git_upload 폴더(`${user}` 치환). 비우면 git 업로드 생략 |
| `inventory_source` | (빈 값) | 인벤토리 소스 ID 고정. 비우면 `inventory` 의 첫 소스(ID 가장 작은 것) |
| `inventory` | (빈 값) | 인벤토리 ID. `inventory_source` 가 비었을 때 필수 |
| `source_field` | `source_path` | yml 이름을 저장할 인벤토리 소스 필드명 |
| `cpu_intel_template` / `cpu_amd_template` | (빈 값) | cpu 단계 Intel/AMD 템플릿 ID 또는 이름. a 선택 시 둘 중 하나는 필수, 실제 대상이 있는 벤더의 것은 반드시 필요 |
| `cpu_intel_hostname_key` / `cpu_amd_hostname_key` | (빈 값) | hostname 목록을 넘길 survey 변수명(템플릿을 쓰면 필수) |
| `cpu_intel_extra_vars` / `cpu_amd_extra_vars` | (빈 값) | 추가 survey 항목 `k=v, k2=v2` |
| `stress_template` | (빈 값) | stress-ng 템플릿. b 선택 시 필수 |
| `stress_hostname_key` | (빈 값) | hostname 변수명. b 선택 시 필수 |
| `stress_extra_vars` | (빈 값) | 추가 survey 항목 |
| `monitor_sec` | 120 | job 완료 후 최대 감시 시간(초). 1 이상 정수 |
| `monitor_interval` | 10 | 1분 로드 확인 간격(초). 1 이상 정수 |
| `load_threshold` | 3 | 1분 로드가 이 값 **이상**이면 부하 실행으로 판정. 0 초과 숫자 |
| `output_dir` | `./output` | 결과(`${user}_stress_result.txt`)·gossh 로그(`${user}_stress_gossh.log`) 폴더 |
| `poll_interval` | 3 | AWX job/동기화 상태 조회 간격(초). 1 이상 정수 |
| `history_file` | `./stress-test_history.log` | 실행 이력(시작/launch/감시/종료) 파일 |

### 3.4 생성 파일

| 파일 | 내용 |
|---|---|
| `<output_dir>/${user}_stress_result.txt` | 결과표 (2.6) |
| `<output_dir>/${user}_stress_gossh.log` | gossh 모든 호출의 stdout/stderr |
| `<output_dir>/.stress_work_*` | 임시 작업 폴더(yaml, 호스트 파일). 종료 시 삭제 |
| `history_file` | 실행 이력 |

## 4. 문서별 설명

| 파일 | 설명 |
|---|---|
| `main.go` | 인자 처리, 접속체크, 단계 실행·재실행 흐름, 결과표·종료코드 |
| `config.go` | conf 파서, 필수 키 검증, user 결정, conf 탐색, 이력 파일 |
| `input.go` | 입력 파일·error_list.txt·`-steps` 파싱, 결과 값 상수 |
| `gossh.go` | gossh 경로 결정·실행, 벤더·로드 출력 파싱 |
| `register.go` | yaml 생성 → scp → custom_inventory.sh → 새 yml 확인 → git 업로드 → invsync |
| `awx.go` | AWX REST 클라이언트(템플릿 조회, launch, job poll, 인벤토리 소스 PATCH+update) |
| `monitor.go` | 1분 로드 감시와 `top` 식 화면 |
| `*_test.go` | 단위 테스트(`awx_test.go`, `logic_test.go`) |
| `stress_run.sh` | 대화형 래퍼 (user 선택, 단계 선택, 바이너리 실행) |
| `build.sh` | 바이너리 빌드 |
| `stress-test` | 빌드 완료 바이너리 (os8, 폐쇄망 배포용, 커밋 대상) |
| `conf/sample_setting.conf` | 설정 파일 샘플 |
| `test/` | 랩 e2e 스크립트와 대역: `fake_awx/`(가짜 AWX 서버), `fake_repo/`(가짜 custom_inventory.sh·.git_upload.sh), `fake_user/`(user 선택 대역), `e2e.sh`, `e2e_c.sh`, 샘플 호스트·설정·error_list |
| `ARCHITECTURE.md` | 파일 역할과 흐름 |
| `PR_CHECKLIST.md` | 변경 시 점검 목록 |
| `.github/workflows/ci.yml` | gofmt·vet·test·bash -n·shellcheck CI |
| `계획서.md` | 인터뷰로 확정한 설계 사양 |

### 4.1 테스트 실행

```bash
go test ./...        # 단위 테스트 (fake AWX httptest, 가짜 ssh/scp, 파서, 감시 로직)
go vet ./...
bash test/e2e.sh     # 랩(.58)에서: 가짜 AWX(127.0.0.1:18080) + 실제 gossh 로 .58/.60 대상 시나리오 A/B/D/E
bash test/e2e_c.sh   # 랩에서: TTY 대화형(화면 갱신, 재실행 y/N)
```

- `test/e2e.sh` 는 `/root/work/stress-test` 에 바이너리가 있고 `test/` 가 그 아래 있는 상태를 전제로 하며, 가짜 repohost 는 랩 자신(`root@192.168.0.58`)입니다. 대상 서버에 `while :; do :; done` 으로 부하를 직접 올려 감시를 확인하고 끝나면 정리합니다.
- 시나리오: A(정상 `ba`), B(미실행 → 자동 재실행 / `-retry 0` 종료코드 1), D(템플릿 job 실패), E(인자·설정·입력 오류 종료코드 2).

## 5. 제약 및 한계 / 미검증 항목

- **실제 AWX 템플릿 미검증** — 가짜 AWX(Go httptest)로만 검증했습니다. 실제 템플릿의 survey 변수명·필수 항목(`*_hostname_key`, `*_extra_vars`)은 현장에서 맞춰야 합니다.
- **실제 `custom_inventory.sh` 의 yml 이름 규칙 미검증** — 가짜 스크립트 기준입니다. 새 yml 이 `svr_dir` 에 정확히 1개 생긴다고 가정합니다(이름이 같아도 수정 시각이 바뀌면 새 것으로 봄).
- **AMD 장비 미검증** — Intel/AMD 구분은 `vendor_id` 문자열로만 하며 AMD 실장비 응답은 확인하지 못했습니다.
- **부하 시작 시점** — 실제 템플릿이 job 종료 후 얼마 만에 로드를 올리는지 알 수 없습니다. 감시는 job 완료 직후 시작해 `monitor_sec`(기본 120초) 동안 보므로, 로드가 늦게 오르면 `미실행` 으로 보일 수 있습니다(재실행 또는 `monitor_sec` 조정).
- 판정 기준은 **1분 로드**뿐입니다. 1분 로드는 서서히 오르므로 `load_threshold`·`monitor_sec` 를 장비에 맞게 조정하세요.
- 접속체크는 gossh 접속 성공 여부만 보며, 접속 후 stress-ng 설치 여부 등은 확인하지 않습니다.
- 여러 사용자가 같은 인벤토리 소스를 동시에 갱신하는 경우는 고려하지 않았습니다(소스 PATCH 는 마지막 값이 남음).
- 범위 밖: 부하 종료·정리, 인벤토리 삭제, os6 빌드.
