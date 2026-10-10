# Cisco BIOS 요약 (관리망 버전별)

작성 2026-10-10. Jev 판정 생략(키 없음), "문서 2중 출처"로 verified 판정. 상태: verified / unverified / unavailable.

| 버전(관리 모드) | 프로토콜 | 해당 사용자 모델 | BIOS 속성 수 | 상태 |
|---|---|---|---|---|
| **UCSM** (3.x/4.0/4.1/4.2/4.3/6.0) | **xml** (`POST https://<ucsm>/nuova`). Redfish **none**(UCSM 인터페이스), 블레이드 CIMC 의 Redfish 는 존재하나 BIOS 문서 없음(4.3+ 기본 활성) | B200 M4(≤4.2(3s)), B200 M5, B480 M5, X210c M7(4.3(2b)+) | XML `biosVf*` 99 클래스 / `vp*` **149** (ucsmsdk 0.9.27, UCSM 6.0(2d) 메타). GUI 가이드 표: X210c M7 118행+델타 26, M5 74행 | 속성명·enum **verified(SDK 단일)**; 모델별 적용·default·M6/M7 신규 토큰 XML 명 **unavailable**; GUI표 **unverified** |
| **CIMC** (standalone C-Series IMC 3.0+) | **Redfish(json)** `/redfish/v1/Systems/<Id>/Bios` + **xml** | 없음(비교용) | `biosVf*` 243 클래스 / `vp*` **495** (imcsdk 0.9.18, IMC 4.3(5.240045) 메타). Redfish 속성 전체 목록 unavailable(실장비 레지스트리 필요) | URI **verified**(가이드 4.1 단일 문서 + DevNet), 속성명 **verified(SDK)** |
| **Intersight (IMM)** | **Intersight REST JSON** `/api/v1/bios/Policies` — **Redfish 아님** | X210c M7 (IMM 관리 시); B200 M5/B480 M5 도 IMM 지원(unverified) | `bios.Policy` 토큰 **473** (+공통 18) (intersight-python OpenAPI 1.0.11-2026072720, terraform provider 와 473/473 일치) | **verified**(SDK+Terraform) |

## 핵심 정리
- Redfish(json) 로 BIOS 를 다루는 Cisco 경로는 **standalone C-Series CIMC 뿐**(문서화됨). 사용자 4개 모델 전부 블레이드(B200 M4, B200 M5, B480 M5, X210c M7) → **xml(UCSM)** 또는 **Intersight REST(IMM)**.
- 버전 간 큰 차이: UCSM xml ↔ Intersight JSON 은 프로토콜 자체가 다름. 토큰명도 `vpCPUPerformance`(UCSM) ↔ `CpuPerformance`(Intersight) ↔ `vpCPUPerformance`(CIMC XML) / Redfish `SelectMemoryRAS`형(접두 vp 없음, CIMC) 으로 상이. 상세는 `Intersight/diff_vs_UCSM.md`, `CIMC/diff_vs_UCSM.md`, `Intersight/diff_vs_CIMC.md`, `UCSM/old_fw_diff.md`.
- 사용자 모델 매핑 정정: **X210c M7 은 UCSM 4.3(2b)+ (이전 "4.2(2)+" 추정은 오류)**, 그리고 4.3(2b)+ 에서 Intersight 라이선스/연결 필요. **B200 M4 는 UCSM 4.2(3s)가 마지막**(4.3+ 목록에서 제외). B200 M5/B480 M5 는 UCSM 3.2~6.0 및 IMM.
- 이전(back/Cisco) 대비 개선: XML `vp*` 속성명·enum·도입 릴리스 확보(91 → 149 + 컨테이너 클래스), X210c M7 표 잘림 해소(PDF 로 118행 전체), M4 는 모델별 토큰표 없음을 증거와 함께 명시, Intersight/CIMC 신규 수록.
- 남은 공백: ① B200 M4 모델별 토큰표(공개 자료 없음) ② UCSM M6/M7 신규 토큰의 XML 이름(`biosTokenParam` 경로 추정, 실장비 `configResolveClass classId=biosTokenParam` 필요) ③ CIMC Redfish 속성 전체(실장비 `/redfish/v1/Registries/CiscoBiosAttributeRegistry.v1_0_0/BiosAttributeRegistry.json`) ④ IMM 모델별 토큰 적용표.

폴더: `UCSM/` `CIMC/` `Intersight/` (각 README.md, bios_attributes.json, diff 문서). 보조: `UCSM/gui_tokens_by_model.json`, `UCSM/source_text/`.
