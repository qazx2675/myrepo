# Cisco UCSM (UCS Manager) BIOS — 프로토콜: **xml** (UCSM XML API), Redfish 아님

- 사용자 모델: **B200 M4**(UCSM 3.x~4.2), **B200 M5 / B480 M5**(UCSM 3.2~6.0), **X210c M7**(UCSM 4.3(2b)+ / 6.0, 또는 IMM → `../Intersight/`)
- 판정: 프로토콜 = `xml` verified (공식 XML API). UCSM 자체의 Redfish = **none** (아래 "Redfish 여부" 참고). Jev 판정은 키가 없어 **생략**, "문서 2중 출처"로만 verified 판정.
- 작성일 2026-10-10. 파일: `bios_attributes.json`(전체 속성), `gui_tokens_by_model.json`(모델별 GUI 토큰표), `old_fw_diff.md`(릴리스 라인별 차이), `source_text/`(근거 PDF 텍스트 발췌).

## 1. BIOS 속성 전체 (bios_attributes.json) — verified(SDK 단일 출처) / 이름·enum 은 이중검증 일부
- 출처: **ucsmsdk 0.9.27** (GitHub CiscoUcs/ucsmsdk master, commit `dc365e4a55f6`, 2026-08-10, HISTORY: "Support for UCSM release 6.0(2d)"). `ucsmsdk/mometa/bios/*.py` + `ucsmsdk/ucsmeta.py`(VersionMeta)를 **import 하지 않고 ast 로 파싱**(다운로드물 비신뢰 규칙).
  (과제 문구의 `ucsmeta/` 는 디렉터리가 아니라 `ucsmsdk/ucsmeta.py` 파일이며 mometa 클래스 정의가 속성/enum 의 실제 위치다.)
- 수록: `biosVf*` 토큰 클래스 **99개 / `vp*` 속성 149개**(속성명, type, allowed_values, 최초 도입 UCSM 릴리스 `introduced_ucsm`, access), 컨테이너 클래스(`biosVProfile`, `biosSettings`, `biosUnit`, `biosBOT`, `biosBootDev(Grp)`, `biosTokenFeatureGroup/Param/Settings`, `biosVIdentityParams` 등) 전 속성.
- **default**: ucsmsdk 메타에는 토큰별 default 가 없다. UCSM 의미상 enum 에 `platform-default` 가 있으면 그것이 기본(서버 플랫폼 기본값 = `biosSettings` under `computePlatform`/`capabilityCatalogue`). 그 외(숫자/자유문자) 는 default=null. 실제 값은 장비/Capability Catalog 에서만 확인 가능.
- **모델 적용 여부는 ucsmsdk 에 없음**. B200 M4/M5/B480 M5/X210c M7 중 어느 토큰이 유효한지는 라이브 UCSM 의 `computePlatform` 아래 `biosSettings` 를 조회해야 한다 (아래 §3 쿼리).
- **한계(중요)**: M5 후기·M6·M7 의 일부 새 토큰(예: Sub NUMA Clustering 의 SNC2/4, TDX, UPI Prefetch 계열 등)은 ucsmsdk 의 `biosVf*` 클래스에 대응이 없다. UCSM 3.2(1d)부터 `biosTokenFeatureGroup`/`biosTokenParam`/`biosTokenSettings`(속성 `targetTokenName`, `paramName`, `uiGroupName`, `targetTokenValue`)라는 범용 토큰 표현이 도입돼 있으므로 이들은 그 경로로 표현될 가능성이 높으나, **실제 XML 이름은 unavailable**(ucsmsdk·공개 문서에 없음, 실장비 `configResolveClass classId=biosTokenParam` 로만 확인). 근거: `ucsmeta.py` 의 3 클래스 introduced=3.2(1d).
- 이전 결과(back/Cisco, 91개, UI 이름만)와의 차이: 이제 `vp*` XML 속성명과 enum 이 확보됨. 단 GUI 이름 ↔ `vp*` 이름 대응은 자동 매칭 후보만 `gui_tokens_by_model.json` 의 `xml_attr_candidate`(unverified)로 제공.

## 2. 모델별 GUI 토큰표 (gui_tokens_by_model.json) — unverified (PDF 레이아웃 파싱)
- X210c M7: Cisco UCS Server BIOS Tokens **Release 4.3** (PDF, 2025-07-07) §4.3(2b) "BIOS Tokens for Cisco UCS X210c M7 Compute Node" **118행 전체**(웹 HTML 요약은 잘렸으나 PDF 는 완전) + 4.3(3a)/(3c)/(4a)/(5a)/(5c)·6.0(1b)/(2b) 변경분(26행, X210c M7 해당분만 수작업 확정).
- B200 M5/B480 M5: **Release 4.1** 가이드의 M5 표(74행, 14행 `needs_review` — PDF 열 넘침) + 4.1(2)/(3a)/(3e)/(3h) 변경분. 4.2/4.3/6.0 가이드는 M5 에 대해 4.1 가이드를 참조하라고 안내(원문: "For Cisco UCS C-series and B-series BIOS tokens supported on M4 and M5 servers, refer Cisco UCS Server BIOS Tokens, Release 4.1").
- B200 M4: **토큰표 unavailable** — 4.1 이후 가이드는 M4 를 다루지 않고(3.2 HTML 가이드는 M5 전용, M4 는 GUI/CLI Server Mgmt 가이드의 "BIOS Settings" 절), 공개 GitHub 에도 모델별 표 없음. M4 에 존재할 수 있는 토큰은 `bios_attributes.json` 중 introduced ≤ 3.1(2b) 클래스 (M4 시대, 2.2(7b)/3.1(1g) CPU 지원 기준) 로 **후보**일 뿐 모델 확정 아님.
- 가이드 오타/특이점 그대로 보존: X210c M7 표의 `Console Redirection` 기본값이 `VT100`(Terminal Type 행과 중복처럼 보임), `SProcessor Epoch 0` 표기.

## 3. URI / DN / 질의 (UCSM XML API)
엔드포인트: `POST https://<ucsm>/nuova` (XML 본문). 2중 검증: ucsmsdk(rn/parent 메타) + Cisco UCSM XML API Programmer's Guide(`aaaLogin`/`configResolveClass`/`configConfMo` 개념). 상태: DN 패턴 verified(ucsmsdk 메타 일치, 가이드 문서 직접 인용은 미수행 → **verified(SDK) / 가이드 unverified**).

| 대상 | 클래스 | DN / 패턴 | 비고 |
|---|---|---|---|
| 서버 BIOS 유닛 | `biosUnit` (rn=`bios`) | `sys/chassis-<N>/blade-<slot>/bios` | parents: computeBlade, computeRackUnit, computeServerUnit, computeExtBoard. Get 전용 |
| 서버 유효 BIOS 설정 | `biosSettings` (rn=`bios-settings`) | `sys/chassis-<N>/blade-<slot>/bios/bios-settings` | 자식 `biosVf*` 가 현재 토큰값. Get 전용 |
| 플랫폼 기본값 | `biosSettings` under `computePlatform`/`computeDefaults`/`capabilityCatalogue` | catalogue 하위 (플랫폼별) | 모델별 default/지원 토큰 확인용 |
| BIOS 정책 | `biosVProfile` (rn=`bios-prof-[name]`) | `org-root/bios-prof-<NAME>` (하위 org: `org-root/org-<O>/bios-prof-<NAME>`) | verbs Add/Get/Remove/Set. 속성: `name`, `descr`, `rebootOnUpdate`(yes/no/true/false), `policyOwner`, `policyLevel` |
| 정책 하위 토큰 | `biosVf*` | `org-root/bios-prof-<NAME>/<rn>` 예) `.../CPU-Performance` (`biosVfCPUPerformance`, 속성 `vpCPUPerformance`) | 값 enum 은 json 참고 |
| 서비스 프로파일의 BIOS 정책 연결 | `lsServer` 속성 `biosProfileName` (최대 16자 `[-.:_a-zA-Z0-9]`) | `org-root/ls-<SP>` | 실제 적용된 정책: `operBiosProfileName` |
| 부팅 순서(BOT) | `biosBOT`(rn `bdgep`), `biosBootDevGrp`(`bdg-[order]`), `biosBootDev` | `sys/.../bios/bdgep/...` | Get 전용 |

호출 예 (값은 플레이스홀더):
```
POST https://<ucsm>/nuova
<aaaLogin inName="<USER>" inPassword="<PASSWORD>"/>                      -> outCookie
<configResolveClass cookie="<COOKIE>" classId="biosVProfile" inHierarchical="true"/>
<configResolveDn cookie="<COOKIE>" dn="sys/chassis-1/blade-1/bios/bios-settings" inHierarchical="true"/>
<configResolveClass cookie="<COOKIE>" classId="biosSettings" inHierarchical="true"/>   (computePlatform 하위 = 플랫폼 기본값)
<configConfMo cookie="<COOKIE>" dn="org-root/bios-prof-<NAME>" inHierarchical="true"><inConfig>
  <biosVProfile name="<NAME>" rebootOnUpdate="no"><biosVfCPUPerformance vpCPUPerformance="hpc"/></biosVProfile>
</inConfig></configConfMo>
<aaaLogout inCookie="<COOKIE>"/>
```
- **액션**: BIOS 전용 ResetBios/ChangePassword 액션 클래스는 ucsmsdk bios 모듈에 없음(verbs 는 Add/Get/Remove/Set 뿐) → "기본값 복원"은 정책 토큰을 `platform-default` 로 두는 방식. BIOS 관리 비밀번호 변경 XML 액션: **unavailable**.
- 정책 변경 후 적용: `rebootOnUpdate=yes` 이거나 서비스 프로파일 재연결/서버 재부팅 필요(동작은 UCSM 문서 의존, 실측 아님 → unverified).

## 4. Redfish 여부 (정확한 표기)
| 대상 | 표기 | 근거 |
|---|---|---|
| UCSM 관리 인터페이스(FI 의 UCSM) | **none** (XML API 만, `xml`) | UCSM 의 공개 프로그래밍 인터페이스는 XML API(`/nuova`)이다. Cisco 커뮤니티 문서(구형): "UCS Manager ... does not have Redfish interface". 단일 구형 출처 → **unverified** |
| UCSM 모드 블레이드 자체 CIMC | Redfish 서버 **존재(기본 활성)** 하나 BIOS URI/속성 **unavailable**, 공식 BIOS 문서 없음 | Cisco 보안 권고 cisco-sa-cimc-redfish-cominj (2024-10-02): "Redfish is enabled by default in Cisco UCS B-Series, Cisco UCS Managed C-Series, and Cisco UCS X-Series Servers"; UCSM 모드 4.3 미만은 영향 없음(=해당 Redfish 비노출/비해당으로 해석, 4.3(4a) 수정). 사용자 BIOS 용도로는 `xml` 사용 권장 |
| B200 M4 | `xml` | UCSM 4.2(3s) 까지. M4 블레이드의 Redfish 근거 없음 → none/unknown |
| X210c M7 | UCSM 모드 `xml`; IMM 모드 `Intersight REST JSON` | `../Intersight/README.md` |

## 5. 사용자 모델 ↔ UCSM 릴리스 매핑 (정정 포함)
| 모델 | UCSM 릴리스 | 근거 |
|---|---|---|
| B200 M4 | 2.2(8a)~3.1~4.0~4.1~**4.2(3s)** 까지. **4.3 이상은 목록에서 빠짐** | UCSM 4.2 Release Notes(Supported Platforms에 B200 M4 포함, 마지막 4.2(3s) / Table 15 B200 M4 행); UCSM 4.2(3m) 번들에 `ucs-b200-m4-bios.B200M4.4.1.2e` (Release Bundle Contents 4.2); 4.3/6.0 BIOS 토큰 가이드의 "supports the following servers" 목록에 M4 없음. (포럼에 "4.3 에서 M4 지원 중단" 서술 있으나 비공식) |
| B200 M5 / B480 M5 | 3.2 ~ 4.3(6c) ~ 6.0(2b) 목록 모두 포함 | 4.1/4.2/4.3/6.0 BIOS 토큰 가이드 서버 목록 |
| X210c M7 | **4.3(2b)부터** ("This platform is supported from 4.3(2b) onwards"), 6.0 에도 포함. 4.3(2b)부터 "X-Series Servers and Cisco M7 Servers require a valid Cisco Intersight license and a connection to Intersight" (UCSM 4.3 Release Notes) | 4.3 BIOS 토큰 가이드, UCSM 4.3 릴리스 노트 |
→ back/ 의 "X210c M7 은 UCSM 4.2(2)+" 류 추정이 있었다면 **4.3(2b)+ 로 정정**.

## 6. 시도 내역 (재시도 규칙 적용)
1차(문서군): Cisco UCS Server BIOS Tokens 가이드 4.3/4.2/4.1/6.0 (PDF → pdftotext), UCSM 4.2/4.3 릴리스 노트, IMM BIOS Tokens 가이드, Cisco 보안 권고. cisco.com 직접 `curl` 은 403 → 우회하지 않고 WebFetch(정식 도구)로만 취득; 4.3 HTML 은 잘림 → 동일 문서 PDF 로 완전 취득.
2차(공개 코드): ucsmsdk(GitHub, git clone)의 mometa/ucsmeta 파싱 → 전 속성. 실패 항목: B200 M4 모델별 토큰표(3.x 가이드 없음), GUI↔XML 이름 공식 매핑표(공개 자료 없음, 후보만), M6/M7 신규 토큰의 XML 이름(ucsmsdk 미수록).
- GitHub API(api.github.com) 는 세션 권한 없음 → raw/git clone 으로 대체.
