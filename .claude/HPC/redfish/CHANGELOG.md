# CHANGELOG.md — redfish (biostool)

## [Unreleased] (v0.3.1 후보) - 2026-10-16

`bash bios.sh` 한 번에 Redfish 장비와 Cisco 장비(UCSM 블레이드·CIMC 랙서버)를 섞어 점검합니다. Cisco 는 XML API(읽기 전용)로 읽습니다. 아래 v0.3.0 항목과 sha256 표는 그대로 두었습니다(이 변경분의 바이너리 sha256 은 이 절 끝 표).

### 추가

- **점검 끝 계정 생성 확대**(`account_check.go` `manualCreate`·`probeUser2`, `account.go` `acctEnv.admin`, `account_input.go` `readAdminPassword`):
  - **AUTH_FAIL 호스트**(1순위 거부 + 벤더에 맞는 `rf.conf` 계정도 거부·없음·벤더 미식별): 목록(hostname·IP·벤더·사유)을 보인 뒤 관리자 ID 와 비밀번호(숨김 입력, 붙여넣기 거부, 저장·출력 안 함)를 직접 입력받아, 호스트마다 로그인 1번으로 1순위 계정·user2 중 **없는 것만** 만듭니다(있는 것은 건너뜀). 그래도 실패한 호스트는 다시 나열하고 다른 ID 를 넣거나 빈 입력으로 끝냅니다(최대 3번). 다른 벤더의 `rf.conf` 계정은 시도하지 않습니다(잠금 방지). 웜업 차단(처음 10대 순차, 절반 이상 실패면 중단) 그대로.
  - **1순위로 로그인되지만 user2 가 없는 호스트**(bios.conf 에 user2 가 있을 때): 점검 세션에서 계정 목록을 읽어(읽기 전용 GET, 호스트당 계정 수+2건, 최대 64) 없으면 1순위 계정으로 로그인해 user2 만 만듭니다. 목록 `account_user2_targets.txt`. `-auto`/`-auto-check`·재점검에서는 확인하지 않습니다.
  - 2순위 대상과 user2 대상은 한 번의 [Y/n] 으로 묻고, 수동 입력 단계는 그다음입니다. 1순위 계정을 만든 호스트(수동 입력으로 만든 호스트 포함)만 자동 재점검. `-dry-run` 은 계획만(수동 단계 없음), `-no-prompt`·터미널 아님은 목록만.
- **수정 단계 “BIOS 값 직접 입력”**(`setdirect.go`, `tui_flow.go` `editStep`·`directStep`): 결과 보기에서 `e` 를 누르면 “점검 결과 기준 수정”(기존) / “BIOS 값 직접 입력” 을 고릅니다. 직접 입력은 프로파일과 관계없이 `<Redfish 속성 이름><공백 또는 탭><설정값>` 을 한 줄에 하나씩 붙여 넣고 Ctrl+D 로 끝내면(빈 줄·`#` 줄 무시, 속성 이름 대소문자 무시) 이번에 점검한 Redfish 호스트 전부에 대해 항목별로 판단합니다.
  - 건너뜀(그 항목만, 남은 항목이 없으면 그 호스트만): 보호 속성(`protect.go` + `protect_attrs`, 항상), 그 호스트에 없는 속성(“미지원 속성”), 제외 목록, 현재값 형식과 맞지 않는 값, **이미 같은 값**. Cisco XML 호스트·점검 오류 호스트는 통째로 건너뜀. 나머지는 진행합니다.
  - “직접 입력 미리보기”(`현재값 -> 변경값 (수정대상)` / `(건너뜀: 사유)`) 뒤 “위 수정대상을 적용할까요?”, 남의 Pending 질문은 기존과 같습니다. 적용은 기존 설정 경로(`applySetPlan` → `runHost` → `send`, Dell Job·중복 BMC·AUTH_FAIL 중단 포함)를 그대로 탑니다. 레지스트리의 허용값·ReadOnly·정수 범위는 적용 때 다시 확인해 맞지 않는 항목만 `INVALID_VALUE`/`SKIPPED_READONLY` 로 빼고 나머지는 보냅니다(값은 레지스트리 표기로 보냄). Pending 까지만, 재부팅 없음.
  - 적용 뒤 자동 재점검 후 “직접 입력 확인” 화면에 호스트별로 입력한 속성의 현재값·Pending 을 보입니다.
- **설정 뒤 자동 재점검**(`tui_flow.go` `recheckHosts`, `setOutcome.Tried`): 수정 단계에서 설정(PATCH)을 보낸 호스트만 묻지 않고 다시 점검해 프로파일 점검 화면에 바꿔 끼웁니다(보통 `PENDING_OK`). 결과는 `results/<일시>/recheck/<일시>/` 와 합친 CSV `profile_merged_*`. 전체 설정 비교 결과는 비우고 다음 Tab 에 다시 비교합니다. 재점검이 끝나면 같은 세션에서 다시 `e` 로 수정할 수 있습니다(재점검하지 못한 호스트가 있으면 그때만 막음). 처음 결과 폴더의 `result.tsv`·`retry.txt` 는 설정 전 기록으로 그대로 둡니다.
- **Cisco XML 혼합 대상**(`xmlhost.go`, `resolve.go` `classifyTargets`): hostname(`-m`·포트를 뗀 이름)이 `bios.conf` 의 `xml_host_prefix`(기본 `c`, 쉼표로 여러 개, 대소문자 무시, 비우면 이름 기준 XML 끔)로 시작하는 대상은 Redfish 대신 UCSM/CIMC XML API 로 점검합니다(`aaaLogin`·`configResolveClass`·`aaaLogout` 만, 읽기 전용). 접속 주소는 `hostname-m`. IP 대상은 `-user` 면 Redfish, **`-user-xml <파일>`** 이면 Cisco(XML). `bios.sh` 도움말·`-auto` 도 `-user-xml` 을 받습니다.
- **TUI**: 대상에 IP 가 있으면 Cisco(XML) 대상 파일/붙여넣기를 따로 묻습니다(`targets-xml-file`·`targets-xml-paste` 단계, 안내 “Cisco 장비는 XML 로 점검”). hostname 만 있으면 접두어로 자동 분류되므로 묻지 않고, `xml_host_prefix` 가 비면 hostname 도 묻습니다.
- **서버 선택**: XML 응답에 서버가 1대면 그 호스트, 여러 대면 서비스 프로파일(`lsServer`) 이름이 hostname 과 같은 서버를 고릅니다. 같은 것이 없으면 모든 서버를 `<호스트>/<서버 dn>` 으로 나열하고 비고에 `[XML SP=...]` 를 남깁니다.
- **폴백**: XML 로그인 전에 실패하면(연결 실패·타임아웃·XML 아닌 응답·HTTP 오류) 그 호스트만 Redfish 로 1번 다시 점검하고 비고에 `XML 실패 → Redfish` 를 남깁니다. `AUTH_FAIL` 은 계정 잠금 방지를 위해 다시 시도하지 않습니다.
- **XML 호스트는 점검만**: 전체비교는 `[N/A]` + 비고 `XML 대상 — 비교 제외(읽기 전용)`(내부 상태 `XML_READONLY`, 화면에서 회색), 수정(설정) 계획에서도 제외합니다.
- **mock**: `cmd/mockucsm` + `internal/mockucsm`(UCSM XML API mock, `aaaLogin`·`configResolveClass`·`aaaLogout` 만 응답). `testcase/demo_env.sh` 에 c101(XML 단일)·c201/c202(도메인, 서비스 프로파일 이름으로 선택)·c301(XML 실패 → Redfish)과 `user_xml.txt` 추가. 빌드 완료 `bin/mockucsm`(`_os6`) 커밋.
- **`trs.sh` / `biostool trs`**(`trs.go`): BMC 접속 장애(403 등) 트러블슈팅. `user.txt` 의 관리망 IP(또는 `-targets`)로 TCP/TLS → 비인증 ServiceRoot(403 이면 User-Agent·Accept·슬래시 변형 비교) → 인증(Basic 1회 + 세션 생성 1회, 401 이면 잠금 방지로 중단, 필요 시 rf.conf 2순위 계정 1회 비교) → 인증 후 읽기(Systems·Bios·Managers·NetworkProtocol·내 계정 상태) → 403 경로의 헤더·인증 방식 변형 비교를 하고 원인 추정(`trsDiagnose`)을 출력합니다. 단계별 상태 코드·응답 헤더·Redfish 오류 MessageId 를 그대로 싣고 `results/<일시>_trs/<호스트>.txt` 로 저장합니다(비밀번호·토큰 없음). 읽기 전용(GET/HEAD/OPTIONS + 이번 실행의 세션 생성·삭제). **bios.sh 방식 재현**: 마지막 단계에서 실제 Client 로 bios.sh 와 같은 조회(ServiceRoot `/redfish/v1` → Systems → System → Bios → Settings → 속성 레지스트리)를 해 보고, 실패하면 기본 User-Agent·연결 재사용·끝 슬래시 없는 경로(리다이렉트)·8MB 초과 여부로 원인을 가립니다(trs 는 정상인데 bios.sh 만 HTTP_ERROR 인 경우용). **4xx 전반 대응**: 비인증 ServiceRoot·인증 후 읽기에서 나온 모든 4xx 를 코드별(400~451) 설명·점검 방법과 해당 경로로 정리하고, 4xx 가 난 경로(405 는 OPTIONS Allow 포함)와 세션 생성이 400/405/406/411/415 등일 때는 Accept·OData-Version·Content-Type·User-Agent·슬래시를 바꿔 어떤 모양이 통과하는지 비교합니다(세션 생성 변형이 성공하면 그 세션을 이어 쓰고 끝에 삭제).
- **`bios.sh` 기능별 명령**: `bash bios.sh <명령> [옵션...]` 으로 지금까지의 기능을 한 진입점에서 실행합니다 — `account`(계정 생성 = `account_create.sh`), `passwd`(1순위 비밀번호 변경 = `biostool account change`), `trs`, `dump`(`xml.sh`), `export`(`bios_export.sh`), `ucsm`(`ucsm_check.sh`), `check`(옛 점검: `-retry-from`·`-dry-run`), `allcheck`, `encrypt`, `update`. 각 래퍼를 그대로 실행하므로 옵션·경로 규칙은 기존과 같습니다(`bash bios.sh <명령> -h`). 명령 없이 실행하면 이전과 같은 TUI/평문 점검입니다.
- **`bios.sh` 점검 끝 계정 생성 질문**(`tui_flow.go` `runAll`, `unified_cmd.go`): `bios_check.sh`(옛 check)에만 있던 기능을 `bios.sh`(TUI·`-plain`)에도 넣었습니다. 1순위 계정(`bios.conf` user)이 거부되어 2순위(`rf.conf`) 계정으로 점검한 호스트가 있으면 TUI 를 닫은 뒤(평문은 결과 뒤) "계정을 만들까요? [Y/n]" 을 묻습니다(빈 입력 = Y, 터미널 입력일 때만, 점검 중 눌러 둔 Enter 는 버림). `-auto`/`-auto-check` 는 묻지 않습니다.
- **두 번째 공용 계정**(`bios.conf` `user2`·`user2_role`·`pass2_file`): 권한이 다른 공용 계정을 하나 더 만들 수 있습니다. `user2_role` 은 `Administrator`/`Operator`/`ReadOnly`(user2 를 쓰면 필수, user 와 같은 id 불가), 비밀번호는 **`bash encrypt.sh 2`** 로 `pass2.enc`(같은 `key.bin`) 에 저장합니다. 점검 접속에는 쓰지 않고 계정 생성(`bash bios.sh account`, 점검 끝 질문)에서만 씁니다.
  - 점검 단계는 두 번째 계정으로 로그인해 보지 않고(인증 실패 기록 방지) 계정 목록에서 이름만 확인합니다: 있으면 `PRESENT`, 없으면 생성 대상. 1순위 계정이 있는 호스트도 두 번째 계정이 없으면 대상입니다.
  - 생성은 호스트마다 2순위(admin) 로그인 1회로 두 계정을 차례로 만들고(빈 슬롯은 계정마다 다시 읽어 서로 다른 슬롯), 각각 새 계정으로 로그인해 검증하고 실패한 계정만 롤백합니다.
  - 권한별 RoleId 후보: Operator → `Operator`/`operator`/`user`(Cisco CIMC), ReadOnly → `ReadOnly`/`readonly`/`read-only`. BMC 의 Roles 에서 맞는 이름을 먼저 고릅니다. HPE iLO4(Oem 권한)는 Operator = 로그인·원격 콘솔·가상 미디어·전원, ReadOnly = 로그인만. UCSM 은 `admin`/`operations`/`read-only`.
  - 안전장치(`account_gate.go`, `ucsmxml` gate): 허용 RoleId 를 위 후보로 넓혔습니다(관리자보다 낮은 표준 역할만 추가, 사용자 정의 역할·그 밖의 값은 계속 거부). UCSM 생성 역할도 `admin`·`operations`·`read-only` 만.
  - `account_result.tsv` 맨 끝에 `account` 열(작업한 계정 id) 추가 — 기존 열 순서(result 는 7번째)는 그대로. 요약에서 두 번째 계정은 `호스트(계정)` 로 표시. `.gitignore`·`update.sh` KEEP 에 `pass2.enc` 추가.
- **`update.sh`**(설치 폴더 갱신): `bash update.sh [ZIP|폴더]` 로 `/tmp` 의 가장 최근 redfish ZIP(`lib.sh`+`bios_check.sh` 가 든 것만 고르고 다른 ZIP 은 건너뜀. 또는 지정한 ZIP·압축 푼 폴더)을 풀어 고정 설치 폴더 `/root/server_vm_python/redhfish/V2` 를 갱신합니다. `bios.conf`·`pass.enc`·`key.bin`·`user.txt`·`diff.txt`·`ignore_attrs.txt`·`refs/`·`results/`·`dumps/` 는 그대로 두고 나머지 항목만 바꿉니다. 바꾸기 전 `results/`·`dumps/` 를 뺀 전체를 `../backup/V2_<일시>.tgz` 로 백업합니다. `profiles/` 는 파일별로, 현장에서 고치지 않은 파일만 새 버전으로 바꾸고 고친 파일(`verified=Y` 등)은 두고 새 버전을 `<이름>.new` 로 둡니다(판단 기준 `.update_profiles.sha`, 첫 업데이트는 새 버전과 다르면 모두 고친 것으로 봄). 새 버전에 없는 기존 항목은 지우지 않고 알려 주고, `bios.conf` 에 없는 새 설정 키도 알려 줍니다.

### 변경 (사용성)

- **2순위 계정(`rf.conf`) 비밀번호 암호화**(`rfenc.go`, `fallback.go` `loadRFConfEnc`, `encrypt.sh`): `rf.conf` 에는 `<vendor>_user` 만 두고 비밀번호는 `bash encrypt.sh rf [벤더]`(또는 `bash bios.sh encrypt rf [벤더]`)로 `rf.enc`(bios.conf `rf_enc_file`, 키는 `pass.enc` 와 같은 `key.bin`, 0600)에 벤더별로 암호화해 저장합니다. 한 벤더만 갱신해도 나머지 벤더 줄은 그대로. 평문 `<vendor>_pass` 줄이 남아 있으면 키 이름만 밝히는 경고 후 계속 동작(과도기), 같은 벤더가 `rf.enc` 에도 있으면 `rf.enc` 우선. `rf.enc` 에 항목이 있는데 키가 없거나 다르면 덮어쓰지 않고 오류. `update.sh` 유지 목록·`.gitignore` 에 `rf.enc` 추가.
- **점검 끝 계정 생성: 대상 목록과 자동 재점검**(`account_check.go`): 점검 끝에 `account_targets.txt`(생성 대상)·`account_unfixable.txt`(1순위 인증 실패 + `rf.conf` 계정도 없거나 거부 = 생성 불가)를 결과 폴더에 씁니다(0600, 호스트·IP 만. `-user` 로 다시 쓸 수 있는 형식). [Y/n] 질문은 그대로이고, Y 로 1순위 계정을 새로 만든 호스트만 1순위 계정으로 다시 점검해 원래 결과에 바꿔 끼운 합친 결과 CSV 를 `recheck/` 에 씁니다. `-dry-run` 은 쓰기·재점검 없음.

- **결과 줄의 비고 색**(`lineout.go` `formatLine`, `tui_render.go` `tuiLineRow`): `bios.sh` 결과(TUI·`-plain`)의 비고 칸을 OK 줄은 초록, 그 밖(difference·N/A)은 노랑으로 보입니다. 호스트 오류 줄의 비고는 전과 같이 빨강. 색이 꺼지는 경우(파이프·리다이렉트·`NO_COLOR`)와 CSV·파일은 그대로.

- **전체비교 결과 CSV 의 `wide_hosts` “기준” 행에 기준 호스트 표시**(`csvout.go` `refLabel`, `resultconv.go`, `ResultLine.RefHost`·`RefIP`): 기준 행의 Hostname·IP 칸이 비어 있어 어떤 서버를 기준으로 비교했는지 알 수 없던 문제. 이제 비교에 쓰인 기준 호스트의 hostname·IP 가 들어가고, 한 그룹에 기준이 둘 이상이면 `기준1 / 기준2` 처럼 잇습니다. 기준 JSON(`-ref-json`)이면 JSON 의 host·ip(없으면 파일 이름)이고, 프로파일 점검 결과는 기준 호스트가 없어 전과 같이 빈 칸입니다. 열 순서·개수는 그대로.

- **비교서버 입력 규칙 통일**: 전체비교 비교서버를 TUI 에서 입력 줄 하나(쉼표)가 아니라 대상 입력과 같은 여러 줄 입력(한 줄에 하나 또는 공백 구분, Ctrl+D 완료, 바로 Ctrl+D 면 diff.txt)으로 받습니다. `-ref` 도 쉼표·공백·줄바꿈으로 여러 개. 이 입력 화면에서도 Tab 으로 프로파일 화면으로 돌아갑니다.
- **종료(q 두 번) 때 수정 여부를 묻지 않음**: 수정 단계는 `e` 로만 갑니다. q 두 번은 설정 여부와 관계없이 그대로 끝납니다.

### 변경 (옛 방식도 같은 분류)

- **`bios_check.sh`(옛 옵션 `-no-prompt`·`-dry-run`·`-retry-from` 등 → `biostool check`)·`all_bios_check.sh`(`biostool allcheck`)도 `bios.sh` 와 같이 대상을 Redfish / Cisco XML 로 알아서 나눕니다**(`addXMLRouting`, main.go). hostname 이 `xml_host_prefix`(기본 `c`)로 시작하는 대상과 새 옵션 **`-user-xml <파일>`** 목록이 XML 입니다(`-user-xml` 만 주면 user.txt 는 읽지 않음).
  - `check`: XML 대상은 XML API 로 점검만 합니다. FAIL 은 `[FAIL 상세]` 가 아니라 `[설정 불가]` 에 “XML API 대상 — 점검만, 설정 안 함” 으로 나오고(UNSUPPORTED 항목도), Y/N 설정·`-dry-run` 대상에서 빠집니다. XML 로그인 전 실패면 Redfish 로 1번 폴백(bios 와 같음).
  - `allcheck`: XML 대상은 `xmlProbe`(aaaLogin·aaaLogout 만)로 XML 장비인지 확인해 `XML_READONLY`(리포트 “XML 대상 제외”, 오류 아님)로 비교에서 빼고, 로그인 전에 실패하면 Redfish 로 비교합니다. AUTH_FAIL 은 다시 시도하지 않습니다.

### 고침 (실행 로직 점검)

- **BIOS 를 가진 System 선택**(`inspect.go` `pickSystem`, `allcheck.go`): Dell XE9680 은 Systems 의 첫 멤버가 GPU 베이스보드(`HGX_Baseboard_0`, Manufacturer=NVIDIA, Bios 없음)라서 벤더가 NVIDIA 로 분류되고 비고가 UNSUPPORTED(“System 에 Bios 리소스가 없습니다”)였습니다. 첫 멤버에 Bios 링크가 있으면 그대로(추가 GET 없음), 없으면 나머지(최대 8개)에서 Bios 링크가 있는 첫 System 을 씁니다. 점검·전체비교·시스템 판별 공통.

- **같은 BMC 안의 리다이렉트 따라가기**(`redfish.go` `Client.do`): Lenovo XCC 등이 슬래시 없는 `/redfish/v1` 에 308 을 주면 도구가 따라가지 않아 모든 조회가 HTTP_ERROR 가 되던 문제. GET 에 한해 같은 호스트(상대 경로·같은 host:port 의 https)로의 리다이렉트를 최대 2번 따라갑니다. 다른 호스트·http 로 가는 것과 허용목록 밖(로그/Actions)으로 가는 것은 따르지 않고 3xx 그대로 HTTP_ERROR 입니다. 토큰은 같은 호스트로만 전송됩니다.

- **`bios.sh` 상대경로**: 다른 폴더에서 `bash /경로/bios.sh -user list.txt` 처럼 부르면 스크립트 폴더로 이동한 뒤 경로를 찾아 파일을 못 찾던 문제. 파일 옵션(`-conf`·`-user`·`-user-xml`·`-hosts`·`-ref-json`·`-diff`·`-ignore`·`-from-dump`·`-rf-conf`)의 상대경로는 호출한 폴더에 그 파일이 있으면 호출한 폴더 기준 절대경로로 바꿔 넘깁니다(없으면 예전처럼 스크립트 폴더 기준, `-ref-json` 파일 이름만은 refs_dir).
- **옛 래퍼 상대경로**: `bios_check.sh`·`all_bios_check.sh` 도 `bios.sh` 와 같은 상대경로 규칙(`lib.sh` `fix_path_args`, `-retry-from` 폴더 포함)을 씁니다.
- **TUI 제목**: 수정 미리보기·설정 결과에서 결과 보기로 돌아와도 제목이 “수정 미리보기”로 남고 스크롤이 처음으로 가던 문제 (`SwitchTab` 이 결과 보기 제목·스크롤을 되돌림).
- **TUI 전체비교의 XML 대상 줄**: 벤더·모델이 `-` 라 벤더 탭이 `[-]`, CSV 이름이 `compare_wide_hosts_-_…` 이던 문제 → 점검에서 읽은 벤더(Cisco)·모델로 표시.
- **TUI 글 보기의 e**: 미리보기·설정 결과 화면 안내에 “e 수정 메뉴” 가 보였지만 실제로는 결과 보기로 돌아가기만 하던 문제 → 글 보기에서는 e 를 받지 않고 안내에서도 뺌 (수정 단계는 결과 보기에서 e).
- **`trs_cisco.sh` / `biostool trs-cisco`**(`trs_cisco.go`, `bios.sh trs-cisco`): Cisco 장비(UCSX-210C-M7 등)가 vendor·모델·BIOS 버전은 나오는데 attr 이 비고 `UNSUPPORTED [XML 실패 → Redfish]` 로 나올 때의 원인 진단. 이 비고는 bios.sh 가 Cisco 를 XML API(UCSM/CIMC)로 먼저 읽다가 로그인 전에 실패해 Redfish 로 넘어갔고, Redfish System 에 Bios 리소스가 없다는 뜻입니다. 진단은 ① bios.sh 의 XML/Redfish 분류 ② 네트워크·인증(trs 와 같음) ③ Cisco Redfish 구조(System 제조사·모델·SKU·Bios 링크 유무·Oem 항목·링크 목록, Bios 후보 경로, Chassis·Manager(CIMC 버전), Registries·펌웨어 목록의 BIOS 항목, System 아래·Oem 하위 리소스를 최대 60건·깊이 3 으로 따라가며 BIOS 속성 찾기) ④ bios.sh 방식 재현 ⑤ XML API(`GET /` → 쿠키 없는 확인 요청 1회 → XML API 가 있을 때만 aaaLogin 1회 + 서버·서비스 프로파일·프로파일 클래스 조회)를 보고 `ciscoDiagnose` 가 원인 후보를 종합합니다. XML API 가 없는 주소에는 비밀번호를 보내지 않고, Redfish 에서 401 이면 XML 로그인도 하지 않습니다. 독립 배포본은 `trs-standalone` 브랜치 `cisco/` 폴더. `trs.go` 는 `trsMain`·`newTrsHost` 로 나눠 재사용합니다(동작 변화 없음). 실장비 미확인(mock 시나리오 4종 시험).
- **TUI 렉(전체 설정 비교, 대상 많을 때)**: 키를 누를 때마다(조회 중에는 0.2초마다) 표 전체 줄을 다시 만들어, 50대 × 1,500 속성(7.5만 줄)이면 키 1번에 약 0.28초·115MB 할당 → 누른 키가 밀려 쌓이고 응답이 없다가 쌓인 q 로 끝나는 것처럼 보이던 문제. 행·건수·칸 너비는 벤더·그룹·필터·화면 폭이 바뀔 때만 만들고(`tuiTableCache`), 그릴 때는 보이는 줄만 만듭니다 → 키 1번 약 0.2ms·57KB. mock 50대 시험: 키 663개 처리가 예전 190초 넘게(타임아웃) → 바뀐 뒤 1초 안. 화면 내용·키 동작은 그대로입니다.
- **TUI 화면 전송량(느린 ssh)**: 바뀐 것이 없는 화면은 다시 보내지 않고(조회 중 0.2초마다 같은 화면을 보내던 것), 방향키·PgUp/PgDn 을 누르고 있어 쌓인 스크롤 키는 모아 처리한 뒤 한 번만 그립니다(스크롤이 아닌 키는 예전처럼 키마다 그림). mock 50대 같은 키 입력에서 터미널로 보낸 양 1.86MB → 37KB.
- **평문 출력(`-plain`·파이프·리다이렉트)**: 결과 줄을 한 줄마다 write 하던 것(`writeResultLines`)과 `report.txt` 쓰기를 버퍼로 모음. mock 50대 `-plain | cat` 에서 write 호출 79,701 → 3,843 (strace 아래 15.4초 → 6.6초).

### 변경

- **프로파일 `profiles/*.tsv` 에 구역 `# [redfish]` / `# [xml]` 추가**(`profile.go`): `[xml]` 행의 attribute 는 `클래스.속성`(예: `biosVfCPUPerformance.vpCPUPerformance`), 벤더는 CISCO 만, `*` 모델 불가. `# UNMAPPED` 주석은 `MAPPING_MISSING` 사유. 기존 `bios_json/standard_map/standard_bios_map_xml.tsv` 는 삭제하고 그 행을 `profiles/VM.tsv`·`VM_research.tsv` 의 `# [xml]` 구역으로 옮겼습니다.
- **`ucsm_check.sh` / `biostool ucsm`**: 기대값을 `-profile <이름>`(기본 VM)의 `# [xml]` 구역에서 읽습니다. **`-xml-map` 옵션은 제거**되었습니다. 조회 클래스에 `computeRackUnit`·`lsServer` 가 더해졌습니다(읽기 전용 허용목록 안).
- 문서: README 2.0절 “Cisco(XML) 혼합 대상”·3.3절(`xml_host_prefix`)·3.4절(구역 형식)·3.6절, ARCHITECTURE(`xmlhost.go`·mockucsm), WORKFLOW 7번 mermaid(분류 → XML/Redfish → 폴백), 사용법.txt 3·13절, FIRST_RUN 38~41. `workflow_bios.svg` 다시 렌더링.

### 알려진 제한

- 실제 UCSM/CIMC 에 접속해 본 적이 없습니다(mock 만). 확인 항목은 [FIRST_RUN.md](FIRST_RUN.md) 38~41(와 18~21).

### 바이너리 sha256

| 파일 | Go | 크기 | sha256 |
|---|---|---|---|
| bin/biostool | go1.26.5 | 8237218 | 5f6785c100e4286c748e3a93f8aa7b2e5474b2530e0e943664f7a14238fdec02 |
| bin/biostool_os6 | go1.20.14 | 6840320 | 98b76c9cb32a5fb2833cccc631ecd115b004436ed11497a5445bee93be367976 |
| bin/mockucsm | go1.26.5 | 6631586 | 58b4b98814a8afb35e9252abe61a62496fdc0053ee5daf1546acc8509146db6e |
| bin/mockucsm_os6 | go1.20.14 | 5197824 | 4c81551e5cfeba14da930d8538d34ad199a80f81629ef078f9bdae5d4b90e55d |

`bin/mockbmc`(`_os6`)는 소스 변경이 없어 그대로입니다(재빌드 sha 동일). 검증: 랩 go1.26.5·go1.20.14 `gofmt`·`go vet`·`go test ./...` 통과, `demo_env.sh` 혼합 실행(c101 XML FAIL 1건, c201/c202 서비스 프로파일 선택, c301 XML 실패 → Redfish, `-user-xml`) 확인.
계정(user2·bios.sh 질문) 변경분 검증: `account_user2_test.go`(두 계정 생성·Dell 슬롯 2개·목록 점검·점검 끝 제안·RoleId 후보), `testcase/rf_fallback_case.sh` 11개 PASS(일반·`RF_FORCE_OS6=1`), mock 으로 `bash bios.sh -plain` → 2순위 점검 → 질문에 Enter → `CREATED` 2건(biosadmin·ops1), 비밀번호 출력 0건, 재실행 시 질문 없음. 실장비 미확인.
TUI 렉·출력 변경분 검증: `tui_bench_test.go`(`go test -bench TUI ./cmd/biostool`), `TestTUISkipsSameFrame`, go1.26.5·go1.20.14 `go test ./...` 통과, mock 50대(속성 1,518개) `bash bios.sh … -ref` → Tab → 키 663개 → qq 로 예전·새 바이너리 비교.

## [Unreleased] (v0.3.0 후보) - 2026-10-15

`bash bios.sh` 하나로 점검·전체비교·수정을 터미널 화면(TUI)에서 진행하는 통합 진입점을 추가했습니다. 확정 사양은 [계획서_v0.2_TUI.md](계획서_v0.2_TUI.md), 사용법은 README 2.0절, 흐름도는 [WORKFLOW.md](WORKFLOW.md) 7·8번입니다. **실제 BMC 에 접속해 본 적은 없습니다**(mock·`-from-dump` 로만 검증).

### 추가

- **통합 진입점 `bios.sh` + `biostool bios`**: 인자 없이 터미널에서 실행하면 TUI(대상 입력 → 프로파일 선택 → 접속·조회 → 프로파일 결과 → (Tab) 기준 선택 → 전체 설정 비교 → 수정 확인 “위 수정대상을 적용할까요?” → 요약). `-profile`·`-user`·`-ref`·`-ref-json`·`-diff`·`-ignore`·`-from-dump`·`-plain` 옵션. TUI 키: Tab 화면 전환 · ←→ 벤더(Enter 로 관리망 그룹) · ↑↓(k j) · PgUp/PgDn · a/o/f/n 필터 · s 요약 · e 수정 · ? 도움말 · q 한 단계 뒤로(벤더 줄에서 q 두 번이면 종료) · **Ctrl+C/Ctrl+Z 무시**(“종료는 q 를 두 번 누르세요”) · **Ctrl+X 진행 중 작업 취소**(접속·조회, 기준 조회, 설정 적용). 구현 `unified_cmd.go`·`tui_flow.go`·`tui_view.go`·`tui_render.go`·`tui_keys.go`·`tui_tty_linux.go`(raw 터미널은 `syscall` 만, 리눅스 외는 `tui_tty_other.go` 가 평문으로 대체).
- **1회 수집(`unified.go`)**: 호스트당 로그인 1회로 BIOS 전체 속성을 읽어 프로파일 판정과 전체비교에 함께 사용합니다(전체비교 때 대상에 다시 접속하지 않음). 기존 결과 파일(`result.tsv` 등)은 그대로 기록합니다.
- **전체비교 기준 3종**: 비교서버 hostname(대상 목록에 없으면 그 서버만 추가 조회, `-ref`), `refs/` 폴더의 JSON(`bios_export.sh` 결과, `-ref-json`·TUI 목록), `diff.txt`(모델별 기준, `-diff`). 같은 모델끼리만 비교하고 BIOS 버전이 달라도 속성 이름 목록이 같으면 허용합니다.
- **색상 줄 출력 + 태그 3종**: `[태그] host↹ip↹vendor↹model↹bios↹attribute↹기준값↹현재값↹비고`. 태그 `[OK]`(초록) / `[difference]`(노랑) / `[N/A]`(회색), 호스트 단위 오류는 빨강, 기준값 칸은 항상 초록, 비고에 원래 상태명. **태그는 상태를 묶은 표시이므로 `[N/A]` 는 “문제 없음”이 아니라 “판정할 수 없음”**(MAPPING_MISSING·NO_DUMP·대상에 속성 없음·접속 실패)이고, 값 없음(`-`)은 `(N/A)` 로 표시합니다(`profiles/*.tsv` 양식은 그대로). `NO_COLOR`·터미널이 아니면 색 없음. 구현 `resultline.go`·`resultconv.go`·`lineout.go`.
- **CSV 4종**(결과 폴더, UTF-8 BOM, 0600; `profile_`/`compare_` 접두어): `summary.csv`(호스트 1줄·불일치 건수·“대상값 / 기준값”), `wide_hosts_<벤더>_<그룹>.csv`, `wide_attrs_<벤더>_<그룹>.csv`, `long.csv`. 그룹은 벤더 안에서 관리 컨트롤러(관리 컨트롤러 모델)별로 나누되 **BIOS 속성 이름 목록이 같으면 하나로 합칩니다**. 구현 `csvout.go`.
- **conf 키 4개**(`bios.conf.example` 갱신): `exclude_attrs`(점검·전체비교 제외 패턴, `ignore_attrs.txt` 와 합침, 프로파일 점검에서도 그 행 제외), `allcheck_set`(전체비교 difference 를 수정 대상에 넣을지, 기본 off), `refs_dir`(기준 JSON 폴더, 기본 `refs`), `auto_profile`(비대화형 기본 프로파일).
- **수정(설정) 단계**(`setplan.go`): 프로파일 difference(**`verified=Y` 행만**)와 `allcheck_set=on` 일 때만 전체비교 difference 를 함께 설정, 같은 속성의 목표값이 다르면 설정하지 않고 `[충돌]`, 제외 목록·위험 속성·N/A·쓰기 불가 벤더 제외, 남의 Pending 이 있는 장비는 목록을 보여 주고 y 포함 / n 제외 / Ctrl+X 취소, **Pending 까지만 설정(재부팅 없음)** 후 Pending 재조회로 반영 여부 표시. 실제 쓰기는 기존 `set.go` 의 안전 경로를 그대로 탑니다.
- **비대화형 `-auto` / `-auto-check <user> <targets>`**(auto_setup 연동): 묻지 않고 프로파일 점검만 하며 **설정은 하지 않습니다**(`-auto` 도 점검만). 결과 `check.res_<user>`(`-auto` 는 `check.res_<user>_postapply` 도)를 호출한 폴더에 만들며(`-res-dir`, `bios.sh` 가 호출 폴더를 넘김) 호스트마다 한 줄(`host: OK` / `host: FAIL difference N건 (…)` / `host: FAIL N/A 접속실패(…)`). 점검을 끝냈으면 FAIL 이 있어도 종료코드 0. 구현 `checkres.go`.
- **진행 표시**(`progress.go`): `[1/4 설정 확인] [2/4 접속·조회 n/N · OK … difference … N/A … 오류 … · 경과 mm:ss] [3/4 CSV 저장] [4/4 TUI]`. 터미널은 제자리 갱신, 비대화형은 10초마다 한 줄, 설정 때는 호스트별 진행 줄.
- **문서**: README 2.0절·표(서브커맨드·플래그·래퍼·conf 키·결과 파일·태그), 사용법.txt 3절, ARCHITECTURE(새 파일·수정 가이드), WORKFLOW 7·8번과 `workflow_bios.svg`·`workflow_auto.svg`, PR_CHECKLIST(TUI·`-auto` 항목).

### 변경

- **`bios_check.sh`·`all_bios_check.sh` 는 호환 래퍼로 남습니다.** `bios_check.sh` 는 옛 전용 옵션(`-retry-from`·`-dry-run`·`-no-prompt`·`-stdin-ok`·`-list-max`·`-fail-max`)이 없고 프로파일을 줬거나 터미널이면 `bios.sh` 로 넘기고, 옵션이 있으면 옛 방식(Y/N 줄 프롬프트)으로 동작합니다. `all_bios_check.sh` 는 인자 없이 터미널에서 실행하면 `bios.sh` TUI 로 넘어가며 **옛 대화형 1.체크/2.전체비교 메뉴는 `-diff-max`·`-list-max`·`-export-attrs` 를 줄 때만** 나옵니다. `-user`·`-diff`·`-ref`·`-from-dump`·`-retry-from` 방식은 그대로입니다.
- 터미널에서 `bash bios_check.sh --profile VM` 은 이제 TUI 로 점검하고 마지막에 수정 여부를 묻습니다(`Y/N` 줄 프롬프트가 아님).
- README 의 주의사항 문구를 “본 스크립트 및 툴은 100% 신뢰하기보다는 참고용(보조 도구)…” 로 맞췄습니다.
- **TUI 이동을 단계식으로**: 벤더 줄에서 ←→ 로 벤더를 고르고 `Enter` 로 들어가면 ←→ 가 관리망(관리 컨트롤러) 그룹을 바꿉니다(Tab 으로 그룹을 넘기지 않음). `q`/`Esc` 는 한 단계 뒤로(그룹 줄 → 벤더 줄)이고 맨 위(벤더 줄)에서만 결과 보기를 마칩니다. 지금 움직이는 줄은 앞에 `>>` 가 붙고 선택 탭이 반전되며, 키 안내 줄도 단계에 맞게 바뀝니다. ↑↓·PgUp/PgDn·Home/End·a/o/f/n/s 는 어느 단계에서나 동작합니다.
- **화면 탭 `[프로파일 점검] [전체 설정 비교]`**: 벤더 줄 위에 화면 탭을 두고 `Tab`(`Shift+Tab`)으로 어느 위치에서나 전환합니다. 옛 “q → 전체비교 결과를 볼까요?” 질문은 없앴고, 처음 `[전체 설정 비교]` 로 넘어갈 때 기준을 고르고(`-ref`/`-ref-json`/`-diff` 를 줬으면 묻지 않음) 비교합니다(진행 표시·Ctrl+X 취소 그대로, 기준 선택을 취소하면 프로파일 화면으로 돌아오고 다음 Tab 에 다시 묻습니다). 그 뒤로는 두 결과를 오가며 화면마다 벤더·그룹·필터·스크롤을 따로 기억하고, `[전체 설정 비교]` 에서 `r` 로 기준을 다시 골라 비교할 수 있습니다(`compare_` CSV 는 새 결과로 덮어씀). 벤더 줄에서 `q` 를 두 번 누르면(설정할 항목이 있을 때) 수정 단계로, `e` 는 어디서든 바로 수정 단계로 갑니다(아래 “수정 확인을 두 단계로” 참고).
- **전체비교의 기준 서버 자신(`REFERENCE`)**: `(N/A) (N/A) REFERENCE` 한 줄 대신 속성마다 기준값 = 현재값 = 자기 값, 태그 `[OK]`, 비고 `REFERENCE` 줄로 보여 줍니다(제외 속성은 빠짐, 화면·`compare_` CSV 공통). 설정 계획은 그대로 이 호스트를 설정하지 않습니다.
- **기준값 칸은 언제나 초록**: 호스트 단위 오류 줄(접속 실패 등)도 기준값은 초록이고 태그·현재값·비고만 빨강입니다(TUI·평문 줄 공통, 이전에는 TUI 에서 그 줄 전체가 빨강).
- **절대 바꾸지 않는 속성(보호 속성, `protect.go`)**: 서버 고유값(IQN·iSCSI·IP·네트워크·ServerName·자산번호·UUID)과 부팅(BootMode·부팅 순서·PXE·URL/HTTP 부팅)·보안(SecureBoot·TPM·비밀번호·인증서)·스토리지(RAID·SATA·NVMe·VMD)·NIC(LOM·OCP·WoL·Option ROM·Slot) 속성은 프로파일이든 전체비교 값이든 설정하지 않습니다. 내장 패턴은 코드에 고정이라 `bios.conf`·`ignore_attrs.txt` 로 끌 수 없고, 새 conf 키 `protect_attrs=A|B|*패턴*` 은 **더하기만** 합니다(`bios.conf.example` 갱신). 겹겹이 막습니다: 설정 계획에서 수정제외 표시 → 적용 단계(`set.go`)에서 하나라도 걸리면 그 호스트는 접속도 하지 않고 통째로 `PROTECTED_ATTR` → PATCH 직전에 본문 속성 재검사 → `redfish.go` `checkAttributes` 가 PATCH 본문 전체 거부. 성능 속성(예: Dell `ProcPwrPerf`)을 잘못 막지 않도록 패턴을 좁혀 두었고 `protect_test.go` 가 `profiles/*.tsv` 의 속성이 막히지 않는지 확인합니다.
- **수정 확인을 두 단계로**: 이전의 “수정할까요?” 한 번을 바꿨습니다. 확인 1 은 차이 항목 전부를 호스트별 `현재값 -> 변경값 (수정대상)` / `(수정제외: 사유)` 로 보이고(값은 자르지 않음) “위 수정대상을 적용할까요?” 를 묻습니다. y 이고 수정제외가 있으면 확인 2 로 수정제외 항목만 “아래 항목은 변경하지 않습니다” 와 함께 다시 보이고 “수정제외 항목을 확인했습니다. 진행할까요?” 를 묻습니다. 그다음 남의 Pending 질문(y 포함 / n 제외 / Ctrl+X 취소). 확인 1·2 에서 `n`·`Esc`·`Ctrl+X` 면 아무것도 쓰지 않고 결과 화면으로 돌아옵니다. 옵션으로 프로파일·대상·기준을 미리 줘도 확인은 항상 묻습니다.
- **수정 계획 규칙 보강**(`setplan.go`): 비교서버와 BIOS 버전이 다른 호스트(`BIOS_VER_DIFF`)의 비교서버 값은 수정제외, 비교서버 값은 적용 때 레지스트리 형식이 Enumeration·Integer·Boolean 일 때만 전송(문자열·형식 불명은 `INVALID_VALUE`), 시스템 프로파일 성격 속성이 한 호스트에 여러 개면 프로파일 행 항목만 보내고 나머지는 수정제외(적용 단계 `DEPENDS_ON_PROFILE`), Dell 은 설정 Job 이 남의 Pending 까지 적용하므로 남의 Pending 이 있는 호스트는 통째로 제외. 설정 중 `Ctrl+X` 로 시작하지 않은 호스트는 `CANCELED` 로 `apply_failed.txt` 에 모입니다. `PROTECTED_ATTR`·`CANCELED` 를 README 3.6절 표에 추가했습니다.
- **종료·되돌아가기 규칙 정리**: `q` 는 어디서나 한 단계 뒤로(관리망 그룹 → 벤더 줄, 미리보기·설정 결과 → 결과 화면, 프로파일 고르기 → 대상 입력), 벤더 줄(맨 위)에서는 `q` 다음에 `q` 를 한 번 더 눌러야 끝나고 `Esc`·`N`·`Ctrl+X` 등 다른 키는 종료 확인만 취소합니다(홈에서 `N`·`Ctrl+X` 로는 끝나지 않음). 끝낼 때 설정할 항목이 있으면(덤프 읽기 제외) 먼저 수정 단계로 가고, 거절한 뒤의 `q` `q` 는 다시 묻지 않고 끝납니다(`r` 로 전체 설정 비교를 새로 하면 다시 묻습니다). 설정을 한 번 적용한 뒤에는 같은 세션에서 다시 설정할 수 없습니다. Ctrl+C 안내는 “종료는 q 를 두 번 누르세요” 입니다.
- **옵션 동작**: `-user` 는 대상, `-profile` 은 프로파일 질문을 건너뛰고, `-ref`/`-ref-json`/`-diff` 를 주면 수집 직후 바로 전체 설정 비교까지 실행합니다(첫 화면은 `[프로파일 점검]`, 실패·취소하면 안내만 남기고 다음 Tab 에 다시 시도). 설정 확인은 항상 묻습니다.
- **견고성**: 흐름 중 패닉이어도 터미널(대체 화면·커서·raw 모드)을 되돌리고, 호스트 하나의 처리 중 패닉은 그 호스트를 `BMC_ERROR` 로 남기고 나머지를 계속합니다(`sweep.go`). 질문 전에 쌓인 키(미리 친 `y`)는 버리고, 취소 후 기다리는 중 종료 시그널을 한 번 더 받으면 터미널을 되돌리고 바로 끝냅니다.
- **출력 안전**: `check.res_<user>`(와 `_postapply`)는 비대화형 실행 시작 때 이전 파일을 먼저 지우고(치명적 오류로 끝나도 auto_setup 이 옛 `OK` 줄을 읽지 않게), 임시 파일 + rename 으로 원자적으로 씁니다. CSV 는 `=` `+` `@` TAB CR 로 시작하는 칸과 음수가 아닌 `-` 로 시작하는 칸 앞에 `'` 를 붙여 엑셀 수식 주입을 막고, 화면·평문 출력·`check.res_<user>` 는 제어 문자(ESC 등)를 공백으로 바꿉니다(`lineSafe`).

- **Tab 으로 기준 고르기 빠져나오기**: 프로파일 화면에서 Tab 으로 넘어가 뜬 전체비교 기준 목록·비교서버 입력 줄에서도 Tab(Shift+Tab)을 누르면 프로파일 화면으로 돌아갑니다(전에는 Tab 이 먹지 않아 Esc/q 로만 나올 수 있었음). 앞서 비교한 결과는 그대로 두고, 접속·조회 중 Tab 은 “끝난 뒤 Tab (취소는 Ctrl+X)” 안내만 합니다.

### 알려진 한계

- 실제 BMC 에서 확인하지 않았습니다. 수정 단계의 반영은 “Pending 에 기대값이 들어간 것을 재조회로 확인”한 것일 뿐이므로 **설정 변경 후 랜덤한 서버 몇 대를 직접 확인**하십시오.
- 리눅스가 아닌 환경(개발용 빌드)에서는 TUI 가 열리지 않고 평문 출력으로 대체됩니다.
- `.gitignore` 에 `refs/` 는 아직 없습니다(실제 호스트 속성 값이 들어갈 수 있으므로 커밋하지 마십시오).

### 바이너리 sha256

| 파일 | Go | 크기 | sha256 |
|---|---|---|---|
| bin/biostool | go1.26.5 | 7852194 | fa2229c5af5cda3668ec8b52d9bc1bd1948b4940c95a8c9ce9416cd8151519af |
| bin/biostool_os6 | go1.20.14 | 6471680 | f207a16679a84edf1e347403983ea289c0f58cff346d0a3f3b93d01b631241d9 |

직접 시험: `bash testcase/demo_env.sh start` (mock BMC 6대·시험용 conf 생성, 따라 할 명령 출력) → `bash testcase/demo_env.sh stop`.

태그(커밋·푸시 후 직접 실행, 아직 만들지 않았습니다):

```bash
git tag -a v0.3.0 -m "redfish bios.sh 통합 점검(TUI)·전체비교·CSV·auto_setup 연동 (CHANGELOG 2026-10-15 항목)"
git push origin v0.3.0
```

## [Unreleased] (v0.2.0 후보) - 2026-10-10

Cisco UCSM 읽기 전용 점검·연구용 프로파일과 2순위(예비) 계정을 추가했습니다. **실제 BMC·UCSM 에 접속해 본 적은 없습니다**(아래 “알려진 한계”, [FIRST_RUN.md](FIRST_RUN.md) 18~25).

### 추가 (설정값 JSON 추출)

- **`bios_export.sh` + `biostool allcheck -export-attrs`**: 비교서버와 대상서버의 BIOS 속성 전체를 호스트별 JSON(`results/<일시>_all/attrs/<호스트>.json`, 0600, 이름 순 정렬, 비밀번호·토큰 없음)으로 저장해 `diff`·`vimdiff` 로 눈으로 비교합니다. 기본 실행은 저장하지 않습니다. 추가 접속은 없고(읽은 값을 저장), 같은 실행의 차이 리포트도 함께 나옵니다. 구현 `cmd/biostool/allexport.go`, 테스트 `TestAllCheckExportAttrs`, 랩 .58 mock BMC 2대 실행으로 확인.

### 변경

- **`all_bios_check.sh` 대화형 모드 + 비교서버(`-ref`)**: 터미널에서 `-user`/`-diff`/`-ref`/`-from-dump`/`-retry-from` 없이 실행하면 `대상 입력 → 1. 체크(bios_check.sh) / 2. 전체비교 → 비교서버 입력` 순으로 묻습니다(대상은 `user.txt` 파일 또는 hostname 직접 입력, 기본 `user.txt`). `biostool allcheck -ref <hostname>` 은 `diff.txt` 없이 비교서버 1대를 직접 받으며(`-diff` 와 동시 사용 불가), 리포트가 "비교서버" 표기로 `비교서버=a → 대상=b`, 한쪽에만 있는 속성은 `(비교서버에만 있음)`/`(대상에만 있음)` 으로 양방향을 말로 보여 주고 모두 같으면 `모두 동일` 한 줄을 냅니다. 화면 리포트 전체를 결과 폴더 `report.txt`(0600, 상한·색 없음)로도 저장합니다. 인자를 주는 기존 사용법(`diff.txt` 방식)과 기존 리포트 문구는 그대로입니다. 테스트 `TestAllCheckRefWordReport`, 랩 .58/.60 mock BMC 3~4대 실행으로 확인.

### 수정

- **`workflow*.svg` 6개가 GitHub 에서 잘려 보이던 문제**: 글자를 `foreignObject`(고정 크기 상자)가 아니라 `<text>` 로 내도록 다시 렌더링(`htmlLabels:false`, 측정 17px → 표시 16px 로 박스 여유). WORKFLOW.md 에 그림별 단계·확인 순서 표 추가, PR_CHECKLIST “흐름도 다시 그리기” 갱신.

### 추가

- **1순위 계정 생성·비밀번호 변경 `biostool account create|change`** + 래퍼 `account_create.sh` (확정 사양 [ACCOUNT_SPEC.md](ACCOUNT_SPEC.md), 사용법·실제 출력 README 2.12절). 1순위 계정(`bios.conf` user + `pass.enc`)이 BMC 에 없으면 rf.conf 의 벤더 admin(2순위)으로 로그인해 **같은 id/pw 의 관리자 계정을 만들고**(같은 id 가 있으면 덮어쓰지 않고 건너뜀), 새 계정으로 1회 로그인해 검증하며, **인증이 거부된 서버만 이번 실행이 만든 계정을 되돌립니다**(롤백). 점검 끝(`bios_check.sh`)에서도 같은 질문을 하며 기본 응답은 Y(빈 입력)입니다. 터미널이 아니면 `-stdin-ok` 없이는 묻지 않습니다. `change` 는 바이너리를 직접 쓰고(래퍼 없음) 새 비밀번호를 터미널에서 `*` 로 2번 입력받으며 붙여넣기를 거부합니다. `pass.enc` 는 자동 갱신하지 않고 `cd <폴더>; bash encrypt.sh` 를 csh 에서 그대로 복사할 수 있게 안내합니다. 결과 `account_result.tsv`(0600, 비밀번호·해시 없음). 구현 `cmd/biostool/account*.go`.
- **쓰기 허용목록 `ModeAccount`** (`redfish.go`): 계정 컬렉션 POST·계정 멤버 PATCH/DELETE 만 열고, PATCH/DELETE 는 이번 실행이 등록한(빈 슬롯·만든 계정·대상 계정) 멤버에만 나갑니다. 본문은 고정 형태만 허용(생성 키 집합, 비밀번호 변경, 슬롯 롤백 2단계). BIOS 점검·설정 모드에서는 계정 경로가 계속 거부됩니다.
- **Cisco UCSM 사용자 생성·변경** (`internal/ucsmxml` 계정 모드): `configConfMo` 로 `sys/user-ext/user-<id>` 의 aaaUser 하나만 생성(`created`, 역할 admin)·비밀번호 변경(`modified`, 허용 등록된 이름)·삭제(`deleted`, 이번에 만든 이름) 합니다. 그 밖의 `configConfMo`·다른 객체·`status="created,modified"` 는 전송 전에 거부됩니다. 읽기 전용 점검기(`biostool ucsm`)는 그대로 쓰기 불가입니다.
- **rf.conf 에 `cray` 벤더 추가**(`cray_user`/`cray_pass`): 비인증 ServiceRoot 의 `Vendor`/`Oem` 에 `cray` 가 들어 있는 BMC 가 대상.
- **mock 계정 서비스**: `mockbmc -accounts [-account-slots N] [-account-slot-style] [-account-deny-new-login]` (AccountService/Accounts/Roles 동적 응답, 슬롯 방식·검증 실패 시험).
- **테스트케이스 시나리오 7~11**: 계정 생성(기본 Y)·중복 건너뜀·슬롯 방식·검증 실패 롤백·변경 dry-run.
- 보안 리뷰(Jev 분류 `security_review`)에서 확신도 7 이하 지적 3건을 반영: 콘솔 출력의 BMC 값 정리, 기본 Y 질문 전에 터미널 입력 버퍼 비움, `change` 를 1순위 로그인 호스트·정확한 이름 일치로 한정(빈 슬롯은 Id 2 이상).
- **Cisco UCSM 읽기 전용 점검**: `biostool ucsm` + 래퍼 `ucsm_check.sh`. 대상은 블레이드 BMC 가 아니라 UCSM(Fabric Interconnect VIP) 주소입니다. 호출은 `POST https://<UCSM>/nuova` 의 `aaaLogin` → `configResolveClass` → `aaaLogout` 뿐이며 전송 직전 관문(`internal/ucsmxml`)이 그 밖의 요청을 막습니다. 기대값은 `bios_json/standard_map/standard_bios_map_xml.tsv`(`-xml-map` 으로 변경). 결과는 `results/<일시>/ucsm_result.tsv`. 판정 OK·FAIL·UNVERIFIED·MAPPING_MISSING·UNSUPPORTED, 호스트 오류 AUTH_FAIL·UNREACHABLE·TIMEOUT·XML_ERROR. 쓰기·재부팅·`-retry-from`·`retry.txt` 는 없습니다. 테스트: `cmd/biostool/ucsm_test.go`, `internal/ucsmxml/ucsmxml_test.go`.
- **연구용 프로파일 `profiles/VM_research.tsv`**: `bios_json/` 조사 데이터에서 만든 Redfish(JSON) 표준값입니다. 모든 행 `verified=N` 이라 점검만 되고 설정은 되지 않습니다. `bash bios_check.sh --profile VM_research`.
- **프로파일 오프라인 검증**: `bios_json/standard_map/validate_profile.ps1` → `profile_validation.md` (120행: OK 89, 허용값 확인 불가 29, Supermicro UI 이름만 2, 오류 0). 데이터와의 대조일 뿐 실장비 확인이 아닙니다.

- **2순위(예비) 계정 `/etc/rf_tool/rf.conf`**: 평문 `key=value`(`<vendor>_user`·`<vendor>_pass`, vendor = dell hpe lenovo supermicro cisco), 경로는 `-rf-conf <경로>` 또는 환경변수 `RF_TOOL_CONF`, 파일이 없으면 기능 꺼짐, 그룹/기타 권한이 열려 있으면 경고. **1순위(`bios.conf` user + `pass.enc`) 로그인이 401 일 때만** 비인증 ServiceRoot 로 벤더를 식별해 그 벤더 예비 계정으로 **1회** 다시 시도하며, 벤더 식별 불가·예비 계정 없음·인증 외 실패(연결 거부·타임아웃·5xx)에는 시도하지 않습니다. 둘 다 실패하면 최종 `AUTH_FAIL`. 결과는 리포트 `[2순위 계정(rf.conf)으로 접속] N대` 구역, 결과 폴더 `fallback_login.txt`(호스트 이름만, 0600), `run_info.txt` 건수 줄. `result.tsv` 열(14열)은 그대로이고 비밀번호는 어디에도 출력되지 않습니다. 한 번 실행(점검+Y 적용)에 같은 BMC 로 인증 시도가 최대 4회(단계마다 2회)입니다. 구현 `cmd/biostool/fallback.go`.
- **테스트케이스 `testcase/rf_fallback_case.sh`**: mock BMC(127.0.0.1:19101~19106)로 1순위 정상·예비 계정 접속·둘 다 실패·접속불가·값 불일치·표기 선택 6개 시나리오를 PASS/FAIL 로 판정합니다. Go 불필요, 실제 BMC 접속 없음, `/etc/rf_tool/rf.conf` 를 건드리지 않습니다(`-rf-conf` 로 임시 파일 지정). `RF_FORCE_OS6=1` 로 `*_os6` 바이너리 강제. 설명 `testcase/README.md`. 시나리오 2 는 mockbmc 에 `-open-serviceroot` 플래그가 있을 때만 활성(없으면 SKIP).
- **mock 서버 바이너리 커밋**: `bin/mockbmc`·`bin/mockbmc_os6`(정적 빌드)를 저장소에 포함해 폐쇄망에서 Go 없이 시험할 수 있습니다(`.gitignore` 의 `/bin/mockbmc` 제외 삭제). `mockbmc.sh` 는 바이너리가 있으면 빌드 없이 그대로 쓰고 없을 때만 go 로 빌드합니다.

### 변경

- **설정 시 허용값 표기 선택**: 프로파일 `value` 가 `Enabled|Enable` 처럼 여러 표기면 점검은 어느 쪽이든 OK 이고, 설정(Y)·`-dry-run` 은 BMC 레지스트리가 받는 표기를 골라 보냅니다(정확히 있는 첫 표기, 없으면 대소문자만 다른 레지스트리 표기). 예: `Enabled|Enable` + Lenovo 레지스트리 `Enable|Disable` → `Enable`.
- **리포트의 오류 구역 분리**: 호스트 오류가 `[접속불가]`(UNREACHABLE·TIMEOUT·BMC_ERROR·HTTP_ERROR, 계정 문제 아님) / `[계정 오류 AUTH_FAIL]` / `[기타 오류]` 로 나뉩니다. 상태 값·`result.tsv`·`retry.txt` 규칙은 그대로입니다.
- `VM_research.tsv` 수동 보정 2건: Lenovo 값 `Enabled|Enable`·`Disabled|Disable` 둘 다 허용, SD650 V3 hyper_threading 대체 후보 `Processors_HyperThreading`. `map_bios_attrs.ps1` 로 재생성하면 덮어써집니다.
- FIRST_RUN 1번(Dell `Performance` ↔ `PerfOptimized`): 조사 데이터의 레지스트리 값이 `PerfOptimized` 이므로 데이터 기준으로 해소했습니다. 실장비 값 대조는 여전히 권장합니다.
- **Redfish `result.tsv`·`fail.tsv`·`merged_result.tsv` 에 `protocol`(항상 `json-redfish`)·`mgmt_version` 열 추가** (`bios_version` 바로 뒤, 12열 → 14열). 점검이 끝난 호스트마다 Manager 리소스를 1회 더 GET 합니다(읽기 전용 허용목록 안). 값은 Manager 의 Model·FirmwareVersion 이며 못 읽으면 `-`. v0.1.0 의 12열 결과는 `-retry-from` 이 읽어 새 형식으로 병합합니다(호환 테스트 포함). **열 위치로 `result.tsv` 를 읽는 외부 스크립트·엑셀은 수정해야 합니다.**
- UCSM system_profile 은 후보 2개(`vpCPUPerformance=hpc`, `vpEnergyPerformance=performance`)를 각각 별도 항목으로 점검합니다(매핑 TSV 에 행 추가, 한 항목에 여러 행 허용).
- 문서: README(VM_research·UCSM 사용법 2.9절, 2순위 계정 2.10절, 테스트케이스 2.11절, 서브커맨드·플래그·래퍼 표), `bios_json/standard_map/README.md`(XML 점검기 존재 반영), FIRST_RUN(확인 항목 18~25, 26~30 추가), ARCHITECTURE(`ucsm` 위치, `fallback.go`, “추후 확장”: 계정 생성 설계 메모), 사용법.txt(UCSM 절, 2순위 계정·테스트케이스 절), PR_CHECKLIST(테스트케이스 항목).

### 검증 (2026-10-10, 랩)

| 항목 | 결과 |
|---|---|
| 랩 .58 (Go 1.26.5) | `go vet ./...`·`go test ./...` 통과. `bash testcase/rf_fallback_case.sh` 통과(6개 시나리오 모두 PASS, SKIP 0) |
| 랩 .60 (Go 1.20.14) | build·vet·test 통과 (Go 1.20 호환 확인). `testcase/rf_fallback_case.sh` 를 기본 바이너리와 `RF_FORCE_OS6=1`(`*_os6`) 로 각각 통과 |
| 바이너리 재빌드 | `bin/biostool` (Go 1.26.5), `bin/biostool_os6` (Go 1.20.14), `bin/mockbmc`, `bin/mockbmc_os6` 를 다시 빌드해 함께 커밋 |
| 실제 UCSM 장비 | 조회하지 않음 (미확인) |

랩으로 소스를 보낼 때 Windows 작업 트리의 Go 파일은 CRLF 이므로 LF 로 바꿔 보내야 `TestModeSetOnlyInSetPath` 가 통과합니다(저장소 blob 은 LF).

빌드 완료 바이너리 (v0.2.0 후보, v0.1.0 항목의 해시와 별도):

| 파일 | 빌드 | 크기(바이트) | sha256 |
|---|---|---|---|
| `bin/biostool` | Go 1.26.5 | 7479458 | `cc1439a4d424ab901368105413e4d4338ce0f3aa21f4ba8ad49d69ccb9612622` |
| `bin/biostool_os6` | Go 1.20.14 | 6111232 | `210249522cd8ee9ede3cc36b10606797724f46310cec4e6010db850467ca5877` |
| `bin/mockbmc` | Go 1.26.5 | 7237794 | `41b6b9a69fbcce12e49ddf09d7823a31cfdcf55f26eeb06ee2988693dab45c97` |
| `bin/mockbmc_os6` | Go 1.20.14 | 5689344 | `8f5e997045fee74b55d71ab88bfa00b2985dcb79561a5235710caacb12c5fc49` |

### 알려진 한계 (2순위 계정)

- **실제 BMC 에서 확인하지 않았습니다.** 비인증 ServiceRoot 가 벤더(`Vendor`·`Oem`)를 돌려주는지, 보안 강화로 ServiceRoot 가 막힌 BMC, 로그인 실패 횟수와 잠금 정책, Basic 인증 BMC 에서의 동작은 [FIRST_RUN.md](FIRST_RUN.md) 26~29 항목입니다. 레지스트리 허용값의 실제 표기(`Enable` 대 `Enabled`)는 30 항목입니다.
- 비인증 ServiceRoot 가 막힌 BMC 는 벤더를 식별하지 못해 예비 계정을 시도하지 않고 `AUTH_FAIL` 로 남습니다(다른 벤더 계정을 돌려가며 넣지 않음).
- 계정 생성·변경의 한계는 아래 “알려진 한계 (계정 생성·변경)” 입니다.
- `mockbmc` 에 `-open-serviceroot` 플래그를 추가했습니다(비인증 ServiceRoot 에 응답, 벤더 식별·2순위 계정 시험용). 기본은 꺼짐(401)이며 실제 BMC 의 설정과는 무관한 mock 전용 옵션입니다.

### 알려진 한계 (계정 생성·변경)

- **실제 BMC·UCSM 에서 확인하지 않았습니다.** 벤더별 생성·변경 방식은 `id_json` 의 문서·공개 스크립트 근거입니다(사양상 미확인 방식도 Y 로 적용되며 결과 `warn` 열에 경고). 첫 실장비는 1대부터 `-dry-run` 으로 본문을 확인하십시오. [FIRST_RUN.md](FIRST_RUN.md) 31~37.
- 관리자 역할 이름은 BMC 의 Roles 컬렉션에서 `Administrator`→`Admin`→`admin` 순으로 고르고, 읽지 못하면 후보를 순서대로 보냅니다. Cray 의 `PasswordChangeRequired` 는 `false` 를 보내지만 실제 동작은 미확인입니다.
- 비밀번호 정책(길이·복잡도)은 사전 검사하지 않고 BMC 응답(`REJECTED`)으로만 압니다. `$ ? =` 처럼 UCSM 이 금지하는 문자도 마찬가지입니다.
- 생성 검증은 로그인 1회입니다. BMC 가 새 계정을 잠시 뒤에 활성화하면 거짓 `VERIFY_FAIL_ROLLED_BACK` 이 날 수 있습니다(다시 실행하면 됨).
- 붙여넣기 거부는 완전하지 않습니다(터미널이 키 입력과 붙여넣기를 구분하지 못하는 환경에서는 안내 문구만 있음).
- 동시 처리 100(최대 300)은 실측 값이 아니며 첫 실행 로그로 조정합니다. RHEL6 커널(2.6.32)·bash 4.1 에서의 동작은 확인하지 못했습니다.
### 알려진 한계 (UCSM 및 프로파일)
- **실 UCSM 미확인.** 응답 형태(outCookie·errorCode), 블레이드 dn·PID 매칭(`ucsmModelKey`), 실효 설정 위치(`<blade dn>/bios/bios-settings/…`)는 [FIRST_RUN.md](FIRST_RUN.md) 18~20 항목입니다.
- BIOS 정책(`bios-prof-*`)은 보지 않고 서버에 실제 적용된 값만 봅니다.
- `biosPlatformDefaults` 를 조회하지 않습니다. 현재값이 `platform-default` 이면 OK 로 추측하지 않고 사유에 확인 안내를 붙입니다.
- 세션 갱신이 없습니다. 긴 실행 중 세션 만료(errorCode 552 등)는 `UNSUPPORTED` 로 보일 수 있습니다.
- 재시도 기능이 없습니다(실패한 UCSM 은 `-user` 로 다시 실행). 랙 서버(computeRackUnit)는 대상이 아닙니다.
- 매핑 TSV 의 모든 행이 `verified=N` 이라 불일치는 `FAIL` 이 아니라 `UNVERIFIED` 로 보입니다.
- `VM_research.tsv` 의 값(Lenovo `Enabled`/`Enable`, HPE iLO6 허용값, SR630/650/SD530 `llc_prefetch`, SR645/675 V3 `sub_numa_cluster`, XE9780 `LogicalProc`, Supermicro 속성명)은 실장비에서 확인하지 않았습니다. FIRST_RUN 19~25 항목입니다.

## [0.1.0] - 2026-10-08

BMC(Redfish)의 BIOS 표준값을 점검하고 승인된 항목만 Pending 으로 설정하는 도구의 첫 릴리스입니다. **실제 BMC 에 접속해 본 적이 없고** 모든 검증은 합성 mock 과 `-from-dump` 로 했습니다(아래 “알려진 제한”, [FIRST_RUN.md](FIRST_RUN.md)).

### 추가 — 단계별 기능

- **골격(단계 1)**: `bios.conf`(알 수 없는 키·conf 안의 비밀번호는 오류)·`user.txt` 파싱, `/etc/hosts` 의 `이름-m` 해석(`IP`/`이름-m`/`이름`, `:포트`), AES-256-GCM 비밀번호 암복호화(`pass.enc`+`key.bin`, 패스워드변경자동화 방식 재사용), bash 래퍼(`bios_check.sh`·`xml.sh`·`all_bios_check.sh`·`encrypt.sh`·`lib.sh`)와 빌드 스크립트. 래퍼는 커널 메이저 버전으로 `bin/biostool`(os8)/`bin/biostool_os6`(RHEL6)을 자동 선택하고 RHEL6 의 bash 4.1 에서 동작.
- **Redfish 클라이언트(단계 2)**: 호스트당 세션 1회·자기 세션만 삭제, **호출 허용목록**(`checkAllowed`/`checkBody`: GET 은 로그·Actions 제외, POST 는 세션과 Dell 설정 Job 만, PATCH 는 `Bios/Settings|Pending` 만, DELETE 는 자기 세션만), 타임아웃·재시도(GET·세션 연결 실패만)·지수 백오프, 401 즉시 중단·재시도 금지, 오류 상태 분류(`UNREACHABLE`·`TIMEOUT`·`BMC_ERROR`·`AUTH_FAIL`·`UNSUPPORTED`·`HTTP_ERROR`), 오류 상세의 비밀번호·토큰 마스킹. 보안 리뷰 지적(F1~F9) 반영.
- **mock BMC(단계 3)**: `internal/mockbmc` + `cmd/mockbmc`(`mockbmc.sh`), `testdata/` 합성 트리 6종(Dell R660, HPE DL360 Gen11/Gen10, Lenovo SR650 V3, Cisco C220 M7, Supermicro X12), 요청 기록(`Writes`·`LogHits`·`Sessions`)·장애 주입.
- **사전조사(단계 4)**: `biostool dump`/`xml.sh` — GET 전용 좁은 순회(호스트당 120경로 상한, LogServices 제외), `dumps/<vendor>/<model>/<biosver>/<host>/…` JSON 저장, `index.tsv`, 사람이 읽고 전달할 짧은 요약(`--compact` 지원).
- **점검(단계 5)**: `biostool check`/`bios_check.sh` — 프로파일 TSV(표준 4항목+추가 항목, `A|B` 허용값, 모델 정규화, `*` 폴백), 병렬 점검, 상태 분류(`OK`·`PENDING_OK`·`FAIL`·`UNVERIFIED`·`PENDING_EXISTS`·`MAPPING_MISSING`·`PENDING_NO_JOB`), 결과 TSV(`result.tsv`·`ok.txt`·`fail.tsv`·`retry.txt`·`run_info.txt`), 터미널 리포트, `-from-dump`, MAPPING_MISSING 모델의 덤프·요약 자동 저장.
- **수천 대 규모(단계 5 확장)**: `mockbmc.Farm`(가상 BMC 수천 대를 한 리스너에), 동시 접속 제어, **AUTH_FAIL 차단기**(`auth_fail_stop`, 기본 3: 처음 N 대 직렬 웜업 → 성공하면 병렬, 누적되면 중단하고 `SKIPPED_AUTH_STOP`).
- **전체 비교(단계 5b)**: `biostool allcheck`/`all_bios_check.sh` — `diff.txt` 기준 호스트와 `user.txt` 대상을 BMC 모델로 자동 분류해 같은 모델끼리 Bios Attributes 전체 비교(`ignore_attrs.txt`·내장 제외 목록), `DIFF`/`ONLY_REF`/`ONLY_HOST`, `NO_REFERENCE`·`REF_UNREACHABLE`·`REFERENCE`, 특이사항(`BIOS_VER_DIFF`·`MULTI_REF`·`DIFF_TXT_MODEL_MISMATCH`), 읽기 전용.
- **설정(단계 6)**: 점검 후 `Y` 일 때만 Pending 설정, `-dry-run`(보낼 PATCH·Job 본문 출력, 읽기 전용 클라이언트), 재조회 후 쓰기(`ALREADY_OK`·`CHANGED`·`PENDING_EXISTS`), 레지스트리 사전검증(`SKIPPED_READONLY`·`INVALID_VALUE`), 시스템 프로파일 종속 처리(`DEPENDS_ON_PROFILE`), Dell 은 BIOS 설정 Job 판별(`PENDING_NO_JOB`)·자동 생성 안 됐을 때만 Job 최대 1회, 반영 재조회(`APPLIED`/`APPLY_UNCONFIRMED`), 같은 BMC 중복 대상 방지(`DUPLICATE_TARGET`), `apply_result.tsv`·`applied.txt`·`apply_failed.txt`·`apply_requests.txt`. 보안 리뷰 지적 반영(`*` 행의 `verified=Y` 금지 등).
- **재시도(단계 7)**: `-retry-from` — 이전 결과의 `retry.txt` 대상만 다시 실행하고 `merged_*` 로 병합(이전 폴더는 읽기만, 재시도 여러 번 연쇄 가능, check·allcheck 공통).
- **빌드·배포(단계 8)**: `bin/biostool`(os8)·`bin/biostool_os6`(RHEL6, Go 1.20.x 정적)를 **저장소에 커밋**(회사 환경에 Go 없음), 두 바이너리의 mock 대상 결과 비교.
- **문서(단계 9)**: README(빌드·설치·사용·옵션·상태 코드·Disclaimer·전역 명령어), ARCHITECTURE, WORKFLOW+`workflow*.svg`, FIRST_RUN, PR_CHECKLIST, CHANGELOG, 사용법.txt, `setup.sh`(오프라인 빌드), 저장소 루트 `.github/workflows/redfish-ci.yml`.

### 안전장치

- 재부팅·즉시 적용(`ApplyTime=Immediate`)·Reset·ClearLog·로그 조회·남의 세션/Job 삭제는 **경로 자체가 허용목록에 없어** 구조적으로 불가능(전송 전 차단).
- 설정은 `set.go` 한 곳뿐이며 프로파일에서 **모델을 지정한 `verified=Y` 행**·Dell/HPE/Lenovo 만, 호스트당 PATCH 1회·재시도 없음. 남의 Pending/Job 은 건드리지 않음(`PENDING_EXISTS`).
- 비대화형 표준입력의 Y 는 N 으로 처리(`-stdin-ok` 로만 예외, 경고 출력).
- 계정 잠금 방지: AUTH_FAIL 재시도 금지 + 차단기. 비밀번호는 인자·로그·TSV 에 남기지 않고 결과 파일은 0600.

### 문서 단계에서 고친 것

- `allcheck.go` 의 `diff.txt`·`ignore_attrs.txt` BOM 제거가 `"\ufeff"` 대신 문자열 `Feff` 로 들어가 있던 버그 수정(윈도우 메모장 BOM 이 붙은 파일의 첫 줄 인식 실패, `Feff` 로 시작하는 호스트명 잘림). 테스트도 같은 오류로 통과하고 있어 함께 바로잡음.
- 내부 버전 문자열 `toolVersion` 을 `0.4.0-dev` → `0.1.0` 으로 정리(`run_info.txt`·`dump_meta.json` 에 기록되는 값).

### 알려진 제한

- **실장비 미검증.** 속성 이름·허용값·Settings 경로·Dell Job 동작은 모두 추정이며 `testdata/` 는 합성 자료입니다. 확인 항목 17건은 [FIRST_RUN.md](FIRST_RUN.md) 에 정리했고, `profiles/VM.tsv` 는 엑셀 표의 시작값(전부 `verified=N`)입니다.
- 엑셀 값 3건(Dell `Performance`↔`PerfOptimized`, HPE `ProHyperthreading` 철자, `HighPerformanceCompute(HPC)` 표기)과 Cisco CIMC/UCSM 구분, Supermicro Settings 경로, Lenovo `Bios/Pending` 은 미확정.
- 쓰기는 Dell·HPE·Lenovo 만. Cisco·Supermicro·Cisco UCSM 관리형·Supermicro X11 이하는 점검만(안 되면 `UNSUPPORTED`).
- 구형 BMC(TLS 1.0/1.1)에는 연결할 수 없습니다(TLS 1.2 이상). `UNREACHABLE` 의 원인(연결 거부·DNS·TLS 실패)은 화면·결과 파일에 나오지 않습니다.
- 실제 RHEL6 커널(2.6.32)에서의 `bin/biostool_os6` 기동은 확인하지 못했습니다(랩 커널 4.18, CentOS 6 userland 컨테이너까지 확인).
- 실제 BMC 응답 지연에서의 소요 시간은 미측정(mock 은 3,000대를 3초대에 처리).

### 검증 현황 (2026-10-08, 랩에서 실행)

| 항목 | 결과 |
|---|---|
| 랩 .58 (Go 1.25.10, Rocky/RHEL8) | `go vet ./...` 통과, `go test ./... -count=1` 통과 (최상위 테스트 210 PASS / 0 FAIL / 0 SKIP, 서브테스트 89 PASS) |
| 랩 .60 (Go 1.20.14) | `go vet ./...`·`go build ./cmd/biostool` 통과, `go test ./... -count=1` 통과 (210 PASS / 0 FAIL / 0 SKIP) → Go 1.20 호환 확인 |
| `bash setup.sh` | .58(Go 1.25.10)·.60(Go 1.20.14) 모두 성공, `vendor/` 없이 `-mod=vendor` 동작 확인. **재빌드 결과가 커밋된 바이너리와 sha256 이 같음**(.58 의 `setup.sh` = `bin/biostool`, .60 의 `build_os6.sh` = `bin/biostool_os6`; .60 에서 `setup.sh` 를 돌린 결과는 `bin/biostool_os6` 와 같음 → os6 바이너리는 Go 1.20.14 산출물). go 가 없으면 안내 후 종료코드 2 |
| 수천 대 규모 | `TestScaleCheckThousands`: mock 3,000대(미등록 30·5xx 30·FAIL 주입 90), 동시 100 — doCheck 3.16초, 힙 최대 36.2 MiB, 고루틴 최대 462 → 종료 후 3, FD 최대 212, 결과 행 수 = 호스트×항목 |
| 허용목록·로그 안전 | 보안 테스트(`redfish_security_test.go`·`set_security_test.go`) 통과, mock 의 `LogHits`=0, 호스트당 세션 생성 1·삭제 1 |
| 바이너리 e2e | 번들된 `bin/biostool` 로 mock 에 대해 점검 → dry-run → Y 적용(`APPLIED`) → 재점검(`PENDING_OK`) → 사전조사 → 전체 비교 → `-from-dump` → `-retry-from` → AUTH_FAIL 차단기를 랩에서 실행, `사용법.txt` 의 mock 연습 블록도 그대로 실행해 성공. os8 ↔ os6 바이너리의 결과 파일 일치 비교는 단계 8 구현 보고 기준이며 이번에 다시 실행하지는 않음 |
| RHEL6 userland | CentOS 6.10 컨테이너(bash 4.1.2)에서 래퍼 5종 `bash -n` 통과, `bin/biostool_os6 help` 동작(커널은 호스트 4.18) |
| **실장비** | **없음 (미검증)** |

빌드 완료 바이너리 (문서 단계에서 발견한 BOM 처리 버그·내부 버전 문자열 수정 후 재빌드한 값; `bin/biostool` = Go 1.25.10 / `bin/biostool_os6` = Go 1.20.14, 둘 다 정적):

| 파일 | 크기(바이트) | sha256 |
|---|---|---|
| `bin/biostool` | 6811832 | `fc7c54e699878f2b0ea322d27077a953396bfd1fb6a904a2e8fbde3f99d0ae67` |
| `bin/biostool_os6` | 5697536 | `258897877b97b78c21ca1e466e9bf883f1847704ea3a2ac8db8deae4d8263546` |

## 버전 태그

배포 시점마다 태그를 남기고, 태그 메시지에 CHANGELOG 의 어느 항목까지 포함하는지 적습니다. (태그는 아직 만들지 않았습니다. 커밋·푸시 후 아래 명령을 실행하십시오.)

```bash
git tag -a v0.1.0 -m "redfish BIOS 표준값 점검·설정 도구 첫 릴리스 (CHANGELOG 2026-10-08 항목)"
git push origin v0.1.0
```
