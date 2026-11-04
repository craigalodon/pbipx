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
list is missing or empty, the commit is refused, and a file the check cannot
read counts as a failure, never as clean. It reports which file matched and
never the term. The list itself must never be committed.

`scripts/check-private-terms.sh` has three more modes, for the maintainer
before going public (phase 7 in the roadmap). None of them prints a term or
text that holds one.

- `--tracked` checks every tracked file in the working tree.
- `--history` checks everything reachable from any ref: every blob, path,
  commit and tag message, author, committer and tagger identity, and ref name.
  Run it in a clone made with `git clone --mirror`, so that `refs/pull/*` is
  included. It reports the kind and object id, and a path when the path holds
  no term. It makes one pass over the objects. grep cannot see inside
  compressed content (zip, xlsx, pbix), so a term inside an archive is not
  found.
- `--github [owner/repo]` reads, with `gh` and `jq`, the text that is not in
  git: pull request and issue titles and bodies, comments, reviews and release
  notes. It reports the kind and number. An API error, such as a token that
  lacks a permission, is a failure. Discussions, wikis and edit history are
  not read.

`go test ./scripts` tests all of this against temporary repositories with a
synthetic term list.

## Commits

Describe structure, never a value. Never use `--no-verify`, or `SKIP=` on a
commit.
