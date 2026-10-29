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
- Recipes live in `recipes/<owner>/<repo>/<task>/`. Each validates against the
  schema, builds from its own folder (the build fetches the pinned source), has a
  `check` that fails when the task did not happen for any input, and a `sample`
  whose known output `sample.check` predicts. Never commit binaries or model
  weights; images come only from CI.
- Docs describe what exists today and mark anything planned as planned.
- Use conventional lowercase commit subjects without tool attribution.
