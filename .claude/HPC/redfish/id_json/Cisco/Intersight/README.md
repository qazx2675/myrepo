# Cisco Intersight (IMM) - X-Series 서버 계정 id/pw 등록·패스워드 변경

## 프로토콜 판정: **json (Intersight REST API, OData `/api/v1`) - Redfish 아님**
- 대상: **UCS X210c M7 이 Intersight Managed Mode(IMM, FI 가 IMM 모드) 로 관리될 때.** 사용자 모델 중 B200 M4 / B200 M5 / B480 M5 는 IMM 대상이 아님(UCSM 전용 -> `../UCSM/`).
- 서버 BMC(IMC) 로컬 계정은 서버에 직접 만들지 않고 Intersight **Local User Policy(`iam.EndPointUserPolicy`)** 를 서버 프로파일에 붙여 배포한다. 정책 안의 사용자: `iam.EndPointUser`(이름) + `iam.EndPointUserRole`(비밀번호/활성/역할 연결) + `iam.EndPointRole`(역할 정의, Type=IMC).
- 서버 접근 IP(KVM/OOB/Inband)는 **IMC Access Policy(`access.Policy`)** — 필드: Name, out_of_band(true=OOB, false=Inband), VLAN(Inband 시 필수), IP Pool (Ansible `intersight_imc_access_policy` 소스).
- Jev 생략. 판정: Ansible `cisco.intersight` 모듈 소스(`intersight_local_user_policy.py`, `intersight_imc_access_policy.py`) + netascode 데이터모델 문서 + 커뮤니티 스레드(검색 요약) 3출처.

## 방식 요약
| 동작 | 호출 | 검증 |
|---|---|---|
| 정책 생성 | `POST /api/v1/iam/EndPointUserPolicies` `{Name, PasswordProperties{EnforceStrongPassword, EnablePasswordExpiry, PasswordHistory}, Organization{Moid}}` | verified (Ansible 소스) |
| 사용자 객체 | `POST /api/v1/iam/EndPointUsers` `{Name, Organization}` | verified (Ansible 소스) |
| 역할 조회 | `GET /api/v1/iam/EndPointRoles?$filter=Name eq 'admin' and Type eq 'IMC'` (Name: admin/readonly/user) | verified (Ansible 소스) |
| 계정 연결 | `POST /api/v1/iam/EndPointUserRoles` `{EndPointUser{Moid,ObjectType}, EndPointRole[{Moid,ObjectType}], Password, Enabled, EndPointUserPolicy{Moid,ObjectType}}` | verified (Ansible 소스) |
| 암호 변경 | 해당 사용자의 `EndPointUserRole` 을 새 `Password` 로 교체 (Ansible `always_update_password=true`: 기존 `EndPointUserRoles` 삭제 후 재생성) | verified (Ansible 소스 흐름); PATCH 로 `Password` 만 바꾸는 방식은 unverified |
| 배포 | 서버 프로파일(`server.Profile`)의 PolicyBucket 에 정책 연결 후 Deploy | unverified (Ansible 모듈은 정책만 만듦, 배포 API 는 이번에 미조사) |
| 인증 | Intersight API Key ID + 시크릿 키, HTTP Signature(요청 서명) | unverified (일반 지식; Ansible `api_key_id`/`api_private_key` 인자로 간접 확인) |

## 기본 계정 / 정책
- 엔드포인트에 **admin** 계정이 이미 존재. 정책에 `admin`(role admin)을 넣어야 암호를 바꾸거나 enable/disable 할 수 있음 (Ansible 문서). 삭제 불가.
- 사용자명 **16자 이하** (Intersight 제한, Ansible 문서). 역할: admin / readonly / user (Type IMC). IPMI 접근은 AccountTypes(`iam.AccountTypeIpmi`/`iam.AccountTypeLocal`)로 표현, `EndPointRole.Type=IPMI` 로는 거부됨.
- **Strong password (EnforceStrongPassword 기본 true)**: 8~20자, 사용자명 포함 불가, 대/소문자/숫자/특수문자(! @ # $ % ^ & * - _ + =) 4종 중 3종.
- **이전 암호 재사용 금지**: `PasswordHistory` 0~5, **기본 5** (0 이면 비활성).
- 만료: `EnablePasswordExpiry` 기본 false; netascode 모델은 expiry_duration 1~3650(기본 90), notification_period 1~15(기본 15), grace_period 0~5(기본 0), always_send_user_password 기본 false.
- 정책 이름 1~64자 `[A-Za-z0-9:_.-]`.
- X-Series IMM 에서 로컬 사용자 정책이 없으면 FI admin 암호가 서버 vKVM 에서 통하지 않는다는 커뮤니티 설명: "The only way ... to allow access to the vKVM ... direct access to the blade is to push the local user in a server profile" (Cisco Community, 직접 열람은 403, 검색 스니펫만 -> unverified).

## 한계
- UCSM 도메인을 Intersight 로 claim 만 한 경우(UCSM 관리)에는 이 정책을 쓰지 않음 - UCSM 로컬 사용자는 UCSM 에서 관리.
- Redfish: X210c M7 IMM 에 고객용 Redfish 계정 API 문서 없음 (BIOS 조사 결론과 동일: "Redfish 경로 unknown"). 이 문서의 방법은 Intersight REST 이다.
- 배포 후 BMC 에 실제 반영되는 시간/에러, 서버 프로파일 연결 API 필드는 확인 못함. Local User Policy 를 서버 프로파일에서 해제하면 사용자가 제거되는지도 unverified.

## 시도 내역
- 1차: WebSearch(Local User Policy/IMC Access Policy), Ansible 모듈 문서 페이지(429), netascode 데이터모델 페이지(속성 표 확보), 커뮤니티 스레드(403).
- 2차(공개 코드): `CiscoDevNet/intersight-ansible` raw 소스 두 개 직접 읽기 -> 엔드포인트/바디 확정.
- 실패/미해결: Cisco 공식 Intersight Help 의 Local User Policy 페이지(동적 사이트), 서버 프로파일 배포 API.

## 파일
- `create_account.md`, `change_password.md`
