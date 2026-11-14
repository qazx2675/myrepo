# awxkit Confluence 문서 업로드 가이드

- **원본 프로젝트 경로 (`origin/master`)**: `.claude/HPC/awxkit`
- **대상 브랜치**: `confluence` 브랜치 / `awxkit_docs/` 폴더
- **포함 파일**:
  - `awxkit.xhtml`: Confluence Storage Format(`.xhtml`) 본문 조각 (`<root>` 래퍼 없음, Template T5 통합 Suite 양식)
  - `README.md`: 본 업로드 안내 문서

## Confluence 업로드(붙여넣기) 방법

1. 사내 Confluence에서 문서를 등록하거나 수정할 페이지를 열고 **편집(Edit)** 모드로 진입합니다.
2. 편집기 우측 상단(또는 도구 모음)의 **Source Editor (`<>`, 소스 편집기)** 아이콘을 클릭합니다.
   - *(Source Editor 앱이 설치되어 있지 않은 환경에서는 Confluence REST API 또는 Storage Format 붙여넣기 메뉴를 사용합니다.)*
3. 이 폴더의 `awxkit.xhtml` 파일 내용 **전체**를 복사해 Source Editor 입력창에 그대로 붙여넣습니다.
4. **적용(Apply)** 을 누른 뒤 미리보기에서 `info`, `toc`, `warning`, `note`, `code` 매크로와 역할 매트릭스·설정 키 표가 정상 렌더링되는지 확인하고 페이지를 **저장(Publish/Save)** 합니다.
