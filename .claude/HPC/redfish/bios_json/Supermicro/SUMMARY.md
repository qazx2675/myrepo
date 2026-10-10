# Supermicro BIOS 요약 (관리망 BMC 세대별)

조사일 2026-10-10. Jev 생략(키 없음). 폴더는 BMC 세대.

| BMC 세대/폴더 | BMC 펌웨어 계열 | BIOS 프로토콜 | 사용자 보유 모델 | 속성 수 | 상태 |
|---|---|---|---|---|---|
| X10 | 3.xx (최신 3.91) | none (Bios 리소스 없음; XML 은 SUM) | - | 0 | unavailable / 미지원 |
| X11 (+H11) | 1.xx (최신 1.74.x) | json 은 X11DP 만 문서화, 그 외 unverified; xml(SUM) | - | 0 | unavailable |
| X12_H12 | 1.xx (01.06~01.08) | json | - | 0 | unavailable (URI verified) |
| X13_H13 | 01.xx (최신 H13SSH 01.13.11) | json | **AS-1115HS-TNR** (H13SSH) | 158행(UI명, 선택지 64, 기본값 62) / 레지스트리 전체는 0 | unverified(부분), URI verified |
| X14_H14 | 01.0x.xx.xx | json | - | 0 | unavailable (URI verified) |

- 공통 BIOS URI: `Systems/1/Bios`(현재), **`Bios/SD`**(대기, Bios/Settings 아님), `Bios/Actions/Bios.ResetBios`, `Bios.ChangePassword`, `Bios/ChangePasswordActionInfo`, `Registries/BiosAttributeRegistry`. 라이선스 SFT-DCMS-SINGLE.
- PATCH 대상은 가이드 개정에 따라 `Bios`(4.0, FAQ) vs `Bios/SD`(6.1, 405 사례) -> `@Redfish.Settings.SettingsObject` 따를 것 (상세 X13_H13/README.md).
- 속성 ID 규칙: 레지스트리는 `QuietBoot_0027` 형태, PATCH 예시는 `QuietBoot`, FAQ 는 `BootOption#1$3` -> 장비 레지스트리 확인 필수.
- 세대 간 diff: X11/diff_vs_X10.md, X12_H12/diff_vs_X11.md, X13_H13/diff_vs_X12_H12.md, X14_H14/diff_vs_X13_H13.md (API 수준만).
- 저장소 `testdata/supermicro-x12` 의 속성명(Hyper-Threading[ALL] 등)은 mock 이며 증거 아님.
- 이전 `back/Supermicro/AS-1115HS-TNR` 정정: 속성 파일을 "전부 실패"가 아니라 매뉴얼 기반 부분 목록으로 생성(명시적 unverified), 액션 URI 출처 1곳 -> 문서 3종.
