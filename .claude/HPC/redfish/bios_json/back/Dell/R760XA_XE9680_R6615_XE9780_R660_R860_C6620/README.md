# Dell iDRAC9/iDRAC10 BIOS 속성 그룹: R760XA / XE9680 / R6615 / XE9780 / R660 / R860 / C6620

## 모델 및 프로토콜 판정
| 모델 | BMC | 프로토콜 |
|---|---|---|
| PowerEdge R760XA, XE9680, R660, R860, C6620 (Intel) | iDRAC9 7.xx | **json** (Redfish) |
| PowerEdge R6615 (AMD) | iDRAC9 7.xx | **json** (Redfish) |
| PowerEdge XE9780 | iDRAC10 1.30.xx | **json** (Redfish) |

XML API 전용 모델 없음. 근거:
- Dell KB 000178045 (Redfish API with iDRAC): iDRAC9/iDRAC10 최신 Redfish API 가이드는 developer.dell.com/apis 의 "iDRAC9 Redfish API" / "iDRAC10 Redfish API". https://www.dell.com/support/kbdoc/en-us/000178045/redfish-api-with-dell-integrated-remote-access-controller
- iDRAC9 AR 가이드 / iDRAC10 AR 가이드: 속성이 가이드에 없으면 `redfish/v1/Registries` GET 으로 확인.
- 모델별 Dell 지원 매뉴얼 목록: R760XA/XE9680/R6615/R660/R860/C6620 모두 "Integrated Dell Remote Access Controller 9 Attribute Registry" (idrac9_ar_guide_7xx, 2026-08-21 갱신), XE9780 은 "iDRAC10 Version 1.30.xx Series Attribute Registry" (2026-09-29).

## 최신 펌웨어
- iDRAC9: 7.30.30.54 (AR 가이드 "New features added" 최신 항목). BIOS 버전은 문서에 별도 명시 없음(unknown).
- iDRAC10: 1.30.60.50 (AR 가이드 "New attributes added in iDRAC10" 최신 항목).

## URI 와 검증 상태
| URI | jev 확률(yes) | 독립 출처(검증2) | 시도 | 판정 |
|---|---|---|---|---|
| `/redfish/v1/Systems/System.Embedded.1/Bios` | 0.96 | dell/iDRAC-Redfish-Scripting 의 GetSetBiosAttributesREDFISH.py 외 BiosResetToDefaultsREDFISH.py, BiosChangePasswordREDFISH.py 가 각각 동일 URI 사용 | 1차 | verified |
| `/redfish/v1/Systems/System.Embedded.1/Bios/Settings` | 0.93 | GetSetBiosAttributesREDFISH.py + BiosChangePasswordREDFISH.py(TargetSettingsURI) | 1차 | verified |
| `/redfish/v1/Systems/System.Embedded.1/Bios/BiosRegistry` | 0.92 | 단일 스크립트(GetSetBiosAttributesREDFISH.py)에서만 확인 | 1차 | unverified (검증2 미충족) |
| `/redfish/v1/Registries` | 1.00 | Dell AR 가이드(iDRAC9/iDRAC10) 본문 + KB | 1차 | verified |
| 프로토콜 json (iDRAC9) | 1.00 | KB + developer.dell.com "iDRAC9 Redfish API" | 1차 | verified |
| 프로토콜 json (iDRAC10, XE9780) | 0.99 | KB 에 "iDRAC10 Redfish API" 게시 언급 + iDRAC10 AR 가이드의 Registries 안내 | 1차 | verified |
참고: 스크립트 출처는 Dell 공식 GitHub 저장소이나 URI 가 코드에서만 확인됨(developer.dell.com 의 Bios 리소스 페이지 본문은 가져오지 못함, WebFetch 403/미노출). iDRAC10 의 Bios URI 는 iDRAC9 와 동일하다고 문서로 직접 확인하지 못했으며(iDRAC10 Redfish API 본문 미확보), 같은 `System.Embedded.1` 체계로 알려진 점은 unverified.

## 묶은 근거
- 7개 모델 모두 Dell 지원 매뉴얼에서 동일한 AR 가이드 토픽(BIOS Attributes, guid-b833fbc5-a267-42ed-8253-2d295970d6b6)을 가리킨다. iDRAC9 7.xx 와 iDRAC10 1.30.xx 의 BIOS Attributes 페이지는 본문 길이가 734,988 vs 734,710 자로 거의 같고, 샘플 구간(경계 항목: DcuIpPrefetcher, SataPortBModel~Slot33, SysSecurity.SetupPwdExpirationDate~UefiPxeIpVersion, SanitizeStatus20 등)이 일치한다. 전체 본문 대조는 못 했다(278자 차이의 위치 미확인).
- 이 문서는 플랫폼 공통 상위집합이다. 속성 이름이 Intel 전용(예: SubNumaCluster, Upi*, EnableTdx)/AMD 전용(예: CcdCores, CcxAsNumaDomain, Dxio/Smu/AgesaVersion)인 것이 섞여 있으며, 모델별 실제 노출 여부는 문서만으로 확정 불가 -> 실장비 `/Bios` 응답과 대조 필요 (unknown).

## bios_attributes.json 한계
- 1431개 속성(번호 시리즈는 개별 이름으로 전개). 허용값은 AR 가이드 문서 기준. `default` 는 문서가 모든 항목에 "None" 이라 null. `read_only` 는 문서가 명시한 CurrentEmbVideoState 만 true, 나머지 null.
- 문서에서 값이 잘린 항목: `ProcSettings.DcuIpPrefetcher`, `SysSecurity.SetupPassword`(type unknown). 이름이 잘려 제외한 항목: SetupPassword 바로 다음의 "...Password Status" 계열 1개. DimmSlot30 도 값이 잘려 DimmSlot 시리즈와 동일하다고 가정함(DimmSlot20 은 문서에 값 두 종류로 중복 기재, Enabled/Disabled 채택). 시리즈를 "동일 값"으로 접어 추출한 도구 결과를 그대로 전개했으므로 일부 시리즈 경계(예: SlotDisablement.Slot34~)는 확인 못 한 구간이 있다.
- `CcdCores` 허용값은 문서가 Rome/Milan 세대별 값을 한 문장으로 적어 합쳐서 나열함.
- 추출은 WebFetch 요약 모델(구간별 100K자)을 거쳤기에 전사 오류 가능성이 있음. 정답 기준은 실장비 `/redfish/v1/Registries` 의 BiosAttributeRegistry.

## 구버전 차이
확인불가. AR 가이드의 "New features added"(7.00.30.00 ~ 7.30.30.54) 및 iDRAC10 "Deprecated/Reorganized/Changed attributes" 절은 목차만 확인했고 본문 URL 을 얻지 못해 old_fw_diff.md 는 만들지 않았다.
