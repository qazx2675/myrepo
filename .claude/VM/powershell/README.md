# powershell (일반 업무용 pwsh 설치)

폐쇄망에 **일반 업무용 PowerShell + PowerCLI**를 설치하는 스크립트. `../vcenter-test-env-vcsim/vcenter-powershell-autocompletion/`에 있는 실습용 설치 스크립트와 번들 형식(파일 배치 방식)은 동일하지만, 이쪽은 순차조회 권고(`VCenterAdvisory.psm1`)와 vCenter 인벤토리 자동완성(`VCenterCompleters.psm1`) 프로필을 프로필에 등록하지 않는다. 그 두 기능은 교육/실습 용도로만 쓰기로 했고, 일반 업무에서는 불필요한 오버헤드이기 때문이다.

## 폴더 구조

```
powershell/
├── setup_폐쇄망pwsh.sh
├── powershell/          # powershell-*-linux-<arch>.tar.gz 를 여기에 넣기
└── module/               # VMware.PowerCLI / PSReadLine .nupkg 를 여기에 그대로 넣기 (압축 해제 불필요)
```

## 설치

```bash
cd powershell
sudo bash setup_폐쇄망pwsh.sh
```

- PowerShell 바이너리 배치, 폐쇄망 업데이트 확인 비활성화(`POWERSHELL_UPDATECHECK=Off`), 모듈(`.nupkg`) 자동 압축 해제·배치, (PowerCLI가 포함된 경우) CEIP 비활성화까지만 하고 끝난다.
- 자동완성/권고 관련 프로필 등록은 하지 않으므로, `$PROFILE`은 설치 후에도 비어 있다.

## 다운로드 링크

`../vcenter-test-env-vcsim/vcenter-powershell-autocompletion/README.md`의 "필요 다운로드" 섹션과 동일 (PowerShell 7.6.4, VMware.PowerCLI, PSReadLine).
