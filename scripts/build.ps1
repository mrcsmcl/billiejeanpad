param(
  [string]$SiteUrl = ""
)

$ErrorActionPreference = "Stop"
$projectRoot = Split-Path -Parent $PSScriptRoot
$configPath = Join-Path $projectRoot "group.local.json"
$distDir = Join-Path $projectRoot "dist"

function New-RandomHex([int]$ByteCount) {
  $bytes = New-Object byte[] $ByteCount
  $rng = [System.Security.Cryptography.RandomNumberGenerator]::Create()
  try { $rng.GetBytes($bytes) } finally { $rng.Dispose() }
  return -join ($bytes | ForEach-Object { $_.ToString("x2") })
}

function Get-TopicPrefix([string]$Key) {
  $sha = [System.Security.Cryptography.SHA256]::Create()
  try {
    $hash = $sha.ComputeHash([System.Text.Encoding]::UTF8.GetBytes($Key))
  } finally {
    $sha.Dispose()
  }
  $publicId = -join ($hash[0..15] | ForEach-Object { $_.ToString("x2") })
  return "bjp-$publicId"
}

if (Test-Path -LiteralPath $configPath) {
  $saved = Get-Content -LiteralPath $configPath -Raw | ConvertFrom-Json
  # One-time migration from the old two-value schema.
  if ($saved.groupKey) {
    $groupKey = [string]$saved.groupKey
  } elseif ($saved.hmacKey) {
    $groupKey = [string]$saved.hmacKey
    [pscustomobject]@{ groupKey = $groupKey } |
      ConvertTo-Json |
      Set-Content -LiteralPath $configPath -Encoding UTF8
    Write-Host "Migrated group.local.json to the single-key format."
  } else {
    throw "group.local.json does not contain groupKey"
  }
} else {
  $groupKey = New-RandomHex 32
  [pscustomobject]@{ groupKey = $groupKey } |
    ConvertTo-Json |
    Set-Content -LiteralPath $configPath -Encoding UTF8
  Write-Host "Created private group key: $configPath"
}

if ($groupKey -notmatch '^[0-9a-f]{64}$') {
  throw "group.local.json: groupKey must be exactly 64 lowercase hex characters"
}

$topicPrefix = Get-TopicPrefix $groupKey
New-Item -ItemType Directory -Force -Path $distDir | Out-Null

function Invoke-BillieJeanBuild([string]$FileName, [bool]$DeleteLauncher, [bool]$GenericUpdate = $false) {
  $outputPath = Join-Path $distDir $FileName
  $deleteValue = if ($DeleteLauncher) { "true" } else { "false" }
  $embeddedKey = if ($GenericUpdate) { "UNSET" } else { $groupKey }
  $ldflags = "-s -w -H windowsgui -X main.GroupKey=$embeddedKey -X main.DeleteLauncher=$deleteValue"

  & go build -trimpath -ldflags $ldflags -o $outputPath .
  if ($LASTEXITCODE -ne 0) {
    throw "go build failed for $FileName with exit $LASTEXITCODE"
  }

  $sizeMB = [math]::Round((Get-Item $outputPath).Length / 1MB, 2)
  Write-Host "  $FileName ($sizeMB MB)" -ForegroundColor Green
}

Push-Location $projectRoot
try {
  Write-Host "Building Billie Jean Pad..."
  Invoke-BillieJeanBuild "BillieJeanPad-Share.exe" $true
  Invoke-BillieJeanBuild "BillieJeanPad-Keep.exe" $false
  Invoke-BillieJeanBuild "BillieJeanPad-Update.exe" $false $true
} finally {
  Pop-Location
}

Write-Host ""
Write-Host "Builds ready in: $distDir" -ForegroundColor Green
Write-Host "  Share:  installs, then deletes only the downloaded launcher."
Write-Host "  Keep:   installs and keeps the downloaded launcher."
Write-Host "  Update: generic signed-release payload; requires protected local state."
Write-Host ""
Write-Host "Group key: $groupKey"
Write-Host "Derived public topic: $topicPrefix"

if ($SiteUrl) {
  $cleanUrl = $SiteUrl.TrimEnd('/') + '/'
  $server = [uri]::EscapeDataString("https://ntfy.sh")
  $encodedKey = [uri]::EscapeDataString($groupKey)
  $invite = "${cleanUrl}#key=${encodedKey}&server=${server}"
  Write-Host ""
  Write-Host "Private controller invite (send only to group members):"
  Write-Host $invite -ForegroundColor Yellow
}
