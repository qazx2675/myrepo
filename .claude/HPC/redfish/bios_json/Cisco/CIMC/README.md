# Cisco CIMC (standalone C-Series IMC) BIOS — 프로토콜: **Redfish(json)** (IMC 3.0+) + **xml** (IMC XML API)

- 사용자 보유 모델 **없음**(B200 M4/M5, B480 M5, X210c M7 은 모두 블레이드 → UCSM 또는 IMM). 이 폴더는 **비교용**.
- 작성일 2026-10-10. Jev 판정은 키 부재로 **생략**, "문서 2중 출처"로만 verified 판정.

## 1. 프로토콜 (정확한 표기)
| 항목 | 표기 | 근거(2중) | 상태 |
|---|---|---|---|
| Redfish | **Redfish(json)**, IMC 3.0 부터 (Redfish 1.0.1 준수 + OEM 확장) | ① Cisco DevNet "UCS IMC" 페이지(검색 스니펫: IMC 3.0 = Redfish 1.0.1) ② Cisco UCS C-Series REST API Programmer's Guide 4.1 (Bios 리소스 `#Bios.v1_0_4.Bios`) | verified(2 출처) |
| XML API | **xml** (`/nuova`, imcsdk) | imcsdk 소스 | verified |
| 4.2/4.3 가이드 | REST API Programmer's Guide 4.2/4.3 존재(4.2 예제 페이지 403 으로 본문 미취득) | 검색 결과 목록 | 4.1 본문만 취득 |

## 2. BIOS 속성 전체 (bios_attributes.json)
- 출처: **imcsdk 0.9.18** (GitHub CiscoUcs/imcsdk master, commit `b7a27c28d806`, 2025-08-28). `imcsdk/mometa/bios/*.py` + `imcsdk/imcmeta.py` 를 ast 로 파싱(import 안 함). IMC 메타 최신 4.3(5.240045).
- 수록: `biosVf*` 토큰 클래스 **243개 / 고유 `vp*` 속성 495개**. 플랫폼 변형 `classic`(일반 C-Series) / `modular`(M-시리즈 모듈형) 별 `allowed_values`, 도입 릴리스, access. 컨테이너 클래스(biosSettings, biosPlatformDefaults, biosProfile, biosProfileManagement, biosProfileToken, biosPassword, biosBootMode, biosBootDev, biosBootDevPrecision, biosUnit, biosBOT, biosBootDevGrp) 포함.
- default: 메타에 없음. enum 에 `platform-default` 가 있으면 default 로 표기(IMC 의미상 플랫폼 기본값), 나머지 null. 모델(C220 M5 등)별 적용 여부는 SDK 에 없음.
- **Redfish 속성명 매핑**: Cisco 문서 예제의 Redfish 속성은 `SelectMemoryRAS`, `IntelVT`, `IMCInterleave` 처럼 XML `vp` 접두 없는 이름으로 보임(문서 예시 3개 한정, 전체 대응은 unverified). 정확한 전체 목록은 실장비 `GET /redfish/v1/Registries/CiscoBiosAttributeRegistry.v1_0_0/BiosAttributeRegistry.json` 으로만 얻을 수 있음 → Redfish 속성 전체는 **unavailable(실장비 필요)**.

## 3. URI (Redfish) — REST API Programmer's Guide 4.1 기준
| 용도 | 메서드 | URI | 상태 |
|---|---|---|---|
| 시스템 | GET | `/redfish/v1/Systems` → `/redfish/v1/Systems/<SerialNumber>` (Id 가 서버 시리얼, 예 `WZP21330G5B`) | verified(가이드 예제) |
| BIOS 현재 토큰 | GET | `/redfish/v1/Systems/<Id>/Bios` (`@odata.type: #Bios.v1_0_4.Bios`, `Id: BiosToken`, `AttributeRegistry: CiscoBiosAttributeRegistry.v1_0_0`, `Attributes{}`) | verified |
| BIOS 설정 변경 | PATCH | `/redfish/v1/Systems/<Id>/Bios` 본문 `{"SelectMemoryRAS":"Mirror Mode 1LM"}` (**`Bios/Settings` 없음**, Bios 에 직접 PATCH) | verified(가이드 예제) |
| 기본값으로 | PATCH | 같은 URI, 본문 `{"SelectMemoryRAS":"default"}` | verified(가이드 예제) |
| 속성 레지스트리(기본값 포함) | GET | `/redfish/v1/Registries/CiscoBiosAttributeRegistry.v1_0_0/BiosAttributeRegistry.json` | verified(가이드; 예제 레지스트리 펌웨어 `4.1(1fS4)` / UCS C220 M5L) |
| ResetBios 액션 | (POST 추정) | target `/redfish/v1/Systems/<Id>/Bios/Actions/Bios.ResetBios` | target 만 가이드에 노출, 메서드/본문 예제 없음 → unverified |
| ChangePassword 액션 | - | 가이드에서 확인 불가 | unavailable |
세션: `curl -k -u <USER>:<PASSWORD> https://<imc>/redfish/v1/...`(가이드 예제 형식; 실사용은 `-k` 대신 CA 지정 권장).
XML API: `POST https://<imc>/nuova` — imcsdk 메타: `biosUnit`(rn=`bios`, parent `computeRackUnit` rn=`rack-unit-1`; modular 는 `computeServerNode`) → 현재 설정 `sys/rack-unit-1/bios/bios-settings`(`biosSettings`, rn=`bios-settings`), 플랫폼 기본값 `sys/rack-unit-1/bios/bios-defaults`(`biosPlatformDefaults`, rn=`bios-defaults`, Get 전용). `rack-unit-1` 은 imcsdk 관례(단일 서버)이며 DN 전체는 실장비 미확인 → unverified.

## 4. 버전 간 차이 / 비교
- `diff_vs_UCSM.md`: UCSM(블레이드) vs CIMC 토큰 이름·enum 비교. IMC 는 M6/M7/M8 신규 토큰이 `biosVf*` 클래스로 풍부(495 속성), UCSM 은 149.

## 5. 시도 내역
1차(문서군): Cisco IMC REST API Programmer's Guide 4.1 HTML(성공), 4.2 HTML(403), Cisco 커뮤니티 Redfish 글(403), DevNet 스니펫. 2차(공개 코드): imcsdk GitHub clone 후 파싱(성공). 실패: 레지스트리 전체 JSON(실장비 필요), Redfish 속성 ↔ XML 이름 전체 대응.
