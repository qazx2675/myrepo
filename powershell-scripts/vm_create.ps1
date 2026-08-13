<#
.SYNOPSIS
    vCenter VM 자동 생성 스크립트 (PowerCLI)
.DESCRIPTION
    인증 정보 로드 -> vCenter 접속 -> 데이터스토어/네트워크 자동 선택 -> VM 생성
#>
$scriptDir = if ($PSScriptRoot) { $PSScriptRoot } else { (Get-Location).Path }
$credPath = Join-Path $scriptDir "vc_cred.xml"

$vcTargetIP = Read-Host "vCenter IP를 입력하세요"
$cred = if (Test-Path $credPath) { Import-CliXml -Path $credPath } else {
    $c = Get-Credential -Message "vCenter 계정 정보를 입력하세요"; $c | Export-CliXml -Path $credPath; $c
}

Connect-VIServer -Server $vcTargetIP -Credential $cred -ErrorAction Stop | Out-Null

$vmName    = Read-Host "생성할 VM 이름을 입력하세요"
$numCpu    = Read-Host "CPU 개수"
$memoryGB  = Read-Host "메모리(GB)"
$diskGB    = Read-Host "디스크 용량(GB)"

$cluster    = Get-Cluster | Select-Object -First 1
$datastore  = Get-Datastore | Sort-Object FreeSpaceGB -Descending | Select-Object -First 1
$network    = Get-VirtualPortGroup | Select-Object -First 1
$vmHost     = $cluster | Get-VMHost | Select-Object -First 1

New-VM -Name $vmName -VMHost $vmHost -Datastore $datastore -NumCpu $numCpu `
    -MemoryGB $memoryGB -DiskGB $diskGB -NetworkName $network.Name `
    -GuestId "rhel8_64Guest" -Confirm:$false

Write-Host "VM 생성 완료: $vmName (Datastore: $($datastore.Name), Host: $($vmHost.Name))"
Disconnect-VIServer -Server $vcTargetIP -Confirm:$false
