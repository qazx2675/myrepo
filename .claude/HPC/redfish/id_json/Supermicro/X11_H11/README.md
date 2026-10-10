# Supermicro X11 / H11 (BMC: ASPEED AST2500, 일부 AST2400, BMC FW 1.xx) - 계정 id/pw 등록·변경

조사일 2026-10-10. Jev 생략(키 없음) - "공식 문서 + 두 번째 출처" 판정.

## 프로토콜 판정: json (Redfish) - 단, OOB 라이선스 필요
- 근거: Redfish Reference Guide 2.0 / 2.0a / 2.0b 소개문 "Supermicro enables Redfish feature sets on their X10/X11 platforms with 3.xx and 1.xx BMC firmware respectively. These features are covered under SFT-OOB-LIC and SFT-DCMS-SINGLE license." -> **X11 은 BMC 펌웨어 1.xx 에서 Redfish**. Supermicro FAQ 24308 도 "redfish 를 쓰려면 OOB key 를 구매·활성화" (X10 시기 글, 동일 정책으로 추정).
- H11(AMD EPYC 7001) 도 Reference Guide 2.0b 의 대상 (H11DSU-in 펌웨어 폴더에 동봉). Redfish 제품 페이지 문구: "Intel-based X10 and AMD-based H11 and later".
- 매뉴얼: "BMC IPMI User's Guide" Rev 1.1b (AST2400/AST2500).
- 해당 사용자 보유 모델: **없음**. 참고용.
- 최신 가이드(6.1)는 Gen 12 이상만 대상이므로 X11 은 구 Reference Guide(2.0b)로만 확인. Redfish 가 켜지는 정확한 최소 BMC 버전(1.xx 내)은 문서에 없음 (unavailable, 실장비 `GET /redfish/v1` 로 확인).

## 방식 요약
| 동작 | 메서드 / URI | 비고 |
|---|---|---|
| 생성 | `POST /redfish/v1/AccountService/Accounts` {UserName, Password, RoleId, Enabled} | 2.0 / 2.0a / 2.0b 모두 동일 |
| 암호 변경 | `PATCH /redfish/v1/AccountService/Accounts/{num}` {"Password"} | 2.0b "Methods supported: Get/Post/Patch/Delete" 로만 명시, Password 예시 없음 (unverified) |
| 삭제 | `DELETE .../Accounts/{num}` | 2.0b 명시 |
| 잠금 정책 | `PATCH /redfish/v1/AccountService` {"AuthFailureLoggingThreshold":5,"AccountLockoutThreshold":2,"AccountLockoutDuration":300,"AccountLockoutCounterResetAfter":300} | 2.0b 3.2.2 |
| 롤 | `/redfish/v1/AccountService/Roles/{Administrator,Operator,ReadOnly,Custom1}` | 2.0b "Revised API": 구 `Admin`->`Administrator`, 구 `ReadOnlyUser`->`ReadOnly` |

- RoleId 값은 문서 예시에 `Admin / Operator / ReadOnlyUser` 로 적혀 있음(2.0, 2.0a). 개명 이후(2.0b) 펌웨어는 `Administrator/Operator/ReadOnly`. 펌웨어에 따라 다르므로 `GET /redfish/v1/AccountService/Roles` 응답 값을 사용.

## 기본 계정 / 규칙
- 기본 계정 `ADMIN`. 구 펌웨어/구 출하품은 `ADMIN/ADMIN` (X11 매뉴얼). **2019-11 이후 출하 신규 X10/X11/H11/H12**: 보드별 고유 비밀번호 (대문자 10자, 보드 스티커 + CPU1 커버 스티커). 지원 BMC 펌웨어 버전표는 Supermicro Security Center 에 있음 (링크만, 미열람). 공장 초기화 시 고유 비밀번호는 "한 번만 reset 가능"이라는 X11 매뉴얼 문구 (D-4).
- 비밀번호 (X11 매뉴얼 Account Security): **8~20자**, 사용자명 역순 금지, 소/대/숫자/특수 중 3종 이상. 이 복잡도 강제는 기본 Enable 이고 "X11 보드만" 해당 (X10 은 해당 기능 없음 문구).
- 사용자 수: 웹 UI 최대 **10 프로파일**. 사용자 ID 2 = ADMIN.
- 잠금/해제: X11 Account Security 에서 Failed Login Lockout Control(기본 Enable), "Unlock User" 기능.
- 웹 UI 경로: **Configuration > Users** (Add User / Modify User / Delete User, 빈 슬롯 선택), 잠금·복잡도는 Configuration > Account Security (X11 매뉴얼 2-7-11).

## 검증 상태
| 항목 | 검증 | 근거 |
|---|---|---|
| Redfish 지원(1.xx) | verified | Reference Guide 2.0/2.0a/2.0b 소개문 3종 |
| POST Accounts | verified | 2.0/2.0a/2.0b 3종 + 서드파티 스크립트 존재 |
| PATCH Password | unverified | Supermicro 문서 예시 없음 |
| 웹 UI 경로/규칙 | verified | X11 매뉴얼 (+ SMCIPMITool 가이드 8~19자 표기와 불일치, 아래) |
| IPMICFG/ipmitool 문법 | verified | X11 매뉴얼의 IPMICFG 도움말(-user add/del/level/setpwd, v1.20.3) + FAQ 41692 + IPMICFG 1.24.0 |
| SUM SetBmcPassword | verified (문법), X11 지원 | SUM 2.4 가이드 ("Supported X11 platform") |

## 문서 불일치
- 길이: X11 매뉴얼 8~20자 vs SMCIPMITool 가이드 8~19자. 19자 이하 사용 권장.
- Reference Guide 의 RoleId 예시(Admin) 와 Roles 목록(Administrator) 이 같은 문서에서 다름 (펌웨어 개명 이력).

## 시도 내역
- 1차: Redfish Reference Guide 1.0a/2.0/2.0a/2.0b PDF, X11 BMC IPMI User's Guide PDF, BMC Unique Password 가이드, SMCIPMITool/IPMICFG/SUM 가이드.
- 2차: 서드파티 flaviotorres/supermicro-redfish 확인. Redfish 시작 정확한 버전은 문서 부재 -> unavailable.

## 파일
- `create_account.md`, `change_password.md`
