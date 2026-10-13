# 작업 흐름도 — redfish (BIOS 표준값 점검·설정 도구)

명령 한 번이 실행되는 주요 경로 6가지를 그렸습니다. 옵션 전체는 [README.md](README.md), 파일별 역할은 [ARCHITECTURE.md](ARCHITECTURE.md), 첫 실장비에서 사람이 확인할 것은 [FIRST_RUN.md](FIRST_RUN.md) 를 참고하세요.

각 흐름은 mermaid 로 그렸고(GitHub 에서 바로 렌더링됩니다), mermaid 를 렌더링한 그림을 같은 폴더에 함께 두었습니다: 1번 [workflow.svg](workflow.svg), 2번 [workflow_set.svg](workflow_set.svg), 3번 [workflow_dump.svg](workflow_dump.svg), 4번 [workflow_allcheck.svg](workflow_allcheck.svg), 5번 [workflow_retry.svg](workflow_retry.svg), 6번 [workflow_account.svg](workflow_account.svg). 글자는 `<text>` 로 들어 있어(foreignObject 아님) 뷰어 글꼴이 달라도 잘리지 않습니다. mermaid 를 보지 못하는 뷰어에서는 SVG 를 여십시오.

## 그림별 단계와 확인 순서

아래 번호는 이 문서의 그림 번호이고, **실제 운영에서 보는 순서는 오른쪽 “순서”** 입니다.

| 순서 | 그림 | 단계 | 명령 | 언제 보나 / 무엇을 확인하나 |
|---|---|---|---|---|
| 1 | 6. [workflow_account.svg](workflow_account.svg) | 준비: 1순위 계정 만들기 | `bash account_create.sh` (점검 끝 `[Y/n]`) | 점검에서 AUTH_FAIL 이 나오고 2순위(rf.conf)로만 접속될 때. 읽기 점검 → 목록 y/n → admin 로 생성 → 새 계정 로그인 검증 → 실패 시 롤백. 결과 상태가 `CREATED` 인지 |
| 2 | 3. [workflow_dump.svg](workflow_dump.svg) | 사전조사: 모델별 속성명·허용값 수집 | `bash xml.sh --compact 대상...` | 처음 보는 모델이거나 점검에서 `MAPPING_MISSING` 이 나올 때. 읽기 전용 덤프 → 요약 → `profiles/*.tsv` 에 `verified=N` 행 추가 |
| 3 | 1. [workflow.svg](workflow.svg) | 점검: 표준값과 대조 | `bash bios_check.sh --profile <이름>` | 기본 작업. 호스트마다 GET 만 하고 OK / FAIL / UNVERIFIED / MAPPING_MISSING 판정. 끝에서 FAIL 이 있으면 설정 여부를 묻는다 |
| 4 | 2. [workflow_set.svg](workflow_set.svg) | 설정: FAIL 항목을 Pending 으로 기록 | 점검 끝의 `Y` (먼저 `-dry-run` 권장) | 점검 결과가 맞고 `verified=Y` 행이 있을 때만. 호스트별 `APPLIED` / `ALREADY_OK` / `UNVERIFIED` / `REJECTED` 등. 재부팅은 하지 않으므로 반영 후 랜덤 서버 확인 |
| 5 | 5. [workflow_retry.svg](workflow_retry.svg) | 재시도: 일시 오류 호스트만 다시 | `-retry-from <이전결과폴더>` | 3~4번 결과에 `retry.txt` 대상이 남았을 때. 새 결과가 이전 결과와 병합되고 `merged_retry.txt` 가 비면 끝 |
| 6 | 4. [workflow_allcheck.svg](workflow_allcheck.svg) | 전체 비교: 같은 모델끼리 속성 전체 비교 | `bash all_bios_check.sh` | 표준값 표(프로파일) 밖의 차이를 찾을 때. 설정 흐름과 독립이며 읽기 전용. `diff.txt` 의 기준 호스트와 비교해 SAME / DIFF |

처음 도입할 때 권장 흐름: 1(계정) → 2(사전조사) → 3(점검) → 4(`-dry-run` 후 설정) → 5(재시도) → 6(전체 비교). 이미 계정과 프로파일이 있으면 3 → 4 → 5 만 반복합니다.

공통 약속: 재부팅은 하지 않습니다. 호스트당 로그인은 1회, 끝나면 자기 세션만 삭제합니다. 허용목록 밖 호출(로그 조회·ClearLog·Reset 등)은 전송 전에 코드가 막습니다.

## 1. 점검 — `bash bios_check.sh --profile VM`

```mermaid
flowchart TD
    S(["bash bios_check.sh --profile VM"]) --> W["bios_check.sh<br/>폴더 이동 → OS 에 맞는 바이너리 선택<br/>커널 3 미만은 bin/biostool_os6, 그 외 bin/biostool<br/>필요 파일 확인: bios.conf · user.txt · pass.enc · key.bin"]
    W --> M{"파일이 모두 있는가"}
    M -->|없음| X1(["안내 후 종료 (코드 1)"])
    M -->|있음| P["프로파일 선택<br/>--profile 또는 번호 메뉴"]
    P --> L["biostool check 시작<br/>bios.conf 로드 · 프로파일 TSV 검증<br/>user.txt 해석 (이름은 /etc/hosts 의 이름-m)<br/>pass.enc 복호화 (메모리에만 보관)"]
    L --> R["대상 호스트 순회 (동시 접속 concurrency)<br/>처음 auth_fail_stop 대는 한 대씩, 로그인 성공하면 병렬"]
    R --> H["호스트 1대<br/>로그인 1회 (세션)<br/>GET 만: ServiceRoot → Systems → Bios → Settings(Pending)<br/>Dell 은 Pending 이 있을 때 Jobs 도 GET<br/>MAPPING_MISSING 모델은 1대분 덤프를 mapping_missing/ 에 자동 저장<br/>끝나면 자기 세션 삭제"]
    H --> J["항목 판정 (프로파일과 대조)<br/>OK · PENDING_OK · FAIL · UNVERIFIED<br/>PENDING_EXISTS · MAPPING_MISSING · PENDING_NO_JOB"]
    J --> AF{"AUTH_FAIL 이<br/>auth_fail_stop 이상 쌓였는가"}
    AF -->|예| SK["남은 호스트는 접속하지 않고<br/>SKIPPED_AUTH_STOP 으로 기록"]
    AF -->|아니오| F
    SK --> F["결과 파일 기록 results/일시/<br/>result.tsv · ok.txt · fail.tsv · retry.txt · run_info.txt"]
    F --> T["터미널 리포트<br/>[OK] · [FAIL 상세] · [설정 불가] · [기타 오류]"]
    T --> Q0{"-dry-run 인가"}
    Q0 -->|예| DR["호스트별로 보낼 PATCH 경로·본문 출력<br/>BMC 에 쓰지 않고 Y/N 도 묻지 않음"]
    Q0 -->|아니오| Q1{"설정할 수 있는<br/>FAIL 이 있는가"}
    Q1 -->|없음| E1(["종료"])
    Q1 -->|있음| Q2{"-from-dump 또는<br/>-no-prompt 인가"}
    Q2 -->|예| E2(["묻지 않고 종료<br/>아무것도 설정하지 않음"])
    Q2 -->|아니오| Q3{"표준입력이 터미널인가<br/>또는 -stdin-ok 가 있는가"}
    Q3 -->|아니오| N1(["N 으로 처리<br/>아무것도 설정하지 않음"])
    Q3 -->|예| YN{"설정하시겠습니까? Y/N"}
    YN -->|"N 또는 그 밖의 입력"| E3(["설정하지 않고 종료"])
    YN -->|Y| SET(["2번 흐름: 설정"])
```

## 2. 설정 (점검 후 Y) — 호스트 1대 기준

```mermaid
flowchart TD
    Y(["Y 입력"]) --> A["설정 대상 = FAIL 항목<br/>(Dell 은 verified=Y 인 PENDING_NO_JOB 도 포함)<br/>같은 BMC 를 가리키는 중복 대상은 DUPLICATE_TARGET"]
    A --> V{"프로파일에서 모델을 지정한<br/>verified=Y 행이 다시 확인되는가"}
    V -->|아니오| U["UNVERIFIED<br/>보내지 않음"]
    V -->|예| VN{"벤더가 Dell · HPE · Lenovo 인가"}
    VN -->|아니오| NV["SET_NOT_SUPPORTED_VENDOR<br/>접속하지 않음"]
    VN -->|예| LG["로그인 1회 (쓰기 허용 모드)"]
    LG --> RE["재조회 GET<br/>모델 · Settings 경로 · 현재값 · Pending · 레지스트리"]
    RE --> C1{"점검 이후 상태가 바뀌었는가"}
    C1 -->|"바뀜"| CH["CHANGED<br/>모델·경로가 바뀌었거나 속성이 없음"]
    C1 -->|"이미 기대값"| AO["ALREADY_OK<br/>현재값이나 Pending 이 이미 기대값<br/>Dell 은 Job 이 없으면 다시 보냄"]
    C1 -->|"남의 Pending"| PE["PENDING_EXISTS<br/>건드리지 않음"]
    C1 -->|"해당 없음"| RG{"레지스트리 사전검증"}
    RG -->|ReadOnly| RO["SKIPPED_READONLY"]
    RG -->|"값·형식 거부"| IV["INVALID_VALUE<br/>허용값 밖 · 형식 오류 · 위험 속성명"]
    RG -->|통과| SP{"시스템 프로파일 항목이 있고<br/>목표가 Custom 이 아닌가"}
    SP -->|예| DP["프로파일 항목만 보내고<br/>나머지는 DEPENDS_ON_PROFILE<br/>재부팅 후 재점검해 다시 설정"]
    SP -->|아니오| PT
    DP --> PT["PATCH 1회<br/>Bios/Settings 또는 Bios/Pending<br/>본문은 Attributes 와 ApplyTime=OnReset 뿐, 재시도 없음"]
    PT -->|"2xx 성공"| DJ{"Dell 인가"}
    DJ -->|예| JB["자동 생성된 BIOS 설정 Job 이 안 보이면<br/>Job POST 최대 1회<br/>TargetSettingsURI 만, 재부팅 필드 없음"]
    DJ -->|아니오| RS
    JB --> RS["Settings 를 다시 읽어 Pending 확인"]
    RS -->|"Pending 이 기대값"| AP["APPLIED"]
    RS -->|"확인 못함"| UC["APPLY_UNCONFIRMED<br/>Job 생성 실패도 포함"]
    PT -->|"HTTP 400"| RJ["REJECTED"]
    PT -->|"404 · 405 · 501"| SU["SET_UNSUPPORTED"]
    PT -->|"5xx · 타임아웃"| SE["SET_ERROR<br/>연결 끊김 포함, 결과 불명<br/>재시도하지 않음, 재점검 필요"]
    PT -->|"401 · 403"| AF["AUTH_FAIL<br/>재시도하지 않음"]
    AP --> OUT
    UC --> OUT
    RJ --> OUT
    SU --> OUT
    SE --> OUT
    AF --> OUT
    OUT["로그아웃 후 결과 기록<br/>apply_result.tsv · applied.txt · apply_failed.txt · apply_requests.txt<br/>리포트 끝에 재부팅 안 함 · 랜덤 서버 직접 확인 안내"]
```

## 3. 사전조사 — `bash xml.sh [대상...]`

```mermaid
flowchart TD
    S(["bash xml.sh --compact 대상..."]) --> W["xml.sh<br/>바이너리 선택 · bios.conf · pass.enc · key.bin 확인<br/>대상은 인자(쉼표로 묶음) 또는 user.txt"]
    W --> D["biostool dump<br/>대상 해석 · 비밀번호 복호화"]
    D --> R["호스트 순회<br/>동시 접속과 AUTH_FAIL 차단기는 점검과 같음"]
    R --> H["호스트 1대: 로그인 1회 후 GET 만<br/>ServiceRoot · Systems · 각 System · Bios · Settings(Pending)<br/>Registries · 속성 레지스트리 · Managers (Jobs · Tasks 는 목록만)<br/>호스트당 서로 다른 경로 120개 상한, LogServices 등 허용목록 밖은 요청하지 않음"]
    H --> SV["JSON 저장<br/>dumps/벤더/모델/BIOS버전/호스트/redfish/v1/... 와 dump_meta.json<br/>index.tsv 에 한 줄 (OK · PARTIAL · SAVE_FAIL · 오류 코드)"]
    SV --> SM["터미널 요약<br/>호스트 헤더 1줄 + 표준 4항목 후보 속성(허용값 · ReadOnly) + pending 여부<br/>--compact 는 호스트당 1줄"]
    SM --> HU["사람이 요약을 읽고 전달<br/>폐쇄망이라 파일은 반출하지 않음"]
    HU --> PR["profiles/*.tsv 에 모델 행 추가<br/>verified=N 으로 시작"]
```

## 4. 모델별 전체 비교 — `bash all_bios_check.sh`

```mermaid
flowchart TD
    S(["bash all_bios_check.sh"]) --> W["all_bios_check.sh<br/>바이너리 선택 · bios.conf · diff.txt · user.txt · pass.enc 확인<br/>-from-dump 이면 접속 안 하므로 pass.enc 불필요"]
    W --> L["biostool allcheck<br/>diff.txt (기준) · ignore_attrs.txt (없으면 내장 목록) · user.txt 로드<br/>읽기 전용, 쓰기 호출 없음"]
    L --> R1["1단계: 기준 호스트 읽기<br/>호스트당 GET 4번: Root · Systems · System · Bios<br/>모델은 BMC 가 보고한 값을 정규화한 키"]
    R1 --> R2["2단계: 대상 호스트 읽기<br/>AUTH_FAIL 예산은 1단계와 공유, 읽는 즉시 비교하고 값은 버림"]
    R2 --> C{"대상과 같은 모델의<br/>기준 호스트가 있는가"}
    C -->|"diff.txt 에 없음"| NR["NO_REFERENCE"]
    C -->|"기준을 읽지 못함"| RU["REF_UNREACHABLE<br/>retry.txt 대상"]
    C -->|"기준 자신뿐"| RF["REFERENCE<br/>비교 안 함"]
    C -->|"있음"| CP["기준이 여럿이면 BIOS 버전이 같은 쪽 선택<br/>Attributes 전체 비교 (제외 속성 뺌)<br/>SAME 또는 DIFF, 속성 단위는 DIFF · ONLY_REF · ONLY_HOST<br/>BIOS 버전이 다르면 BIOS_VER_DIFF 특이사항"]
    CP --> O
    NR --> O
    RU --> O
    RF --> O
    O["results/일시_all/<br/>all_diff.tsv · summary.tsv · retry.txt · run_info.txt"] --> T["터미널 리포트<br/>모델 그룹별 차이 · 속성별 집계 · 특이사항 · 분류 불가와 오류"]
```

## 5. 재시도 — `-retry-from 이전결과폴더`

```mermaid
flowchart TD
    S(["bios_check.sh 또는 all_bios_check.sh<br/>-retry-from 이전결과폴더"]) --> P{"-user 도 함께 지정했는가"}
    P -->|예| E1(["오류로 종료<br/>대상은 이전 결과의 retry.txt"])
    P -->|아니오| RL["대상 읽기<br/>폴더에 merged_retry.txt 가 있으면 그것, 없으면 retry.txt<br/>retry.txt 파일 경로를 직접 줘도 됨"]
    RL --> EM{"목록이 없거나 비어 있는가"}
    EM -->|예| E2(["재시도할 대상이 없습니다<br/>안내 후 정상 종료, 아무것도 쓰지 않음"])
    EM -->|아니오| PV["이전 결과 검증 (읽기만)<br/>check: result.tsv 머리글과 프로파일 일치<br/>allcheck: summary.tsv · all_diff.tsv"]
    PV --> RN["그 대상만 다시 점검 또는 비교<br/>새 결과 폴더에 일반 실행과 똑같이 기록<br/>점검이면 Y/N 설정 단계도 같음"]
    RN --> MG["병합<br/>이전 결과에서 재시도한 호스트의 행을 새 행으로 교체<br/>나머지 호스트의 행은 그대로 (AUTH_FAIL 포함)"]
    MG --> MF["merged 파일을 새 폴더에 기록<br/>check: merged_result.tsv · merged_ok.txt · merged_retry.txt · merged_run_info.txt<br/>allcheck: merged_summary.tsv · merged_all_diff.tsv · merged_retry.txt · merged_run_info.txt"]
    MF --> TR["터미널 끝에 재시도 병합 블록<br/>복구 · 여전히 일시오류 · 다른 오류 · 병합본 전체 집계"]
    TR --> LP{"merged_retry.txt 에 남은 대상이 있는가"}
    LP -->|예| S2(["새 결과 폴더로 다시 -retry-from"])
    LP -->|아니오| DN(["끝"])
```

## 6. 계정 생성 — `bash account_create.sh` (또는 `bios_check.sh` 끝의 Y/n), 비밀번호 변경은 `bin/biostool account change`

```mermaid
flowchart TD
    S(["bash account_create.sh<br/>또는 bios_check.sh 끝 [Y/n]"]) --> P["1. 점검 (읽기 전용)<br/>1순위 계정 로그인<br/>거부(401)면 비인증 ServiceRoot 로 벤더 식별 후 rf.conf admin 로그인 1회"]
    P --> C{"결과"}
    C -->|"1순위로 접속됨"| OK(["PRIMARY_OK<br/>만들 필요 없음"])
    C -->|"2순위로만 접속됨"| NC["NEED_CREATE 목록"]
    C -->|"둘 다 거부·접속 불가"| HE(["호스트 오류 상태 (AUTH_FAIL 등)"])
    NC --> Q{"2. 대상 목록 표시 후<br/>Y/n (빈 입력 = Y)"}
    Q -->|"n · 비대화형 · -no-prompt"| NO(["만들지 않고 종료<br/>안내만"])
    Q -->|"-dry-run"| DR(["계획만 출력, 쓰기 없음"])
    Q -->|"Y"| W["3. admin 로그인 → 계정 컬렉션 읽기"]
    W --> EX{"같은 id 가 있는가"}
    EX -->|"예"| SK(["SKIP_EXISTS<br/>덮어쓰지 않음"])
    EX -->|"아니오"| CR["관리자 역할로 생성<br/>Dell 빈 슬롯 PATCH / 그 밖 POST<br/>UCSM: aaaUser created"]
    CR --> V{"4. 새 계정으로 로그인 1회"}
    V -->|"성공"| CD(["CREATED"])
    V -->|"통신 오류"| VU(["VERIFY_UNKNOWN<br/>롤백 안 함, 수동 확인"])
    V -->|"인증 거부"| RB["5. 이번 실행이 만든 계정만 삭제 (롤백)"]
    RB -->|"성공"| RO(["VERIFY_FAIL_ROLLED_BACK"])
    RB -->|"실패"| RF(["ROLLBACK_FAILED<br/>수동 조치"])
```

`account change` 는 같은 4~5단계(새 비밀번호를 터미널에서 직접 2번 입력 → y → 변경 → 새 비밀번호 로그인 검증 → 인증 거부면 이전 비밀번호 복원)이며 `pass.enc` 는 자동으로 바꾸지 않고 `cd <폴더>; bash encrypt.sh` 를 안내합니다.