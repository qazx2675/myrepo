<#
.SYNOPSIS
    ESXi 호스트 전원 정책 원격 제어 스크립트
.DESCRIPTION
    vCenter 접속 후 대상 호스트에 대해 on/off/reset/soft-off/soft-reset 동작 수행
#>
param (
    [Parameter(Mandatory=$true)]
    [string]$vcTargetIP,
    [Parameter(Mandatory=$true)]
    [ValidateSet("on", "off", "reset", "soft-off", "soft-reset")]
    [string]$powerAction
)

$vcUser = "lscsystems@vsphere.local"
$scriptDir = if ($PSScriptRoot) { $PSScriptRoot } else { (Get-Location).Path }
$credPath = Join-Path $scriptDir "vc_cred.xml"
if (-not (Test-Path $credPath)) {
    $cred = Get-Credential -UserName $vcUser -Message "vCenter 계정 정보를 입력하세요"
    $cred | Export-CliXml -Path $credPath
} else {
    $cred = Import-CliXml -Path $credPath
}

Connect-VIServer -Server $vcTargetIP -Credential $cred -ErrorAction Stop | Out-Null

$vmName = Read-Host "전원 동작을 수행할 VM 이름을 입력하세요"
$vm = Get-VM -Name $vmName -ErrorAction Stop

switch ($powerAction) {
    "on"         { Start-VM -VM $vm -Confirm:$false }
    "off"        { Stop-VM -VM $vm -Confirm:$false }
    "reset"      { Restart-VM -VM $vm -Confirm:$false }
    "soft-off"   { Shutdown-VMGuest -VM $vm -Confirm:$false }
    "soft-reset" { Restart-VMGuest -VM $vm -Confirm:$false }
}

Write-Host "$vmName 에 대해 '$powerAction' 동작을 수행했습니다."
Disconnect-VIServer -Server $vcTargetIP -Confirm:$false
