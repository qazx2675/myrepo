# 표준 BIOS 4항목 → 벤더·관리망 버전별 속성 매핑

`bios_json/<벤더>/<관리망 버전>/bios_attributes*.json`(전체 속성, 수천 줄)에서 점검 스크립트(`biostool`)가 쓰는
표준 4항목만 뽑아 **벤더·관리망 버전별 속성명/값**으로 정리한 데이터입니다.

| 표준 이름 | 의미 |
|---|---|
| `system_profile` | 시스템 성능 프리셋 (Dell `SysProfile`, HPE `WorkloadProfile` 등) |
| `hyper_threading` | Hyper-Threading / SMT |
| `llc_prefetch` | LLC(Last Level Cache) Prefetch |
| `sub_numa_cluster` | Sub-NUMA Clustering (AMD 는 NPS 가 유사 항목이나 같은 설정이 아님) |

## 파일

| 파일 | 용도 |
|---|---|
| `standard_bios_map.json` | 벤더·버전·표준 이름별 전체 결과 (`AttributeName`, `DisplayName`, `ValueName`, `ValueDisplayName`, `status`, 확률, 후보 목록, 출처 파일) |
| `standard_bios_map.csv` | 같은 내용의 표 (엑셀용) |
| `../../profiles/VM_research.tsv` | **`biostool` 프로파일 형식**(`vendor model std_name attribute value verified`)으로 변환한 결과. `bios_check.sh --profile VM_research` 로 바로 사용 |
| `map_bios_attrs.ps1` | 위 파일을 다시 만드는 스크립트 (조사 데이터가 갱신되면 재실행) |

## 만든 방법 (typesafe-ai / Jev)

TypeSafe 문서의 "select instead of generate" 패턴을 따랐습니다.

1. **코드가 후보를 만든다:** 속성 JSON 을 벤더별 스키마로 평탄화하고, 표준 항목별 키워드 정규식으로 후보를 넓게 뽑는다 (속성 수천 개 → 항목당 수 개~수십 개).
2. **Jev `choice` 가 고른다:** 후보 속성을 선택지로, "해당 없음(`__none__`)"을 함께 준다. 반환값은 후보 중 하나이므로 속성명을 만들어내지 않는다.
3. **값도 `choice` 로 고른다:** 선택된 속성의 허용값 중 목표 값(성능 프리셋 / Enabled / Disabled)을 고른다. 허용값이 없으면 표준 의도값(Enabled/Enabled/Disabled)을 쓰고 `mapped_value_assumed` 로 표시한다.
4. **코드가 보정한다:** 같은 벤더·버전의 다른 파일(예: Dell 16G Intel/AMD)에서 이미 고른 속성이 이 파일에도 있으면 그 값을 가져온다(`mapped_from_sibling`).

## status 의미

| status | 뜻 |
|---|---|
| `mapped` | 속성과 값 모두 허용값 목록에서 Jev 가 선택 |
| `mapped_value_matched` | 값은 허용값 중 의도값과 대소문자 무시 일치하는 하나를 코드가 선택 |
| `mapped_value_assumed` | 속성은 선택됐지만 허용값 정보가 없어 의도값을 가정 (**실장비에서 확인 필요**) |
| `mapped_from_sibling` | 같은 벤더·버전의 다른 파일 결과를 가져옴 |
| `attr_only_*` | 속성만 확인, 값을 못 정함 |
| `none_of_candidates` | 후보는 있으나 해당 설정이 아님 (예: AMD 에는 LLC Prefetch 없음) |
| `no_candidates` | 속성 파일에 후보가 없음 (조사 자체가 비어 있는 버전 포함) |

`attr_conf` / `val_conf` 는 Jev 선택지 분포의 집중도(확률의 확신도)이며 정답 확률이 아닙니다.
`low_attr_conf=True` 인 행은 후보 사이에서 선택이 갈렸으므로 먼저 확인하세요.

## 프로파일(`VM_research.tsv`) 사용 주의

- **모든 행이 `verified=N`** 입니다. 실장비를 조회하지 않았으므로 첫 실장비 dry-run 본문을 사람이 확인한 뒤에만 `Y` 로 바꾸세요([FIRST_RUN.md](../../FIRST_RUN.md)).
- 기존 `profiles/VM.tsv`(엑셀 표 기반 시작값)는 그대로 두었고, 이 파일은 별도 프로파일입니다.
- 모델 → 관리망 버전 매핑은 `map_bios_attrs.ps1` 의 `$models` 표에 있습니다 (Dell 14G/15G/16G Intel/16G AMD/17G, HPE iLO4/5/6, Lenovo XCC/XCC2, Cisco UCSM, Supermicro X13/H13). 틀리면 그 표를 고치고 다시 실행하세요.
- 매핑하지 못한 항목은 `# UNMAPPED ...` 주석 줄로 남겼습니다. 여기에는 사유와 후보 목록이 있습니다.
- HPE `WorkloadProfile` 값은 문서에 `HighPerformanceCompute(HPC)` 로 적힌 것과 `HighPerformanceCompute` 두 표기를 허용값(`A|B`)으로 넣었습니다.

## 알려진 한계

- **Lenovo `system_profile`**: 허용값 목록이 레지스트리가 아니라 실측 샘플에서만 나온 값(`MaximumPerformance` 등)이라 `REVIEW` 로 남겼습니다. 실장비에서 `OperatingModes_ChooseOperatingMode` 의 허용값을 확인하세요.
- **Cisco UCSM `system_profile`**: `vpCPUPerformance`(값 `hpc`)와 `vpEnergyPerformance`(값 `performance`) 사이에서 실행마다 선택이 갈렸습니다. 둘 다 후보이므로 실장비에서 정하세요.
- **HPE iLO6/iLO7**: Gen11 Intel 레지스트리(U54/U59) 원본이 없어 문서의 AMD 샘플 기반이고 값은 `mapped_value_assumed` 입니다 ([bios_json/HPE/iLO6/README.md](../HPE/iLO6/README.md)).
- **Cray XD, Supermicro X10/X11/X12/X14**: 속성 목록이 없어 매핑하지 못했습니다.
- Dell 16G AMD(R6615)는 LLC Prefetch 가 없고 NPS 가 Sub-NUMA 와 유사하지만 같은 설정이 아니라 매핑하지 않았습니다.
