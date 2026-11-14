# auto_setup 운영 문서 안내

## 1. 개요 및 실행 구성

- **대상 도구**: `auto_setup` (데몬 + CLI/TUI 통합 자동화 도구, v0.8.8 기준)
- **실행 바이너리**: 사전 빌드된 정적 바이너리 `auto_setup_os8` (`/usr/local/bin/auto_setup`으로 복사·권한 부여 후 실행)
- **주요 연계 도구**: `01.AWX_nodeinfo_V2.sh`, `config_check.sh` (`os_check_sh` / `os6_os_check_sh`), `ip-change-engine`, `ldap-config-engine`, `gossh`
- **본문 문서 파일**: [`auto_setup.xhtml`](./auto_setup.xhtml) (Storage Format XML 본문 조각)

## 2. 사내 위키 소스 편집기 붙여넣기 방법

1. 사내 위키에서 새 페이지를 만들거나 기존 문서를 **편집(Edit)** 모드로 엽니다.
2. 상단 에디터 툴바에서 **Source Editor (`<>`)** 또는 Storage Format 소스 편집 창을 엽니다.
3. [`auto_setup.xhtml`](./auto_setup.xhtml) 파일 전체 내용을 복사하여 소스 편집기에 그대로 붙여넣습니다.
   - `<root>` 래퍼 태그 없이 `<ac:structured-macro ...>`와 HTML 태그로 바로 시작하는 본문 조각입니다.
4. **Apply / 적용**을 눌러 상단 `info` 박스, 목차(`toc`), 아키텍처·명령어 `code` 매크로, 설정/TUI 표(`table`), 주의사항 `warning`·`note` 매크로 렌더링을 확인한 뒤 저장합니다.

## 3. 수록 내용 요약

- `01.AWX_nodeinfo_V2.sh` → 데몬 → LDAP 백업/복원 → `config_check.sh` (`-auto` / `-auto-check`) → 2차 체크 → `codes/` 결과 공유까지의 엔드투엔드 흐름
- `os8_mgmt` 데몬 서버와 `os6_mgmt` 원격 클라이언트 공용 바이너리(`auto_setup_os8`) 배치, `/etc/auto_setup/auto_setup.conf` 작성, `cron`(`ensure`)·`/etc/tmpfiles.d/auto_setup.conf` 등록 절차
- `auto_setup.conf` 36개 설정 키(`vars.manifest` 32개 + `conf` 전용 4개) 및 환경변수, 데몬/CLI/TUI 조작, `x`·`w`·`s`·`m`·`os6_truble.sh` 장애 진단 및 실서버 교차 검증 주의사항
