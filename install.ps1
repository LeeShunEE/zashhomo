# zashhomo one-line installer for Windows (PowerShell).
#   irm https://raw.githubusercontent.com/LeeShunEE/zashhomo/main/install.ps1 | iex
#
# Environment overrides:
#   $env:ZASHHOMO_REPO        owner/repo (default LeeShunEE/zashhomo)
#   $env:ZASHHOMO_BIN         install dir (default %LOCALAPPDATA%\Programs\zashhomo)
#   $env:ZASHHOMO_VERSION     release tag to install (default: latest)
#   $env:ZASHHOMO_NO_INSTALL  set to 1 to download only
$ErrorActionPreference = 'Stop'

$repo = if ($env:ZASHHOMO_REPO) { $env:ZASHHOMO_REPO } else { 'LeeShunEE/zashhomo' }
$binDir = if ($env:ZASHHOMO_BIN) { $env:ZASHHOMO_BIN } else { Join-Path $env:LOCALAPPDATA 'Programs\zashhomo' }

function Info($m) { Write-Host "* $m" -ForegroundColor Cyan }

# Detect architecture.
$arch = switch ($env:PROCESSOR_ARCHITECTURE) {
  'AMD64' { 'amd64' }
  'ARM64' { 'arm64' }
  default { throw "unsupported architecture: $($env:PROCESSOR_ARCHITECTURE)" }
}

# Resolve the release tag. Assets are named with the version embedded
# (zashhomo-<version>-windows-<arch>.exe), so we can't use the fixed
# /releases/latest/download/ path and must look up the tag first.
$version = if ($env:ZASHHOMO_VERSION) {
  $env:ZASHHOMO_VERSION
} else {
  Info "Resolving latest release..."
  (Invoke-RestMethod -Uri "https://api.github.com/repos/$repo/releases/latest" -UseBasicParsing).tag_name
}

$asset = "zashhomo-$version-windows-$arch.exe"
$url = "https://github.com/$repo/releases/download/$version/$asset"
$dest = Join-Path $binDir 'zashhomo.exe'

New-Item -ItemType Directory -Force -Path $binDir | Out-Null

Info "Downloading $asset from $repo..."
Invoke-WebRequest -Uri $url -OutFile $dest -UseBasicParsing

# Add install dir to the user PATH if missing.
$userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
if ($userPath -notlike "*$binDir*") {
  Info "Adding $binDir to your PATH"
  [Environment]::SetEnvironmentVariable('Path', "$userPath;$binDir", 'User')
  $env:Path = "$env:Path;$binDir"
}

if ($env:ZASHHOMO_NO_INSTALL -eq '1') {
  Info "Downloaded. Run: zashhomo install"
  return
}

Info "Running zashhomo install..."
# Installing a Windows service requires elevation.
$isAdmin = ([Security.Principal.WindowsPrincipal] `
  [Security.Principal.WindowsIdentity]::GetCurrent()
  ).IsInRole([Security.Principal.WindowsBuiltinRole]::Administrator)

if ($isAdmin) {
  & $dest install
} else {
  Info "Elevating to register the Windows service..."
  Start-Process -FilePath $dest -ArgumentList 'install' -Verb RunAs -Wait
}

Write-Host "`n[OK] Done. Manage with: zashhomo status | zashhomo sub add <url>" -ForegroundColor Green
