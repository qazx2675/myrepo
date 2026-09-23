# 작업 흐름도 — vm-verifier (VM MAC ↔ DHCP 예약 MAC 교차 설치 검증)

VM 생성 직후, **파워온 전에** vCenter가 인식한 vNIC MAC과 DHCP 정적 예약 MAC을 대조하는 흐름입니다.
옵션은 [README.md](README.md), 설계 배경은 [PLAN.md](PLAN.md)를 참고하세요.

```mermaid
flowchart TD
    A["실행<br/>VC_USER / VC_PASS, -vcenterList vcenter.txt, -f targets.txt"] --> B["대상 목록 정리<br/>VM 이름(svr01ev01)은 접두어(svr01)로 변환 → 중복 제거 · 정렬"]
    B --> C["모든 vCenter 병렬 접속"]
    C --> D["인벤토리 전체 VM 이름만 가볍게 조회"]
    D --> E["접두어ev숫자 패턴 VM 선별<br/>(개수 제한 없음)"]
    E --> F["대상 VM만 config.hardware.device 배치 조회<br/>→ vNIC MAC"]
    E --> G["hostname DNS 조회 → IP 앞 3옥텟"]
    G --> H["-dhcp-root/3옥텟 파일 읽기<br/>(대역당 1회, 캐시)"]
    F --> I["MAC 대조"]
    H --> I
    I --> J{"자기 예약 MAC과 일치?"}
    J -- 예 --> K["PASS"]
    J -- 아니오 --> L{"형제 VM(같은 BM)의 예약 MAC과 일치?"}
    L -- 예 --> M["FAIL: 교차 설치(역설치) 의심"]
    L -- 아니오 --> N["FAIL: MAC 불일치"]
    K --> O["출력 (-failonly 면 FAIL만, 모두 PASS면 요약 1줄)"]
    M --> O
    N --> O
    M --> P["LOG/vm-verifier-YYYYMMDD.log 에 추가"]
    N --> P
    O --> Q{"FAIL 있음?"}
    Q -- 예 --> R["exit 1<br/>DHCP/네트워크 설정 수정 후 재실행"]
    Q -- 아니오 --> S["exit 0 → 파워온 · OS 설치 진행"]
```

<details><summary>SVG 이미지로 보기 (workflow.svg)</summary>

![작업 흐름도](workflow.svg)

</details>

## 단계 설명

| 단계 | 패키지 | 설명 |
|---|---|---|
| 대상 정리 | `main.go` | VM 이름을 적어도 BM 접두어로 바꾸고 중복 제거·정렬 |
| vCenter 조회 | `vc/` | 모든 vCenter 병렬 접속, 이름만 훑은 뒤 대상 VM만 장치 정보 배치 조회 |
| DHCP 조회 | `dhcp/` | DNS로 대역을 정하고, 3옥텟 이름의 대역 파일만 읽어 캐시 |
| 판정 | `verify/` | 자기 예약 MAC과 비교, 틀리면 형제 VM 예약 MAC과 비교해 역설치 판정 |
| 감사 로그 | `auditlog/` | FAIL hostname만 `LOG/vm-verifier-YYYYMMDD.log`에 추가 |
