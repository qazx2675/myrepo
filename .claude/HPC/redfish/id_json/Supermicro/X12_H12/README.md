# Supermicro X12 / H12 (BMC: ASPEED AST2600) - 계정 id/pw 등록·변경

조사일 2026-10-10. Jev 생략(키 없음) - "공식 문서 + 두 번째 출처" 판정.

## 프로토콜 판정: json (Redfish AccountService)
- 매뉴얼: "BMC User's Manual" (X12/H12) Rev 1.0a, AST2600 기반. Supermicro Redfish User Guide 6.1 은 "Gen 12 이상" 에 적용, Gen 12 = Whitley / Tatlow(X12) 로 표기. H12(AMD EPYC 7003) 가 같은 Gen 12 BMC 스택인지는 Redfish 가이드가 H12 를 직접 언급하지 않아 **unverified** (BMC 매뉴얼 표제와 Unique Password 가이드가 X12/H12 를 묶음).
- 해당 사용자 보유 모델: **없음**. (H13 한 대만 보유 - 인접 하위 버전, 참고용)
- 계정 API: 구 Redfish Reference Guide(2.0b) 이후 `POST /Accounts` + Roles(Administrator/Operator/ReadOnly) 유지.

## X13/H13 대비 차이
| 항목 | X12/H12 | X13/H13 |
|---|---|---|
| 웹 UI 길이 규칙 | **8~19자** | 8~20자 |
| 웹 UI Account Type 선택(Redfish/IPMI/SNMP) | 매뉴얼에 없음 | 있음 |
| IPMI/Redfish 분리 공지 | Redfish 가이드에 Gen 13/14/NVIDIA 만 언급 -> Gen 12 는 문서상 분리 아님(추정) | Gen 13 01.05.xx 이상 |
| 사용자 표 Actions | edit / delete / enable / **unlock** | edit/lock/delete (X13 매뉴얼에 unlock 없음) |
| 공장 초기화 시 ADMIN 암호 | 고유 비밀번호 ("X12 and next generation" 문구, X13 매뉴얼 기준) | 동일 |
| `LocalAccountAuth` | 미기재 | Gen 13 1.08 이상 |
| SMCIPMITool `user list` Account Types 열 | "X12 시리즈 이상" 에서 출력 (가이드 2.27) | 동일 |
| SMCIPMITool `user enableType` | X12/H12 이상 (SNMP 타입 0 만) | 동일 |

## 방식 요약
| 동작 | 메서드 / URI |
|---|---|
| 생성 | `POST /redfish/v1/AccountService/Accounts` {UserName, Password, RoleId, Enabled} |
| 암호 변경 | `PATCH /redfish/v1/AccountService/Accounts/{num}` {"Password"} (unverified) |
| 정책 | `PATCH /redfish/v1/AccountService` (AccountLockoutThreshold 등) |
| 롤 | `/redfish/v1/AccountService/Roles/{Administrator,Operator,ReadOnly}` |

## 기본 계정 / 규칙
- 기본 계정 `ADMIN`. X12 이후는 보드 스티커의 고유 비밀번호 (X12 매뉴얼 Appendix D, 로그인 절차 "ADMIN and a BMC unique password").
- 웹 UI 비밀번호: 8~19자, 사용자명 역순 금지, 소/대/숫자/특수 중 3종 이상. 최대 16 프로파일. 잠금 임계 1~5.
- 웹 UI 경로: **Configuration > Account Services > Users** (X12 매뉴얼 2.7.1).
- 공장 초기화(웹 UI Maintenance) 3옵션은 X13 과 동일 구조 (사용자 보존 / 전체 삭제+고유 비밀번호 / 전체 삭제+ADMIN:ADMIN). X13 매뉴얼은 "고유 비밀번호는 X12 및 차세대" 라고 명시.

## 검증 상태
| 항목 | 검증 | 근거 |
|---|---|---|
| POST Accounts | verified | Redfish 6.1/4.0/2.0b 가이드 + 서드파티 스크립트 존재 (X12 실측 없음) |
| PATCH Password | unverified | Supermicro 문서에 예시 없음 |
| 웹 UI 경로·규칙 | verified | X12 매뉴얼 + X13 매뉴얼 구조 일치 |
| ipmitool / IPMICFG / SMCIPMITool / SUM | verified (문법) | Supermicro FAQ 41692, 각 도구 가이드 (X12 실측 없음) |
| X12 의 AccountTypes 지원 여부 | unverified | 문서 모순 없음, 근거 부재 -> `GET Accounts/{n}` 으로 확인 |

## 시도 내역
- 1차: X12/H12 BMC 매뉴얼 PDF, Redfish Guide 6.1·4.0·구 레퍼런스(2.0b), BMC Unique Password 가이드, 도구 가이드.
- 2차: 공개 코드(flaviotorres/supermicro-redfish) 로직 확인, community.general `redfish_utils.py` 는 프록시에서 404 (unavailable).

## 파일
- `create_account.md`, `change_password.md`
