# 91. 트러블슈팅 / FAQ

오류 메시지나 증상으로 찾으세요. 도구별 고유 오류는 각 도구 문서의 "자주 나는 오류" 절에도 있습니다.

---

## 1. 빌드 단계

### `go: command not found`

Go가 없습니다. [02_공통_실행환경](./02_공통_실행환경.md)의 "Go 설치"를 보세요. 폐쇄망에서도 tar.gz 하나면 설치됩니다.

### `Go 버전이 낮습니다: ... (필요: 1.26.5 이상)`

V2 `setup.sh`가 요구하는 버전입니다. 폐쇄망에서는 새 Go를 자동으로 받지 않으므로 서버의 Go를 올려야 합니다.

### `go build`가 인터넷에 접속하려고 함 / `dial tcp ... i/o timeout`

`-mod=vendor`를 빼먹은 것입니다. 항상 `bash setup.sh`를 쓰세요.

### `govendor/ 폴더가 없거나 불완전합니다` / `vendor/ 없음`

의존성 폴더가 함께 옮겨지지 않았습니다.

| 도구 | 함께 있어야 하는 것 |
|---|---|
| V2 | `/home/govendor` (V2 네 폴더를 나란히) |
| vm_verifier | `.claude/공통/govendor/` (두 단계 위) |
| 망변경, vm-ip-change | 폴더 안의 `projects/*/vendor`, `project/vendor` |

### 실행하면 `Permission denied`

```bash
chmod +x <바이너리>
```

---

## 2. 인증 / 접속

### `인증 정보 로드 실패` / `VC_PASSWORD 환경 변수가 설정되지 않았습니다`

**도구마다 환경변수 이름이 다릅니다.**

| 도구 | 계정 | 비밀번호 |
|---|---|---|
| vm-param-check, vm_verifier | `VC_USER` | **`VC_PASS`** |
| V2 VMsetup, power_setting, 망변경 포트그룹 | `-id` / `-i` / `vc_id` | **`VC_PASSWORD`** |
| vm-ip-change | `VC_USER` + `GUEST_USER` | **`VC_PASSWORD`** + `GUEST_PASSWORD` |
| 망변경 IP·LDAP | — | `GOSSH_PW` |

```bash
export VC_USER='<계정>'
read -rsp 'vCenter 비밀번호: ' P; echo
export VC_PASS="$P" VC_PASSWORD="$P"; unset P
```

### 인증은 맞는데 로그인이 안 됨

1. **계정** — V2 개별 도구·power_setting의 `-id` 기본값은 `lscsystems@vsphere.local`, `vm_setup.sh -i` 기본값은 `administrator@vsphere.local`입니다. 다르면 명시하세요.
2. **네트워크** — `curl -k -I https://<vCenter IP>`
3. **특수문자** — 작은따옴표로 감싸서 export

### 권한 오류 (`NoPermission`)

필요 권한은 [02번 3절](./02_공통_실행환경.md) 표를 보세요.

---

## 3. 대상을 못 찾음

### `요청한 대상을 전부 체크하지 못했습니다` (vm-param-check)

1. VM 이름 오타 (대소문자 포함)
2. 그 VM이 있는 vCenter가 `vcenter.txt`에 없음
3. vCenter 접속 실패 — "조회에 실패해 건너뛴 vCenter"가 함께 표시됨

### BM 목록 파일에 VM 이름을 적음

`<user>.txt`/`worklist.txt`에는 **물리 호스트 이름**을 적습니다. VM 이름(`svr01ev01`)을 적으면 `svr01ev01ev01`을 찾게 됩니다.

> 예외: `vm_verifier -f`는 VM 이름도 받고, `numa_preferht_setting -f`와 `vm-param-check -f`는 VM 이름을 받습니다.

### 호스트를 못 찾음 / 같은 짧은 이름이 여러 대 (V2)

V2 `vm_create`·`vswitch_setting`은 이름이 정확히 같은 호스트를 먼저 찾고, 없으면 첫 `.` 앞부분끼리 비교해서 **하나뿐일 때만** 씁니다. 여러 대면 FQDN으로 적으세요.

### 망변경 E1/E3/E5 (포트그룹 단계에서 VM·호스트를 못 찾음)

```bash
./change.sh --debug-inventory
```

vCenter가 보고하는 이름과 `<계정>.txt`·`vswitch_<계정>.txt`의 표기를 대조합니다.

### `please specify a datacenter`

데이터센터가 여러 개입니다. `vm_create -datacenter <이름>`.

---

## 4. 설정 변경이 안 됨

### `-fix`에서 `[동질성 검증 실패]`

같은 스펙·같은 그룹 안의 교정 대상 VM끼리 vCPU/코어/메모리/디스크/Shares/NUMA/HT가 다릅니다. 다른 스펙의 VM이 섞였거나 스펙 폴더가 잘못 매칭된 것입니다. 대상 목록을 나눠 실행하세요. (그룹 간 VM 대수 차이는 경고만 하고 진행합니다.)

### `-fix`에서 `[전원 OFF 검증 실패]`

CPU 토폴로지는 VM이 켜져 있으면 바꿀 수 없습니다. 대상 VM을 끄고 재실행하세요.

### `numa_preferht_setting`이 켜진 VM을 건너뜀

같은 이유입니다. 켜진 VM은 건너뛰고 계속 진행합니다.

### `-fix` 후에도 FAIL이 남음

수동조치 항목입니다: 메모리, 디스크 용량, Shares, "모든 게스트 메모리 예약", 네트워크 포트그룹 → vSphere Client에서 변경. **호스트 전원정책**은 [17번 power_setting](./17_VM_setup_잔여도구.md).

### 코어/소켓/NUMA가 나누어떨어지지 않는다는 오류

vCPU 수는 소켓당 코어 수로 나누어떨어져야 합니다. 스펙 값을 다시 계산하세요.

### affinity 파일 `AUTO` 오류 / affinity 파일이 없다는 오류 (V2)

V2 `affinity_setting`은 자동 계산이 삭제되어 **ev마다 파일이 필수**입니다. `sched.vcpuN.affinity=CPU목록` 줄로 작성하세요. 여러 ev가 같은 파일을 써도 됩니다.

### 적용됐다는데 vSphere Client에서 안 보임

1. 브라우저 새로고침
2. ExtraConfig는 "설정 편집 > VM 옵션 > 고급 > 구성 매개변수 편집"
3. CPU 토폴로지는 "설정 편집 > VM 옵션 > CPU 토폴로지"

---

## 5. 판정 결과가 이상함

### `설정없음`이 많음

1. ev01 필수 기대값을 안 줬거나 스펙 파일에 값이 없음
2. vSphere API 8.0.0.1 미만 — `config.numaInfo.coresPerNumaNode` 없음
3. NUMA가 Auto 모드("전원을 켤 때 할당됨") — 값이 의미 없어 설정없음 처리

### `-onlyFail`인데 PASS가 요약에 나옴

의도된 동작입니다. 요약에서까지 빠지면 "정상이었는지, 검사가 안 된 건지" 구분할 수 없습니다.

### affinity 순서가 다른데 OK

의도된 동작입니다. 순서는 무시하고 개수는 봅니다.

---

## 6. 스펙 자동매칭 (`-specRoot`)

### 스펙을 못 찾음

1. 폴더명이 하이픈 4조각, 2번째가 `CAE<숫자>`/`LSI<숫자>` 규칙에 맞는지
2. 스펙 파일이 있는지 (`-initFolder`로 생성)
3. 접두어가 다른지 (`CAE`와 `LSI`는 다른 스펙)

### Task 폴더 VM / `-yes`로 실행했는데 중단

포트그룹명에서 유추하고, 못 하면 물어봅니다(`?` = 목록). `-yes`가 있으면 물어볼 수 없어 중단합니다 — `-yes` 없이 한 번 실행하세요.

### `-initFolder`가 거부됨

차수만 다른 같은 스펙이 이미 있습니다. 덮어쓰기 방지입니다.

### V2 `vm_setup.sh`가 스펙을 자동 할당하지 않음

스펙에 `affinity-evNN`이 없는 ev가 있으면 자동 할당하지 않고 이유를 출력합니다. 스펙에 ev마다 affinity 파일을 지정하세요.

---

## 7. 망변경 (Network_Change_Integration_Script)

### 화면 첫 줄에 코드만 나옴 (A1~G4)

[14번 5절](./14_Network_Change_Integration_Script.md)의 에러코드 표를 보세요.

### Ctrl+C를 눌렀는데 멈추지 않음

**5초 안에 3번** 눌러야 종료됩니다(1~2번은 안내만). 종료 시 되돌리기는 하지 않고, 안 끝난 호스트 목록을 남긴 뒤 `B1`로 멈춥니다.

### 로그인에 몇 시간씩 걸림

다른 VM이 CPU를 과점유한 경우입니다. ping이 되면 2차에서 최대 48시간(`timeout_pass2`)까지 기다립니다. 구버전 conf는 600초이므로 172800으로 바꾸세요.

### IP 변경 후 VM이 접속 안 됨

IP 변경 직후는 "새 IP 설정 + 옛 VLAN" 상태입니다. 포트그룹 변경 전에 리부팅하면 접속이 끊깁니다. 이미 끊겼다면 [15번 vm-ip-change](./15_vCenter_API_IP_자동변경.md)로 vCenter 경로에서 IP를 고칩니다.

### 되돌리고 싶음

```bash
./change.sh rollback <인시던트이름>     # 실행 끝에 출력된 줄 그대로
```

---

## 8. vm-ip-change

### ev02 대상이 계속 "대기중"

짝 ev01의 부하가 BM 물리 코어 수 이상입니다. 2분마다 재확인하고 내려가면 자동으로 시작합니다.

### 중간에 끊겨서 IP가 반쯤 바뀜

그 VM 콘솔에서 `bash /tmp/vm-ip-change/<호스트네임>.revert.sh`로 원래 설정을 복구합니다.

---

## 9. 결과 파일

### CSV를 엑셀에서 열면 한글이 깨짐

"데이터 > 텍스트/CSV 가져오기"에서 **UTF-8** 지정.

### 여러 사람이 동시에 돌려 파일이 덮어써짐

`vm-param-check`에 `-user=<이름>`을 주세요 (`result_<이름>.csv`).

### 결과 파일 위치

실행한 디렉터리입니다. 망변경은 `results/`, `incidents/`, `logs/`.

---

## 10. 자주 묻는 질문

### Q. 어느 도구부터 봐야 하나요?

[11. vm-param-check](./11_vm-param-check.md)입니다. 다른 도구의 결과를 점검하는 위치에 있습니다. 전체 순서는 [README의 전체 작업 흐름](./README.md).

### Q. `VM_setup`과 `V2`는 뭐가 다른가요?

V2가 V1 `VM_setup`의 도구를 모두 포함하고 ev01~ev99, `vm_setup.sh` 전체 실행, `SPEC_DIR` 공유를 더했습니다. V1에서 계속 쓰는 것은 `power_setting` 바이너리뿐입니다([10번 2절](./10_V2.md)).

### Q. `vm-network-migration`은 어디 갔나요?

`Network_Change_Integration_Script`로 통합되었습니다. 포트그룹만 바꾸려면 `./change.sh --only E -u <계정>`.

### Q. 이 도구를 믿어도 되나요?

보조 도구로 쓰세요. 설정 변경 후에는 **무작위 서버 몇 대를 직접 확인**하는 절차가 필요합니다.

### Q. `power_setting`은 왜 소스가 없나요?

Go 소스를 확정하지 못했습니다. **유일한 사본이므로 삭제하지 마세요.** [17번](./17_VM_setup_잔여도구.md).

### Q. 코드를 고치려면 Go를 알아야 하나요?

아니요. [30_유지보수_AI_활용가이드](./30_유지보수_AI_활용가이드.md)와 [31_변경요청서_양식](./31_변경요청서_양식.md)을 보세요.

### Q. README에 있는 옵션을 줬는데 `flag provided but not defined`

원본 README와 소스가 다른 곳이 있습니다 — [README의 불일치 목록](./README.md). 옵션의 정확한 사실은 소스 기준입니다.

```bash
grep -n "flag\." main.go      # 그 도구가 실제로 받는 옵션
./<바이너리> -h
```

### Q. 문서를 고쳤는데 HTML이 안 바뀝니다

```bash
cd .claude/VM/인수인계
python3 build_handbook.py
```

### Q. 여기에 없는 문제

1. 해당 도구 문서의 "자주 나는 오류"
2. 도구 폴더의 `README.md`, `CHANGELOG.md`
3. 소스의 `flag` 정의
4. [31_변경요청서_양식](./31_변경요청서_양식.md)으로 AI에게 조사·수정 요청
