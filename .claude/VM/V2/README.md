# V2 — VM 생성·설정(VMsetup) + 체크(vm-param-check), SPEC_DIR 공유

호스트(BM)당 VM을 **1~10대(ev01~ev10)** 만들고 설정하는 도구(`VMsetup`)와, 설정이 맞는지 체크·교정하는
도구(`vm-param-check-usability-improvement`)가 **스펙 폴더 `SPEC_DIR` 하나를 같이 쓰는** 구성입니다.
`.claude/VM/VM_setup`, `.claude/VM/vm-param-check-usability-improvement`(V1)는 그대로 두고 여기서 새로 구성했습니다.


⚠️ **주의사항 (Disclaimer)**
본 로그 분석 관련 스크립트 및 툴은 100% 신뢰하기보다는 참고용(보조 도구)으로 사용하는 것을 권장합니다. 설정 변경 스크립트의 경우에는 설정변경후 랜덤한 서버 몇개를 확인해서 실제로 변경되었는지 확인하는 절차가 반드시 필요합니다.

## 1. 빌드 및 설치 방법

의존성(govmomi)은 `govendor/`에 들어 있어 **이 V2 폴더만 내려받으면 인터넷 없이 빌드**됩니다.
각 도구의 `setup.sh`가 `../../govendor/<버전>`을 `vendor`로 링크하고 `-mod=vendor`로 빌드합니다.

```bash
# 서버 배치: V2 안의 폴더를 /home 아래에 그대로 둔다
#   /home/SPEC_DIR  /home/VMsetup  /home/vm-param-check-usability-improvement  /home/govendor
cp -r V2/* /home/

cd /home/VMsetup
for d in *-source; do (cd "$d" && bash setup.sh); done
cd /home/vm-param-check-usability-improvement/vm-param-check && bash setup.sh
```

요구사항: Go 1.26.5 이상(VMsetup), Linux(Rocky Linux 8에서 검증).

## 2. 사용 방법

각 도구의 사용법은 하위 README를 참고하세요. 공통 준비물:

- `SPEC_DIR/vswitch_${user}.txt` — `BM 포트그룹 VLAN` (BM당 여러 줄 가능, 예시: `SPEC_DIR/vswitch_user.txt.example`)
- `SPEC_DIR/<CAE폴더명>/<CAE폴더명>_spec.txt` — ev01~ev10 스펙 (예시: `SPEC_DIR/TST-CAE001-SAMP48c-QRST/`)
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
