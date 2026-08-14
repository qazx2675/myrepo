# vm_affinity_bulk

worklist(호스트 목록) 기반으로 각 호스트의 `ev01`~`ev03` VM에 CPU affinity 설정(`sched.vcpuN.affinity` 등 ExtraConfig 항목)을 비동기(Task) 방식으로 일괄 적용하는 govmomi 기반 도구.

## 빌드 (다운로드 후 처음 할 일 — 오프라인 가능)

이 디렉토리에는 의존성(`govmomi`, `google/uuid`)을 미리 받아둔 **`vendor/` 디렉토리가 포함**되어 있다. 압축을 풀거나 clone한 직후 **인터넷 연결 없이 바로 빌드**할 수 있다.

```bash
cd ".claude/VM설정 go lang"
go build -mod=vendor -o vm_affinity_bulk .
```

- `-mod=vendor`를 꼭 붙여야 `vendor/` 안의 의존성을 사용한다. 빠뜨리면 Go가 인터넷에서 재다운로드를 시도할 수 있다.
- `go.mod`를 수정(의존성 버전 변경 등)했다면, 인터넷이 되는 환경에서 `go mod vendor`를 다시 실행해 `vendor/`를 갱신한 뒤 커밋해야 한다.
- 파일은 `main.go` 하나뿐이라 `go build .`(또는 `-mod=vendor` 버전) 한 번이면 전체가 빌드된다.

## 사용법

```bash
export VC_PASSWORD='실제_비밀번호'

# ev01만
./vm_affinity_bulk -vcTargetIP 192.168.0.50 -vm_cnt 1 \
  -affinityFile01 affinity_ev01.txt

# ev01~ev03 전체
./vm_affinity_bulk -vcTargetIP 192.168.0.50 -vm_cnt 3 \
  -id administrator@vsphere.local \
  -affinityFile01 affinity_ev01.txt \
  -affinityFile02 affinity_ev02.txt \
  -affinityFile03 /etc/vmcfg/affinity_ev03.txt
```

### 옵션

| 플래그 | 기본값 | 설명 |
|---|---|---|
| `-id` | `lscsystems@vsphere.local` | vCenter 로그인 계정 |
| `-vcTargetIP` | (필수) | vCenter 접속 IP |
| `-worklistFile` | `worklist.txt` | 대상 호스트 목록 (한 줄에 호스트 하나) |
| `-vm_cnt` | `2` | 호스트당 대상 VM 개수 (1=ev01, 2=ev01~ev02, 3=ev01~ev03) |
| `-affinityFile01/02/03` | (vm_cnt에 따라 필수) | 각 ev0N 슬롯에 적용할 affinity 설정 파일 |
| `-affinityFile` | (없음) | [구버전 호환] `-affinityFile02` 미지정 시 이 값을 ev02 설정으로 사용 |
| `-ht` | `ON` | 설정 파일 내용이 `AUTO`일 때만 사용. `sched.vcpuN.affinity` 값을 HT ON은 `N*2,N*2+1`, OFF는 `N` 형태로 생성 |

환경 변수 `VC_PASSWORD`가 반드시 설정되어 있어야 한다 (없으면 즉시 종료).

### 설정 파일 형식

- `key=value` 한 줄씩. 예: `sched.vcpu0.affinity=0,1`
- 값(과 키)을 큰따옴표(`"`) 또는 작은따옴표(`'`)로 감싸도 자동으로 제거된 뒤 적용된다 (`stripQuotes`).
  예: `sched.vcpu0.affinity="0,1"` → 실제 적용값은 `0,1` (따옴표 제거됨)
- 파일 전체가 `AUTO` 한 줄뿐이면, vCPU 수를 조회해서 `-ht` 옵션에 따라 1:1 매핑을 자동 생성한다.
- `#`으로 시작하는 줄은 주석으로 무시된다.
- `key=value` 형식이 아니거나 key가 비어 있으면 프로그램 시작 시점에 즉시 오류로 종료된다 (아래 "실패 메시지" 참고).

## 동작 방식

1. `-affinityFile0N` 파일들을 파싱(`loadAffinitySpec`) — 형식 오류 시 여기서 즉시 종료
2. `worklistFile`의 각 호스트에 `ev01`~`ev0N` 접미사를 붙여 대상 VM 이름 목록 생성
3. vCenter 전체 데이터센터를 순회하며 이름이 일치하는 VM을 찾음 (없으면 "PASS"로 건너뜀)
4. `Reconfigure` Task를 VM마다 비동기로 전송, 전부 전송한 뒤 `Wait`으로 완료 대기
5. Task가 성공한 VM만 `config.extraConfig`를 재조회해서 실제로 보낸 값과 일치하는지 검증
6. 최종 결과를 "성공/실패/스킵/적용불일치" 건수로 요약 출력, 문제가 있으면 종료 코드 2

## 실패 시 나오는 메시지 (원인별)

| 상황 | 메시지 예 | 발생 시점 |
|---|---|---|
| affinity 설정 파일이 없음 | `ev01 파일을 찾을 수 없습니다: <경로>` | 시작 직후 (vCenter 접속 전) |
| 파일이 `key=value` 형식이 아님 | `ev01 설정 파일 오류 (<경로>): N번째 줄 형식 오류 (key=value 아님): <내용>` | 시작 직후 |
| key가 비어 있음 | `ev01 설정 파일 오류 (<경로>): N번째 줄 key 가 비어 있습니다: <내용>` | 시작 직후 |
| VC_PASSWORD 미설정 | `인증 정보 로드 실패: VC_PASSWORD 환경 변수가 설정되지 않았습니다.` | 시작 직후 |
| vCenter 접속 실패 | `vCenter 접속 실패: <상세>` | 접속 시도 시 |
| worklist와 매칭되는 VM이 vCenter에 하나도 없음 | `worklist 와 매칭되는 VM 을 vCenter 에서 찾지 못했습니다.` | VM 조회 후 |
| 특정 VM이 vCenter에 없음 | `[호스트명ev0N] 경고: 대상 VM 이 존재하지 않습니다. (PASS)` | VM별 처리 중 (해당 VM만 스킵, 전체는 계속 진행) |
| Reconfigure 명령 자체가 거부됨 | `[VM명] Reconfigure 명령 전송 실패: <상세>` | Task 전송 시 |
| Task는 전송됐지만 vCenter에서 실패 처리됨 | `[VM명] 작업 실패: <상세>` | Task 완료 대기 중 |
| 실제 반영값이 보낸 값과 다름 | `[VM명] 실제 적용 불일치: key(기대=X,실제=Y)` | 재조회 검증 시 |

`ExtraConfig`는 VMX의 자유 형식 key-value 저장소라서, **존재하지 않는/의미 없는 키를 넣어도 vCenter/ESXi 단에서 형식 자체를 거부하지는 않는다.** 즉 "없는 설정"을 넣었을 때 나는 실패는 대부분 위 표의 **① 파일 파싱 단계(형식 오류)** 이거나, **② 재조회 검증 단계(값 불일치)** 이지, "그런 설정 키는 없다"는 형태의 vCenter API 에러가 아니다. 실제로 잘못 입력된 설정을 잡아내려면 위 검증 로직(Wait 이후 재조회 비교)이 핵심이다.

## 참고: 실제 적용 여부 검증 로직

Task가 성공(`Wait` 정상 반환)했다고 해서 값이 실제로 반영됐다고 단정하지 않고, `config.extraConfig`를 다시 읽어서 우리가 보낸 key/value와 실제 값을 하나씩 비교한다. 하나라도 다르면 "실제 적용 불일치"로 표시되고 전체 종료 코드가 2가 된다.
