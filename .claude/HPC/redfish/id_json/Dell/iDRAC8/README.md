# Dell iDRAC8 - 계정 id/pw 등록·패스워드 변경 (지원 범위용)

## 프로토콜 판정: json (Redfish, FW 2.40.40.40 이상) + SCP(XML)/RACADM
- 관리망: iDRAC8 (PowerEdge 13G, 12G 일부는 iDRAC7/8). Dell Redfish 스크립트 최소 FW **iDRAC7/8 2.40.40.40** (README).
- **사용자 보유 모델 없음** (사용자 Dell 모델은 전부 iDRAC9/10). 지원불가로 표시할 모델도 없음.
- Dell 스크립트의 iDRAC8 분기는 iDRAC9 와 같은 Accounts 슬롯 방식이며 삭제만 다르다.

## 방식 요약
| 동작 | 방법 | 검증 |
|---|---|---|
| 생성 | PATCH /redfish/v1/Managers/iDRAC.Embedded.1/Accounts/{3..16} `{UserName,Password,RoleId,Enabled}` | verified (Dell CreateDeleteIdracUsersREDFISH.py: idrac_version<10 공통 분기) |
| 암호 변경 | PATCH .../Accounts/{id} `{"Password"}` | verified (ChangeIdracUserPasswordREDFISH.py) |
| 삭제 | PATCH .../Accounts/{id} `{"Enabled":false,"RoleId":"None"}` 후 `{"UserName":""}` | verified (스크립트 else 분기) |
| 속성/IPMI | SCP(XML) import: `Users.N#UserName` 등 | verified (dellemc.openmanage: generation<14 는 SCP import 로 처리; Dell 커뮤니티 R730 iDRAC8 글) |
| DellAttributes PATCH | 사용 불가에 가까움 (모듈은 iDRAC8 에 SCP import 사용, `SetIdracLcSystemAttributesREDFISH.py` 도 "iDRAC9 이상, iDRAC8 은 SCP 사용"이라 명시) | verified |

## 세부
- 슬롯 2 = root, 슬롯 1 = IPMI anonymous. 사용 슬롯 2~16 (모듈 range(2,17)).
- 기본 계정 `root` / 기본 암호 `calvin` (iDRAC8, Dell KB 000133536 는 iDRAC8 포함 대상이라고만 기재).
- 암호 길이: 해당 KB 에 iDRAC8 값 없음 -> unavailable (일반적으로 20자 이하로 알려지나 문서 확인 못함).
- RACADM(iDRAC8): `racadm set iDRAC.Users.<n>.UserName/Password/Privilege/Enable` 는 Dell 커뮤니티에서 `racadm set idrac.users.3.enable 0` 성공 사례(R730, iDRAC8). 구형 `racadm config -g cfgUserAdmin -o cfgUserAdminUserName -i <n> <NAME>` (cfgUserAdminPassword/cfgUserAdminPrivilege/cfgUserAdminEnable) 문법은 이번에 문서로 확인 못함 -> unverified. `idrac.users.3.delete` 는 "Invalid object" 로 실패(커뮤니티 글) -> 삭제는 UserName 비우기/SCP.
- IPMI: iDRAC8 도 IPMI 지원(LAN 사용자 = Users.N). ipmitool `user set password` 계열은 unverified.
- HTTP Basic Unadvertised 변경은 iDRAC9 7.x / iDRAC10 대상 (KB 000437501), iDRAC8 해당 없음.

## 조사 내역
- 1차(문서): Dell KB 000133536, 000177787(iDRAC9 한정). iDRAC8 User Guide 접근 못함.
- 2차(공개 코드): Dell iDRAC-Redfish-Scripting, dellemc.openmanage idrac_user.py, Dell 커뮤니티 racadm 글.
- Jev 생략. 실장비 미확인. 사용자 모델이 없으므로 상세 create/change 문서는 이 README 로 갈음하고 파일은 요약 형태.

## 파일
`create_account.md`, `change_password.md`
