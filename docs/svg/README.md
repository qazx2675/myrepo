# docs/svg — 명령별 흐름도

V2(`VMsetup` + `vm-param-check`)의 스크립트·도구마다 흐름도 SVG 를 하나씩 둡니다. 각 그림은 **코드에서 확인한 실제 단계**이며 맨 아래에 구현 위치(파일·함수)가 적혀 있습니다. 브라우저로 열면 라이트/다크 설정을 따릅니다 (`prefers-color-scheme`, 양식은 `HPC/auto_setup/docs/svg` 와 같음).

- 전체 흐름: [`../../workflow.svg`](../../workflow.svg) · [`../../WORKFLOW.md`](../../WORKFLOW.md)
- `vm_setup.sh` 전체: [`../../VMsetup/workflow.svg`](../../VMsetup/workflow.svg) · 체크·교정: [`../../vm-param-check-usability-improvement/workflow.svg`](../../vm-param-check-usability-improvement/workflow.svg) · 배포·갱신: [`../../vm-param-check-usability-improvement/workflow-deploy.svg`](../../vm-param-check-usability-improvement/workflow-deploy.svg)
- 사용법·옵션: [`../../README.md`](../../README.md) · 코드 구조: [`../../ARCHITECTURE.md`](../../ARCHITECTURE.md)

**색 범례** (그림 맨 아래에도 있음): 파랑 = 도구 동작 · 주황 = 판정·입력·확인 · 보라 = 외부 호출(vCenter·다른 도구) · 초록 = 기록·결과 · 회색 = 시작·종료·참고.

## 준비

| 파일 | 명령 | 설명 |
|---|---|---|
| [`setup.svg`](setup.svg) | `bash setup.sh [도구…]` · `-l` · `-c` · `--os6` | OS8 오프라인 빌드(`GOPROXY=off`, `-mod=vendor`) / OS6 는 `bin_os6/*.gz` 설치 |
| [`passwd_update.svg`](passwd_update.svg) | `./passwd_update.sh [-esxi] [-id] · -list` | vCenter·ESXi 비밀번호를 `secret/` 에 암호화 저장 |
| [`vswitch_pgname.svg`](vswitch_pgname.svg) | `vswitch_pgname.sh <vswitch_user.txt>` | 포트그룹 칸의 IP → `<폴더명>-cae-a-b-c-0` (원본 `.bak`) |

## VMsetup — `vm_setup.sh`

| 파일 | 단계 | 설명 |
|---|---|---|
| [`vm_setup_input.svg`](vm_setup_input.svg) | 입력 · 할당 · 실행 계획 | user · 설치 정보 · 스펙 자동 할당 · VM 표 · CAE 번호 · vCenter · 계획 확인(`-n`) |
| [`vm_setup_run.svg`](vm_setup_run.svg) | 실행 단계 | 호스트 등록 → 포트그룹 → 스펙마다 VM 생성·MAC·전원·affinity·lpage·PCI → 라이선스 → 태그 (진행 모니터) |
| [`spec_del.svg`](spec_del.svg) | 스펙 제외 키 | `all_del_affinity` · `all_del_lpage` · `evNN_del_affinity` · `evNN_del_lpage` → 설정 도구 · 체크까지 |
| [`affinity_setting.svg`](affinity_setting.svg) | `affinity_setting` | ev 별 affinity 파일 적용 / `-del_affinity` · `-del_affinity_evs` 삭제 |
| [`lpage_setting.svg`](lpage_setting.svg) | `lpage_setting` | HugePage 5개 키 + CPU 토폴로지 / `-del_lpage` · `-del_lpage_evs` 는 HugePage 만 삭제 |
| [`pci_passthrough.svg`](pci_passthrough.svg) | `pci_passthrough` | 스펙 `pci-evNN` 이 있을 때만 — 장치 배분 → 활성화 → 연결 → 전원 ON (종료코드 3 = 재부팅 필요) |
| [`vm_setup_check.svg`](vm_setup_check.svg) | 스펙 체크 · MAC 목록 복사 | 만든 VM 만 `vm-param-check -specFolder` · `mac_all.txt` IP 채움 → `awx_route/<user>.txt` |
| [`lpage_from_spec.svg`](lpage_from_spec.svg) | `lpage_from_spec.sh` | 스펙만 읽어 lpage 단계 단독 실행 (`-del_lpage` 와 같은 동작) |
| [`result_cleanup.svg`](result_cleanup.svg) | `result_cleanup.sh` | 1시간 넘은 실행 결과물만 삭제 (crontab 매시) |

## vm-param-check

| 파일 | 명령 | 설명 |
|---|---|---|
| [`check_insert_01.svg`](check_insert_01.svg) | `01.vm_setting_check_insert.sh` | 단독 실행(`<user>.txt` + 스펙 폴더명) / `-fromSetup`(vm_setup 직후 기록 그대로) |
| [`vm_param_check.svg`](vm_param_check.svg) | `vm-param-check` | 기대값(옵션·스펙·제외 키) → 2단계 조회 → 항목별 체크 → CSV |
| [`vm_param_check_fix.svg`](vm_param_check_fix.svg) | `vm-param-check -fix` | 교정 계획 → 게이트 → dry-run → y/N → 적용 → 재검증 |
| [`update_deploy.svg`](update_deploy.svg) | `update_deploy.sh [-n]` | 인터넷 서버 — standalone 브랜치로 제자리 갱신, 사용자 파일 보존 |
| [`update_package.svg`](update_package.svg) | `make_update_package.sh` → `update.sh [-n]` | 폐쇄망 — 체크섬·`-demo` 확인 후 바뀐 파일만 교체 |

## 고칠 때

- 그림은 손으로 고쳐도 되지만 `<style>` 의 CSS 변수 블록(라이트·다크)은 그대로 두십시오 (색을 직접 쓰면 다크 모드에서 안 보일 수 있음).
- 흐름이 바뀌면 같은 커밋에서 해당 SVG 의 단계와 맨 아래 "코드:" 줄도 함께 고칩니다 (`PR_CHECKLIST.md` 의 문서 항목).
- 검증: `python3 -c "import xml.dom.minidom,sys; [xml.dom.minidom.parse(f) for f in sys.argv[1:]]" *.svg` (또는 `xmllint --noout *.svg`) 이 오류 없이 끝나야 합니다.
