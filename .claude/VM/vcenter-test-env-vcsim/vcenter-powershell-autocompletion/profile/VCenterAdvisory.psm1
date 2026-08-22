#requires -Version 7.0
# vCenter 순차 조회 cmdlet에 대해 Get-View 기반 대안을 안내하는 프록시 함수 모듈.
# 규칙 출처: ../command-advisory-rules.md

# 세션 전체 안내 끄기: $Global:VCAdvisory_Disabled = $true
if (-not (Get-Variable -Name VCAdvisory_Disabled -Scope Global -ErrorAction SilentlyContinue)) {
    $Global:VCAdvisory_Disabled = $false
}

# 규칙 테이블: command-advisory-rules.md 1장과 동일
$script:AdvisoryRules = @(
    @{ Cmdlet = 'Get-VM';              ViewType = 'VirtualMachine' }
    @{ Cmdlet = 'Get-VMHost';          ViewType = 'HostSystem' }
    @{ Cmdlet = 'Get-Datastore';       ViewType = 'Datastore' }
    @{ Cmdlet = 'Get-Cluster';         ViewType = 'ClusterComputeResource' }
    @{ Cmdlet = 'Get-ResourcePool';    ViewType = 'ResourcePool' }
    @{ Cmdlet = 'Get-Datacenter';      ViewType = 'Datacenter' }
    @{ Cmdlet = 'Get-VirtualPortGroup'; ViewType = 'Network' }
)

function Write-VCenterAdvisory {
    param(
        [Parameter(Mandatory)] [string] $Cmdlet,
        [Parameter(Mandatory)] [string] $ViewType
    )
    Write-Host "[성능 안내] '$Cmdlet'은 대규모 인프라에서 순차 조회를 수행하여 성능이 저하될 수 있습니다." -ForegroundColor Yellow
    Write-Host "  권장: Get-View -ViewType $ViewType [-Filter @{'name'='<value>'}]" -ForegroundColor Yellow
    Write-Host "  이 메시지를 끄려면: -SkipAdvisory 스위치를 추가하거나 `$Global:VCAdvisory_Disabled = `$true 를 설정하세요." -ForegroundColor Yellow
}

function New-VCenterAdvisoryProxy {
    <#
    .SYNOPSIS
        지정한 PowerCLI cmdlet을 감싸는 프록시 함수를 만들어, 실행 시 Get-View 대안을 안내한다.
        -SkipAdvisory 스위치를 추가한 프록시 함수를 동적으로 생성해 원본 cmdlet의 파라미터/자동완성을 그대로 보존한다.
    #>
    param(
        [Parameter(Mandatory)] [string] $CmdletName,
        [Parameter(Mandatory)] [string] $ViewType
    )

    $original = Get-Command -Name $CmdletName -Module VMware.VimAutomation.Core -ErrorAction SilentlyContinue
    if (-not $original) {
        Write-Verbose "PowerCLI cmdlet '$CmdletName'을 찾을 수 없어 프록시 생성을 건너뜁니다 (모듈 미로드?)."
        return
    }

    # 원본 cmdlet의 전체 파라미터 시그니처를 그대로 재현 — 자동완성/파라미터 힌트가 원본과 동일하게 동작
    $proxyBody = [System.Management.Automation.ProxyCommand]::Create($original.Metadata)
    $proxyBody = $proxyBody -replace `
        '(\bparam\s*\()', `
        "param(`n        [switch] `$SkipAdvisory,`n"
    $proxyBody = $proxyBody -replace `
        '(process\s*\{)', `
        "process {`n        if (-not `$SkipAdvisory -and -not `$Global:VCAdvisory_Disabled) { Write-VCenterAdvisory -Cmdlet '$CmdletName' -ViewType '$ViewType' }`n"

    Set-Item -Path "function:script:$CmdletName" -Value ([scriptblock]::Create($proxyBody))
    Export-ModuleMember -Function $CmdletName
}

foreach ($rule in $script:AdvisoryRules) {
    New-VCenterAdvisoryProxy -CmdletName $rule.Cmdlet -ViewType $rule.ViewType
}

Export-ModuleMember -Function Write-VCenterAdvisory
