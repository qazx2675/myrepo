# Dell iDRAC8 - 새 계정 id/pw 등록 (사용자 보유 모델 없음, 참고용)

```
curl -sk -u <ADMIN_USER>:<ADMIN_PASSWORD> -H "Content-Type: application/json" \
  -X PATCH https://<BMC_IP>/redfish/v1/Managers/iDRAC.Embedded.1/Accounts/<SLOT> \
  -d '{"UserName":"<NEW_USER>","Password":"<NEW_PASSWORD>","RoleId":"Administrator","Enabled":true}'
```
- SLOT = 빈 슬롯(UserName "") 3~16. 빈 슬롯 조회: `GET /redfish/v1/Managers/iDRAC.Embedded.1/Accounts?$expand=*($levels=1)`. RoleId Administrator|Operator|ReadOnly|None. 성공 200. (verified: Dell 스크립트)
- Redfish 는 FW 2.40.40.40 이상. 그 미만은 RACADM/IPMI/웹 UI.
- IPMI 권한 등 OEM 속성은 SCP(XML) import: Export `POST /redfish/v1/Managers/iDRAC.Embedded.1/Actions/Oem/EID_674_Manager.ExportSystemConfiguration` -> `<Attribute Name="Users.<SLOT>#UserName">`, `#Password`, `#Privilege`(511=Admin), `#IpmiLanPrivilege`(Administrator 등), `#Enable` 수정 -> `...EID_674_Manager.ImportSystemConfiguration` `{"ImportBuffer":"<XML>","ShareParameters":{"Target":["IDRAC"]}}` (엔드포인트명은 Dell 스크립트 iDRAC9 이하 분기, iDRAC8 동일 여부는 unverified).
- RACADM: `racadm set iDRAC.Users.<SLOT>.UserName <NEW_USER>` / `.Password <NEW_PASSWORD>` / `.Privilege 0x1ff` / `.Enable 1` (일부 verified, README 참고).
- 웹 UI: iDRAC Settings > User Authentication > Users.
