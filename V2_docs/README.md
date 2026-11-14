# V2 운영 문서 업로드 가이드

- **대상 도구**: `V2` (`VMsetup/vm_setup.sh` + 12종 하위 도구 + `vm-param-check`)
- **포함 파일**:
  - `vm_v2.xhtml`: 위키 Source Editor 붙여넣기용 XHTML 본문 조각 (`<root>` 래퍼 없음, Template T5 통합 Suite 양식)
  - `README.md`: 본 업로드 안내 문서

## 위키(Source Editor) 업로드(붙여넣기) 방법

1. 사내 위키에서 문서를 등록하거나 수정할 페이지를 열고 **편집(Edit)** 모드로 진입합니다.
2. 편집기 우측 상단(또는 도구 모음)의 **Source Editor (`<>`, 소스 편집기)** 아이콘을 클릭합니다.
   - *(Source Editor 메뉴가 없는 환경에서는 Storage Format XML 입력 기능을 사용합니다.)*
3. 이 폴더의 `vm_v2.xhtml` 파일 내용 **전체**를 복사해 Source Editor 입력창에 그대로 붙여넣습니다.
4. **적용(Apply)** 을 누른 뒤 미리보기에서 `info`, `toc`, `warning`, `note`, `code` 매크로와 도구 매트릭스·플래그 표가 정상 렌더링되는지 확인하고 페이지를 **저장(Publish/Save)** 합니다.
