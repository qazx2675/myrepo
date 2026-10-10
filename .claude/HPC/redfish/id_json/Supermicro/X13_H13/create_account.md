# Supermicro X13/H13 - 새 계정 id/pw 등록

적용 모델: AS-1115HS-TNR (H13SSH)
플레이스홀더만 사용. `-k` 는 자체서명 인증서용.

## Redfish (우선)
- `POST https://<BMC_IP>/redfish/v1/AccountService/Accounts`
- 권한: Administrator.
- 사전: `GET /redfish/v1/AccountService` 의 `PasswordGuidanceMessage` 로 규칙 확인, `GET .../Accounts` 로 빈 슬롯/중복 이름 확인 (최대 16 프로파일, 익명 1 예약).

| 속성 | 타입 | 설명 |
|---|---|---|
| UserName | String | 새 계정명 (필수) |
| Password | String | 새 암호 (필수), 8~20자(웹 UI 기준), 3종 이상 문자군, 사용자명 역순 금지 |
| RoleId | String | `Administrator` / `Operator` / `ReadOnly` (가이드 허용값 3개) |
| Enabled | Boolean | 활성 여부 (4.0 가이드 예시에 포함) |
| AccountTypes | Array | `Redfish`, `IPMI` (BMC 01.05.xx 이상, 미지정 시 `Redfish`) |

```json
{
  "UserName": "<NEW_USER>",
  "Password": "<NEW_PASSWORD>",
  "RoleId": "Administrator",
  "Enabled": true,
  "AccountTypes": ["Redfish", "IPMI"]
}
```
```
curl -sk -u <ADMIN_USER>:<ADMIN_PASSWORD> -H "Content-Type: application/json" \
  -X POST https://<BMC_IP>/redfish/v1/AccountService/Accounts \
  -d '{"UserName":"<NEW_USER>","Password":"<NEW_PASSWORD>","RoleId":"Administrator","Enabled":true,"AccountTypes":["Redfish","IPMI"]}'
```
- `AccountTypes` 에 두 값을 동시에 넣는 것이 허용되는지는 가이드에 명시 없음(예시는 `"(Value)"` 한 개). 400 이면 `["Redfish"]` 로 만들고, IPMI 용은 별도 계정으로 만들거나 SMCIPMITool/ipmitool 사용.
- 응답 코드/본문은 문서에 없음 (unavailable). 표준 Redfish 는 201 + `Location`. 성공 후 `GET /redfish/v1/AccountService/Accounts` 로 새 번호 확인.
- 문서 표기 주의: 6.1 가이드는 Add Account URI 를 `/redfish/v1/AccountService` 로 적었으나 오기로 보이며, 4.0 가이드와 API 표는 `/Accounts` 컬렉션. `/Accounts` 가 405 이면 `GET` 의 `Allow` 헤더를 확인.
- 정책 PATCH 예: `PATCH /redfish/v1/AccountService` `{"AccountLockoutThreshold":5,"AccountLockoutDuration":300,"AccountLockoutCounterResetAfter":300}`.

## 제약
- 사용자 ID 2 = 기본 `ADMIN`. 새 계정은 3~16 사이 빈 번호에 배정됨 (Redfish 는 번호를 서버가 지정하는 것으로 추정, 문서 없음).
- Redfish 6.1 `PasswordGuidanceMessage` 예시: 공백, 탭, `"`, `'`, `:`, `,`, `#`, `-`, `;` 제외 (펌웨어별로 다를 수 있으니 GET 으로 확인).

## 대체 1: 웹 UI
- 로그인 후 **Configuration > Account Services > Users > Add** : User Name, Password, Network Privilege(Administrator / Operator / User), Enable, Account Type(Redfish/IPMI 또는 SNMP) 선택 후 저장 (X13 매뉴얼 2.6.1). Redfish/IPMI 형식은 UserName/Password 필수. SNMP 형식은 Authentication Protocol(MD5/SHA1), Encryption Protocol(DES/AES) 과 키가 추가로 필요.
- 웹 "User" 권한이 Redfish `ReadOnly` 에 대응한다는 매핑은 문서 명시 없음 (추정).

## 대체 2: ipmitool (IPMI 계정, 대역내/대역외)
슬롯 번호 `<ID>` 는 `user list 1` 로 비어 있는 번호 확인 (ADMIN = 2). 채널 1 은 Supermicro FAQ 예시 기준(`user list 1`).
```
ipmitool -I lanplus -H <BMC_IP> -U <ADMIN_USER> -P <ADMIN_PASSWORD> user list 1
ipmitool -I lanplus -H <BMC_IP> -U <ADMIN_USER> -P <ADMIN_PASSWORD> user set name <ID> <NEW_USER>
ipmitool -I lanplus -H <BMC_IP> -U <ADMIN_USER> -P <ADMIN_PASSWORD> user set password <ID> <NEW_PASSWORD> 20
ipmitool -I lanplus -H <BMC_IP> -U <ADMIN_USER> -P <ADMIN_PASSWORD> channel setaccess 1 <ID> callin=on ipmi=on link=on privilege=4
ipmitool -I lanplus -H <BMC_IP> -U <ADMIN_USER> -P <ADMIN_PASSWORD> user enable <ID>
```
- `user set password` 만 Supermicro FAQ 41692 로 확인. `set name`, `channel setaccess`, `enable` 은 일반 ipmitool 문법(Supermicro 에서 실측 없음, unverified). 권한 4=Administrator, 3=Operator, 2=User, 1=Callback (SMCIPMITool 가이드의 IPMI 권한 코드).
- 이 방식으로 만든 계정은 Gen 13 01.05 이상에서 IPMI 형식 계정일 가능성이 높음 (Redfish 로그인 불가일 수 있음, unverified).

## 대체 3: SMCIPMITool (대역외, Java; X14/H14 이상은 미지원)
```
SMCIPMITool <BMC_IP> <ADMIN_USER> <ADMIN_PASSWORD> user list
SMCIPMITool <BMC_IP> <ADMIN_USER> <ADMIN_PASSWORD> user add <ID> <NEW_USER> <NEW_PASSWORD> <PRIV>
```
- `<PRIV>`: 4=Administrator, 3=Operator, 2=User, 1=Callback. `user list` 는 X12 이상에서 Account Types 열을 출력 (가이드 2.27).
- 이 문서는 `user passwd` 가 아니라 **`user setpwd`** 가 실제 이름.
- 암호 규칙 (가이드): 8~19자, 사용자명 역순 금지, 3종 이상.

## 대체 4: IPMICFG (대역내, 호스트 root)
```
./IPMICFG-Linux.x86_64 -user list
./IPMICFG-Linux.x86_64 -user add <ID> <NEW_USER> <NEW_PASSWORD> <PRIV>
```
(IPMICFG 가이드 1.24.0 의 `-user add <user id> <user name> <password> <privilege>`.)

## 대체 5: SUM `ChangeBmcCfg` (BMC 설정 XML)
```
sum -i <BMC_IP> -u <ADMIN_USER> -p <ADMIN_PASSWORD> -c GetBmcCfg --file BMCCfg.xml --overwrite
(BMCCfg.xml 의 사용자 테이블 편집)
sum -i <BMC_IP> -u <ADMIN_USER> -p <ADMIN_PASSWORD> -c ChangeBmcCfg --file BMCCfg.xml
```
- 가이드는 BmcCfg 루트 아래 StdCfg / OemCfg 구조와 `Action="None|Change"` 속성만 설명. **사용자 테이블의 요소 이름은 문서에 없어 unavailable**: 실제 장비에서 `GetBmcCfg` 로 받은 파일에서 확인할 것. 릴리스 노트에 "Support user deletion for new BMC FW for GetBmcCfg and ChangeBmcCfg", "password complexity check for ChangeBmcCfg" 가 있어 사용자 항목이 존재하는 것은 확인.
- 노드 제품키(SFT-DCMS-SINGLE 등) 필요, Thomas-Krenn 표에서 ChangeBmcCfg 는 키 필요로 표기.
- SUM 은 X10 ~ X13/H13 지원 (제품 페이지). 계정 하나의 암호만 바꾸는 목적이면 `SetBmcPassword` 가 간단 (change_password.md).
