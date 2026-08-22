#requires -Version 7.0
# vCenter 순차 조회 cmdlet에 대해 Get-View 기반 대안을 안내하는 프록시 함수 모듈.
# 규칙 출처: ../command-advisory-rules.md

# 원본 cmdlet을 찾으려면 PowerCLI가 먼저 로드되어 있어야 함 (프로필에서 이 모듈을 바로 Import하므로 여기서도 보장)
if (-not (Get-Module -Name VMware.PowerCLI)) {
    try {
        Import-Module VMware.PowerCLI -ErrorAction Stop
    } catch {
        Write-Warning "VMware.PowerCLI 모듈을 불러오지 못해 순차 조회 권고 기능을 건너뜁니다: $($_.Exception.Message)"
        return
    }
}

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

    try {
        $original = Get-Command -Name $CmdletName -CommandType Cmdlet, Function -ErrorAction Stop |
            Select-Object -First 1
    } catch {
        Write-Verbose "PowerCLI cmdlet '$CmdletName'을 찾을 수 없어 프록시 생성을 건너뜁니다: $($_.Exception.Message)"
        return
    }

    try {
        # 원본 cmdlet의 전체 파라미터 시그니처를 그대로 재현 — 자동완성/파라미터 힌트가 원본과 동일하게 동작
        $metadata = [System.Management.Automation.CommandMetadata]::new($original)
        $proxyBody = [System.Management.Automation.ProxyCommand]::Create($metadata)
    } catch {
        Write-Warning "'$CmdletName' 프록시 생성 실패, 원본 cmdlet을 그대로 둡니다: $($_.Exception.Message)"
        return
    }

    # param 블록에 -SkipAdvisory 스위치 추가
    $proxyBody = $proxyBody -replace `
        '(\bparam\s*\()', `
        "param(`n        [switch] `$SkipAdvisory,`n"

    # 안내 메시지 출력 + SkipAdvisory를 $PSBoundParameters에서 제거하는 로직을
    # begin 블록 맨 앞(원본 cmdlet으로 파라미터를 그대로 전달하기 직전)에 삽입.
    # process 블록에 넣으면 안 됨 — @PSBoundParameters 전달은 begin 블록에서 미리 구성되므로
    # 거기서 SkipAdvisory를 지우지 않으면 원본 cmdlet이 모르는 파라미터라며 오류가 난다.
    $proxyBody = $proxyBody -replace `
        'begin\s*\{\s*try\s*\{', `
        "begin`n{`n    try {`n        if (-not `$SkipAdvisory -and -not `$Global:VCAdvisory_Disabled) { Write-VCenterAdvisory -Cmdlet '$CmdletName' -ViewType '$ViewType' }`n        `$PSBoundParameters.Remove('SkipAdvisory') | Out-Null`n"

    Set-Item -Path "function:script:$CmdletName" -Value ([scriptblock]::Create($proxyBody))
    Export-ModuleMember -Function $CmdletName
}

foreach ($rule in $script:AdvisoryRules) {
    New-VCenterAdvisoryProxy -CmdletName $rule.Cmdlet -ViewType $rule.ViewType
}

Export-ModuleMember -Function Write-VCenterAdvisory
