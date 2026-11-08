# CHANGELOG

## [0.2.1] - 2026-11-08

### 변경
- DNS · NTP 비교: chrony.conf/ntp.conf 의 NTP 서버가 호스트 이름이면 그 노드에서 `getent hosts`(이름당 5초 제한)로 IP 로 풀어 비교한다. 못 풀면 비고에 `NTP 이름 풀이 실패 <이름>`. v0.2.0 으로 저장된 l 결과는 다시 조사한다.

## [0.2.0] - 2026-11-08

### 변경
- `l` 패널 칸 형식: LDAP 은 ldap_check 의 infra 만, DNS · NTP 는 resolv.conf nameserver 와 chrony.conf(없으면 ntp.conf) server 를 순서 무시로 비교해 같으면 `ip1/ip2`(숫자 오름차순)·다르면 `다름`, autofs 는 auto.appl 의 `/appl`·`/wappl` 마운트 위치(같으면 `/appl` 값, 다르면 `다름`), 머리줄에 `비고` 칸 추가. 옛 형식 캐시는 다시 조사.
- `q`: 패널(c r d l)에서는 뒤로가기(기본 화면), 기본 화면에서는 종료. Ctrl+C 는 어디서든 종료.
- 메모리 상한을 지키도록 GC 비율을 25 로 낮춤(8000대 RSS 54.4MB → 38.9MB).
- 원격 명령이 작은따옴표로 끝나지 않게 함(옛 gossh 가 끝 따옴표를 잘라 명령이 깨지던 문제).

### 추가
- `demo/demo.sh -n <대수>`(8000대 등)·`-name <이름>`(데모 여러 개 동시 실행), 실행 중인 같은 이름의 데모가 있으면 지우지 않고 거부.
- 8000대 부하 측정(`test/load_run.sh [초] [대수]`, `test/load_report.txt`): CPU 평균 3.30%, RSS 38.9MB.

## [0.1.1] - 2026-11-08

### 추가
- `demo/demo.sh`: 가짜 gossh·ssh·호스트 27대로 데모 환경을 한 번에 만들고 `install_mn` 을 실행, 종료 시 자동 정리. 시간에 따라 설치완료·지연·완료후꺼짐·재부팅됨이 나타난다.

## [0.1.0] - 2026-11-08

### 추가
- 최초 버전. `install_mn <호스트파일>` 로 OS 설치 진행 상태(설치중·지연·디스크미인식·설치완료·ping X·재부팅됨·완료후꺼짐)를 1분마다 갱신하는 TUI.
- raw ICMP(`ping_pps` 제한) · 22번 포트(`tcp_parallel` 제한) · `gossh -pm` 으로 판정, 상태·이력은 `<호스트파일>.pm_state.json` 에 저장.
- 키: 스크롤, `/` 검색, `1`~`7` 필터, `c` 설치현황 코멘트, `r` 점검요청 코멘트, `d` DHCP 조회, `l` LDAP·DNS·NTP·autofs·H/T, `q` 종료.
- `build.sh`(os8 정적 바이너리 `install_mn_os8`), `install_mn.sh` 래퍼, 문서 일체.
