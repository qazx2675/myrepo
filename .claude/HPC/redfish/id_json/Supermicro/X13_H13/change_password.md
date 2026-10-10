# Supermicro X13/H13 - 기존 id 패스워드 변경

적용 모델: AS-1115HS-TNR (H13SSH)

## Redfish
- `PATCH https://<BMC_IP>/redfish/v1/AccountService/Accounts/{num}` (`num` = 사용자 ID, ADMIN 은 보통 2)
- 상태: **unverified** - Supermicro Redfish 가이드(4.0, 6.1)에는 Enabled PATCH 예시만 있고 Password PATCH 예시는 없음. 표준 Redfish ManagerAccount 동작 + 서드파티 스크립트(flaviotorres/supermicro-redfish `update_user_password.sh`: `GET Accounts` -> 각 `Accounts/{id}` 의 UserName 일치 확인 -> `PATCH Accounts/{id}`) 근거.
```json
{ "Password": "<NEW_PASSWORD>" }
```
```
# 슬롯 찾기
curl -sk -u <ADMIN_USER>:<CURRENT_PASSWORD> https://<BMC_IP>/redfish/v1/AccountService/Accounts
curl -sk -u <ADMIN_USER>:<CURRENT_PASSWORD> https://<BMC_IP>/redfish/v1/AccountService/Accounts/<SLOT>
# 변경
curl -sk -u <ADMIN_USER>:<CURRENT_PASSWORD> -H "Content-Type: application/json" \
  -X PATCH https://<BMC_IP>/redfish/v1/AccountService/Accounts/<SLOT> \
  -d '{"Password":"<NEW_PASSWORD>"}'
```
- 성공 코드 문서 미표기 (표준은 200/204). 412/428 이 나오면 GET 의 ETag 를 `If-Match` 로 전송 (unverified).
- 같은 PATCH 로 `Enabled`, `RoleId` 변경 가능한 것으로 보이나 RoleId PATCH 는 Supermicro 문서에 예시 없음 (unverified). 활성 세션이 있는 계정은 `Enabled:false` 불가 (문서).
- 계정 형식(AccountTypes) 이 `IPMI` 만이면 Redfish 로그인/PATCH 대상이 아닐 수 있음 (BMC 01.05.xx 이상 분리).
- 정책 확인: `GET /redfish/v1/AccountService` -> `PasswordGuidanceMessage`.

## 규칙
- 8~20자 (웹 UI), IPMI 도구 쪽은 8~19자 -> 19자 이하 권장. 사용자명 역순 금지, 영문 소/대/숫자/특수 중 3종 이상.
- Redfish 6.1 예시 문구는 공백, 탭, `"`, `'`, `:`, `,`, `#`, `-`, `;` 사용 불가. 셸에서 보낼 때 특수문자 따옴표 처리 주의.
- 잠금: 연속 실패 임계 1~5회. 잠금 해제: X13 매뉴얼에 Unlock 항목이 없어 확인 불가(unavailable). 잠금 시간 경과를 기다리거나 다른 Administrator 로 Users 표에서 확인 (X12 매뉴얼에는 Actions 의 unlock 항목 있음).
- 기본 ADMIN 은 보드 라벨의 고유 비밀번호로 시작. 최초 변경 시 `ADMIN` 으로 되돌리는 것은 가능하나 권장하지 않음.

## 대체 1: ipmitool (Supermicro FAQ 41692 로 확인)
```
ipmitool -I lanplus -H <BMC_IP> -U <ADMIN_USER> -P <CURRENT_PASSWORD> user list 1
ipmitool -I lanplus -H <BMC_IP> -U <ADMIN_USER> -P <CURRENT_PASSWORD> user set password <ID> <NEW_PASSWORD> 20
```
- FAQ 는 호스트에서 root 로 `ipmitool user list 1` / `ipmitool user set password 2 [new password]` 로 안내. 마지막 `20` 은 ipmitool 의 20바이트 키 길이 옵션(일반 문법, 길이 16 이하면 생략 또는 16).
- 채널 번호 1 은 FAQ 기준. 보드에 따라 다르면 `ipmitool lan print <ch>` 로 LAN 채널 확인.

## 대체 2: SMCIPMITool
```
SMCIPMITool <BMC_IP> <ADMIN_USER> <CURRENT_PASSWORD> user list
SMCIPMITool <BMC_IP> <ADMIN_USER> <CURRENT_PASSWORD> user setpwd <ID> <NEW_PASSWORD>
SMCIPMITool <BMC_IP> <ADMIN_USER> <CURRENT_PASSWORD> user test <USER> <NEW_PASSWORD>
```
- 문법은 가이드 2.27 (2023-02). `user passwd` 라는 서브커맨드는 없음.

## 대체 3: IPMICFG (대역내)
```
./IPMICFG-Linux.x86_64 -user list
./IPMICFG-Linux.x86_64 -user setpwd <ID> <NEW_PASSWORD>
```
(Supermicro FAQ 41692 + IPMICFG 1.24.0 가이드.)

## 대체 4: SUM
```
sum -i <BMC_IP> -u <ADMIN_USER> -p <CURRENT_PASSWORD> -c SetBmcPassword --user_id <ID> --new_password <NEW_PASSWORD> --confirm_password <NEW_PASSWORD>
sum -i <BMC_IP> -u <ADMIN_USER> -p <CURRENT_PASSWORD> -c SetBmcPassword --user_id <ID> --pw_file passwd.txt
```
- `--user_id` 생략 시 ID 2(Administrator). `--pw_file` 은 새 암호 한 줄 파일. SUM 가이드 2.4 기준; X13/H13 지원은 SUM 제품 페이지 "X10 ~ X13/H13". 실행에 노드 제품키가 필요할 수 있음 (SetBmcPassword 개별 요구는 문서 미확인).

## 대체 5: 웹 UI
- **Configuration > Account Services > Users** 에서 해당 사용자 연필 아이콘(Modify) -> 암호 입력 (눈 아이콘으로 미리보기). 매뉴얼상 "기본 ADMIN 은 수정 불가" 라는 문구가 있으므로 ADMIN 자신의 암호는 로그인 후 본인 계정 메뉴, 또는 위 Redfish/IPMI 로 변경.
- Operator/User 권한은 자기 자신의 암호만 변경 가능 (매뉴얼).

## 분실 시
- 호스트에서 IPMICFG / ipmitool(root) 로 ID 2 암호 재설정 (FAQ 41692). 또는 Supermicro 가 제공하는 BMC 비밀번호 재설정 스크립트(https://www.supermicro.com/bmcpassword, 링크만 기록, 내용 미확인).
- 공장 초기화: BMC 웹 UI Maintenance 메뉴(BMC Reset 하위, X13 매뉴얼 2.8 의 Factory Default 항목)의 3옵션 (README 참조). 고유 비밀번호 복귀 옵션과 `ADMIN/ADMIN` 옵션이 별개.
