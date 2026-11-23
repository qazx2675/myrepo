# gpu_install 계획서 — GPU 드라이버 + DCGM 설치·설정

작성일 2026-11-23 · 위치 `.claude/HPC/gpu_install/` · 상태: 인터뷰 완료, 구현 전

## 1. 목표

list에 적힌 GPU 서버들에 **지정한 NVIDIA 드라이버(.run)** 를 설치하고, 설치된 드라이버의 CUDA 버전(nvidia-smi 기준)에 맞는 **DCGM 4** 를 설치·활성화한다.
autofs가 마운트되는 서버와 마운트되지 않는 서버 모두 **같은 결과**(같은 드라이버 버전 + 버전에 맞는 DCGM + nvidia-dcgm 동작)가 되어야 한다.

## 2. 구성 (home-test 원칙: 실행 편의 bash + 실제 작업 Go)

| 파일 | 역할 |
|---|---|
| `gpu_install.sh` | 오케스트레이션만 담당. 맨 위에 빈 변수, 인자 처리, Go 바이너리 실행 |
| `main.go` 외 | 실제 작업: list 파싱, 질문, 병렬 처리, gossh 호출, scp, 판정, 결과·fail_list 작성 |
| `gpu_install_os8` | `CGO_ENABLED=0` 정적 빌드 바이너리 (폐쇄망용, 커밋 대상) |
| `test/` | 가짜 nvidia.run / nvidia-smi / DCGM rpm, VM 테스트 스크립트 |

원격 실행은 저장소의 `gossh`(`gossh -w hosts "cmd"`)를 서브프로세스로 호출한다. 파일 전송은 `scp`. 접속 계정은 **root** 이다.

### 2.1 bash 맨 위 변수

```bash
AUTOFS_PATH=""   # GPU 설치파일(.run)이 있는 autofs 디렉토리 (기본값)
MOUNT_USER=""    # 마운트 판정용 특정 계정 → 대상 서버에서 /user/${MOUNT_USER} 가 붙는지 확인
GOSSH=""         # gossh 경로 (비우면 PATH 에서 찾음)
```

### 2.2 사용법

```bash
bash gpu_install.sh [autofs경로(기본 AUTOFS_PATH)] [list파일(기본 ./list)] [-p 동시대수(기본 10)]
```

## 3. list 형식과 버전 결정

```
gpu01                 # 버전 없음 → 한 번 묻고 버전 없는 줄 전체에 적용
gpu02 570.172.08      # 이 호스트는 이 버전으로 설치
```
- 버전은 **드라이버 버전 문자열**이다. `<autofs경로>/NVIDIA-Linux-x86_64-<버전>.run` 파일에 연결한다.
- 버전 없는 줄이 하나라도 있으면 디렉토리의 `.run` 목록을 번호로 보여 주고 **한 번만** 묻는다. 고른 버전을 버전 없는 줄 전부에 적용한다.
- list에 적힌 버전의 .run 파일이 없으면 시작 전에 오류를 내고 멈춘다.

## 4. 실행 흐름 (호스트별, 최대 N대 동시)

0. **시작 경고 (빨간색)**: 대상 목록과 호스트별 요청 버전을 보여 주고 다음 문구를 띄운다.
   "작업 전, 실제로 작업 가능한 장비(서비스 중지·드레인 완료)가 맞는지 확인하세요."
   `y` 를 입력해야 진행한다.
1. **현황 수집 (gossh 1회)**: hostname, 현재 드라이버 버전(nvidia-smi), CUDA 버전, 마운트 여부, 사전점검 항목.
2. **마운트 판정**: root로 `timeout 10 ls /user/${MOUNT_USER}` 가 성공하고 `<autofs경로>/<run파일>` 을 읽을 수 있으면 **mount**, 아니면 **scp** 로 분류한다. 결과는 화면에 표시한다.
3. **재설치 확인**: 이미 드라이버가 깔린 서버를 `hostname 현재버전 → 요청버전` 으로 출력한다.
   `a`(전체) / `s`(골라서) / `n`(건너뛰기) 중에서 고르게 한다. 현재 버전과 요청 버전이 같은 서버는 기본으로 건너뛴다.
4. **사전점검**
   - GPU 존재: `lspci` 에 NVIDIA 장치가 없으면 건너뛰고 결과에 표시한다.
   - kernel-devel(현재 커널) / gcc: 없으면 fail로 처리하고 사유를 기록한다.
   - nouveau: 로드되어 있으면 **언로드**(`rmmod nouveau`)한 뒤 진행한다. 언로드에 실패하면 fail로 처리하고 사유를 기록한다.
   - fabricmanager: NVSwitch 장비이거나 nvidia-fabricmanager가 설치되어 있으면, 버전을 드라이버와 맞춰야 한다는 **경고**를 표시한다. 자동으로 설치하지는 않는다.
5. **드라이버 설치**
   - mount: `gossh` 로 `sh <autofs경로>/NVIDIA-Linux-x86_64-<ver>.run -s` 를 실행한다.
   - scp: 관리 서버가 autofs 경로에서 `.run` 을 `scp` 로 대상의 `/tmp/gpu_install/` 에 보낸 뒤 `sh /tmp/gpu_install/<run> -s` 를 실행한다. 끝나면 파일을 지운다.
6. **실패하면 모듈 언로드 후 1회 재시도**: 서비스 `nvidia-dcgm`, `nvidia-persistenced`, `nvidia-fabricmanager` 를 중지한다.
   이어서 `rmmod nvidia_drm nvidia_modeset nvidia_uvm nvidia`(그리고 nouveau)를 실행한 뒤 재시도한다.
   다시 실패하면 `lsof /dev/nvidia*`(점유 프로세스)와 설치 로그(`/var/log/nvidia-installer.log` 끝부분)를 남기고 **fail_list** 로 보낸다. 프로세스를 kill하지는 않는다.
7. **CUDA 판정**: 설치 후 `nvidia-smi` 의 `CUDA Version: X.Y` 에서 major 값을 읽는다.
   - 12 또는 13이면 DCGM 단계로 간다.
   - 그 밖의 값이면 **DCGM을 건너뛰고** 경고를 표시한다(드라이버는 성공으로 처리).
8. **DCGM 설치**
   - 패키지: `datacenter-gpu-manager-4-core`, `datacenter-gpu-manager-4-proprietary`, `datacenter-gpu-manager-4-cuda<N>`, `datacenter-gpu-manager-4-proprietary-cuda<N>`
   - **CUDA 버전이 바뀐 경우** (예: cuda12 → cuda13): 반대 버전의 `-cuda<M>`, `-proprietary-cuda<M>` 를 먼저 제거한 뒤 설치한다.
   - mount 서버: 대상의 `/etc/yum.repos.d/rhel.repo` 에서 `enabled=0` 을 `enabled=1` 로 바꾸고 `dnf install` 을 실행한다. 끝나면 **원래 값(0)으로 복구**한다.
     - 복구는 대상 쪽 스크립트의 `trap` 으로 처리해서, 성공·실패·중단 어느 경우에도 0으로 돌아가게 한다.
     - repo가 켜지지 않아 설치가 실패하는 일은 없어야 한다. 설치 직전에 enabled=1 이 실제로 적용됐는지 다시 확인한다.
   - scp 서버: 대상 rhel.repo 의 `baseurl`(file:// 경로)을 읽는다. **관리 서버의 같은 경로**에서 필요한 DCGM rpm(최신 버전)을 찾아 `/tmp/gpu_install/` 로 scp한다.
     이어서 `dnf localinstall -y --disablerepo='*'` 로 설치하고 rpm 파일을 지운다.
9. **서비스 기동과 판정**: `systemctl enable nvidia-dcgm` → `systemctl restart nvidia-dcgm` 을 실행한다.
   `is-active`, `is-enabled`, `dcgmi discovery -l` 이 모두 정상이고 GPU가 1개 이상 보여야 **DCGM 정상**으로 판정한다.

## 5. 출력

- 최종 표: `HOST | 방식(mount/scp) | 이전버전 → 설치버전 | CUDA | DCGM 패키지 | dcgm(active/enabled/GPU수) | 결과`
- `fail_list`: `hostname 요청버전 사유` 형식. 사유 예: `module busy(retry failed)`, `no kernel-devel`, `dcgm inactive`
- `fail_list.retry`: `hostname 버전` 형식이라 그대로 list로 다시 쓸 수 있다.
- 실패한 호스트가 있으면 목록을 화면에도 출력하고, 종료 코드는 1로 한다.
- 로그: `logs/<날짜시각>/<host>.log` (호스트별 원격 출력 전체)

## 6. 인터뷰 결정 사항 요약

| 항목 | 결정 |
|---|---|
| 접속 계정 | root (root 가 autofs 경로를 그대로 읽을 수 있음) |
| 마운트 판정 | 자동 판정. root 에서 `/user/<MOUNT_USER>` 가 붙는지 확인 (su 로그인 불가) |
| 비마운트 DCGM | 대상 rhel.repo baseurl 경로 → 관리 서버의 같은 경로에서 rpm scp → localinstall |
| repo 복구 | 항상 0으로 되돌림(trap). 단, 이것 때문에 설치가 실패하면 안 됨 |
| 버전 | 드라이버 버전 문자열. 버전 없는 줄은 한 번 묻고 일괄 적용 |
| 재설치 | 전체/선택/건너뛰기. 같은 버전은 기본 건너뜀 |
| 실패 재시도 | 서비스 중지 + rmmod 후 1회. 시작 시 빨간 경고로 작업 가능 장비인지 확인 |
| CUDA 12/13 이외 | DCGM 건너뛰고 경고 |
| DCGM 판정 | active + enabled + dcgmi discovery |
| nouveau | 언로드 후 진행 (안 되면 fail) |
| Go/bash | bash = 오케스트레이션, 질문·병렬·판정은 Go |
| 병렬·인자 | 기본 10대, list 는 두 번째 인자(기본 ./list) |

## 7. 가정 (구현 중 확인 필요)

1. **설치 대상 서버는 모두 폐쇄망(인터넷/https 불가)** 이다. 모든 파일(.run, DCGM rpm, Go 바이너리)은 관리 서버·autofs 경로에서만 가져오며 외부 다운로드는 하지 않는다. Go 바이너리는 정적 빌드(`CGO_ENABLED=0`, vendor 없이 사전 빌드 커밋)로 배포한다.
   rhel.repo 의 baseurl 은 `file://` 경로이고, 관리 서버에서도 같은 경로로 읽을 수 있다. 그 밖의 형식이면 오류 메시지만 표시한다.
2. DCGM rpm 은 추가 의존성 없이 localinstall 된다. 의존성이 필요하면 그 rpm 도 같은 경로에서 함께 보낸다(테스트에서 확인).
3. 대상 OS 는 RHEL/Rocky 8 이상(dnf)이다.
4. 드라이버를 바꾼 뒤 재부팅은 하지 않는다. 모듈 교체만으로 nvidia-smi 가 새 버전을 보고해야 한다.

## 8. 테스트 (home-test VM 환경)

| VM | 역할 |
|---|---|
| 192.168.0.58 | 관리 서버 + Go 빌드 + NFS 서버(설치파일 디렉토리, DCGM rpm repo) |
| 192.168.0.70 | **마운트 서버**: autofs 로 `/user/<MOUNT_USER>` 와 설치파일 경로가 마운트됨 |
| 192.168.0.60 | **비마운트 서버**: autofs 없음 → scp 경로 |

VM 에는 GPU 가 없으므로 `test/fake/` 의 가짜 도구를 테스트 VM 에서만 PATH 앞에 둔다.
- `NVIDIA-Linux-x86_64-<ver>.run`(가짜): `-s` 로 실행하면 `/var/lib/fakegpu/version` 에 버전을 기록한다. 버전별 CUDA 값은 예를 들어 550.x→12, 580.x→13, 470.x→11 로 정한다.
  `/var/lib/fakegpu/busy` 가 있으면 "module in use" 로 실패한다. `busy_once` 모드는 언로드 후 성공하고, `busy_always` 모드는 계속 실패한다.
- `nvidia-smi`, `lspci`, `rmmod`, `lsmod`(가짜): 위 상태 파일을 기준으로 동작한다.
- 가짜 DCGM rpm 6종(`rpmbuild`): `nvidia-dcgm.service`(sleep 방식) 와 가짜 `dcgmi` 를 포함한다. createrepo 로 만든 repo 를 rhel.repo(enabled=0)로 등록한다.

| # | 시나리오 | 기대 결과 |
|---|---|---|
| T1 | 70(mount)·60(scp) 모두 `hostname` 만 → 버전 한 번 질문(550) | 둘 다 550 / CUDA12 / dcgm cuda12 active, 70 의 rhel.repo enabled=0 복귀 |
| T2 | `hostname 580.x` 로 재설치 | 재설치 확인 목록 출력 → 둘 다 580 / cuda13, cuda12 패키지 제거됨 |
| T3 | 호스트별 다른 버전 | 각자 요청한 버전으로 설치 |
| T4 | busy_once | 언로드 후 재시도로 성공 |
| T5 | busy_always | fail_list 와 fail_list.retry 생성, 종료 코드 1 |
| T6 | 470.x(CUDA11) | 드라이버만 설치, DCGM 건너뜀 경고 |
| T7 | DCGM 설치 중 중단(Ctrl-C) / dnf 실패 | rhel.repo enabled=0 복구 확인 |
| T8 | kernel-devel 없음 / nouveau 로드 상태 / GPU 없음 | 각각 fail 사유 / 언로드 후 진행 / 건너뜀 |
| T9 | 같은 버전 재요청 | 기본 건너뜀 |
| T10 | 인자로 다른 autofs 경로 지정 | 그 경로의 .run 사용 |

Go 단위 테스트: list 파싱, 버전과 파일 연결, nvidia-smi·dcgmi 출력 파싱, fail_list 형식, 결과 표 golden 비교.

**완성 조건**: T1–T10 통과. 70 과 60 의 최종 상태(드라이버 버전, DCGM 패키지, nvidia-dcgm 상태, rhel.repo=0)가 같을 것. 실제 GPU 서버 1대에서 확인하는 것은 사용자가 별도로 수행한다.

## 9. 작업 순서

1. Go 뼈대: 인자·list 파싱, 버전 질문 → 단위 테스트
2. gossh 를 이용한 현황 수집, 마운트 판정, 재설치 확인
3. 드라이버 설치(mount/scp), 언로드 후 재시도, fail_list
4. DCGM(repo 토글과 trap / localinstall, cuda 교체, 판정)
5. bash 래퍼, os8 바이너리 빌드, README
6. VM 환경 구성(NFS/autofs, 가짜 도구, 가짜 rpm repo) → T1–T10
