# survey 문서 안내

- **주요 파일**: `dist/survey`, `dist/survey-rhel6`, `run_survey.sh`, `conf/conf.toml`, `scripts/infra_survey.sh`, `update.sh`
- **양식 유형**: T2 (점검 · 수집형)
- **포함 파일**:
  - `survey.xhtml`: 저장 형식(Storage Format) 본문 프래그먼트
  - `README.md`: 본 문서 적용 안내

## 위키/문서 편집기 붙여넣기 방법

1. `survey.xhtml` 파일 전체 내용을 복사합니다. (외부 `<root>` 래퍼 없이 본문 프래그먼트만 저장되어 있습니다.)
2. 문서 편집 화면에서 상단 툴바의 **Source Editor(`<>`)** 또는 저장 형식(Storage Format) 편집기를 엽니다.
3. 복사한 `.xhtml` 내용을 그대로 붙여넣고 **Apply** / **저장**을 눌러 매크로(`info`, `toc`, `code`, `note`, `warning`)와 표가 정상 렌더링되는지 확인합니다.

## XML 문법 검증 (선택)

```bash
C:/Users/qazx2/AppData/Local/Programs/Python/Python312/python.exe -c "import xml.etree.ElementTree as ET; s=open('survey.xhtml', encoding='utf-8').read(); ET.fromstring('<root xmlns:ac=\"http://atlassian.com/content\" xmlns:ri=\"http://atlassian.com/resource/identifier\">'+s+'</root>'); print('OK')"
```
