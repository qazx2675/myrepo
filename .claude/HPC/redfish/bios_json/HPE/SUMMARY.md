# HPE BIOS 요약 (관리망 버전별) — 2026-10-10

| 관리망 | 프로토콜 | 해당 사용자 모델 | 속성 수 | 최신 FW | 상태 |
|---|---|---|---|---|---|
| iLO4 | json (Redfish 2.30+, 2.00~2.29 는 /rest/v1 만) | DL360 G9, XL170r G9, XL250 G9, XL270d G9 (DL560 G8 은 UNSUPPORTED.md) | 237 | 2.82 (EOL) | URI verified / 속성 unverified |
| iLO5 | json | DL360 G10, DL560 G10, DL580 G10, XL270d G10, DL360 G10 Plus | 763 (합집합) | 3.21 | URI verified / 속성 unverified (U46 이름 수준 verified, U32/U45 unavailable) |
| iLO6 | json | DL360 G11, DL380 G11 (U54), DL560 G11 (U59) | 647 | 1.79 | URI verified / type·허용값 unavailable |
| iLO7 | json | 없음 (best-effort) | 721 | 1.25.00 | URI verified / type·허용값 unavailable |

## 핵심 사실
- iLO5 3.19 / iLO6 1.79 / iLO7 1.25 의 Bios 정의 문서는 같은 AMD 샘플 복사본이라 문서 비교로는 버전 차이를 알 수 없다. 실제 차이는 ROM 패밀리 레지스트리에 있다.
- 같은 iLO5 안에서도 U34 235개 vs U46 276개, 공통 154개.
- Bios OEM URI: iLO4 links.* -> iLO5 Gen10 Bios/<baseconfigs|boot|...> -> Gen10 Plus(2.33+)/iLO6/iLO7 Bios/Oem/Hpe/<...>.
- ResetBios: iLO4 없음 / iLO5 Bios/Settings/Actions / iLO6·7 Bios/Actions.
- iLO4 열거값은 숫자 그대로, iLO5+ 는 접두어.
- DL560 Gen8 은 공식 문서상 BIOS Redfish/REST 미지원 (verified-by-docs, 실장비 404 직접 확인은 아님).
- 최신 FW 중 iLO6 1.79, iLO7 1.25.00 은 포털 기준이며 Wikipedia(1.78, 1.24.00)와 한 단계 다름 -> unverified.
- Jev 생략, 공식 문서 + 독립 2번째 출처로만 verified 판정.
