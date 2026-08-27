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
- `result_sdc_YYYYMMDD_HHMM.tsv` — 인프라망 값이 `SDC` 인 호스트 (A·B 에서 빠져 여기로 이동, 있을 때만)

---

## 1. 빌드 및 설치 방법

### 사전 요구

- Go 1.21 이상
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
| `[gossh].extra_args` | `gossh` 에 넘길 추가 플래그(공백 구분). 비워도 됨 |
| `[scripts].config_value` | 설정값 조사: `gossh ... -script "<이 값>"` 으로 원격 실행. **변경 시 이 값만 수정** |
| `[scripts].infra_net` | 인프라망 조사: `gossh -w <hosts> -script "bash <이 값>"` 으로 원격 실행. 조사 대상 호스트에 배포된 스크립트 경로(또는 명령). 비우면 인프라망 열 공란 |
| `[scripts].infra_regex` | gossh 출력 `hostname: 출력값` 의 **출력값**에 적용하는 정규식(캡처 그룹 1). 비우면 출력값 그대로. **매칭 안 되면 아래 fallback 으로 재조사**. 예: `'\[([^\]]+)\]'` → `INFO⇥LDAP⇥[infra]` 에서 `infra` |
| `[scripts].infra_fallback_cmd` | `infra_regex` 매칭 실패 호스트에 다시 실행할 gossh 커맨드. 비우면 재조사 안 함. 예: `"cat /etc/openldap/ldap.conf \| grep -i binddn"` |
| `[scripts].infra_fallback_regex` | fallback 출력에서 값 추출용 정규식(캡처 그룹 1). 비우면 첫 줄 전체. 예: `'ou=([^,[:space:]]+)'` → binddn 의 `ou=SDC` 에서 `SDC` |
| `[[mountpoint]].name` | 설정값 `이름:/경로` 에서 `:` 앞부분(마운트 대상 이름) |
| `[[mountpoint]].location` | 그 이름이 정상적으로 위치해야 하는 곳. 표1의 `위치` 와 비교 |

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
- **인프라망**: `gossh -w <hosts> -script "bash <infra_net>"` → 각 호스트 출력값에 `infra_regex` 적용.
  - `infra_regex` 매칭 실패(예: `FAIL⇥LDAP⇥확인필요` / `undefined`) → `infra_fallback_cmd` 로 재조사 →
    그 출력에 `infra_fallback_regex` 적용 (예: `binddn` 값의 `ou=` 부분)
  - 설정값 조사와 별개의 gossh 호출이며 `-c` 는 붙지 않는다
- **SDC 분리**: 위에서 구한 인프라망 값이 정확히 `SDC` 면 그 호스트는 A·B 에서 빠져
  `result_sdc_*.tsv` 로만 기록된다.
- **특이사항**: `접속불가` / `타임아웃` / `DNS 미등록` / `gossh 실행 실패` / `mountpoint 미정의` / `인프라 스크립트 없음` / `esxi`

### ESXi / VM 2차 조사

1. 1차 조사에서 `설정값 = 없음` 인 호스트에 `uname` 을 실행한다.
2. 출력에 `VMkernel` 이 있으면 **ESXi** 로 보고, 1차 결과의 `특이사항` 에 `esxi` 를 남긴다.
3. ESXi 가 1대 이상이면 각 ESXi 의 VM 을 **고정 규칙**으로 만들어 2차 조사한다:
   `<esxi_hostname>ev01`, `<esxi_hostname>ev02`, `<esxi_hostname>ev03` (conf 설정 없음, 규칙 고정)
4. VM 도 1차와 **같은** `config_value` / `infra_net` / `infra_regex` / O·X 로직으로 조사한다.
   - VM 의 `위치` / `상태` 는 소속 ESXi(표1)의 값을 상속하고, O·X 판정도 그 `위치` 로 한다.
   - **DNS 미등록** VM 은 `result_vm` 에 행을 만들지 않는다.
   - 어떤 ESXi 의 VM 이 모두 행이 안 만들어지면 그 ESXi 이름으로 `접속불가` 1행을 남긴다.
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
