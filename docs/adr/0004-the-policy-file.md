# ADR-0004: The policy file

- **Status:** Accepted (2026-10-29)
- **Date:** 2026-10-29

## Context

Everything specific to an adopter lives in its policy (principle 13):
protected roots, which files are scanned, block and warn columns, allowlisted
values, lasting acceptances, local state and allowlisted unscanned files. The
predecessor's policy grew one key at a time. Its scan scope is one large
regular expression spanning every file kind, which is hard to review and easy
to get subtly wrong.

## Decision

- The policy is `.pbipx.toml` at the repository root. The baseline is
  `.pbipx-baseline.toml`.
- It keeps the predecessor's concepts and their meaning, so an adopter
  converts once, by hand, in one reviewed change:
  - `protected_roots`
  - block and warn column lists, and a block-column pattern
  - `allowlist_literals`
  - lasting acceptances, as `allow_quasi`
  - local-state and unscanned-allow patterns
- Scan scope is declared **per file kind** (report, model, SQL, YAML, prose),
  each with its own globs, rather than as one regular expression.
- `pbipx init` writes a commented starting policy for a standard PBIP layout.
- An unknown key, a pattern that does not compile, or a protected root that
  no scan scope covers is a policy error. A policy error denies every gated
  call.

## Schema

```toml
version = 1
protected_roots = ["pbip"]          # required, non-empty, plain relative paths

[scan]                              # globs per file kind; at least one kind
report  = ["pbip/**/*.json"]
model   = ["pbip/**/*.tmdl"]
sql     = ["sql/**/*.sql"]
yaml    = ["models/**/*.yml"]
prose   = ["notes/**/*.md"]
exclude = ["**/.pbi/**"]            # removed from every kind

[files]
local_state     = ["**/.pbi/localSettings.json"]
unscanned_allow = ["pbip/**/StaticResources/**/*.png"]

[columns]
block         = ["full_name"]       # bare names, compared lowercased
block_pattern = "(^|_)email(_|$)"   # RE2, over the lowercased bare name
warn          = ["cohort_year"]

[shapes]                            # each defaults to true
person_name = true
email       = true
ssn         = true
date        = true
phone       = true

[values]
allow = ["(none selected)"]         # literals never judged

[[accept]]                          # a lasting acceptance of one warn column
column      = "enrollment.cohort_year"
path        = "pbip/Example.Report/definition"
note        = "why"
reviewed_by = "who"
```

Globs match repository-relative paths with forward slashes: `*` and `?`
within one segment, `[...]` a class, `**` any number of segments. Matching
is case-sensitive.

Policy errors: a missing file; invalid TOML; an unknown key at any level; a
`version` other than 1; a glob or pattern that does not compile; an empty or
malformed `protected_roots`; a protected root that no scan kind covers; an
`accept` entry missing a field, naming a column that is not `table.column` or
not a warn column, or with a path that is absolute, uses `.`, `..` or `\`, or
is shallower than three segments.

Mapping from the predecessor's keys, for the one hand conversion:

| Predecessor | pbipx |
|---|---|
| `protected_roots` | `protected_roots` |
| `scan_path_regex` | `[scan]` globs per kind, and `exclude` |
| `local_state_regex`, `unscanned_allow_regex` | `[files]` globs |
| `block_columns`, `block_column_regex`, `warn_columns` | `[columns]` |
| `heuristics` (`dob`) | `[shapes]` (`date`) |
| `allowlist_literals` | `[values] allow` |
| `allow_quasi` | `[[accept]]` |

## Settled questions

- **Globs or regular expressions for scope?** Globs, per file kind, with one
  `exclude` list in place of negative lookahead. The column pattern stays a
  regular expression, because it matches names, not paths. (2026-10-29)
- **Default column lists in the binary?** No. `pbipx init` writes them out
  in the policy, so everything that decides a verdict is in one reviewed
  file and nothing is hidden in the tool (principle 13). (2026-10-29)

## Open questions

- How does policy integrity (threat model, layer 6) bind to this file: a
  digest of the whole file, or of a normalized form?

## Alternatives considered

**Read the predecessor's file unchanged.** Cutover would be a binary swap,
but its warts would be frozen into a public format.

**A converter command.** There is exactly one existing adopter, so a reviewed
hand conversion costs less than a tool that is then maintained forever.
