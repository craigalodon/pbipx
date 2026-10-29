# pbipx

Tools for developing Power BI projects (PBIP) with AI coding agents, without
leaking the data Power BI Desktop writes into them.

> **Status: pre-alpha.** This repository holds the design and roadmap and a
> skeleton. Nothing here is usable yet. See [docs/ROADMAP.md](docs/ROADMAP.md).

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

## What it will do

| Command | Purpose |
|---|---|
| `pbipx scan` | Scan a repository, staged changes or files for persisted values and personal-data shapes. Block or warn, with a reviewed baseline for warnings. |
| `pbipx scan-text` | Scan prose such as commit messages and notes for personal-data shapes. |
| `pbipx gate` | An AI agent's pre-tool hook. It denies reads of files with findings, and of files it cannot vet. |
| `pbipx explain` | Print a report's or model's structure (pages, visuals, tables, columns, measures, relationships, sources) with no values. |
| `pbipx stage` / `collect` | Round-trip a project between the repository and a working copy outside it that Power BI Desktop can open. |
| `pbipx perf` | Summarize Performance Analyzer exports without printing query text. |

The commands ship as one binary for Windows, macOS and Linux, with no runtime
to install.

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

## License

Not yet chosen. Until a license file is added, no license is granted.
