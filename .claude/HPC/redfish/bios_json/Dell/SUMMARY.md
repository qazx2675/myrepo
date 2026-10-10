# Dell BIOS 요약 (관리망 버전별)

조사일 2026-10-10. Jev 판정 생략(키 없음). verified = 공식 원본/문서 + 독립 2차 출처.
이번 결과는 **실장비 Redfish 원본 JSON(BiosRegistry/Bios)을 스크립트로 변환**한 것이다. 이전 `back/Dell` 의 PDF 요약 기반 목록(965/686/1431)을 대체한다.

## 버전 → 프로토콜 → 사용자 모델 → 속성 수 → 상태

| 관리망 | 프로토콜 | 세대·파일 | 사용자 보유 모델 | 기준 캡처 (iDRAC / BIOS) | 속성 수 (레지스트리 / /Bios 노출) | 상태 |
|---|---|---|---|---|---|---|
| iDRAC8 | json (BIOS 는 ≥2.50.50.50. 그 미만은 xml(WS-MAN)/racadm, SCP 는 ≥2.40.40.40) | 13G `iDRAC8/bios_attributes.json` | 없음 (비교 기준용) | R630 2.81.81.81 / 2.13.0 | 249 / 249 | verified (원본). 다른 13G 모델은 unverified |
| iDRAC9 | json | 14G `iDRAC9/bios_attributes_14g.json` | R640, R740, R840, C6420, DSS8440 | R740xd 7.00.00.182 / 2.24.0 | 641 / 338 | 대리(R740xd). 5개 모델 모두 unverified |
| iDRAC9 | json | 15G `iDRAC9/bios_attributes_15g.json` | **R750**, R750xa, R750xs | **R750 7.10.30.00 / 1.13.2** (+R650 7.10.30, R750 6.00.30) | 687 / 454 | **R750 verified**, R750xa·R750xs unverified |
| iDRAC9 | json | 16G Intel `iDRAC9/bios_attributes_16g_intel.json` | **R760XA**, XE9680, R660, R860, C6620 | **R760xa 7.10.50.00 / 2.1.3** (+R760 동일, R660xs 7.20.10.05 / 2.4.4) | 865 (합집합 877) / 538 | **R760XA verified**, 나머지 unverified |
| iDRAC9 | json | 16G AMD `iDRAC9/bios_attributes_16g_amd.json` | R6615 | R7615 7.00.60.00 / 1.6.10 (/Bios 만) | — / 487 | partial: 이름 verified, type·허용값 unavailable |
| iDRAC10 | json | 17G Intel `iDRAC10/bios_attributes.json` | XE9780 | R770 1.10.17.00 / 1.2.4 | 839 / 499 | 대리(R770). XE9780 unverified |

URI 는 iDRAC8(≥2.50)·iDRAC9·iDRAC10 모두 같다: `/redfish/v1/Systems/System.Embedded.1/Bios`, `/Bios/Settings`, `/Bios/BiosRegistry`, `/Registries/BiosAttributeRegistry.v1_0_0`, `Bios.ResetBios`, `Bios.ChangePassword`, `/Bios/Settings/Actions/Oem/DellManager.ClearPending` — 모두 verified.
SCP: iDRAC8(≥2.40)·iDRAC9 는 `.../Managers/iDRAC.Embedded.1/Actions/Oem/EID_674_Manager.{Export,Import}SystemConfiguration`, **iDRAC10(17G)은 `.../Actions/Oem/OemManager.{Export,Import}SystemConfiguration`** — verified.

## 세대 간 큰 차이 (원본 레지스트리 비교)

| 비교 | 추가 | 삭제 | 허용값 변경 | 파일 |
|---|---|---|---|---|
| 14G(R740xd) vs 13G(R630) | 400 | 8 | 25 | `iDRAC9/diff_vs_iDRAC8.md` |
| 15G(R750) vs 14G(R740xd) | 144 | 98 | 44 | `iDRAC9/diff_15g_vs_14g.md` |
| 16G(R760xa) vs 15G(R750) | 190 | 12 | 53 | `iDRAC9/diff_16g_vs_15g.md` |
| 16G AMD(R7615) vs 16G Intel(R760xa), 노출 이름 | AMD에만 89 | Intel에만 38 | — | `iDRAC9/diff_16g_amd_vs_16g_intel.md` |
| iDRAC10 17G(R770) vs iDRAC9 16G(R760xa) | 27 | 53 | 68 | `iDRAC10/diff_vs_iDRAC9.md` |

같은 모델에서 iDRAC 펌웨어만 바뀐 경우(R750 6.00.30 → 7.10.30)는 +5/−0 이었다. BIOS 속성은 iDRAC 버전보다 **BIOS 버전·세대·CPU 벤더**가 결정한다(`iDRAC9/old_fw_diff.md`).

## 이전 결과(back/) 대비 정정

- 14G: "965개(AR 가이드 합집합)" → R740xd 실레지스트리 641개(노출 338).
- 15G: "R650 대리 686/440" → 사용자 모델 R750 실레지스트리 687/454 (R650 은 보조).
- 16G: "1431개(AR 문서 상위집합, Intel/AMD 혼합)" → Intel R760xa 865/538 과 AMD R7615 노출 487 로 분리.
- 잘렸던 값 복구: `DcuIpPrefetcher` = Enumeration [Enabled, Disabled], `SetupPassword` = Password(0~32자, `^[\x20-\x7E]{0,32}$`, WriteOnly).
- `Bios/BiosRegistry`(16G): unverified → verified. iDRAC10 Bios URI: unverified(0.99) → verified.
- default: Dell 레지스트리에는 DefaultValue 필드가 없음을 원본에서 확인했다(이전처럼 "문서에 없음"이 아니라 Redfish 원본에도 없음).
- 사용자 모델 매핑 정정 사항: 없음(모델별 관리망 버전은 iDRAC9/iDRAC10 으로 맞음). 단 R6615 는 같은 iDRAC9 라도 AMD 라서 Intel 16G 파일을 쓰면 안 된다 → 별도 파일로 분리했다.
