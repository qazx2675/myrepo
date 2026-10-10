# Cisco CIMC (standalone C-Series) - 기존 id 패스워드 변경

참고용(사용자 보유 모델 없음).

## Redfish
```
curl -sk -u <TARGET_USER>:<OLD_PASSWORD> -H "Content-Type: application/json" -X PATCH \
  https://<CIMC_IP>/redfish/v1/AccountService/Accounts/<ID> -d '{"Password":"<NEW_PASSWORD>"}'
```
- 4.2 가이드: 공장 초기화 후 `GET .../Accounts/1` 에서 `"PasswordChangeRequired": true` 이면 위 PATCH 로 변경, 이후 `false`. 기본 admin 은 슬롯 1.
- 다른 사용자: admin 이 PATCH (3.0 "Modifying User" 는 UserName/Password/RoleId/Enabled 를 함께 보냄).
- 이전 암호 재사용 금지: `PATCH /redfish/v1/AccountService` `Oem.Cisco.PasswordHistory` (정책, 0~5 = XML `passwordHistory` 범위).

## XML
imcsdk `local_user_modify`: 이름으로 `aaaUser` 를 찾아 `pwd` 를 설정 후 `set_mo`.
```
<configConfMo cookie="<COOKIE>" dn="sys/user-ext/user-<ID>" inHierarchical="false">
  <inConfig><aaaUser id="<ID>" pwd="<NEW_PASSWORD>"/></inConfig>
</configConfMo>
```
- 4.2(3b)+ 에서 기본 자격으로 XML API 로그인 거부(Cisco 커뮤니티 스레드, CSCwc46717 언급, 직접 열람 실패 - 검색 요약만, unverified). 먼저 Redfish/웹으로 기본 암호를 변경.

## CLI
`scope user <N>` / `set password` / `commit`.

## 암호 규칙
8~20자(Redfish), 강한 정책 ON 시 복잡도. 만료/히스토리 정책: README.
