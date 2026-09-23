# 19. integrated-vm-param-check-test-tool — 폐쇄망 반출용 통합 테스트 패키지

## 한눈에 보기

| 항목 | 값 |
|---|---|
| **위험도** | 🟡 **테스트용** (내부의 `vm-param-check`를 실 vCenter에 직접 쓰면 🔴) |
| 폴더 | `.claude/VM/integrated-vm-param-check-test-tool/` |
| 구성 | `vm-param-check` + `vc-test-env` + 이 둘을 엮는 셸 스크립트 4개 |
| 하는 일 | **폴더 하나만 폐쇄망으로 옮기면 빌드→vcsim 기동→테스트까지 끝** |

---

## 1. 이 폴더는 무엇인가

[11번 도구(`vm-param-check`)](./11_vm-param-check-usability-improvement.md)와 [18번 도구(`vc-test-env`)](./18_vcenter-test-env-vcsim.md)를 **한 폴더로 묶은 배포 패키지**입니다.

```
integrated-vm-param-check-test-tool/
├── vm-param-check/   체크+자동교정 도구 (소스+vendor/)
├── vc-test-env/      실 vCenter → vcsim 복제 도구 (소스+vendor/)
├── testkit/          두 도구를 엮어 실행하는 셸 스크립트 4개
└── README.md
```

**폐쇄망 서버로 이 폴더만 통째로 옮기면 인터넷 없이 빌드하고 테스트할 수 있습니다.**

> 각 도구 자체의 전체 옵션/동작은 [11번](./11_vm-param-check-usability-improvement.md), [18번](./18_vcenter-test-env-vcsim.md) 문서를 보세요. 이 문서는 **두 도구를 묶어서 어떻게 빌드하고 테스트하는지**에 집중합니다.

---

## 2. 폐쇄망으로 이관 (최초 1회)

### 2-1. 인터넷 되는 곳에서 압축

```bash
cd myrepo
tar czf vm-param-check-testkit.tar.gz ".claude/VM/integrated-vm-param-check-test-tool"
scp vm-param-check-testkit.tar.gz <폐쇄망서버>:/tmp/
```

> ⚠️ **`vendor/`가 각 도구 안에 이미 포함되어 있어 폴더 전체를 옮기는 게 핵심입니다.** 일부만 옮기면 빌드가 실패합니다.

### 2-2. 폐쇄망 서버에서 풀기

```bash
cd /tmp && tar xzf vm-param-check-testkit.tar.gz
cd ".claude/VM/integrated-vm-param-check-test-tool"
chmod +x testkit/*.sh

# 원하는 최종 위치로 옮기려면
# mv "$(pwd)" /opt/vm-param-check-testkit && cd /opt/vm-param-check-testkit
```

### 2-3. 필요 환경

| 항목 | 값 |
|---|---|
| Go | 1.21 이상 (Rocky Linux + Go 1.26.5 기준 검증 완료) |
| 인터넷 | **불필요** — `vendor/`에 의존성 전부 포함 |
| 실 vCenter 접속 | vcsim으로만 테스트하면 **불필요**. 단 **최초 1회** 실 vCenter 구조를 복제하려면 필요 |

### 2-4. (선택) 레시피 캐시 미리 챙기기

실 vCenter 구조를 미리 추출해두면 폐쇄망에서 vCenter 접속 없이 바로 vcsim을 띄울 수 있습니다.

```bash
# 인터넷 되는 곳(원 vCenter에 접속 가능한 곳)에서
cd vc-test-env
bash setup.sh
VC_USER=administrator@vsphere.local VC_PASS='<비밀번호>' ./vc-test-env extract -vc=192.168.0.50

# 레시피를 폐쇄망 서버의 같은 경로로 복사
scp ~/.vc-test-env/recipes/192.168.0.50.json <폐쇄망서버>:~/.vc-test-env/recipes/
```

---

## 3. 사용 방법

### 3-1. 빌드

```bash
./testkit/build-all.sh
```

`vm-param-check/vm-param-check`와 `vc-test-env/vc-test-env` 두 바이너리가 생성되면 성공입니다.

### 3-2. vcsim 기동 (테스트용 가상 vCenter)

```bash
# 캐시된 레시피가 있으면 그대로 사용 (실 vCenter 접속 없음)
./testkit/start-vcsim.sh -vc=192.168.0.50

# 레시피가 없어서 최초로 추출부터 해야 하는 경우 (실 vCenter 접속 필요)
VC_USER=administrator@vsphere.local VC_PASS='<비밀번호>' ./testkit/start-vcsim.sh -vc=192.168.0.50
```

`127.0.0.1:54321`(고정 포트)에서 응답할 때까지 자동으로 대기했다가 "기동 완료"를 출력합니다. 백그라운드로 뜨고 PID는 `testkit/out/vcsim.pid`에 저장됩니다.

### 3-3. 체크 + 자동교정 테스트 실행

```bash
export VC_USER=user      # vcsim은 계정/비밀번호 값 자체를 검사하지 않음
export VC_PASS=pass
./testkit/run-tests.sh
```

테스트 결과 CSV/로그는 전부 `testkit/out/`에 남습니다. **재실행할 때마다 덮어써집니다.**

### 3-4. vcsim 종료

```bash
./testkit/stop-vcsim.sh
```

### 3-5. 실제 vCenter를 직접 대상으로 하고 싶을 때 🔴

`testkit/`은 vcsim 검증용입니다. 실제 vCenter를 체크/교정하려면 vcsim을 거치지 않고 `vm-param-check` 바이너리를 바로 실행합니다.

```bash
cd vm-param-check
export VC_USER=administrator@vsphere.local
export VC_PASS='<비밀번호>'
echo "192.168.0.50" > vcenter.txt
./vm-param-check -vcenterList=vcenter.txt \
  -ht=on -cores=1 -numa=2 -cpu=2 -mem=4 -disk=40 -shares-ev01=1000 \
  -out=result.csv
```

---

## 4. testkit 스크립트 4개

| 스크립트 | 하는 일 |
|---|---|
| `build-all.sh` | `vm-param-check`, `vc-test-env` 두 바이너리를 한 번에 빌드 (`-mod=vendor` 항상 붙임) |
| `start-vcsim.sh` | vcsim 기동, 응답할 때까지 대기, PID를 `testkit/out/vcsim.pid`에 저장 |
| `run-tests.sh` | vcsim을 대상으로 체크+자동교정 테스트 실행, 결과를 `testkit/out/`에 저장 |
| `stop-vcsim.sh` | 기동한 vcsim 종료 |

> **이 패키지 자체에는 별도 옵션이 없습니다.** 내부 도구들의 옵션은 [11번](./11_vm-param-check-usability-improvement.md), [18번](./18_vcenter-test-env-vcsim.md) 문서를 보세요.

---

## 5. 자주 겪는 문제

| 증상 | 원인 / 해결 |
|---|---|
| `go build` 시 네트워크 요청 시도 | `-mod=vendor`를 빼먹은 것. `build-all.sh`는 항상 붙여서 실행하므로, **직접 `go build`를 칠 때만** 조심 |
| `run-tests.sh`가 즉시 실패 | `start-vcsim.sh`를 먼저 실행하지 않았거나 이전 vcsim이 비정상 종료됨. `testkit/out/vcsim.log` 확인 |
| `start-vcsim.sh`가 거부됨 | 이전 테스트의 vcsim이 아직 떠 있음. `./testkit/stop-vcsim.sh`로 정리 후 재시도 |
| 레시피 추출 단계 접속 실패 | `VC_USER`/`VC_PASS` 오타 확인, 방화벽/네트워크 경로 확인 |
| vcsim 대상인데 `[미지원]` 태그가 안 보임 | `vm-param-check` 바이너리가 오래된 버전일 수 있음. `build-all.sh` 다시 실행 |

---

## 6. 관련 문서

- 1차 자료: `integrated-vm-param-check-test-tool/README.md`
- 내부 도구 상세: [11. vm-param-check](./11_vm-param-check-usability-improvement.md), [18. vc-test-env](./18_vcenter-test-env-vcsim.md)
- 전 과정 자동화 테스트: [20. gemini_vcsim-pipeline-test](./20_gemini_vcsim-pipeline-test.md)
