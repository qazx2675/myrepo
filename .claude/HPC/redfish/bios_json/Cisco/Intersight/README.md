# Cisco Intersight (IMM = Intersight Managed Mode) BIOS — 프로토콜: **Intersight REST JSON** (Redfish 아님)

- 사용자 모델: **X210c M7**(IMM 관리 시). (B200 M5/B480 M5 도 IMM 지원 목록에 있음 — IMM BIOS Tokens 가이드의 supported platforms 에 B200 M5, B480 M5, X210c M7 표기, 상태 unverified)
- 작성일 2026-10-10. Jev 판정은 키 부재로 **생략**, "문서 2중 출처"로만 verified 판정.
- **BIOS 는 Intersight BIOS 정책(`bios.Policy`)을 서버 프로파일에 붙여 적용**한다. 서버에서 UCSM XML/Redfish 로 직접 토큰을 쓰는 방식이 아님. 이 BIOS 정책 API 는 표준 DMTF Redfish 가 **아니다**(Intersight 전용 OpenAPI/REST; `/api/v1/...`).

## 1. BIOS 속성 전체 (bios_attributes.json) — verified (SDK 스키마 + Terraform provider 일치)
- 출처 ①: **intersight-python** (CiscoDevNet/intersight-python master, commit `0e8dfdb798aa`(api 부분) / OpenAPI 문서 버전 **1.0.11-2026072720**, `intersight/model/bios_policy.py` 클래스 `bios.Policy`). ast 파싱: `allowed_values`, `validations`(정규식), `attribute_map`, `openapi_types`, 도큐스트링의 설명·default.
- 출처 ②(독립 교차): **terraform-provider-intersight** `intersight/resource_intersight_bios_policy.go` (CHANGELOG: build 20260828115928667 동기화). 스키마 키 507개 중 BIOS 토큰 **473개가 SDK 의 토큰 473개와 정규화명 100% 일치**(누락/잉여 0). 단 둘 다 같은 OpenAPI 에서 생성되므로 "독립성"은 약함.
- 수록: BIOS 토큰 **473개** (`attributes`: JSON 키 `PascalCase` 이름, type, allowed_values, default, validation_regex(숫자형), description) + 정책 공통 속성 18개 (`base_properties`: Name, Description, Organization, Profiles, BiosConfigurations, 및 읽기전용 메타 Moid/CreateTime/ModTime/Owners/Tags/VersionContext/Ancestors/Parent/PermissionResources/DisplayNames/SharedScope/AccountMoid/DomainGroupMoid). terraform 에는 추가로 `model`(Not-Applicable/UCSC845A), `bios_type`(Generic/ModelSpecific) 속성이 있음(SDK 최신본에서 확인 필요, unverified).
- default: SDK 도큐스트링 "if omitted the server will use the default value of ..." 의 값(거의 전부 `platform-default`). 숫자형 토큰 중 일부(`CbsCmnApbdisDfPstateRs` 등 정규식 검증) 는 `platform-default` 또는 범위 숫자.
- **모델/펌웨어 적용 범위는 스키마에 없음**(X210c M7 에 어떤 토큰이 유효한지는 Cisco "UCS Server BIOS Tokens in Intersight Managed Mode" 가이드 — 4.3(3a) 이후만 수록 — 의 모델별 표 참조). 가이드 What's New 예: Power Performance Tuning(4.3(4a) New), DFX OSB(4.3(4a) New), UEFI Memory Map Special Purpose Memory Flag(4.3(5c) New), IIO eDPC Support(6.0(1b) New), CDN Support for LOM(6.0(1b) Changed) — 모두 X210c M7 포함.
- 읽기전용 구분: 토큰은 모두 쓰기 가능(`read_only=false`). 정책 메타(Moid 등)는 서버 설정.

## 2. REST URI (SDK `intersight/api/bios_api.py` 기준, verified)
Base: `https://intersight.com` (SaaS) 또는 어플라이언스. 인증: API Key + HTTP Signature(또는 OAuth2). 

| 리소스 | 메서드 | URI |
|---|---|---|
| BIOS 정책 목록/생성 | GET / POST | `/api/v1/bios/Policies` |
| BIOS 정책 단건 | GET / PATCH / POST(=update) / DELETE | `/api/v1/bios/Policies/{Moid}` |
| BIOS 유닛(서버 BIOS 상태) | GET / PATCH | `/api/v1/bios/Units`, `/api/v1/bios/Units/{Moid}` |
| BIOS 부트모드 | GET / PATCH | `/api/v1/bios/BootModes`, `.../{Moid}` |
| BIOS 부트디바이스 | GET | `/api/v1/bios/BootDevices`, `.../{Moid}` |
| BIOS 시스템 부트오더 | GET | `/api/v1/bios/SystemBootOrders`, `.../{Moid}` |
| BIOS 토큰 설정 | GET | `/api/v1/bios/TokenSettings`, `.../{Moid}` |
| Select Memory RAS | GET | `/api/v1/bios/VfSelectMemoryRasConfigurations`, `.../{Moid}` |

- 정책을 서버에 적용: `server/Profiles`(`/api/v1/server/Profiles`)의 `PolicyBucket` 에 `bios.Policy` 를 참조로 추가 후 profile 배포(`Action: Deploy`). 이 연결부는 본 조사 범위 밖 → 상세 unverified.
- 요청 예 (플레이스홀더, 서명 헤더 생략):
```
POST /api/v1/bios/Policies
{ "Name":"<NAME>", "Organization":{"ObjectType":"organization.Organization","Moid":"<ORG_MOID>"},
  "CpuPerformance":"hpc", "ProcessorC1e":"disabled" }
```
  (`CpuPerformance`: platform-default/custom/enterprise/high-throughput/hpc, `ProcessorC1e` 모두 bios_attributes.json 에 존재 확인. 값 enum 은 `allowed_values`.)
- **ResetBios/ChangePassword 액션 없음**(Intersight 정책 모델. 기본값 복원 = 토큰을 `platform-default` 로).

## 3. Redfish 여부
- Intersight BIOS 정책 API: **Redfish 아님** (verified: SDK/Terraform 모두 `/api/v1/bios/*` REST).
- X210c M7 노드 CIMC 자체에는 Redfish 가 기본 활성이라는 Cisco 보안 권고(cisco-sa-cimc-redfish-cominj, 2024-10-02; IMM X-Series 수정 5.0(4g)/5.2(2.240053))가 있으나, IMM 모드 노드 BIOS 용 Redfish URI·속성 문서는 확인 불가 → **unavailable**. 벤더 지원 경로는 Intersight 정책.

## 4. 버전/차이
- `diff_vs_UCSM.md`, `diff_vs_CIMC.md`: 이름·enum 비교(스크립트). Intersight 토큰은 IMC(CIMC) 와 464개(정규화명) 겹침 → CIMC 와 거의 같은 토큰 집합(IMC 계열 모델 기반)이며 UCSM 과는 76개만 이름 일치(UCSM 은 GUI 토큰 중 다수가 `biosVf*` 클래스로 미수록).
- "버전" 개념: Intersight 는 SaaS 라 BIOS 토큰 지원은 서버 펌웨어(IMM 4.3(3a)+ 가이드)와 모델 단위로 결정. UCSM 4.3 ↔ IMM 4.3 토큰 대응 가이드 표현은 거의 동일(GUI 이름).

## 5. 시도 내역
1차(문서군): IMM BIOS Tokens 가이드 intro(성공, What's New 만), Cisco 보안 권고, Intersight API docs(JS 렌더라 미취득). 2차(공개 코드): intersight-python(raw 파일 직접 파싱), terraform-provider-intersight(go 스키마 키 대조), 둘 다 성공. 실패: terraform docs md(404), 모델별 토큰 적용표(가이드 본문 챕터 미취득).
