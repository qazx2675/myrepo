# PowerShell Linux-like Environment Uninstaller
Write-Host "==========================================================" -ForegroundColor Yellow
Write-Host "  PowerShell Linux 환경 설정 제거 스크립트" -ForegroundColor Yellow
Write-Host "==========================================================" -ForegroundColor Yellow
Write-Host ""

$myDocs = [Environment]::GetFolderPath("MyDocuments")
$profileTargets = @(
    "$myDocs\WindowsPowerShell\Microsoft.PowerShell_profile.ps1",
    "$myDocs\PowerShell\Microsoft.PowerShell_profile.ps1",
    "$env:USERPROFILE\Documents\WindowsPowerShell\Microsoft.PowerShell_profile.ps1",
    "$env:USERPROFILE\Documents\PowerShell\Microsoft.PowerShell_profile.ps1"
) | Select-Object -Unique

foreach ($target in $profileTargets) {
    if (Test-Path $target) {
        Remove-Item -Path $target -Force
        Write-Host "[삭제 완료] $target" -ForegroundColor Green
    }
}

Write-Host ""
Write-Host "설정이 정상적으로 제거되었습니다. PowerShell을 재시작하면 기본값으로 복원됩니다." -ForegroundColor Green
