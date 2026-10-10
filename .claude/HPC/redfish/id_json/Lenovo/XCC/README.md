# Lenovo XCC (XCC1) - 계정 id/pw 등록·패스워드 변경

## 프로토콜 판정: json (Redfish AccountService)
- 관리망: Lenovo XClarity Controller (XCC1, Pilot4 기반). Redfish JSON 으로 계정 관리 가능.
- 근거 문서: Lenovo XCC REST API 레퍼런스 (https://pubs.lenovo.com/xcc-restapi/ - Account management resources)
- 이 버전에 해당하는 사용자 보유 모델 (Lenovo Press LP0880 "XCC support on ThinkSystem servers" 서버 지원 표 확인):

| 모델 | XCC | 플랫폼 | 계정 생성 방식 | 삭제 방식 |
|---|---|---|---|---|
| SR630 (7X01/7X02) | XCC1 | Intel Purley (Xeon SP Gen1/Gen2) | **PATCH 빈 슬롯** | PATCH `UserName:""` |
| SR650 (7X05/7X06) | XCC1 | Intel Purley | **PATCH 빈 슬롯** | PATCH `UserName:""` |
| SD530 (7X21) | XCC1 | Intel Purley | **PATCH 빈 슬롯** | PATCH `UserName:""` |
| SR630 V2 (7Z70/7Z71) | **XCC1** (XCC2 아님 - LP0880 이 SR630 V2 를 XCC1 로 표기) | Intel Whitley (Xeon 3rd Gen) | **POST** | DELETE |

> 사용자 규격의 "SR630 V2: XCC 또는 XCC2 - 확인" 항목 결론: **XCC1** (verified. Lenovo Press LP0880 + Lenovo YUM 저장소가 SR630 V2 를 Whitley 플랫폼으로 표기).
> Purley 판정(SR630/SR650/SD530)은 Lenovo 문서가 모델명을 직접 나열하지 않아 "1세대 Xeon SP 서버 = Purley" 라는 일반 지식 + serverproven 세대 표기에서의 추론이다 (실제 장비에서 컬렉션 `Allow` 헤더로 확인 권장).

## 핵심 규칙 (플랫폼에 따라 다름)
- **Whitley / AMD 2소켓**: `POST /redfish/v1/AccountService/Accounts` 로 생성, `DELETE .../Accounts/{id}` 로 삭제.
- **Purley**: 계정 슬롯 12 개가 미리 만들어져 있어 POST/DELETE 불가. `UserName` 이 빈 문자열인 슬롯을 찾아 `PATCH /redfish/v1/AccountService/Accounts/{1..12}` 로 채움. 삭제는 `{"UserName": ""}` PATCH.
- 장비가 어느 쪽인지 모를 때: `GET /redfish/v1/AccountService/Accounts` 응답 헤더 `Allow` 에 `POST` 가 있으면 POST 방식, 없으면 PATCH 방식 (Lenovo 공식 샘플 `lenovo_create_bmc_user.py` 가 같은 방식으로 판별).

## URI 및 검증 상태
| 용도 | 메서드 / URI | 검증 | 시도 |
|---|---|---|---|
| 계정 컬렉션 조회 | GET /redfish/v1/AccountService/Accounts | verified (문서 + 샘플 스크립트) | 1차 |
| 계정 생성 (Whitley: SR630 V2) | POST /redfish/v1/AccountService/Accounts | verified (jev yes 1.00 + 공식 샘플 스크립트 Allow 분기 jev 0.92) | 1차 |
| 계정 생성 (Purley: SR630/SR650/SD530) | PATCH /redfish/v1/AccountService/Accounts/{1..12} (빈 슬롯) | verified (jev yes 0.99 + 샘플 스크립트 PATCH 모드) | 1차 |
| 패스워드 변경 | PATCH /redfish/v1/AccountService/Accounts/{id}  body `{"Password":"..."}` | verified (jev yes 1.00 + XCC2/XCC3 동일 페이지) | 1차 |
| 계정 삭제 | Whitley: DELETE .../Accounts/{id} (204), Purley: PATCH `{"UserName":""}` | verified (jev yes 0.92 + 샘플 스크립트) | 1차 |
| AccountService (정책) | GET/PATCH /redfish/v1/AccountService | verified (문서) | 1차 |
| 역할 | GET /redfish/v1/AccountService/Roles, POST Roles(커스텀, Whitley) / PATCH Roles/CustomRole{N}(Purley) | 문서 확인 | - |
| 세션 | POST /redfish/v1/SessionService/Sessions (201, X-Auth-Token) | 문서 확인(검색 요약) | - |

## 문서의 알려진 오류/불일치 (그대로 기록)
- XCC1 문서의 "Delete an account" 페이지 제목/요청줄이 `POST` 로 되어 있으나 본문은 HTTP DELETE 로 설명 (문서 오타로 판단). 샘플 스크립트는 `Allow` 에 DELETE 가 있으면 DELETE 사용.
- AccountService GET 문서: MinPasswordLength "8 고정", MaxPasswordLength "20 고정" 이라 적혀 있으나 예시 응답은 10/32. 실제 값은 장비에서 GET 으로 확인할 것.
- Redfish 오류 코드: XCC1 문서는 500 InternalError 만 표기.

## 파일
- `create_account.md` : 새 id/pw 등록 (Whitley POST / Purley PATCH, curl, payload, 대체 수단)
- `change_password.md` : 기존 id 패스워드 변경 (Redfish, 강제변경·세션, IPMI/CLI/onecli)

## 한계
- 문서 기반 조사. 실제 장비 접속/응답 코드 확인 없음. HTTP 성공 코드는 문서에 명시되지 않은 곳이 있어 샘플 스크립트 기준(200/201/204 성공)을 병기.
- 펌웨어 하한(어느 XCC 빌드부터 Redfish AccountService 가 되는지)은 Lenovo 문서에 명시 없음 -> unknown.
