# Dell iDRAC8 - 기존 id 패스워드 변경 (사용자 보유 모델 없음, 참고용)

```
curl -sk -u <ADMIN_USER>:<CURRENT_PASSWORD> -H "Content-Type: application/json" \
  -X PATCH https://<BMC_IP>/redfish/v1/Managers/iDRAC.Embedded.1/Accounts/<SLOT> \
  -d '{"Password":"<NEW_PASSWORD>"}'
```
- 성공 200 (verified: Dell ChangeIdracUserPasswordREDFISH.py, iDRAC10 미만 공통 분기). root 슬롯 2.
- RACADM: `racadm set iDRAC.Users.<SLOT>.Password <NEW_PASSWORD>` (iDRAC9 KB 구문, iDRAC8 동작은 커뮤니티 set 사례로 간접 확인, unverified).
- SCP(XML) import 로 `Users.<SLOT>#Password` 변경 가능 (Dell 스크립트 `--new-password`, verified).
- 웹 UI: iDRAC Settings > Users > Edit.
- 기본 `root`/`calvin`. 암호 길이 한도는 문서 확인 못함(unavailable).
