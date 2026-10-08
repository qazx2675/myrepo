# ARCHITECTURE.md

| 폴더/파일 | 역할 |
|---|---|
| collect.sh | 실행 래퍼. BMC 계정(평문 하드코딩), OS 별 바이너리 선택, 결과 폴더 지정 |
| main.go | 수집 본체. list.txt/hosts 파싱 → Redfish GET(Systems → Bios → Bios/Settings) → JSON 저장 |
| ci.yml.example | CI 워크플로 (루트 .github/workflows/ 로 복사해 사용) |
| setup.sh | 오프라인 빌드 (`GOPROXY=off`, `-mod=vendor`) |
| go.mod | 모듈 정의 (외부 의존성 없음, vendor/ 불필요) |
| biosdump | 빌드 완료 바이너리 (Rocky/RHEL8, go1.25 정적) |
| list.txt.example | 대상 hostname 목록 예시 |
| 사용법.txt | 수동 실행용 명령 모음 |

수정 요청이 "수집 항목 추가"면 main.go 의 `collect()`, "관리망 이름 규칙 변경"이면 `-suffix`/`loadHosts()` 만 보면 됨.
작업 흐름도는 [WORKFLOW.md](WORKFLOW.md) 참고.
