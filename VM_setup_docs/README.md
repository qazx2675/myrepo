# vm_setup Confluence 문서 업로드 가이드

## 대상 정보
- **원본 프로젝트 (`origin/master`)**: `.claude/VM/VM_setup`
- **대상 작업 브랜치**: `confluence` 브랜치 / `VM_setup_docs/` 폴더
- **Confluence 본문 파일**: `vm_setup.xhtml` (Template T5 — 레거시 V1 Multi-Tool Suite 통합 가이드)

## 포함 내용 요약
1. **Suite 전체 개요 및 단계별 흐름**: 9개 `*-source` 도구 + `vm-param-fix` + `power_setting` 바이너리 수동 구축 및 보정 흐름
2. **하위 도구 역할 매트릭스**: `main_conn`, `license_assign`, `vswitch_setting`, `vm_create`, `affinity_setting`, `lpage_setting`, `tag_setting`, `numa_preferht_setting`, `mac_info`, `vm-param-fix`, `power_setting`
3. **개별 오프라인 빌드/배치**: 디렉터리별 독립 `vendor/` + `setup.sh` 빌드 및 `power_setting` 바이너리 전용 보존 경고
4. **통합 보정(`vm-param-fix`) vs 단독 실행법**: 사전 게이트, CSV 태그 분류, CLI 실행 예시
5. **하위 도구별 핵심 옵션/입력 파일/출력 요약**
6. **통합 주의사항, 롤백/실서버 표본 확인 및 V2 / `vm-param-check -fix` 전환 비교**

## Confluence Source Editor 붙여넣기 방법
1. 대상 Confluence 페이지를 편집 모드로 엽니다.
2. 우측 상단(또는 에디터 툴바)의 **`<>` (Source Editor)** 버튼을 클릭하여 Storage Format 소스 편집창을 엽니다.
   - *참고: `vm_setup.xhtml`은 바깥 `<root>` 래퍼 없이 Confluence Storage Format 본문 프래그먼트만 포함하고 있습니다.*
3. `vm_setup.xhtml` 파일의 전체 내용을 복사해 Source Editor에 그대로 붙여넣고 **Apply**를 누릅니다.
4. 미리보기에서 `info`, `toc`, `warning`, `note`, `code` 매크로와 HTML `<table>` 렌더링이 정상인지 확인한 뒤 페이지를 **저장(Publish)**합니다.
