# Cisco UCS 블레이드 BIOS 조사 요약 (문서 기반, 실장비 접속 없음)

| 모델 | 그룹 | 프로토콜 | BIOS 속성 파일 | 상태 |
|---|---|---|---|---|
| UCS B200 M5 | B200_M5_B480_M5 | **xml** (UCSM XML API) | bios_tokens.json (91개) | 프로토콜 verified / 클래스·DN unverified(단일 출처) |
| UCS B480 M5 | B200_M5_B480_M5 | **xml** (UCSM XML API) | 동일 | 동일 |
| UCS X210c M7 | UCS_X210c_M7 | **xml** (UCSM 관리 시) / Intersight(IMM) REST JSON(Redfish 아님) | bios_tokens.json (91개, 부분) | unverified, 목록 불완전 |
| UCS B200 M4 | UCS_B200_M4 | **xml** (UCSM XML API) | 없음 (확보 불가) | 토큰 목록 unknown |

- 공통: UCSM 관리 블레이드에는 문서화된 Redfish 가 없음. Redfish 는 Cisco 문서상 standalone C-Series(IMC 3.0+)에서만 확인됨.
- UCSM XML BIOS 클래스/DN 목록: `ucsm_bios_xml_classes.txt`
