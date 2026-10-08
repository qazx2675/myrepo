# ARCHITECTURE.md

| 폴더/파일 | 역할 |
|---|---|
| 조사.sh | 실행 래퍼 (list.txt/bios_json 확인, 바이너리 없으면 setup.sh) |
| main.go | 비교 본체: list 파싱 → 모델 판단 → bios_json 폴더 매칭 → 이름 추출 → 양방향 비교 → 결과 저장 |
| setup.sh | 오프라인 빌드 (`GOPROXY=off`, `-mod=vendor`) |
| ignore.txt | 비교 제외 정규식 (시리얼·MAC 등) |
| bios_json/ | 웹 조사 자료 (`<벤더>/<모델폴더>/bios_attributes.json·bios_tokens.json`) |
| bios_compare | 빌드 완료 바이너리 (RHEL8/Rocky) |
| ci.yml.example | CI 워크플로 (루트 .github/workflows/ 로 복사) |

수정 요청이 "모델 판단 규칙"이면 main.go 의 `canon()`/`aliases()`, "제외 항목"이면 ignore.txt, "조사 파일 형식 추가"면 `researched()` 만 보면 됨.
작업 흐름도는 [WORKFLOW.md](WORKFLOW.md) 참고.
