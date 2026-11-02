# Changelog

Every release of `ugt`, newest first. Each section is the release's notes on GitHub.

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
