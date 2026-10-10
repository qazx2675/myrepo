# Cisco: 지원하지 않는 버전·모델 조합

| 관리 버전 | 지원 안 하는 사용자 모델 / 기능 | 근거 | 상태 |
|---|---|---|---|
| UCSM (프로토콜 xml) — **Redfish 로 BIOS 조회·설정** | **B200 M4, B200 M5, B480 M5, X210c M7 전부** (UCSM 에는 Redfish BIOS 경로 없음. UCSM 인터페이스 = XML API) | 구형 Cisco 커뮤니티 글("UCS Manager ... does not have Redfish interface"); 공식 BIOS Redfish 문서 부재. 반례 주의: UCSM 모드 4.3+ 블레이드 CIMC 에 Redfish 가 기본 활성이라는 보안 권고(cisco-sa-cimc-redfish-cominj) → BIOS 용도 문서/URI 는 unavailable | unverified (재확인 필요: 4.3+ 블레이드에서 `GET /redfish/v1/Systems` 응답 여부) |
| UCSM ≥ 4.3 | **B200 M4** (M4 블레이드 미지원) | 4.3(6c)/4.3(2b)/6.0(2b) BIOS 토큰 가이드의 "supports the following servers" 에 M4 없음; 4.2 릴리스 노트는 B200 M4 지원(마지막 4.2(3s)) | verified(가이드 목록 2중: 4.3·6.0), 포럼 서술 부가 |
| UCSM < 4.3(2b) (3.x, 4.0, 4.1, 4.2) | **X210c M7** (4.3(2b) 에서 처음 지원, 4.3 가이드 "This platform is supported from 4.3(2b) onwards") | UCSM 4.3 BIOS 토큰 가이드, UCSM 4.3 릴리스 노트 | verified |
| UCSM 6.0 | B200 M4(없음); 6.0(2b) 목록에 B200 M5/B480 M5/X210c M7 포함 | 6.0 BIOS 토큰 가이드 | verified |
| UCSM < 3.2 | B200 M5 / B480 M5 (M5 는 3.2 부터) | 3.2 "Cisco UCS M5 Server BIOS Tokens" 가이드 존재(M5 첫 가이드) | unverified(본문 미취득) |
| CIMC (standalone) | **전 사용자 모델** (모두 블레이드이며 standalone CIMC 로 관리되지 않음. 블레이드는 UCSM 또는 IMM) | Cisco 제품 구조(블레이드 = FI/UCSM/Intersight 의존), 보안 권고 "does not affect Cisco UCS C-Series Rack Servers in standalone mode" 의 범주 구분 | verified(구조적) |
| CIMC Redfish `Bios/Settings` (Pending) | 해당 URI 없음 — `Bios` 에 직접 PATCH | REST API Programmer's Guide 4.1 | verified(4.1 단일 문서) |
| Intersight (IMM) — **Redfish** | X210c M7(및 B200 M5/B480 M5): BIOS 는 Intersight REST 정책으로만; 노드 Redfish BIOS 문서 없음 | SDK `/api/v1/bios/*`; IMM Redfish BIOS 문서 미확인 | unavailable |
| Intersight IMM 가이드 범위 | 서버 펌웨어 4.3(3a) 미만의 IMM BIOS 토큰 문서(가이드가 4.3(3a) 이상만 수록) | IMM BIOS Tokens 가이드 intro | verified(가이드 문구) |
| UCSM XML — BIOS ResetBios/ChangePassword 전용 액션 | 모든 UCSM 버전: 해당 액션 클래스 없음(biosVProfile verbs = Add/Get/Remove/Set) | ucsmsdk mometa | verified(SDK) |
