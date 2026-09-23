# numa_preferht_setting

`-f`로 지정한 VM 이름 목록에 `numa.vcpu.preferHT=TRUE`를 병렬(워커풀)로 일괄 적용하는 독립 도구입니다. 모든 VM에 똑같이 적용되는 단일 설정값이므로 ev01/ev02/ev03 같은 그룹 구분이 없습니다.

⚠️ **주의사항 (Disclaimer)**
본 로그 분석 관련 스크립트 및 툴은 100% 신뢰하기보다는 참고용(보조 도구)으로 사용하는 것을 권장합니다. 설정 변경 스크립트의 경우, 설정 변경 후 무작위로 서버 몇 대를 골라 실제로 변경되었는지 직접 확인하는 절차가 반드시 필요합니다.

## 1. 빌드 방법 (다운로드 후 처음 할 일 — 오프라인 가능)

`setup.sh`가 V2 공유 의존성(`../../govendor/govmomi-0.55.1-standard`)을 `vendor`로 링크하므로, V2 폴더만 있으면 인터넷 연결 없이 바로 빌드할 수 있습니다.

```bash
bash setup.sh
# 또는 직접: go build -mod=vendor -o numa_preferht_setting main.go
```

## 2. 사용 방법

```bash
export VC_PASSWORD='실제_비밀번호'

# 대상 VM 이름을 한 줄에 하나씩 적은 파일
printf '192ev01\n192ev02\n' > targets.txt

./numa_preferht_setting -vc 192.168.0.50 -f targets.txt
```

적용 조건:

- **전원이 꺼진(PoweredOff) VM에만 적용됩니다.** 켜져 있는 VM은 건너뛰고 사유를 출력합니다(`[VM명] 전원 ON 상태 — 스킵 (PASS)`).
- `-f` 목록에는 있지만 vCenter에서 찾지 못한 VM은 경고만 남기고 계속 진행합니다.

## 3. 옵션 상세 설명

| 플래그 | 기본값 | 설명 |
|---|---|---|
| `-vc <IP[:포트]>` | (필수) | vCenter 접속 주소 |
| `-id <계정>` | `lscsystems@vsphere.local` | vCenter 로그인 계정 |
| `-f <path>` | (필수) | 대상 VM 이름 목록 파일 (한 줄에 하나, `#` 주석 가능) |
| `-concurrency <N>` | `20` | 동시 처리 개수 제한 — VM 목록 조회(데이터센터별 병렬)와 Reconfigure 전송+완료 대기(워커풀) 두 구간 모두에 적용 |

환경변수 `VC_PASSWORD`가 반드시 설정되어 있어야 합니다(없으면 즉시 종료).

## 4. 동작 순서 (전 과정 병렬)

1. `-f` 파일의 VM 이름으로 정규식을 만들고, 모든 데이터센터를 **동시에** 순회하며 대상 VM을 찾습니다(데이터센터별 고루틴, `-concurrency` 제한).
2. 찾은 VM 전체의 전원 상태를 **단일 배치 조회**로 한 번에 확인합니다.
3. 존재하지 않는 VM과 전원이 켜진 VM은 미리 건너뛰고, 나머지만 작업 목록으로 구성합니다.
4. 워커풀(`-concurrency`개 동시)이 VM마다 `Reconfigure` 전송부터 `Wait` 완료까지 한 워커 안에서 처리합니다.
5. Task가 성공한 VM만 `config.extraConfig`를 **단일 배치로 다시 조회**해 실제로 `TRUE`가 반영됐는지 검증합니다.
6. 최종 결과를 "성공/실패/스킵/적용불일치" 건수로 요약해 출력하고, 문제가 있으면 종료 코드 2로 끝납니다.

## 5. 테스트 방법 (vCenter 없이)

`govmomi/vcsim`으로 대신 검증할 수 있습니다.

```bash
go install github.com/vmware/govmomi/vcsim@latest
go install github.com/vmware/govmomi/govc@latest

./vcsim -l 127.0.0.1:8989
export GOVC_URL='https://user:pass@127.0.0.1:8989/sdk' GOVC_INSECURE=1
./govc find / -type m                                   # 생성된 VM 이름 확인
./govc object.rename /DC0/vm/DC0_H0_VM0 testvm01         # 필요하면 테스트용 이름으로 변경

echo testvm01 > targets.txt
export VC_PASSWORD=pass
./numa_preferht_setting -vc 127.0.0.1:8989 -id user -f targets.txt
```

실측 검증(vc-test-env의 vcsim, VM 2대 기준)에서 다음 세 가지를 모두 확인했습니다.

1. 전원 OFF + 키 없음 → 적용 후 재조회로 `TRUE` 확인
2. 전원 ON → 스킵
3. 목록에는 있지만 vCenter에 없는 이름 → 경고 후 계속 진행

## 6. vm-param-check와의 관계

이 도구가 적용한 `numa.vcpu.preferHT`는 [`vm-param-check-usability-improvement/vm-param-check`](../../vm-param-check-usability-improvement/vm-param-check/)의 `-preferHT` 플래그로 체크할 수 있습니다(자세한 내용은 그 도구의 `CHANGELOG.md` 2026-08-21 항목 참고). 이 도구는 체크와 관계없이 값을 **일괄 적용**만 하는 독립 스크립트입니다.
