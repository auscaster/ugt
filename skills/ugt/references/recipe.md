# Recipe

Provisional format (draft 5). A recipe is a folder that gets one GitHub project
working for the user, and runs unchanged on any machine with a container engine.
Its rule: **`compose.yaml` holds everything that runs; `recipe.json` only says
what the recipe is and what it provides.**

```text
recipe/
  recipe.json     what it is: project, commit, task, kind, what it provides,
                  settings, access
  compose.yaml    everything that runs: build, resources, mounts, network,
                  environment, the checks and the known sample
  Dockerfile      when the recipe builds an image
  check/          check scripts and sample files the services use
```

With the helper installed, start a recipe with
`ugt recipe new <owner>/<repo> <task>`. It adopts the project's own Compose file
when there is one (or another with `--from`), rewriting it to the rules below and
listing what it could not fix; otherwise it writes a blank recipe whose
Dockerfile builds the pinned commit. Its services say what to write and fail
until written. Then `ugt recipe verify <folder>` runs the recipe the way every
machine does and reports the failing step with a diagnosis. Validate alone with
`ugt recipe validate <folder>`. The canonical schema for `recipe.json` is
`spec/recipe/v0/recipe.schema.json` in the UGT repository; the Compose rules
below are checked alongside it.

## compose.yaml

Four services, by name:

| Service | Profile | Does |
| --- | --- | --- |
| `task` | none | the user's task: reads `/work/inputs`, writes `/work/outputs` |
| `check` | `check` | fails unless the task happened, for **any** input: every output exists, with the right structure and plausible values |
| `sample` | `sample` | puts a known input into `inputs/` (from `check/`, a pinned public download, or generated) |
| `sample-check` | `sample` | fails unless the sample's output has what that input predicts: specific words, dimensions, values |

Other services (a database, a browser, an API) are dependencies of `task`. A
`kind: service` recipe keeps its stack running for the user and has no `task`:
`up` starts it, and `check` proves it works. Commands and MCP servers a recipe
provides have services of their own (see [What it provides](#what-it-provides)).

Settings and limits all belong here as plain Compose: `mem_limit`, `shm_size`,
`ulimits`, `environment`, `dns`, `network_mode: none` for offline runs, named
`volumes` for data the user keeps, build `args`. Nothing that runs goes in
`recipe.json`.

### Rules the validator checks

- **Paths are relative to the workspace root**, which is the Compose project
  directory: build from `./recipe` and mount only `./inputs`, `./outputs` and
  `./recipe` (or folders inside them). Named volumes are fine.
- **The executor names things.** Built images are `${UGT_IMAGE}` (or
  `${UGT_IMAGE}-<suffix>` when a recipe builds several); build for
  `linux/${UGT_ARCH}` and pass `TARGETARCH: ${UGT_ARCH}` as a build arg when
  upstream sets its own default. No `container_name` and no top-level `name`.
- **Access beyond the sandbox is declared, never hidden.** A recipe may need
  the internet while it runs, a port on the local network, a folder of the
  user's, a device, the engine's socket, the user's SSH agent, capabilities,
  privileged mode or a host namespace. Each kind it needs goes in `recipe.json`'s
  `access` with why, and `ugt` reads what Compose really grants (defaults and
  variables included) and refuses any grant that is not declared. The user
  agrees to the declared access before the first run. Give a recipe only what
  its task needs: `network_mode: none` for an offline task, the ports the
  user opens or calls in `provides` rather than `ports`, and inputs copied in
  rather than a folder mounted. Data a recipe keeps goes in a
  named volume, never another folder of the workspace.
- **Builds read nothing from the host.** A build context is `./recipe` (or a
  folder inside it) or a Git context pinned to a commit; its Dockerfile stays
  inside the context. Files the services read (`env_file`, `secrets`,
  `configs`) live in `./recipe`. No `include` or `extends`: everything that runs
  is in this one file.
- **No host variables.** `${UGT_IMAGE}` and `${UGT_ARCH}` come from the
  executor; any other `${NAME}` must be declared in `settings`. Write `$$` for a
  literal dollar sign in a command.
- **The build fetches its own source**: `ADD https://github.com/<owner>/<repo>.git#<commit> /src`
  in the Dockerfile, a Git build context
  (`https://github.com/<owner>/<repo>.git#<commit>:<subdirectory>`), or a release
  file fetched with `ADD --checksum=sha256:...`. A clone in `src/` is for reading
  only.
- **Checks run in containers**, using the recipe's image or a small public one,
  so the host needs nothing but the engine.
- **Bases are official images, named by digest.** Start from one of these
  where it fits: recipes that share a base share its layers on the user's
  machine, so one download serves them all. A Dockerfile in `./recipe` names
  every base by digest (`FROM python:3.12-slim-bookworm@sha256:...`), so the
  build its users run is the one that was verified; `ugt recipe new` pins its
  draft's for you. A project that needs another base, such as a CUDA image,
  names that one by digest too.

<!-- bases:start -->
| Base | For |
| --- | --- |
| `python:3.12-slim-bookworm` | Python projects |
| `node:22-bookworm-slim` | JavaScript and TypeScript projects |
| `golang:1.25-bookworm` | building Go projects, finished on Debian |
| `rust:1-bookworm` | building Rust projects, finished on Debian |
| `debian:bookworm-slim` | anything else, and a build's final stage |
<!-- bases:end -->

### How it runs

The executor (the `ugt` helper, the catalogue's verification, or you by hand) runs from the
workspace root with `UGT_IMAGE`, `UGT_ARCH` and `COMPOSE_PROJECT_NAME` set.
`ugt recipe verify` does all of this for you; by hand it is:

```sh
export UGT_IMAGE=ugt/<owner>/<repo>/<task>:local UGT_ARCH=$(docker version --format '{{.Server.Arch}}')
export COMPOSE_PROJECT_NAME=ugt-<owner>-<repo>-<task>
c() { docker compose -f recipe/compose.yaml --project-directory . "$@"; }
c build                  # skipped when a verified image is pulled as $UGT_IMAGE
c run --rm -T sample     # verification only, in a fresh copy of the workspace
c run --rm -T task       # a kind: service recipe: c up -d --wait
c run --rm -T check
c run --rm -T sample-check
c down                   # stops and removes containers; images and named volumes stay
```

Prove the sample in a fresh copy of the workspace, or before the user's input is
in `inputs/`: the sample writes there. For the user's task, copy their input to
the paths in `inputs`, then run `task` and `check`.

## recipe.json

| Field | Meaning |
| --- | --- |
| `recipe_format` | `"ugt-draft-5"` until the format is frozen |
| `id` | `<owner>/<repo>/<task>` in lowercase: the project and the job. In the catalogue it matches the recipe's folder |
| `repo`, `commit` | Repository URL and the full commit SHA the recipe was built from; `release` adds a tag, file and SHA-256 when a release file was used |
| `task` | The user's task as an output, in plain words |
| `kind` | `job` (runs to completion on the user's inputs) or `service` (keeps running for the user) |
| `provides` | What the user keeps using after the run: `pages`, `endpoints`, `commands`, `mcp_servers` ([below](#what-it-provides)) |
| `packaging` | `reused`, `adapted` or `authored`, with a short `packaging_note` saying why |
| `adapted_from` | When adopted from an existing Compose file: its permanent URL at a pinned commit |
| `settings` | What the user must supply: `name` (the `${NAME}` in compose.yaml), `kind` (`secret` or `value`), `required`, `what`, and `how` to get it |
| `access` | Each kind of access beyond the sandbox, with `why` in words the user reads before agreeing: `internet`, `lan`, `host-services`, `host-folder`, `engine`, `ssh-agent`, `device`, `capabilities`, `privileged`, `host-namespace`. Only `internet` skips a maintainer's review in the catalogue |
| `inputs`, `outputs` | Paths under `inputs/` and `outputs/`, and what they are. Only a job has `outputs`: the files a run hands back |
| `known_issues` | Fixes applied and limits found, in plain words |
| `licence` | SPDX id, plus a note if it affects sharing or hosting |

Evidence is not part of a recipe: `ugt` keeps this machine's verification
reports, and the catalogue records where each admitted recipe passed. Both
belong to what the recipe runs, every file but `recipe.json`, so changing only
`recipe.json` (a clearer `what`, a known issue, a page it provides) keeps the
verification and the images, and is never verified or built again.

Never put secrets, private input contents, personal paths or user names in a
recipe. A secret is a setting; its value never appears in any file.

## What it provides

A job hands back files: its `outputs`. Anything the user keeps using after a
run goes in `provides`, so `ugt run` hands it over ready and nobody has to work
out a port or a command. Ask, for the project:

| Question | Answer in `recipe.json` |
| --- | --- |
| Does it keep running for the user, whether or not they are using it? | `kind`: `service`. Otherwise `job`, which includes a recipe whose commands and MCP servers start only while they are used |
| What files does a run hand back? | `outputs` |
| What does a person open in a browser? | `provides.pages` |
| What does other software call, such as an API or a database? | `provides.endpoints` |
| What does the user type in a terminal? | `provides.commands` |
| What does their AI app use, over MCP? | `provides.mcp_servers` |

| Key | Each item | Rules |
| --- | --- | --- |
| `pages` | `service`, `port`, optional `path`, `what` | A service only. `port` is the one the service listens on inside its container |
| `endpoints` | as a page, plus `protocol` when it is not HTTP (`postgresql`, `redis`) | A service only |
| `commands` | `name` the user types, `service`, `what` | The service has `profiles: [command]`; its entrypoint is the program, and the user's arguments follow it |
| `mcp_servers` | `service`, `what` | The service has `profiles: [mcp]`; its entrypoint is an MCP server speaking over stdin and stdout |

- **Do not publish a port you provide.** UGT publishes it on `127.0.0.1` at an
  address that stays the same from run to run, so the validator refuses it in
  `ports`. One port can be both a page and an endpoint.
- **A command runs in the folder the user runs it in**, mounted at its own
  path, so relative paths work as they would natively. In a service recipe it
  reaches the stack by service name.
- **`what` is what the user reads when it is ready**: "the dashboard, in your
  browser", "an OpenAI-compatible API for your models".
- **One recipe per running copy.** Uses that share one running copy and its
  data, such as an API and the command that talks to it, belong in one
  recipe. A different job on files is a recipe of its own.
- **A job that only provides commands or MCP servers** still has all four
  services: its `task` is a representative use on the sample, which `ugt run`
  runs to prove it before setting them up.
- **A service that provides nothing** is a worker that keeps running with
  nothing to open, such as a chat bot or a sync job: `kind: service` alone says
  so, and `ugt stop` stops it. When it needs the user's token or account to do
  its work, that is a secret setting, and its check proves everything up to it.

A service with an API and a command, abridged:

```json
{
  "kind": "service",
  "provides": {
    "endpoints": [{ "service": "ollama", "port": 11434, "path": "/v1", "what": "an OpenAI-compatible API for the models on this machine" }],
    "commands": [{ "name": "ollama", "service": "cli", "what": "chat with a model in your terminal" }]
  }
}
```

```yaml
services:
  ollama:
    image: ollama/ollama@sha256:...
    volumes: [models:/root/.ollama]
  cli:
    image: ollama/ollama@sha256:...
    profiles: [command]
    environment: { OLLAMA_HOST: "http://ollama:11434" }
```

## Example

openai/whisper, subtitles from a recording.

`recipe/recipe.json`:

```json
{
  "recipe_format": "ugt-draft-5",
  "id": "openai/whisper/subtitles",
  "repo": "https://github.com/openai/whisper",
  "commit": "86098128c0b4f24f0e2aa2994de830614b474227",
  "task": "Turn a recording into an .srt subtitle file",
  "kind": "job",
  "packaging": "authored",
  "packaging_note": "No upstream Docker packaging; pip install of the pinned commit with CPU torch and ffmpeg",
  "access": [{ "kind": "internet", "why": "Only the sample: it downloads whisper's own test recording at the pinned commit. Your recording never leaves the machine." }],
  "inputs": [{ "path": "inputs/talk.wav", "what": "the recording; any format ffmpeg reads, whatever its extension" }],
  "outputs": [{ "path": "outputs/talk.srt", "what": "subtitles" }],
  "known_issues": [
    "The base model (139 MB) is baked into the image; pass --model_dir /models or it downloads again",
    "First build takes about 15 minutes, mostly downloading torch"
  ],
  "licence": "MIT"
}
```

`recipe/compose.yaml`:

```yaml
services:
  task:
    image: ${UGT_IMAGE}
    build:
      context: ./recipe
    platform: linux/${UGT_ARCH}
    network_mode: none
    mem_limit: 3g
    volumes:
      - ./inputs:/work/inputs:ro
      - ./outputs:/work/outputs
    command: [inputs/talk.wav, --model, base, --model_dir, /models, --fp16, "False", --output_format, srt, --output_dir, outputs]

  check:
    image: ${UGT_IMAGE}
    profiles: [check]
    network_mode: none
    volumes:
      - ./inputs:/work/inputs:ro
      - ./outputs:/work/outputs:ro
      - ./recipe/check:/work/check:ro
    entrypoint: [python, check/srt.py, inputs/talk.wav, outputs/talk.srt]

  sample:
    image: ${UGT_IMAGE}
    profiles: [sample]
    volumes:
      - ./inputs:/work/inputs
    entrypoint: [ffmpeg, -v, error, -y, -i, "https://raw.githubusercontent.com/openai/whisper/86098128c0b4f24f0e2aa2994de830614b474227/tests/jfk.flac", inputs/talk.wav]

  sample-check:
    image: ${UGT_IMAGE}
    profiles: [sample]
    network_mode: none
    volumes:
      - ./outputs:/work/outputs:ro
    entrypoint: [sh, -c, "grep -qi 'fellow americans' outputs/talk.srt && grep -qi 'ask not what your country' outputs/talk.srt"]
```

`recipe/Dockerfile` fetches the pinned source itself, so the build needs no clone:

```dockerfile
FROM python:3.11-slim-bookworm@sha256:94a8fee35252d06ab75856f15f12c7bae67b10e8a6da93c7fded82b2610926cd
RUN apt-get update && apt-get install -y --no-install-recommends ffmpeg && rm -rf /var/lib/apt/lists/*
RUN pip install --no-cache-dir torch==2.5.1 --index-url https://download.pytorch.org/whl/cpu \
 || pip install --no-cache-dir torch==2.5.1
# the pinned source, fetched by the build itself: no clone on the host
ADD https://github.com/openai/whisper.git#86098128c0b4f24f0e2aa2994de830614b474227 /src
RUN pip install --no-cache-dir /src
# bake the base model (~139 MB) into the image so runs work offline
RUN python -c "import whisper; whisper.load_model('base', download_root='/models')"
ENV XDG_CACHE_HOME=/tmp
WORKDIR /work
ENTRYPOINT ["whisper"]
CMD ["--help"]
```

`recipe/check/srt.py` runs in the image and works for any recording: the
subtitles must be well formed and fit the recording.

```python
"""usage: srt.py RECORDING SUBTITLES

Fails unless the subtitles are well formed and fit the recording: cues numbered
from 1, ordered timestamps that end within the recording, and text in every cue.
Works for any recording; the known sample's words are checked separately."""
import re
import subprocess
import sys

audio, srt = sys.argv[1], sys.argv[2]
duration = float(subprocess.run(
    ["ffprobe", "-v", "error", "-show_entries", "format=duration", "-of", "csv=p=0", audio],
    capture_output=True, text=True, check=True).stdout)
cues = re.findall(r"^(\d+)\n(\S+) --> (\S+)\n(.+?)(?:\n\n|\Z)",
                  open(srt, encoding="utf-8").read(), re.S | re.M)


def seconds(stamp):
    h, m, s = stamp.replace(",", ".").split(":")
    return int(h) * 3600 + int(m) * 60 + float(s)


assert cues, "no cues"
assert [int(c[0]) for c in cues] == list(range(1, len(cues) + 1)), "cues not numbered from 1"
end = 0.0
for number, start, stop, text in cues:
    assert end <= seconds(start) <= seconds(stop), f"cue {number} out of order"
    assert text.strip(), f"cue {number} has no text"
    end = seconds(stop)
assert end <= duration + 1, f"cues run to {end:.1f} s, past the {duration:.1f} s recording"
print(f"OK cues={len(cues)} last={end:.1f}s recording={duration:.1f}s")
```

The known sample is the recording in whisper's own tests, downloaded from the
pinned commit, so the expected words are certain and no audio file is committed.
For the user's recording, `check` proves the structure; then show them the first
few cues to confirm.
