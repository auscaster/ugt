# Architecture

UGT turns "I found this project, make it do my task" into a verified result on the
user's own machine. The user's agent does the reasoning. UGT supplies a workflow, a
recipe format, a helper that runs things safely, and a registry of recipes that
already worked.

Status markers: **exists** is in this repository today; **planned** is designed
but not built.

## Parts

| Part | Status | Responsibility |
| --- | --- | --- |
| Skill (`skills/use-the-github-thing`) | Exists (draft) | Pin the task and source, choose a route, reuse or author packaging, build, run, prove the task, hand over |
| Recipe schema (`spec/recipe/v0`) | Exists (draft) | The record of what was run, for which task, on what, and how it was proved |
| `ugt` helper (`cmd/ugt`) | Partly exists | Today: recipe validation. Planned: host detection, route placement, workspace and ownership ledger, build/run/check with job IDs, lifecycle, registry client, MCP server, browser native messaging |
| Linux layer (`layer/`) | Planned | One Linux image booted through WSL2 on Windows and Apple's Virtualization framework on macOS, running Docker Engine; Linux hosts need no VM |
| Browser extension | Planned, shipped separately through browser stores | Entry point on repository pages, talking to the helper over the native messaging protocol |
| Recipe registry (`recipes/`) | Planned | Recipe records, verified by CI on clean runners and published as attested OCI images with a generated index |
| Index schema (`spec/index`) | Planned | The registry index consumed by the helper, website and extension |

## Routes

The skill picks the first route that does the task well on the user's machine:
agent install (skills, plugins, MCP servers), the project's own installer or
release, a native install (GUI apps, Apple-GPU work), a container (tools that
assume Linux), or Compose (multi-service apps). Remote compute is a last resort,
only with the user's agreement and the cost stated.

## Contracts

| Contract | Producer | Consumers |
| --- | --- | --- |
| Recipe schema | Skill, helper, contributors | Registry CI, helper, skill |
| Index schema | Registry publishing | Helper, website, extension |
| MCP tool surface | Helper | Agents, through the skill |
| Native messaging protocol | Helper | Browser extension |
| Image naming, labels and attestations | Registry CI | Helper |
| Workspace layout (`~/UGT/<id>`) | Helper, or the skill without it | Helper, skill, cleanup |

Each contract is versioned independently. Recipe format `ugt-draft-1` is
provisional until real use settles it; each breaking change before v1 bumps the
draft number.

A recipe is portable by contract: its build fetches the pinned source itself, and
its commands take image, container and project names and the architecture from
the executor (`UGT_IMAGE`, `UGT_NAME`, `UGT_ARCH`), which also selects the engine.
The same recipe therefore runs under the skill, the helper and registry CI, and a
verified image pulled under `UGT_IMAGE` replaces the build without changing any
other command. Each recipe carries two checks: one that holds for any input, and
one for a known sample whose correct output is predictable.

## Isolation

Project code runs in a container inside a Linux VM (Windows, macOS) or on the
host kernel (Linux), with memory limits, ports on `127.0.0.1`, inputs copied in and
outputs copied out. The user's home directory, credentials and the container
engine socket are never mounted into project containers. Registry images are built
only by CI and verified against their attestations before they run.
