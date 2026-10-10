# Dell iDRAC10 - 계정 id/pw 등록·패스워드 변경

## 프로토콜 판정: json (Redfish AccountService) - POST 생성 지원
- 관리망: iDRAC10 (PowerEdge 17G). Redfish 스크립트 최소 FW 1.10.17.00 (Dell iDRAC-Redfish-Scripting README). 해당 KB 대상 FW: 1.10.xx / 1.20.xx / 1.30.xx.
- 해당 사용자 보유 모델: **XE9780**. 지원불가 모델 없음.
- iDRAC9 와 달리 `/redfish/v1/AccountService/Accounts` 컬렉션을 쓰며 **POST 로 생성(201), DELETE 로 삭제(204)**, 계정 Id 지정 가능.

## 방식 요약
| 동작 | 메서드 / URI | 비고 |
|---|---|---|
| 조회 | GET /redfish/v1/AccountService/Accounts?$expand=*($levels=1) | |
| 생성 | POST /redfish/v1/AccountService/Accounts `{UserName,Password,RoleId,Enabled,[Id]}` | 201. Id 생략 시 첫 빈 번호 자동 할당 |
| 암호 변경 | PATCH /redfish/v1/AccountService/Accounts/{id} `{"Password"}` | 200 |
| 삭제 | DELETE /redfish/v1/AccountService/Accounts/{id} | 204 |
| 커스텀 롤 | POST /redfish/v1/AccountService/Roles `{RoleId,AssignedPrivileges,OemPrivileges}` | iDRAC10 이상 |
| Dell OEM 속성 | PATCH /redfish/v1/Managers/iDRAC.Embedded.1/Oem/Dell/DellAttributes/iDRAC.Embedded.1 (`Users.N.Role`, IpmiLanPrivilege 등) | |
| SCP | Managers/iDRAC.Embedded.1/Actions/Oem/OemManager.Export|ImportSystemConfiguration | iDRAC9 의 EID_674_Manager 와 이름 다름 |

## URI 및 검증 상태
| 용도 | URI | 검증 | 시도 |
|---|---|---|---|
| 생성 POST | /redfish/v1/AccountService/Accounts | verified (Dell CreateDeleteIdracUsersREDFISH.py 201 처리 + CreateIdracUserCsvFileREDFISH.py) | 2차(공개 코드) |
| 암호 PATCH | .../Accounts/{id} | verified (Dell ChangeIdracUserPasswordREDFISH.py) | 2차 |
| 삭제 DELETE | .../Accounts/{id} -> 204 | verified (Dell 스크립트) | 2차 |
| 롤 | /redfish/v1/AccountService/Roles (+ `?$expand=*($levels=1)`), Description "Custom User Role" | verified (Dell 스크립트 + dellemc.openmanage idrac_user.py) | 2차 |
| 속성 | .../Oem/Dell/DellAttributes/iDRAC.Embedded.1 (`Users.N.Role`) | verified (dellemc.openmanage `ATTRIBUTE_URI_10`, generation>=17) | 2차 |
| 최대 슬롯 | 2~16 (스크립트 `--user-id` 도움말: 2 to 16) | verified | 2차 |
| Dell 공식 iDRAC10 Redfish 레퍼런스 | developer.dell.com | **unavailable** (본문 못 읽음), iDRAC10 User's Guide 도 접근 못함 | 2회 |

## 권한
- RoleId: Administrator, Operator, ReadOnly, 커스텀 롤 이름. **"None" 불가**(모듈: "None is not an applicable value ... 17G and later"). 커스텀 Privilege 값 범위 **1~511**(iDRAC9 는 0~511).
- 커스텀 롤 권한명: AssignedPrivileges = Login, ConfigureManager, ConfigureUsers, ConfigureComponents, ConfigureSelf(스크립트 도움말); OemPrivileges = ClearLogs, AccessVirtualConsole, AccessVirtualMedia, TestAlerts, ExecuteDebugCommands. 이름은 공백 불가, `-` `_` 만 허용(스크립트).
- 모듈 비트 매핑: 1 Login, 2 ConfigureManager, 4 ConfigureUsers, 8 ClearLogs, 16 ConfigureComponents, 32 AccessVirtualConsole, 64 AccessVirtualMedia, 128 TestAlerts, 256 ExecuteDebugCommands.
- SNMPv3: AuthenticationProtocol 은 SHA-384 / SHA-512 만(SHA, MD5 불가), PrivacyProtocol 은 AES-256 등 (DES/AES 불가). IPMI LAN/Serial 권한 Administrator|Operator|User|No Access.
- `Oem.Dell.DellManagerAccount` 존재 여부는 확인 못함 -> unverified.

## 기본 계정 / 정책
- 기본 계정 `root` (슬롯 2). 기본 암호: 서버 라벨의 고유 암호 또는 레거시 `calvin` (Dell KB 000133536, iDRAC10 1.10/1.20 포함).
- 암호/사용자명 길이: **iDRAC10 값은 unavailable** (Dell KB 000177787 은 iDRAC9 3.xx~7.xx 만 기재; 모듈 사용자명 검증은 16자 이하). 실장비에서 `GET /redfish/v1/AccountService` 의 `MinPasswordLength`/`MaxPasswordLength` 와 Attribute Registry 로 확인.
- FW 1.30.10.50 이상: HTTP Basic 기본값 `Unadvertised`, AccountService `HTTPBasicAuth` (Enabled|Unadvertised|Disabled). 첫 요청부터 자격증명 전송 필요(Dell KB 000437501).
- 로컬 계정 vs LDAP/AD: AccountService 에 별도 LDAP/ActiveDirectory 속성(unverified, 이번 조사 제외).

## 조사 내역
- 1차(문서군): Dell KB 000133536 / 000177787 / 000437501. iDRAC10 User's Guide, developer.dell.com 은 접근 불가.
- 2차(공개 코드): iDRAC-Redfish-Scripting (CreateDelete..., ChangeIdracUserPassword..., CustomIdracUserRole..., Export/Import SCP), dellemc.openmanage idrac_user.py 9.3.0(generation>=17 분기), 공식 Ansible 문서 검색.
- Jev 생략(키 없음). 한계: 실장비 응답 미확인.

## 파일
`create_account.md`, `change_password.md`
