# auto_setup Confluence 문서 업로드 가이드

## 1. 대상 정보

- **원본 소스 경로 (`origin/master`)**: `.claude/HPC/auto_setup`
- **Confluence 문서 브랜치**: `confluence` 브랜치 / `auto_setup_docs/` 폴더
- **업로드할 XHTML 파일**: `auto_setup.xhtml` (Confluence Storage Format 본문 프래그먼트)

## 2. Confluence 붙여넣기 방법

1. 사내 Confluence에서 새 페이지를 만들거나 기존 문서를 **편집(Edit)** 모드로 엽니다.
2. 상단 에디터 툴바에서 **Source Editor (`<>`)** 아이콘(또는 Storage Format 편집 기능)을 엽니다.
3. `auto_setup.xhtml` 파일의 전체 내용을 복사하여 Source Editor 창에 그대로 붙여넣습니다.
   - `<root>` 래퍼 태그 없이 본문 매크로(`<ac:structured-macro ...>`)와 HTML 태그로 바로 시작하는 형식입니다.
4. **Apply / 적용**을 눌러 에디터 화면으로 돌아온 뒤 다음 항목이 정상 렌더링되는지 확인하고 저장합니다.
   - 상단 메타 `info` 박스 및 목차(`toc`)
   - 아키텍처·명령어 `code` 매크로 및 파라미터/TUI 키 표(`table`)
   - 주의사항 `warning`·`note` 매크로
