# Supermicro X10 (BMC: ASPEED AST2400, BMC FW 3.xx) - 계정 id/pw 등록·변경

조사일 2026-10-10. Jev 생략(키 없음) - "공식 문서 + 두 번째 출처" 판정.

## 프로토콜 판정: json (Redfish) 조건부 / 그 외 xml 없음
- Redfish: **BMC 펌웨어 3.xx 이상에서만**. 근거: Redfish Reference Guide 2.0/2.0a/2.0b 소개문 "X10/X11 platforms with 3.xx and 1.xx BMC firmware respectively", Supermicro Redfish 안내("All the BMC firmware designated with 3.xx will support this technology", TinkerTry 인용) , 가이드 1.0a(2015-10-05) 이 최초 문서.
- 라이선스: SFT-OOB-LIC 또는 SFT-DCMS-SINGLE (가이드 소개문). Supermicro FAQ 24308 "redfish 를 쓰려면 OOB key 구매·활성화". OOB 키가 없으면 3.xx 에서도 API 접근 불가라는 사용자 보고가 있음(웹 검색, unverified).
- 최소 3.xx 중 어느 버전부터 Redfish 가 켜지는지는 공식 문서에 없음 (unavailable). 3.14 / 3.20 / 3.26~3.31 / 3.44 / 3.88 / 3.9x / 4.00 번대 펌웨어 이름이 공개되어 있으나 릴리스 노트 미열람. 실장비: `GET /redfish/v1`.
- **BMC FW 2.xx 이하(Redfish 미지원) X10**: Redfish 불가 -> 웹 UI / IPMI(ipmitool, IPMICFG, SMCIPMITool) 만 사용. XML API 는 확인된 것 없음 (none).
- 매뉴얼: X10 은 "BMC IPMI User's Guide" Rev 1.1b (X11 과 공용, "For X10 or Newer Versions of Motherboards" 절).
- 해당 사용자 보유 모델: **없음** (참고용). 사용자 모델 중 X10 이하 지원불가 대상 없음.

## 방식 요약
| 동작 | 메서드 / URI | 비고 |
|---|---|---|
| 생성 | `POST /redfish/v1/AccountService/Accounts` {UserName, Password, RoleId, Enabled} | Reference Guide 1.0a "POST / PATCH / DELETE operations", 2.0 "[POST] redfish/v1/AccountService/Accounts/" |
| 암호 변경 | `PATCH /redfish/v1/AccountService/Accounts/{num}` {"Password"} | 1.0a 에서 PATCH 지원만 명시, Password 예시 없음 (unverified) |
| 삭제 | `DELETE .../Accounts/{num}` | 1.0a, 2.0b |
| 롤 | 1.0a: `/redfish/v1/AccountService/Roles/{Admin,Operator,ReadOnlyUser}` ; 2.0a 이후 목록: `Administrator, Operator, ReadOnly, Custom1` | 2.0b "Revised API" 에서 개명 확인 |
| 잠금 정책 | `PATCH /redfish/v1/AccountService` | 2.0b 3.2.2 |

- RoleId 예시: 1.0a ~ 2.0b 모두 `// Admin, Operator, ReadOnlyUser` 로 적힘. 개명 후 펌웨어는 `Administrator`/`ReadOnly`. `GET /redfish/v1/AccountService/Roles` 로 확인.

## 기본 계정 / 규칙
- 기본 계정 `ADMIN`, 구 출하품 `ADMIN/ADMIN`. 2019-11 이후 신규 출하 X10 은 고유 비밀번호(대문자 10자, 스티커) + 이를 지원하는 BMC 펌웨어 (Unique Password 가이드: "all new X10, X11, H11, H12").
- 사용자 수: 최대 10 프로파일 (X11 매뉴얼의 X10 이상 공용 절, 빈 슬롯을 골라 추가). ID 2 = ADMIN.
- 비밀번호 복잡도 기능(Account Security)은 X11 전용으로 명시 -> X10 은 웹 UI 규칙 문서 없음 (unavailable). IPMI 2.0 비밀번호 최대 20바이트 (ipmitool 문서), SMCIPMITool 가이드는 8~19자 -> 19자 이하 권장.
- 웹 UI 경로: **Configuration > Users** (Add User / Modify User / Delete User).

## 검증 상태
| 항목 | 검증 | 근거 |
|---|---|---|
| X10 Redfish (3.xx) | verified (정확한 시작 버전은 unavailable) | Reference Guide 2.0/2.0a/2.0b + TinkerTry 인용 Supermicro 안내 + FAQ 24308 |
| POST Accounts | verified | Guide 1.0a/2.0/2.0a/2.0b |
| PATCH Password | unverified | Supermicro 문서 예시 없음 |
| 웹 UI 경로 | verified | X11 매뉴얼 "For X10 or Newer" 절 |
| IPMICFG/ipmitool | verified (문법), X10 실측 없음 | IPMICFG 1.20.3 도움말(2014), FAQ 41692 |
| SUM | verified | SUM 2.4 가이드 ("In-band UpdateBios command supports X10 MB", 제품 페이지 "X10 to X13/H13") |

## 시도 내역
- 1차: Reference Guide 1.0a/2.0/2.0a/2.0b PDF, X11 매뉴얼 X10 절, Unique Password 가이드, 도구 가이드.
- 2차: Redfish 시작 펌웨어 버전 검색(웹 2회) -> 공식 릴리스 노트 미확보 (Supermicro 다운로드 센터 페이지는 JS/EULA, 열지 않음). 링크만: https://www.supermicro.com/en/support/resources/downloadcenter/firmware/MBD-x10drl-ct/BMC

## 파일
- `create_account.md`, `change_password.md`
