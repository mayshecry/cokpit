# Regenerates cockpit-standalone.html from index.html + styles.css + js/*.js
# (pure string concatenation — mirrors frontend/build.py without needing Python)
$ErrorActionPreference = 'Stop'
$here = (Get-Location).Path
$htmlPath = Join-Path $here 'frontend\index.html'
$cssPath  = Join-Path $here 'frontend\styles.css'
$outPath  = Join-Path $here 'frontend\cockpit-standalone.html'

$html = [System.IO.File]::ReadAllText($htmlPath, [System.Text.Encoding]::UTF8)
$css  = [System.IO.File]::ReadAllText($cssPath, [System.Text.Encoding]::UTF8)

$names  = @('core','notifications','auth','orders','detail','checklists','manuals','si','users','config','app','order-pages')
$jsParts = @()
foreach ($n in $names) {
  $p = Join-Path $here ("frontend\js\" + $n + ".js")
  $jsParts += [System.IO.File]::ReadAllText($p, [System.Text.Encoding]::UTF8)
}
$js = $jsParts -join "`n`n"

$html = $html.Replace('<link rel="stylesheet" href="styles.css">',
  '<style>' + "`n" + $css + "`n" + '</style>')

$html = $html.Replace('<script src="https://cdnjs.cloudflare.com/ajax/libs/xlsx/0.18.5/xlsx.full.min.js"></script>', '')

$scriptTags = @'
  <script src="js/core.js"></script>
  <script src="js/notifications.js"></script>
  <script src="js/auth.js"></script>
  <script src="js/orders.js"></script>
  <script src="js/detail.js"></script>
  <script src="js/checklists.js"></script>
  <script src="js/manuals.js"></script>
  <script src="js/si.js"></script>
  <script src="js/users.js"></script>
  <script src="js/config.js"></script>
  <script src="js/app.js"></script>
  <script src="js/order-pages.js"></script>
'@ -replace "`r`n", "`n"

$html = $html.Replace($scriptTags, '<script>' + "`n" + $js + "`n" + '</script>')

[System.IO.File]::WriteAllText($outPath, $html, (New-Object System.Text.UTF8Encoding($false)))
Write-Host ("wrote {0} ({1} bytes)" -f $outPath, $html.Length)