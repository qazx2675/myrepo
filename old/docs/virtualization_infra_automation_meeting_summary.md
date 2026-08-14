# 가상화 인프라(VMware) 구축 및 자동화 회의 요약

## 1. 전체 작업 프로세스 (Workflow)
- H/W 및 BIOS 설정: 장비별 최적화된 BIOS 옵션 설정 및 하드웨어 점검
- ESXi 설치: PXE(DHCP/TFTP) 환경을 통한 네트워크 기반 무인 설치
- vCenter 등록 및 클러스터 구성
- VM 생성 및 표준 사양 적용 (docs/vm_standard_instance_types.md 참조)
- 후속 점검(inspection) 및 커스텀 속성 태깅
