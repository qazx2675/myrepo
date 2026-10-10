# HPE ProLiant DL560 Gen8 (iLO 4)

## 프로토콜 판정: json (Redfish 서비스만) / BIOS 속성은 none
- Gen8 + iLO 4 2.00 이상은 RESTful API 제공 (HPE 백서: Gen8, Gen9, Gen10 의 iLO 4 2.00+). iLO 4 2.30+ 에서 `/redfish/v1/` 적합. jev "Gen8 에서 REST API 제공" 0.94.
- **그러나 BIOS 설정 리소스는 Gen8 에서 미지원**: "the UEFI BIOS configuration features ... is not available on Gen8 servers" (https://github.com/shamimur/hp-proliant-sdk/wiki/iLO-4-REST-API-Data-Model, 3자 위키, 구문서). jev "Gen8 BIOS 읽기 불가" 0.92 (verified 기준 jev 충족). Gen8 은 UEFI 가 아닌 레거시 RBSU BIOS 라서 Bios 리소스가 없는 것으로 보임(배경지식, 문서 직접 확인 못함).
- 독립 2번째 출처(HPE 공식 문서)에서 "Gen8 BIOS 미지원" 문구를 확인하지 못함 -> **unverified**. HPE iLO 4 공식 소개문은 "RESTful API first released with iLO 4 2.00 on Gen9", BIOS configuration 에 세대 제한을 명시하지 않음.

## BIOS URI
- `/redfish/v1/Systems/1/Bios`: DL560 Gen8 에서 **없을 가능성이 높음**(404 예상). unverified. 실장비 GET 으로 확인 필요.
- bios_attributes.json: 만들지 않음 (사유: Gen8 BIOS 리소스 미지원, 속성 레지스트리 확보 불가).

## 펌웨어
- DL560 Gen8 System ROM 패밀리 P77 (SPP 2019.05.24 빌드 확인), iLO 4 최신 2.82 (Gen9 와 공용 펌웨어 라인, DL560 Gen8 지원 여부 직접 확인 못함 -> unknown).
- 기존 도구 판단: BIOS 점검 시 이 모델은 UNSUPPORTED/키 없음 처리 권장.
