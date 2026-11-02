# =============================================================================
#  dashboard-genereer.ps1 - maakt elke ~2 s het laptop-voortgangsdashboard (statische HTML, door pxesrv geserveerd):
#     dashboard.html            overzicht van alle laptops (status, voortgang, verstreken tijd, verwachte eindtijd)
#     laptop-<serie>.html       detail: stappenplan (klaar/bezig/nog te doen + duur), gebeurtenissen, eerdere runs
#  Leest <utilities>\status\<serie>.txt (door audit-update.ps1 geschreven, sleutel=waarde) en BEWAART de geschiedenis:
#     <status>\historie\<serie>.log    gebeurtenissen van de lopende run (server-tijd)
#     <status>\historie\<serie>.meta   run-sleutel, huidige stap, stap-starttijden en -duren
#     <status>\runs\<serie>__<run>.txt gearchiveerde afgeronde run (meta + alle gebeurtenissen)
#  De server is de enige schrijver van historie\ en runs\ (de laptops mogen alleen hun eigen <serie>.txt in status\ schrijven).
#  Stoppen: gate-state\dashboard-stop.flag aanmaken. Pad-override: gate-state\dashboard-pad.txt (map met de status-bestanden).
# =============================================================================
$ErrorActionPreference = 'Continue'
$ScriptFolder = Split-Path -Parent $MyInvocation.MyCommand.Path
$StateDir = Join-Path (Split-Path -Parent $ScriptFolder) 'gate-state'
$StopFlag = Join-Path $StateDir 'dashboard-stop.flag'
$StatusDir = Join-Path (Split-Path -Parent (Split-Path -Parent (Split-Path -Parent $ScriptFolder))) 'share\utilities\status'
$ov = Join-Path $StateDir 'dashboard-pad.txt'
if (Test-Path $ov) { $p = (Get-Content $ov -Raw).Trim(); if ($p) { $StatusDir = $p } }
$HistDir = Join-Path $StatusDir 'historie'; $RunsDir = Join-Path $StatusDir 'runs'
$utf8 = New-Object System.Text.UTF8Encoding($false)

# ---- stappenplan: id, naam, standaardduur (min, uit de echte Surface-run van 2 okt 2026: totaal 1u38m), optioneel? ----
$Stappen = @(
  @{ id='start';   naam='1. Voorbereiding: netwerk en server controleren';                 def=4;  opt=$false },
  @{ id='wsus';    naam='2. Windows-updates installeren vanaf de server (kan lang duren)'; def=5; opt=$false },
  @{ id='oem';     naam='3. Drivers en firmware van de fabrikant installeren';     def=12; opt=$false },
  @{ id='wu';      naam='4. Controleren of er nog Windows-updates ontbreken';                          def=50; opt=$false },
  @{ id='feature'; naam='5. Grote Windows-upgrade (alleen als nodig, bv. 25H2 naar 26H2)'; def=25; opt=$true },
  @{ id='store';   naam='6. Microsoft Store-apps bijwerken (alleen als aangezet)';        def=8;  opt=$true },
  @{ id='software';naam='7. Extra software installeren (alleen als aangezet)';def=10; opt=$true },
  @{ id='branding';naam='8. Personalisatie, logo en achtergrond (alleen als aangezet)';              def=3;  opt=$true },
  @{ id='sysprep'; naam='9. Opruimen en Windows klaarzetten voor de klant (Sysprep)';                      def=6;  opt=$false }
)
function Get-StapId([string]$tekst) {
  $t = $tekst.ToLower()
  # 7 okt 2026: WSUS Offline schrijft eigen regels met tijdstempel ('wo 07-10-2026 19:07:23,62 - Info: ...', ook 'Started service Windows Update'). Die horen bij de WSUS-stap en werden aangezien voor 'Windows-updates (online scan)', waardoor het stappenplan te vroeg doorsprong (WSUS toonde 33 s terwijl het ~10 min duurde).
  if ($t -match '\d{1,2}:\d\d:\d\d,\d+ - (info|warning|error|failure):') { return 'wsus' }   # 8 okt 2026: uur kan 1 cijfer zijn (' 1:12:15,71') - met \d\d werden WSUS-regels tussen 00:00 en 09:59 aangezien voor de online scan
  # 9 okt 2026: 'WSUS Offline Update klaar (exitcode 0)' (los woord 'klaar') werd aangezien voor de eindstap: dashboard toonde Opruimen terwijl de laptop nog Lenovo-updates deed. Echte klaar-meldingen lopen via klaar=ja en 'KLAAR - Sysprep'.
  if ($t -match 'sysprep|klaar voor klant|^opruimen|windows klaarzetten') { return 'sysprep' }
  if ($t -match 'branding|personalisatie') { return 'branding' }
  if ($t -match 'software') { return 'software' }
  if ($t -match 'store') { return 'store' }
  if ($t -match 'grote versie|feature') { return 'feature' }
  if ($t -match 'wsus offline|wsus-offline|dosupdate|doupdate') { return 'wsus' }   # echte volgorde in het script: WSUS Offline -> fabrikant -> online Windows Update
  if ($t -match 'surface|lenovo|hp image|dell command|fabrikant|\boem\b|hpia|thininstaller|firmware|bios') { return 'oem' }
  if ($t -match 'windows update|wsus|usoclient|\buso\b|cumulatieve') { return 'wu' }
  return 'start'
}
function Get-Locatie([string]$serie) {
  # locaties.txt in de statusmap: een regel per laptop "serienummer=plek" (bv. 033135623657=Stelling A, niveau 2). Alleen voor het dashboard; raakt de laptop-flow niet aan.
  try {
    $f = Join-Path $StatusDir 'locaties.txt'
    if (Test-Path $f) { foreach ($r in (Get-Content $f -Encoding UTF8 -ErrorAction SilentlyContinue)) { if ($r -match '^\s*([^=#]+?)\s*=\s*(.+?)\s*$' -and $Matches[1].Trim() -eq $serie) { return $Matches[2].Trim() } } }
  } catch {}
  try { $au = Lees-AutoPlekken; if ($au.Contains($serie)) { return $au[$serie].plek } } catch {}   # 19 okt 2026: automatisch toegewezen plek (handmatig gaat voor)
  return ''
}
function Lees-AutoPlekken {
  # locaties-auto.txt: een regel "sleutel=Tafel T, plek P|ISO-tijd laatst gezien" (sleutel = serienummer, of MAC-<12 hex> voor een nog onbekend apparaat). Alleen voor het dashboard. 1,5 s gecachet.
  if ($Script:AutoCache -and ((Get-Date) - $Script:AutoCache.t).TotalSeconds -lt 1.5) { return $Script:AutoCache.h }
  $h = [ordered]@{}
  try { $f = Join-Path $StatusDir 'locaties-auto.txt'; if (Test-Path -LiteralPath $f) { foreach ($r in (Get-Content -LiteralPath $f -Encoding UTF8 -ErrorAction SilentlyContinue)) { if ($r -match '^\s*([^=#]+?)\s*=\s*([^|]+?)\s*\|\s*(\S+)\s*$') { $t = [datetime]::MinValue; try { $t = [datetime]::Parse($Matches[3], [Globalization.CultureInfo]::InvariantCulture, [Globalization.DateTimeStyles]::RoundtripKind) } catch {}; $h[$Matches[1].Trim()] = @{ plek = $Matches[2].Trim(); gezien = $t } } } } } catch {}
  $Script:AutoCache = @{ t = (Get-Date); h = $h }
  return $h
}
function Schrijf-AutoPlekken($h) {
  try { $f = Join-Path $StatusDir 'locaties-auto.txt'; $regels = @(foreach ($k in $h.Keys) { "{0}={1}|{2}" -f $k, $h[$k].plek, $h[$k].gezien.ToString('o') }); [IO.File]::WriteAllText($f, (($regels -join "`r`n") + $(if ($regels.Count) { "`r`n" } else { '' })), $utf8) } catch {}
  $Script:AutoCache = $null
}
function Zorg-Plek([string]$sleutel) {
  # 19 okt 2026 (Roderick: 'tot alles vast staat moet een laptop zeker op een plek staan, welke maakt niet uit'): geeft de plek van deze sleutel (serienummer of MAC-sleutel); heeft hij er geen, dan krijgt hij de eerste vrije plek
  # (Tafel 1 plek 1-12, dan Tafel 2) en wordt die onthouden. Handmatige plekken (locaties.txt, script 101) gaan altijd voor en blijven bezet. Een NIEUW serienummer neemt de plek over van PRECIES 1 recent (< 10 min) gezien onbekend apparaat.
  if (-not $sleutel) { return '' }
  try {
    # handmatige plek (locaties.txt, script 101) gaat ALTIJD voor; verder alleen een automatische plek die NIET botst met een handmatige plek van een andere laptop (die later kan zijn gezet)
    $manueel = ''; $handPlekken = @{}
    $fm0 = Join-Path $StatusDir 'locaties.txt'
    if (Test-Path -LiteralPath $fm0) { foreach ($r0 in (Get-Content -LiteralPath $fm0 -Encoding UTF8 -ErrorAction SilentlyContinue)) { if ($r0 -match '^\s*([^=#]+?)\s*=\s*(.+?)\s*$') { $sl0 = $Matches[1].Trim(); $wd0 = $Matches[2].Trim(); if ($sl0 -eq $sleutel) { $manueel = $wd0 } elseif ($wd0 -match '(?i)tafel\s*(\d+).*?plek\s*(\d+)') { $handPlekken["$([int]$Matches[1]),$([int]$Matches[2])"] = 1 } } } }
    if ($manueel -match '(?i)tafel\s*\d+.*?plek\s*\d+') {
      # 29 okt 2026 (kennisbank #1440): de Surface stond 38 min als tweede kaart (plek 2, alleen netwerkadres) naast zijn eigen plek: met een handmatige plek werd het onbekende apparaat nooit overgenomen. Nu wel: PRECIES 1 recent (< 10 min) gezien onbekend apparaat is deze laptop.
      try {
        if ($sleutel -notlike 'MAC-*') {
          $aM = Lees-AutoPlekken; $nuM = Get-Date; $mkM = @($aM.Keys | Where-Object { $_ -like 'MAC-*' -and ($nuM - $aM[$_].gezien).TotalMinutes -lt 10 })
          if ($mkM.Count -eq 1) {
            $macM = ($mkM[0] -replace '^MAC-', ''); $aM.Remove($mkM[0]); Schrijf-AutoPlekken $aM; if ($Script:ArpLaatst -and $Script:ArpLaatst.ContainsKey($mkM[0])) { $Script:ArpLaatst.Remove($mkM[0]) }
            if ($macM -match '^[0-9A-Fa-f]{12}$') { New-Item -ItemType Directory -Path $HistDir -Force -ErrorAction SilentlyContinue | Out-Null; [IO.File]::WriteAllText((Join-Path $HistDir "$sleutel.mac"), (($macM -replace '(..)(?!$)', '$1-').ToUpper()), $utf8) }
          }
        }
      } catch {}
      return $manueel
    }
    $a0 = Lees-AutoPlekken
    if ($a0.Contains($sleutel)) {
      $botst = $false; if ($a0[$sleutel].plek -match '(?i)tafel\s*(\d+).*?plek\s*(\d+)') { $botst = $handPlekken.ContainsKey("$([int]$Matches[1]),$([int]$Matches[2])") }
      if (-not $botst) {
        try { if (((Get-Date) - $a0[$sleutel].gezien).TotalMinutes -gt 10) { $a0[$sleutel].gezien = Get-Date; Schrijf-AutoPlekken $a0 } } catch {}
        return $a0[$sleutel].plek
      }
      $a0.Remove($sleutel); Schrijf-AutoPlekken $a0   # botst met een later handmatig gezette plek van een andere laptop: opnieuw toewijzen
    }
    $nu2 = Get-Date; $a = Lees-AutoPlekken
    foreach ($k in @($a.Keys)) { if (($nu2 - $a[$k].gezien).TotalHours -gt 12) { $a.Remove($k) } }
    if ($sleutel -notlike 'MAC-*') {
      $mk = @($a.Keys | Where-Object { $_ -like 'MAC-*' -and ($nu2 - $a[$_].gezien).TotalMinutes -lt 10 })
      if ($mk.Count -eq 1) {
        $plek = $a[$mk[0]].plek; $mac = ($mk[0] -replace '^MAC-', ''); $a.Remove($mk[0]); $a[$sleutel] = @{ plek = $plek; gezien = $nu2 }; Schrijf-AutoPlekken $a; if ($Script:ArpLaatst -and $Script:ArpLaatst.ContainsKey($mk[0])) { $Script:ArpLaatst.Remove($mk[0]) }
        try { if ($mac -match '^[0-9A-Fa-f]{12}$') { New-Item -ItemType Directory -Path $HistDir -Force -ErrorAction SilentlyContinue | Out-Null; [IO.File]::WriteAllText((Join-Path $HistDir "$sleutel.mac"), (($mac -replace '(..)(?!$)', '$1-').ToUpper()), $utf8) } } catch {}
        return $plek
      }
    }
    $bezet = @{}
    $fm = Join-Path $StatusDir 'locaties.txt'
    if (Test-Path -LiteralPath $fm) { foreach ($r in (Get-Content -LiteralPath $fm -Encoding UTF8 -ErrorAction SilentlyContinue)) { if ($r -match '^\s*([^=#]+?)\s*=\s*Tafel\s*(\d+)\s*,\s*plek\s*(\d+)' -and $Matches[1].Trim() -ne $sleutel) { $bezet["$([int]$Matches[2]),$([int]$Matches[3])"] = 1 } } }
    foreach ($k in $a.Keys) { if ($a[$k].plek -match '(?i)tafel\s*(\d+).*?plek\s*(\d+)') { $bezet["$([int]$Matches[1]),$([int]$Matches[2])"] = 1 } }
    foreach ($tf in 1..2) { foreach ($pl in 1..12) { if (-not $bezet["$tf,$pl"]) { $nieuw = "Tafel $tf, plek $pl"; $a[$sleutel] = @{ plek = $nieuw; gezien = $nu2 }; Schrijf-AutoPlekken $a; return $nieuw } } }
  } catch {}
  return ''
}
function Get-MerkModel([string]$serie) {
  # 8 okt 2026 (Roderick: 'ik wil merk en type zien, dit is geen surface'): merk + type uit het statusbestand (audit-update >= v3.282 meldt merk= en model=); onthouden in historie\<serie>.merk,
  # zodat het ook zichtbaar blijft als de laptop klaar/losgekoppeld is. Nog niet bekend (bv. tijdens de WinPE-installatie van een nieuwe laptop) = leeg.
  try {
    $merk = ''; $model = ''; $sf = Join-Path $StatusDir "$serie.txt"
    if (Test-Path $sf) { foreach ($r in (Get-Content $sf -Encoding UTF8 -ErrorAction SilentlyContinue)) { if ($r -match '^merk=(.*)$') { $merk = $Matches[1].Trim() } elseif ($r -match '^model=(.*)$') { $model = $Matches[1].Trim() } } }
    $mf = Join-Path $HistDir "$serie.merk"
    if ($merk -or $model) { try { if (-not (Test-Path $HistDir)) { New-Item -ItemType Directory -Path $HistDir -Force | Out-Null }; [IO.File]::WriteAllText($mf, "$merk`n$model", (New-Object Text.UTF8Encoding($false))) } catch {} }
    elseif (Test-Path $mf) { $rr = @(Get-Content $mf -Encoding UTF8 -ErrorAction SilentlyContinue); if ($rr.Count -ge 2) { $merk = "$($rr[0])".Trim(); $model = "$($rr[1])".Trim() } }
    $merk = $merk -replace '(?i)\s*(corporation|inc\.?|ltd\.?)\s*$', ''
    $tekst = (@($merk, $model) | Where-Object { $_ }) -join ' '
    if ($tekst) { return "<br><small>$(E $tekst)</small>" }
  } catch {}
  return ''
}function Get-PlekMerk([string]$serie) {
  # 16 okt 2026 (Roderick: 'ik kan niet goed zien wat het merk is van de plek'): merk (hoofdletters, vet) + kort type op de plek-tegel. Zelfde bron als Get-MerkModel (statusbestand, anders historie\<serie>.merk); losse onzin-woorden met ':' (bv. Surface-modelregel '124I:00061T:...') vallen weg. Leeg als nog onbekend.
  try {
    $t = "$(Get-MerkModel $serie)" -replace '<br><small>', '' -replace '</small>', ''
    $t = [System.Net.WebUtility]::HtmlDecode($t).Trim()
    if (-not $t) { return '' }
    $w = @($t -split '\s+' | Where-Object { $_ -and $_ -notmatch ':' })
    if ($w.Count -eq 0) { return '' }
    $merk = $w[0].ToUpper(); if ($merk -eq 'MICROSOFT') { $merk = 'SURFACE'; $w = @($w | Where-Object { $_ -ne 'Surface' }) }
    $type = (($w | Select-Object -Skip 1) -join ' '); if ($type.Length -gt 26) { $type = $type.Substring(0, 25) + '...' }
    return '<small class="pk-merk" title="' + (E $t) + '">' + (E $merk) + $(if ($type) { ' &middot; ' + (E $type) } else { '' }) + '</small>'
  } catch { return '' }
}function Get-WindowsTekst($s) {
  # "25H2 (26200.9445)" + " -> doel 26H2" als het doel afwijkt; leeg als de laptop (nog) geen v3.265 draait
  $v = "$($s['windows'])".Trim(); $b = "$($s['build'])".Trim(); $dl = "$($s['doel'])".Trim()
  if (-not $v -and -not $b) { return '' }
  $tekst = $v; if ($b) { $tekst += " ($b)" }
  if ($dl -and $v -and ($dl -gt $v)) { $tekst += " &rarr; doel $dl" }
  return $tekst
}
function Get-LiveLogHtml($serie) {
  # laatste regels van het consolevenster van de laptop (status\console\<serienummer>.txt, vanaf audit-update v3.264): toont wat de laptop ECHT doet, ook als de stap-indeling dat niet laat zien
  try {
    $f = Join-Path (Join-Path $StatusDir 'console') "$serie.txt"
    if (-not (Test-Path $f)) { return '<div class="sub">Nog geen console-log van deze laptop (komt vanaf audit-update v3.264).</div>' }
    $leeft = [int](((Get-Date) - (Get-Item $f).LastWriteTime).TotalSeconds)
    $regels = @(Get-Content $f -Tail 120 -Encoding UTF8 -ErrorAction Stop | ? { $_ -and $_.Trim() -and $_ -notmatch '^\*{10,}|^PS>|^Transcript |^Windows PowerShell transcript' } | Select-Object -Last 25)
    $tekst = ($regels | % { E ($_.Substring(0, [Math]::Min(190, $_.Length))) }) -join '<br>'
    return "<div class=""sub"" style=""margin-top:14px"">Live van het scherm van de laptop (laatste 25 regels; bestand $leeft s geleden bijgewerkt - de laptop kopieert dit elke 30 s, een lange stille installatie kan het uitstellen)</div><div style=""font-family:Consolas,monospace;font-size:12px;line-height:1.45;border:1px solid rgba(128,128,128,.35);border-radius:8px;padding:8px 10px;overflow-x:auto;white-space:nowrap"">$tekst</div>"
  } catch { return '' }
}
function Read-KV([string]$pad) {
  $h = @{}
  if (Test-Path $pad) { foreach ($l in (Get-Content $pad -Encoding UTF8 -ErrorAction SilentlyContinue)) { $i = $l.IndexOf('='); if ($i -gt 0) { $k = ($l.Substring(0,$i).Trim() -replace '[^a-z0-9_]','').ToLower(); if ($k) { $h[$k] = $l.Substring($i+1) } } } }
  return $h
}
function Write-KV([string]$pad, $h) { [IO.File]::WriteAllText($pad, ((($h.Keys | Sort-Object) | % { "$_=$($h[$_])" }) -join "`r`n") + "`r`n", $utf8) }
function E([string]$s) { return [System.Net.WebUtility]::HtmlEncode($s) }
function Fmt-Duur([double]$sec) {
  if ($sec -lt 90) { return "$([int]$sec) s" }
  if ($sec -lt 5400) { return "$([int][math]::Floor($sec/60)) min" }
  return "$([int][math]::Floor($sec/3600)) u $([int]([math]::Floor($sec/60) % 60)) min"
}
function Safe-Name([string]$s) { return ($s -replace '[^A-Za-z0-9_-]','') }

# gemiddelde duur per stap uit gearchiveerde runs (seconden), anders null
function Get-Gemiddelden {
  $som = @{}; $n = @{}
  if (Test-Path $RunsDir) {
    foreach ($f in (Get-ChildItem $RunsDir -Filter *.txt -ErrorAction SilentlyContinue)) {
      foreach ($l in (Get-Content $f.FullName -Encoding UTF8 -ErrorAction SilentlyContinue)) {
        if ($l -match '^stapduur_([a-z]+)=(\d+)$') { $id=$Matches[1]; $v=[double]$Matches[2]; if ($v -gt 20) { $som[$id] = [double]$som[$id] + $v; $n[$id] = [int]$n[$id] + 1 } }
      }
    }
  }
  $r = @{}; foreach ($k in $som.Keys) { $r[$k] = $som[$k] / $n[$k] }
  return $r
}

# Is de laptop bereikbaar? ICMP-ping, en als de firewall van de laptop ping blokkeert (zoals de Surface in Windows) een ARP-controle: ARP wordt
# nooit door een firewall tegengehouden zolang de laptop aan staat en op hetzelfde netwerk zit (SendARP, iphlpapi).
try { Add-Type -Namespace Win32 -Name Arp -MemberDefinition '[System.Runtime.InteropServices.DllImport("iphlpapi.dll", ExactSpelling=true)] public static extern int SendARP(int destIp, int srcIp, byte[] macAddr, ref int physicalAddrLen);' -ErrorAction Stop } catch {}
function Get-Cached([string]$sleutel, [int]$sec, [scriptblock]$blok) {
  # 13 okt 2026 (Roderick: 'kan dat niet gewoon live live'): een verversronde duurde 9-10 s doordat trage Windows-aanroepen (Get-NetAdapter 1,8 s, Get-NetTCPConnection 1,8 s, Get-SmbShare 3 s, ping naar een unit die niet antwoordt) elke ronde opnieuw liepen. Wat zelden verandert wordt nu kort onthouden; de unit-gegevens zelf worden elke ronde opnieuw gelezen.
  if ($Script:GeenCache) { return (& $blok) }   # tests zetten dit aan: elke aanroep levert dan een verse waarde
  if (-not $Script:CacheTabel) { $Script:CacheTabel = @{} }
  $e = $Script:CacheTabel[$sleutel]
  if ($e -and ((Get-Date) - $e.t).TotalSeconds -lt $sec) { return $e.v }
  $v = & $blok
  $Script:CacheTabel[$sleutel] = @{ t = Get-Date; v = $v }
  return $v
}function Ping-SnelEcht([string]$ip) {
  try { $p = New-Object System.Net.NetworkInformation.Ping; if ($p.Send($ip, 700).Status -eq [System.Net.NetworkInformation.IPStatus]::Success) { return $true } } catch {}
  try { $mac = New-Object byte[] 6; $len = 6; $adres = [BitConverter]::ToInt32(([System.Net.IPAddress]::Parse($ip)).GetAddressBytes(), 0); return ([Win32.Arp]::SendARP($adres, 0, $mac, [ref]$len) -eq 0) } catch { return $false }
}
function Ping-Snel([string]$ip) { return [bool](Get-Cached ("ping-" + $ip) 5 { Ping-SnelEcht $ip }) }
function Get-MacBijIp([string]$ip) {
  # MAC uit de ARP-tabel van de server (snel, geen netwerkverkeer); de ping/ARP-controle ervoor ververst die tabel al.
  try { foreach ($r in @(& arp.exe -a 2>$null)) { if ($r -match ('^\s*' + [regex]::Escape($ip) + '\s+([0-9a-fA-F]{2}(?:-[0-9a-fA-F]{2}){5})\s') -and $Matches[1] -ne '00-00-00-00-00-00') { return $Matches[1].ToUpper() } } } catch {}
  return ''
}function Is-DezelfdeLaptop([string]$serie, [string]$ip, [bool]$klaarOud) {
  # 8 okt 2026 (Roderick: dashboard klopt niet): een IP-adres kan door een ANDERE laptop worden overgenomen (Lenovo kreeg .10 van de Surface), waardoor een klaar+losgekoppelde laptop bleef 'antwoorden'.
  # Daarom het MAC-adres leren zolang de laptop bezig is en later vergelijken. Onbekend MAC + klaar + >10 min geleden = niet vertrouwen (klaar-laptops zijn na Sysprep uit).
  $mac = Get-MacBijIp $ip; if (-not $mac) { return $false }
  $mf = Join-Path $HistDir "$serie.mac"
  if (Test-Path $mf) { return ((Get-Content $mf -Raw).Trim() -eq $mac) }
  return (-not $klaarOud)
}function Gate-NaMoment([string]$serie, $moment) {
  # is er na $moment een PXE-poging van deze laptop gezien? (een laptop die herstart start altijd even via PXE)
  $gl = Join-Path $StateDir 'gate.log'
  if (-not (Test-Path $gl)) { return $false }
  foreach ($r in (Get-Content $gl -Tail 60 -ErrorAction SilentlyContinue)) {
    if ($r -match "^(\d{4}-\d\d-\d\d \d\d:\d\d:\d\d)\s+sn-$serie\s") { try { if ([datetime]::Parse($Matches[1]) -gt $moment) { return $true } } catch {} }
  }
  return $false
}
function Get-GatePxe {
  # 5 okt 2026 (fase 1): wat de SERVER ziet, 1 op 1. De PXE-poort kent het serienummer zodra een laptop opstart, lang voordat het script in Windows zijn eerste melding stuurt.
  # Geeft per serienummer het LAATSTE moment waarop de poort de laptop naar WinPE liet (alleen de laatste 400 regels).
  $res = @{}; $gl = Join-Path $StateDir 'gate.log'
  if (Test-Path $gl) {
    foreach ($r in (Get-Content $gl -Tail 400 -ErrorAction SilentlyContinue)) {
      if ($r -match '^(\d{4}-\d\d-\d\d \d\d:\d\d:\d\d)\s+sn-(\S+)\s+.*WinPE') { try { $res[(Safe-Name $Matches[2])] = [datetime]::Parse($Matches[1], [System.Globalization.CultureInfo]::InvariantCulture) } catch {} }
    }
  }
  return $res
}
function Get-InstallMediaan {
  # 7 okt 2026: duur per stap van eerdere ECHTE (LIVE) installaties via het netwerk, uit <status>\install-stappen.tsv (de server schrijft die zelf, zie hieronder). Mediaan van de laatste 10 per stap.
  # Geeft @{ stappen = @{ '1' = sec; ... }; runs = aantal eerdere installaties met een eindstap }. DROOGLOOP-regels tellen NIET mee (die doen de zware stappen niet echt).
  $res = @{ stappen = @{}; runs = 0 }
  $p = Join-Path $StatusDir 'install-stappen.tsv'
  if (-not (Test-Path $p)) { return $res }
  $per = @{}
  foreach ($r in @(Get-Content $p -ErrorAction SilentlyContinue)) {
    $d = $r -split "`t"; if ($d.Count -lt 5 -or $d[2] -ne 'LIVE') { continue }
    $sec = 0; if (-not [int]::TryParse($d[4], [ref]$sec)) { continue }
    if (-not $per.ContainsKey($d[3])) { $per[$d[3]] = New-Object System.Collections.Generic.List[int] }
    $per[$d[3]].Add($sec)
  }
  foreach ($k in $per.Keys) {
    $laatste = @($per[$k] | Select-Object -Last 10 | Sort-Object); if ($laatste.Count -eq 0) { continue }
    $res.stappen[$k] = $laatste[[int][math]::Floor(($laatste.Count - 1) / 2)]
    if ($per[$k].Count -gt $res.runs) { $res.runs = $per[$k].Count }
  }
  return $res
}
# 8 okt 2026: eenvoudige, deterministische schakelaar voor tests ($Script:NetAdapterOverride); in productie altijd de echte Ethernet-kaart van de server.
$Script:NetAdapterOverride = $null
function Get-EthAdapter { if ($null -ne $Script:NetAdapterOverride) { return $Script:NetAdapterOverride }; return (Get-Cached 'eth' 30 { Get-NetAdapter -Name 'Ethernet' -ErrorAction Stop }) }function Get-InstallBytes {
  # mediaan van het aantal bytes dat stap 5 (Windows ophalen) in eerdere ECHTE installaties kostte (6e kolom van install-stappen.tsv; 0 = niet gemeten). $null = geen data.
  $p = Join-Path $StatusDir 'install-stappen.tsv'; if (-not (Test-Path $p)) { return $null }
  $v = New-Object System.Collections.Generic.List[double]
  foreach ($r in @(Get-Content $p -ErrorAction SilentlyContinue)) { $d = $r -split "`t"; if ($d.Count -ge 6 -and $d[2] -eq 'LIVE' -and $d[3] -eq '5') { $b = 0.0; if ([double]::TryParse($d[5], [ref]$b) -and $b -gt 100MB) { $v.Add($b) } } }
  if ($v.Count -eq 0) { return $null }
  $s = @($v | Select-Object -Last 10 | Sort-Object); return @{ bytes = $s[[int][math]::Floor(($s.Count - 1) / 2)]; n = $s.Count }
}
function Get-InstallEersteStart {
  # mediaan (seconden) van de fase 'eerste start van Windows' (stap 8 in install-stappen.tsv: einde WinPE -> eerste melding van het script) uit eerdere ECHTE installaties; $null = geen data
  $p = Join-Path $StatusDir 'install-stappen.tsv'; if (-not (Test-Path $p)) { return $null }
  $v = New-Object System.Collections.Generic.List[double]
  foreach ($r in @(Get-Content $p -ErrorAction SilentlyContinue)) { $d = $r -split "`t"; if ($d.Count -ge 5 -and $d[2] -eq 'LIVE' -and $d[3] -eq '8') { $x = 0.0; if ([double]::TryParse($d[4], [ref]$x) -and $x -gt 20) { $v.Add($x) } } }
  if ($v.Count -eq 0) { return $null }
  $s = @($v | Select-Object -Last 10 | Sort-Object); return @{ sec = $s[[int][math]::Floor(($s.Count - 1) / 2)]; n = $s.Count }
}
function Get-HerstartGem {
  $som = 0.0; $n = 0
  if (Test-Path $RunsDir) { foreach ($f in (Get-ChildItem $RunsDir -Filter *.txt -ErrorAction SilentlyContinue)) { foreach ($l in (Get-Content $f.FullName -Encoding UTF8 -ErrorAction SilentlyContinue)) { if ($l -match '^herstartduur_\d+=(\d+)$') { $v=[double]$Matches[1]; if ($v -gt 20) { $som += $v; $n++ } } } } }
  if ($n -gt 0) { return ($som / $n) } else { return 300.0 }   # standaard: 5 min (waargenomen herstarts op de Surface: 4-6 min)
}
function Update-Laptop([string]$statusPad, $now) {
  $s = Read-KV $statusPad
  $serie = Safe-Name $s['serienummer']; if (-not $serie) { return $null }
  New-Item -ItemType Directory -Path $HistDir, $RunsDir -Force -ErrorAction SilentlyContinue | Out-Null
  $logPad = Join-Path $HistDir "$serie.log"; $metaPad = Join-Path $HistDir "$serie.meta"
  $m = Read-KV $metaPad
  $klaar = ($s['klaar'] -eq 'ja')
  $herstarts = 0; [void][int]::TryParse("$($s['herstarts'])", [ref]$herstarts)
  $tekst = if ("$($s['stap']))".Trim().Length -gt 1) { "$($s['stap'])" } else { "$($s['stap']) $($s['fase']) $($s['laatste'])" }
  $nieuweStap = Get-StapId $tekst
  if ($klaar) { $nieuweStap = 'sysprep' }
  $ids = @($Stappen | % { $_.id })
  $vloer = 0
  if ($s['voltooid_oem'] -eq 'True') { $vloer = [array]::IndexOf($ids,'wu') }
  if ($s['voltooid_wu'] -eq 'True') { $vloer = [array]::IndexOf($ids,'feature') }
  $idxNieuw = [array]::IndexOf(@($Stappen | % { $_.id }), $nieuweStap)
  if ($idxNieuw -lt $vloer) { $idxNieuw = $vloer; $nieuweStap = $Stappen[$vloer].id }
  # nieuwe run? (andere run-sleutel, of teller loopt terug)
  $runKey = if ($s['runstart']) { Safe-Name ($s['runstart'] -replace '[:.]','') } else { '' }
  # 7 okt 2026: een run die met win11-auto (wpelogs\install-<serie>.txt) is gestart, is een NIEUWE run: het statusbestand van een vers Windows heeft in het begin nog geen 'runstart', waardoor de oude cijfers van de vorige run bleven staan (25 u, 3 van 5 stappen).
  $instPad = Join-Path (Join-Path (Split-Path -Parent (Split-Path -Parent $StatusDir)) 'wpelogs') ("install-{0}.txt" -f $serie)
  $instNieuwer = $false; $instStart = $null
  try { if ((Test-Path $instPad) -and $m.ContainsKey('eerstgezien') -and ((Get-Item $instPad).LastWriteTime -gt [datetime]::Parse($m['eerstgezien']))) { $instNieuwer = $true; if ($Script:InstRunStart -and $Script:InstRunStart.ContainsKey($serie)) { $instStart = $Script:InstRunStart[$serie] } } } catch {}
  # 19 okt 2026 (Roderick: top-notch data): het statusbestand van de VORIGE, afgeronde run (klaar=ja) bleef staan terwijl een nieuwe win11-auto-installatie (wpelogs\install-<serie>.txt, NIEUWER dan dat statusbestand, < 4 uur) liep.
  # Bij elke nieuwe run schreef de server daardoor een nep-regel 'KLAAR - run afgerond (Sysprep gestart)' (zelfde seconde als START) en archiveerde een nep-run in runs\ (telde mee in 'Eerder klaar' en in de gemiddelden).
  # Zo'n statusbestand is niet van de lopende run: er wordt dan niets als KLAAR vastgelegd of gearchiveerd. Een echte afronding (Windows schrijft NA de installatie) is nooit ouder dan het installatiebestand.
  $staleKlaar = $false
  try { if ($klaar -and (Test-Path -LiteralPath $instPad) -and (Test-Path -LiteralPath $statusPad)) { $stM = (Get-Item -LiteralPath $statusPad).LastWriteTime; $inM = (Get-Item -LiteralPath $instPad).LastWriteTime; if ($stM -lt $inM -and ($now - $inM).TotalHours -lt 4) { $staleKlaar = $true } } } catch {}
  # 29 okt 2026 (kennisbank #1400): een statusbestand dat OUDER is dan het installatiebestand van deze run (klaar=ja van de vorige run) zegt niets over deze run; de stap daaruit (Sysprep, stap 9) hoort er niet te staan.
  # Tot de laptop zich zelf meldt staat de run op de eerste stap en wordt er niets over de vorige stap vastgelegd.
  if ($staleKlaar) { $nieuweStap = $Stappen[0].id; $idxNieuw = 0 }
  $nieuweRun = $false
  if (-not $m.ContainsKey('runkey')) { $nieuweRun = $true }
  elseif ($instNieuwer) { $nieuweRun = $true }  elseif ($runKey -and $m['runkey'] -ne $runKey -and $m['runkeyvoorlopig'] -ne 'ja') { $nieuweRun = $true }
  elseif (-not $runKey -and $m['gearchiveerd'] -eq 'ja' -and -not $klaar) { $nieuweRun = $true }
  elseif (-not $runKey -and [int]$m['herstarts'] -gt $herstarts) { $nieuweRun = $true }
  # 29 okt 2026 (kennisbank #1432): de eerste melding van een verse Windows (WINPE_READY) heeft nog geen runstart; de run kreeg dan een tijdelijke sleutel en werd bij de volgende melding (met runstart) als 'onderbroken/vervangen' gearchiveerd (410 bytes, 24 s). De tijdelijke sleutel wordt nu overgenomen.
  if (-not $nieuweRun -and $runKey -and $m['runkeyvoorlopig'] -eq 'ja') { $m['runkey'] = $runKey; $m.Remove('runkeyvoorlopig') }
  if ($nieuweRun) {
    if ((Test-Path $logPad) -and $m['gearchiveerd'] -ne 'ja' -and $m.ContainsKey('runkey') -and -not $staleKlaar -and $m['stalestart'] -ne 'ja') { Archiveer $serie $m $logPad $now 'onderbroken/vervangen' }
    # 8 okt 2026: ging aan deze run een win11-auto-installatie vooraf (installatiebestand < 90 min geleden geschreven), dan is het begin van de run het begin van WinPE (SERVERklok), zodat de verstreken tijd bij de overgang naar Windows NIET opnieuw begint.
    $startServer = $null
    try {
      if (Test-Path $instPad) {
        if (($now - (Get-Item $instPad).LastWriteTime).TotalMinutes -le 90) {
          $cand = $null
          if ($Script:InstRunStart -and $Script:InstRunStart.ContainsKey($serie)) { $cand = $Script:InstRunStart[$serie] }
          else { $sf = Join-Path $StatusDir "install-start-$serie.txt"; if (Test-Path $sf) { $cand = [datetime]::Parse(((Get-Content $sf -Raw).Trim())) } }
          # 28 okt 2026 (kennisbank #1346): een PXE-start van VOOR het einde van een al afgeronde run is van die eerdere ronde; niet hergebruiken (gaf "2 u 53 min" voor een tweede ronde).
          $eindVorigeRun = $null
          try { foreach ($rfVorig in @(Get-ChildItem -LiteralPath $RunsDir -Filter "${serie}__*.txt" -File -ErrorAction SilentlyContinue)) { $rkVorig = Read-KV $rfVorig.FullName; if ($rkVorig['reden'] -eq 'klaar' -and $rkVorig['einde']) { $eVorig = [datetime]::Parse($rkVorig['einde']); if (-not $eindVorigeRun -or $eVorig -gt $eindVorigeRun) { $eindVorigeRun = $eVorig } } } } catch {}
          if ($cand -and $eindVorigeRun -and $cand -le $eindVorigeRun) { $cand = $null }
          if ($cand -and (($now - $cand).TotalMinutes -le 240) -and $cand -le $now) { $startServer = $cand }
        }
      }
    } catch {}    $m = @{}; $m['runkey'] = $(if ($runKey -and -not $staleKlaar) { $runKey } else { $now.ToString('yyyyMMddHHmmss') }); if (-not $runKey -or $staleKlaar) { $m['runkeyvoorlopig'] = 'ja' }
    $m['start'] = $(try { if ($instStart) { $instStart.ToString('o') } elseif ($s['runstart']) { [datetime]::Parse($s['runstart']).ToString('o') } else { $now.ToString('o') } } catch { $now.ToString('o') }); $m['eerstgezien'] = $now.ToString('o'); $m['stapidx'] = '0'; $m['gearchiveerd'] = 'nee'; $m['herstarts'] = "$herstarts"
    $m['stapstart_start'] = $now.ToString('o')
    if ($startServer) { $m['startserver'] = $startServer.ToString('o') }
    if ($staleKlaar) { $m['stalestart'] = 'ja' }   # 29 okt 2026: voorlopige run tot de laptop zich echt meldt (kennisbank #1400)
    if (Test-Path $logPad) { [IO.File]::Delete($logPad) }
    Add-Content -Path $logPad -Value ("{0}`t{1}`t{2}`t{3}`t{4}" -f $now.ToString('yyyy-MM-dd HH:mm:ss'), $herstarts, 'START', "klant=$($s['klant']) order=$($s['ordernummer']) engineer=$($s['engineer']) tool=v$($s['versie'])", "$($s['accu'])%") -Encoding UTF8
  }
  # ---- herstart-detectie: melding > 90 s stil EN (laptop antwoordt niet op ping | bij oude melding zonder ip: PXE-poging gezien) ----
  $mtijd = (Get-Item $statusPad).LastWriteTime; $leeftijdS = ($now - $mtijd).TotalSeconds
  $inHerstart = $false
  if (-not $klaar -and $leeftijdS -gt 25) {   # snel reageren: de laptop meldt normaal elke paar seconden
    if ($s['ip']) { $inHerstart = -not (Ping-Snel $s['ip']) }
    else {   # oude melding zonder ip: netwerkpoort naar de laptop weg, of een PXE-poging na de laatste melding
      $linkWeg = $false; try { $na = Get-EthAdapter; $linkWeg = ($na.Status -ne 'Up') } catch {}
      $inHerstart = ($linkWeg -and $leeftijdS -gt 25) -or (Gate-NaMoment $serie $mtijd)
    }
  }
  if ($inHerstart -and -not $m['herstartsinds']) {
    $m['herstartsinds'] = $mtijd.ToString('o')
    Add-Content -Path $logPad -Value ("{0}`t{1}`t{2}`t{3}`t{4}" -f $now.ToString('yyyy-MM-dd HH:mm:ss'), $herstarts, 'HERSTART BEGONNEN', ("geen melding sinds " + $mtijd.ToString('HH:mm:ss') + ", laptop start opnieuw op"), "$($s['accu'])%") -Encoding UTF8
  }
  if (-not $inHerstart -and $m['herstartsinds'] -and $leeftijdS -le 60) {
    $dur = [int](($now - [datetime]::Parse($m['herstartsinds'])).TotalSeconds)
    $nr = @($m.Keys | ? { $_ -like 'herstartduur_*' }).Count + 1
    $m["herstartduur_$nr"] = "$dur"
    Add-Content -Path $logPad -Value ("{0}`t{1}`t{2}`t{3}`t{4}" -f $now.ToString('yyyy-MM-dd HH:mm:ss'), $herstarts, 'HERSTART KLAAR', ("laptop is weer terug; herstart duurde " + (Fmt-Duur $dur)), "$($s['accu'])%") -Encoding UTF8
    $m.Remove('herstartsinds')
  }  $idxOud = [int]$m['stapidx']
  if ($idxNieuw -gt $idxOud) {   # alleen vooruit (voorkomt flikkeren)
    $oudId = $Stappen[$idxOud].id
    if ($m.ContainsKey("stapstart_$oudId")) { $m["stapduur_$oudId"] = "$([int](($now - [datetime]::Parse($m["stapstart_$oudId"])).TotalSeconds))" }
    $m['stapidx'] = "$idxNieuw"; $m["stapstart_$nieuweStap"] = $now.ToString('o'); $idxOud = $idxNieuw
    Add-Content -Path $logPad -Value ("{0}`t{1}`t{2}`t{3}`t{4}" -f $now.ToString('yyyy-MM-dd HH:mm:ss'), $herstarts, 'STAP', $Stappen[$idxNieuw].naam, "$($s['accu'])%") -Encoding UTF8
  }
  if ($herstarts -ne [int]$m['herstarts']) {
    Add-Content -Path $logPad -Value ("{0}`t{1}`t{2}`t{3}`t{4}" -f $now.ToString('yyyy-MM-dd HH:mm:ss'), $herstarts, 'HERSTART', "herstart nr $herstarts", "$($s['accu'])%") -Encoding UTF8
    $m['herstarts'] = "$herstarts"
  }
  $laatste = "$($s['laatste'])".Trim()
  if ($laatste -and $laatste -ne $m['laatsteLog'] -and ((-not $m['laatsteLogTijd']) -or (($now - [datetime]::Parse($m['laatsteLogTijd'])).TotalSeconds -ge 60))) {
    Add-Content -Path $logPad -Value ("{0}`t{1}`t{2}`t{3}`t{4}" -f $now.ToString('yyyy-MM-dd HH:mm:ss'), $herstarts, 'MELDING', $laatste, "$($s['accu'])%") -Encoding UTF8
    $m['laatsteLog'] = $laatste; $m['laatsteLogTijd'] = $now.ToString('o')
  }
  try { $m['klokverschilsec'] = "$([int](Get-KlokVerschil @{ s = $s; pad = $statusPad }).TotalSeconds)" } catch {}   # 28 okt 2026: archief op de serverklok (kennisbank #1346)
  if ($klaar -and -not $staleKlaar -and $m['gearchiveerd'] -ne 'ja') {
    $cur = $Stappen[[int]$m['stapidx']].id
    if ($m.ContainsKey("stapstart_$cur")) { $m["stapduur_$cur"] = "$([int](($now - [datetime]::Parse($m["stapstart_$cur"])).TotalSeconds))" }
    Add-Content -Path $logPad -Value ("{0}`t{1}`t{2}`t{3}`t{4}" -f $now.ToString('yyyy-MM-dd HH:mm:ss'), $herstarts, 'KLAAR', 'run afgerond (Sysprep gestart, laptop schakelt uit)', "$($s['accu'])%") -Encoding UTF8
    Archiveer $serie $m $logPad $now 'klaar'
    $m['gearchiveerd'] = 'ja'
    # 19 okt 2026 (Roderick: 'doe dat automatisch de volgende keer'): onafhankelijke EINDCONTROLE (scripts\301): vergelijkt wat het script claimt met wat de laptop zelf teruglas; resultaat in status\eindcontrole\<serie>\laatste.txt.
    # 30 s wachten: de laatste logs (console, hpia) worden vlak na KLAAR nog naar de server gekopieerd. Pad MET spaties staat tussen aanhalingstekens (CLAUDE.md 2c).
    try { $ecPad = Join-Path (Join-Path (Split-Path -Parent (Split-Path -Parent (Split-Path -Parent $ScriptFolder))) 'scripts') '301-eindcontrole-run.ps1'
      if (Test-Path -LiteralPath $ecPad) { $ecCmd = "Start-Sleep 30; & '$ecPad' -Serie '$serie' -StatusDir '$StatusDir'"; Start-Process -FilePath "$env:SystemRoot\System32\WindowsPowerShell\v1.0\powershell.exe" -ArgumentList '-NoProfile', '-ExecutionPolicy', 'Bypass', '-WindowStyle', 'Hidden', '-Command', ('"' + $ecCmd + '"') -WindowStyle Hidden } } catch {}
  }
  Write-KV $metaPad $m
  return @{ s=$s; m=$m; serie=$serie; klaar=$klaar; herstarts=$herstarts; idx=[int]$m['stapidx']; pad=$statusPad; herstart=$inHerstart; herstartsinds=$m['herstartsinds'] }
}
function Archiveer([string]$serie, $m, [string]$logPad, $now, [string]$reden) {
  try {
    $naam = Join-Path $RunsDir ("{0}__{1}.txt" -f $serie, $m['runkey'])
    # 19 okt 2026: een AFGEROND archief (reden=klaar) wordt nooit meer overschreven door een latere, armere schrijfactie (onderbroken/vervangen); dat gebeurde om 21:10:35 en 22:13:40 met de HP-run
    try { if ((Test-Path -LiteralPath $naam) -and $reden -ne 'klaar') { $oudK = @(Get-Content -LiteralPath $naam -TotalCount 8 -Encoding UTF8 -ErrorAction SilentlyContinue); if ($oudK -contains 'reden=klaar') { return } } } catch {}
    $kop = @("serienummer=$serie", "runkey=$($m['runkey'])", "start=$($m['start'])", "einde=$($now.ToString('o'))", "reden=$reden")
    $stSrv = ''; try { if ($m['startserver']) { $stSrv = [datetime]::Parse($m['startserver']).ToString('o') } elseif ($m['klokverschilsec']) { $stSrv = ([datetime]::Parse($m['start']) + [timespan]::FromSeconds([double]$m['klokverschilsec'])).ToString('o') } } catch {}
    if ($stSrv) { $kop += "startserver=$stSrv" }
    $kop += ($m.Keys | ? { $_ -like 'stapduur_*' -or $_ -like 'herstartduur_*' } | Sort-Object | % { "$_=$($m[$_])" })
    $kop += '--- gebeurtenissen (tijd, herstarts, type, tekst, accu) ---'
    $kop += (Get-Content $logPad -Encoding UTF8 -ErrorAction SilentlyContinue)
    [IO.File]::WriteAllText($naam, ($kop -join "`r`n") + "`r`n", $utf8)
  } catch {}
}

$css = @'
:root{--new:#5b21b6;--newbg:#ede9fe;--bg:#f6f7f9;--fg:#1c2330;--card:#fff;--line:#dfe3ea;--ok:#1a7f37;--okbg:#dcf5e3;--go:#0b5cad;--gobg:#dbeafe;--warn:#8a5a00;--warnbg:#fff1cc;--bad:#b42318;--badbg:#fde2e0;--mut:#6b7686}
@media(prefers-color-scheme:dark){:root{--new:#c4b5fd;--newbg:#2e1f5e;--bg:#10151c;--fg:#e6ebf2;--card:#18202a;--line:#2a3441;--ok:#6fd58c;--okbg:#15321f;--go:#8cc3ff;--gobg:#12304f;--warn:#ffcf6b;--warnbg:#3a2d0c;--bad:#ff9d94;--badbg:#431a17;--mut:#93a0b2}}
body{margin:0;background:var(--bg);color:var(--fg);font:15px/1.45 system-ui,Segoe UI,Arial,sans-serif}.wrap{max-width:1280px;margin:0 auto;padding:20px 16px}
h1{font-size:22px;margin:0 0 4px}h2{font-size:17px;margin:24px 0 8px}a{color:var(--go)}.sub{color:var(--mut);margin-bottom:14px}
.tegels{display:flex;gap:10px;flex-wrap:wrap;margin-bottom:16px}.tegel{background:var(--card);border:1px solid var(--line);border-radius:10px;padding:10px 16px;min-width:110px}.tegel b{font-size:26px;display:block}
table{width:100%;border-collapse:collapse;background:var(--card);border:1px solid var(--line);border-radius:10px;overflow:hidden}
th,td{text-align:left;padding:9px 12px;border-bottom:1px solid var(--line);vertical-align:top}th{font-size:12px;text-transform:uppercase;letter-spacing:.04em;color:var(--mut)}
.tafels{display:flex;gap:24px;flex-wrap:wrap;margin:14px 0}.tafel{flex:1;min-width:300px}.tafel h2{font-size:15px;margin:0 0 6px}.plekken{display:grid;grid-template-columns:repeat(6,1fr);gap:6px}.plek{border:1px solid rgba(128,128,128,.35);border-radius:8px;padding:4px 6px;min-height:48px;font-size:12px}.plek small{display:block;opacity:.7}.plek a{text-decoration:none;color:inherit}/* KLAAR = het hele blok fel groen (rij, plattegrondvakje, kop van de detailpagina) */tr.rij-klaar td{background:#00c853!important;color:#04230f;font-weight:600}tr.rij-klaar a{color:#04230f}tr.rij-klaar .badge{background:#04230f;color:#fff}.plek.klaar{background:#00c853;border-color:#00a844;color:#04230f}.plek.klaar small,.plek.klaar a{color:#04230f}.plek.klaar .badge{background:#04230f;color:#fff}.kop-klaar{background:#00c853;color:#04230f;border-radius:12px;padding:12px 16px;margin:8px 0}.kop-klaar h1{margin:0;color:#04230f}.kop-klaar .badge{background:#04230f;color:#fff}.badge{display:inline-block;padding:3px 10px;border-radius:99px;font-weight:700;font-size:13px;white-space:nowrap}
.stp{display:block;font-size:10px;color:var(--mut);line-height:1.2;overflow:hidden;white-space:nowrap;text-overflow:ellipsis;max-width:100%}
.lvb{display:block;height:5px;background:var(--line);border-radius:2px;margin-top:2px;overflow:hidden}.lvb i{display:block;height:5px;border-radius:2px}.lv-d{background:var(--go)}.lv-u{background:var(--warn)}.lvt{display:block;font-size:10px;color:var(--mut);line-height:1.2}.vb{display:block;font-size:10px;line-height:1.3;margin-top:2px}.vb-ok{color:var(--ok)}.vb-warn{color:var(--warn);font-weight:600}.vb-bad{color:var(--bad);font-weight:700}.verbinding{margin:6px 0}.netpaneel{border:1px solid var(--line);border-radius:10px;padding:10px 14px;margin:12px 0;background:var(--card)}.netkop{margin-bottom:8px}.netgetallen{display:flex;gap:22px;flex-wrap:wrap;margin:6px 0 10px}.netgetallen div{min-width:150px}.netgetallen b{display:block;font-size:1.5rem}.netgetallen span{font-size:12px}.netgetallen small{display:block;color:var(--mut)}.blauw{color:var(--go);font-weight:600}.oranje{color:var(--warn);font-weight:600}.badge.klaar{background:var(--okbg);color:var(--ok)}.badge.voorbehoud{background:var(--warnbg);color:var(--warn)}.bar.voorbehoud i{background:var(--warn)}.voorbehoud-uitleg{background:var(--warnbg);color:var(--warn);padding:8px 12px;border-radius:8px;margin:8px 0}.badge.nieuw{background:var(--newbg);color:var(--new)}.plek.nieuw{background:var(--newbg)}tr.rij-nieuw td{background:var(--newbg)}details.eerder{margin:14px 0}details.eerder summary{cursor:pointer;color:var(--mut)}details.eerder li{margin:3px 0}.badge.bezig{background:var(--gobg);color:var(--go)}.badge.stil{background:var(--warnbg);color:var(--warn)}.badge.weg{background:var(--badbg);color:var(--bad)}.badge.herstart{background:#ede9fe;color:#5b21b6}@media(prefers-color-scheme:dark){.badge.herstart{background:#2e1f5e;color:#c4b5fd}}
small,.mut{color:var(--mut)}.leeg{padding:30px;text-align:center;color:var(--mut)}.voet{margin-top:12px;font-size:12px;color:var(--mut)}
.bar{background:var(--line);border-radius:99px;height:10px;width:140px;overflow:hidden}.bar i{display:block;height:10px;background:var(--go)}.bar.klaar i{background:var(--ok)}
.stap{display:flex;gap:10px;align-items:center;padding:9px 12px;border-bottom:1px solid var(--line)}.stap:last-child{border:0}.stap .ic{width:22px;text-align:center;font-weight:700}
.stap.klaar .ic{color:var(--ok)}.stap.bezig{background:var(--gobg)}.stap.bezig .ic{color:var(--go)}.stap.todo{color:var(--mut)}.stap .t{margin-left:auto;color:var(--mut);white-space:nowrap}
.kaart{background:var(--card);border:1px solid var(--line);border-radius:10px;overflow:hidden}
@media(max-width:800px){table,thead,tbody,tr,td,th{display:block}thead{display:none}tr{border-bottom:2px solid var(--line);padding:6px 0}td{border:0;padding:4px 12px}}
/* 13 okt 2026 (Roderick): plek-tegels met een live voortgangsbalk 0-100% (blauw = lopend tot groen = klaar, rood = fout), tijd erbij, verkeer groen (ophalen) en rood (terugsturen); hoofdtegels met fout/klaar/bezig en de verdeling van de voortgang */
.pk-bar{display:block;width:100%;height:14px;border:1px solid rgba(128,128,128,.55);box-sizing:border-box;background:rgba(128,128,128,.35);border-radius:7px;overflow:hidden;margin:3px 0 2px}.pk-bar i{display:block;height:14px;border-radius:7px}
.pk-txt{display:block;font-weight:700;font-size:13px;line-height:1.25}.pk-eta{display:block;font-size:11px;opacity:.9}
.lv-d{background:#16a34a}.lv-u{background:#dc2626}
.tegel.t-fout{border-left:6px solid #dc2626}.tegel.t-fout b{color:#dc2626}.tegel.t-klaar{border-left:6px solid #16a34a}.tegel.t-klaar b{color:#16a34a}.tegel.t-vb{border-left:6px solid #d97706}.tegel.t-vb b{color:#d97706}.tegel.t-bezig{border-left:6px solid #2563eb}.tegel.t-bezig b{color:#2563eb}.tegel.t-stil{border-left:6px solid #6b7280}
.vd{display:inline-block;min-width:54px;margin:2px 6px 0 0;padding:2px 8px;border-radius:6px;color:#fff;font-size:12px;font-weight:700}
/* 13 okt 2026: grotere plek-tegels (4 per rij); verkeer omhoog/omlaag en de verbinding (snelheid, pakketverlies) blijven zichtbaar en leesbaar: belangrijk om schade aan laptop, kabel of poort te zien */.plekken{grid-template-columns:repeat(3,1fr);gap:10px}.plek{min-height:190px;padding:10px 12px;font-size:15px}.stp{font-size:12px}.lvb{height:9px;margin-top:4px}.lvb i{height:9px}.lvt{font-size:12px;font-weight:600}.vb{font-size:12px}.pk-bar{height:18px;border-radius:9px}.pk-bar i{height:18px;border-radius:9px}/* 13 okt 2026 (Roderick: "het volledige vlak"): het HELE plekvak in een volle kleur van de toestand; de balk loopt op een donker spoor van rood via geel naar groen */
.plek.bezig,.plek.herstart{background:#2563eb;border-color:#1d4ed8;color:#fff}.plek.voorbehoud,.plek.stil{background:#d97706;border-color:#b45309;color:#fff}.plek.weg{background:#dc2626;border-color:#b91c1c;color:#fff}
.plek.bezig a,.plek.herstart a,.plek.voorbehoud a,.plek.stil a,.plek.weg a,.plek.bezig small,.plek.herstart small,.plek.voorbehoud small,.plek.stil small,.plek.weg small{color:#fff;opacity:1}
.plek.bezig .stp,.plek.herstart .stp,.plek.voorbehoud .stp,.plek.stil .stp,.plek.weg .stp,.plek.bezig .lvt,.plek.herstart .lvt,.plek.voorbehoud .lvt,.plek.stil .lvt,.plek.weg .lvt,.plek.bezig .vb,.plek.herstart .vb,.plek.voorbehoud .vb,.plek.stil .vb,.plek.weg .vb{color:#fff}
.plek.bezig .pk-bar,.plek.herstart .pk-bar,.plek.voorbehoud .pk-bar,.plek.stil .pk-bar,.plek.weg .pk-bar,.plek.klaar .pk-bar{background:rgba(0,0,0,.5);border:1px solid rgba(255,255,255,.75)}
.plek.bezig .lvb,.plek.herstart .lvb,.plek.voorbehoud .lvb,.plek.stil .lvb,.plek.weg .lvb{background:rgba(0,0,0,.35)}.plek{position:relative}.pk-txt{font-size:24px;font-weight:800;line-height:1.2;display:block}.pk-merk{display:block;font-weight:800;font-size:13px;letter-spacing:.3px;opacity:1}
/* 16 okt 2026 (Roderick: 'pas het dashboard aan voor full screen 1080p, hij moet nu op een breedbeeld tv'): vanaf 1700 px breed en 800 px hoog past ALLES wat op een TV zichtbaar moet zijn op een scherm zonder scrollen: kop + tegels op een rij, de plattegrond (twee tafels naast elkaar, 4 plekken per rij, grotere tekst). De detailtabel, het netwerkpaneel en de voetnoot zijn op de TV verborgen (open het dashboard in een smaller venster, of een laptop-pagina via de plek, voor de details). */
@media (min-width:1700px) and (min-height:800px){html body .wrap{max-width:none;margin:0;padding:6px 20px}html body h1{font-size:26px;margin:0 0 2px}html body .wrap>h1+p+.sub{display:none}html body .wrap>h1+p{margin:0 0 2px;font-size:14px}html body .tegels{display:flex;flex-wrap:nowrap;gap:8px;margin:4px 0}html body .tegels .tegel{flex:1;padding:5px 10px}html body .tegels .tegel b{font-size:20px}html body .netpaneel,html body .wrap>table,html body .voet{display:none}html body .tafels{display:flex;flex-wrap:nowrap;gap:18px;margin:4px 0;height:calc(100vh - 244px)}html body .tafel{flex:1;min-width:0;display:flex;flex-direction:column}html body .tafel h2{font-size:18px;margin:0 0 3px}html body .plekken{flex:1;min-height:0;grid-template-columns:repeat(4,1fr);grid-template-rows:repeat(3,minmax(0,1fr));gap:6px}html body .plek{overflow:hidden;min-height:0;padding:3px 6px}html body .pk-merk{font-size:12px}html body .pk-txt{font-size:15px;line-height:1.15}html body .pk-eta,html body .pk-all,html body .pk-nu{font-size:10px}html body .plek small,html body .plek .lvt,html body .plek .stp,html body .plek .vb{font-size:10px;line-height:1.15}html body .plek .lvb{height:4px;margin-top:1px}}
'@

# 10 okt 2026 (Roderick: 'ik moest verversen... kan je het zo bouwen dat ik niet elke keer op F5 hoef te drukken'): de oude <meta refresh> bleef in een lang openstaand of op de achtergrond staand tabblad hangen. Nu haalt de pagina zelf elke 2 s de nieuwe versie op (fetch, geen cache), ververst meteen zodra je terugkomt in het tabblad, en toont een ROOIE balk als er 10 s niets binnenkomt (dan weet je dat het scherm stilstaat). Zonder JavaScript valt het terug op de oude meta-refresh.
$LiveHead = @'
<noscript><meta http-equiv="refresh" content="2"></noscript><script>(function(){var laatst=Date.now(),bezig=false,balk=null;function toon(){var s=Math.round((Date.now()-laatst)/1000);if(!balk){balk=document.createElement('div');balk.id='verouderd-balk';balk.style.cssText='position:fixed;top:0;left:0;right:0;z-index:99999;background:#b42318;color:#fff;font:700 15px system-ui,Segoe UI,Arial,sans-serif;padding:8px 16px;text-align:center;display:none';document.documentElement.appendChild(balk);}var t=document.title.replace(/^\(verouderd\) /,'');if(s>=10){balk.textContent='LET OP: dit scherm is VEROUDERD - laatste update '+s+' s geleden (server of verbinding weg?)';balk.style.display='block';document.title='(verouderd) '+t;}else{balk.style.display='none';document.title=t;}}
async function ververs(){if(bezig)return;bezig=true;try{var ac=new AbortController();var tm=setTimeout(function(){ac.abort()},8000);var r=await fetch(location.pathname,{cache:'no-store',signal:ac.signal});clearTimeout(tm);if(r.ok){var tekst=await r.text();var d=new DOMParser().parseFromString(tekst,'text/html');var nieuw=d.body.innerHTML;var ns=d.querySelector('style'),os=document.querySelector('style');if(ns&&os&&ns.textContent!==os.textContent){os.textContent=ns.textContent;}try{var ns2=d.querySelector('head script'),os2=document.querySelector('head script');if(ns2&&os2&&ns2.textContent!==os2.textContent&&sessionStorage.getItem('herlaad-voor')!==String(ns2.textContent.length)){sessionStorage.setItem('herlaad-voor',String(ns2.textContent.length));location.reload();return;}}catch(e2){}if(nieuw!==document.body.innerHTML){var open=[].map.call(document.querySelectorAll('details'),function(x){return x.open;});var y=window.scrollY;document.body.innerHTML=nieuw;[].forEach.call(document.querySelectorAll('details'),function(x,i){if(open[i])x.open=true;});window.scrollTo(0,y);}laatst=Date.now();}}catch(e){}bezig=false;toon();}
setInterval(ververs,500);setInterval(toon,1000);document.addEventListener('visibilitychange',function(){if(!document.hidden)ververs();});window.addEventListener('focus',ververs);window.addEventListener('online',ververs);})();</script>
'@
$LiveHead = $LiveHead.Trim()
$LiveHead = '<link rel="icon" type="image/x-icon" href="esprit-ict.ico">' + $LiveHead   # 17 okt 2026: Esprit ICT-icoon in het tabblad

function Get-Klasse($l, $nu) {
  $leeftijd = ($nu - (Get-Item $l.pad).LastWriteTime).TotalSeconds; if ($leeftijd -lt 0) { $leeftijd = 0 }
  if ($l.klaar) {
    # 9 okt 2026 (Roderick: 'opruimen zag ik wel op de laptop maar niet op het dashboard'): het script meldt KLAAR zodra Sysprep STARTT; de laptop is dan nog ca. 1-2 min bezig met opruimen en uitschakelen. Zolang hij nog antwoordt: SYSPREP BEZIG, pas daarna KLAAR.
    if ($l['sysprepBezig']) { return @('bezig','SYSPREP BEZIG (laptop schakelt zo uit - niet uitzetten)',$leeftijd) }
    # 8 okt 2026: KLAAR terwijl de doelversie niet bereikt is (Surface bleef op 25H2, doel 26H2) is NIET groen: oranje 'klaar met voorbehoud', een mens beslist (plan sectie 4)
    $dv = "$($l.s['doel'])".Trim(); $wv = "$($l.s['windows'])".Trim()
    if ($dv -and $wv -and ($dv -ne $wv)) { return @('voorbehoud','KLAAR MET VOORBEHOUD',$leeftijd) }
    # 8 okt 2026: fabrikant-tool installeerde minder pakketten dan van toepassing waren (Lenovo: 17 van 26, o.a. BIOS en audio) = ook NIET groen
    if ("$($l.s['oem_pakketten'])" -match '^\s*(\d+) van (\d+)\s*$' -and [int]$Matches[1] -lt [int]$Matches[2]) { return @('voorbehoud', ('KLAAR MET VOORBEHOUD (fabrikant-pakketten ' + $Matches[1] + ' van ' + $Matches[2] + ')'), $leeftijd) }
    # 10 okt 2026 (Roderick: 'waarom runt hij niet volledig?'): Defender-handtekeningen ouder dan 7 dagen (status defender_dagen=, uit het bewijsrapport) is ook NIET groen
    $dd = -1; [void][int]::TryParse("$($l.s['defender_dagen'])".Trim(), [ref]$dd); if ($dd -gt 7) { return @('voorbehoud', ('KLAAR MET VOORBEHOUD (Defender-definities ' + $dd + ' dagen oud)'), $leeftijd) }
    # 9 okt 2026 (Roderick: 'de tijden zijn te kort, er moet iets niet kloppen - doe dit zelf, voor alle merken'): KLAAR is pas groen als de volledigheidscontrole tegen de FABRIKANT (scripts\180, status\volledigheid\<serie>.txt) 'bewezen' zegt; afwijking of niet-bewezen = oranje voorbehoud
    try {
        $vp = Join-Path $StatusDir ('volledigheid\' + (Safe-Name "$($l.serie)") + '.txt')
        if (Test-Path $vp) { $vk = Read-KV $vp; $vu = "$($vk['uitkomst'])"; if ($vu -and $vu -ne 'bewezen') { $vt = "$($vk['tekst'])"; if ($vt.Length -gt 90) { $vt = $vt.Substring(0, 90) + '...' }; return @('voorbehoud', ('KLAAR MET VOORBEHOUD (volledigheid ' + $vu + ': ' + $vt + ')'), $leeftijd) } }
    } catch {}    # 8 okt 2026 (Lenovo: Apparaatbeheer meldde na afloop nog 2 apparaten zonder driver): apparaatfouten > 0 is ook NIET groen
    $af = 0; [void][int]::TryParse("$($l.s['apparaatfouten'])".Trim(), [ref]$af); if ($af -gt 0) { return @('voorbehoud', ('KLAAR MET VOORBEHOUD (' + $af + ' apparaat/apparaten zonder driver)'), $leeftijd) }
    # 21 okt 2026 (Roderick: 'het dashboard geeft groen aan' terwijl de eindcontrole van de Lenovo 3 punten 'niet bewezen' had): KLAAR is ook pas groen als de eindcontrole (scripts\301, status\eindcontrole\<serie>\laatste.txt) van DEZE run geen [NIET BEWEZEN]-punt heeft. Een oude eindcontrole (kop ouder dan de runstart) telt niet. Uitzondering: 'Windows Update-geschiedenis op de laptop: 0 regels' (een verse installatie heeft per definitie geen geschiedenis) telt niet mee, anders is elke laptop oranje.
    try {
        $ep = Join-Path $StatusDir ('eindcontrole\' + (Safe-Name "$($l.serie)") + '\laatste.txt')
        if (Test-Path -LiteralPath $ep) {
            $eReg = @(Get-Content -LiteralPath $ep -Encoding UTF8 -ErrorAction Stop)
            $eTijd = [datetime]::MinValue; if ("$($eReg | Select-Object -First 1)" -match '(\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2})\s*$') { [void][datetime]::TryParse($Matches[1], [ref]$eTijd) }
            $rs = [datetime]::MinValue; [void][datetime]::TryParse("$($l.s['runstart'])".Trim(), [ref]$rs)
            if ($rs -gt [datetime]::MinValue -and $eTijd -ge $rs) {
                $nb = @($eReg | Where-Object { $_ -match '^\[NIET BEWEZEN\]' -and $_ -notmatch 'Windows Update-geschiedenis op de laptop: 0 regels' })
                if ($nb.Count -gt 0) { return @('voorbehoud', ('KLAAR MET VOORBEHOUD (eindcontrole: ' + $nb.Count + ' punt(en) niet bewezen)'), $leeftijd) }
            }
        }
    } catch {}
    return @('klaar','KLAAR',$leeftijd)
  }
  # 10 okt 2026 (verificatiestop voor Sysprep, audit-update v3.298): de laptop pauzeert tot de server de volledigheid heeft gecontroleerd (wacht) of tot een engineer hem controleert/vrijgeeft (stop, max 30 min)
  $vf = "$($l.s['verificatie'])".Trim()
  if ($vf -eq 'wacht' -and $leeftijd -le 300) { return @('bezig','WACHT OP VOLLEDIGHEIDSCONTROLE (server)',$leeftijd) }
  if ($vf -eq 'stop' -and $leeftijd -le 2400) { return @('stil','WACHT OP ENGINEER - controle vereist (zie laptop)',$leeftijd) }
  if ($l.herstart -and $leeftijd -le 1500) { return @('herstart',('HERSTART ' + (Fmt-Duur (($nu - [datetime]::Parse($l.herstartsinds)).TotalSeconds))),$leeftijd) }
  if ($leeftijd -le 300) { return @('bezig','BEZIG',$leeftijd) }
  if ($leeftijd -le 900) { return @('stil',("STIL " + (Fmt-Duur $leeftijd)),$leeftijd) }
  return @('weg',("GEEN SIGNAAL " + (Fmt-Duur $leeftijd)),$leeftijd)
}

function Get-Plan($l, $gem, $nu) {
  # geeft per stap: staat (klaar/bezig/todo/nvt), duur-tekst, en de verwachte resterende seconden
  $m = $l.m; $idx = $l.idx; $rest = 0.0; $rijen = @(); $volgende = $null; $huidig = $null
  for ($i = 0; $i -lt $Stappen.Count; $i++) {
    $st = $Stappen[$i]; $id = $st.id
    $gemSec = if ($gem.ContainsKey($id)) { [double]$gem[$id] } else { [double]$st.def * 60 }
    $gezien = $m.ContainsKey("stapstart_$id")
    $optKey = switch ($id) { 'store' {'opt_store'} 'software' {'opt_software'} 'branding' {'opt_branding'} default {$null} }
    $optWaarde = if ($optKey) { "$($l.s[$optKey])".Trim() } else { '' }
    $isOpt = $st.opt; if ($optWaarde -eq 'Ja') { $isOpt = $false } elseif ($optWaarde -eq 'Nee') { $isOpt = $true; $gezien = $false }
    $vlag = switch ($id) { 'oem' {$l.s['voltooid_oem']} 'wu' {$l.s['voltooid_wu']} 'feature' {$l.s['voltooid_feature']} default {$null} }
    if ($l.klaar) { $staat = if ($gezien -or -not $isOpt) { 'klaar' } else { 'nvt' } }
    elseif ($vlag -eq 'True' -and $i -ne $idx) { $staat = 'klaar' }
    elseif ($i -lt $idx) { $staat = if ($gezien -or -not $isOpt) { 'klaar' } else { 'nvt' } }
    elseif ($i -eq $idx) { $staat = 'bezig' }
    else { $staat = if ($optWaarde -eq 'Nee') { 'nvt' } elseif ($isOpt) { 'optie' } else { 'todo' } }
    $dtxt = ''
    if ($m.ContainsKey("stapduur_$id")) { $dtxt = Fmt-Duur ([double]$m["stapduur_$id"]) }
    elseif ($staat -eq 'bezig' -and $m.ContainsKey("stapstart_$id")) {
      $el = ($nu - [datetime]::Parse($m["stapstart_$id"])).TotalSeconds
      $dtxt = (Fmt-Duur $el) + ' bezig (verwacht ca. ' + (Fmt-Duur $gemSec) + ')'
      $rest += [math]::Max($gemSec - $el, 120)
      $huidig = @{ naam=$st.naam; el=$el; gem=$gemSec }   # tegel 'Nu bezig' (duur van de HUIDIGE stap, niet de eindtijd)
    }
    elseif ($staat -eq 'todo') { $dtxt = 'verwacht ca. ' + (Fmt-Duur $gemSec); $rest += $gemSec }
    elseif ($staat -eq 'optie') { $dtxt = 'indien van toepassing' }
    if ($staat -eq 'todo' -and -not $volgende) { $volgende = @{ naam=$st.naam; sec=$gemSec } }
    $rijen += @{ id=$id; naam=$st.naam; staat=$staat; dtxt=$dtxt }
  }
  if (-not $volgende -and -not $l.klaar) { $volgende = @{ naam='Klaar: laptop schakelt zichzelf uit'; sec=0 } }
  return @{ rijen=$rijen; rest=$rest; volgende=$volgende; huidig=$huidig }
}

function Get-OnderhoudHtml {
    # 12 okt 2026 (bewijs: het automatische servercache-onderhoud (scripts\187) deed van 11 okt tot 12 okt NIETS en niemand zag het): rode melding op het dashboard als status\onderhoud.txt ontbreekt, ouder is dan 15 min (hoort elke 5 min) of een stap met een foutcode meldt. Bij 'alles goed' komt er niets in beeld.
    try {
        $p = Join-Path $StatusDir 'onderhoud.txt'
        $txt = ''
        if (-not (Test-Path -LiteralPath $p)) { $txt = 'het onderhoud van de server heeft nog nooit een resultaat gemeld (status\onderhoud.txt ontbreekt)' }
        else {
            $kv = @{}; $stappen = @()
            foreach ($l in @(Get-Content -LiteralPath $p -Encoding UTF8 -ErrorAction SilentlyContinue)) { if ("$l" -match '^\s*stap\s*=\s*(.+)$') { $stappen += $Matches[1].Trim() } elseif ("$l" -match '^\s*([^=#\s][^=]*)=(.*)$') { $kv[$Matches[1].Trim()] = $Matches[2].Trim() } }
            $gec = [datetime]::MinValue
            if (-not [datetime]::TryParse("$($kv['gecontroleerd'])", [ref]$gec)) { $txt = 'status\onderhoud.txt is onleesbaar' }
            else {
                $min = ((Get-Date) - $gec).TotalMinutes
                $kort = @($stappen | Where-Object { $_ -match 'exitcode (?!0( |$))' })
                if ($min -gt 15) {
                    # 16 okt 2026 (Roderick: 'server onderhoud werkt niet goed'; bewijs: ronde 187 van 14:35 liep nog om 14:53 - de stap 171 verzoeken duurde 843 s in de vorige ronde, dit is werk aan nieuwe drivers van de HP, geen storing): draait er NU een onderhoudsproces dat korter dan 90 min bezig is, dan is het geen storing maar een lange ronde (oranje i.p.v. rood, met starttijd en lopende stap).
                    $lopend = $null; try { $lopend = @(Get-CimInstance Win32_Process -Filter "Name='powershell.exe'" -ErrorAction Stop | Where-Object { $_.CommandLine -match '187-server-cache-onderhoud' } | Sort-Object CreationDate | Select-Object -First 1)[0] } catch {}
                    if ($lopend -and ((Get-Date) - $lopend.CreationDate).TotalMinutes -lt 90) {
                        $sub = ''; try { $kind = @(Get-CimInstance Win32_Process -Filter "ParentProcessId=$($lopend.ProcessId)" -ErrorAction Stop | Where-Object { $_.CommandLine -match '(\d{3})-[a-z\-]+\.ps1' } | Select-Object -First 1)[0]; if ($kind) { $sub = ' (bezig met stap ' + [regex]::Match($kind.CommandLine, '\d{3}-[a-z\-]+\.ps1').Value + ')' } } catch {}
                        return '<div class="tegel" style="margin:6px 0;border-left:6px solid #d97706;background:#fef3c7;color:#78350f"><b>Server-onderhoud: lange ronde bezig</b> sinds ' + $lopend.CreationDate.ToString('HH:mm') + $sub + ' - de vorige ronde klaar om ' + $gec.ToString('HH:mm') + '. Geen storing zolang dit proces draait.</div>'
                    }
                    $txt = "de laatste onderhoudsronde was $([int]$min) minuten geleden (hoort elke 5 minuten) en er draait geen onderhoudsproces"
                }
                elseif ($kv['uitkomst'] -ne 'ok') { $txt = 'de laatste onderhoudsronde had een fout: ' + ($kort -join '; ') }
                else { return '' }
            }
        }
        return '<div class="tegel" style="margin:12px 0;border-left:6px solid #b42318;background:#fee4e2;color:#7a271a"><b>Server-onderhoud werkt niet goed</b><br>' + (E $txt) + '. Nieuwe drivers en updates komen dan niet automatisch in de cache.</div>'
    } catch { return '' }
}
function Get-KennisbankHtml {
    # 22 okt 2026 (Roderick: 'die kennisbank moet 24/7 aan staan'; zelfde les als 12 okt, onderhoud.txt): rode melding als de kennisbank-dienst (kennisbank\kb-dienst.ps1) niets meldt, een hartslag ouder dan 3 min heeft, op de SSD wacht, of een integriteitsfout meldt; oranje als de laatste backup ouder is dan 12 uur. Bij 'alles goed' komt er niets in beeld.
    try {
        $p = Join-Path $StatusDir 'kennisbank-dienst.txt'; $txt = ''; $kleur = 'rood'
        if (-not (Test-Path -LiteralPath $p)) { $txt = 'de kennisbank-dienst heeft nog nooit gemeld (status\kennisbank-dienst.txt ontbreekt; taak Esprit-Kennisbank)' }
        else {
            $kv = @{}; foreach ($l in @(Get-Content -LiteralPath $p -Encoding UTF8 -ErrorAction SilentlyContinue)) { if ("$l" -match '^\s*([^=#\s][^=]*)=(.*)$') { $kv[$Matches[1].Trim()] = $Matches[2].Trim() } }
            $t = [datetime]::MinValue
            if (-not [datetime]::TryParse("$($kv['tijd'])", [ref]$t)) { $txt = 'status\kennisbank-dienst.txt is onleesbaar' }
            else {
                $min = ((Get-Date) - $t).TotalMinutes
                if ($min -gt 3) { $txt = "de laatste hartslag was $([int]$min) minuten geleden (hoort elke 10 seconden); de dienst draait niet" }
                elseif ($kv['status'] -ne 'OK') { $txt = "de database is niet bereikbaar (status $($kv['status']); is de SSD aangesloten?)" }
                elseif ("$($kv['laatste_integriteit'])" -match '\S' -and "$($kv['laatste_integriteit'])" -notmatch ' ok$') { $txt = 'de integriteitscontrole van de database gaf een afwijking: ' + $kv['laatste_integriteit'] }
                else {
                    $bt = [datetime]::MinValue
                    if (-not [datetime]::TryParse("$($kv['laatste_backup'])", [ref]$bt) -or ((Get-Date) - $bt).TotalHours -gt 12) { $txt = 'de laatste backup van de kennisbank is ouder dan 12 uur of onbekend'; $kleur = 'oranje' }
                    else { return '' }
                }
            }
        }
        if ($kleur -eq 'oranje') { return '<div class="tegel" style="margin:6px 0;border-left:6px solid #d97706;background:#fef3c7;color:#78350f"><b>Kennisbank</b><br>' + (E $txt) + '.</div>' }
        return '<div class="tegel" style="margin:12px 0;border-left:6px solid #b42318;background:#fee4e2;color:#7a271a"><b>Kennisbank werkt niet goed</b><br>' + (E $txt) + '. Fouten en oplossingen worden dan niet vastgelegd of opgezocht.</div>'
    } catch { return '' }
}
function Maak-Unit([string]$html) {
  # 13 okt 2026 (Roderick): het dashboard spreekt van 'unit' i.p.v. 'laptop' (ook voor telefoons en tablets). Alleen de ZICHTBARE tekst tussen de tags wordt aangepast; bestandsnamen (laptop-<serie>.html), links en code blijven zoals ze zijn.
  return [regex]::Replace($html, '>([^<>]+)<', {
    param($m)
    $x = $m.Groups[1].Value
    if ($x -match '[{};]' -or $x -notmatch '(?i)laptop') { return $m.Value }
    $x = $x -creplace 'Laptops', 'Units' -creplace 'laptops', 'units' -creplace 'LAPTOPS', 'UNITS' -creplace 'Laptop', 'Unit' -creplace 'laptop', 'unit' -creplace 'LAPTOP', 'UNIT'
    return '>' + $x + '<'
  })
}function Build-InstDetail($k, $stp, $van, $pct, $verw, $live, $badge, $nu, $leeftijd) {
  # 12 okt 2026 (Roderick: 'als ik op de laptop klik, geeft hij nog een oude versie'): tijdens de WinPE-installatie bestaat er nog geen statusbestand van audit-update, dus de tegel/rij linkte naar de detailpagina van de VORIGE run (KLAAR van 10 okt, elke 5 min opnieuw gemaakt uit oude gegevens). Nu krijgt een laptop die op dit moment installeert een EIGEN detailpagina met de live installatiestatus; de vorige run blijft bereikbaar via het rapportlink.
  $namen = @('Controleren: UEFI, interne schijf, server', 'ISO-share en Windows-editie controleren', 'Aftellen (15 s) voor het wissen van de schijf', 'Schijf wissen en partities maken', 'Windows 11 op de schijf zetten (dism, duurt enkele minuten)', 'Opstartbestanden, herstelpartitie en modelspecifieke drivers', 'KLAAR: Windows staat erop, nu klaarzetten voor Audit Mode')
  $sn = "$($k['serie'])"
  $html = "<!doctype html><html lang=""nl""><head><meta charset=""utf-8"">$LiveHead<meta name=""viewport"" content=""width=device-width,initial-scale=1""><title>Laptop $(E $sn)</title><style>$css</style></head><body><div class=""wrap"">"
  $html += "<p><a href=""dashboard.html"">&larr; alle laptops</a></p><h1>$(E $sn) $badge</h1>"
  $html += "<div class=""voorbehoud-uitleg"">Dit is de <b>nieuwe installatie</b> via het netwerk (WinPE). Er is nog geen melding van Windows zelf; zodra Windows start, wordt dit de gewone detailpagina. $(Get-MerkModel $sn)</div>"
  $html += "<div class=""sub"">locatie $(E (Get-Locatie $sn)) &middot; modus $(E $k['modus']) &middot; bijgewerkt $($nu.ToString('HH:mm:ss')) &middot; laatste melding van de laptop $(Fmt-Duur $leeftijd) geleden</div>"
  $html += "<div class=""tegels""><div class=""tegel""><b>stap $stp van $van</b>$(E $k['stapnaam'])</div><div class=""tegel""><b>$(Fmt-Duur ($nu - $k['sinds']).TotalSeconds)</b>al in deze stap</div>$(if ($verw) { '<div class=""tegel""><b>' + (E $verw) + '</b>verwachting</div>' })$(if ($live) { '<div class=""tegel""><b>' + (E $live) + '</b>ophalen van het image</div>' })<div class=""tegel""><b>$pct%</b>van de installatiestappen</div></div>"
  $html += "<div class=""sub"" style=""margin-top:12px"">Schijf: $(E $k['schijf']) &middot; editie: $(E $k['editie']) &middot; laatste melding: <b>$(E $k['laatste'])</b></div>"
  $html += "<h2>Wat is gedaan en wat moet nog</h2><div class=""kaart"">"
  for ($i = 1; $i -le $namen.Count; $i++) {
    $cls = if ($i -lt $stp) { 'klaar' } elseif ($i -eq $stp) { 'bezig' } else { 'todo' }
    $ic = if ($i -lt $stp) { '&#10004;' } elseif ($i -eq $stp) { '&#9654;' } else { '&#9675;' }
    $nm = if ($i -eq $stp -and "$($k['stapnaam'])".Trim()) { "$($k['stapnaam'])" } else { $namen[$i - 1] }
    $html += "<div class=""stap $cls""><span class=""ic"">$ic</span><span>$(E $nm)</span><span class=""t""></span></div>"
  }
  $html += "</div>"
  try { $rp = @(Get-ChildItem (Join-Path $ScriptFolder 'rapporten') -Filter "${sn}__*.html" -ErrorAction SilentlyContinue | Sort-Object LastWriteTime -Descending | Select-Object -First 1); if ($rp.Count -gt 0) { $html += "<h2>Vorige run van deze laptop</h2><p><a href=""rapporten/$(E $rp[0].Name)"">Rapport van de vorige run ($($rp[0].LastWriteTime.ToString('yyyy-MM-dd HH:mm')))</a> &middot; <a href=""historie.html"">Historie van alle runs</a></p>" } } catch {}
  $html += "</div></body></html>"
  return $html
}
function Get-RunVerloopHtml($l) {
  # 13 okt 2026 (Roderick: 'laat ook wat er nu gebeurt op de achtergrond staan, de stappen, zodat ik kan zien wat er gebeurd is in een totale run'): het HELE verloop van deze run uit het consolelog van de laptop (status\console\<serie>.txt), alleen de belangrijke regels (fasen, extra-updates, oogst, verificatie, installaties, herstarts), in volgorde van oud naar nieuw. Geen inklapbaar blok en geen schuifbalk: de pagina ververst zichzelf en zou die stand kwijtraken. Het volledige log staat als tekstbestand ernaast (verloop-<serie>.txt) en per run bewaard in historie (console-<serie>__<runkey>.log), ook nadat een nieuwe run begint.
  try {
    $f = Join-Path (Join-Path $StatusDir 'console') "$($l.serie).txt"
    if (-not (Test-Path -LiteralPath $f)) { return '' }
    $alle = @(Get-Content -LiteralPath $f -Encoding UTF8 -ErrorAction Stop)
    try { [IO.File]::WriteAllLines((Join-Path $ScriptFolder "verloop-$($l.serie).txt"), [string[]]$alle, $utf8) } catch {}
    try { if ($l.m['runkey']) { $snap = Join-Path $RunsDir ("console-{0}__{1}.log" -f $l.serie, ($l.m['runkey'] -replace '[^A-Za-z0-9_-]', '')); [IO.File]::WriteAllLines($snap, [string[]]$alle, $utf8) } } catch {}
    $belangrijk = '--- FASE|\[extra\]|\[oogst\]|\[VERIFICATIE\]|STOP VOOR|\[LET OP\]|MISLUKT|KLAAR|Doelversie|Totale duur|geinstalleerd|Geinstalleerd|herstart|Sysprep|BEWEZEN|Wi-Fi|Defender|MSRT|Security platform|Lenovo|Windows Update|WSUS|online aangeboden|nog open|bewezen'
    $sel = @($alle | Where-Object { $_ -match '^\d{4}-\d\d-\d\d \d\d:\d\d:\d\d' -and $_ -notmatch '\[bewijs\]' -and $_ -match $belangrijk })
    if ($sel.Count -eq 0) { return '' }
    $totaal = $sel.Count; if ($sel.Count -gt 600) { $sel = @($sel | Select-Object -Last 600) }
    $rij = foreach ($r in $sel) { $tijd = $r.Substring(11, 8); $tekst = $r.Substring(19).Trim(); if ($tekst.Length -gt 230) { $tekst = $tekst.Substring(0, 230) + '...' }; "<tr><td style=""white-space:nowrap"">$(E $tijd)</td><td><small>$(E $tekst)</small></td></tr>" }
    $kop = "Wat er in deze run gebeurd is ($totaal belangrijke regels, oud naar nieuw; tijd = klok van de laptop)"
    return "<h2>$kop</h2><div class=""sub"">Blijft staan tot een nieuwe run van deze laptop begint; daarna is deze run bewaard in de historie. <a href=""verloop-$(E $l.serie).txt"">Het volledige log (alle regels) als tekstbestand</a></div><table><thead><tr><th>Tijd</th><th>Wat</th></tr></thead><tbody>" + ($rij -join '') + "</tbody></table>"
  } catch { return '' }
}
# 17 okt 2026: kop-script voor de actieknoppen (alleen op de server zelf zichtbaar)
$ActieKop = '<style>.alleen-lokaal{display:none}.lokaal .alleen-lokaal{display:block}</style><script>(function(){var h=location.hostname;if(h==="localhost"||h==="127.0.0.1")document.documentElement.className+=" lokaal"})();window.espritActie=function(a,sn){var t=(a==="vrij")?"Laptop "+sn+" VRIJGEVEN? Hij gaat dan door naar Sysprep.":"Laptop "+sn+" STOPPEN? Hij stopt netjes binnen ongeveer 30 seconden.";var o=document.createElement("div");o.style.cssText="position:fixed;top:0;left:0;right:0;bottom:0;background:rgba(0,0,0,.55);z-index:99999;display:flex;align-items:center;justify-content:center";var b=document.createElement("div");b.style.cssText="background:#fff;color:#111;padding:20px;border-radius:8px;min-width:320px;font:16px sans-serif";var p=document.createElement("p");p.textContent=t+" Toegangscode:";var i=document.createElement("input");i.type="password";i.setAttribute("inputmode","numeric");i.setAttribute("autocomplete","off");i.style.cssText="font-size:20px;width:100%;box-sizing:border-box;margin:8px 0 12px;padding:6px";var ok=document.createElement("button");ok.textContent="Bevestigen";var an=document.createElement("button");an.textContent="Annuleren";[ok,an].forEach(function(x){x.style.cssText="font-size:16px;padding:8px 16px;margin-right:8px"});function tonen(x){var u=document.getElementById("actie-uitslag");if(u)u.textContent=x;var t=document.getElementById("esprit-toast");if(!t){t=document.createElement("div");t.id="esprit-toast";t.style.cssText="position:fixed;left:50%;bottom:24px;transform:translateX(-50%);background:#111;color:#fff;padding:14px 22px;border-radius:8px;font:16px sans-serif;z-index:99999;max-width:80%";document.documentElement.appendChild(t)}t.textContent=x;clearTimeout(window.espritT);window.espritT=setTimeout(function(){if(t.parentNode)t.parentNode.removeChild(t)},20000)}function sluit(){if(o.parentNode)o.parentNode.removeChild(o)}an.onclick=sluit;ok.onclick=function(){var c=i.value;sluit();tonen("bezig...");fetch("http://127.0.0.1:8095/actie",{method:"POST",headers:{"Content-Type":"text/plain"},body:a+"|"+sn+"|"+c}).then(function(r){return r.text().then(tonen)}).catch(function(){tonen("Het acties-proces (270) is niet bereikbaar op de server.")})};i.onkeydown=function(e){if(e.key==="Enter")ok.onclick()};b.appendChild(p);b.appendChild(i);b.appendChild(ok);b.appendChild(an);o.appendChild(b);document.documentElement.appendChild(o);i.focus()};</script>'
function Get-Eindcontrole($l) {
  # 19 okt 2026 (Roderick: 'ja doemaar'): resultaat van scripts\301 (onafhankelijke eindcontrole na KLAAR) tonen. Alleen LEZEN van status\eindcontrole\<serie>\laatste.txt; verzint niets.
  # Geeft $null als er geen rapport is. kort = 1 regel voor de hoofdtabel, html = blok voor de laptoppagina. 'oud' = rapport is ouder dan de laatste melding van de laptop (hoort dan niet bij deze run).
  try {
    $f = Join-Path (Join-Path (Join-Path $StatusDir 'eindcontrole') (Safe-Name $l.serie)) 'laatste.txt'
    if (-not (Test-Path -LiteralPath $f)) { return $null }
    $regels = @(Get-Content -LiteralPath $f -Encoding UTF8 -ErrorAction SilentlyContinue); if ($regels.Count -eq 0) { return $null }
    $fm = (Get-Item -LiteralPath $f).LastWriteTime; $oud = $false
    try { if ($l.klaar -and $fm -lt ((Get-Item $l.pad).LastWriteTime).AddSeconds(-5)) { $oud = $true } } catch {}
    $sam = ($regels | Where-Object { $_ -match '^SAMENVATTING:' } | Select-Object -Last 1); if (-not $sam) { return $null }
    $nb = @($regels | Where-Object { $_ -match '^\[NIET BEWEZEN\]' })
    $kort = ($sam -replace '^SAMENVATTING:\s*', 'Eindcontrole: ') + $(if ($oud) { ' (van een eerdere run)' } else { '' })
    $h = "<h2>Eindcontrole (onafhankelijk, automatisch na afronden)</h2><div class=""kaart""><div class=""sub""><b>$(E $sam)</b> &middot; gemaakt $($fm.ToString('yyyy-MM-dd HH:mm'))$(if ($oud) { ' &middot; <b>let op: ouder dan de laatste melding van de laptop, hoort bij een eerdere run</b>' })</div>"
    if ($nb.Count -gt 0) { $h += '<ul>'; foreach ($x in ($nb | Select-Object -First 14)) { $h += "<li><small>$(E (($x -replace '^\[NIET BEWEZEN\]\s*', '')))</small></li>" }; if ($nb.Count -gt 14) { $h += "<li><small>... en nog $($nb.Count - 14) (zie het hele rapport)</small></li>" }; $h += '</ul>' }
    $h += "<details><summary>Hele rapport</summary><pre style=""white-space:pre-wrap;font-size:12px"">$(E ($regels -join "`n"))</pre></details><small>Bron: $(E $f). BEWEZEN = teruggelezen op de laptop of in Windows' eigen logboek; NIET BEWEZEN = geen bewijs, dat is geen bewezen fout.</small></div>"
    return @{ kort = $kort; html = $h; nb = $nb.Count; oud = $oud }
  } catch { return $null }
}
function Build-Detail($l, $gem, $nu, $kl) {
  $s = $l.s; $m = $l.m; $plan = Get-Plan $l $gem $nu
  $hg = Get-HerstartGem; $herstartTegel = ''; $aantalH = @($m.Keys | ? { $_ -like 'herstartduur_*' }).Count
  if ($l.herstart) { $el = ($nu - [datetime]::Parse($l.herstartsinds)).TotalSeconds; $plan.rest += [math]::Max($hg - $el, 60); $herstartTegel = "<div class=""tegel""><b>$(Fmt-Duur $el)</b>herstart bezig sinds $(([datetime]::Parse($l.herstartsinds)).ToString('HH:mm')) (gemiddeld ca. $(Fmt-Duur $hg))</div>" }
  $start = $(if ($m['startserver']) { [datetime]::Parse($m['startserver']) } else { [datetime]::Parse($m['start']) + (Get-KlokVerschil $l) }); $eind = $nu; if ($l.klaar) { try { $eind = (Get-Item $l.pad).LastWriteTime } catch {} }; $verstreken = [math]::Max(0, ($eind - $start).TotalSeconds)   # 8 okt 2026: bij een KLAAR laptop stopt de totale tijd bij de laatste melding (liep door tot nu)
  $html = "<!doctype html><html lang=""nl""><head><meta charset=""utf-8"">$LiveHead<meta name=""viewport"" content=""width=device-width,initial-scale=1""><title>Laptop $(E $l.serie)</title><style>$css</style>$ActieKop</head><body><div class=""wrap"">"
  $html += "<p><a href=""dashboard.html"">&larr; alle laptops</a></p><div class=""kop-$($kl[0])""><h1>$(E $l.serie) <span class=""badge $($kl[0])"">$(E $kl[1])</span></h1></div>"
  # 17 okt 2026 (Roderick: 'kan ik dat ook doen via het dashboard ... en de stop knop'): knoppen Vrijgeven en STOP, ALLEEN zichtbaar als het dashboard op de server zelf (localhost) wordt bekeken; ze praten met scripts\270-dashboard-acties.ps1 (alleen 127.0.0.1, vaste acties).
  try { $vfK = "$($s['verificatie'])".Trim(); $snK = ("$($l.serie)" -replace '[^A-Za-z0-9_-]', ''); $knop = ''; if ($vfK -in @('wacht', 'stop')) { $knop += '<button type="button" onclick="espritActie(''vrij'',''' + $snK + ''')" style="font-size:16px;padding:10px 18px;margin-right:10px;background:#15803d;color:#fff;border:0;border-radius:6px;cursor:pointer">Vrijgeven (door naar Sysprep)</button>' }; if (-not $l.klaar) { $knop += '<button type="button" onclick="espritActie(''stop'',''' + $snK + ''')" style="font-size:16px;padding:10px 18px;background:#b42318;color:#fff;border:0;border-radius:6px;cursor:pointer">STOP laptop</button>' }; if ($knop) { $html += '<div class="alleen-lokaal" style="margin:10px 0">' + $knop + '<div id="actie-uitslag" style="margin-top:8px;font-weight:600"></div></div>' } } catch {}
  if ($kl[0] -eq 'voorbehoud') { $html += "<div class=""voorbehoud-uitleg"">Deze laptop is verwerkt en uitgezet, <b>maar de doelversie is niet bereikt</b>: Windows staat op $(E $s['windows']) (build $(E $s['build'])) terwijl $(E $s['doel']) bedoeld was. Een mens beslist wat er nu gebeurt (opnieuw proberen of zo leveren). De reden staat in het logboek hieronder.</div>" }
  try { $vbD = Get-Verbinding $s; if ($vbD.txt) { $html += "<div class=""verbinding vb-$($vbD.klas)""><b>Verbinding van de laptop:</b> $($vbD.txt)$(if ($vbD.tip) { ' &mdash; ' + (E $vbD.tip) })</div>" } } catch {}
  $html += "<div class=""sub"">$(E $s['klant']) &middot; order $(E $s['ordernummer']) &middot; engineer $(E $s['engineer']) &middot; tool v$(E $s['versie']) &middot; batterij $(E $s['accu'])% ($(if ($s['lader'] -eq 'ja') {'lader'} else {'accu'})) &middot; herstarts $($l.herstarts) &middot; mislukt $(E $s['mislukt']) &middot; PXE-overslaan (BootNext): $(E $(if ($s['bootnext']) { $s['bootnext'] } else { 'onbekend (tool < v3.252)' })) &middot; bijgewerkt $($nu.ToString('HH:mm:ss'))</div>"
  try { $ecD = Get-Eindcontrole $l; if ($ecD) { $html += $ecD.html } } catch {}
  $eta = if ($l.klaar) { 'afgerond' } else { 'verwacht klaar rond ' + $nu.AddSeconds($plan.rest).ToString('HH:mm') + ' (nog ca. ' + (Fmt-Duur $plan.rest) + ')' }
  $html += "<div class=""tegels"">$(if ("$($l.s['oem'])".Trim()) { '<div class=""tegel""><b>' + (E "$($l.s['oem'])".Trim()) + '</b>Fabrikant-updates (drivers/BIOS)</div>' })$(if (Get-WindowsTekst $l.s) { '<div class=""tegel""><b>' + (Get-WindowsTekst $l.s) + '</b>Windows-versie</div>' })$(if (Get-Locatie $l.serie) { '<div class=""tegel""><b>' + (E (Get-Locatie $l.serie)) + '</b>locatie</div>' })$(if (-not $l.klaar -and $plan.huidig) { $h = $plan.huidig; $over = $h.el - $h.gem; '<div class=""tegel""><b>' + (E $h.naam) + '</b>nu bezig: al ' + (Fmt-Duur $h.el) + ', verwacht ca. ' + (Fmt-Duur $h.gem) + $(if ($over -lt 0) { ', nog ca. ' + (Fmt-Duur (-$over)) } else { ' - duurt langer dan gemiddeld' }) + '</div>' })<div class=""tegel""><b>$(Fmt-Duur $verstreken)</b>verstreken sinds start ($($start.ToString('HH:mm')))</div><div class=""tegel""><b>$(E $eta)</b>planning</div>$herstartTegel$(if (-not $l.klaar -and $plan.volgende) { '<div class=""tegel""><b>' + (E $plan.volgende.naam) + '</b>volgende stap' + $(if ($plan.volgende.sec -gt 0) { ', verwacht ca. ' + (Fmt-Duur $plan.volgende.sec) } else { '' }) + '</div>' })<div class=""tegel""><b>$($l.herstarts)</b>herstarts tot nu toe$(if ($aantalH -gt 0) { " (gemeten: $aantalH)" })</div></div>"
  $laatsteTxt = "$($s['laatste'])".Trim(); $laatsteTijd = ""; try { $laatsteTijd = ([datetime]::ParseExact("$($s['tijd'])".Trim(),'yyyy-MM-dd HH:mm:ss',$null).ToLocalTime()).ToString('HH:mm:ss') } catch {}
  if ($laatsteTxt) { $html += "<div class=""sub"" style=""margin-top:12px"">Laatste melding van de laptop$(if ($laatsteTijd) { " (" + $laatsteTijd + ")" }): <b>$(E $laatsteTxt)</b></div>" }
  $html += (Get-LiveLogHtml $l.serie)
  $html += "<h2>Wat is gedaan en wat moet nog</h2><div class=""kaart"">"
  foreach ($r in $plan.rijen) {
    $ic = switch ($r.staat) { 'klaar' {'&#10004;'} 'bezig' {'&#9654;'} 'nvt' {'&ndash;'} 'optie' {'&#9675;'} default {'&#9675;'} }
    $cls = switch ($r.staat) { 'klaar' {'klaar'} 'bezig' {'bezig'} default {'todo'} }
    $html += "<div class=""stap $cls""><span class=""ic"">$ic</span><span>$(E $r.naam)</span><span class=""t"">$(E $r.dtxt)</span></div>"
  }
  $html += "</div>" + (Get-RunVerloopHtml $l) + "<h2>Gebeurtenissen (deze run, server-tijd)</h2><table><thead><tr><th>Tijd</th><th>Type</th><th>Herstarts</th><th>Melding</th><th>Accu</th></tr></thead><tbody>"
  $logPad = Join-Path $HistDir "$($l.serie).log"
  $regels = @(); if (Test-Path $logPad) { $regels = @(Get-Content $logPad -Encoding UTF8) }
  $tail = if ($regels.Count -gt 150) { $regels[($regels.Count-150)..($regels.Count-1)] } else { $regels }
  [array]::Reverse($tail)
  foreach ($r in $tail) { $v = $r -split "`t"; if ($v.Count -ge 4) { $html += "<tr><td>$(E $v[0])</td><td><b>$(E $v[2])</b></td><td>$(E $v[1])</td><td><small>$(E $v[3])</small></td><td>$(E $(if ($v.Count -gt 4) {$v[4]}))</td></tr>" } }
  $html += "</tbody></table><h2>Eerdere runs van deze laptop</h2><table><thead><tr><th>Start</th><th>Einde</th><th>Duur</th><th>Reden</th><th>Bestand</th></tr></thead><tbody>"
  $heeft = $false
  foreach ($f in (Get-ChildItem $RunsDir -Filter "$($l.serie)__*.txt" -ErrorAction SilentlyContinue | Sort-Object LastWriteTime -Descending)) {
    $k = Read-KV $f.FullName; $heeft = $true
    $d = ''; $bsRun = $(if ($k['startserver']) { $k['startserver'] } else { $k['start'] }); try { $d = Fmt-Duur (([datetime]::Parse($k['einde']) - [datetime]::Parse($bsRun)).TotalSeconds) } catch {}
    $html += "<tr><td>$(E $(try {[datetime]::Parse($bsRun).ToString('yyyy-MM-dd HH:mm')} catch {''}))</td><td>$(E $(try {[datetime]::Parse($k['einde']).ToString('yyyy-MM-dd HH:mm')} catch {''}))</td><td>$d</td><td>$(E $k['reden'])</td><td><small>$(E $f.Name)</small></td></tr>"
  }
  if (-not $heeft) { $html += "<tr><td colspan=""5"" class=""leeg"">Nog geen afgeronde runs bewaard. Zodra deze run klaar is, verschijnt hij hier met zijn tijden per stap.</td></tr>" }
  $html += "</tbody></table><div class=""voet"">Alles wordt bewaard in $(E $HistDir) en $(E $RunsDir). Verwachte tijden gebruiken het gemiddelde van afgeronde runs; zolang die er niet zijn, standaardwaarden uit de run van 2 oktober 2026 (totaal 1u 38m).</div></div></body></html>"
  return $html
}

$Script:BrowserGeopend = $false
# 6 okt 2026: de klok van een laptop kan afwijken van de server (bv. na een lege klokbatterij; Surface 033135623657 liep 18,6 min achter, dashboard toonde 24 i.p.v. 5 min). Verschil = ontvangsttijd op de server - tijd die de laptop zelf in het bestand schrijft ('tijd=' is UTC). Alleen als het > 30 s en < 12 u is, anders 0.
function Get-KlokVerschil($l) {
  try {
    if (-not $l.s['tijd'] -or -not $l.pad) { return [timespan]::Zero }
    $lt = [datetime]::SpecifyKind([datetime]::ParseExact(("$($l.s['tijd'])").Trim(), 'yyyy-MM-dd HH:mm:ss', $null), 'Utc').ToLocalTime()
    $d = (Get-Item $l.pad).LastWriteTime - $lt
    if ([math]::Abs($d.TotalSeconds) -gt 30 -and [math]::Abs($d.TotalHours) -lt 12) { return $d }
  } catch {}
  return [timespan]::Zero
}
# 8 okt 2026 (Roderick): NETWERK-PANEEL - inkomend en uitgaand verkeer van de server apart, grafiekje van de laatste 10 min, welke laptop(s) nu verbonden zijn en hoeveel van de kabel wordt gebruikt
# ('we hebben een gig poort, maar 20-30 MB/s is dan niet veel'). Alleen LEZEN (Get-NetAdapter/-Statistics, Get-NetTCPConnection: geen beheerrechten nodig). Per-laptop-verdeling van de bytes is zonder beheerrechten NIET apart meetbaar:
# het paneel toont het TOTAAL van de server-kabel en noemt welke laptop(s) daarop nu verbonden zijn (bij 1 actieve laptop is het verkeer van die laptop).
# 8 okt 2026 (Roderick): snelheid/verbinding PER PLEK. De laptop meldt zelf (audit-update v3.280): nic_mbps (onderhandelde snelheid van zijn netwerkkaart), nic_fouten, down_mbs, up_mbs (korte meting naar de server).
# Een geknikte kabel valt op als LINK (bv. 100 Mbit i.p.v. 1 Gbit), PAKKETFOUTEN of een lage meting.
# 8 okt 2026 (Roderick): per plek LIVE ophalen (down, blauw, boven) en terugsturen (up, oranje, onder). Laptop met audit-update >= v3.281 meldt live_down_mbs/live_up_mbs (eigen netwerkkaart). Tijdens WinPE-installatie (geen
# meldingen van de laptop): als PRECIES 1 laptop installeert, geldt het verkeer van de server-kabel voor die laptop (naar de laptop = down, van de laptop = up); bij meerdere tegelijk niets tonen (niet te verdelen).
function Get-TotaalTijdHtml($ti) {
  # 13 okt 2026 (Roderick): per plek de TOTALE tijd sinds de eerste verbinding met de server (bij een klare unit: tot het einde)
  try { if ($ti.start -and $ti.eind) { $sec = ([datetime]$ti.eind - [datetime]$ti.start).TotalSeconds; if ($sec -ge 0 -and $sec -lt 172800) { return '<span class="pk-tot">totaal ' + (E (Fmt-Duur $sec)) + ' sinds eerste verbinding</span>' } } } catch {}
  return ''
}
# 15 okt 2026 (Roderick: 'op de plek: wat installeert hij? dat geeft hij niet aan'): de laatste melding van de laptop (laatste=) als korte tekst onder de balk. Geen eigen verzinsel: alleen wat de laptop zelf in zijn logboek meldt, opgeschoond en ingekort.
function Get-NuTekst($raw) {
  # 20 okt 2026 (Roderick: 'wat er nu allemaal gezegd wordt op de plek, snapt niemand, alleen codes, graag in nederlands en voor domme mensen'): de laatste melding van de laptop vertaald naar GEWOON NEDERLANDS. Eerste regel die past wint.
  # Een onbekende melding toont NOOIT de ruwe logregel (geen codes, geen bestandsnamen, geen versies), maar een neutrale zin. De ruwe tekst staat op de pagina van de laptop.
  try {
    $t = "$raw".Trim(); if (-not $t) { return '' }
    $regels = @(
      @('STOP VOOR SYSPREP|volledigheid niet bewezen|WACHT OP ENGINEER|controle vereist', 'STOP: de server kon niet bewijzen dat alles is bijgewerkt. Een medewerker moet kijken; anders gaat hij na 30 minuten zelf door (met voorbehoud).'),
      @('VERIFICATIE.*BEWEZEN door de server', 'Controle geslaagd: de server ziet dat alles is bijgewerkt'),
      @('VERIFICATIE|vlak voor Sysprep|server vergelijkt', 'Laatste controle: de server vergelijkt de laptop met wat de fabrikant en Microsoft aanbieden'),
      @('Opruimen|Heartbeat-bestand|Verwijderd\s*:|Niet aanwezig\s*:|Sysprep', 'Laatste stappen: opruimen en klaarzetten voor de klant'),
      @('TPM', 'Controleert de beveiligingschip (TPM)'),
      @('secureboot|Secure Boot', 'Controleert de beveiligingsinstelling Secure Boot (alleen meten)'),
      @('logs-alles|evtx|oem-rapport|hpia-rapport|logbestand', 'Stuurt logbestanden naar de server (alleen kopieren)'),
      @('\[bewijs\]|SOFTWAREDEVICE|SOFTWARECOMPONENT|SYSTEM\s*\||NET\s*\||DISPLAY\s*\||MEDIA\s*\||BLUETOOTH\s*\||HIDCLASS|PROGRAMMA |APP |geinstalleerde KB|Windows Update-geschiedenis|Defender: handtekeningen', 'Legt vast welke drivers en programma''s op de laptop staan (voor het rapport)'),
      @('MSRT', 'Installeert het virusverwijderprogramma van Microsoft (MSRT)'),
      @('Security platform', 'Werkt het Windows-beveiligingsonderdeel bij'),
      @('Defender', 'Werkt Windows Defender (virusbescherming) bij'),
      @('\[voortgang\]|repository kopieren', 'Kopieert de driverbestanden van de server naar de laptop'),
      @('Snelle check', 'Controleert of er drivers of firmware nodig zijn (zonder alles te kopieren)'),
      @('ThinInstaller|Lenovo System|SystemUpdate|Lenovo', 'Installeert drivers en firmware van Lenovo'),
      @('HP Image Assistant|HPIA', 'Installeert drivers en firmware van HP'),
      @('Dell Command|dcu-cli', 'Installeert drivers en firmware van Dell'),
      @('Surface', 'Installeert de updates van Microsoft Surface'),
      @('Repository lokaal|lokaal kopi', 'Kopieert de driverbestanden van de server naar de laptop'),
      @('Windows Search|TiWorker|TrustedInstaller|wachten', 'Wacht tot Windows klaar is met het afronden van updates'),
      @('WSUS Offline|dynamische scan|Found |Medium |screensaver|power scheme|power management|Installed |msedge|KB\d{6,}|Windows Update|updates', 'Installeert Windows-updates vanaf de server (kan even duren)'),
      @('Reboot/start teller|Hervat na reboot|BootNext|CmdLine|herstart', 'De laptop is opnieuw opgestart en gaat verder'),
      @('EventLog Running|net-diensten|Servercache|netwerkfout|geen internet|Server-cache-pad|server-mac', 'Controleert de verbinding met de server (de laptop heeft bewust geen internet)'),
      @('INVENTARIS|Controleren: UEFI|UEFI', 'Kijkt welke schijf erin zit (nog niets gewist)'),
      @('Aftellen', 'Telt af voordat de schijf wordt gewist (15 seconden)'),
      @('Schijf wissen|partities maken', 'Wist de schijf en maakt nieuwe partities'),
      @('apply START|Windows 11 op de schijf|install\.wim', 'Zet Windows 11 op de schijf (wordt van de server gehaald)'),
      @('Opstartbestanden|herstelpartitie', 'Maakt de opstartbestanden en de herstelpartitie'),
      @('modeldrivers', 'Zet de drivers voor dit model in Windows'),
      @('KLAAR: Windows staat', 'Windows staat erop; de laptop start nu op in Windows'),
      @('ISO-share|editie', 'Controleert of Windows van de server beschikbaar is')
    )
    foreach ($r in $regels) { if ($t -match $r[0]) { return $r[1] } }
    return 'Bezig; de details staan op de pagina van de laptop'
  } catch { return '' }
}function Get-WinSchatting($gem) {
  # schatting (seconden) van de Windows-fase na de installatie: som van de verplichte stappen uit het stappenplan, met het gemiddelde van eerdere runs als dat er is, anders de standaardduur. Alleen een SCHATTING.
  try { $som = 0.0; foreach ($st in $Stappen) { if ($st.opt -or $st.id -eq 'start') { continue }; $som += $(if ($gem -and $gem.ContainsKey($st.id)) { [double]$gem[$st.id] } else { [double]$st.def * 60 }) }; return $som } catch { return $null }
}
function Get-NuHtml($ti) { try { if ($ti -and "$($ti.nu)".Trim()) { return '<span class="pk-nu">Nu: ' + (E "$($ti.nu)") + '</span>' } } catch {}; return '' }
function Get-PlanTotaal($serie) {
  # 27 okt 2026 (Roderick: 'hij geeft geen geschatte eindtijd en totaal verwachte tijd op de tegel'): de verwachte TOTALE tijd (PXE tot klaar) per model uit config\planning-tijden.csv (minuten, komma als decimaal). $null = model nog onbekend of geen regel. ok = betrouwbaar (minstens 3 runs).
  try {
    $csv = Join-Path (Split-Path (Split-Path (Split-Path $StatusDir -Parent) -Parent) -Parent) 'config\planning-tijden.csv'
    if (-not (Test-Path -LiteralPath $csv)) { return $null }
    $mf = Join-Path $HistDir "$serie.merk"; if (-not (Test-Path -LiteralPath $mf)) { return $null }
    $rr = @(Get-Content -LiteralPath $mf -Encoding UTF8 -ErrorAction SilentlyContinue); $tekst = (($rr -join ' ') -replace '[^A-Za-z0-9]', '').ToLower()
    if (-not $tekst) { return $null }
    $beste = $null; $lang = 0
    foreach ($r in @(Import-Csv -LiteralPath $csv -Delimiter ';' -Encoding UTF8)) {
      $sl = "$($r.model)"; $dl = $sl -split '-', 2; $kern = $(if ($dl.Count -gt 1) { $dl[1] } else { $sl }); $kn = ($kern -replace '[^A-Za-z0-9]', '').ToLower()
      if ($kn.Length -ge 3 -and $tekst.Contains($kn) -and $kn.Length -gt $lang) { $beste = $r; $lang = $kn.Length }
    }
    if (-not $beste) { return $null }
    $min = [double]::Parse(("$($beste.minuten)" -replace ',', '.'), [Globalization.CultureInfo]::InvariantCulture)
    if ($min -le 0) { return $null }
    $n = 0; [void][int]::TryParse("$($beste.n_runs)", [ref]$n)
    return @{ sec = ($min * 60.0); n = $n; ok = ($n -ge 3 -and "$($beste.betrouwbaar)" -eq 'ja') }
  } catch { return $null }
}
function Get-PlekBalk($c) {
  # 13 okt 2026: voortgangsbalk 0-100% in de plek-tegel in VIER vaste kleuren (Roderick: "alles wat normale bedrijven doen"): BLAUW = bezig, GROEN = klaar, ORANJE = aandacht (klaar met voorbehoud, stil, wacht op engineer), ROOD = fout/geen signaal. De tekst eronder zegt de resterende tijd en de verwachte eindtijd.
  try {
    $ti = $Script:TegelInfo["$($c.serie)"]; if (-not $ti) { return '' }
    $pct = [int][math]::Max(0, [math]::Min(100, [int]$ti.pct)); $cl = "$($c.kl[0])"
    $breedte = $pct; $kleur = ''; $txt = "$pct%"; $eta = ''
    if ($ti.inst -and $cl -notin @('klaar', 'voorbehoud', 'weg', 'stil')) {
      $ip = [int][math]::Max(0, [math]::Min(100, [int]$ti.ipct)); $hh = [int](120 * $ip / 100.0)
      $et = if ($null -ne $ti.rest -and [double]$ti.rest -gt 0) { 'installatie nog ' + (Fmt-Duur ([double]$ti.rest)) + "`nklaar " + (Get-Date).AddSeconds([double]$ti.rest).ToString('HH:mm') } else { 'tijd nog onbekend' }
      if ($et -eq 'tijd nog onbekend') { try { $pt = Get-PlanTotaal $c.serie; if ($pt -and $ti.start) { $et = 'verwacht totaal ca. ' + (Fmt-Duur $pt.sec) + "`nklaar ca. " + ([datetime]$ti.start).AddSeconds($pt.sec).ToString('HH:mm') } } catch {} }
      # 15 okt 2026 (Roderick: 'ik mis de verwachte totale tijd klaar op de plek'): installatie + de Windows-fase (Get-WinSchatting, uit de gemiddelden van eerdere runs; zonder gemiddelde de standaardduur van het stappenplan). Altijd als SCHATTING gemarkeerd.
      $alles = ''; try { if ($null -ne $ti.rest -and [double]$ti.rest -gt 0 -and $null -ne $ti.wrest -and [double]$ti.wrest -gt 0) { $alles = '<span class="pk-all">ALLES KLAAR ca. ' + (Get-Date).AddSeconds([double]$ti.rest + [double]$ti.wrest).ToString('HH:mm') + ' <small>(schatting, totaal ca. ' + (E (Fmt-Duur ([double]$ti.rest + [double]$ti.wrest + $(try { ((Get-Date) - [datetime]$ti.start).TotalSeconds } catch { 0 })))) + ')</small></span>' } } catch {}
      try { $pt2 = Get-PlanTotaal $c.serie; if ($pt2 -and $ti.start) { $eindP = ([datetime]$ti.start).AddSeconds($pt2.sec); $alles = '<span class="pk-all">' + $(if ($eindP -gt (Get-Date)) { 'ALLES KLAAR ca. ' + $eindP.ToString('HH:mm') } else { 'duurt langer dan verwacht' }) + ' <small>(schatting, totaal ca. ' + (E (Fmt-Duur $pt2.sec)) + ', ' + $pt2.n + ' eerdere run(s)' + $(if (-not $pt2.ok) { ', nog weinig data' }) + ')</small></span>' } } catch {}
      return ('<span class="pk-txt">' + (E (("$($ti.fase)").ToUpper() + ' ' + $ip + '%')) + '</span><span class="pk-bar"><i style="width:' + $ip + '%;background:hsl(' + $hh + ',85%,48%)"></i></span><span class="pk-eta">' + (($et -split "`n" | ForEach-Object { E $_ }) -join ' - ') + '</span>' + $alles + (Get-TotaalTijdHtml $ti) + (Get-NuHtml $ti))
    }
    if ($cl -eq 'klaar') { $breedte = 100; $kleur = '#16a34a'; $txt = 'KLAAR'; if ($ti.eind) { try { $eta = 'klaar om ' + ([datetime]$ti.eind).ToString('HH:mm') } catch {} } }
    elseif ($cl -eq 'voorbehoud') { $breedte = 100; $kleur = '#d97706'; $txt = 'KLAAR (voorbehoud)' }
    elseif ($cl -eq 'weg') { $kleur = '#dc2626'; $txt = "FOUT: geen signaal ($pct%)" }
    elseif ($cl -eq 'stil') { $kleur = '#d97706'; $txt = "WACHT / STIL ($pct%)" }
    else {
      $h = [int](120 * $pct / 100.0); $kleur = "hsl($h,85%,48%)"; if ($ti.opgestart) { $txt = 'OPGESTART: netwerkstart gezien' }
      if ($null -ne $ti.rest -and [double]$ti.rest -gt 0) { $eta = 'nog ' + (Fmt-Duur ([double]$ti.rest)) + "`nklaar " + (Get-Date).AddSeconds([double]$ti.rest).ToString('HH:mm') } elseif ($c.inst) { $eta = 'installeert' }
    }
    if (-not $eta -and $ti.opgestart) { try { $pt3 = Get-PlanTotaal $c.serie; if ($pt3 -and $ti.start) { $eta = 'verwacht totaal ca. ' + (Fmt-Duur $pt3.sec) + "`nklaar ca. " + ([datetime]$ti.start).AddSeconds($pt3.sec).ToString('HH:mm') } } catch {} }
    return ('<span class="pk-txt">' + (E $txt) + '</span><span class="pk-bar"><i style="width:' + $breedte + '%;background:' + $kleur + '"></i></span>' + $(if ($eta) { '<span class="pk-eta">' + (($eta -split "`n" | ForEach-Object { E $_ }) -join ' - ') + '</span>' } else { '' }) + (Get-TotaalTijdHtml $ti) + (Get-NuHtml $ti))
  } catch { return '' }
}
function Get-TopTegels {
  # 13 okt 2026 (Roderick): hoofdtegels = fout / klaar / bezig (met gemiddelde voortgang) en de verdeling van de voortgang; het aantal herstarts is bewust weggelaten
  try {
    $fout = 0; $klaar = 0; $vb = 0; $bezig = 0; $stil = 0; $som = 0; $bins = @(0, 0, 0, 0)
    foreach ($ti in @($Script:TegelInfo.Values)) {
      switch ("$($ti.kl)") {
        'weg' { $fout++ }
        'klaar' { $klaar++ }
        'voorbehoud' { $vb++ }
        'stil' { $stil++ }
        default { $bezig++; $p = [int]$ti.pct; $som += $p; $b = [math]::Min(3, [int][math]::Floor($p / 25.0)); $bins[$b]++ }
      }
    }
    $gem = if ($bezig -gt 0) { [int][math]::Round($som / $bezig) } else { 0 }
    $h = ''
    $h += '<div class="tegel t-fout"><b>' + $fout + '</b>fout</div>'
    $h += '<div class="tegel t-klaar"><b>' + $klaar + '</b>klaar</div>'
    $h += '<div class="tegel t-vb"><b>' + $vb + '</b>klaar met voorbehoud</div>'
    $h += '<div class="tegel t-bezig"><b>' + $bezig + '</b>bezig' + $(if ($bezig -gt 0) { ' (gemiddeld ' + $gem + '%)' } else { '' }) + '</div>'
    $h += '<div class="tegel t-stil"><b>' + $stil + '</b>stil / wacht</div>'
    # 15 okt 2026 (Roderick: 'voortgang van de bezige mag weg, niet nuttig'): de tegel 'Voortgang van de bezige' (aantal units per 25%-band) is verwijderd.
    $ins =@($Script:TegelInfo.Values | Where-Object { $_.inst -and "$($_.kl)" -in @('bezig', 'herstart') -and "$($_.fase)" -ne 'Windows start' })
    if ($ins.Count -gt 0) {
      $som2 = 0; $langste = 0.0; foreach ($x in $ins) { $som2 += [int]$x.ipct; if ($null -ne $x.rest -and [double]$x.rest -gt $langste) { $langste = [double]$x.rest } }
      $gp = [int][math]::Round($som2 / $ins.Count); $hh2 = [int](215 - 85 * ($gp / 100.0))
      $h += '<div class="tegel t-bezig"><b>' + $ins.Count + '</b>installaties bezig (samen ' + $gp + '%)<span class="pk-bar" style="width:220px;margin-top:6px"><i style="width:' + $gp + '%;background:hsl(' + $hh2 + ',75%,42%)"></i></span>' + $(if ($langste -gt 0) { '<small>laatste klaar over ca. ' + (E (Fmt-Duur $langste)) + '</small>' } else { '' }) + '</div>'
    }
    return $h
  } catch { return '' }
}function Get-StapLijst([string]$serie, $nu, [int]$max = 3) {
  # 9 okt 2026 (Roderick: 'als jij het kunt zien, moet het dashboard het ook kunnen'): de laatste stappen uit de geschiedenis (historie\<serie>.log) met hun duur, ook kortdurende (bv. Opruimen 13 s) die in een verversing van 5 s makkelijk gemist worden.
  $uit = @()
  try {
    $lp = Join-Path $HistDir "$serie.log"; if (-not (Test-Path $lp)) { return @() }
    $st = @()
    foreach ($r in @(Get-Content $lp -Tail 400 -Encoding UTF8 -ErrorAction SilentlyContinue)) {
      $d = $r -split "`t"; if ($d.Count -ge 4 -and $d[2] -eq 'STAP') { $tt = $null; try { $tt = [datetime]::ParseExact($d[0], 'yyyy-MM-dd HH:mm:ss', [Globalization.CultureInfo]::InvariantCulture) } catch {}; if ($tt) { $st += @{ naam = ($d[3] -replace '\s*\(.*$', '').Trim(); t = $tt } } }
    }
    for ($i = 0; $i -lt $st.Count; $i++) { $eind = if ($i + 1 -lt $st.Count) { $st[$i + 1].t } else { $nu }; $uit += @{ naam = $st[$i].naam; sec = [int][math]::Max(0, ($eind - $st[$i].t).TotalSeconds); huidig = ($i -eq $st.Count - 1); start = $st[$i].t } }
  } catch {}
  return @($uit | Select-Object -Last $max)
}
function Get-StapKort([string]$serie, $nu) {
  # korte tekst voor onder het serienummer op de plek-tegel: de huidige stap; de eerste 30 s na een wisseling ook de vorige ('vorige > huidige')
  $l = @(Get-StapLijst $serie $nu 2); if ($l.Count -eq 0) { return $null }
  $cur = $l[-1]; $kort = { param($n) $n = "$n" -replace '^\d+\.\s*', ''; if ($n.Length -gt 26) { $n.Substring(0, 25) + '...' } else { $n } }
  $tekst = & $kort $cur.naam; $tip = "$($cur.naam) ($($cur.sec) s)"
  if ($l.Count -ge 2 -and $cur.sec -le 30) { $tekst = (& $kort $l[0].naam) + ' > ' + $tekst; $tip = "$($l[0].naam) ($($l[0].sec) s) > $tip" }
  return @{ tekst = $tekst; tip = $tip }
}function Get-LiveBalkHtml($dn, $up, [string]$bron) {
  if ($null -eq $dn -and $null -eq $up) { return '' }
  $ci = [Globalization.CultureInfo]::InvariantCulture
  $cap = 117.0
  $d = [double]$(if ($null -ne $dn) { $dn } else { 0 }); $u = [double]$(if ($null -ne $up) { $up } else { 0 })
  $breed = { param($v) $p = [math]::Min(100.0, 100.0 * $v / $cap); if ($v -gt 0.05 -and $p -lt 3) { $p = 3 }; $p.ToString('0', $ci) }
  $tip = ('ophalen {0:0.0} MB/s, terugsturen {1:0.0} MB/s' -f $d, $u) + $(if ($bron) { ' (' + $bron + ')' } else { '' })
  return ('<span class="lvb" title="' + (E $tip) + '"><i class="lv-d" style="width:' + (& $breed $d) + '%"></i></span><span class="lvb" title="' + (E $tip) + '"><i class="lv-u" style="width:' + (& $breed $u) + '%"></i></span><small class="lvt">&darr;' + ('{0:0}' -f $d) + ' &uarr;' + ('{0:0}' -f $u) + '</small>')
}
function Get-LiveUitStatus($s) {
  $ci = [Globalization.CultureInfo]::InvariantCulture
  $num = { param($k) $v = "$($s[$k])".Trim(); if ($v -ne '') { $x = 0.0; if ([double]::TryParse($v.Replace(',', '.'), [Globalization.NumberStyles]::Float, $ci, [ref]$x)) { return $x } }; return $null }
  return @{ dn = (& $num 'live_down_mbs'); up = (& $num 'live_up_mbs') }
}
function Get-ServerSnelheidNu($nu) {
  # gemiddelde van de laatste 3 metingen van de server-kabel: naar = server->laptop, van = laptop->server (MB/s); $null als er te weinig metingen zijn
  try {
    $H = @($Script:NetHist); if ($H.Count -lt 3) { return $null }
    $r = New-Object System.Collections.Generic.List[object]
    for ($i = $H.Count - 1; $i -ge 1 -and $r.Count -lt 3; $i--) { $dt = ($H[$i].t - $H[$i - 1].t).TotalSeconds; if ($dt -ge 0.5 -and $H[$i].tx -ge $H[$i - 1].tx -and $H[$i].rx -ge $H[$i - 1].rx) { $r.Add(@{ naar = ($H[$i].tx - $H[$i - 1].tx) / $dt / 1MB; van = ($H[$i].rx - $H[$i - 1].rx) / $dt / 1MB }) } }
    if (-not $r.Count) { return $null }
    $sn = 0.0; $sv = 0.0; foreach ($x in $r) { $sn += $x.naar; $sv += $x.van }
    return @{ naar = $sn / $r.Count; van = $sv / $r.Count }
  } catch { return $null }
}function Get-Verbinding($s) {
  $ci = [Globalization.CultureInfo]::InvariantCulture
  $num = { param($k) $v = "$($s[$k])".Trim(); if ($v -ne '') { $d = 0.0; if ([double]::TryParse($v.Replace(',', '.'), [Globalization.NumberStyles]::Float, $ci, [ref]$d)) { return $d } }; return $null }
  $link = & $num 'nic_mbps'; $dn = & $num 'down_mbs'; $up = & $num 'up_mbs'; $fo = & $num 'nic_fouten'
  if ($null -eq $link -and $null -eq $dn -and $null -eq $up) { return @{ txt = ''; klas = ''; tip = '' } }
  $delen = @(); $klas = 'ok'; $tip = @()
  if ($null -ne $link) {
    $lt = if ($link -ge 1000) { ('{0:0.#} Gbit' -f ($link / 1000)) } else { ('{0:0} Mbit' -f $link) }
    $delen += $lt
    if ($link -lt 900) { $klas = 'bad'; $tip += ('link maar ' + $lt + ' (verwacht 1 Gbit): kabel, poort of hub') }
  }
  if ($null -ne $dn -or $null -ne $up) {
    $delen += ('&darr;' + $(if ($null -ne $dn) { '{0:0}' -f $dn } else { '-' }) + ' &uarr;' + $(if ($null -ne $up) { '{0:0}' -f $up } else { '-' }) + ' MB/s')
    $min = 30.0
    if ($klas -ne 'bad' -and (($null -ne $dn -and $dn -lt $min) -or ($null -ne $up -and $up -lt $min))) { $klas = 'warn'; $tip += 'meting onder 30 MB/s: traag (kabel, hub of poort?)' }
  }
  if ($null -ne $fo -and $fo -ge 20) { if ($klas -eq 'ok') { $klas = 'warn' }; $delen += ('fouten ' + ('{0:0}' -f $fo)); $tip += ('pakketfouten op de netwerkkaart van de laptop: ' + ('{0:0}' -f $fo)) }
  return @{ txt = ($delen -join ' &middot; '); klas = $klas; tip = ($tip -join '; ') }
}
function Ensure-SnelheidsTestbestand {
  # server-kant van de snelheidsmeting: een bestand van 256 MB willekeurige data in <cache>\speedtest (de laptop leest er 24 MB uit op een willekeurige plek). Eenmalig; bestaat het al met de juiste grootte, dan gebeurt er niets.
  try {
    $dirT = Join-Path (Split-Path -Parent $StatusDir) 'speedtest'; $pad = Join-Path $dirT 'test-256MB.bin'
    if ((Test-Path $pad) -and ((Get-Item $pad).Length -eq 256MB)) { return }
    New-Item -ItemType Directory -Force -Path $dirT | Out-Null
    $tmp = "$pad.bezig"; $rng = [Security.Cryptography.RNGCryptoServiceProvider]::new(); $buf = New-Object byte[] (4MB)
    $fs = [IO.File]::Create($tmp); try { for ($i = 0; $i -lt 64; $i++) { $rng.GetBytes($buf); $fs.Write($buf, 0, $buf.Length) } } finally { $fs.Dispose(); $rng.Dispose() }
    if (Test-Path $pad) { [IO.File]::Delete($pad) }; [IO.File]::Move($tmp, $pad)
  } catch {}
}
$Script:NetHist = New-Object System.Collections.ArrayList
# 9 okt 2026 (Roderick: 'de grafiek loopt niet'): de verkeersgeschiedenis stond alleen in het geheugen, dus na elke herstart van het dashboard was de grafiek leeg. Nu ook op schijf (gate-state\nethist.csv, laatste 15 min) en bij het starten teruggelezen.
$Script:NetHistPad = Join-Path $StateDir 'nethist.csv'; $Script:NetHistOpgeslagen = [datetime]::MinValue
try {
  if (Test-Path $Script:NetHistPad) {
    $nuStart = Get-Date; $vorig = $null
    foreach ($r in @(Get-Content $Script:NetHistPad -ErrorAction Stop)) {
      $d = $r -split ';'; if ($d.Count -lt 3) { continue }
      $tt = [datetime]::Parse($d[0], $null, [Globalization.DateTimeStyles]::RoundtripKind); $tx = [double]::Parse($d[1], [Globalization.CultureInfo]::InvariantCulture); $rx = [double]::Parse($d[2], [Globalization.CultureInfo]::InvariantCulture)
      if (($nuStart - $tt).TotalSeconds -gt 900) { continue }
      if ($vorig -and ($tx -lt $vorig.tx -or $rx -lt $vorig.rx)) { $Script:NetHist.Clear() }   # tellers liepen terug (herstart/kaart opnieuw): oude punten niet gebruiken
      $p = @{ t = $tt; tx = $tx; rx = $rx }; [void]$Script:NetHist.Add($p); $vorig = $p
    }
  }
} catch { $Script:NetHist.Clear() }
function Add-NetSample($nu) {
  try {
    $st = Get-NetAdapterStatistics -Name 'Ethernet' -ErrorAction Stop
    [void]$Script:NetHist.Add(@{ t = $nu; tx = [double]$st.SentBytes; rx = [double]$st.ReceivedBytes })
    while ($Script:NetHist.Count -gt 1 -and ($nu - $Script:NetHist[0].t).TotalSeconds -gt 900) { $Script:NetHist.RemoveAt(0) }
    if (($nu - $Script:NetHistOpgeslagen).TotalSeconds -ge 15) {
      $Script:NetHistOpgeslagen = $nu
      $regels = @($Script:NetHist | ForEach-Object { '{0};{1};{2}' -f $_.t.ToString('o'), $_.tx.ToString('R', [Globalization.CultureInfo]::InvariantCulture), $_.rx.ToString('R', [Globalization.CultureInfo]::InvariantCulture) })
      [IO.File]::WriteAllLines($Script:NetHistPad, $regels)
    }
  } catch {}
}
function Get-NetPaneel($nu, $lijst, $inst) {
  $na = Get-EthAdapter
  if ($na.Status -ne 'Up') { return '<div class="netpaneel" style="border-left:4px solid #dc2626"><b>Server-netwerk: geen verbinding</b> &middot; de Ethernet-poort van de server is NIET verbonden (kabel, hub of laptop uit?)</div>' }
  $speed = 0.0; try { $speed = [double]$na.Speed } catch {}
  $cap = if ($speed -gt 0) { $speed / 8 * 0.94 / 1MB } else { 0.0 }   # praktisch maximum van de kabel (ca. 94% van de lijnsnelheid)
  $H = @($Script:NetHist)
  $pts = New-Object System.Collections.Generic.List[object]
  for ($i = 1; $i -lt $H.Count; $i++) {
    $dt = ($H[$i].t - $H[$i - 1].t).TotalSeconds
    if ($dt -ge 0.5 -and $H[$i].tx -ge $H[$i - 1].tx -and $H[$i].rx -ge $H[$i - 1].rx) { $pts.Add(@{ t = $H[$i].t; naar = ($H[$i].tx - $H[$i - 1].tx) / $dt / 1MB; van = ($H[$i].rx - $H[$i - 1].rx) / $dt / 1MB }) }
  }
  $w = @($pts | Where-Object { ($nu - $_.t).TotalSeconds -le 600 })
  $laatste = @($w | Select-Object -Last 3)
  # (hashtables: geen Measure-Object -Property; dat geeft niets terug -> zelf rekenen)
  $gemiddeld = { param($lst, $sl) $s = 0.0; foreach ($x in $lst) { $s += [double]$x[$sl] }; if (@($lst).Count) { $s / @($lst).Count } else { 0.0 } }
  $hoogste = { param($lst, $sl) $m = 0.0; foreach ($x in $lst) { if ([double]$x[$sl] -gt $m) { $m = [double]$x[$sl] } }; $m }
  $naarNu = & $gemiddeld $laatste 'naar'; $vanNu = & $gemiddeld $laatste 'van'
  $w60 = @($w | Where-Object { ($nu - $_.t).TotalSeconds -le 60 })
  $naar60 = & $gemiddeld $w60 'naar'; $van60 = & $gemiddeld $w60 'van'
  $piekNaar = & $hoogste $w 'naar'; $piekVan = & $hoogste $w 'van'
  $totNu = $naarNu + $vanNu
  $pctTxt = if ($cap -gt 0) { ('{0:N0}% van wat de kabel per richting kan (ca. {1:N0} MB/s; ophalen en terugsturen tellen apart, full duplex)' -f (100 * [math]::Max($naarNu, $vanNu) / $cap), $cap) } else { '' }   # 8 okt 2026: een gigabitkabel kan in BEIDE richtingen tegelijk ca. 112 MB/s; optellen gaf onterecht >100%
  # --- grafiekje (laatste 10 min) ---
  $gw = 440; $gh = 90
  $yMax = [math]::Max(10.0, [math]::Ceiling([math]::Max($piekNaar, $piekVan) * 1.15 / 5) * 5)
  # coordinaten altijd met punt als decimaal (InvariantCulture), ook als Windows op nl-NL staat
  $polyN = ''; $polyV = ''
  $ci = [Globalization.CultureInfo]::InvariantCulture
  foreach ($p in $w) {
    $x = 4 + (600 - ($nu - $p.t).TotalSeconds) / 600 * ($gw - 8)
    $yN = ($gh - 6) - [math]::Min($p.naar, $yMax) / $yMax * ($gh - 12); $yV = ($gh - 6) - [math]::Min($p.van, $yMax) / $yMax * ($gh - 12)
    $polyN += ($x.ToString('0.0', $ci) + ',' + $yN.ToString('0.0', $ci) + ' '); $polyV += ($x.ToString('0.0', $ci) + ',' + $yV.ToString('0.0', $ci) + ' ')
  }
  $capLijn = ''
  if ($cap -gt 0 -and $cap -le $yMax) { $yc = ($gh - 6) - $cap / $yMax * ($gh - 12); $capLijn = '<line x1="4" x2="' + ($gw - 4) + '" y1="' + $yc.ToString('0.0', $ci) + '" y2="' + $yc.ToString('0.0', $ci) + '" style="stroke:var(--bad);stroke-dasharray:4 3;stroke-width:1"/><text x="' + ($gw - 6) + '" y="' + ($yc - 2).ToString('0.0', $ci) + '" text-anchor="end" style="fill:var(--bad);font-size:9px">max kabel</text>' }
  $svg = '<svg viewBox="0 0 ' + $gw + ' ' + $gh + '" width="100%" style="max-width:' + $gw + 'px;height:auto;background:var(--bg);border-radius:6px" role="img" aria-label="verkeer laatste 10 minuten">' +
    '<line x1="4" x2="' + ($gw - 4) + '" y1="' + ($gh - 6) + '" y2="' + ($gh - 6) + '" style="stroke:var(--line)"/>' + $capLijn +
    $(if ($polyN) { '<polyline points="' + $polyN.Trim() + '" fill="none" style="stroke:var(--go);stroke-width:1.8"/>' }) +
    $(if ($polyV) { '<polyline points="' + $polyV.Trim() + '" fill="none" style="stroke:var(--warn);stroke-width:1.8"/>' }) +
    '<text x="6" y="11" style="fill:var(--mut);font-size:9px">0 - ' + $yMax.ToString('0', $ci) + ' MB/s, laatste 10 min</text></svg>'
  # --- welke laptop(s) ---
  $act = New-Object System.Collections.Generic.List[object]
  foreach ($l in @($lijst)) { try { if (-not $l.klaar -and ($nu - (Get-Item $l.pad).LastWriteTime).TotalSeconds -le 120) { $act.Add(@{ serie = "$($l.serie)"; ip = "$($l.s['ip'])" }) } } catch {} }
  foreach ($k in @($inst)) { try { if (($nu - $k['mtime']).TotalSeconds -le 120 -and -not (@($act) | Where-Object { $_.serie -eq $k['serie'] })) { $act.Add(@{ serie = "$($k['serie'])"; ip = '' }) } } catch {} }
  $perIp = @()
  # 8 okt 2026: GEEN vast netwerkadres in de code (overzetbaar naar de echte server): het laptopnetwerk = de /24 van het IP-adres dat de Ethernet-kaart van de server zelf heeft; terugval: alle eigen private adressen.
  $eigenIps = @(); try { $eigenIps = @(Get-NetIPAddress -AddressFamily IPv4 -ErrorAction Stop | ForEach-Object { "$($_.IPAddress)" }) } catch {}
  $lpref = @(); try { $ea = (Get-EthAdapter).Name; foreach ($a in @(Get-NetIPAddress -AddressFamily IPv4 -InterfaceAlias $ea -ErrorAction Stop)) { if ("$($a.IPAddress)" -match '^(\d+\.\d+\.\d+)\.\d+$') { $lpref += ($Matches[1] + '.') } } } catch {}
  if (-not $lpref.Count) { foreach ($ip in $eigenIps) { if ($ip -match '^(10\.\d+\.\d+|172\.(1[6-9]|2\d|3[01])\.\d+|192\.168\.\d+)\.\d+$' -and $ip -notmatch '^169\.254\.') { $lpref += ($Matches[1] + '.') } } }
  $lpref = @($lpref | Select-Object -Unique)
  try { $perIp = @((Get-Cached 'tcp' 4 { Get-NetTCPConnection -State Established -ErrorAction Stop }) | Where-Object { $ra = "$($_.RemoteAddress)"; $_.LocalPort -in 80, 445, 8080 -and (@($lpref | Where-Object { $ra.StartsWith($_) }).Count -gt 0) -and ($eigenIps -notcontains $ra) } | Group-Object RemoteAddress) } catch {}   # verbindingen van de server met zichzelf (zijn eigen browser) tellen niet mee
  $namen = @(); $ongepaard = @(); $zonderIp = @($act | Where-Object { -not $_.ip })
  foreach ($g in $perIp) {
    $hit = @($act | Where-Object { $_.ip -and $_.ip -eq $g.Name } | Select-Object -First 1)
    if ($hit.Count) { $namen += ('<b>' + (E $hit[0].serie) + '</b> (' + (E $g.Name) + ', ' + $g.Count + ' verbindingen)') } else { $ongepaard += $g }
  }
  foreach ($g in $ongepaard) {
    if ($ongepaard.Count -eq 1 -and $zonderIp.Count -eq 1) { $namen += ('<b>' + (E $zonderIp[0].serie) + '</b> (' + (E $g.Name) + ', ' + $g.Count + ' verbindingen)') }
    else { $namen += ('onbekend toestel (' + (E $g.Name) + ', ' + $g.Count + ' verbindingen)') }
  }
  $wie = if ($namen.Count) { $namen -join '<br>' } elseif ($totNu -gt 0.5 -and $act.Count -eq 1) { '<b>' + (E $act[0].serie) + '</b> (geen open HTTP/SMB-verbinding gezien; waarschijnlijk opstart-verkeer via PXE/TFTP)' } elseif ($totNu -gt 0.5) { 'geen open HTTP/SMB-verbinding gezien (opstart-verkeer via PXE/TFTP of een toestel buiten dit overzicht)' } else { 'geen laptop verbonden die nu iets ophaalt' }
  $delen = if ($namen.Count -gt 1 -or $act.Count -gt 1) { '<div class="sub">Meerdere laptops tegelijk: het verkeer hierboven is het TOTAAL van de server-kabel; per laptop is dat zonder beheerrechten niet apart te meten.</div>' } else { '' }
  # --- kabel-gezondheid ---
  $waarsch = ''
  if ($speed -gt 0 -and $speed -lt 1e9) { $waarsch += '<div class="sub" style="color:var(--bad)">LET OP: de link is maar ' + (E $na.LinkSpeed) + ' (verwacht 1 Gbps): kabel, poort of hub onderhandelt te laag.</div>' }
  $fouten = ''
  try { $s2 = Get-NetAdapterStatistics -Name 'Ethernet' -ErrorAction Stop; $nf = [double]$s2.ReceivedPacketErrors + [double]$s2.OutboundPacketErrors + [double]$s2.ReceivedDiscardedPackets + [double]$s2.OutboundDiscardedPackets; $fouten = ' &middot; pakketfouten sinds opstart: <b>' + ('{0:N0}' -f $nf) + '</b>' } catch {}
  $dup = ''; try { $dup = ' &middot; ' + $(if ($na.FullDuplex) { 'full duplex' } else { 'HALF duplex (dat is traag!)' }) } catch {}
  return ('<div class="netpaneel"><div class="netkop"><b>Server-netwerk</b> &middot; ' + (E $na.InterfaceDescription) + ' (' + (E $na.Name) + ') &middot; link <b>' + (E $na.LinkSpeed) + '</b>' + $dup + $fouten + '</div>' +
    '<div class="netgetallen"><div><span class="blauw">naar de laptops</span><b>' + ('{0:N1}' -f $naarNu) + ' MB/s</b><small>gem. 1 min ' + ('{0:N1}' -f $naar60) + ' &middot; piek ' + ('{0:N1}' -f $piekNaar) + '</small></div>' +
    '<div><span class="oranje">van de laptops</span><b>' + ('{0:N1}' -f $vanNu) + ' MB/s</b><small>gem. 1 min ' + ('{0:N1}' -f $van60) + ' &middot; piek ' + ('{0:N1}' -f $piekVan) + '</small></div>' +
    '<div><span>samen</span><b>' + ('{0:N1}' -f $totNu) + ' MB/s</b><small>' + $pctTxt + '</small></div></div>' +
    $svg + '<div class="sub"><span class="blauw">blauw</span> = server &rarr; laptop (ophalen van Windows/updates) &middot; <span class="oranje">oranje</span> = laptop &rarr; server (logboeken, status)</div>' +
    '<div class="sub">Verkeer nu met: ' + $wie + '</div>' + $delen + $waarsch + '</div>')
}
function Is-PlekOvergenomen($l, $lijst, $gateTijden, $nu) {
  # 8 okt 2026 (Roderick): een KLAAR laptop blijft 30 min in de hoofdtabel staan, tot er op DEZELFDE plek (tafel/poort) een andere laptop verschijnt.
  # Overgenomen = een andere laptop met dezelfde locatie die NIEUWER meldt, een installatiebestand (wpelogs) dat nieuwer is, of een PXE-poging (gate) die nieuwer is. Zonder locatie kan dit niet bepaald worden: dan geldt alleen de 30 minuten.
  try {
    $loc = Get-Locatie $l.serie; if (-not $loc) { return $false }
    $mt = (Get-Item $l.pad).LastWriteTime
    foreach ($o in $lijst) { if ($o.serie -ne $l.serie -and (Get-Locatie $o.serie) -eq $loc -and (Get-Item $o.pad).LastWriteTime -gt $mt) { return $true } }
    $wpe = Join-Path (Split-Path -Parent (Split-Path -Parent $StatusDir)) 'wpelogs'
    foreach ($f in @(Get-ChildItem $wpe -Filter 'install-*.txt' -ErrorAction SilentlyContinue)) { $sn = $f.BaseName -replace '^install-', ''; if ($sn -ne $l.serie -and $f.LastWriteTime -gt $mt -and (Get-Locatie $sn) -eq $loc) { return $true } }
    foreach ($k in @($gateTijden.Keys)) { if ($k -ne $l.serie -and $gateTijden[$k] -gt $mt -and (Get-Locatie $k) -eq $loc) { return $true } }
  } catch {}
  return $false
}function Genereer {
  $nu = Get-Date; $gem = Get-Gemiddelden; $lijst = @(); $kaart = @{}
  if (Test-Path $StatusDir) {
    foreach ($f in (Get-ChildItem $StatusDir -Filter *.txt -File -ErrorAction SilentlyContinue)) {
      # 19 okt 2026 (Roderick: 'ik zie kort contact en dan is de plek leeg'; BEWEZEN door de waarnemer scripts\303: 22:14:46 was plek 5 een ronde lang leeg tijdens de Windows-fase): een mislukte ronde voor 1 statusbestand
      # (halfgeschreven bestand door de laptop, een parse-fout) werd stil ingeslikt, waardoor de unit die ronde NIET in de lijst stond. Nu: de fout wordt gelogd (logs\dashboard-fouten.log) en de laatste goede uitkomst van
      # DEZE unit blijft maximaal 30 s staan. Na 30 s zonder goede uitkomst verdwijnt hij wel (geen eeuwig oud spook).
      if (-not $Script:LaatsteL) { $Script:LaatsteL = @{} }
      $l = $null; $fouttekst = ''; try { $l = Update-Laptop $f.FullName $nu } catch { $fouttekst = "Update-Laptop gaf een fout: $($_.Exception.Message) (regel $($_.InvocationInfo.ScriptLineNumber))" }
      # alleen voor ECHTE unit-bestanden (die eerder een goede uitkomst gaven) loggen; locaties.txt, install-start-*.txt, onderhoud.txt enz. zijn geen statusbestanden en geven altijd niets terug
      if (-not $l -and -not $fouttekst -and $Script:LaatsteL.ContainsKey($f.FullName)) { $fouttekst = 'Update-Laptop gaf niets terug (statusbestand leeg of zonder serienummer?)' }
      if ($l) { $Script:LaatsteL[$f.FullName] = @{ l = $l; t = $nu } }
      elseif ($fouttekst) {
        try { $lg = Join-Path (Join-Path (Split-Path -Parent (Split-Path -Parent (Split-Path -Parent $ScriptFolder))) 'logs') 'dashboard-fouten.log'; Add-Content -Path $lg -Value ("{0}  {1}: {2}" -f $nu.ToString('yyyy-MM-dd HH:mm:ss'), $f.Name, $fouttekst) -Encoding UTF8 } catch {}
        if ($Script:LaatsteL.ContainsKey($f.FullName) -and (($nu - $Script:LaatsteL[$f.FullName].t).TotalSeconds -lt 30)) { $l = $Script:LaatsteL[$f.FullName].l }
      }
      if ($l) { $lijst += $l }
    }
  }
  $lijst = @($lijst | Sort-Object { (Get-Item $_.pad).LastWriteTime } -Descending)
  # --- fase 1 (5 okt 2026): 1 op 1 met wat de server ziet ---
  # (a) een laptop die NA zijn laatste statusbestand opnieuw via het netwerk is opgestart toont niet meer zijn OUDE status (bv. een oude KLAAR) maar 'OPGESTART'
  # (b) een laptop die de poort ziet maar nog geen statusbestand heeft, verschijnt direct als 'OPGESTART'
  # (c) KLAAR en losgekoppeld (geen antwoord op ping/ARP, melding > 2 min oud) verdwijnt uit de hoofdtabel naar 'Eerder klaar en losgekoppeld'
  # 20 okt 2026: botsingen tussen een automatische plek en een LATER handmatig gezette plek (script 101) eerst oplossen, zodat Is-PlekOvergenomen en de plattegrond niet een ronde met een verouderde automatische plek rekenen (bewezen in test 75: H30A werd even verdrongen)
  try { foreach ($ak in @((Lees-AutoPlekken).Keys)) { [void](Zorg-Plek $ak) } } catch {}
  $gateTijden = Get-GatePxe
  $nieuwe = @(); $eerder = @(); $blijf = @()
  foreach ($l in $lijst) {
    $mt = (Get-Item $l.pad).LastWriteTime; $leeft = ($nu - $mt).TotalSeconds; $gt = $gateTijden[$l.serie]
    if ($gt -and $gt -gt $mt -and ($nu - $gt).TotalHours -lt 4 -and ($l.klaar -or $leeft -gt 25)) { $nieuwe += @{ serie = $l.serie; tijd = $gt }; continue }
    if ($l.klaar -and $leeft -gt 120) {
      $aanwezig = $false; $ipk = "$($l.s['ip'])"; if ($ipk) { $aanwezig = (Ping-Snel $ipk) -and (Is-DezelfdeLaptop $l.serie $ipk ($leeft -gt 600)) }
      # 30 min vasthouden (Roderick, 8 okt 2026): tot er op dezelfde plek een andere laptop komt
      $vast = (-not $aanwezig) -and ($leeft -le 1800) -and (-not (Is-PlekOvergenomen $l $lijst $gateTijden $nu))
      if (-not $aanwezig -and -not $vast) { $eerder += $l; continue }
    }
    # MAC leren zolang de laptop bezig is en net gemeld heeft
    if (-not $l.klaar -and $leeft -lt 60 -and $l.s['ip']) { try { $mf0 = Join-Path $HistDir ("{0}.mac" -f $l.serie); if (-not (Test-Path $mf0)) { $m0 = Get-MacBijIp "$($l.s['ip'])"; if ($m0) { [IO.File]::WriteAllText($mf0, $m0) } } } catch {} }
    # 7 okt 2026: een run zonder eindmelding die > 6 uur stil is EN niet meer antwoordt (laptop uit/losgekoppeld) blijft niet eeuwig 'GEEN SIGNAAL' staan; hij gaat naar 'Eerder' met de tekst dat de server geen eindmelding kreeg (NIET dat de run mislukt is: dat weet de server dan niet).
    if (-not $l.klaar -and $leeft -gt 21600) {
      $aanwezig = $false; $ipk = "$($l.s['ip'])"; if ($ipk) { $aanwezig = Ping-Snel $ipk }
      if (-not $aanwezig) { $l.eindmelding = 'ontbreekt'; $eerder += $l; continue }
    }
    $blijf += $l
  }
  foreach ($sn in @($gateTijden.Keys)) { if (-not ($lijst | Where-Object { $_.serie -eq $sn })) { if (($nu - $gateTijden[$sn]).TotalHours -lt 4) { $nieuwe += @{ serie = $sn; tijd = $gateTijden[$sn] } } } }
  # 7 okt 2026: een OUD statusbestand (van een eerdere run) naast een NIEUWER installatiebestand (wpelogs\install-<serie>.txt, <4 uur) hoort niet meer in de hoofdtabel: de lopende installatie is nu de waarheid.
  $wpeD = Join-Path (Split-Path -Parent (Split-Path -Parent $StatusDir)) 'wpelogs'
  $blijf = @($blijf | Where-Object { $ip = Join-Path $wpeD ("install-{0}.txt" -f $_.serie); -not ((Test-Path $ip) -and ((Get-Item $ip).LastWriteTime -gt (Get-Item $_.pad).LastWriteTime) -and (($nu - (Get-Item $ip).LastWriteTime).TotalHours -lt 4)) })
  # 19 okt 2026 (Roderick: 'ik zie kort contact en dan verdwijnt de plek, laat hem open staan met een bericht, letterlijk wat de server ziet'): win11-auto loopt NIET langs de PXE-poort (gate.log: laatste regel 18 okt),
  # dus tussen het netwerkstarten en de eerste installatiemelding kon een plek leeg zijn. Een laptop waarvan het MAC bekend is (historie\<serie>.mac) en die de ARP-tabel van de server nu als BEREIKBAAR meldt,
  # terwijl hij nergens anders op het dashboard staat (niet in de tabel, niet 'opgestart', geen installatiebestand < 4 uur), houdt zijn plek met de LETTERLIJKE waarneming. Verandert niets aan units die al getoond worden.
  try {
    if (-not $Script:ArpEerste) { $Script:ArpEerste = @{} }; if (-not $Script:ArpLaatst) { $Script:ArpLaatst = @{} }
    $arpNu = @{}; foreach ($nb in @(Get-Cached 'arpbuur' 2 { Get-NetNeighbor -InterfaceAlias Ethernet -AddressFamily IPv4 -State Reachable -ErrorAction SilentlyContinue })) { if ($nb.LinkLayerAddress) { $arpNu["$($nb.LinkLayerAddress)".ToUpper()] = "$($nb.IPAddress)" } }
    $gezienNu = @{}
    foreach ($mf1 in @(Get-ChildItem $HistDir -Filter '*.mac' -ErrorAction SilentlyContinue)) {
      $sn1 = $mf1.BaseName; $mac1 = ("$(Get-Content -LiteralPath $mf1.FullName -Raw)").Trim().ToUpper()
      if (-not $mac1) { continue }
      # 19 okt 2026 (BEWEZEN op de Lenovo 23:33:10-23:33:37 met logs\dashboard-plek-leeg.log): tijdens het opstarten reset de netwerkkaart van de laptop en wisselt zijn ARP-status (Reachable/Stale/Probe); zonder vasthouden stond de plek 1 ronde bezet en daarna weer leeg tot de installatie zich meldde.
      # Nu: na de laatste ARP-bevestiging (Reachable) blijft de plek nog 180 s bezet, tenzij de unit elders op het dashboard staat of er een installatiebestand komt (dan neemt dat over).
      if ($arpNu.ContainsKey($mac1)) { $Script:ArpLaatst[$sn1] = @{ t = $nu; ip = $arpNu[$mac1] } }
      elseif (-not ($Script:ArpLaatst.ContainsKey($sn1) -and (($nu - $Script:ArpLaatst[$sn1].t).TotalSeconds -lt 180))) { continue }
      if ($blijf | Where-Object { $_.serie -eq $sn1 }) { continue }
      if ($nieuwe | Where-Object { $_.serie -eq $sn1 }) { continue }
      $ip1 = Join-Path $wpeD ("install-{0}.txt" -f $sn1); if ((Test-Path -LiteralPath $ip1) -and (($nu - (Get-Item -LiteralPath $ip1).LastWriteTime).TotalHours -lt 4)) { continue }
      if (-not $Script:ArpEerste.ContainsKey($sn1)) { $Script:ArpEerste[$sn1] = $nu }
      $gezienNu[$sn1] = $true
      $eerder = @($eerder | Where-Object { $_.serie -ne $sn1 })
      $nieuwe += @{ serie = $sn1; tijd = $Script:ArpEerste[$sn1]; arp = $true; ip = $Script:ArpLaatst[$sn1].ip; mac = $mac1 }
    }
    # 19 okt 2026: een apparaat op de laptoppoort waarvan het MAC bij GEEN bekende laptop hoort (nieuw model/nieuwe laptop bij de eerste PXE-aanvraag, nog zonder serienummer) staat ook meteen op een plek
    $bekendeMacs = @{}; foreach ($mf2 in @(Get-ChildItem $HistDir -Filter '*.mac' -ErrorAction SilentlyContinue)) { try { $bekendeMacs[("$(Get-Content -LiteralPath $mf2.FullName -Raw)").Trim().ToUpper()] = 1 } catch {} }
    foreach ($mac2 in @($arpNu.Keys)) {
      if ($bekendeMacs.ContainsKey($mac2)) { continue }
      $ipx = $arpNu[$mac2]; $srvIp = '192.168.77.1'; $srvVoor = $srvIp.Substring(0, $srvIp.LastIndexOf('.') + 1); if ($ipx -notlike ($srvVoor + '*') -or $ipx -eq $srvIp) { continue }  # 20 okt 2026: prefix afgeleid van het serveradres (Install-Server herschrijft 192.168.77.1), niet vast ingetikt
      $sl = 'MAC-' + ($mac2 -replace '-', '')
      if (-not (Zorg-Plek $sl)) { continue }
      $Script:ArpLaatst[$sl] = @{ t = $nu; ip = $ipx }; if (-not $Script:ArpEerste.ContainsKey($sl)) { $Script:ArpEerste[$sl] = $nu }; $gezienNu[$sl] = $true
      $nieuwe += @{ serie = $sl; tijd = $Script:ArpEerste[$sl]; arp = $true; onbekend = $true; ip = $ipx; mac = $mac2 }
    }
    foreach ($sl in @($Script:ArpLaatst.Keys | Where-Object { $_ -like 'MAC-*' })) {
      if ($gezienNu.ContainsKey($sl)) { continue }
      if ((($nu - $Script:ArpLaatst[$sl].t).TotalSeconds -ge 180) -or -not (Get-Locatie $sl)) { if (($nu - $Script:ArpLaatst[$sl].t).TotalSeconds -ge 600 -or -not (Get-Locatie $sl)) { $Script:ArpLaatst.Remove($sl) }; continue }
      $gezienNu[$sl] = $true; $mac3 = ($sl -replace '^MAC-', '' -replace '(..)(?!$)', '$1-')
      $nieuwe += @{ serie = $sl; tijd = $Script:ArpEerste[$sl]; arp = $true; onbekend = $true; ip = $Script:ArpLaatst[$sl].ip; mac = $mac3 }
    }
    foreach ($k1 in @($Script:ArpEerste.Keys)) { if (-not $gezienNu.ContainsKey($k1)) { $Script:ArpEerste.Remove($k1) } }
  } catch {}
  $lijst = @($blijf)
  foreach ($lz in $lijst) { try { [void](Zorg-Plek $lz.serie) } catch {} }   # 19 okt 2026: elke getoonde laptop staat op een plek (welke maakt niet uit tot de plekken vastgezet worden)
  $rijen = ''; $nB = 0; $nK = 0; $nS = 0; $nH = 0; $Script:TegelInfo = @{}
  foreach ($l in $lijst) {
    $l['sysprepBezig'] = $false
    try { $ipS = "$($l.s['ip'])"; if ($l.klaar -and $ipS -and "$($l.s['fase'])" -match 'Sysprep' -and (($nu - (Get-Item $l.pad).LastWriteTime).TotalSeconds -le 600)) { if ((Ping-Snel $ipS) -and (Is-DezelfdeLaptop $l.serie $ipS $false)) { $l['sysprepBezig'] = $true } } } catch {}
    $kl = Get-Klasse $l $nu
    try { $loc = Get-Locatie $l.serie; if ($loc -match '(?i)tafel\s*(\d+).*?plek\s*(\d+)') { $lvK = $null; try { if (-not $l.klaar -and (((Get-Date) - (Get-Item $l.pad).LastWriteTime).TotalSeconds -le 90)) { $lvK = Get-LiveUitStatus $l.s } } catch {}; $vbK = $null; try { $vbK = Get-Verbinding $l.s } catch {}; $kaart["$([int]$Matches[1]),$([int]$Matches[2])"] = @{ serie=$l.serie; kl=$kl; vb=$vbK; lv=$lvK } } } catch {}
    switch ($kl[0]) { 'bezig' { $nB++ } 'klaar' { $nK++ } 'voorbehoud' { $nK++ } 'herstart' { $nH++ } default { $nS++ } }
    $plan = Get-Plan $l $gem $nu
    $hg = Get-HerstartGem; $herstartTxt = ''
    if ($l.herstart) { $el = ($nu - [datetime]::Parse($l.herstartsinds)).TotalSeconds; $plan.rest += [math]::Max($hg - $el, 60); $herstartTxt = 'Herstarten (sinds ' + ([datetime]::Parse($l.herstartsinds)).ToString('HH:mm') + ', ' + (Fmt-Duur $el) + '; gemiddeld ca. ' + (Fmt-Duur $hg) + ')' }
    $tot = @($Stappen | ? { -not $_.opt }).Count; $gedaan = @($plan.rijen | ? { $_.staat -eq 'klaar' -and -not ($Stappen | ? { $_.id -eq $_.id }) }).Count
    $gedaan = @($plan.rijen | ? { $_.staat -eq 'klaar' }).Count; $totVerplicht = @($plan.rijen | ? { $_.staat -ne 'nvt' -and $_.staat -ne 'optie' }).Count
    if ($totVerplicht -lt 1) { $totVerplicht = 1 }
    $pct = if ($l.klaar) { 100 } else { [int][math]::Min(97, [math]::Round(100 * $gedaan / $totVerplicht)) }
    $Script:TegelInfo["$($l.serie)"] = @{ pct = $pct; rest = $plan.rest; kl = "$($kl[0])"; nu = (Get-NuTekst $l.s['laatste']); start = $(try { if ($l.m['startserver']) { [datetime]::Parse($l.m['startserver']) } else { [datetime]::Parse($l.m['start']) + (Get-KlokVerschil $l) } } catch { $null }); eind = $(if ($l.klaar) { try { (Get-Item $l.pad).LastWriteTime } catch { $nu } } else { $nu }) }
    $start = $(if ($l.m['startserver']) { [datetime]::Parse($l.m['startserver']) } else { [datetime]::Parse($l.m['start']) + (Get-KlokVerschil $l) }); $eind = $nu; if ($l.klaar) { try { $eind = (Get-Item $l.pad).LastWriteTime } catch {} }; $verstreken = [math]::Max(0, ($eind - $start).TotalSeconds)   # 8 okt 2026: bij een KLAAR laptop stopt de totale tijd bij de laatste melding (liep door tot nu)
    $huidig = ($plan.rijen | ? { $_.staat -eq 'bezig' } | select -First 1)
    $hierna = $null; if (-not $l.klaar -and $plan.volgende) { $hierna = $plan.volgende.naam + $(if ($plan.volgende.sec -gt 0) { ' (verwacht ca. ' + (Fmt-Duur $plan.volgende.sec) + ')' } else { '' }) }
    $stapTxt = if ($l.klaar) { 'Afgerond' } elseif ($herstartTxt) { $herstartTxt } elseif ($huidig) { $huidig.naam } else { "$($l.s['fase'])" }
    $eta = if ($l.klaar) { 'klaar' } else { 'ca. ' + $nu.AddSeconds($plan.rest).ToString('HH:mm') + '<br><small>nog ca. ' + (Fmt-Duur $plan.rest) + '</small>' }
    $acc = if ($l.s['accu']) { "$($l.s['accu'])%" + $(if ($l.s['lader'] -eq 'ja') { ' (lader)' } else { ' (accu)' }) } else { '-' }
    $rijen += "<tr class=""rij-$($kl[0])""><td><span class=""badge $($kl[0])"">$(E $kl[1])</span></td><td><a href=""laptop-$(E $l.serie).html""><b>$(E $l.serie)</b></a>$(Get-MerkModel $l.serie)</td><td>$(E (Get-Locatie $l.serie))</td><td>$((Get-WindowsTekst $l.s) -replace '&rarr;','&rarr;')</td>" +
      "<td>$(E $l.s['klant'])<br><small>$(E $l.s['ordernummer']) / $(E $l.s['engineer'])</small></td>" +
      "<td>$(E $stapTxt)<br><small class=""stp"" style=""white-space:normal"">$(try { $sl = @(Get-StapLijst $l.serie $nu 3); if ($sl.Count -gt 1) { 'Laatste stappen: ' + ((@($sl) | ForEach-Object { (E ($_.naam -replace '^\d+\.\s*','')) + ' (' + $_.sec + ' s)' }) -join ' &rsaquo; ') } } catch { '' })</small><small>$(if ($l.klaar) { try { $ecR = Get-Eindcontrole $l; if ($ecR) { '<b>' + (E $ecR.kort) + '</b><br>' } } catch { '' } })$(if ($hierna) { 'Hierna: ' + (E $hierna) + '<br>' })herstarts $($l.herstarts) &middot; mislukt $(E $l.s['mislukt'])</small></td>" +
      "<td><div class=""bar $($kl[0])""><i style=""width:$pct%""></i></div><small>$gedaan van $totVerplicht stappen &middot; $pct%</small></td>" +
      "<td>$(Fmt-Duur $verstreken)<br><small>sinds $($start.ToString('HH:mm'))</small></td><td>$eta</td><td>$(E $acc)</td>" +
      "<td><small>$(E $l.s['laatste'])</small></td></tr>`r`n"
    try { [IO.File]::WriteAllText((Join-Path $ScriptFolder "laptop-$($l.serie).html"), (Maak-Unit (Build-Detail $l $gem $nu $kl)), $utf8) } catch {}
  }
  # 7 okt 2026: AUTOMATISCHE installatie (WinPE-keuze win11-auto, winpe-build\payload-install\install-auto.cmd). Die loopt NIET langs de PXE-poort en Windows draait nog niet,
  # dus er is geen statusbestand van audit-update. De WinPE schrijft per logregel wpelogs\install-<serie>.txt (sleutel=waarde; de SERVER zet de tijd). Hier tonen we dat als 'INSTALLEREN'.
  # Zodra Windows zelf meldt (statusbestand NIEUWER dan het installatiebestand) of het bestand > 4 uur oud is, verdwijnt de rij.
  $instRijen = ''; $inst = @()
  try {
    if (-not $Script:InstStap) { $Script:InstStap = @{} }
    # 7 okt 2026: verzonden bytes van de server-Ethernetkaart (server -> laptop = het ophalen van het Windows-image), voor de snelheids-schatting in stap 5. Houdt ~60 s metingen bij voor een gladde snelheid.
    $txNu = 0.0; try { $txNu = [double](Get-NetAdapterStatistics -Name Ethernet -ErrorAction Stop).SentBytes } catch {}
    if (-not $Script:TxMon) { $Script:TxMon = New-Object System.Collections.ArrayList }
    [void]$Script:TxMon.Add(@{ t = $nu; b = $txNu }); while ($Script:TxMon.Count -gt 1 -and ($nu - $Script:TxMon[0].t).TotalSeconds -gt 60) { $Script:TxMon.RemoveAt(0) }
    $wpeDir =Join-Path (Split-Path -Parent (Split-Path -Parent $StatusDir)) 'wpelogs'
    foreach ($f in @(Get-ChildItem $wpeDir -Filter 'install-*.txt' -ErrorAction SilentlyContinue)) {
      if (($nu - $f.LastWriteTime).TotalHours -ge 4) { continue }
      $kv = @{}; foreach ($r in @(Get-Content $f.FullName -ErrorAction SilentlyContinue)) { if ($r -match '^([a-z]+)=(.*)$') { $kv[$Matches[1]] = $Matches[2].Trim() } }
      if (-not $kv['serie']) { continue }
      $sn = $kv['serie']
      # 8 okt 2026: het statusbestand RECHTSTREEKS van schijf controleren, niet in $lijst: een laptop die KLAAR en losgekoppeld is, verdwijnt uit $lijst (naar 'Eerder klaar') en dan leek de oude WinPE-installatie weer 'bezig' (Roderick: dashboard klopt niet).
      $stPad = Join-Path $StatusDir ("{0}.txt" -f $sn); $st = $null; if (Test-Path $stPad) { $st = @{ pad = $stPad } }
      if ($st -and ((Get-Item $st.pad).LastWriteTime -gt $f.LastWriteTime)) {
        # Windows heeft zich gemeld na de WinPE: duur van de 'eerste start' (stap 7 -> eerste melding) eenmalig bewaren
        try { $v7 = $Script:InstStap[$sn]; if ($v7 -and "$($v7.stap)" -eq '7' -and -not $v7.eerstestart -and $v7.vol -and $kv['modus'] -eq 'LIVE') { Add-Content -Path (Join-Path $StatusDir 'install-stappen.tsv') -Value ("{0}`t{1}`t{2}`t{3}`t{4}`t{5}" -f $sn, $nu.ToString('yyyy-MM-dd HH:mm:ss'), 'LIVE', '8', [int](($nu - $v7.sinds).TotalSeconds), 0) -Encoding UTF8; $v7.eerstestart = $true } } catch {}
        continue
      }
      $kv['mtime'] = $f.LastWriteTime
      # 20 okt 2026 (diagnose, alleen loggen): op de HP flitste de plek 3x kort naar 'Windows start' (installatiebestand) terwijl het Windows-statusbestand bestond. Hier leggen we vast WAAROM deze unit toch als installatie telt (max 1x per 20 s per unit).
      try { if (-not $Script:FlitsLog) { $Script:FlitsLog = @{} }; $stX = Join-Path $StatusDir ("{0}.txt" -f $sn); if ((-not $Script:FlitsLog[$sn]) -or (($nu - $Script:FlitsLog[$sn]).TotalSeconds -ge 20)) { $Script:FlitsLog[$sn] = $nu; $sm = 'ONTBREEKT'; if (Test-Path -LiteralPath $stX) { $sm = (Get-Item -LiteralPath $stX).LastWriteTime.ToString('HH:mm:ss.fff') }; Add-Content -Path (Join-Path (Join-Path (Split-Path -Parent (Split-Path -Parent (Split-Path -Parent $ScriptFolder))) 'logs') 'dashboard-inst-flits.log') -Value ("{0}`t{1}`tstatusbestand mtime={2}`tinstallatiebestand mtime={3}`tstap={4}" -f $nu.ToString('yyyy-MM-dd HH:mm:ss.fff'), $sn, $sm, $f.LastWriteTime.ToString('HH:mm:ss.fff'), $kv['stap']) -Encoding UTF8 } } catch {}
      if (-not $Script:InstRunStart) { $Script:InstRunStart = @{} }
      $stNum = 0; [void][int]::TryParse($kv['stap'], [ref]$stNum)
      # 13 okt 2026 (Roderick: 'de tijden kloppen niet meer op de plek'): de starttijd van deze installatie wordt eerst van schijf gelezen (anders begon de teller na elke herstart van het dashboard opnieuw), en een NIEUWE run begint opnieuw bij de tijd van de netwerkstart: als de poort de unit later heeft gezien dan de bewaarde start, of als de stap weer terugvalt naar een lager nummer
      if (-not $Script:InstRunStart.ContainsKey($sn)) { try { $isf = Join-Path $StatusDir "install-start-$sn.txt"; if (Test-Path -LiteralPath $isf) { $Script:InstRunStart[$sn] = [datetime]::Parse((Get-Content -LiteralPath $isf -Raw).Trim()) } } catch {} }
      $gtN = $null; $nieuwePxe = $false; try { $gtN = $gateTijden[$sn]; $ks = $Script:InstRunStart[$sn]; if ($ks -and $gtN -and $gtN -gt $ks.AddMinutes(3) -and $stNum -le 6) { $nieuwePxe = $true } } catch {}
      # 13 okt 2026 (run 7: totaal toonde 3 u 31 min): win11-auto loopt NIET langs de poort (gate.log), en de eerste stappen duren seconden. Is de bewaarde start OUDER dan de laatste wijziging van het Windows-statusbestand van deze unit, dan hoort die start bij de vorige run: dit is een nieuwe run (begin = eerste waarneming, of de poorttijd als die er is).
      $nieuweNaStatus = $false; try { $stPadN = Join-Path $StatusDir ("{0}.txt" -f $sn); $ksN = $Script:InstRunStart[$sn]; if ($ksN -and (Test-Path -LiteralPath $stPadN) -and (Get-Item -LiteralPath $stPadN).LastWriteTime -gt $ksN.AddMinutes(2) -and $f.LastWriteTime -gt (Get-Item -LiteralPath $stPadN).LastWriteTime) { $nieuweNaStatus = $true } } catch {}
      $teruggegaan = $false; try { $vpS = $Script:InstStap[$sn]; if ($vpS) { $vnS = 0; [void][int]::TryParse("$($vpS.stap)", [ref]$vnS); if ($vnS -gt $stNum) { $teruggegaan = $true } } } catch {}
      if ($kv['modus'] -eq 'LIVE' -and ($nieuwePxe -or $nieuweNaStatus -or ($stNum -le 3 -and (-not $Script:InstRunStart.ContainsKey($sn) -or $Script:InstRunStart[$sn] -lt $nu.AddHours(-4) -or $teruggegaan)))) { $stNieuw = $nu; if ($nieuwePxe -and $gtN) { $stNieuw = $gtN }; $Script:InstRunStart[$sn] = $stNieuw; try { [IO.File]::WriteAllText((Join-Path $StatusDir "install-start-$sn.txt"), $stNieuw.ToString('o')) } catch {} }   # 8 okt 2026: ook op schijf, zodat de TOTALE tijd (WinPE + Windows) een herstart van het dashboard en de overgang overleeft
      # stapovergang: de duur van de VORIGE stap bewaren (alleen als wij die stap vanaf het begin zagen = 'vol'); een nieuwe run (stapnummer lager) begint opnieuw
      $vs = $Script:InstStap[$sn]; $nieuwStap = 0; [void][int]::TryParse($kv['stap'], [ref]$nieuwStap)
      if (-not $vs) { $Script:InstStap[$sn] = @{ stap = $kv['stap']; sinds = $nu; vol = $false; tx0 = $txNu } }
      elseif ($vs.stap -ne $kv['stap']) {
        $vorigNr = 0; [void][int]::TryParse("$($vs.stap)", [ref]$vorigNr)
        if ($nieuwStap -gt $vorigNr) {
          if ($vs.vol -and -not ($kv['laatste'] -match '(?i)\b(STOP|FOUT)\b')) { try { Add-Content -Path (Join-Path $StatusDir 'install-stappen.tsv') -Value ("{0}`t{1}`t{2}`t{3}`t{4}`t{5}" -f $sn, $nu.ToString('yyyy-MM-dd HH:mm:ss'), $kv['modus'], $vs.stap, [int](($nu - $vs.sinds).TotalSeconds), $(if ("$($vs.stap)" -eq '5' -and $txNu -gt $vs.tx0) { [int64]($txNu - $vs.tx0) } else { 0 })) -Encoding UTF8 } catch {} }
          $Script:InstStap[$sn] = @{ stap = $kv['stap']; sinds = $nu; vol = $true; tx0 = $txNu }
        } else { $Script:InstStap[$sn] = @{ stap = $kv['stap']; sinds = $nu; vol = $false; tx0 = $txNu } }
      }
      $kv['sinds'] = $Script:InstStap[$sn].sinds
      $inst += $kv
    }
    $nieuwe = @($nieuwe | Where-Object { $n0 = $_; -not ($inst | Where-Object { $_['serie'] -eq $n0.serie -and $_['mtime'] -gt $n0.tijd }) })
    # 13 okt 2026 (Roderick: 'plek 1 had weer moeten resetten met eerste melding'): zodra de server een unit ziet opstarten via het netwerk (netwerkpoort), begint zijn plek opnieuw: OPGESTART, 0%, totale tijd vanaf dat moment. Anders bleef de oude klare run staan tot de installatie zelf iets meldde.
    foreach ($nn in @($nieuwe)) {
      try {
        $locN = Zorg-Plek $nn.serie
        if ($locN -match '(?i)tafel\s*(\d+).*?plek\s*(\d+)') {
          $kaart["$([int]$Matches[1]),$([int]$Matches[2])"] = @{ serie = $nn.serie; kl = @('bezig', 'OPGESTART', 0); stapkort = $(if ($nn.arp) { "antwoordt op het netwerk ($($nn.ip)), nog geen melding van de laptop" } else { 'netwerkstart gezien, de installatie begint' }) }
          $Script:TegelInfo["$($nn.serie)"] = @{ pct = 0; rest = $null; kl = 'bezig'; start = $nn.tijd; eind = $nu; opgestart = $true }
        }
      } catch {}
    }
    foreach ($k in $inst) {
      $leeftijd = ($nu - $k['mtime']).TotalSeconds; $stp = 0; [void][int]::TryParse($k['stap'], [ref]$stp); $van = 7; [void][int]::TryParse($k['van'], [ref]$van)
      $klaarInst = ($stp -ge $van); $mislukt = ($k['laatste'] -match '(?i)\b(STOP|FOUT)\b')
      $stilInst = ((-not $klaarInst) -and (-not $mislukt) -and $leeftijd -gt 1200)
      $badge = if ($mislukt) { '<span class="badge weg">INSTALLATIE GESTOPT</span>' } elseif ($stilInst) { '<span class="badge stil">STIL ' + (Fmt-Duur $leeftijd) + '</span>' } elseif ($klaarInst) { '<span class="badge herstart">WINDOWS EERSTE START</span>' } else { '<span class="badge bezig">INSTALLEREN</span>' }
      $kl2 = if ($mislukt) { 'weg' } elseif ($stilInst) { 'stil' } elseif ($klaarInst) { 'herstart' } else { 'bezig' }
      try { $loc = Zorg-Plek $k['serie']; if ($loc -match '(?i)tafel\s*(\d+).*?plek\s*(\d+)') { $kaart["$([int]$Matches[1]),$([int]$Matches[2])"] = @{ serie = $k['serie']; kl = @($kl2, 'INSTALLEREN', $leeftijd); inst = $true; stapkort = $(try { $sn0 = ''; if ("$($k['stapnaam'])".Trim()) { $sn0 = ': ' + ("$($k['stapnaam'])" -replace '\s*\(.*$', '').Trim() } elseif ($k['laatste'] -match 'STAP \d+ van \d+:\s*([^(\[]+)') { $sn0 = ': ' + $Matches[1].Trim() }; 'Installeren ' + $stp + '/' + $van + $sn0 } catch { '' }) } } } catch {}
      $pct = [int](100 * [math]::Min($stp, $van) / $van)
      $droog = if ($k['modus'] -eq 'DRYRUN') { ' <b>(DROOGLOOP: er wordt niets gewist)</b>' } else { '' }
      # LIVE-SCHATTING voor stap 5 (Windows ophalen over het netwerk): bytes die de server al verstuurde sinds het begin van de stap, huidige snelheid (laatste ~30 s van de kaart),
      # en het totaal = mediaan van eerdere echte installaties; zonder historie een BOVENGRENS = grootte van install.wim (er kan nooit meer gelezen worden dan het hele bestand).
      # De kaarttelling bevat ALLES wat de server verstuurt (ook dashboard, andere laptops): bij meerdere laptoptegelijk is dit een grove indicatie.
      $live = ''; $dlFrac = $null; $rest5v = $null
      if ($stp -eq 5 -and -not $klaarInst -and -not $mislukt -and $Script:InstStap[$k['serie']] -and $Script:InstStap[$k['serie']].vol) {
        try {
          $sofar = [math]::Max(0.0, $txNu - [double]$Script:InstStap[$k['serie']].tx0)
          $oud = $Script:TxMon | Where-Object { ($nu - $_.t).TotalSeconds -ge 25 } | Select-Object -Last 1
          $mbs = $null; if ($oud) { $dt = ($nu - $oud.t).TotalSeconds; if ($dt -gt 0) { $mbs = ($txNu - $oud.b) / $dt / 1MB } }
          $tot = $null; $bron = ''
          $hb = Get-InstallBytes
          if ($hb) { $tot = $hb.bytes; $bron = "SCHATTING op $($hb.n) eerdere installatie(s)" }
          else { $sh = Get-Cached 'smb' 120 { Get-SmbShare -Name win11 -ErrorAction SilentlyContinue }; if ($sh) { $iw = Join-Path $sh.Path 'sources\install.wim'; if (Test-Path $iw) { $tot = [double](Get-Item $iw).Length; $bron = 'BOVENGRENS: het hele install.wim; het echte aantal is lager' } } }
          if ($tot) { $dlFrac = [math]::Min(0.99, $sofar / $tot) }
          if ($null -ne $mbs -and $mbs -ge 0.5 -and $tot) {
            $rest5 = [math]::Max(0.0, $tot - $sofar) / ($mbs * 1MB); $rest5v = $rest5
            $live = ('Ophalen: {0:N1} van ca. {1:N1} GB, nu {2:N1} MB/s - stap 5 nog ca. {3} ({4})' -f ($sofar / 1GB), ($tot / 1GB), $mbs, (Fmt-Duur $rest5), $bron)
          } elseif ($null -ne $mbs) { $live = ('Ophalen: {0:N1} GB verstuurd, nu {1:N1} MB/s (te laag of geen totaal bekend voor een schatting)' -f ($sofar / 1GB), $mbs) }
        } catch {}
      }
      # verwachte resttijd = (mediaan van de huidige stap - al verstreken, minimaal 0) + mediaan van alle latere stappen (1 t/m 6; stap 7 is het einde). Alleen als ALLE nodige stappen een mediaan hebben.
      $verw = 'geen schatting (nog geen eerdere echte installatie bewaard)'
      if ($k['modus'] -eq 'DRYRUN') { $verw = 'geen schatting in de droogloop' }
      elseif ($klaarInst) {
        $es = Get-InstallEersteStart; $al = ($nu - $k['sinds']).TotalSeconds
        $uitleg = 'Windows rondt zelf zijn installatie af (services starten, apparaten installeren, 1 extra herstart). De server ziet pas weer iets zodra het script in Audit Mode zich meldt. '
        if ($es) { $verw = $uitleg + ('Duurt meestal ca. {0} (SCHATTING op {1} eerdere installatie(s)); al {2} bezig.' -f (Fmt-Duur $es.sec), $es.n, (Fmt-Duur $al)) }
        else { $verw = $uitleg + ('Al {0} bezig; geen schatting (nog geen eerdere eerste start bewaard).' -f (Fmt-Duur $al)) }
      }
      elseif ($mislukt) { $verw = '' }
      else {
        $med = Get-InstallMediaan; $rest = 0.0; $ok = ($med.runs -ge 1)
        for ($i = $stp; $i -le ($van - 1); $i++) {
          if (-not $med.stappen.ContainsKey("$i")) { $ok = $false; break }
          if ($i -eq $stp) { $rest += [math]::Max(0, $med.stappen["$i"] - ($nu - $k['sinds']).TotalSeconds) } else { $rest += $med.stappen["$i"] }
        }
        if ($ok) { $verw = "verwacht klaar ca. $($nu.AddSeconds($rest).ToString('HH:mm')) (nog ca. $(Fmt-Duur $rest); SCHATTING op $($med.runs) eerdere installatie(s), snelheid van netwerk/schijf kan afwijken)" }
      }
      # 13 okt 2026 (Roderick): een unit die installeert (WinPE) krijgt een eigen voortgangsbalk per FASE: DOWNLOADEN (stap 5: bytes die de server al verstuurde / het totaal) en INSTALLEREN (de overige stappen), met de geschatte tijd. De voortgang van de hele run (balk elders) telt de installatie voor ten hoogste 24% mee (run 2: ca. 10 van 41 min).
      $dlDeel = 0.0; if ($stp -eq 5 -and $null -ne $dlFrac) { $dlDeel = [double]$dlFrac }
      $ipct = if ($klaarInst) { 100 } else { [int][math]::Min(99, [math]::Max(0, [math]::Round(100 * (([math]::Max(1, $stp) - 1 + $dlDeel) / [math]::Max(1, $van - 1))))) }
      $fase = if ($mislukt) { 'Gestopt' } elseif ($klaarInst) { 'Windows start' } elseif ($stp -eq 5) { 'Downloaden' } else { 'Installeren' }
      $instRest = $null
      try {
        if (-not $klaarInst -and -not $mislukt) {
          $medT = Get-InstallMediaan
          if ($null -ne $rest5v) { $instRest = [double]$rest5v; for ($ii = 6; $ii -le ($van - 1); $ii++) { if ($medT.stappen.ContainsKey("$ii")) { $instRest += $medT.stappen["$ii"] } } }
          elseif ($medT.runs -ge 1) { $okT = $true; $rT = 0.0; for ($ii = $stp; $ii -le ($van - 1); $ii++) { if (-not $medT.stappen.ContainsKey("$ii")) { $okT = $false; break }; if ($ii -eq $stp) { $rT += [math]::Max(0, $medT.stappen["$ii"] - ($nu - $k['sinds']).TotalSeconds) } else { $rT += $medT.stappen["$ii"] } }; if ($okT) { $instRest = $rT } }
        }
      } catch {}
      $Script:TegelInfo["$($k['serie'])"] = @{ pct = [int][math]::Round($ipct * 0.24); ipct = $ipct; rest = $instRest; kl = "$kl2"; inst = $true; fase = $fase; nu = (Get-NuTekst $k['laatste']); wrest = (Get-WinSchatting $gem); start = $(try { $stI = $(if ($Script:InstRunStart -and $Script:InstRunStart.ContainsKey($k['serie'])) { $Script:InstRunStart[$k['serie']] } else { $k['sinds'] }); $gtI = $gateTijden[$k['serie']]; if ($gtI -and $stI -and $gtI -lt $stI -and ($stI - $gtI).TotalMinutes -lt 30) { $gtI } else { $stI } } catch { $k['sinds'] }); eind = $nu }
      $instRijen += "<tr><td>$badge</td><td><a href=""laptop-$(E $k['serie']).html""><b>$(E $k['serie'])</b></a>$(Get-MerkModel $k['serie'])</td><td>$(E (Get-Locatie $k['serie']))</td><td colspan=""8"">Windows-installatie via het netwerk$droog &middot; stap ${stp} van ${van}: <b>$(E $k['stapnaam'])</b> (al $(Fmt-Duur ($nu - $k['sinds']).TotalSeconds) in deze stap)$(if ($verw) { ' &middot; <b>' + (E $verw) + '</b>' })$(if ($live) { '<br><b>' + (E $live) + '</b>' })<br><div class=""bar""><i style=""width:$pct%""></i></div><small>Schijf: $(E $k['schijf']) &middot; editie: $(E $k['editie']) &middot; laatste melding $(Fmt-Duur $leeftijd) geleden: $(E $k['laatste'])</small></td></tr>`r`n"
      try { [IO.File]::WriteAllText((Join-Path $ScriptFolder "laptop-$($k['serie']).html"), (Maak-Unit (Build-InstDetail $k $stp $van $pct $verw $live $badge $nu $leeftijd)), $utf8) } catch {}   # 12 okt 2026: eigen detailpagina voor een laptop die nu installeert
      if ($kl2 -eq 'bezig') { $nB++ } elseif ($kl2 -eq 'herstart') { $nH++ }
    }
  } catch { }
  $nieuweRijen = $instRijen
  foreach ($n in $nieuwe) {
    $sec = [int]($nu - $n.tijd).TotalSeconds
    $tekstN = "Gezien door de server via het netwerk om $($n.tijd.ToString('HH:mm:ss')) ($(Fmt-Duur $sec) geleden). De laptop wordt nu klaargezet; de eerste melding van de laptop zelf volgt over enkele minuten."
    if ($n.onbekend) { $tekstN = "Onbekend apparaat op het netwerk: MAC $($n.mac), IP $($n.ip), de server ziet het sinds $($n.tijd.ToString('HH:mm:ss')). Er is nog geen serienummer; deze plek is tijdelijk automatisch toegewezen en gaat over naar de laptop zodra die zich meldt." }
    elseif ($n.arp) { $tekstN = "Aan het opstarten: de server ziet deze laptop sinds $($n.tijd.ToString('HH:mm:ss')) ($(Fmt-Duur $sec) geleden) bereikbaar op het netwerk (IP $($n.ip), MAC $($n.mac) klopt met eerdere runs). Er is nog geen melding van de laptop zelf." }
    try { $loc = Zorg-Plek $n.serie; if ($loc -match '(?i)tafel\s*(\d+).*?plek\s*(\d+)') { $kaart["$([int]$Matches[1]),$([int]$Matches[2])"] = @{ serie = $n.serie; kl = @('nieuw', 'OPGESTART', $sec); stapkort = $(if ($n.arp) { "aan het opstarten ($($n.ip))" } else { '' }) } } } catch {}
    $nieuweRijen += "<tr class=""rij-nieuw""><td><span class=""badge nieuw"">OPGESTART</span></td><td><b>$(E $n.serie)</b></td><td>$(E (Get-Locatie $n.serie))</td><td colspan=""8"">$(E $tekstN)</td></tr>`r`n"
  }
  $rijen = $nieuweRijen + $rijen
  $eerderHtml = ''
  if ($eerder.Count -gt 0) {
    $rapportScript = Join-Path (Split-Path -Parent (Split-Path -Parent (Split-Path -Parent $ScriptFolder))) 'scripts\121-genereer-rapport.ps1'
    $eerderHtml = "<details class=""eerder""><summary>Eerder klaar en losgekoppeld ($($eerder.Count))</summary><ul>"
    foreach ($e in $eerder) {
      # 8 okt 2026: ook de detailpagina van een eerder-klaar laptop (opnieuw) maken, hooguit elke 5 min: anders bleef de totale tijd staan op de waarde van het laatste moment in de hoofdtabel
      try { $dp = Join-Path $ScriptFolder "laptop-$($e.serie).html"; if (@($inst | Where-Object { $_['serie'] -eq $e.serie }).Count -eq 0 -and (-not (Test-Path $dp) -or ((Get-Date) - (Get-Item $dp).LastWriteTime).TotalMinutes -ge 5)) { $kle = Get-Klasse $e $nu; [IO.File]::WriteAllText($dp, (Maak-Unit (Build-Detail $e $gem $nu $kle)), $utf8) } } catch {}
      $rk = "$($e.m['runkey'])"; $rap = Join-Path $ScriptFolder "rapporten\$($e.serie)__$rk.html"
      if ($rk -and -not (Test-Path $rap) -and (Test-Path $rapportScript)) { try { & $rapportScript -Serienummer $e.serie -Root (Split-Path -Parent (Split-Path -Parent (Split-Path -Parent $ScriptFolder))) *> $null } catch {} }
      $eerderHtml += "<li><a href=""laptop-$(E $e.serie).html"">$(E $e.serie)</a> $(Get-MerkModel $e.serie) &middot; $(E (Get-Locatie $e.serie)) &middot; $(if ($e.eindmelding -eq 'ontbreekt') { 'laatste melding ' + (Get-Item $e.pad).LastWriteTime.ToString('dd-MM HH:mm') + ' - GEEN eindmelding ontvangen (de server weet niet of de run is afgerond; kijk in het laptoplog/archief)' } else { 'klaar om ' + (Get-Item $e.pad).LastWriteTime.ToString('HH:mm') })$(if (Test-Path $rap) { ' &middot; <a href=""rapporten/' + (E $e.serie) + '__' + (E $rk) + '.html"">rapport in gewone taal</a>' })</li>"
    }
    $eerderHtml += '</ul></details>'
  }
  # 7 okt 2026: automatisch openen in de standaardbrowser staat nu UIT (Roderick kreeg tijdens testen een venster om een app te kiezen). Aanzetten: leeg bestand gate-state\dashboard-autoopen.flag aanmaken.
if ((Test-Path (Join-Path $StateDir 'dashboard-autoopen.flag')) -and -not $Script:BrowserGeopend -and ($nB -gt 0 -or $nH -gt 0 -or $nieuwe.Count -gt 0)) { $Script:BrowserGeopend = $true; try { Start-Process 'http://192.168.77.1/dashboard.html' } catch {} }   # opent zichzelf zodra er een laptop bezig is (1x per generator-sessie)
  $serverBlok = ''
  try {
    $bezig = @()
    $procs = @((Get-Cached 'procs' 15 { Get-CimInstance Win32_Process -ErrorAction SilentlyContinue }) | ? { $_.CommandLine -and ($_.CommandLine -match 'DownloadUpdates\.cmd|hashdeep64|Build-[A-Za-z]+\.ps1|Process-CacheRequests\.ps1|80-cache-onderhoud\.ps1|93-kopieer-stick') -and $_.ProcessId -ne $PID })
    if ($procs.Count -gt 0) {
      if ($procs | ? { $_.CommandLine -match 'hashdeep64' }) { $bezig += 'controlesommen berekenen (hashen) - kan enkele minuten stil lijken' }
      if ($procs | ? { $_.CommandLine -match 'DownloadUpdates\.cmd' }) { $bezig += 'WSUS Offline-cache bijwerken (Windows-updates downloaden en controleren)' }
      foreach ($pr in ($procs | ? { $_.CommandLine -match 'Build-([A-Za-z]+)\.ps1' })) { $null = $pr.CommandLine -match 'Build-([A-Za-z]+)\.ps1'; $bezig += "bouwscript Build-$($Matches[1])" }
      if ($procs | ? { $_.CommandLine -match '93-kopieer-stick' }) { $bezig += 'stick-cache naar de server kopieren' }
      if ($procs | ? { $_.CommandLine -match 'Process-CacheRequests|80-cache-onderhoud' }) { $bezig += 'cache-vuller / dagelijks onderhoud' }
      $logRegels = ''
      $dlLog = Join-Path (Split-Path $StatusDir -Parent) 'WSUS\wsus-offline\log\download.log'
      if ((Test-Path $dlLog) -and ((($nu - (Get-Item $dlLog).LastWriteTime).TotalMinutes) -gt 10)) { $logRegels = '<small>(het WSUS Offline-downloadlog is van ' + (Get-Item $dlLog).LastWriteTime.ToString('dd-MM HH:mm') + ' en dus niet actueel: niet getoond)</small>' }   # 17 okt 2026: een OUD log werd getoond alsof het live was (Roderick zag 'Deleted file' van 13 okt)
      elseif (Test-Path $dlLog) { $laatste = @(Get-Content $dlLog -Tail 8 -ErrorAction SilentlyContinue | % { ($_ -replace '^\S+\s+\d{2}-\d{2}-\d{4}\s+','') -replace 'http://\S+/',' ...' }); $leeft = [int](($nu - (Get-Item $dlLog).LastWriteTime).TotalSeconds); $logRegels = '<div style="font-family:Consolas,monospace;font-size:12px;opacity:.85;margin-top:6px">' + (($laatste | % { E ($_.Substring(0,[Math]::Min(150,$_.Length))) }) -join '<br>') + "</div><small>laatste logregel $leeft s geleden</small>" }
      $serverBlok = '<div class="tegel" style="margin:12px 0;border-left:4px solid #d97706"><b>De server is bezig</b>' + (($bezig | Select-Object -Unique | % { E $_ }) -join '<br>') + $logRegels + '</div>'
    } else { $serverBlok = '<div class="sub" style="margin:10px 0">Server: niets bezig (geen download, build of kopie actief).</div>' }
  } catch { $serverBlok = '' }
  # 19 okt 2026 (Roderick: 'de plek verdwijnt kort'; waarnemer scripts\303 zag het om 22:14:46 en 22:23:17; Update-Laptop was toen NIET mislukt): leg LETTERLIJK vast waar een unit zat in de ronde waarin zijn plek leeg is
  # (in welke lijsten hij stond, tijden van status- en installatiebestand), zodat de echte oorzaak uit logs\dashboard-plek-leeg.log blijkt en niet geraden hoeft te worden.
  try {
    if (-not $Script:VorigeKaart) { $Script:VorigeKaart = @{} }
    foreach ($kk in @($Script:VorigeKaart.Keys)) {
      if (-not $kaart.ContainsKey($kk)) {
        $sn9 = $Script:VorigeKaart[$kk]
        $spad9 = Join-Path $StatusDir ("{0}.txt" -f $sn9); $ipad9 = Join-Path (Join-Path (Split-Path -Parent (Split-Path -Parent $StatusDir)) 'wpelogs') ("install-{0}.txt" -f $sn9)
        $smt9 = $(if (Test-Path -LiteralPath $spad9) { (Get-Item -LiteralPath $spad9).LastWriteTime.ToString('HH:mm:ss.fff') } else { 'geen' }); $imt9 = $(if (Test-Path -LiteralPath $ipad9) { (Get-Item -LiteralPath $ipad9).LastWriteTime.ToString('HH:mm:ss.fff') } else { 'geen' })
        $r9 = "{0}  PLEK-LEEG plek {1} serie={2} | inLijst(blijf)={3} inEerder={4} inInst={5} inNieuwe={6} | statusbestand mtime={7} installatiebestand mtime={8} | kaartsleutels nu: {9}" -f $nu.ToString('yyyy-MM-dd HH:mm:ss.fff'), $kk, $sn9, [bool]($lijst | Where-Object { $_.serie -eq $sn9 }), [bool]($eerder | Where-Object { $_.serie -eq $sn9 }), [bool]($inst | Where-Object { $_['serie'] -eq $sn9 }), [bool]($nieuwe | Where-Object { $_.serie -eq $sn9 }), $smt9, $imt9, (($kaart.Keys | Sort-Object) -join ' ')
        try { Add-Content -Path (Join-Path (Join-Path (Split-Path -Parent (Split-Path -Parent (Split-Path -Parent $ScriptFolder))) 'logs') 'dashboard-plek-leeg.log') -Value $r9 -Encoding UTF8 } catch {}
      }
    }
    $Script:VorigeKaart = @{}; foreach ($kk in @($kaart.Keys)) { try { $Script:VorigeKaart[$kk] = "$($kaart[$kk].serie)" } catch {} }
  } catch {}
  $plattegrond = ''
  try {
    $instAantalPlek = @($inst | Where-Object { try { (($nu - $_['mtime']).TotalSeconds -le 120) } catch { $false } }).Count; $srvNu = $null; try { $srvNu = Get-ServerSnelheidNu $nu } catch {}
  $plattegrond = '<div class="tafels">'
    foreach ($tf in 1..2) {
      $plattegrond += "<div class=""tafel""><h2>Tafel $tf</h2><div class=""plekken"">"
      foreach ($pl in 1..12) {
        $c = $kaart["$tf,$pl"]
        if ($c) { $plattegrond += "<div class=""plek $($c.kl[0])""><small>plek $pl</small>$(try { Get-PlekMerk $c.serie } catch { '' })<a href=""laptop-$(E $c.serie).html"" title=""$(E $c.serie)"">$(Get-PlekBalk $c)</a>$(try { $lvh = ''; if ($c.lv -and ($null -ne $c.lv.dn -or $null -ne $c.lv.up)) { $lvh = Get-LiveBalkHtml $c.lv.dn $c.lv.up 'van de laptop zelf' } elseif ($c.inst -and $instAantalPlek -eq 1 -and $srvNu) { $lvh = Get-LiveBalkHtml $srvNu.naar $srvNu.van 'server-kabel, 1 laptop installeert' } elseif ("$($c.kl[0])" -notin @('klaar', 'voorbehoud')) { $lvh = Get-LiveBalkHtml 0 0 'geen verkeer gemeld (of niet te meten)' }; $lvh } catch { '' })$(try { $sk = $(if ($c.stapkort) { @{ tekst = $($ik = "$($c.stapkort)"; if ($ik.Length -gt 34) { $ik.Substring(0,33) + '...' } else { $ik }); tip = "$($c.stapkort)" } } else { Get-StapKort $c.serie $nu }); if ($sk) { '<small class="stp" title="' + (E $sk.tip) + '">&rsaquo; ' + (E $sk.tekst) + '</small>' } } catch { '' })$(if ($c.vb -and $c.vb.txt) { '<small class=""vb vb-' + $c.vb.klas + '"" title=""' + (E $c.vb.tip) + '"">' + $c.vb.txt + '</small>' })</div>" }
        else { $plattegrond += "<div class=""plek""><small>plek $pl</small></div>" }
      }
      $plattegrond += '</div></div>'
    }
    $plattegrond += '</div>'
  } catch { $plattegrond = '' }
  if (-not $lijst -and -not $nieuwe.Count -and -not $inst.Count) { $rijen = '<tr><td colspan="9" class="leeg">Nog geen laptops gemeld. Een laptop verschijnt hier zodra audit-update.ps1 (v3.251 of nieuwer) zijn eerste voortgang meldt.</td></tr>' }
  Add-NetSample $nu
  $netPaneel = ''; try { $netPaneel = Get-NetPaneel $nu $lijst $inst } catch { $netPaneel = '' }
  $snelTegel = ''
  try {
    $na = Get-EthAdapter
    if ($na.Status -ne 'Up') {
      $script:snelVorig = $null
      $snelTegel = '<div class="tegel" style="border-left:4px solid #dc2626"><b>geen verbinding</b>server-netwerk (Ethernet) is NIET verbonden</div>'
    } else {
      $st = Get-NetAdapterStatistics -Name 'Ethernet' -ErrorAction Stop
      $tel = [double]($st.SentBytes + $st.ReceivedBytes)
      # 30 okt 2026 (Roderick, 3 schermen: "ik zie geen up of download"): de tegel toonde "meten..." zodra twee metingen < 1 s uit elkaar lagen (de generator draait ongeveer elke seconde). Nu: bij < 1 s blijft de vorige waarde staan; download (naar de units) en upload (van de units) apart.
      $mbpsIn = $null; $mbpsUit = $null
      if ($script:snelVorig -and $nu -gt $script:snelVorig.t) {
        $dt = ($nu - $script:snelVorig.t).TotalSeconds
        if ($dt -ge 1 -and $tel -ge $script:snelVorig.b) {
          $mbpsIn = ($tel - $script:snelVorig.b) / $dt / 1MB
          $uitNu = [double]$st.SentBytes; $inNu = [double]$st.ReceivedBytes
          if ($null -ne $script:snelVorig.u -and $uitNu -ge $script:snelVorig.u -and $inNu -ge $script:snelVorig.i) { $mbpsUit = ($uitNu - $script:snelVorig.u) / $dt / 1MB; $mbpsIn = ($inNu - $script:snelVorig.i) / $dt / 1MB }
          $script:snelLaatst = @{ naar = $mbpsUit; van = $mbpsIn }
        }
      }
      if (-not $script:snelVorig -or ($nu - $script:snelVorig.t).TotalSeconds -ge 1) { $script:snelVorig = @{ t = $nu; b = $tel; u = [double]$st.SentBytes; i = [double]$st.ReceivedBytes } }
      $snelTxt = 'meten...'; $snelOnder = 'server-netwerk nu'
      if ($script:snelLaatst) { $snelTxt = ('{0:N1} MB/s' -f $script:snelLaatst.naar); $snelOnder = ('naar de units (download) &middot; van de units {0:N1} MB/s' -f $script:snelLaatst.van) }
      $snelTegel = '<div class="tegel"><b>' + $snelTxt + '</b>' + $snelOnder + ' (link ' + (E $na.LinkSpeed) + ')</div>'
    }
  } catch { $snelTegel = '' }
  # 17 okt 2026 (Roderick: 'de vrijgeef ook op het dashboard ... met code'): blok bovenaan met laptops die op de verificatiestop wachten, met knop Vrijgeven (alleen zichtbaar op de server zelf; vraagt een toegangscode).
  $vrijgaveBlok = ''
  try {
    $wachtRijen = ''
    foreach ($lw in @($lijst)) {
      $vfw = "$($lw.s['verificatie'])".Trim(); if ($vfw -notin @('wacht', 'stop') -or $lw.klaar) { continue }
      try { if (($nu - (Get-Item $lw.pad).LastWriteTime).TotalSeconds -gt 2400) { continue } } catch {}
      $snw = ("$($lw.serie)" -replace '[^A-Za-z0-9_-]', '')
      $wachtRijen += '<div style="margin:6px 0"><b>' + (E $snw) + '</b> ' + (E "$($lw.s['model'])") + ' &mdash; wacht op ' + $(if ($vfw -eq 'stop') { 'een engineer (stop voor Sysprep)' } else { 'de volledigheidscontrole van de server' }) + ' <button type="button" onclick="espritActie(''vrij'',''' + $snw + ''')" style="font-size:15px;padding:6px 14px;margin-left:10px;background:#15803d;color:#fff;border:0;border-radius:6px;cursor:pointer">Vrijgeven</button></div>'
    }
    if ($wachtRijen) { $vrijgaveBlok = '<div class="alleen-lokaal tegel" style="margin:12px 0;border-left:6px solid #15803d"><b>Wacht op vrijgave</b>' + $wachtRijen + '<div id="actie-uitslag" style="margin-top:6px;font-weight:600"></div></div>' }
  } catch { $vrijgaveBlok = '' }  $html = "<!doctype html><html lang=""nl""><head><meta charset=""utf-8"">$LiveHead<meta name=""viewport"" content=""width=device-width,initial-scale=1""><title>Laptop-voortgang</title><style>$css</style>$ActieKop</head><body><div class=""wrap""><h1>Laptop-voortgang</h1><p style=""margin:0 0 6px""><a href=""update-overzicht.html"">Update-overzicht (alle updates met datums, USB en laptops)</a> &middot; <a href=""historie.html""><b>Historie van alle runs</b> (filters en gemiddelden)</a> &middot; <a href=""geschiedenis.html""><b>Geschiedenis per unit</b> (zoeken op serienummer)</a></p>" +
    "<div class=""sub"">Ververst vanzelf elke seconde &middot; bijgewerkt om $($nu.ToString('HH:mm:ss')) &middot; klik op een serienummer voor het stappenplan, de geschiedenis en eerdere runs</div>" +
    "<div class=""tegels""><div class=""tegel""><b>$($lijst.Count + $nieuwe.Count + $inst.Count)</b>laptops</div>$(Get-TopTegels)$snelTegel</div>" +
    (Get-OnderhoudHtml) + (Get-KennisbankHtml) + $serverBlok + $vrijgaveBlok +
    $netPaneel +
    $plattegrond +
    "<table><thead><tr><th>Status</th><th>Serienummer</th><th>Locatie</th><th>Windows</th><th>Klant / order</th><th>Nu bezig met</th><th>Voortgang</th><th>Verstreken</th><th>Verwacht klaar (schatting)</th><th>Batterij</th><th>Laatste actie</th></tr></thead><tbody>$rijen</tbody></table>" +
    $eerderHtml +
    "<div class=""voet"">VERWACHT KLAAR is alleen een SCHATTING op basis van eerdere runs (meestal herhalingen). Bij een net opnieuw geinstalleerde laptop duurt het vaak veel langer; kijk dan naar de voortgangsstappen &middot; OPGESTART = de server ziet de laptop via het netwerk (PXE), de eerste melding van de laptop zelf volgt nog &middot; KLAAR en losgekoppeld (geen antwoord, melding &gt; 2 min oud) verdwijnt naar &quot;Eerder klaar en losgekoppeld&quot; &middot; BEZIG = melding &lt; 5 min &middot; HERSTART = geen melding en de laptop start opnieuw op (duur wordt bewaard) &middot; STIL = 5-15 min (melding blijft uit terwijl de laptop wel reageert) &middot; GEEN SIGNAAL = &gt; 15 min &middot; KLAAR = Sysprep gestart. Geschiedenis en afgeronde runs worden bewaard in $(E $StatusDir).</div></div></body></html>"
  $tmp = Join-Path $ScriptFolder 'dashboard.tmp'
  [IO.File]::WriteAllText($tmp, (Maak-Unit $html), $utf8)
  Move-Item -Path $tmp -Destination (Join-Path $ScriptFolder 'dashboard.html') -Force -ErrorAction SilentlyContinue
}

# 8 okt 2026 (Roderick): historie.html met alle runs, filters en gemiddelden (historie-genereer.ps1), hooguit 1x per minuut
$Script:HistorieLaatst = [datetime]::MinValue
try { $hg = Join-Path $ScriptFolder 'historie-genereer.ps1'; if (Test-Path $hg) { . $hg } } catch {}
# 9 okt 2026 (Roderick): laptops leggen hardware-ID's neer (status\wudrivers\<model>.txt); de server zoekt daarmee drivers op de Microsoft Update Catalog (scripts\171). Hier: elke 5 min kijken of er een onverwerkt verzoek is en 171 dan 1x verborgen starten.
$Script:WuLaatst = [datetime]::MinValue
$Script:VolLaatst = [datetime]::MinValue
$Script:GeschLaatst = [datetime]::MinValue
while (-not (Test-Path $StopFlag)) {
  try { Ensure-SnelheidsTestbestand } catch {}
  # 10 okt 2026 (Roderick: 'laat de SERVER telkens online checken of er nog iets openstaat'): hieronder start 171 elke 5 min (alleen echt werk bij een nieuw of GEWIJZIGD verzoek) en elk uur met -Hercontroleer (de server houdt alle bekende modellen opnieuw tegen de Microsoft Update Catalog en zet wat nieuw is in de cache)
  try { if (((Get-Date) - $Script:WuLaatst).TotalSeconds -ge 300) { $Script:WuLaatst = Get-Date; $wuMap = Join-Path $StatusDir 'wudrivers'; $wuScript = Join-Path (Split-Path -Parent (Split-Path -Parent (Split-Path -Parent $ScriptFolder))) 'scripts\187-server-cache-onderhoud.ps1'; if ((Test-Path $wuMap) -and (Test-Path $wuScript)) { $open = @(Get-ChildItem $wuMap -Filter '*.txt' -File | Where-Object { -not (Test-Path ([IO.Path]::ChangeExtension($_.FullName, '.klaar'))) }); $loopt = @(Get-CimInstance Win32_Process -Filter "Name='powershell.exe'" -ErrorAction SilentlyContinue | Where-Object { $_.CommandLine -match '171-verwerk-wudrivers|186-haal-extra|187-server-cache' }); if ($loopt.Count -eq 0) { $a171 = @('-NoProfile','-ExecutionPolicy','Bypass','-File',"""$wuScript"""); if (-not $Script:WuHerLaatst -or ((Get-Date) - $Script:WuHerLaatst).TotalMinutes -ge 60) { $a171 += '-Uurlijks'; $Script:WuHerLaatst = Get-Date }; Start-Process powershell.exe -WindowStyle Hidden -ArgumentList $a171 } } } } catch {}
  try { if (((Get-Date) - $Script:HistorieLaatst).TotalSeconds -ge 60 -and (Get-Command Schrijf-Historie -ErrorAction SilentlyContinue)) { $Script:HistorieLaatst = Get-Date; $arch = Join-Path (Split-Path -Parent (Split-Path -Parent (Split-Path -Parent $ScriptFolder))) 'archief'; [void](Schrijf-Historie $StatusDir $arch (Join-Path $ScriptFolder 'historie.html')) } } catch {}   # goedkoop als het bestand er al is
  try { if (((Get-Date) - $Script:VolLaatst).TotalSeconds -ge 120) { $Script:VolLaatst = Get-Date; $volScript = Join-Path (Split-Path -Parent (Split-Path -Parent (Split-Path -Parent $ScriptFolder))) 'scripts\180-volledigheid-fabrikant.ps1'; if (Test-Path $volScript) { $loopt2 = @(Get-CimInstance Win32_Process -Filter "Name='powershell.exe'" -ErrorAction SilentlyContinue | Where-Object { $_.CommandLine -match '180-volledigheid-fabrikant' }); if ($loopt2.Count -eq 0) { Start-Process powershell.exe -WindowStyle Hidden -ArgumentList '-NoProfile','-ExecutionPolicy','Bypass','-File',"""$volScript""",'-StatusDir',"""$StatusDir""" } } } } catch {}  try { Genereer } catch {}
  # 17 okt 2026 (Roderick: 'alle laptops moeten gelogd blijven zodat ik in de toekomst dingen kan terugzoeken, maar een overzichtelijk overzicht van history, opzoekbaar op serienummer'): elke 5 min eerst 261 (afgeronde runs archiveren, alleen KOPIEREN) en daarna 260 (geschiedenis.html), verborgen en nooit dubbel.
  try { if (((Get-Date) - $Script:GeschLaatst).TotalSeconds -ge 300) { $Script:GeschLaatst = Get-Date; $sdir = Join-Path (Split-Path -Parent (Split-Path -Parent (Split-Path -Parent $ScriptFolder))) 'scripts'; $gl = @(Get-CimInstance Win32_Process -Filter "Name='powershell.exe'" -ErrorAction SilentlyContinue | Where-Object { $_.CommandLine -match '261-auto-archiveer|260-genereer-geschiedenis' }); if ($gl.Count -eq 0 -and (Test-Path (Join-Path $sdir '260-genereer-geschiedenis.ps1'))) { $cmd = "& '$sdir\261-auto-archiveer.ps1'; & '$sdir\260-genereer-geschiedenis.ps1'"; Start-Process powershell.exe -WindowStyle Hidden -ArgumentList '-NoProfile','-ExecutionPolicy','Bypass','-Command',"""$cmd""" } } } catch {}
  Start-Sleep -Milliseconds 250
}
