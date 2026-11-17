# 받은 파일과 원본을 SHA-256 과 바이트 단위로 비교한다.
# 사용: powershell -File test\compare.ps1 -Orig <원본폴더> -Got <받은폴더>
param([string]$Orig, [string]$Got)
$fail = 0
Get-ChildItem -LiteralPath $Orig -File | Sort-Object Name | ForEach-Object {
    $name = $_.Name
    $safe = ($name.ToCharArray() | ForEach-Object { if ('<>:"/\|?*'.Contains($_)) { '_' } else { $_ } }) -join ''
    $g = Join-Path $Got $safe
    if (-not (Test-Path -LiteralPath $g)) { "MISSING`t$name"; $fail = 1; return }
    $a = [IO.File]::ReadAllBytes($_.FullName); $b = [IO.File]::ReadAllBytes($g)
    $ha = (Get-FileHash -LiteralPath $_.FullName -Algorithm SHA256).Hash
    $hb = (Get-FileHash -LiteralPath $g -Algorithm SHA256).Hash
    $bytesEq = ($a.Length -eq $b.Length) -and ([Linq.Enumerable]::SequenceEqual([byte[]]$a, [byte[]]$b))
    $r = if ($ha -eq $hb -and $bytesEq) { 'PASS' } else { $fail = 1; 'FAIL' }
    "$r`t$name`t$($a.Length) bytes`tsha256=$($ha.Substring(0,16))..`tbytes_equal=$bytesEq"
}
exit $fail
