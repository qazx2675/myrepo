# Dell 14G/15G (iDRAC9) 요약

| 모델 | 세대 | 그룹 폴더 | 프로토콜 | URI 검증 | 속성 수 |
|---|---|---|---|---|---|
| R640 | 14G | R640_R740 | json | verified | 965 (14G 레지스트리 합집합, 모델별 미검증) |
| R740 | 14G | R640_R740 | json | verified | 965 (14G 레지스트리 합집합, 모델별 미검증) |
| R840 | 14G | R840 | json | verified | 965 (14G 레지스트리 합집합, 모델별 미검증) |
| C6420 | 14G | C6420 | json | verified | 965 (14G 레지스트리 합집합, 모델별 미검증) |
| DSS8440 | 14G | DSS8440 | json | verified | 965 (14G 레지스트리 합집합, 모델별 미검증) |
| R750 | 15G | R750_R750xa | json | verified | 686 레지스트리/440 노출 (R650 프록시) |
| R750xa | 15G | R750_R750xa | json | verified | 686 레지스트리/440 노출 (R650 프록시) |
| R750xs | 15G | R750xs | json | verified | 686 레지스트리/440 노출 (R650 프록시) |

공통 URI: /redfish/v1/Systems/System.Embedded.1/Bios , .../Bios/Settings , .../Bios/BiosRegistry
한계: 모델별 실측 레지스트리가 아닌 문서(14G, 합집합) 또는 R650 목업(15G 프록시) 기반. 그룹은 BIOS and UEFI Reference Guide 차이 정도로 구분(DSS8440 은 가이드 없음). 정확한 목록은 실기 BiosRegistry GET 으로 확인 필요.
