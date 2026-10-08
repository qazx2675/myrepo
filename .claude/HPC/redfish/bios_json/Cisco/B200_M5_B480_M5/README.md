# B200 M5 / B480 M5 (UCSM 관리 블레이드)

## 프로토콜: **xml** (UCSM XML API, 필요시 UCSM GUI/CLI). Redfish 미지원/미문서화
- 근거: UCSM 4.3 릴리스 노트 "GUI, CLI, or XML API" (https://www.cisco.com/c/en/us/td/docs/unified_computing/ucs/release/notes/b_release-notes-ucsm-4_3.html) ; Cisco 커뮤니티 BIOS 프로파일 가이드 "token 값은 GUI, CLI, XML API 로 설정" (https://community.cisco.com/t5/unified-computing-system-knowledge-base/cisco-ucs-energy-efficient-bios-profile-policy/ta-p/3656084)
- Redfish 는 standalone C-Series IMC 3.0+ 문서에만 있음 (https://blogs.cisco.com/datacenter/cisco-supports-redfish-standard-api-enhances-ucs-programmability). UCSM 블레이드용 Redfish 문서는 찾지 못함 -> "미문서화"(존재 불가 증명은 아님; 보안공지 CVE-2024-20365 에 B-Series 가 Redfish 영향 제품으로 언급되었다는 2차 요약 있음, 고객용 BIOS 경로는 없음). 판정 근거 jev(choice) yes=1.00 (1차), 독립 출처 2건 일치 -> **verified (프로토콜)**

## 그룹 근거
Cisco "UCS Server BIOS Tokens" 4.0/4.1 가이드가 B200 M5 와 B480 M5 를 같은 토큰 표(동일 이름·허용값·기본값)로 함께 기재 -> 하나의 그룹.

## 최신 펌웨어
UCSM 4.1 가이드(4.1(3h)까지) 기준 목록. 4.2 가이드는 블레이드 M5 변경 없음(M5 는 4.1 가이드 참조 명시). 4.3/6.0 가이드 platform 목록에 B200 M5, B480 M5 가 있으나 M5 행 표는 확인하지 못함 -> 4.1(3h) 이후 변경 unknown. (WebFetch 요약 한계로 4.3 표 일부 미열람)

## XML API 객체 (UCSM)
| 대상 | 클래스 / DN | 검증 |
|---|---|---|
| BIOS 정책 | `biosVProfile`, `org-root/bios-prof-<name>` | unverified: 클래스명은 UCSM API Guide 권한표(`bios:VProfile`, `bios:VfQuietBoot`)와 ucsmsdk 2개 출처 일치, DN 패턴은 ucsmsdk 단일 출처 + jev 0.88 -> 독립 문서 없음. 1차 + 재조사 1회 |
| 서버별 BIOS 설정 | `biosSettings`, `sys/chassis-<id>/blade-<slot>/bios/bios-settings` | unverified (위와 동일, jev 0.94, 독립 DN 문서 미확보). 1차 + 재조사 1회 |
| 토큰 하위객체 | 예 `biosVfCPUPerformance`, rn `CPU-Performance`, 속성 `vpCPUPerformance` (custom/enterprise/high-throughput/hpc/platform-default/platform-recommended) | unverified, jev 0.72~0.86 |
조회: `configResolveClass classId="biosVProfile"`, `configResolveDn dn="sys/chassis-1/blade-1/bios/bios-settings"` (일반 XML API 메서드, 실기 미검증). 클래스 전체 목록: ../ucsm_bios_xml_classes.txt

## bios_tokens.json 안내 (bios_attributes.json 대체)
- 키 = Cisco 가이드의 토큰 표시 이름(UI 이름). XML 속성명(vpXxx)과의 1:1 매핑은 문서로 확인하지 못해 **미기재(unknown)**.
- 91개 토큰, 허용값/기본값은 4.0/4.1 가이드 기준. 일부 이름은 릴리스별 표기가 달라 한 이름으로 통일(P STATE Coordination = EIST PSD Function 등).
- 4.1 가이드 표는 WebFetch 요약(요약 모델 경유)으로 수집 -> 전체 목록이지만 원문 대조는 못 함. 4.1(3h) 이후 신규 토큰은 unknown.

## 구버전 차이 (old_fw_diff.md)
있음 (아래 파일). 3.2(x) / 4.0 / 4.1 사이 변경은 `old_fw_diff.md` 참조.
