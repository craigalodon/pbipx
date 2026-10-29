# Design principles

These come from a predecessor implementation that ran in production on a
repository of Power BI projects holding education records. Each one either
held up there or was learned when its absence caused a failure. They are
written here generically. A change that breaks one needs a decision record.

## 1. Values are protected; names are schema

Column, table, measure and page names describe structure and may be printed,
discussed and committed. A **value** is a literal a model or report holds: a
filter selection, a table cell, a comparison operand, a role member. Values
are never printed, never committed into docs or messages, and never stored in
a baseline.

## 2. Redaction is built in, not a flag

The scanner touches every protected value, so its own output is the riskiest
text it produces. Hook output, in particular, is captured into an agent's
context.

- Each value is wrapped in a type that cannot render itself. Every
  formatting, marshalling and logging path prints a fixed placeholder. A
  single function, `Reveal`, returns plaintext.
- `--show-values` is the only caller of `Reveal`. It is **refused, with a
  usage error**, when stdout is not an interactive terminal or when `CI`,
  `PRE_COMMIT` or `PBIPX_NO_REVEAL` is set. It is refused rather than quietly
  downgraded, because a quiet wrong guess about the context leaks.
- Partial masking (`D***, J***`) is not offered: initials plus a known cohort
  identify a person. A `--show-shape` silhouette (`Aa{2}, Aa{3}`) gives the
  length and character classes and no letters.
- Error messages carry file, line and structure, never source text.

## 3. Never hash a value

A digest of a name looks opaque, but the preimage space is one roster: a few
hundred names that anyone with a class list can enumerate. Salting does not
help, because the salt has to be committed for the baseline to reproduce on
every clone. A value-hashing baseline would therefore be a committed,
reversible encoding of the data, in a file everyone assumes is safe.

Baseline fingerprints hash **location, column and reasons**. The gap this
leaves is narrow and acceptable. A changed value with the same location,
column and reasons stays suppressed, which is the case worth suppressing (a
term code rolling forward). A value that changes *kind* adds a reason,
changes the fingerprint, and fires again.

## 4. Two tiers, and a block is never baselined

- **Block**: a literal on a direct-identifier column (`full_name`, `email`,
  `student_id` ...), or any literal with a personal-data shape whatever its
  column (person name, email, SSN, phone, date of birth).
- **Warn**: a literal on a quasi-identifier column (cohort year, section
  number ...).

A warning may be accepted after review. An acceptance that should expire goes
in the baseline. One meant to last goes in policy: it names one column under
one report or model, with a note and a reviewer, and no value. It applies
only when that column is the finding's sole reason.

**A block can never be accepted.** If one is a false positive, the fix is a
policy change, which is a reviewable diff. Removing that shortcut removes the
whole class of "the baseline quietly hid a real leak".

## 5. Shapes catch what column lists miss

Column lists cover only the columns someone thought of. The shape heuristics
catch the rest: in the predecessor's design phase they caught a name on a
synthetic field that appeared in no list. Both run on every literal.

The person-name shape (`Last, First`) is anchored where a name stands alone,
such as a whole literal, line, list item or field, because searching for it
everywhere fires on every "Word, Word" in prose. The date shape applies to
code and data, not prose, where dates are everywhere and identify no one on
their own.

## 6. Noise is a defect

A control that fires constantly gets bypassed, and a bypassed control
protects nothing. Each rule is measured against a real corpus before it
ships, and the measurement is recorded.

- In report JSON, only quoted string literals are judged. Booleans, nulls and
  typed numbers are formatting flags and thresholds; in the predecessor,
  judging them produced 12 noise findings out of 13.
- In model code, a number is judged only when it is compared against an
  identifier column.
- Temporal dimensions (academic year, fiscal period) are not
  quasi-identifiers by default. Persisted selections on them are a review
  problem (principle 12), not a scanner one.

## 7. Fail closed, including the hook itself

In agent hook protocols, success usually means "no decision, proceed". So
every error path in the gate is a deny: a parse failure, an unreadable file, a
policy that fails to load, a missing git, a crash.

That includes the hook command itself. The predecessor's hook script was
committed without its executable bit, the shell exited 126, the agent runtime
read that as "proceed", and the gate never ran for its first day. Every unit
test passed, because each one called the gate's code directly. So:

- the configured hook command is written so that any failure below it becomes
  a deny;
- tests run the **configured command string** end to end, in both
  directions.

## 8. Default deny under a protected root

Each file under a protected root is classified by policy, in order:

| Class | Read | Recursive search or shell over the root |
|---|---|---|
| Local state (Desktop UI settings) | denied | skipped |
| Scanned | scanned first; denied on a block or an error | denied on a block or an error |
| Allowlisted (images under `StaticResources`) | allowed | skipped |
| Anything else: unvettable | **denied** | **denied while it exists on disk** |

Power BI Desktop's `.pbi/` folder is never scanned. `cache.abf` is a local
copy of the imported data, and `unappliedChanges.json` holds pending queries.
A scanner looking for filter literals would call either one clean.

**The deny comes before the capability.** A new file type is unreadable until
a scanner for it exists. The decision gets made in a reviewed change, not by
omission.

## 9. Association decides which column a literal belongs to

- **Report JSON:** a filter is one small context, so a literal is associated
  with every column in it.
- **Model code (DAX, M, embedded SQL):** a literal is associated only with
  the columns it is compared against, through an operator, an `IN` list or a
  matching function. One partition that merely selects `full_name` must not
  make every other string in the query a block.
- **Tables written in code** (`DATATABLE`, `{...}` constructors, decoded
  "Enter data" rows) associate every cell with the table's columns. Those
  cells are data by definition.
- Every role member is a block. Members belong in the service, not the
  repository.

## 10. A parse failure is an error, not a clean file

A file the parser cannot place has not been vetted. A construct the parser
does not know degrades to the free-text and shape heuristics, not to silence.
Syntax it cannot place at all is an error, and the gate denies on errors.

## 11. `explain` prints structure only

`explain` exists so an agent never needs to read a protected file to learn its
shape. It prints names, types, relationships, storage modes and sources. A
name that comes from a string literal, such as a source table, is printed only
if it is a plain identifier with no personal-data shape. Otherwise it prints a
placeholder.

## 12. Work with Desktop's serialization

- Normalize to LF with `.gitattributes`; Desktop writes CRLF, and git's clean
  filter converts it.
- Report JSON ships with no final newline. Whitespace and pretty-print hooks
  must exclude project folders, or the hooks and Desktop fight forever and
  someone eventually reaches for `--no-verify`, which skips the scan too.
- Desktop writes a data cache beside any model it opens. So `stage` refuses a
  working copy inside the repository, and `collect` brings back only what
  Desktop saved, minus `.pbi/`, saved DAX queries and Windows alternate data
  streams.
- Reports should ship with **neutral filter state**: no persisted
  selections. A persisted selection is presumed to be left over from a save,
  and keeping it needs a recorded reason. A single-select slicer on a person
  column saves its first value without anyone clicking. Its saved selection
  must be a sentinel that names no one.

## 13. Policy is configuration; the tool is generic

Protected roots, scanned paths, column lists, allowlists and acceptances live
in the adopting repository's policy file. The tool hardcodes no path and no
organization. A policy that fails to load or compile denies every gated call.
