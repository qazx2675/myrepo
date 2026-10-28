# PCI 패스스루 변경분만 적용하기 (2026-10-28)

이미 V2(VMsetup + vm-param-check)를 설치해 쓰는 서버에 **이번 PCI 패스스루 변경분만** 반영하는 방법입니다.
V2 폴더 전체를 다시 배치할 수 있으면 그게 더 간단합니다(`README.md` 1장). 이 문서는 그럴 수 없을 때를 위한 것입니다.

⚠️ **주의사항 (Disclaimer)**
본 스크립트 및 툴은 100% 신뢰하기보다는 참고용(보조 도구)으로 사용하는 것을 권장합니다. PCI 패스스루 단계는 VM 전원을 끄고 켜며 호스트 패스스루 설정을 바꿉니다. 적용 후 무작위로 VM 몇 대를 골라 장치가 붙어 있고 게스트에서 장치가 보이는지 직접 확인하세요.

## 1. 대상

- 폐쇄망 서버, **Go 가 없어도 됨** (OS8 은 `bin_os8/`, OS6 은 `bin_os6/` 의 미리 빌드한 실행파일을 씀)
- 이미 설치된 V2 가 **2026-10-19 버전**이어야 합니다. 확인:

  ```bash
  V2=/home/V2      # 이 서버의 V2 설치 위치 (다르면 바꾸세요)
  ls "$V2/vm-param-check-usability-improvement/vm-param-check/01.vm_setting_check_insert.sh"   # 있어야 함
  grep -c pci_passthrough "$V2/setup.sh"                                                         # 0 이어야 함 (아직 적용 전)
  ```

  첫 줄의 파일이 없으면 그보다 오래된 버전입니다. 그때는 이 문서 대신 V2 폴더 전체를 새로 배치하세요(바뀐 것이 더 많음).
- 이 문서는 **V2 안의** vm-param-check 사본을 바꿉니다. V2 와 따로 설치한 vm-param-check 단독 배포본(`vm-param-check-standalone` 브랜치, `update_deploy.sh`/`make_update_package.sh` 로 갱신하는 것)은 이번에 바뀌지 않았으므로 그쪽 갱신 도구는 쓰지 않습니다.

## 2. 바뀐 파일

경로는 모두 V2 폴더 기준입니다.

**동작에 필요한 것 (반드시 복사)**

| 파일 | 구분 | 내용 |
|---|---|---|
| `VMsetup/vm_setup.sh` | 수정 | lpage 다음 "PCI 패스스루" 단계, 스펙 `pci-evNN` 읽기, 실행 계획 표시, 재부팅 뒤 이어하기 스크립트 |
| `VMsetup/pci_passthrough-source/` (`main.go`, `setup.sh`, `go.mod`, `go.sum`, `README.md`, `.gitignore`) | 신규 폴더 | PCI 패스스루 도구 소스와 설명 |
| `VMsetup/pci_passthrough-source/pci_passthrough` | 신규 실행파일 | OS8: `bin_os8/pci_passthrough.gz`, OS6: `bin_os6/pci_passthrough.gz` 를 풀어서 둠 |
| `vm-param-check-usability-improvement/vm-param-check/vm-param-check` | 실행파일 교체 | OS8: `bin_os8/vm-param-check.gz`, OS6: `bin_os6/vm-param-check.gz` 를 풀어서 둠 (`pci-evNN` 스펙 키와 "PCI 패스스루" 체크) |
| `setup.sh` | 수정 | 대상 목록에 `pci_passthrough` 추가. `vm_setup.sh` 는 실행파일이 없으면 이 스크립트로 준비하므로 함께 바꿔야 함 |
| `bin_os6/pci_passthrough.gz` (신규), `bin_os6/vm-param-check.gz` (교체) | OS6 실행파일 | OS6 서버의 `setup.sh` 가 여기서 풂. OS8 서버도 폴더를 같게 맞추려면 복사 |
| `bin_os8/pci_passthrough.gz`, `bin_os8/vm-param-check.gz` | 신규 | OS8 실행파일 (Go 1.26.5, 정적 빌드) |

**소스·문서 (동작에는 영향 없음, 폴더를 저장소와 같게 맞추려면 복사)**

| 파일 | 구분 |
|---|---|
| `vm-param-check-usability-improvement/vm-param-check/main.go`, `model/types.go`, `vcenter/client.go`, `README.md` | 수정 (위 실행파일의 소스) |
| `vm-param-check-usability-improvement/vm-param-check/checker/pci.go` | 신규 (소스) |
| `vm-param-check-usability-improvement/vm-param-check/checker/pci_test.go`, `config/pci_test.go`, `main_test.go` | 신규 (단위 테스트) |
| `vm-param-check-usability-improvement/CHANGELOG.md` | 수정 |
| `VMsetup/README.md`, `VMsetup/CHANGELOG.md`, `VMsetup/workflow.mmd` | 수정 |
| `README.md`, `WORKFLOW.md`, `ARCHITECTURE.md`, `UPDATE_PCI_PASSTHROUGH.md`(이 문서), `build_os6.sh`, `.gitignore` | 수정/신규 |
| `SPEC_DIR/TST-CAE001-SAMP48c-QRST/TST-CAE001-SAMP48c-QRST_spec.txt` | 예시 스펙에 `pci-evNN` 주석 예시만 추가. **서버의 SPEC_DIR 은 덮어쓰지 마세요** |

실행파일 체크섬(전송 중 깨졌는지 확인용, `sha256sum <파일>`):

| 파일 | SHA-256 | 크기(바이트) |
|---|---|---|
| `bin_os8/pci_passthrough.gz` | `abec75c4e67c91bcbdfd315568d03fda2dc6f6ffb562bc6c689d096bc31c197c` | 4321148 |
| `bin_os8/vm-param-check.gz` | `9044f7373a258851459815253ac5eecb6aac92796e5ccfa9046d322bf46eb230` | 4339256 |
| `bin_os6/pci_passthrough.gz` | `d045305c0430335dc56688580978909c308c8c382f5b5d42d3671c610d181594` | 3680727 |
| `bin_os6/vm-param-check.gz` | `2c658ccbb37905570867b1f1b11749d24725241f0443ec461e74e387ea95e46b` | 3689938 |

## 3. 적용 절차

새 V2 폴더(저장소에서 받은 것)를 서버의 임시 위치에 풀어 둔 상태에서 시작합니다.

```bash
V2=/home/V2                       # 서버에 설치된 V2
NEW=/tmp/V2_new                   # 새로 받은 V2 폴더 (USB 등으로 옮겨 풀어 둔 곳)
BK=/home/V2.backup_pci_$(date +%Y%m%d_%H%M)
```

### 3-1. 백업 (되돌리기용)

```bash
mkdir -p "$BK" && cd "$V2" && tar cf "$BK/before.tar" --ignore-failed-read \
  VMsetup/vm_setup.sh setup.sh build_os6.sh .gitignore README.md WORKFLOW.md ARCHITECTURE.md \
  VMsetup/README.md VMsetup/CHANGELOG.md VMsetup/workflow.mmd bin_os6/vm-param-check.gz \
  vm-param-check-usability-improvement/CHANGELOG.md \
  vm-param-check-usability-improvement/vm-param-check/{vm-param-check,main.go,README.md,model/types.go,vcenter/client.go}
tar tf "$BK/before.tar"           # 백업된 파일 목록 확인
```

`vm_setup.sh` 를 서버에서 고쳐 쓰고 있었다면(예: `# 숫자변경기능` 아래 `ask_cae_number` 를 주석 처리) 덮어쓰기 전에 차이를 보고, 복사한 뒤 같은 수정을 다시 하세요.

```bash
diff "$V2/VMsetup/vm_setup.sh" "$NEW/VMsetup/vm_setup.sh" | less
```

### 3-2. 체크섬 확인

```bash
cd "$NEW" && sha256sum bin_os8/*.gz bin_os6/pci_passthrough.gz bin_os6/vm-param-check.gz   # 위 표와 같아야 함
```

### 3-3. 파일 복사 (OS8·OS6 공통)

```bash
cd "$NEW"
cp -f VMsetup/vm_setup.sh "$V2/VMsetup/vm_setup.sh"
cp -rf VMsetup/pci_passthrough-source "$V2/VMsetup/"
cp -f setup.sh build_os6.sh .gitignore README.md WORKFLOW.md ARCHITECTURE.md UPDATE_PCI_PASSTHROUGH.md "$V2/"
cp -f VMsetup/README.md VMsetup/CHANGELOG.md VMsetup/workflow.mmd "$V2/VMsetup/"
mkdir -p "$V2/bin_os8" && cp -f bin_os8/*.gz "$V2/bin_os8/"
cp -f bin_os6/pci_passthrough.gz bin_os6/vm-param-check.gz "$V2/bin_os6/"
C=vm-param-check-usability-improvement/vm-param-check
cp -f vm-param-check-usability-improvement/CHANGELOG.md "$V2/vm-param-check-usability-improvement/"
cp -f $C/main.go $C/main_test.go $C/README.md "$V2/$C/"
cp -f $C/model/types.go "$V2/$C/model/"; cp -f $C/vcenter/client.go "$V2/$C/vcenter/"
cp -f $C/checker/pci.go $C/checker/pci_test.go "$V2/$C/checker/"; cp -f $C/config/pci_test.go "$V2/$C/config/"
chmod +x "$V2/VMsetup/vm_setup.sh" "$V2"/*.sh "$V2/VMsetup/pci_passthrough-source/setup.sh"
```

### 3-4. 실행파일 놓기

**OS8 (Rocky/RHEL 8, Go 없음)** — `bin_os8/` 를 풀어서 제자리에 둡니다.

```bash
cd "$V2"
gzip -dc bin_os8/pci_passthrough.gz > VMsetup/pci_passthrough-source/pci_passthrough
gzip -dc bin_os8/vm-param-check.gz  > vm-param-check-usability-improvement/vm-param-check/vm-param-check
chmod +x VMsetup/pci_passthrough-source/pci_passthrough vm-param-check-usability-improvement/vm-param-check/vm-param-check
```

OS8 에서 `bash setup.sh` 는 쓰지 마세요 — OS8 이면 Go 로 빌드하려 하므로 Go 가 없으면 실패합니다(`bin_os8/` 은 읽지 않음).
OS8 서버에 Go 1.26.5 이상이 있으면 위 대신 `bash setup.sh pci_passthrough vm-param-check` 로 소스에서 빌드해도 됩니다(오프라인, 결과 동작은 같음).

**OS6 (RHEL/CentOS 6)** — `setup.sh` 가 `bin_os6/` 에서 풀어 줍니다(OS6 는 자동 판단).

```bash
cd "$V2" && bash setup.sh pci_passthrough vm-param-check
# 자동 판단이 안 되는 호스트면: bash setup.sh --os6 pci_passthrough vm-param-check
```

같은 일을 손으로 하려면 OS8 명령에서 `bin_os8` 만 `bin_os6` 으로 바꾸면 됩니다.

## 4. 확인

```bash
cd "$V2"
file VMsetup/pci_passthrough-source/pci_passthrough       # ELF 64-bit ... statically linked
./VMsetup/pci_passthrough-source/pci_passthrough -h 2>&1 | head -5   # -ev01Pci, -mmioSizeGB 등이 보여야 함
./vm-param-check-usability-improvement/vm-param-check/vm-param-check -h 2>&1 | grep pci    # -pci-ev01, -pci-ev02~99 두 줄
```

스펙 내보내기에 `pci-evNN` 이 나오는지 (스펙에 `pci-ev01=...` 를 넣은 뒤, 아래 5장):

```bash
cd "$V2/vm-param-check-usability-improvement/vm-param-check"
./vm-param-check -specRoot=../../SPEC_DIR -specExport=<스펙 폴더명> | grep pci     # 예: pci-ev01=mellanox
# vm-param-check 폴더에 SPEC_DIR 을 복원해 쓰는 서버면 -specRoot=./SPEC_DIR
```

`vm_setup.sh` 실행 계획에 PCI 줄이 나오는지 (vCenter 변경 없음):

```bash
cd "$V2/VMsetup" && ./vm_setup.sh -u <user> -v <vCenter IP> -n
```

평소처럼 OS 버전·인프라·스펙 확인 질문에 답하면, 스펙별 실행 계획의 `lpage_setting` 아래에 다음 줄이 나옵니다.

```
   pci_passthrough (패스스루 활성화·VM 연결·전원 ON — VM 은 켜 둠, lpage 다음)
     ev01: Pci=mellanox
```

`pci-*` 가 없는 스펙은 `PCI 패스스루: 스펙에 pci-* 없음 — 건너뜀` 으로 나옵니다.

## 5. 스펙에 PCI 패스스루 넣기

`SPEC_DIR/<스펙 폴더>/<스펙 폴더>_spec.txt` 에 ev 별로 한 줄씩 추가합니다.

```
pci-ev01=mellanox
pci-ev02=nvidia
```

- 값: `mellanox`, `nvidia`, VendorId(10진 `5555`, 16진 `15b3`/`0x15b3` — Intel 은 `0x8086`, `8086` 은 10진으로 해석됨), 그 밖의 값은 장치 이름 일부(대소문자 무시, 값에 공백 불가).
- 그 ev 의 `cpu/mem/disk/shares-evNN` 이 스펙에 있어야 합니다(없으면 `-specExport` 오류).
- ev 마다 장치(PCI function) 1개를 PCI 주소 순서로 줍니다. 호스트 관리망 NIC(vmnic)와 다른 VM 이 쓰는 장치는 배분하지 않습니다.
- NVIDIA(GPU)는 VM 펌웨어가 EFI 여야 합니다(`vm_create` 기본값). BIOS VM 이면 그 VM 은 실패로 남고 아무것도 바꾸지 않습니다.
- VM 은 연결 뒤 **전원이 켜진 채로** 남습니다.
- 호스트 재부팅이 필요한 장치가 있으면 `vm_setup.sh` 가 경고 후 계속하고 `VMsetup/run_<user>/pci_resume_<번호>.sh` 를 남깁니다. 그 호스트를 재부팅한 뒤 `bash VMsetup/run_<user>/pci_resume_<번호>.sh` 로 이어서 연결·전원 ON 합니다.
- 자세한 동작·옵션·전원 ON 실패 진단: `VMsetup/pci_passthrough-source/README.md`, `VMsetup/README.md` 의 "PCI 패스스루".

## 6. 되돌리기

```bash
cd "$V2" && tar xf "$BK/before.tar"                      # 백업한 파일을 원래 내용으로
rm -rf VMsetup/pci_passthrough-source bin_os8 bin_os6/pci_passthrough.gz UPDATE_PCI_PASSTHROUGH.md
rm -f vm-param-check-usability-improvement/vm-param-check/checker/pci.go \
      vm-param-check-usability-improvement/vm-param-check/checker/pci_test.go \
      vm-param-check-usability-improvement/vm-param-check/config/pci_test.go \
      vm-param-check-usability-improvement/vm-param-check/main_test.go
```

되돌린 뒤에는 스펙의 `pci-evNN` 줄도 지우거나 주석(`#`) 처리하세요. 예전 vm-param-check 는 `pci-evNN` 을 모르는 옵션으로 보고 중단합니다.
이미 VM 에 붙인 패스스루 장치와 켜 둔 호스트 패스스루 설정은 되돌리기로 바뀌지 않습니다(필요하면 vCenter 에서 VM 의 PCI 장치를 빼고, 호스트 > 구성 > PCI 디바이스에서 패스스루를 끕니다).

## 7. 실행파일을 다시 만들 때 (빌드 서버)

- `bin_os6/`: Go 1.20 서버(.60)에서 `bash build_os6.sh` (전체 13개를 만들지만, 소스가 그대로인 도구는 바이트까지 같게 나오므로 바뀐 것만 커밋).
- `bin_os8/`: Go 1.26.5 서버(.58)에서 도구 폴더마다 `bash setup.sh`(vendor 링크) 뒤 아래처럼 빌드해 `gzip -9 -n`. 같은 소스·같은 Go 면 바이트까지 같게 나옵니다.

  ```bash
  GOTOOLCHAIN=local CGO_ENABLED=0 GOFLAGS=-mod=vendor GOPROXY=off GOSUMDB=off \
    go build -trimpath -ldflags "-s -w" -o pci_passthrough .     # vm-param-check 도 같은 방식
  gzip -9 -n -c pci_passthrough > ../../bin_os8/pci_passthrough.gz
  ```

  (도구별 `setup.sh` 의 일반 빌드는 `-trimpath -ldflags "-s -w"` 를 쓰지 않아 파일 크기만 다르고 동작은 같습니다.)
