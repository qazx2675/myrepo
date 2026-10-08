# UCS X210c M7 (X-Series 컴퓨트 노드)

## 프로토콜: **xml** (UCSM 관리 모드 시 UCSM XML API) / Intersight Managed Mode 시 Intersight REST API(JSON, `bios.Policy`; Redfish 아님)
- UCSM 관리: Cisco "UCS Server BIOS Tokens" 4.3 가이드가 X210c M7 을 UCSM 토큰 표 대상으로 기재 (https://www.cisco.com/c/en/us/td/docs/unified_computing/ucs/ucs-manager/Reference-Docs/Server-BIOS-Tokens/4-3/b-ucs-bios-tokens-guide-4_3/m-ucs-server-bios-tokens-4-3.html). UCSM 은 XML API (릴리스 노트 근거).
- IMM(Intersight): 설정은 Intersight `bios.Policy` (https://developer.cisco.com/docs/intersight/read-a-bios-policy-resource ), 토큰 가이드 https://www.cisco.com/c/en/us/td/docs/unified_computing/ucs/Intersight/IMM_BIOS_Tokens_Guide/b_IMM_Server_BIOS_Tokens_Guide.html . IMM 아키텍처가 Redfish 기반이고 CIMC 에 Redfish 서버가 있다는 Cisco Live 자료가 있으나, 고객 대상 BIOS Redfish URI 문서는 찾지 못함 -> **Redfish BIOS 경로: unknown**.
- jev: 프로토콜 판정 yes=1.00(B200 M5 증거 기준), X210c M7 별도 판정 안 함.

## 상태: unverified / 목록 부분
- bios_tokens.json (91개): UCSM 4.3 가이드의 X210c M7 표(4.3(2b) 기준 행 중 "Panic and High Watermark"까지 + 이후 릴리스 신규/변경 행) + 6.0 가이드의 X210c M7 해당 행(GPU Direct CPU1 and CPU2, IIO eDPC Support). **4.3(2b) 표의 "Memory Thermal Throttling Mode" 이후 행과 6.0 가이드 중 기본 목록 전체는 도구 한계(페이지 잘림)로 확보 못함 -> 전체 목록 아님, unknown 부분 존재.**
- XML 클래스/DN: ../ucsm_bios_xml_classes.txt (일반 UCSM BIOS 클래스; M7 신규 토큰의 클래스명은 unknown).
- 그룹: X210c M7 은 M5 와 토큰 이름/값이 다르므로(SGX/TDX 등) 별도 그룹. X410c M7 과 같은 표를 쓰는 것으로 보이나 비교 미수행.
