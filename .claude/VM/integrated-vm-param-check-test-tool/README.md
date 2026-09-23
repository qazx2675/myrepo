# integrated-vm-param-check-test-tool

`vm-param-check`(체크+자동교정, `-fix` 내장)와 `vc-test-env`(vcsim 테스트 환경 복제)를
**한 폴더로 묶어**, 인터넷이 안 되는 폐쇄망 서버로 옮겨 빌드부터 테스트까지 끝낼 수 있게
만든 배포 패키지입니다. 두 도구 모두 `bash setup.sh` 한 줄이면 인터넷 없이 빌드됩니다.

```
integrated-vm-param-check-test-tool/
├── vm-param-check/   체크+자동교정 도구 (빌드하면 이 폴더 안에 바이너리 생성)
├── vc-test-env/      실 vCenter -> vcsim 복제 도구 (빌드하면 이 폴더 안에 바이너리 생성)
├── testkit/          위 두 도구를 엮어 실행하는 셸 스크립트 4개 (build-all/start-vcsim/run-tests/stop-vcsim)
└── README.md         이 문서
```

각 도구의 전체 옵션과 동작은 `vm-param-check/README.md`, `vc-test-env/README.md`를
참고하세요. 이 문서는 **두 도구를 묶어서 어떻게 빌드하고 테스트하는지**에 집중합니다.
빌드 → vcsim 기동 → 테스트 → 종료 흐름은 [WORKFLOW.md](WORKFLOW.md)에 그려 두었습니다.

⚠️ **주의사항 (Disclaimer)**
본 로그 분석 관련 스크립트 및 툴은 100% 신뢰하기보다는 참고용(보조 도구)으로 사용하는 것을 권장합니다. 설정 변경 스크립트의 경우, 설정 변경 후 무작위로 서버 몇 대를 골라 실제로 변경되었는지 직접 확인하는 절차가 반드시 필요합니다.

## 1. 빌드 및 설치 방법

### 1.1 폐쇄망으로 옮기기 (최초 1회, 인터넷 되는 곳에서)

두 도구의 의존성은 저장소의 공통 폴더 `.claude/공통/govendor/`에 있고, 각 `setup.sh`가 이를 `vendor`로 링크해서 빌드합니다.
따라서 **이 폴더와 함께 `.claude/공통/govendor/`의 해당 버전 폴더도 같은 상대 위치로 옮겨야** 합니다. 일부만 옮기면 빌드가 실패합니다.

| 도구 | 사용하는 공통 의존성 |
|---|---|
| `vm-param-check` | `.claude/공통/govendor/govmomi-0.39.0` |
| `vc-test-env` | `.claude/공통/govendor/govmomi-0.55.1-vcsim` |

```bash
# 인터넷 되는 곳(예: 이 저장소를 git clone한 서버)에서
cd myrepo
tar czf vm-param-check-testkit.tar.gz \
  ".claude/VM/integrated-vm-param-check-test-tool" \
  ".claude/공통/govendor/govmomi-0.39.0" \
  ".claude/공통/govendor/govmomi-0.55.1-vcsim"

# USB, scp, 사내 파일서버 등으로 폐쇄망 서버에 복사
scp vm-param-check-testkit.tar.gz <폐쇄망서버>:/tmp/
```

### 1.2 폐쇄망 서버에서 압축 해제 + 초기 설정

```bash
cd /tmp
tar xzf vm-param-check-testkit.tar.gz
cd ".claude/VM/integrated-vm-param-check-test-tool"
# 다른 위치로 옮길 때는 .claude/ 구조(공통/govendor 와의 상대 위치)를 그대로 유지하세요
```

**필요 환경 (폐쇄망 서버 기준)**:

- Go 1.21 이상 (Rocky Linux + Go 1.26.5 기준으로 빌드·테스트 검증 완료)
- 인터넷 **불필요** — 위 공통 govendor에 `github.com/vmware/govmomi` 등 의존성이 모두 들어 있음
- vcsim으로만 테스트할 때는 실 vCenter 접속이 필요 없습니다. 단, **최초 1회** 실제 vCenter 구조를
  복제하려면(레시피 추출) 그 vCenter에 접속할 수 있어야 합니다. 그 뒤로는 캐시된 레시피
  (`~/.vc-test-env/recipes/*.json`)만으로 vcsim을 계속 다시 띄울 수 있어 실접속이 필요 없습니다.

```bash
chmod +x testkit/*.sh
```

### 1.3 (선택) 레시피 캐시 미리 챙기기

실 vCenter 구조를 미리 추출해 둔 레시피가 있으면, 폐쇄망 서버에서 vCenter 접속 없이 바로 vcsim을
띄울 수 있습니다. 원본 vCenter에 접속할 수 있는 곳에서 미리 추출해 함께 옮기세요.

```bash
# 원본 vCenter에 접속 가능한 곳에서, vCenter 구조를 레시피로 추출
cd vc-test-env
bash setup.sh
VC_USER=administrator@vsphere.local VC_PASS='<비밀번호>' ./vc-test-env extract -vc=192.168.0.50

# 이 파일을 폐쇄망 서버의 같은 경로로 복사
scp ~/.vc-test-env/recipes/192.168.0.50.json <폐쇄망서버>:~/.vc-test-env/recipes/
```

### 1.4 빌드

```bash
./testkit/build-all.sh
```

`vm-param-check/vm-param-check`, `vc-test-env/vc-test-env` 두 바이너리가 생기면 성공입니다.

## 2. 사용 방법

### 2.1 vcsim 기동 (테스트용 가상 vCenter)

```bash
# 캐시된 레시피가 있으면 그대로 사용 (실 vCenter 접속 없음)
./testkit/start-vcsim.sh -vc=192.168.0.50

# 레시피가 없어 처음부터 추출해야 하는 경우 (실 vCenter 접속 필요)
VC_USER=administrator@vsphere.local VC_PASS='<비밀번호>' ./testkit/start-vcsim.sh -vc=192.168.0.50
```

`127.0.0.1:54321`(고정 포트)이 응답할 때까지 자동으로 기다렸다가 "기동 완료"를 출력합니다.
백그라운드로 실행되며, PID는 `testkit/out/vcsim.pid`에 저장됩니다.

### 2.2 체크 + 자동교정 테스트 실행

```bash
export VC_USER=user      # vcsim은 계정/비밀번호 값을 검사하지 않음 — 아무 문자열이나 가능
export VC_PASS=pass
./testkit/run-tests.sh
```

테스트 결과 CSV/로그는 모두 `testkit/out/`에 남으며, 다시 실행할 때마다 덮어씁니다.

### 2.3 vcsim 종료

```bash
./testkit/stop-vcsim.sh
```

### 2.4 실제 vCenter를 직접 체크·교정하려면

`testkit/`은 vcsim(가상 환경) 검증용입니다. 실제 vCenter를 직접 체크·교정하려면 vcsim을 거치지
않고 `vm-param-check` 바이너리를 바로 실행합니다.

```bash
cd vm-param-check
export VC_USER=administrator@vsphere.local
export VC_PASS='<비밀번호>'
echo "192.168.0.50" > vcenter.txt
./vm-param-check -vcenterList=vcenter.txt \
  -ht=on -cores=1 -numa=2 -cpu=2 -mem=4 -disk=40 -shares-ev01=1000 \
  -out=result.csv
```

## 3. 옵션별 상세 설명

이 패키지 자체에는 별도 옵션이 없습니다. 내부 도구의 옵션은 각각 `vm-param-check/README.md`와 `vc-test-env/README.md`를 참고하세요.

## 4. 문서별 고유 설명

### 4.1 디렉토리 구조

```
integrated-vm-param-check-test-tool/
├── README.md          # 이 문서
├── WORKFLOW.md        # 빌드 → vcsim 기동 → 테스트 → 종료 흐름도
├── ARCHITECTURE.md    # 폴더별 역할
├── PR_CHECKLIST.md    # 수정·배포 전 체크리스트
├── vm-param-check/    # VM 파라미터 체크+자동교정 도구, 상세는 하위 README 참고
├── vc-test-env/       # 실 vCenter -> vcsim 복제 도구, 상세는 하위 README 참고
└── testkit/           # 위 두 도구를 엮어 빌드·테스트하는 셸 스크립트 모음
    ├── build-all.sh   # vm-param-check, vc-test-env 두 바이너리를 한 번에 빌드
    ├── start-vcsim.sh # vcsim(테스트용 가상 vCenter) 기동
    ├── run-tests.sh   # vcsim 대상 체크+자동교정 테스트 실행 (결과는 testkit/out/에 저장)
    └── stop-vcsim.sh  # 기동한 vcsim 종료
```

### 4.2 자주 겪는 문제

| 증상 | 원인/해결 |
|---|---|
| `go build` 중 네트워크 요청 시도 | `-mod=vendor`를 빠뜨린 경우입니다. `testkit/build-all.sh`는 항상 붙여서 실행하므로, 직접 `go build`를 입력할 때만 주의하세요 |
| `setup.sh`에서 vendor 관련 빌드 실패 | `.claude/공통/govendor/`의 해당 버전 폴더를 함께 옮기지 않은 경우입니다(1.1 참고) |
| `run-tests.sh`가 바로 실패 | `start-vcsim.sh`를 먼저 실행하지 않았거나 이전 vcsim이 비정상 종료된 상태입니다. `testkit/out/vcsim.log`를 확인하세요 |
| `start-vcsim.sh`가 거부됨 | 이전 테스트의 vcsim이 아직 떠 있습니다. `./testkit/stop-vcsim.sh`로 정리한 뒤 다시 시도하세요 |
| 레시피 추출 단계에서 접속 실패 | `VC_USER`/`VC_PASS` 오타, 방화벽/네트워크 경로를 확인하세요 |
| vcsim 대상인데 `[미지원]` 태그가 안 보임 | `vm-param-check` 바이너리가 오래된 버전일 수 있습니다. `testkit/build-all.sh`를 다시 실행하세요 |

## 5. 전역 명령어로 사용하기 (선택 사항)

빌드된 실행 파일을 PATH 환경 변수에 포함된 디렉터리로 옮기거나, 실행 파일이 있는 경로를 PATH에 추가하면 어디서든 명령어처럼 쓸 수 있습니다.

```bash
sudo cp vm-param-check/vm-param-check vc-test-env/vc-test-env /usr/local/bin/
```
