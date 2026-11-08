# CHANGELOG

## [0.1.1] - 2026-11-08

### 추가
- `demo/demo.sh`: 가짜 gossh·ssh·호스트 27대로 데모 환경을 한 번에 만들고 `install_mn` 을 실행, 종료 시 자동 정리. 시간에 따라 설치완료·지연·완료후꺼짐·재부팅됨이 나타난다.

## [0.1.0] - 2026-11-08

### 추가
- 최초 버전. `install_mn <호스트파일>` 로 OS 설치 진행 상태(설치중·지연·디스크미인식·설치완료·ping X·재부팅됨·완료후꺼짐)를 1분마다 갱신하는 TUI.
- raw ICMP(`ping_pps` 제한) · 22번 포트(`tcp_parallel` 제한) · `gossh -pm` 으로 판정, 상태·이력은 `<호스트파일>.pm_state.json` 에 저장.
- 키: 스크롤, `/` 검색, `1`~`7` 필터, `c` 설치현황 코멘트, `r` 점검요청 코멘트, `d` DHCP 조회, `l` LDAP·DNS·NTP·autofs·H/T, `q` 종료.
- `build.sh`(os8 정적 바이너리 `install_mn_os8`), `install_mn.sh` 래퍼, 문서 일체.
