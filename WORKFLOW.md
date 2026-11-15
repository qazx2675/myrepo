# WORKFLOW

전체 그림은 [workflow.svg](workflow.svg), 사용법은 [README.md](README.md), 파일 역할은 [ARCHITECTURE.md](ARCHITECTURE.md).

## 백그라운드 수집 (v0.3.0)

1. `install_mn <호스트파일>` 이 같은 호스트파일의 수집 데몬(`/run/install_mn/<id>.sock`)을 찾는다. 있고 호스트파일·설정 내용이 같으면 그대로 붙는다.
2. 없으면(또는 내용이 바뀌었으면 멈추고) 자기 자신을 `--daemon` 으로 다시 실행해 터미널에서 분리하고 시작을 기다린 뒤 붙는다.
3. 화면은 스냅샷을 받아 그리고, `c`·`d`·`l` 은 데몬에 요청한다. `q`·Ctrl+C·연결 끊김은 화면만 닫고, `Q`+`y`·`--stop` 은 수집을 끝낸다.
4. 데몬은 시작 12시간 뒤 스스로 끝나고 붙은 화면도 닫힌다. 상태 파일은 남는다.

## 수집 회차 (기본 1분, 데몬의 고루틴)

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
- **`/` 검색, `1`~`7` 필터, `0` 해제, ↑↓ PgUp PgDn Home End 스크롤, `q`(패널은 뒤로가기, 기본 화면은 화면만 나가기)/Ctrl+C 화면 닫기(터미널 복구), `Q` 수집 종료**.
