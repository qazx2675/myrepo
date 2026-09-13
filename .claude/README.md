# .claude

vCenter/ESXi/VM 인프라 자동화 도구 모음. 폴더별로 독립된 Go 모듈(자체 `go.mod`/`vendor/`)이거나
독립 셸 스크립트라서, 필요한 폴더 하나만 떼어가도 그대로 빌드·실행됩니다. 각 폴더의 상세 사용법은
그 폴더 안의 `README.md`(필요하면 `PLAN.md`/`계획서.md`)를 참고하세요.

## VM/

vCenter/ESXi VM 관련 점검·설정·테스트 도구.

| 폴더 | 설명 |
|---|---|
| [`vm-param-check-usability-improvement`](./VM/vm-param-check-usability-improvement/) | vCenter VM의 CPU/메모리/NUMA 토폴로지, vCPU affinity, Shares, 호스트 전원정책 등 고성능(High Performance) 설정 기준 점검 + FAIL 항목 자동교정(게이트·dry-run·재검증 포함)까지 단일 바이너리로 처리하는 **최신 통합 도구**(`vm-param-check/` 하위)에, 폴더명 기반 스펙 자동매칭·대상 VM만 조회하는 성능 개선·Task 폴더 예외 처리를 얹은 프로젝트. 새로 쓸 때는 이 폴더만 있으면 됩니다. |
| [`Network_Change_Integration_Script`](./VM/Network_Change_Integration_Script/) | IP 변경(`ip_change`) · LDAP 설정(`ldap_setting`) · 포트그룹(VLAN) 이관(`vm-network-migration`)을 **한 번의 실행**으로 순차 처리하는 통합 VM 망변경 스크립트(`change.sh`). 전처리, 결과 집계, 인시던트 관리 및 자동 롤백 기능을 포함하며, 세 프로젝트 소스를 내장하여 오프라인에서도 단독 빌드·실행 가능. |
| [`vm_verifier`](./VM/vm_verifier/) | VM 생성 직후(OS 설치/파워온 전) vCenter API가 인식한 vNIC MAC 주소와 DHCP 정적 예약 MAC을 대조하여 DHCP MAC 오기입 및 형제 VM 간 **교차 설치(역설치)**를 사전 탐지하는 에이전트리스 고속 병렬 검증 CLI 도구(`vm-verifier`). |
| [`integrated-vm-param-check-test-tool`](./VM/integrated-vm-param-check-test-tool/) | `vm-param-check`(체크+자동교정)와 `vc-test-env`(vcsim 시뮬레이터 환경 복제)를 한 폴더로 묶어, 인터넷이 차단된 폐쇄망 서버로 그대로 옮겨 빌드부터 테스트까지 원스톱으로 수행할 수 있도록 구성한 통합 테스트 패키지. |
| [`lpage_search`](./VM/lpage_search/) | ESXi 호스트 총 메모리, ev01 VM에 기할당된 메모리, vCPU 수를 기반으로 ev02 VM에 안전하게 할당 가능한 메모리(2MB Large Page 정렬, ESXi High-State 유지 버퍼 포함) 크기를 계산해 주는 순수 Go 계산 도구(외부 의존성 및 네트워크 접속 불필요). |
| [`VM_setup`](./VM/VM_setup/) | VM/ESXi/vCenter의 개별 설정(affinity, lpage/HugePage, 전원정책, 태그, vSwitch, 라이선스 할당, VM 생성 등)을 적용하는 도구 소스 및 바이너리 모음 폴더. (오케스트레이터인 `vm-param-fix/`는 `vm-param-check-usability-improvement`로 통합 대체됨). |
| [`esxi-log-check`](./VM/esxi-log-check/) | 다중 ESXi 호스트 로그를 패턴 레지스트리로 매칭해서 CRITICAL/HIGH 하드웨어 장애 이벤트를 뽑아내는 도구(`esxi-log-check`). |
| ~~[`powershell`](./VM/powershell/)~~ | 폐쇄망 환경에서 일반 업무용 PowerShell 7 바이너리 및 VMware PowerCLI 모듈(`.nupkg`)을 오프라인으로 자동 설치·배치하는 스크립트(`setup_폐쇄망pwsh.sh`). 자동완성 및 순차조회 권고 등 실습용 오버헤드 프로필을 제외한 순수 업무용 구성. |
| ~~[`vm-network-migration`](./VM/vm-network-migration/)~~ | `Network_Change_Integration_Script`에 통합되어 있으므로 별도 내용 작성 제외 (`Network_Change_Integration_Script/projects/vm-network-migration`에 포함). |
| ~~[`vm-param-setting-check`](./VM/vm-param-setting-check/)~~ | `vm-param-check-usability-improvement` 통합폴더에 존재 (레거시, 기록 보존용으로 유지). |
| ~~[`vm-setting-go-lang`](./VM/vm-setting-go-lang/)~~ | `vm-param-check-usability-improvement` 통합폴더에 존재 (레거시, govmomi 기반 개별 도구 소스 보존용). |
| ~~[`vcenter-test-env-vcsim`](./VM/vcenter-test-env-vcsim/)~~ | 실제로 사용하기 부적합, 설정 테스트는 좋으므로 삭제는 안 함 (`vcsim` 기반 vCenter 인벤토리 구조 재현 도구). |
| ~~[`gemini_vcsim-pipeline-test`](./VM/gemini_vcsim-pipeline-test/)~~ | ~~`vcsim` 기반 VM 생성/설정/점검/수정 전체 파이프라인 통합 테스트 프레임워크.~~ |

## HPC/

고성능(HPC) 서버 OS/IP/자산 레벨 점검·설정 도구.

| 폴더 | 설명 |
|---|---|
| [`OS 환경설정 체크`](./HPC/OS%20환경설정%20체크/) | `gossh` 기반으로 OS 배포 후 상태 점검 → 환경설정 적용 → 재점검까지 한 번에 처리하는 자동화 스크립트(`os_check_final_annotated.sh`). |
| [`ldap_setting`](./HPC/ldap_setting/) | 자산현황(`hostname<TAB>site`)을 읽어 인프라·사이트별 LDAP/DNS/NTP/autofs 설정 7종을 `gossh`로 일괄 적용하는 Go 엔진(`ldap-config-engine`)과 배포 래퍼(`deploy_ldap.sh`). 파일 전체를 덮지 않고 해당 키만 수술적으로 갱신하며, 바뀐 파일에 대응하는 서비스만 재시작. RHEL7/8 및 `s4` 호스트 예외 분기 포함. |
| [`ldap_check`](./HPC/ldap_check/) | 위 설정이 제대로 반영됐는지 노드에서 로컬 점검하는 단독 bash 스크립트(`ldap_check.sh`). DNS+NTP / LDAP / auto.appl 3중 교차 검증으로 인프라·사이트를 판별하고 7개 파일을 OK/FAIL로 출력. jq 불필요, `gossh` 일괄 점검용. |
| [`ip_change`](./HPC/ip_change/) | Red Hat Enterprise Linux(RHEL) 대상 노드들의 IP·게이트웨이 설정을 ifcfg 파일 전체를 덮어쓰지 않고 `IPADDR`/`GATEWAY` 키만 수술적으로 갱신하여 `gossh`로 일괄 배포하는 Go 도구(`ip-change-engine`). 원본 파일 자동 백업 및 표준 라이브러리 기반 오프라인 정적 빌드 지원. |
| [`awxkit`](./HPC/awxkit/) | Ansible AWX 서버 조작 및 상태 점검을 위한 Go 도구 7종(`awxkit-doctor`, `awxkit-nodeinfo`, `awxkit-ls`, `awxkit-survey`, `awxkit-invsync`, `awxkit-dhcp`, `awxkit-pxe`)과 자동 빌드/실행 bash 래퍼 스크립트 모음. 폐쇄망 환경 오프라인 빌드(`vendor/`) 지원. |
| [`조사`](./HPC/조사/) | 리눅스(RHEL) 서버 목록(표1 자산양식)을 입력받아 `gossh`로 병렬 접속하여 자산대장 항목을 자동 수집하고, 엑셀에 바로 붙여넣을 수 있는 탭 구분(TSV) 결과 파일로 저장하는 자산 조사 도구(`survey` 및 `run_survey.sh`). |

## 공통/

`VM/`와 `HPC/` 양쪽에서 같이 쓰는 기반 도구.

| 폴더 | 설명 |
|---|---|
| [`gossh`](./공통/gossh/) | `pdsh` 스타일 고속 병렬 SSH 실행 도구. 실시간 진행률 표시, 표준출력/에러 분리, SSH 키 우선 인증, `-b` 그룹 출력, autofs 마운트 스톰 방지(동시 350 안전가드 및 `-cf` 옵션), 위험 명령(재부팅 등) 가드 및 OS6 정적 바이너리(`gossh_os6`) 내장. `esxi-log-check`, `OS 환경설정 체크`, `ip_change`, `조사` 등 다수 도구의 기반 바이너리 (구버전은 `old/`에 보존). |

## 디렉토리 구조

```
.claude/
├── VM/                                  # vCenter/ESXi VM 점검·설정·테스트 도구 모음 (위 표 참고)
│   ├── Network_Change_Integration_Script # 통합 VM 망변경 스크립트 (IP/LDAP/포트그룹 원스톱 이관)
│   ├── vm-param-check-usability-improvement # vCenter VM 고성능 점검 + 자동교정 통합 도구
│   ├── vm_verifier                      # VM vNIC MAC vs DHCP 정적 MAC 대조 교차설치 사전 검증 도구
│   ├── integrated-vm-param-check-test-tool # vm-param-check + vc-test-env 오프라인 통합 테스트 패키지
│   ├── lpage_search                     # ESXi Large Page(2MB) 메모리 사이징 계산기
│   ├── VM_setup                         # VM/ESXi 개별 설정 도구 및 소스 모음
│   ├── esxi-log-check                   # ESXi 호스트 로그 장애 이벤트 분석 도구
│   ├── powershell                       # 폐쇄망 업무용 PowerShell 7 / PowerCLI 설치 스크립트
│   ├── vm-network-migration             # VM 네트워크 포트그룹 이관 도구 (Network_Change_Integration_Script에 내장)
│   ├── vm-param-setting-check           # 점검 전용 레거시 도구 (통합폴더에 존재)
│   ├── vm-setting-go-lang               # VM 설정 개별 도구 4종 (통합폴더에 존재)
│   └── vcenter-test-env-vcsim           # vcsim 기반 테스트 도구 (실사용 부적합, 테스트용 유지)
├── HPC/                                 # HPC 서버 OS/IP/자산 레벨 점검·설정 도구 모음 (위 표 참고)
│   ├── OS 환경설정 체크                   # gossh 기반 OS 환경 점검 및 설정 자동화 스크립트
│   ├── ldap_setting                     # 인프라·사이트별 LDAP/DNS/NTP/autofs 일괄 설정 Go 엔진
│   ├── ldap_check                       # LDAP/DNS/NTP 설정 반영 로컬 검증 스크립트
│   ├── ip_change                        # RHEL 노드 IP/게이트웨이 설정 일괄 변경 도구
│   ├── awxkit                           # Ansible AWX 조작 및 상태 점검 Go 도구 7종
│   ├── 조사                             # RHEL 서버 자산대장 항목 자동 수집 도구 (survey)
│   └── GPU 체크스크립트                  # GPU power capping 점검 스크립트 (목록 제외, 실제 디렉터리 유지)
├── 공통/                                 # 여러 도구가 같이 쓰는 기반 도구 (gossh 등)
│   └── gossh                            # pdsh 스타일 병렬 SSH 실행 도구 (v2 통합, old/ 아카이브)
├── LDAP 인프라 선택 방식 전환/             # Network_Change_Integration_Script 작업기록 문서
├── update_readme.py                     # 하위 폴더 README.md들의 빌드 안내 문구를 일괄 치환하는 보조 스크립트
├── 바이너리셋업.sh                         # VM/ 하위 Go 도구들을 빌드해서 지정 경로(기본 /usr/local/bin)에 한 번에 설치하는 스크립트
└── settings.local.json                  # Claude Code 로컬 설정 파일
```

- `update_readme.py` — `VM/` 하위 각 폴더의 `README.md`를 순회하며 정해진 빌드 안내 문구를 새 문구로 일괄 치환하는 관리용 스크립트.
- `바이너리셋업.sh` — `VM/` 폴더의 Go 프로젝트들을 각각 빌드한 뒤 지정한 경로(기본값 `/usr/local/bin`)로 복사해 전역 명령어처럼 쓸 수 있게 만드는 설치 스크립트.
- `settings.local.json` — Claude Code 로컬 설정 파일.
