# `stage` and `collect`

> **Status: draft, unbuilt.** This is the specification for roadmap phase 6,
> decided as proposed defaults in
> [ADR-0007](../adr/0007-the-desktop-round-trip.md). It was written without
> running Power BI Desktop. Everything that depends on what Desktop really
> does is marked **[Desktop]** and numbered; the list is collected in
> "What only Desktop can confirm". Treat a marked statement as a hypothesis
> until the maintainer has checked it in a session.

The behavior is pinned by the corpus areas `stage` and `collect`
([corpus.md](corpus.md), "Stage and collect cases"), which are held out of
`enabledAreas` until the commands exist.

## Terms

- **Project**: one report folder (`<Name>.Report`) and the semantic model
  folder beside it (`<Name>.SemanticModel`), in the repository.
- **Source**: the project as committed.
- **Working copy**: the copy `stage` makes outside the repository, which
  Desktop opens and saves.
- **Manifest**: `.pbipx-stage.json` at the working copy's root.
- **Registry entry**: a record in the store (ADR-0006) that the working copy
  exists. The gate denies every path under a registered working copy.

## Commands

```text
pbipx stage <report> --to <dir> [--model <dir>] [--format text|json]
pbipx collect <working-copy> [--strict] [--dry-run] [--done | --discard]
                             [--format text|json]
```

Both are person-only: the gate denies an agent's call that runs either
(ADR-0007). Neither reads a terminal; neither has `--show-values` in v1.

### `stage`

`<report>` is the report folder, relative to the current directory or
absolute, in any of the path forms below. `--to <dir>` is the working copy's
folder, required: `stage` has no default location, because a default would be
a place the person did not choose. `--model <dir>` names the model folder when
it cannot be found. Without it the model is, in order:

1. the folder `definition.pbir` binds the report to, by path, when it exists
   and is a `*.SemanticModel` folder in the same repository;
2. the one `*.SemanticModel` folder beside the report folder, when there is
   exactly one.

Otherwise `stage` refuses with `model-not-found`.

`stage` runs in a git repository. It does this, in order, and stops at the
first refusal, having written nothing:

1. Resolve `<report>`, the model and `<dir>`; refuse a report or model that
   is outside the repository's work tree (usage error).
2. Refuse as the table below says for `<dir>`.
3. Refuse `dirty-tree` if `git status --porcelain` for the two folders is not
   empty. Ignored files do not count.
4. Refuse `already-staged` if a registry entry exists for the same
   repository and model path whose working copy still exists. An entry whose
   folder is gone is stale: `stage` removes it and says so.
5. Write the files from the commit (`HEAD`), not from the work tree: the
   blobs of the two folders, with no filter applied, so that an ignored file
   a previous in-repository Desktop session left (a `.pbi/` folder) is never
   copied.
6. Rebind `definition.pbir` (below).
7. Write the manifest, then the registry entry.
8. Print what to open and what happens next.

If anything fails after the first byte is written, `stage` removes what it
wrote, working backward, and says what it removed. It never deletes a path it
did not create.

Layout of `<dir>`, which is flat and holds the project's folders by their own
names:

```text
<dir>/
  .pbipx-stage.json
  Example.Report/...
  Example.SemanticModel/...
  Example.pbip              only if the source has a .pbip naming the report [D2]
```

### `collect`

`<working-copy>` is the folder `stage` made. `collect` must run in the
repository the working copy was staged from (any directory in its work tree).

1. Resolve `<working-copy>`. Refuse `not-a-working-copy` if it has no valid
   manifest, or no registry entry, or the registry entry names another path.
2. Refuse `source-changed` if the repository does not have the manifest's
   commit, if the two folders have uncommitted changes, or if their trees at
   `HEAD` differ from their trees at the manifest's commit. A `HEAD` that
   moved without touching the project is fine.
3. Walk the working copy and sort every file into: **comes back** candidates
   (known kinds, below), **left behind** (everything else), and **missing**
   (listed in the manifest, a known kind, absent now).
4. For each candidate, compare with the committed blob (below). Equal: not a
   change. Different, or absent from the commit: a **change**.
5. Scan every change as the repository's policy would scan it at its
   destination path. Anything found (a block finding, an error finding, or a
   warning under `--strict`): refuse `findings`, write nothing, and leave the
   working copy untouched.
6. Otherwise apply all changes and deletions to the work tree. The writes go
   to temporary files in the destination directories and are renamed into
   place, so a failure part way is rolled back and reported. Folders left
   empty by a deletion are removed.
7. Print counts and paths. With `--done`, then delete the working copy and its
   registry entry. With `--dry-run`, do steps 1 to 5 and 7 only.

`--discard` deletes the working copy and its registry entry without bringing
anything back. It is the only way, besides `--done`, to end a staging; it asks
for no confirmation, because the person who ran it knows what they are
deleting, and it refuses unless the folder has a valid manifest and registry
entry, so it cannot delete an arbitrary folder. `--done` and `--discard` are
exclusive, and `--discard` takes no other flag but `--format`.

Collecting is repeatable: without `--done`, the working copy stays registered,
and a later collect compares against the same commit. After a first collect
the repository differs from the manifest's commit by the collected changes
(uncommitted), so step 2's clean check would fail for a second collect.
Therefore a second collect needs the first one committed or stashed, and the
refusal says that. **Open question 1.**

### Flags

| Flag | Command | Meaning |
|---|---|---|
| `--to <dir>` | stage | the working copy's folder; required |
| `--model <dir>` | stage | the model folder, when it cannot be found |
| `--strict` | collect | warnings refuse too, as for `scan` |
| `--dry-run` | collect | do everything but change anything |
| `--done` | collect | after a successful collect, delete the working copy |
| `--discard` | collect | delete the working copy and bring nothing back |
| `--format text\|json` | both | the output form; `text` is the default |

### Exit codes

As elsewhere ([corpus.md](corpus.md), `exit-codes`):

| Code | `stage` | `collect` |
|---|---|---|
| 0 | staged | collected, or nothing to collect |
| 1 | | refused for a block finding |
| 2 | | refused for warnings under `--strict` |
| 3 | any refusal, or a failure such as `not-a-repository` | any other refusal or failure; refused for an error finding |
| 4 | usage error | usage error |

A refusal other than `findings` is an operational refusal and exits 3, as
`baseline record` under `CI` does. A usage error is a missing or unknown flag
or argument, a report or model outside the repository, or `--done` with
`--discard`.

### Output

`--format json` prints exactly one of these on standard output, and nothing
else, so a tool can read it ([corpus.md](corpus.md)):

```json
{ "ok": true, "open": "<path>", "files": 12 }
{ "ok": true, "changed": 2, "added": 1, "deleted": 0, "left_behind": 3 }
{ "refusal": "dirty-tree" }
{ "refusal": "findings", "findings": [ { "path": "...", "context": "...",
  "column": "...", "tier": "block", "reasons": ["block-column"] } ] }
{ "failure": "not-a-repository" }
```

The first is `stage`'s, the second `collect`'s. A `findings` refusal has the
shape of a scan's findings, with `path` the repository path the file would
have had. Text output carries the same facts in prose.

## Refusals

Each refusal is a class from the vocabulary's `refusals` and exits 3, except
`findings`. Messages are fixed sentences. They may name a path as the person
gave it, a repository-relative path, a class, a count or a line number. They
never quote file content, a finding's value, or text from a file's body
(principle 2).

| Class | Command | When |
|---|---|---|
| `inside-repository` | stage | a parent of `<dir>`, or `<dir>` itself, holds a `.git` (folder or file) |
| `link-in-path` | stage | a component of `<dir>`'s path that exists is a symbolic link, a junction or another reparse point |
| `dirty-tree` | stage | the report or model folder has uncommitted changes, untracked files included |
| `target-not-empty` | stage | `<dir>` exists and holds anything |
| `sync-folder` | stage | `<dir>` is under a known sync folder |
| `already-staged` | stage | the model has a registered working copy that exists |
| `model-not-found` | stage | no model, or more than one, can be named |
| `not-a-working-copy` | collect | no valid manifest or no registry entry |
| `source-changed` | collect | the project's tree is not the one that was staged |
| `findings` | collect | the changes have findings; nothing was brought back |

Examples of the message form, one line each, all structure:

```text
pbipx: stage: refused (dirty-tree): 2 files under pbip/Example.Report changed
pbipx: stage: refused (sync-folder): the working copy would be in a sync folder
pbipx: collect: refused (findings): 1 block finding in 1 file; nothing came back
  pbip/Example.Report/definition/pages/0000/visuals/0001/visual.json
    slicer-selection  student.full_name  block  block-column person-name-shape
```

A `findings` refusal also says what to do next without naming a value: "reset
the selection in Desktop, save, and collect again".

### Where a working copy may not be

- **Inside any git repository.** Not only the source's: a scratch repository
  is still a tree an agent may search, and `git add` of a stray folder is one
  command. The check walks `<dir>` and each parent, resolving nothing, and
  stops at a `.git`.
- **Behind a link.** Every component of `<dir>` that exists is looked at with
  `lstat`, not followed. A symbolic link refuses. On Windows, a junction or
  mount point refuses too (Go reports one as an irregular file; the
  implementation must test for it and not only for `ModeSymlink`). A
  component that does not exist yet cannot be a link.
- **In a sync folder.** The path, after translation to one form, is checked
  against both: any component whose name, ignoring case, starts with one of
  `OneDrive`, `Dropbox`, `Google Drive`, `GoogleDrive`, `iCloud`, `Box`,
  `Nextcloud`, `ownCloud`, `pCloud`, `MEGA`, `SharePoint`; and the
  directories named by the environment variables `OneDrive`,
  `OneDriveCommercial`, `OneDriveConsumer`, when set. "Starts with" is what
  catches `OneDrive - Contoso`. The list is a starting point and is data in
  the implementation, not policy: a product it misses gets added.
  Under WSL, the Windows variables are usually not in the environment, so the
  path components do the work; whether to also ask Windows (`cmd.exe /c
  echo %OneDrive%`) is **open question 2**.

## The manifest

`.pbipx-stage.json`, written by `stage`, UTF-8, LF, one JSON object, with a
final newline (it is pbipx's file, not Desktop's):

```json
{
  "version": 1,
  "commit": "0000000000000000000000000000000000000000",
  "report": "pbip/Example.Report",
  "model": "pbip/Example.SemanticModel",
  "staged": "2100-01-01T00:00:00Z",
  "files": [
    "Example.Report/definition.pbir",
    "Example.Report/definition/pages/0000/visuals/0001/visual.json",
    "Example.SemanticModel/definition.pbism"
  ]
}
```

- `commit` is the full object name of `HEAD` at stage time.
- `report` and `model` are repository-relative, forward slashes.
- `files` are the staged files, relative to the working copy, sorted by
  byte order, forward slashes: exactly the list `collect` uses to find a
  deletion. It lists every staged file, `definition.pbir` included.
- There is **no digest of any file**. `collect` decides "changed" by comparing
  bytes with the blob at `commit` (principle 3, ADR-0007).
- Unknown keys, a `version` other than 1, a `files` entry that is absolute or
  contains `..` or a backslash, or a duplicate, make the manifest invalid
  (`not-a-working-copy`). The reader never follows a path out of the working
  copy.

The registry entry is a separate file in the store,
`stages/<key>.json` with `key` the SHA-256 of the working copy's absolute
path in one canonical form. It holds the working copy's path, the
repository's git directory, the model's repository path and the date. It is
the gate's source of truth for "this path is protected". It holds no content.
(Hashing a path is not hashing a value; a path is structure.)

## Rebinding

A report is bound to its model by `definition.pbir`, JSON, in the report
folder. **[Desktop D1]** The file has a `datasetReference` that is either
`byPath` (a relative path to a model folder) or `byConnection` (a service
model's connection). In the repository a report is often bound by connection,
so that it works against the published model; Desktop must open it against the
local copy of the model beside it, so that edits are made against what was
staged and no query goes to the service.

`stage` rewrites the working copy's `definition.pbir` so that:

```json
"datasetReference": {
  "byPath": { "path": "../Example.SemanticModel" }
}
```

with the model folder's own name, and every other key left as it was. If the
file is already bound by that path, its bytes are left alone. The rewritten
file is serialized with two-space indentation, no final newline and LF, the
way Desktop is believed to write it. **[Desktop D1]** If the source has no
`datasetReference` or the file does not parse, `stage` refuses
`model-not-found` and writes nothing; it does not guess a binding.

The rebinding is **restored by not coming back**: `collect` never reads the
working copy's `definition.pbir`, and the repository's copy is never written.
A binding the person changes in Desktop is lost; that is documented, not
fixed. The case `collect/rebound-definition-pbir-does-not-come-back` pins
this.

## What comes back

`collect` considers only files below the two folders, and only these kinds.
Paths are relative to the folder. **[Desktop D4]** The list is the proposer's
reading of the PBIR and TMDL layouts and is the thing most likely to need
editing.

| Folder | Comes back |
|---|---|
| `<Name>.Report` | `definition/**/*.json`; `StaticResources/RegisteredResources/**` with an image extension (`png`, `jpg`, `jpeg`, `gif`, `svg`) |
| `<Name>.SemanticModel` | `definition/**/*.tmdl`; `definition.pbism`; `diagramLayout.json` |

Everything else is **left behind**, and listed, by path and never read:

- `.pbi/` anywhere: `cache.abf`, `localSettings.json`, `editorSettings.json`,
  `unappliedChanges.json`, and whatever else Desktop puts there. **[D4]**
- Saved DAX queries, in the folder Desktop keeps them. **[D4]** Their folder
  name is believed to be `DAXQueries`; confirm it.
- Alternate data streams and `*:Zone.Identifier` files. **[D5]** Under WSL a
  stream appears as a file named `<file>:Zone.Identifier`; on Windows it is not
  a separate file. Any name containing `:` after the first path character is
  left behind, whatever follows it.
- Any file of a kind not in the table: lock files, temporary files, a
  `.platform`, a `.pbip`, `definition.pbir`.

Left-behind files are not scanned. They are not in the repository either, and
the gate denies the working copy anyway.

A file that is on the table and is a **link**, a device or anything but a
regular file is left behind, and counted, not followed.

## Comparing and writing bytes

**[Desktop D3]** Report JSON as Desktop writes it has no final newline, and
Desktop is believed to write CRLF. The repository stores LF
(`.gitattributes`, `* text=auto eol=lf`), and report JSON in it has no final
newline (principle 12). `collect` therefore compares and writes as follows.

For a candidate file and the committed blob it came from:

1. **Line endings.** Convert each CRLF in the working copy's bytes to LF.
   A lone CR is left alone. If the blob itself contains CRLF, the repository's
   convention is CRLF for this file: convert to CRLF instead, so a repository
   that does not normalize does not get a diff on every line.
2. **Final newline.** The final-newline state of the result is the blob's.
   For a file the repository does not have, the state is none for JSON
   (`.json`, `.pbism`), and as saved for TMDL. **[D3]** Whether Desktop writes a
   final newline on TMDL is not known.
3. **Equal means unchanged.** After steps 1 and 2, if the result equals the
   blob, the file is not a change and is not written. The repository's file
   keeps its mode and mtime. That is how a save that changes nothing leaves
   `git status` empty even if Desktop rewrote every file.
4. **A change** is written with the result of steps 1 and 2, never with the
   working copy's bytes as they were.

A file with bytes that are not UTF-8 is handled as bytes; the scan reports it
as an error finding where its kind is scanned (`not-utf8`).

What `collect` must not do is reformat JSON, reorder keys or touch
indentation: if Desktop re-serializes a file differently from the committed
one, that is a change, shown by `git diff`, and a person decides. **[D3]**
Whether Desktop re-serializes files the person did not touch (key order,
escapes, indentation) is the single biggest unknown; if it does, the
round-trip's exit criterion needs a normalization this spec does not yet
have. **Open question 3.**

## Paths on Windows and WSL

Desktop is a Windows program. pbipx may run on Windows, or inside WSL on a
repository in the Linux file system. The working copy must be somewhere
Desktop can open it, so `stage` has to read and print paths in both worlds.
**[Desktop D6]**

`stage` and `collect` accept, for `<dir>`, `<report>`, `--model` and
`<working-copy>`, these forms, and convert each to the host's native form
before doing anything:

| Form | Example | On Windows | Under WSL |
|---|---|---|---|
| Drive path | `C:\Work\x`, `C:/Work/x` | native | `/mnt/c/Work/x` |
| WSL mount | `/mnt/c/Work/x` | `C:\Work\x` | native |
| WSL UNC, current | `\\wsl.localhost\Distro\home\u\x` | native UNC | `/home/u/x` |
| WSL UNC, legacy | `\\wsl$\Distro\home\u\x` | native UNC | `/home/u/x` |
| Plain | `/home/u/x`, `x\y`, `.\x` | native | native |

Rules:

- A drive letter is case-insensitive; the mount is its lower-case form.
- The mount root is `/mnt/` unless `/etc/wsl.conf` sets `automount.root`.
  Reading that file is **open question 4**; calling `wslpath` instead is the
  alternative, and costs a process per path.
- A UNC path whose distribution is not the current one (`WSL_DISTRO_NAME`)
  under WSL is refused with a usage error: it names another file system.
- A path in any other UNC form (`\\server\share`) is accepted only on
  Windows, and counts as a link-like path: `stage` does not know what it is.
  It is refused as `link-in-path`. **[D6]**
- Nothing is converted by guessing: a path that is not one of these forms is
  `Plain`.
- Conversion is textual. It resolves nothing, so it runs before the
  link-in-path check and cannot hide a link.

After staging, `stage` prints the file to open in the form the person will
paste into Desktop: the drive form for a path under `/mnt/<letter>/`, the
`\\wsl.localhost\<distro>\...` form for a path in the Linux file system, the
native form on Windows. Whether Desktop works against a working copy in the
Linux file system, over the `\\wsl.localhost` share, is **[D6]**, and the
recommendation to put it on a Windows drive is the proposer's guess. `stage`
does not forbid it in v1.

The gate must recognize a registered working copy in every form in which a
tool call could name it. That is the later gate pull request's work
(ADR-0007), and it reuses these conversions.

Windows path length: a deep project under a deep `<dir>` can pass 260
characters. **[D10]** `stage` checks the longest path it would write and
refuses with a usage error that suggests a shorter `--to` before writing
anything, rather than fail part way.

## Gate interplay

Nothing here changes `internal/gate`. For the later protection pull request
(ADR-0007), the gate needs: the registry in the store; a deny for every path
under a registered working copy, any tool, no scanning; the same deny for a
shell command that names such a path or `cd`s into one; and, in that
pull request or the one that builds the commands, the two commands in the
person-only list. Gate corpus cases for these belong to those pull requests.

## What only Desktop can confirm

Nothing below was checked against a running Desktop. Each item says what the
spec assumes and what a session would show.

| # | Assumption | How to confirm |
|---|---|---|
| D1 | `definition.pbir` has `datasetReference` with `byPath` or `byConnection`; `byPath` with `../<Model>.SemanticModel` opens the staged model; Desktop leaves it alone on save; two-space indentation and no final newline are its serialization | open a staged copy; save; diff `definition.pbir` before and after |
| D2 | Desktop opens a report from a `.pbip` file, or from `definition.pbir` directly; whether a `.pbip` is needed, and how it is named | try both on a staged copy; note which file `stage` should print |
| D3 | Report JSON is saved with CRLF and no final newline; TMDL is saved with LF or CRLF and with or without a final newline; Desktop rewrites files the person did not change, with or without different bytes (key order, escapes, indentation) | open, change one visual, save; compare every file byte for byte with the staged one |
| D4 | The set of files Desktop writes (the table above); the names `.pbi/`, `cache.abf`, `localSettings.json`, `editorSettings.json`, `unappliedChanges.json`; the saved-DAX-query folder; whether `cache.abf` appears for every model or only some storage modes; whether images land under `StaticResources/RegisteredResources` | list the working copy after a session that adds an image and a DAX query |
| D5 | Alternate data streams or `:Zone.Identifier` files can appear in the working copy at all | list the working copy under WSL after a session; check how the staged files were created |
| D6 | Desktop can open and save a project on a `C:` drive, in a folder reached through `\\wsl.localhost\`, and through a `\\wsl$\` path; a UNC share other than WSL works or not | open a staged copy through each form |
| D7 | Desktop does not hold files open or write while idle, so `collect` while Desktop is still running reads the last saved state; lock or temporary files that Desktop leaves in the folder, and their names | run `collect --dry-run` with Desktop open; list the folder |
| D8 | Deleting a visual or page in Desktop removes its file, and updates a parent file (`pages.json`) in the same save | delete a visual; list the working copy |
| D9 | The `.pbi/` folder is created beside the model, and not beside the report | list after the first open |
| D10 | Desktop and the folder layout survive a long path, and what the limit is | stage deep and open |

## Open questions

1. **A second collect from one session.** The clean-tree check of step 2
   means the first collect's changes must be committed or stashed before a
   second. That is safe and may be annoying. The alternative is to remember
   the changes collected and tolerate exactly those; it is more state and a
   subtler check. Which does the maintainer want?
2. **Sync folders under WSL.** Ask Windows for its `%OneDrive%` variables, or
   rely on path components alone?
3. **Normalizing Desktop's re-serialization.** If D3 shows Desktop rewrites
   untouched files differently, the round trip cannot meet its exit criterion
   without a documented, reversible normalization. Not designed here, on
   purpose: it depends on what Desktop does.
4. **The WSL mount root.** Parse `/etc/wsl.conf`, call `wslpath`, or assume
   `/mnt/`?
5. **Opening Desktop.** Should `stage` offer `--open`? It would start a
   Windows program from WSL or Windows. Left out of v1.
6. **`.platform` and the other metadata files.** They are left behind in this
   spec. If Desktop changes them in a way that matters, they join the table.
7. **Whether the working copy should be deletable by `collect --done` when
   Desktop has the folder open.** Windows refuses; the error is reported, and
   the registry entry stays.

### What the predecessor's behavior could inform

These questions are for the predecessor's maintainer, to answer from
experience. Nothing in this repository reads the predecessor, and no answer
should arrive as a fixture or a copied file.

- Which files, in real use, Desktop leaves in a project folder that are not
  the kinds listed here (lock files, temporary files, backups), and how often.
- Whether Desktop rewrote untouched files on save, and what the hooks or
  checks that dealt with it had to normalize: line endings, the final
  newline, key order, escapes. This answers D3 and open question 3 in
  practice.
- Which sync products, and which path shapes, people stage into by accident.
- Which path forms between Windows and WSL went wrong, and what the failure
  looked like (this informs "Paths on Windows and WSL" and D6).
- Whether reports commonly reference a model that is not beside them, or share
  one, which decides how soon multi-report is needed.
- What a person did next after a persisted selection was caught at the Desktop
  boundary, which shapes the text of a `findings` refusal.
- How often an agent tried to read a Desktop folder outside the repository,
  which informs the later gate pull request.

## Test plan

The corpus cases are the acceptance test for behavior that can be put in
fixtures. These are tested in Go when the commands exist, because a fixture
cannot hold them:

- a name with a colon, for the alternate-data-stream rule (it cannot be
  checked out on Windows);
- CRLF input, which `.gitattributes` normalizes in fixtures;
- `source-changed`, which needs the repository to change after the stage;
- the path forms and their conversions, on both operating systems, with a
  fake of `wslpath` and of `/etc/wsl.conf`;
- junction detection on Windows;
- a failure part way through `stage` and through `collect` writes, rolled
  back;
- a binary or non-UTF-8 file in a known kind.
