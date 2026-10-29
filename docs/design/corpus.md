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
    expected.json                the normalized result
```

`<area>` groups cases: `pbir`, `tmdl`, `sql`, `yaml`, `text`, `baseline`,
`policy`, `gate`, `explain`. A case directory is named in kebab case for what
it shows (`slicer-on-block-column-blocks`).

## Running a case

A runner, one per implementation, does this for each case:

1. Copy `tree/` to a fresh temporary directory, the **root**.
2. Unless `policy = "tree"`, write `policies/<policy>.toml` to
   `<root>/.pbipx.toml`.
3. Unless `git = false`, make the root a git repository and commit every
   file, except those listed in `untracked` (left untracked) and `staged`
   (added to the index, not committed). The commit uses a fixed synthetic
   author and date.
4. Replace `{root}` in `args` and `stdin` with the root's absolute path.
5. Run the command in the root, with `CI`, `PRE_COMMIT` and
   `PBIPX_NO_REVEAL` unset and standard output not a terminal, unless `env`
   sets them.
6. Check, in order: the exit code; that no sentinel appears in stdout or
   stderr; the normalized result against `expected.json`.

An implementation's runner may translate the corpus policy into its own
format. The predecessor's runner does.

## case.toml

```toml
description = "slicer selection on a direct-identifier column blocks"
command     = "scan"                 # scan | scan-text | gate | explain | baseline
args        = ["--all"]
policy      = "default"              # policies/default.toml, or "tree" for tree/.pbipx.toml
exit        = 1

# Synthetic values planted in tree/ or stdin. None may appear in stdout or
# stderr. This is what proves redaction (principle 2): every case checks it.
sentinels = ["Doe, Jane"]

# Optional.
git        = true                    # false: the root is not a git repository
untracked  = []                      # paths left untracked
staged     = []                      # paths staged but not committed
env        = { CI = "true" }         # environment for the command
known      = "predecessor"           # see "Known differences"
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

- `findings` are sorted by path, context, column, then reasons.
- `path` is relative to the root, with forward slashes.
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
that implementation; pbipx writes it first in the deny reason, as
`pbipx: <class>:`.

For `explain`, the result is the structure as JSON. Its schema is settled
with the first `explain` cases (phase 3).

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
