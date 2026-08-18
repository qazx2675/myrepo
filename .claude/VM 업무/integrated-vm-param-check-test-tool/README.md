# integrated-vm-param-check-test-tool

`vm-param-check`(체크+자동교정, `-fix` 내장)와 `vc-test-env`(vcsim 테스트환경 복제)를
**한 폴더로 묶어**, 인터넷이 안 되는 폐쇄망 서버로 그대로 옮겨서 빌드→테스트까지 끝낼 수 있게
만든 배포 패키지입니다. 두 도구 모두 Go 모듈 자체에 `vendor/`를 포함하고 있어 `bash setup.sh`
한 줄이면 인터넷 없이 빌드됩니다.

```
integrated-vm-param-check-test-tool/
├── vm-param-check/   체크+자동교정 도구 (소스+vendor/, 빌드하면 이 폴더 안에 바이너리 생성)
├── vc-test-env/      실 vCenter -> vcsim 복제 도구 (소스+vendor/, 빌드하면 이 폴더 안에 바이너리 생성)
├── testkit/          위 두 도구를 엮어서 실행하는 셸 스크립트 4개 (build-all/start-vcsim/run-tests/stop-vcsim)
└── README.md         이 문서
```

각 도구 자체의 전체 옵션/동작 설명은 `vm-param-check/README.md`, `vc-test-env/README.md`를
참고하세요. 이 문서는 **두 도구를 묶어서 어떻게 빌드하고 테스트하는지**에 집중합니다.

⚠️ **주의사항 (Disclaimer)**
본 로그 분석 관련 스크립트 및 툴은 100% 신뢰하기보다는 참고용(보조 도구)으로 사용하는 것을 권장합니다. 설정 변경 스크립트의 경우에는 설정변경후 랜덤한 서버 몇개를 확인해서 실제로 변경되었는지 확인하는 절차가 반드시 필요합니다.

## 1. 빌드 및 설치 방법

### 1.1 폐쇄망으로 이관하기 (최초 1회, 인터넷 되는 곳에서)
이 폴더를 통째로 압축해서 옮기면 됩니다. `vendor/`가 각 도구 안에 이미 포함되어 있어서
부분 복사 없이 폴더 전체를 옮기는 게 핵심입니다 — 일부만 옮기면 빌드가 실패합니다.

```bash
# 인터넷 되는 곳(예: 이 저장소를 git clone한 서버)에서
cd myrepo
tar czf vm-param-check-testkit.tar.gz \
  ".claude/VM 업무/integrated-vm-param-check-test-tool"

# USB, scp, 사내 파일서버 등으로 폐쇄망 서버에 복사
scp vm-param-check-testkit.tar.gz <폐쇄망서버>:/tmp/
```

### 1.2 폐쇄망 서버에서 압축 해제 + 초기 설정
```bash
cd /tmp
tar xzf vm-param-check-testkit.tar.gz
cd ".claude/VM 업무/integrated-vm-param-check-test-tool"

# 원하는 최종 위치로 옮기고 싶다면 (예: /opt/vm-param-check-testkit)
# mv "$(pwd)" /opt/vm-param-check-testkit && cd /opt/vm-param-check-testkit
```

**필요 환경 (폐쇄망 서버 기준)**:
- Go 1.21 이상 (Rocky Linux + Go 1.26.5 기준으로 빌드·테스트 검증 완료)
- 인터넷 **불필요** — `vendor/`에 `github.com/vmware/govmomi` 등 의존성이 전부 포함됨
- vcsim으로만 테스트할 경우 실 vCenter 접속 불필요. 단, **최초 1회** 실제 vCenter 구조를
  복제하려면(레시피 추출) 그 vCenter에 접속 가능해야 함 — 그 이후로는 캐시된 레시피
  (`~/.vc-test-env/recipes/*.json`)만으로 vcsim을 계속 재기동할 수 있어 실접속이 불필요해짐.

```bash
chmod +x testkit/*.sh
```

### 1.3 (선택) 레시피 캐시 미리 챙기기
실 vCenter 구조를 미리 추출해둔 레시피가 있으면, 폐쇄망 서버에서 vCenter 접속 없이 바로 vcsim을
띄울 수 있습니다. 인터넷 되는 곳(원 vCenter에 접속 가능한 곳)에서 미리 추출해서 같이 옮기세요.

```bash
# 인터넷 되는 곳에서, 원본 vCenter 구조를 레시피로 추출
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
`vm-param-check/vm-param-check`, `vc-test-env/vc-test-env` 두 바이너리가 생성되면 성공입니다.

### 1.5 전역 명령어로 사용하기 (선택 사항)
빌드된 실행 파일을 PATH 환경 변수에 포함된 디렉터리로 이동하거나, 실행 파일이 있는 경로를 PATH에 추가하면 어디서든 명령어처럼 사용할 수 있습니다.

예시:
```bash
sudo cp vm-param-check/vm-param-check vc-test-env/vc-test-env /usr/local/bin/
```

## 2. 사용 방법

### 2.1 vcsim 기동 (테스트용 가상 vCenter)
```bash
# 캐시된 레시피가 있으면 그걸 그대로 사용 (실 vCenter 접속 없음)
./testkit/start-vcsim.sh -vc=192.168.0.50

# 레시피가 아예 없어서 최초로 추출부터 해야 하는 경우 (실 vCenter 접속 필요)
VC_USER=administrator@vsphere.local VC_PASS='<비밀번호>' ./testkit/start-vcsim.sh -vc=192.168.0.50
```

`127.0.0.1:54321`(고정 포트)에서 응답할 때까지 자동으로 대기했다가 "기동 완료"를 출력합니다.
백그라운드로 뜨고, PID는 `testkit/out/vcsim.pid`에 저장됩니다.

### 2.2 체크 + 자동교정 테스트 실행
```bash
export VC_USER=user      # vcsim은 계정/비밀번호 값 자체를 검사하지 않음 — 아무 문자열이나 가능
export VC_PASS=pass
./testkit/run-tests.sh
```
테스트 결과 CSV/로그는 전부 `testkit/out/`에 남습니다. 재실행할 때마다 덮어써집니다.

### 2.3 vcsim 종료
```bash
./testkit/stop-vcsim.sh
```

### 2.4 실제 vCenter를 직접 대상으로 체크/교정하고 싶을 때
`testkit/`은 vcsim(가상환경) 검증용입니다. 실제 vCenter를 직접 체크/교정하려면 vcsim을 거치지
않고 `vm-param-check` 바이너리를 바로 실행하면 됩니다.
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
(이 패키지에는 별도 옵션이 없으며, 내부 툴들의 옵션은 각각 `vm-param-check/README.md`와 `vc-test-env/README.md`를 참고하세요.)

## 4. 문서별 고유 설명

### 자주 겪는 문제

| 증상 | 원인/해결 |
|---|---|
| `go build` 시 네트워크 요청 시도 | `-mod=vendor`를 빼먹은 것. `testkit/build-all.sh`는 항상 붙여서 실행하므로, 직접 `go build`를 칠 때만 조심 |
| `run-tests.sh`가 즉시 실패 | `start-vcsim.sh`를 먼저 실행하지 않았거나, 이전 vcsim이 비정상 종료된 상태. `testkit/out/vcsim.log` 확인 |
| `start-vcsim.sh`가 거부됨 | 이전 테스트의 vcsim이 아직 떠 있음. `./testkit/stop-vcsim.sh`로 정리 후 재시도 |
| 레시피 추출 단계 접속 실패 | `VC_USER`/`VC_PASS` 오타 확인, 방화벽/네트워크 경로 확인 |
| vcsim 대상인데 `[미지원]` 태그가 안 보임 | `vm-param-check` 바이너리가 오래된 버전일 수 있음. `testkit/build-all.sh` 다시 실행 |
