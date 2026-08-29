# survey — 자산 조사 스크립트

리눅스(RHEL) 서버의 자산대장(표2) 항목을 `gossh` 병렬 접속으로 수집해
**엑셀에 바로 붙여넣을 수 있는 탭 구분 결과 파일**로 저장하는 도구.

조사 대상은 **표1(자산양식) 텍스트의 hostname 열 전체**다. 별도 목록 파일을 손으로 만들지 않는다.

설정은 **`conf/conf.toml` 하나만** 사용한다(실행 파일 옆 `conf/` 디렉터리). 옵션으로 경로를 지정하지 않는다.

결과 컬럼:

```
hostname	위치	상태	설정값	인프라망	appl설정유무	특이사항
```

- 표1의 모든 hostname은 접속 실패와 무관하게 **1행씩** 출력된다(실패 시 사유는 `특이사항`).
- 셀 구분은 **오직 탭**. 필드 내부의 탭/개행은 스페이스로 치환된다.

**출력 파일은 최대 3개** (호스트는 파일 간 중복되지 않음):
- `result_YYYYMMDD_HHMM.tsv` — 표1 전체 조사 결과 (항상 생성)
- `result_vm_YYYYMMDD_HHMM.tsv` — ESXi 로 판별된 호스트의 VM 조사 결과 (ESXi ≥ 1대, 행이 있을 때만)
- `result_sdc_YYYYMMDD_HHMM.tsv` — 인프라망 값이 `SDC` 인 호스트 (다른 파일에서 빠져 여기로 이동, 있을 때만)

`run_survey.sh` 는 위 조사 앞뒤로 **자산 필터링**과 **A 서버 재조사·병합**까지 처리한다(2·3장).
`survey` 바이너리만 단독 실행하면 필터/재조사 없이 conf 의 `asset_file` 을 그대로 조사한다.

---

## 1. 빌드 및 설치 방법

### 사전 요구

- Go 1.20 이상 (`go.mod` 는 `go 1.20`). **RHEL 6 대상 바이너리는 반드시 Go 1.20.x 로 빌드**
  (Go 1.21+ 런타임은 커널 3.2+ 를 요구 → RHEL 6 커널 2.6.32 에서 실행 불가).
  RHEL 6 정적 빌드: `CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "-s -w" -o survey ./cmd/survey`
- `gossh` (pdsh 유사 병렬 실행 툴) — 경로는 `conf.toml` 의 `[gossh].bin` 에 지정
- 인프라망 판별 스크립트 — `scripts/infra_survey.sh` 를 사내 규칙에 맞게 교체

> 이 프로젝트는 **표준 라이브러리만** 사용한다. 외부 모듈 의존성이 없으므로
> `go mod download` / `vendor` 없이 `go build` 만으로 폐쇄망에서 빌드된다.

### 온라인 빌드

```bash
git clone <repo>
cd <repo>/.claude/HPC/조사/survey
go build -o survey ./cmd/survey
```

### 폐쇄망(오프라인) 빌드

```bash
# 저장소를 zip 으로 내려받아 폐쇄망으로 반입 후
unzip <repo>.zip
cd <repo>/.claude/HPC/조사/survey
go build -o survey ./cmd/survey      # 네트워크 불필요 (stdlib only)
```

### 설치

빌드된 `survey` 와 아래 파일을 같은 디렉터리 구조로 둔다.

```
survey
run_survey.sh
conf/conf.toml
scripts/infra_survey.sh
```

`survey` 는 자신이 있는 디렉터리의 `conf/conf.toml` 을 자동으로 읽는다.

### 폐쇄망 증분 업데이트

설치 디렉터리(예: `/root/HPC/조사`)를 통째로 덮어쓰지 않고 **바뀐 파일만** 갱신한다.

```bash
# 새 버전 zip 을 폐쇄망에 반입해 다른 경로에 풀고
unzip survey-new.zip -d /tmp/survey-new
/tmp/survey-new/.../survey/update.sh /root/HPC/조사
# -> 변경/신규 파일만 복사, 오래된 *.go 제거
(cd /root/HPC/조사 && go build -o survey ./cmd/survey)
```

- `conf/conf.toml`, `result_*.tsv`, `asset_list.txt` 는 **건드리지 않는다**.
  단 새 배포본의 `conf/conf.toml` 이 다르면 `diff` 를 출력하고 "직접 반영 필요" 경고를 낸다.
- 같은 내용 파일은 건너뛴다(멱등). 대상에만 있는 오래된 `cmd/**/*.go` 는 빌드 깨짐 방지를 위해 제거한다.

---

## 2. 사용 방법

### 사전작업 (RHEL)

1. 표1(자산양식)을 터미널에 출력해 텍스트 파일로 저장한다. 예: `asset_list.txt`
   ```
   자산ID<TAB>hostname<TAB>상태<TAB>위치
   A-001<TAB>web01<TAB>운영<TAB>서울 A센터
   A-002<TAB>db01<TAB>운영<TAB>부산 B센터
   ```
   - **탭(`\t`) 구분**. 상태·위치에 공백이 들어가도 됨(탭으로만 나눔). 첫 행이 헤더면 자동 스킵.
   - 조사할 hostname은 이 파일에 넣기만 하면 된다(중간 목록 파일 불필요).
   - `examples/asset_list.sample.txt` 참고.
2. `conf/conf.toml` 의 `asset_file` 을 이 파일 경로로 맞춘다.

### 실행

```bash
./run_survey.sh
```

- 같은 폴더에 `result_YYYYMMDD_HHMM.tsv` 가 생성된다.
- 진행/요약(`조사 대상 N대`, `성공 N / 실패 M`)은 화면(stderr)에 출력된다.

### 활용

결과 파일(들)을 열어 전체 복사 → 엑셀에 붙여넣으면 탭 구분으로 셀이 자동 분리된다.
ESXi 가 있었으면 `result_vm_*.tsv` 도 같이 생성된다.

### A·B 서버 연동 (`run_survey.sh` 전용)

B 서버(전용, 팀만 접근)에서 전체를 조사하고, **`타임아웃`/`접속불가`** 로만 나온 소수를
A 서버(공용, 일부 대상망 전용)에서 1회 재조사해 결과를 합친다. `DNS 미등록` 은 재조사 대상이 아니다.

**전제**: A·B 가 `[server_a].dir` 을 **auto mount 로 공유**한다(양쪽에서 같은 절대경로).
파일은 B 가 그 경로에 직접 만들고, **실행만** `ssh` 로 A 에서 한다(대상망 접근이 A 에서만 되므로).

1. 공유 디렉토리(예: `/user/autofs1/조사`)에 B 것과 함께 **RHEL 6 정적 빌드** 바이너리를
   `[server_a].bin` 이름(예: `survey-rhel6`)으로 둔다. `conf/`·`scripts/` 는 B 것을 공유한다.
2. `conf/conf.toml` 의 `[server_a]` 를 채우고 `enabled = true`.
3. B→A 는 `[server_a].user`(기본 `root`) 로 **패스워드리스 SSH** 가 돼야 한다(scp 는 불필요).
4. 동작: run_survey.sh 가 `dir` 아래 임시 폴더(`.resurvey.XXXXXX/`)에 재조사 목록과
   **`[input].asset_file` 만 그 목록으로 바꾼 conf 사본**을 만들고,
   `ssh A "cd .resurvey.XXXXXX && ./survey-rhel6"` 로 실행 → 결과를 읽어 병합 후 임시 폴더 삭제.

`./run_survey.sh` 한 번이면 필터 → B 조사 → A 재조사 → 병합까지 끝나고
최종 `result_*.tsv` (+ `_sdc_` / `_vm_`) 만 이 폴더에 남는다. 파일 간 hostname 중복 없음.
A 실행이 안 되면 경고만 내고 **B 결과만으로** 최종 파일을 만든다(재조사분은 원래 행 유지).

---

## 3. 옵션별 상세 설명

### 실행

`./run_survey.sh` — 인자 없음. `survey` 바이너리를 직접 실행해도 된다(`./survey`).
설정은 항상 실행 파일 옆 `conf/conf.toml` 을 읽는다.

### `conf/conf.toml` 항목

| 키 | 설명 |
|---|---|
| `[input].asset_file` | 표1 텍스트 경로 (탭 구분) |
| `[gossh].bin` | `gossh` 실행 파일 경로 |
| `[gossh].concurrency` | `gossh` 동시 실행 수(`-c`). 기본 `4000`. **설정값(appl) 조사에만 적용** |
| `[gossh].timeout` | `gossh` 타임아웃 초(`-t`). 양의 정수. 지우면 `gossh` 기본값. 두 조사(설정값·인프라망) 모두 적용 |
| `[gossh].retry_timeout` | 재조사(일반서버의 `DNS 미등록`/`타임아웃`, VM 제외) 때 쓸 **긴** 타임아웃 초. 지우면 `timeout` 그대로 |
| `[gossh].extra_args` | `gossh` 에 넘길 추가 플래그(공백 구분). 비워도 됨 |
| `[scripts].config_value` | 설정값 조사: `gossh ... -script "<이 값>"` 으로 원격 실행. **변경 시 이 값만 수정** |
| `[scripts].infra_net` | 인프라망 조사: `gossh -w <hosts> -script "bash <이 값>"` 으로 원격 실행. 조사 대상 호스트에 배포된 스크립트 경로(또는 명령). 비우면 인프라망 열 공란 |
| `[scripts].infra_regex` | gossh 출력값에 적용하는 정규식(캡처 그룹 1). `\s` 는 공백·탭 모두 매칭. **여러 줄이면 각 줄에 적용해 처음 매칭되는 줄**을 쓴다(`FAIL ldap_site` + `INFO ldap infra` → `infra`). 비우면 첫 줄 그대로. **매칭 안 되면 아래 fallback 재조사**. 예: `'^INFO\s+\S+\s+(\S+)'` |
| `[scripts].infra_fallback_cmd` | `infra_regex` 매칭 실패 호스트에 다시 실행할 gossh 커맨드. 비우면 재조사 안 함. 예: `"cat /etc/openldap/ldap.conf \| grep -i binddn"` |
| `[scripts].infra_fallback_regex` | fallback(binddn) 출력에서 값 추출용 정규식(캡처 그룹 1). 비우면 첫 줄 전체. 기본 `'uid=([^,\s]+)'` → binddn 의 `uid=` 값. `uid=` 없는 binddn 은 매칭 실패 → 원본 줄 기록(SDC 자동 분리 안 됨) |
| `[[mountpoint]].name` | 설정값 `이름:/경로` 에서 `:` 앞부분(마운트 대상 이름) |
| `[[mountpoint]].location` | 그 이름이 정상적으로 위치해야 하는 곳. 표1의 `위치` 와 비교 |
| `[asset_filter].source` | **(run_survey.sh 전용)** 원본 표1 경로. include/exclude 로 걸러 `[input].asset_file` 을 만든다. 비우면 필터 생략 |
| `[asset_filter].include` / `.exclude` | `egrep -E` 패턴. `include` 매칭 줄만 남기고 `exclude` 매칭 줄을 뺀다. OR 은 `\|`. 특수문자는 따옴표로 감쌀 것 |
| `[server_a].enabled` | **(run_survey.sh 전용)** `true` 면 B 의 `타임아웃`/`접속불가` 호스트를 A 서버에서 1회 재조사 |
| `[server_a].host` / `.user` | A 서버 주소 / 패스워드리스 SSH 계정 (기본 `root`) |
| `[server_a].dir` | **A·B 가 auto mount 로 공유하는 디렉토리(같은 절대경로).** 이 아래에 A 용 바이너리와 `conf/` 가 있어야 함 |
| `[server_a].bin` | A(RHEL6)에서 돌릴 바이너리 파일명(`dir` 아래). 기본 `survey`. B 것과 안 겹치게 예: `survey-rhel6` |

### 판정 규칙

- **설정값**: `gossh` 로 받은 auto.appl 매칭 행(`/` 로 시작)의 마지막 필드(`이름:/경로`).
  - 해당 호스트 출력이 아예 없거나 `/appl` 매칭 행이 없으면 → `없음`
  - 접속 실패(ssh 에러 등)면 → 공란 (사유는 특이사항 `접속불가`/`타임아웃`)
- **appl설정유무**:
  - 설정값이 `없음` 이면 → `없음`
  - 설정값에서 `:` 앞부분(이름) 분리 → `[[mountpoint]]` 에서 그 이름의 `location` 조회
  - `location == 표1의 위치` → `O`, 다르면 `X`
  - 이름이 conf 에 없으면 `X` + 특이사항 `mountpoint 미정의`
  - 접속 실패면 → 공란
- **인프라망**: `gossh -w <hosts> -script "bash <infra_net>"` → 각 호스트 출력에 `infra_regex` 적용
  (출력이 여러 줄이면 줄마다 시도해 처음 매칭되는 줄).
  - 매칭 실패(`FAIL ldap Undefined` 등) **또는** 스크립트 미배포(`no such file`) →
    `infra_fallback_cmd`(기본 binddn) 로 재조사 → `infra_fallback_regex` 적용
  - 폴백(binddn) 출력이 있으면: `infra_fallback_regex` 로 뽑은 값(예: `SDC`), 못 뽑으면 **binddn 원본 줄** 을 인프라망에 기록
  - 폴백 응답이 아예 없을 때만 `특이사항` 에:
    - 스크립트 없음 → `인프라 스크립트 없음`
    - 그 외 → `인프라 확인필요(FAIL ldap Undefined)` — 공백으로 두지 않고 원본을 남김
  - 설정값 조사와 별개의 gossh 호출이며 `-c` 는 붙지 않는다
  - VM(파일 B)에도 **동일하게** 적용된다
- **값 치환**: 인프라망 값이 정확히 `VIP` 면 `SLSI_VIP` 로 바꿔 기록한다.
- **SDC 분리**: 위에서 구한 인프라망 값이 정확히 `SDC` 면 그 호스트는 A·B 에서 빠져
  `result_sdc_*.tsv` 로만 기록된다.
- **일반서버 재조사(바이너리 내부)**: VM 이 아닌 표1 호스트가 `DNS 미등록`/`타임아웃` 이면 **1회 더** 조사한다
  (`[gossh].retry_timeout` 이 있으면 그 긴 타임아웃으로 답을 기다림). VM 은 별도 규칙(위 참고).
- **A 서버 재조사(`run_survey.sh`)**: 위 내부 재조사 후에도 `타임아웃`/`접속불가` 인 호스트만
  A 서버로 넘겨 **1회** 재조사하고, 성공하면 그 행으로 교체한다(`DNS 미등록` 제외). 2·3장 참고.
- **특이사항**: `접속불가` / `타임아웃` / `DNS 미등록` / `gossh 실행 실패` / `mountpoint 미정의` / `인프라 스크립트 없음` / `esxi`

### ESXi / VM 2차 조사

> `survey` 를 실행하는 호스트(크론 서버)가 VM 이름을 DNS/`/etc/hosts` 로 해석할 수 있어야 한다.

1. 1차 조사에서 `설정값 = 없음` 인 호스트에 `uname` 을 실행한다.
2. 출력에 `VMkernel` 이 있으면 **ESXi 물리장비**로 본다. 이 호스트는
   **인프라·설정값 조사를 하지 않고** `설정값`/`인프라망`/`appl설정유무` 를 모두 `없음`,
   `특이사항` 을 `esxi` 로 기록한다.
3. ESXi 가 1대 이상이면 각 ESXi 의 VM 을 **고정 규칙**으로 만든다:
   `<esxi_hostname>ev01`, `<esxi_hostname>ev02`, `<esxi_hostname>ev03` (conf 설정 없음, 규칙 고정)
4. 만든 VM 이름을 **`survey` 가 직접 DNS 조회**(`/etc/hosts` 포함)한다.
   - **해석되지 않는 VM(존재하지 않음)은 조사 대상에서 빠지고 어떤 행도 남기지 않는다.**
     (gossh 의 에러 메시지에 의존하지 않음 → `ev01`,`ev02` 만 있는 ESXi 는 `ev03` 이 기록되지 않음)
5. 해석되는 VM 만 1차와 **같은** `config_value` / `infra_net` / `infra_regex` / O·X 로직으로 조사한다.
   - VM 의 `위치` / `상태` 는 소속 ESXi(표1)의 값을 상속하고, O·X 판정도 그 `위치` 로 한다.
   - 해석되지만 SSH 실패(전원 꺼짐 등)면 그 VM 은 `접속불가` 행으로 남는다.
   - 어떤 ESXi 의 조사 가능한 VM 이 하나도 없으면 그 ESXi 이름으로 `접속불가` 1행을 남긴다.
   - `타임아웃` 인 VM 만 모아 **딱 1회** 재조사한다(무한 루프 방지).

---

## 4. 문서별 설명

| 파일 | 내용 |
|---|---|
| `README.md` | 본 문서 |
| `ARCHITECTURE.md` | 폴더/파일별 역할 표 (수정 지점 빠르게 찾기용) |
| `CHANGELOG.md` | 날짜순 변경 이력 |
| `PR_CHECKLIST.md` | 배포/수정 전 점검 목록 |
| `conf/conf.toml` | 실행 설정 (유일한 설정 파일) |
| `scripts/infra_survey.sh` | 인프라망 판별 스크립트 **샘플**. 조사 대상 호스트에 배포해 `gossh ... -script "bash <경로>"` 로 원격 실행됨 (교체 대상) |
| `examples/asset_list.sample.txt` | 표1(탭 구분) 샘플 |
| `update.sh` | 폐쇄망 증분 업데이트 스크립트 |
| `.github/workflows/ci.yml` | 커밋마다 build/vet/test 자동 실행 |

---

## 5. 전역 명령어로 사용하기 (선택 사항)

```bash
sudo cp survey /usr/local/bin/survey
# 또는
ln -s "$(pwd)/survey" ~/bin/survey    # ~/bin 이 PATH 에 있을 때
```

단, `survey` 는 **실행 파일 옆 `conf/conf.toml`** 을 읽으므로, 전역 설치 시에는
설치 위치(`/usr/local/bin/`)에 `conf/conf.toml` 도 함께 두거나 심볼릭 링크한다.
설치 폴더를 그대로 쓰려면 `run_survey.sh` 실행을 권장한다.

---

## 주의사항 (Disclaimer)

> 본 로그 분석 관련 스크립트 및 툴은 100% 신뢰하기보다는 참고용(보조 도구)으로
> 사용하는 것을 권장합니다.

> 이 저장소에 설정 변경 스크립트가 함께 포함될 경우: 설정 변경 후 랜덤한 서버 몇 개를
> 직접 확인하여 실제로 변경되었는지 검증하십시오.

---

## 참고

- CI(`.github/workflows/ci.yml`)의 `working-directory` 는 이 폴더가
  저장소의 `.claude/HPC/조사/survey` 에 있다고 가정한다. 이 폴더 자체가 저장소 루트가 되면
  `defaults.run.working-directory` 블록을 제거한다.
- `gossh` 출력은 pdsh 규칙(`hostname: 결과`)으로 파싱한다. 실제 출력 형식이 다르면
  `cmd/survey/collect.go` 의 `parsePdshLine` / `detectError` 를 조정한다.
- **원격 로그인 셸이 csh 여도 무관**: `infra_net` 은 `bash <경로>` 로 실행되고,
  `config_value`(`grep -v '#'` 등)·`uname`·binddn 커맨드는 csh 에서도 그대로 동작한다.
  인프라망 값이 안 뽑히면 셸 문제가 아니라 `infra_regex` 패턴 문제이니 실제 출력값과 맞춰본다.
- 인프라망 출력값(`hostname:` 뒤)은 앞뒤 공백이 제거된 뒤 `infra_regex` 에 들어간다.
  `\s` 는 공백·탭 모두 매칭하므로 구분자가 공백이든 탭이든 같은 패턴을 쓴다.
