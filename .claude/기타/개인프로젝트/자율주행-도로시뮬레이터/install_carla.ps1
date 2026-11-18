# install_carla.ps1 - CARLA 0.9.16 (Windows) 자동 설치. install_carla.bat 이 이 파일을 실행한다.
#
#   install_carla.bat                          # D:\CARLA_0.9.16 에 설치
#   install_carla.bat -InstallDir E:\CARLA     # 다른 위치
#   install_carla.bat -ZipPath C:\Down\CARLA_0.9.16.zip   # 이미 받은 zip 사용 (인터넷 없이)
#
# 하는 일: 디스크 여유 공간 확인 -> CARLA zip 다운로드(이어받기 가능) -> 압축 해제 -> zip 삭제
#          -> 이 프로젝트 .venv 생성 + numpy/pygame/moderngl/carla 설치(wheels\ 에서, 인터넷 불필요)
#          -> CARLA_ROOT 사용자 환경변수 등록 -> run_carla.bat 로 실행
param(
    [string]$InstallDir = "D:\CARLA_0.9.16",
    [string]$ZipPath = "",
    [string]$ZipUrl = "https://carla-releases.s3.us-east-005.backblazeb2.com/Windows/CARLA_0.9.16.zip",
    [switch]$KeepZip
)
$ErrorActionPreference = "Continue"   # 외부 프로그램은 $LASTEXITCODE 로 확인
$Project = Split-Path -Parent $MyInvocation.MyCommand.Path
$ReleasePage = "https://github.com/carla-simulator/carla/releases/tag/0.9.16"
$NeedGB = 40          # 압축 해제본 약 20 GB + zip 약 15 GB + 여유

function Fail($msg) { Write-Host "오류: $msg" -ForegroundColor Red; exit 1 }

# 1) 설치 위치 / 디스크 공간
$drive = (Split-Path -Qualifier $InstallDir).TrimEnd(':')
$disk = Get-PSDrive -Name $drive -ErrorAction SilentlyContinue
if (-not $disk) { Fail "$($drive): 드라이브가 없습니다. 예: install_carla.bat -InstallDir C:\CARLA_0.9.16" }
$freeGB = [math]::Floor($disk.Free / 1GB)
$need = if ($ZipPath) { 25 } else { $NeedGB }
Write-Host "설치 위치: $InstallDir   ($($drive): 여유 $freeGB GB, 필요 약 $need GB)"
if ($freeGB -lt $need) { Fail "$($drive): 드라이브 공간이 부족합니다. 다른 드라이브를 -InstallDir 로 지정하십시오." }

if (Test-Path (Join-Path $InstallDir "CarlaUE4.exe")) {
    Write-Host "CARLA 가 이미 설치되어 있습니다. 다운로드는 건너뜁니다."
} else {
    New-Item -ItemType Directory -Force -Path $InstallDir -ErrorAction Stop | Out-Null
    # 2) 다운로드 (BITS: 끊겨도 다시 실행하면 이어받기)
    if (-not $ZipPath) {
        $ZipPath = Join-Path $InstallDir "CARLA_0.9.16.zip"
        if (-not (Test-Path $ZipPath)) {
            Write-Host "다운로드 중 (약 15 GB, 회선에 따라 30분~수 시간): $ZipUrl"
            try {
                Start-BitsTransfer -Source $ZipUrl -Destination $ZipPath -DisplayName "CARLA 0.9.16" -ErrorAction Stop
            } catch {
                Write-Host "BITS 실패, curl 로 다시 시도합니다..."
                & curl.exe -L -C - -o $ZipPath $ZipUrl
                if ($LASTEXITCODE -ne 0) {
                    Fail "다운로드 실패. $ReleasePage 에서 Windows zip 을 직접 받은 뒤`n  install_carla.bat -ZipPath <받은 zip 경로>  로 다시 실행하십시오."
                }
            }
        }
    }
    if (-not (Test-Path $ZipPath)) { Fail "zip 파일이 없습니다: $ZipPath" }
    # 3) 압축 해제 (Windows 10+ 기본 tar 가 Expand-Archive 보다 훨씬 빠름)
    Write-Host "압축 해제 중 (10~20분)..."
    & tar.exe -xf $ZipPath -C $InstallDir
    if ($LASTEXITCODE -ne 0) { Fail "압축 해제 실패. zip 이 손상됐을 수 있습니다 (삭제 후 다시 실행)." }
    # zip 안에 폴더가 한 겹 더 있으면 끌어올림
    if (-not (Test-Path (Join-Path $InstallDir "CarlaUE4.exe"))) {
        $inner = Get-ChildItem $InstallDir -Directory | Where-Object { Test-Path (Join-Path $_.FullName "CarlaUE4.exe") } | Select-Object -First 1
        if (-not $inner) { Fail "CarlaUE4.exe 를 찾지 못했습니다: $InstallDir" }
        Get-ChildItem $inner.FullName -Force | Move-Item -Destination $InstallDir
        Remove-Item $inner.FullName -Recurse -Force
    }
    if (-not $KeepZip -and $ZipPath.StartsWith($InstallDir)) { Remove-Item $ZipPath -Force }
}

# 3.5) 언리얼 엔진 필수 런타임 (DirectX June 2010 + VC++). 없으면 CarlaUE4 실행 시 "DirectX runtime" 오류
$prereq = Join-Path $InstallDir "Engine\Extras\Redist\en-us\UE4PrereqSetup_x64.exe"
if (-not (Test-Path "$env:WINDIR\System32\XINPUT1_3.dll") -or -not (Test-Path "$env:WINDIR\System32\d3dx9_43.dll")) {
    if (Test-Path $prereq) {
        Write-Host "DirectX/VC++ 런타임 설치 중 (관리자 권한 확인 창이 뜹니다)..."
        $p = Start-Process -FilePath $prereq -ArgumentList "/quiet", "/norestart" -Verb RunAs -Wait -PassThru
        if ($p.ExitCode -ne 0) { Write-Host "런타임 설치 종료 코드 $($p.ExitCode). 오류가 계속되면 $prereq 를 직접 실행하십시오." -ForegroundColor Yellow }
    } else {   # CARLA 0.9.16 패키지에는 UE4Prereq 가 없음 -> winget 으로 설치
        Write-Host "DirectX End-User Runtime(June 2010) + VC++ 설치 중 (관리자 권한 확인 창이 뜹니다)..."
        & winget install --id Microsoft.DirectX -e --accept-package-agreements --accept-source-agreements
        & winget install --id Microsoft.VCRedist.2015+.x64 -e --accept-package-agreements --accept-source-agreements
        if (-not (Test-Path "$env:WINDIR\System32\d3dx9_43.dll")) {
            Write-Host "경고: DirectX 런타임 설치를 확인하지 못했습니다. microsoft.com 의 'DirectX End-User Runtime Web Installer' 를 직접 설치하십시오." -ForegroundColor Yellow
        }
    }
}

# 4) Python 패키지 (인터넷 불필요: wheels\)
Set-Location $Project
$py = "python"; $pyArgs = @()
if (Get-Command py -ErrorAction SilentlyContinue) {
    & py -3.11 -c "import sys" 2>$null
    if ($LASTEXITCODE -eq 0) { $py = "py"; $pyArgs = @("-3.11") }
}
if (-not (Get-Command $py -ErrorAction SilentlyContinue)) { Fail "Python 3.11 이 없습니다. https://www.python.org/downloads/release/python-3119/ 설치 후 다시 실행하십시오." }
& $py @pyArgs -c "import sys; assert sys.version_info[:2] == (3, 11)" 2>$null
if ($LASTEXITCODE -ne 0) { Fail "Python 3.11 이 필요합니다. https://www.python.org/downloads/release/python-3119/ 설치 후 다시 실행하십시오." }
if (-not (Test-Path ".venv\Scripts\python.exe")) { & $py @pyArgs -m venv .venv }
& .venv\Scripts\pip install --no-index --find-links wheels -r requirements.txt -r requirements-carla.txt
if ($LASTEXITCODE -ne 0) { Fail "패키지 설치 실패" }

# 5) 환경변수 (run_carla.bat 이 사용)
[Environment]::SetEnvironmentVariable("CARLA_ROOT", $InstallDir, "User")
Write-Host ""
Write-Host "설치 완료. CARLA_ROOT=$InstallDir" -ForegroundColor Green
Write-Host "실행: run_carla.bat                (서버를 켜고 시뮬레이터 실행)"
Write-Host "      run_carla.bat --level long --cars 0"
