# esxi-log-check (ESXi Log Check) 툴 및 모의 환경 기술문서

ESXi 서버에서 발생하는 치명적인 로그(MCE, PSOD, APD/PDL, vSAN ESA, NVMeoF 단절 등)를 수집·분석하는 도구(`esxi-log-check`)와, 이를 검증하기 위한 **ESXi 모의 환경(Mock Generator)**의 설치·빌드·사용 방법을 안내합니다.

> 수집 → 매칭 → 리포트 흐름은 [WORKFLOW.md](WORKFLOW.md), 폴더·파일별 역할은 [ARCHITECTURE.md](ARCHITECTURE.md)를 참고하세요.

⚠️ **주의사항 (Disclaimer)**
본 로그 분석 관련 스크립트 및 툴은 100% 신뢰하기보다는 참고용(보조 도구)으로 사용하는 것을 권장합니다. 설정 변경 스크립트의 경우, 설정 변경 후 무작위로 서버 몇 대를 골라 실제로 변경되었는지 직접 확인하는 절차가 반드시 필요합니다.

## 1. 빌드 및 설치 방법

이 프로젝트는 **인터넷이 차단된 폐쇄망 환경**에서도 빌드할 수 있도록 의존성 모듈(`vendor/`)을 모두 포함하고 있습니다. Go 컴파일러만 준비되어 있으면 됩니다.

1. **프로젝트 다운로드 및 이동**
   Git 등에서 저장소를 내려받아 통째로 압축한 뒤, 폐쇄망 서버(Rocky Linux 등)로 옮겨 압축을 풉니다.
2. **빌드 경로로 이동**
   ```bash
   cd ".claude/VM/esxi-log-check"
   ```
3. **바이너리 빌드**
   외부 네트워크 없이 로컬 패키지(`vendor/`)만 쓰도록 설정된 `setup.sh`로 빌드합니다.
   ```bash
   bash setup.sh
   # (내부에서 go build -mod=vendor 가 실행됩니다)
   ```
4. **실행 권한 부여**
   ```bash
   chmod +x run_analyzer.sh
   chmod +x esxi-log-check
   ```

> 원격 로그 수집(`-w` 모드)은 `gossh`를 서브프로세스로 호출합니다. 기본 경로는 `/usr/local/bin/gossh`이며, 다른 위치라면 `-gosshPath`로 지정하세요(`.claude/공통/gossh` 참고).

## 2. 사용 방법

가장 쉽고 권장하는 방법은 메뉴 기반 셸 스크립트 `run_analyzer.sh`입니다. CLI로 직접 실행할 수도 있습니다(3장 참고).

### 2.1 대화형 스크립트 실행 (권장)

```bash
./run_analyzer.sh
```

실행하면 다음 메뉴가 나타납니다.

| 번호 | 메뉴 | 설명 |
|---|---|---|
| 1 | 실제 ESXi 서버 분석 (IP 직접 입력) | 단일 대상 IP를 입력해 즉시 로그를 수집하고, 요약 결과를 텍스트로 확인합니다. |
| 2 | 실제 ESXi 서버 분석 (호스트 목록 파일) | 대상 서버 IP를 한 줄에 하나씩 적은 파일(예: `real_hosts.txt`)을 받아 일괄 분석하고 HTML 리포트를 만듭니다. |
| 3 | 모의 테스트 모드 | 모든 에러 패턴을 모의(Mock)로 주입해 분석 기능이 정상인지 확인하고 `mock_test_report.html`을 만듭니다(결함 확인용). |
| 4 | 웹 서버 모드 (백그라운드) | 대상(모의 테스트 / 실제 IP / 호스트 파일)을 골라 백그라운드 웹 서버(기본 12345 포트)를 띄우고 실시간 대시보드를 제공합니다. |
| 5 | 임시 파일 및 캐시 정리 | 분석 중 생긴 캐시, 임시 수집 폴더, 불필요한 `.csv` 파일 등을 정리합니다. |
| 6 | 종료 | 스크립트를 끝냅니다. |

### 2.2 웹 서버 대시보드로 모니터링하기

1. `./run_analyzer.sh` 실행 후 **4번(웹 서버 모드)**을 고릅니다.
2. "2) 실제 ESXi IP 직접 입력" 또는 "3) 실제 호스트 목록 파일 기반" 등으로 대상을 지정합니다.
3. 웹 브라우저에서 해당 리눅스 서버의 IP와 포트로 접속합니다(예: `http://192.168.0.58:12345`).
4. 화면에서 ESXi 서버별 위험(Critical)·경고(Warning) 로그 통계를 한눈에 확인할 수 있습니다.

## 3. 옵션별 상세 설명

대화형 스크립트(`run_analyzer.sh`) 대신 분석기 바이너리(`esxi-log-check`)를 직접 실행해 자동화 파이프라인이나 스케줄러(cron 등)에 연동할 때 쓰는 인자입니다.

| 옵션 | 설명 |
|---|---|
| `-w <파일명>` | 분석할 대상 호스트(IP) 목록 파일. 한 줄에 하나씩 적습니다. (예: `-w hosts.txt`) |
| `-format <형식>` | 결과 형식. `text`(터미널용), `html`(시각적 리포트), `json`(다른 시스템 연동용) |
| `-out <파일명>` | 결과를 저장할 파일. 생략하면 표준 출력(터미널)에 표시합니다. (예: `-out report.html`) |
| `-server <주소:포트>` | 결과를 파일로 남기지 않고 내장 HTTP 웹 서버로 제공합니다. (예: `-server :12345`) 브라우저로 접속해 리포트를 봅니다. |
| `-onlyProblems` | 이상(Finding)이 발견된 호스트만 출력합니다. 정상 호스트는 리포트에서 생략됩니다. |
| `-gosshPath <경로>` | `-w` 모드에서 호출할 gossh 바이너리 경로 (기본 `/usr/local/bin/gossh`) |
| `-tailLines <N>` | `-w` 모드에서 vobd.log/vmkernel.log를 각각 몇 줄 tail 할지 (기본 300) |
| `-hostlist <파일>` | 무응답 호스트 계산용 호스트 목록 (`-w`를 쓰면 생략 시 그 파일을 사용) |
| `-patterns <파일>` | 패턴 레지스트리 YAML 경로 (기본 `esxi_critical_patterns.yaml`) |

그 밖의 고급 옵션(`-input`, `-silentRebootGap`, `-noCorrelate` 등)은 `./esxi-log-check -h`로 확인하세요.

## 4. 문서별 고유 설명

### 4.1 디렉토리 구조

```
esxi-log-check/
├── README.md                     # 이 문서
├── WORKFLOW.md / ARCHITECTURE.md # 작업 흐름도 / 폴더별 역할
├── PR_CHECKLIST.md               # 수정·배포 전 체크리스트
├── PLAN.md                       # 개발/변경 계획 메모
├── walkthrough.md                # 작업 완료 보고서 (변경 이력 정리)
├── main.go                       # esxi-log-check CLI 진입점 (옵션 파싱, 분석 실행)
├── collect.go                    # -w 모드 전용 로그 수집 로직 (gossh를 서브프로세스로 호출)
├── esxi_critical_patterns.yaml   # 로그 패턴 분류 규칙(카테고리/정규표현식) 정의 파일
├── go.mod / go.sum               # Go 모듈 정의 파일
├── setup.sh                      # vendor 패키지로 폐쇄망에서도 빌드하는 스크립트
├── run_analyzer.sh               # 대화형 메뉴 기반 실행 스크립트 (권장 사용법)
├── run_test_server.sh            # 모의 테스트용 웹 서버 실행 스크립트
├── start_server.sh / stop_server.sh  # 웹 서버 모드 시작/중지 스크립트
├── internal/
│   ├── correlate/                # 로그 이벤트 간 상관관계 분석 로직
│   ├── gossh/                    # gossh 실행 결과 파싱
│   ├── match/                    # 로그 패턴 매칭 엔진
│   ├── mock/                     # 모의(Mock) 로그 생성기 및 테스트 시나리오
│   ├── registry/                 # 패턴 레지스트리(카테고리·힌트·권고안 로딩)
│   └── report/                   # 텍스트/HTML 리포트 생성
└── vendor/                       # 빌드에 필요한 Go 의존성 패키지 모음 (서드파티, 문서화 대상 제외)
```

### 4.2 로그 패턴 분류 (32종 카테고리)

`esxi_critical_patterns.yaml`에 정의된 주요 카테고리는 다음과 같습니다.

| 카테고리 | 내용 | 담당 |
|---|---|---|
| **CPU_MCE** | MCE(Machine Check Exception), PSOD를 일으키는 치명적인 하드웨어 에러 | 엔지니어 |
| **MEMORY** | 메모리 컨트롤러 불량, Uncorrectable ECC 에러, DIMM 폴트 | 엔지니어 |
| **STORAGE_APD_PDL** | All Paths Down, Permanent Device Loss 같은 스토리지 단절 | 서버운영/엔지니어 복합 |
| **STORAGE_LATENCY** | 스토리지 읽기/쓰기 응답 지연 | 서버운영 |
| **VSAN_ESA** | vSAN 네트워크 단절, 디스크 그룹 장애, 커밋 실패 | 엔지니어/운영 |
| **NETWORK_LINK** | NIC 링크 다운, vmnic 단절 | 서버운영 |
| **NFS_LOST** | NFS 데이터스토어 연결 유실 | 서버운영 |
| **HOSTD_CRASH** | hostd 프로세스 크래시 (보통 서비스 재시작 또는 재부팅으로 조치) | 서버운영 |
| **KERNEL_PANIC** | 커널 패닉, 비정상 재부팅 | 서버운영 |
| **HARDWARE_SENSOR** | 온도·팬·전원·배터리 등 IPMI 센서 에러 | 엔지니어 |
| **DUMP** | 코어덤프·크래시 덤프 수집 관련 에러 | 엔지니어 |

*(그 밖에 NVM_EOF, VM_STUN, SECURE_BOOT 등 모두 32개의 세분화된 정규표현식 카테고리로 문제를 분류합니다.)*

### 4.3 로그 패턴 테스트 케이스

모의 테스트 도구(`esxi_mock_logger`)는 실제 운영망에서 생길 수 있는 32개 카테고리, 100여 개 세부 상황을 코드 수준에서 재현합니다. 이를 통해 도구의 신뢰성을 미리 검증할 수 있습니다.

* **MCE / PSOD 발생 테스트**
  - `vmkernel.log`에 `@BlueScreen: Machine Check Exception` 메시지를 가상으로 주입합니다.
  - 분석기가 이를 `CRITICAL` 등급으로 식별하고 **하드웨어 엔지니어 조치 권고**로 안내하는지 확인합니다.
* **메모리 Uncorrectable 에러 테스트**
  - `Memory controller error` 메시지가 나왔을 때 불량 메모리 뱅크 점검이 필요한 **엔지니어 영역**으로 분류하는지 확인합니다.
* **커널 패닉(Kernel Panic) 테스트**
  - `hostd.log`나 시스템 로그에 커널 패닉 메시지를 주입했을 때 **서버운영 영역(재부팅 조치 필요)**으로 안내하는지 확인합니다.
* **코어 덤프(DUMP) 테스트**
  - 디스크 공간 부족 등으로 덤프 수집에 실패한 로그가 나왔을 때, 덤프 수집을 다루는 **하드웨어 엔지니어**에게 배정하는지 확인합니다.
* **스토리지(APD/PDL) 테스트**
  - 일시적 경로 단절(APD)과 영구 장치 손실(PDL)을 구분해 보고하고, 스토리지 스위치 조치(포트 확인, LUN 확인 등)를 올바르게 권고하는지 검증합니다.

위 테스트 케이스는 `./run_analyzer.sh`의 **3번 메뉴(모의 테스트 모드)**나 웹 인터페이스에서 언제든 한 번에 검증해 볼 수 있습니다.

## 5. 전역 명령어로 사용하기 (선택 사항)

빌드된 `esxi-log-check`를 매번 이 폴더로 이동하지 않고 어디서든 명령어처럼 쓰려면, PATH에 포함된 경로로 복사하면 됩니다.

```bash
# 예: /usr/local/bin 경로로 복사해 전역 명령어로 등록
sudo cp esxi-log-check /usr/local/bin/
```

이후로는 터미널 어느 경로에서나 `esxi-log-check`만 입력하면 바로 실행됩니다.
