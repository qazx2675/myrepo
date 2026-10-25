# 작업 흐름도 — vm_power_onoff + cross_check

`vm_power_onoff -u hong` 한 번이 실행되는 흐름입니다(`cross_check=on`, 0.3.0 부터 기본). 옵션 전체는 [README.md](README.md), 파일별 역할은 [ARCHITECTURE.md](ARCHITECTURE.md), 교차검증 데몬은 [cross_check/README.md](cross_check/README.md)를 참고하세요. 이 문서는 README.md 와 ARCHITECTURE.md 에서 링크됩니다.

```mermaid
flowchart TD
    A["시작: vm_power_onoff -u hong"] --> B["conf 읽기 + 권한 검사<br/>허용 경로(allowed_dir) 확인"]
    B -->|위반| X["종료 (exit 1)"]
    B --> C["user → vCenter → 비밀번호 → 기능 선택<br/>1 Off / 2 순차 On / 3 일괄 On / 4 재부팅"]
    C --> F["vCenter 에서 대상 VM 조회 → 계획표<br/>-n 이면 여기서 종료 (전원 작업 없음)"]
    F -->|"대상 없음/중복"| X
    F --> H{"VWP 해당 VM?"}
    H -->|예| I["경고 배너 + 6자리 담당자 확인 코드"]
    I -->|불일치| X
    I --> J1
    H -->|아니오| J1["안전장치 1/5: 6자리 확인 코드 입력<br/>통과하면 대상 호스트 목록(BM별) + 총 N대"]
    J1 -->|"불일치/Ctrl+C"| X
    J1 --> CC{"conf cross_check<br/>(키가 없으면 on)"}

    CC -->|"off (기존 방식)"| J2["2/5 Enter<br/>3/5 60초 안에 VM 이름 직접 입력<br/>틀리거나 시간 초과 → 취소 (exit 1)"]

    CC -->|"on (교차검증 방식)"| C2["2/5 확인자 번호 선택<br/>작업자 본인 제외 · -c 면 생략 · 3회 오입력 → 취소"]
    C2 -->|"취소/Ctrl+C"| X
    C2 --> C3["3/5 cross_check --run (대상은 stdin)<br/>VM 이름 입력은 없음 · 호출 스크립트 이름도 기록<br/>상태: 대기"]
    C3 -->|"Ctrl+C·세션 끊김"| X
    C3 --> K1["확인자: cross_check --user 확인자 (전체 화면)<br/>작업 선택 → 작업자에게 전달받은 대상 붙여넣기 → 패스워드<br/>확인자 화면에는 대상이 보이지 않음(개수만)"]
    K1 -->|"c + 패스워드: 취소"| X
    K1 --> K2{"붙여넣은 대상 = 작업 대상?<br/>순서 무관 · 중복 불가"}
    K2 -->|"다름 (개수만 알림)"| X
    K2 -->|"같음 + 패스워드 일치"| K3["작업 '승인' — 승인 유효시간 시작<br/>approval_ttl_sec (기본 120초 = 2분)<br/>작업자에게 승인 이벤트 + 내 작업 리스트"]

    J2 --> J4
    K3 --> J4["안전장치 4/5: 정말 진행? (YeS/No, ls)<br/>YeS 만 진행 · ls = 대상 다시 보기 · 그 밖은 취소 (exit 1)"]
    J4 --> J5["안전장치 5/5: 10초 카운트다운<br/>Ctrl+C 로 취소 (exit 1)"]
    J5 --> CG["(on) cross_check --start --job ID<br/>승인 유효시간 이내 1회만 통과 → 상태: 승인/작업중<br/>(off 면 이 단계 없음)"]
    CG -->|"유효시간 초과/거부"| X
    CG --> P["전원 작업 실행: 1 Off / 2 순차 On / 3 일괄 On / 4 재부팅<br/>작업중이 work_timeout_sec 를 넘기면 승인/작업중단"]
    P --> R["결과 표 + 실행 기록 (run_user/)"]
    R --> S["(on) cross_check --finish --result done | aborted<br/>done = 스크립트가 취소 없이 끝까지 실행 → 승인/작업완료 (일부 VM 실패 여부는 보지 않음)<br/>aborted = 4/5 에서 No · Ctrl+C 등 → 승인/작업중단"]
    S --> E["종료 (실패 VM 이 있으면 exit 1)"]
```

![작업 흐름도](workflow.svg)

위 그림(`workflow.svg`)은 같은 흐름을 손으로 그린 SVG 이고, 위 mermaid 와 내용이 같습니다(흐름을 바꾸면 둘 다 고칩니다).

## 읽는 법

- **파란색**은 vm_power_onoff 가 하는 일, **주황색**은 cross_check(확인자 터미널·데몬 기록)가 하는 일, **빨간색**은 취소·중단입니다. 취소는 모두 **전원 작업 없이 종료 코드 1** 입니다(전원 작업이 시작된 뒤 Ctrl+C 는 이미 vCenter 에 요청한 작업이 계속될 수 있음).
- 승인 유효시간(`approval_ttl_sec`, 기본 2분)은 **승인 → 4/5 → 카운트다운(10초) → `--start`** 를 덮습니다. 넘기면 데몬이 `승인 후 {TTL} 내 미진행` 으로 중단합니다.
- 작업의 상태: `대기` → `승인`(본 작업 전) → `승인/작업중`(`--start`) → `승인/작업완료`(`--finish --result done`) 또는 `승인/작업중단`(`--finish --result aborted`, 4/5 에서 No, Ctrl+C, `work_timeout_sec` 초과). `승인/작업완료` 는 **작업 스크립트가 취소 없이 끝까지 실행됐다**는 뜻이며 VM 별 성공·실패는 검사하지 않습니다. 대기·승인(시작 전) 중 확인자 취소, 대상 불일치, 작업자 종료, 유효시간 초과 등은 `중단` 입니다.
- `cross_check=off` 면 2/5 Enter → 3/5 VM 이름 입력 → 4/5 → 5/5 의 기존 방식이고 cross_check 는 호출하지 않습니다. `-n`, `--demo` 도 호출하지 않습니다.
- 데몬 내부(상태 전이·한도·인증)는 [cross_check/README.md](cross_check/README.md)의 1장 흐름도를 참고하세요.
