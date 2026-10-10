# UCS B200 M4

## 프로토콜: **xml** (UCSM XML API). Redfish 미문서화.
- 근거: 위 M5 그룹과 동일(UCSM 은 GUI/CLI/XML API). M4 블레이드 전용 Redfish 문서 없음.

## BIOS 속성: unknown (bios_tokens.json 미생성)
- Cisco 토큰 레퍼런스 목록에는 M4 전용 문서가 없고(3.2 M5 가이드, 4.0~6.0 가이드), 4.2 가이드는 "M4/M5 서버는 4.1 가이드 참조"라고 안내하나 4.1 가이드 블레이드 표는 M5 계열(B200 M5, B480 M5 등)만 열거함. 웹으로 M4 토큰 표를 확보하지 못했고 추측하지 않음.
- 참고: UCSM BIOS 클래스 전체(ucsm_bios_xml_classes.txt)에 M3/M4 세대 토큰(예 biosVfDDR3VoltageSelection, biosVfQPISnoopMode)이 존재하나 모델별 적용 여부는 unknown.
- 조회 방법(권장, 실기 필요): UCSM `configResolveClass classId="biosSettings"` 로 서버별 토큰 확인, 또는 UCSM GUI Servers > Policies > BIOS Defaults.
