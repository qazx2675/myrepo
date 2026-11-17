# CHANGELOG

## 2.0.0 (2026-11-17)
- Go 단일 바이너리 `awx_copy -u {user} [file] [-csv]` (bash 래퍼 삭제). 설정은 `/etc/awx_copy/{user}.conf`.
- 전송 형식을 평문 텍스트(YAML 리터럴 블록)로 변경. 조각별·전체 SHA-256 검증. 텍스트 전용.
- 업로드 기록 TUI, `-u root` 전체 기록 조회(SHA-256 비밀번호).
- 윈도우 창 앱 `awx_fetch.exe` (받은 파일 관리, 기록, 자동 감시, DPAPI).
- 사용 목적 주석 추가: 서버 설정값 비교용, 텍스트 기록 목적 전용.
- 폐쇄망 빌드(`setup.sh`), CI, 문서 정리.

## 2.0.1 (2026-11-18)
- https 자체서명 인증서에서 "x509: certificate signed by unknown authority" 로 연결 테스트가 실패하던 문제 수정. 기본은 인증서 검증 안 함, conf 의 verify_tls = true 로 검증 가능.
- url 에 브라우저 주소(#/inventories/inventory/N/details)를 그대로 붙여 넣어도 동작. 주소에서 inventory_id 를 읽는다.
