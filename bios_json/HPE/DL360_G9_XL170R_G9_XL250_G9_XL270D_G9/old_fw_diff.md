# 구버전 펌웨어 차이 (iLO 4)
- iLO 4 2.00~2.29: Redfish 미적합. `/rest/v1/...` 레거시 REST 만 (HPE "Legacy Rest API is available starting in iLO 4 2.00"). Oem 네임스페이스 `Oem/Hp`.
- iLO 4 2.30+: `/redfish/v1/` 적합. `OData-Version: 4.0` 헤더를 보내면 레거시 속성이 제거된 Redfish 전용 응답, 헤더가 없으면 둘 다 반환. 문서는 비적합 속성이 향후 제거될 수 있다고 안내.
- 속성 단위(추가/삭제/허용값 변경)는 펌웨어/ROM 버전별 레지스트리를 확보하지 못해 **확인불가**. 레지스트리는 ROM 릴리스마다 갱신된다는 HPE 릴리스노트 문구만 확인.
- 예: P89 레지스트리 이름이 ROM 별로 HpBiosAttributeRegistryP89.1.1.30(ROM 2.30) -> 1.1.60(ROM 2.60) 처럼 증가 (HPE 커뮤니티 게시글).
