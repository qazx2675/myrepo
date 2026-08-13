<#
.SYNOPSIS
    vCenter 대상 ESXi 호스트에서 활성화된 PCI 패스스루 디바이스(벤더명 기준)를
    동일 이름의 VM에 병렬로 장착하는 스크립트 (PowerCLI)
.DESCRIPTION
    - vCenter IP / PCI 벤더명(or VendorId) / 병렬 처리 개수를 파라미터로 입력
    - 파일 기반 인증 정보 로드로 보안 객체 직렬화 오류 방지
    - Get-VMHost -> Get-VMHostPciDevice 로 대상 PCI 디바이스 검색 후 Add-PassthroughDevice
    - RunspacePool 기반 병렬 처리 (maxParallel로 동시 실행 수 제한)
#>
param (
    [Parameter(Mandatory=$true, HelpMessage="vCenter IP를 입력하세요")]
    [string]$vcTargetIP,
    [Parameter(Mandatory=$true, HelpMessage="추가할 PCI 벤더 이름이나 키워드(예: Mellanox, NVIDIA, 0000:01:00.0)를 입력하세요")]
    [string]$vendorName,
    [Parameter(Mandatory=$false)]
    [int]$maxParallel = 10
)

$vcUser = "administrator@vsphere.local"

# 1. 파일 기반 인증 정보 로드 (보안 객체 직렬화 오류 방지)
$scriptDir = if ($PSScriptRoot) { $PSScriptRoot } else { (Get-Location).Path }
$credPath = Join-Path $scriptDir "vc_cred.xml"
if (-not (Test-Path $credPath)) {
    $cred = Get-Credential -UserName $vcUser -Message "vCenter 계정 정보를 입력하세요"
    $cred | Export-CliXml -Path $credPath
} else {
    $cred = Import-CliXml -Path $credPath
}

Connect-VIServer -Server $vcTargetIP -Credential $cred -ErrorAction Stop | Out-Null

$vmHosts = Get-VMHost
$results = @()

$scriptBlock = {
    param($vmHost, $vendorName)
    $device = Get-VMHostPciDevice -VMHost $vmHost | Where-Object {
        $_.DeviceName -match $vendorName -or $_.VendorId -match $vendorName
    } | Select-Object -First 1

    if ($null -eq $device) { return "$($vmHost.Name): 대상 PCI 없음" }

    $matchedVM = Get-VM | Where-Object { $_.Name -eq $vmHost.Name.Split('.')[0] }
    if ($null -eq $matchedVM) { return "$($vmHost.Name): 동일 이름의 VM 없음" }

    try {
        Add-PassthroughDevice -VM $matchedVM -PciDevice $device -ErrorAction Stop | Out-Null
        return "$($vmHost.Name): PCI 패스스루 장착 완료"
    } catch {
        return "$($vmHost.Name): 오류 - $($_.Exception.Message)"
    }
}

$pool = [RunspaceFactory]::CreateRunspacePool(1, $maxParallel)
$pool.Open()
$jobs = @()

foreach ($vmHost in $vmHosts) {
    $ps = [PowerShell]::Create().AddScript($scriptBlock).AddArgument($vmHost).AddArgument($vendorName)
    $ps.RunspacePool = $pool
    $jobs += [PSCustomObject]@{ Pipe = $ps; Handle = $ps.BeginInvoke() }
}

foreach ($job in $jobs) {
    $results += $job.Pipe.EndInvoke($job.Handle)
    $job.Pipe.Dispose()
}
$pool.Close()
$pool.Dispose()

$results | ForEach-Object { Write-Host $_ }
Disconnect-VIServer -Server $vcTargetIP -Confirm:$false
