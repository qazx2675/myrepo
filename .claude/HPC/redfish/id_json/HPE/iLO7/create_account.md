# HPE iLO 7 - 새 계정 id/pw 등록 (best-effort, 사용자 모델 없음)

플레이스홀더만 사용. 호출 계정은 `UserConfigPriv` 필요. **애플리케이션 계정(AppAccounts) 세션으로는 계정 생성 불가.**
iLO 7 은 in-band 도 인증 필수(vNIC) - 아래는 out-of-band(`<BMC_IP>`) 기준.

## Redfish
- `POST https://<BMC_IP>/redfish/v1/AccountService/Accounts/` (iLO 5/6 과 동일 요청 본문, HPE managingusers 문서가 iLO 7 까지 공통으로 서술)
```json
{ "UserName": "<NEW_USER>", "Password": "<NEW_PASSWORD>", "RoleId": "Operator" }
```
```
curl -sk -u <ADMIN_USER>:<ADMIN_PASSWORD> -H "Content-Type: application/json" \
  -X POST https://<BMC_IP>/redfish/v1/AccountService/Accounts/ \
  -d '{"UserName":"<NEW_USER>","Password":"<NEW_PASSWORD>","RoleId":"Operator"}'
```
- RoleId: Administrator / Operator / ReadOnly(생략 시 ReadOnly, Login 만). 권한 직접 지정은 `Oem.Hpe.Privileges` (LoginPriv, RemoteConsolePriv, VirtualMediaPriv, VirtualPowerAndResetPriv, HostBIOSConfigPriv, HostNICConfigPriv, HostStorageConfigPriv, iLOConfigPriv, UserConfigPriv, SystemRecoveryConfigPriv). `Oem.Hpe.LoginName` 은 설명용 이름.
- 문서의 최신 계정 예시: `@odata.type #ManagerAccount.v1_12_1`, `AccountTypes:["WebUI"]`, `PasswordChangeRequired:false`, `Oem.Hpe.ServiceAccount:false`. `AccountTypes` 를 생성 시 지정 가능한지는 unverified.
- 검증: verified (문서) / 실장비 응답 코드 unverified.

## ilorest (6.0.0.0+)
```
ilorest login <BMC_IP> -u <ADMIN_USER> -p <ADMIN_PASSWORD>
ilorest iloaccounts add <NEW_USER> "<LOGIN_NAME>" <NEW_PASSWORD> --role Operator
ilorest logout
```
- 인자 순서 `USERNAME LOGINNAME PASSWORD`. 검증: verified (ilorest 가이드, iLO 4/5 중심 서술 + iLO 7 에도 동일 명령군; iLO 7 전용 예시는 확인 못함).

## 대체/미확인
- 웹 UI: Administration > User Administration > New (문서 직접 확인 못함, iLO 5/6 과 동일 추정).
- RIBCL/HPONCFG/SSH CLI/IPMI: iLO 7 지원 근거 없음 -> unverified / unavailable.
- 애플리케이션 계정 생성은 기존 iLO 사용자 자격증명으로 한 번만 수행되며(`ilorest appaccount ...` 계열, 정확한 서브명령 미확인) 일반 사용자 계정과 별도 개념.
