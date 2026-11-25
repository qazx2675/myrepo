# make_shortcut.ps1 - 바탕화면에 "Autodrive CARLA" 바로가기를 만든다 (run_carla.bat 실행).
# 코스/난이도/차량 등은 모두 실행 후 게임 안 시작 메뉴에서 고른다.
$Project = Split-Path -Parent $MyInvocation.MyCommand.Path
$root = [Environment]::GetEnvironmentVariable("CARLA_ROOT", "User")
if (-not $root) { $root = "D:\CARLA_0.9.16" }
$desktop = [Environment]::GetFolderPath("Desktop")
$lnk = Join-Path $desktop "Autodrive CARLA.lnk"
$sh = New-Object -ComObject WScript.Shell
$s = $sh.CreateShortcut($lnk)
$s.TargetPath = "$env:WINDIR\System32\cmd.exe"
$s.Arguments = '/c "' + (Join-Path $Project "run_carla.bat") + '"'
$s.WorkingDirectory = $Project
$s.WindowStyle = 7                      # 콘솔 창은 최소화
$icon = Join-Path $root "CarlaUE4.exe"
if (Test-Path $icon) { $s.IconLocation = "$icon,0" }
$s.Description = "자율주행 도로 시뮬레이터 (CARLA)"
$s.Save()
Write-Host "바탕화면 바로가기 생성: $lnk"
