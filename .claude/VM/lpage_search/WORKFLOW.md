# 작업 흐름도 — lpage_search (ESXi Large Page 메모리 사이징)

입력값으로 ev02 VM에 안전하게 줄 수 있는 메모리를 계산하는 흐름입니다. vCenter/ESXi에는 접속하지 않습니다.
계산식은 [README.md](README.md)의 "4.1 계산 로직 개요"를 참고하세요.

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
