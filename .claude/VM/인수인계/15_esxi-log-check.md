# 15. esxi-log-check — ESXi 치명적 로그 수집·분석

## 한눈에 보기

| 항목 | 값 |
|---|---|
| **위험도** | 🟢 **읽기 전용.** 로그를 읽어 분석만 합니다 |
| 폴더 | `.claude/VM/esxi-log-check/` |
| 바이너리 | `esxi-log-check` |
| 하는 일 | ESXi의 치명적 로그(MCE, PSOD, APD/PDL, vSAN ESA, NVMeoF 단절 등)를 수집·분류하고 담당 영역(엔지니어/운영)을 권고 |
| 권장 진입점 | **`./run_analyzer.sh`** (메뉴 방식) |
| 특이점 | 이 도구만 **vCenter가 아니라 ESXi에 직접 접속**해서 로그를 읽습니다 |

---

## 1. 이 도구는 무엇인가

ESXi 호스트에는 `vmkernel.log`, `hostd.log` 같은 로그가 쌓입니다. 하드웨어 고장, 스토리지 단절, 커널 패닉 같은 문제가 여기 남는데, **수백 대 서버의 로그를 사람이 다 볼 수 없습니다.**

이 도구는 정규표현식 패턴(32개 카테고리)으로 로그를 자동 분류하고, **각 문제가 하드웨어 엔지니어 영역인지 서버 운영 영역인지까지 권고**합니다. 텍스트/HTML/JSON 리포트와 실시간 웹 대시보드를 제공합니다.

**검증용 모의(Mock) ESXi 환경**도 함께 들어 있어서, 실제 장애 없이 분석기가 제대로 동작하는지 확인할 수 있습니다.

---

## 2. 빌드

```bash
cd .claude/VM/esxi-log-check
bash setup.sh            # 내부: go build -mod=vendor
chmod +x run_analyzer.sh esxi-log-check
```

전역 명령어로 쓰려면:

```bash
sudo cp esxi-log-check /usr/local/bin/
```

---

## 3. 사용 방법

### 3-1. 대화형 메뉴 (권장)

```bash
./run_analyzer.sh
```

메뉴가 나옵니다.

| 번호 | 메뉴 | 설명 | 동작 |
|---|---|---|---|
| 1 | 실제 ESXi 서버 분석 (IP 직접 입력) | 단일 타겟 IP를 입력해 즉시 로그를 수집하고 **텍스트**로 요약 | ✅ |
| 2 | 실제 ESXi 서버 분석 (호스트 목록 파일) | IP가 줄바꿈으로 적힌 파일(예: `real_hosts.txt`)을 받아 일괄 분석하고 **HTML 리포트** 생성 | ✅ |
| 3 | 모의 테스트 모드 | 전체 에러 패턴을 모의 주입해 분석 기능이 정상인지 확인 → HTML 리포트 | ✅ |
| 4 | 웹 서버 모드 (백그라운드) | 백그라운드 웹서버(12345 포트) 구동 | ⚠️ **아래 5절 참고 — 현재 소스에 미구현** |
| 5 | 임시 파일 및 캐시 정리 | 캐시, 임시 수집 폴더, 불필요한 `.csv` 정리 | ✅ |
| 6 | 종료 | | ✅ |

### 3-2. CLI 직접 실행 (자동화/cron 연동)

```bash
# 호스트 목록으로 HTML 리포트 생성
./esxi-log-check -w hosts.txt -format html -out report.html -onlyProblems

# 이미 수집해둔 로그 파일을 직접 지정 (gossh 없이)
./esxi-log-check -input vmkernel.log=/var/log/vmkernel.log \
                 -input vobd.log=/var/log/vobd.log \
                 -format text
```

---

## 4. 옵션 상세표

**소스(`main.go`의 `flag` 정의) 기준입니다.**

### 입력

| 플래그 | 기본값 | 설명 |
|---|---|---|
| `-w <파일>` | (없음) | **원샷 모드**: 대상 호스트(IP) 목록 파일 하나만 지정하면 내부적으로 `gossh`를 호출해 로그를 수집한 뒤 분석 |
| `-input <이름>=<경로>` | (없음) | **반복 지정 가능.** 이미 수집해둔 로그 파일을 직접 지정. 이름은 `patterns.yaml`의 소스 이름과 일치해야 매칭됨 (예: `vobd.log`, `vmkernel.log`, `ipmi_sel`) |
| `-hostlist <파일>` | (없음) | `gossh -w`에 사용한 호스트 목록 파일 (선택). **무응답 호스트 계산용** |
| `-patterns <경로>` | `esxi_critical_patterns.yaml` | 패턴 레지스트리 YAML 경로 |

> `-w`와 `-input` 중 **최소 하나는 반드시 필요**합니다. 둘 다 없으면 종료 코드 2로 중단합니다.

### 출력

| 플래그 | 기본값 | 설명 |
|---|---|---|
| `-format <형식>` | `text` | `text` / `html` / `json`. ⚠️ 도움말 문구에는 `text \| json`만 적혀 있지만 **`html`도 정상 동작**합니다 |
| `-out <파일>` | (없음 → stdout) | 결과 저장 경로. `-format=html`에서 `-out`을 안 주면 `esxi-report.html`로 저장 |
| `-onlyProblems` | `false` | text 리포트의 호스트별 요약에서 **완전히 정상인 호스트는 개별 나열하지 않음** |

### 수집·분석 동작

| 플래그 | 기본값 | 설명 |
|---|---|---|
| `-gosshPath <경로>` | `/usr/local/bin/gossh` | `-w` 모드에서 내부적으로 호출할 `gossh` 바이너리 경로 |
| `-tailLines <N>` | `300` | `-w` 모드에서 `vobd.log`/`vmkernel.log`를 각각 몇 줄 tail 할지 |
| `-silentRebootGap <기간>` | `30m` | `vmkernel.log` 타임스탬프 공백이 이 값 이상이면 **무증상 리부트**로 판단 (예: `1h`, `45m`) |
| `-noCorrelate` | `false` | 상관관계 분석 비활성화 (aggregate 격상은 항상 적용됨) |

### 모의 테스트

| 플래그 | 기본값 | 설명 |
|---|---|---|
| `-test-mock <N>` | `0` | 모의 로그를 N건 주입해 분석기를 검증. 지정하면 `-format`이 자동으로 `html`로 바뀜 |
| `-test-mock-host <IP>` | `192.168.0.58` | 모의 로그를 주입할 대상 호스트 |

---

## 5. ⚠️ 웹 서버 모드는 현재 소스에 구현되어 있지 않습니다

이 문서를 만들면서 소스와 대조한 결과 발견한 사항입니다.

| 사실 | 근거 |
|---|---|
| `esxi-log-check` 바이너리에 **`-server` 플래그가 없습니다** | `main.go`의 `flag` 정의에 없음 |
| 소스 어디에도 **HTTP 서버 코드가 없습니다** | `net/http` / `ListenAndServe` 가 `vendor/` 밖 어디에도 없음 |
| 그런데 셸 스크립트들은 `-server :12345`를 넘깁니다 | `start_server.sh`, `run_test_server.sh`, `run_analyzer.sh`(메뉴 4) |

Go의 `flag` 패키지는 정의되지 않은 플래그를 만나면 오류로 종료하므로, **이 스크립트들을 실행하면
`flag provided but not defined: -server`로 실패할 가능성이 높습니다.**

**대안**: HTML 리포트 생성은 정상 동작합니다. 웹으로 보고 싶으면 리포트를 만든 뒤 정적 파일로 서비스하세요.

```bash
./esxi-log-check -w hosts.txt -format html -out report.html
python3 -m http.server 12345      # report.html 이 있는 디렉터리에서
# 브라우저에서 http://<리눅스IP>:12345/report.html
```

**웹 서버 기능을 정말 복구해야 한다면** [31_변경요청서_양식](./31_변경요청서_양식.md)으로 요청하세요.
요청 시 "`-server <주소:포트>` 플래그를 추가하고 내장 HTTP 서버로 HTML 리포트를 서비스할 것,
기존 `start_server.sh`/`run_test_server.sh`/`run_analyzer.sh` 메뉴 4와 호환될 것"을 명시하면 됩니다.

---

## 6. 로그 패턴 분류 (32종 카테고리)

`esxi_critical_patterns.yaml`에 정의되어 있습니다. 주요 카테고리:

| 카테고리 | 내용 | 담당 영역 |
|---|---|---|
| `CPU_MCE` | MCE(Machine Check Exception), PSOD 유발 치명적 하드웨어 에러 | 엔지니어 |
| `MEMORY` | 메모리 컨트롤러 불량, Uncorrectable ECC, DIMM 폴트 | 엔지니어 |
| `STORAGE_APD_PDL` | All Paths Down, Permanent Device Loss | 서버운영/엔지니어 복합 |
| `STORAGE_LATENCY` | 스토리지 읽기/쓰기 응답 지연 | 서버운영 |
| `VSAN_ESA` | vSAN 네트워크 단절, 디스크 그룹 장애, 커밋 실패 | 엔지니어/운영 |
| `NETWORK_LINK` | NIC 링크 다운, vmnic 단절 | 서버운영 |
| `NFS_LOST` | NFS 데이터스토어 연결 유실 | 서버운영 |
| `HOSTD_CRASH` | hostd 프로세스 크래시 | 서버운영 (서비스 재시작/재부팅) |
| `KERNEL_PANIC` | 커널 패닉, 비정상 리부트 | 서버운영 |
| `HARDWARE_SENSOR` | 온도/팬/전원/배터리 등 IPMI 센서 에러 | 엔지니어 |
| `DUMP` | 코어덤프/크래시덤프 수집 에러 | 엔지니어 |

그 외 `NVM_EOF`, `VM_STUN`, `SECURE_BOOT` 등 총 32개 카테고리를 지원합니다.

> **패턴을 추가하려면** `esxi_critical_patterns.yaml`에 카테고리/정규표현식을 추가하면 됩니다. 코드 수정 없이 설정 파일만 고치면 되는 구조입니다.

---

## 7. 모의 테스트 (신뢰성 검증)

모의 테스트 도구(`esxi_mock_logger`)는 32개 카테고리의 **100여 개 세부 상황을 코드 수준에서 재현**합니다. 실제 장애 없이 분석기가 제대로 판정하는지 검증할 수 있습니다.

| 테스트 | 주입하는 로그 | 확인하는 것 |
|---|---|---|
| MCE / PSOD | `vmkernel.log`에 `@BlueScreen: Machine Check Exception` | `CRITICAL` 등급 식별 + **하드웨어 엔지니어 조치 권고** |
| 메모리 Uncorrectable | `Memory controller error` | 불량 메모리 뱅크 점검 필요 → **엔지니어 영역** 분류 |
| 커널 패닉 | `hostd.log`/시스템 로그에 커널 패닉 | **서버운영 영역(재부팅 조치)** 안내 |
| 코어 덤프 | 디스크 공간 부족 등 덤프 수집 실패 | 덤프 수집 권한 담당 → **하드웨어 엔지니어** 배정 |
| 스토리지 APD/PDL | 경로 단절(APD) / 영구 장치 손실(PDL) | 둘을 **구분해서** 리포팅 + 스토리지 스위치 조치 권고 |

`./run_analyzer.sh`의 **3번 메뉴**나 웹 인터페이스에서 원클릭으로 실행합니다.

---

## 8. 주의사항

- 🟢 읽기 전용이지만, **분석 결과를 100% 신뢰하지 마세요.** 정규표현식 기반 분류라 오탐/미탐이 있을 수 있습니다. 참고용 보조 도구로 쓰세요.
- 이 도구는 **ESXi에 직접 접속**합니다. `-w` 모드는 **`/usr/local/bin/gossh` 바이너리를 서브프로세스로 호출**하므로, 그 경로에 `gossh`가 설치되어 있어야 합니다(`-gosshPath`로 변경 가능). ESXi SSH 접근 권한과 방화벽도 필요합니다.
- `gossh`가 없으면 `-input`으로 미리 수집해둔 로그 파일을 직접 지정해 분석할 수 있습니다.
- 웹 서버 모드는 위 5절 참고 — 현재 소스에 없습니다.

---

## 9. 파일 구조

```
esxi-log-check/
├── README.md                     # 1차 자료
├── PLAN.md                       # 개발/변경 계획 메모
├── walkthrough.md                # 작업 완료 보고서 (변경 이력 정리)
├── main.go                       # CLI 진입점 (옵션 파싱, 분석 실행)
├── collect.go                    # -w 모드 로그 수집 (gossh를 서브프로세스로 호출)
├── esxi_critical_patterns.yaml   # ★ 로그 패턴 분류 규칙 ← 패턴 추가는 여기
├── setup.sh                      # 폐쇄망 빌드
├── run_analyzer.sh               # ★ 대화형 메뉴 (권장 진입점)
├── run_test_server.sh            # 모의 테스트용 웹서버
├── start_server.sh / stop_server.sh
├── internal/
│   ├── correlate/   # 로그 이벤트 간 상관관계 분석
│   ├── gossh/       # gossh 실행 결과 파싱
│   ├── match/       # 로그 패턴 매칭 엔진
│   ├── mock/        # 모의 로그 생성기 + 테스트 시나리오
│   ├── registry/    # 패턴 레지스트리 (카테고리·힌트·권고안 로딩)
│   └── report/      # 텍스트/HTML 리포트 생성
└── vendor/
```

---

## 10. 관련 문서

- 1차 자료: `esxi-log-check/README.md`
- 작업 이력: `esxi-log-check/walkthrough.md`
