# HPE — 지원하지 않는 버전·모델 (BIOS Redfish)

| 관리망 / 버전 | 모델 | 사유 | 상태 | 대체 수단 |
|---|---|---|---|---|
| iLO 4 (Gen8) | **DL560 Gen8** | iLO4 RESTful API 레퍼런스: "available on ProLiant **Gen9** servers running iLO 4 2.00 or later"; iLOrest 설치 문서: "HPE **Gen9 or greater** servers". Gen8 은 레거시 RBSU BIOS(UEFI System Utilities 아님)라 `Bios` 리소스 대상이 아님. | verified-by-docs (문서 2건; 실장비 404 미관측) | 실장비 확인: `GET https://<ILO>/redfish/v1/Systems/1/Bios` (404 면 미지원). BIOS 설정은 RBSU/`conrep`(OS)·RIBCL XML 사용 |
| iLO 4 펌웨어 2.00 ~ 2.29 | DL360 G9, XL170r G9, XL250 G9, XL270d G9 (펌웨어가 낮은 경우) | `/redfish/v1` 없음, 레거시 `/rest/v1/systems/1/bios[/Settings]` 만 (속성은 최상위, `HpBios.1.2.0`) | verified (iLO4 레퍼런스 + python-redfish-library) | 펌웨어를 2.30+(최신 2.82)로 올리거나 `/rest/v1` 경로 사용 |
| iLO 4 (모든 버전) | 모든 iLO4 모델 | `Bios.ResetBios` / `Bios.ChangePassword` 액션 없음 (iLO5 신규). 초기화는 Settings 에 `BaseConfig:"default"` PUT/PATCH | verified (iLO5 adaptation) | 위 방법 |
| iLO 5 Gen10 (2.33 이전 구조) | DL360/DL560/DL580/XL270d Gen10 | `Bios/Oem/Hpe/*` 하위 경로 없음(구 경로 `Bios/baseconfigs` 등) | verified | 장비의 `Bios` 응답 `Oem.Hpe.Links` 를 따라가기 |

- 사용자 보유 모델 중 iLO5/6 은 모두 Redfish BIOS 지원. iLO7 은 보유 모델 없음.
- 이 표의 "지원 안 함"은 BIOS Redfish 기준이며, DL560 Gen8 의 서비스 루트(`/redfish/v1/`) 응답 여부는 실장비 확인 필요(unverified).
