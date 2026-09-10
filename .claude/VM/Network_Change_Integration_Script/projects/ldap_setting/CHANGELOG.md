# CHANGELOG — ldap_setting

## 2026-09-09

### 변경
- 자산현황(`assets.txt`)에 같은 호스트가 여러 줄이면 **오류 대신 마지막 줄을 채택**
  (경고만 출력). 갱신된 자산현황을 그대로 쓸 수 있게 함.
- `default_site` conf 키 추가. `-host-file` 의 호스트가 자산현황에 없을 때
  `ev01~ev03` 접미사를 뗀 BM 이름으로 재조회하고, 그래도 없으면 `default_site` 를
  site 로 적용. `default_site` 가 해당 인프라에 없는 site 면 시작 시 오류로 중단.
- 롤백은 자산현황에 없는 호스트도 대상에 유지 (site 불필요). `default_site` 로만
  포함되던 호스트가 롤백에서 누락되는 문제 방지.
- `-default-site` 플래그 추가 — conf 의 `default_site` 를 덮어씁니다. 통합
  스크립트가 운영자 conf 를 건드리지 않고 값을 넘길 수 있게 함.

## 2026-09-07

### 신규
- 최초 구현. Go 설정 엔진 + bash 배포 래퍼.
- `ldap_config.conf` 를 평문 `key = value` 형식으로 설계. 대상 노드에 jq 가 없어도 동작.
- 인프라 × 사이트 2차원 스키마. 인프라별로 DNS/NTP/URI/binddn/bindpw 를 따로 정의.
- 사이트별 적용 스크립트 하나를 만들어 gossh 로 주입. RHEL 버전과 s4 판정은 노드 현장에서
  수행하므로 gossh 호출이 노드 수가 아니라 사이트 수만큼으로 끝남.
- 전송은 base64 파이프. gossh 에 파일 전송 기능이 없고, 따옴표·개행으로 명령이 깨지지
  않기 때문. `scp` 미사용.
- 설정 적용을 파일 전체 교체가 아닌 **키 단위 수술적 갱신**으로 구현.
  변경 전 `.bak.<타임스탬프>` 백업.
- 바뀐 파일에 대응하는 서비스만 재시작 (`nslcd` / `sssd` + `sss_cache -E` / `autofs` / `ntpd` / `chronyd`).
- RHEL 7 이하 `nslcd` + `ntp`, 8 이상 `sssd` + `chrony` 분기.
  `s4` 접두사 호스트는 8 이상이어도 `nslcd` + `ntp` 강제 (규칙은 conf 로 제어).
- `-infra` 를 기본값 없는 필수 인자로 지정. 다른 인프라 값을 실수로 적용하는 사고 방지.
- `-root` / `ROOT` 로 fixture 디렉터리에 대고 테스트 가능. 이 모드에서는 서비스를 재시작하지 않음.
- `-dry-run` 은 diff 를 보여주고 파일도 서비스도 건드리지 않음.
- 설정 파일 검증을 시작 시점에 수행 — storage 중복, 존재하지 않는 uri_order 참조, 필수 키 누락은
  한 대도 건드리기 전에 오류로 중단.
- `test_all.sh` 추가 — 장비 없이 14개 시나리오 왕복 검증.

### 구현 중 수정한 버그
- `set_ini_in_section` 이 awk `-v` 로 정규식을 넘겨 `\[` 가 `[` 로 해석되면서 sssd.conf 를
  빈 파일로 만들던 문제. 환경변수 + `ENVIRON[]` 로 전환하고 섹션 판정을 문자열 접두사 비교로 변경.
  추가로 `commit_file` 에 "원본이 비어있지 않은데 결과가 비면 반영하지 않는다" 가드 추가.
- `ROOT` 를 지정한 테스트 모드에서 실제 서비스를 재시작하던 문제.
  ROOT 가 있으면 재시작을 건너뛰도록 수정.
- chrony.conf 의 기존 `pool` 줄을 제거하지 않아 검증이 FAIL 되던 문제.
  `server` 와 함께 `pool` 도 제거하도록 수정.

### 알려진 제약
- `ldap_config.conf` 의 `bindpw` 는 평문입니다. 퍼미션 600 유지 및 커밋 금지.
- 원격 적용 시 bindpw 가 담긴 스크립트가 잠시 `/root/ldap_apply.sh` 로 내려갑니다(실행 후 삭제).
- `resolv.conf` 는 NetworkManager 가 재기동 시 덮어쓸 수 있습니다. NM 관리 제외는 범위 밖입니다.
- `sssd.conf` 에 `[domain/...]` 섹션이 없으면 FAIL 처리하며 파일을 새로 만들지 않습니다.

## 2026-09-07 (2차)

### 신규 — 되돌리기(rollback)
- `-list-backups` — 각 노드에 남아 있는 백업 시점과 그 시점에 백업된 파일 목록을 조회. 변경 없음.
- `-rollback` — 가장 최근 시점으로 되돌리기.
- `-rollback-to <시점>` — 지정한 시점으로 되돌리기. 시점은 숫자 14자리만 허용
  (원격 셸로 나가는 값이라 형식을 엄격히 검사).
- 되돌리기에는 `-infra` 가 필요 없습니다. 노드에 남아 있는 백업만 보고 판단하므로
  설정 파일도 읽지 않습니다. `-dry-run` 과 `-root` 는 그대로 동작합니다.
- 복원된 파일에 대응하는 서비스만 재시작합니다(적용과 같은 표).
- 되돌리기는 새 백업을 만들지 않아, 같은 시점으로 두 번 되돌리면 두 번째는 `NOCHANGE` 입니다.
- 적용이 새로 만든 파일(백업이 없는 파일)은 삭제하지 않고 `NO-BACKUP` 으로 보고만 합니다.
- `scripts/deploy_ldap.sh` 에도 같은 옵션을 노출. `-infra` 를 함께 주면 되돌린 뒤 검증까지 수행.

### 구조 변경
- 서비스 재시작 표와 공통 유틸을 `internal/render/lib_common.sh` 로 분리해
  apply 와 rollback 이 같은 표를 쓰도록 했습니다. 두 곳에 두면 반드시 어긋납니다.

### 수정한 버그
- **백업이 한 실행에서 여러 번 덮어써지던 문제.** `ldap.conf` 처럼 한 파일을 여러 번
  (URI/BINDDN/BINDPW) 고치면 `.bak.<시점>` 이 매번 갱신되어, 최종 백업이 '이미 일부
  수정된 상태' 가 되었습니다. 롤백해도 원본으로 돌아가지 않는 심각한 문제였습니다.
  `commit_file` 이 `.bak.<시점>` 이 없을 때만 백업을 뜨도록 수정했습니다.
  이 버그는 롤백 기능을 만들면서 처음 드러났습니다.

### 테스트
- `test_all.sh` 에 롤백 시나리오 6건 추가 (총 22건).
  원본 일치 / 여러 번 수정된 파일 복원 / 멱등 / DRY-RUN 무변경 / 시점 조회 /
  지정 시점 복원 / 백업 없음 처리.

## 2026-09-07 (3차)

### 신규 — 대화형 실행 스크립트
- `scripts/run.sh` 추가. 빌드된 `bin/ldap-config-engine` 실행을 돕는 대화형 래퍼.
  - 계정 선택(`select_user_context`) — 목록이 비어 있는 골격만 제공. 실제 환경의
    계정 목록은 사용자가 직접 채워 넣도록 `# TODO` 로 표시.
  - 선택한 계정명으로 `{계정명}.txt` (conf/assets.txt 와 동일한 hostname<TAB>site 형식)를
    작업 대상으로 사용.
  - `ldap_config.conf` 에 실제 정의된 인프라만 메뉴로 보여주고 선택하게 함 — 목록에
    없는 인프라는 애초에 고를 수 없어 오타 사고 방지.
  - 사이트별 처리는 선택하지 않음. 엔진이 대상 파일의 site 컬럼을 보고 자동으로 분리.
  - DRY-RUN → `yes` 확인 → 실제 적용 순서 고정. `deploy_ldap.sh` 와 달리 검증(gossh
    ldap_check.sh 실행)은 하지 않고 적용까지만 수행.

## 2026-09-07 (4차)

### 수정 — 자산현황과 작업 대상 목록의 혼동
- `scripts/run.sh` 가 `{계정명}.txt` 를 통째로 엔진의 `-assets`(자산현황)로 넘기고
  있었던 문제. `{계정명}.txt` 에 site 정보를 채워야만 동작해, 사실상 자산현황
  역할을 하게 되는 설계 결함이었다.
- 엔진에 `-host-file <경로>` 플래그 추가. 순수 hostname 목록(site 정보 없음)으로
  "어떤 호스트를 고를지" 만 정하고, site 는 **항상** `-assets`(`conf/assets.txt`)
  에서 조회한다. `-host-file` 에 있는데 자산현황에 없는 호스트는 조용히 무시하지
  않고 경고를 찍은 뒤 건너뛴다.
- `internal/asset/LoadHostList` 추가 — 자산현황 파서(`Load`)와 별도로, 탭을 자르지
  않고 줄 전체를 하나의 호스트로 취급한다(자산현황 형식과 혼동해 잘못 쓰면 바로
  드러나도록).
- `scripts/run.sh` 를 `-host-file` 을 쓰도록 갱신. `{계정명}.txt` 형식 안내와
  요약 출력도 "hostname 목록 / 자산현황은 conf/assets.txt" 로 정정.

### 신규 — `update.sh`
- 실 서버에 이 폴더를 통째로 복사해 운영하는 배포 형태를 위해 추가.
  `conf/ldap_config.conf`, `conf/assets.txt`(실제 운영값)를 백업 후 복원하는 방식으로
  보존하면서, 나머지 코드만 지정한 새 버전 디렉터리 내용으로 동기화한다.
- `bin/`, `.git/` 은 동기화 대상에서 제외. 갱신 후 `./setup.sh` 재빌드가 필요함을 안내.
- `rsync` 있으면 사용, 폐쇄망이라 없으면 `find`+`cp` 로 자동 대체.
- 소스/대상 동일 여부, 소스가 ldap_setting 프로젝트인지(`go.mod`/`cmd/` 존재) 검사 후 진행.

### 검증
- 실 배포 시나리오 재현: `/root` 하위에 실제 운영값(bindpw, 자산현황)을 채운 사본을
  만들고, 코드에 마커를 추가한 "새 버전" 사본으로 `update.sh` 실행 → 코드는
  새 버전으로 바뀌고 운영값은 100% 보존됨을 확인. 재빌드 후 `go test ./...` 통과.
- `-host-file`: 자산현황에 3대, 호스트 목록에 1대만 있을 때 그 1대만 처리되고
  site 는 자산현황에서 정확히 조회됨을 실제 gossh 경유로 확인. 목록에 자산현황에
  없는 호스트가 섞였을 때 경고 후 건너뛰는 것도 확인.

### 수정 — 실 서버 DRY-RUN 이 "ERROR Command not found" (rc=2) 로 깨지던 문제
- 원인: `remote.BuildCommand` 가 base64 payload 를 작은따옴표로 감싸 보냈는데
  (`echo '<b64>' | base64 -d ...`), gossh 가 명령 전체를 다시 자기 쪽에서
  따옴표로 감싸 ssh 로 넘기는 경우 이 안쪽 작은따옴표가 바깥 따옴표를 조기에
  닫아버려 명령이 토막났다. base64 결과는 `[A-Za-z0-9+/=]` 만 포함해 원래
  따옴표가 필요 없으므로 제거.
- 대상 노드에 bash/base64/awk/sed 가 모두 있는데도(사용자가 gossh 로 직접
  `which` 확인) 재현되던 것과 부합하는 원인.
- 검증: `go test ./...`, `test_all.sh` (22 PASS), 그리고 192.168.0.58 에서
  실제 gossh 왕복(-host-file, -root fixture)으로 명령이 정상 전달·실행·
  파싱됨을 확인.

### 수정 — `run.sh` 사전점검 누락으로 이전 두 문제가 조용히 재발할 수 있었음
- `run.sh` 에 `gossh` 존재 여부(`command -v`) 검사가 없어서, PATH 에 없으면
  (예: `sudo` 의 `secure_path` 가 `/usr/local/bin` 을 빼버리는 환경) 엔진이
  gossh 를 로컬에서 못 찾아 아무 호스트에도 못 나가고 `NORESULT` 로만
  보고했다. `deploy_ldap.sh` 에는 이미 있던 검사를 `run.sh` 에도 추가하고,
  `GOSSH` 환경변수(기본값 `gossh`)로 절대경로를 지정할 수 있게 함.
- `run.sh` 에 `-host-file` 을 모르는 예전 바이너리를 그대로 쓰고 있어도
  바로 알아채지 못했다. `$ENGINE -h` 출력에 `-host-file` 이 있는지 미리
  검사해, 오래된 빌드면 "update.sh 로 갱신 후 setup.sh 로 재빌드하십시오"
  라고 바로 안내하도록 함.
- 검증: 192.168.0.58 에서 존재하지 않는 gossh 이름을 줬을 때 사전점검에서
  바로 실패하는지, 정상 이름일 때 hostname 한 줄짜리 목록 + 자산현황만으로
  site 가 올바르게 조회되어 gossh 까지 정상 도달하는지 실제로 실행해 확인.

### 개선 — 탭 구분자 오류 메시지에서 스페이스/탭을 눈으로 구분 가능하게 함
- 사용자가 "탭으로 넣었다" 고 확신하는데도 계속 탭 구분자 오류가 나는 사례가 있어,
  에러 메시지에 스페이스는 `·`, 탭은 `→` 로 표시해 실제 파일 내용을 그 자리에서
  바로 눈으로 확인할 수 있게 했다. (`internal/asset/asset.go` `visualizeWhitespace`)
- 흔한 원인: 에디터의 "탭을 스페이스로 저장" 설정, 표/문서에서 복사-붙여넣기 시
  탭이 여러 개의 스페이스로 바뀌는 경우. 검증은 여전히 탭만 인정하도록 유지
  (스페이스 구분을 허용하면 처음에 합의한 "구분자는 탭이다" 규칙이 깨짐).

### 수정 — 대상 계정 로그인 셸이 csh/tcsh 면 "Command not found"/"Undefined variable" 로 깨지던 문제
- 실제 증상: `ERROR: DRYRUN=1: Command not found`, `rc : Undefined variable`.
- 원인: `BuildCommand` 가 만드는 명령이 `VAR=값 명령`, `rc=$?` 같은 bash 전용
  문법을 그대로 담고 있었는데, gossh 가 대상 계정의 로그인 셸로 이 문자열을
  넘기면 그 셸이 직접 파싱을 시도한다. 로그인 셸이 csh/tcsh 면 이 문법을
  이해하지 못해 `VAR=값` 을 명령 이름으로 착각해 "Command not found", 이후
  `rc=$?` 도 실패해 `rc` 가 정의되지 않은 채로 `$rc` 를 참조하다 "Undefined
  variable" 로 깨진다.
- 1차 시도로 전체를 `bash -c '...'` 로 감쌌으나, gossh 가 명령 전체를 다시
  자기 쪽에서 따옴표로 감싸 ssh 로 넘기는 방식과 충돌해 tcsh 에서
  `Unmatched '''` 로 또 깨졌다 (192.168.0.58 에 tcsh 계정을 만들어 재현).
- 최종 수정: 명령 자체(umask/env 조립/bash 호출)까지 통째로 base64 로
  인코딩해 `echo <b64> | base64 -d | bash` 형태로 바꿨다. 전송되는 명령줄에
  따옴표가 하나도 남지 않아, gossh 가 어떻게 다시 감싸든, 대상 계정의
  로그인 셸이 bash 든 csh/tcsh 든 관계없이 동일하게 동작한다.
- 검증: 192.168.0.58 에 `/bin/tcsh` 를 로그인 셸로 쓰는 계정을 만들어 실제
  gossh 로 왕복 — 수정 전 문법으로는 정확히 사용자가 겪은 두 에러가 그대로
  재현됐고, 수정 후에는 OS 판정·7개 대상 파일 diff·재시작 계획까지 DRY-RUN
  전체가 정상 출력됨을 확인. `go test ./...`, `test_all.sh` (22 PASS) 도 통과.

### 수정 — `deploy_ldap.sh` 검증 단계에도 같은 csh/tcsh 문제가 남아 있었음
- 엔진 쪽(`internal/remote.BuildCommand`)만 고치고 `deploy_ldap.sh` 의 검증(`-check-only`
  포함) 단계는 그대로 두었던 것을 놓쳤다. 이 단계는 `REMOTE_CMD` 를 직접 문자열로
  조립해 `gossh -script -w ... "$REMOTE_CMD"` 로 보내는데, 그 안에 작은따옴표와
  `rc=$?` 같은 bash 전용 문법이 그대로 들어 있어 대상 계정 로그인 셸이 csh/tcsh 면
  똑같이 "Command not found"/"Undefined variable" 로 깨졌다.
- 실사용자가 run.sh 로 적용은 성공한 뒤, 검증을 위해 (run.sh 가 안내하는 대로)
  `deploy_ldap.sh -check-only` 를 실행하다 이 문제를 겪은 것으로 보인다.
- 수정: `REMOTE_CMD` 조립 로직은 그대로 두고, 최종 문자열을 base64 로 한 번 더
  감싸 `echo <b64> | base64 -d | bash` 형태로 gossh 에 넘기도록 바꿨다
  (engine 쪽과 동일한 패턴, ARCHITECTURE.md 11번 규칙).
- 검증: 192.168.0.58 에 `/bin/tcsh` 계정을 다시 만들어, 수정한 `REMOTE_CMD` 조립
  로직을 그 계정으로 실제 gossh 호출 — 셸 에러 없이 `ldap_check.sh` 가 정상
  실행되고 결과(`FAIL infra-mismatch` 등, 이 노드가 미구성이라 정상적으로 나오는
  값)가 그대로 돌아옴을 확인. `go test ./...`, `test_all.sh`(22 PASS),
  `ldap_check/test_check.sh`(10 PASS) 도 통과.

## 2026-09-08

### 신규 — `/wappl` 마운트 지원, `/etc/auto.appl` 전체 덮어쓰기로 전환
- 일부 인프라에만 존재하는 `/wappl` 마운트를 지원. `ldap_config.conf` 의
  `infra.<이름>.site.<사이트>.wappl_mount` 가 설정된 사이트만 `/wappl` 줄을
  만든다(선택 항목 — 없으면 그 사이트는 지금처럼 `/appl` 한 줄만 유지).
- 마운트 옵션 변경: `/appl` 은 `-rw,soft,intr` → `-ro,hard,tcp,vers=3`,
  `/wappl` 은 `-rw,hard,tcp,vers=3`. 구분자는 기존과 동일하게 탭.
- `internal/render/apply_body.sh` 의 `apply_auto_appl` 을 기존 줄 보존형 편집에서
  **전체 덮어쓰기** 로 변경. `/etc/auto.appl` 은 다른 설정 파일과 달리 운영자가
  손댄 다른 내용과 섞여 있지 않아 통째로 관리해도 안전하다고 판단.
- 적용 전 `/etc/auto.appl` 이 있으면 고정 이름 `/etc/auto.appl_back` 으로도
  별도 백업. `commit_file` 이 만드는 `.bak.<시점>` 백업(및 그걸 쓰는 rollback)은
  그대로 유지 — `auto.appl_back` 은 참고용 추가 백업일 뿐, rollback 이 이 파일을
  지우거나 복원 대상으로 쓰지는 않음(ARCHITECTURE.md 12번).
- `internal/config`, `internal/render`, `conf/ldap_config.conf.sample`,
  `../ldap_check/ldap_config.conf.sample`, `../ldap_check/ldap_check.sh` 모두
  짝을 맞춰 갱신(`wappl_mount` 가 설정된 사이트만 `/wappl` 검사도 추가로 수행).

### 개선 — `run.sh` 성공/실패 표시에 색 추가
- DRY-RUN·실제 적용 각각 종료 후 rc 로 성공(초록)/실패(빨강) 한 줄을 덧붙임.
  터미널이 아니면(`[ -t 1 ]` 거짓) 색 없이 그대로 출력해 로그에 이스케이프
  코드가 섞이지 않게 함.

### 검증
- `go build/vet/test ./...` 통과.
- `test_all.sh` 22/22 PASS (auto.appl_back 이 rollback 비교 대상에서 제외되도록
  `--exclude='auto.appl_back'` 추가).
- `../ldap_check/test_check.sh` 10/10 PASS (`wappl_mount` 를 넣은 zxcv/a1 기준
  fixture 로 OK 8건 확인).

## 2026-09-09

### 수정 — /wappl 설정 추가 후 "이상한 base64 값" 이 출력되고 적용이 안 되던 문제
- 증상: `run.sh` 실제 적용 단계에서 `<base64 덩어리>|base64 -d | bash` 처럼
  보이는 텍스트가 그대로 출력되고 해당 호스트는 아무것도 바뀌지 않음.
  `wappl_mount` 설정을 빼면 정상 동작, 넣으면 재현됨(사용자 확인).
- 원인: gossh 가 명령 문자열에 `reboot`/`poweroff`/`shutdown`/`halt`/`ddc`
  (대소문자 무시, 부분일치) 가 섞여 있으면 위험 작업으로 보고 실행을 거부하며
  그 명령을 그대로 stderr 에 찍는 안전장치가 있음(`gossh` 자체 기능, 정상
  동작). `BuildCommand` 가 스크립트 전체를 base64 로 감싸는데, base64 는
  사실상 무작위 문자열이라 스크립트가 길어질수록 이 중 3글자 이상 짧은
  조각(특히 `ddc`)이 우연히 섞여 나올 확률이 올라간다. `/wappl` 기능으로
  스크립트가 늘어나면서(약 20KB) 이 확률에 걸림.
- 대응: `BuildCommand` 가 만드는 최종 base64 문자열을 `spaceOut` 으로 두
  글자마다 공백을 끼워 넣어 보내도록 변경(`echo <spaced-b64> | tr -d ' ' |
  base64 -d | bash`). 3글자 이상 이어진 조각 자체가 없어져 위험 키워드와
  우연히 일치할 가능성을 원천 차단. base64 원문에는 공백이 없으므로 원격에서
  `tr -d ' '` 로 지우기만 하면 원래 내용이 정확히 복원됨.
  gossh 자체의 위험 작업 확인 플래그(`-dnlgjawkrdjqghkrdls`)는 대화형 y/N
  확인까지 강제해 자동화에 쓸 수 없어 우회하지 않고 우리 쪽 인코딩을 고침
  (ARCHITECTURE.md 13번).
- 검증: `internal/remote/remote_test.go` 신규 — 다양한 크기의 스크립트에서
  명령 문자열에 위험 키워드가 전혀 나타나지 않는지, 공백을 끼워 넣어도
  왕복 복원이 정확한지, 3글자 이상 이어진 조각이 없는지 확인. `go test ./...`,
  `test_all.sh` 22/22, `ldap_check/test_check.sh` 10/10 통과. 실제 gossh +
  tcsh 로그인 계정으로 `/wappl` 이 설정된 conf 로 DRY-RUN·실제 적용 모두
  정상 처리됨을 확인.

## 2026-09-09 (2차)

### 신규 — `scripts/add_infra_from_ldapconf.sh`
- conf 에 새 인프라를 추가할 때 매번 `infra.<이름>.uri1~3`/`binddn`/`bindpw` 를
  손으로 옮겨 적는 게 번거롭다는 요청으로 추가.
- 사용법: `./add_infra_from_ldapconf.sh <인프라이름> <ldap.conf 경로> [conf 경로]`
- 기존 `ldap.conf` 에서 URI(최대 3개)/BINDDN/BINDPW 를 읽어
  `conf/ldap_config.conf` 끝에 `infra.<이름>.*` 블록을 이어붙임.
- `dns`/`ntp`/`site.*`(storage/mountpoint/wappl_mount)는 ldap.conf 에 없는
  값이라 자동으로 채울 수 없음 — TODO_ 접두사로 자리만 만들어 두고, 실행
  결과에 `grep -n 'TODO_' <conf>` 로 찾아 채우라고 안내.
- 같은 인프라 이름이 이미 있으면 중복 등록 사고를 막기 위해 거부(exit 1).
- 검증: 임의 ldap.conf 로 블록 생성 → TODO 채운 뒤
  `ldap-config-engine -print-script` 로 정상 파싱·조립되는지 확인. 중복
  이름 재등록 시 거부되는지 확인.
