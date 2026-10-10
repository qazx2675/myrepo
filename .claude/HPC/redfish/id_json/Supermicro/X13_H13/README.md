# Supermicro X13 / H13 (BMC: ASPEED AST2600, "Super BMC") - 계정 id/pw 등록·변경

조사일 2026-10-10. Jev 생략(키 없음) - "공식 문서 + 두 번째 출처" 로만 판정.

## 프로토콜 판정: json (Redfish AccountService)
- 관리망: Supermicro BMC (AST2600), 사용자 매뉴얼 "BMC for X13, H13, B13 Series" Rev 1.0b.
- Redfish: Supermicro Redfish User Guide Rev 6.1 (Redfish 1.22.2-00.04, 2026-03) 이 Gen 12 이상에 적용. X13/H13 = "Gen 13".
- 라이선스: Redfish 가이드 Available APIs 표에서 AccountService / Accounts / Roles 는 License 열이 `Standard` (유료 키 불필요 표기). 소개문에는 SFT-OOB-LIC / SFT-DCMS-SINGLE 로 기능 일부가 보호된다고 되어 있음 -> 계정 API 는 Standard (문서 기준).
- 해당 사용자 보유 모델: **AS-1115HS-TNR** (보드 H13SSH, 1U AMD EPYC 9004). 이전 조사(bios_json/back/Supermicro/AS-1115HS-TNR/README.md)의 최신 펌웨어 `H13SSH_4.0_AS01.13.11_SAA1.5.0-p9` (BMC 01.13.11 로 표기, 이 조사에서 재확인 못함).
- 지원불가(계정 Redfish 없음) 사용자 모델: 없음.

## 방식 요약
| 동작 | 메서드 / URI | 비고 |
|---|---|---|
| 생성 | `POST /redfish/v1/AccountService/Accounts` | UserName, Password, RoleId, (AccountTypes), Enabled. 6.1 가이드의 Add Account 표기는 `/redfish/v1/AccountService` 이지만 오타로 보임 (아래 불일치 참조) |
| 암호 변경 | `PATCH /redfish/v1/AccountService/Accounts/{num}` `{"Password":"..."}` | Supermicro 문서에 Password PATCH 예시 없음 (unverified, 표준 Redfish) |
| 활성/비활성 | `PATCH .../Accounts/{num}` `{"Enabled":false}` | 문서 예시 있음. 활성 세션 있는 계정은 비활성 불가 |
| 강제 로그아웃 | `PATCH .../Accounts/{num}` `{"Enabled":false,"Oem":{"Supermicro":{"ForceLogout":true}}}` | 문서 예시 (JSON 괄호 오류 있는 예시) |
| 잠금 정책 | `PATCH /redfish/v1/AccountService` | AccountLockoutThreshold / Duration / CounterResetAfter |
| 정책 문구 | `GET /redfish/v1/AccountService` -> `PasswordGuidanceMessage` | 6.1 에서 추가된 속성 |
| 롤 | `GET /redfish/v1/AccountService/Roles/{Administrator,Operator,ReadOnly}` | RoleId 허용값 3개 |
| 삭제 | `DELETE .../Accounts/{num}` | 1.0a~2.0b 구 가이드는 "POST/PATCH/DELETE" 명시. 6.1 에는 DELETE 예시 없음 (unverified) |

## 기본 계정 / 비밀번호 규칙
- 기본 계정명 `ADMIN` (사용자 ID 2). 2019-11 이후 신규 제품은 `ADMIN` 비밀번호가 아니라 **보드별 고유 비밀번호**(대문자 알파벳 10자) - 보드 스티커 또는 서버 라벨 (BMC Unique Password 가이드, X13 매뉴얼 Appendix). 라벨 표기가 다르면 보드 스티커가 정답.
- 공장 초기화 옵션 3종 (X13 매뉴얼 Factory Default): 사용자 보존 / 전체 삭제 + 고유 비밀번호로 복귀 / 전체 삭제 + `ADMIN/ADMIN`.
- 웹 UI 길이: **8~20자**, 사용자명 역순 금지, 영문 소/대/숫자/특수 중 3종 이상 (X13 매뉴얼). Redfish 6.1 `PasswordGuidanceMessage` 예시: 공백/탭/`"`/`'`/`:`/`,`/`#`/`-`/`;` 제외, 3종 이상.
- IPMI 도구(SMCIPMITool 가이드) 는 8~19자라 기재 -> 도구 간 호환을 위해 **19자 이하 권장**.
- 사용자 수: 최대 15개 설정 가능 + 익명 1개 예약 (총 16). ID 2 = ADMIN.
- 잠금: 임계 1~5회 (0 이면 무제한).

## IPMI / Redfish 계정 분리 (중요)
- Redfish 가이드: "IPMI and Redfish accounts are now separate" - BMC 펌웨어 **Gen 13 01.05.xx 이상**부터. 이 버전 이상 업데이트 시 사용자 보존 옵션 사용 금지 권고.
- Add Account 의 `AccountTypes` 값: `IPMI`, `Redfish`. 미지정 시 기본 `Redfish`. 웹 UI 도 Account Type(Redfish/IPMI 또는 SNMP) 선택.
- 따라서 Redfish 로 만든 계정이 IPMI(ipmitool) 로그인에 쓰이려면 AccountTypes 에 IPMI 포함 필요. 두 값을 한 계정에 동시에 줄 수 있는지는 문서에 명시 없음 (SMCIPMITool 의 `user list` 예시는 `Redfish/IPMI` 로 표시 -> 가능해 보임, unverified).

## URI 및 검증 상태
| 용도 | URI | 검증 | 근거 |
|---|---|---|---|
| 계정 생성 | POST /redfish/v1/AccountService/Accounts | verified | Redfish Guide 4.0 "Creating a User" + 6.1 Add Account(URI 오기 가능) + 구 가이드 1.0a/2.0/2.0a/2.0b + 서드파티 flaviotorres/supermicro-redfish(create_user_supermicro.sh, 본문 미확인) |
| 계정 속성/Enabled | PATCH /redfish/v1/AccountService/Accounts/{num} | verified | 6.1 Accounts 페이지 + Available APIs 표 |
| 암호 변경 | PATCH .../Accounts/{num} {"Password"} | **unverified** | Supermicro 문서 예시 없음. 서드파티 update_user_password.sh (PATCH, 템플릿 JSON 본문 미확인) + 표준 Redfish |
| AccountService 정책 | PATCH /redfish/v1/AccountService | verified | 6.1, 4.0 |
| 롤 | /redfish/v1/AccountService/Roles/{Administrator,Operator,ReadOnly} | verified | 6.1 Available APIs |
| 웹 UI | Configuration > Account Services > Users | verified | X13 매뉴얼 2.6.1 (2번째 근거는 X14 매뉴얼 동일 구조) |
| IPMI | ipmitool user set password 2 ... | verified (암호 변경만) | Supermicro FAQ 41692 + IPMICFG 가이드 |
| SMCIPMITool user add/setpwd | 아래 change_password.md | verified (문법) | SMCIPMITool 가이드 2.27 (X13 에서의 동작은 미실측) |
| SUM SetBmcPassword | 아래 | verified (문법), X13 지원 | SUM 가이드 2.4 + SUM 제품페이지 "X10 ~ X13/H13" |

## 문서 불일치/특이사항
- 6.1 가이드 Add Account 의 URI 가 `/redfish/v1/AccountService` 로 쓰여 있으나, 같은 가이드 Available APIs 표는 `Accounts` 컬렉션을 별도 열거하고 4.0 가이드는 `/Accounts` 로 POST. 장비에서 `GET /redfish/v1/AccountService/Accounts` 응답 헤더 `Allow` 에 POST 가 있는지 확인.
- 6.1 가이드의 ForceLogout 예시 JSON 은 괄호가 맞지 않음 (`"Oem": { { "Supermicro": ...`). 올바른 형태는 `{"Enabled":false,"Oem":{"Supermicro":{"ForceLogout":true}}}`.
- X13 웹 UI: "기본 ADMIN 계정은 수정할 수 없다"(연필 아이콘) 로 표기. ADMIN 암호 변경은 본인 로그인 상태, Redfish PATCH, IPMI(`user set password 2`) 로 가능한 것으로 보이나 웹 UI 동작은 실측 필요.
- SMCIPMITool 가이드 본문에 "user add / user password" 라는 표현이 있으나 실제 서브커맨드 이름은 `user setpwd` (목차·사용법 기준).

## 시도 내역
- 1차: Supermicro Redfish User Guide 4.0 / 1.22.2-00.04 HTML, Rev 6.1 PDF, X13/X14/X12/X11 BMC 매뉴얼 PDF, BMC Unique Password 가이드, SMCIPMITool 2.27, SUM 2.4 가이드, IPMICFG 1.24 가이드, Supermicro FAQ 41692.
- 2차(Password PATCH 근거): 공개 코드 flaviotorres/supermicro-redfish (update_user_password.sh 로직만 확인), 웹 검색 재시도 1회 -> Supermicro 공식 PATCH Password 예시 없음. community.general `redfish_utils.py` 는 raw GitHub 접근이 프록시에서 404 라 읽지 못함 (unavailable).
- 판정은 Jev 생략, 문서 2중 출처 기준.

## 파일
- `create_account.md`, `change_password.md`
