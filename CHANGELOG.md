# CHANGELOG — Network_Change_Integration_Script

날짜순(최신이 위).

## 2026-09-10 — 3개 하위 프로젝트 소스를 `projects/`에 자체 보관 (완전 독립 이관)

- **사유**: "다른 폴더와 연관 없이 이 폴더에서 독립적으로만 작동하면 된다"는
  요구 확인. 앞선 `conf/` 자체 보관(같은 날짜, 아래 항목)만으로는 부족했음
  — `setup.sh`의 빌드 소스(`../../HPC/ip_change`, `../../HPC/ldap_setting`)와
  포트그룹(E) 단계가 런타임에 위임하는 `nm_dir`(`../vm-network-migration`)이
  여전히 외부 폴더를 상대경로로 참조하고 있었음. 사용자에게 빌드까지 이
  폴더 안에서 끝나야 하는지, 포트그룹 단계도 포함해야 하는지 확인 후(둘 다
  예) 세 프로젝트 소스 전체를 vendoring.
- **`projects/ip_change`, `projects/ldap_setting`, `projects/vm-network-migration`**
  신규 — 각각 `.claude/HPC/ip_change`, `.claude/HPC/ldap_setting`,
  `.claude/VM/vm-network-migration` 의 전체 소스 스냅샷 복사(빌드 스크립트·
  `internal/`·`cmd/`·`vendor/`·자체 `README.md`/`CHANGELOG.md`/`.gitignore`
  포함). 세 프로젝트 모두 자체 스크립트가 `cd "$(dirname "$0")"` 로 시작해
  상대경로에 의존하지 않아, 그대로 옮겨도 수정 없이 동작함을 확인.
- **`setup.sh`**: `IP_DIR`/`LDAP_DIR`/`NM_DIR` 을 `../../HPC/...`,
  `../vm-network-migration` → `./projects/ip_change`, `./projects/ldap_setting`,
  `./projects/vm-network-migration` 로 변경. 헤더 주석·완료 안내 문구도 갱신.
- **`integration.conf.sample`**: `nm_dir` 기본값을 `../vm-network-migration`
  → `./projects/vm-network-migration` 로 변경.
- **`change.sh`, `lib/incident.sh`**: `conf_get nm_dir <기본값>` 4곳을 동일하게
  변경 (`run_portgroup`, `--debug-inventory`, `incident_save`, `rollback_incident`).
- **`README.md`**: 1.1 빌드 섹션 전면 갱신("이 폴더 하나만 있으면 됨"),
  프로젝트 위치 표, `nm-*` 바이너리 경로, 에러코드 E9 대처 갱신. `projects/`
  가 원본의 스냅샷이며 자동 동기화되지 않는다는 안내 추가.
- **`ARCHITECTURE.md`**: 흐름도의 `../vm-network-migration/run.sh` →
  `projects/vm-network-migration/run.sh`.
- **`Network_Change_Integration_Plan.md`**: §11.6 신규 — vendoring 배경과
  "원본이 갱신돼도 자동 동기화 안 됨, 이관 전 diff 확인 필요" 트레이드오프 명시.
- **영향 범위**: `setup.sh`, `integration.conf.sample`, `change.sh`,
  `lib/incident.sh`, `README.md`, `ARCHITECTURE.md`,
  `Network_Change_Integration_Plan.md`, `projects/*`(신규 디렉터리 3개).
- **검증**: `bash -n change.sh setup.sh lib/*.sh` 전체 통과.
  `tests/test_preprocess.sh` 통과(회귀 없음). `GOMODCACHE=$(mktemp -d)
  GOPROXY=off ./setup.sh` 실행 — `./projects/ip_change` 로 정상 진입해
  빌드를 시도하는 것까지 확인(경로 배선 정상). 이 샌드박스는 `go1.24.7`,
  세 프로젝트 `go.mod` 는 `go 1.26.5` 요구라 툴체인 다운로드가 막혀
  실제 컴파일은 이 환경에서 끝까지 못 돌림 — 원본(`HPC/ip_change`)도
  동일한 제약이라 이번 변경과 무관한 환경 제약임을 확인(`go.mod` 비교).
  실제 배포 환경(Go 1.26.5 보유)에서 `./setup.sh` 로 재검증 필요.

## 2026-09-10 — LDAP 설정 참조를 `conf/` 자체 보관으로 전환 (타 부서 이관 대비)

- **사유**: 이 폴더가 조만간 다른 부서로 **단독 이관**될 예정. 기존
  `ldap_conf`/`ldap_assets` 기본값이 `../../HPC/ldap_setting/conf/...` 상대
  경로로 원본을 참조하고 있었는데, `Network_Change_Integration_Script` 폴더만
  떼어 이관하면 그 경로가 깨짐.
- **`integration.conf.sample`**: `ldap_conf`/`ldap_assets` 기본값을
  `./conf/ldap_config.conf` / `./conf/assets.txt` 로 변경. 원본과는 별개
  사본이며 자동 동기화되지 않는다는 점을 주석으로 명시.
- **`conf/ldap_config.conf.sample`, `conf/assets.txt.sample`** 신규 추가 —
  `HPC/ldap_setting/conf/`의 동일 샘플을 그대로 복사해 이 프로젝트 안에
  자체 보관(둘 다 `.gitignore`로 실 파일 커밋은 차단됨, `.sample`만 추적).
- **`setup.sh`**: 완료 안내에 `conf/ldap_config.conf.sample` →
  `conf/ldap_config.conf`, `conf/assets.txt.sample` → `conf/assets.txt` 복사
  단계 추가 (번호 재정렬).
- **`README.md`**: 1.2 설정 섹션에 `conf/` 사본 복사 명령 추가, 설정 표의
  `ldap_conf`/`ldap_assets` 설명과 자체 보관 이유를 명시.
- **범위 밖(의도적으로 안 건드림)**: `setup.sh`의 빌드 소스 의존(`../../HPC/*`,
  `../vm-network-migration`)과 포트그룹(E) 단계가 런타임에 위임하는
  `nm_dir = ../vm-network-migration` 은 이번에 손대지 않음 — 사용자가
  "이 폴더만 독립 이관, 빌드/포트그룹 쪽은 그대로 둬도 됨"으로 범위를
  한정함. **다만 포트그룹 단계까지 함께 이관한다면 `nm_dir`도 같은 문제가
  생기므로 별도로 짚어야 함** (README/작업기록에 안내만 남김, 코드 변경 없음).
- **영향 범위**: `integration.conf.sample`, `conf/ldap_config.conf.sample`(신규),
  `conf/assets.txt.sample`(신규), `setup.sh`, `README.md`.
- **검증**: `bash -n change.sh lib/*.sh setup.sh` 전체 통과. `git status` 로
  샘플 파일이 `.gitignore`에 걸리지 않고 정상 추적됨을 확인.

## 2026-09-09 — LDAP 대상 인프라를 매 실행 필수 선택으로 변경

- **`change.sh`**: `--infra <이름>` 옵션 추가 (usage 에 반영, `usage()` 의
  헤더 주석 추출 범위도 수정).
- **`lib/stages.sh`**: `select_ldap_infra()`/`_ldap_infra_names()` 추가.
  `--infra` 가 없으면 `ldap_conf`(`ldap_config.conf`)에서 `infra.<이름>.` 패턴을
  뽑아 대화형(`select`)으로 고르게 하고, 비대화형이면 사용 가능한 이름 목록과
  함께 D2 로 중단. `stage_ldap()` 진입 시 선택된 인프라를 로그로 남기고
  (`-y`/`--dry-run` 이 아니면) 적용 전 확인 프롬프트를 추가. `_run_ldap` 은
  `conf_get ldap_infra` 대신 전역 `$INFRA` 사용.
- **`lib/incident.sh`**: 인시던트 저장 시 선택된 `INFRA` 를 `meta` 의
  `ldap_infra` 로 기록하고, `incident_load` 에서 복원. `rollback_incident` 는
  `conf_get ldap_infra` 대신 인시던트에 저장된 `$INFRA` 로 롤백해 항상 원래
  적용됐던 인프라로 정확히 되돌아가게 함. 저장값이 없는(구버전) 인시던트는
  자동 롤백을 건너뛰고 수동 명령을 안내.
- **`integration.conf.sample`**: `ldap_infra` 키 제거(주석으로 사유 설명만
  남김). `README.md` 설정 표·옵션 표·에러코드(D2) 대응표·전체 흐름 다이어그램
  갱신. `Network_Change_Integration_Plan.md` §11.4 에 배경 설명 추가.
- **사유**: 원본 `ldap_setting`(`deploy_ldap.sh`)은 `-infra` 를 매 실행 필수
  CLI 인자로 요구해(기본값 없음, ARCHITECTURE 규칙 5) 이전 작업의 인프라가
  남아 다음 실행에 잘못 적용되는 사고를 막았는데, 통합 스크립트는 이를
  `integration.conf` 의 정적 값으로 되돌려 그 안전장치를 무력화하고 있었음.
  원본과 동일한 안전장치로 복원.
- **영향 범위**: `change.sh`, `lib/stages.sh`, `lib/incident.sh`,
  `integration.conf.sample`, `README.md`, `Network_Change_Integration_Plan.md`.
- **검증**: `bash -n` 전체 통과. `tests/test_preprocess.sh` 통과(무관 영역
  회귀 없음 확인). `select_ldap_infra` 를 `ldap_config.conf.sample` 로 단위
  실행: `--infra zxcv` 지정 시 그대로 사용, 미지정+비대화형(`</dev/null`) 시
  사용 가능한 이름 목록과 함께 D2 로 정상 중단되는 것 확인.
- **배포**: 2026-09-10, 사용자 요청으로 PR 없이 `claude/ldap-single-config-infra-select-ii8uzm`
  → `master`(기본 브랜치) fast-forward 로 직접 반영 (커밋 `46bbe3d`).

## 2026-09-09 — 최초 구현 (nci-v0.1.0)

### 신규

- **`change.sh`** — 통합 오케스트레이터. 전처리 → IP 변경 → LDAP → 결과 집계 →
  포트그룹 질의. 서브커맨드: `port` / `rollback` / `--retry ip|ldap` /
  `--debug-inventory`. 단계 격리: `--only` / `--from`. 디버그: `-d1`~`-d3`.
- **에러코드 체계 A~G** (§10). 화면 복사가 안 되는 환경을 전제로 코드만 크게
  출력하고, 원인·대처는 `README.md` 대응표에.
- **전처리** (`lib/preprocess.sh`, §4) — `vswitch_<계정>.txt` 를 Case 1/2/3 규칙으로
  정확히 3열(`BM.도메인 PG VLAN`)로 표준화. IP 앞 3옥텟 + `-0`. 도메인 기본값
  `seccae.com`. 이미 FQDN 인 줄과 IP 를 BM 으로 쓰는 줄은 멱등 처리.
- **2패스 타임아웃** (§8.2) — 1차 짧게 → 타임아웃 호스트 중 ping 되는 것만 2차
  길게. ping 안 되면 즉시 실패 확정.
- **OS 6 분기** (§8.1) — `os6_hostgroup` 의 호스트는 별도 gossh/바이너리로 나눠
  호출. OS6/일반 혼재 시 그룹별 2회.
- **결과 리다이렉션** (§6) — `on_off` / `ip_ok` / `ldap_ok` / `on_off.failed` /
  `failed`(에러코드 포함) 5종. 기존 파일은 `.bak.<시점>` 백업 후 재작성.
  포트그룹 대상은 `ip_ok` 만 (IP 실패 VM 의 VLAN 고립 방지, §5.2).
- **인시던트** (`lib/incident.sh`, §5.3) — 포트그룹 보류 시 저장, `change.sh port
  <인시던트>` 로 재개. `backup_index.txt` 에 세 프로젝트 백업 위치를 인덱싱만
  (자체 백업 안 만듦, §7.1).
- **롤백** (§7.2) — `change.sh rollback <인시던트>` 가 역순(포트그룹 → LDAP → IP).
  포트그룹은 `nm run.sh --rollback`, LDAP 은 `ldap-config-engine -rollback` 자동.
  IP 는 자동 롤백 없어 대상 목록 출력 + 수동 안내.
- **중앙 conf 렌더링** (§2.2) — `integration.conf` → `conf/ip_change.conf`.
  ldap/nm 은 플래그로 전달.
- **`setup.sh`** — 폐쇄망 오프라인 빌드. 3개 프로젝트를 각자 위치에서 빌드하고
  엔진 바이너리를 `bin/` 으로 수집.

### 하위 프로젝트 변경 (원본에도 반영, 계획서 §1.2 예외)

- `ldap_setting`: 자산현황 중복 → 마지막 줄 우선 / `default_site`
  (conf 키 + `-default-site` 플래그) / `ev01~03` 접미사 fallback / 롤백 시
  미매칭 호스트 유지.
- `vm-network-migration`: `LoadVMList` 첫 필드만 읽음 / 호스트 이름 FQDN↔short
  매칭 / `nm-inventory`(`run.sh --debug-inventory`) 추가 / "찾을 수 없음" 오류에
  후보 목록·색인 개수 표시.

### 알려진 제약

- `ip-change-engine` 은 dry-run 을 지원하지 않아 `--dry-run` 의 IP 단계는
  대상 표시만 하고 엔진을 실행하지 않습니다.
- IP 변경은 자동 롤백이 없습니다.
- 엔진 stdout 파싱에 의존합니다 (계약은 CI 로 감시).
- 계획서 §16 미해결: `on_off` 성공 기준(IP·LDAP 둘 다), 짧은 이름 DNS 해석
  전제, 상위폴더 다중 환경 근본 원인.
