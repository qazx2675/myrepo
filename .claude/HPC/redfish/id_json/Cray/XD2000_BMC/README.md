# HPE Cray XD2000 BMC (XD220v / XD225v / XD295v) - 계정 id/pw 관리

| 항목 | 내용 |
|---|---|
| 해당 사용자 모델 | **XD220V** (XD225v, XD295v 는 같은 BMC 계열) |
| 관리 컨트롤러 | HPE Cray XD BMC (iLO 아님, AMI MegaRAC 계열로 판단) |
| 프로토콜 | **json** (Redfish) + IPMI 2.0 + Web UI |
| 최신 문서 | XD220v Server User Guide `sd00002298en_us` (Edition 4, 2026-07), XD2000 System BMC Web UI User Guide `dp00002278en_us` (PDF 22MB, 읽기 불가) |
| BMC 펌웨어 최신 버전 | unknown (공개 릴리스 노트 미발견) |
| 지원불가 모델 | 없음 (Redfish 미지원 펌웨어 범위 확인된 바 없음) |
| 생성/변경 방법 | create_account.md, change_password.md 참조 |

## 방법 요약
| 작업 | 1순위 (Redfish) | 대체 |
|---|---|---|
| 새 id/pw 등록 | `POST /redfish/v1/AccountService/Accounts` (UserName, Password, RoleId) | `ipmitool user set name/password/priv/enable`, Web UI |
| 기존 id 비밀번호 변경 | `PATCH /redfish/v1/AccountService/Accounts/<ACCOUNT_ID>` {"Password"} + `If-Match`/`If-None-Match` 헤더 | `ipmitool user set password`, Web UI |

## URI
| URI | 메서드 | 상태 |
|---|---|---|
| /redfish/v1/AccountService | GET, PATCH | verified (AMI 공통) |
| /redfish/v1/AccountService/Accounts | GET, POST | verified (AMI 공통) |
| /redfish/v1/AccountService/Accounts/<id> | GET, PATCH, DELETE | verified (AMI 공통) |
| /redfish/v1/AccountService/Roles | GET (POST=사용자 정의 롤) | verified (AMI 공통) |
| /redfish/v1/SessionService/Sessions | POST (세션 로그인, X-Auth-Token) | verified (AMI 공통) |
| /redfish/v1/UpdateService, /redfish/v1/UpdateService/upload | XD220v 전용 코드(PFUT)에서 확인 | verified |

"AMI 공통" = AMI MegaRAC Redfish 레퍼런스(Lenovo SR635/655 BMC Redfish API Reference 7판, 2025-05)와 NVIDIA DGX H100, Cray CSM 문서에서 일치. XD220v 실기 응답으로는 미확인.

## 검증 상태 (이중검증)
| 항목 | 시도 | Jev | 독립 출처 | 판정 |
|---|---|---|---|---|
| Redfish 지원(json) | 1차 | yes 1.00 | HPE XD2000 제품 페이지/QuickSpecs "DMTF Redfish" (bios_json/back/HPE/XD220V README) | verified |
| BMC 가 iLO 아님 | 1차 | iLO 확인 no 0.98 | HPE XD220v 가이드에 iLO 표기 없음 | verified |
| BMC 가 AMI 계열 | 1차 | yes 0.97 | PFUT 코드(`Oem.AMIUpdateService`), JViewer, `.hpm` | verified (OEM 속성 기반 추론) |
| POST 로 계정 생성 | 1차 | yes 1.00 | Cray CSM `Manage_System_Passwords`(EX), `Configure_root_user_on_HPE_iLO_BMCs`(iLO) 가 같은 POST 방식. AMI 전용 문서는 레퍼런스 1건뿐 (DGX H100 페이지는 PATCH 예만 있음) | verified (AMI 레퍼런스 + 타 플랫폼 일치) |
| PATCH 로 비밀번호 변경 + 조건부 헤더 | 1차 | yes 0.99 | NVIDIA DGX H100 (`If-Match: *`), Cray CSM `Change_River_BMC_Credentials` (`If-None-Match`) | verified (AMI 계열) |
| IPMI 계정 생성 | 1차 | yes 0.97 | HPE XD675 BMC 가이드, Cray CSM Gigabyte(AMI) 문서 | verified (XD675/Gigabyte 문서, XD220v 전용 아님) |
| XD220v 전용 계정 URI/슬롯/비밀번호 정책 | 1차 + 재시도 2회 | XD220v 가이드에 계정 URI 있는가: no 1.00 | 없음 | **unverified** |
| Web UI 메뉴 경로 | 재시도 2회 | - | XD2000 BMC Web UI Guide(PDF) 읽기 불가 | **unavailable** |

## 재시도 기록 (최신 버전 항목 미확보 -> 범위 확대)
- 미확보 항목: XD2000 BMC 전용 계정 관리 문서(슬롯 ID, 비밀번호 정책, Web UI 메뉴).
- 재시도 1 (다른 문서군): XD220v/XD225v/XD665 서버 가이드(BMC overview, Redfish, System Inventory 페이지), XD675 BMC 가이드(Managing users, IPMI), XD2000 Chassis 가이드, HPE Community 스레드(Cloudflare 차단, 우회 안 함). 결과: 계정 URI 문서 없음, XD675 가이드에서 사용자 관리/IPMI 확보.
- 재시도 2 (공개 코드/샘플): HewlettPackard/CrayXD_PFUT(AMI OEM 확인), Cray-HPE/docs-csm(`Manage_System_Passwords`, `Change_River_BMC_Credentials`, `Add_Root_Service_Account_for_Gigabyte_Controllers`, `Configure_root_user_on_HPE_iLO_BMCs`), Cray-HPE/hms-scsd, AMI MegaRAC Redfish 레퍼런스(Lenovo 포장본), NVIDIA DGX H100. 결과: AMI 계열 일반 동작 확보, XD220v 전용 값은 미확보.
- 재시도 3 (다른 증거): HPE XD220v 가이드 페이지(JS 렌더링으로 본문 빈 응답), 웹 검색(XD220v BMC 사용자 관리·AMI SP-X AccountService 문서 미발견), CrayXD_PFUT `Bmc_Update.py`(AccountService/조건부 헤더 미사용, UpdateService 업로드만), docs-csm release/1.6 `Add_Root_Service_Account_for_Gigabyte_Controllers`(AMI: IPMI ID 4 = Redfish Accounts/4), `Change_Air-Cooled_Node_BMC_Credentials`(20자 경고). 결과: XD220v 전용 값 미확보, 항목 상태 변동 없음. 정정: 위 검증표/재시도 2 에서 인용한 `Change_River_BMC_Credentials`(If-None-Match)는 docs-csm release 1.0~1.6 에서 원문을 재확인하지 못했다(404). 조건부 헤더 verified 근거는 NVIDIA DGX H100 `If-Match: *` 로 한정한다.
- 결론: AMI 공통 방법을 `verified (AMI 계열)` 로 기록, XD220v 전용 값은 `unverified`. 추측으로 슬롯 번호를 만들지 않음 (반드시 GET 으로 확인).

## 한계
- 로그인 정보: HPE 문서상 초기 사용자명/비밀번호는 서버 라벨에 있다. 문서에 기본 계정명이 없다 (XD675 BMC 는 `admin`, Gigabyte AMI 는 `admin` 이지만 XD220v 는 확인 안 됨).
- AMI 레퍼런스의 "ID 1=Administrator, 2=HostAutoFW, 3=HostAutoOS, 4=IPMI admin, 신규 ID 는 5부터" 는 Lenovo 포장본의 OEM 값이다. DGX H100 은 Accounts/2 가 admin, Cray CSM 예시는 Accounts/1~4 이다. XD220v 는 반드시 `GET .../Accounts` 로 확인한다.
- 보안: AMI MegaRAC SP-X 의 Redfish 인증 우회(CVE-2024-54085, HPE 권고 HPESBCR04828 은 XD670 대상)는 패치 전 펌웨어에서 비인증 계정 생성이 가능했다. XD220v 영향 여부는 확인 안 됨. 의도하지 않은 계정이 생겼는지 `GET /redfish/v1/AccountService/Accounts` 로 점검한다.

## 출처
- HPE Cray XD220v Server User Guide: https://support.hpe.com/hpesc/public/docDisplay?docId=sd00002298en_us
- HPE Cray XD225v Server User Guide (같은 BMC 페이지): https://support.hpe.com/hpesc/public/docDisplay?docId=sd00002263en_us
- HPE Cray XD2000 System BMC Web UI User Guide (PDF, 링크만): https://support.hpe.com/hpesc/public/docDisplay?docId=dp00002278en_us&docLocale=en_US
- CrayXD_PFUT: https://github.com/HewlettPackard/CrayXD_PFUT
- AMI MegaRAC Redfish 레퍼런스(Lenovo 포장, PDF 링크만): https://pubs.lenovo.com/tsm/bmc_redfish_api_reference_sr635_sr655.pdf
- NVIDIA DGX H100 Redfish: https://docs.nvidia.com/dgx/dgxh100-user-guide/redfish-api-supp.html
- Cray CSM: https://github.com/Cray-HPE/docs-csm (operations/security_and_authentication, operations/bare_metal)
- Eclypsium AMI MegaRAC BMC&C Part 3: https://eclypsium.com/blog/ami-megarac-vulnerabilities-bmc-part-3/
