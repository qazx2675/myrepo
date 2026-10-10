# Supermicro X13 / H13 (BMC 펌웨어 01.0x ~ 01.13.xx)

- 프로토콜 판정: **json** (Redfish). Intel = X13, AMD = H13 (BMC 세대는 같고 BIOS 속성은 보드별로 다름).
- 사용자 보유 모델: **AS-1115HS-TNR** (보드 H13SSH, AMD EPYC 9004/9005 지원 -- 매뉴얼 MNL-2587 Rev 1.1c 서문). 매핑 맞음, 정정 없음.
- 최신 펌웨어(Supermicro 다운로드 피드, 빌드 2026-07-19 직접 curl): **`H13SSH_4.0_AS01.13.11_SAA1.5.0-p9`** = BIOS 4.0 / BMC 01.13.11 / SAA 1.5.0-p9, SHA256 `8b8d6f5da94b601f164f3ebf1cab91a0d78169b46367c9f6dc589603fbd06b47`. (웹 검색 요약이 말한 "BIOS 3.9 / BMC 01.13.00" 은 구버전 캐시였고 피드 원문이 우선.) 링크: https://www.supermicro.com/en/support/resources/downloadcenter/firmware/AS-1115HS-TNR/BIOS (다운로드는 EULA 뒤라 받지 않음)
- 같은 BMC 세대 다른 보드: X13SEI-F BMC 01.13.11/BIOS 3.1, X13DEI BMC 01.05.21/BIOS 2.8, H13DSH BMC 01.09.05/BIOS 3.9a (피드). 즉 BMC 펌웨어 번호는 보드마다 다르다.
- 속성: **부분(unverified)**. `bios_attributes.json` = H13SSH 사용자 매뉴얼 4장(158행, 그 중 선택지 있는 64행, 기본값 62행)을 스크립트로 파싱한 **UI 이름** 목록. Redfish AttributeName(ID)·type·read_only 는 공개 자료 없어 null. 완전한 레지스트리는 **unavailable** -> 아래 덤프 방법.

## BIOS URI (verified = 공식 Redfish User Guide Rev 6.1 (2026-03-11, Redfish 1.22.2-00.04) + HTML 4.0(2024-07-25) 두 문서 일치)
| URI | 용도 | 상태 |
|---|---|---|
| `GET /redfish/v1/Systems/1/Bios` | 현재 설정 | verified |
| `/redfish/v1/Systems/1/Bios/SD` | **대기(pending) 설정**. `Bios/Settings` 가 아님. GET 시 "will be applied on next reboot", `@odata.etag` 포함 | verified |
| `POST /redfish/v1/Systems/1/Bios/Actions/Bios.ResetBios` | 기본값 복원, 응답 200, 이후 시스템 리셋 필요 | verified (Guide 6.1, 4.0, 2.0a X11DP) |
| `POST /redfish/v1/Systems/1/Bios/Actions/Bios.ChangePassword` | 본문 `{"PasswordName":"AdministratorPassword"|"UserPassword","OldPassword":..,"NewPassword":..}` | verified |
| `/redfish/v1/Systems/1/Bios/ChangePasswordActionInfo` | ActionInfo | verified(Available APIs 표) |
| `/redfish/v1/Registries/BiosAttributeRegistry` | 레지스트리 파일 항목. 문서 예시 파일명: `BiosAttributeRegistry.v1_0_0` / `/registries/BiosAttributeRegistry.1.0.0.json` | 항목 verified, 파일 경로는 장비 Location.Uri 를 따를 것 |
| Bios 리소스 값 | `"AttributeRegistry": "BiosAttributeRegistry.v1_0_0"`, `@odata.type "#Bios.v1_1_1.Bios"` | verified(예시) |

- 라이선스: 위 BIOS API 전부 **SFT-DCMS-SINGLE** (Guide 6.1 / 4.0 Available APIs). 미보유 시 거부될 수 있음 -- unverified(실장비 동작).
- 변경 후 시스템 리셋 필요 ("Changes in BIOS attributes require a system reboot").

### PATCH 대상 불일치 (중요)
- Guide 4.0 HTML 과 Supermicro FAQ 33504(`PATCH /redfish/v1/Systems/1/Bios`, `{"Attributes":{"BootOption#1$3":"UEFI CD/DVD"}}`, 202) : **Bios 에 PATCH**.
- Guide Rev 6.1(2026) 과 libredfish 이슈 #147(SSG-222B-NE3X24R): `Bios` 는 GET 전용, `@Redfish.Settings.SettingsObject` 가 `Bios/SD` 를 광고, Bios 에 PATCH 하면 **405**. 해결은 광고된 SettingsObject 를 읽고 없을 때만 `/Bios` 로 폴백. https://github.com/dsx-ai-factory/libredfish/issues/147
- 결론: 두 문서가 개정에 따라 다름. 도구는 `Bios` 응답의 `@Redfish.Settings.SettingsObject.@odata.id` 우선 -> 없으면 `/Bios/SD` -> `/Bios`. AS-1115HS-TNR 실장비 확인 필요 (unverified).

## 속성 키 규칙
- 레지스트리의 `AttributeName` 은 `QuietBoot_0027`, `OptionROMMessages_0028`, `WatchDogFunction_002E`, `WatchDogAction_0030` 처럼 **UI 이름에서 공백 제거 + `_` + 4자리 16진 접미사**로 보인다 (가이드 예시 4개에서의 관찰, 규칙으로 일반화는 unverified).
- 그러나 같은 가이드의 PATCH 예시는 접미사 없는 `QuietBoot`, `PowerButtonFunction`("4 Seconds Override") 를 쓰고, FAQ 33504 는 `BootOption#1$3` 를 씀 -> `Boot Option #1` 같은 UI 이름 -> `BootOption#1$3` 형태(공백 제거, `#`유지, `$n` 접미사)로 보이나 `$3` 의 의미 문서 없음 (unverified). 실제 키는 반드시 장비 레지스트리에서 확인.
- 레지스트리 항목 필드: `AttributeName, CurrentValue, DefaultValue, DisplayName, GrayOut, HelpText, Hidden, MenuPath("./Advanced/Boot Feature"), ReadOnly, Type(Boolean|Enumeration…), Value[{ValueDisplayName, ValueName("1"/"0")}]`; 별도 `Menus`, `Dependencies`(MapFrom/MapTo, 예: WatchDogFunction==Disabled 이면 WatchDogAction Hidden).
- 매뉴얼에서 Quiet Boot 는 "Disabled/Enabled" 이지만 레지스트리 예시는 `Boolean` (true/false) -> UI 값과 Redfish 값 타입이 다를 수 있음.
- 확인된 Gen13 이상 전용 속성명(가이드 SED 절): `KMSSecurityPolicy`, `TPMSecurityPolicy`, `Super_GuardiansProtectionPolicy`(Bios/SD PATCH), SecureBoot 는 `SecureBootEnable` (Bios PATCH, Secure Boot 절).

## 첫 Redfish BMC 펌웨어
- Redfish 자체/Bios 기능이 시작된 정확한 X13/H13 BMC 빌드: **unavailable**. 단 per-feature 표기: FixedBootOrder "Redfish 1.11 (2020.3), Gen 13", SED "since Gen 13", UefiDevicePath "Gen 13 1.07 / Gen 14 1.05", virtual media "2020.3 X13/H13 (BMC FW 1.01.xx)" (HTML Available APIs).

## 시도 내역
1차: Redfish User Guide Rev 6.1 PDF(18k 행) 와 HTML 4.0 직접 읽기, Reference Guide 2.0a, H13SSH 매뉴얼 MNL-2587 PDF, BMC User Guide X13/H13/B13(Redfish BIOS 속성 없음), 다운로드 피드 4개. 2차: SUM 가이드(XML 포맷), Supermicro FAQ, libredfish PR/이슈, NVIDIA/Blackcore(타 AMI BMC, 참고만), GitHub 코드 검색(이 세션 허용 저장소 외 불가) -- Supermicro 레지스트리 JSON 공개 덤프 없음. 이전 결과(back/…/README.md)에서 정정: 최신 펌웨어는 4.0/01.13.11 맞음(유지), 액션 경로 출처 1곳 -> 3개 문서로 보강, `/registries/BiosAttributeRegistry.1.0.0.json` 은 문서 예시일 뿐. Jev 생략(키 없음).

## 실장비 덤프 (필수 후속)
```
curl -sk -u <USER>:<PASSWORD> https://<BMC>/redfish/v1/Registries
curl -sk -u <USER>:<PASSWORD> https://<BMC>/redfish/v1/Registries/BiosAttributeRegistry      # Location[].Uri 확인
curl -sk -u <USER>:<PASSWORD> https://<BMC>/redfish/v1/Systems/1/Bios                        # Attributes, Actions, @Redfish.Settings
curl -sk -u <USER>:<PASSWORD> https://<BMC>/redfish/v1/Systems/1/Bios/SD
```
대안(XML): `sum -i <BMC> -u <USER> -p <PASSWORD> -c GetCurrentBiosCfg --file bios.xml` (`<Setting name=.. type=Option|CheckBox|Numeric|String|Password>`, `<DefaultOption>` 포함).
