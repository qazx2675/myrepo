# Lenovo XCC2 - 계정 id/pw 등록·패스워드 변경

## 프로토콜 판정: json (Redfish AccountService)
- 관리망: Lenovo XClarity Controller 2 (XCC2). ThinkSystem/ThinkAgile/ThinkEdge V3 서버. Redfish 1.20.0 지원(Lenovo Press LP1800), IPMI 2.0.
- 근거 문서: https://pubs.lenovo.com/xcc2-restapi/ (Account management resources)
- 해당 사용자 보유 모델 (Lenovo Press LP1800 서버 지원 표): **SR645 V3, SR675 V3, SD650 V3**
- 사용자 모델 중 지원불가(Redfish 계정 API 없음)는 없음.

## 방식 요약
| 동작 | 메서드 / URI | 성공/비고 |
|---|---|---|
| 생성 | POST /redfish/v1/AccountService/Accounts | UserName, Password, RoleId 필수 |
| 암호 변경 | PATCH /redfish/v1/AccountService/Accounts/{1..12} `{"Password":"..."}` | 응답 Password=null |
| 삭제 | DELETE /redfish/v1/AccountService/Accounts/{id} | 204 (XCC1 Whitley/XCC3 문서 기준. XCC2 삭제 페이지는 직접 확인 못함 - 추정) |
| 정책 | GET/PATCH /redfish/v1/AccountService | 잠금/최소길이 |
| 역할 | /redfish/v1/AccountService/Roles | 커스텀 롤 POST |

XCC1 Purley 와 달리 슬롯을 PATCH 로 채우지 않고 POST 로 생성한다 (XCC2 문서에는 Purley 구분 없음).

## URI 및 검증 상태
| 용도 | URI | 검증 | 시도 |
|---|---|---|---|
| 계정 생성 | POST /redfish/v1/AccountService/Accounts | verified (jev yes 0.99-1.00 + XCC/XCC3 동일 페이지 + 샘플 스크립트) | 1차 |
| 암호 변경 | PATCH /redfish/v1/AccountService/Accounts/{1..12} | verified (jev yes 1.00 + XCC1/XCC3 동일 페이지) | 1차 |
| 계정 속성 조회 | GET /redfish/v1/AccountService/Accounts/{1..12} | verified (문서) | 1차 |
| 컬렉션 | GET /redfish/v1/AccountService/Accounts (예시에 `HostBootStrap` 멤버 포함) | verified (문서) | 1차 |
| AccountService | GET /redfish/v1/AccountService | verified (문서) | 1차 |
| 롤 | GET /redfish/v1/AccountService/Roles/{Administrator,Operator,ReadOnly,CustomRoleN}, POST .../Roles | verified (문서) | 1차 |

## 문서 불일치/특이사항
- 오류코드: 500 InternalError, 400 에 CreateFailedMissingReqProperties, PropertyValueTypeError, PropertyValueFormatError, ResourceChangeRequried(문서 철자), NotRecommandedOperation(문서 철자), ForbiddenOperation, ResourceAlreadyExists, PropertyMissing, PasswordChangeRequired. 문서는 ID 별 상태코드를 개별 매핑하지 않음(400 은 추정).
- AccountService: `MinPasswordLength` 기본 10(문서), `MaxPasswordLength` 255 고정(문서). 반면 XCC2 사용자 가이드의 복잡도 규칙은 8~32자. 실제 적용값은 장비 GET 으로 확인.
- 계정 예시 응답 `@odata.type #ManagerAccount.v1_8_1.ManagerAccount`, `AccountTypes` 허용값 Redfish/SNMP/ManagerConsole/IPMI/WebUI, `HostBootstrapAccount:false`.

## 파일
- `create_account.md`, `change_password.md`

## 한계
- 문서 기반, 실제 장비 응답 코드 확인 없음. 성공 코드는 문서 미표기 항목이 있어 샘플 스크립트 기준(200/201/204)을 병기.
