# V2 — VM 생성·설정(VMsetup) + 체크(vm-param-check), SPEC_DIR 공유

호스트(BM)당 VM을 **1~99대(ev01~ev99)** 만들고 설정하는 도구(`VMsetup`)와, 설정이 맞는지 체크·교정하는
도구(`vm-param-check-usability-improvement`)가 **스펙 폴더 `SPEC_DIR` 하나를 같이 쓰는** 구성입니다.
`.claude/VM/VM_setup`, `.claude/VM/vm-param-check-usability-improvement`(V1)는 그대로 두고 여기서 새로 구성했습니다.


⚠️ **주의사항 (Disclaimer)**
본 로그 분석 관련 스크립트 및 툴은 100% 신뢰하기보다는 참고용(보조 도구)으로 사용하는 것을 권장합니다. 설정 변경 스크립트의 경우에는 설정변경후 랜덤한 서버 몇개를 확인해서 실제로 변경되었는지 확인하는 절차가 반드시 필요합니다.

## 1. 빌드 및 설치 방법

의존성(govmomi)은 `govendor/`에 들어 있어 **이 V2 폴더만 내려받으면 인터넷 없이 빌드**됩니다. 필요한 것은 **Go 1.26.5 이상과 bash 뿐**입니다(모듈 프록시·git·인터넷 불필요).

```bash
# 서버 배치: 네 폴더를 같은 부모 폴더 아래에 나란히 둔다 (README.md 등은 복사하지 않아도 됨)
#   /home/govendor  /home/SPEC_DIR  /home/VMsetup  /home/vm-param-check-usability-improvement
cp -r V2/govendor V2/SPEC_DIR V2/VMsetup V2/vm-param-check-usability-improvement V2/setup.sh /home/

# 전체 바이너리 빌드 (오프라인)
cd /home && bash setup.sh
```

`setup.sh` 사용법:

| 명령 | 동작 |
|---|---|
| `bash setup.sh` | 11개 도구 전체 빌드 후 성공/실패 요약 (하나라도 실패하면 종료코드 1) |
| `bash setup.sh vm_create nic_assign` | 지정한 도구만 빌드 |
| `bash setup.sh -l` | 빌드 대상과 실행파일 경로 목록 |
| `bash setup.sh -c` | 빌드된 실행파일과 vendor 링크 정리 |

오프라인 보장: `GOPROXY=off`, `GOFLAGS=-mod=vendor`, `GOTOOLCHAIN=local`, `GOSUMDB=off`를 강제하므로 모듈이나 새 Go 툴체인을 내려받으려 시도하지 않습니다. Go가 없거나 버전이 낮거나 `govendor/`가 없으면 빌드 전에 이유를 알려주고 멈춥니다. 각 도구의 `setup.sh`(`*-source/setup.sh`, `vm-param-check/setup.sh`)는 그대로 있어서 도구 하나만 직접 빌드할 수도 있습니다.

요구사항: Go 1.26.5 이상, Linux(Rocky Linux 8에서 검증).

## 2. 사용 방법

각 도구의 사용법은 하위 README를 참고하세요. 공통 준비물:

- `SPEC_DIR/vswitch_${user}.txt` — `BM 포트그룹 VLAN` (BM당 여러 줄 가능, 예시: `SPEC_DIR/vswitch_user.txt.example`)
- `SPEC_DIR/<CAE폴더명>/<CAE폴더명>_spec.txt` — ev01~ev99 스펙 (예시: `SPEC_DIR/TST-CAE001-SAMP48c-QRST/`)
- `VMsetup/${user}.txt` — BM 목록 (한 줄에 하나)

스펙을 VM 생성용 값으로 확인하려면:

```bash
cd /home/vm-param-check-usability-improvement/vm-param-check
./vm-param-check -specRoot=../../SPEC_DIR -specExport=TST-CAE001-SAMP48c-QRST
```

## 3. 옵션별 상세 설명

- `VMsetup/README.md`, `VMsetup/*-source/README.md` — VM 생성/설정 도구별 옵션
- `vm-param-check-usability-improvement/vm-param-check/README.md` — 체크/교정 옵션
- 변경 이력: `VMsetup/CHANGELOG.md`, `vm-param-check-usability-improvement/CHANGELOG.md`

## 4. 문서별 고유 설명

```
V2/
├── README.md                              # 이 문서
├── govendor/                              # 공유 의존성 (govmomi 0.55.1 = VMsetup, 0.39.0 = vm-param-check)
├── SPEC_DIR/                              # 공유 스펙 (git에는 예시만)
├── VMsetup/                               # VM 생성·설정 도구 (vm_create, affinity, lpage, tag, vswitch, nic_assign ...)
└── vm-param-check-usability-improvement/  # 체크·교정 도구 (-specRoot, -specExport)
```

## 5. 전역 명령어로 사용하기 (선택 사항)

빌드된 실행 파일을 PATH에 포함된 디렉터리로 복사하거나, 실행 파일이 있는 경로를 PATH에 추가하면 어디서든 명령어처럼 사용할 수 있습니다.

```bash
sudo cp /home/VMsetup/vm_create-source/vm_create /home/VMsetup/nic_assign-source/nic_assign /usr/local/bin/
```
