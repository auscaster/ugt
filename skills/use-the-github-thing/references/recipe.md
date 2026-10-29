# Recipe record

Provisional format (draft 0). The recipe is the only thing UGT adds on top of the
project's own packaging: what was run, for which task, on what, and how it was
proved. Everything else stays in standard files (Dockerfile, compose.yaml, the
project's lockfiles or installer).

Save it as `recipe/recipe.json` in the project workspace. The canonical JSON Schema
is `spec/recipe/v0/recipe.schema.json` in the UGT repository; when the `ugt` helper
is installed, `ugt recipe validate recipe/recipe.json` checks a record against it.
Fields not listed here are rejected, except extensions named `x-...`.

## Fields

| Field | Meaning |
| --- | --- |
| `recipe_format` | `"ugt-draft-0"` until the format is frozen |
| `id` | `<owner>-<repo>` in lowercase |
| `repo` | Repository URL |
| `commit` | Full commit SHA the package was built from; for a release, also the tag and artifact checksum |
| `task` | The user's task as an output, in plain words |
| `route` | `agent-install`, `upstream-installer`, `native`, `container` or `compose` |
| `packaging` | `reused`, `adapted` or `authored`, with a short `packaging_note` saying why |
| `platforms_tested` | Where the check passed, e.g. `"linux/arm64 in Docker Desktop on macOS"` |
| `requirements` | `memory_gb`, `disk_gb`, `gpu` (`none`, `optional`, `required`), `network_at_run` (true/false) |
| `user_supplies` | Keys, accounts or files the user must provide; empty if none |
| `inputs`, `outputs` | Paths in the workspace and what they are |
| `build`, `run`, `check` | Exact commands, run from the workspace root |
| `stop`, `remove`, `delete_data` | Exact commands; `remove` keeps saved data, `delete_data` removes it |
| `known_issues` | Fixes applied and limits found, in plain words |
| `licence` | SPDX id, plus a note if it affects sharing or hosting |
| `verified` | `date`, `agent`, and `evidence`: the check's output summary |

Never put secrets, private input contents, personal paths or user names in a
recipe. Use `$PWD`-relative paths.

## Example

From the coverage test (openai/whisper, subtitles from a recording):

```json
{
  "recipe_format": "ugt-draft-0",
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
  "inputs": [{ "path": "inputs/talk.wav", "what": "the recording; most audio formats work" }],
  "outputs": [{ "path": "outputs/talk.srt", "what": "subtitles" }],
  "build": "docker build --platform linux/arm64 -t ugt/openai-whisper:8609812 --label ugt.recipe=openai-whisper -f recipe/Dockerfile src",
  "run": "docker run --rm --name ugt-openai-whisper-run --network none --memory 3g -v \"$PWD/inputs:/work/inputs:ro\" -v \"$PWD/outputs:/work/outputs\" ugt/openai-whisper:8609812 inputs/talk.wav --model base --model_dir /models --language en --fp16 False --output_format srt --output_dir outputs",
  "check": "sh recipe/check.sh outputs/talk.srt",
  "stop": "docker stop ugt-openai-whisper-run",
  "remove": "docker image rm ugt/openai-whisper:8609812",
  "delete_data": "rm -r outputs",
  "known_issues": [
    "The base model (139 MB) is baked into the image; pass --model_dir /models or it downloads again",
    "First build takes about 15 minutes, mostly downloading torch"
  ],
  "licence": "MIT",
  "verified": {
    "date": "2026-10-29",
    "agent": "Claude Sonnet 5.5",
    "evidence": "8 cues, 10/10 distinctive words from the script; 30 s of audio in 12 s on CPU, 865 MB peak"
  }
}
```

`recipe/Dockerfile`:

```dockerfile
FROM python:3.11-slim-bookworm
RUN apt-get update && apt-get install -y --no-install-recommends ffmpeg && rm -rf /var/lib/apt/lists/*
RUN pip install --no-cache-dir torch==2.5.1 --index-url https://download.pytorch.org/whl/cpu \
 || pip install --no-cache-dir torch==2.5.1
COPY . /src
RUN pip install --no-cache-dir /src
# bake the base model (~139 MB) into the image so runs work offline
RUN python -c "import whisper; whisper.load_model('base', download_root='/models')"
ENV XDG_CACHE_HOME=/tmp
WORKDIR /work
ENTRYPOINT ["whisper"]
CMD ["--help"]
```

`recipe/check.sh` fails unless the subtitles are well formed and contain words the
speaker actually said:

```sh
#!/bin/sh
# usage: check.sh outputs/talk.srt
python3 - "$1" <<'PY'
import re, sys
t = open(sys.argv[1], encoding="utf-8").read()
cues = re.findall(r"^(\d+)\n(\d\d:\d\d:\d\d,\d{3}) --> (\d\d:\d\d:\d\d,\d{3})\n(.+?)(?:\n\n|\Z)", t, re.S | re.M)
assert len(cues) >= 3, "too few cues"
assert [int(c[0]) for c in cues] == list(range(1, len(cues) + 1)), "bad numbering"
words = ["lighthouse", "marmalade", "cornwall", "barnaby", "cardamom",
         "driftwood", "copper", "oranges", "sunshine", "cupboard"]
hits = [w for w in words if w in t.lower()]
assert len(hits) >= 5, ("few distinctive words", hits)
print("OK cues=%d hits=%s" % (len(cues), hits))
PY
```

This check uses a known sample recording, made with macOS `say` from a script, so
the expected words are certain. The user's own recording comes second: check the
same structure, then show them the first few cues to confirm.
