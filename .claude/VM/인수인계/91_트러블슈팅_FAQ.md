# 91. 트러블슈팅 / FAQ

오류 메시지나 증상으로 찾으세요. 도구별 고유 오류는 각 도구 문서의 "자주 나는 오류" 절에도 있습니다.

---

## 1. 빌드 단계

### `go: command not found`

Go가 설치되지 않았습니다. [02_공통_실행환경](./02_공통_실행환경.md)의 "Go 설치"를 보세요. 폐쇄망에서도 tar.gz 하나면 설치됩니다.

### `go build`가 인터넷에 접속하려고 함 / `dial tcp ... i/o timeout`

`-mod=vendor` 플래그를 빼먹은 것입니다.

```bash
bash setup.sh          # ✅ 항상 이걸 쓰세요
go build .             # ❌ 폐쇄망에서 실패
```

### `vendor/ 없음 — 인터넷 되는 PC에서 'go mod vendor' 실행 후 옮기세요`

폴더를 복사할 때 `vendor/`가 빠진 것입니다. `vendor/`는 파일이 수천 개라 실수로 빼기 쉽습니다. **폴더를 통째로** 다시 복사하세요.

```bash
# 인터넷 되는 곳에서
cd myrepo && tar czf 도구.tar.gz ".claude/VM/<폴더>"
```

### `go build .` 에서 `main redeclared in this block`

`vm-setting-go-lang/` 폴더입니다. **한 폴더에 `main()`이 4개** 있어서 디렉토리 전체 빌드가 안 됩니다.

```bash
bash setup.sh    # 파일명을 지정해 4개를 각각 빌드
```

### 빌드는 됐는데 실행하면 `Permission denied`

```bash
chmod +x <바이너리>
```

---

## 2. 인증 / 접속

### `인증 정보 로드 실패` / `VC_PASSWORD 환경 변수가 설정되지 않았습니다`

**도구마다 환경변수 이름이 다릅니다.** 가장 흔한 실수입니다.

| 도구 계열 | 계정 | 비밀번호 |
|---|---|---|
| 체크 계열 (`vm-param-check`, `vm_verifier`, `vc-test-env`) | `VC_USER` | **`VC_PASS`** |
| 설정변경 계열 (`VM_setup/*`, `vm-setting-go-lang/*`, `vm-network-migration`) | `-id` 플래그 | **`VC_PASSWORD`** |

안전하게 하려면 둘 다 설정하세요.

```bash
read -rsp 'vCenter 비밀번호: ' P; echo
export VC_PASS="$P" VC_PASSWORD="$P" VC_USER='administrator@vsphere.local'
unset P
```

### `vCenter 접속 실패` / 인증은 맞는데 로그인이 안 됨

1. **계정이 맞는가?** 대부분의 설정변경 도구는 `-id`의 기본값이 `lscsystems@vsphere.local`입니다. 다른 계정을 쓰려면 **반드시 명시**하세요.
2. **네트워크가 되는가?**
   ```bash
   curl -k -I https://<vCenter IP>
   ```
3. **비밀번호에 특수문자가 있는가?** 환경변수에 넣을 때 작은따옴표로 감싸세요.
   ```bash
   export VC_PASS='p@ss!word#123'
   ```

### 권한 오류 (`NoPermission`, `Permission to perform this operation was denied`)

계정에 필요한 권한이 없습니다.

| 작업 | 필요 권한 |
|---|---|
| 점검만 | Read-Only |
| VM 설정 변경 | Virtual machine > Configuration > **Reconfigure** |
| 포트그룹 생성 | Host > Configuration > **Network configuration** |
| 호스트 등록 | Host > Inventory > **Add host** |
| 라이선스 할당 | Global > **Licenses** |

---

## 3. 대상을 못 찾음

### `요청한 대상을 전부 체크하지 못했습니다` / `어느 vCenter에서도 찾지 못한 대상`

1. **VM 이름 오타** — vSphere Client에서 정확한 이름을 확인하세요. 대소문자도 봅니다.
2. **다른 vCenter에 있음** — `vcenter.txt`에 그 vCenter가 들어 있는지 확인
3. **vCenter 접속 실패** — 같은 경고에 "조회에 실패해 건너뛴 vCenter"가 함께 나옵니다

### `worklist 와 매칭되는 VM 을 vCenter 에서 찾지 못했습니다`

`worklist.txt`에는 **물리 호스트 이름**을 적고, 도구가 여기에 `ev01`/`ev02`를 붙입니다. VM 이름(`svr01ev01`)을 적으면 `svr01ev01ev01`을 찾게 되어 실패합니다.

```
svr01        ✅ 호스트 이름
svr01ev01    ❌ VM 이름 (worklist에는 안 됨)
```

> 예외: `vm_verifier`의 `-f`와 `numa_preferht_setting`의 `-f`는 VM 이름을 그대로 받습니다.

### `please specify a datacenter`

데이터센터가 여러 개인데 지정하지 않았습니다. `-datacenter <이름>`을 주세요. (`vm_connect` v1의 알려진 버그이기도 했습니다 — v2에서 수정됨)

---

## 4. 설정 변경이 안 됨

### `-fix`에서 "게이트 실패 — 그룹 동질성"

대상에 **스펙이 다른 VM이 섞여** 있습니다. ev01끼리, ev02끼리 vCPU/코어/메모리/디스크/Shares/NUMA/HT가 전부 같아야 하고, 그룹 간 VM 대수도 같아야 합니다.

→ 대상 목록을 스펙별로 나눠서 따로 실행하세요.

### `-fix`에서 "게이트 실패 — 전원 OFF 아님"

**CPU 토폴로지는 VM이 켜져 있으면 바꿀 수 없습니다.** 하드웨어 레벨 설정이기 때문입니다.

→ 대상 VM을 전부 끄고 재실행하세요.

### `numa_preferht_setting`이 `[VM명] 전원 ON 상태 — 스킵 (PASS)`

같은 이유입니다. 이 도구는 켜진 VM을 **건너뛰고 계속 진행**합니다(중단하지 않음).

### `-fix` 후에도 FAIL이 남음

**수동조치 대상**일 가능성이 높습니다. 아래는 도구가 고치지 않습니다.

- 메모리 (`config.hardware.memoryMB`)
- 디스크 총 용량
- CPU/메모리 Shares(ratio)
- 호스트 전원정책
- "모든 게스트 메모리 예약"
- 네트워크 포트그룹

→ vSphere Client에서 직접 변경하세요.

### 코어/소켓/NUMA가 안 나누어떨어진다는 에러

vSphere 제약입니다. **vCPU 수는 소켓당 코어 수로 나누어떨어져야** 하고, NUMA를 지정하면 코어 수가 그 값으로도 나누어떨어져야 합니다.

```
[ev01] 코어 수(5)가 소켓 수(2)로 나누어떨어지지 않습니다.
```

→ 값을 다시 계산하세요.

### 설정이 반영됐다는데 vSphere Client에서 안 보임

1. **브라우저 새로고침** — vSphere Client는 캐시가 강합니다
2. **ExtraConfig는 "설정 편집 > VM 옵션 > 고급 > 구성 매개변수 편집"** 에서 봅니다. 기본 화면에는 안 나옵니다
3. **CPU 토폴로지는 "설정 편집 > VM 옵션 > CPU 토폴로지"** 에서 봅니다
4. **Auto 모드**면 NUMA 값이 의미 없게 표시됩니다

---

## 5. 판정 결과가 이상함

### 전부 `설정없음`으로 나옴

1. **옵션을 안 줬을 수 있습니다.** `-cores-ev02` 같은 그룹 옵션은 안 주면 **스킵**되지만, 공통 옵션은 필수입니다.
2. **구버전 vCenter** — `config.numaInfo.coresPerNumaNode`는 vSphere API **8.0.0.1 이상**이 필요합니다.

### `미지원` 판정이 나옴

**vcsim 시뮬레이터를 대상으로 체크한 것입니다.** 실제 vCenter에서는 나오지 않습니다.

vcsim이 구현하지 않는 필드 3개:
- `config.memoryReservationLockedToMax`
- `config.numaInfo.coresPerNumaNode`
- `cpuid.coresPerSocket`

### `-onlyFail`을 줬는데 PASS 서버가 요약에 나옴

**의도된 동작입니다.** 상세에서는 빠지지만 **요약 표에는 PASS도 나옵니다** — 여기서까지 빠지면 "이 서버가 정상이었는지, 아예 검사가 안 된 건지" 구분할 수 없기 때문입니다.

### affinity 값 순서가 다른데 OK로 나옴

**의도된 동작입니다.** `sched.vcpuN.affinity`는 "돌 수 있는 물리 CPU 후보 목록"이라 순서에 의미가 없습니다. `31,29,27`과 `27,29,31`은 같은 설정입니다.

단 **개수는 봅니다** — `16,17`과 `16,17,17`은 다르게 판정합니다.

### CPU 토폴로지 값이 우연히 맞는데 왜 "설정없음"인가

`autoCoresPerNumaNode=true`(vSphere UI: "NUMA 노드 — 전원을 켤 때 할당됨")인 VM은 `coresPerNumaNode`에 **지난 전원 켜짐 시점의 값이 남아 있을 뿐**입니다. 우연히 값이 같아도 OK로 판정하면 안 되므로 "설정없음"으로 처리합니다. (2026-08-23 수정)

---

## 6. 스펙 자동매칭 (`-specRoot`)

### 스펙을 못 찾음

1. **폴더명이 규칙에 안 맞음** — 하이픈 4조각, 2번째가 `CAE<숫자>`/`LSI<숫자>`여야 합니다
2. **스펙 파일이 없음** — `-initFolder`로 만드세요
3. **접두어가 다름** — `CAE`와 `LSI`는 서로 다른 스펙으로 취급합니다
4. **허용 접두어에 없음** — `config/spec.go`의 `caeRecord` 정규식에 목록이 고정되어 있습니다. 새 접두어는 거기 추가해야 합니다

### `Task` 폴더에 있는 VM

1. 포트그룹명(`<원래폴더명>-cae-옥텟-옥텟-옥텟-옥텟`)에서 자동 유추를 시도합니다
2. 못 정하면 물어봅니다 — **`?`를 입력하면 사용 가능한 스펙 목록**이 나옵니다
3. `-yes`가 켜져 있으면 물어볼 수 없어 **즉시 중단**합니다 → `-yes` 없이 한 번 대화형으로 먼저 돌리세요

### 이미 같은 스펙이 있다며 `-initFolder`가 거부됨

**의도된 동작입니다.** 차수만 달라도 같은 스펙으로 보므로, 실수로 덮어쓰는 걸 막습니다. 기존 것을 고치거나 `-template`으로 복사해서 다른 이름을 쓰세요.

---

## 7. vcsim / 테스트 환경

### `start-vcsim.sh`가 거부됨 / 포트 54321 사용 중

이전 vcsim이 아직 떠 있습니다.

```bash
./testkit/stop-vcsim.sh
# 또는
lsof -i :54321      # PID 확인 후 kill
```

`vc-test-env`는 기동 직전에 자동 감지해서 "종료하고 계속할까요?"를 물어봅니다.

### `run-tests.sh`가 즉시 실패

`start-vcsim.sh`를 먼저 실행하지 않았거나 이전 vcsim이 비정상 종료됐습니다. `testkit/out/vcsim.log`를 확인하세요.

### PowerCLI `Get-View`에서 deserializing 에러

```
Error in deserializing body of reply message for operation 'RetrieveProperties'
```

**govmomi 시뮬레이터 자체의 한계**입니다. `VirtualMachineRuntimeInfo.OperationNotSupported` 필드가 PowerCLI의 .NET SOAP 클라이언트가 못 읽는 형태로 직렬화됩니다.

우회:

```powershell
Get-View -ViewType VirtualMachine -Property Name,Config,Guest   # Runtime/Summary 제외
Get-VM                                                           # 고수준 cmdlet은 정상
```

### 레시피 캐시가 오염됨

`-vc`에 vcsim 자기 주소(`127.0.0.1:54321`)를 실수로 지정한 경우입니다(지금은 가드가 있어 막힙니다).

```bash
rm ~/.vc-test-env/recipes/127.0.0.1_54321.json
# ~/.vc-test-env/history.json 에서도 해당 항목 삭제
```

---

## 8. 네트워크 이관 (vm-network-migration)

### 종료 코드 2로 즉시 중단

**입력 오류입니다. VM은 하나도 안 건드렸습니다.** 비밀번호 미설정, `-user` 누락, `-id` 빈 값을 확인하세요.

### 종료 코드 3

**자동 롤백까지 실패했습니다. 수동 확인이 필요합니다.**

```bash
cat rollback_failed_hong.txt
```

여기 나온 VM을 vSphere Client에서 직접 원복하세요.

### 특정 VM이 백업 단계에서 실패

그 VM이 올라간 호스트가 `vswitch_{user}.txt`에 **여러 줄** 있습니다. 어느 포트그룹으로 옮길지 정할 수 없어 제외합니다. **호스트당 한 줄만** 남기세요.

### 재실행하니 뭘 할지 물어봄

`state_{user}.json`이 남아 있습니다.

- 원복이 목적 → `--rollback`
- 이어서 진행 → `--resume`
- `--force-backup`은 **원본 기록을 잃습니다.** 이미 일부가 바뀐 상태에서 쓰면 롤백이 무의미해집니다

### 포트그룹은 바뀌었는데 통신이 안 됨

이 도구는 **게스트 OS 내부를 확인하지 않습니다.** vCenter 인벤토리 기준으로만 검증합니다. 게스트에서 IP/라우팅을 확인하세요.

---

## 9. 결과 파일

### CSV를 엑셀에서 열면 한글이 깨짐

UTF-8 인코딩입니다. 엑셀에서 **"데이터 > 텍스트/CSV 가져오기"** 로 열고 인코딩을 **UTF-8**로 지정하세요. 더블클릭으로 열면 깨집니다.

### 여러 사람이 동시에 돌려서 파일이 덮어써짐

`-user` 옵션을 쓰세요.

```bash
./vm-param-check ... -out=result.csv -user=kdh    # → result_kdh.csv
```

### 결과 파일이 어디 생겼는지 모르겠음

**실행한 디렉토리**에 생깁니다. `-out`을 안 주면 타임스탬프 이름으로 자동 생성됩니다.

```bash
ls -lt *.csv | head
```

---

## 10. 자주 묻는 질문

### Q. 어느 도구부터 봐야 하나요?

[11. vm-param-check](./11_vm-param-check-usability-improvement.md)입니다. 주력 도구이고 다른 도구들의 결과를 점검하는 위치에 있습니다.

### Q. 이 도구를 믿어도 되나요?

**보조 도구로 쓰세요.** 모든 README에 같은 경고가 있습니다 — 설정 변경 후에는 **랜덤한 서버 몇 대를 vSphere Client에서 직접 확인**하는 절차가 반드시 필요합니다.

### Q. `VM_setup`과 `vm-setting-go-lang`은 뭐가 다른가요?

기능이 겹칩니다. `vm-setting-go-lang` 쪽이 병렬 처리(워커풀)와 v2 개선을 반영한 버전입니다. 설정 점검·교정은 [11번](./11_vm-param-check-usability-improvement.md)이 가장 낫습니다.

### Q. `power_setting`은 왜 소스가 없나요?

Go 소스를 어디에서도 확정하지 못했습니다. **컴파일된 바이너리가 유일한 사본이므로 절대 삭제하지 마세요.** 자세한 내용은 [10번 문서 13절](./10_VM_setup.md)에 있습니다.

### Q. 코드를 고치려면 Go를 알아야 하나요?

아니요. 이 도구들은 AI로 만들어졌고, 유지보수도 AI로 합니다. [30_유지보수_AI_활용가이드](./30_유지보수_AI_활용가이드.md)와 [31_변경요청서_양식](./31_변경요청서_양식.md)을 보세요.

### Q. 폐쇄망에 어떻게 가져가나요?

폴더를 **통째로** 압축해서 옮기고 `bash setup.sh`만 실행하면 됩니다. `vendor/`에 의존성이 전부 들어 있어 인터넷이 필요 없습니다. Go 자체가 없으면 `go1.26.5.linux-amd64.tar.gz`도 함께 가져가세요.

### Q. 실 vCenter 없이 연습할 수 있나요?

네. 세 가지 방법이 있습니다.

1. `./vm-param-check -demo` — 가짜 VM 3대로 출력 확인
2. [18. vc-test-env](./18_vcenter-test-env-vcsim.md) — 실 vCenter 구조를 복제한 vcsim
3. [19. 통합 테스트 패키지](./19_integrated-vm-param-check-test-tool.md) — 위 둘을 묶은 것

### Q. 문서를 고쳤는데 HTML이 안 바뀝니다

재생성해야 합니다.

```bash
cd .claude/VM/인수인계
python3 build_handbook.py
```

### Q. README에 있는 옵션을 줬는데 `flag provided but not defined` 오류가 납니다

**원본 README와 소스가 다른 곳이 몇 군데 있습니다.** 이 문서 세트를 만들면서 확인한 것:

| 도구 | README에 있지만 소스에 없는 것 |
|---|---|
| `esxi-log-check` | `-server <주소:포트>` (웹 서버 기능 자체가 미구현) → [15번 5절](./15_esxi-log-check.md) |
| `vm_lpage_bulk` (vm-setting-go-lang) | `-concurrency`, ev03 그룹 옵션 → [17번 10절](./17_vm-setting-go-lang.md) |
| `vm_create` (vm-setting-go-lang) | `-folderName`, Share의 `nomal` 문자열 → [17번 10절](./17_vm-setting-go-lang.md) |

**옵션의 정확한 사실은 항상 `main.go`의 `flag` 정의를 기준으로 하세요.**

```bash
grep -n "flag\." main.go        # 그 도구가 실제로 받는 옵션 전부
./<바이너리> -h                   # 또는 도움말
```

### Q. 여기에 없는 문제가 생겼습니다

1. 해당 도구 문서의 "자주 나는 오류" 절
2. 도구 폴더의 `README.md`(1차 자료)와 `CHANGELOG.md`
3. `main.go`의 `flag` 정의 (옵션의 정확한 동작)
4. 그래도 안 되면 [31_변경요청서_양식](./31_변경요청서_양식.md)으로 AI에게 조사·수정을 요청
