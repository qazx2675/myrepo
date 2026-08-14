# vm_affinity_bulk - 계획서

## 목적

worklist에 등록된 다수 호스트(각각 `ev01`~`ev03` VM을 가짐)에 대해 CPU affinity 설정(ExtraConfig)을 한 번에, 안전하게(비동기 Task + 완료 대기 + 재조회 검증) 적용한다.

## 배경

- 수십~수백 대 VM에 affinity를 하나씩 수동으로 설정하는 것은 비효율적이고 실수하기 쉽다.
- vCenter `Reconfigure` API는 비동기(Task)이므로, Task 전송 성공 ≠ 실제 반영 성공이다. 반영 여부를 재확인하는 절차가 없으면 "적용한 줄 알았는데 실제로는 안 된" 상황이 발생할 수 있다.
- affinity 값은 VM마다, 호스트 그룹마다 다를 수 있어 설정 파일(`key=value` 텍스트)로 분리해서 관리할 필요가 있다.

## 처리 흐름

1. **설정 파일 파싱**: `-affinityFile01/02/03`을 `key=value` 형식으로 파싱. 전체가 `AUTO` 한 줄이면 vCPU 수 기준 1:1 자동 매핑 모드로 전환.
2. **대상 VM 이름 생성**: `worklistFile`의 각 호스트 이름 뒤에 `ev01`~`ev0N` 접미사를 붙임.
3. **vCenter 조회**: 전체 데이터센터를 순회하며 정규식으로 대상 VM을 찾음. 없는 VM은 경고만 남기고 건너뜀(PASS).
4. **비동기 적용**: VM마다 `Reconfigure` Task를 보내고, 전부 보낸 뒤 한꺼번에 `Wait`으로 완료를 기다림 (VM 개수가 많아도 순차 대기로 인한 지연을 최소화).
5. **재조회 검증**: Task가 성공한 VM만 골라 `config.extraConfig`를 다시 읽고, 우리가 보낸 값과 실제 값을 비교. 불일치가 있으면 "적용불일치"로 표시.
6. **결과 집계**: 성공/실패/스킵/적용불일치 건수를 출력하고, 문제가 하나라도 있으면 비정상 종료(exit 2).

## 수정 방법

### 1. affinity 값 자체를 바꾸고 싶을 때

코드 수정 불필요 — `-affinityFile01/02/03`으로 지정하는 텍스트 파일만 수정하면 된다.

### 2. 지원 VM 개수(ev01~ev03)를 늘리고 싶을 때

`maxVMCount` 상수와 `suffixes`/`flagNames`/`fileFlags` 슬라이스에 새 항목(예: `ev04`)을 추가해야 한다. 이 부분은 하드코딩되어 있어 코드 수정 + 재빌드가 필요하다.

### 3. AUTO 모드의 코어 매핑 규칙을 바꾸고 싶을 때

`buildExtraConfig` 함수의 AUTO 분기(HT ON/OFF 시 `sched.vcpuN.affinity` 값 생성 로직)를 수정한다.

### 4. 재조회 검증을 끄거나 완화하고 싶을 때

현재는 플래그로 끌 수 없고, 코드에서 "성공한 Task만 재조회 → key/value 비교" 로직(`main` 함수 하단)이 항상 실행된다. 필요 시 이 블록을 조건부로 만드는 플래그 추가가 필요하다.

## 테스트 방법 (vCenter 없이)

`govmomi/vcsim`(가짜 vCenter 시뮬레이터)을 이용하면 실제 vCenter 없이도 테스트할 수 있다.

```bash
go install github.com/vmware/govmomi/vcsim@latest
go install github.com/vmware/govmomi/govc@latest

./vcsim -l 127.0.0.1:8989 -dc 1 -cluster 1 -host 1 -vm 4
# 출력된 GOVC_URL(user:pass 계정)을 사용

export GOVC_URL='https://user:pass@127.0.0.1:8989/sdk' GOVC_INSECURE=1
./govc find / -type m         # 생성된 VM 목록 확인
./govc object.rename /DC0/vm/DC0_H0_VM0 192ev01   # 테스트하려는 이름으로 변경

echo "192" > worklist.txt
export VC_PASSWORD=pass
./vm_affinity_bulk -vcTargetIP 127.0.0.1:8989 -id user -vm_cnt 1 -affinityFile01 test_ev01.txt
```

`ExtraConfig`는 VMX 자유 형식 저장소라서 vcsim/실제 ESXi 모두 임의의 key를 그대로 저장한다 — "존재하지 않는 설정 키"를 넣어도 API 레벨에서 거부되지 않는다는 점을 테스트로 확인함(자세한 실패 메시지 표는 [README.md](./README.md) 참고).

## 제외한 항목

- 컴파일된 바이너리 — 소스에서 재빌드 가능하므로 저장소에 포함하지 않음 (`.gitignore`로 제외)
