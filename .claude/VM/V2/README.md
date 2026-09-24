# V2 — VM 생성·설정(VMsetup) + 체크(vm-param-check), SPEC_DIR 공유

호스트(BM)당 VM을 **1~99대(ev01~ev99)** 만들고 설정하는 도구(`VMsetup`)와, 그 설정이 맞는지 체크·교정하는
도구(`vm-param-check-usability-improvement`)가 **스펙 폴더 `SPEC_DIR` 하나를 함께 쓰도록** 묶은 구성입니다.
기존 `.claude/VM/VM_setup`, `.claude/VM/vm-param-check-usability-improvement`(V1)는 그대로 두고, V2는 이 폴더에 새로 구성했습니다.

> 작업 흐름은 [WORKFLOW.md](WORKFLOW.md), 폴더·파일별 역할은 [ARCHITECTURE.md](ARCHITECTURE.md)를 참고하세요.

⚠️ **주의사항 (Disclaimer)**
본 로그 분석 관련 스크립트 및 툴은 100% 신뢰하기보다는 참고용(보조 도구)으로 사용하는 것을 권장합니다. 설정 변경 스크립트의 경우, 설정 변경 후 무작위로 서버 몇 대를 골라 실제로 변경되었는지 직접 확인하는 절차가 반드시 필요합니다.

## 1. 빌드 및 설치 방법

의존성(govmomi)이 `govendor/`에 들어 있으므로 **V2 폴더만 내려받으면 인터넷 없이 빌드**할 수 있습니다. 필요한 것은 **Go 1.26.5 이상과 bash**뿐입니다(모듈 프록시·git·인터넷 불필요).

```bash
# V2 폴더를 통째로 둔다 (예: /home/V2). 폴더 안의 상대 위치로 서로를 찾는다.
cp -r V2 /home/V2 && cd /home/V2

# 전체 바이너리 준비 — OS8: 오프라인 빌드 / OS6: bin_os6/ 의 미리 빌드한 실행파일을 제자리에 푼다(자동 판단)
bash setup.sh

# vCenter 계정 비밀번호를 암호화해 등록 (한 번, 비밀번호가 바뀌면 다시)
./passwd_update.sh                 # 계정 lscsystems@vsphere.local (다른 계정: -id <계정>)
./passwd_update.sh -esxi           # ESXi root (필요할 때)
```

`setup.sh` 사용법:

| 명령 | 동작 |
|---|---|
| `bash setup.sh` | 11개 도구 전체 빌드 후 성공/실패 요약 (하나라도 실패하면 종료코드 1). OS6 면 빌드 대신 `bin_os6/*.gz` 를 푼다 |
| `bash setup.sh vm_create nic_assign` | 지정한 도구만 |
| `bash setup.sh --os6` | OS 와 상관없이 `bin_os6/` 실행파일을 설치 |
| `bash setup.sh -l` | 빌드 대상과 실행파일 경로 목록 |
| `bash setup.sh -c` | 빌드된 실행파일과 vendor 링크 정리 |

**오프라인 보장** — `GOPROXY=off`, `GOFLAGS=-mod=vendor`, `GOTOOLCHAIN=local`, `GOSUMDB=off`를 강제하므로 모듈이나 새 Go 툴체인을 내려받으려 하지 않습니다. Go가 없거나, 버전이 낮거나, `govendor/`가 없으면 빌드 전에 이유를 알려 주고 멈춥니다. 도구별 `setup.sh`(`*-source/setup.sh`, `vm-param-check/setup.sh`)도 그대로 있으므로 도구 하나만 따로 빌드할 수도 있습니다.

요구사항: OS8(Rocky/RHEL 8) — Go 1.26.5 이상 + bash. OS6(RHEL/CentOS 6) — Go 불필요(`bin_os6/`, Go 1.20 정적 빌드), bash 4.1, openssl.

**OS6 실행파일 다시 만들기** (소스가 바뀌었을 때, Go 1.20 이 있는 빌드 서버에서): `bash build_os6.sh [/opt/go1.20/bin/go]` → `bin_os6/<이름>.gz`.
RHEL 6 커널(2.6.32)은 Go 1.21+ 실행파일을 못 띄워서, govmomi vendor 사본을 Go 1.20 호환으로 고쳐 빌드한다(원본 `govendor/` 는 그대로).

### 비밀번호 (암호 파일)

- `passwd_update.sh` 가 `secret/key`(처음 한 번 생성, 600) 로 `secret/vcenter_<계정>.enc`, `secret/esxi_<계정>.enc` 를 만든다
  (`openssl enc -aes-256-cbc -md sha256` — OS6 의 openssl 1.0.1 과 OS8 의 1.1.1 이 같은 파일을 푼다).
- `vm_setup.sh`, `vm-param-check/vm_setting_check_insert.sh` 는 환경변수 → 암호 파일 → 직접 입력 순으로 비밀번호를 얻는다.
- 다른 서버로 옮길 때는 `secret/` 폴더(키 포함)를 그대로 복사한다. `./passwd_update.sh -list` 로 풀리는지 확인.
- 키 파일이 있으면 누구든 풀 수 있다 — 파일 권한(root 전용)으로 지키고 저장소에는 올리지 않는다(`.gitignore`).

### 기존 vm-param-check 의 `vcenter.txt`, `SPEC_DIR` 를 그대로 쓰기

예전 vm-param-check 배포 폴더의 `vcenter.txt`, `SPEC_DIR/` 를 `V2/vm-param-check-usability-improvement/vm-param-check/` 에 그대로 복원하면
체크(vm-param-check)는 바로 되고, VM 생성(vm_setup.sh)도 그 두 파일을 찾아 쓴다. 추가로 필요한 것:

1. `VMsetup/<user>.txt` (BM 목록), `VMsetup/vswitch_<user>.txt` (BM 포트그룹 VLAN) — V2 에서 새로 생긴 파일
2. `./passwd_update.sh` 로 비밀번호 등록 (안 하면 실행할 때 물어본다)
3. 예전 스펙에는 `affinity-evNN` 줄이 없다(ev01 affinity 는 자동 계산이었음) — vm_setup.sh 가 처음 쓸 때 "지금 ev 별 affinity 를 골라
   이 스펙에 추가할까요?" 라고 묻고, 고른 파일을 스펙에 적어 둔다 (한 번만)

SPEC_DIR 을 찾는 순서: `-s` → `vm-param-check/SPEC_DIR` (있으면) → `V2/SPEC_DIR`. vcenter.txt: `V2/vcenter.txt` → `vm-param-check/vcenter.txt`.

## 2. 사용 방법

도구별 사용법은 하위 README를 참고하세요. 공통으로 준비할 파일은 다음과 같습니다.

| 파일 | 내용 |
|---|---|
| `VMsetup/vswitch_${user}.txt` | `BM 포트그룹 VLAN` (BM당 여러 줄 가능, 예시: `VMsetup/vswitch_user.txt.example`) |
| `SPEC_DIR/<CAE폴더명>/<CAE폴더명>_spec.txt` | ev01~ev99 스펙 (예시: `SPEC_DIR/TST-CAE001-SAMP48c-QRST/`). CAE 번호는 빼고 매칭한다(`SAC-CAE001` 스펙을 `SAC-CAE100` 포트그룹으로 실행해도 같은 스펙) |
| `VMsetup/${user}.txt` | BM 목록 (한 줄에 하나). `vm_setup.sh` 를 `-u` 없이 실행하면 이 파일들을 번호로 보여준다 |

```bash
cd /home/V2/VMsetup
./vm_setup.sh                 # user 번호 선택 → VM 표 확인 → (CAE 번호 변경) → vCenter 선택 → 실행 → 스펙 체크
./vm_setup.sh -u lsh -n       # 계획만 확인
./vm_setup.sh -u lsh -id other@vsphere.local
```

스펙이 VM 생성용 값으로 어떻게 바뀌는지 미리 확인하려면:

```bash
cd /home/vm-param-check-usability-improvement/vm-param-check
./vm-param-check -specRoot=../../SPEC_DIR -specExport=TST-CAE001-SAMP48c-QRST
```

## 3. 옵션별 상세 설명

- `VMsetup/README.md`, `VMsetup/*-source/README.md` — VM 생성·설정 도구별 옵션
- `vm-param-check-usability-improvement/vm-param-check/README.md` — 체크·교정 옵션
- 변경 이력: `VMsetup/CHANGELOG.md`, `vm-param-check-usability-improvement/CHANGELOG.md`

## 4. 문서별 고유 설명

```
V2/
├── README.md                              # 이 문서
├── WORKFLOW.md / workflow.svg             # 전체 작업 흐름도 (mermaid / SVG)
├── ARCHITECTURE.md / PR_CHECKLIST.md      # 폴더별 역할 / 수정·배포 전 체크리스트
├── setup.sh                               # 전체 오프라인 빌드 (OS6 면 bin_os6/ 설치)
├── build_os6.sh / bin_os6/                # OS6 용 실행파일 빌드 스크립트 / 미리 빌드한 실행파일(.gz)
├── passwd_update.sh / secret_lib.sh       # 비밀번호 암호화 등록·갱신 / 풀기 함수 (secret/ 는 git 제외)
├── govendor/                              # 공유 의존성 (govmomi 0.55.1 = VMsetup, 0.39.0 = vm-param-check)
├── SPEC_DIR/                              # 공유 스펙 (git에는 예시만)
├── VMsetup/                               # VM 생성·설정 도구 (vm_create, affinity, lpage, tag, vswitch, nic_assign ...)
└── vm-param-check-usability-improvement/  # 체크·교정 도구 (-specRoot, -specExport)
```

## 5. 전역 명령어로 사용하기 (선택 사항)

빌드된 실행 파일을 PATH에 포함된 디렉터리로 복사하거나, 실행 파일이 있는 경로를 PATH에 추가하면 어디서든 명령어처럼 쓸 수 있습니다.

```bash
sudo cp /home/V2/VMsetup/vm_create-source/vm_create /home/V2/VMsetup/nic_assign-source/nic_assign /usr/local/bin/
```
