# Lenovo XCC 계정 id/pw 등록·패스워드 변경 요약

모두 Redfish(JSON) `AccountService` 지원. Lenovo 는 XML API 없음. 지원 불가 사용자 모델 없음.
버전별 상세: `XCC/`, `XCC2/`, `XCC3/` 의 `README.md`, `create_account.md`, `change_password.md`.

| 버전 | 사용자 모델 | 계정 생성 | 패스워드 변경 | 삭제 | 슬롯 | 검증 |
|---|---|---|---|---|---|---|
| XCC1 / Purley | SR630, SR650, SD530 (그룹핑은 추정) | `PATCH /redfish/v1/AccountService/Accounts/{N}` 빈 슬롯(UserName 비어 있는 곳)에 UserName/Password/RoleId/Enabled | `PATCH .../Accounts/{N}` `{"Password":"..."}` | `PATCH {"UserName":""}` | 1~12 | Jev 0.99 + Lenovo 공식 샘플 스크립트 |
| XCC1 / Whitley | SR630 V2 (XCC2 아님, XCC1) | `POST /redfish/v1/AccountService/Accounts` | `PATCH .../Accounts/{N}` | `DELETE` (204) | 1~12 | Jev 1.00 + 샘플 스크립트 |
| XCC2 | SR645 V3, SR675 V3, SD650 V3 | `POST` (AccountTypes 허용) | `PATCH .../Accounts/{N}` | `DELETE` 204 (추정) | 1~12 | Jev 1.00 |
| XCC3 | 해당 모델 없음 (인접 최신 버전) | `POST` | `PATCH .../Accounts/{N}` | `DELETE` | 1~14 | Jev 0.93 |

## 공통 규칙
- **권한:** Administrator(OEM Supervisor) 또는 `UserAccountManagement` 권한이 있는 커스텀 롤. Operator 는 불가.
- **패스워드 길이:** XCC1/XCC2 사용자 가이드 8~32자 (XCC2 Redfish `MaxPasswordLength` 255), XCC3 8~255자, IPMI 사용 계정은 20자 이하.
- **기본 계정:** `USERID` / `PASSW0RD`. 기본 설정상 최초 로그인 시 변경이 강제되며, 변경 전까지 Redfish 요청은 `Base.1.12.PasswordChangeRequired` 로 거부되고 해당 계정 URI 에 Password 를 PATCH 하라고 안내한다.
- **생성 방식 판별:** `GET .../Accounts` 응답의 `Allow` 헤더에 POST 가 있으면 POST, 없으면 빈 슬롯 PATCH.
- **대체 수단:** XCC SSH CLI `users -<n> -n/-p/-r`(XCC2 문서), OneCLI 설정명 `IMM.LoginId.n`/`IMM.Password.n`(XCC1·XCC2), `BMC.LoginID_n`/`BMC.Password_n`(XCC3) — 설정명만 확인, 명령 전체 문법은 unverified. ipmitool `user set name/password` 는 일반 문법만(unverified).

## 미확인·문서 불일치
- 패스워드 변경 후 기존 세션 무효화 여부: 문서에 없음(재인증 권장). PATCH 시 `If-Match`(ETag) 필요 여부: 문서에 없으나 샘플 스크립트는 전송.
- XCC3 의 요청/응답 예시는 XCC2 예시를 준용(추정 표기), 에러 ID 별 HTTP 코드는 `unavailable`.
- XCC1 "Delete an account" 문서는 제목·요청행에 POST 로 되어 있으나 본문은 HTTP DELETE 로 설명. XCC1 AccountService GET 은 Min 8/Max 20 이라 하나 예시는 10/32.
- XCC1 모델 그룹핑(Purley)은 Lenovo 문서에 모델명이 없어 추론.

## 재시도 기록 (XCC3, 최신 버전)
- 1차(다른 문서군): XCC3 계정 GET 페이지, 사용자 가이드 보안 정책, 설정명 매핑, Lenovo Press LP2273 → 길이 규칙 8~255, V4 서버 목록, 스키마 `ManagerAccount.v1_9_0`, 슬롯 14 확보. REST API 북 PDF 는 압축 문제로 읽지 못해 링크만 기록.
- 2차(공개 코드): python-redfish-lenovo 의 create/delete/set_user_global 스크립트 → POST/PATCH 판단 로직과 payload 키 확보. community.general `redfish_utils.py` 는 404.
