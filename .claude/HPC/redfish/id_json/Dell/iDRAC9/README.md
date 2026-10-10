# Dell iDRAC9 - 계정 id/pw 등록·패스워드 변경

## 프로토콜 판정: json (Redfish) - 슬롯 PATCH 방식
- 관리망: iDRAC9 (PowerEdge 14G/15G/16G). 펌웨어 3.x ~ 7.x. Redfish 스크립트 최소 FW 3.00.00.00 (Dell iDRAC-Redfish-Scripting README).
- 해당 사용자 보유 모델: **R640, C6420, R840, DSS8440, R740 (14G)**, **R750, R750xa, R750xs (15G)**, **R760XA, XE9680, R6615, R660, R860, C6620 (16G)**.
- 지원불가 모델 없음. (R6615 는 AMD 16G, 나머지와 같은 iDRAC9 7.x 계열.)
- **Redfish 로 "계정 생성"은 POST 가 아니라 빈 슬롯에 PATCH** 한다. 슬롯 1 = IPMI anonymous(변경 불가), 슬롯 2 = root(기본), 슬롯 **3~16** 이 신규 생성용. 사용 가능 슬롯 범위 2~16.

## 방식 요약
| 동작 | 메서드 / URI | 비고 |
|---|---|---|
| 슬롯 조회 | GET /redfish/v1/Managers/iDRAC.Embedded.1/Accounts?$expand=*($levels=1) | UserName 이 빈 슬롯 = 빈 슬롯 |
| 생성 | PATCH /redfish/v1/Managers/iDRAC.Embedded.1/Accounts/{3..16} `{UserName,Password,RoleId,Enabled}` | 응답 200 |
| 암호 변경 | PATCH .../Accounts/{N} `{"Password":"..."}` | 응답 200 |
| 삭제 | PATCH /redfish/v1/Managers/iDRAC.Embedded.1/Oem/Dell/DellAttributes/iDRAC.Embedded.1 로 Users.N.* 비우기 | 200 |
| Dell OEM 속성(IPMI 권한 등) | PATCH .../Oem/Dell/DellAttributes/iDRAC.Embedded.1 `{"Attributes":{"Users.N.IpmiLanPrivilege":...}}` | 아래 |
| SCP | Managers/iDRAC.Embedded.1/Actions/Oem/EID_674_Manager.ExportSystemConfiguration / ImportSystemConfiguration | 대체 수단 |

## URI 및 검증 상태
| 용도 | URI | 검증 | 시도 |
|---|---|---|---|
| 계정 컬렉션 GET | /redfish/v1/Managers/iDRAC.Embedded.1/Accounts | verified (Dell CreateDeleteIdracUsersREDFISH.py + dellemc.openmanage idrac_user.py `ACCOUNT_URI`) | 2차(공개 코드) |
| 계정 생성 PATCH | .../Accounts/{id} (UserName,Password,RoleId,Enabled) | verified (Dell 스크립트 + community.general redfish_utils `add_user_via_patch`, 빈 슬롯 UserName=="" 및 Enabled false) | 2차 |
| 암호 변경 PATCH | .../Accounts/{id} `{"Password"}` | verified (Dell ChangeIdracUserPasswordREDFISH.py: iDRAC10 미만은 Managers/.../Accounts) | 2차 |
| 속성 PATCH | /redfish/v1/Managers/iDRAC.Embedded.1/Oem/Dell/DellAttributes/iDRAC.Embedded.1 (Dell 스크립트), /redfish/v1/Managers/iDRAC.Embedded.1/Attributes/ (dellemc.openmanage 모듈 ATTRIBUTE_URI) | verified (두 경로 모두 코드에 존재) | 2차 |
| 삭제 | DellAttributes PATCH (UserName "", Privilege 0, Enable Disabled, IPMIKey/MD5v3Key/SHA1v3Key/SHA256PasswordSalt "") | verified (Dell 스크립트 idrac_version==9 분기) | 2차 |
| AccountService/Accounts 별칭 (iDRAC9) | /redfish/v1/AccountService/Accounts | **unverified** (iDRAC10 에서만 코드가 사용. iDRAC9 존재/POST 지원은 확인 못함) | 2회 |
| Dell 공식 Redfish API 레퍼런스(ManagerAccount) | developer.dell.com/apis/2978 | **unavailable** (JS 페이지, 본문 못 읽음) | 2회 |

## 권한(RoleId / Privilege 비트)
- RoleId: Administrator, Operator, ReadOnly, None (스크립트 `--privilege-role` 1~4; None 은 iDRAC9 이하만).
- Privilege 숫자(dellemc.openmanage `USER_ROLES`): **Administrator=511(0x1FF), Operator=499, ReadOnly=1, None=0**.
- 비트: 1 Login, 2 ConfigureManager, 4 ConfigureUsers, 8 ClearLogs, 16 ConfigureComponents, 32 AccessVirtualConsole, 64 AccessVirtualMedia, 128 TestAlerts, 256 ExecuteDebugCommands (모듈 `PRIVILEGE_BIT_MAP`).
- 커스텀 롤(`/redfish/v1/AccountService/Roles` POST)은 Dell 스크립트상 **iDRAC10 이상** 기능 ("custom roles are only supported for iDRAC10 or newer"). iDRAC9 는 Privilege 마스크 사용.
- IPMI LAN/Serial 권한: Users.N.IpmiLanPrivilege / IpmiSerialPrivilege = Administrator | Operator | User | No Access (모듈 choices). Serial 은 랙/타워만 해당. 그 외 Users.N.SolEnable, ProtocolEnable(SNMPv3), AuthenticationProtocol(SHA/MD5), PrivacyProtocol(DES/AES/AES-256).
- OEM 리소스 `Oem.Dell.DellManagerAccount` 존재 여부는 이번에 직접 확인 못함 -> unverified (실장비 `GET .../Accounts/2` 로 확인).

## 기본 계정 / 정책
- 기본 계정명 `root` (슬롯 2). 기본 암호: 신형은 서버 라벨(풀아웃 Service Tag/시스템 정보 태그)에 인쇄된 **고유 임의 암호**, 레거시 옵션은 `calvin` (Dell KB 000133536). 어느 세대/펌웨어가 어느 쪽인지 KB 에 매핑 없음 -> 장비 라벨 확인.
- 기본 암호 로그인 시 경고(SEC0701) 표시. 초기화: `racadm racresetcfg -all`(공장 기본), `-rc`(레거시 암호).
- 사용자명 1~16자 (Dell KB 000177787). 암호 길이: **FW 3.xx 1~20, 4.xx~6.xx 1~40, 7.xx 1~127** (같은 KB). 대/소문자·숫자·특수문자 포함, 8자 이상 권장.
- 모듈 사용자명 검증: 영숫자+공백+일부 특수문자, 16자 이하, 앞뒤 공백 불가 (`validate_username`).
- 7.30.10.50(14G 는 7.00.00.184) 이상: HTTP Basic 인증 기본값이 `Enabled`->`Unadvertised`. 401 챌린지를 기다리지 않고 처음부터 자격증명을 보내야 한다 (`curl -u` 는 선제 전송이므로 문제 없음, 라이브러리는 force_basic_auth). 끄는 경우: AccountService `HTTPBasicAuth`=Disabled -> 세션 토큰 사용. (Dell KB 000437501)

## 로컬 계정 vs LDAP/AD
- 위 Users.N 은 로컬 계정. LDAP/AD 는 별도 속성군(iDRAC.LDAP.*, ActiveDirectory.*)이며 이번에 조사하지 않음 -> unverified. 로컬 계정 슬롯과 별개로 인증 우선순위는 장비 설정에 따름.

## 문서 불일치/특이사항
- dellemc.openmanage idrac_user 는 계정 슬롯을 SCP export(`Users.N#UserName`)로 찾고 range(2,17) 순회, 속성 PATCH 사용. Dell 스크립트는 Accounts 컬렉션 GET. 둘 다 슬롯 2~16 확인.
- Dell 스크립트의 iDRAC 버전 판별은 Manager Model 문자열에 "12/13"->8, "14/15/16"->9, 그 외->10 (이름 휴리스틱).

## 조사 내역 / 시도
- 1차(문서군): Dell KB 000133536(기본 암호), 000177787(암호 길이·racadm), 000437501(Basic 인증). Dell iDRAC9 User Guide 페이지 404, infohub SCP 가이드 403, developer.dell.com 본문 없음.
- 2차(공개 코드): github.com/dell/iDRAC-Redfish-Scripting raw(CreateDeleteIdracUsersREDFISH, ChangeIdracUserPasswordREDFISH, CustomIdracUserRoleREDFISH, CreateIdracUserCsvFileREDFISH, Export/ImportSystemConfiguration*), dellemc.openmanage idrac_user.py v9.3.0, community.general stable-9 redfish_utils.py, Dell 커뮤니티 racadm 글, PowerShell Parmis.Idrac.
- Jev 생략(키 없음). verified = 공개 코드 2종 이상 일치.
- 한계: 실장비 응답/HTTP 코드 미확인(스크립트의 성공 코드: PATCH 200).

## 파일
`create_account.md`, `change_password.md`
