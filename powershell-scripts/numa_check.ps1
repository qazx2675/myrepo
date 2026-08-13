<#
.SYNOPSIS
    VM의 numa.vcpu.maxPerVirtualNode ExtraConfig 값 조회 스크립트
#>
Get-View -ViewType VirtualMachine -Property Name, Config.ExtraConfig | ForEach-Object {
    $numa = $_.Config.ExtraConfig | Where-Object { $_.Key -eq "numa.vcpu.maxPerVirtualNode" }
    [PSCustomObject]@{
        Name = $_.Name
        NumaMaxPerVirtualNode = if ($numa) { $numa.Value } else { "N/A" }
    }
} | Format-Table -AutoSize
