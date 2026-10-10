# Cisco UCSM - 기존 id 패스워드 변경 (xml)

적용: B200 M4, B200 M5, B480 M5, X210c M7(UCSM 관리 시). Redfish 없음.

## XML API
같은 DN 에 `pwd` 만 재설정 (status=modified). 로그인/로그아웃은 `create_account.md` 와 동일.
```
curl -sk -H "Content-Type: text/xml" https://<UCSM_IP>/nuova -d '
<configConfMo cookie="<COOKIE>" dn="sys/user-ext/user-<TARGET_USER>" inHierarchical="false">
  <inConfig>
    <aaaUser name="<TARGET_USER>" pwd="<NEW_PASSWORD>" dn="sys/user-ext/user-<TARGET_USER>" status="modified"/>
  </inConfig>
</configConfMo>'
```
- 권한: `aaa`/`admin` 이 다른 사용자 암호를 설정. 일반 사용자 본인 변경은 GUI/CLI 의 본인 변경 경로 (XML 본인 변경 제약은 미확인).
- ucsmsdk: `u = h.query_dn("sys/user-ext/user-<TARGET_USER>"); u.pwd = "<NEW_PASSWORD>"; h.set_mo(u); h.commit()` (`set_mo` -> status=modified, 소스 확인).
- 응답 `<outConfig><aaaUser ... pwdSet="yes"/></outConfig>`; 암호는 반환되지 않음.

## 이전 암호 재사용 / 변경 빈도 제한
- `aaaPwdProfile`(`sys/user-ext/pwd-profile`) `historyCount` 0~15 (기본 0 = 재사용 허용). N>0 이면 최근 N개 암호와 동일하면 거부.
- 재사용 허용이 필요하면 해당 사용자에 `clearPwdHistory="yes"` (XML: `<aaaUser name=".." clearPwdHistory="yes" status="modified"/>`, CLI `scope local-user <U>` / `set clear password-history yes` / `commit-buffer`). admin 권한 필요.
- `changeDuringInterval`(기본 enable, 48시간 내 2회) 와 `noChangeInterval`(기본 24시간) 로 짧은 시간 내 연속 변경이 거부될 수 있음. admin/aaa 계정에는 `historyCount` 외 프로파일 항목이 적용되지 않음 (Cisco 문서).
- 정책 변경 예 (XML):
```
<configConfMo cookie="<COOKIE>" dn="sys/user-ext/pwd-profile" inHierarchical="false">
  <inConfig><aaaPwdProfile dn="sys/user-ext/pwd-profile" historyCount="5" status="modified"/></inConfig>
</configConfMo>
```
  (속성명은 ucsmsdk `AaaPwdProfile` 메타; DN 은 mo_meta rn `pwd-profile` + 부모 `sys/user-ext`.)

## CLI
```
UCS-A# scope security
UCS-A /security # scope local-user <TARGET_USER>
UCS-A /security/local-user # set password          (2회 입력)
UCS-A /security/local-user* # commit-buffer
```
- `scope local-user` + `set clear password-history yes` 형태는 Cisco 6.0 가이드 예시(admin) 확인. `set password` 는 계정 생성 절차에서 확인; 기존 사용자에 대한 동작은 같은 모드이므로 동일하나 단독 예시는 미열람(unverified).

## admin 계정
- admin 은 삭제/비활성화 불가이지만 암호 변경은 가능 (XML/CLI 동일). 분실 시 FI 별 복구 절차가 필요 (Cisco 문서 Password Recovery - 이번에 열람 안 함).

## 암호 규칙
README 의 "기본 계정 / 정책" 참조 (8~127자, 4종 중 3종, `$ ? =` 불가 등).
