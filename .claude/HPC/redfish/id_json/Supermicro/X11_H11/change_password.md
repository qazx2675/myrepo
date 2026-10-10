# Supermicro X11/H11 - 기존 id 패스워드 변경

해당 사용자 모델 없음 (참고용).

## Redfish (unverified: 구 Reference Guide 에 Password PATCH 예시 없음)
```
curl -sk -u <ADMIN_USER>:<CURRENT_PASSWORD> https://<BMC_IP>/redfish/v1/AccountService/Accounts
curl -sk -u <ADMIN_USER>:<CURRENT_PASSWORD> -H "Content-Type: application/json" \
  -X PATCH https://<BMC_IP>/redfish/v1/AccountService/Accounts/<SLOT> \
  -d '{"Password":"<NEW_PASSWORD>"}'
```
- Reference Guide 2.0b 는 AccountService 를 "Get/Post/Patch/Delete" 지원으로만 설명. 비밀번호 8~20자(3종 이상, 역순 금지).
- 잠금 계정은 웹 UI Configuration > Account Security 의 Unlock User.

## 대체
- **웹 UI:** Configuration > Users > `Modify User` -> 사용자 선택 후 암호 수정.
- **ipmitool (Supermicro FAQ 41692):**
  ```
  ipmitool -I lanplus -H <BMC_IP> -U <ADMIN_USER> -P <CURRENT_PASSWORD> user list 1
  ipmitool -I lanplus -H <BMC_IP> -U <ADMIN_USER> -P <CURRENT_PASSWORD> user set password <ID> <NEW_PASSWORD> 20
  ```
- **IPMICFG:** `./IPMICFG-Linux.x86_64 -user setpwd <ID> <NEW_PASSWORD>`
- **SMCIPMITool:** `SMCIPMITool <BMC_IP> <ADMIN_USER> <CURRENT_PASSWORD> user setpwd <ID> <NEW_PASSWORD>`
- **SUM (X11 지원):** `sum -i <BMC_IP> -u <ADMIN_USER> -p <CURRENT_PASSWORD> -c SetBmcPassword --user_id <ID> --new_password <NEW_PASSWORD> --confirm_password <NEW_PASSWORD>` (`--user_id` 생략 시 2).
- **고유 비밀번호 장비:** 최초에는 보드 스티커의 대문자 10자 비밀번호로 로그인 후 위 방법으로 변경 (Unique Password 가이드 FAQ: IPMI GUI 또는 IPMICFG 로 변경). 공장 초기화는 고유 비밀번호가 "한 번만" 재설정된다는 X11 매뉴얼 문구가 있으므로 주의.
