# 구현 설계 명세 (모든 구현 단계 공통 기준)

계획서: `../계획서.md`, 결정: `../요구사항.md`. 이 문서는 구현 에이전트가 같은 규약을 쓰도록 정한 인터페이스다.

## 0. 공통 제약

- **bash 4.1 호환 + sed/awk/grep/coreutils 만** (python·perl 금지, `local -n`·`${var^^}` 등 4.3+ 기능 금지. `declare -A`, `mapfile` 는 4.0 이므로 허용).
- 입력은 **stdin 에서 읽는다** (`read -r -p`, 비밀번호는 `read -r -s`) → 파이프로 테스트 가능. tty 가 아니면 프롬프트를 stderr 로 출력.
- 색: tty 이고 `NO_COLOR` 가 없을 때만. 정상=초록 `\033[32m`, 경고=노랑 `33`, 오류=빨강 `31`, 안내=시안 `36`, 변수명=굵게 `1`, 예시=회색 `90`. 접두 `[O]` `[!]` `[X]` `[i]`.
- 비밀번호(secret) 값은 **화면·로그·`.bak` 안내·요약 어디에도 출력 금지**(마스킹 `****`). 소스에 평문 기록 + 해당 파일 `chmod 600` + "저장소 커밋 금지" 경고 1줄.
- 현장 사본은 항상 **LF 로 정규화**(`tr -d '\r'`) 후 처리. 수정 전 `<파일>.bak.<YYYYmmddHHMMSS>` 백업, 문법 검사(`bash -n`) 실패·앵커 오류·마스킹 위반 시 **자동 원상복구**, `--undo` 는 가장 최근 백업 복원.
- 임시 파일은 `mktemp` + `trap` 정리. 대상 파일을 쓸 때는 임시파일 → `cat >` (권한·inode 유지).

## 1. manifest (`vars.manifest`)

`|` 구분 11필드, `#` 주석·빈 줄 무시, 설명 안 줄바꿈은 `\n`, 값에 `|` 금지.

```
name|target|kind|role|group|required|desc|example|check|since|renamed_from
```

| 필드 | 값 |
|---|---|
| name | 변수명 (`[A-Za-z_][A-Za-z0-9_]*`) |
| target | `sh:<상대경로>` (소스의 `name="..."` 줄을 치환) 또는 `conf:<상대경로>` (`name=값` 줄) |
| kind | `path` `host` `secret` `text` |
| role | `common` 또는 쉼표 목록(`os8,os6`) — 가이드가 고른 역할과 일치하거나 `common` 인 변수만 질문 |
| group | `-` / `xor:<id>` (같은 id 중 **하나만** 채움) / `dep:<다른변수>` (그 변수가 채워졌을 때만 질문) |
| required | `y` / `n` |
| desc | 무엇을 설정하는지·정확한 동작 |
| example | 예시 (경로면 `/path/config_check.sh` 형태) |
| check | `file` (파일 존재) / `dir` (디렉터리 존재) / `host` (형식) / `probe` (host 형식 + 연결 점검 훅) / `none` |
| since | 이 변수가 추가된 버전 (예: `1.0.0`) |
| renamed_from | 이전 변수명 (없으면 `-`) → 업데이트 시 기존 값 자동 이전 |

`xor` 그룹은 가이드가 **상황 질문**으로 하나만 고르게 한다: manifest 에 `#@xor <id>|<질문>|<변수1>:<선택지 설명>|<변수2>:<선택지 설명>` 주석 지시자로 질문 문구를 둔다.
마스킹 판정 키워드는 `#@guard <ERE>` 지시자(여러 개 가능)로 추가한다 (기본: `^[[:space:]]*#` 로 시작하는 주석 줄, `담당자`, `감사합니다`, `점검부탁`).
`#@meta setup=<상대경로|->` 로 가이드가 마지막에 이어서 실행할 `setup.sh` 를 지정한다 (없거나 파일이 없으면 건너뜀). `#@meta project=<이름>` `#@meta version=<버전>` 도 둔다.

## 2. 공용 함수 (`lib_common.sh`, 가이드·패치 스크립트가 source / 합쳐서 포함)

생성기는 이 파일을 최종 스크립트에 **그대로 인라인**한다 (자체포함). 함수 계약:

| 함수 | 계약 |
|---|---|
| `ui_init` | 색 변수·`ok/warn/err/info` 출력 함수 설정 |
| `lf_normalize <file>` | CR 제거 (있었으면 info 1줄) |
| `backup_file <file>` | `.bak.<ts>` 생성, 경로 echo |
| `restore_latest <file>` | 가장 최근 `.bak.*` 로 복원 (`--undo`) |
| `manifest_load <file\|stdin블록>` | 전역 배열(`M_NAME[] M_TARGET[] …`) 채움, 지시자(`GUARDS` `XOR_Q` `META`) 파싱 |
| `target_get <var>` / `target_set <var> <value>` | `sh:`/`conf:` 형식에 맞게 현재값 읽기·치환(값의 `& \ / "` 안전 이스케이프, 선언 줄 없으면 오류 코드 2) |
| `check_value <idx> <value>` | check 종류별 정적 검증, 결과 `OK` 또는 `이유 문자열` |
| `ask_var <idx>` | 설명·예시 echo → 현재값 표시 → `read -p`(secret 은 `-s`) → Enter 면 유지 → 검증 실패 시 재입력/건너뛰기(`s`) |
| `run_guide <role>` | 역할 필터 → xor/dep 처리 → 변수별 질문 → 검증표 → 요약 y/n → 적용 |
| `summary_table` | 변수별 `정상/이상(이유)` 표 (secret 은 마스킹) |
| `confirm <질문>` | y/n 반복 질문 |

## 3. 가이드 (`setup_guide.sh`)

```
bash setup_guide.sh [--role os8|os6|common] [--dir <프로젝트루트>] [--no-setup] [--yes]
```
흐름: 소개·Disclaimer → (빈 변수 ≤5 이면 `가이드를 실행하시겠습니까? (y/n)`, n 이면 변수 목록·위치만 출력하고 종료) → 역할 선택 → 대상 파일 LF 정규화·백업 → 변수 질문 → 검증표(이상 변수만 재입력/건너뛰기) → 변경 요약 y/n → 적용(secret 있으면 chmod 600 + 경고) → `#@meta setup` 이 있으면 `setup.sh 를 이어서 실행하시겠습니까? (y/n)` → 최종 요약 + Disclaimer("설정 변경 후 랜덤 서버 몇 대에서 실제 변경 확인"). 종료코드: 0 정상, 1 사용자 중단/실패(복구됨), 2 인자·파일 오류.
`--yes` 는 확인 질문에 y (테스트용), 값 질문은 stdin 그대로.
"빈 변수" = manifest 의 변수 중 target 에서 현재 값이 빈 것의 개수.

## 4. 업데이트 스크립트 (`update_v<ver>.sh`, 생성물) 와 엔진 (`update_engine.sh`)

```
bash update_v<ver>.sh [<대상 파일>...] [--dir <루트>] [--undo] [--dry] [--yes] [--role ...]
```
스크립트 구성(위에서 아래): 헤더·버전 → `lib_common.sh` 인라인 → 엔진 → `#__MANIFEST_BEGIN__ … #__MANIFEST_END__` (최신 manifest) → `#__SPEC_BEGIN__ … #__SPEC_END__` (변경 명세) → main.

### 변경 명세 형식
```
@file <상대경로>
@change <id> <op>            # op: insert_after | insert_before | replace_line | replace_range
@anchor <ERE>                # 코드 줄과 정확히 1곳 일치해야 함
@anchor_end <ERE>            # replace_range 만: 끝 줄(앵커 이후 첫 일치)
@text
...삽입·치환할 줄(들)...
@endtext
```
- 앵커가 0곳/2곳 이상이면 **중단**하고 `앵커 id, 일치 줄 수, 줄 번호 목록` 을 출력한다.
- `replace_*` 로 **사라지는 원본 줄**이 마스킹 가드(주석·코멘트 키워드)에 걸리면 중단한다 → 대상 파일 불변.
- 이미 적용된 변경은 `@text` 첫 줄이 이미 앵커 주변에 있으면 건너뜀(멱등: 이미 적용됨 표시).
- 모든 변경 적용 후: `bash -n` (sh 대상) → 성공하면 원본을 바꾸고, 실패하면 복구.
- 적용 후 **변수 처리**: ① manifest 의 `renamed_from` 이 대상에 있으면 값 이전 ② 이번에 추가된 변수(= 대상에 선언이 없어 명세로 삽입된 변수, 또는 값이 비고 since == 이 버전) 만 가이드 질문(`run_guide` 의 부분 집합) ③ 이미 값이 있는 변수는 건드리지 않음 ④ manifest 에서 사라진 변수는 유지 + 경고.
- 출력 끝: `주석·코멘트 문구 변경 0건` 같은 검증 요약, 백업 경로, `--undo` 안내, Disclaimer.

## 5. 생성기 (`make_update.sh`)

```
bash make_update.sh --old <이전 소스> --new <새 소스> --manifest <vars.manifest> --version <v> [--file <상대경로>] --out <update_v<ver>.sh>
```
`diff` 로 이전→신규의 추가/삭제/변경 hunk 를 구한 뒤 명세 후보를 만든다: 앵커는 hunk 바로 앞(없으면 뒤) **코드 줄 중 대상 파일 안에서 유일한 줄**을 고른다(후보 3개를 `#candidates` 주석으로 병기). 생성 후 **사람 검토용 요약**(변경 id·앵커·삽입 줄 수·가드 위반 여부)을 출력하고, 앵커가 유일하지 않으면 경고. 출력 스크립트는 `lib_common.sh` + `update_engine.sh` + manifest + 명세를 합친 단일 파일이고 크기·줄 수·끝줄 마커(`#__END_OF_UPDATE__`)로 붙여넣기 잘림을 감지한다.

## 6. 디렉터리·산출물 (개발 트리)

```
구현/
├─ 설계명세.md
├─ templates/        # 스킬에 들어갈 틀
│   ├─ lib_common.sh
│   ├─ setup_guide.sh   (lib 를 source; 생성/배포 시에는 인라인 가능)
│   ├─ update_engine.sh
│   └─ make_update.sh
├─ pilot/config_check/   # 파일럿: vars.manifest, 생성물, 검증용 사본
└─ test/                 # run_tests.sh (+ 픽스처)
```
작업·검증은 랩 **.58**(root, 로그인 셸 csh → `ssh root@192.168.0.58 'bash -s' <<'EOF'`)에서 한다. 개발 트리는 `tar | ssh` 로 `/root/gus_work/` 에 올린다. RHEL6 bash 4.1 검증은 단계 7 에서 별도로 한다(가능한 RHEL6 환경이 없으면 bash 4.1 호환 정적 검사 + 사용 가능한 가장 오래된 bash 로 대체하고 **그 사실을 명시**).
