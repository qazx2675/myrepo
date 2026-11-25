# gpu_install

list 에 적은 GPU 서버들에 지정한 **NVIDIA 드라이버(.run)** 를 설치하고, 설치된 드라이버의 CUDA 버전(nvidia-smi 기준)에
맞는 **DCGM 4** 를 설치·활성화하는 도구. 원격 실행은 [`gossh`](../../공통/gossh/README.md), 파일 전송은 `scp` 를 쓴다.
autofs 가 마운트되는 서버와 안 되는 서버가 **같은 결과**(같은 드라이버 + 버전에 맞는 DCGM + nvidia-dcgm 동작)가 되도록 만든다.

> 계획과 결정 사항은 [PLAN.md](PLAN.md) 를 참고하세요.

> ⚠️ **주의사항**
> - **실제로 작업 가능한 장비**(서비스 중지·드레인 완료)에만 실행하세요. 시작할 때 빨간 경고와 함께 `y` 확인을 받습니다.
> - **재부팅하지 않습니다.** 모듈 교체만으로 `nvidia-smi` 가 새 버전을 보고해야 하며, 안 되면 사용자가 직접 재부팅해야 합니다.
> - **fabricmanager**(NVSwitch 장비)는 자동 설치하지 않습니다. 드라이버와 버전을 맞춰야 한다는 경고만 표시합니다.
> - 모듈을 못 내리면 점유 프로세스(`lsof /dev/nvidia*`)를 로그에 남기고 fail 로 처리합니다. 프로세스를 kill 하지 않습니다.
> - 도구는 참고용 보조 수단입니다. 작업 후 서버 몇 대에 직접 접속해 `nvidia-smi`, `systemctl status nvidia-dcgm` 을 확인하세요.

---

## 1. 설치 (폐쇄망)

이 저장소에는 `vendor/` 가 없고 설치 대상은 모두 폐쇄망이므로 **사전 빌드한 정적 바이너리**를 복사해 씁니다.
외부 다운로드는 하지 않습니다.

| 파일 | 설명 |
|---|---|
| `gpu_install.sh` | 실행 편의 스크립트 (인자 처리만 하고 바이너리를 실행) |
| `gpu_install_os8` | RHEL 8 / Rocky 8 관리 서버용 Linux amd64 정적 바이너리 (`CGO_ENABLED=0`) |
| `list.example` | list 예시 |

1. `gpu_install.sh`, `gpu_install_os8` 를 **같은 폴더**에 두고 관리 서버로 복사합니다 (실행 권한: `chmod +x gpu_install_os8`).
2. 관리 서버에 `gossh`(저장소 [`.claude/공통/gossh`](../../공통/gossh/) 의 `gossh_os8`)와 `scp` 가 있어야 합니다. 대상 서버에 root 로 접속할 수 있어야 합니다.

소스를 고쳤다면 인터넷이 되는 곳에서 다시 빌드해 바이너리를 함께 커밋합니다 (Go 1.25 호환, 표준 라이브러리만 사용).

```bash
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o gpu_install_os8 .
```

## 2. 변수 설정

`gpu_install.sh` 맨 위의 빈 변수를 채웁니다.

| 변수 | 의미 |
|---|---|
| `AUTOFS_PATH` | GPU 설치파일(`NVIDIA-Linux-x86_64-<버전>.run`)이 있는 autofs 디렉토리. 인자로 주면 인자가 우선 |
| `MOUNT_USER` | 마운트 판정용 계정. 대상 서버에서 `/user/<MOUNT_USER>` 가 붙는지 확인. 비우면 전부 scp 방식 |
| `GOSSH` | gossh 경로. 비우면 PATH 에서 찾음 |

## 3. list 형식

```
gpu01                 # 버전 없음 → 실행할 때 한 번 묻고, 버전 없는 줄 전체에 적용
gpu02 570.172.08      # 이 호스트는 이 버전으로 설치
```

- 버전은 **드라이버 버전 문자열**이고 `<autofs경로>/NVIDIA-Linux-x86_64-<버전>.run` 파일에 연결됩니다.
- 버전 없는 줄이 있으면 디렉토리의 `.run` 목록을 번호로 보여 주고 **한 번만** 묻습니다.
- list 에 적힌 버전의 .run 파일이 없으면 시작 전에 오류를 내고 멈춥니다.
- `#` 뒤는 주석입니다. 예시는 [list.example](list.example).

## 4. 실행

```bash
bash gpu_install.sh [autofs경로(기본 AUTOFS_PATH)] [list파일(기본 ./list)] [-p 동시대수(기본 10)]
```

```bash
# AUTOFS_PATH 를 채워 둔 경우: ./list 를 10대씩 동시에
bash gpu_install.sh

# 경로·list·동시 대수를 직접 지정
bash gpu_install.sh /autofs/nvidia ./list_a -p 5

# 실패한 호스트만 다시
bash gpu_install.sh /autofs/nvidia fail_list.retry
```

`bash gpu_install.sh -h` 로 준비 사항, list 형식, 진행 순서, 결과 파일, 종료코드를 볼 수 있습니다.

AUTOFS_PATH 와 인자가 모두 비어 있으면 사용법을 출력하고 종료합니다. 종료코드는 Go 바이너리의 것을 그대로 씁니다
(`0` 정상, `1` 실패 호스트 있음 또는 오류, `2` 스크립트 인자 오류).
`fail_list`, `logs/` 는 **실행한 곳(현재 디렉토리)** 에 생깁니다.

## 5. 동작 흐름

1. **시작 경고(빨간색)**: 대상과 호스트별 요청 버전을 보여 주고 `y` 를 받아야 진행합니다.
2. **현황 수집**: gossh 1회로 hostname, 현재 드라이버·CUDA 버전, 마운트 여부, 사전점검 항목을 모읍니다.
3. **재설치 확인**: 이미 드라이버가 있는 서버를 `hostname 현재버전 → 요청버전` 으로 보여 주고 `a`(전체) / `s`(골라서) / `n`(건너뛰기)를 고릅니다.
   현재 버전과 요청 버전이 같으면 기본으로 건너뜁니다. 단, 같은 버전인데 DCGM 이 비정상이면 드라이버는 건너뛰고 DCGM 만 다시 맞춥니다.
4. **사전점검**: `lspci` 에 NVIDIA 가 없으면 건너뜀 / kernel-devel(현재 커널)·gcc 없으면 fail / nouveau 가 로드돼 있으면 `rmmod` 후 진행(실패하면 fail) / fabricmanager 있으면 경고.
5. **드라이버 설치**: `.run -s`. 실패하면 `nvidia-dcgm`, `nvidia-persistenced`, `nvidia-fabricmanager` 를 중지하고 `rmmod` 한 뒤 **1회 재시도**합니다. 그래도 실패하면 점유 프로세스와 `/var/log/nvidia-installer.log` 끝부분을 남기고 fail.
6. **CUDA 판정**: 설치 후 `nvidia-smi` 의 `CUDA Version` major 가 12 또는 13 이면 DCGM 으로 갑니다. 그 밖이면 **DCGM 을 건너뛰고 경고**(드라이버는 성공).
7. **DCGM 설치**와 8. **서비스 기동·판정**은 아래 6~7절을 봅니다.
9. **rc.local 실행**: 드라이버·DCGM 까지 성공한 서버에서만 `/etc/rc.local` 을 실행합니다. 파일이 없으면 건너뛰고, 실행 권한이 없으면 `bash /etc/rc.local` 로 실행합니다(최대 10분).
   종료코드가 0 이 아니면(10분 초과는 124) `rc.local failed(rc=N)` 사유로 fail 입니다. 드라이버·DCGM 은 설치된 상태로 남습니다. 출력은 호스트 로그에 남습니다.

## 6. mount / scp 분기

root 로 `timeout 10 ls /user/<MOUNT_USER>` 가 성공하고 `<autofs경로>/<run파일>` 을 읽을 수 있으면 **mount**, 아니면 **scp** 입니다. 호스트별 결과를 화면에 표시합니다.

| | mount | scp |
|---|---|---|
| 드라이버 | 대상에서 `sh <autofs경로>/<run> -s` | 관리 서버가 `.run` 을 대상 `/tmp/gpu_install/` 로 scp → 실행 → 파일 삭제 |
| DCGM | 대상 `/etc/yum.repos.d/rhel.repo` 의 `enabled=0` 을 `1` 로 바꾸고 `dnf install`, 끝나면 **0 으로 복구** | 대상 rhel.repo 의 `baseurl`(`file://`) 경로를 읽고, **관리 서버의 같은 경로**에서 필요한 rpm(최신)을 `/tmp/gpu_install/` 로 scp → `rpm -U --test` 로 부족한 의존성을 찾아 같은 경로 repodata 의 제공 rpm 도 자동 scp(부족분이 없을 때까지 최대 10회; 실패 사유 `dcgm no repodata`, `dcgm dep not found(cap,...)`) → `dnf localinstall -y --disablerepo='*'` → 보낸 rpm 모두 삭제 |

- mount 의 repo 복구는 대상 쪽 스크립트의 `trap` 이 처리하므로 성공·실패·중단 어느 경우에도 0 으로 돌아갑니다.
- scp 방식에서 baseurl 이 `file://` 이 아니거나 관리 서버에 같은 경로가 없으면 오류 메시지를 남기고 fail 입니다.

## 7. DCGM 규칙

- 패키지: `datacenter-gpu-manager-4-core`, `-proprietary`, `-cuda<N>`, `-proprietary-cuda<N>` (N = CUDA major, 12 또는 13).
- CUDA 가 바뀐 경우(예: cuda12 → cuda13)는 반대 버전의 `-cuda<M>`, `-proprietary-cuda<M>` 를 먼저 제거하고 설치합니다.
- `systemctl enable` + `restart nvidia-dcgm` 후 **`is-active` + `is-enabled` + `dcgmi discovery -l` 에 GPU 1개 이상**이어야 정상입니다.
  아니면 `dcgm inactive`, `dcgm not enabled`, `dcgm no gpu`, `dcgm missing(...)` 등의 사유로 fail 입니다.

## 8. 결과와 로그

- 최종 표: `HOST | 방식 | 이전→설치버전 | CUDA | DCGM | dcgm상태 | 결과`
- `fail_list`: `hostname 요청버전 사유` (예: `gpu01 550.163.01 module busy(retry failed)`)
- `fail_list.retry`: `hostname 버전` 형식이라 그대로 list 로 다시 쓸 수 있습니다 (`bash gpu_install.sh <경로> fail_list.retry`).
- 실패가 없으면 두 파일을 만들지 않고(이전 실행이 남긴 파일은 지움), 실패가 있으면 화면에도 출력하고 종료코드는 1 입니다.
- 호스트별 원격 출력 전체: `logs/<날짜시각>/<host>.log`

`logs/`, `fail_list`, `fail_list.retry`, `list` 는 `.gitignore` 대상입니다.
