# Supermicro X14 / H14 (BMC: ASPEED AST2600) - 계정 id/pw 등록·변경

조사일 2026-10-10. Jev 생략(키 없음) - "공식 문서 + 두 번째 출처" 판정.

## 프로토콜 판정: json (Redfish AccountService)
- 매뉴얼: "BMC for X14 and H14 Series" Rev 1.1. Redfish Guide 6.1 (1.22.2-00.04) 에서 X14/H14 = "Gen 14".
- 해당 사용자 보유 모델: **없음** (사용자 모델은 H13 한 대뿐). 인접 최신 버전으로 참고용 조사.
- Redfish Accounts API 는 X13/H13 과 동일 구조 (같은 Redfish 가이드). 차이는 아래.

## X13/H13 대비 차이
| 항목 | X13/H13 | X14/H14 | 근거 |
|---|---|---|---|
| IPMI/Redfish 계정 분리 시작 BMC FW | 01.05.xx (Gen 13) | **01.02.xx.xx** (Gen 14) | Redfish 6.1 Accounts |
| Redfish 계정 속성 | 동일 | 동일 | 6.1 |
| 웹 UI 길이 규칙 | 8~20 | 8~20 | X13 / X14 매뉴얼 |
| 웹 UI Delete | 로그인 중 계정 삭제 불가 | ADMIN(기본 관리자) 삭제 불가 문구 추가 | X14 매뉴얼 |
| SNMP 계정 필드 | MD5/SHA1, DES/AES | `HMAC_MD5`/`HMAC_SHA96`, `CBC_DES`/`CFB128_AES128` | X14 매뉴얼 |
| SMCIPMITool / IPMICFG / IPMIView | 지원 | **미지원** ("X14/H14 generation or later") | Supermicro IPMI Utilities 페이지 |
| SUM | 지원 (X10 ~ X13/H13) | **미지원**, 대신 SAA / SSM | SUM 제품 페이지 ("For X14/H14 and above ... please use ...") |
| `LocalAccountAuth` PATCH | Gen 13 1.08 이상 | Gen 14 1.06 이상 (SFT-DCMS-SINGLE + SFT-OOB-LIC 필요) | Redfish 6.1 Account Service |

## 방식 요약
| 동작 | 메서드 / URI |
|---|---|
| 생성 | `POST /redfish/v1/AccountService/Accounts` (UserName, Password, RoleId, AccountTypes[IPMI/Redfish], Enabled) |
| 암호 변경 | `PATCH /redfish/v1/AccountService/Accounts/{num}` `{"Password":"..."}` (unverified: Supermicro 문서 예시 없음) |
| 활성/비활성 | `PATCH .../Accounts/{num}` `{"Enabled":false}` |
| 정책 | `PATCH /redfish/v1/AccountService`, `GET` -> `PasswordGuidanceMessage` |
| 롤 | Administrator / Operator / ReadOnly |

## 기본 계정 / 규칙
- 기본 계정 `ADMIN`, 신규 제품은 보드/서버 라벨의 고유 비밀번호 (BMC Unique Password 가이드: "all future generation products"). X14 매뉴얼에도 "BMC unique password" 로그인 안내.
- 길이 8~20자, 사용자명 역순 금지, 3종 이상 문자군. 최대 16 프로파일 (설정 15 + 익명 1).
- 웹 UI 경로: **Configuration > Account Services > Users** (X14 매뉴얼 2.6.1).

## 검증 상태
| 항목 | 검증 | 근거 |
|---|---|---|
| POST Accounts | verified | Redfish 4.0/6.1 가이드 + 서드파티 스크립트 존재 (X14 장비 실측 없음) |
| PATCH Password | unverified | 위와 동일 (README X13_H13 참조) |
| 웹 UI 경로/규칙 | verified | X14 매뉴얼 + X13 매뉴얼 동일 |
| ipmitool 암호 설정 | verified (일반 IPMI) | Supermicro FAQ 41692 (플랫폼 불문 예시) |
| SMCIPMITool / IPMICFG / SUM | unavailable (이 세대 미지원) | IPMI Utilities / SUM 페이지 |
| SAA(SetBmcPassword/ChangeBmcCfg) | unverified | 서드파티 스킬 목록에만 명령 존재, 공식 SAA 가이드 못 구함 |

## 시도 내역
- 1차: X14 BMC 매뉴얼 PDF, Redfish Guide 6.1, Supermicro IPMI Utilities / SUM / SAA 제품 페이지.
- 2차: SAA 사용자 가이드 PDF 직접 다운로드 시도(`/manuals/other/SAA_UserGuide.pdf` -> 302 후 HTML 로 응답) 실패, 웹 검색 1회 -> 공식 가이드 미확보. 실장비에서 `saa -h` 로 확인 필요.

## 파일
- `create_account.md`, `change_password.md`
