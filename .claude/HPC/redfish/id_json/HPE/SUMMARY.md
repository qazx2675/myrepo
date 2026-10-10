# HPE iLO 계정 id/pw 등록·패스워드 변경 요약

모든 iLO 버전이 Redfish(JSON) `AccountService` 로 계정 CRUD 가능 (iLO 4 는 2.30+ Redfish, 이전은 `/rest/v1`). XML 은 RIBCL(HPONCFG/HPQLOCFG) 이 별도 존재. 버전별 상세: `iLO4/`, `iLO5/`, `iLO6/`, `iLO7/` 의 `README.md`, `create_account.md`, `change_password.md`.
검증 방식: Jev API 키 없음 -> Jev 생략, HPE 공식 문서(RESTful API 레퍼런스·User Guide·Redfish 포털) + 독립 2차(python-ilorest-library, ilorest 가이드, SaltStack 소스 등) 이중 확인.

| 버전 | 사용자 모델 | 생성 | 암호 변경 | 삭제 | OEM 키 | 슬롯 | 검증 |
|---|---|---|---|---|---|---|---|
| iLO 4 (Gen8/Gen9) | DL360 G9, XL170r G9, XL250 G9, XL270d G9, DL560 G8(*) | `POST /redfish/v1/AccountService/Accounts/` (구 `/rest/v1/...`) privileges 로 생성 | `PATCH .../Accounts/{id}` `{"Password"}` | `DELETE` | `Oem.Hp` (권한 6개) | 12 | verified (G8 Redfish 는 unverified) |
| iLO 5 (Gen10/10+) | DL360 G10, DL560 G10, DL580 G10, XL270d G10, DL360 G10 Plus | 동일 URI, `RoleId` 또는 privileges | 동일 | 동일 | `Oem.Hpe` (권한 10개) | 12 | verified |
| iLO 6 (Gen11) | DL360 G11, DL380 G11, DL560 G11 | 동일 | 동일 | 동일 | `Oem.Hpe` (10개) | 12 | verified |
| iLO 7 (Gen12) | 없음 (best-effort) | 동일 URI | 동일 | 동일 | `Oem.Hpe` + AppAccounts | unverified | CRUD verified(문서) / 세부 unverified |

(*) DL560 G8 은 iLO 4 이나 Gen8. RIBCL·SSH CLI·웹 UI 는 가능, Redfish/REST 계정 API 의 Gen8 지원은 HPE 문서가 "Gen9, iLO 4 2.00+" 로 기술해 공식 확인 못함. 실장비 `GET /redfish/v1/AccountService` 로 판별.

## 공통 규칙
- 권한: `UserConfigPriv`(Administer User Accounts). `iLOConfigPriv` 는 계정 관리 불포함. 비관리자도 웹 UI 에서 자기 암호는 변경 가능(iLO 4 UG).
- 용어 반전: Redfish `UserName` = 웹 UI Login Name(로그인 ID), `Oem/Hp(e)/LoginName` = 웹 UI User Name(설명).
- 기본 계정: `Administrator`, 암호는 serial label pull tab 의 랜덤 8자 (iLO 5/6 은 주문 SKU 에 따라 공통 기본 암호; iLO 6 공통 암호 SKU P08040-B21). 보통 Id `1` 이나 `UserName` 으로 확인.
- 암호: 최대 39자(iLO 4/5/6), 최소 `MinPasswordLength` 0~39 기본 8, 복잡도 강제는 기본 off(iLO 5 1.40+ `EnforcePasswordComplexity`; iLO 4 는 권고만). IPMI 계정은 로그인 16자/암호 20자 이하. 사용자명·LoginName 39자.
- 슬롯: 로컬 12개(iLO 4/5/6 UG). 초과는 디렉터리 서비스. Id 는 `1..12` 또는 `65536` 대(문서 예시 모두 존재) -> 항상 컬렉션 GET.
- 생성 방식: 모든 버전 POST. iLO 4 는 privileges 만, iLO 5+ 는 RoleId 도 가능(미지정 시 ReadOnly). `RoleId` PATCH 는 기존 권한을 role 권한으로 초기화.
- 대체 수단:
  - ilorest `iloaccounts add USERNAME LOGINNAME PASSWORD [--role R | --addprivs 1,..]` / `changepass ID|USER PW` / `delete` - verified (iLO 4 지원, `--role` 및 권한 7~10 은 iLO 5+).
  - RIBCL `ADD_USER`/`MOD_USER`/`DELETE_USER` (`hponcfg` 로컬, `hpqlocfg`/`locfg.pl` 원격) - 구조 verified(Salt 소스), 원문 가이드 못 읽음. iLO 6 부터 sustenance stage. RL3xx Gen11 은 미지원(사용자 모델 아님).
  - SSH CLI `create /map1/accounts1 username=.. password=..`, `set /map1/accounts1/<USER> password=..` - iLO 4 2.55 실사례 verified, iLO 5/6 unverified.
  - ipmitool 로 iLO 계정 관리: HPE 문서 근거 없음 -> unavailable. IPMI over LAN 은 기본 Disabled.
  - 웹 UI, UEFI iLO x Configuration Utility(User Management).
- 암호 분실 복구: iLO 4/5/6 은 OS 에서 ilorest/hponcfg in-band(Production 보안 상태, 자격증명 불필요)로 변경 가능(HPE 블로그, 명령은 그림이라 텍스트 확인 못함 - unverified). iLO 7 은 불가(CHIF 제거). 공장 초기화는 모든 설정/계정 삭제.

## 버전 간 큰 차이
1. iLO 4: `Oem.Hp`, 권한 6개, RoleId 생성 없음, 복잡도 토글 없음, 기본 계정 사전지정 속성 없음.
2. iLO 5+: `Oem.Hpe`, 권한 10개, RoleId, EnforcePasswordComplexity(1.40+), Default 계정 사전지정(1.17+), ServiceAccount.
3. iLO 6: iLO 5 와 같은 모델, RIBCL sustenance.
4. iLO 7: CHIF 제거(vNIC 인증 필수), 앱 계정(AppAccounts), 무인증 in-band 암호 변경 불가, RequireHostAuthentication 삭제.

## 미확인·실패 항목
- HPE iLO 4/5/6 Scripting and Command Line Guide(RIBCL 파라미터 원문, SSH CLI 권한 그룹 문법) 접근 차단(403) -> unverified.
- python-redfish-utility(ilorest) 소스 raw 404 -> ilorest 공식 가이드로 대체.
- 정확한 HTTP 성공 코드, 암호 변경 후 세션 무효화, If-Match 필요 여부 unverified.
- iLO 7 슬롯 수·암호 정책·기본 계정·RIBCL/CLI 지원 unverified.
- 사용자 모델 매핑 정정 없음. DL560 G8 만 Redfish 지원 확인 불가.
