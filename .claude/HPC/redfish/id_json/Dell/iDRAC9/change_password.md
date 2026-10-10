# Dell iDRAC9 - 기존 id 패스워드 변경

적용 모델: R640, C6420, R840, DSS8440, R740, R750, R750xa, R750xs, R760XA, XE9680, R6615, R660, R860, C6620

## Redfish (verified: Dell ChangeIdracUserPasswordREDFISH.py)
```
curl -sk -u <ADMIN_USER>:<CURRENT_PASSWORD> -H "Content-Type: application/json" \
  -X PATCH https://<BMC_IP>/redfish/v1/Managers/iDRAC.Embedded.1/Accounts/<SLOT> \
  -d '{"Password":"<NEW_PASSWORD>"}'
```
- 성공: HTTP 200 (스크립트 기준). root 는 슬롯 2.
- 슬롯 찾기: `GET /redfish/v1/Managers/iDRAC.Embedded.1/Accounts?$expand=*($levels=1)` 에서 UserName 확인.
- 세션 토큰(X-Auth-Token)으로 호출했고 그 토큰의 계정 암호를 바꾸면 토큰이 무효가 되므로 재생성 필요(스크립트 안내문).

## 공장 기본 암호 -> 변경 강제 상태 (verified: 동일 스크립트 `--force-change-enabled`)
기본 암호 변경 강제가 켜져 있으면 일반 Accounts PATCH 대신 DellAttributes 로 root(슬롯 2) 암호를 설정:
```
curl -sk -u root:<DEFAULT_OR_LABEL_PASSWORD> -H "Content-Type: application/json" \
  -X PATCH https://<BMC_IP>/redfish/v1/Managers/iDRAC.Embedded.1/Oem/Dell/DellAttributes/iDRAC.Embedded.1 \
  -d '{"Attributes":{"Users.2.Password":"<NEW_PASSWORD>"}}'
```
- 기본 계정 `root`. 암호는 서버 라벨의 고유 암호 또는 레거시 `calvin` (Dell KB 000133536).

## 정책
- 암호 길이: FW 3.xx 1~20, 4.xx~6.xx 1~40, 7.xx 1~127 (Dell KB 000177787). 대·소문자·숫자·특수문자 포함, 8자 이상 권장.
- 7.30.10.50 이상(14G 는 7.00.00.184) Basic 인증 `Unadvertised`: 첫 요청부터 자격증명 전송 필요.
- IPMI 로 쓰는 계정은 암호 20자 제한 가능성(IPMI 표준) - unverified.

## 대체
| 도구 | 명령 | 상태 |
|---|---|---|
| RACADM | `racadm set iDRAC.Users.<SLOT>.Password <NEW_PASSWORD>` (root 기본 슬롯 2, 인덱스 1~16) | verified (Dell KB 000177787 + PowerShell 모듈) |
| 웹 UI | iDRAC Settings > User > 사용자 ID 선택 > Edit (기본 암호 경고 페이지에서도 변경) | verified (Dell KB) |
| SCP | Export 후 `Users.<SLOT>#Password` 수정하여 Import (Dell 스크립트 `--new-password` 예시) | verified(스크립트) |
| ipmitool | `ipmitool -I lanplus -H <BMC_IP> -U <USER> -P <PASS> user set password <ID> <NEW_PASSWORD>` | unverified (일반 문법) |
| Ansible | dellemc.openmanage.idrac_user `user_name`, `user_password`, `state: present` | verified (모듈 소스) |
