# ARCHITECTURE.md — Network_Change_Integration_Script

통합 스크립트는 **얇은 bash 오케스트레이터**입니다. vСenter/노드와 실제로
통신하는 것은 전부 기존 3개 프로젝트의 Go 엔진이고, 이 스크립트는 그 앞뒤만
맡습니다.

```
change.sh ──> lib/preprocess.sh ─(표준화)─> work/vswitch_*.std.txt
          ──> lib/stages.sh ──> bin/ip-change-engine     ─(gossh)─> 노드
                            ──> bin/ldap-config-engine   ─(gossh)─> 노드
          ──> lib/results.sh ─(집계)─> results/*.txt
          ──> lib/incident.sh ─(저장/롤백)─> incidents/<name>/
          ──> (포트그룹) projects/vm-network-migration/run.sh ─(govmomi)─> vCenter
```

## 파일별 역할

| 폴더/파일 | 역할 |
|---|---|
| `change.sh` | 진입점. 인자 파싱, 서브커맨드 분기(port/rollback/--retry/--debug-inventory), 단계 순서 제어, 포트그룹 위임 |
| `setup.sh` | 폐쇄망 오프라인 빌드. `projects/` 아래 3개 프로젝트를 빌드하고 엔진 바이너리를 `bin/` 으로 모음 |
| `update.sh` | 폐쇄망 증분 업데이트. 배포 폴더에서 `bash update.sh <새버전경로>` — 운영값 파일 백업 → 코드 동기화 → 복원. `projects/` 포함 |
| `integration.conf.sample` | 중앙 설정 예시. 운영자는 `integration.conf` 하나만 편집 |
| `lib/common.sh` | 설정 로드(`load_conf`/`conf_get`), 로그(`log`/`dlog`), 디버그 레벨, 에러코드 출력(`die`), **`user선택()`(기본: 텍스트 입력, 사이트별로 교체 가능)** |
| `lib/conf.sh` | `integration.conf` → `conf/ip_change.conf` 렌더링 (§2.2). ldap/nm 은 플래그로 받아 렌더링 없음 |
| `lib/preprocess.sh` | `vswitch_<계정>.txt` Case 1/2/3 표준화 (§4). 출력은 정확히 3열 |
| `lib/stages.sh` | C(IP)·D(LDAP) 단계 실행. OS6 분기(§8.1, 관리서버 hostname 기준), 2패스 타임아웃(§8.2), **엔진 stdout 파싱** |
| `lib/results.sh` | 결과 5종 파일 기록(§6), `.bak` 백업, `--retry` 대상 산정 |
| `lib/incident.sh` | 인시던트 저장/로드(§5.3), 백업 위치 인덱싱, 역순 롤백(§7) |
| `conf/` | 렌더링된 프로젝트 conf (gitignore) |
| `work/` | 전처리 결과·중간 산출물 (gitignore, `-d2` 로 보존) |
| `results/` | 결과 목록 파일 (gitignore) |
| `incidents/<name>/` | `meta` + 결과 사본 + `state_*.json` + `backup_index.txt` |
| `logs/` | 실행별 로그 (gitignore) |

## 수정 요청별 진입점

| 요청 | 볼 곳 |
|---|---|
| "전처리 케이스 추가/변경" | `lib/preprocess.sh` |
| "에러코드 추가" | `lib/*.sh` 의 `die` 호출 + `README.md` 대응표 |
| "결과 파일 형식 변경" | `lib/results.sh` 의 `write_results` |
| "2패스/타임아웃 로직" | `lib/stages.sh` 의 `_stage_run` |
| "OS6 분기 규칙" | `lib/stages.sh` 의 `_mgmt_is_os6` / `_stage_run` |
| "포트그룹 OS6 바이너리 경로" | `lib/stages.sh` 의 `_nm_bin_dir` (`NM_BIN_DIR` 로 `run.sh` 에 전달) |
| "증분 업데이트 보존 규칙" | `update.sh` 의 `KEEP_FILES` / rsync `--exclude` |
| "포트그룹 위임 방식" | `change.sh` 의 `run_portgroup` |
| "포트그룹 상태 파일 충돌 처리" | `change.sh` 의 `run_portgroup` 앞부분 (`nm_state` 삭제 확인) |
| "인시던트/롤백" | `lib/incident.sh` |
| "계정 선택 로직 채우기" | `lib/common.sh` 의 `user선택()` |
| "IP+LDAP 확인 통합 로직" | `change.sh` 메인 흐름의 "2+3. C+D" 블록 + `lib/stages.sh` 의 `SKIP_LDAP_CONFIRM` |
| "색상 추가/변경" | `lib/common.sh` 상단 `C_*` 변수 + `log`/`die`/`warn_box`, `change.sh` 의 `confirm_targets`/`ask_yes` |
| "conf 키 추가" | `integration.conf.sample` + `lib/common.sh`(읽기) + 필요 시 `lib/conf.sh`(렌더링) |

## 설계상 지켜야 할 규칙

1. **자체 백업을 만들지 않습니다.** 세 프로젝트가 각자 백업을 만듭니다.
   특히 `ldap_setting` 은 "한 실행에서 파일당 백업 1회" 가 전제라, 통합이
   추가 백업을 뜨면 롤백이 원본으로 돌아가지 않습니다. `lib/incident.sh` 는
   백업 **위치만** 인덱싱합니다.

2. **gossh 를 직접 호출하지 않습니다.** 항상 엔진(`ip-change-engine` /
   `ldap-config-engine`)을 경유합니다. 엔진은 base64 `spaceOut` 으로 gossh
   위험키워드 오탐을 이미 막고 있습니다. 통합 레벨에서 자체 gossh 호출을
   추가하면 **같은 `spaceOut` 규칙을 반드시 복제**해야 합니다 (`reference_gossh_v2`).

3. **엔진 출력 파싱 계약.** `lib/stages.sh` 의 awk 패턴은 엔진의 화면 출력
   형식에 의존합니다.
   - `ip-change-engine` OK 줄: `<host> <ip> -> <ip>   (GW <ip>)`
   - `ip-change-engine` FAIL 줄: `<host> FAIL: <사유>` / `<host> UNREACHABLE ...`
   - `ldap-config-engine` 호스트 줄: `  <host> <OK|FAIL|NORESULT|UNREACHABLE>`
   엔진 출력이 바뀌면 CI 의 "엔진 출력 계약 확인" 이 먼저 깨지도록 해 두었습니다
   (`.github/workflows/network-change-integration.yml`).

4. **`change.sh rollback` 은 세 단계를 역순으로 모두 자동 원복합니다.**
   포트그룹(`nm run.sh --rollback`) → LDAP(`ldap-config-engine -rollback`) →
   IP(`ip-change-engine -rollback`, 각 노드의 최근 `<ifcfg>.bak.<STAMP>` 복원).
   IP 엔진 실행이 실패하면 대상 목록을 출력하고 수동 복원을 안내합니다.

5. **포트그룹 대상은 `ip_ok` 목록입니다.** IP 변경이 실패한 VM 을 새 VLAN 으로
   옮기면 "옛 IP + 새 VLAN" 으로 고립됩니다 (§5.2).

6. **`user선택()` 은 통합에서 단 한 번.** 하위 프로젝트 래퍼의 빈 선택 함수는
   호출하지 않습니다.

7. **로그 파일에는 색상 코드를 절대 넣지 않습니다.** `lib/common.sh` 의
   `log`/`die`/`warn_box` 는 파일에 쓰는 줄과 화면에 찍는 줄을 따로 만듭니다
   (색은 화면용에만). 이후 로그 출력에 색을 추가할 때도 이 분리를 유지하십시오
   — `grep`/자동화가 로그를 파싱하는 경우가 있습니다.

## 하위 프로젝트에 반영된 변경

이 통합을 위해 원본 프로젝트에도 넣은 변경 (자세한 내용은 각 `CHANGELOG.md`):

| 프로젝트 | 변경 | 이유 |
|---|---|---|
| `ldap_setting` | 자산현황 중복 → 마지막 줄 우선 / `default_site` conf 키·`-default-site` 플래그 / `ev01~03` 접미사 fallback / 롤백 시 미매칭 호스트 유지 | 자산현황에 VM 이 없어 site 판정 실패하는 문제 (§11) |
| `vm-network-migration` | `LoadVMList` 첫 필드만 / 호스트 이름 FQDN↔short 매칭 / `nm-inventory`(`--debug-inventory`) / 오류 메시지에 후보 목록 / `run.sh` 의 `NM_BIN_DIR` 오버라이드 / `build_os6.sh`(OS6 빌드) | `{user}.txt` 2열 공유, 상위폴더 다중 환경 진단 (§3.3, §9), 포트그룹 OS6 지원 |
| `ip_change` | `-rollback` / `-rollback-to` 서브모드 (`<ifcfg>.bak.<STAMP>` 복원), `target.LoadHosts` | `change.sh rollback` 의 IP 단계 자동 원복 (§7) |

> 이 변경들은 원본에도 반영했습니다. 통합본과 원본이 갈라지면 두 코드가 서로
> 다른 대상을 고르게 되어 추적이 불가능해지기 때문입니다 (계획서 §1.2 예외).
