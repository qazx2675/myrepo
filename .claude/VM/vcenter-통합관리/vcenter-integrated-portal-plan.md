# vCenter 통합 관리 포털 프로젝트 계획서 (확정)

> 확정일: 2026-09-29
> 확정 사항: 로그인 방식 A(로그인 런처, **공유폴더의 exe 직접 실행**) · conf 는 탐색기 경로 복사/붙여넣기 · 평문 비밀번호 허용 · SSO 도메인 분리 · 공용 계정 1개 · vCenter 전부 8.0.3 · 수집기는 Windows 서버에서 매일 12:00 · 화면은 vSphere Client 8 디자인 유지(`design-reference.png`)

## 1. 목표 및 범위

### 해결할 문제
- vCenter 약 15대(계속 증설 예정)에 VM 약 13,000대(계속 증가)가 흩어져 있어, 특정 호스트나 VM이 어디 있는지 찾기 어렵다.
- 찾더라도 해당 vCenter에 따로 접속하고 로그인해야 한다.

### 만들려는 것
- 웹 서버/DB 없이 **사내 공유폴더의 정적 파일만으로 동작하는 통합 조회 포털**.
- vSphere Client 디자인을 유지하고, 15개 vCenter의 모든 호스트/VM을 **하나의 vCenter에 있는 것처럼** 보여준다.
- 객체마다 **원래 vCenter 화면을 로그인된 상태로 여는 링크**를 둔다.

### 포함
- 수집기 (Windows 서버, 매일 12:00)
- 통합 트리 / 통합 검색 / Summary 화면
- vCenter 딥링크 + 로그인 런처
- 설정 파일 신규 작성

### 제외
- VM/호스트 제어 (조회 전용)
- 실시간 성능 데이터 (12:00 기준 스냅샷)
- 포털 자체 로그인 (공유폴더 권한으로 대체)

## 2. 주요 기능

| 우선순위 | 기능 | 설명 |
|---|---|---|
| 필수 | 수집기 | 전체 vCenter 병렬 조회 → vCenter별 데이터 파일 생성 |
| 필수 | 통합 인벤토리 트리 | vCenter > Datacenter > (Folder) > Cluster > Host > VM |
| 필수 | 통합 검색 | 호스트명 / VM명 / IP (`vm_host` alias 대체) |
| 필수 | Host Summary | 모델, ESXi 버전/빌드, 상태, CPU·메모리 사용량, 가동 시간, VM 목록, 데이터스토어, 네트워크 |
| 필수 | vCenter에서 열기 | MoRef ID로 8.0.3 vSphere Client URL 생성 → 런처를 통해 로그인 상태로 열기 |
| 필수 | 로그인 런처 | `vcportal://` 링크 처리, conf 공용 계정으로 자동 로그인, 이미 로그인된 vCenter는 로그인 생략 |
| 필수 | 수집 상태 표시 | 마지막 수집 시각, vCenter별 성공/실패 |
| 선택 | VM / Cluster Summary, 대시보드, CSV 내보내기 | |

## 3. 설정 파일 (`vcportal.conf`, 1개)

공용 계정 1개 + 공유폴더 자체가 권한 통제되어 있으므로 **수집기와 런처가 같은 conf를 공유**한다.
위치: `\\fileserver\share\vc-portal\config\vcportal.conf`

```ini
[paths]
output_dir = \\fileserver\share\vc-portal
work_dir   = D:\vcportal\work
log_dir    = D:\vcportal\logs

[collect]
parallel = 5
timeout  = 300

[account]
user     = lscsystems@vsphere.local
password = P@ssw0rd!

[browser]
type = edge

[vcenters]
vc01 = https://vc01.corp.local
vc02 = https://vc02.corp.local
```

- INI 형식: 탐색기 경로를 그대로 붙여넣어도 동작(역슬래시 이스케이프 불필요).
- 경로 값 정리: 앞뒤 공백·따옴표 제거, 끝 `\` 무시, 공백·한글 폴더명 허용.
- 인코딩: UTF-8(BOM 포함) / ANSI(CP949) 모두 읽음.
- 드라이브 문자(`Z:\`) 경로는 네트워크 드라이브일 수 있으므로 시작 시 차단하고 UNC 경로 사용을 안내.
- `--check`: conf 해석 결과와 경로/vCenter 접속 여부만 점검.
- 수집기: exe 옆 `vcportal.conf` 기본, `--conf <경로>`로 지정. 런처: exe 위치 기준 `..\config\vcportal.conf`.

## 4. 로그인 런처 동작

```
[vCenter에서 열기] 클릭
 → vcportal://open?url=<딥링크>
 → \\share\vc-portal\launcher\vcportal.exe 직접 실행 (HKCU 프로토콜 등록)
 → ..\config\vcportal.conf 읽기, 딥링크 호스트가 conf [vcenters]에 있는지 확인
 → 전용 브라우저 프로필로 Edge 실행 (이미 떠 있으면 새 탭)
 → 세션 있으면 바로 이동 / 없으면 SSO 로그인 폼 자동 입력 → 대상 화면
```

- SSO 도메인이 분리되어 있어 vCenter마다 1회 로그인, 이후 세션 만료 전까지 재로그인 없음.
- 딥링크(8.0.3 고정): `https://{vc}/ui/app/{vm|host|cluster|datacenter};nav=h/urn:vmomi:{Type}:{moref}:{instanceUuid}/summary`
- 자동 로그인 실패 시 로그인 화면에서 멈춤(수동 로그인 가능).

## 5. 데이터 스키마 (공유폴더 `data\`)

`file://`/UNC 에서는 `fetch()`가 막히므로 모든 데이터는 `<script>`로 읽는 `.js` 파일.

- `manifest.js` — `window.VCP_MANIFEST = { generated, vcenters:[{id,url,instanceUuid,version,status:"ok"|"fail",error,collectedAt,counts:{hosts,vms,clusters}}] }`
- `index.js` — 검색용 경량 인덱스 `window.VCP_INDEX = [[vcId, type("H"|"V"|"C"), moref, name, "ip1 ip2", hostName, guestHostName], ...]`
- `vcNN.js` — vCenter별 상세 `window.VCP_DATA["vc01"] = { id, url, instanceUuid, rootChildren:[moref...], objects:{ moref:{type,name,parent,children:[...],...상세} }, datastores:{...}, networks:{...} }`
  - 트리는 Hosts and Clusters 뷰 기준: VM의 부모는 `runtime.host`, 단독 호스트의 ComputeResource는 생략하고 호스트를 상위에 직접 붙인다.

## 6. 단계별 마일스톤

1. **프로젝트 골격 + conf 패키지** — go.mod/vendor(govmomi, chromedp), `internal/conf`(경로 정리·인코딩·드라이브 문자 차단·테스트), 빌드 스크립트.
2. **수집기** — govmomi PropertyCollector로 전 vCenter 병렬 수집, 원자적 파일 교체, 실패 vCenter는 직전 데이터 유지, `--check`, 래퍼(`run-collector.ps1` + `run-collector.sh`). 랩 vCenter(192.168.0.50)로 검증.
3. **UI 뼈대 + 트리 + 검색** — vSphere Client 8 디자인(헤더/좌측 Navigator/탭/카드), 지연 로딩.
4. **Summary 화면** — vCenter/Datacenter/Cluster/Host/VM Summary, VM 목록 탭.
5. **로그인 런처** — Go+chromedp, 프로토콜 등록 스크립트, 딥링크 연동. 랩 vCenter로 자동 로그인 검증.
6. **문서/배포 패키지** — README·ARCHITECTURE·CHANGELOG·PR_CHECKLIST·사용법.txt, 공유폴더에 그대로 복사하는 `dist\` 구성, 전체 흐름 검증.

## 7. 기술 스택 / 아키텍처

```
[Windows 수집 서버]                     [공유폴더 \\fileserver\share\vc-portal]            [사용자 PC]
 작업 스케줄러 (12:00)                   ├ index.html, assets\                          탐색기로 index.html 열기
  └ run-collector.ps1                   ├ config\vcportal.conf  ◀ 수집기·런처 공용          │
     └ vcportal-collector.exe ─쓰기─▶    ├ launcher\vcportal.exe, install-launcher.ps1     ▼
                                        └ data\ manifest.js, index.js, vcNN.js       vcportal:// → 런처 → Edge
```

- 수집기: Go + govmomi — 단일 exe(복사 배포), PropertyCollector로 필요한 속성만 조회해 VM 13,000대+ 규모에도 부하가 작음. 래퍼는 PowerShell(운영) + bash(랩 검증).
- 데이터: `.js` 분할(인덱스/상세) + 지연 로딩 — 첫 로딩이 VM 증가에 거의 영향받지 않음.
- 원자적 교체: `work_dir`에서 완성 → 공유폴더에 임시 이름으로 복사 → rename.
- UI: 빌드 도구·CDN 없는 순수 HTML/CSS/JS, vSphere Client 8(Clarity) 스타일을 CSS로 재현, 아이콘은 인라인 SVG.
- 런처: Go + chromedp — 설치된 Edge/Chrome을 원격 디버깅 포트로 제어, 드라이버 불필요, 수집기와 같은 conf 파서 사용.

## 8. 리스크 및 제약사항

| 리스크 | 영향 | 대응 |
|---|---|---|
| 공유폴더 exe 실행/프로토콜 등록이 보안 정책에 막힘 | 런처 사용 불가 | 실제 사용자 PC에서 확인, 막히면 `%LOCALAPPDATA%` 복사 방식으로 전환 |
| 자동 로그인이 로그인 화면 구조에 의존 | 업그레이드 시 폼 변경 가능 | 선택자를 상수로 분리, 실패 시 로그인 화면에서 멈춤 |
| 자체 서명 인증서 | 전용 브라우저 프로필에 인증서 경고 | 전용 프로필에서만 인증서 오류 무시 옵션 사용 |
| conf 평문 비밀번호 | 공유폴더 권한 확대 시 노출 | 현재 권한자만 접근 → 허용, `config\` 권한 별도 축소 권장 |
| 공용 계정 잠김/변경 | 수집·자동 로그인 동시 중단 | 실패 사유를 화면에 표시, conf 한 곳만 수정 |
| 수집 서버 실행 계정의 공유폴더 권한 | 배포 실패 | `--check` 사전 점검, 드라이브 문자 경로 차단 |
| 하루 1회 수집 | 최대 24시간 전 상태 | 수집 시각 상시 표시, 최신은 "vCenter에서 열기"로 확인 |
| 데이터 증가 | 로딩 지연 | 인덱스/상세 분리 + 지연 로딩 |
