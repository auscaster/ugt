# Installs UGT (Use the Geek Thing) on Windows, in PowerShell:
#
#   irm https://usegeekthing.com/install.ps1 | iex
#
# It puts ugt.exe in %LOCALAPPDATA%\UGT\bin, checked against the release's
# digests, adds that folder to your PATH, then sets UGT up and connects the AI
# apps it finds. Turning WSL on the first time asks for admin approval and may
# need a restart. $env:UGT_VERSION = '0.2.0' installs that release instead of
# the newest; $env:UGT_INSTALL_DIR puts ugt somewhere else.

# its own scope, so the settings below leave the PowerShell it runs in as it was
& {
  $ErrorActionPreference = 'Stop'
  $ProgressPreference = 'SilentlyContinue' # Windows PowerShell downloads far faster without the bar
  [Net.ServicePointManager]::SecurityProtocol = [Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12

  $releases = if ($env:UGT_RELEASES) { $env:UGT_RELEASES } else { 'https://github.com/usegeekthing/ugt/releases' }
  $dir = if ($env:UGT_INSTALL_DIR) { $env:UGT_INSTALL_DIR } else { Join-Path $env:LOCALAPPDATA 'UGT\bin' }
  $arch = if ($env:PROCESSOR_ARCHITECTURE -eq 'ARM64' -or $env:PROCESSOR_ARCHITEW6432 -eq 'ARM64') { 'arm64' } else { 'amd64' }
  $base = if ($env:UGT_VERSION) { "$releases/download/v$($env:UGT_VERSION)" } else { "$releases/latest/download" }
  $name = "ugt-windows-$arch.exe"

  # downloaded beside where it goes; PowerShell's downloads carry no mark of the
  # web, so Windows does not hold the file for SmartScreen
  New-Item -ItemType Directory -Force $dir | Out-Null
  $part = Join-Path $dir ".ugt-install-$PID.exe"
  $sums = "$part.sums"
  try {
    Write-Host "Downloading $name from $base"
    Invoke-WebRequest -UseBasicParsing "$base/SHA256SUMS" -OutFile $sums
    Invoke-WebRequest -UseBasicParsing "$base/$name" -OutFile $part
    $want = Get-Content $sums | ForEach-Object { $f = $_ -split '\s+'; if ($f[1] -eq $name) { $f[0] } } | Select-Object -First 1
    if (-not $want) { throw "the release lists no $name" }
    if ((Get-FileHash -Algorithm SHA256 $part).Hash -ne $want) { throw "the download of $name does not match the release's digest: not installing it" }
    $exe = Join-Path $dir 'ugt.exe'
    # Windows lets a running ugt.exe be moved aside, not replaced
    if (Test-Path $exe) { Move-Item -Force $exe "$exe.old-$([DateTime]::UtcNow.Ticks)" }
    Move-Item $part $exe
  } finally {
    Remove-Item -Force -ErrorAction SilentlyContinue $part, $sums
  }
  Write-Host "Installed $((& $exe version) | Select-Object -First 1) in $dir"

  $path = [Environment]::GetEnvironmentVariable('Path', 'User')
  if (-not (($path -split ';') -contains $dir)) {
    [Environment]::SetEnvironmentVariable('Path', (($path, $dir) | Where-Object { $_ }) -join ';', 'User')
    $env:Path = "$env:Path;$dir"
    Write-Host "Added $dir to your PATH"
  }

  & $exe setup --yes
  if ($LASTEXITCODE -ne 0) { Write-Host 'UGT is installed; ugt setup said what is left above.' }
  & $exe connect
  if ($LASTEXITCODE -ne 0) { Write-Host "Connect an AI app later with: ugt connect" }
  Write-Host 'When setup finishes, return to your project at https://usegeekthing.com.'
}
