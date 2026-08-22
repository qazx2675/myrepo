#requires -Version 7.0
# vCenter 인벤토리 객체(VM/호스트/클러스터/데이터스토어) 이름을 Tab으로 실시간 완성해 주는 ArgumentCompleter 모듈.
# 매 키 입력마다 vCenter를 조회하면 느려지므로, 짧은 TTL 캐시를 둔다.

$script:CompletionCache = @{}
$script:CacheTtlSeconds = 15

function Get-VCenterNameCandidates {
    param(
        [Parameter(Mandatory)] [string] $ViewType
    )

    if (-not $global:DefaultVIServers -or $global:DefaultVIServers.Count -eq 0) {
        return @()
    }

    $cacheKey = $ViewType
    $now = [DateTimeOffset]::UtcNow
    $cached = $script:CompletionCache[$cacheKey]
    if ($cached -and ($now - $cached.FetchedAt).TotalSeconds -lt $script:CacheTtlSeconds) {
        return $cached.Names
    }

    try {
        $names = (Get-View -ViewType $ViewType -Property Name -ErrorAction Stop).Name
    } catch {
        return @()
    }

    $script:CompletionCache[$cacheKey] = @{ Names = $names; FetchedAt = $now }
    return $names
}

function Register-VCenterNameCompleter {
    param(
        [Parameter(Mandatory)] [string[]] $CommandName,
        [Parameter(Mandatory)] [string] $ParameterName,
        [Parameter(Mandatory)] [string] $ViewType
    )

    Register-ArgumentCompleter -CommandName $CommandName -ParameterName $ParameterName -ScriptBlock {
        param($commandName, $parameterName, $wordToComplete, $commandAst, $fakeBoundParameters)

        Get-VCenterNameCandidates -ViewType $using:ViewType |
            Where-Object { $_ -like "$wordToComplete*" } |
            ForEach-Object {
                [System.Management.Automation.CompletionResult]::new(
                    "'$_'", $_, 'ParameterValue', $_
                )
            }
    }
}

# 규칙 출처: ../command-advisory-rules.md 1장과 동일한 cmdlet 대상
Register-VCenterNameCompleter -CommandName 'Get-VM'              -ParameterName 'Name' -ViewType 'VirtualMachine'
Register-VCenterNameCompleter -CommandName 'Get-VMHost'          -ParameterName 'Name' -ViewType 'HostSystem'
Register-VCenterNameCompleter -CommandName 'Get-Datastore'       -ParameterName 'Name' -ViewType 'Datastore'
Register-VCenterNameCompleter -CommandName 'Get-Cluster'         -ParameterName 'Name' -ViewType 'ClusterComputeResource'
Register-VCenterNameCompleter -CommandName 'Get-ResourcePool'    -ParameterName 'Name' -ViewType 'ResourcePool'
Register-VCenterNameCompleter -CommandName 'Get-Datacenter'      -ParameterName 'Name' -ViewType 'Datacenter'
Register-VCenterNameCompleter -CommandName 'Get-VirtualPortGroup' -ParameterName 'Name' -ViewType 'Network'

Export-ModuleMember -Function Get-VCenterNameCandidates, Register-VCenterNameCompleter
