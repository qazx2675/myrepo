# WORKFLOW

## 01.AWX_nodeinfo_V2.sh 실행 흐름 (Mermaid)

```mermaid
flowchart TD
    A["[0] 초기화<br/>trap cleanup EXIT<br/>변수/로그 준비"] --> B["[1] user 호출<br/>user() 빈 함수<br/>현장 코드 삽입 필요"]
    B --> C{"user 값<br/>확인"}
    C -->|비어 있음| C1["[X] 오류 종료<br/>user 함수 확인"]
    C1 --> END1["exit 1"]
    C -->|값 있음| D["LOG 파일 열기<br/>LOG/${user}.log"]
    
    D --> E["[2] download_txt<br/>프롬프트: Y/N"]
    E -->|Y| E1["nodeinfo 호출<br/>-hosts 절대경로"]
    E1 -->|실패| E2["[X] 오류 종료<br/>입력 파일 보호"]
    E2 --> END2["exit 1"]
    E1 -->|성공| E3["결과를 ${user}.txt"]
    E -->|N| E3
    E3 --> F["[3] msg 파싱<br/>또는 12필드 검증"]
    
    F -->|msg 있음| F1["값만 남김"]
    F -->|msg 없음| F2["12필드 검증"]
    F -->|실패| F3["[X] 오류 종료"]
    F3 --> END3["exit 1"]
    F1 --> G
    F2 --> G
    
    G["[4] MAC 짝수 보정<br/>spice/pice/dspr/pspr<br/>+ ev 미포함"] --> H["[5] 등록 대상 출력<br/>20개씩 세로 다단"]
    H --> I["프롬프트: 작업진행여부"]
    
    I -->|N| END4["작업 취소<br/>exit 0"]
    I -->|Y| J["빈 변수 사전 검사<br/>inventory_delete_host 등"]
    J -->|미충족| END5["[X] 오류 종료<br/>exit 1"]
    J -->|완료| K["[7] inventory_delete<br/>원격 호스트 삭제"]
    
    K -->|실패| END6["[X] 오류 종료"]
    K -->|성공| L["[8] 분할 파일 생성<br/>mktemp -d 안"]
    
    L --> M["[9] dhcp_pool 기록<br/>누적"]
    M --> N["[10] scp 파일 전송<br/>custom_inventory.sh<br/>yml 수집"]
    N -->|실패| END7["[X] 오류 종료"]
    N -->|성공| O["[11] git 업로드<br/>yml 각각<br/>cd 복귀"]
    
    O -->|실패| END8["[X] 오류 종료"]
    O -->|성공| P["[12] LDAP/LACP 점검<br/>gossh -script<br/>tmp/all_${user}"]
    
    P --> Q["[13] 메뉴 루프<br/>while true"]
    Q -->|su| Q1["break"]
    Q -->|exit| END9["exit 0"]
    Q -->|ls| Q2["yml 파일 + boot"]
    Q2 --> Q
    Q -->|파일명| Q3["내용 출력"]
    Q3 --> Q
    Q -->|Enter| Q
    Q1 --> R["[14] 작업 리스트<br/>그룹 yml 이름"]
    
    R --> S["bash 02.source_dhcp_pxe.sh<br/>yml=infra,os,boot,splunk"]
    S -->|성공| END10["break<br/>완료"]
    S -->|실패| T["프롬프트: 재시도"]
    T -->|Y| S
    T -->|N| END11["exit 1"]
```

## 02.source_dhcp_pxe.sh 실행 흐름

```mermaid
flowchart TD
    A["[시작]<br/>user=$1; shift"] --> B{"인자<br/>개수"}
    B -->|0개| B1["대화형 모드<br/>ls *.yml"]
    B1 --> B2["프롬프트: yml"]
    B2 --> C1["invsync"]
    C1 --> C2["dhcp"]
    C2 --> C3["pxe"]
    C3 --> END1["exit 0"]
    
    B -->|1개 이상| D["인자 파싱<br/>yml=infra,os,boot,splunk"]
    D -->|형식 오류| D1["[X] 오류"]
    D1 --> END2["exit 1"]
    D -->|성공| E["배열 구성<br/>ymls/infras/oss/boots/splunks"]
    
    E --> F["옵션 확인표 출력<br/>번호|yml|infra|os|boot|splunk|호스트수"]
    F --> G["프롬프트: 맞습니까"]
    G -->|Y| H["직접 실행 시작"]
    G -->|N| H1["yml별 값 재입력<br/>Enter=유지"]
    H1 --> H2["옵션 확정"]
    H2 --> H
    
    H --> I["결과 배열 초기화<br/>fail_cnt=0"]
    I --> J["for 루프: yml별"]
    J --> K["invsync -user -file yml"]
    K -->|실패| K1["skip 나머지<br/>fail_cnt++"]
    K1 --> J
    K -->|성공| L["dhcp -user -infra"]
    L -->|실패| K1
    L -->|성공| M["pxe -user -infra -os -boot -splunk"]
    M -->|실패| K1
    M -->|성공| M1["성공 기록"]
    M1 --> J
    J -->|모든 yml 완료| N["요약 출력<br/>전체/성공/실패"]
    N --> O{"실패<br/>여부"}
    O -->|0| END3["exit 0"]
    O -->|1+| END4["exit 1"]
```

## 부연 설명

### 01 흐름의 주요 분기
- **노드정보 수집**: Y 선택 시 `awxkit/nodeinfo.sh` 호출, N 선택 시 기존 `${user}.txt` 사용
- **작업 진행**: N 선택 시 즉시 종료 (사전 점검 목적), Y 선택 시 inventory_delete → 이후 진행
- **메뉴 선택**: su(02 진입), exit(전체 종료), ls(yml 목록), 파일명(내용 출력)
- **02 호출 재시도**: 02 실패 시 "재시도 Y|N" 프롬프트로 반복

### 02 흐름의 주요 특징
- **호환 모드**: 인자 0개 시 기존 대화형 방식(1개 yml만) 유지
- **배치 모드**: 인자 1개 이상 시 자동 반복 (01에서 호출)
- **옵션 확인**: 최초 1회 전체 옵션 표시 후 N 선택 시 수동 수정
- **실패 처리**: 한 단계 실패 시 해당 yml의 나머지는 건너뛰고 다음 yml 진행
