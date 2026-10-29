# Contributing

## Setup

You need Go (the version in `go.mod` or newer) and
[pre-commit](https://pre-commit.com) (`uv tool install pre-commit` or
`pipx install pre-commit`).

```sh
go build ./...
go test ./...
pre-commit install              # installs the pre-commit and commit-msg hooks
pre-commit run --all-files      # what CI's lint job runs
```

The hooks run golangci-lint (`.golangci.yml`, which includes `gofmt`,
`goimports` and `go vet`), `go mod tidy`, rumdl for Markdown (`.rumdl.toml`),
actionlint for workflows, and general file checks. CI runs the same file, so a
commit that passes locally passes there. A clone with `core.hooksPath` set
from an older setup needs `git config --unset core.hooksPath` first.

## Synthetic data

Every fixture is written by hand, from nothing. Never copy a file from a real
project, even with the values changed: structure, column names and
neighbouring values leak too.

| Kind | Use |
|---|---|
| Person names | Placeholder names that are obviously fake: `Doe, Jane`, `Roe, Richard`, `Example, Pat` |
| Emails, domains | `example.com`, `example.org`, `example.net` (RFC 2606) |
| Phone numbers | `555-0100` to `555-0199` |
| SSN-shaped values | area `000`, `666` or `9xx`, none of which is ever issued |
| Dates | years `2100` to `2199` |
| GUIDs and tenant ids | all zeros, or generated fresh, never copied |
| Organizations, workspaces, servers | `Contoso`, `Fabrikam`, `Example Workspace`, `example.cloud` |
| Tables and columns | generic schema: `student`, `enrollment`, `full_name`, `student_id` |

PBIR fixtures keep Power BI Desktop's serialization: no final newline and no
reformatting. `.gitattributes` keeps LF.

## The private-terms check

A maintainer keeps a list of the terms of their real deployments
(organizations, systems, people, schema names) **outside** the repository,
one case-insensitive term per line:

```text
${PBIPX_PRIVATE_TERMS:-$HOME/.config/pbipx/private-terms.txt}
```

With the pre-commit hooks installed, every commit's staged paths,
content and message are checked against it. The check fails closed: if the
list is missing or empty, the commit is refused. It reports which file matched
and never the term. The list itself must never be committed.

## Commits

Describe structure, never a value. Never use `--no-verify`, or `SKIP=` on a
commit.
