# Pitfalls

Failures seen while packaging ten popular repositories on an 8 GB linux/arm64
Docker VM (29 October 2026). None was a dead end; each had a specific fix. Check
here before guessing.

## Silent out-of-memory

**Symptom:** exit code 137, an empty or 0-byte output file, no error message.
`docker inspect <container> --format '{{.State.OOMKilled}}'` prints `true`.

**Cause:** the project's default model or worker count needs more memory than the
limit or the machine has.

**Fix:** pick a smaller documented model or fewer workers, or raise the limit if
the machine has room. Measure: `docker stats --no-stream` while it runs.

Seen in: rembg (default `bria-rmbg` model exceeded 5 GB on a 1000 × 667 photo;
`-m u2net` peaked near 630 MB); Firecrawl (API service killed at 1.8 GB, idles at
about 2.3 GB).

## Wrong architecture from upstream defaults

**Symptom:** on Apple silicon, `failed to open elf`, `exec format error`, or
Rosetta errors during build or run.

**Cause:** an upstream Dockerfile hard-codes `ARG TARGETARCH=amd64`, downloads an
x86 binary, or the published image is amd64 only.

**Fix:** build with `--platform "linux/$UGT_ARCH"` and also override the argument
(`--build-arg TARGETARCH="$UGT_ARCH"`): an upstream `ARG TARGETARCH=amd64` default
wins even over `--platform`. Check published images with
`docker manifest inspect <image>` before reusing them. If arm64 is impossible,
amd64 under emulation is a slower fallback; record it.

Seen in: hyperframes (render Dockerfile defaulted to amd64); VoiceStudio (upstream
image is amd64 with CUDA only; an authored CPU arm64 build worked).

## Runtime version newer than expected

**Symptom:** the app refuses to start or asks for a runtime path.

**Cause:** the project needs a newer runtime than common base images.

**Fix:** read the project's own config for the minimum version (for example
`application.properties`, `.python-version`, `engines` in `package.json`) and
choose the base image from that.

Seen in: Ghidra 12.2 (needs JDK 25; JDK 21 failed with "Unable to prompt user
for JDK path").

## Undocumented commands

**Symptom:** the documented command fails with an unclear message.

**Fix:** read the project's tests, examples and source for how the function is
really called. Prefer its library API over the CLI when the CLI needs runtime
state.

Seen in: text-to-cad (`cadgen export <name>` needs a live document id; calling
`export()` from a script run by `cadgen run` worked).

## DNS failing inside the VM

**Symptom:** some public hosts fail to resolve inside containers while others
work.

**Fix:** set explicit DNS servers on the affected services (`dns: [1.1.1.1,
8.8.8.8]` in compose, `--dns` for `docker run`) and tell the user, since it may
affect their other tools.

Seen in: Firecrawl (example.com did not resolve until DNS was set).

## Model downloads at run time

**Symptom:** the first run downloads hundreds of MB to GB, or fails offline.

**Fix:** if the licence allows, download the model during the build into a known
folder, point the app at it (for example whisper's `--model_dir`), and run with
`--network none` to prove it is offline. Tell the user the size first.

Seen in: whisper (base model 139 MB baked in); rembg (176 MB u2net baked in);
VoiceStudio (about 136 MB downloaded at first use; its flagship engine is 2.4 GB).

## Very long first builds

**Symptom:** a build runs 15 to 60 minutes with no error.

**Cause:** large dependency downloads (PyTorch, Chromium, fonts, models) on a slow
connection.

**Fix:** run the build in the background, give the user an estimate and progress,
and keep the image so later runs start in seconds. Do not keep rebuilding layers
that already succeeded; order the Dockerfile so slow downloads come first and
your edits come last.

Seen in: markitdown 16 minutes, whisper 15, text-to-cad 15, Firecrawl 20, rembg 57,
VoiceStudio 40.

## Checks that pass or fail for the wrong reason

**Symptom:** a check passes on empty output, or fails on a good result.

**Fix:** run the check against a deliberately wrong input once to see it fail. Fit
thresholds to the output's nature: rembg's u2net produces soft edges, so "opaque"
needed alpha ≥ 200 rather than exactly 255.

## Features behind logins, cookies or keys

Some projects copy cookies from the user's browser profile or need provider API
keys for their main feature (Agent-Reach login channels; FreeLLMAPI completions).
Never do this without the user's explicit agreement. Prove the rest and say
exactly what they would need to provide.

## Side effects the user should know about

Tell the user when a project sends telemetry (turn it off if there is a
documented switch, as with text-to-cad's `cadgen telemetry off`), watermarks its
output (VoiceStudio adds an inaudible AudioSeal watermark), calls outside services
at run time (hyperframes compositions loaded GSAP from a CDN; FreeLLMAPI polls its
catalog feed), or runs an unauthenticated local API (Firecrawl: keep it on
`127.0.0.1`).

## Bind mounts on Docker Desktop

Writing to a file in a mounted folder right after deleting it on the host
occasionally failed with "Could not open file". Write to a fresh filename or
re-run.
