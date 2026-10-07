# redfish — BMC BIOS 표준값 점검·설정 도구 (biostool)

관리망 BMC 에 **Redfish** 로 접속해 서버의 BIOS 설정이 표준값(프로파일)과 같은지 **점검**하고, 사람이 승인(Y)한 항목만 BIOS **Pending** 으로 **설정**하는 도구입니다. Go(실제 작업) + bash(실행 편의) 한 쌍으로 만들어져 있고, Go 는 표준 라이브러리만 씁니다.

- 대상: Dell(iDRAC), HPE(iLO), Lenovo(XCC), Cisco(CIMC), Supermicro. 점검은 5개 벤더 모두, **설정(쓰기)은 Dell·HPE·Lenovo 이고 모델을 지정한 `verified=Y` 행에만** 합니다.
- **재부팅은 하지 않습니다.** 설정은 BMC 의 Pending(다음 재부팅 때 반영)까지입니다. OS 설치 같은 정상 재부팅에서 반영됩니다.
- 수천 대를 한 번에 처리합니다(동시 접속 기본 100). 일시 오류는 자동 재시도하고, 실패 목록(`retry.txt`)만 다시 돌릴 수 있습니다.
- BMC 에 남기는 흔적을 줄이려고 호스트당 **로그인 1회**, 허용목록 밖 호출(로그 조회·ClearLog·Reset 등)은 **전송 전에 코드가 차단**합니다.
- 사전조사(`xml.sh`)로 처음 만나는 모델의 속성 이름을 확인하고, 모델별 BIOS 전체 비교(`all_bios_check.sh`)도 할 수 있습니다.

> **현재 상태(v0.1.0): 실제 BMC 에 접속해 본 적이 없습니다.** 모든 검증은 합성 데이터로 만든 mock BMC 와 `-from-dump` 로만 했습니다. 모델을 처음 만날 때의 절차와 사람이 확인할 항목은 [FIRST_RUN.md](FIRST_RUN.md) 에 모았습니다. 반드시 먼저 읽으십시오.

관련 문서: [ARCHITECTURE.md](ARCHITECTURE.md)(파일별 역할·수정 가이드) · [WORKFLOW.md](WORKFLOW.md)(작업 흐름도) · [FIRST_RUN.md](FIRST_RUN.md)(첫 실장비 절차) · [PR_CHECKLIST.md](PR_CHECKLIST.md) · [CHANGELOG.md](CHANGELOG.md) · [사용법.txt](사용법.txt)(명령만 모은 목록) · [계획서.md](계획서.md)(설계·결정 이력)

---

## 1. 빌드 및 설치 방법

### 1.1 폐쇄망에서 받기 (Go 없이 바로 사용)

저장소를 통째로 내려받으면(`git clone` 또는 웹에서 ZIP 다운로드 후 압축 해제) **빌드가 끝난 바이너리가 이미 `bin/` 에 들어 있어** Go 를 설치하지 않고 바로 쓸 수 있습니다.

```bash
git clone <저장소 주소>           # 폐쇄망이면 ZIP 을 옮겨 압축 해제
cd <저장소>/.claude/HPC/redfish
chmod +x bin/biostool bin/biostool_os6 *.sh    # 실행 권한이 없을 때 (없어도 아래 `bash x.sh` 형태로 실행 가능)
```

| 파일 | 대상 | 비고 |
|---|---|---|
| `bin/biostool` | RHEL8 등 커널 3.x 이상 | 정적 링크(CGO 없음). Go 1.25 로 빌드 |
| `bin/biostool_os6` | **RHEL6**(커널 2.6.32) | 정적 링크. **Go 1.20.x** 로 빌드(RHEL6 커널에서는 Go 1.24 이상 바이너리가 동작하지 않음). **RHEL6 에서는 빌드하지 않고 이 파일을 그대로 씁니다.** `setup.sh` 는 os8 만 만들고, RHEL6 용은 저장소에 빌드 완료 상태로 커밋되어 있습니다 |

`bios_check.sh`·`xml.sh`·`all_bios_check.sh`·`encrypt.sh` 는 `uname -r` 의 커널 메이저 버전이 3 미만이면 `bin/biostool_os6`, 아니면 `bin/biostool` 을 **자동 선택**합니다(선택한 바이너리에 실행 권한이 없으면 `chmod +x` 를 시도합니다). 바이너리가 없고 `go` 도 없으면 안내 후 종료합니다(종료코드 2). 스크립트는 RHEL6 의 bash 4.1 에서도 돌아가도록 썼습니다.

> 스크립트에 실행 권한이 없어도 **`bash bios_check.sh` 처럼 `bash` 를 앞에 붙이면** 됩니다. 이 문서의 명령은 모두 그 형태입니다.

### 1.2 소스에서 빌드 (Go 가 있는 PC·랩)

| 방법 | 설명 |
|---|---|
| `bash setup.sh` | Go 가 있는 **랩·PC 전용**. `GOPROXY=off`, 인터넷 접속 시도 없음. os8 용 `bin/biostool` 만 만듭니다. go 가 없으면 안내 후 종료코드 2. **회사 폐쇄망(Go 없음)과 RHEL6 에서는 실행하지 않습니다** |
| `bash build.sh` | os8 용 `bin/biostool` (`setup.sh` 와 같은 결과) |
| `bash build_os6.sh` | **관리자용.** 소스를 고친 뒤 RHEL6 용 `bin/biostool_os6` 를 **다시 만들 때만** 사용합니다(`/opt/go1.20/bin` 의 Go 1.20.x 필요). 일반 사용자는 실행하지 않고 커밋된 파일을 씁니다 |
| `go vet ./... && go test ./...` | 정적 분석 + 전체 테스트(약 10초). 실제 BMC 에는 접속하지 않습니다 |

```bash
bash setup.sh          # (Go 가 있는 랩·PC 에서만) os8 바이너리 bin/biostool 생성
bash build_os6.sh      # (관리자가 소스를 고쳤을 때만) RHEL6 바이너리 bin/biostool_os6 재생성, Go 1.20.x 필요
```

**vendor/ 가 없는 이유.** 이 프로젝트는 외부 의존성이 **하나도 없습니다**(`go.mod` 에 `require` 가 없고 표준 라이브러리만 사용, `module biostool`, `go 1.20`). 그래서 `go mod vendor` 로 모을 것이 없고 `vendor/` 디렉터리가 필요 없습니다. `setup.sh` 는 다른 프로젝트와 같은 틀(`GOFLAGS=-mod=vendor`, `go build -mod=vendor`)을 그대로 쓰는데, 의존성이 없으면 vendor 디렉터리가 없어도 이 옵션이 정상 동작합니다(랩의 Go 1.25.10 과 Go 1.20.14 에서 둘 다 `setup.sh` 성공을 확인했습니다). 나중에 외부 모듈을 쓰게 되면 `go mod vendor` 로 `vendor/` 를 만들어 커밋해야 폐쇄망 빌드가 유지됩니다.

**소스를 고쳤다면** 바이너리도 다시 빌드해 **두 파일(`bin/biostool`, `bin/biostool_os6`)을 함께 커밋**해야 합니다. 회사 환경에는 Go 가 없어 저장소의 바이너리가 곧 배포본입니다([PR_CHECKLIST.md](PR_CHECKLIST.md)).

### 1.3 처음 한 번 준비할 것

```bash
cp bios.conf.example bios.conf       # user= 에 BMC 계정 ID 입력 (나머지는 기본값으로 시작)
bash encrypt.sh                      # BMC 비밀번호 입력(2회) → pass.enc + key.bin 생성 (권한 600)
cp user.txt.example user.txt         # 점검할 대상을 한 줄에 하나씩
cp diff.txt.example diff.txt         # (all_bios_check.sh 를 쓸 때) 정상 설정값을 가진 기준 호스트
cp ignore_attrs.txt.example ignore_attrs.txt   # (선택) 비교에서 뺄 속성
```

- **BMC 비밀번호는 평문으로 디스크·명령행·로그에 남지 않습니다.** `encrypt.sh` 가 화면에 보이지 않게 두 번 입력받아 파이프(표준입력)로만 넘기고, AES-256-GCM 으로 암호화한 `pass.enc` 와 키 `key.bin` 으로 나눠 보관합니다. 둘은 짝이므로 함께 보관하고 **git 에 올리지 마십시오**.
- `user.txt` 한 줄에 대상 하나: `IP` / `hostname-m` / `hostname`(+ 선택적으로 `:포트`). `hostname` 은 `/etc/hosts` 에서 `hostname-m` 의 IP 를, `hostname-m` 은 그 이름의 IP 를 찾습니다(`/etc/hosts` 형식은 `IP  hostname-m`, 한 줄에 별칭이 여럿이어도 됨). 빈 줄·`#` 주석은 무시하고 중복은 한 번만 처리합니다.
- `bios.conf`, `pass.enc`, `key.bin`, `user.txt`, `diff.txt`, `ignore_attrs.txt`, `results/`, `dumps/` 는 실제 호스트 이름·계정 정보가 들어가므로 `.gitignore` 로 커밋이 막혀 있습니다(예시 파일 `*.example` 만 저장소에 있습니다).

---

## 2. 사용 방법

### 2.1 점검과 설정 — `bios_check.sh`

```bash
bash bios_check.sh                    # profiles/*.tsv 를 번호 메뉴로 선택
bash bios_check.sh --profile VM       # 메뉴 생략
```

1. 스크립트가 필요한 파일(`bios.conf`, `user.txt`, `pass.enc`, `key.bin`)이 있는지 확인하고 없으면 만드는 방법을 알려 줍니다.
2. 프로파일(표준값 파일 `profiles/<이름>.tsv`)을 고릅니다. 새 프로파일(예: BM)은 파일만 추가하면 메뉴에 나옵니다.
3. 모든 대상을 **읽기 전용**으로 점검하고 결과를 `results/<일시>/` 에 저장한 뒤 터미널에 리포트를 보여 줍니다.
4. 설정할 수 있는 FAIL 이 있으면 **`FAIL N건(호스트 M대)을 Pending 으로 설정하시겠습니까? 재부팅은 하지 않습니다. (Y/N):`** 하고 묻습니다. `Y`/`y` 만 설정하고, 그 밖의 입력·빈 입력·입력 끝은 모두 N 입니다.

> **Y/N 은 사람이 터미널에서 직접 입력할 때만 받습니다.** 표준입력이 파이프·파일이면(`yes | …`, `< 답.txt`) 묻지 않고 **N 으로 처리**합니다(미리 넣어 둔 Y 로 의도치 않게 설정되는 것을 막기 위해). 자동화가 꼭 필요하면 `-stdin-ok` 를 붙이십시오(실행 시 경고 한 줄이 나옵니다).

실제 점검 결과 예시입니다(합성 mock 서버 4대를 대상으로 한 실제 출력이며, 호스트 이름이 `127.0.0.1:포트` 로 보이는 것은 mock 이라서입니다).

```text
프로파일: VM
설정: bios.conf (user=admin, concurrency=4, timeout=10s, insecure=true, retries=1, auth_fail_stop=3)
대상: 4개 중 해석 4개, NO_HOSTS_ENTRY 0개
프로파일: VM (testdata/profiles-test/VM.tsv)
모드: 실제 접속 (비밀번호 복호화 성공: pass.enc)

== BIOS 점검 결과 (profile=VM, 대상 4 / 2026-10-07 16:28) ==
OK                     2  → results/20261007_162818/ok.txt (OK·PENDING_OK 호스트 이름)
PENDING_OK             0
FAIL                   2

[OK] 2대
  127.0.0.1:18441  127.0.0.1:18443

[FAIL 상세] 3건 (호스트 2대)
  127.0.0.1:18442  Dell PowerEdge R660       hyper_threading  LogicalProc  기대=Enabled  현재=Disabled
  127.0.0.1:18442  Dell PowerEdge R660       llc_prefetch     LlcPrefetch  기대=Enabled  현재=Disabled
  127.0.0.1:18444  HPE ProLiant DL360 Gen11  hyper_threading  ProcHyperthreading  기대=Enabled  현재=Disabled

* 결과 파일: results/20261007_162818/  (result.tsv 전체, fail.tsv FAIL·설정 불가 행, retry.txt 재시도 대상, run_info.txt)

FAIL 3건(호스트 2대)을 Pending 으로 설정하시겠습니까? 재부팅은 하지 않습니다. (Y/N):
```

리포트의 구역:

| 구역 | 내용 |
|---|---|
| 상단 집계 | 호스트 대표 상태별 호스트 수. `OK` 줄 끝에 `ok.txt` 경로. 일시 오류 등은 `기타 오류` 한 줄로 묶음 |
| `[OK] N대` | 모든 항목이 OK/PENDING_OK 인 호스트 이름(PENDING_OK 는 `(재부팅 대기)` 표시). `-list-max`(기본 100)대를 넘으면 개수와 `ok.txt` 경로만 |
| `[FAIL 상세]` | **Y 로 설정할 수 있는** 항목: 호스트·모델·항목·속성·기대값·현재값. `-fail-max`(기본 200)줄을 넘으면 `fail.tsv` 안내 |
| `[설정 불가]` | UNVERIFIED · PENDING_EXISTS · MAPPING_MISSING · (설정 못 하는) PENDING_NO_JOB 항목과 사유. MAPPING_MISSING 은 속성 이름 후보도 보여 줍니다 |
| `[기타 오류]` | 상태별 호스트 목록, 재시도 대상 수와 `retry.txt` 경로 |

**Y 를 입력하면** 점검 이후 상태를 다시 읽어 확인한 뒤 호스트당 PATCH 1회로 Pending 만 설정하고, 반영을 재조회로 확인합니다.

```text
설정 시작: 호스트 2대, 항목 3건 (재조회 후 Pending 으로만 설정, 재부팅 없음)

== BIOS Pending 설정 결과 (호스트 2대, 항목 3건) ==
APPLIED                         3건 (호스트 2대)

[적용된 호스트 (Pending 설정 확인)] 2대 → results/20261007_162818/applied.txt
  127.0.0.1:18442  127.0.0.1:18444

* 설정 결과 파일: results/20261007_162818/  (apply_result.tsv 항목별 결과, applied.txt 적용 호스트, apply_failed.txt 실패·미확인 대상(있을 때), apply_requests.txt 보낸 요청)
※ 재부팅은 하지 않았습니다. 설정은 BMC 의 Pending 까지이며, 다음 재부팅(OS 설치 등) 때 BIOS 에 반영됩니다.
※ 반영(재부팅) 후 bios_check.sh 로 재점검하십시오.
※ [Disclaimer] 설정 변경 후 랜덤한 서버 몇 대를 직접 확인해 실제 변경되었는지 확인하십시오.
```

설정 후에는 같은 명령으로 **재점검**합니다. 적용한 항목은 재부팅 전까지 `PENDING_OK`(`[OK]` 에 `(재부팅 대기)`)로 보이고, 재부팅 후에는 `OK` 가 됩니다.

```bash
bash bios_check.sh --profile VM -no-prompt     # 재점검 (묻지 않고 결과만)
```

설정이 적용되지 않은 항목은 결과 리포트의 `[적용 안 됨 · 사유별]` 에 상태와 안내가 나옵니다(상태 표는 3.6절). 설정 쓰기는 **자동 재시도하지 않습니다**(`SET_ERROR`·`APPLY_UNCONFIRMED` 는 재점검으로 먼저 확인하십시오).

### 2.2 설정 없이 미리 보기 — `-dry-run`

```bash
bash bios_check.sh --profile VM -dry-run
```

점검 리포트 뒤에 **호스트별로 Y 를 누르면 보낼 PATCH 경로·본문(과 Dell Job)** 만 출력하고, BMC 에 아무것도 쓰지 않으며 Y/N 도 묻지 않습니다(읽기 전용 클라이언트라 쓰기가 구조적으로 불가능합니다). 미검증(`verified=N`) 모델의 FAIL 은 `[미검증 — 실제 설정 불가, 시험 출력]` 으로 따로 보여 줘서, 사람이 본문을 확인하는 데 쓸 수 있습니다.

```text
== dry-run: 보낼 요청 (BMC 에 아무것도 쓰지 않음) ==

127.0.0.1:18442 (127.0.0.1)  Dell PowerEdge R660
  [Y 응답 시 보낼 요청]
  PATCH /redfish/v1/Systems/System.Embedded.1/Bios/Settings
  본문  {"@Redfish.SettingsApplyTime":{"ApplyTime":"OnReset"},"Attributes":{"LlcPrefetch":"Enabled","LogicalProc":"Enabled"}}
  Job   POST /redfish/v1/Managers/iDRAC.Embedded.1/Jobs {"TargetSettingsURI":"/redfish/v1/Systems/System.Embedded.1/Bios/Settings"}
        (Dell: PATCH 후 자동 생성된 BIOS 설정 Job 이 보이지 않을 때만, 최대 1회. 재부팅 필드 없음)

127.0.0.1:18444 (127.0.0.1)  HPE ProLiant DL360 Gen11
  [Y 응답 시 보낼 요청]
  PATCH /redfish/v1/systems/1/bios/settings/
  본문  {"@Redfish.SettingsApplyTime":{"ApplyTime":"OnReset"},"Attributes":{"ProcHyperthreading":"Enabled"}}
  Job   없음

dry-run 요약: Y 응답 시 보낼 호스트 2대(항목 3건), 미검증 시험 출력 0대.
dry-run 이므로 BMC 에 아무것도 쓰지 않았고 설정 여부(Y/N)를 묻지 않습니다.
```

### 2.3 사전조사 — `xml.sh`

처음 만나는 모델은 BMC 에서 속성 이름·허용값을 눈으로 확인해야 프로파일에 행을 추가할 수 있습니다. (이름은 요청에 따라 `xml.sh` 로 두었지만 산출물은 JSON 입니다.)

```bash
bash xml.sh 192.0.2.10 host0002-m     # 대상을 직접 지정 (ip:포트 도 가능)
bash xml.sh                           # 대상 생략 시 user.txt 전체
bash xml.sh --compact 192.0.2.10      # 호스트당 1줄 요약
```

호스트마다 로그인 1회로 **GET 만** 보내 Redfish 트리를 좁게 순회하고(ServiceRoot, Systems, Bios, Settings, 속성 레지스트리, Managers; LogServices 등은 요청하지 않음) `dumps/` 에 JSON 으로 저장한 뒤, 사람이 읽고 옮겨 적을 수 있는 **짧은 요약**을 터미널에 출력합니다.

```text
== 127.0.0.1:18441 Dell PowerEdge R660 1.8.2 auth=session
  settings=/redfish/v1/Systems/System.Embedded.1/Bios/Settings (SettingsObject)  bios_attrs=19
  pending=없음 (settings_diff=0 jobs=0)  registry=BiosAttributeRegistry.v1_0_0
  system_profile   SysProfile = PerfOptimized [허용값: PerfPerWattOptimizedDapc | PerfPerWattOptimizedOs | PerfOptimized | Custom]
  hyper_threading  LogicalProc = Enabled [허용값: Enabled | Disabled]
  llc_prefetch     LlcPrefetch = Enabled [허용값: Enabled | Disabled]
  sub_numa_cluster SubNumaCluster = Disabled [허용값: Enabled | Disabled]
-- 합계 1대: OK 1 / PARTIAL 0 / 실패 0 | 저장 위치: dumps (index.tsv 포함)
```

**폐쇄망이라 덤프 파일은 반출할 수 없으므로, 이 요약을 사람이 읽어 전달합니다.** 요약에는 비밀번호·토큰·시리얼이 없습니다(호스트 헤더 한 줄에만 식별 이름이 나옵니다). 전달할 때 필요한 정보는 **모델(Vendor Model), BIOS 버전, 4개 표준 항목의 속성 이름·현재값·허용값·ReadOnly 표시, Settings 경로(괄호 안 출처), pending 여부** 입니다. 전달받은 값으로 `profiles/*.tsv` 에 `verified=N` 행을 추가하는 절차는 [FIRST_RUN.md](FIRST_RUN.md) 에 있습니다.

저장 위치: `dumps/<vendor>/<model>/<biosver>/<host>/redfish/v1/…` (+ `dump_meta.json`), 호스트 목록은 `dumps/index.tsv`(열: `host ip vendor model biosver path status`). 같은 호스트 폴더는 다시 실행하면 덮어씁니다.

### 2.4 모델별 BIOS 전체 비교 — `all_bios_check.sh`

표준 4개 항목이 아니라, **같은 모델 안에서 정상(기준) 호스트와 BIOS Attribute 전체를 비교**합니다. 읽기 전용이며 설정은 하지 않습니다.

```bash
bash all_bios_check.sh                       # diff.txt(기준) 와 user.txt(대상) 비교
bash all_bios_check.sh -diff-max 10          # 나머지 인자는 biostool allcheck 에 그대로 전달
```

- `diff.txt` 에는 정상 설정값을 가진 대표 호스트의 **hostname 만** 한 줄에 하나씩 적습니다(모델은 BMC 에서 자동 조회). 모델마다 한 대 이상 적으십시오.
- 기준·대상 모두 BMC 가 보고한 실제 모델을 정규화(`ProLiant DL360 Gen11` = `DL360Gen11`)해 **같은 모델끼리** 비교합니다.
- 호스트마다 값이 달라야 하는 속성(서비스태그·부팅순서·MAC 등)은 `ignore_attrs.txt`(없으면 내장 기본 목록)로 비교에서 뺍니다.
- BIOS 버전이 달라도 비교는 하고, 특이사항(`BIOS_VER_DIFF`)으로 따로 보여 줍니다.

```text
== BIOS 전체 비교 (대상 4대, 기준 2대 / 2026-10-07 16:28) ==
완전 일치                  0
차이 있음                  2  → results/20261007_162826_all/all_diff.tsv
기준 자신                  2  (같은 모델의 다른 기준이 없어 비교 안 함)

[Dell PowerEdge R660] 기준: 127.0.0.1:18441 (BIOS 1.8.2) / 대상 1대 / 완전 일치 0대 / 차이 있음 1대 (기준 자신 1대 제외)
  127.0.0.1:18442  DIFF 2, ONLY_REF 0, ONLY_HOST 0
      LlcPrefetch  기준=Enabled → 대상=Disabled
      LogicalProc  기준=Enabled → 대상=Disabled

[속성별 집계] 같은 속성이 몇 대에서 다른가 (모델 그룹별, 많은 순)
  [Dell PowerEdge R660] 비교 1대 중
    LlcPrefetch  1대 (100%)  기준 Enabled(1) → 대상 Disabled(1)
    LogicalProc  1대 (100%)  기준 Enabled(1) → 대상 Disabled(1)

* 결과 파일: results/20261007_162826_all/  (all_diff.tsv 차이 전체, summary.tsv 호스트별 요약·특이사항, retry.txt 재시도 대상, run_info.txt)
```

같은 속성이 대상의 과반에서 다르게 나오면 기준 호스트의 값이 예외일 수 있으니 기준을 다시 확인하라는 안내가 `[속성별 집계]` 에 붙습니다.

### 2.5 저장된 덤프로 오프라인 실행 — `-from-dump`

BMC 에 접속하지 않고 `xml.sh` 가 저장한 덤프를 읽어 같은 판정을 합니다(비밀번호 복호화도 하지 않으므로 `pass.enc`·`key.bin` 불필요). 설정은 할 수 없습니다(Y/N 도 묻지 않음).

```bash
bash xml.sh                                          # (먼저) 대상 전체 덤프 저장
bash bios_check.sh --profile VM -from-dump dumps     # 점검
bash bios_check.sh --profile VM -from-dump dumps -dry-run   # 덤프 시점 기준으로 보낼 요청 계산
bash all_bios_check.sh -from-dump dumps              # 전체 비교 (diff.txt 와 user.txt 의 모든 호스트 덤프가 있어야 함)
```

덤프 폴더 이름(호스트 이름)이 대상 이름과 같아야 하며, 없으면 그 호스트는 `NO_DUMP` 입니다. Dell 의 `PENDING_OK` 는 개별 Job 상세가 덤프에 없어 BIOS 설정 Job 존재를 확인할 수 없으므로 `Job 을 확인하지 못했습니다` 경고가 붙는 것이 정상입니다.

### 2.6 실패 건만 다시 — `-retry-from`

일시 오류(`UNREACHABLE`·`TIMEOUT`·`BMC_ERROR`)와 차단기가 건너뛴 `SKIPPED_AUTH_STOP` 호스트는 결과 폴더의 `retry.txt` 에 모입니다(`AUTH_FAIL` 은 계정부터 고쳐야 하므로 넣지 않음). 이전 결과 폴더를 지정하면 그 대상만 다시 점검하고 **이전 결과와 병합**합니다.

```bash
bash bios_check.sh --profile VM -retry-from results/20261007_162934/        # 폴더 또는 그 안의 retry.txt 경로
bash all_bios_check.sh -retry-from results/20261007_162826_all/
```

```text
== BIOS 점검 결과 (profile=VM, 대상 1 / 2026-10-07 16:29) ==
OK                     1  → results/20261007_162940/ok.txt (OK·PENDING_OK 호스트 이름)
...
* [재시도 병합] 이전 결과: results/20261007_162934/
  재시도 1대 → 복구 1대 / 여전히 일시오류 0대 (→ merged_retry.txt) / 다른 오류 0대
  병합본 전체 3대: FAIL 1, OK 2
  병합 파일: results/20261007_162940/ (merged_result.tsv · merged_ok.txt · merged_retry.txt · merged_run_info.txt)
```

- `-retry-from` 과 `-user` 는 함께 쓸 수 없습니다. 폴더에 `merged_retry.txt` 가 있으면(재시도 결과 폴더) 그것을 읽으므로 재시도를 여러 번 이어 갈 수 있습니다.
- 이전 결과 폴더는 읽기만 합니다. 이전 결과의 재시도 대상이 아닌 호스트 행은 한 글자도 바꾸지 않습니다. 같은 프로파일로 실행해야 합니다.
- 재시도 목록이 없거나 비어 있으면 `재시도할 대상이 없습니다` 를 출력하고 정상 종료합니다.
- `retry.txt` 는 그대로 `-user` 로 넘겨 다시 실행할 수도 있습니다.

**AUTH_FAIL 차단기.** 모든 호스트가 같은 공통 계정을 쓰므로, 비밀번호가 틀린 채 수천 대에 로그인하면 계정이 잠깁니다. 그래서 처음 `auth_fail_stop`(기본 3)대는 한 대씩 접속해 로그인이 한 번이라도 성공하면 그때부터 병렬로 처리하고, 전부 AUTH_FAIL 이면(또는 병렬 중 누적 AUTH_FAIL 이 그 수에 이르면) 새 호스트 접속을 멈추고 나머지를 `SKIPPED_AUTH_STOP` 으로 기록합니다.

```text
기타 오류                  3  (AUTH_FAIL 2, SKIPPED_AUTH_STOP 1)
!! 계정 잠금 방지를 위해 2대에서 AUTH_FAIL → 나머지 1대 중단. 계정/비밀번호 확인 후 retry.txt 로 재실행
```

### 2.7 결과 파일 위치와 컬럼

결과 폴더는 실행 위치 기준 `results/` 아래에 만들어집니다(`conf` 의 `result_dir`). 모든 결과 파일은 권한 0600 이고 비밀번호·토큰은 들어 있지 않습니다. 같은 초에 또 실행하면 폴더 이름 끝에 `_2`, `_3` 이 붙습니다.

| 파일 | 만드는 쪽 | 내용 |
|---|---|---|
| `results/<YYYYMMDD_HHMMSS>/result.tsv` | check | 호스트×항목 1행 전체 결과 |
| `…/ok.txt` | check | 모든 항목이 OK/PENDING_OK 인 호스트 이름 |
| `…/fail.tsv` | check | FAIL·UNVERIFIED·PENDING_EXISTS·MAPPING_MISSING·PENDING_NO_JOB 항목 행(같은 열) |
| `…/retry.txt` | check·allcheck | 재시도 대상 입력 원문(그대로 `-user` 로 사용 가능) |
| `…/run_info.txt` | 모두 | 실행 요약: 프로파일·시각·모드·대상 수·상태별 집계·BMC 호출 수(세션 생성 N, GET N) |
| `…/mapping_missing/` | check | MAPPING_MISSING 모델의 덤프 1대분 + `index.tsv` + `summary.txt`(사람이 읽고 프로파일에 행을 추가) |
| `…/apply_result.tsv` | 설정(Y) | 항목별 설정 결과 |
| `…/applied.txt` | 설정(Y) | Pending 반영을 재조회로 확인한(APPLIED) 호스트 |
| `…/apply_failed.txt` | 설정(Y) | 실패·미확인·접속 못 한 대상(있을 때만) |
| `…/apply_requests.txt` | 설정(Y) | 실제로 보낸 PATCH·Job 요청과 결과 |
| `…/merged_*` | `-retry-from` | 병합 결과(check: `merged_result.tsv`·`merged_ok.txt`·`merged_retry.txt`·`merged_run_info.txt`, allcheck: `merged_summary.tsv`·`merged_all_diff.tsv`·…) |
| `results/<일시>_all/all_diff.tsv` | allcheck | 차이가 있는 속성만 한 행씩 |
| `…_all/summary.tsv` | allcheck | 호스트별 요약 한 행 |
| `dumps/index.tsv`, `dumps/<vendor>/…` | dump | 덤프 목록과 JSON |

**`result.tsv` 열** (탭 구분, 호스트 단위 오류는 `std_name` 이 `-` 인 한 행):

| 열 | 의미 |
|---|---|
| `hostname` | 보고용 이름. 입력의 `-m` 을 뗀 기본 이름(IP 로 입력하면 IP, 포트가 있으면 `:포트` 포함) |
| `ip` | 접속한 BMC 주소 |
| `profile` | 사용한 프로파일 이름 |
| `vendor` / `model` / `bios_version` | BMC 가 보고한 값(알 수 없으면 `-`) |
| `std_name` | 표준 항목 이름(`system_profile`, `hyper_threading`, `llc_prefetch`, `sub_numa_cluster` + 프로파일이 추가한 항목) |
| `attribute` | BMC 의 실제 속성 이름 |
| `expected` | 프로파일의 기대값(`A\|B` 면 원문 그대로) |
| `current` | BMC 현재값 |
| `pending` | 그 속성의 Pending 값(없으면 `-`) |
| `result` | 상태(3.6절) |

**`apply_result.tsv` 열:** `host ip vendor model std_name attribute expected status detail`.
**`all_diff.tsv` 열:** `model ref_host host bios_version ref_bios_version attribute ref_value host_value status`(`status` = DIFF / ONLY_REF / ONLY_HOST, 없는 쪽 값은 `<없음>`, 빈 문자열은 `""`).
**`summary.tsv` 열:** `host ip model ref_host bios_version ref_bios_version compared ignored diff only_ref only_host status notes`.

### 2.8 mock 서버로 연습하기 (실서버 아님)

`mockbmc.sh` 는 `testdata/` 의 **합성 Redfish 트리**를 BMC 처럼 서빙하는 개발·검증용 서버입니다(Go 필요, `bin/mockbmc` 는 저장소에 없고 필요할 때 빌드). 실제 서버에 쓰기 전에 흐름을 익히는 용도이며 임시 conf 를 따로 쓰는 방법은 [사용법.txt](사용법.txt) 의 “mock 연습” 에 있습니다. testdata 의 설명과 한계는 [testdata/README.md](testdata/README.md) 를 보십시오.

---

## 3. 옵션별 상세 설명

### 3.1 서브커맨드

`biostool` 은 한 바이너리에 서브커맨드 네 개가 있고, bash 래퍼가 파일 확인·바이너리 선택·프로파일 메뉴를 맡습니다. 직접 실행하려면 `bin/biostool help` 를 보십시오.

| 서브커맨드 | 래퍼 | 하는 일 | BMC 쓰기 |
|---|---|---|---|
| `check` | `bios_check.sh` | 프로파일 표준값 점검, 승인 시 Pending 설정, `-dry-run` | **Y 응답 시에만**(PATCH, Dell 은 Job POST) |
| `allcheck` | `all_bios_check.sh` | 모델별 BIOS 전체 Attribute 비교 | 없음 |
| `dump` | `xml.sh` | 사전조사 덤프(GET 전용) | 없음 |
| `encrypt` | `encrypt.sh` | 표준입력의 비밀번호를 AES-256-GCM 으로 암호화 | 해당 없음 |

### 3.2 플래그

플래그 앞에는 `-` 또는 `--` 를 쓸 수 있습니다. 래퍼에 준 인자는 `--profile` 을 뺀 나머지가 그대로 `biostool` 에 전달됩니다.

| 플래그 | 기본값 | 대상 | 설명 |
|---|---|---|---|
| `-conf <파일>` | `bios.conf` | check allcheck dump | 설정 파일 |
| `-user <파일>` | `user.txt` | check allcheck dump | 대상 목록 파일(BMC 계정 ID 는 conf 의 `user`) |
| `-hosts <파일>` | `/etc/hosts` | check allcheck dump | 이름 해석에 쓸 hosts 파일 |
| `-profile <이름>` | (필수) | check | `profile_dir/<이름>.tsv`. 경로 문자(`/` `\` `..`)는 쓸 수 없음. (`bios_check.sh` 는 `--profile`) |
| `-from-dump <디렉터리>` | (없음) | check allcheck | 접속 대신 덤프에서 읽기. `dump` 와는 함께 쓸 수 없음 |
| `-retry-from <경로>` | (없음) | check allcheck | 이전 결과 폴더 또는 그 안의 `retry.txt`. `-user` 와 함께 쓸 수 없음. `dump` 에는 쓸 수 없음 |
| `-dry-run` | `false` | check | 설정 없이 보낼 PATCH·Job 만 출력. Y/N 안 묻음. `allcheck` 에는 쓸 수 없음 |
| `-no-prompt` | `false` | check | FAIL 이 있어도 묻지 않고 종료(아무것도 설정하지 않음) |
| `-stdin-ok` | `false` | check | 표준입력이 터미널이 아니어도(파이프·파일) Y/N 을 읽음. 자동화 전용(경고 출력) |
| `-list-max <N>` | check 100 / allcheck 20 | check allcheck | check: `[OK]` 호스트 이름을 나열할 최대 대수. allcheck: 차이 상세·특이사항·오류에 나열할 호스트 수 |
| `-fail-max <N>` | `200` | check | `[FAIL 상세]`·`[설정 불가]`·`[적용 안 됨]` 에 보여 줄 최대 줄 수(넘으면 `fail.tsv`·`apply_result.tsv` 안내) |
| `-diff <파일>` | `diff.txt` | allcheck | 기준(정상) 호스트 목록. 한 줄에 hostname(뒤에 모델을 적어도 확인용) |
| `-ignore <파일>` | `ignore_attrs.txt` | allcheck | 비교 제외 속성 목록. 파일이 없으면 내장 기본 목록, `-ignore` 로 직접 준 파일이 없으면 오류 |
| `-diff-max <N>` | `30` | allcheck | 호스트당 터미널에 보일 차이 속성 수(넘으면 `all_diff.tsv` 안내) |
| `-targets <대상,...>` | (없음) | dump | 대상을 쉼표·공백으로 직접 지정(지정하면 `-user` 파일 대신 사용, `ip:포트` 가능). `xml.sh` 가 인자를 모아 전달 |
| `-out <디렉터리>` | conf 의 `dump_dir` | dump | 덤프 저장 디렉터리 |
| `-compact` | `false` | dump | 호스트당 1줄 요약 |
| `-key <파일>` | `key.bin` | encrypt | AES-256 키 파일(없으면 새로 생성, 0600) |
| `-out <파일>` | `pass.enc` | encrypt | 암호문 출력 경로 |

`-list-max`·`-fail-max`·`-diff-max` 는 0 이상이어야 합니다. 색은 표준출력이 터미널이고 `NO_COLOR`·`TERM=dumb` 가 없을 때만 켜집니다. `encrypt` 는 비밀번호를 **인자로 받지 않고** 표준입력(파이프)으로만 받습니다(터미널이면 거부).

**래퍼 스크립트**

| 스크립트 | 사용법 | 설명 |
|---|---|---|
| `bios_check.sh` | `[--profile <이름>] [biostool check 옵션...]` | 파일 확인 → 프로파일 메뉴(3회까지 재입력) → `biostool check -profile …`. `-retry-from` 이면 `user.txt` 불필요, `-from-dump` 이면 `pass.enc`·`key.bin` 불필요. `-h` 도움말 |
| `xml.sh` | `[--compact] [대상...]` | 대상 인자를 쉼표로 묶어 `biostool dump -targets …`. 대상이 없으면 `user.txt`. **설정은 항상 `./bios.conf` 를 쓰며 `-conf` 등 다른 옵션은 받지 않습니다**(알 수 없는 옵션은 오류). 일부 호스트가 실패해도 안내를 보여 준 뒤 종료코드를 전달 |
| `all_bios_check.sh` | `[biostool allcheck 옵션...]` | 파일 확인 후 `biostool allcheck` 실행 |
| `encrypt.sh` | (인자 없음) | 비밀번호를 두 번 입력받아 같을 때만 `pass.enc`·`key.bin`(권한 600) 생성 |
| `mockbmc.sh` | `<트리이름> [포트]` 또는 `-dir <경로> -listen <주소> -user <ID> [-v]` | mock BMC 기동(개발·검증용). 비밀번호는 환경변수 `MOCKBMC_PASS`. 종료 시 집계 한 줄 출력 |
| `setup.sh` / `build.sh` / `build_os6.sh` | (인자 없음) | 1.2절 |
| `lib.sh` | (직접 실행 안 함) | 래퍼가 `source` 하는 공용 함수: 바이너리 선택·conf 값 읽기 |

종료코드: 래퍼는 필요한 파일이 없으면 `1`, 바이너리를 못 고르거나 go 가 없으면 `2`. `biostool` 은 정상 `0`, 오류 `1`(점검 결과에 FAIL 이 있어도 `0`; `dump` 는 덤프하지 못한 호스트가 있으면 `1`).

### 3.3 설정 파일 `bios.conf`

`key=value` 한 줄씩. `#` 로 시작하는 줄과 빈 줄은 무시하며 **줄 끝 주석은 지원하지 않습니다**. 같은 키가 여러 번 나오면 마지막 값이 적용됩니다. 알 수 없는 키, 그리고 `pass`·`password`·`passwd`·`pw` 키(비밀번호를 conf 에 두는 시도)는 **오류**입니다.

| 키 | 기본값 | 필수 | 설명 |
|---|---|---|---|
| `user` | (없음) | **예** | BMC 계정 ID. 전 벤더 공통 계정 1개를 전제 |
| `pass_file` | `pass.enc` | | 암호화된 비밀번호 파일 |
| `key_file` | `key.bin` | | AES-256 키 파일 |
| `concurrency` | `100` | | 동시에 접속할 호스트 수(1 이상). ulimit 은 3.5절 참고 |
| `timeout` | `20` | | 요청 1건당 타임아웃(초, 1 이상) |
| `insecure` | `true` | | `true`: BMC 자체서명 인증서 검증 생략 / `false`: 검증 |
| `retries` | `2` | | 일시 오류(UNREACHABLE·TIMEOUT·BMC_ERROR) 자동 재시도 횟수(0 이상, 코드 상한 5). **GET 과 세션 연결 실패만** 재시도하고 PATCH·Job POST·DELETE 는 재시도하지 않으며 401(AUTH_FAIL)은 어떤 경우에도 재시도하지 않음 |
| `auth_fail_stop` | `3` | | AUTH_FAIL 호스트가 이 수에 이르면 새 호스트 접속을 멈추고 나머지를 SKIPPED_AUTH_STOP 으로 기록. 처음 이 수만큼은 한 대씩 접속해 로그인이 한 번이라도 성공하면 병렬로 전환. `0` 이면 끔(계정 잠금 위험) |
| `result_dir` | `results` | | 결과 폴더의 부모 |
| `dump_dir` | `dumps` | | 사전조사 덤프 저장 위치 |
| `profile_dir` | `profiles` | | 프로파일(`<이름>.tsv`) 디렉터리 |

같은 BMC 로 가는 연속 요청은 최소 100ms 간격을 두며(BMC 부하 방지), 환경변수 프록시는 거치지 않고 리다이렉트는 따라가지 않습니다.

### 3.4 프로파일 `profiles/<이름>.tsv`

표준값 파일 1개가 프로파일 1개입니다. **탭 구분**(엑셀에서 그대로 붙여넣기 가능, 끝의 빈 열은 무시)이며 첫 줄은 머리글이고 `#` 로 시작하는 줄과 빈 줄은 무시합니다(줄 끝 주석 없음).

```text
vendor	model	std_name	attribute	value	verified
DELL	R660	system_profile	SysProfile	PerfOptimized	N
DELL	R660	hyper_threading	LogicalProc	Enabled	N
DELL	*	llc_prefetch	LlcPrefetch	Enabled	N
HPE	DL360 Gen11	hyper_threading	ProcHyperthreading	Enabled	N
```

| 열 | 설명 |
|---|---|
| `vendor` | 벤더(`DELL` / `Dell Inc.` 모두 `dell` 로 비교) |
| `model` | 모델. 공백·하이픈·밑줄·벤더 접두(`ProLiant`·`PowerEdge`·`ThinkSystem` 등)를 지워 비교(`DL360 Gen11` = `DL360Gen11` = `ProLiant DL360 Gen11`). `*` 는 그 벤더의 공통 행 |
| `std_name` | 표준 항목 이름(소문자·숫자·밑줄) |
| `attribute` | BMC 의 속성 이름(대소문자 무시로 찾음) |
| `value` | 기대값. `A\|B` 로 허용값 여러 개(하나라도 같으면 OK). **값 비교는 대소문자까지 같아야 함**(Cisco 처럼 소문자 값은 행에 그대로). 둘 다 10진 정수면 정수로 비교(`05` = `5`) |
| `verified` | `Y` 또는 `N` |

규칙:

- **모델 선택:** 같은 `std_name` 에서 모델이 정확히 맞는 행이 있으면 그것만 쓰고, 없으면 `*` 행을 씁니다(항목별로 따로 판단).
- **후보 이름:** 한 `std_name` 에 행이 여러 개면 속성 이름 **후보**입니다. 파일 순서가 우선순위이고, 실제 BMC 에 있는 첫 후보의 `value`·`verified` 를 적용합니다.
- **표준 4항목**(`system_profile`, `hyper_threading`, `llc_prefetch`, `sub_numa_cluster`)은 프로파일에 행이 없어도 항상 점검 대상이며 행이 없으면 `MAPPING_MISSING` 입니다. 그 밖의 `std_name` 행은 추가 점검 항목이 됩니다.
- **`verified=Y` 는 모델을 지정한 행에만** 쓸 수 있습니다. **`*` 행에 `verified=Y` 를 쓰면 프로파일 로드 오류**입니다(검증하지 않은 모델에도 맞으므로 쓰기 허가를 줄 수 없음). `*` 행은 점검 비교용입니다.
- `verified=Y` 는 첫 실장비에서 dry-run 본문을 **사람이 확인한 뒤에만** 표시합니다. `N` 인 모델은 FAIL 이어도 `UNVERIFIED` 로 점검만 되고 Y 를 눌러도 설정되지 않습니다.
- 같은 (벤더, 모델, 항목, 속성) 행이 중복되면 오류입니다. `profiles/VM.tsv` 에 들어 있는 행은 **엑셀 표에서 가져온 시작값(전부 `verified=N`)** 이며 첫 실장비에서 확정해야 합니다([FIRST_RUN.md](FIRST_RUN.md)).

### 3.5 입력 파일 형식

| 파일 | 형식 |
|---|---|
| `user.txt` | 한 줄에 대상 하나(`IP` / `이름-m` / `이름`, 뒤에 `:포트` 가능). `#` 주석·빈 줄 무시, 중복은 처음 것만 |
| `diff.txt` | 한 줄에 기준 호스트 하나(`user.txt` 와 같은 이름 해석). 뒤에 공백/탭 뒤로 모델을 적어도 되지만 확인용(실제 모델과 다르면 `DIFF_TXT_MODEL_MISMATCH` 경고 후 실제 모델로 분류) |
| `ignore_attrs.txt` | 한 줄에 패턴 하나, `#` 주석, 대소문자 무시, `*` 만 와일드카드. 파일이 있으면 그 파일만 씀 |

**동시 접속과 ulimit.** 호스트마다 연결이 열리므로 `ulimit -n`(열린 파일 수)은 `concurrency` 보다 넉넉해야 합니다(동시 수 + 수십 이상, 기본 100 이면 1024 로 충분). 수천 대를 처리하려고 `concurrency` 를 크게 올릴 때는 `ulimit -n` 도 함께 올리십시오.

### 3.6 결과 상태 코드 표

#### 점검(`check`) — 항목 단위 (`result.tsv` 의 `result`)

| 상태 | 의미 | 재시도 | Y 설정 대상 |
|---|---|---|---|
| `OK` | 현재값 = 기대값 | - | - |
| `PENDING_OK` | 현재값은 다르나 Pending 이 기대값(재부팅 대기). Dell 은 예약된 BIOS 설정 Job 이 확인될 때, 확인 못 하면 경고 | - | - |
| `FAIL` | 현재값 ≠ 기대값(`verified=Y` 행) | - | ○ |
| `UNVERIFIED` | 불일치지만 `verified=N` → 설정 불가(dry-run 시험 출력만) | - | × |
| `PENDING_EXISTS` | 불일치인데 남의 Pending(우리 항목이 아닌 속성, 또는 기대값도 현재값도 아닌 Pending)이 있음 | - | × (**남의 Pending 은 건드리지 않음**) |
| `PENDING_NO_JOB` | Dell: Pending 은 기대값인데 예약된 BIOS 설정 Job 이 없음 → 재부팅해도 반영되지 않을 수 있음 | - | ○ (`verified=Y` 이고 남의 Pending 이 없을 때만 같은 값을 다시 PATCH + Job) |
| `MAPPING_MISSING` | 프로파일에 이 모델 행이 없거나 후보 속성이 BMC 에 없음 → 덤프 자동 저장 | × | × |

#### 점검 — 호스트 단위 (`std_name` 이 `-` 인 한 행)

| 상태 | 의미 | 자동 재시도 | `retry.txt` |
|---|---|---|---|
| `UNREACHABLE` | 연결 거부·no route·DNS | ○ (지수 백오프, conf `retries`) | ○ |
| `TIMEOUT` | 응답 시간 초과 | ○ | ○ |
| `BMC_ERROR` | 5xx | ○ | ○ |
| `AUTH_FAIL` | 401(또는 로그인 403) | **금지**(계정 잠금·로그인 실패 기록 방지) | × (계정부터 고칠 것) |
| `SKIPPED_AUTH_STOP` | AUTH_FAIL 차단기가 작동해 접속하지 않음 | - | ○ |
| `UNSUPPORTED` | Redfish 미지원(`/redfish/v1` 404, Bios 리소스 없음·속성 비어 있음 등. Cisco UCSM 관리형, Supermicro X11 이하 등) | × | × |
| `HTTP_ERROR` | 그 밖의 4xx·3xx·응답 해석 실패 | × | × |
| `NO_HOSTS_ENTRY` | `/etc/hosts` 에 `이름-m` 이 없음 | × | × |
| `NO_DUMP` | `-from-dump` 폴더에 그 호스트 덤프가 없음 | × | × |

호스트 대표 상태(상단 집계)는 오류가 있으면 그 상태, 없으면 항목 중 가장 나쁜 것(FAIL → PENDING_NO_JOB → PENDING_EXISTS → UNVERIFIED → MAPPING_MISSING → PENDING_OK → OK)입니다.

#### 설정(Y) — 항목 단위 (`apply_result.tsv` 의 `status`)

| 상태 | 의미 | `apply_failed.txt` |
|---|---|---|
| `APPLIED` | PATCH 후 Settings 재조회로 Pending 반영 확인 | |
| `APPLY_UNCONFIRMED` | BMC 는 받았으나 반영을 확인하지 못함(Settings 재조회 실패·Pending 불일치·Dell Job 생성 실패) → 재점검·BMC 화면 확인 | ○ |
| `ALREADY_OK` | 재조회해 보니 현재값 또는 Pending 이 이미 기대값(Dell 은 Job 도 확인) → 쓰지 않음 | |
| `CHANGED` | 점검 이후 모델·Settings 경로·속성이 바뀜 → 쓰지 않음 | |
| `PENDING_EXISTS` | 점검 이후 남의 Pending 이 생김 → 쓰지 않음 | |
| `UNVERIFIED` | 프로파일 재검증 실패(모델을 지정한 `verified=Y` 행 없음, 모델명이 비어 있음) → 쓰지 않음 | |
| `SKIPPED_READONLY` | 레지스트리에서 ReadOnly → 보내지 않음 | |
| `INVALID_VALUE` | 허용값 밖·형식 불일치·위험 속성명 등 안전 검사 거부 → 보내지 않음 | |
| `DEPENDS_ON_PROFILE` | 시스템 프로파일 변경이 먼저(다른 항목이 종속될 수 있음) → 프로파일 항목만 보내고 나머지는 재부팅 후 재점검해 다시 설정 | |
| `SET_NOT_SUPPORTED_VENDOR` | Dell·HPE·Lenovo 외(Cisco·Supermicro 등) → 접속하지 않음 | |
| `DUPLICATE_TARGET` | 같은 BMC(IP:포트)를 가리키는 앞의 대상에서 처리 → 두 번 쓰지 않음 | |
| `REJECTED` | BMC 가 400 등으로 거부 | ○ |
| `SET_UNSUPPORTED` | 404/405/501, Settings·Job 경로를 만들 수 없음(허용목록 밖 포함) | ○ |
| `SET_ERROR` | 5xx·타임아웃·연결 끊김: **쓰기 결과 불명**(재시도하지 않음, 재점검 필요) | ○ |
| `AUTH_FAIL` | 인증 실패(재시도하지 않음) | ○ |
| `SKIPPED_AUTH_STOP` | 차단기 작동 → 접속하지 않음 | ○ |
| `UNREACHABLE` `TIMEOUT` `BMC_ERROR` `HTTP_ERROR` `UNSUPPORTED` | 쓰기 전 로그인·조회 단계의 호스트 오류 | ○ |

설정 단계는 **자동 재시도하지 않습니다**(비멱등 호출). 실패 대상은 `apply_failed.txt` 로 알려 주며, 재점검(`bios_check.sh`)으로 현재 상태를 확인한 뒤 필요하면 다시 Y 를 누르십시오.

#### 전체 비교(`allcheck`)

| 상태 | 의미 | `retry.txt` |
|---|---|---|
| `SAME` | 제외 속성을 뺀 모든 속성이 기준과 같음 | |
| `DIFF` | 차이 있음(속성 단위 상태는 아래) | |
| `REFERENCE` | 기준 호스트 자신이라 같은 모델의 다른 기준이 없어 비교하지 않음 | |
| `NO_REFERENCE` | 이 모델의 기준 호스트가 `diff.txt` 에 없음(비교하지 않고 목록화) | |
| `REF_UNREACHABLE` | 이 모델의 기준일 수 있는 호스트를 모두 읽지 못함 | ○ |
| 호스트 오류 | `UNREACHABLE` `TIMEOUT` `BMC_ERROR` `SKIPPED_AUTH_STOP` 는 `retry.txt` ○, `AUTH_FAIL` `UNSUPPORTED` `HTTP_ERROR` `NO_HOSTS_ENTRY` `NO_DUMP` 는 × | |

- 속성 단위(`all_diff.tsv`): `DIFF`(값 다름), `ONLY_REF`(기준에만 존재), `ONLY_HOST`(대상에만 존재; BIOS 버전이 다르면 흔함).
- 특이사항(`summary.tsv` 의 `notes`): `BIOS_VER_DIFF`(대상 버전 ≠ 기준 버전, 비교는 그대로 진행), `MULTI_REF`(같은 모델 기준이 여럿: BIOS 버전이 같은 쪽, 없으면 `diff.txt` 순서의 첫 기준), `DIFF_TXT_MODEL_MISMATCH`(`diff.txt` 의 모델 칸이 실제와 다름).

#### 사전조사(`dump`) — `index.tsv` 의 `status`

`OK`, `PARTIAL`(일부 리소스 실패 또는 호스트당 120경로 상한), `SAVE_FAIL`(저장 실패), 그리고 `UNREACHABLE` `TIMEOUT` `BMC_ERROR` `AUTH_FAIL` `UNSUPPORTED` `HTTP_ERROR` `NO_HOSTS_ENTRY` `SKIPPED_AUTH_STOP`(HTTP 코드가 있으면 `AUTH_FAIL(HTTP 401)` 처럼 붙음).

---

## 4. 문서별 설명

| 파일 | 설명 |
|---|---|
| `README.md` | 이 문서: 설치·빌드·사용법·옵션·상태 코드·주의사항 |
| [ARCHITECTURE.md](ARCHITECTURE.md) | 폴더·파일별 역할 표, “OO 추가면 볼 곳” 가이드, 안전 설계 요약 |
| [WORKFLOW.md](WORKFLOW.md) + `workflow*.svg` | 점검·설정·사전조사·전체 비교·재시도의 작업 흐름도(mermaid, 렌더링한 SVG 포함) |
| [FIRST_RUN.md](FIRST_RUN.md) | 첫 실장비 절차와 사람이 확인할 항목 목록(결과를 어디에 반영할지 포함) |
| [PR_CHECKLIST.md](PR_CHECKLIST.md) | 수정·배포 전 체크리스트(바이너리 재빌드·두 바이너리 커밋·보안 테스트 등) |
| [CHANGELOG.md](CHANGELOG.md) | 변경 이력(v0.1.0), 버전 태그 안내 |
| [사용법.txt](사용법.txt) | 수동 시험용 명령만 모은 목록(각 명령 위에 `#` 설명) |
| [계획서.md](계획서.md) | 설계 원본·인터뷰 결정·성공기준 검증 현황·단계 진행표 |
| `setup.sh`, `build.sh`, `build_os6.sh` | 오프라인 빌드 / os8 빌드 / os6(RHEL6) 빌드 |
| `bios_check.sh`, `xml.sh`, `all_bios_check.sh`, `encrypt.sh`, `lib.sh` | 실행 래퍼(점검·설정 / 사전조사 / 전체 비교 / 비밀번호 암호화 / 공용 함수) |
| `mockbmc.sh` | mock BMC 기동(개발·검증용) |
| `bin/biostool`, `bin/biostool_os6` | 빌드 완료 바이너리(os8 / RHEL6) — 저장소에 **커밋**되어 있음 |
| `cmd/biostool/` | Go 소스(점검·설정·덤프·전체 비교·재시도·Redfish 클라이언트)와 테스트 |
| `cmd/mockbmc/`, `internal/mockbmc/` | mock BMC 서버와 가상 BMC 수천 대를 한 리스너에 올리는 Farm(시험용) |
| `testdata/` | mock 용 **합성** Redfish 트리 6종(+ 테스트용 프로파일). 실제 덤프가 아님 → [testdata/README.md](testdata/README.md) |
| `profiles/VM.tsv` | 표준값(프로파일) — 엑셀 표 시작값, 전부 `verified=N` |
| `bios.conf.example`, `user.txt.example`, `diff.txt.example`, `ignore_attrs.txt.example` | 설정·입력 파일 예시(실제 값 없음) |
| `go.mod` | `module biostool`, `go 1.20`, 외부 의존성 없음 |
| `.gitignore`, `.gitattributes` | 실제 호스트·계정·결과물 커밋 차단 / `.sh` 등은 LF 로 체크아웃 |
| 저장소 루트 `.github/workflows/redfish-ci.yml` | 이 폴더가 바뀔 때 build/vet/test 자동 실행 |

---

## 주의사항 (Disclaimer)

> 본 로그 분석 관련 스크립트 및 툴은 100% 신뢰하기보다는 참고용(보조 도구)으로 사용하는 것을 권장합니다.

> **설정 변경(Y 적용) 후에는 랜덤한 서버 몇 대를 직접 확인해 실제로 변경되었는지 확인하십시오.** (BMC 웹 화면·BIOS Setup 에서 값을 보거나, 재부팅 후 `bios_check.sh` 로 재점검합니다.) 이 도구가 출력하는 `APPLIED` 는 “BMC 의 Pending 에 기대값이 들어간 것을 재조회로 확인했다”는 뜻일 뿐, BIOS 에 실제로 반영되었다는 보증이 아닙니다.

- **이 도구는 아직 실제 BMC 에서 검증되지 않았습니다(v0.1.0).** 모델을 처음 만날 때는 [FIRST_RUN.md](FIRST_RUN.md) 의 절차대로 1대부터 확인하십시오. 속성 이름·허용값·Settings 경로·Dell Job 동작은 실제 응답으로 확정해야 합니다.
- **재부팅을 하지 않습니다.** 설정은 BMC 의 Pending 까지이고 **다음 재부팅 때 반영**됩니다. 반영 전에는 현재값이 그대로이며 재점검에서 `PENDING_OK` 로 보입니다. Dell 은 iDRAC Job Queue 에 BIOS 설정 Job 이 있어야 반영됩니다(없으면 `PENDING_NO_JOB`).
- **BMC 로그에 흔적이 남습니다.** 호스트당 로그인/로그아웃 기록 1쌍, 설정(Y)을 하면 설정 변경(및 Dell 은 Job) 기록이 추가됩니다. 같은 기능을 짧은 간격으로 반복 실행하지 마십시오. 세션 생성을 지원하지 않는 BMC 는 Basic 인증으로 폴백하므로(`auth=basic`) 요청마다 인증 기록이 남을 수 있습니다. 기존 로그·Job·남의 Pending 은 절대 건드리지 않습니다.
- **`testdata/` 는 합성 자료입니다.** 공개 문서를 바탕으로 사람이 만든 것이라 실제 BMC 덤프가 아니며 속성명·허용값·경로가 실제 장비와 다를 수 있습니다. 테스트가 통과한 것은 실제 장비에서 된다는 뜻이 아닙니다.
- **미검증 모델은 설정하지 않습니다.** `verified=Y`(모델을 지정한 행)만 Y 로 설정되고 `*` 행은 `verified=Y` 를 쓸 수 없습니다. 쓰기는 Dell·HPE·Lenovo 만 하며 Cisco·Supermicro 와 Cisco UCSM 관리형·Supermicro X11 이하는 점검만(안 되면 `UNSUPPORTED`)입니다.
- **AUTH_FAIL(401)은 재시도하지 않고, 차단기(`auth_fail_stop`)가 계정 잠금을 막습니다.** 공통 계정이 틀렸을 때 수천 대에 시도하지 않도록 처음 몇 대만 접속하고 멈춥니다. `auth_fail_stop=0` 으로 끄면 계정이 잠길 수 있습니다. 계정 잠금 정책이 있는 환경에서는 비밀번호를 먼저 확인하십시오.
- **실제 RHEL6 커널(2.6.32)에서의 기동은 확인되지 않았습니다.** `bin/biostool_os6` 는 Go 1.20.14 정적 빌드이고 CentOS 6 userland(bash 4.1.2)에서의 동작은 확인했지만, 랩의 커널은 4.18 입니다. 회사 RHEL6 에서 처음 실행할 때 `bin/biostool_os6 help` 가 뜨는지부터 확인하십시오.
- 점검·설정 결과(`results/`)에는 호스트 이름·모델·BIOS 설정값이 들어 있고 `pass.enc`·`key.bin` 은 비밀번호와 직결됩니다. 모두 커밋 금지(`.gitignore` 등록)이며 필요 없으면 직접 삭제하십시오. 결과 파일 권한은 0600 입니다.
- 같은 BMC 를 가리키는 입력이 둘(예: `host` 와 `host-m`)이면 설정은 한 번만 합니다(`DUPLICATE_TARGET`). 입력 목록에 같은 서버를 다른 표기로 중복해 넣지 않는 것이 좋습니다.

---

## 5. 전역 명령어로 사용하기 (선택 사항)

빌드된 실행 파일을 PATH 환경 변수에 포함된 디렉터리로 이동하거나, 실행 파일이 있는 경로를 PATH에 추가하면 어디서든 명령어처럼 사용할 수 있습니다.

```bash
# 방법 1: 이 폴더를 PATH 에 추가 (래퍼 스크립트와 bin/ 을 그대로 쓰는 방법 — 권장)
echo 'export PATH="$PATH:/opt/redfish"' >> ~/.bashrc      # /opt/redfish = 이 폴더를 놓은 경로
bios_check.sh --profile VM          # 어디서든 실행 (스크립트가 자기 폴더로 이동해 실행합니다)

# 방법 2: 바이너리만 복사 (직접 biostool 을 쓰는 경우)
sudo cp bin/biostool /usr/local/bin/biostool        # RHEL6 은 bin/biostool_os6 를 biostool 이름으로 복사
```

- 래퍼 스크립트는 `cd "$(dirname "$0")"` 로 **자기 폴더**에서 `lib.sh`·`bin/`·`profiles/`·`bios.conf` 를 찾으므로, 스크립트만 `/usr/local/bin` 으로 복사하거나 **심볼릭 링크로 걸면 동작하지 않습니다**(`lib.sh` 를 찾지 못함). 폴더를 PATH 에 추가하는 방식(방법 1)을 쓰십시오.
- 바이너리만 복사했다면 `bios.conf`·`user.txt`·`profiles/` 등을 **현재 디렉터리 기준**으로 찾으므로, 작업 디렉터리로 이동하거나 `-conf`·`-user` 로 경로를 지정하십시오(예: `biostool check -profile VM -conf /opt/redfish/bios.conf …`, 프로파일 위치는 conf 의 `profile_dir`).
