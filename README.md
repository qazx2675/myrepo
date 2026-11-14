# Confluence Storage Format(`.xhtml`) 문서 모음 (`confluence` 브랜치)

`https://github.com/qazx2675/myrepo` 저장소의 `origin/master` (`.claude/` 하위 선택 20개 프로젝트)에 대해 각 도구가 **무엇을 하는 도구인지**와 **어떻게 사용하는지**를 **한국어 Confluence Storage Format(`.xhtml`)**으로 정리한 통합 문서 브랜치입니다.

- **대상 브랜치**: `confluence` (원본 소스 코드를 제외한 문서 전용 독립 orphan 브랜치)
- **폴더 규칙**: 원본 프로젝트명 기반 `<프로젝트명>_docs/` (공백은 `_` 치환)
- **양식 분류 (`/jev-plan-router` · Jev API)**:
  - `T1` 단일 CLI · 스크립트형 (3개)
  - `T2` 점검 · 수집 · 부하판정형 (4개)
  - `T3` 설정 변경 · 적용 엔진형 (6개)
  - `T4` 데몬 · TUI · 중계 브릿지형 (3개)
  - `T5` 복합 Suite · 파이프라인형 (4개)

---

## 사내 Confluence 업로드 방법

1. 업로드하려는 프로젝트의 `<프로젝트명>_docs/` 폴더로 이동해 `<slug>.xhtml` 파일 전체 내용을 복사합니다.
   - 모든 `.xhtml` 파일은 `<root>` 래퍼 태그 없이 본문 매크로(`<ac:structured-macro ...>`)와 HTML로 바로 시작하는 Storage Format 본문 프래그먼트입니다.
2. 사내 Confluence 페이지 **편집(Edit)** 모드에서 우측 상단 **Source Editor (`<>`, 소스 편집기)** 또는 Storage Format 편집 창을 엽니다.
3. 복사한 `.xhtml` 내용을 그대로 붙여넣고 **Apply / 적용**을 누른 뒤 `info`, `toc`, `code`, `warning`, `note` 매크로 및 표 렌더링을 확인하고 저장합니다.

---

## 프로젝트별 문서 목차 (총 20개)

### HPC (15개)

| ID | 원본 경로 (`origin/master`) | 양식 | 문서 폴더 | `.xhtml` 문서 | 한 줄 요약 |
|---|---|---|---|---|---|
| `H1` | `.claude/HPC/auto_setup` | `T4` | [`auto_setup_docs/`](./auto_setup_docs/README.md) | [`auto_setup.xhtml`](./auto_setup_docs/auto_setup.xhtml) | OS 설치(01) 감시 및 os_check 자동 실행·TUI 상태판/변경 메뉴 통합 데몬 |
| `H2` | `.claude/HPC/awx_script` | `T5` | [`awx_script_docs/`](./awx_script_docs/README.md) | [`awx_script.xhtml`](./awx_script_docs/awx_script.xhtml) | HPC 노드정보 수집·가공 → AWX 인벤토리/DHCP/PXE 일괄 등록 Bash Suite |
| `H3` | `.claude/HPC/awxkit` | `T5` | [`awxkit_docs/`](./awxkit_docs/README.md) | [`awxkit.xhtml`](./awxkit_docs/awxkit.xhtml) | AWX API 제어·NodeInfo/인벤토리/DHCP/PXE 7종 Go CLI + Bash 래퍼 Suite |
| `H4` | `.claude/HPC/config_check` | `T3` | [`config_check_docs/`](./config_check_docs/README.md) | [`config_check.xhtml`](./config_check_docs/config_check.xhtml) | OS 체크 → y/n/set 환경설정 적용 → 재체크 → 벤더 코멘트 생성 Bash 도구 |
| `H5` | `.claude/HPC/copy` | `T4` | [`copy_docs/`](./copy_docs/README.md) | [`copy.xhtml`](./copy_docs/copy.xhtml) | 폐쇄망 ETX → AWX Job Template description → Windows 클립보드 단방향 브릿지 |
| `H6` | `.claude/HPC/GPU 체크스크립트` | `T2` | [`GPU_체크스크립트_docs/`](./GPU_체크스크립트_docs/README.md) | [`gpu_power_check.xhtml`](./GPU_체크스크립트_docs/gpu_power_check.xhtml) | nvidia-persistenced·gpu-power-limit.service 기반 GPU 전력 제한 일괄 점검 Bash |
| `H7` | `.claude/HPC/ip_change` | `T3` | [`ip_change_docs/`](./ip_change_docs/README.md) | [`ip_change.xhtml`](./ip_change_docs/ip_change.xhtml) | RHEL ifcfg 파일 내 IPADDR·GATEWAY 키만 안전하게 갱신하는 일괄 IP/GW 변경 Go 엔진 |
| `H8` | `.claude/HPC/ldap_check` | `T1` | [`ldap_check_docs/`](./ldap_check_docs/README.md) | [`ldap_check.xhtml`](./ldap_check_docs/ldap_check.xhtml) | LDAP·DNS·NTP·autofs 7종 설정 정합성을 읽기 전용으로 검증하는 단독 Bash(RHEL 6~10) |
| `H9` | `.claude/HPC/ldap_setting` | `T3` | [`ldap_setting_docs/`](./ldap_setting_docs/README.md) | [`ldap_setting.xhtml`](./ldap_setting_docs/ldap_setting.xhtml) | 사이트별 LDAP·DNS·NTP·autofs 설정 키 갱신 및 서비스 재시작/롤백 Go 엔진 |
| `H10` | `.claude/HPC/OS 환경설정 체크` | `T3` | [`OS_환경설정_체크_docs/`](./OS_환경설정_체크_docs/README.md) | [`os_config_check.xhtml`](./OS_환경설정_체크_docs/os_config_check.xhtml) | OS 배포 후 상태 점검·환경설정 적용·재점검 일괄 Bash(auto_setup -auto 연계) |
| `H11` | `.claude/HPC/pm_monitor` | `T4` | [`pm_monitor_docs/`](./pm_monitor_docs/README.md) | [`pm_monitor.xhtml`](./pm_monitor_docs/pm_monitor.xhtml) | 1분 주기 ping·22포트·gossh -pm 기반 OS 설치 7종 상태 판정 PM 전용 경량 TUI |
| `H12` | `.claude/HPC/redfish` | `T3` | [`redfish_docs/`](./redfish_docs/README.md) | [`redfish.xhtml`](./redfish_docs/redfish.xhtml) | BMC Redfish API 기반 5개 벤더 BIOS 표준값 점검 및 Pending 설정 Go+Bash 도구 |
| `H13` | `.claude/HPC/setup_check` | `T2` | [`setup_check_docs/`](./setup_check_docs/README.md) | [`setup_check.xhtml`](./setup_check_docs/setup_check.xhtml) | 신규 OS 설치 5상태 일괄 판정 및 config_check·stress-test(-run) 자동 연계 도구 |
| `H14` | `.claude/HPC/stress-test` | `T2` | [`stress-test_docs/`](./stress-test_docs/README.md) | [`stress_test.xhtml`](./stress-test_docs/stress_test.xhtml) | AWX cpu/stress-ng 부하 실행 및 gossh /proc/loadavg 1분 로드 감시·실행 판정 Go 도구 |
| `H15` | `.claude/HPC/조사` | `T2` | [`조사_docs/`](./조사_docs/README.md) | [`survey.xhtml`](./조사_docs/survey.xhtml) | 표1 자산 목록을 gossh로 병렬 수집해 엑셀 붙여넣기용 TSV로 생성하는 자산 조사 도구 |

### VM (3개)

| ID | 원본 경로 (`origin/master`) | 양식 | 문서 폴더 | `.xhtml` 문서 | 한 줄 요약 |
|---|---|---|---|---|---|
| `V1` | `.claude/VM/V2` | `T5` | [`V2_docs/`](./V2_docs/README.md) | [`vm_v2.xhtml`](./V2_docs/vm_v2.xhtml) | BM당 ev01~ev99 VM 생성·설정(VMsetup)과 체크/자동교정(vm-param-check) 차세대 통합 Suite |
| `V7` | `.claude/VM/vCenter API IP 자동변경` | `T3` | [`vCenter_API_IP_자동변경_docs/`](./vCenter_API_IP_자동변경_docs/README.md) | [`vm_ip_change.xhtml`](./vCenter_API_IP_자동변경_docs/vm_ip_change.xhtml) | 네트워크 불통 RHEL 8.10 VM IP를 vCenter Guest Operations API(nmcli)로 재설정하는 Go CLI/TUI |
| `V11` | `.claude/VM/VM_setup` | `T5` | [`VM_setup_docs/`](./VM_setup_docs/README.md) | [`vm_setup.xhtml`](./VM_setup_docs/vm_setup.xhtml) | V1 기준 ESXi/VM 개별 설정 9종 + vm-param-fix/power_setting 레거시 통합 가이드 |

### 공통 (2개)

| ID | 원본 경로 (`origin/master`) | 양식 | 문서 폴더 | `.xhtml` 문서 | 한 줄 요약 |
|---|---|---|---|---|---|
| `C1` | `.claude/공통/gossh` | `T1` | [`gossh_docs/`](./gossh_docs/README.md) | [`gossh.xhtml`](./gossh_docs/gossh.xhtml) | pdsh 스타일 고속 병렬 SSH 실행 도구(다수 HPC·VM 도구의 공통 실행 기반) |
| `C2` | `.claude/공통/dirmap` | `T1` | [`dirmap_docs/`](./dirmap_docs/README.md) | [`dirmap.xhtml`](./dirmap_docs/dirmap.xhtml) | 자주 쓰는 디렉터리 트리 및 FOLDER_DESC 한글 주석을 함께 출력하는 Bash 스크립트 |

---

## 서로 연계된 프로젝트 통합 흐름

- **OS 배포 후 점검·설정·모니터링 흐름**: `setup_check` (`-run`) ↔ `config_check` / `stress-test`, `auto_setup` 데몬 ↔ `OS 환경설정 체크`(`-auto`) / `ip_change` / `ldap_setting` / `awx_script`
- **LDAP 적용·검증·모니터링 연계**: `ldap_setting` (설정 키 갱신·롤백) ↔ `ldap_check` (읽기 전용 정합성 검증) ↔ `pm_monitor` (`l` 키 단축 점검)
- **AWX 노드 등록 파이프라인**: `awx_script` (`01.AWX_nodeinfo_V2.sh` · `02.source_dhcp_pxe.sh`) ↔ 내장 `awxkit` 7종 Go 바이너리
- **VM 세대/도구 관계**: `V2` (`VMsetup` + `vm-param-check -fix` + 공유 `SPEC_DIR`)는 `VM_setup`(V1 개별 도구 9종 + `vm-param-fix`)의 차세대 통합 Suite
- **병렬 실행 공통 기반**: `gossh`는 다수 HPC/VM 스크립트와 바이너리의 원격 병렬 실행 기반 도구로 동작
