# Instructions for AI agents

## This repository is public

Every file, fixture, comment and commit message is public as soon as it is
pushed, and an exposure is hard to reverse
([ADR-0002](docs/adr/0002-built-for-public-from-the-first-commit.md)).

- **Nothing from a real deployment.** No organization, tenant, workspace,
  catalog, schema, table, report or person names. No ticket ids, internal
  hostnames or local paths from any other project.
- **Never copy from another repository.** That includes fixtures, policy
  files, test cases and prose, even "anonymized". A real file reworded is
  still shaped by real data. Write it fresh from the rules in
  [CONTRIBUTING.md](CONTRIBUTING.md#synthetic-data).
- **Port behavior, not artifacts.** When matching the predecessor
  implementation, work from the corpus and the design docs, never its tests'
  data.
- If a task seems to need real data, stop and ask.

## The tool never prints a value

Every value read from a scanned file is held as a `secret.Value`
([internal/secret](internal/secret/secret.go)) before it leaves the code that
judges it. Do not add a way to render one other than `secret.Reveal`, and call
`Reveal` only on the `--show-values` path. Error messages carry structure and
line numbers, never source text. See
[docs/design/principles.md](docs/design/principles.md).

## Commands

```sh
go build ./...
go test ./...
go vet ./...
gofmt -l .                      # must print nothing
pre-commit run --all-files      # every lint CI runs, plus private terms
pre-commit install              # once per clone
```

Lint rules live in `.golangci.yml`, `.rumdl.toml` and
`.pre-commit-config.yaml`. Fix the code rather than loosen a rule; a
`//nolint` needs the linter named and a reason.

Never use `git commit --no-verify`, or `SKIP=` on a commit. If a hook blocks
you, fix the cause or tell the user.

## Layout

```text
cmd/pbipx/          the entry point; commands on cobra (ADR-0005)
internal/           packages; nothing is a public Go API yet
examples/           a synthetic sample PBIP project and its demo test
testdata/corpus/    the language-neutral behavior corpus (docs/design/corpus.md)
docs/ROADMAP.md     phases and exit criteria
docs/design/        principles, threat model, corpus format
docs/adr/           decision records
scripts/            the maintainer's private-terms check, run by pre-commit
```
