# Windows setup package

This package gives the existing Windows installer a native window. It installs
for the current user, downloads the helper for the computer's architecture and
checks its release digest. `install/install.ps1` owns installation and PATH;
the helper owns connections, WSL, setup, restart indication and removal. The
package owns its Start-menu retry shortcut and Windows removal entry only.

Build with NSIS 3 from the repository root, choosing an existing helper release:

```sh
makensis -DVERSION=0.3.3 -DSOURCE_ROOT="$PWD" -DOUTPUT=/absolute/path/ugt-setup-windows.exe install/windows/setup.nsi
```

The package asks before starting. If setup needs a restart, it keeps a
**Finish UGT Setup** shortcut and does not report readiness. Reopening it calls
the same setup operation again. It never restarts Windows automatically. Removal
explains the saved settings, secrets and environment it removes, uses the
helper's uninstall operation and keeps the person's results and authored recipes.
Close AI apps before removal; a locked program is refused before removing state.

`.github/workflows/installer.yml` runs the native lifecycle on a disposable public
Windows runner, using a locally served fake helper. It checks ready, restart
indication and retry, setup failure, digest refusal, result preservation and
removal. This is packaging qualification; it does not prove a physical Windows
restart or a clean-machine WSL installation. Never run it against a personal
account or credential store. A version tag builds the same source and includes
the installer in the release's checksums. Existing Authenticode secrets sign it
when configured; otherwise it is unsigned. No package or new release is published
by qualification.
