# pbipx

Tools for developing Power BI projects (PBIP) with AI coding agents, without
leaking the data Power BI Desktop writes into them.

> **Status: pre-alpha (v0.1).** `scan`, `scan-text`, `baseline`, `explain`,
> `gate`, `init` and `policy` work; `stage`, `collect` and `perf` are not
> built yet. Try it on synthetic projects before relying on it. See
> [docs/ROADMAP.md](docs/ROADMAP.md).

## Start here

- **New to pbipx?** Read the [guide for analysts](docs/guide.md). It covers
  setup, what a refusal looks like and what to do about it.
- **Want to try it safely?** Copy the sample project and follow its
  walkthrough: [`examples/sample-pbip/`](examples/sample-pbip/README.md). It
  is invented, and holds no real data.
- **Ready to install?** See [Install](#install) and
  [Getting started](#getting-started) below.

## The problem

A Power BI project saved as PBIP looks like code: JSON for the report (PBIR),
TMDL for the semantic model, ready for git and for an AI agent to edit. But
Power BI Desktop serializes real model values into those files:

- **Reports** keep whatever was selected at save time: slicer selections,
  filter-pane state, expanded matrix rows, conditional-formatting
  comparisons, and typed titles.
- **Semantic models** carry values in code: hardcoded filters and ID lists,
  "Enter data" tables, `DATATABLE` rows, row-level security rules and role
  members, and free-text descriptions.

Leave a slicer on one person and their name is in the file. Commit it and it
is in git history. Let an agent read it and it is in a transcript nobody can
retract. Nothing in Desktop warns you.

pbipx finds those values before they are committed or read. It also covers the
SQL, YAML and Markdown that usually live beside a Power BI project, and it
never prints a value it finds.

## Commands

The Status column says what exists in this version.

| Command | Status | Purpose |
|---|---|---|
| `pbipx scan` | works | Scan a repository, staged changes or files for persisted values and personal-data shapes. Block or warn, with a reviewed baseline for warnings. |
| `pbipx scan-text` | works | Scan prose such as commit messages and notes for personal-data shapes. |
| `pbipx baseline` | works | Record a warning as accepted until a date (`record`), and check that none has expired (`verify`). |
| `pbipx gate` | works | An AI agent's pre-tool hook. It denies reads of files with findings, and of files it cannot vet, in the repositories set up for pbipx (`pbipx init`, or an approved policy), and leaves every other repository alone. |
| `pbipx init` | works | Write a starting policy and register the gate as an agent hook. |
| `pbipx policy approve` / `status` | works | Approve the repository's policy at a terminal, or report whether it is approved. The gate denies every call until a person approves the policy, and again after any change to it. |
| `pbipx policy unenroll` | works | Remove this clone's setup for the gate on this machine, at a terminal, for every work tree of the clone. The gate stops protecting its files from agents, and every approval is forgotten. Refused while the work tree has a `.pbipx.toml`. |
| `pbipx explain` | works | Print a report's or model's structure (pages, visuals, tables, columns, measures, relationships, sources) with no values. |
| `pbipx stage` / `collect` | not built yet | Round-trip a project between the repository and a working copy outside it that Power BI Desktop can open. Designed in [docs/design/stage-collect.md](docs/design/stage-collect.md). |
| `pbipx perf` | not built yet | Summarize Performance Analyzer exports without printing query text. Specified in [docs/design/perf.md](docs/design/perf.md). |

The commands ship as one binary, with no runtime to install. Releases are built
for Linux. The code is written for Windows and macOS too, and is tested on them
only by hand, so on those build it from source with Go.

## Install

Pre-alpha: releases are for trying it out, not for relying on it.

1. From the [releases page](https://github.com/craigalodon/pbipx/releases),
   download the archive for your architecture
   (`pbipx_<version>_linux_<arch>.tar.gz`), `checksums.txt` and
   `checksums.txt.sigstore.json`. v0.1.1 and earlier also have Windows and
   macOS archives.
2. Check that `checksums.txt` was signed by this repository's release
   workflow, with [cosign](https://docs.sigstore.dev/cosign/system_config/installation/):

   ```sh
   workflow='^https://github\.com/craigalodon/pbipx/'
   workflow="$workflow"'\.github/workflows/release\.yml@refs/tags/v'
   cosign verify-blob \
     --bundle checksums.txt.sigstore.json \
     --certificate-identity-regexp "$workflow" \
     --certificate-oidc-issuer https://token.actions.githubusercontent.com \
     checksums.txt
   ```

3. Check the archive against it:

   ```sh
   sha256sum --ignore-missing -c checksums.txt
   ```

4. Extract the binary and put it on your `PATH` in a directory outside any
   repository, such as `~/.local/bin`. A binary inside the working tree can be
   changed by the agent it is meant to check
   ([threat model](docs/design/threat-model.md), layer 4).

## Getting started

1. Install pbipx as above.
2. In the repository that holds your Power BI projects, run `pbipx init`. It
   writes a commented starting policy, `.pbipx.toml`, that protects the
   directories holding your `*.Report` and `*.SemanticModel` folders, and
   registers the gate as a Claude Code hook in your user-level settings
   (`~/.claude/settings.json`, or `$CLAUDE_CONFIG_DIR`), where a project
   cannot remove it. A project can still turn user-level hooks off with
   `disableAllHooks`, so on shared machines and for teams use `--managed`: it
   prints the hook as a drop-in file for an administrator's managed settings
   instead. `--dry-run` shows what it would do; `--gitignore` adds `.pbi/`,
   `*.pbix` and `*.abf` to `.gitignore`. init writes nothing through a
   `.pbipx.toml` or `.gitignore` that is a symbolic link.

   After upgrading pbipx, run `pbipx init` again: it updates the registered
   hook in place, widening its matcher when a new version gates more tools
   (this one adds PowerShell and LSP), and removes duplicates.
   A hook left by a pbipx binary at another path is reported, and replaced
   with `--force`.
3. Review the policy, above all its column lists: they are a generic start.
4. Approve it at a terminal with `pbipx policy approve`
   ([ADR-0006](docs/adr/0006-policy-integrity.md)), and again after every
   change.
5. Add the commit hooks to `.pre-commit-config.yaml` and run
   `pre-commit install --hook-type pre-commit --hook-type commit-msg`. They
   run the `pbipx` on your `PATH`:

   ```yaml
   - repo: https://github.com/craigalodon/pbipx
     rev: <a release tag>
     hooks:
       - id: pbipx-scan
       - id: pbipx-commit-msg
   ```

## Principles

The rules everything else follows. The reasoning is in
[docs/design/principles.md](docs/design/principles.md).

- **Values are protected; names are schema.** Column and table names are
  fine to print. Values are not, ever.
- **Redaction is built in, not a flag.** A value read from a file cannot
  render itself. Showing it takes an explicit flag on an interactive
  terminal, and that flag is refused in CI and in hooks.
- **Fail closed.** If the gate cannot decide, it denies. A file it cannot
  vet is denied.
- **Noise is a defect.** People bypass a control that fires on everything,
  so each rule is measured before it ships.
- **Policy is configuration.** Nothing about a particular organization lives
  in the tool.

## Design documents

For maintainers and the curious.

- [docs/design/principles.md](docs/design/principles.md): the rules, and why.
- [docs/design/threat-model.md](docs/design/threat-model.md): what pbipx
  protects, and what it does not.
- [docs/design/behavior.md](docs/design/behavior.md) and
  [docs/design/corpus.md](docs/design/corpus.md): how verdicts are decided,
  and the language-neutral test corpus.
- [docs/adr/](docs/adr/): decision records.
- [docs/ROADMAP.md](docs/ROADMAP.md): phases and exit criteria.
- [CONTRIBUTING.md](CONTRIBUTING.md): working on the code.

## License

Copyright 2026 Craig Green. Licensed under the Apache License, Version 2.0
(Apache-2.0). See [LICENSE](LICENSE) and [NOTICE](NOTICE).
