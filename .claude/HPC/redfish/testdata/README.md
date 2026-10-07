# testdata — mock BMC 용 합성 Redfish 트리

**이 디렉터리의 데이터는 공개 문서를 바탕으로 사람이 만든 합성 자료이며, 실제 BMC 덤프가 아닙니다. 속성명·허용값·경로·BIOS 버전 문자열은 실제 장비와 다를 수 있으므로, 첫 실장비 덤프(`xml.sh`)로 반드시 대체·확정해야 합니다.** 모든 JSON 에는 `"Oem": {"MockData": true}` 표식이 들어 있고, 테스트가 이를 강제합니다 (표식이 없는 파일이 섞이면 실패).

실제 BMC 에는 접속하지 않습니다. 이 트리는 `internal/mockbmc` 서버(`mockbmc.sh`, Go 테스트)가 서빙하며, 이후 단계(덤프·점검·설정·all_bios_check·바이너리 e2e)를 실장비 없이 검증하는 데 씁니다.

## 파일 규칙 (덤프 디렉터리 레이아웃과 동일)

요청 경로 뒤에 `.json` 을 붙인 것이 파일입니다. 조회는 **대소문자와 끝 슬래시를 무시**합니다.

| 요청 경로 | 파일 |
|---|---|
| `/redfish/v1` | `<트리>/redfish/v1.json` |
| `/redfish/v1/Systems/1/Bios` | `<트리>/redfish/v1/Systems/1/Bios.json` |
| `/redfish/v1/Systems/1/Bios/Settings` | `<트리>/redfish/v1/Systems/1/Bios/Settings.json` |
| `/redfish/v1/Registries/X.v1_0_0.json` (URI 가 `.json` 으로 끝남) | `<트리>/redfish/v1/Registries/X.v1_0_0.json.json` |
| HPE 의 `/redfish/v1/systems/1/bios/settings/` | `hpe-*/redfish/v1/systems/1/bios/settings.json` (소문자 경로·끝 슬래시 그대로 저장, 대문자 경로로도 조회됨) |

`xml.sh`(dump)가 저장하는 덤프 디렉터리도 같은 규칙이므로, 실제 덤프를 이 디렉터리 옆에 두고 `mockbmc.sh` 로 그대로 재생할 수 있습니다. `/redfish/v1` 밖의 파일과 `.json` 이 아닌 파일은 무시합니다.

## 트리 6종

| 트리 | 벤더/모델 (합성) | 시스템 ID / 관리자 ID | Settings 경로 | 특징 |
|---|---|---|---|---|
| `dell-r660` | Dell / PowerEdge R660 (iDRAC9 흉내) | `System.Embedded.1` / `iDRAC.Embedded.1` | `Bios/Settings` | `Managers/<id>/Jobs` 있음 → Job POST 가능. 레지스트리 URI 가 `.json` 으로 끝남. `Dependencies` 예시 포함 |
| `hpe-dl360gen11` | HPE / ProLiant DL360 Gen11 (iLO6 흉내) | `1` / `1` | `bios/settings` | 소문자 경로 + 끝 슬래시 (`/redfish/v1/systems/1/`), 일부 값에 `/`, `-` 포함 (`I/OThroughput`) |
| `hpe-dl360gen10` | HPE / ProLiant DL360 Gen10 (iLO5 흉내) | `1` / `1` | `bios/settings` | gen11 과 **의도적으로 다름**: BIOS 버전 다름, `LlcPrefetch` 속성 **없음**(→ 표준 항목 4개 중 3개만), `TpmState`/`SecureBootStatus` 없음, `AdvancedMemProtection` 추가, `WorkloadProfile`·`PowerRegulator`·`MinProcIdlePower`·`EnergyPerfBias` 현재값 다름 |
| `lenovo-sr650v3` | Lenovo / ThinkSystem SR650 V3 (XCC 흉내) | `1` / `1` | **`Bios/Pending`** | 값이 `Enable`/`Disable` (Dell·HPE 는 `Enabled`/`Disabled`), Integer 속성 1개 |
| `cisco-c220m7` | Cisco / UCSC-C220-M7S (CIMC 단독 흉내) | `MOCK-C220M7` / `CIMC` | `Bios/Settings` | 시스템 ID 가 `1` 이 아님, 값이 소문자 `enabled`/`disabled` |
| `supermicro-x12` | Supermicro / X12DPi-N6 | `1` / `1` | **없음 (쓰기 미지원)** | `Bios/Settings` 파일 없음, `@Redfish.Settings` 없음 → PATCH 는 405. 속성명에 `[`, `]`, `-` 포함 (`Hyper-Threading[ALL]`) |

각 트리에는 ServiceRoot, SessionService(+Sessions), Systems(Manufacturer/Model/SKU/SerialNumber/BiosVersion), Managers, Bios, Bios Settings/Pending(Supermicro 제외), 속성 레지스트리(허용값·ReadOnly), 그리고 **LogServices 경로**(Systems·Managers 하위, Entries 포함)가 있습니다. LogServices 는 정상 클라이언트가 절대 읽지 않아야 하므로, mock 이 접근을 `LogHits` 로 세어 "0 이어야 정상" 을 시험하는 용도입니다.

### 표준 4개 항목의 속성명 (합성)

| 트리 | system_profile | hyper_threading | llc_prefetch | sub_numa_cluster |
|---|---|---|---|---|
| dell-r660 | `SysProfile` | `LogicalProc` | `LlcPrefetch` | `SubNumaCluster` |
| hpe-dl360gen11 | `WorkloadProfile` | `ProcHyperthreading` | `LlcPrefetch` | `SubNumaClustering` |
| hpe-dl360gen10 | `WorkloadProfile` | `ProcHyperthreading` | *(없음)* | `SubNumaClustering` |
| lenovo-sr650v3 | `OperatingModes_ChooseOperatingMode` | `Processors_HyperThreading` | `Processors_LLCPrefetch` | `Processors_SNC` |
| cisco-c220m7 | `CPUPerformance` | `IntelHyperThread` | `LLCPrefetch` | `SNC` |
| supermicro-x12 | `PowerTechnology` | `Hyper-Threading[ALL]` | `LLCPrefetch` | `SNC` |

호스트마다 달라야 하는 값(비교 제외 대상)은 `ServiceTag`/`AssetTag`/`SerialNumber`/`ServerName`/`BiosBootSeq` 계열입니다. 기본 현재값은 모두 "표준을 만족하는 쪽"(Dell `PerfOptimized`/`Enabled`/`Enabled`/`Disabled` 등)이며, FAIL·차이 시나리오는 테스트에서 `Server.SetBiosAttr` / `Server.Modify` 로 만듭니다.

## 확신 정도

| 구분 | 내용 |
|---|---|
| 계획서(엑셀 표)에서 가져온 이름 | Dell `SysProfile`(`PerfOptimized`), `LogicalProc`, `LlcPrefetch`, `SubNumaCluster`, `ServiceTag`. HPE `WorkloadProfile`, `ProcHyperthreading`, `LlcPrefetch`, `SubNumaClustering` |
| 공개 문서 기억에 근거한 이름 (미검증) | Dell 의 `ProcCStates`, `ProcTurboMode`, `NodeInterleave`, `BootMode` 등 나머지 / HPE 의 `PowerRegulator`, `MinProcIdlePower`, `EnergyPerfBias`, `NumaGroupSizeOpt`, `ThermalConfig` 등 |
| 그럴듯하게 지어낸 이름 | Lenovo·Cisco·Supermicro 의 거의 모든 속성명과 값 표기(`Enable`/`enabled` 등), 각 벤더의 BIOS 버전·펌웨어 문자열, 레지스트리 파일 URI, Dell `Oem.Dell.Jobs` 링크 |
| 경로 구조 | DMTF 표준(`/Systems/<id>/Bios`, `@Redfish.Settings.SettingsObject`, `Registries/<id>` → `Location[].Uri`)을 따름. Dell Job 의 `TargetSettingsURI` 와 `Managers/<id>/Jobs` 는 계획서 3장 기준 |

Cisco 단독 CIMC 와 UCSM 관리형의 구분, Supermicro X12 의 실제 Settings 경로, Lenovo 의 `Bios/Pending` 여부(계획서 7장 미해결)는 여기서 **가정**했을 뿐 확정이 아닙니다.

## mock 서버의 동작 범위

- 인증: 모든 요청에 세션 토큰 또는 Basic 이 필요하며 없으면 401 (실제 BMC 는 ServiceRoot 를 무인증으로 열어 주는 경우가 많지만, 클라이언트가 토큰을 빼먹는 버그를 잡으려고 엄격하게 둠).
- 쓰기: Bios 의 `@Redfish.Settings.SettingsObject` 경로에 대한 PATCH 만 받아 pending 에 병합합니다 (현재 `Bios` 의 Attributes 는 안 바뀜). 레지스트리의 이름·ReadOnly·허용값(Enumeration/Integer/Boolean/String)을 어기면 400, 하나라도 틀리면 전체 거부.
- 강제하지 않는 것: 레지스트리 `Dependencies`, `@Redfish.SettingsApplyTime`(본문 기록만), Job 이 pending 을 소비하는 동작, 재부팅 반영, 기존 Pending/Job 의 충돌 (필요하면 `SetFault`·`Modify` 로 시나리오를 만듦).
- 기록: 모든 요청을 (method, path, status[, PATCH·Job 본문]) 로 남기고 `Calls`/`Count`/`Writes`/`Sessions`/`LogHits` 로 조회합니다. 세션 POST 본문(비밀번호)은 기록하지 않습니다.

## 사용법

테스트에서 (Go):

```go
s, _ := mockbmc.New("../../testdata/dell-r660", mockbmc.Options{User: "u", Pass: "p"})
defer s.Close()
base := s.Start() // https://127.0.0.1:포트 (httptest TLS)
// ... Client{BaseURL: base, Insecure: true} 로 접속 ...
// s.Sessions(), s.LogHits(), s.Writes(), s.Calls() 로 확인
```

별도 프로세스로 (바이너리 e2e, 포트만 바꿔 여러 개 가능):

```bash
MOCKBMC_PASS='비밀번호' bash mockbmc.sh dell-r660 8443         # 127.0.0.1:8443
MOCKBMC_PASS='비밀번호' bash mockbmc.sh hpe-dl360gen11 8444 -v # 요청마다 한 줄 출력
curl -k https://127.0.0.1:8443/redfish/v1                      # 401 (세션 필요)
# Ctrl-C / SIGTERM 으로 종료하면 "MOCKBMC_SUMMARY calls=.. sessions_created=.. log_hits=.. writes=.." 출력
```

## Farm(다중 가상호스트)과 3,000대 시험

수천 대 규모는 `internal/mockbmc` 의 `Farm` 으로 시험합니다. `Farm` 은 한 TLS 리스너(`0.0.0.0:포트` 1개)가 접속한 로컬 IP 로 가상 BMC 를 구분합니다. 리눅스는 `127.0.0.0/8` 전체가 루프백이라 `127.0.x.y:포트` 3,000개 주소를 리스너·FD 3,000개 없이 쓸 수 있고, 이 디렉터리의 트리 6종은 호스트 사이에 **공유(읽기 전용)** 하며 호스트별 변경(속성 덮어쓰기·Pending·세션·호출 기록·장애 주입)만 개별로 보관합니다(리눅스 전용, macOS·Windows 는 불가). `cmd/biostool/scale_test.go` 의 `TestScaleCheckThousands` 가 3,000대(미등록 IP 1%·5xx 1%·FAIL 주입 3%, 나머지는 6종 트리 순환)를 동시 100 으로 점검해 완료·결과 행 수(호스트×항목)·호스트당 세션 생성 1·삭제 1·쓰기 호출 0·로그 경로 접근 0·자원 사용(고루틴·연결·FD·힙)을 확인합니다(`BIOSTOOL_SCALE`·`BIOSTOOL_SCALE_CONC` 환경변수로 규모 조절, `-short` 면 건너뜀, `ulimit -n` 이 낮으면 건너뜀). 이 시험도 합성 데이터이므로 실제 BMC 의 응답 지연·장애 양상은 반영하지 않습니다.

## 실제 덤프로 대체하기

1. `xml.sh` 로 받은 덤프 디렉터리(`dumps/<벤더>/<모델>/<BIOS버전>/redfish/v1/...`)를 이 규칙 그대로 서빙할 수 있습니다: `mockbmc -dir dumps/<...>`.
2. 저장소에 올릴 실덤프는 IP·시리얼·MAC·UUID·자산태그 등을 지운 뒤 `testdata/<이름>/` 으로 옮기고, 합성 트리의 해당 이름을 교체하십시오. 실덤프에는 `MockData` 표식이 없으므로 이 디렉터리의 `TestTreesLoadAndAreConsistent` 대상 목록(`internal/mockbmc/mockbmc_test.go` 의 `treeNames`)에 넣지 말고 별도 이름으로 두십시오.
3. 합성 트리를 고칠 때는 JSON 을 직접 수정하되 `Oem.MockData`, 링크 무결성(모든 `@odata.id`/`Uri` 가 트리 안에 존재), Bios 속성 ↔ 레지스트리 일치를 지키십시오 (테스트가 검사).
