# PowerShell Linux-like Environment Installer (Air-gapped / Offline Friendly)
[CmdletBinding()]
param (
    [string]$CustomBinPath = ""
)

Write-Host "==========================================================" -ForegroundColor Cyan
Write-Host "  PowerShell Linux 환경 설정 스크립트 (폐쇄망 지원)" -ForegroundColor Cyan
Write-Host "==========================================================" -ForegroundColor Cyan
Write-Host ""

# 1. Linux Coreutils (usr/bin) 경로 탐색
$targetBin = ""

if ($CustomBinPath -and (Test-Path $CustomBinPath)) {
    $targetBin = (Resolve-Path $CustomBinPath).Path
    Write-Host "[INFO] 사용자 지정 경로 사용: $targetBin" -ForegroundColor Green
} else {
    $scriptDir = $PSScriptRoot
    $candidatePaths = @(
        "$scriptDir\bin",
        "$scriptDir\PortableGit\usr\bin",
        "C:\Program Files\Git\usr\bin",
        "C:\Program Files (x86)\Git\usr\bin",
        "$env:LOCALAPPDATA\Programs\Git\usr\bin",
        "C:\Git\usr\bin"
    )

    foreach ($path in $candidatePaths) {
        if (Test-Path "$path\ls.exe") {
            $targetBin = (Resolve-Path $path).Path
            break
        }
    }
}

if (-not $targetBin) {
    Write-Host "[경고] Git usr\bin (Linux Coreutils) 경로를 자동으로 찾지 못했습니다." -ForegroundColor Yellow
    Write-Host "폐쇄망 환경인 경우 다음 중 하나를 수행해 주세요:" -ForegroundColor Yellow
    Write-Host " 1) Git for Windows 설치"
    Write-Host " 2) PortableGit 압축 해제본을 현재 폴더의 'PortableGit' 또는 'bin'에 배치"
    Write-Host " 3) .\install.ps1 -CustomBinPath 'D:\Tools\Git\usr\bin' 형식으로 경로 직접 전달"
    Write-Host ""
    $inputPath = Read-Host "Linux 도구(ls.exe 등)가 위치한 폴더 경로를 입력하세요 (엔터 시 기본값 취소)"
    if ($inputPath -and (Test-Path "$inputPath\ls.exe")) {
        $targetBin = (Resolve-Path $inputPath).Path
    } else {
        Write-Host "[오류] 올바른 경로가 확인되지 않아 설치를 중단합니다." -ForegroundColor Red
        return
    }
}

Write-Host "[성공] Linux 바이너리 경로 확인: $targetBin" -ForegroundColor Green

# 2. 프로필 스크립트 템플릿 생성 (ANSI/ASCII 안전 인코딩)
$profileContent = @"
# ==========================================================
# PowerShell Linux-like Environment Profile (Auto-generated)
# ==========================================================

# 1. Add Linux Utilities to PATH
`$linuxBin = "$targetBin"
if (Test-Path `$linuxBin) {
    if (`$env:PATH -notlike "*`$linuxBin*") {
        `$env:PATH = "`$linuxBin;`$env:PATH"
    }
}

# 2. Remove PowerShell Default Aliases (Enable real Linux binaries)
`$linuxCommands = @('ls', 'cat', 'rm', 'cp', 'mv', 'clear')
foreach (`$cmd in `$linuxCommands) {
    if (Get-Alias -Name `$cmd -ErrorAction SilentlyContinue) {
        Remove-Item "Alias:\`$cmd" -Force -ErrorAction SilentlyContinue
    }
}

# 3. Helper Functions: ll, la, which
function ll { & ls.exe -la --color=auto `$args }
function la { & ls.exe -a --color=auto `$args }
function which (`$name) { 
    `$cmd = Get-Command `$name -ErrorAction SilentlyContinue
    if (`$cmd) {
        `$cmd.Source
    } else {
        Write-Error "which: no `$name in PATH"
    }
}

# 4. Bash/Emacs Keybindings (Ctrl+A, Ctrl+E, Ctrl+R, Ctrl+U, Ctrl+W)
if (Get-Module -ListAvailable -Name PSReadLine) {
    Import-Module PSReadLine -ErrorAction SilentlyContinue
    Set-PSReadLineOption -EditMode Emacs -ErrorAction SilentlyContinue
}

# 5. Console UTF-8 Output Encoding
[Console]::OutputEncoding = [System.Text.Encoding]::UTF8
`$OutputEncoding = [System.Text.Encoding]::UTF8
"@

# 3. 대상 프로필 파일 경로 수집 (WindowsPowerShell 및 PowerShell 7)
$myDocs = [Environment]::GetFolderPath("MyDocuments")
$profileTargets = @(
    "$myDocs\WindowsPowerShell\Microsoft.PowerShell_profile.ps1",
    "$myDocs\PowerShell\Microsoft.PowerShell_profile.ps1",
    "$env:USERPROFILE\Documents\WindowsPowerShell\Microsoft.PowerShell_profile.ps1",
    "$env:USERPROFILE\Documents\PowerShell\Microsoft.PowerShell_profile.ps1"
) | Select-Object -Unique

Write-Host ""
Write-Host "[진행] PowerShell 프로필 등록 시작..." -ForegroundColor Cyan

foreach ($target in $profileTargets) {
    $parentDir = Split-Path $target -Parent
    if (-not (Test-Path $parentDir)) {
        New-Item -Path $parentDir -ItemType Directory -Force | Out-Null
    }

    if (Test-Path $target) {
        $backupPath = "$target.bak_" + (Get-Date -Format "yyyyMMddHHmmss")
        Copy-Item -Path $target -Destination $backupPath -Force
        Write-Host "  -> 기존 프로필 백업 완료: $backupPath" -ForegroundColor DarkGray
    }

    Set-Content -Path $target -Value $profileContent -Encoding ASCII -Force
    Write-Host "  -> 프로필 적용 완료: $target" -ForegroundColor Green
}

# 4. ExecutionPolicy 체크 및 가이드
$policy = Get-ExecutionPolicy -Scope CurrentUser
if ($policy -eq 'Restricted' -or $policy -eq 'Undefined') {
    try {
        Set-ExecutionPolicy -ExecutionPolicy RemoteSigned -Scope CurrentUser -Force -ErrorAction SilentlyContinue
        Write-Host "[정보] CurrentUser ExecutionPolicy를 'RemoteSigned'로 설정했습니다." -ForegroundColor Green
    } catch {
        Write-Host "[참고] ExecutionPolicy가 Restricted인 경우 관리자 권한으로 'Set-ExecutionPolicy RemoteSigned'를 실행하세요." -ForegroundColor Yellow
    }
}

Write-Host ""
Write-Host "==========================================================" -ForegroundColor Green
Write-Host "  설치가 성공적으로 완료되었습니다!" -ForegroundColor Green
Write-Host "==========================================================" -ForegroundColor Green
Write-Host "새로운 PowerShell 창을 열면 다음 리눅스 명령어를 바로 쓸 수 있습니다:"
Write-Host "  - ll, la, ls -la"
Write-Host "  - grep, sed, awk, find, head, tail, which"
Write-Host "  - rm -rf [폴더]"
Write-Host "  - 단축키: Ctrl+A (맨 앞), Ctrl+E (맨 뒤), Ctrl+R (검색), Ctrl+U (전체삭제)"
Write-Host ""
