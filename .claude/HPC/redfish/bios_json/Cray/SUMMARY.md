# HPE Cray: BIOS 조사 요약 (관리망 버전별)

Cray XD 계열은 iLO 를 쓰지 않는다. 제품군마다 BMC 가 다르고, 전체 체계는 `id_json/Cray/CONTROLLERS.md` 를 따른다.

| 관리망 버전 | 프로토콜 | 해당 사용자 모델 | BIOS URI | 속성 수 | 상태 | 폴더 |
|---|---|---|---|---|---|---|
| XD2000_BMC (HPE Cray XD BMC, AMI MegaRAC 계열, ODM Inventec) | json (Redfish) | **XD220V** | `/redfish/v1/Systems/Self/Bios`, `.../Bios/SD` | 0 (unavailable) | URI verified (AMI 공통 + HPE Cray XD 언급, XD220v 실장비 응답은 미확인) / Actions·Registry 파일명 unverified / 속성 unavailable | `XD2000_BMC/` |
| XD670_BMC (AMI MegaRAC SP-X, 이중 BIOS·BMC) | json | 없음 | 조사 안 함 | - | 범위 밖 (사용자 보유 없음). PFUT 로 확인한 것은 `FirmwareInventory/BIOS`, `BIOS2` 뿐 | 폴더 없음 |
| XD675_BMC | json | 없음 | 조사 안 함 | - | 범위 밖 (사용자 보유 없음). PFUT 대상도 아님 | 폴더 없음 |

## 핵심
- XD220V 의 BIOS 는 AMI Aptio(`CU2K_5.xx_vX.XX`)다. 공개 자료에서 확인한 가장 최신 값은 `CU2K_5.32_v3.80` (Ubuntu 인증 목록).
- BIOS 설정은 AMI 방식으로 한다. `Systems/Self/Bios` 는 읽기 전용이고, 변경은 `Bios/SD` 에 PATCH(`If-Match`)한 뒤 재부팅해야 적용된다.
- HPE 는 XD2000 BIOS Attribute Registry 를 공개하지 않았다. 다른 AMI 플랫폼의 속성명을 그대로 쓸 수도 없다: Gigabyte/DGX 는 `TCG001`·`IPMI002` 같은 코드형, Lenovo SR635 는 `Q00001_Boot_Mode` 형이다. 그래서 속성은 실장비에서 `GET /redfish/v1/Systems/Self/Bios` 와 레지스트리를 받아서 채워야 한다 (방법: XD2000_BMC/README.md 3절).
- 버전 간 diff: 비교할 데이터가 없어 만들지 않았다.
- 모델 매핑 정정: 이전(back/HPE/XD220V)에는 HPE 로 분류됐고 iLO 여부가 unknown 이었다. 이번에 Cray / XD2000_BMC (iLO 아님, AMI) 로 정정하고 시스템 ID 를 `Self` 로 확정했다.
- Jev 판정은 생략했다 (키 없음).
