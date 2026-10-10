# HPE Cray: Redfish BIOS 미지원 범위

| 관리망 버전 | 미지원 펌웨어 범위 | 해당 사용자 모델 | 판정 |
|---|---|---|---|
| XD2000_BMC | **unknown**. Redfish BIOS 를 지원하지 않는 BMC/BIOS 버전 범위를 명시한 공개 자료가 없다 | XD220V (미지원으로 확인된 바 없음) | unknown |

## 근거와 주의
- XD2000 은 처음 출시될 때부터 Redfish 를 지원한다 (QuickSpecs "DMTF Redfish", BMC Web UI 가이드 초판 2023-08). Redfish 를 아예 쓸 수 없는 XML 전용이나 none 버전은 확인되지 않았다.
- AMI MegaRAC 레퍼런스에 따르면 Redfish BIOS 설정은 **BIOS 에 AMI BIOS REST/Redfish 모듈이 있고 BMC Host Interface 가 켜져 있을 때만** 동작한다. XD220v BIOS 의 어느 버전부터 이 모듈이 들어갔는지는 공개 자료로 확인할 수 없었다. 오래된 BIOS(예: 2022년 CU2K_5.27_v0.81)에서 `Systems/Self/Bios` 의 `Attributes` 가 비어 있으면 BIOS 를 갱신하고 Host Interface 를 켠 뒤 다시 확인한다.
- 다음 응답은 미지원을 뜻하지 않는다: `Bios/SD` 가 404 (대기 중인 변경 없음), 호스트가 부팅 중일 때 PATCH/Action 이 503.
- 확인 방법: `GET /redfish/v1/Systems/Self` 에서 `BiosVersion` 과 `Bios` 링크가 있는지 보고, `GET /redfish/v1/Systems/Self/Bios` 에서 `Attributes` 개수와 `AttributeRegistry` 를 본다.
- XD670 과 XD675 는 사용자가 보유하지 않아 조사하지 않았다.
