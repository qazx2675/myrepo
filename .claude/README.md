# .claude

vCenter/ESXi/VM 인프라 자동화 도구 모음. 폴더별로 독립된 Go 모듈(자체 `go.mod`/`vendor/`)이거나
독립 셸 스크립트라서, 필요한 폴더 하나만 떼어가도 그대로 빌드·실행됩니다. 각 폴더의 상세 사용법은
그 폴더 안의 `README.md`(필요하면 `PLAN.md`/`계획서.md`)를 참고하세요.

## VM 업무/

vCenter/ESXi VM 관련 점검·설정·테스트 도구.

| 폴더 | 설명 |
|---|---|
| [`VM 매개변수 체크 및 설정 통합 툴 사용성 개선`](./VM%20업무/VM%20매개변수%20체크%20및%20설정%20통합%20툴%20사용성%20개선/) | vCenter VM의 CPU/메모리/NUMA 토폴로지, vCPU affinity, Shares, 호스트 전원정책 등 고성능(High Performance) 설정 기준 점검 + FAIL 항목 자동교정(게이트·dry-run·재검증 포함)까지 단일 바이너리로 처리하는 **최신 통합 도구**(`VM 매개변수 체크 및 설정 통합 툴/` 하위)에, 폴더명 기반 스펙 자동매칭·대상 VM만 조회하는 성능 개선·Task 폴더 예외 처리를 얹은 프로젝트. 새로 쓸 때는 이 폴더만 있으면 됩니다. |
| [`VM 매개변수설정체크`](./VM%20업무/VM%20매개변수설정체크/) | 위 통합 도구의 전신 — 점검(체크) 전용 버전. 내부 `FAIL기반 매개변수 수정/`에 점검 결과를 별도 외부 도구(affinity/lpage/power)로 교정하던 예전 방식이 같이 들어 있습니다(레거시, 기록 보존용으로 유지). |
| [`VM설정 go lang`](./VM%20업무/VM설정%20go%20lang/) | CPU affinity/large page/전원정책 설정과 VM/ESXi 호스트 생성·등록을 담당하는 govmomi 기반 도구 4종(`vm_affinity_bulk`/`vm_lpage_bulk`/`vm_create`/`vm_connect`). 위 레거시 교정 파이프라인이 서브프로세스로 호출하는 실행파일들의 소스. |
| [`ESXi 로그점검`](./VM%20업무/ESXi%20로그점검/) | 다중 ESXi 호스트 로그를 패턴 레지스트리로 매칭해서 CRITICAL/HIGH 하드웨어 장애 이벤트를 뽑아내는 도구(`esxi-log-check`). |
| [`vcenter 테스트환경구축 (vcsim)`](./VM%20업무/vcenter%20테스트환경구축%20%28vcsim%29/) | 실 vCenter의 인벤토리 구조를 읽어와 `vcsim`(vCenter 시뮬레이터) 위에 동일하게 재현하는 도구(`vc-test-env`). 실 인프라를 건드리지 않고 다른 govmomi 도구를 안전하게 테스트할 때 사용. |
| ~~[`gemini_vcsim-pipeline-test`](./VM%20업무/gemini_vcsim-pipeline-test/)~~ | ~~`vcsim` 기반 VM 생성/설정/점검/수정 전체 파이프라인 통합 테스트 프레임워크.~~ |

## HPC/

고성능(HPC) 서버 OS 레벨 점검·설정 도구.

| 폴더 | 설명 |
|---|---|
| [`OS 환경설정 체크`](./HPC/OS%20환경설정%20체크/) | `gossh` 기반으로 OS 배포 후 상태 점검 → 환경설정 적용 → 재점검까지 한 번에 처리하는 자동화 스크립트(`os_check_final_annotated.sh`). |

## 공통/

`VM 업무/`와 `HPC/` 양쪽에서 같이 쓰는 기반 도구.

| 폴더 | 설명 |
|---|---|
| [`gossh`](./공통/gossh/) | `pdsh` 스타일 병렬 SSH 실행 도구. `ESXi 로그점검`, `OS 환경설정 체크` 등 여러 도구가 서브프로세스로 호출하는 기반 바이너리. 위험 명령(재부팅/전원종료 등) 이중 확인 가드 포함. |

## 개인프로젝트/

개인 또는 실험 목적의 프로젝트.

| 폴더 | 설명 |
|---|---|
| [`home_lab`](./개인프로젝트/home_lab/) | (`vm 생성 홈페이지 프로젝트`) govmomi 기반 vCenter/ESXi 자동화 CLI들을 RBAC + 암호화 자격증명 + 감사 로그를 갖춘 웹 포털로 감싼 프로젝트. |
| [`clipSend`](./개인프로젝트/clipSend/) | 클립보드 텍스트를 자동으로 메일로 보내주는 Android 애플리케이션. Kotlin/Jetpack Compose 기반. |

## 기타

- `settings.local.json` — Claude Code 로컬 설정 파일.
