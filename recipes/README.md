# Recipes

Recipes that got a GitHub project to do a specific task, each verified on clean
machines before it was admitted. The skill and the `ugt` helper look for one
first, so a project only has to be worked out once.

**Status: draft.** A pull request that changes a recipe is admitted on its own:
the catalogue service verifies that one recipe on clean amd64 and arm64
machines, and once it is merged, serves it to every `ugt` at once, with no
release of `ugt`.

## Layout

```text
recipes/<owner>/<repo>/<task>/
  compose.yaml       everything that runs, including the check and sample services
  recipe.json        what it is (validated with ugt recipe validate)
  Dockerfile         when the recipe builds an image; the folder is the whole build context
  check/             check scripts and sample files the services use
```

A recipe runs unchanged wherever it lands: its build fetches the pinned source
itself, and its compose file takes image names and the architecture from
`UGT_IMAGE` and `UGT_ARCH`. See the
[recipe reference](../skills/ugt/references/recipe.md).

## Trust levels

- **submitted**: a pull request is open.
- **admitted**: verified on clean amd64 and arm64 machines, every check
  rejected deliberately broken outputs, the licence and a review by a different
  model passed, and it was merged.
- **reported** (later): opt-in reports from real runs, counted separately and
  deduplicated.

## Access beyond the sandbox

A recipe may reach beyond its sandbox when its task needs it: the internet
while it runs, a port on the local network, a folder of the user's, a device,
the container engine, the user's SSH agent, capabilities, privileged mode or a
host namespace. It declares each kind in `recipe.json`'s `access`, with why;
`ugt` refuses any access Compose would grant that is not declared. Every kind
but `internet` waits for a maintainer before it is verified here, and that review
stands for the user's agreement: an admitted recipe runs without asking. A
recipe from anywhere else asks the user before it reaches their computer itself;
the internet and the folder a command is used in need no agreement.

## Images

A recipe is built on its user's machine the first time they run it, from bases
named by digest, so it builds the way it was verified. Maintainers choose some
recipes to prebuild, listed in [`prebuilt.json`](prebuilt.json), every recipe
the README features among them: once merged, those are built and verified again
from `main`, and that run pushes the images it verified to
`ghcr.io/auscaster/ugt/recipes`, where users pull them by digest. An image is
published only when the upstream licence permits redistribution; model weights
are baked in only when their own licence permits it, and are otherwise
downloaded at first run. Nothing a pull request builds leaves its verification,
and nobody uploads images. Verification and images belong to what a recipe
runs, every file but `recipe.json`: a pull request that changes only a recipe's
`recipe.json` is checked, not verified or built again.

## Contributing a recipe

A recipe earns a place by doing a real task, proved by a check that fails when the
task did not happen.

1. Follow the [Use the GitHub Thing skill](../skills/ugt/SKILL.md)
   to get the project working, with everything that runs in `compose.yaml`.
2. Give it two checks as services: `check`, which passes for any input, and
   `sample-check` for the known input that the `sample` service puts in place
   (from the recipe folder, a pinned public download, or generated). Never
   include private or personal data, binaries or model weights.
3. Pin the commit, base images and the versions you depended on, and declare
   the access it needs and nothing more.
4. `ugt recipe verify` it until it passes, then `ugt recipe submit` it, which
   checks nothing personal goes with it and opens the pull request as you
   (`ugt login` signs you in). A pull request opened by hand is admitted the
   same way, as long as it changes only its own recipe's folder.

Not accepted: checks that only prove a container started or a port answered,
and access a recipe does not need.

Recipe files are Apache-2.0 like the rest of this repository. Each packaged project
keeps its own licence, recorded in its `recipe.json`. A file copied or adapted from
the upstream project (a Dockerfile or compose file, say) keeps the upstream licence:
put a `LICENSE` or `NOTICE` beside it naming the source file and its licence.
