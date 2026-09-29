# run-collector.ps1 - vcportal-collector.exe 실행 래퍼 (Windows PowerShell 5.1)
#
#   .\run-collector.ps1                      수집 실행
#   .\run-collector.ps1 -Check               설정/경로/vCenter 접속 점검만
#   .\run-collector.ps1 -Conf D:\x\vcportal.conf
#     (-Conf 생략 시 ..\config\vcportal.conf 가 있으면 그것, 없으면 이 스크립트 폴더의 vcportal.conf)
#   .\run-collector.ps1 -RegisterTask        작업 스케줄러에 매일 12:00 실행 등록
#
# 종료 코드: exe 의 종료 코드(0 성공, 1 설정/경로 오류, 2 일부 실패, 3 전부 실패)
#            4 = 이미 다른 수집이 실행 중이라 건너뜀
param(
    [string]$Conf,
    [switch]$RegisterTask,
    [switch]$Check
)

$ErrorActionPreference = 'Stop'
[Console]::OutputEncoding = [System.Text.Encoding]::UTF8

$here = Split-Path -Parent $MyInvocation.MyCommand.Path
if (-not $Conf) {
    # 배포 폴더 구조(collector\ 옆의 config\)면 ..\config\vcportal.conf, 아니면 스크립트와 같은 폴더의 vcportal.conf
    $Conf = Join-Path $here '..\config\vcportal.conf'
    if (-not (Test-Path -LiteralPath $Conf)) { $Conf = Join-Path $here 'vcportal.conf' }
}
$Conf = [System.IO.Path]::GetFullPath($Conf)
$exe  = Join-Path $here 'vcportal-collector.exe'

if ($RegisterTask) {
    if (-not (Test-Path -LiteralPath $Conf)) {
        Write-Host "conf 파일을 찾을 수 없어 등록하지 않습니다: $Conf  (-Conf 로 경로를 지정하세요)"
        exit 1
    }
    $taskName = 'vcportal-collector'
    $arg = '-NoProfile -ExecutionPolicy Bypass -File "{0}" -Conf "{1}"' -f $MyInvocation.MyCommand.Path, $Conf
    $action  = New-ScheduledTaskAction -Execute 'powershell.exe' -Argument $arg
    $trigger = New-ScheduledTaskTrigger -Daily -At '12:00'
    Register-ScheduledTask -TaskName $taskName -Action $action -Trigger $trigger -Force | Out-Null
    Write-Host "작업 스케줄러에 '$taskName' 을(를) 등록했습니다 (매일 12:00, 현재 사용자 $env:USERDOMAIN\$env:USERNAME)."
    Write-Host "참고: 이 작업 계정에 conf 의 output_dir 쓰기 권한이 있어야 합니다."
    Write-Host "      먼저 '.\run-collector.ps1 -Check' 로 점검해 보세요."
    exit 0
}

if (-not (Test-Path -LiteralPath $exe)) {
    Write-Host "vcportal-collector.exe 를 찾을 수 없습니다: $exe"
    exit 1
}

$exeArgs = @('--conf', $Conf)
if ($Check) { $exeArgs += '--check' }

# 동시에 두 번 실행되지 않도록 이름 있는 뮤텍스를 잡는다.
$mutex = New-Object System.Threading.Mutex($false, 'Global\vcportal-collector')
$got = $false
try {
    try { $got = $mutex.WaitOne(0) } catch [System.Threading.AbandonedMutexException] { $got = $true }
    if (-not $got) {
        Write-Host "이미 vcportal-collector 가 실행 중입니다. 이번 실행은 건너뜁니다."
        exit 4
    }
    & $exe @exeArgs
    $code = $LASTEXITCODE
} finally {
    if ($got) { $mutex.ReleaseMutex() }
    $mutex.Dispose()
}
exit $code
