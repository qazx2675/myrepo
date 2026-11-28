# CHANGELOG.md — ip_change

## 2026-11-28 — 설정 파일만 바꾸는 방식으로 재설계 (RHEL 6~10, DHCP·keyfile·새 프로필)

> **RHEL 10 keyfile 양식(새 프로필 생성·keyfile 변경)은 잠정안이며, 이후 사용자 요청에 따라 바뀔 수 있습니다.**

- 장치 선택: `SSH_CONNECTION` 의 서버 IP 를 가진 장치만 바꿈(없으면 default route 장치). usb0 등은 선택되지 않음. 링크가 내려가 있으면 WARN(`link down`).
- 설정 파일 탐색을 IP 값이 아니라 장치 이름 기준으로 변경(nmcli 읽기 조회 → ifcfg → keyfile). 그래서 DHCP ifcfg(IPADDR 없음)와 두 번째 실행도 같은 파일을 찾음. 결과에 OS 버전과 파일 경로 표시.
- ifcfg 와 NetworkManager keyfile(`.nmconnection`) 둘 다 지원. RHEL 6~10.
- DHCP → 고정 전환(`BOOTPROTO=none` / `method=manual`, IP·넷마스크·게이트웨이 기록, DNS 는 쓰지 않음).
- 설정 파일이 없으면 새 프로필 생성(이더넷·bond·bond 포트·VLAN). bond 기본 active-backup, `-bond-mode lacp` 는 802.3ad. bridge·team 등은 FAIL.
- 이미 바뀐 노드는 `OK (already)` 로 두고 파일·백업을 건드리지 않음.
- OK 는 "재부팅 뒤에도 새 IP 유지" 점검(파일·넷마스크·ONBOOT·경쟁 프로필·링크·예전 게이트웨이)을 통과해야 함. 걸리면 WARN. 결과 줄은 `RESULT|<상태>|host|기존IP|새IP|GW|장치|방식|OS|파일|비고` 로 확장(상태에 WARN·CHECK 추가).
- 예전 게이트웨이(`/etc/sysconfig/network`, `route-<장치>`)는 첫 실행에서 WARN 만 하고, 끝에 한 번 묻고(`[y/N]`) 고침. `-fix-gateway` 는 묻지 않음. 새 `internal/render/fixgw_body.sh`.
- 백업을 `/var/lib/ip_change/backup/<STAMP>/`(원래 전체 경로) + `manifest` 로 이동. 설정 디렉토리에 `.bak` 을 두지 않음(NetworkManager·network 서비스가 추가 프로필로 읽을 위험). 롤백은 manifest 기준(modified 복원·created 는 `.removed` 로 이동), 백업 디렉토리가 없으면 예전 `.bak.<STAMP>` 방식.
- 명령줄: `ip-change-engine_os8 list` 만으로 동작(목록 파일은 위치 인자, `-targets` 는 별칭). 설정 파일은 선택(`-config`, `./ip_change.conf`, 실행 파일 옆 `conf/`). 새 옵션 `-check`, `-fix-gateway`, `-bond-mode`. `-p` 는 값을 받지 않고 키 인증이 실패한 노드에만 패스워드를 물음(`internal/remote/runner.go`, `tty_*.go`).
- gossh 자동 탐색: `-gossh` → 환경변수 `IP_CHANGE_GOSSH` → 바이너리 옆 → PATH. 못 찾으면 안내 후 종료.
- 결과 표(`hostname | 결과 | 기존IP -> 새IP | 장치 | 방식 | OS | 설정파일 | 비고`)와 요약, 결과 파일 `ip_change_result_<시각>.txt`(`-check` 는 `ip_change_check_<시각>.txt`).
- `scripts/run_ip_change.sh`: 목록 파일을 위치 인자로 넘기고 `-check`, `-fix-gateway`, `-bond-mode`, `-p` 를 전달.
- 알려진 영향: DHCP → 고정 시 DNS 를 쓰지 않음, 재부팅 전 NetworkManager 메모리는 옛 값 유지(nmcli 쓰기 명령은 쓰지 않음), `-p` 패스워드는 프로세스 목록에 보일 수 있음, master 의 통합 스크립트는 WARN 상태를 모를 수 있음.

## 2026-11-27 (추가) — usb0 제외, CRLF 처리

- `DEVICE`(없으면 파일명)가 `usb0` 인 ifcfg 는 대상에서 제외.
- CRLF 줄바꿈 ifcfg 의 `IPADDR` 을 인식. 수정한 파일은 전체를 LF 로 저장(백업은 원본 그대로).
- `bin/ip-change-engine_os6`, `_os8` 다시 빌드.

## 2026-11-27 — 빈 `IPADDR=` ifcfg 적용

- IP 가 일치하는 ifcfg 가 없으면, 값이 빈 `IPADDR=` 파일 중 장치에 IPv4 가 있는 파일을 골라 적용. 후보가 둘 이상이면 실패.
- ifcfg 탐색에서 백업(`.bak`) 파일 제외.
- `BOOTPROTO`·`NETMASK`/`PREFIX` 는 바꾸지 않음.
- `bin/ip-change-engine_os6`, `_os8` 다시 빌드. 테스트하지 않음.

## 2026-10-12 — `go.mod` `go 1.20` · os6 정적 바이너리 `bin/ip-change-engine_os6`

- `go.mod`: `go 1.26.5` → `go 1.20`. Go 1.20·1.25 툴체인이 1.26.5 를 거부해 빌드할 수 없었음. 소스는 그대로이므로 동작은 바뀌지 않음.
- `bin/ip-change-engine_os6` 추가: RHEL 6 용 정적 바이너리 (`CGO_ENABLED=0 GOOS=linux GOARCH=amd64`, Go 1.20). `build_os6.sh` 가 같이 빌드하며, RHEL 6 에서 엔진을 단독 실행할 때 쓴다. auto_setup 의 TUI `m` 메뉴(IP 변경·원복)는 os8_mgmt 에 설치된 엔진 (`setup.sh` → `/usr/local/bin/ip-change-engine`) 을 호출한다.

## 2026-09-23 — `-monitor` 진행 화면 · Ctrl+C 3회 종료

- `-monitor` 플래그 추가(기본 꺼짐 — 끄면 기존과 동일). 켜면 60초가 지나도 안
  끝날 때 `/dev/tty` 에 호스트별 진행 화면(페이지 보기, ↑/↓·←/→·Enter 상세)을
  띄우고, Ctrl+C 는 5초 안에 3번 눌러야 종료(gossh 정리 후 종료코드 130, 끝난/안
  끝난 호스트 요약을 표준출력에도 남김). SIGTERM/SIGHUP 에도 터미널 원복.
  새 패키지 `internal/monitor`(표준 라이브러리만, Go 1.20 호환 — ldap_setting 에
  같은 파일).
- `internal/remote.Run`: gossh 출력을 끝까지 모아 두는 동작·반환값은 그대로 두고,
  줄 단위로 읽으면서 모니터에 호스트별 결과를 바로 넘김. 모니터가 있으면 gossh 를
  별도 프로세스 그룹으로 띄워 터미널 Ctrl+C 가 gossh 에 직접 가지 않게 함.

## 2026-09-14 (추가) — gossh 표준에러를 완전히 버려서 실패 사유가 안 보이던 문제 수정

- 직전 항목에서 `cmd.Stderr = nil` 로 gossh 의 표준에러를 아예 버렸는데, 이
  때문에 모든 호스트가 진짜로 접속 실패하는 경우 표준출력이 완전히 비어
  `Run()` 결과가 0건이 되고, 정작 이유를 담은 gossh 의 `ERROR: ...` 문구
  (표준에러)까지 같이 사라져 "gossh 출력이 비어 있습니다" 만 뜨고 원인을
  알 수 없게 됨. `Run()` 이 표준출력·표준에러를 각각 별도 버퍼로 받아
  **파싱은 표준출력만** 쓰되(줄 중간 끼어들기 방지), **화면 표시용 raw
  에는 표준에러도 붙이도록** 수정. 가짜 gossh(표준출력 없음 + 표준에러에
  ERROR 2줄)로 재현 → 수정 후 `raw` 에 그 ERROR 문구가 정상적으로 포함됨을
  확인.

## 2026-09-14

### 버그 수정
- `internal/remote.Run()` 이 `cmd.CombinedOutput()` 으로 gossh 의 표준출력·
  표준에러를 합쳐서 파싱하고 있었는데, gossh v2 에 1초 간격 진행률 표시
  기능(표준에러, 개행 없는 `\r진행: N/M (P%)`)이 추가되면서 그 조각이 다음
  호스트 결과 줄 앞에 붙어 파싱이 깨지는 경우가 생김 → 그 호스트는 항상
  UNREACHABLE/FAIL 로 집계됨. `Run()` 이 표준출력만 별도 버퍼로 받아
  파싱하도록 수정(`cmd.Stderr = nil`). 가짜 gossh 30회 반복 재현(80% 유실
  확인) 및 실제 gossh 로 로컬 12호스트 15회 반복 검증(수정 전 매회 오염
  줄 발생 → 수정 후 0건, 남은 실패는 전부 sshd 동시접속 제한으로 인한
  진짜 SSH 실패였음).

## 2026-09-11

- `-rollback` 서브모드 추가 — 각 대상 노드에서 `<ifcfg>.bak.<STAMP>` 를 되돌립니다
  (`ldap-config-engine -rollback` 과 대칭). `-rollback-to <STAMP>` 로 시점 지정 가능.
  - 새 임베드 스크립트 `internal/render/rollback_body.sh` + `render.RollbackScript`
  - `-rollback` 시 대상 파일은 `hostname` 한 컬럼만 있어도 됩니다 (`target.LoadHosts`)
  - 최신 STAMP 백업을 가진 ifcfg 가 둘 이상이면 실패 → `-rollback-to` 로 지정
  - 통합 스크립트(`Network_Change_Integration_Script`) 의 `change.sh rollback` 이 이걸 호출합니다

## 2026-09-09

- 최초 작성. `hostname 변경될ip` 목록으로 RHEL 대상 노드의 `ifcfg-*` 내 `IPADDR`/`GATEWAY`
  를 gossh 로 일괄 변경하는 Go 엔진 + bash 래퍼.
  - 게이트웨이는 변경 IP의 마지막 옥텟을 `.1` 로 고정 계산 (모든 대상 `/24` 전제)
  - 네트워크 서비스 재시작 없음
  - 원본 ifcfg 는 `.bak.<시각>` 백업 후 `IPADDR`/`GATEWAY` 두 키만 갱신 (통째 덮어쓰기 아님)
  - RHEL 9+ 대비 `conf` 의 `rhel9_path` 항목 (ifcfg 형식 가정, keyfile 형식 미지원)
  - 대상 노드에는 Go 바이너리가 아니라 **bash 스크립트를 base64 로 주입** (`ldap_setting` 과
    동일한 방식) — RHEL6/9 바이너리 호환 이슈 자체가 없음
  - 관리 노드 출력에 ANSI 색상 적용 (성공=녹색, 실패=굵은 빨강, 안내=청록). 대상 노드로
    전송되는 스크립트 출력에는 영향 없음

## 2026-09-09 (수정)

- 현재 서비스 IP 조회(`getent hosts`)가 IPv6 를 IPv4 보다 먼저 반환하는 노드에서
  엉뚱한 IPv6 값을 "현재 서비스 IP" 로 오인해 ifcfg 탐색에 실패하던 문제 수정.
  이제 `getent hosts` 출력 중 IPv4(점 4개짜리) 줄만 골라 씁니다. 실제 랩(192.168.0.58)에서
  `getent hosts $(hostname)` 가 `fe80::...` 를 먼저 찍는 것을 확인해 재현·검증했습니다.

## 2026-09-09 (개선)

- 실행 전 작업 대상 목록(hostname -> 변경 예정 IP)을 화면에 출력하도록 추가.
- 결과 출력 형식을 `hostname 기존IP -> 변경IP (GW 게이트웨이)` 로 변경 (기존: `hostname 변경IP 게이트웨이`).
  - `apply_body.sh` 는 이제 `RESULT|OK|host|기존IP|변경IP|GW` / `RESULT|FAIL|host|사유` 형태의
    기계용 한 줄만 찍고, `cmd/ip-change-engine/main.go` 의 `formatResultLine` 이 화면 표시 형식을 만든다.

## 2026-09-09 (수정 2)

- 현재 서비스 IP 확인 방식을 `getent hosts $(hostname)` 에서 `hostname -I`
  (실패 시 `ip -4 addr show scope global`) 로 교체. 실제 랩에서 작업대상 서버의
  /etc/hosts·DNS 항목에 IPv4 가 아예 없고(또는 IPv6 만 등록되어) getent 로는
  현재 IP 를 알 수 없어 "getent hosts 로 현재 서비스 IP(IPv4)를 확인할 수
  없습니다" FAIL 이 발생하는 것을 확인했다. hostname 해석에 기대지 않고 이
  노드에 실제로 할당된 IPv4 주소를 인터페이스에서 직접 조회하도록 바꿔
  /etc/hosts·DNS 설정 상태와 무관하게 동작하게 했다.
