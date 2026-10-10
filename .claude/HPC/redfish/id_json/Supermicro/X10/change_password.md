# Supermicro X10 - 기존 id 패스워드 변경

해당 사용자 모델 없음 (참고용).

## Redfish (BMC FW 3.xx + OOB 라이선스; Password PATCH 는 unverified)
```
curl -sk -u <ADMIN_USER>:<CURRENT_PASSWORD> https://<BMC_IP>/redfish/v1/AccountService/Accounts
curl -sk -u <ADMIN_USER>:<CURRENT_PASSWORD> -H "Content-Type: application/json" \
  -X PATCH https://<BMC_IP>/redfish/v1/AccountService/Accounts/<SLOT> \
  -d '{"Password":"<NEW_PASSWORD>"}'
```
- 구 Reference Guide 는 PATCH 지원만 명시하고 Password 본문 예시가 없음. 실패 시 아래 IPMI 방식.

## 대체 (Redfish 없는 2.xx 펌웨어 포함 권장 경로)
- **웹 UI:** Configuration > Users > `Modify User` -> 사용자 선택 후 수정.
- **ipmitool (Supermicro FAQ 41692):**
  ```
  ipmitool -I lanplus -H <BMC_IP> -U <ADMIN_USER> -P <CURRENT_PASSWORD> user list 1
  ipmitool -I lanplus -H <BMC_IP> -U <ADMIN_USER> -P <CURRENT_PASSWORD> user set password <ID> <NEW_PASSWORD> 20
  ```
  호스트 root 에서는 `ipmitool user set password 2 <NEW_PASSWORD>`.
- **IPMICFG:** `./IPMICFG-Linux.x86_64 -user setpwd <ID> <NEW_PASSWORD>` (IPMICFG 1.20.3 도움말, 2014 에도 존재).
- **SMCIPMITool:** `SMCIPMITool <BMC_IP> <ADMIN_USER> <CURRENT_PASSWORD> user setpwd <ID> <NEW_PASSWORD>`
- **SUM:** `sum -i <BMC_IP> -u <ADMIN_USER> -p <CURRENT_PASSWORD> -c SetBmcPassword --user_id <ID> --new_password <NEW_PASSWORD> --confirm_password <NEW_PASSWORD>` (SUM 2.x 의 SetBmcPassword, `--user_id` 생략 시 2).
- 비밀번호 길이: 8~19자 권장 (SMCIPMITool 가이드), IPMI 2.0 은 최대 20바이트. 복잡도 규칙은 X10 문서 없음.
- 고유 비밀번호 출하품: 스티커의 대문자 10자로 첫 로그인 후 변경 (Unique Password 가이드).
