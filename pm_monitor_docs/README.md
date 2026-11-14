# pm_monitor Confluence 문서 업로드 가이드

- **원본 저장소 경로 (`origin/master`)**: `.claude/HPC/pm_monitor`
- **대상 브랜치**: `confluence` 브랜치 / `pm_monitor_docs/` 폴더
- **Confluence 본문 파일**: [`pm_monitor.xhtml`](./pm_monitor.xhtml)

## Confluence Source Editor 업로드 방법

1. Confluence에서 문서를 등록할 대상 페이지로 이동한 뒤 편집(Edit) 모드로 전환합니다.
2. 상단 툴바의 **`<>` (Source Editor / 소스 편집기)** 버튼을 엽니다.
3. [`pm_monitor.xhtml`](./pm_monitor.xhtml) 파일 전체 내용을 복사하여 Source Editor 창에 그대로 붙여넣습니다.
   - 주의: 본문은 Confluence Storage Format 조각(`<root>` 래퍼 없음)으로 작성되어 있으므로 별도의 XML 래퍼를 추가하지 마세요.
4. **Apply** 클릭 후 상단 목차(`toc`), 정보/경고 매크로(`info`, `warning`, `note`), 코드 블록(`code`), 표(`table`) 렌더링을 확인하고 페이지를 저장(Publish)합니다.

## 포함 내용 요약

- 5단계 수집 파이프라인(Raw ICMP Ping → TCP 22 → `gossh -pm` → `/tmp/anaconda.log` 디스크 미인식 → 지연/상태 저장) 및 `ldap_check.sh` / `dhcp_server` 연동 경로
- 폐쇄망 사전 빌드 바이너리(`install_mn_os8`), 심볼릭 링크 전역 설치(`install_mn.sh`), `build.sh`, `demo/demo.sh`
- CLI 사용법, 종료 코드(`0`/`1`/`2`), TUI 키 조작(`c`/`r`/`d`/`l`/`1`~`7`/`/`), 7종 상태 판정 표, `l` 패널 열 규칙, `pm_monitor.conf` 전체 11개 키 및 면책/운영 주의사항
