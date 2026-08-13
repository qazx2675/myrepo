<#
.SYNOPSIS
    VM에서 vusb0 (USB 컨트롤러) 디바이스를 일괄 제거하는 스크립트
#>
param (
    [Parameter(Mandatory=$true)]
    [string]$vcTargetIP,
    [Parameter(Mandatory=$false)]
    [int]$maxParallel = 10
)

$vcUser = "administrator@vsphere.local"
$scriptDir = if ($PSScriptRoot) { $PSScriptRoot } else { (Get-Location).Path }
$credPath = Join-Path $scriptDir "vc_cred.xml"
$cred = if (Test-Path $credPath) { Import-CliXml -Path $credPath } else {
    $c = Get-Credential -UserName $vcUser; $c | Export-CliXml -Path $credPath; $c
}

Connect-VIServer -Server $vcTargetIP -Credential $cred -ErrorAction Stop | Out-Null

$vms = Get-VM
foreach ($vm in $vms) {
    $usbDevices = Get-USBDevice -VM $vm -ErrorAction SilentlyContinue
    if ($usbDevices) {
        $usbDevices | Remove-USBDevice -Confirm:$false
        Write-Host "$($vm.Name): vusb0 제거 완료"
    }
}

Disconnect-VIServer -Server $vcTargetIP -Confirm:$false
