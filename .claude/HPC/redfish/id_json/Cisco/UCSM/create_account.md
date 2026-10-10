# Cisco UCSM - 새 계정 id/pw 등록 (xml)

적용: B200 M4, B200 M5, B480 M5, X210c M7(UCSM 관리 시). **Redfish 없음 - XML API / CLI / GUI.**
플레이스홀더만 사용. `-k` 는 자체서명 인증서용. 요청 본문은 한 줄 XML 이어도 됨.

## 권한
호출 계정이 `aaa` 또는 `admin` 권한 (ucsmsdk `AaaUser` mo_meta 권한 `["aaa","admin"]`).

## XML API (엔드포인트 `POST https://<UCSM_IP>/nuova`, Content-Type: text/xml)
1) 로그인 (쿠키 획득, 기본 유효시간은 세션 정책)
```
curl -sk -H "Content-Type: text/xml" https://<UCSM_IP>/nuova \
  -d '<aaaLogin inName="<ADMIN_USER>" inPassword="<ADMIN_PASSWORD>"/>'
```
응답 `<aaaLogin ... outCookie="<COOKIE>" .../>`.

2) 계정 생성 - `configConfMo` (DN = `sys/user-ext/user-<NEW_USER>`)
```
curl -sk -H "Content-Type: text/xml" https://<UCSM_IP>/nuova -d '
<configConfMo cookie="<COOKIE>" dn="sys/user-ext/user-<NEW_USER>" inHierarchical="false">
  <inConfig>
    <aaaUser name="<NEW_USER>" pwd="<NEW_PASSWORD>" accountStatus="active"
             expires="no" firstName="" lastName="" email="" phone=""
             dn="sys/user-ext/user-<NEW_USER>" status="created,modified">
      <aaaUserRole name="read-only" status="created"/>
    </aaaUser>
  </inConfig>
</configConfMo>'
```
- `status="created"` 는 이미 있으면 오류, `"created,modified"` 는 있으면 수정(ucsmsdk `add_mo(modify_present=True)` 와 동일 의미). 
- `aaaUserRole` 자식이 역할 부여(`admin`, `read-only`, `operations`, `aaa` 등). 역할 name 은 `[-.:_a-zA-Z0-9]{1,16}`.
- 만료일: `expires="yes" expiration="2030-12-31T00:00:00"` (설정 후 never 로 되돌릴 수 없음 - Cisco 문서).
- 성공 응답: `<configConfMo ...><outConfig><aaaUser ... /></outConfig></configConfMo>` (pwd 는 반환 안 됨, `pwdSet="yes"`). 실패: `<error ... errorCode errorDescr/>` (코드표는 확인 못함).

3) 로그아웃: `<aaaLogout inCookie="<COOKIE>"/>`

## ucsmsdk (파이썬; 위 XML 과 동일 호출, 소스 이중검증용)
```python
from ucsmsdk.ucshandle import UcsHandle
from ucsmsdk.mometa.aaa.AaaUser import AaaUser
from ucsmsdk.mometa.aaa.AaaUserRole import AaaUserRole
h = UcsHandle("<UCSM_IP>", "<ADMIN_USER>", "<ADMIN_PASSWORD>"); h.login()
u = AaaUser(parent_mo_or_dn="sys/user-ext", name="<NEW_USER>", pwd="<NEW_PASSWORD>", account_status="active")
AaaUserRole(parent_mo_or_dn=u, name="read-only")
h.add_mo(u, modify_present=False); h.commit(); h.logout()
```

## CLI (SSH `ssh <ADMIN_USER>@<UCSM_IP>`) - Cisco UCSM 6.0 CLI 가이드 절차
```
UCS-A# scope security
UCS-A /security # create local-user <NEW_USER>
UCS-A /security/local-user* # set account-status active
UCS-A /security/local-user* # set password          (프롬프트에서 2회 입력)
UCS-A /security/local-user* # set firstname <FIRST>  (선택, lastname/expiration <mon> <day> <year>/email/phone/sshkey 도 선택)
UCS-A /security/local-user* # create role read-only  (역할 부여, 명령 이름은 RBAC 장 일반 문법 - 예시는 문서 미열람, unverified)
UCS-A /security/local-user* # commit-buffer
```
- `create local-user ... / set account-status / set password / commit-buffer` 순서는 verified (6.0 가이드 예시 `kikipopo`).
- 역할 부여 CLI 줄은 미열람(unverified) - GUI 또는 XML `aaaUserRole` 사용 권장.

## 제약 / 정책 요약 (상세는 README)
- 사용자명 1~32자, 영문 시작, `[A-Za-z0-9_.-]`, 숫자만 불가, 예약어(root, bin, daemon, ... debug) 불가, 이름 변경 불가.
- 암호 강도 검사 ON 이면 8~127자, 4종 중 3종, 동일문자 3연속 초과 불가, 사용자명 포함 불가, `$ ? =` 불가. OFF 면 1~127자. 
- 도메인당 최대 48 로컬 사용자.
- 암호 프로파일 `historyCount`, 변경 간격 등은 신규 계정에도 적용.

## 대체
- GUI: Admin > User Management > User Services > Locally Authenticated Users > Add.
- Intersight 로 UCSM 도메인을 claim 해도 UCSM 로컬 계정 CRUD 는 UCSM 에서 한다 (Intersight 로컬 사용자 정책은 IMM 서버 전용 - `../Intersight/`).
