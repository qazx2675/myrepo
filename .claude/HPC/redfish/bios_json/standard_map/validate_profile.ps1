# validate_profile.ps1 - biostool 프로파일(profiles/*.tsv)의 오프라인 정합성 검증기.
#
# (a) 구문 검증: cmd/biostool/profile.go parseProfile 규칙을 그대로 재현한다 (오류를 줄 번호와 함께 모두 모은다).
# (b) 데이터 대조: 각 행을 map_bios_attrs.ps1 의 $models 표로 bios_json/<벤더>/<버전>/bios_attributes*.json 에 연결하고,
#     속성 이름과 기대값(A|B 분해)이 조사 데이터의 허용값 목록에 있는지 확인한다.
# (c) 모델 정규화 점검: 같은 벤더 안에서 modelKey 가 같거나 접두 관계인 모델을 정보로 표시한다.
#
# 사용: powershell -File validate_profile.ps1 [-Profile <tsv>] [-Out <md>].
#
# 주의 (PowerShell 5.1): BOM 없는 UTF-8 파일은 시스템 ANSI(cp949)로 읽힌다. 그래서 이 파일은:
#  1) 한글을 주석에만 쓰고 모든 한글 주석 줄을 ASCII 문자로 끝낸다 (줄 끝 한글이 개행을 삼키는 것을 막는다).
#  2) 코드의 문자열과 보고서(-Out)는 ASCII 영어로만 쓴다.
# 이 파일을 고칠 때도 이 두 가지를 지킬 것.
#
# 외부 모듈을 쓰지 않는다. 원본 map_bios_attrs.ps1 은 읽기만 한다 ($models/$vdir 표를 AST 로 읽어 재사용).
param(
  [string]$Profile = (Join-Path $PSScriptRoot '..\..\profiles\VM_research.tsv'),
  [string]$Out = ''
)
$ErrorActionPreference = 'Stop'
$dataRoot  = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path        # bios_json 폴더.
$mapScript = Join-Path $PSScriptRoot 'map_bios_attrs.ps1'
$profPath  = (Resolve-Path -LiteralPath $Profile).Path

function Arr($v) { if ($null -eq $v) { @() } else { @($v) } }

# ---- 키 정규화 (inspect.go normalizeVendor / modelnorm.go normalizeModel 과 동일) ----------------
function Get-VendorKey([string]$v) {
  $l = $v.Trim().ToLowerInvariant()
  if ($l.Contains('dell')) { return 'dell' }
  if ($l.Contains('hpe') -or $l.Contains('hewlett')) { return 'hpe' }
  if ($l.Contains('lenovo')) { return 'lenovo' }
  if ($l.Contains('cisco')) { return 'cisco' }
  if ($l.Contains('supermicro') -or $l.Contains('super micro')) { return 'supermicro' }
  return $l
}
$script:ModelPrefixes = @('hewlettpackardenterprise', 'thinksystem', 'thinkserver', 'supermicro', 'poweredge', 'proliant', 'dellemc', 'lenovo', 'cisco', 'dell', 'hpe', 'hp')
function Get-ModelKey([string]$s) {
  $sb = New-Object System.Text.StringBuilder
  foreach ($c in $s.ToLowerInvariant().ToCharArray()) {
    if ($c -eq ' ' -or $c -eq "`t" -or $c -eq '-' -or $c -eq '_' -or [int]$c -eq 0x00A0) { continue }
    [void]$sb.Append($c)
  }
  $k = $sb.ToString()
  $stripped = $true
  while ($stripped) {                    # 접두는 남는 글자가 있을 때만 반복해서 뗀다.
    $stripped = $false
    foreach ($p in $script:ModelPrefixes) {
      if ($k.Length -gt $p.Length -and $k.StartsWith($p, [StringComparison]::Ordinal)) { $k = $k.Substring($p.Length); $stripped = $true; break }
    }
  }
  return $k
}

# ---- (a) 구문 검증 ---------------------------------------------------------------------------
$header = @('vendor', 'model', 'std_name', 'attribute', 'value', 'verified')
$rows = New-Object System.Collections.ArrayList
$synErrors = New-Object System.Collections.ArrayList
function Add-SynError([int]$n, [string]$msg) { [void]$script:synErrors.Add([pscustomobject]@{ Line = $n; Msg = $msg }) }

$text = [IO.File]::ReadAllText($profPath, (New-Object System.Text.UTF8Encoding($false))).TrimStart([char]0xFEFF)
$headerDone = $false
$seen = @{}
$lineNo = 0
foreach ($raw in $text.Split("`n")) {
  $lineNo++
  $line = $raw.TrimEnd("`r")
  $t = $line.Trim()
  if ($t -eq '' -or $t.StartsWith('#')) { continue }
  $cols = New-Object 'System.Collections.Generic.List[string]'
  $cols.AddRange([string[]]$line.Split("`t"))
  while ($cols.Count -gt 6 -and $cols[$cols.Count - 1].Trim() -eq '') { $cols.RemoveAt($cols.Count - 1) }   # 엑셀 붙여넣기의 끝 빈 열은 무시.
  if ($cols.Count -ne 6) { Add-SynError $lineNo ("expected 6 tab-separated columns, got {0} (spaces instead of tabs?)" -f $cols.Count); continue }
  for ($j = 0; $j -lt 6; $j++) { $cols[$j] = $cols[$j].Trim() }
  if (-not $headerDone) {
    $bad = $false
    for ($j = 0; $j -lt 6; $j++) { if ($cols[$j] -ine $header[$j]) { $bad = $true } }
    if ($bad) { Add-SynError $lineNo ('first non-comment line must be the header: ' + ($header -join ' ')) }
    $headerDone = $true
    continue
  }
  $err = $null
  for ($j = 0; $j -lt 5 -and -not $err; $j++) { if ($cols[$j] -eq '') { $err = $header[$j] + ' is empty' } }
  if (-not $err -and $cols[2] -cnotmatch '^[a-z0-9_]+$') { $err = "std_name '" + $cols[2] + "' may contain only a-z, 0-9 and underscore" }
  $allowed = @()
  if (-not $err) {
    foreach ($v in $cols[4].Split('|')) {
      $v = $v.Trim()
      if ($v -eq '') { $err = "value '" + $cols[4] + "' has an empty alternative (A|B form)"; break }
      $allowed += $v
    }
  }
  $verified = $false
  if (-not $err) {
    switch ($cols[5].ToUpperInvariant()) {
      'Y' { $verified = $true }
      'N' { }
      default { $err = "verified must be Y or N (got '" + $cols[5] + "')" }
    }
  }
  $vk = Get-VendorKey $cols[0]
  $mk = ''
  if (-not $err) {
    if ($cols[1] -eq '*') {
      if ($verified) { $err = 'verified=Y is allowed only on rows that name a model; * rows are comparison-only (keep verified=N)' } else { $mk = '*' }
    } else {
      $mk = Get-ModelKey $cols[1]
      if ($mk -eq '') { $err = "model '" + $cols[1] + "' has no comparable characters left after normalization" }
    }
  }
  if (-not $err) {
    $key = $vk + '|' + $mk + '|' + $cols[2] + '|' + $cols[3].ToLowerInvariant()
    if ($seen.ContainsKey($key)) { $err = ('duplicate of line {0} ({1} {2} {3} {4})' -f $seen[$key], $cols[0], $cols[1], $cols[2], $cols[3]) } else { $seen[$key] = $lineNo }
  }
  if ($err) { Add-SynError $lineNo $err; continue }
  [void]$rows.Add([pscustomobject]@{ Line = $lineNo; Vendor = $cols[0]; Model = $cols[1]; Std = $cols[2]; Attr = $cols[3]; Value = $cols[4]; Alts = $allowed; Verified = $verified; VKey = $vk; MKey = $mk })
}
if (-not $headerDone) { Add-SynError 0 ('no header line (' + ($header -join ' ') + ')') }
elseif ($rows.Count -eq 0 -and $synErrors.Count -eq 0) { Add-SynError 0 'no data rows' }

# ---- 모델 -> 데이터 파일 표 ($models, $vdir 는 map_bios_attrs.ps1 에서 읽는다) ------------------------
function Get-ScriptLiteral([string]$path, [string]$varName) {
  $tokens = $null; $perr = $null
  $ast = [System.Management.Automation.Language.Parser]::ParseFile($path, [ref]$tokens, [ref]$perr)
  $node = $ast.Find({ param($n) $n -is [System.Management.Automation.Language.AssignmentStatementAst] -and $n.Left.Extent.Text -eq $varName }, $true)
  if (-not $node) { throw "$varName assignment not found in $path" }
  return $node.Right.Expression.SafeGetValue()
}
$modelsRaw = Get-ScriptLiteral $mapScript '$models'
$vdir = Get-ScriptLiteral $mapScript '$vdir'
$tbl = New-Object System.Collections.ArrayList
foreach ($m in $modelsRaw) {
  $fname = if ($m[3]) { $m[3] } else { 'bios_attributes.json' }        # 파일명이 비어 있으면 폴더의 bios_attributes.json.
  [void]$tbl.Add([pscustomobject]@{ VKey = (Get-VendorKey $m[0]); MKey = (Get-ModelKey $m[1]); Vendor = $m[0]; Model = $m[1]; Rel = ($vdir[$m[0]] + '/' + $m[2] + '/' + $fname) })
}

# ---- 속성 파일 읽기: 스키마별 평탄화 (map_bios_attrs.ps1 Flatten 과 같은 구분, 허용값 외 관측값/표시값도 보관) ----
function ConvertTo-Str($x) {
  if ($null -eq $x) { return $null }
  if ($x -is [bool]) { if ($x) { return 'true' } else { return 'false' } }
  if ($x -is [string]) { return $x }
  if ($x -is [ValueType]) { return ([IFormattable]$x).ToString($null, [Globalization.CultureInfo]::InvariantCulture) }
  return $null                          # 객체/배열은 값으로 쓰지 않는다.
}
function Add-Attr($idx, $name, $kind, $allowed, $observed, $display, $cls, $plats) {
  if (-not $name) { return }
  $name = [string]$name
  if (-not $idx.ContainsKey($name)) {
    $idx[$name] = [pscustomobject]@{ Name = $name; Kind = $kind; Allowed = (New-Object System.Collections.ArrayList); Observed = (New-Object System.Collections.ArrayList); Display = (New-Object System.Collections.ArrayList); Classes = (New-Object System.Collections.ArrayList); Platforms = (New-Object System.Collections.ArrayList); NotExposed = '' }
  }
  $e = $idx[$name]
  foreach ($pair in @(@($e.Allowed, $allowed), @($e.Observed, $observed), @($e.Display, $display), @($e.Classes, $cls), @($e.Platforms, $plats))) {
    foreach ($x in (Arr $pair[1])) {
      $s = ConvertTo-Str $x
      if ($null -ne $s -and -not $pair[0].Contains($s)) { [void]$pair[0].Add($s) }
    }
  }
}
function Get-Observed($e) {              # 실제 장비/샘플에서 관측된 값 (레지스트리 허용값이 아님).
  $o = New-Object System.Collections.ArrayList
  foreach ($x in (Arr $e.observed)) { if ($null -ne $x -and @($x.PSObject.Properties.Name) -contains 'value') { [void]$o.Add($x.value) } }
  $sv = $e.sample_value
  if ($null -ne $sv) {
    if ($sv -is [pscustomobject]) { foreach ($p in $sv.PSObject.Properties) { [void]$o.Add($p.Value) } } else { [void]$o.Add($sv) }
  }
  if ($null -ne $e.sample_default) { [void]$o.Add($e.sample_default) }
  return $o
}
function Get-Platforms($e) {             # 속성 근거(관측/튜닝 가이드)가 나온 플랫폼 이름. unknown* 은 제외한다.
  $o = New-Object System.Collections.ArrayList
  $src = @()
  $src += Arr $e.observed
  $src += Arr $e.tuning_guide
  foreach ($x in $src) {
    if ($null -ne $x -and @($x.PSObject.Properties.Name) -contains 'platform' -and $x.platform) {
      foreach ($p in ([string]$x.platform).Split('+')) { if ($p -and -not $p.StartsWith('unknown')) { [void]$o.Add($p) } }
    }
  }
  return $o
}
function Read-AttrFile([string]$full) {
  $j = Get-Content -LiteralPath $full -Raw -Encoding UTF8 | ConvertFrom-Json
  $idx = New-Object 'System.Collections.Generic.Dictionary[string,object]'
  $plat = @{}                              # 모델 키 -> 플랫폼 (_meta.models 가 플랫폼 -> 모델 목록 객체일 때).
  if ($null -ne $j._meta -and $j._meta.models -is [pscustomobject]) {
    foreach ($p in $j._meta.models.PSObject.Properties) { foreach ($mn in (Arr $p.Value)) { $plat[(Get-ModelKey ([string]$mn))] = ([string]$p.Name) -replace '_xcc$', '' } }
  }
  $a = $j.attributes
  if ($null -ne $a -and $a -isnot [array] -and @($a.PSObject.Properties).Count -gt 0) {        # HPE: attributes 가 이름 -> 정의 dict.
    foreach ($p in $a.PSObject.Properties) { Add-Attr $idx $p.Name 'redfish' $p.Value.allowed_values (Get-Observed $p.Value) $null $null }
  } elseif ($null -ne $a) {
    foreach ($e in (Arr $a)) {
      $names = @($e.PSObject.Properties.Name)
      if ($names -contains 'name') {                                                              # Dell / Lenovo.
        $disp = $e.allowed_value_display
        if (-not $disp) { $disp = $e.allowed_values_display }
        Add-Attr $idx $e.name 'redfish' $e.allowed_values (Get-Observed $e) $disp $null (Get-Platforms $e)
        $ex = $e.exposed_in_bios_resource                                                         # Dell: 캡처별로 /Bios Attributes 에 노출되는지.
        if ($ex -is [pscustomobject]) {
          $exv = @($ex.PSObject.Properties | ForEach-Object { $_.Value })
          if ($exv.Count -gt 0 -and @($exv | Where-Object { $_ -eq $true }).Count -eq 0) { $idx[[string]$e.name].NotExposed = (@($ex.PSObject.Properties.Name) -join ', ') }
        }
      } elseif ($names -contains 'ui_name') {                                                     # Supermicro: UI 이름.
        $n = $e.attribute_id; $kind = 'redfish'
        if (-not $n) { $n = $e.ui_name; $kind = 'ui' }
        Add-Attr $idx $n $kind $e.allowed_values $null $null $null
      }
    }
  }
  foreach ($tc in (Arr $j.token_classes)) {                                                      # Cisco UCSM/CIMC XML 클래스.
    foreach ($t in (Arr $tc.tokens)) {
      $av = $t.allowed_values
      if (-not $av -and $t.platforms) { $pp = $t.platforms.PSObject.Properties | Select-Object -First 1; if ($pp) { $av = $pp.Value.allowed_values } }
      Add-Attr $idx $t.attribute 'xml' $av $null $null $tc.class
    }
  }
  return [pscustomobject]@{ Idx = $idx; Plat = $plat }
}
$script:FileCache = @{}
function Get-AttrFile([string]$rel) {                                  # 없는 파일이면 $null.
  if (-not $script:FileCache.ContainsKey($rel)) {
    $full = Join-Path $dataRoot ($rel -replace '/', '\')
    if (Test-Path -LiteralPath $full) { $script:FileCache[$rel] = Read-AttrFile $full } else { $script:FileCache[$rel] = $null }
  }
  return $script:FileCache[$rel]
}
function Get-AttrIndex([string]$rel) {
  $f = Get-AttrFile $rel
  if ($null -eq $f) { return $null }
  return ,$f.Idx
}

# ---- 값 비교 (check.go matchAny 와 같음: 정확히 같거나 둘 다 10진 정수로 같음) -----------------------
function Test-MatchAny($list, [string]$v) {
  $v = $v.Trim()
  $ns = [Globalization.NumberStyles]::AllowLeadingSign
  $ic = [Globalization.CultureInfo]::InvariantCulture
  $vn = 0L; $vok = [long]::TryParse($v, $ns, $ic, [ref]$vn)
  foreach ($a in $list) {
    if ($a -ceq $v) { return $true }
    $an = 0L
    if ($vok -and [long]::TryParse([string]$a, $ns, $ic, [ref]$an) -and $an -eq $vn) { return $true }
  }
  return $false
}
function Get-Cmp($alts, $list) {          # none / match / case-only / similar / absent
  if (@($list).Count -eq 0) { return 'none' }
  foreach ($a in $alts) { if (Test-MatchAny $list $a) { return 'match' } }
  foreach ($a in $alts) { foreach ($x in $list) { if ($x -ieq $a) { return 'case-only' } } }
  if (@(Get-Similar $alts $list).Count -gt 0) { return 'similar' }
  return 'absent'
}
function Get-Similar($alts, $list) {      # Enable/Enabled 같은 접두 관계의 값.
  $o = New-Object System.Collections.ArrayList
  foreach ($a in $alts) {
    foreach ($x in $list) {
      if ($x -ceq $a) { continue }
      if ([Math]::Min($a.Length, $x.Length) -ge 3 -and ($x.StartsWith($a, [StringComparison]::OrdinalIgnoreCase) -or $a.StartsWith($x, [StringComparison]::OrdinalIgnoreCase))) {
        if (-not $o.Contains($x)) { [void]$o.Add($x) }
      }
    }
  }
  return $o
}
function Format-List2($list, [int]$max = 20) {
  $l = @($list)
  if ($l.Count -eq 0) { return '(none)' }
  $s = ($l | Select-Object -First $max) -join ', '
  if ($l.Count -gt $max) { $s += ', ... (+' + ($l.Count - $max) + ')' }
  return $s
}

# ---- (b) 행 판정 ------------------------------------------------------------------------------
function Get-Targets($row) {                # 행 하나가 대조할 파일 목록 (model * 는 벤더의 모든 파일).
  if ($row.MKey -eq '*') {
    $rels = @($tbl | Where-Object { $_.VKey -eq $row.VKey } | ForEach-Object { $_.Rel } | Select-Object -Unique)
    if ($rels.Count -eq 0) { return @([pscustomobject]@{ Rel = $null; Why = 'vendor not present in $models of map_bios_attrs.ps1' }) }
    return @($rels | ForEach-Object { [pscustomobject]@{ Rel = $_; Why = $null } })
  }
  $hit = $tbl | Where-Object { $_.VKey -eq $row.VKey -and $_.MKey -eq $row.MKey } | Select-Object -First 1
  if ($hit) { return @([pscustomobject]@{ Rel = $hit.Rel; Why = $null }) }
  return @([pscustomobject]@{ Rel = $null; Why = ('model not in $models of map_bios_attrs.ps1 (vendorKey=' + $row.VKey + ', modelKey=' + $row.MKey + ')') })
}
function Test-Row($row, $target) {
  $res = [ordered]@{ Line = $row.Line; Vendor = $row.Vendor; VKey = $row.VKey; Model = $row.Model; Std = $row.Std; Attr = $row.Attr; Value = $row.Value
                     Verdict = ''; Partial = $false; Caution = $false; Evidence = ''; AllowedText = ''; Other = ''; Note = ''; Source = '' }
  $notes = New-Object System.Collections.ArrayList
  $alts = @($row.Alts)
  if ($null -eq $target.Rel) {
    $res.Verdict = 'NO_DATA_FILE'; [void]$notes.Add($target.Why)
  } else {
    $res.Source = $target.Rel
    $idx = Get-AttrIndex $target.Rel
    if ($null -eq $idx) {
      $res.Verdict = 'NO_DATA_FILE'; [void]$notes.Add('file not found: bios_json/' + $target.Rel)
    } else {
      $e = $null; $caseOnly = $false
      if ($idx.ContainsKey($row.Attr)) { $e = $idx[$row.Attr] }
      else {                                                  # biostool lookupAttr: 정확히 일치 우선, 없으면 대소문자 무시.
        $best = $null
        foreach ($k in $idx.Keys) { if ($k -ieq $row.Attr -and ($null -eq $best -or [string]::CompareOrdinal($k, $best) -lt 0)) { $best = $k } }
        if ($null -ne $best) { $e = $idx[$best]; $caseOnly = $true }
      }
      if ($null -eq $e) {
        $res.Verdict = 'ATTR_NOT_IN_DATA'
        $low = $row.Attr.ToLowerInvariant()
        $sim = @($idx.Keys | Where-Object { $n = $_.ToLowerInvariant(); $n.Contains($low) -or ($n.Length -ge 4 -and $low.Contains($n)) } | Select-Object -First 8)
        if ($sim.Count -gt 0) { [void]$notes.Add('similar names in data: ' + ($sim -join ', ')) }
      } else {
        if ($caseOnly) { [void]$notes.Add('name matches only ignoring case (biostool accepts it): ' + $e.Name) }
        if ($e.Kind -eq 'xml') { [void]$notes.Add('XML token (UCSM class ' + ($e.Classes -join ',') + '), not a Redfish attribute') }
        if ($e.NotExposed) {
          $res.Caution = $true
          [void]$notes.Add('EXPOSURE: registry entry exists but exposed_in_bios_resource is false on every capture (' + $e.NotExposed + '), so it may be absent from Bios Attributes on a live host')
        }
        if ($row.MKey -ne '*' -and @($e.Platforms).Count -gt 0) {     # 속성 근거의 플랫폼이 모델의 플랫폼과 다르면 표시한다.
          $mp = (Get-AttrFile $target.Rel).Plat[$row.MKey]
          if ($mp -and @($e.Platforms) -notcontains $mp) {
            $res.Caution = $true
            [void]$notes.Add('PLATFORM: data evidence for this attribute exists only for [' + ((@($e.Platforms) | Select-Object -Unique) -join ', ') + '] but _meta.models lists this model under ' + $mp)
          }
        }
        $allowed = @($e.Allowed)
        $res.AllowedText = Format-List2 $allowed
        $obs = @($e.Observed); $disp = @($e.Display)
        $oc = Get-Cmp $alts $obs; $dc = Get-Cmp $alts $disp
        $other = New-Object System.Collections.ArrayList
        if ($allowed.Count -eq 0) {
          if ($obs.Count -gt 0) { [void]$other.Add('observed values [' + (Format-List2 $obs 12) + '] -> ' + $oc) }
          if ($disp.Count -gt 0) { [void]$other.Add('UI labels [' + (Format-List2 $disp 12) + '] -> ' + $dc) }
          if ($e.Kind -ne 'ui') {                                  # 같은 벤더의 다른 파일에 같은 이름의 레지스트리 허용값이 있으면 참고로 보인다.
            $sibLists = [ordered]@{}
            foreach ($s in @($tbl | Where-Object { $_.VKey -eq $row.VKey -and $_.Rel -ne $target.Rel } | ForEach-Object { $_.Rel } | Select-Object -Unique)) {
              $si = Get-AttrIndex $s
              if ($null -ne $si -and $si.ContainsKey($e.Name) -and @($si[$e.Name].Allowed).Count -gt 0) {
                $lt = Format-List2 $si[$e.Name].Allowed 8
                if ($sibLists.Contains($lt)) { $sibLists[$lt] += ', ' + $s } else { $sibLists[$lt] = $s }
                $sibLists['cmp:' + $lt] = Get-Cmp $alts @($si[$e.Name].Allowed)
              }
            }
            foreach ($lt in @($sibLists.Keys | Where-Object { -not $_.StartsWith('cmp:') })) { [void]$other.Add('same attribute has registry list [' + $lt + '] in ' + $sibLists[$lt] + ' -> ' + $sibLists['cmp:' + $lt]) }
          }
        }
        if ($e.Kind -eq 'ui') {
          $res.Verdict = 'UI_NAME_ONLY'
          [void]$notes.Add('Supermicro UI setup name from the manual, not a Redfish AttributeName; verify the real name in the BMC registry')
          if ($allowed.Count -gt 0) {
            $c = Get-Cmp $alts $allowed
            [void]$notes.Add('UI option list vs expected value -> ' + $c)
          } else { [void]$notes.Add('no UI option list in data') }
        } elseif ($allowed.Count -eq 0) {
          $res.Verdict = 'NO_ALLOWED_VALUES'
          if ($oc -ne 'none') { $res.Evidence = 'observed-' + $oc } elseif ($dc -ne 'none') { $res.Evidence = 'ui-' + $dc } else { $res.Evidence = 'no-evidence' }
          if ($oc -eq 'similar') { [void]$notes.Add('similar observed value: ' + ((@(Get-Similar $alts $obs)) -join ', ')) }
          if ($oc -eq 'absent') { [void]$notes.Add('observed values are current states of sample devices; their absence does not prove a spelling problem') }
        } else {
          $hits = @($alts | Where-Object { Test-MatchAny $allowed $_ })
          if ($hits.Count -gt 0) {
            $res.Verdict = 'OK'
            if ($hits.Count -lt $alts.Count) {
              $res.Partial = $true
              $miss = @($alts | Where-Object { -not (Test-MatchAny $allowed $_) })
              [void]$notes.Add('alternatives not in data allowed list: ' + ($miss -join ', ') + ' (matching alternative: ' + ($hits -join ', ') + ')')
            }
          } else {
            $res.Verdict = 'VALUE_NOT_ALLOWED'
            $cis = @($alts | Where-Object { $a = $_; @($allowed | Where-Object { $_ -ieq $a }).Count -gt 0 })
            if ($cis.Count -gt 0) { $res.Evidence = 'case-only'; [void]$notes.Add('differs only in case from an allowed value (biostool compares case-sensitively)') } else { $res.Evidence = 'no-match' }
            $sim = @(Get-Similar $alts $allowed)
            if ($sim.Count -gt 0) { [void]$notes.Add('similar allowed value: ' + ($sim -join ', ')) }
          }
        }
        $res.Other = ($other -join '; ')
      }
    }
  }
  $res.Note = ($notes -join '; ')
  return [pscustomobject]$res
}

$results = New-Object System.Collections.ArrayList
foreach ($row in $rows) {
  foreach ($tg in (Get-Targets $row)) {
    $r = Test-Row $row $tg
    if ($row.MKey -eq '*' -and $r.Source) { $r.Model = '* -> ' + $r.Source }
    [void]$results.Add($r)
  }
}

# ---- (c) 모델 정규화 충돌 점검 (정보) ----------------------------------------------------------
$infos = New-Object System.Collections.ArrayList
foreach ($g in ($rows | Group-Object VKey)) {
  $named = @($g.Group | Where-Object { $_.MKey -ne '*' })
  $byKey = @($named | Group-Object MKey)
  foreach ($k in $byKey) {
    $strs = @($k.Group | ForEach-Object { $_.Model } | Select-Object -Unique)
    if ($strs.Count -gt 1) { [void]$infos.Add([pscustomobject]@{ Vendor = $g.Group[0].Vendor; Kind = 'same modelKey'; Detail = ("modelKey '{0}' is produced by different model strings: {1}" -f $k.Name, (($strs | ForEach-Object { "'" + $_ + "'" }) -join ', ')) }) }
  }
  foreach ($a in $byKey) {
    foreach ($b in $byKey) {
      if ($a.Name -ne $b.Name -and $b.Name.StartsWith($a.Name, [StringComparison]::Ordinal)) {
        $am = (@($a.Group | ForEach-Object { $_.Model } | Select-Object -Unique) -join ', ')
        $bm = (@($b.Group | ForEach-Object { $_.Model } | Select-Object -Unique) -join ', ')
        [void]$infos.Add([pscustomobject]@{ Vendor = $g.Group[0].Vendor; Kind = 'prefix'; Detail = ("modelKey '{0}' ({1}) is a prefix of '{2}' ({3}); matching is exact, so this is informational" -f $a.Name, $am, $b.Name, $bm) })
      }
    }
  }
}

# ---- 출력 -------------------------------------------------------------------------------------
$verdictOrder = @('OK', 'VALUE_NOT_ALLOWED', 'NO_ALLOWED_VALUES', 'ATTR_NOT_IN_DATA', 'UI_NAME_ONLY', 'NO_DATA_FILE')
$legend = [ordered]@{
  OK                = 'attribute exists in data and at least one expected alternative is in its allowed list (case-sensitive)'
  VALUE_NOT_ALLOWED = 'attribute exists, allowed list known, no expected alternative is in it'
  NO_ALLOWED_VALUES = 'attribute exists but data has no registry allowed list, so the value cannot be confirmed (observed/UI values shown as evidence only)'
  ATTR_NOT_IN_DATA  = 'attribute name not found in the data file'
  UI_NAME_ONLY      = 'Supermicro: name matches a BIOS setup UI name, not a confirmed Redfish AttributeName'
  NO_DATA_FILE      = 'no data file could be linked (model not in the $models table, or file missing)'
}
function MdCell([string]$s) { if ($null -eq $s) { return '' }; return (($s -replace '\|', '\|') -replace "[\r\n]+", ' ') }
function MdCode([string]$s) { if ([string]::IsNullOrEmpty($s)) { return '' }; return ('`' + (MdCell $s) + '`') }
function Count-Of($list, [string]$verdict) { return @($list | Where-Object { $_.Verdict -eq $verdict }).Count }

$total = $results.Count
$nonOk = @($results | Where-Object { $_.Verdict -ne 'OK' })
$fixes = @($results | Where-Object { $_.Verdict -in 'VALUE_NOT_ALLOWED', 'ATTR_NOT_IN_DATA' -or $_.Caution -or ($_.Verdict -eq 'NO_ALLOWED_VALUES' -and $_.Evidence -in 'observed-case-only', 'observed-similar') })
$cautionCount = @($results | Where-Object { $_.Caution }).Count
$vendorGroups = @($results | Group-Object VKey)

# 콘솔 출력.
Write-Host ("Profile : {0}" -f $profPath)
Write-Host ("Syntax  : {0} data rows parsed, {1} error(s)" -f $rows.Count, $synErrors.Count)
foreach ($e in $synErrors) { Write-Host ("  line {0}: {1}" -f $e.Line, $e.Msg) }
Write-Host ("Verdicts: " + (($verdictOrder | ForEach-Object { '{0}={1}' -f $_, (Count-Of $results $_) }) -join '  ') + ("  (total {0})" -f $total))
Write-Host ("Rows with a data caution (PLATFORM:/EXPOSURE: notes): {0}" -f $cautionCount)
Write-Host ''
if ($results.Count -gt 0) {
  ($results | Select-Object Line, Vendor, Model, Std, Attr, Value, Verdict, Evidence | Format-Table -AutoSize -Wrap | Out-String -Width 250).TrimEnd() | Write-Host
}
foreach ($i in $infos) { Write-Host ("[info] {0}: {1}" -f $i.Vendor, $i.Detail) }

# 마크다운 보고서.
if ($Out) {
  $md = New-Object System.Text.StringBuilder
  function L([string]$s = '') { [void]$script:md.Append($s + "`n") }
  L '# Profile validation report'
  L ''
  $repoRoot = Split-Path $dataRoot -Parent
  $profShown = if ($profPath.StartsWith($repoRoot, [StringComparison]::OrdinalIgnoreCase)) { $profPath.Substring($repoRoot.Length).TrimStart('\') -replace '\\', '/' } else { $profPath }
  L ('- Profile: `' + $profShown + '`')
  L ('- Data: `bios_json/` (model -> file mapping read from `map_bios_attrs.ps1` `$models`)')
  L ('- Generated by `validate_profile.ps1` on ' + (Get-Date -Format 'yyyy-MM-dd'))
  L '- Offline check only: no device was queried. A passing row means "consistent with the research data", not "verified on hardware".'
  L ''
  L '## 1. Syntax check (profile.go parseProfile rules)'
  L ''
  if ($synErrors.Count -eq 0) { L ('No errors. {0} data rows parsed.' -f $rows.Count) }
  else { L '| Line | Error |'; L '|---|---|'; foreach ($e in $synErrors) { L ('| {0} | {1} |' -f $e.Line, (MdCell $e.Msg)) } }
  L ''
  L '## 2. Summary'
  L ''
  L ('Rows checked: {0} (rows with syntax errors are skipped).' -f $total)
  L ''
  L '| Verdict | Count | Meaning |'
  L '|---|---:|---|'
  foreach ($v in $verdictOrder) { L ('| `{0}` | {1} | {2} |' -f $v, (Count-Of $results $v), (MdCell $legend[$v])) }
  L ''
  $okPartial = @($results | Where-Object { $_.Verdict -eq 'OK' -and $_.Partial }).Count
  if ($okPartial -gt 0) { L ('`OK` rows where some `A|B` alternatives are not in the data list: {0} (see section 4).' -f $okPartial); L '' }
  L ('Rows with a data caution, any verdict: {0}. `PLATFORM:` = the attribute is evidenced only for another platform than the model; `EXPOSURE:` = the registry entry is not exposed in the Bios resource on any captured model. Listed in section 5.' -f $cautionCount)
  L ''
  L 'By vendor:'
  L ''
  L ('| Vendor | ' + ($verdictOrder -join ' | ') + ' | Total |')
  L ('|---|' + (($verdictOrder | ForEach-Object { '---:' }) -join '|') + '|---:|')
  foreach ($g in $vendorGroups) {
    $cells = ($verdictOrder | ForEach-Object { Count-Of $g.Group $_ }) -join ' | '
    L ('| {0} | {1} | {2} |' -f (MdCell $g.Group[0].Vendor), $cells, $g.Count)
  }
  $nav = @($results | Where-Object { $_.Verdict -eq 'NO_ALLOWED_VALUES' })
  if ($nav.Count -gt 0) {
    L ''
    L '`NO_ALLOWED_VALUES` rows by the strongest non-registry evidence in the data (observed = values seen on real/sample devices, ui = display labels from guides):'
    L ''
    L '| Evidence | Count |'
    L '|---|---:|'
    foreach ($gg in ($nav | Group-Object Evidence | Sort-Object Name)) { L ('| `{0}` | {1} |' -f $gg.Name, $gg.Count) }
  }
  L ''
  L '## 3. Rows that are not OK (grouped by vendor)'
  L ''
  if ($nonOk.Count -eq 0) { L 'None.'; L '' }
  foreach ($g in $vendorGroups) {
    $sub = @($g.Group | Where-Object { $_.Verdict -ne 'OK' })
    if ($sub.Count -eq 0) { continue }
    $sub = @($sub | Sort-Object @{ Expression = { [array]::IndexOf($verdictOrder, $_.Verdict) } }, Line)
    L ('### {0} ({1} rows)' -f (MdCell $g.Group[0].Vendor), $sub.Count)
    L ''
    L '| Line | Model | std_name | Attribute | Expected | Verdict | Allowed values in data | Evidence / notes | Source |'
    L '|---:|---|---|---|---|---|---|---|---|'
    foreach ($r in $sub) {
      $ev = (@($r.Other, $r.Note) | Where-Object { $_ }) -join '; '
      L ('| {0} | {1} | {2} | {3} | {4} | `{5}` | {6} | {7} | {8} |' -f $r.Line, (MdCell $r.Model), $r.Std, (MdCode $r.Attr), (MdCode $r.Value), $r.Verdict, (MdCell $r.AllowedText), (MdCell $ev), (MdCell $r.Source))
    }
    L ''
  }
  L '## 4. OK rows with remarks'
  L ''
  $okNotes = @($results | Where-Object { $_.Verdict -eq 'OK' -and $_.Note })
  if ($okNotes.Count -eq 0) { L 'None.'; L '' }
  else {
    L '| Vendor | Line | Model | std_name | Attribute | Expected | Allowed values in data | Remark | Source |'
    L '|---|---:|---|---|---|---|---|---|---|'
    foreach ($r in $okNotes) {
      L ('| {0} | {1} | {2} | {3} | {4} | {5} | {6} | {7} | {8} |' -f (MdCell $r.Vendor), $r.Line, (MdCell $r.Model), $r.Std, (MdCode $r.Attr), (MdCode $r.Value), (MdCell $r.AllowedText), (MdCell $r.Note), (MdCell $r.Source))
    }
    L ''
  }
  L '## 5. Rows that look like they need a profile edit'
  L ''
  L 'Subset of section 3: `VALUE_NOT_ALLOWED`, `ATTR_NOT_IN_DATA`, and `NO_ALLOWED_VALUES` rows whose observed values differ from the expected value only by case or by a near spelling (for example Enable vs Enabled), plus rows flagged `PLATFORM:` or `EXPOSURE:` (see section 2). Observed values come from sample devices and are not an allowed list, so confirm on a live host before editing.'
  L ''
  if ($fixes.Count -eq 0) { L 'None.'; L '' }
  else {
    L '| Vendor | Line | Model | std_name | Attribute | Expected | Verdict | Evidence / notes | Source |'
    L '|---|---:|---|---|---|---|---|---|---|'
    foreach ($r in $fixes) {
      $ev = (@($r.AllowedText, $r.Other, $r.Note) | Where-Object { $_ -and $_ -ne '(none)' }) -join '; '
      L ('| {0} | {1} | {2} | {3} | {4} | {5} | `{6}` | {7} | {8} |' -f (MdCell $r.Vendor), $r.Line, (MdCell $r.Model), $r.Std, (MdCode $r.Attr), (MdCode $r.Value), $r.Verdict, (MdCell $ev), (MdCell $r.Source))
    }
    L ''
  }
  L '## 6. Model normalization (informational)'
  L ''
  L 'Matching in biostool is exact on `normalizeModel(model)`, so these are not conflicts; they are listed so a human can confirm the model strings are intended.'
  L ''
  if ($infos.Count -eq 0) { L 'None.'; L '' }
  else {
    L '| Vendor | Kind | Detail |'
    L '|---|---|---|'
    foreach ($i in $infos) { L ('| {0} | {1} | {2} |' -f (MdCell $i.Vendor), $i.Kind, (MdCell $i.Detail)) }
    L ''
  }
  $outPath = $ExecutionContext.SessionState.Path.GetUnresolvedProviderPathFromPSPath($Out)
  [IO.File]::WriteAllText($outPath, $md.ToString(), (New-Object System.Text.UTF8Encoding($false)))
  Write-Host ("report  : {0}" -f $outPath)
}
if ($synErrors.Count -gt 0) { exit 1 }
