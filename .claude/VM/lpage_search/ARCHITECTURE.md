# ARCHITECTURE.md

| 폴더/파일 | 역할 |
|---|---|
| `main.go` | 단일 파일 프로그램: 옵션 파싱, `calculateHostReservedMB`(High-State 버퍼), `calculateVmOverheadMB`(VM 오버헤드), ev02 가용량 산출·출력 |
| `go.mod` | 모듈 정의 (외부 의존성 없음) |
| `setup.sh` | `go build -o lpage_search main.go` |
| `README.md` / `WORKFLOW.md` | 사용법 / 계산 흐름도 |

수정 요청별로 볼 곳:

- **"오버헤드 계수·버퍼 비율 조정"** → `main.go`의 `calculateVmOverheadMB`, `calculateHostReservedMB`
- **"입력 옵션 추가"** → `main.go`의 flag 정의 + README 3장 표

## 작업 흐름도

단계별 설명은 [WORKFLOW.md](WORKFLOW.md)를 참고하세요.

```mermaid
flowchart TD
    A["입력<br/>-h 호스트(라벨) · -v1 ev01 메모리 GB<br/>-hm 호스트 총 메모리 GB · -vcpu1 · -vcpu2"] --> B["High-State 버퍼 계산<br/>minFree(총 메모리의 2%) × 3 + 4096MB"]
    A --> C["ev01 오버헤드<br/>할당 메모리 × 1.2% + vCPU × 64MB"]
    A --> D["ev02 오버헤드 추정<br/>(같은 식, -vcpu2 기준)"]
    B --> E["ev02 가용량<br/>총 메모리 − 버퍼 − (ev01 + ev01 오버헤드) − ev02 오버헤드"]
    C --> E
    D --> E
    E --> F["짝수 GB 단위로 내림 정렬"]
    F --> G["출력: Recommended ev02 Size<br/>+ 2MB Large Page 매핑 수"]
    G --> H["사람이 vSphere Client 등으로 실제 설정"]
    H --> I["무작위 호스트 몇 대의 minFree / Large Page 상태 확인"]
```

<details><summary>SVG 이미지로 보기 (workflow.svg)</summary>

![작업 흐름도](workflow.svg)

</details>
