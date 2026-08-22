# vcenter-powershell-autocompletion

vCenter/PowerCLI를 처음 접하는 교육생을 위해, 터미널(Linux pwsh)에서도 IDE 수준의 명령어·API 자동완성을 제공하고 순차 조회 cmdlet 사용 시 `Get-View` 기반 고성능 대안을 안내하는 실습 환경 구축 프로젝트. 실습 대상 인프라는 형제 프로젝트 [`../` (vc-test-env)](../README.md)가 만들어 주는 vcsim으로 안전하게 대체한다.

## 문서 구성

| 문서 | 내용 |
|---|---|
| [`vcenter-powershell-autocompletion-plan.md`](./vcenter-powershell-autocompletion-plan.md) | 전체 프로젝트 계획서 |
| [`command-advisory-rules.md`](./command-advisory-rules.md) | `Get-VM` 등 → `Get-View -ViewType` 매핑 및 안내 메시지 규칙 |
| [`tooling-inventory.md`](./tooling-inventory.md) | 기존 도구 활용 범위 vs 신규 개발 범위 |
| [`offline-package-structure.md`](./offline-package-structure.md) | 폐쇄망 오프라인 번들 구조 |
| [`setup.sh`](./setup.sh) | 폐쇄망 설치 스크립트 |
| [`profile/VCenterAdvisory.psm1`](./profile/VCenterAdvisory.psm1) | 순차 조회 권고 프록시 함수 모듈 |
| [`profile/VCenterCompleters.psm1`](./profile/VCenterCompleters.psm1) | vCenter 인벤토리 이름 ArgumentCompleter 모듈 |
| [`vcsim-integration-guide.md`](./vcsim-integration-guide.md) | vcsim 연동 절차 및 스모크테스트 체크리스트 |

## 필요 다운로드

### PowerShell 7.6.4 (오프라인 번들용, `offline-package-structure.md` 1단계)

인터넷이 되는 환경에서 미리 받아 `setup.sh`가 참조하는 `powershell/` 폴더에 넣어둔다.

```bash
# x64
curl -LO https://github.com/PowerShell/PowerShell/releases/download/v7.6.4/powershell-7.6.4-linux-x64.tar.gz

# arm64
curl -LO https://github.com/PowerShell/PowerShell/releases/download/v7.6.4/powershell-7.6.4-linux-arm64.tar.gz
```

- 릴리스 페이지: https://github.com/PowerShell/PowerShell/releases/tag/v7.6.4
- (참고) 이후 패치 버전(v7.6.5 등)으로 올릴 경우 위 URL의 버전 문자열만 바꾸면 됨. 버전을 바꿀 땐 `setup.sh`가 파일명 패턴(`powershell-*-linux-<arch>.tar.gz`)으로 자동 탐지하므로 스크립트 수정은 불필요.

### PowerCLI / PSReadLine

`Save-Module`로 오프라인 저장하는 방법은 `offline-package-structure.md`의 "각 구성요소 확보 방법" 참고. 수동으로 받고 싶으면 아래 링크 사용.

**VMware.PowerCLI**
- 페이지: https://www.powershellgallery.com/packages/VMware.PowerCLI/13.3.0.24145081
- 직접 다운로드(.nupkg): https://www.powershellgallery.com/api/v2/package/VMware.PowerCLI/13.3.0.24145081
- ⚠️ 같은 버전 문자열의 `13.3.0.24145083`은 PowerShell Gallery에서 deprecated 처리되어 `VCF.PowerCLI`로 이전 안내 중 — 위 `13.3.0.24145081`이 정상 배포 버전.

**PSReadLine**
- 페이지: https://www.powershellgallery.com/packages/PSReadLine/2.4.5
- 직접 다운로드(.nupkg): https://www.powershellgallery.com/api/v2/package/PSReadLine/2.4.5
- 참고: PSReadLine 2.4.5는 PowerShell 7.6 계열에 이미 기본 포함되어 있어, 다른 버전을 강제 고정하고 싶을 때만 별도 다운로드가 필요함.

`.nupkg`는 zip 포맷이므로 확장자를 `.zip`으로 바꿔 압축을 풀면 `offline-package-structure.md`가 기대하는 `modules/<ModuleName>/` 폴더 구조를 만들 수 있음.

## 현재 상태

- Phase 1~4 설계/코드 산출물 작성 완료 (Windows 환경에서 문법 검증만 완료, 실제 pwsh 실행 검증은 아직)
- Phase 5(통합 테스트 및 교육 환경 검증)는 실제 Rocky Linux/RHEL 환경에서 진행 필요
