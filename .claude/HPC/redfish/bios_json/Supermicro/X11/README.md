# Supermicro X11 / H11 (BMC 펌웨어 1.xx, 예 1.74.xx)

- 프로토콜 판정(BIOS): **json 은 X11DP 한정 문서화, 그 외 X11 은 unverified**. 나머지 경로는 XML(SUM).
- 사용자 보유 모델: 없음 (X11 계열 보유 목록에 없음).
- 속성: **unavailable** (`bios_attributes.json` 메타만).
- 최신 펌웨어(피드 2026-07-19): X11DPi-NT BMC 1.74.19 / BIOS 4.7, X11DPH-T BMC 1.74.25 / BIOS 4.7. https://www.supermicro.com/en/support/resources/downloadcenter/firmware/MBD-X11DPI-NT/BMC

## 근거
| 항목 | 내용 | 출처 | 상태 |
|---|---|---|---|
| Redfish 지원 | X11 은 1.xx BMC 펌웨어, H11 포함 "X10 and H11 and later" | Guide 2.0a 1장 ; Guide Rev 6.1 서문 | verified |
| Bios URI | `Bios`(현재), `Bios/SD`(대기), `Bios/Actions/Bios.ResetBios`, `Bios.ChangePassword` -> 전부 "only X11DP supports" (2019-02 기준) | Guide 2.0a 2.3 표 + 5.3 | verified (X11DP) / 그 외 X11 unverified |
| 레지스트리 | `/redfish/v1/Registries/BiosAttributeRegistry.v1_0_0` (메뉴/속성/의존성) | Guide 2.0a 5.3 | verified(문서) |
| 이후 X11 펌웨어(1.7x)에서 다른 X11 보드 확장 여부 | X11 전용 Redfish 가이드 후속 개정 없음(Guide Rev 3.x 이후는 X12/Gen12 이상 대상) | Guide 6.1 "applies to platforms starting with Gen 12" | unavailable |

예시 속성(2.0a 5.3 의 의존성 예시에 등장): `PowerTechnology`, `PowerPerformanceTuning`, `ENERGY_PERF_BIAS_CFGmode` -- 전체 목록 아님.

## 시도 내역
1차: Guide 2.0a/Rev 6.1 직접 읽기, 피드. 2차: SUM 가이드, 서드파티 검색(Thomas-Krenn 등)에서 X11 Redfish BIOS 속성 덤프 없음. Jev 생략.

## 실장비에서 확인
`GET /redfish/v1/Registries` -> BiosAttributeRegistry 의 Location 따라 GET. `Systems/1` 응답에 `Bios` 가 없으면 XML(SUM)로.
