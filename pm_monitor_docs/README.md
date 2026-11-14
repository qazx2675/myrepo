# pm_monitor 운영 문서 안내

## 1. 개요 및 실행 구성

- **대상 도구**: `pm_monitor` (대규모 베어메탈 OS 설치 모니터링 TUI)
- **실행 파일**: 사전 빌드된 정적 바이너리 `install_mn_os8` 및 실행 래퍼 `install_mn.sh` (`/usr/local/bin/install_mn` 심볼릭 링크 등록, root 권한 필수)
- **주요 연계 도구**: `gossh -pm`, `ldap_check.sh`, `dhcp_server` (`/root/a.sh`)
- **본문 문서 파일**: [`pm_monitor.xhtml`](./pm_monitor.xhtml) (Storage Format XML 본문 조각)

## 2. 사내 위키 소스 편집기 붙여넣기 방법

1. 사내 위키에서 문서를 등록할 대상 페이지로 이동한 뒤 **편집(Edit)** 모드로 전환합니다.
2. 상단 툴바의 **`<>` (Source Editor / 소스 편집기)** 버튼을 엽니다.
3. [`pm_monitor.xhtml`](./pm_monitor.xhtml) 파일 전체 내용을 복사하여 소스 편집기 창에 그대로 붙여넣습니다.
   - `<root>` 래퍼 태그 없는 Storage Format 본문 조각이므로 앞뒤에 별도 루트 태그를 추가하지 마세요.
4. **Apply / 적용**을 누른 뒤 상단 목차(`toc`), `info`·`warning`·`note` 매크로, 코드 블록(`code`), 표(`table`) 렌더링을 확인하고 저장합니다.

## 3. 수록 내용 요약

- 5단계 수집 파이프라인(Raw ICMP Ping → TCP 22 → `gossh -pm` → `/tmp/anaconda.log` 디스크 미인식 → 지연/상태 저장) 및 `ldap_check.sh` / `dhcp_server` 연동 경로
- 사전 빌드 바이너리(`install_mn_os8`) 및 `install_mn.sh` 심볼릭 링크 전역 등록, `pm_monitor.conf` 준비, `demo/demo.sh` 시연
- CLI 사용법, 종료 코드(`0`/`1`/`2`), TUI 키 조작(`c`/`r`/`d`/`l`/`1`~`7`/`/`), 7종 상태 판정 표, `l` 패널 열 규칙, `pm_monitor.conf` 전체 11개 설정 키 및 실서버 교차 검증 주의사항
