# `vm_ip_change` (`vm-ip-change`) Confluence 업로드 안내

- **원본 경로 (`origin/master`)**: `.claude/VM/vCenter API IP 자동변경` (Go 프로젝트 루트: `project/`)
- **대상 브랜치**: `confluence` 브랜치 / `vCenter_API_IP_자동변경_docs/` 폴더
- **양식 유형**: T3 (설정 변경 / 프로비저닝 엔진, Go CLI/TUI)
- **포함 파일**:
  - [`vm_ip_change.xhtml`](vm_ip_change.xhtml): Confluence Storage Format 본문 (루트 래퍼 없음)
  - [`README.md`](README.md): 업로드 및 활용 가이드

## Confluence에 업로드(붙여넣기)하는 방법

1. Confluence에서 대상 문서를 열거나 **새 페이지 만들기**를 클릭합니다.
2. 상단 우측 편집기 도구 모음에서 **소스 편집기(`< >` 또는 `Source Editor`)** 플러그인 아이콘을 엽니다. *(소스 편집기가 없는 경우 페이지 편집 시 Insert More Content → Markup 또는 Source Editor 앱 필요)*
3. [`vm_ip_change.xhtml`](vm_ip_change.xhtml) 파일 전체 내용을 복사하여 소스 편집기에 그대로 붙여넣습니다.
4. **적용(Apply)** 을 누른 뒤 매크로(`info`, `toc`, `code`, `warning`, `note`), 표(Table), 본문 한글이 정상 렌더링되는지 확인하고 저장합니다.
5. 본 문서에는 최상위 `<root>` 래퍼가 제외되어 있으므로 별도 태그 삭제 없이 바로 붙여넣을 수 있습니다.
