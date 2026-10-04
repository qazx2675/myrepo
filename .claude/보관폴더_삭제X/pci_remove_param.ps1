<#
.SYNOPSIS
    매개변수 제거스크립트 - vCenter 단일 접속 후 VM에서 특정 PCI 패스스루 디바이스 제거
.DESCRIPTION
    RHEL 8.10 / PowerShell 7.5.4 호환 파일 기반 인증 정보 로드 방식 사용.
#>
param (
    [Parameter(Mandatory=$true, HelpMessage="vCenter IP를 입력하세요")]
    [string]$vcTargetIP
)

$vcUser = "lscsystems@vsphere.local"

# 1. 파일 기반 인증 정보 로드 (RHEL 8.10 / PS 7.5.4 호환)
$scriptDir = if ($PSScriptRoot) { $PSScriptRoot } else { (Get-Location).Path }
$credPath = Join-Path $scriptDir "vc_cred.xml"
if (-not (Test-Path $credPath)) {
    $cred = Get-Credential -UserName $vcUser -Message "vCenter 계정 정보를 입력하세요"
    $cred | Export-CliXml -Path $credPath
} else {
    $cred = Import-CliXml -Path $credPath
}

Connect-VIServer -Server $vcTargetIP -Credential $cred -ErrorAction Stop | Out-Null

$vmName = Read-Host "PCI 디바이스를 제거할 VM 이름을 입력하세요"
$vm = Get-VM -Name $vmName -ErrorAction Stop

$passthroughDevices = Get-PassthroughDevice -VM $vm
if ($passthroughDevices.Count -eq 0) {
    Write-Host "해당 VM에 등록된 패스스루 디바이스가 없습니다."
} else {
    $passthroughDevices | ForEach-Object {
        Write-Host "제거 중: $($_.Name)"
        Remove-PassthroughDevice -PassthroughDevice $_ -Confirm:$false
    }
    Write-Host "PCI 디바이스 제거 완료: $vmName"
}

Disconnect-VIServer -Server $vcTargetIP -Confirm:$false
