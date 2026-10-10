# Supermicro X14 / H14 (Gen 14 BMC 펌웨어 01.0x.xx.xx)

- 프로토콜 판정: **json**. Redfish User Guide Rev 6.1 이 Gen 14 를 명시("Supported since Gen 14", 펌웨어 표기 "Gen 14 1.10" 등, X14SAE/X14SBHM 언급). Gen 15 도 문서에 등장(BIOS 와 무관).
- 사용자 보유 모델: 없음. 속성: **unavailable** (`bios_attributes.json` 메타만).
- 최신 펌웨어(피드): X14DBI-T BMC 01.06.04.13 / BIOS 1.5 (보드별로 다름). X14DEI-T 피드는 비어 있음.
- BIOS URI 는 Gen 12/13 과 동일 문서 항목(`Bios`, `Bios/SD`, `ResetBios`, `ChangePassword`, `Registries/BiosAttributeRegistry`; Guide 6.1). verified(문서).
- 시도: Guide 6.1 직접 읽기, 피드. 공개 레지스트리 덤프 없음. 실장비 덤프는 `../X13_H13/README.md` 하단 참조. Jev 생략.
