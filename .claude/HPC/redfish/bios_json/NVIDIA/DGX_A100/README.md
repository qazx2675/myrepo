# NVIDIA DGX A100

- 프로토콜 판정: **json** (Redfish). 근거: DGX A100 사용자 가이드, BMC 펌웨어에 Redfish 서버 내장, DSP0266 1.7.0 / Schema 2019.1, 기본 활성, BIOS 설정 및 부팅순서 관리 지원 (jev 1.00)
  - https://docs.nvidia.com/dgx/dgxa100-user-guide/redfish-api-supp.html
- 최신 펌웨어 (DGX A100 FW 릴리스노트, 2026-09-16 갱신): BMC 00.25.02 / SBIOS 1.33
  - https://docs.nvidia.com/dgx/dgxa100-fw-container-release-notes/dgxa100-fw-release-notes.html

## URI 와 검증상태
| URI | 상태 | 비고 |
|---|---|---|
| /redfish/v1/Registries | 가이드(H100/B200/B300) 의 방식을 A100 에 유추 | 같은 계열 문서이며 A100 문서에는 명시 없음. unverified |
| /redfish/v1/Systems/<id>/Bios | **unverified** | A100 문서에 BIOS URI 없음. System ID(Self/DGX/1 등)도 확인 못 함 |
| /redfish/v1/Systems/Self/Bios | **기각** | jev 0.00 (A100 증거에서 확인 안 됨. Lenovo/AMI 문서 유래) |

시도: 1차 (jev) + 재조사(검색 2회) + 합동조사 모두 A100 전용 URI 문서를 찾지 못해 unverified.
BMC 는 AMI MegaRAC 계열로 추정되나 문서 근거는 확보하지 못함(추정, 단정 금지).

## bios_attributes.json 미생성 사유
A100 BIOS Attribute Registry 는 공개되어 있지 않고 BMC 의 `/redfish/v1/Registries` 에서만 얻을 수 있다.

## 구버전 차이 (SBIOS 릴리스노트 기준 요약, 속성 이름 변경은 기록 없음)
- 0.30: 기본값 변경 (Determinism Control=Manual, Determinism Slider=Power, cTDP Control=Manual, cTDP=240, Package Power Limit Control=Manual, Package Power Limit=240, DF Cstates=Disabled)
- 1.18: 미구현 setup 메뉴 항목(User Defaults, Boot NumLock State) 제거
- 1.33 / BMC 00.25.02: BIOS 속성 변경 기록 없음
(old_fw_diff.md 는 이 요약으로 대체; 전체 속성 단위 diff 는 확인불가)

## 그룹 근거
단일 모델.
