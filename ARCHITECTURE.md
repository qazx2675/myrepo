# ARCHITECTURE

pm_monitor(`install_mn`) 의 파일별 역할. 표준 라이브러리만 사용, 패키지는 `main` 하나입니다.

| 파일 | 역할 |
|---|---|
| `main.go` | 인자 파싱(`-c`, `-f`, `--status`, `--stop`, 내부용 `--daemon`, 호스트파일), 호스트파일 읽기(`readHostSpecs`: 두 번째 칸이 `yum` 이면 yum 모드, 그 밖은 OS 설치), root 확인, 실행 방식 분기 |
| `daemon.go` | 백그라운드 수집: 소켓 서버(`watch`·`info`·`check`·`report`·`busy`·`stop`), 12시간 자동 종료, 데몬 시작·재시작(`ensureDaemon`), 화면용 `remoteSource`, `--status`·`--stop`. 지문(`fingerprint`)에 프로그램 버전(`programVersion`)·호스트파일·설정 내용이 들어감. `--status` 줄은 yum 서버가 있을 때만 `yum D/T` 가 붙음 |
| `daemon_linux.go` / `daemon_other.go` | setsid 로 터미널에서 분리·flock 으로 데몬 하나만 / 그 외 OS 대체 |
| `conf.go` | `pm_monitor.conf` 파싱과 기본값(`Config`, `yum_delay_min` 포함) |
| `vendor.go` | hostname 정규식으로 벤더 표시 이름 판별 |
| `icmp.go` | raw ICMP ping(소켓 하나, echo id·seq 매핑, `ping_pps` 속도 제한) |
| `port22.go` | 22번 포트 TCP 확인(refused 판정, `tcp_parallel` 동시 연결 제한) |
| `gossh.go` | gossh 실행(`-pm` 확인, 디스크 확인 명령, 출력 파싱, `nice -n 10`, 한 번에 하나). yum 서버용 `YumCheck`(`echo NS_YUM; uname -r; uptime -s` 한 줄 명령, 출력 `parseYumOutput`) |
| `state.go` | 상태 분류·지연·완료후꺼짐·재부팅됨, 이력 JSON 저장(`Record.Mode`·`YumKernel`·`YumBoot`·`YumSince`·`YumDoneAt`·`YumNow`), 수집 회차(`Collector`), 스냅샷(`Snapshot.Yum` 집계). yum 전이는 순수 함수 `applyYum`, yum 지연은 `yumDelayed`, 완료 후 `c` 판정은 `ApplyCompletedPing` |
| `comment.go` | 설치현황·점검요청 코멘트 문구 생성, yum 요약 줄·목록·`r` 의 yum 블록, `y` 표(`YumTable`) |
| `dhcp.go` | `d` 키: ping X 목록(yum 서버 제외)을 `dhcp_server` 로 보내 조회 |
| `ldap.go` | `l` 키: ldap_check.sh 실행·결과 요약, H/T 판정, 캐시. 대상은 OS 서버의 설치완료·재부팅됨 + yum 서버의 `yum_update/접속가능` |
| `render.go` | 스냅샷을 문자열로 바꾸는 순수 렌더(보이는 줄만 만듦). yum 서버가 있을 때만 머리줄 `(yum M대)`·푸터 `1~9 필터`·`y yum`, 긴 라벨 `yum_update/접속가능` 폭 |
| `tui.go` | 키 처리(`q` 화면만·`Q` 수집 종료, `y` 패널, 필터 0~9), 화면 출력, 수집(`source`)과 연결, 데몬에 붙는 `runTUIRemote` |
| `tty_linux.go` / `tty_other.go` | raw tty(termios) 진입·복구 / 그 외 OS 대체 |
| `*_test.go`, `testdata/` | 단위 테스트와 렌더 골든 파일. yum 시험은 `comment_yum_test.go`·`tui_yum_test.go`·`yum_record_test.go`·`yum_state_test.go` |
| `test/stub_gossh.sh`, `test/stub_ssh.sh` | 스텁 gossh·ssh(시험용). gossh 스텁은 yum 시나리오(`NS_YUM` 조회, 바뀐 커널·부팅 시각·전환 시각 칸) 지원 |
| `test/daemon_check.py` | 백그라운드 수집 실제 프로세스 시험(pty): 붙기·떼기·재접속·종료·만료, yum 서버 점검(51 PASS) |
| `test/func_check.py` | 상태 7가지·키·터미널 복구 기능 시험(26 PASS) |
| `test/demo_yum_check.py` | `demo/demo.sh` 를 가상 터미널로 돌려 yum 시나리오·정리 확인(20/20 PASS) |
| `test/load_run.sh`, `test/load_check.sh` | 부하 시험(데몬 프로세스 기준). 세 번째 인자로 yum 비율(%), 환경변수 `LOAD_DIR` |
| `demo/demo.sh` | 데모 환경 한 번에 구성·실행·정리(가짜 호스트 27대 + yum 서버 6대, 시간별 시나리오, 끝낼 때 백그라운드 수집도 정리) |
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

## yum update 모드 데이터 흐름 (v0.4.0)

```
호스트파일 `hostname [yum]` ─ readHostSpecs ─▶ hosts, yum 집합 ─ WithYum ─▶ Collector ─ NewCollector ─▶ Record.Mode (""=OS, "yum")
                                                                                       (저장된 Mode 와 다르면 그 서버 기록 초기화)
수집 회차: ping(OS·yum 공통) ─▶ 22번 포트(공통) ─┬─ OS 서버: gossh -pm ─▶ 디스크 확인
                                                └─ yum 서버: YumCheck(gossh 한 번, NS_YUM) ─▶ YumObs{Kernel, Boot, Err}
                                                       └─ applyYum(prev, ping, port, obs, now) ─▶ Record(Status·YumKernel·YumBoot·YumSince·YumDoneAt·YumNow)
buildSnapshot ─▶ Snapshot.Counts(전체) · Snapshot.Yum(집계·지연·벤더별 지연 목록) ─▶ 화면
화면: InstallComment(c) · DelayComment(r) · YumTable(y, 스냅샷만) · ldap(l)/dhcp(d)가 yum 서버를 걸러냄
```

- **순수 함수**: `applyYum`(전이)·`yumDelayed`(지연)·`yumKernelSame`(재부팅됨이 커널 동일 확인인지)·`YumTable`·`InstallComment`·`DelayComment` 는 입출력이 없어 단위 테스트로 전부 확인한다.
- **상태 전이(yum 서버)**: ping X 이고 기준 없음 → `ping X`, 기준 있음 → `재부팅됨`(재부팅 중). ping O 이고 ssh 불가 → 기준 없음 `확인전`, 기준 있음 상태 유지. ssh O 이고 기준 없음 → `접속가능`(기준 커널·부팅 시각·`YumSince` 기록). 기준 있음이면 커널이 다르면 `yum_update/접속가능`(완료), 같고 이전이 `재부팅됨` 이거나 부팅 시각이 60초 넘게 다르면 `재부팅됨`(이유 `재부팅됨, 커널 동일 <커널>`), 아니면 `접속가능`. 완료(`yum_update/접속가능`·`완료후꺼짐`)는 평소 회차에서 건드리지 않는다.
- **완료 판정은 모드에 따라 다르다**: `Record.Completed()` 는 OS 서버는 설치완료·재부팅됨·완료후꺼짐, yum 서버는 `yum_update/접속가능`·완료후꺼짐이다. `Snapshot.Done()`(설치완료 수)와 `osCounts()` 는 OS 서버만 센다.
- **부하**: yum 서버가 있어도 gossh 는 한 번에 하나, `nice -n 10`이며 회차당 한 번이 늘 뿐이다. yum 서버가 하나도 없으면 `YumCheck` 를 호출하지 않아 v0.3.0 과 똑같이 동작한다.
- **호환**: yum 필드는 `omitempty`(시각은 `Record.MarshalJSON` 이 zero 를 뺌)라 OS 서버만 있는 `state.json`·소켓 출력은 v0.3.0 과 같고 옛 파일도 그대로 읽힌다. 스냅샷 모양이 달라지므로 데몬 지문에 프로그램 버전을 넣어 바이너리 교체 뒤 옛 데몬을 새 데몬으로 바꾼다.
