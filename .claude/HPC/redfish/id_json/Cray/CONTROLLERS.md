# HPE Cray 관리 컨트롤러 체계 (문서 확정본)

사용자 보유 모델: **HPE Cray XD220v** (HPE Cray XD2000 시스템의 1U 2P Intel 노드).
Cray 계열은 "iLO" 가 아니라 제품군마다 BMC 구현이 다르다. 계정 방법은 이 체계를 먼저 확정한 뒤 버전 폴더별로 정리했다.

## 1. 확정된 체계

| 제품군 | 관리 컨트롤러 | 펌웨어 계열 | Redfish | IPMI | 사용자 모델 | 폴더 |
|---|---|---|---|---|---|---|
| HPE Cray XD2000 (XD220v / XD225v / XD295v) | "HPE Cray XD BMC" (문서 표기는 "BMC") | AMI MegaRAC 계열 (AMI OEM Redfish 확장, JViewer KVM, `.hpm` 펌웨어) | json | 지원(문서에 ipmi 로그인 명시) | **XD220V** | `XD2000_BMC/` |
| HPE Cray XD670 (GPU) | "HPE Cray XD670 BMC" | AMI MegaRAC SP-X (HPE 권고문 제목: "Using AMI BMC Redfish API") | json | 지원 | 없음 (인접) | `XD670_BMC/` |
| HPE Cray XD675 (GPU) | "HPE BMC" (Web UI + SSH CLI) | AMI 아님 추정(OpenBMC 스타일 UpdateService/update, SSH 쉘). 제조사 firmware 명 미확인 | json | 지원 | 없음 (인접) | `XD675_BMC/` |
| HPE Cray XD665 | "HPE BMC" (XD220v 가이드와 동일 문구) | 미확인 | json | 지원 | 없음 | 폴더 없음 (자료 부족, SUMMARY 참조) |
| HPE Cray EX / Shasta 액체냉각 (cC / nC / sC) | Cray 내장 컨트롤러 (CMM controller, node controller, switch controller) + CEC | Cray 자체 Redfish (Redis 기반 계정 DB) | json | 해당 없음 | 없음 | `EX_Shasta_Redfish/` |
| HPE Cray EX 공랭 River 노드 | 노드 제조사 BMC: HPE iLO / Gigabyte(AMI) / Intel | 제조사별 | json | 지원 | 없음 | `EX_Shasta_Redfish/` 에 병기 |

## 2. 판정 근거 (문서)
- XD220v 가이드(sd00002298en_us, Edition 4, 2026-07): BMC 로그인은 Web UI / Redfish / ipmi 모두 "서버 라벨의 사용자명·비밀번호". iLO 라는 표기 없음. 펌웨어 업데이트 화면 "Maintenance > Firmware Update", KVM 은 JViewer.
- HPE 공식 도구 `HewlettPackard/CrayXD_PFUT` 의 `XD295v_XD220v_XD225v/Bmc_Update.py`: `Oem.AMIUpdateService`, `/redfish/v1/UpdateService/upload`, `.hpm` 이미지 사용 -> AMI 계열 BMC 로 판단.
- Jev 확인: "XD220v BMC 가 iLO 인가?" -> no 0.98. "AMI 계열인가?" -> yes 0.97.
- XD670 은 Eclypsium/HPE 권고문(HPESBCR04828)에서 AMI MegaRAC Redfish 로 명시.
- EX/Shasta 는 Cray-HPE/docs-csm 문서에서 cC/nC/sC 와 CEC 로 명시.

## 3. 해석상 주의
- XD220V 는 iLO 가 아니므로 `ilorest`, `/redfish/v1/Managers/1/...Oem/Hpe` 경로를 쓰지 않는다.
- AMI 계열이므로 PATCH/PUT 시 조건부 헤더(If-Match 또는 If-None-Match)가 필요할 수 있다 (없으면 HTTP 428).
- 공식 XD2000 BMC Web UI 가이드(dp00002278en_us)는 22MB PDF 라 읽지 못했다. XD2000 전용 계정 슬롯/비밀번호 정책은 AMI 공통 문서에서 유추한 값이며 실기 확인이 필요하다.
