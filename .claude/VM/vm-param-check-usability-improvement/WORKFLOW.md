# 작업 흐름도 — vm-param-check (체크 + 자동교정)

`vm-param-check` 한 번 실행으로 **스펙 결정 → 대상 VM 조회 → 체크 → CSV → (선택) 교정 → 재검증**까지 이어지는 흐름입니다.
옵션 설명은 [vm-param-check/README.md](vm-param-check/README.md), 파일별 역할은 [ARCHITECTURE.md](ARCHITECTURE.md)를 참고하세요.

```mermaid
flowchart TD
    A["실행<br/>VC_USER / VC_PASS, -vcenterList, -f 대상 목록"] --> B{"기대값을 어떻게 정하나?"}
    B -- "옵션 직접 지정" --> C["-ht -cores -numa -cpu -mem -disk -shares-ev01 ..."]
    B -- "-specRoot" --> D["VM이 속한 vCenter 폴더명 조회"]
    D --> D1{"정식 CAE 폴더?"}
    D1 -- 예 --> D2["폴더명 정규화 → SPEC_DIR/폴더/_spec.txt 매칭"]
    D1 -- "아니오 (Task 폴더)" --> D3["포트그룹 이름으로 원래 폴더명 추정<br/>실패하면 대화형 입력"]
    D3 --> D2
    D2 --> D4["매칭 결과 확인 (-yes 면 생략)"]
    C --> E
    D4 --> E["2단계 조회<br/>① 전체 VM 이름만 가볍게 → ② 대상 VM만 무거운 속성"]
    E --> E1{"못 찾은 대상 / 접속 실패 vCenter?"}
    E1 -- 예 --> E2["경고 출력 (시작·끝 양쪽)"] --> F
    E1 -- 아니오 --> F["항목별 체크<br/>하드웨어 · 토폴로지 · affinity · 전원정책 · preferHT"]
    F --> G["콘솔 요약 + CSV 2종<br/>상세 / _summary (-user 접미사)"]
    G --> H{"-fix ?"}
    H -- 아니오 --> Z["끝"]
    H -- 예 --> I{"게이트<br/>그룹 동질성 + 대상 전원 OFF"}
    I -- 실패 --> Z2["아무것도 바꾸지 않고 중단"]
    I -- 통과 --> J["dry-run: VM별 변경 내역 출력<br/>수동조치 항목은 개수만"]
    J --> K{"y/N 확인<br/>(-yes 여도 -fix 확인은 항상 물음)"}
    K -- N --> Z3["취소 (변경 없음)"]
    K -- y --> L["VM당 Reconfigure 1회로 적용<br/>워커풀 병렬 (-fixConcurrency)"]
    L --> M["교정된 VM만 재조회 → 같은 판정으로 재검증<br/>_recheck_시각.csv"]
    M --> N["무작위 VM 몇 대 직접 확인"]
```

## 배포·갱신 흐름

```mermaid
flowchart LR
    subgraph ONLINE["인터넷 되는 서버"]
        U1["git clone --branch vm-param-check-standalone"] --> U2["update_deploy.sh -n → update_deploy.sh<br/>빌드 성공 후에만 교체, 사용자 파일 보존"]
        P1["make_update_package.sh<br/>정적 빌드 + SHA256SUMS"]
    end
    subgraph OFFLINE["폐쇄망 서버"]
        P2["tar xzf 패키지"] --> P3["update.sh -n → update.sh<br/>체크섬·-demo 확인 후 실행 파일만 교체, 백업"]
    end
    P1 -- "USB / nfs" --> P2
```

## 단계 설명

| 단계 | 설명 |
|---|---|
| 기대값 결정 | 옵션을 직접 주거나, `-specRoot`로 VM이 속한 vCenter 폴더 이름에 맞는 스펙을 자동으로 찾음 |
| Task 폴더 예외 | 정식 폴더 규칙을 안 따르는 임시 폴더의 VM은 포트그룹 이름에서 폴더명을 추정, 실패하면 물어봄 |
| 2단계 조회 | 인벤토리 전체 이름만 먼저 훑고 대상 VM만 상세 조회 (3,000대 기준 3.22초 → 0.20초) |
| 체크·리포트 | 콘솔 `[1]` 요약 표 + `[2]` 상세, CSV 상세/요약 2종 |
| 교정 (`-fix`) | 게이트 → dry-run → y/N → VM당 Reconfigure 1회 → 재검증 CSV |
| 사후 확인 | 설정 변경 후 무작위 VM 몇 대를 직접 확인 |
