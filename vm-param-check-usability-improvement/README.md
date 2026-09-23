# VM 매개변수 체크/설정 통합 툴 — 사용성 및 성능 개선 프로젝트

`vm-param-check`를 현업에서 쓸 때 겪던 두 가지 문제 — ① 매번 옵션을 손으로 다 입력해야 하는 번거로움, ② 대상 VM이 몇 대뿐이어도 초기 조회가 느린 문제 — 를 해결한 개선 프로젝트입니다. 실제 도구(소스코드)는 이 폴더 바로 아래 [`vm-param-check/`](./vm-param-check/)에 그대로 있습니다.

> 체크 → 교정 → 재검증 흐름은 [../WORKFLOW.md](../WORKFLOW.md), 폴더·파일별 역할은 [../ARCHITECTURE.md](../ARCHITECTURE.md)(V2 전체 기준)에 정리해 두었습니다.

⚠️ **주의사항 (Disclaimer)**
본 로그 분석 관련 스크립트 및 툴은 100% 신뢰하기보다는 참고용(보조 도구)으로 사용하는 것을 권장합니다. 설정 변경 스크립트의 경우, 설정을 변경한 후 반드시 랜덤하게 몇 개의 서버를 직접 접속·확인하여 실제로 설정이 제대로 반영되었는지 교차 검증을 진행하십시오.

## 빠른 시작 (지금 바로 실행해 보기)

```bash
# 1) 저장소를 받고 실제 도구 폴더로 이동 (인터넷 필요, 딱 이번 한 번만)
git clone https://github.com/qazx2675/myrepo.git myrepo
cd "myrepo/.claude/VM/vm-param-check-usability-improvement/vm-param-check"

# 2) 빌드 (인터넷 불필요 — vendor/에 의존성이 전부 포함되어 있음)
bash setup.sh

# 3) 실제 인프라 없이 동작만 먼저 확인 (아무것도 안 건드림)
./vm-param-check -demo
```

여기까지 되면 빌드는 끝났습니다. 이제 실제 vCenter를 체크하려면 다음과 같이 합니다.

```bash
# 4) vCenter 인증 정보
export VC_USER='administrator@vsphere.local'
export VC_PASS='...'

# 5) 체크할 vCenter 주소 목록 (한 줄에 하나)
echo '192.168.0.50' > vcenter.txt

# 6) 체크할 VM hostname 목록 (한 줄에 하나)
printf '1번VM\n2번VM\n' > targets.txt

# 7) 옵션을 하나도 안 주고, 폴더명으로 자동매칭해서 체크
#    (-specRoot 아래에 스펙이 미리 준비되어 있어야 함 — 없으면 8번으로)
./vm-param-check -vcenterList=vcenter.txt -f=targets.txt -specRoot=./SPEC_DIR -out=result.csv
```

**스펙이 아직 없다면**(`-specRoot` 아래 디렉터리가 비어 있다면) 먼저 만들어야 합니다. 이름은 VM이 속한 vCenter 폴더 이름(예: `TST-CAE001-SAMP48c-QRST`)과 똑같이 맞춥니다.

```bash
# 8) 스펙 디렉터리+틀 생성 (vCenter 연결 안 함)
mkdir -p ./SPEC_DIR
./vm-param-check -specRoot=./SPEC_DIR -initFolder="TST-CAE001-SAMP48c-QRST"

# 9) 생성된 ./SPEC_DIR/TST-CAE001-SAMP48c-QRST/TST-CAE001-SAMP48c-QRST_spec.txt 파일을
#    열어서 ht/cores/numa/cpu/mem/disk/shares-ev01 값을 채운 뒤, 7번 명령을 다시 실행
```

### 보조 스크립트로 한 번에 하기

위 4~9번을 매번 손으로 치는 대신, 이 폴더(도구 폴더)에 있는 보조 스크립트 두 개를 쓸 수 있습니다.

- **`folder_setup.sh`** — 위 8~9번을 대신합니다. `SPEC_DIR`(스크립트와 같은 위치에 만들어지는 로컬 폴더) 아래에 새 스펙을 만들 때, 폴더 이름과 필수 값(ht/cores/numa/cpu/mem/disk/shares-ev01)을 대화형으로 물어보고, 필요하면 ev02/ev03 값도 이어서 물어봅니다(비워 두면 건너뜀). 폴더명 규칙 검사와 중복 거부는 `vm-param-check -initFolder`가 그대로 해 줍니다.
  ```bash
  bash folder_setup.sh
  ```
  이렇게 만든 스펙은 팀이 함께 쓰도록 **git에 커밋하는 것을 권장**합니다(인증정보 같은 민감정보가 아니라 CAE 스펙 정의값이라서).

- **`vm_setting_check_insert.sh`** — 위 4~7번을 대신합니다. 스크립트 상단의 `VC_USER`/`VC_PASS`/`VCENTER_LIST`/`SPEC_ROOT` 변수와, `set_user()` 함수 안의 `user` 값(예: `user="kdh"`)을 채워 두면, `-f "<user>.txt"`로 대상 목록을 읽고 `result_<user>.csv`로 결과를 저장합니다. 실행하면 "실제로 설정을 변경(-fix)하시겠습니까?"를 먼저 물어보고, `y`를 답해도 `vm-param-check` 자체의 최종 변경 확인을 한 번 더 거칩니다(이중 확인).
  ```bash
  bash vm_setting_check_insert.sh
  ```

스펙 자동매칭 없이 옵션을 매번 직접 지정하려면 다음과 같이 실행합니다.

```bash
./vm-param-check -vcenterList=vcenter.txt -f=targets.txt \
  --ht=on --cores=8 --numa=8 --cpu=16 --mem=64 --disk=500 --shares-ev01=2000 \
  --out=result.csv
```

**폐쇄망(오프라인) 서버에 옮겨서 쓰려면** 1~2번 대신, 인터넷 되는 곳에서 이 폴더 전체를 압축해 USB/scp로 옮긴 뒤 그 서버에서 `bash setup.sh`만 실행하면 됩니다 — 더 자세한 절차와 각 옵션의 의미는 아래 "사용법" 절의 하위 문서를 참고하세요.

> **V2 안의 사본입니다.** 의존성은 `V2/govendor/govmomi-0.39.0`을 `setup.sh`가 `vendor`로 링크해서 쓰므로, V2 폴더를 통째로 옮기면 오프라인 빌드가 됩니다(이 폴더만 떼어 가면 링크 대상이 없어 빌드되지 않음). 이 폴더만 따로 쓰려면 vendor가 실제 파일로 들어 있는 **독립 브랜치 `vm-param-check-standalone`**을 받으세요(아래 "인터넷이 되는 서버 갱신하기" 절).

### 폐쇄망 서버 갱신하기 (`update.sh`) — 실행 파일만 교체

폐쇄망 서버는 GitHub에 접속할 수 없으므로, 인터넷이 되는 빌드 서버에서 **업데이트 패키지**를 만들어 USB/nfs로 가져가고 폐쇄망 서버에서는 `update.sh`만 실행합니다. `git`, `go`, 인터넷이 필요 없습니다. 바뀐 파일(지금은 `vm-param-check` 실행 파일)만 교체하고, 사용 중인 디렉토리의 나머지(`01.vm_setting_check_insert.sh`, `vcenter.txt`, `SPEC_DIR/`, 대상 목록 등)는 하나도 건드리지 않습니다.

```bash
# [인터넷 되는 빌드 서버 — Go 필요] 패키지 만들기
bash make_update_package.sh          # -> dist/vm-param-check-update-YYYYMMDD-HHMM.tar.gz (정적 빌드, linux/amd64)

# [폐쇄망 서버] 패키지를 가져가서 압축을 풀고
tar xzf vm-param-check-update-YYYYMMDD-HHMM.tar.gz
cd vm-param-check-update-YYYYMMDD-HHMM
bash update.sh -n "/내가/사용중인/디렉토리"   # 미리보기(아무것도 안 바꿈)
bash update.sh "/내가/사용중인/디렉토리"      # 실제 교체
```

- **사용 중인 디렉토리** = `./vm-param-check` 실행 파일이 있는 디렉토리입니다. 경로에 공백/한글이 있어도 됩니다.
- **교체 전에 확인**합니다: 패키지가 깨지지 않았는지(`SHA256SUMS`), 새 실행 파일이 이 서버에서 실제로 실행되는지(`-demo`, vCenter 접속 안 함). 실행이 안 되면 아무것도 바꾸지 않고 중단합니다.
- **이전 실행 파일은 백업**합니다: `<디렉토리>.update_backup.<시각>/`. 되돌리려면 그 안의 `vm-param-check`를 다시 복사하면 됩니다.
- `01.*`, `vcenter.txt`, `SPEC_DIR/`, `*.csv`, `*.log`는 패키지에 같은 이름이 들어 있어도 건너뜁니다. 바뀐 게 없으면 "이미 최신"으로 끝납니다.
- 실행 파일은 정적 빌드라서 서버의 glibc 버전이 달라도 돌아갑니다. x86_64가 아니면 `GOARCH=arm64 bash make_update_package.sh`처럼 바꿔서 만드세요.

### 인터넷이 되는 서버 갱신하기 (`update_deploy.sh`)

인터넷(GitHub)이 되는 서버에서 소스 전체를 받아 갱신할 때 씁니다(폐쇄망에서는 위 `update.sh`를 쓰세요). **배포 경로에 있는 사용자 파일(`01.vm_setting_check_insert.sh`, `vcenter.txt`, `SPEC_DIR/`, 대상 목록 `*.txt`, 결과 `*.csv` 등)은 건드리지 않고**, 새 버전에 들어 있는 소스·실행 파일만 그 자리에서 갱신합니다.

```bash
# 1) 서버에서 이 스크립트를 실행 (한 번 받아두면 됨 — 독립 브랜치의 루트에 있음)
git clone --depth 1 --branch vm-param-check-standalone https://github.com/qazx2675/myrepo.git /tmp/vpc-update
cd /tmp/vpc-update

# 2) 먼저 무엇이 바뀔지만 확인 (아무것도 안 바꿈)
bash update_deploy.sh -n /실제/배포/경로/vm-param-check

# 3) 문제 없으면 실제 갱신
bash update_deploy.sh /실제/배포/경로/vm-param-check
```

- **배포 경로는 도구 폴더 자체**(`go.mod`와 `vm-param-check` 실행 파일이 있는 곳)를 지정합니다. 안 주면 `/root/vm-param-check-usability-improvement/vm-param-check`.
- **빌드가 성공한 뒤에만** 배포 경로를 바꿉니다(임시 폴더에서 먼저 빌드). 실패하면 아무것도 안 바뀐 채 중단합니다.
- **덮어쓰는 파일은 백업**합니다: `<배포경로>.update_backup.<시각>/`. 되돌리려면 그 안의 파일을 같은 경로로 복사하면 됩니다.
- **직접 값을 채워 쓰는 템플릿**(`vm_setting_check_insert.sh`, `folder_setup.sh`, `testfiles/*`)은 배포본과 다르면 덮어쓰지 않고 `<이름>.new`로 새 버전만 옆에 둡니다. 필요하면 `diff`로 비교해서 직접 반영하세요.
- `01.*`, `vcenter.txt`, `SPEC_DIR/`, `*.csv`, `*.log`는 저장소에 같은 이름의 파일이 생겨도 절대 덮어쓰지 않습니다.
- **GitHub에 접속이 안 되는 서버**는 인터넷 되는 곳에서 `git clone --branch vm-param-check-standalone`으로 받은 폴더를 nfs/USB로 옮기고, 그 폴더를 소스로 지정하면 됩니다: `REPO_URL=file:///옮긴/폴더 bash update_deploy.sh /실제/배포/경로/vm-param-check`. (사내 미러 주소도 `REPO_URL`로 지정 가능, 브랜치를 바꾸려면 `REPO_BRANCH`.) 빌드는 `vendor/`만 쓰므로 오프라인으로 됩니다.

## 이 프로젝트에서 무엇이 바뀌었나

| 문제 | 해결 |
|---|---|
| 체크할 때마다 `-cpu`/`-cores`/`-numa`/`-mem`/`-disk`/`-shares-ev01`/`-ht`를 손으로 입력해야 함 | VM이 속한 vCenter 인벤토리 폴더 이름(예: `TST-CAE001-SAMP48c-QRST`)만으로 스펙 파일을 자동으로 찾아 옵션을 채우는 **`-specRoot` 폴더명 기반 스펙 자동매칭** 추가 |
| 신규 스펙을 만들 때마다 디렉터리·spec.txt를 손으로 작성 | **`-initFolder`/`-template`**로 스캐폴드 자동 생성(기존 스펙 복사 가능) |
| VM이 정식 CAE 폴더 규칙을 안 따르는 `Task`(임시) 폴더에 있으면 스펙을 못 찾음 | 포트그룹 이름에서 원래 폴더명을 유추 → 실패 시 대화형으로 직접 입력받는 **Task 폴더 예외 처리** 추가 |
| `-f`로 대상 VM 2대만 지정해도 인벤토리 전체 크기에 비례해서 느려짐 | 대상 이름만 가볍게 먼저 조회한 뒤, 그 VM들에 대해서만 무거운 속성을 조회하도록 **2단계 조회로 전환** — 3,000대 인벤토리 기준 실측 3.22초 → 0.20초 |
| 대상을 여러 vCenter에 나눠 지정했을 때 일부를 못 찾아도 조용히 넘어감 | 요청한 대상 중 못 찾은 게 있거나 vCenter 접속이 실패하면 **반드시 경고**하도록 추가(실행 초반·마지막 양쪽에 표시) |
| `-yes`가 실제 설정 변경 확인까지 건너뜀 | `-yes`는 스펙 자동매칭 확인만 생략하고, **`-fix`의 실제 변경 확인은 항상 물어보도록** 변경 |

## 사용법

전부 하위 [`vm-param-check/README.md`](./vm-param-check/README.md)에 있습니다. 특히 처음 `-specRoot`를 써보신다면 그 문서의 **"2-4. 폴더명 기반 스펙 자동매칭"** 절이 처음부터 끝까지 예시 명령어로 따라 할 수 있게 구성되어 있습니다.

빌드·설치(폐쇄망 오프라인 빌드 절차 포함)도 같은 문서의 "1. 빌드 및 설치 방법"을 참고하세요 — `vendor/`에 의존성이 전부 포함되어 있어 이번 개선으로 새로 추가된 의존성 없이 그대로 오프라인 빌드됩니다.

## 변경 이력

이 프로젝트에 매개변수/기능이 추가되거나 수정될 때마다의 기록은 [`CHANGELOG.md`](./CHANGELOG.md)에 날짜순으로 남깁니다.

## 설계 배경과 검증 근거

이 개선을 진행하며 확인한 사실(코드 근거), 결정 사항, vcsim/실 vCenter 검증 결과는 [`계획서.md`](./계획서.md)에 단계별로 정리되어 있습니다. 특히 "속도 문제"의 실제 원인이 어디였는지(0장), 폴더명 매칭 규칙이 실제 예시로 어떻게 검증됐는지(3단계), 다중 vCenter/`-yes` 관련 결정 배경(3-1·3-2단계) 등은 이 문서에서만 확인할 수 있습니다.

## 기존 개별 도구와의 관계

저장소의 `vm-param-setting-check/`(체크 전용, 구세대)와 `VM_setup/`는 삭제하지 않고 그대로 남겨 두었습니다. 이 프로젝트는 하위의 통합 도구(`vm-param-check`) 위에 사용성 개선을 얹은 것으로, 새로 시작하는 경우 이 폴더 아래의 도구만 쓰면 됩니다.

## 디렉토리 구조

```
vm-param-check-usability-improvement/
├── README.md                    # 이 문서
├── CHANGELOG.md                 # 날짜별 변경 이력
├── 계획서.md                      # 설계 배경/검증 근거 문서
├── update.sh                    # [폐쇄망] 업데이트 패키지의 바뀐 파일(실행 파일)만 사용 중인 디렉토리에 반영. 나머지는 건드리지 않음
├── make_update_package.sh       # 폐쇄망으로 가져갈 업데이트 패키지(update.sh + 실행 파일 + 체크섬)를 빌드 서버에서 만듦
├── update_deploy.sh             # [인터넷 되는 서버] 배포 경로를 사용자 파일은 그대로 두고 제자리 갱신 + 재빌드(독립 브랜치 vm-param-check-standalone에서 받음)
└── vm-param-check/              # 실제 도구 소스코드 (스펙 자동매칭, 2단계 조회 등 개선사항 포함), 상세는 하위 README 참고
    ├── README.md                 # 하위 도구 사용법 문서 (옵션 설명, 튜토리얼)
    ├── 계획서.md                   # 하위 도구 자체의 별도 설계 메모
    ├── main.go                   # CLI 진입점 (옵션 파싱, -specRoot 병합, 실행 흐름)
    ├── demo.go                   # -demo 모드 (합성 VM으로 동작만 확인, vCenter 미접속)
    ├── scaletest.go              # -scale 모드 (대량 인벤토리 성능 측정용)
    ├── setup.sh                  # vendor 패키지로 폐쇄망에서도 빌드하는 스크립트
    ├── folder_setup.sh           # -initFolder 대화형 래퍼 스크립트
    ├── vm_setting_check_insert.sh  # 체크/-fix 실행 래퍼 스크립트(이중 확인 포함)
    ├── real_test.sh              # 실 vCenter 대상 테스트 스크립트
    ├── scale_test.sh             # 스케일 테스트 스크립트
    ├── go.mod / go.sum           # Go 모듈 정의 파일
    ├── checker/                  # CPU/메모리/NUMA/affinity/전원정책/preferHT 등 점검 로직
    │   ├── hardware.go             # CPU/메모리/디스크/shares 체크
    │   ├── hardware_test.go
    │   ├── topology.go             # cores/NUMA 토폴로지 체크 (Auto 모드 예외 처리 포함)
    │   ├── affinity.go             # CPU affinity 체크
    │   ├── affinity_test.go
    │   ├── power.go                # 전원 정책 체크
    │   ├── preferht.go             # numa.vcpu.preferHT 체크
    │   └── fixed.go                # 고정값 체크 유틸
    ├── config/                   # 스펙 자동매칭(-specRoot)·폴더 스캐폴드(-initFolder)·대상 목록 로딩
    │   ├── spec.go                 # 폴더명 정규화 + _spec.txt 탐색/파싱
    │   ├── spec_test.go
    │   ├── init.go                 # -initFolder 스캐폴드 생성
    │   ├── init_test.go
    │   ├── portgroup.go            # Task 폴더 예외 처리(포트그룹명 파싱)
    │   ├── portgroup_test.go
    │   └── targets.go              # 대상 VM 목록 파일 로딩
    ├── fixer/                    # -fix 자동교정 계획 수립·적용 (동질성/전원OFF 게이트 포함)
    │   ├── plan.go                 # 교정 계획(diff) 생성
    │   ├── plan_test.go
    │   ├── apply.go                 # 워커풀 병렬 교정 적용
    │   ├── gates.go                 # 안전장치(그룹 동질성, 전원 OFF 확인)
    │   └── describe.go              # 교정 계획 dry-run 출력
    ├── model/                    # 체크 결과 등 공용 데이터 모델
    │   └── types.go                 # VMInfo 등 구조체 정의
    ├── report/                   # 콘솔/CSV 리포트 출력
    │   ├── console.go
    │   ├── csv.go
    │   └── summary.go
    ├── vcenter/                  # vCenter API 접속 클라이언트
    │   └── client.go                # FetchVMs(2단계 조회), 폴더/포트그룹 조회
    ├── testfiles/                # affinity 등 테스트용 샘플 파일
    │   ├── affinity-ev01.txt
    │   ├── affinity-ev02.txt
    │   ├── affinity-ev03.txt
    │   └── kdh.txt
    └── vendor/                   # 폐쇄망 오프라인 빌드용 Go 의존성 패키지 (문서화 대상 제외)
        ├── github.com/
        └── modules.txt
```
