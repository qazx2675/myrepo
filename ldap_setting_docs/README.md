# ldap_setting — Confluence 업로드 안내

- **원본 경로 (`origin/master`)**: `.claude/HPC/ldap_setting/`
- **대상 브랜치**: `confluence` 브랜치 / `ldap_setting_docs/` 폴더
- **양식 유형**: T3 (설정 변경 · 적용 엔진형 — `dry-run` → 소수 검증 → 전체 적용 → `ldap_check` 사후 검증 및 `auto_setup` `m` 메뉴 연계 포함)
- **포함 파일**:
  - `ldap_setting.xhtml`: Confluence Storage Format 본문 프래그먼트
  - `README.md`: 본 업로드 가이드

## Confluence에 업로드(붙여넣기)하는 방법

1. `ldap_setting.xhtml` 파일의 전체 내용을 복사합니다. (외부 `<root>` 래퍼 없이 Confluence Storage Format 본문 프래그먼트만 저장되어 있습니다.)
2. Confluence에서 새 페이지를 생성하거나 기존 페이지 편집 화면으로 들어갑니다.
3. 편집기 상단 툴바에서 **Source Editor(`<>`)** 또는 저장 형식(Storage Format) 편집기를 엽니다.
4. 복사한 `ldap_setting.xhtml` 내용을 그대로 붙여넣고 **Apply** → 페이지 **저장**을 눌러 `info`, `toc`, `code`, `note`, `warning` 매크로와 표가 정상 렌더링되는지 확인합니다.

## XML 문법 검증 (선택)

```bash
C:/Users/qazx2/AppData/Local/Programs/Python/Python312/python.exe -c "import xml.etree.ElementTree as ET; s=open('ldap_setting.xhtml', encoding='utf-8').read(); ET.fromstring('<root xmlns:ac=\"http://atlassian.com/content\" xmlns:ri=\"http://atlassian.com/resource/identifier\">'+s+'</root>'); print('OK')"
```
