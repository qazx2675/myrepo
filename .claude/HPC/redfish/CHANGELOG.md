# CHANGELOG.md — redfish (biostool)

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
