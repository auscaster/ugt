<div align="center">

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="assets/ugt-lockup-white.svg">
  <img alt="ugt: Use the Geek Thing" src="assets/ugt-lockup-black.svg" width="380">
</picture>

<em>You found it on GitHub. Now use it.</em>

Paste the GitHub link to the project you want to use.<br>
UGT helps get it working on your computer and shows you how to use it again.<br>
Free and open source. Windows, Mac and Linux.

[![CI](https://img.shields.io/github/actions/workflow/status/auscaster/ugt/ci.yml?branch=main&style=flat-square&label=ci&labelColor=111&color=111)](https://github.com/auscaster/ugt/actions/workflows/ci.yml)
[![Go Reference](https://img.shields.io/badge/go-reference-111?style=flat-square&logo=go&logoColor=white)](https://pkg.go.dev/github.com/auscaster/ugt)
[![Release](https://img.shields.io/github/v/release/auscaster/ugt?style=flat-square&label=release&labelColor=111&color=111)](https://github.com/auscaster/ugt/releases/latest)
[![License: Apache-2.0](https://img.shields.io/badge/license-Apache--2.0-111?style=flat-square)](LICENSE)

[How it works](#how-it-works) · [Get started](#get-started) · [What you can do](#what-you-can-do-with-it) · [Why not just ask your agent?](#why-not-just-ask-your-agent) · [The catalogue](#the-catalogue) · [Share your recipe](#share-your-recipe) · [House rules](#house-rules)

</div>

<!-- demo: a 15-second recording of a real run goes here: the phrase typed into an AI app, then the result opening -->

<br>

## How it works

1. **Start with the GitHub link.** You can also add an error you ran into or something you want to do with the project.
2. **Follow the setup for your computer.** UGT looks for a ready setup or the project's own installer. If it needs a closer look, your AI app continues with the same link and details. A recipe is a setup someone has already checked; your agent can make one when the project has none.
3. **Use what it made.** Open the resulting file or app. File jobs check their output, and apps keep their saved data when you stop and reopen them. If a run fails, ask for help with that run without starting the conversation over.
4. **Share it, if you like.** Once it works, UGT can submit your recipe to the catalogue, so the next person gets it fast.

## Get started

[Paste your GitHub project into UGT](https://usegeekthing.com/). The link is enough. If you already tried to install it, you can include the error you saw.

UGT checks for a way to use that project on your computer. If it offers **Open in UGT**, open it, choose your file if it needs one, and run it. When a file is ready, UGT shows you its folder. An app opens at its own address.

If the project needs a closer look, continue in your desktop AI app. The website carries your link and notes into the project message. If UGT needs installing, it shows how to set it up. UGT has no subscription; your AI app's own usage limits and pricing still apply.

<details>
<summary><b>Set up UGT yourself</b></summary>

- **Claude Desktop:** install the UGT desktop extension for your computer. The [project page](https://usegeekthing.com/) helps you pick the download.
- **On a Mac or Linux:** open Terminal and run `curl -fsSL https://usegeekthing.com/install | sh`.
- **On Windows:** open PowerShell and run `irm https://usegeekthing.com/install.ps1 | iex`. Windows may ask for permission and a restart.

Return to your project when setup finishes. On an Intel Mac, start Docker Desktop, OrbStack or Docker Engine first.

</details>

<details>
<summary><b>For developers</b></summary>
<br>

| Where | How | |
| --- | --- | --- |
| Windows | `scoop bucket add auscaster https://github.com/auscaster/scoop-bucket`, then `scoop install ugt` | Today |
| Windows | `winget install auscaster.ugt` | Next |
| macOS or Linux | `brew install auscaster/tap/ugt` | Today |
| Linux | `curl -fsSL https://usegeekthing.com/install \| sh` | Today |
| Any MCP client | `com.usegeekthing/ugt` in the MCP Registry, or `ugt mcp` over stdio | Today |
| Any AI app, with nothing installed | The hosted MCP server `https://mcp.usegeekthing.com/mcp`: it finds how UGT runs a project, shows what a recipe runs and reaches, and gives the line that installs UGT | Today |
| Claude Code, Codex, Gemini CLI | UGT's plugin: `claude plugin install ugt@auscaster` after `claude plugin marketplace add auscaster/agent-plugins`, the same for Codex, or `gemini extensions install https://github.com/auscaster/ugt` | Today |
| Your browser | A "Use it" button on every GitHub repository | Next |
| From source, any OS | `go install github.com/auscaster/ugt/cmd/ugt@latest`, then `ugt setup` and `ugt connect` | Today |

</details>

## What you can do with it

GitHub is the world's biggest shelf of free software, and most of it never reaches the people it could help, because it ships as code, not as an app. UGT turns any of it into something you can just ask for.

| Ask your agent | What runs |
| --- | --- |
| "Put subtitles on this interview" | [openai/whisper](https://github.com/openai/whisper): speech to text, fully offline. [Verified recipe](https://usegeekthing.com/recipes/openai/whisper/subtitles) |
| "Turn my HTML into a four-second intro video" | [heygen-com/hyperframes](https://github.com/heygen-com/hyperframes): a headless browser and ffmpeg. [Verified recipe](https://usegeekthing.com/recipes/heygen-com/hyperframes/video) |
| "Design a wall hook I can 3D print" | [earthtojake/text-to-cad](https://github.com/earthtojake/text-to-cad): an industrial CAD kernel. [Verified recipe](https://usegeekthing.com/recipes/earthtojake/text-to-cad/stl) |
| "Save these pages as clean Markdown, on my own computer" | [firecrawl/firecrawl](https://github.com/firecrawl/firecrawl): a five-service crawler stack. [Verified recipe](https://usegeekthing.com/recipes/firecrawl/firecrawl/markdown) |
| "Cut the background out of this photo" | [danielgatis/rembg](https://github.com/danielgatis/rembg): an image segmentation model |
| "Convert this PDF and Word file to Markdown" | [microsoft/markitdown](https://github.com/microsoft/markitdown): Microsoft's document converter |
| "Read this paragraph aloud" | [debpalash/VoiceStudio](https://github.com/debpalash/VoiceStudio): local text to speech |
| "List the functions in this program" | [NationalSecurityAgency/ghidra](https://github.com/NationalSecurityAgency/ghidra): the NSA's reverse-engineering suite, headless |

<sub>A few featured examples. You never need a recipe from the catalogue: without one, your agent works the project out the first time. A recipe just makes it faster, because someone already did.</sub>

## Why not just ask your agent?

Your agent can read the code. It usually can't run it for you, and it starts from zero every time.

- **Most of GitHub assumes Linux.** On Windows and on a Mac your agent hits a wall. UGT brings its own private Linux engine, set up in one step, on Linux too: no Docker Desktop, no account, nothing to learn. On an Intel Mac it drives the engine you already have.
- **Proven once, quick for everyone after.** The first agent to get a project working can spend an hour building and fixing: memory limits, wrong-architecture binaries, missing system libraries, a resolver that cannot find the internet. The recipe it leaves behind turns the next person's run into a download.
- **Proof, not "it started".** A container starting is not your job done. Every recipe carries a check that fails unless the output is real: subtitles that fit the recording, a video of the length you asked for, a mesh you can actually print.
- **Your machine, your files.** Projects run in a sandbox with only the files and access their recipe declares. The catalogue reviews that access before admitting a recipe; UGT asks before a locally made recipe reaches beyond its ordinary sandbox. Cloud compute requires your agreement and a stated cost.

## The catalogue

**Recipes are a shortcut, never a requirement.** Each one is a project someone already got doing a job, shared so nobody has to work it out again. A recipe is a folder anyone can read, in [`recipes/`](recipes): a `compose.yaml` that holds everything that runs, and a `recipe.json` that says what it does. One project can have many recipes, one per job.

Browse them at [usegeekthing.com/recipes](https://usegeekthing.com/recipes), or read their source in [`recipes/`](recipes). Every one was verified on clean machines before it was admitted, and a new recipe works for everyone the moment it lands, with no update to UGT.

<sub>Verified means every service ran as written on clean amd64 and arm64 machines and passed both checks, and both checks rejected deliberately broken outputs. A featured recipe's image is the one that was verified; any other builds on your machine from the same pinned commit and bases.</sub>

<details>
<summary><b>What's inside a recipe</b></summary>
<br>

```text
recipes/openai/whisper/subtitles/
  compose.yaml    everything that runs: build, limits, mounts, network, and the four services
  recipe.json     what it is: project, commit, task, settings, the access it needs, licence
  Dockerfile      fetches the pinned source itself, so the build needs no clone
  check/          the check scripts
```

| Service | Does |
| --- | --- |
| `task` | Your job, on your files |
| `check` | Fails unless the job really happened, whatever your input was |
| `sample` | Puts a known input in place |
| `sample-check` | Fails unless the known input gave the known answer |

It is plain [Compose](https://docs.docker.com/compose/), so there is no new language to learn, and every recipe runs anywhere Compose does. The [recipe reference](skills/ugt/references/recipe.md) has the rules.

</details>

## Share your recipe

Got a project doing something useful? Share it, so the next person gets it fast instead of working it out again.

Tell your AI **"Use the Geek Thing: share this recipe"**, or run `ugt recipe submit`. UGT checks it runs again from scratch, makes sure nothing personal goes with it (your files, paths and keys), and opens a pull request as you. It is then verified on clean machines with no secrets available, and admitted once it passes; a maintainer reviews any recipe that reaches beyond its sandbox first.

<sub><b>Coming with the UGT app.</b> Until then, from the source:</sub>

```bash
go install github.com/auscaster/ugt/cmd/ugt@latest
ugt recipe new <owner>/<repo> <task>     # start one, or adopt the project's own Compose file
ugt recipe verify <folder>               # until it passes
ugt login                                # once: approve a code in your browser
ugt recipe submit <folder>               # opens the pull request as you
```

`ugt recipe submit --dry-run` shows exactly what would be sent first. [CONTRIBUTING.md](CONTRIBUTING.md) and the [catalogue rules](recipes/README.md) have the details.

**Maintain a project?** A "Run with UGT" badge is on the way: put it in your README, and one click opens your project in UGT.

## House rules

- **Your files stay yours.** Inputs are copied in. Your home folder, keys and the container engine are never handed to a project unless its recipe says why and a maintainer reviewed it, or you agree.
- **Run anything, hide nothing.** A recipe can need the internet, a folder, a device or more. It declares each one, and UGT refuses anything it did not declare. A maintainer reviews it before the catalogue admits it; a recipe from anywhere else asks you before it reaches your computer.
- **Proof or it didn't happen.** No check, no success.
- **You decide.** Installs, admin prompts, big downloads, accounts, keys and cloud compute are always your call, asked plainly before they happen.
- **Secrets never pass through your agent.** UGT asks you for them itself, once, and they live in your system keychain.
- **Credit travels with the code.** Every recipe records its project's licence, and files adapted from upstream keep theirs.
- **Your files never leave your computer.** UGT asks its catalogue which recipes a project has, downloads what a recipe needs, and sends run reports only if you agree: which recipe ran and how it went, never your files, paths or output. The [privacy policy](https://usegeekthing.com/privacy) has the rest.

## License

[Apache-2.0](LICENSE). Each packaged project keeps its own licence. The UGT name and logo are not licensed; see [NOTICE](NOTICE). UGT is independent and not affiliated with GitHub. Security issues go to [SECURITY.md](SECURITY.md).

<br>

<div align="center">
<picture>
  <source media="(prefers-color-scheme: dark)" srcset="assets/ugt-wordmark-white.svg">
  <img alt="" src="assets/ugt-wordmark-black.svg" width="56">
</picture>
<br>
<sub>Made by <a href="https://auscaster.com">auscaster</a></sub>
</div>
