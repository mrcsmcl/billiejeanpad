# Copies curated WAVs from the Michael Jackson sound pack into the project
# sounds/ tree, renaming them to lowercase with underscores so they match the
# ids used in control.html / ByName lookups.
#
# Run from the project root:  pwsh -File scripts\curate.ps1

$ErrorActionPreference = "Stop"

$pack = "C:\Users\mrcsmcl\Desktop\Michael Jackson\Pack\Michael Jackson - King of Pop (Sound Pack)"
$projectRoot = Split-Path -Parent $PSScriptRoot
$out = Join-Path $projectRoot "sounds"

$map = @{
  "subtle" = @(
    "Vocal Chants\Hee.wav",  "Vocal Chants\Huh.wav",  "Vocal Chants\Uh.wav",
    "Vocal Chants\Hoo.wav",  "Vocal Chants\He.wav",   "Vocal Chants\Kiss.wav",
    "Vocal Chants\Sshh.wav", "Vocal Chants\Ha.wav",   "Vocal Chants\Ha 2.wav",
    "Vocal Chants\Ooh.wav"
  )
  "punchy" = @(
    "Vocal Chants\Hehe.wav",       "Vocal Chants\Hehehe.wav",
    "Vocal Chants\AH.wav",         "Vocal Chants\Aah.wav",
    "Vocal Chants\Ooh strong.wav", "Vocal Chants\Go.wav",
    "Vocal Chants\No no.wav",      "Vocal Chants\Oh baby.wav",
    "Vocal Chants\Daoh.wav"
  )
  "scare" = @(
    "Sounds\Stab Hit 3.wav",
    "Sounds\Stab Hit 5.wav",
    "Sounds\DJ Scratches - One Shot.wav"
  )
}

$total = 0
foreach ($cat in $map.Keys) {
  $dstDir = Join-Path $out $cat
  New-Item -ItemType Directory -Force -Path $dstDir | Out-Null

  foreach ($rel in $map[$cat]) {
    $src = Join-Path $pack $rel
    if (-not (Test-Path -LiteralPath $src)) {
      Write-Warning "missing: $src"
      continue
    }

    $leaf = (Split-Path -Leaf $rel).ToLower()
    $leaf = $leaf -replace " ", "_"
    $leaf = $leaf -replace "_-_", "_"
    $leaf = $leaf -replace "_one_shot", ""     # trim redundant suffix
    $leaf = $leaf -replace "__+", "_"

    $dst = Join-Path $dstDir $leaf
    Copy-Item -LiteralPath $src -Destination $dst -Force
    $total++
  }
}

Write-Host "Copied $total WAVs into $out"
Get-ChildItem $out -Recurse -Filter *.wav | ForEach-Object {
  "  {0,-8} {1,8:N0} B  {2}" -f $_.Directory.Name, $_.Length, $_.Name
}
