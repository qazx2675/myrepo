# 전체 자동화 프로세스 순서도 (Go 자동화 스크립트 체인)

[시작: Bash 스크립트 실행 및 파일/파라미터 전달]
  -> [Phase 1: ESXi 호스트 자동 등록] worklist.txt 읽기 -> 폴더/클러스터에 비동기(-RunAsync) 등록
  -> [Phase 2: 포트 그룹 초고속 생성] vswitch.txt 읽기 -> vCenter SDK 다이렉트 메모리 주입
  -> [Phase 3: VM 동적 생성 및 속성 주입] vmCount 만큼 VM 생성 -> Memory Lock(VMX Pinning) 및 Custom Attribute 주입
  -> [Phase 4: 킥스타트 배포 리스트 추출] 생성된 VM의 MAC 주소 수집 -> Kickstart 규격용 텍스트(.txt) 출력
  -> [Phase 5: 인프라 통합 튜닝 (전원 + VMX + 어피니티)]
     - Phase A: ESXi 물리 호스트 전원 정책 -> 고성능(High Performance) 일괄 적용
     - Phase B: VM 성능 파라미터(HugePage, Core/Socket) 및 CPU 어피니티 -> 단일 Task로 통합 병합 주입
  -> [종료: vCenter 세션 안전 종료 및 완료]

## 단계별 상세

### Phase 1: ESXi 호스트 자동 등록 (Host Addition)
- worklist.txt로부터 물리 서버의 IP/DNS 목록을 읽어 vCenter 내 대상 위치(폴더/클러스터/데이터센터)에 등록
- -RunAsync로 백그라운드 태스크 동시 등록 후 Wait-Task로 대기
- vCenter 계정과 ESXi root 계정 자격증명을 분리 로드

### Phase 2: 포트 그룹 초고속 생성 (Network Provisioning)
- vswitch.txt(호스트, 포트그룹, VLAN) 파싱 후 HostPortGroupSpec으로 vSwitch0 하위에 포트그룹/VLAN 생성
- AlreadyExists 에러는 스킵 처리

### Phase 3: VM 동적 생성 및 속성 주입 (VM Provisioning)
- BM이름ev01/ev02/ev03 형식으로 VM 동적 생성, 여유 공간이 가장 많은 데이터스토어 자동 선택
- MemoryReservationLockedToMax, sched.mem.pin 등 VMX 파라미터 주입
- Custom Attribute(DEPT_NAME/PURPOSE/VM_TYPE) 각인

### Phase 4: Kickstart 리스트 추출
- 생성된 VM의 첫 NIC MAC 주소 추출, Provisioning_List_[IP].txt 저장

### Phase 5: 인프라 통합 성능 튜닝
- 호스트 전원 정책 고성능(static) 일괄 적용
- EV01: vCPU 1:1 매핑 어피니티 자동 계산, EV02: affinity.txt 커스텀 값 적용
- VMX 성능 파라미터(1GB HugePage, coresPerSocket)까지 단일 ReconfigVM_Task로 병합 적용
