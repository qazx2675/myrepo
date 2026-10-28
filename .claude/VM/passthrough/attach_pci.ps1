param (
    [Parameter(Mandatory=$true, HelpMessage="vCenter IP를 입력하세요")]
    [string]$vcTargetIP,

    [Parameter(Mandatory=$true, HelpMessage="찾을 PCI 벤더명이나 VendorId (예: Mellanox, 5555, 0000:06:00.0)")]
    [string]$pciVendor
)

$vcUser = "lscsystems@vsphere.local"

# 1. 파일 기반 인증 정보 로드 (RHEL 8.10 / PS 7.5.4 호환)
$scriptDir = if ($PSScriptRoot) { $PSScriptRoot } else { (Get-Location).Path }
$keyPathStr = Join-Path $scriptDir "Create_VM_pwsh_dir/password_dir/secret_VMw.key"
$passPathStr = Join-Path $scriptDir "Create_VM_pwsh_dir/password_dir/password_VMw.enc"

try {
    [byte[]]$localKeyBytes = [System.IO.File]::ReadAllBytes($keyPathStr)
    $localEncText = [System.IO.File]::ReadAllText($passPathStr).Trim()
    $vcSecurePassword = $localEncText | ConvertTo-SecureString -Key $localKeyBytes
    $globalCred = [PSCredential]::new($vcUser, $vcSecurePassword)
} catch {
    Write-Host "[오류] 인증 정보 로드 실패 : $($_.Exception.Message)" -ForegroundColor Red
    exit
}

Set-PowerCLIConfiguration -InvalidCertificateAction Ignore -Confirm:$false | Out-Null

$worklistPath = Join-Path $scriptDir "worklist_pci.txt"
if (-not (Test-Path $worklistPath)) { Write-Error "worklist_pci.txt 파일을 찾을 수 없습니다."; exit }
$vmListLines = Get-Content $worklistPath | Where-Object { $_.Trim() -ne "" }

try {
    # 2. 초고속 단일 세션 연결 (Connect 1회 원칙)
    Write-Host "`nvCenter($vcTargetIP)에 접속 중입니다..." -ForegroundColor Cyan
    $vCenterConn = Connect-VIServer -Server $vcTargetIP -Credential $globalCred -ErrorAction Stop
    Write-Host "-> [CHECKBOX] vCenter 접속 완료" -ForegroundColor Gray

    Write-Host "`n=======================================================" -ForegroundColor Yellow
    Write-Host " 가상 머신(VM) 순수 DirectPath IO (PCI) 연결 시작" -ForegroundColor Green
    Write-Host "=======================================================" -ForegroundColor Yellow

    # 3. 대상 VM 순회 및 작업
    foreach ($line in $vmListLines) {
        $vmName = $line.Trim()

        # Get-VM으로 VM 객체 로드
        $vmo = Get-VM -Name $vmName -Server $vCenterConn -ErrorAction SilentlyContinue
        if (-not $vmo) {
            Write-Host "`n[$vmName] [오류] VM을 인벤토리에서 찾을 수 없습니다." -ForegroundColor Red
            continue
        }

        $ho = $vmo.VMHost
        Write-Host "`n[$vmName] PCI 장착 작업 시작... (소속 호스트: $($ho.Name))" -ForegroundColor Cyan

        if ($vmo.PowerState -eq 'PoweredOn') {
            Write-Host "  -> [경고] VM이 켜져 있습니다. 작업 실패 시 VM을 끄고 시도하세요." -ForegroundColor Magenta
        }

        # [핵심 로직] 호스트의 활성화된 패스스루(DirectPath IO) 장치 목록 로드
        $hostPcis = Get-PassthroughDevice -VMHost $ho -Type Pci -ErrorAction SilentlyContinue

        # 입력받은 $pciVendor(예: Mellanox 또는 5555) 값으로 Vendor Name, Vendor Id, 장치명 매칭
        $matchedPciDevs = $hostPcis | Where-Object {
            $_.Vendor.Name -match $pciVendor -or
            $_.Vendor.Id -match $pciVendor -or
            $_.Name -match $pciVendor
        }

        if (-not $matchedPciDevs) {
            Write-Host "  -> [스킵] 호스트에 사용 가능한 '$pciVendor' 장치가 없습니다. (패스스루 활성화 확인 요망)" -ForegroundColor Gray
            continue
        }

        # 검색된 첫 번째 장치 선택
        $targetPci = $matchedPciDevs[0]
        Write-Host "  -> [발견] 대상 디바이스: $($targetPci.Name) (VendorId: $($targetPci.Vendor.Id))" -ForegroundColor White

        # VM에 이미 장착되어 있는지 중복 검사
        $existingPci = Get-PassthroughDevice -VM $vmo -Type Pci -ErrorAction SilentlyContinue
        $isAlreadyAttached = $false
        if ($existingPci) {
            foreach ($ep in $existingPci) {
                if ($ep.ExtensionData.Id -eq $targetPci.ExtensionData.Id) {
                    $isAlreadyAttached = $true
                    break
                }
            }
        }

        if ($isAlreadyAttached) {
            Write-Host "  -> [스킵] 이미 동일한 PCI 장치가 VM에 장착되어 있습니다." -ForegroundColor Gray
        } else {
            try {
                Write-Host "  -> [진행] 네이티브 파워쉘 명령어로 안전하게 장치를 추가합니다..." -ForegroundColor Yellow

                # 고급 매개변수나 메모리 조작 없이 100% GUI 수동 추가(DirectPath IO)와 동일한 동작 수행
                Add-PassthroughDevice -VM $vmo -PassthroughDevice $targetPci -Confirm:$false | Out-Null

                Write-Host "  -> [성공] VM($vmName)에 순수 PCI 장착(DirectPath IO)이 완료되었습니다!" -ForegroundColor Green
            } catch {
                Write-Host "  -> [실패] 장착 중 API 에러: $($_.Exception.Message)" -ForegroundColor Red
            }
        }
    }
} finally {
    if ($vCenterConn) { Disconnect-VIServer -Server $vCenterConn -Confirm:$false -ErrorAction SilentlyContinue }
}
