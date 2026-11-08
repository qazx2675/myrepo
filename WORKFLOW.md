# WORKFLOW

전체 그림은 [workflow.svg](workflow.svg), 사용법은 [README.md](README.md), 파일 역할은 [ARCHITECTURE.md](ARCHITECTURE.md).

## 수집 회차 (기본 1분, 백그라운드 고루틴)

1. **ping**: 완료·완료후꺼짐을 제외한 모든 서버에 raw ICMP 를 `ping_pps` 이하 속도로 보낸다. 무응답이면 ping X.
2. **22번 포트**: ping O 인 미완료 서버에 TCP 연결(동시 `tcp_parallel`). refused 이면 설치중.
3. **gossh -pm**: 22번이 열린 미완료 서버만 `gossh -pm -w <임시목록> "echo PM_OK"`. `PM_OK` 이면 설치완료, OS 설치중 목록이면 설치중.
4. **디스크 확인**: 설치중이고 ssh 가 되는 서버에 gossh 로 `/tmp/anaconda.log` 의 `disk_pattern` 확인 → 디스크미인식.
5. 상태·이력 갱신, 지연 판정, 바뀐 것이 있으면 `<호스트파일>.pm_state.json` 저장(임시 파일에 쓰고 이름 변경), 스냅샷을 TUI 로 전달.
6. 다음 회차까지 쉰다. 앞 회차가 `interval_sec` 를 넘기면 끝난 뒤에 다음 회차를 시작한다(겹치지 않음).

TUI 는 스냅샷만 받아 그리므로 수집 중에도 키 입력에 즉시 반응한다. 1초 tick 은 시계 글자만 바꾼다.

## 주요 키 흐름

- **`c` 설치현황**: 완료 서버에 ping → ping X 면 완료후꺼짐, 완료후꺼짐이 ping O 면 재부팅됨 → 요약 + 벤더별(디스크미인식·접속불가·완료후꺼짐) 목록 출력.
- **`r` 점검요청**: 지연 서버를 벤더별로 묶어 점검 요청 문구 출력.
- **`d` DHCP**: ping X 목록을 stdin 으로 `ssh <dhcp_server>` 에 넘겨 원격 `a.sh` 결과를 스크롤 창에 표시(로컬·원격에 파일 안 남김).
- **`l` LDAP·H/T**: 결과가 비어 있는 설치완료 서버만 `gossh -script` 로 ldap_check.sh 와 H/T 를 실행 (DNS·NTP·auto.appl 값도 함께 수집) → LDAP infra · DNS·NTP ip1/ip2 또는 다름 · autofs 마운트 위치 · H/T · 비고로 요약, 값은 state.json 에 보관.
- **`/` 검색, `1`~`7` 필터, `0` 해제, ↑↓ PgUp PgDn Home End 스크롤, `q`/Ctrl+C 종료(터미널 복구)**.
