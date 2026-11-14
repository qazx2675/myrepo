# `ldap_setting` — 위키 문서 업로드 안내

- **양식 유형**: T3 (설정 변경 · 적용 엔진형 — `dry-run` → 소수 검증 → 전체 적용 → `ldap_check` 사후 검증 및 `auto_setup` `m` 메뉴 연계 포함)
- **포함 파일**:
  - [`ldap_setting.xhtml`](ldap_setting.xhtml): 위키 Storage Format 본문 프래그먼트 (루트 래퍼 없음)
  - [`README.md`](README.md): 본 업로드 가이드

## 사내 위키에 업로드(붙여넣기)하는 방법

1. [`ldap_setting.xhtml`](ldap_setting.xhtml) 파일의 전체 내용을 복사합니다. (외부 `<root>` 래퍼 없이 Storage Format 본문 프래그먼트만 저장되어 있습니다.)
2. 사내 위키에서 새 페이지를 생성하거나 기존 페이지 **편집(Edit)** 화면으로 들어갑니다.
3. 편집기 상단 툴바에서 **Source Editor(`<>`, 소스 편집기)** 또는 Storage Format 편집 창을 엽니다.
4. 복사한 `.xhtml` 내용을 그대로 붙여넣고 **Apply / 적용** → 페이지 **저장**을 눌러 `info`, `toc`, `code`, `note`, `warning` 매크로와 표가 정상 렌더링되는지 확인합니다.
