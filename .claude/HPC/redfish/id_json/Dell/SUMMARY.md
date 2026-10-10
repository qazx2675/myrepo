# Dell iDRAC 계정 id/pw 등록·패스워드 변경 요약

모두 Redfish(JSON) 지원 (iDRAC8 은 FW 2.40.40.40 이상). Dell 은 XML API 대신 SCP(XML/JSON) 사용. **지원불가 사용자 모델 없음.**
버전별 상세: `iDRAC8/`, `iDRAC9/`, `iDRAC10/` 의 `README.md`, `create_account.md`, `change_password.md`.

| 버전 | 사용자 모델 | 계정 생성 | 패스워드 변경 | 삭제 | 슬롯 | 검증 |
|---|---|---|---|---|---|---|
| iDRAC8 | (없음, 참고용) | `PATCH /redfish/v1/Managers/iDRAC.Embedded.1/Accounts/{N}` 빈 슬롯 | 같은 URI `{"Password"}` | PATCH Enabled=false,RoleId=None 후 UserName="" | 2~16 (1 anonymous, 2 root) | Dell 스크립트 + dellemc.openmanage (코드 2종) |
| iDRAC9 | R640, C6420, R840, DSS8440, R740 (14G) / R750, R750xa, R750xs (15G) / R760XA, XE9680, R6615, R660, R860, C6620 (16G) | 위와 동일 (빈 슬롯 3~16 PATCH, POST 미확인) | PATCH `.../Accounts/{N}` | DellAttributes PATCH 로 Users.N.* 비우기 | 2~16 | 코드 2종 (Dell 스크립트, community.general) |
| iDRAC10 | XE9780 | `POST /redfish/v1/AccountService/Accounts` (201, Id 선택) | `PATCH /redfish/v1/AccountService/Accounts/{id}` | `DELETE` 204 | 2~16 | Dell 스크립트 + dellemc.openmanage |

## 공통 규칙
- **슬롯 방식:** 슬롯 1=IPMI anonymous(변경 불가), 2=root. UserName 이 빈 슬롯을 찾아 사용.
- **기본 계정:** `root`. 암호는 서버 라벨 고유 암호 또는 레거시 `calvin` (Dell KB 000133536).
- **권한:** Privilege 511=Administrator, 499=Operator, 1=ReadOnly, 0=None. iDRAC10 은 None 불가, 최소 1, `Users.N.Role` 및 커스텀 롤(`/AccountService/Roles`) 사용. 커스텀 롤은 iDRAC10 이상.
- **IPMI:** Users.N.IpmiLanPrivilege/IpmiSerialPrivilege = Administrator|Operator|User|No Access.
- **길이:** 사용자명 16자. 암호 iDRAC9 FW 3.xx 20 / 4~6.xx 40 / 7.xx 127 (Dell KB 000177787). iDRAC8·iDRAC10 은 문서 확인 못함(unavailable).
- **Basic 인증:** iDRAC9 7.30.10.50(14G 7.00.00.184) / iDRAC10 1.30.10.50 부터 기본 `Unadvertised` - 첫 요청부터 자격증명 전송(`curl -u` 는 문제 없음).
- **대체 수단:** `racadm set iDRAC.Users.<n>.UserName|Password|Privilege|Enable` (Password 구문 Dell KB, 나머지 PowerShell 모듈 근거), SCP export/import(iDRAC9 이하 `EID_674_Manager.*`, iDRAC10 `OemManager.*`), ipmitool(unverified), Ansible dellemc.openmanage.idrac_user.

## 미확인·불일치
- iDRAC9 에서 `/redfish/v1/AccountService/Accounts` 별칭·POST 지원: unverified (Dell 스크립트는 iDRAC9 에 쓰지 않음). 생성 시 405 가 나면 PATCH.
- Dell 공식 Redfish 레퍼런스(developer.dell.com), iDRAC8/9/10 User Guide 는 본문을 읽지 못함(JS 페이지, 404/403). 근거는 Dell 공개 스크립트·모듈 소스·KB.
- `Oem.Dell.DellManagerAccount`, LDAP/AD 연동, 성공 HTTP 코드(실장비), IPMI User ID↔슬롯 대응, racadm Enable 값 형식, iDRAC10 racadm 구문은 unverified.
- Dell 스크립트 `--user-id` 도움말(iDRAC9 이하)과 iDRAC10 분기 코드 불일치. 스크립트는 모델명 문자열로 세대를 판별.
- 모델 매핑 정정 없음: R6615 는 AMD 16G 로 iDRAC9 7.x. DSS8440/C6420/R840 는 14G iDRAC9.
- Jev 생략(키 없음).
