# X13/H13 vs X12/H12

속성 단위 diff: **불가** (X12/H12 목록 없음, X13/H13 는 H13SSH UI 이름 부분 목록만). 문서 기준 Gen 13 에서 추가·변경된 BIOS 관련 항목 (Redfish User Guide Rev 6.1):
- FixedBootOrder: "Redfish 1.11 (2020.3), Gen 13 및 이후".
- Self-Encrypted Drive: Bios/SD 의 `KMSSecurityPolicy`, `TPMSecurityPolicy`, `Super_GuardiansProtectionPolicy` ("supported since Gen 13", SFT-DCMS-Single).
- Boot Option `UefiDevicePath`: Gen 13 1.07 이상.
- Secure Boot 데이터베이스: "Supported since X13/H13".
- 레거시 OEM FirmwareInventory 업데이트 엔드포인트는 X12/H12 지원, X13/H13 부터 deprecated.
- 계정: IPMI/Redfish 계정 분리 (BMC 01.05.xx 부터, Gen 13) -- BIOS 와는 무관하나 자동화 계정 영향.
