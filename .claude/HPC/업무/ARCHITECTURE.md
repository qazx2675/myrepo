# ARCHITECTURE.md

| 폴더/파일 | 역할 |
|---|---|
| main.go | CLI 진입점, 전체 흐름 조립 및 REPL 구성 |
| hangul.go | 영타 -> 한타(2벌식) 자동 변환 로직 (순수 Go 내부 구현) |
| jev.go | Typesafe JEV API 연동 및 키워드 기반 분류/점수 채점, 매뉴얼 응답 매핑 |
| vendor/ | 폐쇄망 환경을 위한 모든 외부 의존성(라이브러리) 보관 |
| PLAN.md | 챗봇 프로젝트 기획 및 구축 계획서 |

> **수정 팁**: 새로운 기능 "질의응답 룰 추가" 요구사항이 있는 경우, `<jev.go>`의 `categories` 및 `manualResponses` 맵핑만 확인하고 수정하면 됩니다. AI 연동 고도화 시에도 `jev.go`를 확장하세요.
