# ARCHITECTURE

pm_monitor(`install_mn`) 의 파일별 역할. 표준 라이브러리만 사용, 패키지는 `main` 하나입니다.

| 파일 | 역할 |
|---|---|
| `main.go` | 인자 파싱(`-c`, `-f`, `--status`, `--stop`, 내부용 `--daemon`, 호스트파일), 호스트파일 읽기, root 확인, 실행 방식 분기 |
| `daemon.go` | 백그라운드 수집: 소켓 서버(`watch`·`info`·`check`·`report`·`busy`·`stop`), 12시간 자동 종료, 데몬 시작·재시작(`ensureDaemon`), 화면용 `remoteSource`, `--status`·`--stop` |
| `daemon_linux.go` / `daemon_other.go` | setsid 로 터미널에서 분리·flock 으로 데몬 하나만 / 그 외 OS 대체 |
| `conf.go` | `pm_monitor.conf` 파싱과 기본값(`Config`) |
| `vendor.go` | hostname 정규식으로 벤더 표시 이름 판별 |
| `icmp.go` | raw ICMP ping(소켓 하나, echo id·seq 매핑, `ping_pps` 속도 제한) |
| `port22.go` | 22번 포트 TCP 확인(refused 판정, `tcp_parallel` 동시 연결 제한) |
| `gossh.go` | gossh 실행(`-pm` 확인, 디스크 확인 명령, 출력 파싱, `nice -n 10`, 한 번에 하나) |
| `state.go` | 상태 분류·지연·완료후꺼짐·재부팅됨, 이력 JSON 저장, 수집 회차(`Collector`), 스냅샷 |
| `comment.go` | 설치현황·점검요청 코멘트 문구 생성 |
| `dhcp.go` | `d` 키: ping X 목록을 `dhcp_server` 로 보내 조회 |
| `ldap.go` | `l` 키: ldap_check.sh 실행·결과 요약, H/T 판정, 캐시 |
| `render.go` | 스냅샷을 문자열로 바꾸는 순수 렌더(보이는 줄만 만듦) |
| `tui.go` | 키 처리(`q` 화면만·`Q` 수집 종료), 화면 출력, 수집(`source`)과 연결, 데몬에 붙는 `runTUIRemote` |
| `tty_linux.go` / `tty_other.go` | raw tty(termios) 진입·복구 / 그 외 OS 대체 |
| `*_test.go`, `testdata/` | 단위 테스트와 렌더 골든 파일 |
| `test/stub_gossh.sh`, `test/stub_ssh.sh` | 스텁 gossh·ssh(시험용) |
| `test/daemon_check.py` | 백그라운드 수집 실제 프로세스 시험(pty): 붙기·떼기·재접속·종료·만료 |
| `demo/demo.sh` | 데모 환경 한 번에 구성·실행·정리(가짜 호스트 27대, 시간별 시나리오, 끝낼 때 백그라운드 수집도 정리) |
| `build.sh` | `install_mn_os8` 빌드(vet+test 후, `-n` 은 생략) |
| `install_mn.sh` | 실행 래퍼(심볼릭 링크 해석, root 확인) |
| `pm_monitor.conf.sample` | 설정 샘플 |
| `workflow.svg` | 전체 흐름도 |

## 프로세스 구조 (v0.3.0)

```
install_mn <호스트파일>  ──(소켓 /run/install_mn/<id>.sock, JSON 한 줄씩)──▶  install_mn --daemon  (setsid, 12시간 뒤 종료)
   화면(TUI, remoteSource)                                                    수집기(Collector) + 소켓 서버
   q/Ctrl+C/SIGHUP = 화면만 닫힘                                              상태 파일 <호스트파일>.pm_state.json
```

화면은 `source` 인터페이스만 쓰므로 `-f`(수집기를 직접 붙임)와 같은 코드로 그려진다. 데몬은 스냅샷을 갱신 때마다 붙은 화면 모두에게 보내고(밀리면 합침), `c`·`d`·`l` 은 데몬이 실행한다.
