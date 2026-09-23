# VM_setup — VM 설정 적용 스크립트 모음

VM/ESXi/vCenter의 개별 설정(affinity, lpage/HugePage, 전원정책, 태그, vSwitch, 라이선스 할당, VM 생성 등)을
적용하는 도구를 모아 둔 폴더입니다. 각 도구는 자체 소스와 `vendor/`를 갖추고 있어 따로따로 빌드할 수 있습니다.

이 중 `vm-param-fix/`(체크 CSV 기반 오케스트레이터)는 같은 기능을 자체 내장한
`../vm-param-check-usability-improvement/vm-param-check/`(`-fix` 옵션, 외부 도구 불필요)로 대체되었으므로,
새로 시작한다면 그쪽을 쓰는 것을 권장합니다. 나머지 개별 도구(affinity/lpage/tag/vswitch/license/VM 생성 등)는
지금도 그대로 쓸 수 있습니다.

> **ev01~ev99 확장판은 [`../V2/VMsetup`](../V2/VMsetup/)에 있습니다.** V2는 이 폴더를 바탕으로 `vm_setup.sh`(스펙·포트그룹 자동 할당 → 생성 → 설정)와 `nic_assign`을 더하고, 스펙 폴더(`SPEC_DIR`)를 vm-param-check와 공유하도록 새로 구성한 버전입니다. 이 폴더(V1)는 기록 보존을 위해 그대로 둡니다.
> 작업 흐름은 [WORKFLOW.md](WORKFLOW.md), 폴더별 역할은 [ARCHITECTURE.md](ARCHITECTURE.md)를 참고하세요.

⚠️ **주의사항 (Disclaimer)**
본 로그 분석 관련 스크립트 및 툴은 100% 신뢰하기보다는 참고용(보조 도구)으로 사용하는 것을 권장합니다. 설정 변경 스크립트의 경우, 설정 변경 후 무작위로 서버 몇 대를 골라 실제로 변경되었는지 직접 확인하는 절차가 반드시 필요합니다.

## 1. 빌드 및 설치 방법

### 빌드

| 폴더 | 내용 |
|---|---|
| `vm-param-fix/` | vm-param-check가 낸 CSV를 태그(affinity/lpage/power)별로 분류해 아래 3개 외부 바이너리를 호출하는 오케스트레이터. 소스+`vendor/` 포함, `setup.sh`로 빌드 |
| `affinity_setting-source/` | affinity 태그를 담당하는 외부 도구의 실제 소스(원래 `/root/affinity-test/main.go`). 소스+`vendor/` 포함, `setup.sh`로 빌드 |
| `lpage_setting-source/` | lpage(HugePage/CPU 토폴로지) 태그를 담당하는 외부 도구의 실제 소스(원래 `/root/lpage-test/main.go`). 소스+`vendor/` 포함, `setup.sh`로 빌드 |
| `vm-param-fix/power_setting` | **호스트 고성능 전원정책 자동교정 도구. 컴파일된 바이너리만 존재**합니다. 아래 "4.2 power_setting에 대한 중요 안내" 참고 |

```bash
cd "affinity_setting-source" && ./setup.sh   # -> affinity_setting 바이너리 생성
cd "../lpage_setting-source" && ./setup.sh   # -> lpage_setting 바이너리 생성
cd "../vm-param-fix" && ./setup.sh           # -> vm-param-fix 바이너리 생성 (power_setting은 이미 포함되어 있음)
```

그 밖의 도구(`license_assign`, `mac_info`, `main_conn`, `tag_setting`, `vm_create`, `vswitch_setting`, `numa_preferht_setting`)도 각 `*-source/` 폴더에서 `bash setup.sh`로 빌드합니다.

## 2. 사용 방법

### vm-param-fix (참고용 — 새 프로젝트에는 비권장)

세 도구를 vm-param-fix와 같은 디렉토리에 모아 두고 실행합니다.

```bash
VC_PASSWORD='<비밀번호>' ./vm-param-fix -checkResult=<체크CSV> -vcTargetIP=<vCenter> -id=<계정> \
  -affinityTool=./affinity_setting -lpageTool=./lpage_setting -powerTool=./power_setting \
  -recheckTool=<vm-param-check 경로>
```

개별 도구의 사용법은 각 `*-source/README.md`를 참고하세요.

## 3. 옵션별 상세 설명

도구별 옵션은 하위 디렉토리의 `README.md`에 정리되어 있습니다.

## 4. 문서별 고유 설명

### 4.1 디렉토리 구조

```
VM_setup/
├── README.md                    # 이 문서
├── WORKFLOW.md                  # 작업 흐름도
├── ARCHITECTURE.md              # 폴더별 역할 / 수정 요청별로 볼 곳
├── PR_CHECKLIST.md              # 수정·배포 전 체크리스트
├── vm-param-fix/                # 체크 CSV를 태그별로 분류해 아래 외부 도구들을 호출하는 오케스트레이터 (power_setting 바이너리 포함)
├── affinity_setting-source/     # affinity 태그 담당 외부 도구 소스
├── lpage_setting-source/        # lpage(HugePage/CPU 토폴로지) 태그 담당 외부 도구 소스
├── license_assign-source/       # 라이선스 할당 도구 소스 (하위 README 참고)
├── mac_info-source/             # MAC 주소 정보 조회 도구 소스 (하위 README 참고)
├── main_conn-source/            # ESXi 호스트를 vCenter 클러스터에 병렬 등록하는 도구 소스 (하위 README 참고)
├── tag_setting-source/          # VM 태그 설정 도구 소스 (하위 README 참고)
├── vm_create-source/            # VM 생성 도구 소스 (하위 README 참고)
├── vswitch_setting-source/      # 가상 스위치 설정 도구 소스 (하위 README 참고)
└── numa_preferht_setting-source/ # numa.vcpu.preferHT=TRUE 일괄 적용 도구 소스, 병렬(워커풀), 전원 OFF 조건 (하위 README 참고)
```

### 4.2 power_setting에 대한 중요 안내

`power_setting`의 **Go 소스 코드는 Rocky Linux 어디에서도 확실하게 찾지 못했습니다.**
로컬 파일 여러 개(`/root/pro/main.go` 등)를 대조해 봤지만, 실행 바이너리의 플래그 구성
(`-vcTargetIP`, `-worklistFile`, `-worklistBmFile`(default) 등)과 정확히 일치하는 소스를
확정하지 못했습니다. 그래서 **컴파일된 바이너리(`vm-param-fix/power_setting`)만** 이 폴더에
보관합니다. 소스 없이 바이너리만 있으므로 다시 빌드할 수 없고, 이 파일이
유일한 사본입니다. **삭제하지 마세요.**

같은 이유로 새 통합 도구(`vm-param-check`)에는 **호스트 전원정책 자동교정
기능이 없습니다**(체크만 함, 그 README의 "알려진 한계" 참고). 이 기능이 다시 필요해지면
`power_setting` 바이너리를 (vm-param-fix 오케스트레이터를 통해) 그대로 재사용하거나,
같은 로직을 처음부터 새로 작성해야 합니다.

## 5. 전역 명령어로 사용하기 (선택 사항)

빌드된 실행 파일을 PATH 환경 변수에 포함된 디렉터리로 옮기거나, 실행 파일이 있는 경로를 PATH에 추가하면 어디서든 명령어처럼 쓸 수 있습니다.

예시 (실행 파일을 `/usr/local/bin`으로 복사):

```bash
sudo cp affinity_setting-source/affinity_setting lpage_setting-source/lpage_setting vm-param-fix/vm-param-fix /usr/local/bin/
# 이후 어느 위치에서나 명령어처럼 실행 가능
```
