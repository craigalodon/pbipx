# The behavior corpus

pbipx's behavior is specified by a corpus of cases that does not depend on any
language: input files, a command, and the expected output. Two
implementations agree when both pass it. That is how this rewrite reaches
parity with its predecessor ([ADR-0003](../adr/0003-parity-through-a-language-neutral-corpus.md)),
and it stays as the regression suite after the predecessor retires.

> **Status: draft.** The format is settled when phase 1 of the
> [roadmap](../ROADMAP.md) has run the predecessor against it.

## Layout

```text
testdata/corpus/
  policies/<name>.toml           shared policies, referenced by name
  <area>/<case>/
    case.toml                    what to run, and what must never appear
    tree/...                     the repository the command runs in
    stdin                        optional: the gate's hook event, as JSON
    expected.json                the normalized result
```

`<area>` groups cases: `pbir`, `tmdl`, `sql`, `yaml`, `text`, `gate`,
`explain`, `baseline`, `policy`.

## case.toml

```toml
description = "slicer selection on a direct-identifier column blocks"
command     = "scan"                 # scan | scan-text | gate | explain
args        = ["--all"]
policy      = "default"              # policies/default.toml, or "tree" for tree/.pbipx.toml
exit        = 1

# Synthetic values planted in tree/. None may appear in stdout or stderr.
# This is what proves redaction (principle 2): every case checks it.
sentinels = ["Doe, Jane"]
```

## expected.json

The normalized result, never raw output. Text formatting can differ between
implementations; findings cannot.

```json
{
  "findings": [
    {
      "path": "Example.Report/definition/pages/0000/visuals/0001/visual.json",
      "context": "slicer-selection",
      "column": "student.full_name",
      "tier": "block",
      "reasons": ["block-column", "person-name-shape"]
    }
  ],
  "errors": []
}
```

- Findings are sorted by path, context, column, then reasons.
- `context` and `reasons` come from a closed vocabulary, defined in
  `testdata/corpus/vocabulary.toml` once phase 1 settles it.
- For `gate`, the result is `{"decision": "allow" | "deny", "reason": "<class>"}`.
  The reason class is something like `finding`, `unvettable`, `scan-error`,
  `policy-error` or `tree-unclean`, never free text.
- For `explain`, the result is the structure as JSON.

A value never appears in `expected.json`. The tool never prints one, so a
normalized result has nowhere to put it.

## Rules for cases

- Every value is synthetic, from the table in
  [CONTRIBUTING.md](../../CONTRIBUTING.md#synthetic-data). Every planted value
  is listed in `sentinels`.
- Every case asserts the exit code as well as the result.
- A case is added for every bug fixed, before the fix.
- A noise measurement (principle 6) is recorded as a case that must stay
  clean, with the reason in its description.
- Report fixtures keep Desktop's serialization: no final newline.
