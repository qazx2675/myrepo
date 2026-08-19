# .claude

vCenter/ESXi/VM 인프라 자동화 도구 모음. 폴더별로 독립된 Go 모듈(자체 `go.mod`/`vendor/`)이거나
독립 셸 스크립트라서, 필요한 폴더 하나만 떼어가도 그대로 빌드·실행됩니다. 각 폴더의 상세 사용법은
그 폴더 안의 `README.md`(필요하면 `PLAN.md`/`계획서.md`)를 참고하세요.

## VM/

vCenter/ESXi VM 관련 점검·설정·테스트 도구.

| 폴더 | 설명 |
|---|---|
| [`vm-param-check-usability-improvement`](./VM/vm-param-check-usability-improvement/) | vCenter VM의 CPU/메모리/NUMA 토폴로지, vCPU affinity, Shares, 호스트 전원정책 등 고성능(High Performance) 설정 기준 점검 + FAIL 항목 자동교정(게이트·dry-run·재검증 포함)까지 단일 바이너리로 처리하는 **최신 통합 도구**(`vm-param-check/` 하위)에, 폴더명 기반 스펙 자동매칭·대상 VM만 조회하는 성능 개선·Task 폴더 예외 처리를 얹은 프로젝트. 새로 쓸 때는 이 폴더만 있으면 됩니다. |
| [`vm-param-setting-check`](./VM/vm-param-setting-check/) | 위 통합 도구의 전신 — 점검(체크) 전용 버전. 내부 `fail-based-param-fix/`에 점검 결과를 별도 외부 도구(affinity/lpage/power)로 교정하던 예전 방식이 같이 들어 있습니다(레거시, 기록 보존용으로 유지). |
| [`vm-setting-go-lang`](./VM/vm-setting-go-lang/) | CPU affinity/large page/전원정책 설정과 VM/ESXi 호스트 생성·등록을 담당하는 govmomi 기반 도구 4종(`vm_affinity_bulk`/`vm_lpage_bulk`/`vm_create`/`vm_connect`). 위 레거시 교정 파이프라인이 서브프로세스로 호출하는 실행파일들의 소스. |
| [`esxi-log-check`](./VM/esxi-log-check/) | 다중 ESXi 호스트 로그를 패턴 레지스트리로 매칭해서 CRITICAL/HIGH 하드웨어 장애 이벤트를 뽑아내는 도구(`esxi-log-check`). |
| [`vcenter-test-env-vcsim`](./VM/vcenter-test-env-vcsim/) | 실 vCenter의 인벤토리 구조를 읽어와 `vcsim`(vCenter 시뮬레이터) 위에 동일하게 재현하는 도구(`vc-test-env`). 실 인프라를 건드리지 않고 다른 govmomi 도구를 안전하게 테스트할 때 사용. |
| ~~[`gemini_vcsim-pipeline-test`](./VM/gemini_vcsim-pipeline-test/)~~ | ~~`vcsim` 기반 VM 생성/설정/점검/수정 전체 파이프라인 통합 테스트 프레임워크.~~ |

## HPC/

고성능(HPC) 서버 OS 레벨 점검·설정 도구.

| 폴더 | 설명 |
|---|---|
| [`OS 환경설정 체크`](./HPC/OS%20환경설정%20체크/) | `gossh` 기반으로 OS 배포 후 상태 점검 → 환경설정 적용 → 재점검까지 한 번에 처리하는 자동화 스크립트(`os_check_final_annotated.sh`). |
| [`GPU 체크스크립트`](./HPC/GPU%20체크스크립트/) | `nvidia-persistenced` + `gpu-power-limit.service`로 설정한 GPU power capping(최대 전력의 75%)이 정상 적용됐는지 일회성으로 점검하는 스크립트(`check-gpu-power-limit.sh`). `gossh`로 여러 서버 일괄 점검용. |

## 공통/

`VM/`와 `HPC/` 양쪽에서 같이 쓰는 기반 도구.

| 폴더 | 설명 |
|---|---|
| [`gossh`](./공통/gossh/) | `pdsh` 스타일 병렬 SSH 실행 도구. `esxi-log-check`, `OS 환경설정 체크` 등 여러 도구가 서브프로세스로 호출하는 기반 바이너리. 위험 명령(재부팅/전원종료 등) 이중 확인 가드 포함. |

## 개인프로젝트/

개인 또는 실험 목적의 프로젝트.

| 폴더 | 설명 |
|---|---|
| [`home_lab`](./개인프로젝트/home_lab/) | (`vm 생성 홈페이지 프로젝트`) govmomi 기반 vCenter/ESXi 자동화 CLI들을 RBAC + 암호화 자격증명 + 감사 로그를 갖춘 웹 포털로 감싼 프로젝트. |
| [`clipSend`](./개인프로젝트/clipSend/) | 클립보드 텍스트를 자동으로 메일로 보내주는 Android 애플리케이션. Kotlin/Jetpack Compose 기반. |

## 디렉토리 구조

```
.claude/
├── VM/                  # vCenter/ESXi VM 점검·설정·테스트 도구 모음 (위 표 참고)
├── HPC/                 # HPC 서버 OS/GPU 레벨 점검·설정 도구 모음 (위 표 참고)
├── 공통/                 # 여러 도구가 같이 쓰는 기반 도구 (gossh 등)
├── 개인프로젝트/           # 개인/실험 목적 프로젝트 (home_lab, clipSend)
├── update_readme.py     # 하위 폴더 README.md들의 빌드 안내 문구를 일괄 치환하는 보조 스크립트
├── 바이너리셋업.sh         # VM/ 하위 Go 도구들을 빌드해서 지정 경로(기본 /usr/local/bin)에 한 번에 설치하는 스크립트
└── settings.local.json  # Claude Code 로컬 설정 파일
```

- `update_readme.py` — `VM/` 하위 각 폴더의 `README.md`를 순회하며 정해진 빌드 안내 문구를 새 문구로 일괄 치환하는 관리용 스크립트.
- `바이너리셋업.sh` — `VM/` 폴더의 Go 프로젝트들을 각각 빌드한 뒤 지정한 경로(기본값 `/usr/local/bin`)로 복사해 전역 명령어처럼 쓸 수 있게 만드는 설치 스크립트.
- `settings.local.json` — Claude Code 로컬 설정 파일.
