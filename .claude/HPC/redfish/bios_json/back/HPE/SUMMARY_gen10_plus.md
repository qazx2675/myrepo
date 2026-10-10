# HPE Gen10 Plus / Gen10 / Gen11 BIOS 조사 요약 (이 파일: 담당 모델 9종)

| 모델 | 그룹 | 프로토콜 | URI 검증 | 속성 |
|---|---|---|---|---|
| DL360 Gen10 | DL360_G10 | json | verified | 401 (iLO5 Intel 샘플, 모델 전용 보장 없음) |
| DL560 Gen10 | DL560_G10_DL580_G10 | json | verified | 401 (동일 샘플) |
| DL580 Gen10 | DL560_G10_DL580_G10 | json | verified | 401 (동일 샘플) |
| XL270d Gen10 | XL270d_G10 | json | verified | 401 (동일 샘플) |
| DL360 Gen10 Plus | DL360_G10_Plus | json | verified | 401 (동일 샘플) |
| DL380 Gen11 | DL360_G11_DL380_G11 | json | verified | 확보 불가 |
| DL360 Gen11 | DL360_G11_DL380_G11 | json | verified | 확보 불가 |
| DL560 Gen11 | DL560_G11 | json | verified | 확보 불가 |
| XD220V | XD220V | json | unverified (프로토콜만 verified) | 확보 불가 |

URI: Gen10/Gen10 Plus(iLO 5), Gen11(iLO 6) 모두 /redfish/v1/Systems/1/Bios, /Bios/Settings. OEM 링크(BaseConfigs 등)는 Gen10 은 Bios/*, Gen10 Plus·Gen11 은 Bios/Oem/Hpe/*.
