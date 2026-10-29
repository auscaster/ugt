# Recipe record

Provisional format (draft 1). The recipe is the only thing UGT adds on top of the
project's own packaging: what was run, for which task, on what, and how it was
proved. Everything else stays in standard files (Dockerfile, compose.yaml, the
project's lockfiles or installer).

Save it as `recipe/recipe.json` in the project workspace. The canonical JSON Schema
is `spec/recipe/v0/recipe.schema.json` in the UGT repository; when the `ugt` helper
is installed, `ugt recipe validate recipe/recipe.json` checks a record against it.
Fields not listed here are rejected, except extensions named `x-...`.

## A recipe runs anywhere

A recipe is written once and run by whoever has it next: the user again, another
user's agent, or registry CI on a clean machine. So it never depends on the
machine it was written on:

- **The build fetches its own source.** The build context is `recipe/` and
  nothing else. Fetch the pinned source inside the build, for example
  `ADD https://github.com/<owner>/<repo>.git#<full-commit> /src` in the Dockerfile,
  or build straight from upstream with a Git context
  (`https://github.com/<owner>/<repo>.git#<commit>:<subdirectory>`). A clone in
  `src/` is for reading only.
- **Names come from the executor.** Every command runs with POSIX `sh` from the
  workspace root, with these set:

  | Variable | Meaning |
  | --- | --- |
  | `UGT_IMAGE` | The image to build and run. Compose recipes tag their services `$UGT_IMAGE-<service>`. When a verified image exists, it is pulled under this name and `build` is skipped. |
  | `UGT_NAME` | The prefix for container, volume and network names. It is also exported as `COMPOSE_PROJECT_NAME`. |
  | `UGT_ARCH` | `amd64` or `arm64`. Build with `--platform "linux/$UGT_ARCH"`, and pass `--build-arg TARGETARCH="$UGT_ARCH"` when upstream sets its own default. Name a fixed architecture only for a recipe that works on one. |

  The engine is chosen with `DOCKER_CONTEXT` and `BUILDX_BUILDER`, never inside
  a command: no `--context`, `--builder` or `--project-name`. Container names
  start with `$UGT_NAME`. The validator rejects commands that break these rules.
- **Two checks.** `check` passes for any input: every output exists and has the
  right structure and plausible values. `sample` holds a known input and a
  second check for what that input predicts: specific words, dimensions, values.
  The known sample proves the recipe; the user's own input is then checked by
  `check` and shown to them.
- **Only the engine and POSIX tools on the host.** Run checks that need Python,
  ffprobe or a parser inside the image, so the recipe works on a host that has
  none of them.

Without the helper, set the names yourself before running a recipe's commands:

```sh
export UGT_IMAGE=ugt/<id>:<short-commit> UGT_NAME=ugt-<id> COMPOSE_PROJECT_NAME=ugt-<id>
export UGT_ARCH=$(docker version --format '{{.Server.Arch}}')
```

To verify a recipe, run `build`, `sample.setup`, `run`, `check` and `sample.check`
in that order in a fresh workspace. `sample.setup` writes to `inputs/`, so prove
the sample before the user's input is placed, or in a separate copy of the
workspace; never over the user's files. For the user's task, copy their input to
the paths in `inputs`, then run `build` (or pull), `run` and `check`.

## Fields

| Field | Meaning |
| --- | --- |
| `recipe_format` | `"ugt-draft-1"` until the format is frozen |
| `id` | `<owner>-<repo>` in lowercase |
| `repo` | Repository URL |
| `commit` | Full commit SHA the package was built from; for a release, also the tag and artifact checksum |
| `task` | The user's task as an output, in plain words |
| `route` | `agent-install`, `upstream-installer`, `native`, `container` or `compose` |
| `packaging` | `reused`, `adapted` or `authored`, with a short `packaging_note` saying why |
| `platforms_tested` | Where the checks passed, e.g. `"linux/arm64 in Docker Desktop on macOS"` |
| `requirements` | `memory_gb`, `disk_gb`, `gpu` (`none`, `optional`, `required`), `network_at_run` (true/false) |
| `user_supplies` | Keys, accounts or files the user must provide; empty if none |
| `inputs`, `outputs` | Paths in the workspace and what they are |
| `build` | Builds `$UGT_IMAGE` from `recipe/`; omitted when the recipe runs a published image |
| `run` | Does the task on `inputs/` and writes `outputs/` |
| `check` | Fails unless the task happened, for any input |
| `sample` | `setup` puts a known input in `inputs/`; `check` fails unless the output has what that input predicts. Required except for agent installs |
| `stop`, `remove`, `delete_data` | Exact commands; `remove` keeps saved data, `delete_data` removes it |
| `known_issues` | Fixes applied and limits found, in plain words |
| `licence` | SPDX id, plus a note if it affects sharing or hosting |
| `verified` | `date`, `agent`, and `evidence`: the checks' output summary |

Never put secrets, private input contents, personal paths or user names in a
recipe. Use `$PWD`-relative paths.

## Example

openai/whisper, subtitles from a recording, verified from a fresh workspace:

```json
{
  "recipe_format": "ugt-draft-1",
  "id": "openai-whisper",
  "repo": "https://github.com/openai/whisper",
  "commit": "86098128c0b4f24f0e2aa2994de830614b474227",
  "task": "Turn a recording into an .srt subtitle file",
  "route": "container",
  "packaging": "authored",
  "packaging_note": "No upstream Docker packaging; pip install of the pinned commit with CPU torch and ffmpeg",
  "platforms_tested": ["linux/arm64 in Docker Desktop on macOS"],
  "requirements": { "memory_gb": 3, "disk_gb": 2.5, "gpu": "optional", "network_at_run": false },
  "user_supplies": ["the recording"],
  "inputs": [{ "path": "inputs/talk.wav", "what": "the recording; any format ffmpeg reads, whatever its extension" }],
  "outputs": [{ "path": "outputs/talk.srt", "what": "subtitles" }],
  "build": "docker buildx build --load --platform \"linux/$UGT_ARCH\" -t \"$UGT_IMAGE\" --label ugt.recipe=openai-whisper recipe",
  "run": "docker run --rm --name \"$UGT_NAME-run\" --network none --memory 3g -v \"$PWD/inputs:/work/inputs:ro\" -v \"$PWD/outputs:/work/outputs\" \"$UGT_IMAGE\" inputs/talk.wav --model base --model_dir /models --fp16 False --output_format srt --output_dir outputs",
  "check": "docker run --rm --network none -v \"$PWD:/work:ro\" --entrypoint python \"$UGT_IMAGE\" recipe/check/srt.py inputs/talk.wav outputs/talk.srt",
  "sample": {
    "setup": "docker run --rm -v \"$PWD/inputs:/work/inputs\" --entrypoint ffmpeg \"$UGT_IMAGE\" -v error -y -i https://raw.githubusercontent.com/openai/whisper/86098128c0b4f24f0e2aa2994de830614b474227/tests/jfk.flac inputs/talk.wav",
    "check": "grep -qi 'fellow americans' outputs/talk.srt && grep -qi 'ask not what your country' outputs/talk.srt"
  },
  "stop": "docker stop \"$UGT_NAME-run\"",
  "remove": "docker image rm \"$UGT_IMAGE\"",
  "delete_data": "rm -r outputs",
  "known_issues": [
    "The base model (139 MB) is baked into the image; pass --model_dir /models or it downloads again",
    "First build takes about 15 minutes, mostly downloading torch"
  ],
  "licence": "MIT",
  "verified": {
    "date": "2026-10-29",
    "agent": "Claude Opus 5.5",
    "evidence": "Sample (whisper's tests/jfk.flac, 11 s): 2 cues ending at 11.0 s with both expected phrases; the structural check also passed on an unrelated 15.5 s recording"
  }
}
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

`recipe/check/srt.py` runs inside the image and works for any recording: the
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
