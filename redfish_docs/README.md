# `redfish` (`biostool` v0.1.0) — 위키 문서 업로드 안내

- **양식 유형**: T3 (설정 변경 / 프로비저닝 엔진, 사전 빌드 바이너리 + Bash 래퍼)
- **포함 파일**:
  - [`redfish.xhtml`](redfish.xhtml): 위키 Storage Format 본문 프래그먼트 (루트 래퍼 없음)
  - [`README.md`](README.md): 본 업로드 가이드

> **주의**: v0.1.0은 실제 물리 BMC에 아직 연결 테스트되지 않았으며 모의 BMC 및 덤프 기반 검증만 완료되었습니다. 실장비 연결 전 반드시 프로젝트 루트의 `FIRST_RUN.md`를 확인하십시오.

## 사내 위키에 업로드(붙여넣기)하는 방법

1. [`redfish.xhtml`](redfish.xhtml) 파일의 전체 내용을 복사합니다. (외부 `<root>` 래퍼 없이 Storage Format 본문 프래그먼트만 저장되어 있습니다.)
2. 사내 위키에서 새 페이지를 생성하거나 기존 페이지 **편집(Edit)** 화면으로 들어갑니다.
3. 편집기 상단 툴바에서 **Source Editor(`<>`, 소스 편집기)** 또는 Storage Format 편집 창을 엽니다.
4. 복사한 `.xhtml` 내용을 그대로 붙여넣고 **Apply / 적용** → 페이지 **저장**을 눌러 `info`, `toc`, `code`, `warning`, `note` 매크로와 표가 정상 렌더링되는지 확인합니다.
