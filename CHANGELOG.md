# Changelog

Every release of `ugt`, newest first. Each section is the release's notes on GitHub.

## 0.2.3 - 2026-11-05

- Your AI app can show how much of a recipe image has downloaded while you wait. Interrupted downloads of UGT's Linux machine resume when the registry supports it.
- A job that fails its recipe check reports failure to the AI app and keeps the reason. On a Mac, running the same recipe again reads the new input and check files. Simultaneous runs of the same recipe can no longer overwrite each other's inputs.
- On Windows, a service stays available after the command that started it returns. UGT's Linux layer includes Git for recipes that build from pinned source.

## 0.2.2 - 2026-11-04

### UGT as a plugin

- Claude Code and Codex install UGT as a plugin from the auscaster marketplace (`claude plugin marketplace add auscaster/agent-plugins`, then `claude plugin install ugt@auscaster`), and Gemini CLI as an extension (`gemini extensions install https://github.com/auscaster/ugt`). Cursor's marketplace follows once it accepts UGT. In Claude Code and Codex the plugin brings UGT's skill, which gives the one line that installs UGT when it is not there yet, and installing connects UGT's MCP server to the app by `ugt`'s full path, so it starts whatever PATH the app was opened with. Gemini CLI's extension starts `ugt` from the terminal's PATH.

### What AI apps and directories show

- UGT's MCP server names itself in full, UGT: Use the GitHub Thing, with its description, site and icon. Every tool has a title and UGT's icon, and says what it changes, so an AI app can ask first when a run replaces the last run's outputs, a verification writes over a recipe's files, or a command or a removal changes what is on your computer.
- UGT for Claude Desktop shows UGT's icon, with links to its privacy policy, support and documentation. Codex shows UGT's name and icon.
- Homebrew, Scoop, winget and the MCP Registry describe UGT as getting open-source GitHub projects running on your computer, with your AI app.

### Fixes

- Claude Code and Codex keep UGT's own MCP server, which runs `ugt` by its full path, when UGT's plugin is there too, rather than being left to a plugin server that starts `ugt` from a PATH the app may not have. The plugin's skill replaces UGT's own copy, so there is one, and Codex hides its plugin's server behind UGT's own.

## 0.2.1 - 2026-11-03

### UGT's own Linux machine on Arm Linux

- Arm Linux now runs UGT's own Linux machine too, under QEMU with KVM, as Linux on a PC does. Nothing else is installed, and where this user cannot use KVM, an engine already on the computer is used.

### Install with a package manager

- Homebrew (`brew install auscaster/tap/ugt`) on a Mac or Linux, and Scoop (`scoop install ugt`, from the auscaster bucket) on Windows. winget follows once Microsoft's review accepts it.
- Each release writes its manifests from its own signed list of files, and installs and runs `ugt` from each before publishing it. `ugt update` leaves a package manager's `ugt` to that manager, and names the command that updates it.

### One UGT in each AI app

- An AI app with UGT's plugin (Claude Code, Codex) or UGT's extension (Claude Desktop) gets no second UGT from setup or `ugt connect`, and one that UGT connected before the plugin came is left with the plugin's alone. `ugt disconnect` leaves the plugin to its app and says how to remove it.
- `ugt connect` with every AI app connected already says how each one is, rather than failing, so installing again finishes cleanly.

### UGT in any AI app

- UGT's hosted MCP server, https://mcp.usegithubthing.com/mcp, joins the bundles in UGT's MCP Registry entry, for AI apps with no UGT on the computer yet, such as Claude and ChatGPT on the web. It finds how UGT runs a project, shows exactly what it will run and reach, and gives the one line that installs UGT.

### Fixes

- On Linux, an install that runs its recipes in the engine you run keeps it when UGT's own machine comes to the computer, so its containers and saved data stay in use and `ugt uninstall` still removes them. A new install runs UGT's own machine.
- `ugt update` knows a `ugt` that winget installed, for one user, for the whole computer or in a folder you chose, and names `winget upgrade auscaster.ugt`.

## 0.2.0 - 2026-11-03

### UGT's own Linux machine on Linux

- On Linux on a PC (amd64), UGT now runs its own Linux machine, the one it runs on an Apple silicon Mac and verifies every recipe in, under QEMU with KVM. It needs nothing else installed and no admin rights: QEMU comes with UGT, built from QEMU's signed release, and a desktop session can already use KVM. Where this user cannot, setup names the one command that allows it, and an engine already on the computer is used until then.
- Your folders reach the machine only while a command uses them, and no container in it can reach them, or another recipe's workspace, on its own.
- Arm Linux keeps using the engine you run, until UGT's machine is proved there.

### Fixes

- A page or endpoint a recipe provides is handed over only once it answers on your computer. Its address could arrive a moment before UGT's machine had opened it.

## 0.1.0 - 2026-11-03

The first release.

### Run a GitHub project for a job

- `ugt run OWNER/REPO [TASK]` runs a catalogue recipe on your files and puts the result in your UGT folder. It builds the recipe exactly as it was verified, or pulls it ready-built for the featured ones.
- A recipe can leave something usable after its run: a page or endpoint on 127.0.0.1, a command on your PATH (`ugt command`), or an MCP server your AI apps can call through UGT's own.
- `ugt status`, `ugt list`, `ugt stop`, `ugt remove` and `ugt delete-data` manage what you have run. `ugt settings` keeps a recipe's keys and other settings out of its files.
- A recipe from outside the catalogue that reaches more than the internet, such as a folder or a device of yours, runs only once you allow it in UGT's own prompt (or with `ugt allow`). Catalogue recipes run without asking: their review is the agreement.

### Your AI apps do the asking

- `ugt setup` connects UGT to the AI apps you have: Claude Desktop, Claude Code, Codex, Cursor and VS Code. Claude Code and Codex get the `ugt` skill. `ugt connect` and `ugt disconnect` change that later.
- `ugt mcp` is UGT's MCP server over stdio. Claude Desktop installs it from the `.mcpb` bundle on this release.
- With no recipe for a project, your agent works one out with `ugt recipe new`, proves it on a known sample with `ugt recipe verify`, then uses it on your files. `ugt recipe submit` shares it with the catalogue.

### A Linux engine with nothing else to install

- **Windows:** UGT imports its own Linux layer into WSL 2 and keeps it up to date in place. A command's folder is mounted into it for that command only.
- **Apple silicon Macs:** UGT runs its own Linux machine on Apple's Virtualization framework. It boots in about two seconds, shares only your workspaces and each command's folder, forwards the ports recipes publish to 127.0.0.1, runs amd64 images through Rosetta when it is installed, and powers off after five idle minutes.
- **Intel Macs and Linux:** UGT uses Docker Desktop, OrbStack or Docker Engine if one is running.

### Open in UGT

- An "Open in UGT" link on a web page (`ugt://run/OWNER/REPO/TASK`) opens UGT's own page on your computer. It shows what the recipe runs, what it gives you and what it can reach, then takes your files. Nothing runs until you press Run. Links name only catalogue recipes.

### Install and update

- One line installs `ugt` and runs its setup: `curl -fsSL https://usegithubthing.com/install | sh` on a Mac or Linux, `irm https://usegithubthing.com/install.ps1 | iex` on Windows.
- `ugt update` installs the newest release once its signed list of files checks out against the release key built into `ugt`.
- `ugt doctor` says what is wrong and how to fix it. `ugt uninstall` removes everything UGT set up.
