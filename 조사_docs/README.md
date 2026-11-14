# survey — Confluence 업로드 안내

- **원본 경로 (`origin/master`)**: `.claude/HPC/조사/survey/`
- **대상 브랜치**: `confluence` 브랜치 / `조사_docs/` 폴더
- **양식 유형**: T2 (점검 · 수집형)
- **포함 파일**:
  - `survey.xhtml`: Confluence Storage Format 본문 프래그먼트
  - `README.md`: 본 업로드 가이드

## Confluence에 업로드(붙여넣기)하는 방법

1. `survey.xhtml` 파일 전체 내용을 복사합니다. (외부 `<root>` 래퍼 없이 Confluence Storage Format 본문만 저장되어 있습니다.)
2. Confluence에서 새 페이지를 만들거나 기존 페이지 편집 화면을 엽니다.
3. 상단 툴바의 **Source Editor(`<>`)** 또는 저장 형식(Storage Format) 편집기를 엽니다.
4. 복사한 `.xhtml` 내용을 그대로 붙여넣고 **Apply** / **저장**을 눌러 매크로(`info`, `toc`, `code`, `note`, `warning`)와 표가 정상 렌더링되는지 확인합니다.

## XML 문법 검증 (선택)

```bash
C:/Users/qazx2/AppData/Local/Programs/Python/Python312/python.exe -c "import xml.etree.ElementTree as ET; s=open('survey.xhtml', encoding='utf-8').read(); ET.fromstring('<root xmlns:ac=\"http://atlassian.com/content\" xmlns:ri=\"http://atlassian.com/resource/identifier\">'+s+'</root>'); print('OK')"
```
