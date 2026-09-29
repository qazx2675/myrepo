# CHANGELOG

`vcenter-portal` 의 변경 사항을 날짜순(최신이 위)으로 기록합니다.

---

## 2026-09-29 — 런처 포털 모드 (Edge 확인 창 제거)

### 추가
- `vcportal.exe` 를 인자 없이 실행하면 **포털 모드**: 전용 Edge 에 포털을 열고, `file://` 탭에 `window.vcpOpen`
  바인딩을 연결해 "vCenter에서 열기"를 `vcportal://` 확인 창 없이 처리. 중복 실행 시 새 포털 탭만 연다.
- `install-launcher.ps1`: 바탕화면 `vCenter 포털` 바로가기 생성(`-Uninstall` 시 삭제).
- 웹 UI: `window.vcpOpen` 이 있으면 `vcportal:` 링크 클릭을 가로채 런처로 직접 전달(없으면 기존 프로토콜 링크).

### 변경
- 런처를 `-H windowsgui` 로 빌드(`build.ps1`/`build.sh`) — 이전 빌드는 콘솔 창이 함께 떴음.

### 검증
- 이 PC(Edge)에서 포털 모드 → 링크 클릭 시 확인 창 없이 `요청(포털)` → vSphere 탭 열림, 포털 새로고침 후에도 바인딩 유지,
  두 번째 실행 시 새 탭만 열고 종료, `install-launcher.ps1` 바로가기 생성 확인.

---

## 2026-09-29 — 최초 릴리스

### 추가
- 수집기 `vcportal-collector.exe`: 전체 vCenter 병렬 수집, `work_dir` 에서 완성 후 `output_dir\data` 로 원자적 교체,
  실패 vCenter 는 직전 성공 데이터 유지, `--check`, 종료 코드 0/1/2/3.
- 통합 웹 UI(`index.html` + `assets\`): vSphere Client 8 디자인, 통합 트리, 통합 검색(호스트/VM/IP),
  vCenter/Datacenter/Cluster/Host/VM Summary, 수집 상태 표시줄/경고 배너, 지연 로딩.
- 로그인 런처 `vcportal.exe`: `vcportal://` 링크 처리, 전용 프로필 Edge/Chrome(127.0.0.1:9333), SSO 자동 로그인, 딥링크 이동.
- 공용 conf `vcportal.conf`(탐색기 경로 붙여넣기 허용, UTF-8/CP949, 네트워크 드라이브 차단).
- 래퍼/설치 스크립트: `run-collector.ps1`(`-Check`, `-RegisterTask` 12:00, 중복 실행 방지 종료 코드 4),
  `run-collector.sh`, `install-launcher.ps1`(HKCU 프로토콜 등록/해제).
- `build.ps1`(Windows PowerShell, 폐쇄망: GOPROXY=off·GOTOOLCHAIN=local·vendor) / `build.sh`(Linux): 빌드 후 공유폴더에 그대로 복사하는 `dist/vc-portal/` 조립(.ps1 UTF-8 BOM + CRLF), 런처는 `-H windowsgui`.
- 문서: README, ARCHITECTURE, PR_CHECKLIST, 사용법.txt, docs/DATA_SCHEMA.md, 계획서. CI: `.github/workflows/vcenter-portal.yml`.

### 검증
- Windows `build.ps1`: Go 1.26.5, 빈 GOMODCACHE + 네트워크 차단 상태에서 빌드 성공(폐쇄망 확인), 런처 PE 서브시스템 GUI 확인.
- 랩 vCenter(192.168.0.50, 8.0.3)로 수집(`--check` 및 실제 수집, 종료 코드 0), 배포 폴더 구조에서
  `run-collector.ps1` 의 기본 conf(`..\config\vcportal.conf`) 탐색, 중복 실행 시 종료 코드 4,
  `index.html` 을 `file://` 로 열어 트리/VM Summary 렌더링, 런처 자동 로그인(Step 5).

### 알려진 제한
- Edge 의 `vcportal://` 확인 창은 매번 [열기] 필요.
- Cluster 관련 수집 필드는 클러스터가 없는 랩에서 미검증.
- conf 에서 제거한 vCenter 의 `data\<id>.js` 는 자동 삭제되지 않음.
- `run-collector.ps1 -RegisterTask` 는 실제 스케줄러 등록까지는 이 릴리스 검증에서 실행하지 않음(문법 검사와 코드 검토만).
