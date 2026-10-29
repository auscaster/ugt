# Agent instructions

This is the public UGT repository: the agent skill, the recipe and index
specifications, the `ugt` helper and their tests. Keep it free of private context:
no personal paths, account or team IDs, internal plans, or references to private
repositories.

- Go module `github.com/auscaster/ugt`. Run `gofmt -l .`, `go vet ./...` and
  `go test ./...` before finishing; all must be clean.
- Specifications in `spec/` are contracts. Every rule needs a conformance example;
  breaking changes need a new format version.
- Skills must stay self-contained and pass the skill lint.
- Recipes live in `recipes/<owner>/<repo>/<task>/`. Everything that runs is in
  the recipe's `compose.yaml`; `recipe.json` only describes it. Each passes
  `ugt recipe validate`, builds from its own folder (the build fetches the pinned
  source), and has a `check` service that fails when the task did not happen for
  any input, plus `sample` and `sample-check` services for a known input. Never
  commit binaries or model weights; images come only from CI.
- Docs describe what exists today and mark anything planned as planned.
- Use conventional lowercase commit subjects without tool attribution.
