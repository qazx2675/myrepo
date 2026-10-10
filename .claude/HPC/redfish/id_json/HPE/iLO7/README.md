# HPE iLO 7 - 계정 id/pw 등록·패스워드 변경 (best-effort)

## 프로토콜 판정: json (Redfish AccountService)
- 관리망: HPE iLO 7 (ProLiant Gen12). **사용자 보유 모델 없음** -> 인접 최신 버전 참고용. 실장비 검증 불가, 문서 기반.
- 근거: HPE Redfish 문서 포털 managingusers(iLO 7 언급·예시), iLO 7 Redfish 리소스 정의 페이지(v1.19~1.21 존재), ilorest 가이드(iLO 7 전용 설명).
- 계정 생성/변경/삭제 URI 는 iLO 5/6 과 동일 모델(`/redfish/v1/AccountService/Accounts`). HPE 문서의 최신 예시는 `ManagerAccount.v1_12_1`, `AccountTypes:["WebUI"]`, `Keys`, `PasswordChangeRequired` 포함, Id 예 `65536`~ 대.

## iLO 5/6 과 달라진 점 (HPE 문서)
| 항목 | 내용 | 검증 |
|---|---|---|
| CHIF 제거 | OS-iLO 간 채널 인터페이스(CHIF) 삭제, in-band 는 Virtual NIC(vNIC) 만. vNIC in-band Redfish 는 **항상 인증 필요** | verified (security service 문서) |
| 공장 암호 무인증 변경 불가 | iLO 4/5/6 의 "Production 상태에서 자격증명 없이 in-band 로 Administrator 암호 변경" 방식은 iLO 7 에서 사용 불가 (Production 보안 모드가 없어짐) | verified (HPE 블로그 Note) |
| `RequireHostAuthentication` 제거 | iLO 7 이후 속성 삭제 | verified |
| 애플리케이션 계정 | `/redfish/v1/AccountService/Oem/Hpe/AppAccounts`, 스키마 `HpeiLOAppAccount`. AMS/iSUT/SUM/iLOrest 용. 토큰은 TPM 저장. **AppAccount 세션은 `UserConfigPriv` 없음 -> 일반 계정 생성/수정/삭제 불가** | verified |
| AccountService 타입 | 예시 `#AccountService.v1_15_0.AccountService`, `Oem.Hpe.AppAccounts` 링크 | verified |
| 권한 용어 | `UserConfigPriv`(Administer User Accounts) 로 계정 관리 - 변함 없음 | verified |
| 로컬 인증 끄기 | `ilorest set LocalAccountAuth=Disabled --select AccountService. --commit` (표준 속성) | verified |
| ilorest | 6.0.0.0+ 에서 iLO 7 지원(문서 예시) | verified |

## 방식 요약 (iLO 5/6 과 동일 문서 기준)
| 동작 | 메서드 / URI |
|---|---|
| 생성 | `POST /redfish/v1/AccountService/Accounts/` `{"UserName","Password","RoleId" 또는 "Oem":{"Hpe":{"Privileges":{...}}}}` |
| 암호 변경 | `PATCH /redfish/v1/AccountService/Accounts/{id}/` `{"Password":"..."}` |
| 삭제 | `DELETE /redfish/v1/AccountService/Accounts/{id}/` |
| 활성/비활성 | `PATCH {"Enabled":true|false}` (UserConfigPriv 보유 계정은 자기 자신 포함 가능) |
| 대체 | ilorest `iloaccounts add/changepass/delete`, 웹 UI |

## 미확인(unavailable/unverified) - 실장비에서 확인할 것
- 기본 Administrator 계정/암호 형태(라벨 랜덤 8자 등)와 로컬 계정 슬롯 수(12 여부): iLO 7 사용자 가이드를 읽지 못함 -> **unverified**. `GET /redfish/v1/AccountService` 의 표준 `MinPasswordLength/MaxPasswordLength`, 컬렉션 `Members@odata.count` 로 확인.
- 암호 길이 최대(39?)·복잡도 기본값: iLO 7 리소스 정의 원문을 이번에 읽지 않음 -> **unverified**. `GET /redfish/v1/AccountService` (`MaxPasswordLength`, `Oem.Hpe.EnforcePasswordComplexity`) 로 확인.
- RIBCL/HPONCFG/SSH CLI: iLO 7 지원 문서를 못 찾음 -> **unverified** (CHIF 제거로 hponcfg 로컬 방식은 사실상 불가로 보임, 추정).
- IPMI: 문서 근거 없음 -> **unavailable**.

## 시도 내역 (Jev 생략 - API 키 없음)
- 1차(공식 문서군): HPE Redfish 문서 포털 securityservice.md("Transitioning to HPE iLO 7", Application accounts), managingusers.md, 공장 암호 블로그(iLO 7 불가 노트), ilorest 가이드 iloaccounts 절(+iLO 7 vNIC 서술).
- 2차(공개 코드): 이번 범위에서는 iLO 7 전용 공개 코드·iLO 7 Redfish 레퍼런스 원문 미확보 (iLO 5/6 의 python-ilorest-library 샘플이 동일 API 를 사용한다는 점만 근거).
- 판정: Redfish 계정 CRUD 는 **verified(문서 2중: managingusers + ilorest 가이드)**, 나머지 세부는 unverified.

## 파일
- `create_account.md`, `change_password.md`
