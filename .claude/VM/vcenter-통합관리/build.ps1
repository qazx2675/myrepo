# build.ps1 - Windows(PowerShell 5.1) 용 빌드 스크립트. build.sh 와 같은 결과를 만든다.
#   ./cmd/* 를 windows/amd64 로 빌드(bin\windows\)하고, 공유폴더에 그대로 복사할 dist\vc-portal\ 을 조립한다.
#   의존성은 vendor\ 만 사용하므로 폐쇄망(인터넷 없음)에서도 동작한다.
#
# 실행:  powershell -ExecutionPolicy Bypass -File .\build.ps1
$ErrorActionPreference = 'Stop'
Set-Location -LiteralPath $PSScriptRoot

# ---- 폐쇄망 설정: 네트워크 접근 차단, vendor 만 사용 ----
$env:GOFLAGS     = '-mod=vendor'
$env:GOPROXY     = 'off'
$env:GOSUMDB     = 'off'
$env:GOTOOLCHAIN = 'local'   # go.mod 보다 낮은 Go 일 때 툴체인 자동 다운로드를 시도하지 않게 한다
$env:CGO_ENABLED = '0'

# ---- Go 확인 ----
if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
    Write-Host "[오류] go 명령을 찾을 수 없습니다. Go 1.26.5 를 설치한 뒤 다시 실행하세요." -ForegroundColor Red
    exit 1
}
$goVer = (& go env GOVERSION).Trim()          # 예: go1.26.5
$need  = ((Select-String -Path go.mod -Pattern '^go\s+(\S+)').Matches[0].Groups[1].Value)   # 예: 1.26.0
$have  = $goVer -replace '^go', '' -replace '[^0-9.].*$', ''
function ConvertTo-Ver([string]$s) { [version]((($s.Split('.') + '0', '0')[0..2]) -join '.') }
if ((ConvertTo-Ver $have) -lt (ConvertTo-Ver $need)) {
    Write-Host "[오류] 설치된 Go($goVer) 가 go.mod 요구 버전($need) 보다 낮습니다. Go 1.26.5 를 설치하세요." -ForegroundColor Red
    exit 1
}
Write-Host "Go: $goVer (go.mod 요구: $need 이상)"

# ---- 빌드 ----
New-Item -ItemType Directory -Force -Path bin\windows | Out-Null
$env:GOOS = 'windows'; $env:GOARCH = 'amd64'
$built = 0
foreach ($d in Get-ChildItem -Directory -Path cmd) {
    $name = $d.Name
    # 런처(vcportal)는 링크 클릭 시 콘솔 창이 뜨지 않도록 GUI 서브시스템으로 빌드한다.
    $goArgs = @('build', '-trimpath')
    if ($name -eq 'vcportal') { $goArgs += @('-ldflags', '-H windowsgui') }
    & go @goArgs -o "bin\windows\$name.exe" "./cmd/$name"
    if ($LASTEXITCODE -ne 0) { Write-Host "[오류] $name 빌드 실패" -ForegroundColor Red; exit 1 }
    Write-Host "built bin\windows\$name.exe"
    $built++
}
Write-Host "완료: ${built}개 명령 빌드"

# ---- 배포 패키지: dist\vc-portal\ ----
$out = 'dist\vc-portal'
if (Test-Path -LiteralPath $out) { Remove-Item -LiteralPath $out -Recurse -Force }
foreach ($sub in 'data', 'config', 'launcher', 'collector') {
    New-Item -ItemType Directory -Force -Path (Join-Path $out $sub) | Out-Null
}
Copy-Item web\index.html $out
Copy-Item web\assets (Join-Path $out 'assets') -Recurse
Copy-Item bin\windows\vcportal.exe (Join-Path $out 'launcher')
Copy-Item bin\windows\vcportal-collector.exe (Join-Path $out 'collector')

# .ps1 / conf 예제는 Windows PowerShell 5.1·메모장이 한글을 읽도록 UTF-8 BOM + CRLF 로 맞춘다.
function Copy-CrlfBom([string]$src, [string]$dst) {
    $text = [System.IO.File]::ReadAllText((Resolve-Path $src), [System.Text.Encoding]::UTF8)
    $text = $text.TrimStart([char]0xFEFF) -replace "`r`n", "`n" -replace "`n", "`r`n"
    if (-not $text.EndsWith("`r`n")) { $text += "`r`n" }
    [System.IO.File]::WriteAllText((Join-Path (Get-Location) $dst), $text, (New-Object System.Text.UTF8Encoding $true))
}
Copy-CrlfBom scripts\install-launcher.ps1 (Join-Path $out 'launcher\install-launcher.ps1')
Copy-CrlfBom scripts\run-collector.ps1    (Join-Path $out 'collector\run-collector.ps1')
Copy-CrlfBom config\vcportal.conf.example (Join-Path $out 'config\vcportal.conf.example')

Write-Host "배포 패키지: $out  (이 폴더를 공유폴더의 vc-portal 로 그대로 복사)"
