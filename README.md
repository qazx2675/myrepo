# auto_setup + awx_script

HPC 서버 OS 설치 자동 연계 도구 묶음입니다. 용량을 줄이려고 두 프로젝트만 담았습니다 (원본은 master 의 `.claude/HPC/`).

| 폴더 | 내용 | 시작 문서 |
|---|---|---|
| [auto_setup/](auto_setup/) | OS 설치(01) → OS 설정체크 자동 연계 데몬·상태 화면(TUI). 화면에서 체크스크립트 단독 실행(`x`), AWX 실행(`w`) | [auto_setup/README.md](auto_setup/README.md), [사용법.txt](auto_setup/사용법.txt) |
| [awx_script/](awx_script/) | AWX 인벤토리/DHCP/PXE 등록 스크립트 (`01.AWX_nodeinfo_V2.sh`, `02.source_dhcp_pxe.sh`) | [awx_script/README.md](awx_script/README.md), [사용법.txt](awx_script/사용법.txt) |

## 설치 순서 (폐쇄망)

1. `awx_script/` 를 현장 경로에 두고 `bash awx_script/setup/setup_guide.sh` 로 빈 변수를 채웁니다 (이미 쓰던 사본은 `bash awx_script/setup/update_v0.6.0.sh`).
2. `auto_setup/` 에서 `bash conf/setup_guide.sh`(또는 기존 현장은 `bash conf/update_v0.6.0.sh`) 후 `bash setup.sh` → `auto_setup --restart`. RHEL6(os6_mgmt)는 커밋된 `auto_setup_os6` 를 그대로 사용합니다.
3. `/etc/auto_setup/auto_setup.conf` 의 `awx_dir` 에 awx_script 경로, 필요하면 `awx_profile_1~9` 를 채웁니다.

## 주의사항 (Disclaimer)

본 스크립트 및 툴은 100% 신뢰하기보다는 참고용(보조 도구)으로 사용하는 것을 권장합니다. 설정 변경 스크립트를 실행한 뒤에는 랜덤한 서버 몇 대를 직접 확인해 실제로 변경되었는지 확인하십시오.
