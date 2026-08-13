<#
.SYNOPSIS
    여러 vCenter에 동시 접속하여 특정 VM 그룹의 메모리 Ratio(Shares)를 일괄 조정하는 스크립트
#>
param (
    [string]$vCenter,
    [string]$WorklistPath = ".\worklist.txt",
    [int]$Ev01Ratio = 20,
    [int]$Ev02Ratio = 10
)

# 1. vCenter 입력 확인
if ([string]::IsNullOrWhiteSpace($vCenter)) {
    $vCenter = Read-Host "vCenter 주소를 입력하세요"
}

$cred = Get-Credential -Message "vCenter 계정 정보를 입력하세요"
Connect-VIServer -Server $vCenter -Credential $cred -ErrorAction Stop | Out-Null

if (-not (Test-Path $WorklistPath)) {
    Write-Warning "워크리스트 파일이 없습니다: $WorklistPath. 전체 VM을 대상으로 진행합니다."
    $targetVMs = Get-VM
} else {
    $names = Get-Content $WorklistPath | Where-Object { $_ -match '\S' }
    $targetVMs = Get-VM | Where-Object { $names -contains $_.Name }
}

foreach ($vm in $targetVMs) {
    $ratio = if ($vm.Name -match "ev01") { $Ev01Ratio } elseif ($vm.Name -match "ev02") { $Ev02Ratio } else { $null }
    if ($null -eq $ratio) { continue }

    $spec = New-Object VMware.Vim.VirtualMachineConfigSpec
    $spec.CpuAllocation = New-Object VMware.Vim.ResourceAllocationInfo
    $spec.CpuAllocation.Shares = New-Object VMware.Vim.SharesInfo
    $spec.CpuAllocation.Shares.Level = "Custom"
    $spec.CpuAllocation.Shares.Shares = $ratio * 1000

    $vm.ExtensionData.ReconfigVM_Task($spec) | Out-Null
    Write-Host "$($vm.Name): Ratio $ratio 적용 완료"
}

Disconnect-VIServer -Server $vCenter -Confirm:$false
