# Recipe

Provisional format (draft 3). A recipe is a folder that gets one GitHub project
to do one task, and runs unchanged on any machine with a container engine. Its
rule: **`compose.yaml` holds everything that runs; `recipe.json` only says what
the recipe is.**

```text
recipe/
  recipe.json     what it is: project, commit, task, kind, settings, proof
  compose.yaml    everything that runs: build, resources, mounts, ports,
                  network, environment, the checks and the known sample
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
`kind: service` recipe keeps its stack running for the user; its `task` is
optional.

Settings and limits all belong here as plain Compose: `mem_limit`, `shm_size`,
`ulimits`, `environment`, `dns`, `network_mode: none` for offline runs, `ports`,
named `volumes` for data the user keeps, build `args`. Nothing that runs goes in
`recipe.json`.

### Rules the validator checks

- **Paths are relative to the workspace root**, which is the Compose project
  directory: build from `./recipe` and mount only `./inputs`, `./outputs` and
  `./recipe` (or folders inside them). Named volumes are fine.
- **The executor names things.** Built images are `${UGT_IMAGE}` (or
  `${UGT_IMAGE}-<suffix>` when a recipe builds several); build for
  `linux/${UGT_ARCH}` and pass `TARGETARCH: ${UGT_ARCH}` as a build arg when
  upstream sets its own default. No `container_name` and no top-level `name`.
- **No host access.** No `privileged`, host network, PID or IPC, `cap_add`,
  `devices`, Docker socket, home folder or `extra_hosts` pointing at
  `host-gateway`. Ports are published on `127.0.0.1` only; `127.0.0.1::<port>`
  lets the engine pick a free host port.
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

### How it runs

The executor (the `ugt` helper, registry CI, or you by hand) runs from the
workspace root with `UGT_IMAGE`, `UGT_ARCH` and `COMPOSE_PROJECT_NAME` set:

```sh
export UGT_IMAGE=ugt/<id>:<short-commit> UGT_ARCH=$(docker version --format '{{.Server.Arch}}')
export COMPOSE_PROJECT_NAME=ugt-<id>
c() { docker compose -f recipe/compose.yaml --project-directory . "$@"; }
c build                  # skipped when a verified image is pulled as $UGT_IMAGE
c run --rm -T sample     # verification only, in a fresh copy of the workspace
c run --rm -T task       # a kind: service recipe without a task: c up -d --wait
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
| `recipe_format` | `"ugt-draft-3"` until the format is frozen |
| `id` | `<owner>/<repo>/<task>` in lowercase: the project and the job. In the catalogue it matches the recipe's folder |
| `repo`, `commit` | Repository URL and the full commit SHA the recipe was built from; `release` adds a tag, file and SHA-256 when a release file was used |
| `task` | The user's task as an output, in plain words |
| `kind` | `job` (runs to completion, writes `outputs/`) or `service` (keeps running for the user) |
| `packaging` | `reused`, `adapted` or `authored`, with a short `packaging_note` saying why |
| `adapted_from` | When adopted from an existing Compose file: its permanent URL at a pinned commit |
| `platforms_tested` | Where the checks passed, e.g. `"linux/arm64 in Docker Desktop on macOS"` |
| `settings` | What the user must supply: `name` (the `${NAME}` in compose.yaml), `kind` (`secret` or `value`), `required`, `what`, and `how` to get it |
| `inputs`, `outputs` | Paths under `inputs/` and `outputs/`, and what they are |
| `known_issues` | Fixes applied and limits found, in plain words |
| `licence` | SPDX id, plus a note if it affects sharing or hosting |
| `verified` | `date`, `agent`, and `evidence`: the checks' output summary |

Never put secrets, private input contents, personal paths or user names in a
recipe. A secret is a setting; its value never appears in any file.

## Example

openai/whisper, subtitles from a recording.

`recipe/recipe.json`:

```json
{
  "recipe_format": "ugt-draft-3",
  "id": "openai/whisper/subtitles",
  "repo": "https://github.com/openai/whisper",
  "commit": "86098128c0b4f24f0e2aa2994de830614b474227",
  "task": "Turn a recording into an .srt subtitle file",
  "kind": "job",
  "packaging": "authored",
  "packaging_note": "No upstream Docker packaging; pip install of the pinned commit with CPU torch and ffmpeg",
  "platforms_tested": ["linux/arm64 in Docker Desktop on macOS"],
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
FROM python:3.11-slim-bookworm
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
