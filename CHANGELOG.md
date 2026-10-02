# CHANGELOG

## [Unreleased]

## [0.2.1] - 2026-10-02

### 변경
- 01: `user()` 를 현장 코드(`info_mn.sh` 선택 메뉴 → `info.sh` 로 user 결정, `user_route` 는 빈 값)로 교체

## [0.2.0] - 2026-10-02

### 변경
- 01: `infra_alias` 추가(nodeinfo 의 미등록 infra 이름 치환, `adjfg:infra1`), 최상단 변수 7개
- 02: 그룹 yml 2개 이상이고 모두 성공하면 마지막에 전체 yml 로 invsync(AWX 소스 1단계)만 실행, 1개면 생략. 01 이 `--all=<yml>` 전달
- 02: 최종 수량 출력(On-premise=HPC, Cloud=SDS, `구분 infra OS버전 : N대` + 합계)
- 01: 마지막에 등록 후 확인(붙여넣은 서버가 모두 등록 대상에 있는지) 추가
- test: 7j·12a·12b·13a-c 추가, 전체 invsync 반영

## [0.1.3] - 2026-10-02

### 변경
- 02: 옵션 확인표 출력 전에 OS 버전 선택 추가(1=기본 2026-ECAD_TCAD, 2=yml 별로 2024/2025/2026/2026-OPC_MDP/2026-ECAD_TCAD 중 선택). 01 이 넘긴 os 값은 사용하지 않음
- test: 7i 추가, 02 입력에 OS 모드 응답 반영

## [0.1.2] - 2026-10-02

### 변경
- 01·02: 가독성용 색상 출력 추가(터미널일 때만, NO_COLOR 로 끔, 로그에는 색 코드 제외). LDAP 동일 요약은 LDAP 값만 초록, 메뉴 su=초록 / exit=빨강 / ls=청록
- test: 11a·11b(색상 켜짐/꺼짐, 로그에 색 코드 없음) 추가

## [0.1.1] - 2026-10-02

### 변경
- 02: infra 값을 소문자로 변환해 전달(conf `*_choices` 와 대소문자 불일치 방지)
- 02: dhcp 와 pxe 를 동시 실행하고 둘 다 끝나야 다음 yml 진행(출력은 완료 후 순서대로 표시, 한쪽 실패 시에도 다른 쪽은 완료)
- test: 7h(동시 실행 이벤트 순서) 추가, 호출 순서·infra 소문자 기대값 갱신

## [0.1.0] - 2026-10-02

### 신규
- `01.AWX_nodeinfo_V2.sh` — HPC 서버 노드정보 수집·가공·분할·yml 생성·git 업로드 자동화
- `02.source_dhcp_pxe.sh` — yml 파일별로 인벤토리/DHCP/PXE 자동 반복 등록
- `test/run_tests.sh` — 계획서 §9 검증 기준을 따르는 실제 실행 기반 하네스
- 문서: `README.md`, `CHANGELOG.md`, `ARCHITECTURE.md`, `WORKFLOW.md`, `PR_CHECKLIST.md`
- CI: `.github/workflows/ci.yml` (폴더 안), `.github/workflows/awx-script.yml` (저장소 루트)

### 보정사항 (계획서 §8 결정사항 반영)
- `download_txt()`: nodeinfo 실패 시 입력 파일(${user}.txt)을 덮어쓰지 않고 종료
- `parse_msg()` 함수: 빈 파일 검증 추가
- 입력 EOF 처리: 01 메뉴 / 02 옵션 확인표(Y|N) 에서 stdin 이 끝나면 `[X] 입력이 끝났습니다` 후 exit 1 (무한 루프 방지)
- `ai_server_list` 는 선택(빈 값이면 AI 안내만 건너뜀). 나머지 5개 변수는 작업진행 Y 직후 원격 삭제 전에 선검사
- `02 인자 형식 검증`: `<yml>=<infra>,<os>,<boot>,<splunk>` 형식 확인, 잘못된 형식 시 `[X]` + exit 1
- `inventory_delete 가드`: 작업진행(Y) 전에는 미호출, Y 직후 빈 변수 사전 검사 (`require_var` + `exit 1`)
- `splitdir/hostfile` 임시 파일: `mktemp`/`mktemp -d` + `trap cleanup EXIT` 로 정리
- `yml 수집`: `svr_dir` 전후 목록 비교(`comm -13`) 로 정확히 1개 신규 파일 검증
- 로컬 분할 파일: `mktemp -d` 안에서만 생성, scp 후 자동 삭제 (로컬 잔여 없음)
- `user()` 함수: 빈 본문 유지 (`:` 또는 주석만), 사용자가 현장 코드 직접 삽입
- Shellcheck SC2006/SC2086/SC2116/SC2154/SC2164 git 블록 원문 보존 부분에만 disable
- UTF-8/LF 파일 인코딩 통일

### 주의사항
- 테스트 값은 스크립트 본체에 들어 있지 않으며, `test/` 하네스가 스크래치 복사본에만 sed 로 주입함
- 설정 변경 스크립트이므로 실행 후 랜덤한 서버 몇 대를 확인해 실제로 변경되었는지 검증 필수
- `inventory_delete` 는 원격 삭제 기능이므로 '작업진행 Y' 이후에만 실행되며, 실행 전 최종 확인 권장
- 계획서 §10 알려진 한계(custom_inventory.sh 덮어쓰기, infra/os 문자열 일치 필요, gossh 색상 꺼짐, sed 7T 부작용) 인식 필요
