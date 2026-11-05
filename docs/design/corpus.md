# The behavior corpus

pbipx's behavior is specified by a corpus of cases that does not depend on any
language: input files, a command, and the expected output. Two
implementations agree when both pass it. That is how this rewrite reaches
parity with its predecessor ([ADR-0003](../adr/0003-parity-through-a-language-neutral-corpus.md)),
and it stays as the regression suite after the predecessor retires.

> **Status: draft.** The format is settled when phase 1 of the
> [roadmap](../ROADMAP.md) has run the predecessor against it. What the
> predecessor does, and where pbipx differs, is in
> [behavior.md](behavior.md).

## Layout

```text
testdata/corpus/
  vocabulary.toml                the closed vocabulary
  policies/<name>.toml           shared policies, referenced by name
  <area>/<case>/
    case.toml                    what to run, and what must never appear
    tree/...                     the repository the command runs in
    stdin                        optional: standard input (the gate's hook event)
    after/tree/...               optional: baseline, stage and collect cases
    expected/root/...            optional, stage/collect: files the root holds
    expected/work/...            optional, stage/collect: the working copy's
    expected.json                the normalized result
```

`<area>` groups cases: `pbir`, `tmdl`, `sql`, `yaml`, `text`, `baseline`,
`policy`, `gate`, `explain`, `stage`, `collect`. A case directory is named in
kebab case for what it shows (`slicer-on-block-column-blocks`).

## Running a case

A runner, one per implementation, does this for each case:

1. Copy `tree/` to a fresh temporary directory, the **root**. A case with
   no `tree/` starts from an empty root.
2. Unless `policy = "tree"`, write `policies/<policy>.toml` to
   `<root>/.pbipx.toml`. For a `gate` case, unless `approved = false`,
   approve that policy for the root (see "Policy approval").
3. Unless `git = false`, make the root a git repository and commit every
   file, except those listed in `untracked` (left untracked) and `staged`
   (added to the index, not committed). A file the tree's own `.gitignore`
   ignores is not committed. The commit uses a fixed synthetic author and
   date. Then delete the files listed in `deleted` from the root: they stay
   in the commit, so git history holds what the working tree no longer
   does. Then, for each path in `worktrees`, add a linked work tree of the
   repository there, detached at the commit (`git worktree add --detach
   <path> HEAD`), as Claude Code adds one under `.claude/worktrees/`: it
   holds the committed files, deleted ones included.
4. Replace `{root}` in `args` and `stdin` with the root's absolute path.
   In a `gate` case's `stdin`, a JSON event, the path is written as JSON
   string content: a Windows path's backslashes are escaped.
5. Run the command in the root, with `CI`, `PRE_COMMIT` and
   `PBIPX_NO_REVEAL` unset and standard output not a terminal, unless `env`
   sets them.
6. Check, in order: the exit code; that no sentinel appears in stdout or
   stderr; the normalized result against `expected.json`.

An implementation's runner may translate the corpus policy into its own
format, command names (`baseline record`), and the tool's own name where a
gate event's Bash command runs it (`pbipx scan --all`). The predecessor's
runner does all three.

## case.toml

```toml
description = "slicer selection on a direct-identifier column blocks"
command     = "scan"                 # scan | scan-text | gate | explain | baseline
                                     #   | stage | collect
args        = ["--all"]
policy      = "default"              # policies/default.toml, or "tree" for tree/.pbipx.toml
exit        = 1

# Synthetic values planted in tree/, stdin or args. None may appear in
# stdout or stderr. This is what proves redaction (principle 2): every case
# checks it.
sentinels = ["Doe, Jane"]

# Optional.
git        = true                    # false: the root is not a git repository
untracked  = []                      # paths left untracked
staged     = []                      # paths staged but not committed
deleted    = []                      # paths committed, then deleted
worktrees  = []                      # linked work trees added at the commit
env        = { CI = "true" }         # environment for the command
known      = "predecessor"           # see "Known differences"
approved   = false                   # gate only: run with no policy approval
working-copies = ["copy"]            # gate only: working copies under {work}

# Optional, stage and collect only ("Stage and collect cases").
setup              = [["stage", "pbip/Example.Report", "--to", "{work}/copy"]]
work-deleted       = ["copy/Example.Report/definition/pages/0000/visuals/0002/visual.json"]
status             = [" M pbip/Example.Report/definition/report.json"]
expect-root-absent = ["pbip/Example.Report/.pbi"]
expect-work-absent = ["copy/.git"]
```

Every key except the optional ones is required, and an unknown key is an
error in the case. `sentinels` may be empty only for a case that plants no
value, and its description must say so.

A case with `exit = 4` (a usage error) has no `expected.json`: only the exit
code and the sentinels are checked.

## expected.json

The normalized result, never raw output. Text formatting can differ between
implementations; findings cannot.

```json
{
  "findings": [
    {
      "path": "pbip/Example.Report/definition/pages/0000/visuals/0001/visual.json",
      "context": "slicer-selection",
      "column": "student.full_name",
      "tier": "block",
      "reasons": ["block-column", "person-name-shape"]
    }
  ],
  "accepted": 0,
  "suppressed": 0
}
```

- `findings` are sorted by path, context, column, then reasons: strings in
  byte order, `null` before any string, and reasons element by element
  (a shorter list first when one is a prefix of the other).
- `path` is relative to the root, with forward slashes. A finding read from
  standard input (`scan-text -`) has the path `-`.
- `context` and each reason come from
  [vocabulary.toml](../../testdata/corpus/vocabulary.toml). Reasons within a
  finding are in the vocabulary's `reason-order`.
- `column` is `table.column` when the table is known, the bare column when it
  is not, and `null` when no column decided the verdict.
- `tier` is `block`, `warn` or `error`. An error finding has no context, so
  its `context` and `column` are `null` and its one reason is an error
  reason.
- `accepted` counts warnings accepted by policy, `suppressed` those accepted
  by the baseline. Both default to 0 when absent.
- `locator` (where in the file) is compared only when the expected finding
  has one. Implementations need not agree on locators; the baseline is
  regenerated at cutover.

A failure before or instead of a scan result replaces the findings:

```json
{ "failure": "policy-error" }
```

For `gate`, the result is the decision and a reason class from the
vocabulary's `gate-reasons`, never free text:

```json
{ "decision": "deny", "reason": "finding" }
```

An allow has no reason: `{"decision": "allow"}`. The gate exits 0 on allow
and 2 on deny. How a runner reads the class from the gate's output is up to
that implementation. pbipx starts its deny reason with `pbipx: <class>:`,
and the first line of stderr, which carries it, is authoritative: Claude
Code reads stderr, not stdout, from a hook that exits 2.

For `explain`, the result is the structure as JSON, in the schema of
[explain.md](explain.md).

For `stage` and `collect`, the result is exactly one of `{"ok": true}` and
`{"refusal": "<class>"}`, a class from the vocabulary's `refusals`. The
refusal `findings` (collect refused to bring anything back) also carries
`findings`, in the shape above, with `path` the repository-relative path the
file would have had:

```json
{
  "refusal": "findings",
  "findings": [
    {
      "path": "pbip/Example.Report/definition/pages/0000/visuals/0001/visual.json",
      "context": "slicer-selection",
      "column": "student.full_name",
      "tier": "block",
      "reasons": ["block-column", "person-name-shape"]
    }
  ]
}
```

A failure before any of that, such as `not-a-repository`, is
`{"failure": "<class>"}` as for a scan. Their exit codes are in
[stage-collect.md](stage-collect.md): 0 for `ok`, 3 for any refusal but
`findings`, and for `findings` as a scan's (1 for a block finding, 2 for
warnings under `--strict`, 3 for an error finding).

A value never appears in `expected.json`. The tool never prints one, so a
normalized result has nowhere to put it.

## stdin

For `gate`, the Claude Code PreToolUse event, as JSON. Paths in it use
`{root}`:

```json
{
  "hook_event_name": "PreToolUse",
  "cwd": "{root}",
  "tool_name": "Read",
  "tool_input": { "file_path": "{root}/pbip/A.Report/definition/report.json" }
}
```

For `scan-text -`, the prose to scan.

## Known differences

Where the predecessor and pbipx are meant to differ
([behavior.md](behavior.md), "Divergence"), the case specifies pbipx's
behavior and sets `known = "predecessor"`. The predecessor's runner reports
such a case as an expected failure, not a pass. A case the predecessor fails
by accident is a bug in one of them, and gets an issue.

## Rules for cases

- Every value is synthetic, from the table in
  [CONTRIBUTING.md](../../CONTRIBUTING.md#synthetic-data). Every planted value
  is listed in `sentinels`.
- Every case asserts the exit code as well as the result.
- One behavior per case. A case that must stay clean says why in its
  description.
- A case is added for every bug fixed, before the fix.
- A noise measurement (principle 6) is recorded as a case that must stay
  clean, with the reason in its description.
- Report fixtures keep Desktop's serialization: no final newline.
- A fixture holds only what its case needs. A report case needs the one
  visual or page file that carries the context, not a whole report.

## Policy approval

pbipx's gate denies every call unless the policy is the one a person
approved ([ADR-0006](../adr/0006-policy-integrity.md)). So every `gate`
case runs against an approved policy unless it says `approved = false`: the
runner records the policy written to the root in step 2 as approved for
that root, as the implementation's approval command would, but without its
terminal prompt. pbipx keys an approval by the repository's git directory,
so its runner records it at the end of step 3, once the root is a
repository; the policy approved is still the one step 2 wrote. A root with
no policy file, or with `git = false`, has nothing to approve. The key
`approved` is valid only in a `gate` case.

A `gate` case whose policy is approved may have an `after/tree/`, copied
over the root after the policy is written and before the commit of step 3.
That is
how a case changes the policy after it was approved: `policy = "tree"`,
with the approved policy in `tree/.pbipx.toml` and the edited one in
`after/tree/.pbipx.toml`.

An implementation with no approval check, such as the predecessor, has
nothing to approve and ignores `approved`. The cases that expect
`policy-unapproved` are marked `known = "predecessor"`.

The gate acts only for a repository that has adopted pbipx (behavior.md,
"The gate", adoption): one with a policy file, or one with an approval
record. A `gate` case runs in a repository that has **not** adopted pbipx
when it says `policy = "tree"` with no `tree/.pbipx.toml`, and
`approved = false`: no policy is written and none is approved. A case shows
a repository that stays adopted with its policy gone by approving the policy
in `tree/.pbipx.toml` and listing `.pbipx.toml` in `deleted`: the runner
approves it at the end of step 3, after the file left the working tree. A
case's event may name a working directory outside the root, such as
`{root}/..`, to show a call made from elsewhere that reaches into it.

## Baseline cases

A baseline entry is keyed by a fingerprint, and the fingerprint hashes the
locator, which implementations need not agree on. So a case never holds a
fingerprint. It describes a finding, and the runner asks its own
implementation for that finding's fingerprint.

pbipx's baseline is `.pbipx-baseline.toml` at the root (ADR-0004):

```toml
version = 1

[entries."<fingerprint>"]
severity = "warn"                  # anything else is a baseline error
expires  = "2199-12-31"            # YYYY-MM-DD; unparseable counts as expired
path     = "pbip/.../visual.json"  # for the reviewer; the key is the identity
context  = "slicer-selection"
column   = "enrollment.cohort_year"
reasons  = ["warn-column"]
note     = "why"                   # optional; `baseline record` writes it
```

Any other key is a baseline error. An expiry is the last day the entry
accepts its finding.

A case adds, all optional:

```toml
[[baseline]]                       # one entry of the baseline to run with
path     = "pbip/Example.Report/definition/pages/0000/visuals/0001/visual.json"
context  = "slicer-selection"
column   = "enrollment.cohort_year"  # omitted for a null column
reasons  = ["warn-column"]
severity = "warn"
expires  = "2199-12-31"            # written as given, valid or not

[fingerprint]                      # the finding `{fingerprint}` in args names
path     = "..."
context  = "..."
column   = "..."
reasons  = ["..."]
```

and an `after/tree/` directory beside `tree/`. Between steps 2 and 3 of
"Running a case", when the case has `[[baseline]]` or `[fingerprint]`:

1. Scan the root with the case's policy and no baseline, with the
   implementation under test (in process or on the command line), and match
   each description to the findings by path, context, column and reasons. A
   description that matches no finding, or more than one, is an error in the
   case.
2. Write the `[[baseline]]` entries as a baseline in the implementation's own
   format and default location, each with its resolved fingerprint and its
   `severity` and `expires` as given. A case with no `[[baseline]]` has no
   baseline file.
3. Replace `{fingerprint}` in `args` with the resolved fingerprint.
4. Copy `after/tree/` over the root, replacing files.

`after/tree/` is how a case shows what happens when a finding changes after
it was accepted (a new reason, a new value of the same kind, a cleared
selection) without writing a fingerprint down. Setup trusts the
implementation's scan, but a scan that misses the finding fails the case at
step 1 instead of passing it.

`pbipx baseline record <fingerprint> --note <text>` records an acceptance;
the note is required, so every `record` case passes one and a refusal cannot
come from a missing argument. Refusal under `CI` or `PRE_COMMIT` exits 3 (an
operational refusal); refusal of a block or an unknown fingerprint exits 4.
`pbipx baseline verify` checks expiry. Cases for both have no
`expected.json`: their exit code and sentinels are checked.

An expiry is metadata, not a planted value. A case that needs an expired
entry uses a past year; the years 2100 to 2199 are for values.

## Gate cases with working copies

The gate denies every path under a registered working copy
([behavior.md](behavior.md), "Working copies"), and the registry is in the
store, which a gate case's runner owns. So a gate case can register some:

- **`working-copies`** is a list of folders, relative to `{work}` (clean,
  forward slashes, none inside another). Before the command runs, the runner
  makes each folder, empty, and registers it in the case's store, as `stage`
  will. A gate case without the key has no working copy and no `{work}`.
- **`{work}`** is the working-copy base, as in a stage or collect case: an
  existing, empty directory outside the root, in no git repository, with no
  symbolic link in its path. In a gate case it is valid in `stdin`, `args`
  and `env`, and only when `working-copies` is set. In `stdin` it is written
  with forward slashes (`C:/Users/...` on Windows), as a user of Bash
  there writes a path, and then as JSON string content, as `{root}` is. A
  backslash in a Bash command is an escape, so a command written with the
  native path would name another path (`C:\a\b` is `C:ab`), and the case
  would test nothing. The gate's own tests cover the native spellings.

A working copy holds nothing in a case, because the gate never opens a file
under one: whether a path exists changes nothing. A path through a glob
must therefore match the folder itself, since a pattern that matches
nothing names nothing. The cases are pbipx only, and set
`known = "predecessor"`.

## Stage and collect cases

`stage` and `collect` ([stage-collect.md](stage-collect.md)) copy a project
outside the repository, so a case needs a place outside the root for the
working copy, a way to build one, a way to play Desktop's save, and checks on
the files left behind. These areas exist only in pbipx: the predecessor has
no such commands, and is retiring ([ADR-0007](../adr/0007-the-desktop-round-trip.md),
amending ADR-0003).

- **`{work}`** in `args`, in `setup` and in `env` values is the **working-copy
  base**: an existing, empty directory outside the root, in no git repository,
  with no symbolic link in its path. A case names the working copy inside it
  (`--to {work}/copy`). `{work}` is valid only in a stage or collect case,
  or in a gate case with `working-copies` (above).
- **`setup`** is a list of command lines, each an array starting with `stage`
  or `collect`, run in the root after step 3 with the case's environment. Each
  must exit 0. A collect case uses it to stage first.
- **`after/tree/`**, in a stage or collect case, is laid over `{work}` after
  `setup`, replacing files: Desktop's save, or whatever the case needs to
  already exist there (a leftover file, a symbolic link). It is not applied
  to the root. **`work-deleted`** then removes paths, relative to `{work}`: a
  deletion in Desktop.
- **`status`**, when present, is the root's `git status --porcelain
  --untracked-files=all` lines after the command, as a set. `status = []`
  asserts a clean root. When absent, the status is not checked.
- **`expected/root/`** and **`expected/work/`**: every file under them must
  exist at the same path in the root, or in `{work}`, with the same bytes,
  checked after the command. Extra files are allowed. **`expect-root-absent`**
  and **`expect-work-absent`** list paths that must not exist.

A mismatch names the path and the rule, never the bytes. Fixtures keep
Desktop's serialization: no final newline in report JSON and LF, in
`after/tree/` and `expected/` as in `tree/`. A fixture holds only what its
case needs. `expected/root/` never holds a sentinel (the working copy may: a
refused collect leaves it intact).

Not representable as fixtures, so tested in Go when the commands exist: a file
name with a colon (a Windows alternate data stream appears under WSL as
`name:Zone.Identifier`, and such a name cannot be checked out on Windows), CRLF
input (`.gitattributes` normalizes fixtures to LF), and a repository whose
project changed after the stage (`source-changed`).

## pbipx's runner

pbipx's runner is in [internal/corpus](../../internal/corpus).
`TestCorpusValid` holds every case to this format in every `go test ./...`.
`TestCorpus` runs each case as a subtest named `<area>/<case>`, for the
areas listed in `enabledAreas` in its test file or in `PBIPX_CORPUS_AREAS`
(`pbir,text`, or `all`). It skips without git on `PATH`.

How it does each step, where this format leaves the choice open:

- The commit's author and committer are `Corpus Runner
  <corpus-runner@example.com>`, at 2100-01-01T00:00:00Z, on branch `main`.
  Every `GIT_` variable of the caller is removed, and user and system git
  configuration are turned off, for setup and for the command. A `staged`
  path is staged even if ignored.
- A case other than the gate that has an `expected.json` gets
  `--format json` appended to its `args`. A case does not set `--format`
  itself.
- `--format json` prints the `expected.json` shape, and may add fields (a
  finding's `locator` and `fingerprint`, for instance). The runner compares
  only what `expected.json` can say: the failure, or the findings as a
  multiset with `accepted` and `suppressed`. An expected finding's locator
  is compared, as JSON, only when it has one. For `explain`, every key of
  the expected object is compared, recursively.
- The gate's exit code is the decision: 0 is an allow, 2 a deny. The class
  of a deny is read from the first line of stderr. Only when that has no
  class is it read from `permissionDecisionReason` in the hook JSON on
  stdout.
- The baseline setup scan is `pbipx scan --all --no-baseline --format json`,
  run on a committed copy of the root, so that the root itself is committed
  only after `after/tree/` is applied. Each finding in its output carries
  `fingerprint`. The baseline is written as shown in "Baseline cases", with
  `column` left out for a null column.
- A sentinel counts as present in output as written, with each apostrophe
  doubled, or as the body of a JSON string that encodes either.
- The working-copy base is a fresh directory next to the case's configuration
  directory, with symbolic links in its path resolved. `setup` commands run
  with the same environment as the command. The commands `stage` and
  `collect` get `--format json` appended like a scan, and print `{"ok": true}`
  or `{"refusal": "<class>", ...}`; the runner compares `ok`, `refusal` and,
  for `findings`, the findings as a multiset. The areas `stage` and `collect`
  are not in `enabledAreas` until the commands exist.
- Every command a case runs, the baseline setup scan included, gets
  `PBIPX_CONFIG_DIR` set to a fresh temporary directory of the case's own,
  outside the root, so no case reads or writes the user's approvals. A case
  does not set it in `env`. The approval of "Policy approval" is written
  into that directory with internal/approval, the package
  `pbipx policy approve` uses, dated 2100-01-01T00:00:00Z.

The validator adds rules this format implies:

- An empty `sentinels` needs a description that says, in one clause, that
  the case plants no value ("plants no value", "nothing is planted", "no
  values").
- A sentinel appears in `tree/`, `after/tree/`, `stdin` or `args`, and
  never in `description` or `expected.json`. It counts as present with each
  apostrophe doubled (`''`, as a quoted literal stores it), and, in a
  `.tmdl` file, inside an Enter data blob: the base64 argument of
  `Binary.FromText`, raw-deflated or plain.
- `policy = "tree"` needs `tree/.pbipx.toml`, unless the expected failure is
  `policy-error`, or the case is a `gate` case with `approved = false` (a
  repository that has not adopted pbipx).
- `[[baseline]]` and `[fingerprint]` describe a finding of `tree/` with a
  context and reasons from the vocabulary; `{fingerprint}` is in `args` if
  and only if `[fingerprint]` is set; `after/tree/` needs one of them, or
  a `gate` case whose policy is approved.
- `approved` is set only in a `gate` case, and `env` never sets
  `PBIPX_CONFIG_DIR`.
- Report JSON (a `.json` or `.pbir` file below a `*.Report` directory, in
  `tree/` or `after/tree/`) parses, unless the case expects an error for it.
- Exit codes agree with the result: a failure or an error finding exits 3;
  a block finding, with no error finding, exits 1; an allow exits 0 and a
  deny 2.
- Each finding's path names a path in `tree/` or `after/tree/`, or
  `.pbipx.toml`, or is `-` in a `scan-text` case with a `stdin`. A `collect`
  finding's path is where the file would land in the repository, and is not
  checked against the trees.
- `{work}`, `setup`, `work-deleted`, `status` and the `expect-*` keys are set
  only in a stage or collect case. A `setup` line starts with `stage` or
  `collect`. `work-deleted` and the `expect-*` paths are clean relative
  paths, and a `status` line is a `git status --porcelain` line.
- A stage or collect result is `ok` (exit 0) or a refusal. A refusal other
  than `findings` exits 3; `findings` needs `findings` and exits as a scan's
  findings do. `expected/` holds only `root/` and `work/`, and report JSON in
  them follows the same rules as in `tree/`. A collect case that expects an
  error finding may hold report JSON that does not parse.
