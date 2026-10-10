# Supermicro BMC 계정 id/pw 등록·패스워드 변경 요약

문서 기준 (실장비 실측 없음). Jev 생략, 공식 문서 + 두 번째 출처로만 판정. XML API 는 어느 세대에서도 확인되지 않음(none).
버전별 상세: `X10/`, `X11_H11/`, `X12_H12/`, `X13_H13/`, `X14_H14/` 의 `README.md`, `create_account.md`, `change_password.md`.

| 버전 | Redfish | 사용자 모델 | 상태 |
|---|---|---|---|
| X10 (AST2400, BMC FW 3.xx) | json. FW 3.xx 이상 + OOB 라이선스에서만, 2.xx 이하는 none | 없음 | 계정 생성 verified. Redfish 시작 정확한 3.xx 버전 unavailable |
| X11 / H11 (FW 1.xx) | json. OOB 라이선스 필요 | 없음 | 계정 생성 verified. 시작 버전 unavailable |
| X12 / H12 (AST2600) | json | 없음 | 계정 생성 verified. H12 가 X12 와 같은 스택이라는 점 unverified |
| X13 / H13 (AST2600) | json (계정 API 라이선스 Standard 표기) | AS-1115HS-TNR (H13SSH) | 계정 생성 verified |
| X14 / H14 (AST2600) | json | 없음 | 계정 생성 verified (구조는 X13 과 동일) |

지원불가 사용자 모델: 없음 (보유 Supermicro 는 AS-1115HS-TNR 한 대).

## 핵심 사실
- 계정 생성: `POST /redfish/v1/AccountService/Accounts`, 본문 UserName/Password/RoleId/Enabled. RoleId 는 Administrator/Operator/ReadOnly (구 펌웨어 Admin/ReadOnlyUser).
- 암호 변경 `PATCH .../Accounts/{num}` `{"Password":...}`: 공식 가이드(4.0, 6.1, 2.0b)에 Password PATCH 예시 없음 -> 서드파티 스크립트 + 표준 Redfish 에 의존, **전 세대 unverified**. 공식 예시가 있는 것은 Enabled PATCH 뿐.
- 기본 계정 `ADMIN` (사용자 ID 2). 2019-11 이후 출하품은 보드별 고유 비밀번호(대문자 10자, 보드 스티커), 구 출하품은 ADMIN/ADMIN.
- 웹 UI: X10/X11 Configuration > Users, X12 이상 Configuration > Account Services > Users. 비밀번호 길이 X11 8~20, X12 8~19, X13/X14 8~20 (안전 기준: 19자 이하, 문자군 3종 이상, 사용자명 역순 금지).
- 사용자 수: X10/X11 10명, X12 이상 16 프로파일.
- IPMI/Redfish 계정 분리: Gen13 BMC 01.05.xx+, Gen14 01.02.xx.xx+ 에서 `AccountTypes`. 미지정 시 Redfish. ipmitool 로 만든 계정은 Redfish 로그인 안 될 수 있음(unverified).
- 도구:
  - ipmitool `user set password <ID> <PW>` (FAQ 41692, 채널 1).
  - IPMICFG `-user add/setpwd/del/level/list`.
  - SMCIPMITool `user add <id> <name> <pw> <priv>`, `user setpwd <id> <pw>` (요청서의 `user passwd` 는 실제 서브커맨드가 아님).
  - SUM `SetBmcPassword --user_id <ID> --new_password --confirm_password` (X10~X13/H13, 제품키 필요). `ChangeBmcCfg` 사용자 테이블 요소명은 공개 문서에 없음(unavailable, 실장비 `GetBmcCfg` 로 확인).
  - X14/H14 는 SMCIPMITool/IPMICFG/SUM 미지원, 후속 SAA/SSM 의 계정 명령은 unverified.

## 문서 불일치
- Redfish 6.1 가이드 Add Account URI 가 `/redfish/v1/AccountService` 로 적혀 있으나 API 표·4.0 가이드는 `/Accounts` -> 오기로 판단. 장비에서 `GET .../Accounts` 의 `Allow` 헤더로 확인.
- 6.1 가이드 ForceLogout 예시 JSON 괄호 오류 (올바른 형태는 X13_H13/README.md).
- X13 웹 UI 는 기본 ADMIN 을 Modify 불가로 표기 -> ADMIN 암호는 Redfish/IPMI/SUM 으로 변경.
- 응답 HTTP 코드, If-Match 요구 여부, 6.1 DELETE 는 문서에 없음(unavailable).

## 실패 항목
- community.general `redfish_utils.py` 404, flaviotorres/supermicro-redfish 일부만 확인, SAA 가이드 PDF 다운로드 실패.
- X10/X11 Redfish 시작 BMC 버전: 릴리스 노트가 다운로드 센터(EULA/JS) 뒤 -> 링크만 README.

## 출처
- Redfish 6.1 가이드: https://www.supermicro.com/manuals/other/RedfishUserGuide.pdf
- Redfish 6.1 Accounts: https://www.supermicro.com/en/support/manuals/product/software/redfish-user-guide/Content/general-content/accounts.htm
- Redfish 4.0 Account Service: https://www.supermicro.com/manuals/other/redfish-user-guide-4-0/Content/general-content/account-service.htm
- BMC 매뉴얼: https://www.supermicro.com/manuals/other/BMC_IPMI_X13_H13_B13.pdf , https://www.supermicro.com/manuals/other/BMC_IPMI_X14_H14.pdf , https://www.supermicro.com/manuals/other/BMC_Users_Guide_X12_H12.pdf , https://www.supermicro.com/manuals/other/IPMI_Users_Guide.pdf
- BMC Unique Password: https://www.supermicro.com/en/support/BMC_Unique_Password
- FAQ 41692: https://www.supermicro.com/en/support/faqs/faq.php?faq=41692
- SMCIPMITool 2.27: https://www.supermicro.com/wdl/utility/SMCIPMITool/SMCIPMITool_User_Guide.pdf
- 서드파티: https://github.com/flaviotorres/supermicro-redfish
