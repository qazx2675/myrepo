# ESXi Large Page(2MB) 메모리 사이징 계산기 (lpage_search)

ESXi 호스트의 총 메모리, ev01 VM에 이미 할당된 메모리, vCPU 수를 입력하면 ev02 VM에 안전하게 할당 가능한 메모리(2MB Large Page 정렬, ESXi High-State 유지 버퍼 포함) 크기를 계산해 주는 단일 파일 Go 프로그램이다. vCenter/ESXi에 실제로 접속하지 않는 **순수 계산 도구**이며, 외부 의존성이 없어 Go 표준 라이브러리만으로 오프라인 빌드가 가능하다.

⚠️ **주의사항 (Disclaimer)**
본 계산 결과는 참고용(보조 도구)이며 100% 신뢰하기보다는 실제 설정값과 대조하는 용도로 사용하는 것을 권장합니다. 이 도구는 값을 계산만 할 뿐 실제 설정을 변경하지 않으므로, 산출된 ev02 메모리 크기로 실제 설정 변경(vSphere Client 또는 별도 설정 도구)을 수행한 뒤에는 랜덤한 서버 몇 대를 확인해서 실제로 반영되었는지 확인하는 절차가 반드시 필요합니다.

## 1. 빌드 및 설치 방법

외부 의존성이 없으므로(Go 표준 라이브러리 `flag`/`fmt`/`math`/`os`만 사용) `go.mod`만 있으면 인터넷 연결 없이 바로 빌드할 수 있다. `vendor/` 디렉토리가 필요 없다.

```bash
cd ".claude/VM/lpage_search"
bash setup.sh
```

`setup.sh`는 내부적으로 `go build -o lpage_search main.go`를 실행한다. 직접 빌드하려면:
```bash
go build -o lpage_search main.go
```

Windows에서는:
```powershell
cd ".claude\VM\lpage_search"
go build -o lpage_search.exe main.go
```

빌드 없이 바로 실행해보려면:
```bash
go run main.go -h 192.168.10.50 -v1 240 -hm 512
```

### 전역 명령어로 사용하기 (선택 사항)
빌드된 실행 파일을 PATH 환경 변수에 포함된 디렉터리로 이동하거나, 실행 파일이 있는 경로를 PATH에 추가하면 어디서든 명령어처럼 사용할 수 있습니다.

예시 (실행 파일을 `/usr/local/bin`으로 복사):
```bash
sudo cp lpage_search /usr/local/bin/
# 이후 어느 위치에서나 명령어처럼 실행 가능
```

## 2. 사용 방법

```bash
./lpage_search -h <호스트명/IP> -v1 <ev01_메모리_GB> [-hm <호스트_총메모리_GB>] [-vcpu1 <ev01_vCPU수>] [-vcpu2 <ev02_vCPU수>]
```

예시:
```bash
./lpage_search -h esxi01 -v1 240 -hm 512 -vcpu1 32 -vcpu2 32
```

출력 예시:
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

## 3. 옵션별 상세 설명

| 플래그 | 기본값 | 설명 |
|---|---|---|
| `-h` | (필수) | 대상 ESXi 호스트명/IP (계산에는 사용되지 않고 출력용 라벨로만 쓰인다) |
| `-v1` | (필수) | ev01 VM에 할당된(또는 할당 예정인) 메모리 크기(GB) |
| `-hm` | `512` | 호스트 총 메모리(GB). `-h`에 실제 호스트를 지정해도 이 값은 자동 조회되지 않으므로 반드시 직접 입력해야 한다 |
| `-vcpu1` | `32` | ev01 vCPU 수 (오버헤드 계산용) |
| `-vcpu2` | `32` | ev02 vCPU 수 (오버헤드 계산용) |

## 4. 문서별 고유 설명

### 4.1 계산 로직 개요
1. **ESXi High-State 버퍼** (`calculateHostReservedMB`): 호스트 총 메모리의 2%(`minFree`)의 3배 + 시스템 기본 상주 영역(4096MB)을 예약분으로 뺀다. ESXi는 여유 메모리가 `minFree`의 3배 이상(High state)일 때만 Large Page(2MB)를 유지하므로, 이 버퍼를 확보해야 Large Page가 깨지지 않는다.
2. **VM 오버헤드** (`calculateVmOverheadMB`): 할당 메모리의 1.2% + vCPU당 64MB로 커널/MMU/스케줄링 오버헤드를 추정한다.
3. **ev02 가용량 산출**: `호스트 총 메모리 - High-State 버퍼 - (ev01 메모리 + ev01 오버헤드) - ev02 자체 오버헤드`를 계산하고, 최종적으로 짝수 GB 단위로 내림 정렬한다.

### 4.2 디렉토리 구조

```
lpage_search/
├── README.md   # 이 문서
├── main.go     # 계산 로직 + CLI 옵션 파싱을 담은 단일 파일 프로그램
├── go.mod      # Go 모듈 정의 파일 (외부 의존성 없음)
└── setup.sh    # go build로 빌드하는 스크립트
```

### 4.3 알려진 한계
- vCenter/ESXi API를 조회하지 않는 **순수 로컬 계산기**다. `-h`, `-hm` 값은 사용자가 직접 정확히 입력해야 하며, 틀린 값을 넣으면 결과도 그대로 틀어진다.
- 오버헤드 추정 계수(1.2%, vCPU당 64MB, minFree 2%)는 경험적 근사치이며 ESXi 버전/워크로드에 따라 실제 값과 차이가 날 수 있다. 산출된 값은 시작점으로만 사용하고, 실제 적용 전 vSphere Client의 실제 minFree/여유 메모리 상태를 함께 확인할 것을 권장한다.
