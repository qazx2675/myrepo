# auto_setup — OS 설치 → OS 설정체크 자동 연계

OS 설치(01) 완료 후 자동으로 OS 설정을 검사하고 결과를 공유하는 데몬 + CLI 도구.

## 빌드 및 설치 방법

### 의존성

| 항목 | 요구사항 | 설명 |
|---|---|---|
| 운영체제 | RHEL 8+ (Go 1.20+) | bash, go, gossh, ssh |
| Go | 1.20 이상 | 표준 라이브러리만 (외부 의존성 0) |
| 폐쇄망 | vendor 불필요 | 오프라인 빌드 가능 |
| 권한 | root | raw ICMP, cron 등록, /usr/local/bin |

### 파일 배치

```
auto_setup/
├── main.go / daemon.go / state.go / ... (Go 소스)
├── setup.sh              ← 빌드 + 설치 + cron + tmpfiles 등록
├── build_os6.sh          ← os6 용 정적 빌드 (Go 1.20 서버에서)
├── test/
│   ├── manual_check.sh   ← 대화형 단계별 점검
│   ├── run_e2e.sh        ← 목업 E2E 테스트
│   └── *.sh              ← 스텁 (gossh, ssh, wall)
├── 사용법.txt
└── 문서 (README.md / CHANGELOG.md / ARCHITECTURE.md / WORKFLOW.md / workflow.svg / PR_CHECKLIST.md)
```

### 최상단 빈 변수 4개

`main.go` 최상단의 다음 변수들은 **항상 빈 문자열로 둡니다**(저장소 커밋 시). 운영 환경에서는 값을 채우고 빌드합니다.

```go
var (
	os6_mgmt      = ""  // 모든 대역 접근 가능한 서버 (비우면 os6 경로 미사용)
	os6_gossh     = ""  // os6_mgmt 위 gossh 실행 파일 전체 경로
	os6_autosetup = ""  // os6_mgmt 위 auto_setup(os6 빌드) 이 있는 디렉터리
	os_check_sh   = ""  // 이 서버의 os_check_final_annotated.sh 전체 경로
)
```

**주입 방법** (소스 수정 없이):
```bash
go build -ldflags "\
  -X main.os_check_sh=/절대경로/os_check_final_annotated.sh \
  -X main.os6_mgmt=os6_mgmt_호스트 \
  -X main.os6_gossh=/remote/gossh \
  -X main.os6_autosetup=/remote/auto_setup_dir" \
  -o auto_setup .
```

### 빌드 및 설치 (폐쇄망)

폐쇄망 환경에서는 외부 의존성이 없으므로 소스만 복사하면 됩니다.

```bash
# 1. 소스 복사 및 이동
cp -r auto_setup /root/로컬경로/
cd /root/로컬경로/auto_setup

# 2. 빌드만 (테스트용)
go build -o auto_setup .

# 3. 빌드 + /usr/local/bin 설치 + cron(매분 ensure) + tmpfiles 등록 (root, 멱등)
bash setup.sh

# 4. os6 용 빌드 (Go 1.20 서버 .60 에서, 정적 바이너리)
bash build_os6.sh
# → auto_setup_os6 생성 (os6_mgmt 의 os6_autosetup 경로에 'auto_setup' 이름으로 배치)
```

### setup.sh 수행 내용

1. `go build -o auto_setup .` (main.go 포함 전체 빌드)
2. `install -m 0755 auto_setup /usr/local/bin/auto_setup`
3. crontab 에 `* * * * * /usr/local/bin/auto_setup ensure >/dev/null 2>&1` 등록 (중복 방지)
4. `/etc/tmpfiles.d/auto_setup.conf` 에 `x /tmp/auto_setup` 등록 (무기한 감시 작업이 tmp 정리로 지워지지 않도록)

## 사용 방법

### 01 과의 연계 흐름

```
01.AWX_nodeinfo_V2.sh 실행
    ↓ (02 성공)
/tmp/auto_setup/queue/<epoch>_<user>_<pid>.job 생성 (프롬프트 없음)
    ↓
데몬(cron 매분 ensure)이 queue 수거 → jobs/<jobid>.json
    ↓
자동 감시: ping 상태 판별 → 경로(local/os6) 결정 → 무응답 감지(설치 시작) → 준비확인(7분/3분)
    ↓
os_check -auto <user> <목록> 자동 실행 (동시 1개)
    ↓
wall + codes/<code>.txt 생성 (호스트 현황, 4자리 code)
```

### 명령어

| 명령 | 동작 |
|---|---|
| `auto_setup daemon` | 포그라운드 데몬 루프 (보통 ensure 가 백그라운드로 기동) |
| `auto_setup ensure` | 데몬이 없으면 백그라운드 기동 (cron 매분 실행) |
| `auto_setup status` | 작업별 진행 상태 (jobid, user, 호스트 상태별 대수, 남은 호스트 가로) |
| `auto_setup code NNNN` | code 에 해당하는 결과 코멘트 출력 (wall 에 나온 4자리) |
| `auto_setup cancel <jobid>` | 무기한 감시 중인 작업 종료 (status 에서 확인) |
| `auto_setup probe [-i 10s]` | (os6_mgmt 쪽) stdin 호스트 목록을 받고 ping 상태 출력 (`host up\|down`) |
| 인자 없음 | 사용법 출력 후 exit 1 |

### 단계별 점검 (테스트)

#### 기본 점검 (대화형, 데이터 격리)

```bash
# 기본: 각 단계마다 Enter 를 받으며 진행 (운영 /tmp/auto_setup 은 건드리지 않음)
bash test/manual_check.sh

# 단계 명령
bash test/manual_check.sh -y          # 전 단계 연속 실행 (wall·설치 단계 제외)
bash test/manual_check.sh -s 5        # 5단계부터 재시작
bash test/manual_check.sh --install   # 9단계 설치까지 포함

# 실제 ICMP 점검 (root 필요, 응답/무응답 IP 지정)
PING_UP=192.168.0.59 PING_DOWN=192.168.0.250 bash test/manual_check.sh -s 4
```

#### 자동 테스트

```bash
# Go 단위 테스트 (가짜 시계·pinger·checker)
go test ./...

# 정적 검사
go vet ./... && gofmt -l .

# 목업 E2E (스크래치, 스텁 gossh/ssh/wall, root 필요, 약 1분)
bash test/run_e2e.sh

# E2E 스크래치 보존 (로그 확인)
KEEP_SCRATCH=1 bash test/run_e2e.sh
```

## 옵션별 상세

### 조정 가능한 상수 (main.go)

| 상수 | 기본값 | 설명 |
|---|---|---|
| `pingInterval` | 10s | ICMP 송신 주기 |
| `downMisses` | 2 | 연속 무응답 횟수 → ping X 판정 |
| `checkInterval` | 30s | 준비확인 체크 주기 |
| `bootWait` | 7분 | 첫 READY 이후 모든 호스트 대기 |
| `lateWait` | 3분 | 늦게 올라온 호스트 묶음 대기 |
| `queuePoll` | 5s | queue 디렉터리 수거 주기 |

### 환경변수

| 이름 | 기본값 | 설명 |
|---|---|---|
| `AUTO_SETUP_DIR` | `/tmp/auto_setup` | 작업 데이터 위치 (테스트만 오버라이드) |

### CLI daemon/ensure/code/status/cancel/probe

각 명령은 사용법 출력 시 `auto_setup` 만 입력하면 나옵니다.

## 문서별 설명

| 파일 | 설명 |
|---|---|
| `main.go` | 최상단 빈 변수 + CLI 분기 + 상수 정의 |
| `daemon.go` | queue 수거·경로 판별·ping 감시·7분/3분 스케줄·run 큐 (상태기계) |
| `state.go` | Job/Host 구조체, JSON 원자 저장·복원 |
| `icmp.go` | raw 소켓 ping (표준 net, 체크섬·파싱 직접) |
| `probe.go` | probe 하위명령 + os6 장기 ssh 세션 관리 |
| `check.go` | gossh -pm uptime 준비확인 (local/os6) |
| `runner.go` | os_check 실행(-auto), os6 gossh 래퍼, code 생성, wall |
| `ensure.go` | ensure 하위명령: flock + setsid + 데몬 기동 |
| `log.go` | 로그 파일 (/tmp/auto_setup/auto_setup.log) 관리 |
| `notify.go` | wall 알림 |
| `iface.go` | Pinger/Checker/Runner/Notifier/Clock 인터페이스 |
| `*_test.go` | 상태기계·스케줄·복원 등 단위 테스트 |
| `test/manual_check.sh` | 대화형 점검: 단계별 설명·실행·확인 |
| `test/run_e2e.sh` | 목업 E2E: 01→queue→데몬→os_check→code 전체 흐름 |
| `setup.sh` | 빌드·설치·cron·tmpfiles 등록 (멱등) |
| `build_os6.sh` | CGO_ENABLED=0 정적 빌드 (Go 1.20, os6_mgmt 용) |
| `README.md` | 이 문서 — 설치·사용·옵션 가이드 |
| `CHANGELOG.md` | 변경 이력 |
| `ARCHITECTURE.md` | 폴더 구조·함수 역할·런타임 디렉터리 |
| `WORKFLOW.md` | 실행 흐름도 (텍스트 mermaid) |
| `workflow.svg` | 실행 흐름도 (SVG) |
| `PR_CHECKLIST.md` | 풀리퀘스트 검증 항목 |
| `사용법.txt` | 명령어 중심 빠른 참고 (운영 담당자 용도) |
| `.github/workflows/ci.yml` | 폴더 내 CI (bash -n / shellcheck / go test) |

## 주의사항 (Disclaimer)

본 스크립트 및 도구는 **100% 신뢰하기보다는 참고용(보조 도구)으로 사용하는 것을 권장합니다.**

**설정 변경 후 반드시 다음을 확인하세요:**

1. 설정 변경 스크립트이므로, **설정 변경 후 랜덤한 서버 몇 대를 선택하여 실제로 변경되었는지 확인하세요.**
2. 데몬이 상시 실행되므로 감시 대상이 많거나 os_check 가 오래 걸리면 리소스를 모니터링하세요.
3. 무기한 감시 작업은 `auto_setup cancel` 로 종료할 때까지 계속되므로, 필요시 `jobs/done/` 을 확인해 정리하세요.

### 기존 01·os_check 사용법은 변경되지 않음

- **01.AWX_nodeinfo_V2.sh**: 프롬프트·출력·종료코드 동일, auto_setup 전달은 로그 1줄뿐
- **os_check_final_annotated.sh**: 인자 없이 실행하면 지금과 100% 동일 (`-auto` 일 때만 분기)

## 전역 명령어로 사용하기 (선택)

setup.sh 가 이미 `/usr/local/bin/auto_setup` 에 설치했으므로 PATH 에 있습니다.

```bash
# 데몬 상태 확인
auto_setup status

# 4자리 code 결과 확인
auto_setup code 4821

# 진행 중 작업 취소
auto_setup cancel 20261002-153000-user1
```

## 버전 관리 및 태그

git 태그 규칙:

```bash
# 버전 태그 지정 예 (첫 릴리스)
git tag -a auto_setup-v0.1.0 -m "OS 설치 → 설정체크 자동 연계 데몬 및 CLI"
git push origin auto_setup-v0.1.0

# 향후 버전
git tag -a auto_setup-v0.2.0 -m "..."
```

태그 규칙: `auto_setup-v<major>.<minor>.<patch>`

## 직접 테스트 방법 요약

```bash
# 1단계: 기본 검사
bash -n main.go daemon.go runner.go check.go probe.go setup.sh build_os6.sh test/*.sh
go vet ./... && gofmt -l .

# 2단계: 대화형 점검
bash test/manual_check.sh -y

# 3단계: 자동 테스트
go test ./...
bash test/run_e2e.sh

# 4단계: 랩 스모크 테스트 (.58)
bash setup.sh
auto_setup status
# 실제 ping·ssh 동작 확인
```

자세한 명령은 `사용법.txt` 참조.
