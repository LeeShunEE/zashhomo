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

$isAdmin = ([Security.Principal.WindowsPrincipal] `
  [Security.Principal.WindowsIdentity]::GetCurrent()
  ).IsInRole([Security.Principal.WindowsBuiltinRole]::Administrator)

# Inspect any existing install so we can upgrade in place. A running service
# holds an open handle on zashhomo.exe, so it must be stopped before the file
# can be replaced.
$svc = Get-Service -Name 'zashhomo' -ErrorAction SilentlyContinue
$svcRunning = $svc -and $svc.Status -eq 'Running'

if ($svcRunning) {
  Write-Host "* 检测到 zashhomo 服务正在运行。" -ForegroundColor Yellow
  $ans = Read-Host "  停止服务、更新 exe 并重启？[Y/n]"
  if ($ans -and $ans -notmatch '^[Yy]') {
    Info "已取消，未做任何更改。"
    return
  }
}

# Download to a temp file first so a running zashhomo.exe can't block the
# download, then swap it into place.
$tmp = Join-Path $binDir ("zashhomo.exe.new-" + [Guid]::NewGuid().ToString('N'))
Info "Downloading $asset from $repo..."
try {
  Invoke-WebRequest -Uri $url -OutFile $tmp -UseBasicParsing

  # Add install dir to the user PATH if missing.
  $userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
  if ($userPath -notlike "*$binDir*") {
    Info "Adding $binDir to your PATH"
    [Environment]::SetEnvironmentVariable('Path', "$userPath;$binDir", 'User')
    $env:Path = "$env:Path;$binDir"
  }

  if ($svcRunning) {
    # Upgrade a running service: stop -> replace exe -> restart. Service
    # control needs elevation, so bundle all three into a single elevated step.
    Info "Stopping service, updating exe, and restarting..."
    $swap = "Stop-Service -Name zashhomo -Force; " +
            "Move-Item -LiteralPath '$tmp' -Destination '$dest' -Force; " +
            "Start-Service -Name zashhomo"
    if ($isAdmin) {
      Invoke-Expression $swap
    } else {
      Info "Elevating to restart the Windows service..."
      Start-Process powershell -Verb RunAs -Wait -ArgumentList '-NoProfile', '-Command', $swap
    }
    Write-Host "`n[OK] Updated to $version. Manage with: zashhomo status | zashhomo sub add <url>" -ForegroundColor Green
    return
  }

  # Not running (fresh install, or service stopped): just put the exe in place.
  Move-Item -LiteralPath $tmp -Destination $dest -Force
} catch {
  if (Test-Path $tmp) { Remove-Item -LiteralPath $tmp -Force -ErrorAction SilentlyContinue }
  throw
}

if ($svc) {
  # Service already registered and pointing at the same path; the new exe is in
  # place, nothing else to do.
  Write-Host "`n[OK] Updated to $version. Manage with: zashhomo status | zashhomo sub add <url>" -ForegroundColor Green
  return
}

if ($env:ZASHHOMO_NO_INSTALL -eq '1') {
  Info "Downloaded. Run: zashhomo install"
  return
}

Info "Running zashhomo install..."
# Installing a Windows service requires elevation.
if ($isAdmin) {
  & $dest install
} else {
  Info "Elevating to register the Windows service..."
  Start-Process -FilePath $dest -ArgumentList 'install' -Verb RunAs -Wait
}

Write-Host "`n[OK] Done. Manage with: zashhomo status | zashhomo sub add <url>" -ForegroundColor Green
