# Contributing

## Before a pull request

```sh
gofmt -l .        # must print nothing
go vet ./...
go test ./...     # unit tests, schema conformance and skill lint
```

CI runs the same checks.

## Changing the recipe schema

The schema in `spec/recipe/v0/` is a contract shared by the skill, the helper and
the catalogue. Add an example under `examples/` for every rule you add or
change, so the conformance tests prove the rule: `valid/`, `invalid/` for the
static rules, or `undeclared/` for access Compose would grant that a recipe does
not declare, which only the resolved model catches. Breaking changes need a new
`recipe_format` value.

## Changing a skill

Skills are installed by copying their directory, so everything a skill links to
must live inside it. The skill lint checks frontmatter, links and any recipe
examples. Keep `SKILL.md` to the workflow; put detail in `references/`.

## Recipes

Recipes for specific projects live in [`recipes/`](recipes/); its README covers the
layout, checks and review.
