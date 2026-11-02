# Platforms

What each host changes about route choice. Only macOS on Apple silicon has been
exercised by the coverage test; Windows and Linux notes are from documentation.

## Before choosing a route, on any host

- OS and CPU architecture (`uname -sm`; on Windows `systeminfo` or
  `$env:PROCESSOR_ARCHITECTURE`).
- Free disk. Packages from the coverage test were 0.3 to 5.1 GB each.
- Memory available to containers, which is the VM's allocation, not the host's
  RAM: `docker info --format '{{.MemTotal}}'`.
- GPU, if the task benefits from one.
- Available engine: `docker info`, `podman info`, or the UGT helper when it exists.

## macOS

- Containers run in a Linux VM: on Apple silicon, UGT's own machine, which the
  helper downloads once and starts with `ugt setup`, with no install or admin
  rights, and which has half the Mac's memory, up to 8 GB; on an Intel Mac,
  Docker Desktop's, OrbStack's or Colima's. On Apple silicon the guest is
  linux/arm64; amd64 images run under Rosetta when the Mac has it installed
  (Docker Desktop can also use the much slower QEMU). Build for arm64 whenever
  the project allows.
- Containers get no Metal/MPS GPU. For AI apps where speed or quality depends on
  the GPU, prefer the project's native Mac app or installer. Small models can
  still work on CPU in a container: whisper base and VoiceStudio's KittenTTS both
  did.
- Containers cannot show GUI windows on a Mac. Use the native app for GUI work;
  headless modes (for example Ghidra's analyzer) work in containers.
- Host-to-VM file sharing is slower than native. Keep app data in volumes and
  share only inputs and outputs.
- With the helper on Apple silicon, there is no engine to install: `ugt setup`
  (`ugt_setup`) is the whole of it. Without the helper, or on an Intel Mac with
  no engine: explain that a one-time container engine install is needed, its
  size, that it needs admin approval, and ask. Docker Desktop does not require
  signing in by default; its licence terms apply at larger companies.

## Windows

- Linux containers run in WSL2. Check with `wsl --status`. Enabling WSL the first
  time needs admin approval and a restart, and hardware virtualisation must be on
  in firmware settings. Explain each step before it happens; firmware settings are
  where newcomers get stuck.
- Managed work machines may block WSL or admin rights. Explain the blocker and
  offer the user's options rather than trying workarounds.
- NVIDIA GPUs work inside WSL2 with a current Windows driver. Whether a given app
  runs still depends on its framework's GPU generation, driver and VRAM needs.
- If the project ships a Windows installer that does the task, prefer it.
- Native Windows installs of Linux-oriented Python projects often fail on long
  paths, line endings, PowerShell script policy or missing C++ build tools. Prefer
  the Linux route for those.
- When working inside WSL, keep the workspace on the Linux filesystem for speed and
  put results somewhere the user can open from Windows.
- Linux GUI apps can display on Windows 11 through WSLg.

## Linux

- Containers run on the host kernel with no VM.
- NVIDIA GPUs need the NVIDIA Container Toolkit; check before promising GPU use.
- Adding the user to the `docker` group is equivalent to giving root access. Do
  not do it without explaining that and getting agreement; rootless Podman or
  rootless Docker are alternatives.
