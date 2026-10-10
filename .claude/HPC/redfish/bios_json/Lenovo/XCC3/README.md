# Lenovo XCC3 (XClarity Controller 3) - BIOS 속성 (사용자 보유 모델 없음, 비교용 최신)

조사일 2026-10-10. 브랜치 bios-collect. **Jev 생략**.

## 대상 / 프로토콜
- 사용자 보유 모델: **없음** (ThinkSystem V4 세대 = XCC3. 근거 LP1607: "Lenovo XCC3 Premier (V4 servers) or XCC2 Platinum (V3 servers)").
- 프로토콜: **json (Redfish)**. XML API 없음.

## URI (Lenovo xcc3-restapi 가이드 기준)
| 용도 | URI | 검증 |
|---|---|---|
| BIOS | `GET /redfish/v1/Systems/1/Bios` | verified (XCC3 문서; XCC/XCC2 문서와 동일 구조) - 실장비 덤프는 못 구해 2차 출처는 문서 3종(xcc/xcc2/xcc3) 일치뿐 -> 2중 출처 기준으로는 "문서 3종 일치" |
| Pending | `GET/PATCH /redfish/v1/Systems/1/Bios/Pending` (`SupportedApplyTimes: OnReset`) | verified (문서) |
| 레지스트리 | `GET /redfish/v1/schemas/registries/BiosAttributeRegistry.1.0.0.json` | verified (문서) |
| ResetBios | 예시 JSON 은 `/redfish/v1/Systems/1/Bios/Actions/Bios.ResetBios`, 필드표는 `/redfish/v1/Systems/system/Bios/Actions/Bios.ResetBios` 로 **문서 내부 불일치** | unverified -> 장비에서 `Bios.Actions."#Bios.ResetBios".target` 읽어 사용 |
| ChangePassword | `POST .../Bios/Actions/Bios.ChangePassword`, ActionInfo `/redfish/v1/Systems/1/Bios/ChangePasswordActionInfo` | verified (문서; 필드표 target 은 선행 `/` 누락 오타) |

## 속성 목록 상태: **minimal**
`bios_attributes.json`: 34개 이름 (verified 2).
- 공식 문서 레지스트리 예시(SR650 V4, FW "1.20", SystemId 7DGCCTO1WW) 의 `CXLMemoryModule_MemoryMode`, `DevicesandIOPorts_Bifurcation_Slot7` 는 type/허용값/기본값/MenuPath 까지 확정.
- BIOS 응답 예시의 `AdvancedRAS_DIMMDisablePolicy`(= `DisableFaultyDIMMPersistently`), `iSCSI_TargetPort_8`(= 3260) 2개.
- LP2210(EPYC 9005 Turin 튜닝 가이드, 2025-04-28) 30개 설정(Redfish 이름/표시 선택지/기본 표시) - **LP2210 은 모델을 특정하지 않으며 SR645 V3 도 9005 를 지원하므로 XCC3 전용 증거가 아님**.
- 실덤프/Intel Xeon 6(V4) 목록: **unavailable**. GitHub 코드 검색(`CXLMemoryModule_MemoryMode`, `DevicesandIOPorts_Bifurcation_Slot`) 결과 0건. vmware/vcf-readiness 가 주장하는 `SystemSettings_WorkloadProfile` 은 Lenovo 문서에서 확인되지 않아 **제외**(AI 생성 코드 추정).
- LXCA scripting 문서의 "ExtendedV4BIOS" 예시는 `Q00004 Setup Prompt Timeout` 형태의 공백 포함 이름 (SR635/SR655 V4 AMD 계열로 문서가 안내) -> 이 계열 서버는 `Group_Setting` 이 아닌 다른 명명을 쓸 수 있다는 단서(unverified, 참고만). 같은 맥락에서 Lenovo 공식 스크립트는 SR635/SR655(AMD 1세대)에서 `Q00999_Boot_Option_Priorities` 와 `Q00999 Boot Option Priorities` 두 철자를 모두 처리.

## 실장비 덤프 방법
`bios_attributes.json` -> `unavailable.how_to_dump`.

## 시도 내역
1. 다른 문서군: xcc3-restapi 의 Bios/Pending/Reset/Password/Registry 전 페이지, LP1607(XCC3 언급), LP2210, pubs.lenovo.com uefi_xeon_6th(메뉴 문서만 존재, `operating_modes` 404, Redfish 이름 없음), LXCA scripting.
2. 공개 코드: GitHub 검색 3회(Bifurcation, CXLMemoryModule, SystemSettings_WorkloadProfile) -> 실덤프 0건. DMTF org Lenovo 레지스트리 0건. vcf-readiness 는 근거 없는 주장이라 폐기.
3. 결론: XCC3 는 사용자 모델이 없고 공개 자료가 빈약 -> 최소 세트만 기록.
