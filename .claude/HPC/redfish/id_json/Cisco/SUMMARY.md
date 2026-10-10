# Cisco 계정 id/pw 등록·패스워드 변경 요약

문서 기반(실장비 접속 없음). Jev 생략(API 키 없음) -> 문서/공개 SDK 소스 2중 출처로 판정.
버전별 상세: `UCSM/`, `CIMC/`, `Intersight/` 의 `README.md`, `create_account.md`, `change_password.md`.

| 관리 모드 | 사용자 모델 | 프로토콜 | 계정 생성 | 암호 변경 | 이전 암호 재사용 금지 | 검증 |
|---|---|---|---|---|---|---|
| **UCSM** | B200 M4, B200 M5, B480 M5, X210c M7(UCSM 관리 시) | **xml** (`POST /nuova`), CLI, GUI. Redfish 없음 | `configConfMo` dn `sys/user-ext/user-<NAME>` + `aaaUser`(name/pwd/accountStatus) + 자식 `aaaUserRole` | 같은 DN `pwd` 재설정 / CLI `scope local-user <U>; set password` | `aaaPwdProfile.historyCount` 0~15 (기본 0), `clearPwdHistory` | verified (Cisco UCSM 6.0 CLI 가이드 + ucsmsdk 메타 소스) |
| **CIMC** (standalone C-Series) | 해당 모델 없음 | **json** Redfish AccountService (IMC 3.0+) + xml `aaaUser` + CLI | 4.2: `POST /redfish/v1/AccountService/Accounts` / 3.0: `POST .../Accounts/<n>` | `PATCH .../Accounts/<id> {"Password"}` | `Oem.Cisco.PasswordHistory` 0~5 | verified (Cisco REST API 가이드 3.0/4.2 + imcsdk 소스), 참고용 |
| **Intersight (IMM)** | X210c M7(IMM 관리 시) | **json** Intersight REST `/api/v1` (Redfish 아님) | Local User Policy `iam.EndPointUserPolicy` + `EndPointUser` + `EndPointUserRole` | `EndPointUserRole` 교체(`Password`) | `PasswordHistory` 0~5 (기본 5) | verified(정책/호출 흐름: Ansible 소스+netascode), 배포 단계 unverified |

## 사용자 모델 -> 모드 매핑 (UNSUPPORTED 포함)
| 모델 | 가능한 관리 모드 | 계정 방식 |
|---|---|---|
| UCS B200 M4 | UCSM 만 (블레이드, standalone/IMM 불가 추정 - M4 는 IMM 미지원은 이번에 문서 확인 못함) | xml |
| UCS B200 M5 / B480 M5 | UCSM 만 (IMM 지원 여부는 이번에 확인 못함) | xml |
| UCS X210c M7 | UCSM(4.3 이상, BIOS 가이드가 X210c M7 표 기재) 또는 IMM | UCSM 이면 xml, IMM 이면 Intersight REST |
- **Redfish AccountService 불가(UNSUPPORTED)**: B200 M4, B200 M5, B480 M5 는 어떤 모드에서도 Redfish 계정 API 문서가 없다. UCSM 관리 X210c M7 도 동일. standalone CIMC 모드로 쓰이는 사용자 모델은 없음.
- 모델별 모드 판별: 장비가 FI 에 연결되어 UCSM 에 보이면 UCSM, Intersight 에서 "UCS Domain(IMM)" 으로 보이면 IMM.

## 공통 기본 계정/정책
- 기본 계정명: UCSM **admin**(삭제·수정 불가, 기본 암호 없음 - 초기 setup 때 지정), CIMC **admin**(슬롯 1; 기본 암호 문서 미기재), Intersight BMC **admin**(엔드포인트에 기존재, 정책에 넣어야 암호 변경).
- UCSM 암호 강도: 8~127자, 4종 중 3종, 3연속 초과 동일문자·사용자명·사전단어 금지, `$ ? =` 금지. 로컬 사용자 최대 48.
- CIMC 암호 8~20자 (Redfish MinPasswordLength/MaxPasswordLength). Intersight 8~20자, 4종 중 3종, 사용자명 16자 이하.
- **이전 암호 재사용 금지**: UCSM historyCount(0~15, 기본 0=허용) / CIMC·Intersight 0~5.

## 한계 - 블레이드 개별 CIMC (KVM IP)
- UCSM 은 각 블레이드 CIMC 에 별도 로컬 계정을 만드는 API 가 없다. 블레이드 CIMC/KVM 은 UCSM ext-mgmt IP 풀의 주소로 접근하며 UCSM 사용자(`ls-ext-access` 권한 포함 역할)로 인증한다고 알려져 있으나 직접 로그인 계정 복제 여부는 **unverified**.
- IMM 은 Local User Policy 를 서버 프로파일로 배포해야 BMC/vKVM 계정이 생기며, 접근 IP 는 IMC Access Policy 로 지정.

## 미확인 / 실패
- UCSM 기본 역할 목록 원문, UCSM 본인 암호 변경의 XML 경로, XML 오류 코드표.
- Intersight 서버 프로파일 배포 API, PATCH 로 암호만 갱신 가능 여부, Intersight 공식 Help 페이지(동적 사이트).
- CIMC 4.3 계정 장, 최신 CLI 문법, 기본 암호 공식 표기.
- M4/M5 블레이드의 IMM 지원 여부, 블레이드 Redfish 존재 여부 (실장비 `GET /redfish/v1/` 로 확인 필요).
