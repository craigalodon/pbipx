# Predecessor behavior

What the predecessor distinguishes, written down so the corpus can specify it
without anyone reading the predecessor's code or tests (ADR-0002, ADR-0003).
This is a description of behavior, in this repository's words. It holds no
fixture, policy entry or value from any real deployment.

Where pbipx should behave differently, the difference is marked
**Divergence** and gets a corpus case that says which behavior is right. The
slugs in this document are settled in
[vocabulary.toml](../../testdata/corpus/vocabulary.toml); the divergences
were accepted on 2026-10-29.

## Judging one literal

Every scanner reduces a file to literals, each with the columns it is
associated with. One rule then decides, in this order:

1. An empty literal, or one exactly equal to an `allowlist_literals` entry,
   is not judged.
2. **Column verdict.** Each associated column is compared by its bare name
   (the column, not its table), lowercased. If any is in `block_columns` or
   matches `block_column_regex`, the column verdict is *block*. Otherwise, if
   any is in `warn_columns`, it is *warn*, and the first such column is the
   matched one.
3. **Shapes** are checked unless the literal is a number. A number has no
   name or email shape, and unassociated numbers are thresholds and flags.
4. The result:
   - block column: **block**, reasons `block-column` plus any shapes, matched
     column recorded;
   - any shape: **block**, the shapes as reasons, no matched column;
   - warn column: **warn**, reason `warn-column`, matched column recorded;
   - otherwise nothing.

Reasons are ordered: the column reason first, then shapes in the order
person name, email, SSN, date, phone.

A few literals are blocked without this rule, with a fixed reason: every role
member identity in a model (`role-member`).

## Shapes

Each shape can be switched off in policy (`person_name`, `email`, `ssn`,
`dob`, `phone`; all on by default).

- **Person name:** a literal that *starts* `Last, First`. `Last` is one or
  more letter runs joined by a hyphen or an apostrophe (straight or curly),
  then a comma, whitespace, and a letter. Both initial letters must be
  uppercase or titlecase in Unicode terms, so non-ASCII names count. Anchored
  at the start: `Totals, Region` inside a sentence is not checked this way
  (but see prose, below).
- **Email:** `local@domain.tld` anywhere in the literal.
- **SSN:** `ddd-dd-dddd` as a whole word, anywhere.
- **Date (of birth):** ISO `yyyy-mm-dd` or US `m/d/yyyy`, anywhere, with the
  year starting `19` or `20`.
- **Phone:** a North American number, `ddd-ddd-dddd` with `-`, `.` or space,
  an optional parenthesized area code and an optional `+1`, as whole words.

**Divergence (dates).** CONTRIBUTING.md asks for synthetic dates in the years
2100 to 2199, which the predecessor's date shape never matches, so a synthetic
fixture could not exercise it. pbipx's date shape accepts years
`19xx` to `21xx`. Cases that need the predecessor's exact behavior plant
2100s dates and expect the date shape from pbipx, and are marked as a known
difference.

## Reports (PBIR JSON)

Only **quoted string literals** are judged: PBIR writes a string as `'...'`,
with an embedded quote doubled, and writes booleans, nulls and typed numbers
(`0L`, `1D`) bare. The doubled quote is un-doubled before judging, or a name
with an apostrophe would escape the name shape.

A literal is any `{"Literal": {"Value": "<string>"}}` object, at any depth.
A column is any `{"Column": {"Property": "<name>", "Expression":
{"SourceRef": ...}}}`. The traversal does not enumerate filter shapes (`In`,
`Not`, `Comparison`, `Between`, `Contains`, `And`, `Or` ...); it collects
every literal and column inside a context, so a shape Power BI adds later is
not missed.

**Association is the whole context.** Every literal in a context is associated
with every column in it. That over-associates on purpose: it fails towards
blocking. A column's table comes from its `SourceRef.Entity`, or from its
`SourceRef.Source` alias resolved through every `From[]` (`Name` to `Entity`)
anywhere in the context. An alias that cannot be resolved still matches on
the bare column name.

### The contexts

Seven places in a report hold model values. Everything else, such as column
width metadata (which names columns), is not visited.

| Slug | Where | Notes |
|---|---|---|
| `filter` | `filterConfig.filters[i]` in a page or visual | the entry's own `field.Column` is added to the columns |
| `slicer-selection` | `visual.objects.<any>[i].properties.filter.filter` | any object class, not only `general` |
| `expansion-state` | `visual.expansionStates` | expanded matrix and decomposition-tree rows |
| `insight-selector` | `visual.objects.insights[i].selector` | |
| `conditional-format` | any `Conditional.Cases` under `visual.objects.<name>[i].properties.<prop>` | found at any depth |
| `title-text` | `visual.visualContainerObjects.title[i]` and `subTitle[i]`, `.properties.text` | no columns: shapes only |
| `bookmark` | a bookmark's `explorationState` | see below |

**Bookmarks** re-serialize the other contexts in their own shape. Each
`sections.<page>.visualContainers.<visual>` is its own context, so a literal
is not associated with the columns of unrelated visuals on the same page.
Everything else in the state (report and page filters) is one more context.
Each `cachedFilterDisplayItems[].label`, a bare string holding the display
text of a selection, is judged as a literal of its container.

The locator of a finding is the JSON path from the context to the literal
(`filterConfig.filters[0].filter.Where[0]...Literal.Value`).

### Neutral filter state

Reports should ship with no persisted selections. A single-select slicer on a
person column saves its first value without anyone clicking, so its saved
selection is a fixed sentinel that names no one, listed in
`allowlist_literals`. The corpus needs a case showing the sentinel passes and
any other value on the same column blocks.

## Models (TMDL)

### Parsing

The parser builds a tree of declarations from tab indentation. It fails
closed: anything it cannot place is an error, and errors name a line and a
structure, never source text.

- Line endings are normalized first.
- Errors: space indentation; an indentation jump of more than one level; a
  statement that does not start with an identifier; an unterminated quoted
  name; a missing name; text after a name that is not `= ...`; an
  unterminated ```` ``` ```` block; an expression the lexer rejects; an
  "Enter data" blob that is not base64 or cannot be decoded.
- `kind: value` is a property. `kind name = expr` and `kind = expr` carry an
  expression, which continues on lines at least two levels deeper than the
  declaration, or between ```` ``` ```` fences.
- `///` lines are a description of the next declaration. A trailing one with
  nothing after it is still judged as text.

### What is judged, and its context

| Slug | What |
|---|---|
| `tmdl-dax` | DAX: `measure`, `column`, `calculationItem` and `tablePermission` expressions; the DAX-valued properties (format string, detail rows, KPI target, status and trend expressions, field-parameter selection expressions); an unnamed `expression`; `calculated` partition sources |
| `tmdl-m` | M: named `expression`s, `sourceExpression`, `pollingExpression`, and `m` and `policyRange` partition sources |
| `tmdl-native-sql` | a string inside M that looks like SQL (`select ... from`), lexed as SQL |
| `tmdl-enter-data` | each cell of an "Enter data" table |
| `tmdl-property` | every `kind: value` property value, on shape |
| `tmdl-description` | every `///` description |
| `tmdl-comment` | every comment inside DAX, M or native SQL |
| `tmdl-text` | any other `=` body (annotations, extended properties, linguistic metadata): JSON strings one by one, or the whole text |
| `tmdl-role-member` | every role `member`; always blocks |

Locators are built from declaration kinds and names
(`table[student]/measure[Count]#lit2`). A role member's locator uses its
ordinal, never its identity.

### Association in code

Narrow, unlike reports, so one partition that selects `full_name` does not
make every string in its query a block. A literal (string or number) is
associated with columns only when:

1. it is an operand of a comparison (`=`, `==`, `<>`, `!=`, `<`, `>`, `<=`,
   `>=`), and the other operand is a column, an expression wrapping columns
   (`lower(email)`), or a chain of those joined by the language's
   concatenation operator (`&` in DAX and M, `||` in SQL);
2. it is the pattern of `LIKE`, `ILIKE`, `RLIKE` or `REGEXP`, with or without
   `NOT`;
3. it is in a list or tuple after `IN` or `NOT IN`;
4. it is an argument, or in a list argument, of a matching function. DAX:
   `CONTAINSSTRING`, `CONTAINSSTRINGEXACT`, `SEARCH`, `FIND`, `SWITCH`,
   `LOOKUPVALUE`, `CONTAINSROW`, `CONTAINS`, `TREATAS`, `EXACT`,
   `PATHCONTAINS`. M: `Text.Contains`, `Text.StartsWith`, `Text.EndsWith`,
   `List.Contains`, `List.ContainsAny`, `List.ContainsAll`, `Value.Equals`,
   `Comparer.Equals`. Case-insensitive. The literal is associated with the
   columns passed directly to the same call.

Tables written in code associate every cell with the table's columns:

- `DATATABLE` rows, with the header names declared in the same call;
- `{...}` constructors in a calculated table, with the table's columns;
- "Enter data" cells, with the field names of the expression's
  `type table [...]`, or else the table's columns.

A column in M without a table takes the table being declared.

Lexing:

- **DAX:** `"..."` is a string (`""` escapes). `'Table'` is a name, never a
  value. `[col]` is a column, and `Table[col]` or `'Table'[col]`, adjacent,
  gives its table. Comments are `//`, `--` and `/* */`.
- **M:** `"..."` is a string. `#"..."` is a name. `[field]` and `[#"a b"]` are
  column access; `[Name = "x"]` is a record, not a column. Comments are `//`
  and `/* */`.
- **SQL:** `'...'` is a string. A backticked run is a column. A bare
  identifier that is not a keyword and is not followed by `(` is a column; in
  `a.b.c`, `c` is the column and `b` its table. `"..."` is read both ways in
  SQL files (a string in some dialects, an identifier in others) and the
  associations are merged.
- Unterminated strings, names, column references and block comments are
  errors. Unknown punctuation is skipped.

"Enter data" blobs are `Binary.FromText("<base64>", ...)`, usually raw
deflate around JSON. Strings and numbers in the JSON are cells; booleans and
nulls are not. A blob that is not base64, or decodes to neither JSON nor
UTF-8 text, is an error.

### Free text inside code

A comment or description is judged whole, on shape. Because the name shape
anchors at the start, two more readings run:

- every quoted run (straight or curly quotes, to the closing quote or the end
  of the line) is judged on its own, for the name shape only;
- a comment inside SQL is lexed as SQL, best effort, so a commented-out
  filter still ties its literal to its column. If the comment does not lex,
  it is retried with prose apostrophes blanked, then line by line.

## SQL files

Contexts `sql` and `sql-comment`. Association as above. Templated SQL (Jinja
in dbt) is not rendered: its delimiters are blanked and the inside is lexed as
SQL, so a value cannot hide in a template block. A template comment is a
comment. An unterminated template comment, string or block comment is an
error.

## YAML files

Contexts `yaml`, `yaml-key`, `yaml-comment` and `yaml-sql`.

- Every key is judged on shape. Every scalar except nulls, booleans and empty
  strings is judged; integers and floats are numbers.
- A scalar is associated with its nearest mapping key, through sequences. So
  `full_name: X` ties `X` to `full_name`.
- dbt conventions: items of an `accepted_values` test's `values` are also tied
  to the column the test is on (from `columns[].name` or `column_name`).
  `where` and `expression` values are SQL. Unit-test `rows` are tied to
  nothing and judged on shape only, whether given as YAML, CSV or SQL.
- Comments are `#` at a line start or after whitespace, outside any scalar.
- Quoted runs inside a value are judged for the name shape.
- Anchors and aliases are walked once. Several documents in one file are
  scanned.
- A YAML error reports its kind and position only. A parser's message can
  quote the source.

## Prose (Markdown notes, commit messages)

Context `text`, one finding per line. There are no columns, so every finding
blocks and nothing warns.

- Email, SSN and phone shapes count anywhere in a line. The date shape is not
  checked.
- The name shape counts only where a name stands alone: the whole line after
  any list bullet, number, quote marker or heading; or a whole field of a
  pasted row, split at tabs, pipes, double quotes, backticks, curly quotes,
  and apostrophes that are not inside a word. The field must be
  `Word, Word` plus at most two more words. A doubled apostrophe is
  un-doubled first.
- Allowlisted literals are removed from a line before it is checked.
- For commit messages, lines starting with `#` are skipped, as git does.

## Files and the default deny

The policy classifies each repository-relative path:

- **Outside every protected root:** scanned if in scan scope, otherwise
  ignored.
- **Under a protected root,** in order: local state (never read, skipped by
  tree scans); scanned; allowlisted unscanned (images); otherwise
  **unvettable**, which is an error finding. A `.bim` (a model saved as TMSL)
  is unvettable with a hint to save as TMDL.

Errors are findings with reasons and no value:

| Reason | When |
|---|---|
| `unvettable` | under a root and no class covers it |
| `unreadable` | the file could not be read |
| `not-utf8` | not UTF-8 (a BOM is accepted) |
| `unparseable-json`, `unparseable-tmdl`, `unlexable-sql`, `unparseable-yaml` | the scanner for its kind rejected it |

Model, SQL, YAML and prose files are always strict: a parse error is an
error.

**Divergence (lenient JSON).** The predecessor skips an unparseable report
JSON file silently unless `--strict` is given (the gate always is). Principle
10 says a parse failure is never a clean file. pbipx always reports it as an
error.

## Accepting a warning

- **Policy (`allow_quasi`):** accepts a warning whose only reason is
  `warn-column`, whose matched column (`table.column`) equals the entry's
  column, under a path prefix at least three levels deep. An accepted
  warning is counted, not reported.
- **Baseline:** accepts a warning whose fingerprint is listed and has not
  expired. An unparseable expiry date counts as expired. A missing baseline
  file is empty. An entry whose severity is not `warn` is a baseline error.
  Default expiry when recording is 180 days.
- A block is never accepted by either.
- The **fingerprint** hashes a format version, the path, the context, the
  locator, the matched column and the sorted reasons. Never the value.
- Recording an acceptance is refused under `PRE_COMMIT` or `CI`, for a block,
  and for a fingerprint with no current finding (a usage error).
- `baseline verify` exits 2 when any entry has expired.

## The policy file

Keys: `protected_roots` (required, non-empty, plain relative paths),
`scan_path_regex` (required), `local_state_regex`, `unscanned_allow_regex`,
`block_columns`, `block_column_regex` (case-folded automatically),
`warn_columns`, `heuristics` (the five shape switches), `allowlist_literals`,
`allow_quasi` (entries with `column`, `path`, `note`, `reviewed_by`, all
required).

Policy errors: a missing file; invalid TOML; an unknown key at any level; a
pattern that does not compile; a protected root that the scan scope does not
cover for any scanned file kind; an `allow_quasi` entry that is malformed,
names a column that is not a warn column, or has a path that is absolute,
uses `.`, `..` or backslashes, or is shallower than three levels. An absent
optional pattern matches nothing.

ADR-0004 replaces the regular expressions with globs per file kind.

## Commands, output and exit codes

| Exit | Meaning |
|---|---|
| 0 | clean (warnings without `--strict` are clean) |
| 1 | a block |
| 2 | warnings under `--strict`; expired baseline entries on `verify` |
| 3 | an operational error: any error finding, a policy or baseline error, a git failure, a crash |
| 4 | a usage error |

Error findings take precedence over blocks.

pbipx adopts this table, so a broken invocation is never mistaken for
"warnings" (vocabulary.toml).

`scan` sources: `--staged` (the index, with no fallback to the working
tree); `--all`, the default, which covers every file git would commit (tracked,
plus untracked files that are not ignored) and every file on disk under a
protected root; `--rev <commit>` (that tree); or paths, with a directory
expanded to the files under it. Flags: `--strict`, `--format human|json`, `--show-values`,
`--show-shape`, `--policy`, `--baseline`, `--no-baseline`.

`scan-text` reads files, `-` for standard input, or `--commits <range>`;
`--commit-msg` skips comment lines. With nothing to scan it is a usage error.
It exits 1 on any finding.

Redaction:

- `--show-values` is refused (usage error) when stdout is not a terminal, or
  when `CI`, `PRE_COMMIT` or the tool's no-reveal variable is set, and always
  with `--format json`.
- `--show-shape` prints a silhouette: uppercase and titlecase letters `A`,
  lowercase `a`, digits `9`, spaces and punctuation kept, anything else `.`,
  runs written `A{3}`.

**Divergence (digest).** The predecessor prints each value's length and a
short hash of the value by default. Principle 3 forbids hashing a value, and
a length alone is part of the shape. pbipx prints neither unless
`--show-shape` is given.

**Divergence (error text).** The predecessor puts exception messages into
some error details and gate reasons. pbipx carries only structure, line
numbers and reason classes (principle 2).

## The gate (Claude Code)

The event is JSON on stdin. Every failure denies:

- the event does not parse, or is not an object;
- the working directory is not in a git repository;
- the policy fails to load. It is loaded before anything is classified, so a
  broken policy denies every call, not only calls near a protected root;
- any other error.

Decisions by tool. Every tool not listed is allowed.

- **Read** of `file_path`: allowed if it is empty, outside the repository, a
  directory, outside the protected roots and not in scan scope, or
  allowlisted. Denied if it is local state or unvettable, whether or not it
  exists. Allowed if it does not exist. Otherwise the file is scanned, strict,
  with the baseline: denied on a block or an error, allowed on warnings.
- **Grep** of `path` (default: the working directory): if the path is a
  protected root, inside one, or contains one, every file on disk under the
  protected roots is scanned, without following symlinks. Denied on any block
  or error, including an unvettable file anywhere there. Local state is
  skipped.
- **Bash** `command`, split into simple commands at `;`, `&&`, `||`, `|`,
  `|&`, `&`, parentheses and newlines, with redirection targets dropped. With
  unbalanced quotes it falls back to splitting on whitespace.
  1. The tool itself alone (one simple command, optionally after environment
     assignments, with no `$(` or backtick) is allowed: it redacts on its own.
  2. A git command that prints file content is **denied** outright (git
     history cannot be vetted), unless its pathspecs provably avoid every
     protected root:
     - always content: `blame`, `annotate`, `grep`, `archive`,
       `format-patch`, `difftool`, `range-diff`;
     - `diff` and `show` unless a summary flag is given (`--stat`,
       `--numstat`, `--shortstat`, `--name-only`, `--name-status`,
       `--dirstat`, `--compact-summary`, `--summary`, `--raw`, `--no-patch`,
       `-s`, `--quiet`), and a patch flag overrides the summary;
     - `log`, `reflog` and `whatchanged` only with a patch flag (`-p`, `-u`,
       `--patch` and kin, `--cc`, `--full-diff`, `-L`);
     - `stash show` with a patch flag; `cat-file` except `-t`, `-s`, `-e`,
       `--batch-check`;
     - git's global options `-C`, `-c`, `--git-dir`, `--work-tree` and
       `--namespace` consume their argument;
     - pathspecs avoid the roots when every root is excluded (`:!`, `:^`,
       `:(exclude)`), or when there is no glob and no positive pathspec
       touches a root. Pathspecs are those after `--`; for `show` and
       `cat-file`, `REV:path`, relative to the repository root; for `blame`,
       the last argument.
  3. If the working directory is inside a protected root, or the command
     names a root as a whole word, the protected tree is scanned as for Grep.
  4. A recursive search (`grep`, `egrep` and `fgrep` with `-r` or `-R`;
     `rg`, `ag` and `ack` always) over a path that touches a root scans the
     tree. Its paths are the positional arguments after the pattern (unless
     `-e` or `-f` gave it), default `.`.
  5. Otherwise allowed.

A deny writes the Claude Code decision JSON (`permissionDecision: deny` and a
reason) to stdout, writes the reason to stderr, and exits 2. An allow writes
nothing and exits 0. The reason names the file, the column and the reasons,
and never a value.

Gate reason classes: `finding`, `unvettable`, `scan-error`,
`local-state`, `git-content`, `policy-error`, `event-error`,
`not-a-repository`, `internal-error`.

## `explain`

- **Report JSON:** the number of contexts, and for each context its locator,
  the number of string literals and the sorted column names.
- **TMDL:** tables with columns (data type, calculated or not), measures and
  partitions (source type, mode, connectors used, the navigation path read,
  relations native SQL reads); relationships from and to; roles with the
  tables they filter and a member count, never the members; expressions as
  parameter or query; the model's default mode; counts of literals by
  context. A name with any shape prints as a placeholder.
- **SQL:** relations created, written and read (qualified, unqualified or
  CTE, and template `ref()`/`source()` calls), and literal counts by context.
  A name part is printed only if it is a plain identifier (letters, digits
  and `_`, not all digits) with no shape. Otherwise a placeholder, or a
  placeholder for a call whose arguments are not quoted names. `FROM` inside
  `EXTRACT`, `TRIM`, `SUBSTRING`, `SUBSTR` and `OVERLAY`, and after
  `IS [NOT] DISTINCT`, is not a relation.
- A file that does not parse prints a line saying so, with no detail.
