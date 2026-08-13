# VMware 원격 조작/정보수집 자동화 환경 구축 요구사항

## Python 기반 자동화 환경 (pyVmomi)
vSphere API를 Python으로 제어하기 위해 가장 널리 사용되는 라이브러리.

## PowerShell(pwsh) 기반 자동화 환경 (PowerCLI)
- RHEL 8.10 / PowerShell 7.5.4 환경에서 VMware.PowerCLI 모듈 설치 후 사용
- 크로스 플랫폼 지원으로 리눅스 서버에서도 파워쉘 스크립트 실행 가능

## 실사례: VMware 기반 가상화 인프라 운영
'ESXi 호스트 - VM 생성 - vCenter 통합 관리' 구조
1. Bare Metal(BM) 기반 ESXi 설치 — 고성능 물리 서버에 ESXi 하이퍼바이저 설치
2. VM 생성 및 리소스 할당
3. vCenter를 통한 통합 관리/모니터링

## VMware 개요
- 가상화 기술의 선두주자: 물리적인 하드웨어 자원(CPU, 메모리, 저장장치 등)을 소프트웨어적으로
  분리하여, 하나의 물리 서버에서 여러 개의 독립적인 운영체제(OS)를 실행
