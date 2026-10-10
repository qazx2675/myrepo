# HPE Cray 계정 id/pw 등록·패스워드 변경 요약

사용자 모델은 **XD220V** 하나 (HPE Cray XD2000, 1U 2P Intel). BMC 는 iLO 가 아니라 AMI MegaRAC 계열 "HPE Cray XD BMC". 체계 판정은 `CONTROLLERS.md`, 상세는 `XD2000_BMC/` 의 `README.md`, `create_account.md`, `change_password.md`.

| 버전 | 프로토콜 | 사용자 모델 | 계정 생성 | 패스워드 변경 | 슬롯 | 검증 |
|---|---|---|---|---|---|---|
| XD2000 BMC (XD220v / XD225v / XD295v) | **json** (Redfish) + IPMI 2.0 + Web UI | **XD220V** | `POST /redfish/v1/AccountService/Accounts` (UserName, Password, RoleId, `PasswordChangeRequired:false`). 대체: `ipmitool user set name/password/priv/enable` | `PATCH .../Accounts/<ID>` `{"Password"}` + `If-Match`/`If-None-Match` (없으면 428 가능). 대체: `ipmitool user set password` | GET 으로 확인 (XD220v 값 unverified, AMI 기준 최대 14) | AMI 계열 verified (레퍼런스 + DGX H100 + Cray CSM), XD220v 실기 미확인 |
| XD670 BMC | json (AMI MegaRAC SP-X) | 없음 (인접) | 폴더 없음 (미조사) | - | - | CONTROLLERS.md 참조 |
| XD675 BMC | json + IPMI | 없음 (인접) | 폴더 없음 (미조사) | - | - | CONTROLLERS.md 참조 |
| EX / Shasta (cC/nC/sC) | json (Cray 자체) | 없음 | 폴더 없음 (미조사) | - | - | CONTROLLERS.md 참조 |

## 지원 불가 모델
- 없음. XD220V 가 Redfish AccountService 를 지원하지 않는 BMC 펌웨어 범위는 확인된 바 없다 (최저 BMC 펌웨어 버전 unknown). 낮은 펌웨어에서 미지원이면 그 시점에 XD220V 를 지원불가로 기재해야 하나 현재 증거 없음 -> "지원불가 없음 (unverified)".
- XML API 없음 (xml 아님). Redfish 외에는 IPMI/Web UI.

## 공통 규칙 (AMI 계열 기준)
- 권한: Administrator (ConfigureUsers). 롤: Administrator / Operator / ReadOnly / 사용자 정의.
- 길이: 비밀번호 8~20자 (IPMI 동기화), 사용자명 1~16자. 20자 초과 시 ipmitool 거부.
- 생성은 POST 에 조건부 헤더 불필요, PATCH/PUT 은 조건부 헤더 필요 가능 (428).
- 기본 계정: HPE 가이드는 서버 라벨의 사용자명/비밀번호라고만 기재. 기본 계정명 unavailable.
- 보안: AMI MegaRAC SP-X Redfish 인증 우회(CVE-2024-54085) 이력. 의도치 않은 계정을 `GET .../Accounts` 로 점검.

## 미확인 / 불일치
- XD220v 계정 슬롯, 비밀번호 정책 실제값, Web UI 메뉴 경로(PDF 읽기 불가), 기본 계정명, 최저 BMC 펌웨어 버전, 성공 코드(200/204): 모두 unverified/unavailable. 실장비에서 `GET /redfish/v1/AccountService`, `.../Accounts` 로 확인.
- README 기존 인용 "Cray CSM `Change_River_BMC_Credentials` 가 `If-None-Match` 사용"은 재조사에서 docs-csm release 1.0~1.6 경로로 원문을 찾지 못함 (재시도 3). NVIDIA DGX H100 의 `If-Match: *` 만 확인된 근거.

## 재시도 기록
- 1차/재시도 1·2: README 참조.
- 재시도 3 (다른 증거): HPE XD220v 가이드 URL 은 JS 렌더링이라 본문 빈 응답, 웹 검색에서 XD220v BMC 사용자 관리 문서 미발견, CrayXD_PFUT 소스는 AccountService/조건부 헤더를 쓰지 않음(UpdateService 업로드만) 확인, docs-csm release/1.6 의 `Add_Root_Service_Account_for_Gigabyte_Controllers`(IPMI ID 4 + Redfish Accounts/4, AMI) 및 `Change_Air-Cooled_Node_BMC_Credentials`(SAT `sat bmccreds`, 20자 경고) 확보. AMI SP-X 공식 AccountService 문서는 공개본 미발견. 결과: XD220v 전용 값 여전히 미확보.
