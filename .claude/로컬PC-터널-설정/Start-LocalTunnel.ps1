# 로컬 PC 사설IP 서비스 외부 노출 스크립트 (Cloudflare Quick Tunnel / ngrok)
<#
.SYNOPSIS
    같은 LAN의 사설 IP(예: 192.168.0.58/50/60) HTTP(S) 서비스를 공인 HTTPS 주소로 노출합니다.
    이 PC가 대상 IP들에 접근할 수 있으면, 한 PC에서 여러 대상을 동시에 터널링할 수 있습니다.

.EXAMPLE
    # Cloudflare (계정 불필요, 대상마다 임시 https://xxxx.trycloudflare.com 발급)
    .\Start-LocalTunnel.ps1 -Target 192.168.0.58:80, 192.168.0.50:8080, https://192.168.0.60:443

.EXAMPLE
    # ngrok (무료 계정 authtoken 필요)
    .\Start-LocalTunnel.ps1 -Provider ngrok -NgrokAuthToken <토큰> -Target 192.168.0.58:80

.EXAMPLE
    .\Start-LocalTunnel.ps1 -Status
    .\Start-LocalTunnel.ps1 -Stop
#>
[CmdletBinding(DefaultParameterSetName = 'Start')]
param(
    [Parameter(ParameterSetName = 'Start', Mandatory = $true)]
    [string[]]$Target,

    [Parameter(ParameterSetName = 'Start')]
    [ValidateSet('cloudflare', 'ngrok')]
    [string]$Provider = 'cloudflare',

    [Parameter(ParameterSetName = 'Start')]
    [string]$NgrokAuthToken = $env:NGROK_AUTHTOKEN,

    [Parameter(ParameterSetName = 'Stop', Mandatory = $true)]
    [switch]$Stop,

    [Parameter(ParameterSetName = 'Status', Mandatory = $true)]
    [switch]$Status
)

$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'
[Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12

$WorkDir   = Join-Path $PSScriptRoot 'tunnel-work'
$BinDir    = Join-Path $WorkDir 'bin'
$LogDir    = Join-Path $WorkDir 'logs'
$StateFile = Join-Path $WorkDir 'state.json'
$ResultTxt = Join-Path $WorkDir 'tunnels.txt'

$CloudflaredUrl = 'https://github.com/cloudflare/cloudflared/releases/latest/download/cloudflared-windows-amd64.exe'
$NgrokZipUrl    = 'https://bin.equinox.io/c/bNyj1mQVY4c/ngrok-v3-stable-windows-amd64.zip'

function Read-SharedText([string]$Path) {
    # 실행 중인 프로세스가 쓰고 있는 로그를 공유 모드로 읽음
    if (-not (Test-Path $Path)) { return '' }
    $fs = [IO.File]::Open($Path, 'Open', 'Read', 'ReadWrite')
    try { (New-Object IO.StreamReader($fs)).ReadToEnd() } finally { $fs.Dispose() }
}

function Get-State {
    if (-not (Test-Path $StateFile)) { return $null }
    Get-Content $StateFile -Raw -Encoding UTF8 | ConvertFrom-Json
}

function Show-State($State) {
    $State.Items | ForEach-Object {
        $alive = [bool](Get-Process -Id $_.Pid -ErrorAction SilentlyContinue)
        [pscustomobject]@{ Target = $_.Target; PublicUrl = $_.Url; Pid = $_.Pid; Running = $alive }
    } | Format-Table -AutoSize
}

function ConvertTo-TargetUri([string]$Raw) {
    if ($Raw -notmatch '^[a-z]+://') { $Raw = "http://$Raw" }
    $u = [uri]$Raw
    if ($u.Scheme -notin 'http', 'https') { throw "HTTP/HTTPS 대상만 지원합니다: $Raw" }
    $u
}

function Get-Cloudflared {
    $local = Join-Path $BinDir 'cloudflared.exe'
    if (Test-Path $local) { return $local }
    $cmd = Get-Command cloudflared -ErrorAction SilentlyContinue
    if ($cmd) { return $cmd.Source }
    Write-Host "[다운로드] cloudflared -> $local"
    Invoke-WebRequest -Uri $CloudflaredUrl -OutFile $local -UseBasicParsing
    $local
}

function Get-Ngrok {
    $local = Join-Path $BinDir 'ngrok.exe'
    if (Test-Path $local) { return $local }
    $cmd = Get-Command ngrok -ErrorAction SilentlyContinue
    if ($cmd) { return $cmd.Source }
    $zip = Join-Path $BinDir 'ngrok.zip'
    Write-Host "[다운로드] ngrok -> $local"
    Invoke-WebRequest -Uri $NgrokZipUrl -OutFile $zip -UseBasicParsing
    Expand-Archive -Path $zip -DestinationPath $BinDir -Force
    Remove-Item $zip
    $local
}

function Start-Cloudflare($Uris) {
    $exe = Get-Cloudflared
    $items = @()
    $i = 0
    foreach ($u in $Uris) {
        $i++
        $log = Join-Path $LogDir "cloudflared_$i.log"
        $out = Join-Path $LogDir "cloudflared_$i.out"
        $cfArgs = @('tunnel', '--no-autoupdate', '--url', $u.AbsoluteUri.TrimEnd('/'))
        # 내부 HTTPS 서비스는 대부분 자체서명 인증서라 검증 생략
        if ($u.Scheme -eq 'https') { $cfArgs += '--no-tls-verify' }
        $p = Start-Process -FilePath $exe -ArgumentList $cfArgs -WindowStyle Hidden -PassThru `
                -RedirectStandardError $log -RedirectStandardOutput $out

        $url = $null
        $deadline = (Get-Date).AddSeconds(40)
        while (-not $url -and (Get-Date) -lt $deadline -and -not $p.HasExited) {
            Start-Sleep -Seconds 1
            $m = [regex]::Match((Read-SharedText $log), 'https://[a-z0-9-]+\.trycloudflare\.com')
            if ($m.Success) { $url = $m.Value }
        }
        if (-not $url) { Write-Warning "[$($u.Host):$($u.Port)] 주소 발급 실패 - 로그 확인: $log" }
        $items += [pscustomobject]@{ Target = "$($u.Host):$($u.Port)"; Url = $url; Pid = $p.Id; Log = $log }
    }
    $items
}

function Start-Ngrok($Uris) {
    if (-not $NgrokAuthToken) { throw 'ngrok는 authtoken이 필요합니다. -NgrokAuthToken 또는 $env:NGROK_AUTHTOKEN 을 지정하세요.' }
    $exe = Get-Ngrok
    $cfg = Join-Path $WorkDir 'ngrok.yml'
    $log = Join-Path $LogDir 'ngrok.log'

    $lines = @('version: "2"', "authtoken: $NgrokAuthToken", 'tunnels:')
    $i = 0
    foreach ($u in $Uris) {
        $i++
        $addr = if ($u.Scheme -eq 'https') { "https://$($u.Host):$($u.Port)" } else { "$($u.Host):$($u.Port)" }
        $lines += "  t${i}:", '    proto: http', "    addr: $addr"
    }
    Set-Content -Path $cfg -Value $lines -Encoding ASCII

    $p = Start-Process -FilePath $exe -ArgumentList @('start', '--all', '--config', $cfg, '--log', $log) `
            -WindowStyle Hidden -PassThru

    $tunnels = $null
    $deadline = (Get-Date).AddSeconds(40)
    while ((Get-Date) -lt $deadline -and -not $p.HasExited) {
        Start-Sleep -Seconds 1
        try { $tunnels = (Invoke-RestMethod -Uri 'http://127.0.0.1:4040/api/tunnels').tunnels } catch { continue }
        if (@($tunnels).Count -ge $Uris.Count) { break }
    }

    $items = @()
    $i = 0
    foreach ($u in $Uris) {
        $i++
        $t = $tunnels | Where-Object { $_.name -eq "t$i" } | Select-Object -First 1
        if (-not $t) { Write-Warning "[$($u.Host):$($u.Port)] 주소 발급 실패 - 로그 확인: $log (무료 플랜 엔드포인트 수 제한일 수 있음)" }
        $items += [pscustomobject]@{ Target = "$($u.Host):$($u.Port)"; Url = $t.public_url; Pid = $p.Id; Log = $log }
    }
    $items
}

# ---------- Status / Stop ----------
if ($Status) {
    $s = Get-State
    if (-not $s) { Write-Host '실행 중인 터널 기록이 없습니다.'; return }
    Write-Host "Provider: $($s.Provider)  (시작: $($s.StartedAt))"
    Show-State $s
    return
}

if ($Stop) {
    $s = Get-State
    if (-not $s) { Write-Host '중지할 터널 기록이 없습니다.'; return }
    $s.Items.Pid | Sort-Object -Unique | ForEach-Object { Stop-Process -Id $_ -Force -ErrorAction SilentlyContinue }
    Remove-Item $StateFile, $ResultTxt -ErrorAction SilentlyContinue
    Write-Host '터널을 모두 중지했습니다.'
    return
}

# ---------- Start ----------
if (Get-State) { throw '이미 실행 중인 터널 기록이 있습니다. 먼저 -Stop 으로 중지하세요.' }
New-Item -ItemType Directory -Force -Path $BinDir, $LogDir | Out-Null

$uris = @()
foreach ($t in $Target) {
    $u = ConvertTo-TargetUri $t
    Write-Host -NoNewline "[점검] $($u.Host):$($u.Port) ... "
    $ok = Test-NetConnection -ComputerName $u.Host -Port $u.Port -InformationLevel Quiet -WarningAction SilentlyContinue
    if ($ok) { Write-Host 'OK'; $uris += $u } else { Write-Host '연결 불가 - 제외' }
}
if ($uris.Count -eq 0) { throw '이 PC에서 접근 가능한 대상이 없습니다. IP/포트와 대상 PC 방화벽을 확인하세요.' }

$items = if ($Provider -eq 'cloudflare') { Start-Cloudflare $uris } else { Start-Ngrok $uris }

$state = [pscustomobject]@{ Provider = $Provider; StartedAt = (Get-Date).ToString('s'); Items = $items }
$state | ConvertTo-Json -Depth 4 | Set-Content -Path $StateFile -Encoding UTF8
$items | Where-Object Url | ForEach-Object { "$($_.Target) -> $($_.Url)" } | Set-Content -Path $ResultTxt -Encoding UTF8

Show-State $state
Write-Host "결과 파일: $ResultTxt  (이 내용을 클라우드 세션에 붙여넣으세요)"
Write-Host '주의: 발급된 주소는 인증 없이 누구나 접근 가능합니다. 사용 후 반드시 -Stop 으로 중지하세요.'
