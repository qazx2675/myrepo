# HPE XD220V (HPE Cray XD220v)

- 모델: XD220V (Cray XD2000 1U 노드)
- 프로토콜 판정: **json** (Redfish) - 근거: HPE Cray XD2000 제품 페이지에 "DMTF Redfish 지원" 명시 https://buy.hpe.com/fi/en/compute/cray-systems/cray-supercomputer/hpe-cray-xd2000/p/1014691449
- 관리 컨트롤러: "HPE Cray XD BMC" (iLO 로 명시된 자료 없음, iLO 5/6 여부 unknown)
- 최신 펌웨어: unknown (XD220v 전용 릴리스 노트 미발견)

## URI 검증
| URI | 상태 | 비고 |
|---|---|---|
| /redfish/v1/Systems/<id>/Bios | unverified | jev "XD220v URI 확정?" no 1.00 - XD220v 전용 문서 없음. Redfish 표준 경로 추정이며 id 도 미확인 |
| 프로토콜 json | verified | 1차 yes 0.78 (미달), 재조사 yes 0.99 + XD2000 제품 페이지(Redfish 지원 명시) |

## BIOS 속성 파일
**bios_attributes.json 미생성(확보 불가)**: XD220v 용 BIOS 레지스트리/속성 문서를 공개 자료에서 찾지 못함. iLO 문서의 속성 목록을 적용할 근거 없음. unknown.

## 묶은 근거
단독 모델(다른 HPE 모델과 BMC 가 달라 묶지 않음).

## 구버전 차이
확인불가.
