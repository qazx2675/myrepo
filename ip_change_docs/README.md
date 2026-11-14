# ip_change — Confluence 업로드 안내

- **원본 경로 (`origin/master`)**: `.claude/HPC/ip_change/`
- **대상 브랜치**: `confluence` 브랜치 / `ip_change_docs/` 폴더
- **양식 유형**: T3 (설정 변경 · 적용 엔진형 — `auto_setup` `m` 메뉴 및 `gossh` v2 연계 포함)
- **포함 파일**:
  - `ip_change.xhtml`: Confluence Storage Format 본문 프래그먼트
  - `README.md`: 본 업로드 가이드

## Confluence에 업로드(붙여넣기)하는 방법

1. `ip_change.xhtml` 파일의 전체 내용을 복사합니다. (외부 `<root>` 래퍼 없이 Confluence Storage Format 본문 프래그먼트만 저장되어 있습니다.)
2. Confluence에서 새 페이지를 생성하거나 기존 페이지 편집 화면으로 들어갑니다.
3. 편집기 상단 툴바에서 **Source Editor(`<>`)** 또는 저장 형식(Storage Format) 편집기를 엽니다.
4. 복사한 `ip_change.xhtml` 내용을 그대로 붙여넣고 **Apply** → 페이지 **저장**을 눌러 `info`, `toc`, `code`, `note`, `warning` 매크로와 표가 정상 렌더링되는지 확인합니다.

## XML 문법 검증 (선택)

```bash
C:/Users/qazx2/AppData/Local/Programs/Python/Python312/python.exe -c "import xml.etree.ElementTree as ET; s=open('ip_change.xhtml', encoding='utf-8').read(); ET.fromstring('<root xmlns:ac=\"http://atlassian.com/content\" xmlns:ri=\"http://atlassian.com/resource/identifier\">'+s+'</root>'); print('OK')"
```
