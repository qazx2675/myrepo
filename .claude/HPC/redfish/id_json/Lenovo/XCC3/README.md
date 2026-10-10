# Lenovo XCC3 - 계정 id/pw 등록·패스워드 변경 (최신 버전)

## 프로토콜 판정: json (Redfish AccountService)
- 관리망: Lenovo XClarity Controller 3 (XCC3, OpenBMC 기반). ThinkSystem V4 서버. Redfish Spec 1.17.0 / Schema Bundle 2024.3, IPMI 2.0 (Lenovo Press LP2273).
- 근거 문서: https://pubs.lenovo.com/xcc3-restapi/ (Account management)
- 해당 사용자 보유 모델: **없음** (사용자 모델은 XCC/XCC2). 인접(최신) 버전으로 조사. XCC3 적용 서버 예: SR630 V4, SR650 V4, SR650a V4, SR680a V4, SR850 V4, SR860 V4, SC750 V4, SC777 V4.
- 지원불가 모델: 없음.

## 방식 요약
| 동작 | 메서드 / URI | 비고 |
|---|---|---|
| 생성 | POST /redfish/v1/AccountService/Accounts | UserName, Password, RoleId, AccountTypes, Enabled, SNMP |
| 암호 변경 | PATCH /redfish/v1/AccountService/Accounts/{1..14} `{"Password":"..."}` | **슬롯 14 개**(XCC1/XCC2 는 12) |
| 삭제 | DELETE /redfish/v1/AccountService/Accounts/{1..14} | 204 |
| 정책 | GET/PATCH /redfish/v1/AccountService | |
| 설정명 변경 | OneCLI 설정명이 `IMM.*` -> `BMC.*` (Redfish 정렬) | 아래 표 |

XCC3 의 계정 속성 스키마는 `#ManagerAccount.v1_9_0` (문서 예시), Links.Keys(`.../Accounts/{X}/Keys`) 추가.

## URI 및 검증 상태
| 용도 | URI | 검증 | 시도 |
|---|---|---|---|
| 계정 생성 | POST /redfish/v1/AccountService/Accounts | verified (jev yes 0.93 + XCC/XCC2 동일 구조 + 공식 샘플 스크립트) | 1차 |
| 암호 변경 | PATCH /redfish/v1/AccountService/Accounts/{1..14} | verified (jev yes 0.93 + 문서) | 1차 |
| 계정 조회 | GET /redfish/v1/AccountService/Accounts/{1..14} | verified (문서) | 1차 |
| 컬렉션 | GET /redfish/v1/AccountService/Accounts | verified (문서) | 1차 |
| 삭제 | DELETE /redfish/v1/AccountService/Accounts/{1..14} (+HostBootStrap 별도 URL) | verified (문서 검색 결과 요약 + 샘플 스크립트 jev 0.92) | 1차 |
| AccountService | GET /redfish/v1/AccountService | verified (문서) | 1차 |

## 최신 버전 재시도 (필수 항목 확보 실패 대응)
최초 조사에서 XCC3 페이지에 없었던 항목: (1) 요청/응답 예시, (2) 지원 시스템 목록, (3) 오류 ID 별 상태코드 매핑, (4) 패스워드 정책 값.

| 단계 | 범위 | 시도한 출처 | 결과 |
|---|---|---|---|
| 재시도 1 | 다른 문서군 | xcc3-restapi `account_properties_get`, `collection_for_accounts_get`, XCC3 사용자 가이드 `nn1ia_c_accountsecuritypolicysettings`, `dw1lm_t_loggingintotheimm`, `dw1lm_t_xcc3settingname`, Lenovo Press LP2273, xcc3_restapi_book.pdf | (4) 확보: 복잡도 8~255자 등. (2) 확보: LP2273 서버 목록(V4). 계정 속성 예시 응답(v1_9_0) 확보. PDF 는 압축 스트림이라 판독 불가 -> 링크만 기록 |
| 재시도 2 | 공개 코드 | github lenovo/python-redfish-lenovo `lenovo_create_bmc_user.py`, `lenovo_delete_bmc_user.py`, `lenovo_set_bmc_user_global.py`; community.general `redfish_utils.py` (raw 404) | (1) POST/PATCH 분기 로직 확보(모델 비의존, Allow 헤더 기반). XCC3 전용 요청 예시는 공개 코드에서 못 구함 -> XCC2 예시 준용 (같은 속성 집합) |
| 결과 | - | - | (1) XCC3 전용 예시 `unavailable` (XCC2 예시 준용, inferred). (3) ID 별 코드 매핑 `unavailable` (문서가 개별 매핑 안 함; 400 계열 추정) |

시도 횟수 기록: 1차(Jev 판정) 1회, 재조사 2회(위), 합동 없음.

## 문서 불일치/특이사항
- AccountService GET(XCC3): `MinPasswordLength` 기본 10, `MaxPasswordLength` 255 고정. (XCC1 문서는 8/20 고정으로 다름)
- 슬롯 수: GET/PATCH/DELETE 페이지는 `{1...14}`. POST 페이지는 슬롯 수 미언급.
- 오류 ID: 500 InternalError, 400 CreateFailedMissingReqProperties 외 PropertyValueTypeError, PropertyValueFormatError, ResourceChangeRequried, NotRecommandedOperation, ForbiddenOperation, ResourceAlreadyExists, PropertyMissing, PasswordChangeRequired.

## 파일
- `create_account.md`, `change_password.md`

## 한계
- 실제 장비 접속 없음. XCC3 예시 payload 는 XCC2 문서/공개 샘플에서 준용(속성명은 XCC3 문서 표와 일치 확인).
