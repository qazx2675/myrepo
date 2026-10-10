# SSONIC 조립서버 (공개 자료 없음)

대상 모델: CSRL18A, CSPR17C, CSRL20A, CSBA20B, SO101A2 G12, CSRL17B, CSBA18B, CSRL19A, SO103A1G12, SO101A1G12, SO102A1G12

- 프로토콜 판정: **unknown** (전 모델). 영어/한국어 웹검색 4회(모델명, 제조사명 조합)에서 SSONIC 또는 해당 모델에 대한 공식 사양·매뉴얼·BMC 정보가 한 건도 나오지 않았다.
- 메인보드/BMC 제조사: **unknown**. 근거가 없어 AMI/ASPEED 등으로 추정하지 않는다.
- Redfish URI, 속성, 펌웨어, 구버전 차이: 전부 확인불가. jev 는 증거 텍스트가 없어 실행하지 않음.
- 그룹: 동일 JSON 사용 여부를 판단할 근거가 없어 모델별 분리 불가, 묶음도 불가. 임시로 한 폴더에 모두 기록한 것이며 그룹이 아님.
- 모델명 규칙에서 유추 가능한 것(참고, 미검증): CSxx/SOxxx 접두와 숫자는 세대/플랫폼 코드일 수 있으나 공개 매핑 없음.

실제 장비에서 `GET /redfish/v1/` (서비스 루트의 Vendor/Product), `/redfish/v1/Systems`, `/redfish/v1/Registries` 및 BMC 웹 UI 의 펌웨어 정보로 확인 필요. 보드 모델이 확인되면 해당 보드 제조사(예: Supermicro, ASRock Rack, Gigabyte 등) 그룹에 편입.
