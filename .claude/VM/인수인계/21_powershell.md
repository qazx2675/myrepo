# 21. powershell — 폐쇄망 PowerShell + PowerCLI 설치

## 한눈에 보기

| 항목 | 값 |
|---|---|
| **위험도** | 🟡 **작업용 PC/서버에만 설치.** vCenter 인프라는 건드리지 않습니다 |
| 폴더 | `.claude/VM/powershell/` |
| 하는 일 | 인터넷이 없는 폐쇄망 리눅스에 **일반 업무용 PowerShell + VMware PowerCLI**를 설치 |
| 실행 | `sudo bash setup_폐쇄망pwsh.sh` |

---

## 1. 이 폴더는 무엇인가

Go 도구로 다 되지 않는 작업(임시 조회, 일회성 스크립트)은 **PowerCLI**(VMware의 PowerShell 모듈)로 하는 게 편합니다. 그런데 폐쇄망에서는 `Install-Module`이 동작하지 않습니다.

이 폴더는 **미리 받아둔 설치 파일들을 배치해서 오프라인으로 설치**하는 스크립트입니다.

---

## 2. 사전 준비 — 파일 배치

설치 파일을 직접 받아서 폴더에 넣어야 합니다.

```
powershell/
├── setup_폐쇄망pwsh.sh
├── powershell/          ← powershell-*-linux-<arch>.tar.gz 를 여기에 넣기
└── module/              ← VMware.PowerCLI / PSReadLine .nupkg 를 여기에 그대로 넣기
                           (압축 해제 불필요)
```

필요한 다운로드 목록(버전 포함)은 `../vcenter-test-env-vcsim/vcenter-powershell-autocompletion/README.md`의 **"필요 다운로드"** 섹션과 동일합니다: PowerShell 7.6.4, VMware.PowerCLI, PSReadLine.

---

## 3. 설치

```bash
cd .claude/VM/powershell
sudo bash setup_폐쇄망pwsh.sh
```

스크립트가 하는 일:

1. PowerShell 바이너리 배치
2. 폐쇄망 업데이트 확인 비활성화 (`POWERSHELL_UPDATECHECK=Off`)
3. 모듈(`.nupkg`) 자동 압축 해제·배치
4. (PowerCLI가 포함된 경우) CEIP(고객 체험 개선 프로그램) 비활성화

**여기까지만 하고 끝납니다.**

---

## 4. 이 폴더와 실습용 버전의 차이

`../vcenter-test-env-vcsim/vcenter-powershell-autocompletion/`에 거의 같은 설치 스크립트가 하나 더 있습니다. **번들 형식(파일 배치 방식)은 동일**하지만 차이가 있습니다.

| | 이 폴더 (`powershell/`) | 실습용 (`vcenter-powershell-autocompletion/`) |
|---|---|---|
| 용도 | **일반 업무용** | 교육/실습용 |
| 순차조회 권고 (`VCenterAdvisory.psm1`) | ❌ 등록 안 함 | ✅ 프로필에 등록 |
| vCenter 인벤토리 자동완성 (`VCenterCompleters.psm1`) | ❌ 등록 안 함 | ✅ 프로필에 등록 |
| 설치 후 `$PROFILE` | **비어 있음** | 자동완성/권고 프로필이 들어감 |

> 자동완성/권고 기능은 **일반 업무에서는 불필요한 오버헤드**라고 판단해 이쪽에서는 등록하지 않습니다.

---

## 5. 설치 후 사용 예

```powershell
pwsh
Connect-VIServer -Server 192.168.0.50 -User administrator@vsphere.local -Force
Get-VM
Get-VMHost
Disconnect-VIServer -Confirm:$false
```

vcsim(테스트 환경)에 붙일 수도 있습니다:

```powershell
Connect-VIServer -Server 127.0.0.1:54321 -User administrator@vsphere.local -Password 아무값 -Force
```

> ⚠️ vcsim에 PowerCLI로 붙을 때 `Get-View`의 `Runtime`/`Summary` 조회는 에러가 납니다. 우회 방법은 [18번 문서](./18_vcenter-test-env-vcsim.md)의 "PowerCLI Get-View 에러"를 보세요.

---

## 6. 파일 구조

```
powershell/
├── README.md               # 1차 자료
├── setup_폐쇄망pwsh.sh      # ★ 설치 스크립트
├── powershell/             # PowerShell tar.gz 를 넣는 곳
└── module/                 # .nupkg 모듈을 넣는 곳
```

---

## 7. 관련 문서

- 1차 자료: `powershell/README.md`
- 다운로드 목록 및 실습용 버전: `vcenter-test-env-vcsim/vcenter-powershell-autocompletion/README.md`
- PowerCLI로 붙을 테스트 환경: [18. vc-test-env](./18_vcenter-test-env-vcsim.md)
