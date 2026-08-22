# 오프라인 설치 패키지 구조도 (Phase 2 산출물)

인터넷이 되는 환경(예: Rocky Linux)에서 아래 구조로 번들을 만들고, 통째로 압축해서 폐쇄망(RHEL 8.10 등) 서버로 이관한다. `vc-test-env`가 이미 채택한 "폴더 하나에 vendor까지 전부 포함" 방식과 동일한 원칙을 따른다.

```
vcenter-ps-bundle/
├── setup.sh                        # 폐쇄망 설치 스크립트 (본 문서 옆 setup.sh)
├── VERSIONS.txt                    # 번들에 포함된 각 구성요소 버전 고정 기록
├── powershell/
│   └── powershell-7.6.4-linux-<arch>.tar.gz     # 공식 릴리스 tar.gz (x64/arm64 등 타겟별)
├── modules/
│   ├── VMware.PowerCLI/            # Save-Module -Name VMware.PowerCLI -Path 로 받은 오프라인 모듈 전체
│   └── PSReadLine/                 # Save-Module -Name PSReadLine -Path 로 받은 오프라인 모듈 전체 (2.2.2+)
├── profile/
│   ├── VCenterAdvisory.psm1        # Phase 3: 순차 조회 권고 프록시 함수 모듈
│   └── VCenterCompleters.psm1      # Phase 3: ArgumentCompleter 모듈
└── vc-test-env/
    ├── vc-test-env                 # 미리 빌드된 실행 바이너리 (또는 소스+vendor 통째로 포함해 현장 빌드)
    └── export_vcsim_env.sh
```

## 각 구성요소 확보 방법 (인터넷 되는 환경에서 1회 수행)

```bash
# 1) PowerShell 7.6.4 릴리스 tar.gz 다운로드 (타겟 아키텍처에 맞게)
curl -LO https://github.com/PowerShell/PowerShell/releases/download/v7.6.4/powershell-7.6.4-linux-x64.tar.gz

# 2) PowerCLI / PSReadLine을 오프라인 모듈 폴더로 저장
pwsh -NoProfile -Command "Save-Module -Name VMware.PowerCLI -Path ./modules -Repository PSGallery"
pwsh -NoProfile -Command "Save-Module -Name PSReadLine -Path ./modules -Repository PSGallery"

# 3) vc-test-env는 형제 프로젝트에서 vendor 포함 빌드 (해당 README 3장 참고)
```

`VERSIONS.txt`에는 위 3가지의 정확한 버전 문자열을 기록해, 이후 재현·업그레이드 시 비교 기준으로 삼는다.

## 이관

```bash
tar czf vcenter-ps-bundle.tar.gz vcenter-ps-bundle/
# USB/scp로 폐쇄망 서버에 복사 후
tar xzf vcenter-ps-bundle.tar.gz
cd vcenter-ps-bundle
sudo bash setup.sh
```
