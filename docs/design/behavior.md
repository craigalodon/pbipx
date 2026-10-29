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
| `tmdl-dax` | DAX: `measure`, `column`, `calculationItem`, `tablePermission` and `function` expressions; the DAX-valued properties (format string, detail rows, KPI target, status and trend expressions, field-parameter selection expressions); an unnamed `expression`; `calculated` partition sources |
| `tmdl-m` | M: named `expression`s, `sourceExpression`, `pollingExpression`, and `m` and `policyRange` partition sources |
| `tmdl-native-sql` | a string inside M that looks like SQL (`select ... from`), lexed as SQL; a legacy `query` partition's SQL (its `source`, or a `query` expression) |
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
  associations are merged. A dollar-quoted string (`$$...$$`,
  `$tag$...$tag$`) is a string. In SQL files, dialects that disagree are
  read every way (see "SQL files").
- Unterminated strings, names, column references and block comments are
  errors. Unknown punctuation is skipped.

**Limits.** Association is near-linear in the size of the code. What is left
is bounded, and code past a bound is an error (`unlexable-sql` in a SQL file
or a SQL value of YAML, `unparseable-tmdl` in a model), never association
cut short and called clean:

- one literal is associated with at most 256 distinct columns;
- associating an expression of `n` tokens may take at most
  `65536 + 64 * n` units of work (a token visited, a literal tied to a
  group of columns, a column found for a literal).

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

Dialects disagree on two things a value can hide behind, so a SQL file is
lexed under four readings, each with `"..."` read both ways:

| Reading | A quote in a string | `#` |
|---|---|---|
| first | doubled (`''`) only | an operator (PostgreSQL) |
| second | doubled, or after a backslash (`\'`: MySQL, BigQuery, Spark) | an operator |
| third | doubled only | starts a line comment (MySQL, BigQuery) |
| fourth | doubled, or after a backslash | starts a line comment |

The literals and comments judged are the union of every reading that lexes,
so `'O\'Doe' ... 'Doe, Jane'` cannot shift a name out of a literal, and the
text after a `#` is judged as a comment either way. The file is an error only
when no reading lexes. Under the backslash readings, `\'`, `\"` and `\\`
stand for the quote or the backslash, and any other pair is kept as written.
A reading is left out when the file has no backslash, or no `#`, for it to
differ on.

Dollar-quoted strings (`$$...$$`, `$tag$...$tag$`: PostgreSQL, Snowflake)
are strings in every reading, and an unterminated one is an error. Because a
dollar-quoted string is usually a function body, each reading is also run a
second time with the body lexed as SQL, its tags passed over, so a value in
it is a literal of its own. A `$` right after a name character, or before a
digit (`$1`), opens none.

## YAML files

Contexts `yaml`, `yaml-key`, `yaml-comment` and `yaml-sql`.

- Every key is judged on shape. Every scalar except nulls, booleans and empty
  strings is judged; integers and floats are numbers. A tag does not
  decide that: a tagged scalar is judged by its text.
- A scalar is associated with its nearest mapping key, through sequences. So
  `full_name: X` ties `X` to `full_name`.
- dbt conventions: items of an `accepted_values` test's `values` are also tied
  to the column the test is on (from `columns[].name` or `column_name`).
  `where` and `expression` values are SQL. Unit-test `rows` are tied to
  nothing and judged on shape only, whether given as YAML, CSV or SQL.
- Comments are `#` at a line start or after whitespace, outside any scalar.
- Quoted runs inside a value are judged for the name shape.
- An anchored node is walked where it is anchored and again at each alias,
  with the alias's association. Several documents in one file are scanned.
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
  **Divergence:** pbipx scans them and stops at the scissors line (see
  Clarifications, "Prose").

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
`--commit-msg` skips comment lines (pbipx: scans them, up to the scissors line;
see Clarifications). With nothing to scan it is a usage error.
It exits 1 on any finding. `--commits` in a shallow clone is refused with
exit 3 (Clarifications, "Prose").

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
  A `ref()` or `source()` call is a read only where it is code: inside a
  template block (`{{ }}`, `{% %}`) outside the template's strings,
  wherever the block stands, so `-- depends_on: {{ ref('x') }}` is a read;
  or in the SQL outside every comment and string. A call in comment prose or
  in a string is not a read.
  A name part is printed only if it is a plain identifier (letters, digits
  and `_`, not all digits) with no shape. Otherwise a placeholder, or a
  placeholder for a call whose arguments are not quoted names. `FROM` inside
  `EXTRACT`, `TRIM`, `SUBSTRING`, `SUBSTR` and `OVERLAY`, and after
  `IS [NOT] DISTINCT`, is not a relation.
- A file that does not parse prints a line saying so, with no detail.

## Clarifications

Questions the first corpus cases raised, answered from the predecessor's
behavior (2026-10-29).

### Verdicts

- When several associated columns are block columns, the matched column is
  the first in association order. In a report filter, that is the order the
  walk meets them, with the filter entry's own field column last.
- A column is reported as written in the file. Only matching is lowercased.
  A table or column name with a personal-data shape or a control character
  is reported as `{?}`, in every scanner (spaces and hyphens print). Such a
  name only decides a verdict when the policy lists or patterns it, so a
  printed name is schema the policy already names.
  **Divergence (shaped names):** the predecessor prints table and column
  names as written, treating every name as schema.
  (Found by the predecessor's corpus runner, 2026-10-29.)
- A shaped value on a warn column blocks with the shapes as its only reasons
  and no matched column. The warn reason does not join them.
- Each literal that fires is one finding. Two findings with the same path,
  context, column and reasons are both reported. Overlapping readings of the
  same text (a comment judged whole, its quoted runs, and its SQL) are not
  merged either.

### Reports

- A quoted string of digits (`'2105'`) is a string: column verdict and
  shapes both apply.
- Typed literals other than strings, such as `datetime'...'`, are not
  judged by the predecessor. **Divergence (typed dates):** pbipx judges a
  `datetime'...'` literal by its column only. The date shape is not applied,
  because every typed date has it and every date filter would block.
  (Decided 2026-10-29.)
- `cachedFilterDisplayItems[].label` is judged wherever it appears in a
  bookmark's state, against the context it sits in.
- `allow` values match exactly, case included.
- **Divergence (hierarchy levels):** the predecessor associates only
  `Column` references, so a selection on a hierarchy level is tied only to
  the context's other columns. pbipx also associates a `HierarchyLevel` as a
  column, named after its level, of the hierarchy's table. A level is
  usually named after the column it shows. (Decided 2026-10-29.)

### Models

- `DATATABLE` header columns, `{...}` cells and "Enter data" fields take the
  table being declared: `table.column`.
- A bare column in native SQL stays bare; it does not take the M table.
- An M string that looks like SQL is judged as an M literal and also lexed
  as native SQL.
- A comment inside native SQL has context `tmdl-comment`.
- Comment and description markers (`//`, `--`, `/* */`, `///`) and the
  whitespace around the text are removed before it is judged, so the name
  shape can anchor.
- A file that does not parse produces one error finding and nothing else.
- **Divergence (matching functions):** pbipx ties no number in `SEARCH`
  and `FIND` (a start position and a not-found value), ties only `SWITCH`'s
  match values (not its results or else value), and takes `SWITCH`'s
  columns from its first argument only. The predecessor ties every literal
  argument, so `SEARCH("x", [full_name], 1, 0)` blocked on `1` and `0`. A
  result string with a shape still blocks on the shape (principle 6;
  2026-10-29).
- A bare flag (`isHidden`) is a valid declaration.
- A DAX user-defined function (`function name = (x) => ...`) is DAX: its
  body's literals are associated as in a measure, in context `tmdl-dax`.
- A legacy `query` partition's SQL, the `source` of a partition declared
  `= query` or a `query` expression under its `source`, is read as SQL in
  a SQL file is (`"..."` both ways, the associations merged), in context
  `tmdl-native-sql`. A bare column stays bare. Its comments are
  `tmdl-comment` and are also lexed as SQL. SQL that does not lex is
  `unparseable-tmdl`.
- A table or column name in a finding's column is printed only when it is
  an identifier with no personal-data shape, and otherwise as `{?}`
  (`{?}.full_name`), as in SQL and YAML. The verdict is decided on the
  name as written.

### SQL and YAML

- In `s.full_name`, the recorded table is `s`, even when `s` is an alias.
- Literals found by lexing a comment as SQL have the comment's context.
- The readings of a SQL file (see "SQL files") keep locators stable. The
  literals and comments of the first reading that lexes are numbered in
  order, as they always were; one found only by a later reading is
  numbered after them, in reading order. A literal at the same offset with
  the same value under two readings is one literal, with the columns of
  both. A file that every reading lexes alike has the locators it would
  have with one reading.
- A file's literal count in `explain` counts the union of the readings.
- `explain` reads relations from the first reading that lexes.
- A literal past the association limits (see "Association in code") is not
  judged on fewer columns: the whole file is an error.
- Unit-test rows have context `yaml`, including rows written as SQL. CSV
  rows are judged cell by cell.
- An anchored node is judged where it is anchored, and again at each alias
  with the alias site's key and column association: `student_id: *id` ties
  the anchored value to `student_id`, an aliased `values` list is tied to
  the accepted_values column, and an aliased string under `where` is lexed
  as SQL. A judgement the new association does not change (a key's shape, a
  quoted run, a value with the same columns) is not repeated. At one alias
  site, an alias inside the walked node to a node that site has already
  walked is not followed, so cycles end. A file whose aliases walk more than
  100,000 nodes in all is one `unparseable-yaml` error at the alias that
  crossed the cap. (This replaces "an anchored node is judged once".)
- An explicit tag is not trusted. A tagged scalar (`!!null`, `!!bool`,
  `!!int`, a custom `!tag`) is judged by its text: as a string, column
  verdict and shapes, unless its text read as a plain scalar is a number.
  Only plain, untagged nulls and booleans, and empty strings, are skipped.
  `!!str` is a string.
- A quote, or a `|` or `>` block indicator, opens a scalar only where a node
  can start: after a key's `:`, a sequence entry's `-` or an explicit key's
  `?`, a document marker, at a flow collection's start or after its commas,
  or first on a line that does not continue a plain scalar. On a plain
  scalar's continuation line it is text, and a `#` after it is still a
  comment. The comments the parser attaches to nodes are judged too, merged
  with the scanner's by text, so a mistake in either cannot by itself hide a
  comment.

### Prose

- The "name plus at most two words" limit applies to the whole line as well
  as to fields. The extra words need not be capitalized.
- `--commit-msg` is a flag. `scan-text` scans every path it is given,
  whatever the prose scope says. Findings from standard input have the path
  `-`. An argument that names no regular file in the repository is reported
  as `argument N`, never by what was typed.
- An allowlisted literal is removed from a line only where no letter or
  digit is next to it, so a short allowed word cannot cut down a name.
- **Divergence (commit-message comments):** with `--commit-msg`, pbipx
  scans every line, comment lines included. `git commit -m` keeps a line
  starting with `#`, and a hook cannot tell which comment lines git's
  cleanup will strip. Scanning stops at git's scissors line (the comment
  character, a space, then `------------------------ >8 ------------------------`),
  which git discards with everything below it. The comment character is
  read from `core.commentString` or `core.commentChar`; `auto`, or no
  setting, is `#`. (Decided by the user, 2026-10-29.)
- `--commits` needs the repository's full history. In a shallow clone git
  lists only the commits it has, so a range would pass scanned in part. A
  shallow repository, or a listed commit whose parent is missing, is an
  operational error (exit 3) with a fixed message that says to fetch the
  full history, and never echoes the range. A CI job that runs it checks out
  with `fetch-depth: 0`.

### Gate

- The repository and the policy are checked before the tool is looked at,
  so `not-a-repository` and `policy-error` deny every tool.
- When a scan finds both a block and an error, the gate reports `finding`.
  (A scan's exit code reports the error: 3.)
- A shell command that reads local state (`cat .pbi/localSettings.json`) is
  allowed by the predecessor when the rest of the tree is clean, because tree
  scans skip local state, while a Read of the same file is denied.
  **Divergence (local state in shell):** pbipx denies a Bash command that
  names a local-state path or directory, with `local-state`. Tree scans
  still skip local state, so a recursive search that does not name it is
  decided as before. (Decided 2026-10-29.)

### Baseline

- Recording an acceptance under `CI` or `PRE_COMMIT` is an operational
  refusal (exit 3). Recording a block or an unknown finding is a usage error
  (exit 4).

## Known predecessor bugs

Found by the predecessor's corpus runner (2026-10-29). Each case is marked
`known = "predecessor"`. pbipx handles them; the predecessor misses them
until cutover replaces it. This list is the record of them; there is no
separate issue tracker.

| Case | What the predecessor misses |
|---|---|
| `tmdl/dax-function-body-blocks` | a DAX user-defined function body is judged as free text, without its columns |
| `tmdl/query-partition-sql-comparison-blocks` | a legacy `query` partition's SQL is judged as free text, not lexed |
| `sql/backslash-escaped-quote-keeps-literals-apart` | backslash-escaped quotes shift which text is read as a literal |
| `sql/hash-comment-is-a-comment` | a `#` comment is lexed as code |
| `sql/dollar-quoted-value-on-block-column-blocks` | a `$$` string is lexed as code |
| `yaml/alias-takes-site-association` | an alias does not take the key at the alias site |
| `yaml/tagged-scalar-judged-by-text` | tagged nulls and booleans are skipped; tagged numbers get no shape checks |
