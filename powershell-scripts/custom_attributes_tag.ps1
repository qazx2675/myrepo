<#
.SYNOPSIS
    VM Custom Attribute(DeptName/Purpose) 일괄 태깅 스크립트 (ev01/ev02 공통+전용 특성 지원)
#>
param (
    [Parameter(Mandatory=$true)]
    [string]$vcTargetIP,
    [Parameter(Mandatory=$true)]
    [string]$deptName,          # ev01, ev02 공통 부서 태그
    [Parameter(Mandatory=$true)]
    [string]$purposeEv01,       # ev01 전용 PURPOSE 태그
    [Parameter(Mandatory=$true)]
    [string]$purposeEv02        # ev02 전용 PURPOSE 태그
)

$vcUser = "administrator@vsphere.local"
$scriptDir = if ($PSScriptRoot) { $PSScriptRoot } else { (Get-Location).Path }
$credPath = Join-Path $scriptDir "vc_cred.xml"
if (-not (Test-Path $credPath)) {
    $cred = Get-Credential -UserName $vcUser -Message "vCenter 계정 정보를 입력하세요"
    $cred | Export-CliXml -Path $credPath
} else {
    $cred = Import-CliXml -Path $credPath
}

Connect-VIServer -Server $vcTargetIP -Credential $cred -ErrorAction Stop | Out-Null

foreach ($attrName in @("DEPT_NAME", "PURPOSE")) {
    if (-not (Get-CustomAttribute -Name $attrName -ErrorAction SilentlyContinue)) {
        New-CustomAttribute -Name $attrName -TargetType VirtualMachine | Out-Null
    }
}

$vms = Get-VM
foreach ($vm in $vms) {
    Set-Annotation -Entity $vm -CustomAttribute "DEPT_NAME" -Value $deptName | Out-Null
    if ($vm.Name -match "ev01") {
        Set-Annotation -Entity $vm -CustomAttribute "PURPOSE" -Value $purposeEv01 | Out-Null
    } elseif ($vm.Name -match "ev02") {
        Set-Annotation -Entity $vm -CustomAttribute "PURPOSE" -Value $purposeEv02 | Out-Null
    }
}

Write-Host "Custom Attribute 태깅 완료 (대상 VM: $($vms.Count)대)"
Disconnect-VIServer -Server $vcTargetIP -Confirm:$false
