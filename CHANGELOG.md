# 변경 이력 (`CHANGELOG.md`)

## v0.1.0 (2026-11-25)

### 초기 릴리즈
- **메인 스크립트 (`change_ilo_ip.sh`)**
  - RHEL 6(Bash 4.1) ~ RHEL 9 환경 호환 순수 Bash 스크립트 구현 (`jq`/`python3` 무의존)
  - `list.txt` 기반 공백/탭 구분 3컬럼(`[현재 관리망 IP] [변경될 관리망 IP] [변경될 VLAN]`) 파싱 및 실행 전 전수 유효성 검사
  - VLAN 활성화(`1~4094`) 및 비활성화(`0`, `off`, `disable`, `none`) 지원
  - 대화형 프롬프트 흐름(`ILO_USER`, 숨김 `ILO_PASS`, 서브넷 마스크, 자동 `.1` 또는 고정 게이트웨이, 작업 모드 `y`/`c`/`n`) 및 CLI 비대화형 오버라이드(`-f`, `-m`, `-g`, `-c`, `-y`, `--ilorest-bin`, `-h`) 지원
  - `/redfish/v1/Managers/1` 및 `/redfish/v1/Managers/1/EthernetInterfaces` 자동 탐색으로 iLO 4/5/6 세대(`Hp`/`Hpe` OEM 키) 및 Dedicated/Shared 포트 자동 식별, IP 경계 매칭 적용
  - 변경 전 원본 JSON 백업(`logs/backup_<timestamp>/<IP>.json`) 및 최종 요약 리포트(`logs/result_<timestamp>.log`) 저장
  - `rawpatch` 실패 시 `iloreset` 차단 및 개별 장비 오류 격리(배치 계속 진행)
  - RHEL 6 `prelink` 바이너리 손상(`Cannot open self`) 사전 감지 및 조치 안내
- **테스트 및 CI**
  - 상태 기반 Mock (`tests/mock_ilorest`) 및 7개 시나리오 E2E 자동 테스트 (`tests/run_tests.sh`) 추가
  - GitHub Actions 워크플로우 (`.github/workflows/ci.yml`) 추가
- **문서 및 템플릿**
  - `list.txt.example`, `.gitignore`, `README.md`, `ARCHITECTURE.md`, `CHANGELOG.md`, `PR_CHECKLIST.md`, `사용법.txt`, `계획서.md` 작성
