# Use the GitHub Thing

Found a project on GitHub that does something you want? Give your coding agent the
link and the task. UGT helps the agent get the project working on your machine,
prove it did your task on your input, and hand you the result with a package you
can run again.

**Status: early draft.** Nothing here is released. The agent skill and the recipe
format exist; the `ugt` helper currently only validates recipes.

## What is here

| Path | What it is |
| --- | --- |
| [`skills/use-the-github-thing/`](skills/use-the-github-thing/SKILL.md) | The agent skill: a workflow for getting an unfamiliar repository to do a user's task, with recipe, pitfall and platform references |
| [`spec/recipe/v0/`](spec/recipe/v0/) | JSON Schema for recipe records (`ugt-draft-1`), with valid and invalid examples |
| [`cmd/ugt/`](cmd/ugt/) | The `ugt` helper. Today: `ugt recipe validate FILE...` |
| [`recipes/`](recipes/) | Recipes for specific projects and tasks, verified by CI (none published yet) |
| [`docs/architecture.md`](docs/architecture.md) | How the parts fit together, including what is planned |

## Try the skill

Copy `skills/use-the-github-thing/` into your agent's skills directory (for Claude
Code, `~/.claude/skills/`), then ask your agent to use a GitHub project for a task.

## Develop

Requires Go (see `go.mod`).

```sh
go test ./...                      # unit tests, schema conformance, skill lint
go run ./cmd/ugt recipe validate spec/recipe/v0/examples/valid/*.json
```

See [CONTRIBUTING.md](CONTRIBUTING.md) and [SECURITY.md](SECURITY.md).

## Licence

Apache-2.0. See [LICENSE](LICENSE) and [NOTICE](NOTICE). The UGT name and logo are
not covered by the licence.
