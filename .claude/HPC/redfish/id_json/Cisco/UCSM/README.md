# Cisco UCSM - 계정 id/pw 등록·패스워드 변경

## 프로토콜 판정: **xml** (UCSM XML API, 엔드포인트 `https://<UCSM_VIP>/nuova`). Redfish 계정 API 없음.
- 관리망: UCS Manager (Fabric Interconnect 의 VIP 또는 FI-A/B). 계정은 **블레이드가 아니라 UCSM 도메인** 에 만든다. 블레이드 개별 BMC(CIMC)용 계정 API 는 없음.
- 해당 사용자 모델: **B200 M4, B200 M5, B480 M5** (UCSM 관리 전용 블레이드), **X210c M7 (UCSM 관리 모드일 때)**. X210c M7 이 Intersight Managed Mode(IMM) 이면 `../Intersight/` 참조.
- **UNSUPPORTED (Redfish)**: 위 모델이 UCSM 관리일 때 Redfish AccountService 로 계정을 만들 수 없음 (UCSM 에 문서화된 Redfish 없음 - 이전 BIOS 조사 `bios_json/back/Cisco` 결론과 동일, "미문서화"이며 존재 불가의 증명은 아님). xml/CLI/GUI 만 사용.
- Jev 생략 (API 키 없음). 판정은 문서 + 공개 SDK 소스 2중 출처.

## 방식 요약
| 동작 | 방법 | 검증 |
|---|---|---|
| 로그인 | `POST /nuova` `<aaaLogin inName=".." inPassword=".."/>` -> `outCookie` | verified (ucsmsdk `ucsmethodfactory.aaa_login`, `ucssession.py` 의 `"/nuova"`) |
| 계정 생성 | `configConfMo` dn=`sys/user-ext/user-<NAME>` inConfig=`<aaaUser name pwd accountStatus .../>` | verified (ucsmsdk AaaUser 메타 rn=`user-[name]`, parent `aaaUserEp`=`sys/user-ext`; `ucshandle.add_mo` status=created + Cisco CLI 대응 속성) |
| 암호 변경 | 같은 DN 에 `configConfMo` `<aaaUser pwd="..."/>` (status=modified) | verified (ucsmsdk `set_mo` status=modified, `pwd` READ_WRITE) |
| CLI 대체 | `scope security` / `create local-user` / `set password` / `commit-buffer` | verified (Cisco UCSM 6.0 CLI Admin Mgmt Guide RBAC 장 + 2.2 CLI 가이드 검색 결과) |

## 계정 객체 (ucsmsdk `AaaUser` 메타 원문 기준)
- 클래스 `aaaUser`, rn `user-[name]`, DN `sys/user-ext/user-<name>`, 부모 `aaaUserEp`(`sys/user-ext`), 자식 `aaaUserRole`(역할), `aaaUserLocale`, `aaaSshAuth`.
- 속성: `name`(naming, 정규식 `[a-zA-Z][a-zA-Z0-9_.-]{0,31}`), `pwd`(0~127자, 허용 문자 `!"#%&'()*+,-./:;<>@[\]^_` + 백틱 + `{|}~` 영숫자, **`$ ? =` 및 공백 불가**), `accountStatus`(active/inactive), `expires`(yes/no), `expiration`(`YYYY-MM-DDTHH:MM:SS` 또는 never), `firstName/lastName`(0~32), `email`, `phone`, `descr`, `clearPwdHistory`(yes/no), `pwdSet`(읽기전용), `priv`(읽기전용, 역할에서 계산), `passwdexpiration/passwdexpirystatus`(읽기전용, UCSM 4.1.3a+).
- 역할: 자식 `aaaUserRole` rn `role-[name]` (name `[-.:_a-zA-Z0-9]{1,16}`). 기본 역할 이름은 admin, aaa, read-only, operations, network, server-equipment, server-profile, server-security, storage, facility-manager (UCSM 문서 지식, 이번 조사에서 원문 재확인 못함 - unverified). `priv` 정규식에 admin, aaa, read-only, operations, ls-*, pn-*, ext-* 등 권한 토큰이 열거됨(SDK 원문).

## 기본 계정 / 정책
- **admin**: 도메인마다 기본 admin 계정이 있고 수정·삭제 불가, 전체 권한, 항상 active, 만료 없음, 비활성화 불가. 기본 암호 없음 - 초기 설정(setup) 시 지정 (UCSM 6.0 CLI Admin Mgmt Guide, verified 1출처 + 2.x 관행).
- 로컬 사용자 최대 48개/도메인, 사용자명 고유·대소문자 구분, 생성 후 이름 변경 불가(삭제 후 재생성), 숫자만으로 된 이름 불가, 시스템 예약어(root, bin, daemon ... debug 등 27개) 사용 불가.
- **암호 강도 검사** (`aaaUserEp.pwdStrengthCheck` yes/no; CLI `scope security` 의 `show enforce-strong-password` 로 확인 - set 문법은 unverified): 활성 시 8~127자, 소문자/대문자/숫자/특수문자 중 **3종 이상**, 동일 문자 3회 초과 연속 금지, 사용자명/역순 금지, 사전 단어 금지, `$ ? =` 금지. 비활성 시 1~127자 (Cisco 6.0 Password Management 장). 
- **암호 프로파일** (`aaaPwdProfile`, DN `sys/user-ext/pwd-profile`, 전체 로컬 사용자에 공통 적용 - 사용자별 지정 불가; `historyCount` 외 항목은 admin/aaa 권한 사용자에 미적용):
  | 속성 | 범위 | 기본 |
  |---|---|---|
  | `historyCount` (이전 암호 재사용 금지 횟수) | 0~15 | **0** (재사용 허용) |
  | `changeDuringInterval` / `changeCount` / `changeInterval` | enable/disable, 0~10, 1~745시간 | 기본 enable, 48시간 내 2회 |
  | `noChangeInterval` | 1~745시간 | 24시간 |
  | `enablePWDExpiry` / `expirationPeriod` / `expirationWarnTime` | yes/no, 1~180일, 0~30일 | 문서 기본 90일/15일 |
  | `minPassphraseLen` | 6~80 | (SDK 메타) |
  이상은 ucsmsdk `AaaPwdProfile` 메타와 Cisco 문서 표가 일치 (이중 검증). 운용 팁: `historyCount>0` 이면 변경 시 이전 N개 암호와 같을 수 없음, 일시 해제는 `clearPwdHistory="yes"` (CLI `set clear password-history yes`).
- 암호 암호화 키: UCSM 4.2(3d)+ 는 백업 파일 생성 전 `set password-encryption-key` 필요 (계정과 무관).

## 한계 - 블레이드 개별 CIMC
- UCSM 관리 블레이드의 CIMC 는 UCSM 이 관리하며 독립 로컬 계정 CRUD 를 제공하지 않는다. 각 블레이드 CIMC/KVM IP 는 UCSM 의 ext-mgmt IP 풀에서 할당되고(Cisco 커뮤니티/KVM 가이드), KVM 접근은 UCSM 사용자 + 권한(`ls-ext-access` 포함 역할)으로 이뤄진다. UCSM 로컬 계정이 블레이드 CIMC 의 직접 로그인 계정으로 복제되는지는 **미확인(unverified)** - 확인되지 않았으므로 CIMC 직접 로그인용 계정 생성법을 이 문서에 쓰지 않는다.
- 블레이드에 Redfish `/redfish/v1/AccountService` 가 열려 있는지: 문서화 없음(unknown). 실장비에서 `GET https://<KVM_IP>/redfish/v1/` 로 확인 가능.

## 시도 내역 (재시도 포함)
- 1차(Cisco 문서): UCSM 6.0 CLI Admin Mgmt Guide - RBAC 장(계정 가이드/생성 CLI/예약어/admin), Password Management 장(강도/프로파일/히스토리 CLI). 4.3 GUI 가이드 URL 은 404, 2.0/2.2 CLI 가이드는 WebFetch 가 인덱스로 리다이렉트 (검색 결과 스니펫만 확보). 직접 curl 은 Akamai 403.
- 2차(공개 코드): ucsmsdk `AaaUser.py`, `AaaUserRole.py`, `AaaUserEp.py`, `AaaPwdProfile.py` 원문 메타 직접 읽기, `ucsmethodfactory.py`, `ucshandle.py`, `ucssession.py`. -> 위 표의 속성·범위는 SDK 와 Cisco 문서 일치.
- 미해결: 기본 역할 목록 원문, ext-mgmt 계정 연동, 블레이드 Redfish (모두 unverified/unknown, 상기 표기).
- 실장비 응답 코드/오류 메시지는 확인하지 못함 (문서 기반).

## 파일
- `create_account.md`, `change_password.md`
