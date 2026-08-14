# ESXi VM Large Page(2MB) 설정 가이드 및 조사 결과

## 1. 배경 및 문제 현상
VM 메모리 크기를 2MB 배수로 정확히 맞춰도, vsish의 2MB page mappings 값이 이론상 최대치(511)로 나오지 않는 현상 확인.
호스트의 free memory 여유량이 핵심 변수임을 실측으로 확인.

## 2. Large Page 적용 규칙
ESXi는 호스트 free memory 상태를 minFree 대비 비율로 5단계 관리:

| 상태 | 기준(minFree 대비) |
|---|---|
| High | 300% 이상 |
| Clear | 100~300% |
| Soft | 64% |
| Hard | 32% |
| Low | 16% |

- Large page는 보장이 아닌 best-effort 최적화이며, free memory가 High(minFree x 3) 이상을 유지해야 선제적으로 4KB로 깨지지 않음
- High 밑으로 떨어지면(Clear 진입) large page가 선제적으로 분해되어 2MB 매핑 수가 급격히 감소 (실측: 511 -> 13)
- 항상 4KB로 남는 구조적 영역 존재: 0~1MB legacy hole, 3~4GB PCI hole, MMIO/framebuffer 등
- 계산 공식(근사): 할당 예산(GB) = free(GB) - minFree(GB) x 3; VM 최대 크기 ≈ 할당 예산 - VM overhead

## 3. 조사 방법
- vsish: /memory/lpage/vmLPage/[VM ID] -> "Current number of 2MB page mappings"
- esxtop 배치모드(-b -n 1)로 PMEM/VMKMEM 상태(managed, minfree, free, memory state) 확인
- MB 입력은 1024 배수(이진 단위) 기준으로 정확히 계산 필요
- 성공/실패 경계를 이진탐색으로 좁혀 실제 임계값 확인 권장

## 4. Go 기반 자동화 도구 (lpage_calc.go)
- -h: 대상 ESXi 호스트 지정
- 단일 VM 모드: 호스트 상태 기반 안전한 메모리 크기 3단계(공격적/권장/보수적) 추천
- -v1: VM1 메모리 고정 지정 시 남은 예산으로 VM2 적정값 자동 계산
- -verify: 실행 중인 VM의 실제 2MB/1GB page mapping 값을 vsish로 조회하여 511 달성 여부 확인
- 자동 수집(esxtop -> memstats -> vsish) 실패 시 -managed/-minfree/-free 수동 입력 가능

사용 예:
```
go build -o lpagecalc
./lpagecalc -h esxi01 -p '패스워드'
./lpagecalc -h esxi01 -p '패스워드' -v1 1000
./lpagecalc -h esxi01 -p '패스워드' -verify
```
운영 환경에는 "권장" 또는 "보수적" 값 사용 권장. 적용 후 -verify로 실측 재확인 필요.
