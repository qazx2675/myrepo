# ARCHITECTURE.md

| 폴더/파일 | 역할 |
|---|---|
| `main.go` | CLI 진입점: 옵션, 대상 목록 정리(VM 이름 → 접두어), 전체 흐름·출력·종료 코드 |
| `main_test.go` | 대상 목록 정리 등 진입점 로직 테스트 |
| `vc/` | vCenter 병렬 접속, 이름 조회 → 대상 VM만 vNIC MAC 배치 조회(`devices.go`) |
| `dhcp/` | DNS로 대역 결정, 3옥텟 이름의 DHCP 대역 파일 읽기·캐시 |
| `verify/verify.go` | 자기 예약 MAC 대조, 형제 VM 예약 MAC과 비교해 교차 설치 판정 |
| `auditlog/auditlog.go` | FAIL hostname을 `LOG/vm-verifier-YYYYMMDD.log`에 추가 |
| `setup.sh` | 오프라인 빌드 (`../../공통/govendor/govmomi-0.39.0`을 `vendor`로 링크) |
| `run.sh` | 빌드·계정·옵션을 물어보는 대화형 실행 스크립트 |
| `PLAN.md` / `CHANGELOG.md` | 설계 배경 / 변경 이력 |

수정 요청별로 볼 곳:

- **"판정 규칙 변경(역설치 조건 등)"** → `verify/verify.go`
- **"DHCP 파일 형식·경로가 바뀌었다"** → `dhcp/dhcp.go`
- **"조회가 느리다"** → `vc/vc.go`, `vc/devices.go`
- **"옵션 추가"** → `main.go` + README 3장 표

## 작업 흐름도

단계별 설명은 [WORKFLOW.md](WORKFLOW.md)를 참고하세요.

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
