# Recipes

Recipes that got a GitHub project to do a specific task, verified by CI on clean
machines. The skill and the `ugt` helper look here first, so a project only has to
be worked out once.

**Status: draft.** The `ugt` helper reads the recipes here live, so a recipe
works for everyone the moment it is merged, with no release of `ugt`. Pull
requests that change a recipe are admitted and verified on a clean machine, one
recipe at a time. Publishing their images to GHCR is not built yet.

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
- **verified**: CI built the recipe, ran it and passed its check on every listed
  platform, and a maintainer merged it.
- **reported** (later): opt-in reports from real runs, counted separately and
  deduplicated.

## Images

Images are built only by this repository's CI and published to GHCR with
provenance attestations. Nobody uploads images directly. An image is published
only when the upstream licence permits redistribution; model weights are baked in
only when their own licence permits it, and are otherwise downloaded at first run.

## Contributing a recipe

A recipe earns a place by doing a real task, proved by a check that fails when the
task did not happen.

1. Follow the [Use the GitHub Thing skill](../skills/ugt/SKILL.md)
   to get the project working, with everything that runs in `compose.yaml`.
2. Validate it: `go run ./cmd/ugt recipe validate recipes/<owner>/<repo>/<task>`.
3. Give it two checks as services: `check`, which passes for any input, and
   `sample-check` for the known input that the `sample` service puts in place
   (from the recipe folder, a pinned public download, or generated). Never
   include private or personal data, binaries or model weights.
4. Pin the commit, base images and the versions you depended on.
5. Open a pull request. CI builds, runs and checks it on each listed platform with
   no secrets available; a maintainer merges once it passes.

Not accepted: checks that only prove a container started or a port answered;
images built outside CI; recipes that need the user's home directory, credentials
or the container engine socket.

Recipe files are Apache-2.0 like the rest of this repository. Each packaged project
keeps its own licence, recorded in its `recipe.json`. A file copied or adapted from
the upstream project (a Dockerfile or compose file, say) keeps the upstream licence:
put a `LICENSE` or `NOTICE` beside it naming the source file and its licence.
