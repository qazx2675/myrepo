<#
.SYNOPSIS
    vCenter VM 인벤토리 상태 수집 스크립트 (RHEL 8.10 리눅스 PowerShell 7 환경 호환)
.DESCRIPTION
    전체 VM의 이름/전원상태/CPU/Mem/데이터스토어/IP 등을 조회하여
    vcenter_vm_inventory_status.txt 로 저장.
#>
param (
    [Parameter(Mandatory=$false)]
    [string]$outputFileName = "vcenter_vm_inventory_status.txt"
)

# 1. 파일 기반 환경 설정 (RHEL 8.10 리눅스 대소문자 및 경로 호환)
$scriptDir = if ($PSScriptRoot) { $PSScriptRoot } else { (Get-Location).Path }
$outputPath = Join-Path $scriptDir $outputFileName

$credPath = Join-Path $scriptDir "vc_cred.xml"
if (-not (Test-Path $credPath)) {
    $cred = Get-Credential -Message "vCenter 계정 정보를 입력하세요"
    $cred | Export-CliXml -Path $credPath
} else {
    $cred = Import-CliXml -Path $credPath
}

$vcTargetIP = Read-Host "vCenter IP를 입력하세요"
Connect-VIServer -Server $vcTargetIP -Credential $cred -ErrorAction Stop | Out-Null

$vmList = Get-VM | Sort-Object Name

$report = foreach ($vm in $vmList) {
    [PSCustomObject]@{
        Name        = $vm.Name
        PowerState  = $vm.PowerState
        NumCpu      = $vm.NumCpu
        MemoryGB    = $vm.MemoryGB
        Datastore   = ($vm | Get-Datastore | Select-Object -First 1 -ExpandProperty Name)
        IPAddress   = ($vm.Guest.IPAddress -join ",")
        Host        = $vm.VMHost.Name
    }
}

$report | Format-Table -AutoSize | Out-String -Width 300 | Out-File -FilePath $outputPath -Encoding UTF8

Write-Host "인벤토리 상태가 저장되었습니다: $outputPath"
Disconnect-VIServer -Server $vcTargetIP -Confirm:$false
