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
- ⚠️ **이 .nupkg 하나만 받아서는 안 됨.** `VMware.PowerCLI`는 자체 코드가 없는 메타 패키지라 실제
  기능은 `RequiredModules`로 선언된 하위 모듈 80여 개(`VMware.VimAutomation.Sdk` 등)에 들어있는데,
  이 다운로드 링크는 딱 그 패키지 자신의 nupkg만 주고 의존성은 안 준다. 이 방법만 쓰면
  `Import-Module VMware.PowerCLI` 시점에 `The required module 'VMware.VimAutomation.Sdk' is not
  loaded` 에러로 실패한다(실제로 겪음). **의존성까지 전부 받으려면 위 `Save-Module` 방법을 쓸 것.**
- ⚠️ 같은 버전 문자열의 `13.3.0.24145083`은 PowerShell Gallery에서 deprecated 처리되어 `VCF.PowerCLI`로 이전 안내 중 — 위 `13.3.0.24145081`이 정상 배포 버전.

**PSReadLine**
- 페이지: https://www.powershellgallery.com/packages/PSReadLine/2.4.5
- 직접 다운로드(.nupkg): https://www.powershellgallery.com/api/v2/package/PSReadLine/2.4.5
- 참고: PSReadLine 2.4.5는 PowerShell 7.6 계열에 이미 기본 포함되어 있어, 다른 버전을 강제 고정하고 싶을 때만 별도 다운로드가 필요함.

받은 `.nupkg` 파일은 직접 풀 필요 없이 `modules/`(또는 `module/`) 폴더에 그대로 두면 된다 — `setup.sh`가 실행 시점에 알아서 `unzip`으로 풀고, 안의 `.nuspec` 메타데이터를 읽어 `Modules/<ModuleName>/<Version>/` 구조로 자동 배치한다.

## 문제 해결

### pwsh 실행/셸 진입이 느리다 (특히 폐쇄망)

`pwsh`는 기본적으로 시작할 때마다 새 버전이 있는지 인터넷으로 확인하려 시도한다. 폐쇄망에서는 이 DNS 조회가 실패할 때까지 대기하면서 그만큼 셸 진입이 느려진다 — `A new PowerShell stable release is available: v7.x.x` 메시지가 뜨는 것도 이 기능 때문이다.

`setup.sh`가 `/etc/environment`와 `/etc/profile.d/pwsh-updatecheck-off.sh`에 `POWERSHELL_UPDATECHECK=Off`을 등록해 자동으로 꺼주지만, **이미 설치한 뒤라면 재로그인해야 적용**된다. 지금 세션에 바로 적용하려면:

```bash
export POWERSHELL_UPDATECHECK=Off
```

(참고) `VMware.PowerCLI` 자체가 여러 하위 모듈을 로드하느라 원래 import에 몇 초 걸리는 건 별개 — 이건 PowerCLI 고유의 특성이라 이 프로젝트에서 줄일 수 있는 부분이 아니다.

## 현재 상태

- Phase 1~4 설계/코드 산출물 작성 완료 (Windows 환경에서 문법 검증만 완료, 실제 pwsh 실행 검증은 아직)
- Phase 5(통합 테스트 및 교육 환경 검증)는 실제 Rocky Linux/RHEL 환경에서 진행 필요
