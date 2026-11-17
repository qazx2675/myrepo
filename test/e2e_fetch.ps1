# 다운로더 E2E (윈도우): awx_fetch.exe 를 N 번 실행해 파일을 받는다.
# 사용: powershell -File test\e2e_fetch.ps1 -Exe dist\awx_fetch.exe -Config <conf> -Out <폴더> -Count 11
param(
    [string]$Exe,
    [string]$Config,
    [string]$Out,
    [int]$Count
)
$ErrorActionPreference = 'Continue'
New-Item -ItemType Directory -Force $Out | Out-Null
for ($i = 1; $i -le $Count; $i++) {
    $t = Measure-Command { & $Exe --no-pause --config $Config --out $Out *> "$Out\..\fetch_$i.log" }
    $last = (Get-Content "$Out\..\fetch_$i.log" -Encoding utf8 | Select-Object -Last 1)
    "FETCH`t$i`trc=$LASTEXITCODE`t$([math]::Round($t.TotalSeconds,2))s`t$last"
}
