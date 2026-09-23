# 작업 흐름도 — V2 (VMsetup + vm-param-check, SPEC_DIR 공유)

V2 폴더를 서버에 배치한 뒤 **빌드 → VM 생성·설정(`vm_setup.sh`) → 체크·교정(`vm-param-check`)** 으로 이어지는 전체 흐름입니다.
두 도구는 같은 스펙 폴더 `SPEC_DIR`를 읽습니다. 자세한 옵션은 [README.md](README.md), 파일별 역할은 [ARCHITECTURE.md](ARCHITECTURE.md)를 참고하세요.

```mermaid
flowchart TD
    A["V2 폴더를 /home 에 배치<br/>govendor · SPEC_DIR · VMsetup · vm-param-check"] --> B["bash setup.sh<br/>11개 도구 오프라인 빌드"]
    B --> C{"vswitch_{user}.txt 포트그룹 칸이 IP?"}
    C -- 예 --> C1["vswitch_pgname.sh<br/>IP → 폴더명-cae-a-b-c-0 변환"] --> D
    C -- 아니오 --> D["./vm_setup.sh -u {user}"]
    D --> E["1. 스펙 할당<br/>포트그룹 폴더명 → SPEC_DIR 자동 매칭<br/>(n → 번호 선택 / 0 → vim 새 스펙)"]
    E --> F["2. 포트그룹 할당(네트워크 어댑터 1)<br/>자동 선택 → y/n → 번호 선택 / vim"]
    F --> G["3. vCenter 선택<br/>vcenter.txt 목록, Enter = 이전 실행"]
    G --> H{"4. 실행 계획 확인 (y/n)<br/>-n 이면 여기서 종료"}
    H -- n --> Z1["종료 (vCenter 변경 없음)"]
    H -- y --> I["vswitch_setting<br/>BM 포트그룹 생성 (호스트 병렬)"]
    I --> J["vm_create<br/>스펙별 VM 생성 ev01~ev99"]
    J --> K["affinity_setting<br/>ev별 affinity 파일 적용"]
    K --> L["lpage_setting<br/>HugePage / CPU 토폴로지"]
    L --> M{"단계 실패?"}
    M -- 예 --> M1["그 단계에서 멈춤<br/>원인 수정 후 재실행 → 이미 있는 것은 건너뜀"] --> D
    M -- 아니오 --> N["[완료]"]
    N --> O["vm-param-check -specRoot=../../SPEC_DIR<br/>같은 스펙으로 체크 → CSV"]
    O --> P{"FAIL 있음?"}
    P -- 아니오 --> Q["끝"]
    P -- 예 --> R["-fix: 게이트(동질성·전원 OFF) → dry-run → y/N → 적용 → 재검증"]
    R --> S["무작위 VM 몇 대 직접 확인"]
```

## 단계 설명

| 단계 | 도구 | 설명 |
|---|---|---|
| 배치·빌드 | `setup.sh` | `GOPROXY=off`, `-mod=vendor`로 11개 도구를 오프라인 빌드. `-l` 목록, `-c` 정리 |
| 포트그룹명 변환 (선택) | `VMsetup/vswitch_pgname.sh` | `vswitch_<user>.txt`의 포트그룹 칸이 IP이면 `<폴더명>-cae-a-b-c-0`으로 변환, 원본은 `.bak` |
| 스펙·포트그룹 할당 | `VMsetup/vm_setup.sh` | 자동 매칭 결과를 표로 보여 주고 y/n. 못 정한 것은 번호 선택 또는 vim 입력 |
| 생성·설정 | `vswitch_setting` → `vm_create` → `affinity_setting` → `lpage_setting` | 실패하면 그 단계에서 멈춤. 재실행 시 이미 있는 포트그룹/VM은 건너뜀 |
| 체크·교정 | `vm-param-check` | 같은 `SPEC_DIR`로 체크, `-fix`로 게이트 → dry-run → 확인 → 적용 → 재검증 |
| 사후 확인 | 사람 | 설정 변경 후 무작위 VM 몇 대를 직접 확인 |
