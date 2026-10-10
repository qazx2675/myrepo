# Supermicro X12/H12 - 기존 id 패스워드 변경

해당 사용자 모델 없음 (참고용).

## Redfish (unverified: Supermicro 문서에 Password PATCH 예시 없음, 표준 Redfish)
```
curl -sk -u <ADMIN_USER>:<CURRENT_PASSWORD> https://<BMC_IP>/redfish/v1/AccountService/Accounts
curl -sk -u <ADMIN_USER>:<CURRENT_PASSWORD> -H "Content-Type: application/json" \
  -X PATCH https://<BMC_IP>/redfish/v1/AccountService/Accounts/<SLOT> \
  -d '{"Password":"<NEW_PASSWORD>"}'
```
- 번호 `<SLOT>` = IPMI 사용자 ID (ADMIN 은 2). 비밀번호 8~19자, 3종 이상, 사용자명 역순 금지.
- 잠금 계정: 웹 UI Users 표 Actions 의 **unlock** (X12 매뉴얼). 잠금 임계 1~5회.

## 대체
- **웹 UI:** Configuration > Account Services > Users > 해당 행의 연필(Modify) -> 암호 입력 (눈 아이콘으로 확인).
- **ipmitool (Supermicro FAQ 41692):**
  ```
  ipmitool -I lanplus -H <BMC_IP> -U <ADMIN_USER> -P <CURRENT_PASSWORD> user list 1
  ipmitool -I lanplus -H <BMC_IP> -U <ADMIN_USER> -P <CURRENT_PASSWORD> user set password <ID> <NEW_PASSWORD> 20
  ```
- **SMCIPMITool:** `SMCIPMITool <BMC_IP> <ADMIN_USER> <CURRENT_PASSWORD> user setpwd <ID> <NEW_PASSWORD>`
- **IPMICFG:** `./IPMICFG-Linux.x86_64 -user setpwd <ID> <NEW_PASSWORD>`
- **SUM:** `sum -i <BMC_IP> -u <ADMIN_USER> -p <CURRENT_PASSWORD> -c SetBmcPassword --user_id <ID> --new_password <NEW_PASSWORD> --confirm_password <NEW_PASSWORD>` (`--user_id` 생략 시 2, `--pw_file` 가능)
- **분실/초기화:** 호스트 root 에서 ipmitool/IPMICFG 로 ID 2 재설정. BMC 공장 초기화는 고유 비밀번호 또는 `ADMIN/ADMIN` 선택 (X13 매뉴얼 Factory Default, X12 도 같은 구조로 추정). 재설정 스크립트: https://www.supermicro.com/bmcpassword (링크만, 내용 미확인).
