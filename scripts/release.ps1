param(
  [switch]$Upload
)

$ErrorActionPreference = "Stop"
$projectRoot = Split-Path -Parent $PSScriptRoot
Push-Location $projectRoot
try {
  powershell -ExecutionPolicy Bypass -File .\scripts\build.ps1
  if ($LASTEXITCODE -ne 0) { throw "build failed" }

  $configSource = Get-Content .\config.go -Raw
  $match = [regex]::Match($configSource, 'Version\s*=\s*"([0-9]+\.[0-9]+\.[0-9]+)"')
  if (-not $match.Success) { throw "could not read Version from config.go" }
  $version = $match.Groups[1].Value
  $tag = "v$version"
  $asset = ".\dist\BillieJeanPad-Update.exe"
  $assetUrl = "https://github.com/mrcsmcl/billiejeanpad/releases/download/$tag/BillieJeanPad-Update.exe"

  go run ./cmd/releaser `
    -version $version `
    -asset $asset `
    -url $assetUrl `
    -out .\docs\update.json
  if ($LASTEXITCODE -ne 0) { throw "manifest signing failed" }

  Write-Host ""
  Write-Host "Signed update manifest ready: docs\update.json" -ForegroundColor Green
  Write-Host "Release tag: $tag"
  Write-Host "Release asset: $asset"

  if ($Upload) {
    if (-not (Get-Command gh -ErrorAction SilentlyContinue)) {
      throw "GitHub CLI (gh) is required for -Upload"
    }
    gh release create $tag $asset --title "Billie Jean Pad $version" --notes "Silent client update $version"
    if ($LASTEXITCODE -ne 0) { throw "GitHub release upload failed" }
    Write-Host "Release uploaded. Commit and push docs\update.json only after the asset is available." -ForegroundColor Yellow
  } else {
    Write-Host "Upload the asset to a GitHub Release named $tag, then commit docs\update.json."
  }
} finally {
  Pop-Location
}
