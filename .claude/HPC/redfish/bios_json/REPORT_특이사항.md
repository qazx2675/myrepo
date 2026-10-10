# Redfish BIOS / 계정 조사 특이사항 리포트 (벤더 × 관리망 버전 기준)

조사일 2026-10-10. 문서·공개 코드·공개 실장비 캡처 기반이며 사용자 장비에는 접속하지 않았다.
Jev API 키가 이 환경에 없어 Jev 판정은 전부 생략하고 "공식 문서 + 독립된 두 번째 출처"로만 verified 를 판정했다.
이전 모델별 결과(`back/`)는 수정하지 않았고, 이 리포트는 `back/REPORT_특이사항.md` 를 대체하는 새 기준 문서다.
세부 근거: `bios_json/<Vendor>/SUMMARY.md`, `UNSUPPORTED.md`, 버전 폴더 `README.md` / `id_json/<Vendor>/SUMMARY.md`.

## 1. 한눈에 보기 (BIOS)

| 벤더 | 관리망 | 프로토콜 | 사용자 모델 | 속성 수 | 상태 |
|---|---|---|---|---|---|
| HPE | iLO4 | json (Redfish 2.30+) | DL360 G9, XL170R G9, XL250 G9, XL270d G9 | 237 | URI verified / 속성 unverified |
| HPE | iLO4 | **none (BIOS)** | **DL560 G8** | - | UNSUPPORTED (문서 근거, 실장비 404 미확인) |
| HPE | iLO5 | json | DL360/DL560/DL580/XL270d G10, DL360 G10 Plus | 763 (합집합) | URI verified / 속성 unverified |
| HPE | iLO6 | json | DL360 G11, DL380 G11 (U54), DL560 G11 (U59) | 647 | URI verified / type·허용값 unavailable |
| HPE | iLO7 | json | 없음 | 721 | URI verified / type·허용값 unavailable |
| Cray | XD2000_BMC (AMI) | json | XD220V | **0** | URI verified / 속성 unavailable |
| Dell | iDRAC8 | json (BIOS ≥2.50.50.50), 그 미만 xml(WS-MAN) | 없음 | 249 | verified (R630) |
| Dell | iDRAC9 14G/15G/16G | json | R640,R740,R840,C6420,DSS8440 / R750,R750xa,R750xs / R760XA,XE9680,R660,R860,C6620,R6615 | 641 / 687 / 865(Intel) · 487(AMD) | R750·R760XA 실장비 verified, 나머지 대리 unverified |
| Dell | iDRAC10 | json | XE9780 | 839 (R770 대리) | 대리 unverified |
| Lenovo | XCC | json | SR630, SR650, SD530, SR630 V2 | 231 (이름 합집합) | partial |
| Lenovo | XCC2 | json | SR645 V3, SR675 V3, SD650 V3 | 199 | partial |
| Lenovo | XCC3 | json | 없음 | 34 | minimal |
| Cisco | UCSM | **xml** | B200 M4, B200 M5, B480 M5, X210c M7(UCSM 관리) | biosVf* 99 / vp* 149 | 속성명 SDK 단일출처, default unavailable |
| Cisco | CIMC | json(Redfish)+xml | 없음 (비교용) | vp* 495 | URI verified / Redfish 속성 unavailable |
| Cisco | Intersight | **Intersight REST** (Redfish 아님) | X210c M7(IMM 관리) | 473+18 | verified |
| Supermicro | X10 | **none (BIOS)** | 없음 | 0 | UNSUPPORTED |
| Supermicro | X11 | json (X11DP 만 문서화) | 없음 | 0 | unverified |
| Supermicro | X12_H12 / X14_H14 | json | 없음 | 0 | URI verified / 속성 unavailable |
| Supermicro | X13_H13 | json | AS-1115HS-TNR | UI 이름 158행 | URI verified / 속성 unverified(부분) |

## 2. 한눈에 보기 (계정 id/pw)

| 벤더 | 관리망 | 방식 | 비고 |
|---|---|---|---|
| HPE | iLO4/5/6/7 | Redfish `POST .../Accounts` / `PATCH .../Accounts/{id}` | verified. iLO4 `Oem.Hp`·권한 6개, iLO5+ `Oem.Hpe`·권한 10개. DL560 G8 의 Redfish 는 unverified |
| Cray | XD2000_BMC | Redfish POST/PATCH(If-Match 헤더), IPMI | AMI 공통 verified, XD220v 슬롯·정책·기본 계정명 unverified/unavailable |
| Dell | iDRAC8/9 | **빈 슬롯 PATCH** `Managers/iDRAC.Embedded.1/Accounts/{3..16}` | verified. iDRAC9 POST 지원은 unverified |
| Dell | iDRAC10 | `POST /redfish/v1/AccountService/Accounts` | verified |
| Lenovo | XCC/XCC2/XCC3 | XCC(Purley)=빈 슬롯 PATCH, Whitley 이후=POST | 기존 완료분, 이번 작업에서 미수정 |
| Cisco | UCSM | **xml** `aaaUser` (`/nuova`) | Redfish 계정 API 없음 |
| Cisco | CIMC | Redfish(IMC 3.0+)/xml | 3.0 과 4.2 의 POST 형식 상이 |
| Cisco | Intersight | Local User Policy REST | 서버 프로파일 배포 단계 unverified |
| Supermicro | X10~X14 | Redfish `POST .../Accounts` | 암호 변경 PATCH 는 공식 예시 없어 전 세대 unverified |

## 3. 수집 실패 / 미확보 항목

| 대상 | 내용 | 해결 방법 |
|---|---|---|
| Cray XD220V BIOS 속성 | 공개 레지스트리·덤프 전무 (0개). 다른 AMI 플랫폼 속성명은 체계가 달라 옮기지 않음 | 실장비 `GET /redfish/v1/Systems/Self/Bios` 와 Registries 덤프 |
| HPE iLO6 U54/U59 전체 레지스트리 | 지원센터 EULA 뒤. 공개 문서 540 + DL380a Gen11 실캡처로 합집합 | 아래 링크 zip 을 직접 받아 `BiosAttributeRegistryU54/U59` 파싱 |
| HPE iLO5 U32/U45, iLO7 전용 type·허용값 | 공개 캡처 없음 / 공개 문서가 iLO6 복사본 | 실장비 덤프 |
| Lenovo 전 버전 레지스트리 전체 | BMC 에서만 제공. type/허용값/default 확정은 문서 예시 4개뿐 | 실장비 `/redfish/v1/schemas/registries/BiosAttributeRegistry.1.0.0.json` |
| Supermicro 전 세대 레지스트리 | 공개 덤프 없음. X13_H13 은 매뉴얼의 UI 이름만 (attribute_id/type 은 null) | 실장비 `Registries/BiosAttributeRegistry` |
| Cisco B200 M4 토큰표 | 공개 자료 없음. X210c M7 신규 토큰의 XML 이름은 SDK 에 없음 | 실장비 `configResolveClass classId=biosTokenParam` |
| Dell 대리 모델 | R640,R840,C6420,DSS8440,R750xa,R750xs,XE9680,R660,R860,C6620,R6615(AMD 는 이름만),XE9780 의 개별 레지스트리 | 실장비 `Bios/BiosRegistry` |
| 계정 문서 일부 | HPE RIBCL/SSH CLI 원문(403), Cisco UCSM 일반 사용자 본인 암호 변경, Supermicro SUM 사용자 테이블 요소명 | 실장비/공식 가이드 직접 확인 |

HPE Gen11 Support Center 링크 (링크만, 다운로드 안 함):
- DL360/DL380/ML350 Gen11 (U54): https://support.hpe.com/km/software/MTX-e269fb8869a04cb2
- DL560 Gen11 (U59): https://support.hpe.com/km/software/MTX-14a1b1423ce3475a

## 4. 관리망 버전별 BIOS 설정값 차이

- **HPE**: iLO4→iLO5 에서 구조 변경(`HpBios.1.2.0`→`Bios.v1_0_0`, 속성이 `Attributes` 아래로, 열거값 숫자→접두어 `10`→`Timeout10`, 공통 201개 중 34개 허용값 변경, `ResetBios`/`ChangePassword` 신규). OEM URI: iLO5 Gen10 `Bios/baseconfigs`, Gen10 Plus·iLO6·iLO7 `Bios/Oem/Hpe/*`. 같은 iLO5 안에서도 ROM 별로 다름(U34 235개 vs U46 276개, 공통 154개). iLO5 3.19 / iLO6 1.79 / iLO7 1.25 Bios 정의 문서는 AMD 샘플 복사본이라 문서 비교로는 차이가 0.
- **Dell**: BIOS 속성은 iDRAC 펌웨어가 아니라 세대·BIOS 버전·CPU 벤더가 결정(같은 R750 에서 iDRAC 6.00→7.10 은 +5). 14G vs 13G +400/−8, 15G vs 14G +144/−98, 16G vs 15G +190/−12, iDRAC10 vs 16G +27/−53. **R6615(AMD)는 Intel 16G 파일 사용 금지**(AMD 전용 89, Intel 전용 38). iDRAC10 은 SCP 액션이 `EID_674_Manager.*` → `OemManager.*` 로 바뀌어 iDRAC9 용 호출이 실패하며 ApplyTime 이 OnReset 만 나온다.
- **Lenovo**: XCC→XCC2 는 REST 가이드·URI 동일, 차이는 플랫폼(Intel/AMD)과 속성. XCC3 는 레지스트리 `AttributeRegistry.v1_3_6`, `CXLMemoryModule_*`·`DevicesandIOPorts_Bifurcation_Slot<N>` 신규. Skylake `Enable/Disable` vs Cascade Lake 이후 `Enabled/Disabled`, Whitley 에서 `Power_PCIePowerBrake`·`Power_ASPM` 신규, UEFI 빌드별 철자 상이(`Memory_MirrorBelow4GB`/`Mirrorbelow4GB`). SD650 V3 는 Intel 이라 같은 XCC2 안에서도 SR645/SR675 V3(AMD)와 속성 집합이 다름.
- **Cisco**: 관리 모드가 곧 프로토콜 차이(UCSM xml / CIMC Redfish / Intersight REST). 같은 토큰이 `vpCPUPerformance`(UCSM) / `CpuPerformance`(Intersight) / `vp` 없는 이름(CIMC Redfish). 이름 일치: Intersight↔CIMC 464, UCSM↔Intersight 76, UCSM↔CIMC 77.
- **Supermicro**: 대기 설정 URI 가 `Bios/Settings` 가 아닌 `Bios/SD`. 문서끼리 PATCH 대상 충돌(`Bios` vs `Bios/SD`, 일부 장비 `Bios` PATCH 405) → `@Redfish.Settings.SettingsObject` → `/Bios/SD` → `/Bios` 순 폴백 권장. 속성 키 규칙(`QuietBoot_0027` / `QuietBoot` / `BootOption#1$3`) 문서마다 상이.
- **Cray**: 비교할 속성 데이터가 없어 diff 없음.

## 5. 지원 불가 모델 / 버전

| 대상 | 내용 |
|---|---|
| HPE DL560 G8 | iLO4 레퍼런스가 "Gen9" 한정, iLOrest 도 "Gen9 or greater" → BIOS Redfish/REST 미지원(문서 근거). 확인: `GET /redfish/v1/Systems/1/Bios` |
| Cisco B200 M4, B200 M5, B480 M5 | UCSM 관리 블레이드라 Redfish BIOS/계정 경로 문서 없음 → **xml 만** (Redfish 부재는 구형 글 1건 근거, 실장비 `GET /redfish/v1/Systems` 로 재확인) |
| Cisco X210c M7 | UCSM 4.3(2b)부터 지원(기존 "4.2(2)+" 추정은 오류). UCSM 관리=xml, IMM 관리=Intersight REST |
| Supermicro X10 / X11(X11DP 외) | Bios 리소스 없음 / 확인 불가 (사용자 보유 모델 없어 영향 없음) |
| Dell iDRAC8 | 사용자 보유 모델 없음. BIOS Redfish ≥2.50.50.50, 그 미만 WS-MAN/racadm |
| 그 외 | Lenovo, Dell(iDRAC9/10), HPE iLO5/6, Cray 중 Redfish BIOS 미지원 사용자 모델은 확인되지 않음 |

## 6. 사용자 모델 매핑 정정

- Lenovo SR630 V2 = **XCC(1세대, Whitley)**, XCC2 아님 (LP1391 근거, id_json 분류와 일치).
- Lenovo SD650 V3 는 AMD 가 아니라 Intel(Sapphire/Emerald Rapids).
- Cray XD220V 는 HPE iLO 가 아니라 **AMI Aptio / AMI MegaRAC 계열 BMC (ODM Inventec)**, 시스템 ID `Self`, 공개 최신 BIOS `CU2K_5.32_v3.80`. 폴더를 `HPE/XD220V` 에서 `Cray/XD2000_BMC` 로 이동 정정.
- Dell R6615 는 iDRAC9 이지만 AMD 라 별도 파일. 나머지 Dell 매핑(iDRAC9, XE9780=iDRAC10)은 맞음.
- HPE 매핑(iLO4/5/6)은 모두 맞음. DL560 G8 만 BIOS 미지원으로 재분류.

## 7. 이전 결과(back/) 대비 정정

- HPE iLO4 `Bios`·`Bios/Settings` URI: unverified → verified. iLO4 속성 235→237(직접 파싱).
- Dell 14G 965→641, 15G R650 대리→R750 실장비 687, 16G 1431(혼합)→Intel 865 / AMD 487. 잘렸던 `DcuIpPrefetcher`, `SetupPassword` 복구. 이전의 14G `SysPrepClean` 추가 이력은 14G BIOS 2.24.0 원본에 없어 오류로 확인. Dell 레지스트리 원본에는 DefaultValue 필드가 없음(default 전부 null).
- Cray 문서 인용 정정: `Change_River_BMC_Credentials` (If-None-Match 근거)는 docs-csm 1.0~1.6 에서 404 → 조건부 헤더 verified 근거는 NVIDIA DGX H100 의 `If-Match: *` 로 한정.
- Supermicro `Bios.ResetBios`/`ChangePassword` 액션 경로: 출처 1곳 → 문서 3종 확인.
- NVIDIA DGX A100, SSONIC 은 이번 범위(HPE/Cray/Dell/Lenovo/Cisco/Supermicro) 밖이라 갱신하지 않음(`back/` 결과 유지).

## 8. 한계 / 권장 후속 조치

1. 실장비에서 `Bios` 와 레지스트리를 덤프해 합집합·대리·프록시 데이터를 실측으로 교체 (특히 Cray XD220V, Lenovo, Supermicro, HPE iLO6, Dell 대리 모델).
2. 이전부터 이어진 기록(Cray CONTROLLERS.md/README 의 "Jev 확인" 문구)은 Jev 로 확인한 이전 세션 결과로 남아 있다. 이번 세션은 Jev 를 쓰지 않았다.
3. 모델-ROM 대응(HPE DL360 G9=P89, DL360 G10=U32 등)은 이전 조사 승계라 unverified (확인된 것은 DL360 G10 Plus=U46 뿐).
4. 최신 펌웨어 중 HPE iLO6 1.79, iLO7 1.25.00, Dell iDRAC9 7.30.30.54, iDRAC10 1.30.60.50, iDRAC8 2.86.86.86 은 단일 출처/포털 기준이라 unverified.
5. 로그인/EULA 뒤 다운로드(HPE 레지스트리 zip, Supermicro·Lenovo 펌웨어, Cisco 릴리스 노트)는 받지 않았다.
