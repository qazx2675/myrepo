<#
.SYNOPSIS
    다수 vCenter/VM 대상 CPU Affinity 병렬 설정 스크립트
#>
param (
    [Parameter(Mandatory=$true)]
    [string]$vcTargetIP,
    [Parameter(Mandatory=$false)]
    [int]$maxParallel = 20
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

$vms = Get-VM
$pool = [RunspaceFactory]::CreateRunspacePool(1, $maxParallel)
$pool.Open()
$jobs = @()

$scriptBlock = {
    param($vm)
    try {
        $spec = New-Object VMware.Vim.VirtualMachineConfigSpec
        $spec.CpuAffinity = New-Object VMware.Vim.VirtualMachineAffinityInfo
        $spec.CpuAffinity.AffinitySet = 0..($vm.NumCpu - 1)
        $vm.ExtensionData.ReconfigVM_Task($spec) | Out-Null
        return "$($vm.Name): CPU Affinity 설정 완료"
    } catch {
        return "$($vm.Name): 오류 - $($_.Exception.Message)"
    }
}

foreach ($vm in $vms) {
    $ps = [PowerShell]::Create().AddScript($scriptBlock).AddArgument($vm)
    $ps.RunspacePool = $pool
    $jobs += [PSCustomObject]@{ Pipe = $ps; Handle = $ps.BeginInvoke() }
}

foreach ($job in $jobs) {
    $job.Pipe.EndInvoke($job.Handle) | ForEach-Object { Write-Host $_ }
    $job.Pipe.Dispose()
}
$pool.Close()
$pool.Dispose()

Disconnect-VIServer -Server $vcTargetIP -Confirm:$false
