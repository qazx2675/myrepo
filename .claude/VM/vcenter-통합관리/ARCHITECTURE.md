# ARCHITECTURE.md

`vcenter-portal` 의 구조와 "무엇을 바꾸려면 어디를 고치나"를 정리한 문서입니다.
설계 배경은 [vcenter-integrated-portal-plan.md](vcenter-integrated-portal-plan.md), 데이터 형식은 [docs/DATA_SCHEMA.md](docs/DATA_SCHEMA.md).

## 1. 폴더 / 파일별 역할

| 경로 | 역할 |
|---|---|
| `cmd/vcportal-collector/main.go` | 수집기 진입점. `--conf`/`--check` 처리, 로그 설정, vCenter 병렬 수집 오케스트레이션, 실패 시 직전 캐시 사용, 종료 코드(0/1/2/3) 결정 |
| `cmd/vcportal-collector/publish.go` | `work_dir` 캐시 저장/읽기, `data\*.js` (상세/index/manifest) 작성, `output_dir` 로 임시 이름 복사 후 rename(원자적 교체) |
| `cmd/vcportal-collector/check.go` | `--check` 구현: conf 출력, 드라이브 경로/쓰기 권한/폴더 생성/vCenter 로그인 점검 |
| `cmd/vcportal/main.go` | 로그인 런처. `vcportal://` 파싱, 딥링크 검증(https + conf 호스트 + `/ui/`), 전용 프로필 브라우저 기동, chromedp 로 SSO 자동 로그인. **로그인 화면 선택자 상수(`selUsername`, `selPassword`, `selSubmit`, `selLoginErr`, `selLoggedIn`, `ssoPathMark`)와 디버그 포트/제한 시간 상수가 파일 상단에 있음** |
| `cmd/vcportal/sys_windows.go`, `sys_other.go` | OS 별 구현: 메시지 창, 브라우저 exe 탐색, 분리 실행(런처 종료 후에도 브라우저 유지), 포털 모드 중복 실행 방지 뮤텍스 |
| `cmd/vcportal/portal.go` | 포털 모드(인자 없이 실행): 전용 브라우저에 `index.html` 을 열고, `file://` 탭마다 `window.vcpOpen` 바인딩(CDP `Runtime.addBinding`)을 붙여 "vCenter에서 열기"를 확인 창 없이 처리. 이름 있는 뮤텍스로 중복 실행 시 새 탭만 연다 |
| `internal/conf` | 수집기·런처 공용 conf(INI) 파서. 경로 정리(`CleanPath`), UTF-8/CP949 디코딩, 검증, 드라이브 문자(네트워크 드라이브) 차단(`drive_windows.go`) |
| `internal/collect` | govmomi PropertyCollector 로 vCenter 1대를 수집해 트리 객체 맵(`objects`)·인덱스·카운트 생성. **트리 구성 규칙(Hosts and Clusters 기준, VM 부모 = `runtime.host`, 단독 호스트는 ComputeResource 생략)과 필드 추출은 여기** |
| `web/index.html`, `web/assets/` | 정적 웹 UI. `app.js`(라우팅/트리/검색/상태 배너), `summary.js`(Summary 화면), `helpers.js`(링크·포맷·스크립트 로더), `render.js`, `style.css`, `summary.css` |
| `scripts/run-collector.ps1` | 운영용 수집기 래퍼(Windows). 기본 conf 탐색, 뮤텍스(종료 코드 4), `-Check`, `-RegisterTask`(작업 스케줄러 12:00) |
| `scripts/run-collector.sh` | 랩/Linux 용 래퍼(없으면 빌드 후 인자 그대로 전달) |
| `scripts/install-launcher.ps1` | `vcportal://` 프로토콜을 HKCU 에 등록/해제 |
| `config/vcportal.conf.example` | conf 예제 |
| `build.ps1` / `build.sh` | 빌드(`bin/`) + 배포 패키지 조립(`dist/vc-portal/`, .ps1 은 BOM+CRLF 변환). Windows(PowerShell, 폐쇄망 설정 포함) / Linux(bash) 용으로 같은 결과를 만든다. 한쪽을 바꾸면 다른 쪽도 같이 바꾼다 |
| `testdata/gen-fake.js` | 가짜 데이터 생성기(UI 테스트, 대규모 15 vCenter / VM 13,000 등) → `testdata/fake/`(gitignore) |
| `testdata/sample-data/` | 랩 vCenter 실데이터 기반 소규모 샘플 |
| `docs/DATA_SCHEMA.md` | 데이터 파일 스키마(수집기와 웹 UI 사이의 계약) |
| `vendor/`, `go.mod`, `go.sum`, `tools.go` | 폐쇄망 빌드용 의존성(govmomi, chromedp, x/text) |
| `.github/workflows/vcenter-portal.yml` (저장소 루트) | CI |

## 2. 데이터 흐름

```
[수집 서버] 작업 스케줄러(12:00)
  └ run-collector.ps1 (뮤텍스, conf 탐색)
     └ vcportal-collector.exe --conf ...\config\vcportal.conf
        ├ internal/conf 로 conf 로드/검증
        ├ vCenter 별 goroutine (parallel 개수 제한, timeout)
        │    └ internal/collect: 로그인 → PropertyCollector → 트리/인덱스/카운트
        ├ 성공: work_dir\cache\<id> 저장, work_dir\out\<id>.js 작성
        ├ 실패: 직전 캐시로 대체(없으면 데이터 없음), manifest 에 status=fail + 사유
        ├ index.js / manifest.js 작성 (전체 vCenter 합본)
        └ output_dir\data\ 로 임시 이름 복사 → rename (상세 파일 먼저, manifest 마지막)

[사용자 PC] index.html (file:// 또는 UNC)
  ├ <script src=data/manifest.js>  → 상태 바/배너, vCenter 목록
  ├ <script src=data/index.js>     → 검색 인덱스
  └ 트리/Summary 진입 시 <script src=data/<id>.js?v=<collectedAt>> 지연 로딩
     (file:// 에서 fetch() 가 막히므로 모든 데이터가 .js)

[vCenter에서 열기] vcportal://open?url=<딥링크>
  → Edge 확인 창 → launcher\vcportal.exe (HKCU 프로토콜 등록, 공유폴더 exe 직접 실행)
  → ..\config\vcportal.conf 로 호스트 검증 → 전용 프로필 Edge(127.0.0.1:9333)
  → 로그인 필요 시 SSO 폼 자동 입력 → 딥링크 화면
```

## 3. 배포 폴더 구조와 conf 기본 위치

```
\\서버\공유\vc-portal\
  index.html, assets\, data\
  config\vcportal.conf          ← 수집기·런처 공용
  launcher\vcportal.exe         ← 기본 conf = <exe 폴더>\..\config\vcportal.conf
  collector\vcportal-collector.exe, run-collector.ps1
                                ← 래퍼 기본 conf = <스크립트 폴더>\..\config\vcportal.conf (없으면 스크립트 폴더)
```

## 4. 무엇을 바꾸려면 어디를 고치나

| 하고 싶은 일 | 위치 |
|---|---|
| vSphere Client 업그레이드로 자동 로그인이 깨짐 | `cmd/vcportal/main.go` 상단 선택자 상수 |
| 딥링크 URL 형식(버전별) 변경 | `web/assets/helpers.js` 의 `deepLink` / `openLink` |
| 트리 구성 규칙(폴더/클러스터/호스트/VM 부모) | `internal/collect/collect.go` 의 `walk`, `cluster`, `host`, `vm` |
| 수집 속성 추가/삭제 | `internal/collect/collect.go` (속성 목록 + 빌더) → `docs/DATA_SCHEMA.md` 갱신 → `web/assets/summary.js` 표시 |
| Summary 화면 항목 | `web/assets/summary.js` |
| 검색 대상/정렬 | `web/assets/app.js` (검색) + 인덱스 컬럼은 `cmd/vcportal-collector/publish.go` |
| 경고 배너 기준 시간(26시간) | `web/assets/app.js` 의 `STALE_HOURS` |
| conf 키 추가 | `internal/conf/conf.go` (파싱/검증) + `conf_test.go` + `README.md` 3.1 표 + `config/vcportal.conf.example` |
| 종료 코드/점검 항목 | `cmd/vcportal-collector/main.go`, `check.go` (+ `run-collector.ps1` 헤더 주석, README 3.2 표) |
| 스케줄 시각(12:00) | `scripts/run-collector.ps1` 의 `New-ScheduledTaskTrigger` |
| 디버그 포트/브라우저 인자/프로필 위치 | `cmd/vcportal/main.go` (`debugPort`, `startBrowser`, `appDataDir`) |
| 배포 패키지 구성 | `build.ps1`, `build.sh` (둘 다) |

## 5. 설계 메모

- **원자적 교체**: 수집 중에 사용자가 보는 `data\` 가 깨지지 않도록 `work_dir` 에서 완성한 뒤 임시 이름으로 복사하고 rename 합니다.
- **실패 격리**: 한 vCenter 의 실패가 전체를 막지 않으며, 직전 성공 데이터를 유지하고 manifest 에 실패 사유만 기록합니다.
- **conf 공유**: 수집기와 런처가 같은 파서(`internal/conf`)를 씁니다. 런처는 conf 에 없는 호스트의 링크는 열지 않습니다(임의 주소로 자격증명 입력 방지).
- **지연 로딩**: 인덱스(가벼움)와 vCenter 별 상세를 분리해, VM 이 늘어도 첫 로딩 시간이 거의 늘지 않습니다.
