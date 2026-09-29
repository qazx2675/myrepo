<#
.SYNOPSIS
  vcportal:// 프로토콜을 현재 사용자(HKCU)에 등록/해제한다. 관리자 권한 불필요.

.DESCRIPTION
  포털의 [vCenter에서 열기] 링크(vcportal://open?url=...)를 누르면 공유폴더의
  vcportal.exe(로그인 런처)가 실행되도록 HKCU:\Software\Classes\vcportal 을 만든다.
  exe 는 공유폴더에서 직접 실행하므로 UNC 전체 경로로 등록된다.

  참고: 포털(file:// 로 연 index.html)에서 링크를 누르면 Edge 가
  "이 사이트에서 vcportal 을(를) 열려고 합니다" 확인 창을 띄운다. [열기] 를 누르면 된다.
  file:// 페이지에서는 "항상 허용" 체크박스가 제공되지 않을 수 있으며, 확인 창을 없애는
  Edge 정책(AutoLaunchProtocolsFromOrigins)은 HKCU\Software\Policies 에 쓰려면 관리자 권한이
  필요하므로 이 스크립트에서는 설정하지 않는다(필요하면 GPO/Intune 으로 배포).

.PARAMETER ExePath
  런처 exe 경로. 기본값: 이 스크립트와 같은 폴더의 vcportal.exe

.PARAMETER Uninstall
  등록을 해제한다.

.EXAMPLE
  powershell -ExecutionPolicy Bypass -File \\fileserver\share\vc-portal\launcher\install-launcher.ps1
.EXAMPLE
  powershell -ExecutionPolicy Bypass -File \\fileserver\share\vc-portal\launcher\install-launcher.ps1 -Uninstall
#>
param(
    [string]$ExePath,
    [switch]$Uninstall
)

$ErrorActionPreference = 'Stop'
$key = 'HKCU:\Software\Classes\vcportal'

if ($Uninstall) {
    if (Test-Path $key) {
        Remove-Item -Path $key -Recurse -Force
        Write-Host "vcportal:// 프로토콜 등록을 해제했습니다."
    } else {
        Write-Host "vcportal:// 프로토콜이 등록되어 있지 않습니다."
    }
    exit 0
}

if (-not $ExePath) {
    $ExePath = Join-Path $PSScriptRoot 'vcportal.exe'
}
if (-not (Test-Path -LiteralPath $ExePath -PathType Leaf)) {
    Write-Host "런처 파일을 찾을 수 없습니다: $ExePath" -ForegroundColor Red
    exit 1
}
# Microsoft.PowerShell.Core\FileSystem:: 접두사 없이 전체(UNC) 경로로 변환
$full = (Get-Item -LiteralPath $ExePath).FullName
if ($full -match '^[A-Za-z]:\\') {
    $drive = Get-PSDrive -Name $full.Substring(0, 1) -ErrorAction SilentlyContinue
    if ($drive -and $drive.DisplayRoot) {
        Write-Host "주의: $($full.Substring(0, 2)) 는 네트워크 드라이브($($drive.DisplayRoot))입니다. UNC 경로로 실행하는 것을 권장합니다." -ForegroundColor Yellow
    }
}

New-Item -Path "$key\shell\open\command" -Force | Out-Null
Set-ItemProperty -Path $key -Name '(default)' -Value 'URL:vCenter Portal Launcher'
Set-ItemProperty -Path $key -Name 'URL Protocol' -Value ''
Set-ItemProperty -Path "$key\shell\open\command" -Name '(default)' -Value ('"{0}" "%1"' -f $full)

Write-Host "vcportal:// 프로토콜을 등록했습니다."
Write-Host "  실행 파일: $full"
Write-Host "포털에서 [vCenter에서 열기] 를 누르고 Edge 확인 창이 뜨면 [열기] 를 선택하세요."
exit 0
