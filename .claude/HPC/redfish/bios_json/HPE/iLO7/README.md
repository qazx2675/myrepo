# HPE iLO 7 — BIOS (Redfish)

조사일 2026-10-10. **사용자 보유 모델 없음** — 공개 자료 범위 best-effort. Jev 생략.

## 1. 프로토콜: **json** (Redfish, DMTF Redfish 1.22.1 / Data Model 2025.4)
- 근거: 포털 iLO7 changelog("fully conformant with the Redfish protocol … Any remaining support for the pre-Redfish iLO RESTful API has been removed"), iLO7 v1.25 Bios 정의, HPE ilo-redfish-emulator 의 iLO 7 실캡처 3종. verified.
- 대상 서버: ProLiant **Gen12** (DL360/DL380a Gen12, Alletra Storage Server 4220 Gen12 등). 사용자 보유 Gen12 모델 없음.
- 최신: 포털 기준 **iLO 7 v1.25.00** (Wikipedia 표는 1.24.00). 관측 샘플은 1.13.01 / 1.14.00 / 1.17.00.

## 2. 해당 사용자 모델
없음 (HPE 보유 모델은 iLO4/5/6 만). 아래 내용은 향후 대비.

## 3. URI (iLO 7)
| URI | 상태 | 근거 |
|---|---|---|
| `/redfish/v1/Systems/1/Bios` (GET) | verified | 포털 iLO7 v1.25 Bios 정의 인스턴스 + 3개 실캡처(`Bios.v1_2_3`) |
| `/redfish/v1/Systems/1/Bios/Settings` (GET/POST/PATCH) | verified | 동일, iLO7 adaptation 의 PATCH 예제 `PATCH /redfish/v1/Systems/1/bios/settings/ {"Attributes":{"EmbeddedSerialPort":"Disabled"}}` |
| `/redfish/v1/systems/1/bios/Actions/Bios.ResetBios` | verified(캡처 target) | |
| `/redfish/v1/systems/1/bios/Actions/Bios.ChangePassword`(캡처 표기 `…ChangePasswords`) | verified(캡처) | |
| `/redfish/v1/systems/1/bios/oem/hpe/{baseconfigs,boot,mappings,nvmeof,serverconfiglock,tlsconfig,iscsi}/` | verified(캡처 `Oem.Hpe.Links`) | `kmsconfig` 링크는 관측 3종 모두 없음 |
| `/redfish/v1/Registries` -> `BiosAttributeRegistryU68.v1_1_40` / `U71.v1_1_52` / `U72.v1_1_34` -> `/redfish/v1/registrystore/registries/en/<소문자 이름>` | verified(경로), 내용 unavailable | 캡처 |
- iLO7 1.11.00+ 에서 `SerialInterface` 가 읽기 전용이 되어 BIOS 속성 `EmbeddedSerialPort` 로 대체(adaptation 문서).
- 기본값: 레지스트리 `DefaultValue` 또는 `Bios/Oem/Hpe/BaseConfigs`.

## 4. ROM 패밀리 (관측)
| 서버 | iLO 7 | ROM | 속성 수 |
|---|---|---|---|
| ProLiant Compute DL360 Gen12 | 1.14.00 (2025-05-28) | U68 v1.40 (2025-05-22) | 256 |
| HPE Alletra Storage Server 4220 Gen12 (mock 폴더명 DL340e) | 1.17.00 (2025-08-13) | U71 v1.52 (2025-10-03) | 256 |
| ProLiant Compute DL380a Gen12 | 1.13.01 (2025-05-13) | U72 v1.34 (2025-04-18) | 350 |
(mock 은 "SIMULATED" 표기이나 속성 이름은 실제 장비 캡처 기반.)

## 5. 속성 파일 `bios_attributes.json` (721개)
- 출처: 문서 v1.25(540, 정의 있음 — **iLO6 v1.79 문서와 100% 동일한 복사본**, 문서에 "Added iLO6 1.10" 문구까지 그대로, iLO8 문서도 동일) + 관측 3종(값만). 정의 있는 540 + 관측 전용 181(type=null).
- 즉 **iLO7 전용 type/허용값은 unavailable**. 공개 문서로는 iLO6 와 구분 불가 -> 실장비 레지스트리(Support Center "RESTful API BIOS Schemas/Registries" U68/U72 등, 로그인/EULA, 링크만 https://support.hpe.com)로 확정.
- default=null.

## 6. 검증표
| 항목 | 상태 |
|---|---|
| Redfish / Bios / Settings URI | verified |
| Oem/Hpe 하위 구조(iLO6 와 동일) | verified(캡처) |
| 속성 이름(Gen12 Intel) | verified(실캡처 이름 수준) |
| type/허용값/기본값 | unavailable |
| 최신 FW 1.25.00 | verified(포털) / Wikipedia 1.24.00 과 불일치 |

## 7. 시도 횟수
3회: (1) 포털 iLO7 v1.25 문서·adaptation·changelog, (2) GitHub 코드 검색 -> HPE ilo-redfish-emulator Gen12 mockups, (3) 문서끼리 스크립트 비교로 복사본 확인. 레지스트리 원본은 EULA 라 미수신.
