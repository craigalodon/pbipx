# ADR-0007: The Desktop round trip: `stage` and `collect`

- **Status:** Proposed. Every default below is the proposer's, for the
  maintainer to confirm or overturn before the commands are built. Where a
  default rests on how Power BI Desktop behaves, that is named in the
  [spec](../design/stage-collect.md) as something only Desktop can confirm.
- **Date:** 2026-11-04

## Context

Power BI Desktop is the only tool that edits a report visually, and it is a
Windows program that wants a folder of its own to work in. Two things about
it fight the rest of pbipx.

- **It writes real data.** Next to any model it opens, Desktop writes a data
  cache (`.pbi/cache.abf`) holding an imported copy of the model's tables, and
  it can save DAX queries, pending query changes and other local state. In a
  repository, that folder sits in a tree agents search. The gate refuses it
  by class (principle 8), but the safest copy of that data is the one that is
  never in the repository at all.
- **It re-serializes.** Desktop writes CRLF and no final newline in report
  JSON. A hook that "fixes" either and Desktop then fight forever
  (principle 12), and the cost is someone reaching for `--no-verify`, which
  skips the scan too.

[ROADMAP phase 6](../ROADMAP.md) answers both with two commands: `stage`
copies a project out of the repository so Desktop can open it, and `collect`
brings back only what Desktop saved. The exit criterion is a staged edit that
round-trips with `git status` showing only real changes.

This record decides the design. The behavior is specified in
[stage-collect.md](../design/stage-collect.md) and in the corpus areas
`stage` and `collect`.

## Decision

### The commands are person-only

`stage` and `collect` are person-only: the gate denies an agent's call that
runs either, as it denies `policy approve` (ADR-0006), and they are added to
the gate's person-only list in the pull request that builds them. The
maintainer has decided this. In the first version the commands do not demand
a terminal, as `policy approve` does: the corpus runs them with output piped,
and the gate is the control. The maintainer wants a terminal check added once
the commands exist and can be tested, with a way for the corpus runner to
supply a terminal.

### `stage`

`pbipx stage <report> --to <dir>` copies one report folder and the semantic
model beside it into `<dir>`, from the committed state, rebinds the report to
that copy of the model, writes a manifest, and registers the working copy. It
prints the file to open in Desktop.

It refuses, with a class from the corpus vocabulary, and writes nothing:

| Refusal | When |
|---|---|
| `inside-repository` | the working copy would be inside any git repository |
| `link-in-path` | a symbolic link or a junction is in the path of the working copy |
| `dirty-tree` | the report or model folder has changes git has not committed, untracked files included |
| `target-not-empty` | `<dir>` exists and is not empty |
| `sync-folder` | the working copy would be in a known sync folder, found by path and environment variables |
| `already-staged` | the same model already has a registered working copy |
| `model-not-found` | the model beside the report cannot be named |

Why each. Inside any repository: `cache.abf` would land in a tree some agent
searches. A link or junction: the path the person sees is not where the data
is, and the gate's path rules and the sync check would judge the wrong place.
A dirty tree: `collect` compares against the commit, and a clean base is what
makes the final `git status` show only Desktop's changes. A non-empty target:
`stage` never merges into, or deletes, what is there. A sync folder: the
cache would be uploaded to a service, which is a copy of the data nobody can
retract. A second stage of the same model: two working copies would be two
sets of edits against one base, and only one could be collected cleanly.

The dirty check covers the report and model folders only. A change elsewhere
in the repository does not block a stage.

### Scope: one report and its model

Version 1 stages one report with its model. A report that points at a model
that is not beside it, or two reports sharing one model, are not handled;
multi-report staging comes later and gets its own record if it changes any
rule here.

### `collect` brings back only what Desktop saved

For the report and model folders of the manifest, `collect` brings back the
files of known kinds that Desktop wrote, and nothing else. It never brings
back:

- anything under `.pbi/` (the data cache, local settings, pending changes);
- saved DAX queries;
- Windows alternate data streams, and the `*:Zone.Identifier` files they
  appear as under WSL;
- any file of an unknown kind. The set of known kinds is a closed allowlist in
  the spec. A file outside it is left behind and listed by path, never read or
  printed.

The rebound `definition.pbir` never comes back: it holds the staging's
binding, not the repository's. Deletions round-trip: a file the manifest lists
that is gone from the working copy is deleted from the repository. A save that
changes nothing leaves `git status` empty, which includes line endings and the
missing final newline: a file whose bytes differ from the committed ones only
in line endings, or in the final newline that report JSON does not carry, is
unchanged.

### Findings at `collect`: all or nothing

`collect` scans what is coming back, as the repository's policy would scan it
at the destination path. If anything is found, it refuses the whole collect,
brings nothing back, and leaves the working copy exactly as it was. It reports
the findings redacted (principle 2): path, context, column, tier and reasons.
A file the scanner cannot vet, such as an image the policy does not allow
unscanned, is an error finding and refuses too (principle 10). Warnings refuse
only under `--strict`. A partial collect is not offered: the person fixes the
selection in Desktop, saves, and collects again.

### The manifest holds the commit and the file list

The manifest, `.pbipx-stage.json` at the working copy's root, holds the source
commit, the repository paths of the report and model folders, and the list of
staged files. It holds **no per-file content digests**. A digest of a report
file is a digest of something that can hold a value, and principle 3 says
never to hash a value. `collect` compares bytes: the working copy's file
against the committed blob it was staged from. The cost is a read of each file
at collect time, which is cheap beside the Desktop session that precedes it.

### Gate protection for working copies

> **Built** (2026-11-05): the registry, the deny rule, the reason class
> `working-copy` and the gate corpus cases are in, ahead of `stage`.
> [behavior.md](../design/behavior.md), "Working copies", says what they
> do; the person-only entries for `stage` and `collect` come with those
> commands.

Every path under a registered working copy is denied by the gate, with no
scanning, because `cache.abf` holds real data that a scanner would call clean
(principle 8). The registry of working copies lives in the store, beside the
approval records (ADR-0006), where the gate already denies an agent's write.
`stage` adds an entry and `collect --done` or `--discard` removes it.

**This protection merges before `stage` does.** It is a separate, later pull
request than the one that adds this record and the corpus, and `stage` is not
built until it is in: a stage that creates a working copy the gate does not
yet protect would be worse than no stage. That pull request adds the
registry, the deny rule, a gate reason class for it, and the gate's corpus
cases. This record changes nothing in `internal/gate`.

### The corpus areas are pbipx-only

The areas `stage` and `collect` exist only in pbipx. The predecessor has no
such commands and is retiring, so ADR-0003's order of work (specify against
the predecessor, then port) does not apply. ADR-0003 carries a dated
amendment. The cases are written from this design, held out of
`enabledAreas` until the commands exist, and become the commands' acceptance
test.

## Consequences

The data cache lives outside the repository for the whole of a Desktop
session, and the gate, not the person's care, keeps agents out of it. A
person's round trip costs two commands and a second `git status` that is
clean when nothing changed.

The sync-folder and link checks are heuristics, not guarantees, as the threat
model says of every control here: a sync client with a name the list lacks, or
a mount that is a link in disguise, gets through. The list is data in the
spec and grows with reports of misses.

Refusing a dirty tree makes the person commit or stash first. That is the
price of a base `collect` can trust, and it keeps the final diff honest.

All-or-nothing collect means one persisted selection blocks an otherwise good
edit until it is cleared. That is principle 12's point: the selection is the
leak.

Much of the design rests on facts about Desktop that this record cannot check:
what it writes, in what bytes, and where. The spec lists them. Those defaults
are provisional until the maintainer has run a real session against them, and
the cases that pin bytes (the rebound `definition.pbir`, the final newline)
are the first to change if Desktop disagrees.

## Alternatives considered

**Stage inside the repository, in an ignored folder.** Simpler, no path
translation, no sync check. But the cache then sits in a tree every agent
searches, protected only by a gate rule and a `.gitignore` line, and a
mistake in either puts real data one `git add -f` from history. Rejected on
principle 12.

**Per-file digests in the manifest.** Cheaper to compare. Rejected on
principle 3. The manifest is also read by tools that treat it as safe.

**A partial collect that skips files with findings.** Convenient, and it
leaves the repository in a state no one saved: a visual from one save and a
page from another. Rejected for all-or-nothing.

**Scanning the working copy for the gate instead of denying it.** The data
cache is opaque and a scan would pass it. Deny, then, as for `.pbi/` in a
protected root.

**Bringing back everything Desktop wrote and filtering in a commit hook.**
Puts the cache in the repository first. Rejected.

## Questions for the maintainer

1. Is a dirty-tree refusal limited to the report and model folders right, or
   should any change in the repository block a stage?
2. Is removing the working copy only on an explicit `--done`, as proposed,
   right? It lets a person collect repeatedly from one session, at the cost
   of a working copy, and its data cache, that lives until someone ends it.
3. Is refusing a working copy inside *any* git repository too strong for
   someone who stages into a scratch repository? The proposal says no
   exception.
4. Should warnings refuse at `collect` without `--strict`, given that a saved
   warn-tier selection is what Desktop most often leaves?
