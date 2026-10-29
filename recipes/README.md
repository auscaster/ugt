# Recipes

Recipes that got a GitHub project to do a specific task, verified by CI on clean
machines. The skill and the `ugt` helper look here first, so a project only has to
be worked out once.

**Status: draft.** No recipes are published yet, and the verification and
publishing workflows are not built.

## Layout

```text
recipes/<owner>/<repo>/<task>/
  recipe.json        validated against spec/recipe (ugt recipe validate)
  Dockerfile         and/or compose.yaml, scripts; the folder is the whole build context
  check/             the checks, plus a known sample or a script that fetches or makes one
```

A recipe runs unchanged wherever it lands: its build fetches the pinned source
itself, and its commands take their names and architecture from `UGT_IMAGE`,
`UGT_NAME` and `UGT_ARCH`. See the
[recipe reference](../skills/use-the-github-thing/references/recipe.md).

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

1. Follow the [Use the GitHub Thing skill](../skills/use-the-github-thing/SKILL.md)
   to get the project working and record `recipe.json`.
2. Validate it: `go run ./cmd/ugt recipe validate recipes/<owner>/<repo>/<task>/recipe.json`.
3. Give it two checks: `check`, which passes for any input, and a `sample` whose
   known input (from the recipe folder, a pinned public download, or a script
   that makes one) has a predictable output for `sample.check` to confirm. Never
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
