# Changelog

Every release of `ugt`, newest first. Each section is the release's notes on GitHub.

## 0.3.3 - 2026-11-11

### Project workflow

- Keep input, progress and checked results clearly grouped in the local project page. Runs show elapsed time and live download or unpacking activity without guessed completion percentages.
- Open delivered files and return to the same task for another run.

### Setup and recovery

- Connect Gemini CLI, find the bundled Codex command on Mac, and continue configuring other AI apps if one connection fails.
- Confirm a new connection with a real local readiness check. Plugins can guide installation before the helper is available on PATH.
- Keep the selected project through installation and restarts. An AI app can inspect recent jobs before starting a duplicate run.
- Preserve a running job when another agent connection checks it. A genuinely stopped owner leaves an interrupted record that can be inspected before retrying.

## 0.3.2 - 2026-11-11

### MCP connections

- Keep using a recipe's MCP tools after another UGT run replaces its inputs. UGT checks the provider connection before dispatching the next tool request and reconnects when its previous container has stopped. A tool request with a lost response is reported as failed and is never replayed automatically.

### Recipe information

- Recipes can supply a short title, summary, longer description and original licence-source links. These fields travel with the recipe; the catalogue uses them without a separate content file.

## 0.3.1 - 2026-11-09

### Name

- UGT is now Use the Geek Thing, at usegeekthing.com. Ask your AI app to "Use the Geek Thing", or say "ugt". The install lines, the catalogue at api.usegeekthing.com, the hosted MCP server at mcp.usegeekthing.com and the MCP Registry entry `com.usegeekthing/ugt` move with it; the old addresses keep working.
- UGT's code lives at [usegeekthing/ugt](https://github.com/usegeekthing/ugt), and the Go module is `github.com/usegeekthing/ugt`: `go install github.com/usegeekthing/ugt/cmd/ugt@latest`. Links to the old address on GitHub redirect.
- `ugt update` follows a moved repository's redirects to its newest release. ugt 0.3.0 reads only the first of them, so if its `ugt update` says UGT's releases have no newest release, run the install line once (`curl -fsSL https://usegeekthing.com/install | sh`, or `irm https://usegeekthing.com/install.ps1 | iex` on Windows); Homebrew, Scoop and winget update as usual.

### Recipes

- A recipe names the catalogue's job it does in `x-job`, from [spec/catalogue/v0/jobs.json](spec/catalogue/v0/jobs.json), such as `remove-vocals`. `ugt recipe validate --admit` refuses a job that isn't on the list, and the catalogue lists recipes by job.

## 0.3.0 - 2026-11-05

### Separate copies of an app

- Run independent named installations of a supported service, such as test and production. Each copy has its own settings, credentials, address and saved data. Existing unnamed installations keep their original data and addresses.
- Start, stop and reopen a named copy from UGT's local page or your AI app. Copies that cannot isolate their resources, expose machine-wide commands or bake settings into a shared image are refused with an explanation.

### Setup and recovery

- Finish setup from the project's local page, refresh while a job runs, replace a bad input and retry, and open the checked result folder when it finishes.
- A failed run's help link carries its project and job reference back to the website. Your AI app on the same computer can read the error; logs and file contents stay out of the link.
- Installing UGT succeeds when no supported AI app is installed. You can use the browser flow and connect an AI app later.
- Exact recipe discovery includes the recipe's stated limitations, so your AI app can check whether it fits your task before running it.

### Runtime fixes

- Your AI app can show image download progress. Interrupted downloads of UGT's Linux machine resume when the registry supports it.
- Failed recipe checks report failure and retain the reason. Repeated Mac runs read the new input and check files, and simultaneous runs cannot overwrite each other's inputs.
- Windows services stay available after the starting command exits and release the folder they were launched from. Setup failures identify the host or guest operation that failed.
- AI apps that validate tool output schemas can discover UGT's tools and start jobs. Mac credential access refuses an alternate home directory instead of silently using the account's Keychain.

### Upgrade

Run `ugt update`, or update through the package manager that installed UGT. Recipes and their images are published independently and do not require a helper upgrade.

## 0.2.2 - 2026-11-04

### UGT as a plugin

- Claude Code and Codex install UGT as a plugin from the auscaster marketplace (`claude plugin marketplace add auscaster/agent-plugins`, then `claude plugin install ugt@auscaster`), and Gemini CLI as an extension (`gemini extensions install https://github.com/usegeekthing/ugt`). Cursor's marketplace follows once it accepts UGT. In Claude Code and Codex the plugin brings UGT's skill, which gives the one line that installs UGT when it is not there yet, and installing connects UGT's MCP server to the app by `ugt`'s full path, so it starts whatever PATH the app was opened with. Gemini CLI's extension starts `ugt` from the terminal's PATH.

### What AI apps and directories show

- UGT's MCP server names itself in full, with its description, site and icon. Every tool has a title and UGT's icon, and says what it changes, so an AI app can ask first when a run replaces the last run's outputs, a verification writes over a recipe's files, or a command or a removal changes what is on your computer.
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

- UGT's hosted MCP server, https://mcp.usegeekthing.com/mcp, joins the bundles in UGT's MCP Registry entry, for AI apps with no UGT on the computer yet, such as Claude and ChatGPT on the web. It finds how UGT runs a project, shows exactly what it will run and reach, and gives the one line that installs UGT.

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

- One line installs `ugt` and runs its setup: `curl -fsSL https://usegeekthing.com/install | sh` on a Mac or Linux, `irm https://usegeekthing.com/install.ps1 | iex` on Windows.
- `ugt update` installs the newest release once its signed list of files checks out against the release key built into `ugt`.
- `ugt doctor` says what is wrong and how to fix it. `ugt uninstall` removes everything UGT set up.
