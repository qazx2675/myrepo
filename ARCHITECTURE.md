# ARCHITECTURE.md

| 폴더/파일 | 역할 |
|---|---|
| `main.go` | 진입점·흐름 제어: 플래그 파싱(`-end` 포함), conf/입력/장애목록 로드, 신호 처리기 등록, 종료 시각 입력(`askEnd`), AWX 사전 확인(`prepareAWX`), 접속체크(`checkVendors`, gossh `vendor_id`), 결과 초기값(`initResults`), 단계별 `runStage`(묶음 `split` → `launchBatch` → 감시 → `askRetry` 재실행), 결과표(`resultLines`)·종료코드(`hasProblem`) |
| `invswitch.go` | 템플릿 인벤토리 바꾸기·되돌리기: `prepareAWX`(템플릿·`username`/`default_inventory` 인벤토리 ID), `launchSwitched`(확인 → PATCH → 확인 GET → launch → 되돌림), 사용 중 질문 `askBusy`(대화형 r/x, 그 밖 `[인벤토리사용중]` 줄 + stdin), `curSwitch`(되돌릴 템플릿 기억)·`onSignal`(SIGINT/SIGTERM 시 되돌리고 128+n 종료) |
| `inspect.go` | `stress-test -inspect` (읽기 전용, GET 만): `runInspect`(conf 확인) → `inspectRun`(템플릿 조회·survey 표·인벤토리 목록·`inventory` 소스 목록·동기화 인벤토리 vs username 비교), 순수 점검 로직 `checkKeys`(hostname_key·`stress_end_keys` 가 survey 에 있는지, 필수 항목 누락)·`suggestLines`/`endCandidates`(conf 제안 줄). 종료코드 0 정상 / 1 불일치·경고 / 2 설정·접속·API 오류 |
| `endtime.go` | stress-ng 종료 시각 5값: `checkEndField`/`parseEnd`(범위·없는 날짜·지금 이후), `parseEndFlag`(-end), `askEnd`(키 하나씩 대화형), `endVars`(survey type integer/float → 숫자, 그 밖 → 입력 글자) |
| `config.go` | `Config` 구조체와 `key = value` 파서(기본값 포함, 옛 `cpu_intel_*`/`cpu_amd_*` 키 거부, `stress_end_keys` 5개 검사), 단계별 필수 키 검증 `validate`, user 결정(`-user` → `STRESS_USER` → `$USER`), conf 탐색(`./conf` → `~/.stress-test` → `<바이너리 위치>/conf`), `key=value, ...` 분해, 이력 파일 기록 |
| `input.go` | 입력 파일 파싱(`parseHosts`: 1필드/12·13필드 줄 판정, DNS·MAC 생성), error_list.txt 파싱(`parseErrorList`), `-steps` 파싱, 단계 결과 값 상수(`실행` 등) |
| `gossh.go` | gossh 경로 결정(conf → `GOSSH` → PATH), 임시 호스트 파일로 gossh 실행·결과 파일(`_res_off` 등) 수거, 출력(`host: 내용`) 파싱, 벤더(`parseVendors`)·1분 로드(`parseLoads`) 해석 |
| `register.go` | 인벤토리 등록: yaml 생성 → scp → ssh `custom_inventory.sh` → `svr_dir` 새 yml 1개 확인(수정 시각 비교) → (git_dir) `.git_upload.sh` → invsync |
| `awx.go` | AWX REST(`/api/v2`) 최소 클라이언트: 템플릿 조회(ID/이름, 현재 인벤토리 `summary_fields.inventory`)·인벤토리 PATCH, 인벤토리 이름 조회(`?name=`), `survey_spec` type, launch, job poll·stdout tail, 인벤토리 소스 ID 결정·PATCH·update·완료 대기 (awxkit 에서 필요한 부분만 복사) |
| `monitor.go` | 감시 루프 `monitorLoad`: interval 마다 로드 확인, 기준 이상 호스트 제거, 목록 비면 조기 종료, 대화형은 ANSI 화면 갱신/비대화형은 한 줄 요약, 한글 폭 맞춘 표 |
| `awx_test.go` | fake AWX(httptest)·가짜 ssh/scp 로 `launchBatch` 성공(인벤토리 PATCH 순서·종료 시각 type 변환)·job 실패, hostname_key 없음, launch 실패 시 되돌림, 되돌리기 실패, 사용 중 → r / EOF / 대화형 x, PATCH 직후 경합, 종료 시각 지남, 신호 시 되돌림, 사전 확인 실패, 새 yml 없음, 고정 소스 invsync 검증 |
| `inspect_test.go` | `-inspect`: `checkKeys`·`suggestLines`·`endCandidates`·표 행, 가짜 AWX(GET 만 허용, 쓰기 요청 기록)로 종료코드 0/1/2 시나리오(키 없음·필수 누락·사용 중·인벤토리/소스 없음·접속 불가) |
| `logic_test.go` | 파서(입력·장애목록·steps·conf·옛 키·`stress_end_keys`·벤더·로드)·종료 시각 검증·대화형 입력·type 변환·결과표·yaml 이름·`_Nea` 경고·감시 루프·화면 단위 테스트 |
| `go.mod` | `go 1.20`, 표준 라이브러리만 사용 (vendor 불필요) |
| `setup.sh` | 대화형 설정: 샘플 키 순서대로 묻고 `conf/${user}_setting.conf` 저장(백업·600), 선택 점검(curl AWX, ssh repohost). `--user`/`--conf`, 종료코드 0/1/2 — setup_check `setup.sh` 가 호출 |
| `stress_run.sh` | bash 래퍼: 상단 `GOSSH`/`USER_ROUTE` 변수, user 메뉴, `a/b` 단계 입력, 바이너리가 없으면 `build.sh`, 나머지 인자 그대로 전달 |
| `build.sh` | `CGO_ENABLED=0 go build -o stress-test .` |
| `stress-test` | os8(Rocky 8) 빌드 완료 바이너리 (커밋 대상, os6 빌드 없음) |
| `conf/sample_setting.conf` | 설정 파일 샘플 (모든 키) |
| `test/fake_awx/` | 가짜 AWX 서버(별도 go 모듈): launch 시 대상 서버에 gossh 로 busy loop 부하 생성. 템플릿 인벤토리 GET/PATCH, 인벤토리 `?name=` 목록, stress-ng `survey_spec`(integer/text 섞음). `-skipfile`/`-skiponce`(미실행 재현), `-failtmpl`(job 실패), `-busyfile`(템플릿 인벤토리 사용 중 재현), `-launchdelay`(신호 시험). `-inspect` 용으로 cpu-test·stress-ng survey(required/default/choices/min/max)와 인벤토리 소스 상세 필드 응답 |
| `test/fake_repo/` | 가짜 `custom_inventory.sh`, `.git_upload.sh` |
| `test/fake_user/` | `USER_ROUTE` 대역 (`info_mn.sh`, `info.sh`, 래퍼 시험용 `stress_run_wrap.sh`) |
| `test/e2e.sh`, `test/e2e_c.sh` | 랩(.58) 시나리오 시험(비대화형 / TTY 대화형) |
| `test/tester_setting.conf`, `test/hosts_*.txt`, `test/error_list.txt` | e2e 입력 샘플 |
| `.github/workflows/ci.yml` | gofmt·vet·test·bash -n·shellcheck |
| `README.md` / `PR_CHECKLIST.md` | 사용 설명 / 변경 점검 목록 |
| `계획서.md` / `계획서_템플릿변경.md` | 인터뷰로 확정한 사양 / 2026-11-06 템플릿 실행 방식 변경(단일 cpu 템플릿, 인벤토리 바꾸기·되돌리기, 종료 시각) |

수정 요청별로 볼 곳:

- **"입력 형식(필드 수·hostname 변환·MAC/IP 규칙) 변경"** → `input.go` `parseHosts` + `logic_test.go` + README 2.3
- **"장애 목록 규칙 변경"** → `input.go` `parseErrorList` (setup_check `-er` 와 같은 규칙 유지)
- **"conf 키 추가·기본값 변경"** → `config.go`(`Config`, `setters`, `parseConfig` 기본값, `validate`) + `conf/sample_setting.conf` + README 3.3
- **"벤더 판정·접속체크 명령 변경"** → `gossh.go` `vendorCmd`/`parseVendors`, `main.go` `checkVendors`/`initResults`
- **"감시 기준·화면 변경"** → `monitor.go`, 로드 명령은 `gossh.go` `loadCmd`/`parseLoads`
- **"등록 방식(yaml/scp/custom_inventory/git/invsync) 변경"** → `register.go`, invsync 세부는 `awx.go`
- **"템플릿 launch 변수 전달 방식 변경"** → `main.go` `launchBatch` + `config.go` `TemplateConf`, 종료 시각은 `endtime.go`
- **"템플릿 인벤토리 바꾸기·되돌리기·사용 중 질문/프로토콜 변경"** → `invswitch.go` (프로토콜 줄 `busyPrefix` 는 setup_check `-run` 이 읽으므로 함께 바꿀 것)
- **"-inspect 점검 항목·출력·제안 줄 변경"** → `inspect.go` (`checkKeys`/`suggestLines`) + `inspect_test.go` + README 2.8, `test/e2e.sh` 시나리오 G
- **"결과표 열·값 변경"** → `main.go` `resultLines` + `input.go` 상수 + README 2.6 (setup_check 연동 시 열 규약 확인)
- **"단계 추가·순서 변경"** → `input.go` `parseSteps`, `main.go` `runStage`/`split`, `stress_run.sh` 메뉴
- **"래퍼 메뉴·user 선택 변경"** → `stress_run.sh`
- **소스 변경 시** → `bash build.sh` 로 os8(랩 .58) 에서 바이너리 재빌드 후 함께 커밋

## 실행 흐름

```
stress_run.sh <user.txt> [추가 인자]
  ├ USER_ROUTE 있으면 info_mn.sh 메뉴 → 번호 → info.sh <번호> = user
  ├ 단계 입력 (a/b/ab/ba)
  ├ 바이너리 없으면 build.sh
  └ stress-test -user <u> -hosts <user.txt> -steps <ab> [추가 인자]    (환경변수 GOSSH 전달)

main.run
  [0] 인자 검증(-steps/-hosts/-retry) → user 결정 → conf 탐색·로드(옛 cpu_intel_*/cpu_amd_* → exit 2)·validate(단계별 필수 키)
      대화형 판정 = !-batch && stdin·stdout 이 터미널
      -end 검사 (b 가 있고 비대화형인데 -end 없음/잘못됨 → exit 2)
      입력 파일 parseHosts (형식 오류 → exit 2) → 장애 목록(-er / ./error_list.txt)을 비고로
      gossh 경로 결정 → output_dir, 임시 작업 폴더, gossh 로그
      SIGINT/SIGTERM 처리기 (curSwitch.onSignal: 바꿔 둔 템플릿 인벤토리를 되돌리고 128+n 종료)
      b 가 있고 -end 없음(대화형) → askEnd: stress_end_keys 를 하나씩 입력
      prepareAWX: 단계별 템플릿 + 인벤토리 username·default_inventory 이름 → ID (없으면 exit 2)

  [1] checkVendors  (DNS실패 제외)
      gossh -script 'grep -m1 vendor_id /proc/cpuinfo'
        ├ 출력 없음 / _res_off·_res_refsed·_res_cancel ─→ 접속불가
        ├ GenuineIntel ─→ Intel   AuthenticAMD ─→ AMD   그 외 ─→ unknown
      initResults: DNS실패 / 접속불가 / 나머지 = 미실행 (unknown 벤더도 cpu 대상)

  [2] 선택한 단계 순서대로 runStage(stage)       (앞 단계 감시가 끝난 뒤 다음 단계)
      pending = 결과가 '미실행' 인 호스트
      round 0,1,2 ...
        split: 단계마다 1묶음 (stress / cpu — Intel·AMD 는 템플릿 안에서 구분)
        launchBatch
          (stress) 종료 시각이 지났으면: 대화형 askEnd 다시 / 비대화형 묶음 '실패'(종료 시각이 지남)
          register (yaml → scp → custom_inventory.sh → 새 yml 1개 → [git_upload] → invsync)
          vars = extra_vars + [hostname_key = 줄바꿈 hostname] + (stress) survey_spec type 에 맞춘 종료 시각 5값
          launchSwitched
            loop: GET 템플릿 → 인벤토리 ≠ default_inventory → askBusy
                    대화형 "r=다시 확인, x=이 묶음 중단"
                    그 밖  stdout "[인벤토리사용중] <안내>" + stdin 한 줄 (r=다시, 그 밖·EOF=중단)
                    r → loop (등록은 다시 안 함) / 중단 → 묶음 '실패'(템플릿 인벤토리 사용 중)
                  curSwitch.begin → PATCH inventory=username → GET 확인
                    (우리 것 아님 → curSwitch.cancel, 다른 이름이면 askBusy)
                  launch → curSwitch.restore (PATCH inventory=default, launch 실패해도)
            되돌리기 실패 → ✗✗✗ 경고 + 이력 action=restore + 비고 "인벤토리 복원 실패 — 수동으로 DEFAULT 로"
          pollJob → successful 아니면 묶음 전체 '실패' (+ 비고, stdout tail) → 감시 제외
        launched 전체를 monitorLoad 한 번
          monitor_interval 마다 gossh 'cat /proc/loadavg'
          1분 로드 >= load_threshold → 목록에서 제거(= 실행)
          목록 empty → 조기 종료 / monitor_sec 만료 → 남은 호스트 = 미실행
        남은 호스트 있으면 askRetry
          대화형: y/N       비대화형: round < -retry
          yes → pending = 남은 호스트 (새 yaml ${user}_retryN_<묶음>.yaml) 로 반복

  [3] finish: 결과표 "hostname|CPU벤더|cpu|stress-ng|비고" (입력 순서) 출력 + <output_dir>/${user}_stress_result.txt
      history_file 에 이력 기록
      exit 0 (미실행/실패 없음) / 1 (미실행 또는 실패 있음) / 2 (설정·입력·AWX 사전 확인 오류, [0]~[1] 단계) / 130·143 (신호)
```

## 핵심 설계 결정

- **`-inspect` 는 읽기 전용 하위 모드** — `-inspect` 가 있으면 `main.run` 이 입력 파일·단계·gossh·신호 처리기를 건너뛰고 `runInspect` 로 간다(conf 에서 AWX 접속 정보만 확인). 템플릿 인자는 플래그 사이에 있어도 받도록 `fs.Parse` 를 반복한다. AWX 쪽은 GET(템플릿·`survey_spec`·인벤토리·소스)만 쓰고, 템플릿 인벤토리 바꾸기(`invswitch.go`)는 건드리지 않는다. 동기화 인벤토리와 `username` 인벤토리가 다른 것은 참고 경고(종료코드 0 유지), 그 밖의 불일치·경고는 종료코드 1, 템플릿 조회·접속 실패는 2.

- **단일 정적 바이너리, 표준 라이브러리만** — 폐쇄망·Go 없는 환경에서 빌드 없이 쓰려고 vendor 대신 빌드된 os8 바이너리를 커밋합니다. awxkit·setup_check 의 필요한 코드(AWX 클라이언트, conf 파서, gossh 처리, `-er` 규칙)는 **복사**해서 이 폴더 안에서 단독 빌드되게 했습니다.
- **user 선택은 bash 래퍼, Go 에는 `-user`** — AWX 01 스크립트와 같은 메뉴 방식을 유지하고, Go 는 프롬프트 없이도(비대화형) 같은 값을 받도록 했습니다. 비대화형(`-batch`)은 프롬프트를 띄우지 않으며, stdin 은 템플릿 인벤토리 사용 중일 때 `[인벤토리사용중]` 줄을 찍은 뒤 한 줄만 읽습니다(setup_check `-run` 이 TUI 질문으로 바꿔 답을 넣음).
- **CPU 템플릿 인벤토리는 launch 순간에만 바꾼다**(stress-ng 템플릿은 이미 개인 인벤토리를 쓰므로 그대로 launch) — 템플릿은 평소 `default_inventory`(DEFAULT) 에 두고, launch 직전 conf `username` 인벤토리(같은 이름, 없으면 이름에 username 이 들어간 것 하나 — `matchUserInventory`)로 바꿨다가 launch 응답 직후(실패해도) 되돌립니다. AWX 는 job 생성 때 인벤토리를 고정하므로 실행에 영향이 없고, 다른 사용자가 바꿔 둔 상태(DEFAULT 아님)면 덮어쓰지 않고 사람에게 묻습니다. PATCH 직후 확인 GET 으로 경합을 감지하면 남의 설정을 되돌리지 않습니다. PATCH 전에 `curSwitch` 에 기록해 두어 신호를 받으면 되돌린 뒤 종료합니다(잠금을 쥔 채 종료해 본 흐름과 겹치지 않음).
- **종료 시각은 입력 글자 그대로, survey type 만 맞춤** — 사용자가 친 `07` 같은 글자를 기억해 두고, 템플릿 survey 가 integer/float 일 때만 숫자로 바꿉니다. 재실행·다음 단계에서 이미 지났으면 대화형은 다시 묻고 비대화형은 그 묶음을 실패로 둡니다.
- **등록은 01 스크립트와 같은 방식(삭제 없음)** — yaml → scp → `custom_inventory.sh` → 새 yml → git_upload → invsync. 새 yml 은 이름이 아니라 `svr_dir` 의 *.yml 수정 시각 비교로 찾아 같은 이름 덮어쓰기도 인식하고, 정확히 1개가 아니면 실패로 봅니다.
- **접속 여부와 벤더를 한 번에** — uptime 대신 `grep -m1 vendor_id /proc/cpuinfo` 한 번으로 접속체크와 Intel/AMD 확인을 함께 합니다. 벤더는 결과표 표시용이며(템플릿이 Intel/AMD 를 구분), `unknown` 도 cpu 대상입니다.
- **감시 = 1분 로드, job 완료 직후 시작** — job 은 부하를 백그라운드로 띄우고 바로 끝나므로, launch 가 `successful` 이 된 뒤 로드가 기준 이상으로 오르는지를 본다. 목록이 비면 즉시 끝나 불필요하게 기다리지 않습니다.
- **cpu 는 템플릿 하나, 묶음 하나** — CPU 템플릿 안에서 Intel/AMD 를 구분하므로 접속된 전체를 `${user}_cpu.yaml` 한 묶음으로 등록·launch 합니다(2026-11-06 변경, 옛 `cpu_intel_*`/`cpu_amd_*` 키는 설정 오류).
- **재실행은 남은 호스트만, 새 yaml** — 미실행 호스트만 `${user}_retryN_<묶음>.yaml` 로 다시 등록해 같은 템플릿을 실행합니다. job 실패·접속불가·DNS실패는 재실행하지 않습니다.
- **대화형/비대화형 분리** — 화면 갱신·재실행 질문은 stdin·stdout 이 모두 TTY 일 때만 하고, 그 외에는 한 줄 요약과 `-retry` 로 동작합니다.
- **종료코드** — 0 전부 실행, 1 미실행·실패 있음(접속불가·DNS실패만 있으면 0), 2 설정·입력 오류. 호출자(setup_check)가 코드만 보고 분기할 수 있게 했습니다.
- **테스트 대역 구성** — 단위 테스트는 httptest fake AWX·가짜 ssh/scp(`sshBin`/`scpBin` 변수)·`lookupHost`·`nowFn`/`sleepFn`/`pollUnit` 교체로 시간·네트워크 없이 돌립니다. 랩 e2e 는 가짜 AWX 가 실제 gossh 로 부하(busy loop)를 걸어 감시 경로까지 실제로 검증합니다.
