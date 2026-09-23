# 13. lpage_search — Large Page 메모리 사이징 계산기

## 한눈에 보기

| 항목 | 값 |
|---|---|
| **위험도** | 🟢 **완전 안전.** vCenter/ESXi에 **접속조차 하지 않는** 순수 계산기 |
| 폴더 | `.claude/VM/lpage_search/` |
| 바이너리 | `lpage_search` |
| 하는 일 | ESXi 호스트 총 메모리와 ev01 할당량을 넣으면 **ev02에 안전하게 줄 수 있는 메모리 크기**를 계산 |
| 인증 | **불필요** |
| 대표 명령 | `./lpage_search -h esxi01 -v1 240 -hm 512 -vcpu1 32 -vcpu2 32` |

> **가장 먼저 빌드해볼 도구입니다.** 외부 의존성이 없어(Go 표준 라이브러리만) `vendor/` 폴더도 필요 없고, 30초면 빌드됩니다. Go 환경이 정상인지 확인하는 용도로도 좋습니다.

---

## 1. 이 도구는 무엇인가

ESXi는 메모리 여유가 충분할 때만 **Large Page(2MB)** 를 유지합니다. 여유가 줄면 Large Page를 잘게 쪼개버려서 성능이 떨어집니다 (개념은 [01_기초지식](./01_기초지식.md)의 "Large Page" 절 참고).

그래서 **VM에 메모리를 꽉 채워 할당하면 안 되고, 버퍼를 남겨야 합니다.** 이 버퍼를 계산해서 "ev02에 몇 GB까지 줘도 되나"를 알려주는 도구입니다.

---

## 2. 언제 쓰나

신규 서버 스펙을 정할 때, 또는 ev01 메모리를 바꿔서 ev02를 재조정해야 할 때 씁니다. **계산만 하고 설정은 바꾸지 않으므로**, 나온 값으로 vSphere Client나 다른 도구(`vm_create`, `vm-param-check`)에서 실제 설정을 해야 합니다.

---

## 3. 빌드

```bash
cd .claude/VM/lpage_search
bash setup.sh          # 내부: go build -o lpage_search main.go
```

빌드 없이 바로 실행해보려면:

```bash
go run main.go -h 192.168.10.50 -v1 240 -hm 512
```

Windows에서도 됩니다:

```powershell
cd .claude\VM\lpage_search
go build -o lpage_search.exe main.go
```

---

## 4. 사용 방법

```bash
./lpage_search -h <호스트명/IP> -v1 <ev01_메모리_GB> [-hm <호스트_총메모리_GB>] [-vcpu1 <ev01_vCPU수>] [-vcpu2 <ev02_vCPU수>]
```

예시:

```bash
./lpage_search -h esxi01 -v1 240 -hm 512 -vcpu1 32 -vcpu2 32
```

출력:

```
==================================================
[ESXi Large Page (2MB) Memory Sizing Tool]
Target Host           : esxi01 (512 GB)
Configured ev01       : 240 GB (245760 MB)
--------------------------------------------------
ESXi High-State Buffer: 35471 MB (3x minFree + Base)
ev01 VM Overhead      : 4998 MB
ev02 Est. Overhead    : 2698 MB
--------------------------------------------------
>> Recommended ev02 Size: 226 GB (231424 MB)
   (Total 2MB LPage Mappings: 115712 pages)
==================================================
```

**`>> Recommended ev02 Size`** 가 답입니다. 이 값을 ev02 VM의 메모리로 설정하면 됩니다.

---

## 5. 옵션 상세표

| 플래그 | 기본값 | 필수 | 설명 |
|---|---|---|---|
| `-h <값>` | (없음) | ✅ | 대상 ESXi 호스트명/IP. ⚠️ **계산에는 쓰이지 않고 출력용 라벨일 뿐입니다** — 이 값으로 호스트에 접속하지 않습니다 |
| `-v1 <GB>` | (없음) | ✅ | ev01 VM에 할당된(또는 할당 예정인) 메모리 크기(GB) |
| `-hm <GB>` | `512` | | 호스트 총 메모리(GB). ⚠️ **자동 조회되지 않으므로 반드시 직접 입력**해야 정확합니다 |
| `-vcpu1 <N>` | `32` | | ev01 vCPU 수 (오버헤드 계산용) |
| `-vcpu2 <N>` | `32` | | ev02 vCPU 수 (오버헤드 계산용) |

> **가장 흔한 실수**: `-hm`을 생략하면 무조건 512GB로 계산합니다. 실제 호스트가 1TB면 결과가 완전히 틀립니다. **항상 `-hm`을 명시하세요.**

---

## 6. 계산 로직

이해하고 쓰면 결과를 의심할 수 있습니다.

### (1) ESXi High-State 버퍼 (`calculateHostReservedMB`)

```
버퍼 = (호스트 총 메모리 × 2%) × 3  +  4096MB
        └─ minFree ─┘        └─ High 상태 조건 ─┘   └ 시스템 기본 상주 영역
```

ESXi는 여유 메모리가 `minFree`의 **3배 이상(High state)** 일 때만 Large Page(2MB)를 유지합니다. 이 버퍼를 확보해야 Large Page가 깨지지 않습니다.

### (2) VM 오버헤드 (`calculateVmOverheadMB`)

```
오버헤드 = 할당 메모리 × 1.2%  +  vCPU 수 × 64MB
```

커널/MMU/스케줄링 오버헤드 추정치입니다.

### (3) ev02 가용량

```
ev02 = 호스트 총 메모리
       − High-State 버퍼
       − (ev01 메모리 + ev01 오버헤드)
       − ev02 자체 오버헤드
```

최종적으로 **짝수 GB 단위로 내림 정렬**합니다 (2MB Large Page 정렬).

---

## 7. 주의사항과 한계

- **순수 로컬 계산기입니다.** vCenter/ESXi API를 조회하지 않습니다. `-h`, `-hm` 값은 사용자가 직접 정확히 입력해야 하며, **틀린 값을 넣으면 결과도 그대로 틀어집니다.**
- 오버헤드 추정 계수(1.2%, vCPU당 64MB, minFree 2%)는 **경험적 근사치**입니다. ESXi 버전/워크로드에 따라 실제와 차이가 날 수 있습니다.
- 산출된 값은 **시작점으로만** 쓰고, 실제 적용 전 vSphere Client에서 실제 `minFree`/여유 메모리 상태를 함께 확인하는 것을 권장합니다.
- 이 도구는 값을 계산만 할 뿐 **설정을 바꾸지 않습니다.** 산출된 크기로 실제 설정을 변경한 뒤에는 랜덤한 서버 몇 대를 확인해서 반영되었는지 확인하세요.

---

## 8. 파일 구조

```
lpage_search/
├── README.md   # 1차 자료
├── main.go     # 계산 로직 + CLI 옵션 파싱을 담은 단일 파일 프로그램
├── go.mod      # Go 모듈 정의 (외부 의존성 없음)
└── setup.sh    # go build로 빌드
```

**단일 파일 프로그램**이라 수정이 가장 쉽습니다. 계수를 바꾸고 싶으면 `main.go`의 `calculateHostReservedMB` / `calculateVmOverheadMB` 함수만 보면 됩니다.

---

## 9. 관련 문서

- 1차 자료: `lpage_search/README.md`
- Large Page 설정을 실제로 적용하려면: [10. VM_setup](./10_VM_setup.md)의 `lpage_setting-source`
- 설정이 맞는지 점검하려면: [11. vm-param-check](./11_vm-param-check-usability-improvement.md)
