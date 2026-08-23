# VM

ESXi/vCenter 운영·점검·자동화 관련 도구 모음. 폴더별 상세 내용은 각 폴더 안의 `README.md` 참고.

- `esxi-log-check` : ESXi 치명적 로그(MCE, PSOD, APD/PDL, vSAN ESA, NVMeoF 단절 등) 수집·분석 도구 + 검증용 모의(Mock) ESXi 환경
- `gemini_vcsim-pipeline-test` : govmomi 내장 vcsim(vCenter Simulator)으로 VM 생성/설정/점검/수정 전체 흐름을 검증하는 통합 테스트 프레임워크
- `integrated-vm-param-check-test-tool` : `vm-param-check`(체크+자동교정)와 `vc-test-env`(vcsim 복제)를 한 폴더로 묶어, 폐쇄망 서버로 그대로 옮겨 빌드→테스트까지 끝낼 수 있게 만든 배포 패키지
- `VM_setup` : VM 설정 적용(affinity/lpage/전원정책/태그/vSwitch/라이선스 할당/VM 생성 등)용 스크립트 모음
- `lpage_search` : ESXi Large Page(2MB) 메모리 사이징 계산기. vCenter/ESXi 접속 없이 순수 계산만 수행하는 단일 파일 Go 프로그램
- `powershell` : 폐쇄망에 일반 업무용 PowerShell + PowerCLI를 설치하는 스크립트 모음
- `vcenter-test-env-vcsim` : 실 vCenter의 인벤토리 구조를 읽어와 vcsim 위에 그대로 재현하는 도구. 실 vCenter를 건드리지 않고 다른 도구를 테스트할 때 사용
- `vm-param-check-usability-improvement` : VM 매개변수 체크/설정 통합 툴의 사용성·성능 개선 버전(옵션 자동화, 조회 속도 개선). 실제 도구는 이 폴더 아래 `vm-param-check/`
- `vm-param-setting-check` : vCenter의 VM들이 고성능 설정 기준(CPU/메모리/NUMA, vCPU affinity, Shares, 전원정책 등)을 만족하는지 자동 점검하는 도구
- `vm-setting-go-lang` : worklist 기반으로 VM에 설정 적용/생성, ESXi 호스트를 vCenter에 등록하는 govmomi 기반 도구 4종(vm_affinity_bulk, vm_lpage_bulk, vm_create, vm_connect)
- `vm_verifier` : VM 생성 직후(파워온 전) vCenter가 인식한 vNIC MAC과 DHCP 예약 MAC을 대조해 교차 설치(역설치)를 탐지하는 에이전트리스 도구
- `test_all.sh` : 록키 리눅스(192.168.0.60)에 SSH로 접속해 `/root/VM` 밑의 모든 `setup.sh`를 찾아 순회 빌드하는 스크립트
