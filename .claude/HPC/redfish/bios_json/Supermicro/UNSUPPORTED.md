# Supermicro: Redfish BIOS 미지원/제한

| 세대 | 상태 | 사용자 보유 모델 | 대안 |
|---|---|---|---|
| X10 (BMC 3.xx) | Redfish 는 있으나 Bios 리소스 없음 (Guide 2.0a) | 해당 모델 없음 | SUM `GetCurrentBiosCfg`/`ChangeBiosCfg` (XML) |
| X11/H11 | Bios 리소스는 X11DP 만 문서화 (2019); 나머지 unverified | 해당 모델 없음 | 위와 동일 |

사용자 보유 AS-1115HS-TNR(X13/H13 계열, H13SSH)은 Redfish BIOS 지원 세대이므로 미지원 아님. 단 BIOS API 는 SFT-DCMS-SINGLE 라이선스 표기가 있어 장비에서 라이선스 확인 필요(unverified).
