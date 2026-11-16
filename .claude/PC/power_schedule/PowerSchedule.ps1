# PowerSchedule.ps1 — 정해진 시간대에 PC를 절전(S3, 설정으로 최대 절전 S4)으로 끄고, 정해진 시각에 다시 켠다.
#
#   -Mode Install    작업 스케줄러 등록 + 바탕화면 아이콘 생성 (처음 한 번)
#   -Mode Gui        설정 창 (바탕화면 아이콘이 실행하는 것, 기본값)
#   -Mode Guard      끄는 시각에 실행: 시간대 안에서 유휴가 이어지면 끔, 사용 중이면 미룸
#   -Mode Wake       켜는 시각에 실행(절전 해제 타이머): 무인 깨우기 상태를 풀어 다시 잠들지 않게 함
#   -Mode Uninstall  작업과 바탕화면 아이콘 제거
param([ValidateSet('Gui', 'Guard', 'Wake', 'Install', 'Uninstall')][string]$Mode = 'Gui')

$ErrorActionPreference = 'Stop'
$ConfigPath = Join-Path $PSScriptRoot 'config.json'
$LogPath = Join-Path $PSScriptRoot 'power_schedule.log'
$TaskOff = 'PowerSchedule-Off'
$TaskWake = 'PowerSchedule-Wake'
$TaskTest = 'PowerSchedule-Test'
$PsExe = "$env:WINDIR\System32\WindowsPowerShell\v1.0\powershell.exe"
$Shortcut = Join-Path ([Environment]::GetFolderPath('Desktop')) 'PC 전원 예약.lnk'

Add-Type -AssemblyName System.Windows.Forms
Add-Type @'
using System;
using System.Runtime.InteropServices;
public static class PsNative {
    [StructLayout(LayoutKind.Sequential)] struct LASTINPUTINFO { public uint cbSize; public uint dwTime; }
    [DllImport("user32.dll")] static extern bool GetLastInputInfo(ref LASTINPUTINFO p);
    public static double IdleSeconds() {
        var i = new LASTINPUTINFO(); i.cbSize = (uint)Marshal.SizeOf(i);
        GetLastInputInfo(ref i);
        return unchecked((uint)Environment.TickCount - i.dwTime) / 1000.0;
    }
    [DllImport("kernel32.dll")] public static extern uint SetThreadExecutionState(uint flags);
    [DllImport("user32.dll")] public static extern void mouse_event(uint flags, int dx, int dy, uint data, UIntPtr extra);
    [DllImport("user32.dll")] public static extern IntPtr SendMessageTimeout(IntPtr hWnd, uint msg, IntPtr wp, IntPtr lp, uint flags, uint timeout, out IntPtr result);
    [DllImport("powrprof.dll")] public static extern bool SetSuspendState(bool hibernate, bool force, bool disableWakeEvent);
}
'@

function Get-Config {
    $c = [ordered]@{
        Enabled = $true; OffTime = '01:00'; OnTime = '07:30'; IdleMinutes = 10
        BlockProcesses = @()                              # 실행 중이면 끄지 않음 (예: vmware-vmx = 켜진 VM)
        BusyProcesses = @('claude', 'Antigravity IDE')    # CPU를 쓰고 있으면(작업 중) 끄지 않음
        BusyCpuPercent = 10                               # 작업 중 판단 기준 (1코어 기준 %)
        PowerMode = 'Sleep'                               # Sleep = 절전(S3), Hibernate = 최대 절전(S4)
        SuspendVMs = $true                                # 끄기 전 켜진 VMware VM 일시 중지, 켜진 뒤 다시 시작
    }
    if (Test-Path $ConfigPath) {
        $j = Get-Content $ConfigPath -Raw -Encoding UTF8 | ConvertFrom-Json
        foreach ($k in @($c.Keys)) { if ($null -ne $j.$k) { $c[$k] = $j.$k } }
    }
    [pscustomobject]$c
}

function Save-Config($c) { $c | ConvertTo-Json | Set-Content $ConfigPath -Encoding UTF8 }

function Write-Log($msg) { "$(Get-Date -Format 'yyyy-MM-dd HH:mm:ss') $msg" | Add-Content $LogPath -Encoding UTF8 }

# 끄는 시각 <= 지금 < 켜는 시각 (자정을 넘는 구간도 처리)
function Test-InWindow($c, [datetime]$now) {
    $t = $now.TimeOfDay; $off = [timespan]$c.OffTime; $on = [timespan]$c.OnTime
    if ($off -lt $on) { $t -ge $off -and $t -lt $on } else { $t -ge $off -or $t -lt $on }
}

$VmRun = "${env:ProgramFiles(x86)}\VMware\VMware Workstation\vmrun.exe"
if (-not (Test-Path $VmRun)) { $VmRun = "$env:ProgramFiles\VMware\VMware Workstation\vmrun.exe" }
$VmListPath = Join-Path $PSScriptRoot 'suspended_vms.txt'

# 켜진 VM을 일시 중지하고 목록을 남긴다 (최대 절전에서 VM이 켜진 채 복귀하면 세션이 멈춘 적이 있음)
function Suspend-VMs {
    if (-not (Test-Path $VmRun)) { return }
    $vms = @(& $VmRun -T ws list | Select-Object -Skip 1 | ? { $_ -match '\.vmx$' })
    foreach ($v in $vms) {
        & $VmRun -T ws suspend $v | Out-Null
        if ($LASTEXITCODE -eq 0) { Add-Content $VmListPath $v -Encoding UTF8; Write-Log "VM 일시 중지: $v" }
        else { Write-Log "VM 일시 중지 실패: $v" }
    }
}

# Suspend-VMs 가 멈춘 VM을 다시 시작 (Wake·Guard 어느 쪽이 먼저 불러도 한 번만)
function Resume-VMs {
    if (-not (Test-Path $VmListPath)) { return }
    $vms = @(Get-Content $VmListPath -Encoding UTF8 | ? { $_ })
    Remove-Item $VmListPath
    foreach ($v in $vms) {
        & $VmRun -T ws start $v | Out-Null
        Write-Log "VM 다시 시작: $v (결과 $LASTEXITCODE)"
    }
}

function Enter-PowerOff($c) {
    if ($c.SuspendVMs) { Suspend-VMs }
    $hib = $c.PowerMode -eq 'Hibernate'
    Write-Log $(if ($hib) { '최대 절전 전환' } else { '절전 전환' })
    [void][PsNative]::SetSuspendState($hib, $false, $false)
    # 여기부터는 깨어난 뒤. 켜기 작업(Wake)이 실패해도 화면이 켜지도록 여기서도 켬
    Wake-Display
}

# 타이머로 깨면 화면이 꺼진 채라 크롬 원격 데스크톱에 화면이 안 나옴 → 화면 켜기.
# 깨어난 직후엔 무시될 수 있어 몇 번 반복 (화면 필요 신호 + 마우스 1px 왕복 + 모니터 켜기 신호)
function Wake-Display {
    $r = [IntPtr]::Zero
    for ($i = 0; $i -lt 5; $i++) {
        [void][PsNative]::SetThreadExecutionState(3)  # SYSTEM|DISPLAY (1회성: 유휴 타이머만 초기화)
        [PsNative]::mouse_event(0x0001, 1, 0, 0, [UIntPtr]::Zero)
        [PsNative]::mouse_event(0x0001, -1, 0, 0, [UIntPtr]::Zero)
        [void][PsNative]::SendMessageTimeout([IntPtr]0xffff, 0x0112, [IntPtr]0xF170, [IntPtr](-1), 2, 1000, [ref]$r)  # WM_SYSCOMMAND SC_MONITORPOWER 켬
        Start-Sleep -Seconds 2
    }
    Write-Log '화면 켜기 신호 보냄'
}

function Get-TaskArgs($m) { "-NoProfile -WindowStyle Hidden -ExecutionPolicy Bypass -File `"$PSCommandPath`" -Mode $m" }

function Register-Tasks($c) {
    $principal = New-ScheduledTaskPrincipal -UserId "$env:USERDOMAIN\$env:USERNAME" -LogonType Interactive
    # 끄기: 끄는 시각에 시작. 그 시각에 PC가 꺼져 있었으면 켜질 때 바로 시작(StartWhenAvailable)
    $setOff = New-ScheduledTaskSettingsSet -StartWhenAvailable -AllowStartIfOnBatteries -DontStopIfGoingOnBatteries `
        -ExecutionTimeLimit (New-TimeSpan) -MultipleInstances IgnoreNew
    Register-ScheduledTask $TaskOff -Action (New-ScheduledTaskAction $PsExe (Get-TaskArgs 'Guard')) `
        -Trigger (New-ScheduledTaskTrigger -Daily -At $c.OffTime) -Principal $principal -Settings $setOff -Force | Out-Null
    # 켜기: WakeToRun = 절전 해제 타이머로 최대 절전에서 깨움
    $setWake = New-ScheduledTaskSettingsSet -WakeToRun -AllowStartIfOnBatteries -DontStopIfGoingOnBatteries `
        -ExecutionTimeLimit (New-TimeSpan -Minutes 10)
    Register-ScheduledTask $TaskWake -Action (New-ScheduledTaskAction $PsExe (Get-TaskArgs 'Wake')) `
        -Trigger (New-ScheduledTaskTrigger -Daily -At $c.OnTime) -Principal $principal -Settings $setWake -Force | Out-Null
    foreach ($n in $TaskOff, $TaskWake) {
        if ($c.Enabled) { Enable-ScheduledTask $n | Out-Null } else { Disable-ScheduledTask $n | Out-Null }
    }
}

# 전원 옵션 '절전 해제 타이머 허용'(AC) 값. 1 = 사용
function Get-WakeTimerAc {
    $out = powercfg /q SCHEME_CURRENT SUB_SLEEP RTCWAKE | Out-String
    if ($out -match 'AC[^\r\n]*0x([0-9a-fA-F]{8})') { [Convert]::ToInt32($Matches[1], 16) } else { -1 }
}

# 60초 경고. 취소 버튼이나 키보드·마우스 입력이 있으면 $false
function Confirm-Hibernate {
    $f = New-Object Windows.Forms.Form -Property @{
        Text = 'PC 전원 예약'; Width = 380; Height = 160; TopMost = $true; StartPosition = 'CenterScreen'
        FormBorderStyle = 'FixedDialog'; MaximizeBox = $false; MinimizeBox = $false
    }
    $l = New-Object Windows.Forms.Label -Property @{ Left = 15; Top = 15; Width = 340; Height = 40 }
    $b = New-Object Windows.Forms.Button -Property @{ Text = '취소 (계속 사용)'; Left = 110; Top = 65; Width = 150; Height = 30; DialogResult = 'Cancel' }
    $f.Controls.AddRange(@($l, $b)); $f.CancelButton = $b
    $script:left = 60
    $l.Text = "$($script:left)초 뒤 PC를 끕니다(절전).`n사용 중이면 마우스를 움직이거나 취소를 누르세요."
    $t = New-Object Windows.Forms.Timer -Property @{ Interval = 1000 }
    $t.add_Tick({
            $script:left--
            $l.Text = "$($script:left)초 뒤 PC를 끕니다(절전).`n사용 중이면 마우스를 움직이거나 취소를 누르세요."
            if ([PsNative]::IdleSeconds() -lt 2 -and $script:left -lt 58) { $f.DialogResult = 'Cancel' }
            elseif ($script:left -le 0) { $f.DialogResult = 'OK' }
        })
    $t.Start()
    $r = $f.ShowDialog(); $t.Stop(); $f.Dispose()
    $r -eq 'OK'
}

function Get-CpuSeconds($names) {
    $h = @{}
    foreach ($n in $names) { $h[$n] = [double](Get-Process -Name $n -ErrorAction SilentlyContinue | Measure-Object CPU -Sum).Sum }
    $h
}

# 끄면 안 되는 이유. 없으면 $null
function Get-BlockReason($c, $cpuPrev, $cpuNow, $seconds) {
    foreach ($n in $c.BlockProcesses) { if (Get-Process -Name $n -ErrorAction SilentlyContinue) { return "$n 실행 중" } }
    foreach ($n in $c.BusyProcesses) {
        $pct = ($cpuNow[$n] - $cpuPrev[$n]) / $seconds * 100
        if ($pct -ge $c.BusyCpuPercent) { return ('{0} 작업 중 (CPU {1:N0}%)' -f $n, $pct) }
    }
    $null
}

function Invoke-Guard {
    $c = Get-Config
    if (-not $c.Enabled) { return }
    Write-Log "감시 시작 ($($c.OffTime)~$($c.OnTime), 유휴 $($c.IdleMinutes)분)"
    $since = (Get-Date).AddDays(-1)   # 유휴 계산 기준: 복귀·취소 이전의 유휴 시간은 세지 않음
    $last = Get-Date
    $cpu = Get-CpuSeconds $c.BusyProcesses
    $lastReason = $null
    while ($true) {
        if (-not (Test-InWindow $c (Get-Date))) { break }
        Start-Sleep -Seconds 30
        $now = Get-Date
        $cpuNow = Get-CpuSeconds $c.BusyProcesses
        $elapsed = ($now - $last).TotalSeconds
        if ($elapsed -gt 90) { $since = $now; Write-Log '절전에서 복귀'; Resume-VMs }
        $last = $now
        if (-not (Test-InWindow $c $now)) { break }
        $reason = Get-BlockReason $c $cpu $cpuNow $elapsed
        $cpu = $cpuNow
        if ($reason -ne $lastReason) { if ($reason) { Write-Log "보류: $reason" } else { Write-Log '보류 해제' } }
        $lastReason = $reason
        if ($reason) { continue }
        $idle = [math]::Min([PsNative]::IdleSeconds(), ($now - $since).TotalSeconds)
        if ($idle -lt $c.IdleMinutes * 60) { continue }
        if (-not (Confirm-Hibernate)) { Write-Log '사용 감지 → 이번 전환 취소'; $since = Get-Date; continue }
        Enter-PowerOff $c
        $since = Get-Date; $last = Get-Date
    }
    Resume-VMs
    Write-Log '감시 종료'
}

function Invoke-Wake {
    Write-Log '절전 해제 타이머로 깨어남'
    Unregister-ScheduledTask $TaskTest -Confirm:$false -ErrorAction SilentlyContinue
    # 타이머로 깨면 '무인' 상태라 2분 뒤 다시 잠든다 → 입력 1회 + 화면 켬으로 일반 상태로 전환
    [void][PsNative]::SetThreadExecutionState([Convert]::ToUInt32('80000003', 16))  # CONTINUOUS|SYSTEM|DISPLAY
    Wake-Display
    Resume-VMs
    Start-Sleep -Seconds 300
}

function Install-Shortcut {
    $s = (New-Object -ComObject WScript.Shell).CreateShortcut($Shortcut)
    $s.TargetPath = $PsExe
    $s.Arguments = "-NoProfile -WindowStyle Hidden -ExecutionPolicy Bypass -File `"$PSCommandPath`""
    $s.WorkingDirectory = $PSScriptRoot
    $s.IconLocation = "$env:WINDIR\System32\shell32.dll,27"
    $s.Description = 'PC 전원 예약 설정 (최대 절전/자동 켜기)'
    $s.Save()
}

function Get-StatusText {
    $lines = @()
    $hib = (powercfg /a | Out-String) -match '최대 절전 모드|Hibernate'
    $lines += "최대 절전 사용 가능: $(if ($hib) {'예'} else {'아니요 (관리자 cmd: powercfg /h on)'})"
    $wt = Get-WakeTimerAc
    $lines += "절전 해제 타이머 허용: $(switch ($wt) { 1 {'사용'} 0 {'사용 안 함 → 저장 시 켬'} default {'중요 타이머만/확인 불가'} })"
    foreach ($n in $TaskOff, $TaskWake) {
        $t = Get-ScheduledTask $n -ErrorAction SilentlyContinue
        if ($t) { $lines += "$n : $($t.State), 다음 실행 $((Get-ScheduledTaskInfo $n).NextRunTime)" }
        else { $lines += "$n : 등록 안 됨" }
    }
    if (Test-Path $LogPath) { $lines += ''; $lines += '최근 기록:'; $lines += Get-Content $LogPath -Tail 6 -Encoding UTF8 }
    $lines -join "`r`n"
}

function Show-Gui {
    $c = Get-Config
    $f = New-Object Windows.Forms.Form -Property @{
        Text = 'PC 전원 예약'; Width = 480; Height = 560; StartPosition = 'CenterScreen'
        FormBorderStyle = 'FixedDialog'; MaximizeBox = $false
        Icon = [Drawing.Icon]::ExtractAssociatedIcon($PsExe)
    }
    $font = New-Object Drawing.Font('맑은 고딕', 9)
    $f.Font = $font

    $chk = New-Object Windows.Forms.CheckBox -Property @{ Text = '예약 사용'; Left = 20; Top = 15; Width = 120; Checked = [bool]$c.Enabled }
    $f.Controls.Add((New-Object Windows.Forms.Label -Property @{ Text = '끄는 방식'; Left = 200; Top = 18; Width = 70 }))
    $mode = New-Object Windows.Forms.ComboBox -Property @{ DropDownStyle = 'DropDownList'; Left = 275; Top = 14; Width = 170 }
    [void]$mode.Items.AddRange(@('절전 (권장)', '최대 절전'))
    $mode.SelectedIndex = $(if ($c.PowerMode -eq 'Hibernate') { 1 } else { 0 })
    $vmChk = New-Object Windows.Forms.CheckBox -Property @{ Text = '끄기 전 VM 일시 중지'; Left = 275; Top = 50; Width = 175; Checked = [bool]$c.SuspendVMs }
    function New-TimeBox($top, $label, $value) {
        $f.Controls.Add((New-Object Windows.Forms.Label -Property @{ Text = $label; Left = 20; Top = $top + 3; Width = 150 }))
        $p = New-Object Windows.Forms.DateTimePicker -Property @{ Format = 'Custom'; CustomFormat = 'HH:mm'; ShowUpDown = $true; Left = 180; Top = $top; Width = 80 }
        $p.Value = [datetime]::Today + [timespan]$value
        $f.Controls.Add($p); $p
    }
    $off = New-TimeBox 50 '끄는 시각' $c.OffTime
    $on = New-TimeBox 85 '켜는 시각 (자동 켜기)' $c.OnTime
    $f.Controls.Add((New-Object Windows.Forms.Label -Property @{ Text = '유휴 기준 (분)'; Left = 20; Top = 123; Width = 150 }))
    $idle = New-Object Windows.Forms.NumericUpDown -Property @{ Minimum = 1; Maximum = 180; Left = 180; Top = 120; Width = 80 }
    $idle.Value = [int]$c.IdleMinutes
    $f.Controls.Add((New-Object Windows.Forms.Label -Property @{
                Text = '이 시간 동안 입력이 없을 때만 끕니다. 사용 중이면 미룹니다.'
                Left = 20; Top = 150; Width = 430; Height = 20; ForeColor = 'DimGray'
            }))
    function New-ListBox($top, $label, $list) {
        $f.Controls.Add((New-Object Windows.Forms.Label -Property @{ Text = $label; Left = 20; Top = $top + 3; Width = 160 }))
        $t = New-Object Windows.Forms.TextBox -Property @{ Left = 180; Top = $top; Width = 265; Text = (@($list) -join ', ') }
        $f.Controls.Add($t); $t
    }
    $block = New-ListBox 180 '실행 중이면 끄지 않음' $c.BlockProcesses
    $busy = New-ListBox 212 '작업 중이면 끄지 않음' $c.BusyProcesses
    $f.Controls.Add((New-Object Windows.Forms.Label -Property @{ Text = '작업 중 기준 CPU (%)'; Left = 20; Top = 247; Width = 150 }))
    $busyPct = New-Object Windows.Forms.NumericUpDown -Property @{ Minimum = 1; Maximum = 400; Left = 180; Top = 244; Width = 80 }
    $busyPct.Value = [int]$c.BusyCpuPercent
    $f.Controls.Add((New-Object Windows.Forms.Label -Property @{
                Text = '프로세스 이름(.exe 제외), 쉼표로 구분. CPU는 1코어 기준.'
                Left = 20; Top = 272; Width = 430; Height = 20; ForeColor = 'DimGray'
            }))
    # 실시간 CPU: '작업 중이면 끄지 않음' 칸의 프로그램을 3초마다 측정 (감시는 30초 평균으로 판단)
    $live = New-Object Windows.Forms.Label -Property @{ Text = '현재 CPU: 측정 중…'; Left = 20; Top = 294; Width = 430; Height = 20; ForeColor = 'DarkBlue' }
    $script:cpuPrev = $null; $script:cpuAt = Get-Date
    $tick = New-Object Windows.Forms.Timer -Property @{ Interval = 3000 }
    $tick.add_Tick({
            $names = @($busy.Text -split ',' | % { $_.Trim() -replace '\.exe$', '' } | ? { $_ })
            $now = Get-Date; $cur = Get-CpuSeconds $names
            if ($script:cpuPrev) {
                $sec = ($now - $script:cpuAt).TotalSeconds
                $parts = foreach ($n in $names) {
                    if (-not $script:cpuPrev.ContainsKey($n)) { continue }
                    $pct = ($cur[$n] - $script:cpuPrev[$n]) / $sec * 100
                    '{0} {1:N0}%({2})' -f $n, $pct, $(if ($pct -ge $busyPct.Value) { '작업 중' } else { '쉼' })
                }
                $live.Text = '현재 CPU: ' + ($parts -join ' · ')
            }
            $script:cpuPrev = $cur; $script:cpuAt = $now
        })
    $tick.Start()
    $status = New-Object Windows.Forms.TextBox -Property @{
        Multiline = $true; ReadOnly = $true; ScrollBars = 'Vertical'; Left = 20; Top = 320; Width = 425; Height = 130
        Font = New-Object Drawing.Font('Consolas', 9)
    }
    $status.Text = Get-StatusText

    $btnSave = New-Object Windows.Forms.Button -Property @{ Text = '저장·적용'; Left = 20; Top = 465; Width = 100; Height = 30 }
    $btnTest = New-Object Windows.Forms.Button -Property @{ Text = '켜기 테스트'; Left = 128; Top = 465; Width = 100; Height = 30 }
    $btnLog = New-Object Windows.Forms.Button -Property @{ Text = '기록 열기'; Left = 236; Top = 465; Width = 100; Height = 30 }
    $btnClose = New-Object Windows.Forms.Button -Property @{ Text = '닫기'; Left = 345; Top = 465; Width = 100; Height = 30; DialogResult = 'Cancel' }
    $f.CancelButton = $btnClose

    $btnSave.add_Click({
            if ($off.Value.ToString('HH:mm') -eq $on.Value.ToString('HH:mm')) {
                [Windows.Forms.MessageBox]::Show('끄는 시각과 켜는 시각이 같습니다.', 'PC 전원 예약') | Out-Null; return
            }
            $c.Enabled = $chk.Checked; $c.OffTime = $off.Value.ToString('HH:mm'); $c.OnTime = $on.Value.ToString('HH:mm'); $c.IdleMinutes = [int]$idle.Value
            $c.BlockProcesses = @($block.Text -split ',' | % { $_.Trim() -replace '\.exe$', '' } | ? { $_ })
            $c.BusyProcesses = @($busy.Text -split ',' | % { $_.Trim() -replace '\.exe$', '' } | ? { $_ })
            $c.BusyCpuPercent = [int]$busyPct.Value
            $c.PowerMode = $(if ($mode.SelectedIndex -eq 1) { 'Hibernate' } else { 'Sleep' })
            $c.SuspendVMs = $vmChk.Checked
            try {
                Save-Config $c
                if ((Get-WakeTimerAc) -eq 0) { powercfg /setacvalueindex SCHEME_CURRENT SUB_SLEEP RTCWAKE 1; powercfg /setactive SCHEME_CURRENT }
                Register-Tasks $c
                Write-Log "설정 저장: 사용=$($c.Enabled) $($c.OffTime)~$($c.OnTime) 유휴 $($c.IdleMinutes)분 CPU $($c.BusyCpuPercent)% $($c.PowerMode) VM중지=$($c.SuspendVMs)"
                $status.Text = Get-StatusText
                [Windows.Forms.MessageBox]::Show('저장했습니다.', 'PC 전원 예약') | Out-Null
            } catch { [Windows.Forms.MessageBox]::Show("저장 실패: $_", 'PC 전원 예약', 'OK', 'Error') | Out-Null }
        })

    $btnTest.add_Click({
            $msg = "30초 뒤 자동으로 켜지도록 예약하고 저장된 설정의 방식으로 지금 바로 끕니다.`n" +
            "열려 있는 작업을 저장한 뒤 진행하세요.`n`n30초쯤 뒤 저절로 켜지면 성공입니다. 계속할까요?"
            if ([Windows.Forms.MessageBox]::Show($msg, '켜기 테스트', 'OKCancel', 'Question') -ne 'OK') { return }
            $principal = New-ScheduledTaskPrincipal -UserId "$env:USERDOMAIN\$env:USERNAME" -LogonType Interactive
            $set = New-ScheduledTaskSettingsSet -WakeToRun -AllowStartIfOnBatteries -DontStopIfGoingOnBatteries -ExecutionTimeLimit (New-TimeSpan -Minutes 10)
            Register-ScheduledTask $TaskTest -Action (New-ScheduledTaskAction $PsExe (Get-TaskArgs 'Wake')) `
                -Trigger (New-ScheduledTaskTrigger -Once -At (Get-Date).AddSeconds(30)) -Principal $principal -Settings $set -Force | Out-Null
            Write-Log '켜기 테스트: 30초 뒤 깨우기 예약'
            Enter-PowerOff (Get-Config)
        })

    $btnLog.add_Click({ if (Test-Path $LogPath) { notepad.exe $LogPath } })

    $f.Controls.AddRange(@($chk, $mode, $vmChk, $idle, $busyPct, $live, $status, $btnSave, $btnTest, $btnLog, $btnClose))
    [void]$f.ShowDialog()
    $tick.Stop()
}

# 작업 스케줄러에서는 창이 숨겨져 있어 오류가 보이지 않으므로 기록에 남긴다
trap { Write-Log "오류($Mode): $_ [$($_.InvocationInfo.ScriptLineNumber)행]"; break }

switch ($Mode) {
    'Guard' { Invoke-Guard }
    'Wake' { Invoke-Wake }
    'Gui' { Show-Gui }
    'Install' {
        $c = Get-Config
        Save-Config $c
        if ((Get-WakeTimerAc) -eq 0) { powercfg /setacvalueindex SCHEME_CURRENT SUB_SLEEP RTCWAKE 1; powercfg /setactive SCHEME_CURRENT }
        Register-Tasks $c
        Install-Shortcut
        Write-Log "설치: $($c.OffTime)~$($c.OnTime) 유휴 $($c.IdleMinutes)분"
        Write-Output "설치 완료. 바탕화면 아이콘: $Shortcut"
    }
    'Uninstall' {
        foreach ($n in $TaskOff, $TaskWake, $TaskTest) { Unregister-ScheduledTask $n -Confirm:$false -ErrorAction SilentlyContinue }
        Remove-Item $Shortcut -ErrorAction SilentlyContinue
        Write-Output '작업과 바탕화면 아이콘을 제거했습니다.'
    }
}
