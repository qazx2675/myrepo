# Standard BIOS name -> vendor attribute mapping, selected by Jev (TypeSafe System One).
# Design: code builds candidate attributes (regex over-find), Jev "choice" picks one (or none),
# then a second dependent request picks the target value among that attribute's allowed values.
param([string]$OutDir = $PSScriptRoot, [string]$Only = '')
$ErrorActionPreference = 'Stop'
$root = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$keyLine = (Get-Content "$HOME\.jev-claude.env" | Where-Object { $_ -match '^JEV_API_KEY=' } | Select-Object -First 1)
$key = ($keyLine -replace '^JEV_API_KEY=', '').Trim('"', "'", ' ')
$cache = Join-Path $env:TEMP 'jev_std_map_cache.json'
$script:Cache = @{}
if (Test-Path $cache) { (Get-Content $cache -Raw | ConvertFrom-Json).PSObject.Properties | ForEach-Object { $script:Cache[$_.Name] = $_.Value } }

function Jev($state, $questions) {
  $body = @{ model = 'jev-latest'; state = $state; questions = $questions } | ConvertTo-Json -Depth 8 -Compress
  $ck = [BitConverter]::ToString([Security.Cryptography.SHA1]::Create().ComputeHash([Text.Encoding]::UTF8.GetBytes($body))) -replace '-', ''
  if ($script:Cache.ContainsKey($ck)) { return $script:Cache[$ck] }
  for ($i = 0; $i -lt 3; $i++) {
    try {
      $r = Invoke-RestMethod -Method Post -Uri 'https://api.typesafe.ai/v1/systemone' -Headers @{ Authorization = "Bearer $key" } -ContentType 'application/json; charset=utf-8' -Body ([Text.Encoding]::UTF8.GetBytes($body)) -TimeoutSec 90
      $script:Cache[$ck] = $r.answers
      $script:Cache | ConvertTo-Json -Depth 10 | Set-Content $cache -Encoding UTF8
      return $r.answers
    } catch { Start-Sleep -Seconds (2 * ($i + 1)); $err = $_ }
  }
  throw "Jev call failed: $($err.Exception.Message)"
}

function Arr($v) { if ($null -eq $v) { @() } else { @($v) } }
function Flatten($vendor, $file) {
  $j = Get-Content $file -Raw -Encoding UTF8 | ConvertFrom-Json
  $out = New-Object System.Collections.ArrayList
  function Add($n, $d, $av, $avd, $def) { if ($n) { [void]$out.Add([pscustomobject]@{ name = [string]$n; display = [string]$d; allowed = @(Arr $av); allowed_display = @(Arr $avd); default = $def; src = 'registry' }) } }
  $a = $j.attributes
  if ($null -ne $a -and $a -isnot [array] -and $a.PSObject.Properties.Count -gt 0) {            # HPE dict
    foreach ($p in $a.PSObject.Properties) { Add $p.Name $p.Value.title $p.Value.allowed_values $null $p.Value.default }
  } elseif ($null -ne $a) {
    foreach ($e in (Arr $a)) {
      if ($e.PSObject.Properties.Name -contains 'name') { $ov = @($e.observed | ForEach-Object { $_.value } | Where-Object { $_ } | Select-Object -Unique); $avx = if ($e.allowed_values) { $e.allowed_values } else { $ov }; Add $e.name $e.display_name $avx ($(if ($e.allowed_value_display) { $e.allowed_value_display } else { $e.allowed_values_display })) $e.default; if (-not $e.allowed_values) { $out[$out.Count-1].allowed_display = @(); if ($ov.Count -gt 0) { $out[$out.Count-1].src = 'observed' } }; if (-not $e.display_name -and $e.title) { $out[$out.Count-1].display = [string]$e.title } }
      elseif ($e.PSObject.Properties.Name -contains 'attribute') { Add $e.attribute $e.python_name $e.allowed_values $null $e.default }            # Intersight
      elseif ($e.PSObject.Properties.Name -contains 'ui_name') { $n = if ($e.attribute_id) { $e.attribute_id } else { $e.ui_name }; Add $n $e.ui_name $e.allowed_values $null $e.default }   # Supermicro
    }
  }
  foreach ($tc in (Arr $j.token_classes)) {                                                       # Cisco UCSM / CIMC
    foreach ($t in (Arr $tc.tokens)) {
      $av = $t.allowed_values
      if (-not $av -and $t.platforms) { $pp = $t.platforms.PSObject.Properties | Select-Object -First 1; if ($pp) { $av = $pp.Value.allowed_values } }
      Add $t.attribute $tc.class $av $null $t.default
    }
  }
  return $out
}

$std = [ordered]@{
  system_profile    = @{ re = 'sysprofile|system.?profile|workload|performance.?profile|power.?profile|operating.?mode|power.?(mode|policy|management|technology)|energy.?(perf|efficien)|cpu.?performance|cpu.?power|efficiency.?mode|profile'
                         ask = 'Which attribute is the top-level system performance / workload / power profile selector (the single setting that applies a preset such as Performance, High Performance Compute, Maximum Performance or Efficiency)? Not individual power-saving knobs.';
                         val = 'Which allowed value is the high-performance / HPC / maximum-performance preset (favoring performance and low latency over power saving)?' }
  hyper_threading   = @{ intent = 'Enabled'; re = 'hyper.?thread|logical.?proc|(^|[^a-z])smt([^a-z]|$)|smt.?(mode|control)|simultaneous.?multi|ht.?support|(^|[^a-z])ht([^a-z]|$)|cpu.?ht'
                         ask = 'Which attribute turns processor Hyper-Threading / Simultaneous Multithreading (logical processors per core) on or off?';
                         val = 'Which allowed value means Hyper-Threading / SMT / logical processors is ENABLED?' }
  llc_prefetch      = @{ intent = 'Enabled'; re = 'llc|last.?level|prefetch'
                         ask = 'Which attribute enables or disables the Last Level Cache (LLC) prefetch feature specifically (not L1/L2, DCU, adjacent-line or stream hardware prefetchers)?';
                         val = 'Which allowed value means LLC prefetch is ENABLED?' }
  sub_numa_cluster  = @{ intent = 'Disabled'; re = 'sub.?numa|(^|[^a-z])snc([^a-z]|$)|numa.?cluster|cluster.?on.?die|(^|[^a-z])cod([^a-z]|$)|(^|[^a-z])nps([^a-z]|$)|nodes?.?per.?socket|numa.?(per|node)'
                         ask = 'Which attribute controls Sub-NUMA Clustering (SNC) of the processor (on AMD the closest equivalent is NPS - NUMA nodes per socket)?';
                         val = 'Which allowed value means Sub-NUMA Clustering is DISABLED (single NUMA domain per socket; for AMD NPS use NPS1)?' }
}

$files = Get-ChildItem $root -Recurse -Filter 'bios_attributes*.json' | Where-Object { $_.FullName -notmatch '\\back\\' }
$results = New-Object System.Collections.ArrayList
foreach ($f in $files) {
  $rel = $f.FullName.Substring($root.Length + 1)
  $parts = $rel.Split('\'); $vendor = $parts[0]; $ver = $parts[1]; $fname = $parts[-1]
  if ($Only -and $rel -notlike "*$Only*") { continue }
  $cands = Flatten $vendor $f.FullName
  Write-Host ("{0}: {1} attrs" -f $rel, $cands.Count)
  foreach ($k in $std.Keys) {
    $pool = @($cands | Where-Object { ($_.name + ' ' + $_.display) -match $std[$k].re } | Sort-Object name -Unique | Select-Object -First 120)
    $row = [ordered]@{ vendor = $vendor; mgmt_version = $ver; source = $rel; std_name = $k; candidates = $pool.Count
                       note = $null; val_src = $null; pool = (($pool | Select-Object -First 12 | ForEach-Object { $_.name }) -join ','); AttributeName = $null; DisplayName = $null; ValueName = $null; ValueDisplayName = $null; attr_conf = $null; val_conf = $null; status = 'no_candidates' }
    if ($pool.Count -gt 0) {
      $crit = [ordered]@{}
      foreach ($c in $pool) { $d = if ($c.display) { $c.display } else { ($c.name -creplace '([a-z0-9])([A-Z])', '$1 $2') -replace '_', ' ' }; $al = ($c.allowed | Select-Object -First 8) -join '/'; $crit[$c.name] = (("{0} | values: {1}" -f $d, $al).Trim()) }
      $crit['__none__'] = 'None of these attributes is the requested setting.'
      $state = @{ vendor = $vendor; management_controller_version = $ver; task = 'Map a standard BIOS setting to this vendor BIOS attribute name' }
      $ans = Jev $state @{ attr = @{ type = 'choice'; instructions = $std[$k].ask; criteria = $crit } }
      $pick = $ans.attr.choice; $row.attr_conf = [math]::Round([double]$ans.attr.confidence, 2)
      if ($pick -and $pick -ne '__none__') {
        $c = $pool | Where-Object { $_.name -eq $pick } | Select-Object -First 1
        $row.AttributeName = $c.name; $row.DisplayName = $c.display; $row.val_src = $c.src; $row.status = 'attr_selected'
        if (@($c.allowed).Count -gt 0) {
          $vc = [ordered]@{}
          for ($i = 0; $i -lt @($c.allowed).Count; $i++) { $label = if (@($c.allowed_display).Count -gt $i) { $c.allowed_display[$i] } else { '' }; $vc[[string]$c.allowed[$i]] = "$label" }
          $vc['__none__'] = 'None of these values fits.'
          $s2 = @{ vendor = $vendor; management_controller_version = $ver; attribute = $c.name; attribute_display_name = $c.display }
          $a2 = Jev $s2 @{ val = @{ type = 'choice'; instructions = $std[$k].val; criteria = $vc } }
          $vp = $a2.val.choice; $row.val_conf = [math]::Round([double]$a2.val.confidence, 2)
          if ($vp -and $vp -ne '__none__') {
            $row.ValueName = $vp
            $ix = [array]::IndexOf(@($c.allowed | ForEach-Object { [string]$_ }), $vp)
            if ($ix -ge 0 -and @($c.allowed_display).Count -gt $ix) { $row.ValueDisplayName = [string]$c.allowed_display[$ix] } else { $row.ValueDisplayName = $vp }
            $row.status = 'mapped'
          } elseif ($std[$k].intent -and (@($c.allowed | Where-Object { [string]$_ -ieq $std[$k].intent }).Count -eq 1)) {
            $row.ValueName = [string](@($c.allowed | Where-Object { [string]$_ -ieq $std[$k].intent })[0]); $row.ValueDisplayName = $row.ValueName; $row.status = 'mapped_value_matched'
          } else { $row.status = 'attr_only_value_unresolved' }
        } else {
          $row.status = 'attr_only_no_allowed_values'
          if ($std[$k].intent) { $row.ValueName = $std[$k].intent; $row.ValueDisplayName = $std[$k].intent; $row.status = 'mapped_value_assumed' }
        }
      } else { $row.status = 'none_of_candidates'; if ($k -eq 'sub_numa_cluster') { $nps = @($pool | Where-Object { $_.name -match 'nps|nodes.?per.?socket' } | ForEach-Object { $_.name }); if ($nps) { $row.note = 'AMD analog (NPS): ' + ($nps -join ',') } } }
    }
    [void]$results.Add([pscustomobject]$row)
    Write-Host ("  {0,-17} cand={1,3} -> {2} = {3} [{4}]" -f $k, $pool.Count, $row.AttributeName, $row.ValueName, $row.status)
  }
}






# ---- stage 2: sibling fill, outputs ------------------------------------------------------------
# Same vendor + mgmt version, different generation/platform files: if a row has no usable mapping
# but the attribute chosen by a sibling file exists in this file, reuse the sibling's choice.
$fileText = @{}
foreach ($r in $results) { if (-not $fileText.ContainsKey($r.source)) { $fileText[$r.source] = Get-Content (Join-Path $root $r.source) -Raw -Encoding UTF8 } }
foreach ($r in $results) {
  if ($r.status -in 'none_of_candidates','attr_only_no_allowed_values','attr_only_value_unresolved' -and $r.std_name -ne 'sub_numa_cluster' -and $r.std_name -ne 'llc_prefetch') {
    $sib = $results | Where-Object { $_.vendor -eq $r.vendor -and $_.mgmt_version -eq $r.mgmt_version -and $_.std_name -eq $r.std_name -and $_.source -ne $r.source -and $_.status -eq 'mapped' } | Select-Object -First 1
    if ($sib -and $fileText[$r.source].Contains('"' + $sib.AttributeName + '"')) {
      $r.AttributeName = $sib.AttributeName; $r.DisplayName = $sib.DisplayName; $r.ValueName = $sib.ValueName; $r.ValueDisplayName = $sib.ValueDisplayName
      $r.status = 'mapped_from_sibling'; $r.note = 'value taken from ' + $sib.source
    }
  }
}
# model -> (vendor key used by biostool, mgmt version folder, source file name) ; verified is always N (no real hardware)
$models = @(
 @('HPE','DL360 Gen9','iLO4',''),@('HPE','XL170r Gen9','iLO4',''),@('HPE','XL250a Gen9','iLO4',''),@('HPE','XL270d Gen9','iLO4',''),@('HPE','DL560 Gen8','iLO4',''),
 @('HPE','DL360 Gen10','iLO5',''),@('HPE','DL560 Gen10','iLO5',''),@('HPE','DL580 Gen10','iLO5',''),@('HPE','XL270d Gen10','iLO5',''),@('HPE','DL360 Gen10 Plus','iLO5',''),
 @('HPE','DL360 Gen11','iLO6',''),@('HPE','DL380 Gen11','iLO6',''),@('HPE','DL560 Gen11','iLO6',''),
 @('DELL','R640','iDRAC9','bios_attributes_14g.json'),@('DELL','R740','iDRAC9','bios_attributes_14g.json'),@('DELL','R840','iDRAC9','bios_attributes_14g.json'),@('DELL','C6420','iDRAC9','bios_attributes_14g.json'),@('DELL','DSS8440','iDRAC9','bios_attributes_14g.json'),
 @('DELL','R750','iDRAC9','bios_attributes_15g.json'),@('DELL','R750xa','iDRAC9','bios_attributes_15g.json'),@('DELL','R750xs','iDRAC9','bios_attributes_15g.json'),
 @('DELL','R660','iDRAC9','bios_attributes_16g_intel.json'),@('DELL','R760xa','iDRAC9','bios_attributes_16g_intel.json'),@('DELL','XE9680','iDRAC9','bios_attributes_16g_intel.json'),@('DELL','R860','iDRAC9','bios_attributes_16g_intel.json'),@('DELL','C6620','iDRAC9','bios_attributes_16g_intel.json'),
 @('DELL','R6615','iDRAC9','bios_attributes_16g_amd.json'),
 @('DELL','XE9780','iDRAC10','bios_attributes.json'),
 @('LENOVO','SR630','XCC',''),@('LENOVO','SR650','XCC',''),@('LENOVO','SD530','XCC',''),@('LENOVO','SR630 V2','XCC',''),
 @('LENOVO','SR645 V3','XCC2',''),@('LENOVO','SR675 V3','XCC2',''),@('LENOVO','SD650 V3','XCC2',''),
 @('CISCO','UCS B200 M4','UCSM',''),@('CISCO','UCS B200 M5','UCSM',''),@('CISCO','UCS B480 M5','UCSM',''),@('CISCO','UCS X210c M7','UCSM',''),
 @('SUPERMICRO','AS-1115HS-TNR','X13_H13','')
)
$vdir = @{ HPE = 'HPE'; DELL = 'Dell'; LENOVO = 'Lenovo'; CISCO = 'Cisco'; SUPERMICRO = 'Supermicro' }
$okStatus = 'mapped','mapped_value_matched','mapped_value_assumed','mapped_from_sibling'
$tsv = New-Object System.Collections.ArrayList; $unres = New-Object System.Collections.ArrayList
[void]$tsv.Add("vendor`tmodel`tstd_name`tattribute`tvalue`tverified")
foreach ($m in $models) {
  $folder = $vdir[$m[0]]
  foreach ($k in $std.Keys) {
    $rows = @($results | Where-Object { $_.vendor -eq $folder -and $_.mgmt_version -eq $m[2] -and $_.std_name -eq $k -and ($m[3] -eq '' -or $_.source.EndsWith($m[3])) })
    $r = $rows | Where-Object { $_.status -in $okStatus } | Select-Object -First 1
    if (-not $r) { $r = $rows | Select-Object -First 1 }
    $review = $null
    if ($r -and $r.status -in $okStatus) {
      if ($k -eq 'system_profile' -and $r.val_src -eq 'observed') { $review = 'preset value only from observed samples, not from a registry list' }
    }
    if ($r -and $r.status -in $okStatus -and -not $review) {
      $val = $r.ValueName
      if ($m[0] -eq 'HPE' -and $val -eq 'HighPerformanceCompute(HPC)') { $val = 'HighPerformanceCompute|HighPerformanceCompute(HPC)' }
      [void]$tsv.Add(("{0}`t{1}`t{2}`t{3}`t{4}`tN" -f $m[0], $m[1], $k, $r.AttributeName, $val))
    } else {
      $why = if ($review) { 'REVIEW (' + $review + '; chosen ' + $r.AttributeName + '=' + $r.ValueName + '; candidates ' + $r.pool + ')' } elseif ($r) { $r.status } else { 'no_source_file' }
      [void]$unres.Add(("# UNMAPPED {0} {1} {2}: {3}" -f $m[0], $m[1], $k, $why))
    }
  }
}
$prof = Join-Path (Resolve-Path (Join-Path $root '..\profiles')).Path 'VM_research.tsv'
$hdr = @('# Generated by bios_json/standard_map/map_bios_attrs.ps1 from bios_json/*/ (Jev choice over code-built candidates).', '# verified=N for every row: no real hardware was queried. Confirm on a first live host (FIRST_RUN.md) before marking Y.')
(($hdr + $tsv + $unres) -join "`n") + "`n" | Set-Content $prof -Encoding UTF8
$results | ConvertTo-Json -Depth 5 | Set-Content (Join-Path $OutDir 'standard_bios_map.json') -Encoding UTF8
foreach ($r in $results) { $lc = ($r.status -in $okStatus) -and ($r.status -ne 'mapped_from_sibling') -and ([double]$r.attr_conf -lt 0.30); $r | Add-Member -NotePropertyName low_attr_conf -NotePropertyValue $lc -Force }
$results | ConvertTo-Json -Depth 5 | Set-Content (Join-Path $OutDir 'standard_bios_map.json') -Encoding UTF8
$flat = $results | Select-Object vendor, mgmt_version, std_name, low_attr_conf, DisplayName, ValueDisplayName, AttributeName, ValueName, status, attr_conf, val_conf, candidates, note, source
$flat | Export-Csv (Join-Path $OutDir 'standard_bios_map.csv') -NoTypeInformation -Encoding UTF8
Write-Host "profile: $prof ($($tsv.Count - 1) rows, $($unres.Count) unmapped)"


