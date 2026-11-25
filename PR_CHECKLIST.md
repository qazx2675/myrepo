# PR 및 배포 전 검증 체크리스트 (`PR_CHECKLIST.md`)

코드 변경 또는 `ilo` 브랜치 반영 전 아래 항목을 모두 확인합니다.

- [ ] **Bash 문법 검사 (`bash -n`) 통과**
  - `bash -n change_ilo_ip.sh tests/mock_ilorest tests/run_tests.sh` 실행 시 오류 없음 확인
- [ ] **Mock E2E 테스트 (`tests/run_tests.sh`) 전체 통과**
  - `bash tests/run_tests.sh` 실행 시 아래 시나리오 모두 PASS 확인:
    - `--check` 사전 점검 모드 (iLO 4 Shared `/2/` `Hp`, iLO 5 Dedicated `/1/` `Hpe`, iLO 6 `/1/`)
    - 대화형 파이프 입력(`y` 변경 모드) 및 기본 넷마스크·자동 `.1` 게이트웨이·VLAN 활성화/해제(`0`/`off`) 적용
    - 대화형 `c` 모드 선택 시 변경 없이 사전 점검 전환
    - 일부 장비 인증 실패 / `rawpatch` 실패 시 배치 중단 없이 진행, 패치 실패 장비 `iloreset` 미호출, 최종 종료 코드 `1` 반환
    - `--netmask`, `--gw`, `-y` CLI 오버라이드 정확성
    - 잘못된 `list.txt`(오류 IP/VLAN) 사전 차단 및 `MOCK_PRELINK_ERR=1` RHEL 6 `prelink` 안내 출력
    - IP 경계 매칭(`10.0.0.1` vs `10.0.0.10`) 오탐 방지
- [ ] **대화형 프롬프트 흐름 검증**
  - 계정 ID 기본값(`Administrator`), 비밀번호 숨김 입력(`read -r -s`), 서브넷 기본값(`255.255.255.0`), 게이트웨이 자동(`.1`)/고정 입력, 모드 선택(`[y/c/n]`) 정상 동작 확인
- [ ] **실행 권한(`100755`) 및 개행(`LF`) 확인**
  - `change_ilo_ip.sh`, `tests/mock_ilorest`, `tests/run_tests.sh`가 Git 인덱스상 실행 권한(`100755`)으로 등록되어 있는지 확인
  - 모든 셸 스크립트, 테스트, 문서 파일이 `LF(\n)` 개행인지 확인 (`CRLF` 혼입 금지)
- [ ] **문서 정합성 확인**
  - `README.md`, `ARCHITECTURE.md`, `CHANGELOG.md`, `사용법.txt`, `계획서.md`의 CLI 옵션명, 환경변수명, 파일명(`list.txt`, `list.txt.example`), 로그 경로(`logs/backup_*`, `logs/result_*.log`), `ilo` 브랜치 안내가 실제 스크립트와 일치하는지 확인
  - `README.md` 내 필수 권장 고지(보조 도구 권장 및 표본 교차 검증 안내) 포함 확인
