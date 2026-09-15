# vm-ip-change

네트워크로 접속할 수 없는(IP 가 잘못 할당된) RHEL 8.10 VM 의 IP 를, **vCenter API 경로만으로**
재설정하는 도구입니다. 대상 VM 에 SSH/네트워크로 접속하지 않고, vCenter -> ESXi -> VMware
Tools(vmtoolsd) 경로(Guest Operations API)로 게스트 내부에서 `nmcli` 를 실행합니다. 이
경로는 VM 의 네트워크 스택을 거치지 않으므로, 관리서버가 그 VM 의 (잘못된) IP 로 접속하지
못하는 지금 같은 상황에서도 동작합니다.

> 설정 변경 도구입니다. 적용 후 대상 VM 몇 대는 직접(콘솔 등으로) 접속해 설정이 실제로
> 반영됐는지 확인하는 것을 권장합니다.

같은 저장소의 [`ip_change`](../../Network_Change_Integration_Script/projects/ip_change)
와 목적은 비슷하지만, 그쪽은 `gossh` 로 **대상 노드에 SSH 접속**해 `ifcfg-*` 파일을 고치는
방식이라 네트워크가 살아있어야 합니다. 이 도구는 **네트워크 접속이 불가능한 VM** 을 위한
것입니다.

---

## 1. 빌드 방법

### 사전 준비

| 항목 | 필요 위치 | 비고 |
|---|---|---|
| Go 툴체인 | 관리 노드 | `go.mod` 기준 1.25 이상 |
| 대상 VM 의 VMware Tools | 대상 VM | 실행 중이어야 합니다(Guest Operations 전제조건) |

### 내려받아 빌드하기 (폐쇄망 가능)

```bash
git clone https://github.com/qazx2675/myrepo.git
cd "myrepo/.claude/VM/vCenter API IP 자동변경/project"
./setup.sh
```

`setup.sh` 는 `GOPROXY=off` + `-mod=vendor` 로, 인터넷 접속 없이 저장소 안의
`vendor/`(govmomi, google/uuid) 만으로 `bin/vm-ip-change` 를 만듭니다.

수동 빌드:

```bash
GOFLAGS=-mod=vendor GOPROXY=off go build -o bin/vm-ip-change ./cmd/vm-ip-change
```

### 테스트

```bash
GOFLAGS=-mod=vendor GOPROXY=off go test ./...
```

---

## 2. 입력 파일 준비

```bash
cp vcenter.txt.example vcenter.txt
cp list.txt.example    list.txt
```

- **`vcenter.txt`** — 작업 대상을 찾아야 하는 vCenter 주소, 한 줄에 하나. 여러 vCenter 를
  순서대로 뒤져 VM 이 있는 vCenter 에서 처리합니다.
- **`list.txt`** — `VM호스트네임 새IP` 공백 구분, 한 줄에 하나. `VM호스트네임` 은 vCenter
  인벤토리상의 **VM 표시 이름**입니다(도메인 없음, 게스트 OS 안의 hostname 이 아닙니다).

두 파일 모두 빈 줄과 `#` 로 시작하는 줄은 무시합니다. 실제 값이 든 두 파일은
`.gitignore` 에 이미 제외되어 있습니다(커밋하지 마십시오).

---

## 3. 실행

```bash
export VC_USER=administrator@vsphere.local
export VC_PASSWORD='********'      # vCenter 로그인
export GUEST_USER=root
export GUEST_PASSWORD='********'   # 대상 VM 게스트 OS(RHEL) 로그인

./bin/vm-ip-change
```

플래그로 파일 경로를 바꿀 수 있습니다(기본값은 각각 `vcenter.txt`, `list.txt`):

```bash
./bin/vm-ip-change -vcenter /path/to/vcenter.txt -list /path/to/list.txt
```

### 실행 흐름

1. `list.txt` 의 대상을 먼저 화면에 출력합니다(호스트네임 -> 새 IP, 게이트웨이 포함).
2. `vcenter.txt` 를 순서대로 접속하며, 아직 처리하지 못한 호스트네임과 일치하는 VM 을
   그 vCenter 인벤토리에서 찾습니다.
3. VM 을 찾으면:
   - 전원이 켜져 있고 VMware Tools 가 실행 중인지 확인합니다(둘 다 Guest Operations
     전제조건입니다). 아니면 `[FAIL]` 로 기록하고 건너뜁니다.
   - Guest Operations API 로 `/bin/bash -c '...'` 를 실행해, 게스트 안에서 현재
     활성화된 NetworkManager 연결(`nmcli con show --active` 의 첫 번째, `lo` 제외)의
     `ipv4.addresses`/`ipv4.gateway` 를 `manual` 로 재설정하고 그 연결만 다시 올립니다
     (`nmcli con up`). 서비스 전체를 재시작하지 않습니다.
   - 서브넷마스크는 `255.255.255.0`(prefix 24) 고정, 게이트웨이는 새 IP 의 마지막
     옥텟을 `1` 로 바꾼 값입니다 (예: `10.10.5.23` -> 게이트웨이 `10.10.5.1`).
4. 모든 vCenter 를 다 뒤져도 못 찾은 호스트네임은 `[FAIL]` 로 표시합니다.
5. 하나라도 실패하면 종료 코드 1 을 반환합니다.

```
작업 대상:
  web01                    -> 10.10.5.23 (GW 10.10.5.1)
  web02                    -> 10.10.5.24 (GW 10.10.5.1)
------------------------------------------------------------
[OK]   web01                    -> 10.10.5.23 (GW 10.10.5.1) [192.168.0.50]
[OK]   web02                    -> 10.10.5.24 (GW 10.10.5.1) [192.168.0.50]
```

---

## 4. 제약 사항

- 대상 VM 은 **RHEL 8.10, NetworkManager(nmcli) 사용**을 전제합니다. 다른 OS/네트워크
  구성 도구(netplan, systemd-networkd 등)는 지원하지 않습니다.
- VM 에 활성 네트워크 연결(NIC)이 **여러 개**인 경우, `nmcli con show --active` 가 돌려주는
  첫 번째(=lo 제외 가장 먼저 나열되는) 연결만 변경합니다. 어느 연결을 바꿀지 지정하는
  기능은 없습니다.
- `list.txt` 의 새 IP 는 항상 `/24`(`255.255.255.0`) 로 취급됩니다. 다른 서브넷마스크가
  필요하면 지금 버전으로는 처리할 수 없습니다.
- vCenter 인증서 검증은 하지 않습니다(폐쇄망 자체서명 인증서 환경 전제).
- 실행 후 네트워크 서비스 자체는 재시작하지 않고, 변경한 연결만 `nmcli con up` 으로
  다시 올립니다.
