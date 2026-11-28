# ip_change

Red Hat Enterprise Linux 대상 노드들의 **IP·게이트웨이 설정을 일괄 변경**하는 도구입니다.

> 작업 흐름은 [WORKFLOW.md](WORKFLOW.md), 폴더·파일별 역할은 [ARCHITECTURE.md](ARCHITECTURE.md)를 참고하세요.

관리 노드에서 Go 바이너리를 실행하면, 대상 전체를 담은 apply 스크립트 하나를 만들어
`gossh` 로 대상 노드에 밀어 넣고 실행합니다. **설정 파일만 바꿉니다.** 네트워크 서비스·
NetworkManager 를 재시작하지 않고, 재부팅하지 않고, `nmcli` 쓰기 명령도 쓰지 않습니다.
새 IP 는 **다음 재부팅 때** 적용됩니다.

> **본 스크립트 및 툴은 100% 신뢰하기보다는 참고용(보조 도구)으로 사용하는 것을
> 권장합니다.** 설정 변경 스크립트이므로, 적용 후 대상 서버 중 무작위로 몇 대를
> 직접 접속해 설정이 실제로 반영됐는지 반드시 확인하십시오.

- 저장소를 통째로 내려받으면 **폐쇄망에서 그대로 빌드**됩니다. 외부 의존성이 없습니다(Go 표준 라이브러리만 사용).
- `CGO_ENABLED=0` 정적 빌드라 구형 배포판에서도 glibc 버전과 무관하게 동작합니다.
- 대상 노드에 Go 도 필요 없습니다. 노드에서 도는 것은 bash 뿐입니다.
- RHEL 6~10 을 지원합니다. 설정 형식은 ifcfg 와 NetworkManager keyfile(`.nmconnection`) 둘 다 다룹니다.
- 파일을 **통째로 덮어쓰지 않고 필요한 줄만 갱신**합니다. 다른 줄과 주석은 그대로 둡니다.
- 변경 전 원본을 `/var/lib/ip_change/backup/<STAMP>/` 에 백업합니다(설정 디렉토리에는 백업을 두지 않음).

> **RHEL 10 keyfile 양식(새 프로필 생성·keyfile 변경)은 잠정안이며, 이후 사용자 요청에 따라 바뀔 수 있습니다.**

---

## 1. 빌드 및 설치 방법

### 1.1 사전 준비

| 항목 | 필요 위치 | 비고 |
|---|---|---|
| Go 툴체인 | 관리 노드 | `go.mod` 기준 1.26.5. 빌드할 때만 필요 |
| `gossh` | 관리 노드 | 저장소의 [`gossh-standalone` 브랜치](https://github.com/qazx2675/myrepo/tree/gossh-standalone) |
| bash / awk / sed / `hostname -I`(또는 `ip`) | 대상 노드 | 기본 설치분으로 충분 |

> **`gossh` 는 v2 를 쓰십시오.** 구버전에는 명령 앞뒤 따옴표를 잘라내는 버그가 있어
> 원격 주입이 깨집니다. `gossh -h` 에 `-cf` 와 `-b` 가 보이면 v2 입니다.

### 1.2 내려받아 빌드하기

```bash
git clone -b ip-change-standalone https://github.com/qazx2675/myrepo.git ip_change
cd ip_change
./setup.sh
```

`setup.sh` 는 `GOPROXY=off` 로 인터넷 접속을 시도하지 않고 `bin/ip-change-engine` 을 만듭니다.

수동 빌드:

```bash
CGO_ENABLED=0 go build -o bin/ip-change-engine ./cmd/ip-change-engine
```

### RHEL 8 — 빌드 없이 바로 실행

`bin/ip-change-engine_os8` 은 미리 빌드한 정적 바이너리(linux/amd64)입니다. Go 가 없어도 됩니다.

```bash
./bin/ip-change-engine_os8 <대상목록>
```

### 1.3 설정 파일 채우기

```bash
cp conf/testuser.txt.sample    conf/<계정명>.txt          # 작업 대상 목록
cp conf/ip_change.conf.sample  conf/ip_change.conf       # 선택 사항(보통 필요 없음)
```

- `conf/<계정명>.txt` — 작업 대상. `hostname 변경될ip` 공백 구분, 한 줄에 하나.

```
hostname1 3.3.3.3
hostname2 2.2.2.2
```

- `conf/ip_change.conf` — **선택 사항.** 노드가 설정 파일을 스스로 찾으므로 보통은 필요 없습니다.
  노드의 설정 위치가 표준과 다를 때만 강제 경로(`network_scripts_dir`, `rhel9_path`)를 지정합니다.
  찾는 순서: `-config` 로 준 경로, `./ip_change.conf`, 실행 파일 옆 `conf/ip_change.conf`.
- `gossh` 위치는 자동으로 찾습니다(아래 3장). 못 찾으면 `-gossh <경로>` 또는 환경변수
  `IP_CHANGE_GOSSH=<경로>` 를 지정하십시오.
- `scripts/run_ip_change.sh` 의 `select_user_context()` — 비워둔 자리입니다. 계정 선택 로직을
  채우고 `RUN_USER` 변수에 계정명을 대입하십시오. 그 값으로 `conf/${RUN_USER}.txt` 를 로드합니다.

### 1.4 설치 확인

```bash
go test ./...
```

---

## 2. 사용 방법

대상 목록 파일을 **첫 번째 인자**로 줍니다. 설정 파일·`-targets` 없이 이것만으로 동작합니다.

```bash
./bin/ip-change-engine_os8 list              # 변경 적용 (list = 대상 목록 파일)
./bin/ip-change-engine_os8 -check list       # 바꿀 내용만 보기(노드에 아무것도 쓰지 않음)
./bin/ip-change-engine_os8 -p list           # 키 인증이 실패한 노드에만 패스워드를 물어 다시 시도
./bin/ip-change-engine_os8 -rollback list    # 변경 되돌리기
```

옵션은 목록 파일 앞뒤 어디에 써도 됩니다. `-targets list` 도 그대로 쓸 수 있습니다(별칭).
래퍼를 쓰려면 `cd scripts && ./run_ip_change.sh [-check] [-fix-gateway] [-bond-mode lacp] [-p]`.

SSH 는 등록된 키를 먼저 씁니다. 키 인증이 실패한 노드는 FAIL(`키 인증 실패(패스워드 필요)`)로
표시되고, `-p` 로 다시 실행하면 패스워드를 한 번 묻고(입력이 화면에 보이지 않음) **실패한 노드만**
다시 시도합니다. 키로 접속된 노드는 다시 실행하지 않습니다.

### 이 도구가 바꾸는 것

| 상황 | 동작 | 결과 방식 |
|---|---|---|
| 고정 IP(static) | 새 IP·게이트웨이로 갱신. 넷마스크는 그대로 | `static` |
| DHCP 인터페이스 | **고정 IP 로 전환**(ifcfg `BOOTPROTO=none`, keyfile `method=manual`)하고 IP·넷마스크·게이트웨이 기록 | `dhcp->static` |
| 이 장치용 설정 파일이 없음 | **새 프로필 파일 생성**(ifcfg 또는 keyfile) | `new-file` |
| 이미 새 IP 로 바뀐 노드 | 파일을 건드리지 않고 백업도 만들지 않음 | `already` |

- 게이트웨이는 새 IP 의 마지막 옥텟을 `1` 로 바꾼 값입니다(예: `3.3.3.3` → `3.3.3.1`).
- 형식: RHEL 6~9 는 ifcfg(`/etc/sysconfig/network-scripts`), RHEL 10 은 keyfile
  (`/etc/NetworkManager/system-connections`). 기존 파일이 있으면 그 형식 그대로 고칩니다.
- ifcfg 는 CRLF 도 인식하며, 수정한 파일은 LF 로 저장합니다.

### 이 도구가 하지 않는 것

- 네트워크 서비스, NetworkManager 등 어떤 데몬도 재시작하지 않습니다.
- 노드를 재부팅하지 않습니다. 실행 중인 인터페이스에 새 주소를 적용하지도 않습니다.
- `nmcli` 쓰기 명령(modify, load, reload, up, down)을 쓰지 않습니다(읽기 조회만 사용).
- DNS 설정·`/etc/resolv.conf`·`/etc/hosts` 를 건드리지 않습니다.

### 어떤 NIC 를 바꾸는가

관리 노드가 접속한 IP(`SSH_CONNECTION` 의 세 번째 값)를 가진 장치 **하나만** 바꿉니다.
`SSH_CONNECTION` 이 비어 있으면 기본 경로(default route)의 장치를 씁니다. 그래서
`usb0`, `virbr0` 처럼 SSH 세션을 나르지 않는 장치는 선택되지 않습니다. 장치가 UP/LOWER_UP 이
아니면 파일은 바꾸되 WARN(`link down`)으로 표시합니다.

### 설정 파일은 어떻게 찾는가

IP 값이 아니라 **장치 이름**으로 찾으므로 두 번째 실행도 같은 파일을 찾습니다. 순서:

1. conf 에 `network_scripts_dir`/`rhel9_path` 를 지정했으면 그 디렉토리만 찾음(강제 경로)
2. `nmcli`(읽기 조회)가 있고 NetworkManager 가 돌면 활성 프로필의 파일(`/run` 아래 메모리 전용이면 "파일 없음")
3. `/etc/sysconfig/network-scripts/ifcfg-*` 중 `DEVICE` 가 장치와 같은 것(`DEVICE` 가 없으면 파일명, `HWADDR`)
4. `/etc/NetworkManager/system-connections/*.nmconnection` 중 `interface-name` 또는 `mac-address` 가 맞는 것

백업·`.rpmnew`·`.rpmsave`·`.orig`·`~` 파일은 무시합니다. 같은 장치 파일이 둘 이상이면 활성 프로필의
파일을 고르고 나머지는 "경쟁 프로필"로 보아 WARN 입니다.

### DHCP → 고정 전환 예

ifcfg — `BOOTPROTO` 줄 바로 아래에 `IPADDR`/`NETMASK`/`GATEWAY` 를 넣습니다(`BOOTPROTO` 줄이 없으면 파일 끝에
`BOOTPROTO=none` 과 함께 추가). `PEERDNS`·`DHCP_HOSTNAME` 등 기존 줄은 그대로 둡니다.

```
BOOTPROTO=none
IPADDR=3.3.3.3
NETMASK=255.255.255.0
GATEWAY=3.3.3.1
```

keyfile — `[ipv4]` 섹션만 바꿉니다. 게이트웨이는 `address1` 줄이 아니라 별도 `gateway=` 줄입니다.
파일 모드 600·소유자 root 를 유지하고 IPv6 줄과 `dhcp-*` 키는 그대로 둡니다.

```
[ipv4]
method=manual
address1=3.3.3.3/24
gateway=3.3.3.1
```

### 설정 파일이 없을 때 새로 만드는 프로필

RHEL 6~9 는 ifcfg, RHEL 10 은 keyfile(모드 600)입니다. 아래는 이더넷 예입니다.

```
# ifcfg-eth0 (RHEL 6~9)                      # eth0.nmconnection (RHEL 10, 잠정안)
DEVICE=eth0                                  [connection]
NAME=eth0                                    id=eth0
TYPE=Ethernet                                type=ethernet
ONBOOT=yes                                   interface-name=eth0
BOOTPROTO=none                               autoconnect=true
IPADDR=3.3.3.3
NETMASK=255.255.255.0                        [ipv4]
GATEWAY=3.3.3.1                              method=manual
                                             address1=3.3.3.3/24
                                             gateway=3.3.3.1

                                             [ipv6]
                                             method=ignore
```

- **bond**: `ifcfg-bond0`(`TYPE=Bond`, `BONDING_OPTS="mode=active-backup miimon=100"`) 또는
  `bond0.nmconnection`(`[bond] mode=active-backup`, `miimon=100`)을 만들고,
  `/sys/class/net/bond0/bonding/slaves` 의 포트 중 파일이 없는 것마다 포트 파일
  (ifcfg 는 `MASTER=bond0`/`SLAVE=yes`, keyfile 은 `bond0-port-eth0.nmconnection` 의 `controller=bond0`/`port-type=bond`)을 만듭니다.
  기본 모드는 active-backup 이고, `-bond-mode lacp` 를 주면 `mode=802.3ad` 입니다.
  실제 bond 모드(`/sys/class/net/bond0/bonding/mode`)가 파일 모드와 다르면 WARN 입니다.
- VLAN 장치는 `TYPE=Vlan`/`type=vlan`(부모 장치 포함)으로 만듭니다.
- bridge·team 등 그 밖의 장치 종류는 새 프로필을 만들지 않고 FAIL(`unsupported device type for a new profile`)입니다.

> **RHEL 10 keyfile 양식(새 프로필 생성·keyfile 변경)은 잠정안이며, 이후 사용자 요청에 따라 바뀔 수 있습니다.**

### 결과 화면

실행 전 작업 대상 목록을, 실행 후 아래 표와 요약(전체·OK·WARN·FAIL)을 출력합니다.
같은 표(색상 없음)를 `./ip_change_result_<YYYYmmdd_HHMMSS>.txt` 로도 저장합니다
(`-check` 는 `./ip_change_check_<YYYYmmdd_HHMMSS>.txt`).

```
hostname | 결과 | 기존IP -> 새IP | 장치 | 방식 | OS | 설정파일 | 비고
```

| 결과 | 뜻 |
|---|---|
| **OK** | 재부팅 뒤에도 새 IP 를 유지할 조건을 모두 만족 |
| **WARN** | 파일은 바뀌었지만 아래 점검 중 하나가 걸림. 비고에 어느 점검인지 표시 |
| **FAIL** | 실패. 쓴 뒤 검증에서 틀리면 백업을 되돌리고 FAIL |
| **CHECK** | `-check` 모드. 비고에 바꿀 내용을 표시, 파일·백업 변경 없음 |
| OK (already) | 이미 새 IP·게이트웨이·고정 방식이라 건드리지 않음(백업 없음) |

OK 판정 점검: ① 파일에 새 IP·게이트웨이·고정 방식 ② 넷마스크(`NETMASK`/`PREFIX`/주소의 prefix) ③ 부팅 때
시작(`ONBOOT` 가 no 아님, `autoconnect` 가 false 아님) ④ 경쟁 프로필 없음 ⑤ 장치 UP/LOWER_UP ⑥ `/etc/sysconfig/network`·
`route-<장치>` 에 예전 게이트웨이 없음. ①②가 틀리면 백업을 되돌리고 FAIL, ③~⑥은 WARN 입니다.

### 백업과 롤백

- 백업 위치: `/var/lib/ip_change/backup/<STAMP>/` 아래에 바뀐 파일의 원래 전체 경로로 복사합니다
  (예: `.../20261128120000/etc/sysconfig/network-scripts/ifcfg-eth0`).
- 같은 디렉토리의 `manifest` 에 `<modified|created> <경로>` 가 한 줄씩 있습니다.
- `-rollback list`: 최근 STAMP(`-rollback-to <STAMP>` 로 지정 가능)를 골라 `modified` 는 백업을 원래 경로로 복사,
  `created` 는 만든 파일을 백업 디렉토리로 `<이름>.removed` 로 옮깁니다. 다시 읽어 OK/FAIL 을 보고합니다.
  백업 디렉토리가 없으면 예전 방식(`<파일>.bak.<STAMP>`)으로 복원합니다.

### 예전 게이트웨이 정리

`/etc/sysconfig/network` 의 `GATEWAY` 나 `route-<장치>` 에 예전 게이트웨이가 남아 있으면 첫 실행은 그 노드를 WARN 으로
두고 건드리지 않습니다. 실행이 끝나면 해당 노드·파일 목록을 보여 주고 한 번만 묻습니다.
`이 파일들의 옛 게이트웨이도 함께 바꿀까요? [y/N]` — `y` 면 그 노드에만 두 번째 스크립트를 보내 백업 후 새 게이트웨이로 바꿉니다.
표준입력이 터미널이 아니면 묻지 않고 WARN 으로 남습니다. `-fix-gateway` 를 주면 묻지 않고 바로 고칩니다.

### 알려진 영향

- DHCP → 고정으로 바꾸면 DNS 를 쓰지 않으므로(DNS 항목을 쓰지 않음) 재부팅 뒤 DHCP 가 주던 네임서버가 `/etc/resolv.conf` 에서 사라질 수 있습니다.
- NetworkManager 는 재부팅 전까지 **메모리의 옛 값**을 유지합니다. 재부팅 전에 `nmcli connection up` 등으로 그 프로필을 건드리면
  옛 값이 되살아날 수 있습니다(비고에 `재부팅 전 nmcli 로 이 프로필을 건드리지 마십시오` 표시).
- `-p` 의 패스워드는 gossh 인자로 전달되므로 실행 중 관리 노드의 프로세스 목록에 보일 수 있습니다.
- 다른 브랜치(예: master 통합 스크립트)의 결과 파서는 WARN 상태를 모를 수 있습니다.

---

## 3. 옵션별 상세 설명

### `bin/ip-change-engine` (os8 바이너리는 `bin/ip-change-engine_os8`)

`ip-change-engine [옵션] <목록파일>` — 목록 파일은 옵션이 아닌 첫 인자입니다.

| 플래그 | 기본값 | 설명 |
|---|---|---|
| `<목록파일>` | (필수) | 대상 목록 파일(`hostname 변경될ip`). `-targets <파일>` 도 같은 뜻(별칭) |
| `-config` | `./ip_change.conf`, 실행 파일 옆 `conf/ip_change.conf` | 설정 파일 경로. 없어도 동작 |
| `-check` | `false` | 바꿀 내용만 보여주고 노드에는 아무것도 쓰지 않음(CHECK 결과) |
| `-fix-gateway` | `false` | 예전 게이트웨이가 남은 파일을 묻지 않고 함께 고침 |
| `-bond-mode` | `active-backup` | 새로 만드는 bond 의 모드. `active-backup` 또는 `lacp`(802.3ad) |
| `-gossh` | (자동 탐색) | gossh 실행 파일 경로. 순서: `-gossh` → 환경변수 `IP_CHANGE_GOSSH` → 바이너리 옆 `gossh_os8`/`gossh` → PATH |
| `-u` | `root` | SSH 접속 계정 |
| `-p` | `false` | **불리언.** 키 인증이 실패한 노드에만 쓸 패스워드를 물음(입력 숨김). 패스워드가 프로세스 목록에 보일 수 있음 |
| `-i` | (없음) | SSH 키 파일 경로 |
| `-P` | `22` | SSH 포트 |
| `-c` | `0`(gossh 기본값) | gossh 동시 접속 수 |
| `-t` | `0`(gossh 기본값) | gossh 접속 타임아웃(초) |
| `-remote-path` | `/root/ip_change_apply.sh` | 원격에 떨어뜨릴 스크립트 경로 (실행 후 삭제됨) |
| `-rollback` | `false` | 변경을 되돌립니다. 각 노드의 최근 백업 STAMP 를 복원 |
| `-rollback-to` | (없음) | 되돌릴 백업 STAMP 지정. 미지정 시 가장 최근 백업 |
| `-monitor` | `false` | 60초가 지나도 안 끝나면 제어 터미널(`/dev/tty`)에 호스트별 진행 화면(↑/↓·←/→·Enter)을 띄우고, Ctrl+C 는 5초 안에 3번 눌러야 종료(종료코드 130, 되돌리기 없음). 표준출력 형식은 그대로. 통합 스크립트(`change.sh`)가 켭니다 |

> gossh 를 못 찾으면 `gossh 를 찾지 못했습니다. -gossh <경로> 또는 환경변수 IP_CHANGE_GOSSH=<경로> 를 지정하십시오.` 로 멈춥니다.
> `-rollback` 시 목록 파일은 `hostname` 한 컬럼만 있어도 됩니다(변경 IP 불필요).
> 백업 디렉토리는 복원 후에도 지우지 않고 남깁니다.

### `conf/ip_change.conf` (선택 사항, 강제 경로)

| 키 | 기본값 | 설명 |
|---|---|---|
| `network_scripts_dir` | `/etc/sysconfig/network-scripts` | 기본값과 다르게 적었을 때만 강제 경로가 되어 이 디렉토리만 찾음 |
| `rhel9_path` | (비움) | 채우면, 노드 OS 메이저 버전이 9 이상일 때 이 디렉토리만 찾음 |

### `scripts/run_ip_change.sh`

| 옵션 | 설명 |
|---|---|
| `-config <경로>` | 설정 파일 지정 (기본 `../conf/ip_change.conf`, 없으면 생략) |
| `-check`, `-fix-gateway`, `-bond-mode <모드>`, `-p` | 엔진에 그대로 전달 |
| 환경변수 `GOSSH_ARGS` | 엔진에 그대로 전달할 gossh 관련 플래그 (예: `GOSSH_ARGS="-i ~/.ssh/id_rsa"`) |

---

## 4. 문서별 설명

| 폴더/파일 | 역할 |
|---|---|
| `setup.sh` | 폐쇄망 오프라인 빌드 |
| `cmd/ip-change-engine/main.go` | CLI 진입점. 대상 로드 → 스크립트 생성 → gossh 호출 → 결과 표·결과 파일 출력, 예전 게이트웨이 질문 |
| `internal/target/` | `{user}.txt`(`hostname 변경될ip`) 파싱, 게이트웨이(`.1`) 계산 |
| `internal/config/` | `ip_change.conf`(평문 key=value) 파싱 |
| `internal/render/` | apply 스크립트 조립 (`apply_body.sh` 를 `go:embed`) |
| `internal/render/apply_body.sh` | **실제 설정을 바꾸는 본체.** 장치 선택, 설정 파일 탐색, 변경·새 프로필 생성, 백업, 검증, OK 점검 |
| `internal/render/rollback_body.sh` | 롤백 본체. 백업 디렉토리의 manifest 로 복원 |
| `internal/render/fixgw_body.sh` | 예전 게이트웨이 정리 본체 (`/etc/sysconfig/network`, `route-<장치>`) |
| `internal/remote/` | gossh 탐색·호출. base64 로 인코딩해 원격 주입하고 `<host>: <내용>` 출력을 파싱. `runner.go`(키 실패 감지·패스워드 재실행), `tty_*.go`(패스워드 숨김 입력) |
| `internal/color/` | 관리 노드 출력에만 쓰는 ANSI 색상(성공=녹색, 실패=굵은 빨강). 대상 노드로 전송되는 스크립트 출력에는 적용하지 않음 |
| `scripts/run_ip_change.sh` | 관리자용 래퍼. 계정 선택(`select_user_context`, 빈 자리) → 엔진 실행 |
| `conf/*.sample` | 설정·대상 목록 예시. 실제 값 파일은 `.gitignore` 로 커밋 차단 |

자세한 파일별 역할과 수정 요청별 참고 위치는 [ARCHITECTURE.md](ARCHITECTURE.md) 를 보십시오.
