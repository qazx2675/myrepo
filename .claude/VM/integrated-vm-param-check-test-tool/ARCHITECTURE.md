# ARCHITECTURE.md

| 폴더/파일 | 역할 |
|---|---|
| `README.md` | 두 도구를 묶어 빌드·테스트하는 절차 |
| `WORKFLOW.md` | 빌드 → vcsim 기동 → 테스트 → 종료 흐름도 |
| `vm-param-check/` | 체크+자동교정 도구 사본 (구조는 `../vm-param-check-usability-improvement/ARCHITECTURE.md`와 같음) |
| `vc-test-env/` | 실 vCenter 구조를 레시피로 추출해 vcsim에 재현하는 도구 (`extract` / `tree` / `build` / `diff`) |
| `testkit/build-all.sh` | 두 도구 빌드 |
| `testkit/start-vcsim.sh` | 레시피로 vcsim 기동 (127.0.0.1:54321, PID는 `testkit/out/vcsim.pid`) |
| `testkit/run-tests.sh` | vcsim 대상 체크+자동교정 테스트, 결과는 `testkit/out/` |
| `testkit/stop-vcsim.sh` | vcsim 종료 |

의존성: `vm-param-check` → `.claude/공통/govendor/govmomi-0.39.0`, `vc-test-env` → `.claude/공통/govendor/govmomi-0.55.1-vcsim` (각 `setup.sh`가 `vendor`로 링크).

수정 요청별로 볼 곳:

- **"테스트 시나리오 추가"** → `testkit/run-tests.sh`
- **"vcsim에 재현할 필드 추가"** → `vc-test-env/README.md`의 "필드 추가하는 법"
- **"체크/교정 로직 수정"** → `vm-param-check/`(다른 사본과 함께 반영)

## 작업 흐름도

단계별 설명은 [WORKFLOW.md](WORKFLOW.md)를 참고하세요.

```mermaid
flowchart TD
    subgraph ONLINE["인터넷 되는 곳 (최초 1회)"]
        A["tar czf: 이 폴더 + 공통/govendor 해당 버전"] --> A2{"레시피 미리 추출?"}
        A2 -- 예 --> A3["vc-test-env extract -vc=실vCenter<br/>~/.vc-test-env/recipes/IP.json"]
        A2 -- 아니오 --> B
        A3 --> B["USB / scp 로 폐쇄망 서버에 복사"]
    end
    B --> C["압축 해제 · chmod +x testkit/*.sh"]
    C --> D["./testkit/build-all.sh<br/>vm-param-check + vc-test-env 빌드"]
    D --> E{"레시피 캐시 있음?"}
    E -- 예 --> F["./testkit/start-vcsim.sh -vc=IP<br/>(실 vCenter 접속 없음)"]
    E -- 아니오 --> F2["VC_USER/VC_PASS 와 함께 start-vcsim.sh<br/>실 vCenter에서 추출 후 기동"]
    F --> G["127.0.0.1:54321 응답 대기 → 기동 완료<br/>PID: testkit/out/vcsim.pid"]
    F2 --> G
    G --> H["./testkit/run-tests.sh<br/>vcsim 대상 체크 + 자동교정"]
    H --> I["결과: testkit/out/ (CSV · 로그)"]
    I --> J["./testkit/stop-vcsim.sh"]
    J --> K{"실제 vCenter에도 적용?"}
    K -- 예 --> L["vm-param-check 를 실 vCenter 대상으로 직접 실행<br/>(-fix 후 무작위 VM 확인)"]
    K -- 아니오 --> M["끝"]
```

<details><summary>SVG 이미지로 보기 (workflow.svg)</summary>

![작업 흐름도](workflow.svg)

</details>
