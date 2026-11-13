# Installs UGT (Use the Geek Thing) on Windows, in PowerShell:
#
#   irm https://usegeekthing.com/install.ps1 | iex
#
# It puts ugt.exe in %LOCALAPPDATA%\UGT\bin, checked against the release's
# digests, adds that folder to your PATH, then sets UGT up and connects the AI
# apps it finds. Turning WSL on the first time asks for admin approval and may
# need a restart. $env:UGT_VERSION = '0.2.0' installs that release instead of
# the newest; $env:UGT_INSTALL_DIR puts ugt somewhere else.

param([switch]$Installer, [switch]$Uninstall)

# -Installer is the native setup package's child process. Its exit status is
# 0 only when ready, 3010 when Windows needs a restart, and nonzero on failure.
# The ordinary pasted command never exits the person's PowerShell session.
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
  $exe = Join-Path $dir 'ugt.exe'

  if ($Uninstall) {
    if (-not $Installer) { throw 'Uninstall is for the setup package; use ugt uninstall to review removal from a terminal.' }
    # Refuse before removing app state if an AI app still holds the program open.
    $programs = @(Get-ChildItem -LiteralPath $dir -File | Where-Object { $_.Name -eq 'ugt.exe' -or $_.Name.StartsWith('ugt.exe.old-') })
    try {
      foreach ($program in $programs) {
        $handle = [IO.File]::Open($program.FullName, 'Open', 'ReadWrite', 'None'); $handle.Close()
      }
    }
    catch { throw 'Close your AI apps and running UGT commands, then try uninstalling again.' }
    & $exe uninstall --yes
    if ($LASTEXITCODE -ne 0) { throw 'UGT could not finish removing its setup. The program is kept so you can retry.' }
    foreach ($program in $programs) { Remove-Item -LiteralPath $program.FullName }
    foreach ($target in @('User', 'Process')) {
      $current = [Environment]::GetEnvironmentVariable('Path', $target)
      $kept = @($current -split ';' | Where-Object { $_.TrimEnd('\') -ine $dir.TrimEnd('\') }) -join ';'
      [Environment]::SetEnvironmentVariable('Path', $kept, $target)
    }
    exit 0
  }

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

  # connected first, so setup's last word is that UGT is ready, not an app to connect
  & $exe connect
  if ($LASTEXITCODE -ne 0) { Write-Host "Connect an AI app later with: ugt connect" }
  if ($Installer) {
    $result = (& $exe setup --yes --json) | ConvertFrom-Json
    $setupExit = $LASTEXITCODE
    foreach ($step in $result.done) { Write-Host $step }
    foreach ($step in $result.next) { Write-Host $step }
    if ($result.needs_restart) {
      Write-Host 'Restart Windows, then open UGT Setup again. Your project stays saved in your browser.'
      exit 3010
    }
    if ($setupExit -ne 0 -or -not $result.ready) { throw 'UGT is installed, but setup needs attention. Reopen UGT Setup to retry.' }
    Write-Host 'UGT is ready.'
  } else {
    & $exe setup --yes
    if ($LASTEXITCODE -ne 0) { Write-Host 'UGT is installed; ugt setup said what is left above. If Windows restarts, run this same line again afterwards.' }
  }
  if ($env:UGT_RETURN_URL -and $env:UGT_RETURN_URL.StartsWith('https://usegeekthing.com/#project=')) {
    Write-Host "Continue with your project at $env:UGT_RETURN_URL. It stays saved in your browser, including after a Windows restart."
  } else {
    Write-Host 'Without an AI app, open a project at https://usegeekthing.com and choose Open in UGT.'
  }
}
