# Architecture

UGT turns "I found this project, make it do my task" into a verified result on the
user's own machine. The user's agent does the reasoning. UGT supplies a workflow, a
recipe format, a helper that runs recipes the same way everywhere, and a catalogue
of recipes that already worked.

Status markers: **exists** is in this repository today; **planned** is designed
but not built.

## Parts

| Part | Status | Responsibility |
| --- | --- | --- |
| Skill (`skills/ugt`) | Exists (draft) | Routes "Use the GitHub Thing" to the helper; without it, the same workflow by hand: pin the task and source, choose a route, package, prove the task, hand over |
| Recipe format (`spec/recipe/v0`) | Exists (draft 5) | `recipe.json` says what a recipe is and what it provides; `compose.yaml` holds everything that runs; conformance examples for both |
| `ugt` helper (`cmd/ugt`) | Exists | Finds and runs recipes on the user's files, hands over the pages, endpoints, commands and MCP servers they provide, authors them (scaffold, adopt existing Compose, verify with diagnosis and the check-strength test, exec, resume), sets the machine up, connects AI apps, keeps settings and secrets, cleans up only what it made, and uninstalls itself. A CLI and an MCP server over stdio |
| Linux layer (`layer/`) | Exists (Windows) | Docker Engine with Compose and Buildx on Alpine Linux, provisioned by one script: in UGT's own WSL distro on Windows. macOS uses an existing engine until UGT's VM is built (planned) |
| Catalogue (`recipes/`) | Exists | Recipes at `recipes/<owner>/<repo>/<task>/`, written and reviewed through pull requests. The catalogue service at `api.usegithubthing.com` admits each change on its own (verified on clean amd64 and arm64 machines in `auscaster/ugt-verify`, then built and verified again from `main`, which publishes its image to GHCR) and serves the catalogue to the helper, so adding a recipe never needs a release. While this repository is private, it answers only its owner and collaborators |
| Browser extension | Planned, shipped separately through browser stores | Entry point on repository pages, talking to the helper over native messaging |

## The helper

One core, ports at its edge, one adapter per technology, and one composition root
(`cmd/ugt`) that wires them. A test fails the build when a package breaks the
import rules.

```text
internal/
  domain/    the model and its rules: recipes, the catalogue, workspaces, the
             ledger, jobs, placement, diagnosis, Compose adoption. No operating
             system or network calls.
  app/       the use cases, one file each; ports.go declares every port
  adapter/   docker, host, wsl, github, catalogue, localdir, keychain, fsstate,
             agents, prompt
  surface/   cli and mcp
```

| Port | What it does | Adapters |
| --- | --- | --- |
| Runtime | Where the engine runs: status, setup, programs, files | `host` (an engine already on the machine), `wsl` (UGT's distro) |
| Engine | Compose and Buildx, through the `docker` CLI | `docker` |
| Catalogue | Recipes to find and run, asked one project at a time | `localdir` (the local catalogue of recipes made on this machine) first, then `catalogue` (the published catalogue, from the catalogue service, each file checked against its Git hash and kept by revision) |
| Disk | The user's own folders | `host` |
| GitHub | Read public repositories; open a recipe's pull request as the user | `github` |
| Secrets, Values | Settings: secrets in the system's store, values in UGT's state | `keychain`, `fsstate` |
| Consents, Prompt | What access the user agreed to, and asking them directly, never through an agent | `fsstate`; `prompt` (the desktop's own dialog), or the CLI's terminal |
| RunReports | How a run of a catalogue recipe went, for a user who agreed when one first failed (`ugt reports on\|off`): identifiers only, checked against `spec/catalogue/v0` before sending | `catalogue` |
| Jobs, Ledger | Long operations, and everything UGT created | `fsstate` |
| AgentHosts | Add MCP servers to Claude Desktop, Claude Code, Codex, Cursor and VS Code: UGT's own, and those recipes provide | `agents` |
| Launchers, FreePort | Put the commands recipes provide on the user's PATH; keep each provided address the same from run to run | `launchers`; `host` |

## Folders

| Folder | Holds |
| --- | --- |
| `~/UGT/` (Windows `%USERPROFILE%\UGT\`) | The only folder users see: input files go in, results come out at `~/UGT/<owner>/<repo>/<task>/` |
| State: macOS `~/Library/Application Support/UGT/`, Windows `%LOCALAPPDATA%\UGT\`, Linux `$XDG_DATA_HOME/ugt/` | The local catalogue (`recipes/`), the ledger, jobs, settings, verification reports and, on Windows, the WSL distro's disk |
| Workspaces, in the runtime | `<owner>/<repo>/<task>/` with `recipe/`, `inputs/` and `outputs/`: under the state folder, or `/var/lib/ugt/workspaces` in the WSL distro |

## Contracts

| Contract | Producer | Consumers |
| --- | --- | --- |
| Recipe format | Contributors, the skill, `ugt recipe` | Verification, the helper |
| The catalogue's layout, `recipes/<owner>/<repo>/<task>/` | Contributors, through admission | The catalogue service's admission |
| Catalogue API (`spec/catalogue/v0`) | The catalogue service | Helper, website, extension |
| MCP tools | The helper | Agents |
| Layer: distro `UGT`, Alpine branch, how the engine starts | `layer/` | The `wsl` adapter |
| Native messaging protocol | The helper | Browser extension |

Each contract is versioned independently. Recipe format `ugt-draft-5` is
provisional until real use settles it; each breaking change before v1 bumps the
draft number.

A recipe is a folder whose `compose.yaml` holds everything that runs and whose
`recipe.json` only describes it. Compose is the execution profile, so UGT adds no
deployment language of its own: resources, mounts, network, environment, the
checks and the known sample are all ordinary Compose services and settings.
`recipe.json`'s `provides` says what the user keeps using after a run: pages and
endpoints, which UGT publishes on `127.0.0.1` at addresses that stay the same;
commands, which it puts on the user's PATH; and MCP servers, which it adds to
their AI apps. Each names a Compose service, so nothing that runs moves out of
`compose.yaml`.
The build fetches the pinned source itself; image names and the architecture come
from the executor (`UGT_IMAGE`, `UGT_ARCH`), which also names the project and
selects the engine. The same recipe therefore runs under the skill, the helper and
the catalogue's verification, and a verified image pulled as `UGT_IMAGE` replaces the build without
changing anything else. The validator enforces the rules that keep a recipe
portable: builds and files that never read the host, data in named volumes, and
no host variables beyond declared settings. Access beyond the sandbox is allowed
and never hidden: `recipe.json` declares each kind with why, `ugt` reads what
Compose really grants from its resolved model (defaults and variables included),
and refuses any grant the recipe does not declare.

## Isolation

Project code runs in a container: inside UGT's WSL distro on Windows, inside the
engine's VM on macOS, on the host kernel on Linux. Containers get memory limits,
`no-new-privileges`, ports on `127.0.0.1`, inputs copied in and outputs copied
out. The user's home directory, credentials and the container engine socket are
never mounted into them unless the recipe declares that access and the user
agrees to it. The WSL distro mounts no Windows drives and cannot call
Windows programs. Secrets live in the system's own secret store and never pass
through an agent. Catalogue images will be built only by CI and verified against
their attestations before they run.
