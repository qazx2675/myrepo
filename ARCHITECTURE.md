# ARCHITECTURE.md

| 폴더/파일 | 역할 |
|---|---|
| `main.go` | 진입점·흐름 제어: 플래그 파싱, conf/입력/장애목록 로드, 접속체크(`checkVendors`, gossh `vendor_id`), 결과 초기값(`initResults`), 단계별 `runStage`(묶음 분할 `split` → `launchBatch` → 감시 → `askRetry` 재실행), 결과표(`resultLines`)·종료코드(`hasProblem`) |
| `config.go` | `Config` 구조체와 `key = value` 파서(기본값 포함), 단계별 필수 키 검증 `validate`, user 결정(`-user` → `STRESS_USER` → `$USER`), conf 탐색(`./conf` → `~/.stress-test` → `<바이너리 위치>/conf`), `key=value, ...` 분해, 이력 파일 기록 |
| `input.go` | 입력 파일 파싱(`parseHosts`: 1필드/12·13필드 줄 판정, DNS·MAC 생성), error_list.txt 파싱(`parseErrorList`), `-steps` 파싱, 단계 결과 값 상수(`실행` 등) |
| `gossh.go` | gossh 경로 결정(conf → `GOSSH` → PATH), 임시 호스트 파일로 gossh 실행·결과 파일(`_res_off` 등) 수거, 출력(`host: 내용`) 파싱, 벤더(`parseVendors`)·1분 로드(`parseLoads`) 해석 |
| `register.go` | 인벤토리 등록: yaml 생성 → scp → ssh `custom_inventory.sh` → `svr_dir` 새 yml 1개 확인(수정 시각 비교) → (git_dir) `.git_upload.sh` → invsync |
| `awx.go` | AWX REST(`/api/v2`) 최소 클라이언트: 템플릿 조회(ID/이름), launch, job poll·stdout tail, 인벤토리 소스 ID 결정·PATCH·update·완료 대기 (awxkit 에서 필요한 부분만 복사) |
| `monitor.go` | 감시 루프 `monitorLoad`: interval 마다 로드 확인, 기준 이상 호스트 제거, 목록 비면 조기 종료, 대화형은 ANSI 화면 갱신/비대화형은 한 줄 요약, 한글 폭 맞춘 표 |
| `awx_test.go` | fake AWX(httptest)·가짜 ssh/scp 로 `launchBatch` 성공·job 실패, 새 yml 없음, 고정 소스 invsync 검증 |
| `logic_test.go` | 파서(입력·장애목록·steps·conf·벤더·로드)·결과표·yaml 이름·`_Nea` 경고·감시 루프·화면 단위 테스트 |
| `go.mod` | `go 1.20`, 표준 라이브러리만 사용 (vendor 불필요) |
| `stress_run.sh` | bash 래퍼: 상단 `GOSSH`/`USER_ROUTE` 변수, user 메뉴, `a/b` 단계 입력, 바이너리가 없으면 `build.sh`, 나머지 인자 그대로 전달 |
| `build.sh` | `CGO_ENABLED=0 go build -o stress-test .` |
| `stress-test` | os8(Rocky 8) 빌드 완료 바이너리 (커밋 대상, os6 빌드 없음) |
| `conf/sample_setting.conf` | 설정 파일 샘플 (모든 키) |
| `test/fake_awx/` | 가짜 AWX 서버(별도 go 모듈): launch 시 대상 서버에 gossh 로 busy loop 부하 생성. `-skipfile`/`-skiponce`(미실행 재현), `-failtmpl`(job 실패) |
| `test/fake_repo/` | 가짜 `custom_inventory.sh`, `.git_upload.sh` |
| `test/fake_user/` | `USER_ROUTE` 대역 (`info_mn.sh`, `info.sh`, 래퍼 시험용 `stress_run_wrap.sh`) |
| `test/e2e.sh`, `test/e2e_c.sh` | 랩(.58) 시나리오 시험(비대화형 / TTY 대화형) |
| `test/tester_setting.conf`, `test/hosts_*.txt`, `test/error_list.txt` | e2e 입력 샘플 |
| `.github/workflows/ci.yml` | gofmt·vet·test·bash -n·shellcheck |
| `README.md` / `PR_CHECKLIST.md` | 사용 설명 / 변경 점검 목록 |
| `계획서.md` | 인터뷰로 확정한 사양 |

수정 요청별로 볼 곳:

- **"입력 형식(필드 수·hostname 변환·MAC/IP 규칙) 변경"** → `input.go` `parseHosts` + `logic_test.go` + README 2.3
- **"장애 목록 규칙 변경"** → `input.go` `parseErrorList` (setup_check `-er` 와 같은 규칙 유지)
- **"conf 키 추가·기본값 변경"** → `config.go`(`Config`, `setters`, `parseConfig` 기본값, `validate`) + `conf/sample_setting.conf` + README 3.3
- **"벤더 판정·접속체크 명령 변경"** → `gossh.go` `vendorCmd`/`parseVendors`, `main.go` `checkVendors`/`initResults`
- **"감시 기준·화면 변경"** → `monitor.go`, 로드 명령은 `gossh.go` `loadCmd`/`parseLoads`
- **"등록 방식(yaml/scp/custom_inventory/git/invsync) 변경"** → `register.go`, invsync 세부는 `awx.go`
- **"템플릿 launch 변수 전달 방식 변경"** → `main.go` `launchBatch` + `config.go` `TemplateConf`
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
  [0] 인자 검증(-steps/-hosts/-retry) → user 결정 → conf 탐색·로드·validate(단계별 필수 키)
      입력 파일 parseHosts (형식 오류 → exit 2) → 장애 목록(-er / ./error_list.txt)을 비고로
      gossh 경로 결정 → output_dir, 임시 작업 폴더, gossh 로그
      대화형 판정 = !-batch && stdin·stdout 이 터미널

  [1] checkVendors  (DNS실패 제외)
      gossh -script 'grep -m1 vendor_id /proc/cpuinfo'
        ├ 출력 없음 / _res_off·_res_refsed·_res_cancel ─→ 접속불가
        ├ GenuineIntel ─→ Intel   AuthenticAMD ─→ AMD   그 외 ─→ unknown
      initResults: DNS실패 / 접속불가 / (cpu & unknown → '-') / 나머지 = 미실행
      checkCPUTemplates: 대상 있는 벤더의 템플릿 conf 누락 → exit 2

  [2] 선택한 단계 순서대로 runStage(stage)       (앞 단계 감시가 끝난 뒤 다음 단계)
      pending = 결과가 '미실행' 인 호스트
      round 0,1,2 ...
        split: stress = 전체 1묶음 / cpu = Intel 묶음 → AMD 묶음 (없는 벤더 생략)
        묶음마다 launchBatch
          register (yaml → scp → custom_inventory.sh → 새 yml 1개 → [git_upload] → invsync)
          resolveTemplate → launch(hostname_key = 줄바꿈 hostname, + extra_vars)
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
      exit 0 (미실행/실패 없음) / 1 (미실행 또는 실패 있음) / 2 (설정·입력 오류, [0]~[1] 단계)
```

## 핵심 설계 결정

- **단일 정적 바이너리, 표준 라이브러리만** — 폐쇄망·Go 없는 환경에서 빌드 없이 쓰려고 vendor 대신 빌드된 os8 바이너리를 커밋합니다. awxkit·setup_check 의 필요한 코드(AWX 클라이언트, conf 파서, gossh 처리, `-er` 규칙)는 **복사**해서 이 폴더 안에서 단독 빌드되게 했습니다.
- **user 선택은 bash 래퍼, Go 에는 `-user`** — AWX 01 스크립트와 같은 메뉴 방식을 유지하고, Go 는 프롬프트 없이도(비대화형) 같은 값을 받도록 했습니다. 비대화형 호출자(setup_check)는 `-batch` 로 stdin 을 쓰지 않는 규약을 따릅니다.
- **등록은 01 스크립트와 같은 방식(삭제 없음)** — yaml → scp → `custom_inventory.sh` → 새 yml → git_upload → invsync. 새 yml 은 이름이 아니라 `svr_dir` 의 *.yml 수정 시각 비교로 찾아 같은 이름 덮어쓰기도 인식하고, 정확히 1개가 아니면 실패로 봅니다.
- **접속 여부와 벤더를 한 번에** — uptime 대신 `grep -m1 vendor_id /proc/cpuinfo` 한 번으로 접속체크와 Intel/AMD 구분을 함께 합니다. `unknown` 은 cpu 단계에서만 제외합니다.
- **감시 = 1분 로드, job 완료 직후 시작** — job 은 부하를 백그라운드로 띄우고 바로 끝나므로, launch 가 `successful` 이 된 뒤 로드가 기준 이상으로 오르는지를 본다. 목록이 비면 즉시 끝나 불필요하게 기다리지 않습니다.
- **cpu 는 벤더별 묶음, 감시는 한 번** — Intel·AMD 를 각각 등록·launch 하되 감시는 launch 가 모두 끝난 뒤 합쳐서 한 번만 합니다.
- **재실행은 남은 호스트만, 새 yaml** — 미실행 호스트만 `${user}_retryN_<묶음>.yaml` 로 다시 등록해 같은 템플릿을 실행합니다. job 실패·접속불가·DNS실패는 재실행하지 않습니다.
- **대화형/비대화형 분리** — 화면 갱신·재실행 질문은 stdin·stdout 이 모두 TTY 일 때만 하고, 그 외에는 한 줄 요약과 `-retry` 로 동작합니다.
- **종료코드** — 0 전부 실행, 1 미실행·실패 있음(접속불가·DNS실패만 있으면 0), 2 설정·입력 오류. 호출자(setup_check)가 코드만 보고 분기할 수 있게 했습니다.
- **테스트 대역 구성** — 단위 테스트는 httptest fake AWX·가짜 ssh/scp(`sshBin`/`scpBin` 변수)·`lookupHost`·`nowFn`/`sleepFn`/`pollUnit` 교체로 시간·네트워크 없이 돌립니다. 랩 e2e 는 가짜 AWX 가 실제 gossh 로 부하(busy loop)를 걸어 감시 경로까지 실제로 검증합니다.
