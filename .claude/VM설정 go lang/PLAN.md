# VM 설정 일괄 적용 도구 - 계획서

## 목적

worklist에 등록된 다수 호스트(각각 `ev01`~`ev03` VM을 가짐)에 대해 두 종류의 설정을 **병렬로, 안전하게**(워커풀 + 완료 대기 + 필요 시 재조회 검증) 적용한다.

1. **`vm_affinity_bulk`**(main_affinity.go) — CPU affinity(ExtraConfig)
2. **`vm_lpage_bulk`**(main_lpage.go) — HugePage/메모리 고정 + CPU 토폴로지(소켓당 코어, NUMA)

## 배경

- 수십~수백 대 VM에 설정을 하나씩 수동으로 적용하는 것은 비효율적이고 실수하기 쉽다.
- vCenter `Reconfigure` API는 비동기(Task)이므로, Task 전송 성공 ≠ 실제 반영 성공이다. (affinity 도구는) 반영 여부를 재확인하는 절차가 필요하다.
- **규모 문제**: 호스트 700~800대 × VM 2대 = 1,400~1,600대 규모에서, "전송을 순차적으로 다 하고 → Wait도 순차적으로 하나씩" 하는 구조는 API 왕복이 VM 수만큼 누적되어(약 3,000회 순차 왕복) 수 분~십수 분까지 걸릴 수 있음이 실측(예상)으로 확인됨. → **워커풀 기반 병렬 처리로 전면 재작성**.

## 변경 이력

| 버전 | 방식 |
|---|---|
| v1 | 순차: Reconfigure를 모두 전송(응답만 받고 안 기다림) → 전송이 끝난 뒤 Task.Wait()을 하나씩 순서대로 대기 |
| **v2 (현재)** | **병렬 워커풀**: `-concurrency`(기본 **20**) 개의 고루틴이 각자 담당 VM의 "Reconfigure 전송 + Wait 완료"를 통째로 병렬 수행. VM 목록 조회(데이터센터 순회)도 동일한 동시성 제한으로 병렬화 |

v1→v2로 바뀌면서 코드 흐름은 "먼저 다 보내고 나중에 다 기다림"에서 "워커 하나가 한 VM을 처음부터 끝까지 책임지고, 여러 워커가 동시에 돈다"로 바뀌었다. 이 방식이 대규모(1,000+ VM)에서 훨씬 유리한 이유:
- 순차 방식은 "전송 N번 + 대기 N번"이 모두 직렬로 쌓이지만, 병렬 방식은 `-concurrency`개씩 묶어서 처리하므로 총 소요 시간이 `N / concurrency`에 비례해 줄어든다.
- 재조회 검증(성공한 VM 전체를 한 번에 배치 조회)은 원래도 이미 단일 배치 호출이라 병렬화 대상이 아니었고, 그대로 유지했다.

## 디렉토리 구조상 주의점

`main_affinity.go`와 `main_lpage.go`는 **같은 디렉토리에 있지만 서로 독립적인 단일 파일 프로그램**이다. 둘 다 `package main`이며 동일한 이름의 헬퍼(`vmJob`, `printMu`, `safePrintf`, `readLines`, `defaultConcurrency` 등)를 각자 따로 정의하고 있다.

- **의도된 설계**: 사용자가 "같은 디렉토리에 파일명만 다르게 업로드"를 요청했기 때문에 이렇게 구성함.
- **제약**: 이 때문에 `go build .`(디렉토리 전체 빌드)는 "redeclared" 컴파일 오류가 난다. 반드시 `go build main_affinity.go` / `go build main_lpage.go`처럼 **파일명을 직접 지정**해서 빌드해야 한다 (README.md에 명시함).
- 두 프로그램을 진짜로 하나의 Go 모듈처럼 공유 코드/서브패키지 구조로 정리하려면 `cmd/vm_affinity_bulk/main.go`, `cmd/vm_lpage_bulk/main.go` 식으로 하위 디렉토리를 분리하는 리팩터링이 필요하지만, 현재는 요청대로 평면 구조를 유지했다.

## 테스트 방법 (vCenter 없이, vcsim)

`govmomi/vcsim`(가짜 vCenter 시뮬레이터)으로 실제 vCenter 없이 병렬 동작을 검증했다.

```bash
go install github.com/vmware/govmomi/vcsim@latest
go install github.com/vmware/govmomi/govc@latest

./vcsim -l 127.0.0.1:8989 -dc 1 -cluster 1 -host 1 -vm 15
export GOVC_URL='https://user:pass@127.0.0.1:8989/sdk' GOVC_INSECURE=1

# VM 이름을 host01ev01, host01ev02 ... 형태로 변경
./govc object.rename /DC0/vm/DC0_C0_RP0_VM0 host01ev01
...

echo -e "host01\nhost02\nhost03\nhost04\nhost05" > worklist.txt
export VC_PASSWORD=pass
./vm_affinity_bulk -vcTargetIP 127.0.0.1:8989 -id user -vm_cnt 2 \
  -affinityFile01 affinity_ev01.txt -affinityFile02 affinity_ev02.txt -concurrency 3
```

### 검증한 항목

1. **race detector** (`go build -race`)로 빌드해서 10대 VM(호스트 5대 × ev01/ev02), concurrency=3으로 실행 — **데이터 레이스 없음** 확인.
2. **교차 오염(고루틴 클로저 캡처 버그) 없음** — ev01/ev02에 서로 다른 값(affinity 도구: 일반값 vs 따옴표 포함 값 / lpage 도구: 8코어 vs 4코어)을 넣고, `govc`로 vCenter에 실제 저장된 값을 VM별로 직접 대조하여 전부 정확히 분리 적용됨을 확인.
3. 기본값(`-concurrency` 미지정 시 20) 정상 동작 확인.
4. 기존 실패 시나리오(설정 파일 형식 오류, 코어/소켓/NUMA 나누어떨어지지 않음)가 병렬화 이후에도 동일하게 vCenter 접속 전 즉시 실패로 동작함을 회귀 테스트로 확인 (exit code 유지).
5. `ExtraConfig`는 자유 형식 저장소라 존재하지 않는 키를 넣어도 API 레벨에서 거부되지 않는다는 점은 v1과 동일하게 유지됨 (자세한 내용은 README.md 참고).

## numa.vcpu.maxPerVirtualNode 관련 참고

`main_lpage.go`의 `numa.vcpu.maxPerVirtualNode` ExtraConfig 값은 **NUMA 노드당 코어 수가 아니라 소켓당 코어 수**를 사용한다. 코드 리뷰 중 이 부분이 처음에는 버그로 의심되었으나, **의도된 설정값**임을 확인함 (사용자 확인, 2026-08-15). 향후 코드를 다시 보는 사람이 "왜 NUMA 키에 소켓 값이 들어가지?"라고 헷갈리지 않도록 README.md와 코드 주석에 명시해 두었다.

## 제외한 항목

- 컴파일된 바이너리 — 소스에서 재빌드 가능하므로 저장소에 포함하지 않음 (`.gitignore`로 제외)
