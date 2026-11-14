# copy Confluence 문서 업로드 가이드

- **원본 저장소 경로 (`origin/master`)**: `.claude/HPC/copy`
- **대상 브랜치**: `confluence` 브랜치 / `copy_docs/` 폴더
- **Confluence 본문 파일**: [`copy.xhtml`](./copy.xhtml)

## Confluence Source Editor 업로드 방법

1. Confluence에서 문서를 게시할 페이지를 열고 편집(Edit) 모드로 들어갑니다.
2. 상단 툴바에서 **`<>` (Source Editor / 소스 편집기)** 창을 엽니다.
3. [`copy.xhtml`](./copy.xhtml) 전체 내용을 복사해 붙여넣습니다.
   - 본문은 Confluence Storage Format 조각(`<root>` 태그 없음)이므로 앞뒤에 별도 루트 태그를 추가하지 마세요.
4. **Apply** 후 목차(`toc`), 경고/정보/참고 매크로(`warning`, `info`, `note`), 코드 블록(`code`), 표(`table`)가 정상 렌더링되는지 확인한 뒤 저장(Publish)합니다.

## 포함 내용 요약

- ETX `copy-send` → AWX Job Template `description` → Windows `copy-widget.exe` 클립보드 릴레이 구조 및 원자적 순서 보장
- 외부 의존성 없는 Go 표준 라이브러리 전용 폐쇄망 빌드(`setup.sh`, `build.bat`, `go.mod` `1.26.5` / README 표기 `1.27.0`) 및 `go vet` Windows 오탐 안내
- `ResolvePath` 4단계 설정 탐색 순서, `copy_setting.conf` 6개 키, CLI 플래그·종료 코드, 위젯 마우스 조작(좌클릭/드래그/우클릭), 2,000바이트 미리보기 동작 및 보안/더미 템플릿 주의사항
