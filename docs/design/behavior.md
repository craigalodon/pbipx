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
| `hooks-disabled` | a Claude Code settings file that can turn hooks off (below) |

`pbipx scan`, from every source, checks each Claude Code settings file in
the files it scans (`.claude/settings.json` or `.claude/settings.local.json`
in any directory), whatever the policy's scan scope: one that is not a JSON
object, that sets `disableAllHooks` to anything but `false`, or whose `env`
is not an object or sets one of `PBIPX_CONFIG_DIR`, `CLAUDE_CONFIG_DIR`,
`PATH`, `BASH_ENV`, `ENV` or `SHELLOPTS` (names compared without regard to
case), is an error finding with `hooks-disabled`. It names the path and the
reason only. Such a file turns off the gate's user-level hook, or defeats
it (threat model, layer 5): `PBIPX_CONFIG_DIR` points the gate at an
approval store that approves anything, `CLAUDE_CONFIG_DIR` moves the
settings the hook is in, `PATH` changes the programs the hook's shell runs,
`BASH_ENV` and `ENV` run a script in it, and `SHELLOPTS` sets its options.
`PBIPX_NO_REVEAL` is allowed: it only takes a power away. The
gate's own scans do not check it; the gate denies an agent's write to it
instead.

Model, SQL, YAML and prose files are always strict: a parse error is an
error.

**Divergence (Performance Analyzer exports).** A Power BI Performance
Analyzer export is a JSON file of query text and visual titles, which can
hold values, and no scanner vets it. Classification alone fails it twice:
outside every root and scan scope it is ignored, so an agent can read it;
under a root it is in the report scanner's glob, which judges quoted
literals one at a time and may call a query string clean. So the gate
recognizes an export **by its shape** and denies a Read of it with
`perf-export`, whatever the class (principle 8: the deny comes before the
capability; `pbipx perf` is the summary that prints counts and durations
only, [perf.md](perf.md)). (Decided 2026-11-04.) The rule:

- It applies to a Read (and an Edit, which shows the file) of a regular
  file inside an adopted repository, whatever its name or class. A file
  outside the repository is not the gate's to judge, as before; a file the
  existing rules already deny keeps its reason. The name is not looked at,
  since a rename must not defeat it; the cost is bounded instead. The check
  opens the file, reads at most its first 64 KiB, and stops at the first
  byte that is not `{` (after a byte order mark and white space), so a file
  that is not a JSON object costs one small read.
- The shape is one JSON object with a top-level `version`, then, after it, an
  `events` list whose first element is an object with a `name` and a
  `metrics` or a `parentId`. Common keys (`id`, `start`, `end`) do not
  count, since calendars and logs have them. A file whose first line is a
  whole object with more after it (lines of values) is not one object. The
  field names are from memory of the export's general shape and are **to be
  confirmed against a real export, schema only**; the check is in one place
  (`internal/gate/perfexport.go`). A UTF-8 or UTF-16 byte order mark is
  accepted.
- It must not over-block ordinary JSON (principle 6). Anything it cannot
  tell falls back to the existing rules, which allow outside the roots: a
  file that cannot be opened, is empty, is truncated or unparseable in the
  head, has its events before its version, an empty or non-object event
  list, a first event without a name and a distinctive key, or a shape not
  decided in the first 64 KiB. So `package.json`, `tsconfig.json`, a lock
  file, a log, and a calendar's `events` list (a `name`, `id`, `start` and
  `end` per event) stay readable. The cost of the fall-back is that an export
  that puts its `events` key past 64 KiB of other keys, or before its
  `version`, is not caught.
- **Budget.** A command that names files is checked within a budget per
  decision: at most 512 files opened and 16 MiB read (each file counts at
  most its 64 KiB head). Past it the gate cannot say the rest are not exports,
  so it denies with `scan-error` and a fixed message that names the limits,
  not a path: `cat many/*.json` over thousands of files is refused, and
  naming fewer files is the way through.
- **Bash and PowerShell.** Agents on Linux and macOS read through Bash, so
  the same shape check, and the same deny, apply to a file a reading or
  searching command is given (`cat`, `head`, `jq`, `sed`, `grep` and the
  rest of the readers table, and any program taken to read its operands),
  and to the target of an input redirection (`wc -c < trace.json`): once the
  gate can place the word, against the working directory the line leaves it
  in, as it places any word, a regular file inside the repository is
  checked. A word it cannot place keeps today's rules. A path is checked once
  per decision. In PowerShell, each word that looks like a path and names a
  regular file in the repository is checked.
- **Tree scans.** The scan that a Grep, a recursive search or a shell command
  runs over the protected roots applies the check to each file the scanner
  reads, on the bytes it read, so an export under a root denies with
  `perf-export` where it would have been judged as a clean report file. It is
  not another pass. So an export under a protected root makes **every
  command that scans the tree deny**, naming the export: any PowerShell
  command, `grep -r x .` from the repository's top, a Grep over a root. That
  is intended: the export is where the scan would read it, and it is removed
  from the roots (or summarized with `pbipx perf`) to clear them.
- **Not covered.** A recursive search whose paths hold no protected root
  (`grep -r x analysis`, a Grep of a directory outside the roots) is not a
  tree scan and reads files nothing here names, so it stays as today: an
  export it reads is not caught. Nor is a command that finds its file at run
  time (`find . -name '*.json' -exec cat {} +`, `xargs`, a glob the gate
  cannot expand), a copy outside the repository, a reader of a
  here-document, git printing an export from the index or history
  (`git show :analysis/trace.json`, for a file outside the roots; under a
  root the git rules already deny), and an interpreter that opens the file
  itself (`python -c`, `node -e`). Each is a limitation of the gate's reading
  of a shell line,
  not of this rule. A future `pbipx perf <file>` alone is allowed by the
  tool-alone rule before this check; the change that builds it must keep a
  pipeline of it from tripping the check on its own argument.

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

**Divergence (adoption).** The predecessor ran from a repository's own
hook settings, so it saw only calls made in that repository. pbipx
registers the gate in the user's settings, where it runs for every session
on the machine, in every directory. So, first in order, pbipx decides which
repositories a call is the gate's to judge
([ADR-0006](../adr/0006-policy-integrity.md), amendment of 2026-10-30):

- A git repository has **adopted** pbipx when its work tree has a
  `.pbipx.toml` (any file of that name, loadable or not); or its clone
  (keyed by `git rev-parse --git-common-dir`, as approvals are) has a
  record in the approval store, written by `pbipx init` or
  `pbipx policy approve`; or its work tree is in the store's index of
  adopted work trees. A record that cannot be read counts. The record and
  the index live outside the repository, so once pbipx is set up there,
  deleting or breaking `.pbipx.toml` cannot switch the gate off: the call
  is denied with `policy-error`.
- A call is judged against **each adopted repository it touches**, as if
  made there (that repository's policy, approval, baseline and roots),
  with the event's working directory as it is. The paths it touches are:
  the working directory; a Read's or a Write's `file_path`; a Grep's
  `path` (default: the working directory); an LSP call's paths; for Bash,
  every working directory a command runs in, every word placed against it
  as the shell rule places words (globs, `--flag=value`, `cd` and all), the
  paths a recursive search covers, and those `find`, `fd` and `xargs` read
  under; and for PowerShell (pbipx only), the working directory and
  everything below it, and every word that looks like a path (below).
  Each path, as given and with its symlinks resolved, counts for the
  innermost adopted work tree that holds it: of the directories above it
  that hold a `.git` entry (walking up), and the indexed work trees it is
  inside. So an adopted work tree nested in another, such as a linked work
  tree under `.claude/worktrees/`, is judged alone, as before adoption;
  and a repository that has not adopted pbipx, nested in one that has,
  leaves its paths to the one around it. An indexed work tree below a
  Grep's path or a Bash word counts too, since those can reach under it;
  the working directory alone does not reach below itself. Every work
  tree of an indexed clone counts as indexed, as `git worktree list`
  reports them, read without running git: each linked one (the common git
  directory's `worktrees/*/gitdir` files), and the main one (the parent of
  a non-bare common directory, `<main>/.git`, or `core.worktree` in the
  common config). So `grep -rn X ..` from a clone reaches a sibling made by
  `git worktree add ../wt`, and a record that roots only a linked work
  tree reaches the main one (`grep -r X sub` from a parent of both). When
  the call
  touches something that cannot be placed (the shell rule's "touches
  every root": a working directory that cannot be told, a reading
  command's word that cannot be placed, a line built
  at run time), every indexed work tree counts. The repositories that hold
  the working directory are judged first, innermost first; any deny wins.
- A call that touches **no adopted repository** is allowed: in a
  repository that has not adopted pbipx, and outside every repository. One
  exception holds everywhere: a call that changes what controls the gate
  (a Write into the approval store or to a user-level or managed Claude
  Code settings file, a Bash command naming one or writing into a
  user-level settings directory, a PowerShell command naming one or such a
  directory, or one running a person-only subcommand of the tool,
  `pbipx policy approve` or `unenroll`) is denied with `agent-settings`,
  since from any directory it would switch the gate off, or forge an
  approval, for every adopted repository.
  A word naming `PBIPX_CONFIG_DIR` or `CLAUDE_CONFIG_DIR` (`$VAR`,
  `${VAR}`, or `$env:VAR` in PowerShell) is judged as the directory it
  stands for, since the agent's shell expands it: the gate's own value where
  set, and otherwise the directory the variable defaults to (the store, or
  `~/.claude`). So `rm -rf $PBIPX_CONFIG_DIR/approved` denies like the
  literal path, and a plain read of it stays allowed.
- **A registered working copy is denied before any of this**
  ([ADR-0007](../adr/0007-the-desktop-round-trip.md); "Working copies",
  below). A call that touches a path under one is denied with
  `working-copy`, adopted repository or not, from any working directory.
- An adopted repository that git cannot open as a work tree (its `.git`
  removed, say, while its work tree stays indexed), or a working directory
  inside one that git cannot place (inside the git directory, or gone),
  denies with `not-a-repository`. An index that cannot be read denies
  every call with `internal-error`. With no store to be found, a
  repository is adopted by its `.pbipx.toml` alone.

The index is the work trees each record was written from (`roots` in the
record; see the record format below). `pbipx init` writes a record with no
digest, so the gate acts for the repository from then on, before its
policy is approved. An indexed work tree that no longer exists is skipped;
one that is now a different repository with no record and no policy is
still adopted while it is indexed, so removing a work tree from the gate's
reach is a person's act: `pbipx policy unenroll` for a whole clone (below),
or a person editing the store by hand for a single index entry.

The event is JSON on stdin. Every failure denies:

- the event does not parse, or is not an object;
- in an adopted repository, the working directory is not in a work tree
  (adoption, above);
- the policy of an adopted repository the call touches fails to load. It
  is loaded before anything is classified, so a broken policy denies every
  call there, not only calls near a protected root;
- any other error;
- the decision takes longer than 120 seconds: a watchdog started when the
  gate begins deciding writes the deny (`internal-error`) and exits 2
  whatever the decision is doing, well before the hook's own timeout,
  past which Claude Code lets the call proceed.

**Divergence (policy integrity).** In pbipx, the first check after the
policy loads, before the baseline or the tool is looked at, is its approval
([ADR-0006](../adr/0006-policy-integrity.md)). The SHA-256 digest of
`.pbipx.toml`'s exact bytes must be one a person recorded with
`pbipx policy approve` for this clone on this machine. With no approval,
or a policy changed since (a comment or whitespace edit included), every call
is denied with `policy-unapproved`. An approval store that cannot be found
or read denies with `internal-error`, and so does one whose directory, or
a directory above it, is a file: that store is broken, not empty, on every
platform (Windows reports a path through a file as missing). A policy
that fails to load still denies with `policy-error`, since that check runs
first. The predecessor has no integrity check; the corpus cases for it are
marked `known = "predecessor"`.

`pbipx policy approve` is refused with a usage error (exit 4) unless stdin
and stdout are terminals and none of `CI`, `PRE_COMMIT` and
`PBIPX_NO_REVEAL` is set (set but empty counts). It loads the policy first
(a policy error exits 3 and approves nothing), shows `.pbipx.toml` and its
digest, and records the approval only when `approve` is typed in full;
anything else exits 4. `pbipx policy status` prints `approved`,
`unapproved`, `changed` or `missing` (no policy file), and exits 0 only
when approved, 3 otherwise. Neither prints policy content.

`pbipx policy unenroll` removes this clone's adoption, for every one of its
work trees, and is a person's step as approval is: refused with a usage
error (exit 4) unless stdin and stdout are terminals and none of `CI`,
`PRE_COMMIT` and `PBIPX_NO_REVEAL` is set (set but empty counts). It does
not load the policy, so a policy that is missing or broken does not block
it. In order (ADR-0006, amendment of 2026-11-04):

- A store inside the repository or the clone exits 3, as for `approve`.
- A `.pbipx.toml` in this work tree exits 3 and removes nothing: the file
  would adopt the repository again, and the gate would deny every call
  with `policy-unapproved`. Remove or move it first.
- With nothing to remove (no record of the clone, no legacy record of a
  work tree, no other record naming one) and the work tree not adopted, it
  says so and exits 0.
- Otherwise it prints the store directory, the clone's work trees (as
  `git worktree list` reports them, read without running git), the number
  of approved digests, a warning for each linked work tree that still holds
  a policy file (which adopts it again whatever is removed; a warning, not
  a block), and that the gate stops protecting these files from agents and
  every approval is forgotten. It asks for `unenroll` typed in full;
  anything else exits 4 and removes nothing.
- On confirmation it strips these work trees from the `roots` of every
  other record, comparing across symlink, case and short-name spellings
  (and by the file system's own identity where the directory exists). A
  record left with no roots and no digests is deleted, since a version 3
  record with neither is invalid and an invalid record counts as adopted. A
  record left with digests and no roots is kept: it is another clone's, it
  adopts that clone by its key and no work tree it no longer names. It then
  removes the `version = 1` records keyed by these work trees, and last the
  clone's own record, whatever it holds: a record of the clone that cannot
  be read is removed too. Other unreadable records are left, since whose
  they are cannot be told. A work tree that git can no longer open (`.git`
  removed, still indexed) has its root stripped like any other. An error at
  any step exits 3 with the clone's record still in place, so the
  repository remains adopted.
- It then asks the gate's own adoption check (the one `Decide` uses) about
  each work tree of the clone, this one first. A linked work tree on
  another branch that holds a `.pbipx.toml` is adopted again by its file:
  exit 3, naming that tree and saying the record is gone. Paths are
  printed with a segment that holds a control character shown as `{?}`.
  The word must be followed by a newline: end of input does not confirm,
  and neither do spaces around it (`policy approve` is the same). All
  adopted: not adopted: it prints `pbipx: this clone is no longer
  set up for the gate on this machine. The hook stays registered for other
  repositories. pbipx init or policy approve sets it up again.` and exits
  0. Still adopted, by a policy file or by an enclosing adopted repository
  (the gate judges a nested repository's paths against the one around it):
  exit 3, naming the reason.

There is no tombstone. A `.pbipx.toml` that arrives later (a pull, a
checkout of another branch) adopts the repository again with no approval,
and the gate denies with `policy-unapproved` until a person approves it.
No output carries policy content. A `PBIPX_CONFIG_DIR` that differs from the
one the gate runs with cannot be detected; the store directory shown is the
check a person makes.

The approval is kept outside the repository, in
`${PBIPX_CONFIG_DIR:-<user config dir>/pbipx}/approved/<key>`. The key is
the hex SHA-256 of the absolute, cleaned, symlink-resolved path of the
clone's shared git directory (`git rev-parse --git-common-dir`), so every
work tree of one clone, Claude Code's `.claude/worktrees/<name>` included,
shares one record, and a second clone needs its own approval (ADR-0006,
amendment of 2026-10-30). The record is TOML holding `version = 3`,
`digests` (the approved `"sha256:<hex>"` digests, oldest first, at most
16; none in a record `pbipx init` wrote before any approval), `roots` (the
absolute, clean work-tree paths the record was written from, oldest first,
at most 64: the index of adopted work trees) and `approved` (the UTC time
of the last approval, absent before one), written atomically
(a temporary file renamed into place) with mode 0600 in directories created
0700. `policy approve` adds the policy's digest, or moves it to the end,
and drops the oldest past 16; it and `init` add the work tree to `roots`.
A policy is approved when its digest is in the set. A `version = 2`
record (digests, no `roots`) is still read; it adopts its clone and adds
no work tree to the index until the next approval rewrites it. A
`version = 1` record (one `digest`, keyed by the work tree's
path) is still read for that work tree, and the next approval there merges
it into the clone's record and removes it. A relative `PBIPX_CONFIG_DIR` is
refused: resolved against the working directory, it could point into the
repository. So is a configuration directory inside the repository root, the
clone's git directory, or the main work tree that git directory belongs to,
as given or with symlinks resolved (of the root, and of the directory's
deepest existing ancestor): the gate denies every call with
`internal-error`, and `policy approve` and `policy status` exit 3. `scan`
does not check approval.

### Working copies

**Divergence (pbipx only).** A working copy is a folder `pbipx stage`
makes outside every repository for Power BI Desktop to open
([stage-collect.md](stage-collect.md)). Desktop writes a data cache there
(`.pbi/cache.abf`) holding a copy of real data that no scanner vets, so
the gate denies **every path under a registered working copy, with no
scanning**, from any working directory, in a repository that adopted pbipx
or not (principle 8; ADR-0007). The deny comes first: before the policy of
any repository is loaded, so a working copy nested in a repository (a
person ran `git init` there) is still a working copy, not whatever its
repository's policy would say.

*The registry.* Working copies are registered in the approval store,
outside every repository, beside the approval records:
`${PBIPX_CONFIG_DIR:-<user config dir>/pbipx}/stages/<key>.json`, one file
per working copy. The key is the hex SHA-256 of the working copy's
absolute, symlink-resolved path. An entry is JSON holding `version = 1`
and `path` (absolute and clean, symlinks resolved as far as the folder
exists), and optionally `git-dir` (the git directory of the clone it was
staged from, absolute), `model` (the model's repository path) and
`staged` (a UTC time): paths and a date, never policy content or anything
read from a file. An entry with an unknown key or version, a relative or
unclean path, or invalid JSON is not valid. Entries are written as
approval records are: through a temporary file renamed into place, mode
0600, in directories created 0700. Adding an entry for a path that has one
replaces it; removing a path that has none is not an error; a file in the
directory whose name is not a hex SHA-256 and `.json` is not an entry.
`stage` adds an entry and `collect --done` or `--discard` removes it
(`Store.AddWorkingCopy` and `Store.RemoveWorkingCopy`). Adding refuses a
path that cannot be a copy: a file system root, the home folder or a
parent of it, a folder that holds a `.git` entry, and a folder with a
`.git` entry in a folder above it (a folder inside a git work tree), all
judged with symlinks resolved; and it refuses an entry past 64. Nothing
else writes the registry, and the gate denies an agent's write into the store
(`agent-settings`, above), the registry included.

*What counts as touching.* The same paths the adoption finder places for
an adopted repository, with the same placement: the working directory; a
Read's, Edit's or Write's path; a Grep's path (default: the working
directory); an LSP call's paths; for Bash, every working directory a
command runs in (a `cd` into one included), every word placed against it
(globs, `..`, `~`, `$HOME`, `${HOME}`, spellings of Git Bash and of case on
a file system that folds it, symlinks as given and resolved), every
redirection target, and the paths a recursive search, `find`, `fd` and
`xargs` read under; for PowerShell, every word that looks like a path. A
path **at or under** a working copy denies, for every program. A path
**above** one denies when the call reads under it: a Grep or LSP path, a
search or `find` that goes under it, and a Bash word of a program that
could read under it. So `grep -r X ~`, `rg X` from the folder above,
`cp -r ~/work x` (a source) and `tar czf x -C ~ .` deny, and so does a
program the gate does not know. A word naming a folder above a copy does
not deny only when the program and the word's position are **proven** to
read nothing under it. This is a short allowlist, and anything the gate
does not fully understand (an unknown program or option, a word it cannot
tell) is not quiet. It is not the `neverReadsOperands` table, which answers
another question (is an operand the gate cannot tell read as a file); the
allowlist is its own, much smaller one (`wcQuietPrograms`):

- Programs that print their arguments, test or name a path, change the
  shell's state, or make, remove or change the mode of a file without
  opening what is below: `echo`, `printf`, `true`, `false`, `:`, `test`,
  `[`, `[[`, `basename`, `dirname`, `realpath`, `readlink`, `which`,
  `whereis`, `type`, `cd`, `pushd`, `popd`, `chdir`, `export`, `unset`,
  `set`, `declare`, `typeset`, `local`, `readonly`, `alias`, `unalias`,
  `mkdir`, `rmdir`, `touch`, `rm`, `chmod`, `chown`, `chgrp`, `mkfifo`,
  `unlink`, and the listers (`ls`, `dir`, `vdir`, `stat`, `du`, `df`,
  `tree`; `find` that only tests names), whose listing is documented
  below. `mv`, `ln`, `file`, `cat` and the rest are not here.
- The destination of `cp`, `mv`, `ln`, `install`, `rsync` and `tee`, by its
  position among the words and never by its text. The options are read as
  the program reads them: a cluster (`-rt`, `-vrt`, `-nt`), an attached
  value (`-tDIR`, `--target-directory=DIR`) and a unique prefix of a long
  option (`--target`, `--target-dir`). A command with an option the gate
  does not know, an ambiguous prefix, or a word it cannot tell (which could
  be an option) has no quiet word at all. An option that moves or reads a
  path beside the destination is one the tables leave out on purpose, so
  the command has no quiet word: `mv --exchange` (it swaps the operands, so
  every one is a source; `--exch` too), `cp -b`, `--backup` and
  `--remove-destination`, and rsync's `--link-dest`, `--compare-dest`,
  `--copy-dest` and `--backup-dir`. `mv --backup` and `rsync --backup` stay
  in: they set aside only a file the command replaces. The cost is
  `cp -b f ~/`, which denies while a copy is registered. A **source** is
  never quiet: `cp` and `rsync` read it, and `mv` and `ln` of a folder above
  a copy move the copy out of its registered path (the entry is then
  stale, and a stale entry is skipped) or make a path that reads through to
  it (`ln -s ~ L` and a read of `L` in the same call). A source of `mv` or
  `ln` that cannot be told counts as touching every working copy.
- The directory `tar -C` or `--directory` names, only when tar is proven to
  extract: the one operation is `x`, as a leading letter bundle (`xzf`,
  with the values of `f` and `C` following in order), `-x`, `--extract` or
  `--get`. Any other operation (`-c`, `-r`, `-u`, `-A`, `-t`, `-d`,
  `--delete`) anywhere, an option the gate does not know, or a word that
  could be one makes tar a reader. `-fx.tar` names an archive; it is not
  `x`. And `unzip -d` and `wget -P`, the directory they unpack or download
  into.
- `git` and `hub`, only for a read-only subcommand that opens no file
  below the folder, with every word known and no `-c` of an unsafe key,
  `--git-dir`, `--work-tree` or alias: `status` (not `-v`), `log` and
  `show` with no patch (`show` needs `--no-patch`, `-s`, `--stat` or the
  like; `-p`, `-u`, `-c`, `-L`, `--patch`, `--no-index` and any
  abbreviation of one deny), `branch` and `tag` that only list, `remote`
  without a changing subcommand, `rev-parse`, `rev-list`, `describe`,
  `for-each-ref`, `show-ref`, `merge-base`, `name-rev`, `count-objects`,
  `var`, `version`, `check-ref-format`, `symbolic-ref` that reads,
  `reflog` and `worktree list`. Every other subcommand denies with
  `working-copy` when git's effective directory (`-C`, applied in turn, or
  the working directory) or a path word it names is at or above a copy:
  `grep` and `diff --no-index`, `add`, `hash-object`, `archive` and `clone`
  read files below the folder, and a relative path is placed against the
  `-C` directory, not the working directory. `git status` in a repository
  that holds no copy, with no word above one, is unaffected.
- `pbipx`, by subcommand: `version` and `help` read no operand and are
  quiet. `scan`, `scan-text`, `explain`, `init`, `policy` and `baseline`
  read a folder or the repository around the working directory, so their
  words and the working directory above a copy are not quiet.
- `code`, `cursor`, `open` and `explorer`, only given operands and no
  option, every word known: `code --diff`, `--goto`, `--merge`, `code -`,
  `open -a`, `-f` and `-t` can put content before the agent or run
  something and are not quiet. `xdg-open` runs whatever handler the system
  has and is not in the allowlist, so `xdg-open ~` denies above a copy; so
  does `code --diff ~ x`, which is a small cost.

So `echo ~`, `export X=$HOME`, `cp f ~/`, `mv f ~/`, `unzip a.zip -d ~`,
`tar xzf a.tgz -C ~` and `git -C ~ status` are allowed, and `cp -rt d ~`,
`git -C ~ grep --no-index X .`, `mv ~ elsewhere` and `tar -fx.tar -c -C ~ .`
deny. The cost is a call whose word is above a copy and cannot be told,
such as `cp "$f" ~/`, which denies while a copy is registered. Moving or
deleting a registered folder by a means outside the call (a person, another
program) leaves a stale entry that is skipped; denying the agent's `mv` and
`ln` of a folder above a copy closes the agent's way to it, and nothing
further is done about a copy a person moves. `rm` of a folder above a copy
removes the copy and reads nothing, and stays allowed. A path attached to
a short option (`make -C<dir>`, `cc -I<dir>/x`, `sort -o<file>`) is
placed for working copies, which the adoption finder does not do for
adopted repositories; a glob attached to one is not expanded. A call that
touches something that cannot be placed (the finder's "touches every
indexed work tree": a reading command's word with a `$` or `` ` `` the gate
cannot expand, a working directory it cannot tell, a line built at run
time) counts as touching every registered working copy. A Bash or
PowerShell line that spells a working copy's absolute path anywhere as a
whole word (a string handed to an interpreter, say) denies too, a git
message included; the words of a git message are not placed as paths.
Names are compared without regard to case on a file system that folds it
(macOS, Windows), and exactly elsewhere.

*Fed programs.* A program a runner feeds (`xargs`, `parallel`, the other
runners of the feed table, under `env` or `sudo` too) gets words the gate
cannot see: they arrive on the runner's input. So `echo ~ | xargs cp -rt OUT`,
`echo ~ | xargs mv -t OUT`, `ln -st`, `install -t`, `parallel cp -rt OUT` and
`echo ~ | xargs cat` read or move a copy with no word of the line naming it
(`xargs -I{} cp -r {} OUT` already denied: the placeholder is a word the
gate cannot tell). With a live copy registered, a fed program **touches every
copy, unless the pipeline's producer is provably confined**: it prints only
names the gate can place, and none of them is, holds or lies inside a copy.
The producer is the first command of the pipeline, read through the
filters between it and the runner:

- `git` `ls-files`, `diff`, `diff-index`, `diff-files` and `ls-tree` with
  `--name-only` or `--name-status`, and `status --porcelain` or `--short`,
  with every word known, no `-c` of an unsafe key, `--git-dir`,
  `--work-tree`, `--format`, `--pretty`, `--output` or `--no-index`: confined
  when git's directory (`-C` applied) and each path word are not at, under
  or above a copy (the placement `gitSpots` gives a git command that is not
  quiet).
- `find P...` and `fd`, with no action (`-exec`, `-printf`, `-ls`,
  `-fprint`), no link followed (`-L`, `-H`, `-follow`) and, for `fd`, only
  options that select what it lists: confined when each path it walks (the
  working directory by default) is not at, under or above a copy. `ls`,
  with no option but the flags that print one name per line (`-1aAdFprtSUvX`
  and the like, not `-R` or `-l`): the same, and the runner's working
  directory too, for `ls` prints names relative to the folder it lists.
- `echo` and `printf` with every word known and no glob: the text they
  print is cut as the runner cuts it (blanks for `xargs`, lines for
  `parallel`, NUL for `-0`, and at blanks too) and each piece is placed as a
  path. A quote or a backslash in the text, which `xargs` reads, is not
  confined, nor is a piece that starts with `-` or `@`: to the program fed
  it is an option (`echo -T LL | xargs tar -cO`) or an `@file`, not a name.
- `rg -l` (`--files-with-matches`) and `rg --files`, and `grep -rl`
  (`-r -l`, `-lr`, `--recursive --files-with-matches`), by an option table
  alone: `rg` with `-i -F -w -S -l -0 -g -t -e` and their long forms and
  `--hidden`, `grep` with `-r` (not `-R`) `-l -i -F -E -w -e` and
  `--include=`, `--exclude=`, `--exclude-dir=`. An option not in the table
  (`rg -L`, `--follow`, `--pre`, `-z`, `--search-zip`, `--json`; `grep -R`,
  `-L`, `-f`), a missing `-l` (or `grep -r`), or a word the gate cannot tell
  is not confined. The first operand is the pattern unless `-e` or `--files`;
  the rest are paths, placed as `find`'s are, the working directory by
  default.
- The filters `sort`, `uniq`, `head`, `tail` and `grep` pass lines whole, and
  keep the confinement only with the options in a short table and no file
  operand (`grep PATTERN`, `grep -e P`, `grep -f L` read the pipe; `grep -o`,
  `-r`, `-l`, `-c`, `-n`, `head -c`, `sort L` do not). `cut`, `sed` and `awk`
  are not filters here: they can make a path from the pieces of one, and
  `cut -d/ -f1-3` of an absolute path names a folder above it. A redirection
  on a stage, a group or subshell or substitution in the line (the flat list
  of commands cannot tell its stages from a pipeline's) or a producer the
  gate does not know is not confined.
- Anything else, no pipe at all, a file on the input (`xargs cat < L`,
  `xargs -a L`, `parallel -a L`, `::::`), a here-document or here-string, an
  input cut some other way (`-d` other than newline or NUL, `--colsep`,
  `--pipe`), a replacement string that is part of a word (`{}/..`, parallel's
  `{.}` and `{//}`), or a runner whose options the gate cannot read, is not
  confined.

A runner whose program only prints its arguments gets no feed spot, whatever
the input: `xargs` with no program, `xargs echo` and `xargs printf` (also
under `env`). `git log --oneline | head | xargs echo` and `cat files.txt |
xargs` are allowed; the words and redirections of the line are placed as
usual, so `xargs echo < COPY/x` still denies. A runner whose options the gate
cannot read, and `parallel` (even `parallel echo`: its options are not read
apart from the program), is still a feed.

And `cp`, `mv`, `ln`, `install`, `rsync` and `tee` have no quiet word when a
runner feeds them, or when `find` or `fd -exec` runs them: their words are
not all in the line. So `git ls-files | xargs gofmt -l`, `find src -name
'*.go' | xargs wc -l`, `printf 'a\n' | xargs echo` and `ls | head` stay
allowed with a copy registered elsewhere in the home folder. The costs:
`git ls-files | cut -c4- | xargs ...` and the same with `sed` or `awk` deny,
as does `xargs cp -t ~ f` (no quiet destination) while a copy is registered,
and any fed program from a pipeline the gate cannot read.

*Name lists.* A program that reads the names it works on from a file (or
from the input) gets words the gate cannot see, as a fed program does, and
**touches every copy** without any condition on the list: `xargs -a L`,
`--arg-file`, `parallel -a L` and `::::`, `xargs cat < L`, `tar -T L` and
`--files-from` (`--verbatim-files-from`, `-T` in any cluster, `bsdtar` and
`gtar` too), `--files0-from` of `wc`, `sort` and `du`, `rsync --files-from`,
`cpio` and `pax` (they read names from the input), `zip -@` and the `@file` of
`7z` and `rar`. Long options are matched by prefix (three characters or
more). `grep -f L` reads patterns, not names, and is not one, and **writing**
a list is not either: `find ~ -name '*.abf' -print0 > L` is a lister, and
stays allowed (the listing's names are schema). The cost is a `tar -T` with a
list that is harmless, such as `tar cf x -T files.txt`, which denies while a
copy is registered.

*Symbolic links.* The text of a symbolic link is read when the link is
followed, against the folder the link is in, not the working directory of the
`ln` that made it: `ln -s a/stage1 $H/L` from elsewhere makes `$H/L` a path
into `$H/a/stage1`, and `cat $H/L/.pbi/cache.abf` names no copy. For `ln -s`
(`--symbolic` and its abbreviations, `-s` in a cluster), each source's text is
placed against each destination's folder and against the destination itself
(under `-t`, or when it is an existing directory), as well as against the
working directory, cleaned first (`copy/.` and `copy/.//.` name `copy`; a
word that only ends in `/.` is a path, not the `.` builtin as a program). An
absolute text is placed once. A source or destination
that cannot be told, or options the gate does not understand (any two words
may then be a source and a destination), count as touching every copy. The
cost is `ln -s work ~/L` and the like, whose text holds a copy once placed
against the home folder, which deny; a link to a sibling of a copy is
allowed.

*Listing.* **A listing with a working copy, or a path in it, as an
operand denies** (`ls`, `stat`, `du`, `tree`, `find` that only tests
names). Principle 1 allows listing a protected root, since names are
schema, but ADR-0007 denies a path under a working copy, and nothing a
person needs from an agent depends on a listing: the person works in the
folder in Desktop, and `collect` reports what it brought back by path and
count. **A recursive lister of a directory above a working copy is
allowed**, and what it prints is not nothing: `ls -R ~`, `du -a ~`,
`tree ~` and `find ~ -printf '%p %s'` show the names of the files in the
working copy and their sizes, and so can `ls -l`'s sizes and times. This
is a deliberate reading of principle 1 (names are schema, a size is
metadata), not an oversight, and the maintainer can overturn it by
treating a lister's word as reaching what is below it. A `cd` to a folder
above a copy is allowed too, as are PowerShell calls whose working
directory is above one.

*Registry errors.* A registry that cannot be read (an entry that does not
parse or cannot be read, a `stages` that is a file) denies every call with
`internal-error`, as a broken store does: a working copy the gate cannot
see is one it cannot protect. So does a registry with more than 256
entries (Add stops at 64, so only a hand edit gets there). The message
says what to do and names no path: for an entry, the entry's file name (a
hash, which names nothing) and "remove that file", and that
`pbipx collect --discard` will do it once it exists. An entry whose folder
no longer exists is skipped; a registry of only such entries, or with none,
changes nothing. An entry the store would refuse to add (the home folder,
say), written by hand, still protects what it names. The registry is read
for every call the gate decides.

*The cache by name.* A stale entry is skipped, and everything above leans
on the registry finding the copy: a copy a person (or a call that slipped
through) moved reads at a path no entry names. As a backstop, **while any
entry exists, stale or not**, a Read, a Grep path, or a word of a program not
known to leave its words unread (the quiet allowlist's complement) that
names a path ending in `/.pbi/cache.abf` denies with `working-copy`, in any
repository or none, whether or not the path is a registered copy ("the call
reads a Power BI Desktop data cache (.pbi/cache.abf), ..."). The path is
placed as any is (`..`, `~`, symlinks); a Write, `echo`, `test`, `ls` and
`rm` of it are not reads. This is the same file the local-state rule keeps
unread inside a protected root, now by name outside one. The cost is a
person's own PBIP project outside every repository: while a working copy is
registered, the agent cannot read that project's `.pbi/cache.abf` either,
which it could not usefully do anyway. With no entry at all the rule is off.

*The message.* The class is `working-copy` (pbipx only). The message is
fixed, and prints a path only as `<working copy>/<path inside it>` with
each segment displayed as a finding displays a name (a segment with the
shape of personal data, or a control character, is `{?}`), never where the
working copy is and never a file's content: "`<working copy>/<path>` is in
a Power BI Desktop working copy, which holds a copy of real data that
cannot be vetted.", "`<working copy>` is a Power BI Desktop working copy,
..." for the folder itself, or "the call could reach a Power BI Desktop
working copy, ..." for a path above one, a word that cannot be placed or a
line that spells one. The hint ends it: "Leave it out of the call: a
person works there in Power BI Desktop."

*Limits (a program text that builds a path at run time).* Besides the
interpreters named below, the gate does not read these, and they pass:
`awk '{system("cat " $0)}' L` and `sed 's/^/cat /e' L` over a list of names;
`rsync` or `scp` of `host:path` to the local machine; a name in a producer's
tree that holds whitespace, which a default `xargs` cuts into pieces that
resolve against another folder, a name in that tree that starts with `-` or
`@`, which the fed program takes as an option, and a symlink in that tree, for
`find P | xargs` without `-0`; a Grep or `grep -r` over a folder above the
`.pbi` of a copy whose entry is stale (the name backstop catches a path
that ends in
`.pbi/cache.abf`, not a search that finds it); a sourced script (`source f`,
`. f`), which can `cd` into a copy or set `CDPATH` unseen, the same as any
script the gate is not shown; and a variable name built from expansions
(`CDP${E}ATH`, `CDPAT{H,}`). A further cost: a link whose text goes up to
a folder that holds a copy, such as `ln -s .. sub/up` in a repository
directly under a home folder that holds one, denies. And these denials,
which an honest line can meet: with an approved adopted repository or a
copy registered, an input redirection from a word the gate cannot tell
denies, even outside the repository (`python3 x.py < "$IN"`), and so do
`~+`, `~-`, `~N` and `~user` at a word start; a line with `$'` or `$"` and
a later bare-name `cd` denies. A deny whose line holds a `cd` judged from the
folder before it as well (one not directly followed by `&&`) ends with a fixed
sentence saying that a `cd` joined with `&&` is judged from its target alone;
it holds no source text. Not placed against the folder a program is told to
work in, for want of a small change: the operands of `env -C DIR cmd` and of
`tar -C DIR` (`env -C work/other cat ../stage1/x`), which are placed against
the working directory; the `-C` word itself is placed, so a folder at or above
a copy denies.

*Not covered.* The gate cannot see a path an interpreter builds at run
time inside a script it is not shown, a glob attached to a short option
(`-I<dir>/*`), nor a PowerShell variable that is not
in a word that looks like a path; as for every control here, a process
outside the gate is the sandbox's to stop (threat model, layer 7). Without
adoption configured (`Options.Adoption` nil, as in a library caller) the
gate has no store and the registry is not consulted.

The gate reads `.pbipx.toml` once. It parses those bytes and checks the
digest of the same bytes against the approval, so a policy swapped between
two reads cannot be enforced while a different one is checked. `policy
approve` likewise records the digest of the bytes it loaded.

Decisions by tool. Every tool not listed is allowed. `pbipx init`
registers the hook for exactly the gated tools, as a matcher of exact
names: `Read|Grep|Bash|PowerShell|LSP|Write|Edit|MultiEdit|NotebookEdit`.

| Tool | Gated by | Judged as |
|---|---|---|
| Read | `file_path` | Read, below |
| Grep | `path` (default: the working directory); `glob`, `output_mode` and the rest narrow nothing the gate judges | Grep, below |
| Bash | `command` | Bash, below |
| PowerShell (pbipx only) | `command`, required | the protected tree, below |
| LSP (pbipx only) | every string field named like a path | Read of each path, or the protected tree, below |
| Write | `file_path` | Write, below |
| Edit, MultiEdit | `file_path` | Read of the file, then Write, below |
| NotebookEdit | `notebook_path`, else `file_path`; one is required | Read of the file, then Write, below |
| Glob | not gated | prints names only |
| any other | not gated | allowed |

A PowerShell call with no `command` string, or a NotebookEdit with no
path, is denied with `event-error`: the call cannot be judged.

- **Read** of `file_path` (in pbipx, a leading `~` expanded from the gate's
  home directory, and the file judged where the operating system opens it,
  symlinks before `..`): allowed if it is empty, outside the repository, a
  directory, outside the protected roots and not in scan scope, or
  allowlisted. Denied if it is local state or unvettable, whether or not it
  exists. Allowed if it does not exist. Denied with `perf-export` if the
  file, anywhere in the repository, has the shape of a Performance
  Analyzer export (above). Otherwise the file is scanned, strict, with the
  baseline: denied on a block or an error, allowed on warnings.
- **Grep** of `path` (default: the working directory; in pbipx a leading
  `~` expanded): if the path is a
  protected root, inside one, or contains one, every file on disk under the
  protected roots is scanned, without following symlinks. Denied on any block
  or error, including an unvettable file anywhere there. Local state is
  skipped.
- **Bash** `command`, split into simple commands at `;`, `&&`, `||`, `|`,
  `|&`, `&`, parentheses and newlines, with redirection targets dropped. With
  unbalanced quotes it falls back to splitting on whitespace. In pbipx the
  line is read further, as a shell reads it (see Clarifications, "Gate",
  divergence on reading a command line):
  - words are dequoted (`pb""ip` is `pbip`); `$'...'` is decoded as bash
    decodes it and `$"..."` read as `"..."`;
  - reserved words and grouping tokens (`if then elif else fi do done while
    until for in case esac ! { } time function`, and a `name()` definition)
    are skipped before a command's program, for every rule, git's included;
    the words of a `for`, `select` or `case` list run nothing;
  - `cd X` and `pushd X` with a literal target move the working directory
    for the commands after them (a subshell's scope is not kept: the move
    carries forward). A `cd` can fail (a mistyped folder name), so unless
    it is directly followed by `&&` (what follows then runs only if it
    worked), the working directory for the commands after it is the old one
    and the new one: `cd nosuch; cat work/stage1/x` is judged in both. After
    `cd x && a`, the commands after the chain (`; b`) are judged in the old
    directory too, and after `cd a || cd b` in any of the three. A `cd`
    on the right of a pipe runs in a subshell (`true | cd w && cat f`), and one
    after `||` is skipped when the left side succeeds (`test -d w || cd /tmp
    && a`), so it is widened even when chained with `&&`, and a subshell's
    `cd` (`(cd x) && a`) does not carry out of it: `a` is judged in the old
    directory too. `cd x && a` itself, where the `cd` has nothing `||`
    before it, is judged in the new directory only. The cost is a line like `cd
    sql; rg x` in an adopted repository, whose `rg` is also judged from the
    folder it started in. `cd` to a target
    that cannot be told (`cd $X`,
    `cd -` with no earlier `cd`, `popd`, a bare name while `CDPATH` is set
    in the gate's environment or may be set by the line itself (the name
    appears anywhere but as `$CDPATH` or `${CDPATH}`, quotes and backslashes
    removed first, so `CDP""ATH` counts, and a line with `$'` or `$"` is
    not fixed: `export`, `declare`,
    `read`, `printf -v`, a prefix assignment, `env`; or the line holds an
    `eval`, which can set it by a name built at run time; a sourced file is
    a script the gate is not shown, and is not judged; the cost is that a
    bare or quoted mention of the word on a line with a later bare-name `cd`
    denies while a copy is registered (`cd tools && git commit -m "fix the
    CDPATH docs"`); a path that
    starts with `./` or `../` is still told), `cd` with `HOME` unknown,
    a target with an unquoted glob character such as `cd pb*`, which the
    shell expands to a directory the gate cannot tell: also after
    `builtin`, `command`, `cd -P`, `pushd`, `chdir` and in a subshell) makes
    it unknown;
  - `~`, `$HOME`, `${HOME}` (the gate process's home directory), `$PWD`,
    `${PWD}` (the working directory as the line leaves it) and `$OLDPWD`
    are expanded; any other expansion (`$X`, `${X:-y}`, `$1`, `$(...)`,
    backticks) leaves its word unknown. An unquoted tilde prefix other than
    a plain `~` (`~+`, `~-`, `~N`, `~+N`, `~-N`, which bash takes from `PWD`,
    `OLDPWD` and the directory stack, and `~user`), at the start of a word or
    after `=` or `:` in an assignment, where bash expands it, marks the line
    uncertain, the fail-closed state below: it is denied with
    `internal-error` in an adopted repository and as touching every working
    copy. `a~+`, `~+x`, `--opt=~+/x`, a quoted or an escaped `~+` are
    literal, as in bash. Nothing in the corpus uses `~user`; the cost is a
    `cd ~-`, which denies;
  - an input redirection's target the gate cannot tell (`tr a b < "$X"`) is a
    word the command reads, whatever the command is, for a working copy as
    for an adopted repository;
  - brace expansion is done as bash does it, before anything else reads
    the word: an unquoted `{...}` with a comma at its top level gives
    each alternative (`{a,b}`, `x{,.bak}`, nested ones expanded too), and
    `{x..y}` or `{x..y..step}` a sequence of integers (zero-padded when
    an end is: `{01..03}`) or of single letters, each combined with the
    text before and after it. Every word that results is a word of its
    own for every rule: `cat {pbip,sql}/x` names the root, `git log
    {-p,-2}` is `git log -p -2`, and `git add src/{a,b}.go` adds two
    files. A word the expansion leaves empty is dropped, as bash drops
    it: `{rg,} x` is `rg x`. Any other brace is literal, as in bash:
    `{a}`, `{}`, `{a..}`, `HEAD@{1}`, `${...}`, a quoted brace, the
    grouping `{` and `}`, and a `{name}` just before a redirection, which
    is the redirection's named descriptor (`{fd}>/dev/null`).
    Past 64 words, with an end or a step outside the 64-bit integers, or
    in a word of more than 4,096 characters, the word is unknown, as a
    variable is, and a refusal for it says it is a brace expansion. A
    line whose brace expansion makes more than 20,000 words in all is
    denied with `internal-error`, as one with too many commands is.
    (Decided 2026-11-02.) The predecessor reads such a word
    literally;
  - an unquoted word with a glob character in git's arguments is read as
    the words bash expands it to in the working directory, before git
    sees them: `git log -p -- p?ip` is `git log -p -- pbip` where the root
    is there (and the tree is scanned for naming it). A pattern that
    matches nothing stays as written, which git reads as a pathspec glob,
    unprovable but judged by the name-only run; a word whose expansion
    cannot be done (the directory or the word cannot be told, or the glob
    budget runs out) is unknown. A quoted glob is left to git. The words
    of `commit`, `merge`, `tag`, `notes` and `stash` (messages and names)
    are judged as written. (Decided 2026-11-02.)
  - a here-document (`<<WORD`, `<<-WORD`) is read as bash reads it: its
    body runs from the line after the operator to the line that is the
    delimiter (leading tabs removed for `<<-`), or to the end of the line
    when there is none, and the command line goes on after it. A body is
    data, never commands: with a quoted delimiter (`<<'EOF'`, `<<"EOF"`,
    `<<\EOF`) all of it; with an unquoted one, all but its command
    substitutions (`$(...)` and backticks), which are read as nested lines.
    A here-document inside a command substitution is read the same way.
    The delimiter's quoting is removed as bash removes it, `$'E'` and
    `$"E"` included (both end at a line `E`); a delimiter that still holds
    `$` or a backtick, and a body whose delimiter line never comes, leave
    the line uncertain (below);
  - a command substitution's body is read by the tokenizer itself, as
    bash reads it: its end is the first `)` at the body's top level, after
    its quotes, `${...}` (a `)` or `#` in one is text), backticks, nested
    substitutions, comments, here-documents and case patterns. Inside
    arithmetic (`((...))`, `$((...))`, `$[...]`) `<<` is a shift, not a
    here-document, and only the substitutions in it run. In a case
    pattern `|` separates patterns, and a pattern's `)` closes nothing.
    Each nested construct is read once, whatever reader tries it first
    (an arithmetic expression that turns out to be a subshell): the ends
    found are remembered for the line, so reading it takes time linear in
    its length (200 nested `$((` read at once);
  - a carriage return is part of a word, as in bash (`git log -p\r` gives
    git the word `-p\r`), not a blank. A backslash-newline outside single
    quotes, comments and the body of a here-document with a quoted
    delimiter is removed before the line is read, as bash removes it,
    wherever it is: `npm ci &&\<newline>npm test` is `npm ci &&npm test`,
    `&\<newline>&` is `&&`, `$\<newline>(` is `$(`, and a multi-line `if`
    or a pipeline continued after `|` reads as one line. In an unquoted
    here-document body the lines are joined too, as bash joins them
    (`RUN apt-get update && \<newline>  apt-get install ...` in a
    Dockerfile written with `cat <<EOF`), and in arithmetic, double quotes
    and a here-document delimiter (`$((\<newline>`, `"$\<newline>(...)"`).
    The gate reads the line once to find where no join is made, removes
    every other backslash-newline, and reads it again; each join removed must
    fall where the second reading reads code, not inside single quotes,
    `$'...'`, a comment or the body of a here-document with a quoted
    delimiter (a join that splits `<<` makes the text after it such a
    body: `cat <\<newline><'E'`). The script `echo` or `printf` feeds a
    shell is the text it prints, read the same way, joins included. The
    gate reads exactly two tables of escapes, as bash's builtins do:
    printf's format reads `\\`, `\a`, `\b`, `\f`, `\n`, `\r`, `\t`, `\v`,
    `\"` and `\'`; an `echo -e` or `%b` argument reads the same but `\"`
    and `\'`, which it keeps as written. Any other escape (`\x`, `\0NNN`,
    `\NNN`, `\u`, `\U`, `\c`, `\e`), any conversion but `%s`, `%b` and
    `%%`, and `printf -v` leave the text untold, and the script counts as
    built at run time (below). Whose echo prints the text matters: bash's
    reads escapes only with `-e` (or with `shopt -s xpg_echo`, `bash -O
    xpg_echo`), the Bash tool's own shell may be zsh (macOS), and `sh`
    (dash), zsh and ksh read them without `-e`. Where the shell running
    the echo is not known to be bash (the top level; `sh -c`, `dash -c`,
    `zsh -c`, `ksh -c`; any line that sets `xpg_echo`), a backslash in the
    text of an echo with no `-e` leaves the text untold (`-E` is an
    option only to bash and zsh; sh prints it as a word and reads the
    escapes, so `echo -E 'x\ngit log -p' | sh` is built at run time):
    `echo 'git log \055p' | bash` is built at run time, `echo 'git log
    --oneline -1' | bash` is read. Only inside `bash -c` is the echo
    bash's, and `-E` trusted. A test checks every text the gate claims
    against what the shells print: printf and `echo -e`/`-E`/`-n`, a
    plain echo and the xpg_echo variant against bash, and a plain echo
    against the system `sh` (and zsh when installed), over a few thousand
    random feeds and the escapes the reviews found (the test never calls
    an unread text wrong);
  - the tokenizer fails closed on its own uncertainty. These are lines the
    gate cannot read as bash would, each named in the deny in its own
    fixed words: a line it does not end at the plain top level (a `case`
    or a parenthesis left open, a case pattern being read, a
    here-document left open: "leaves a case, a parenthesis or a
    here-document open"), a `)` that closes nothing, unbalanced quotes or
    a trailing backslash, a line nested more than four deep, a
    here-document delimiter that holds `$`, a backtick or a newline, a
    backslash-newline in an unquoted here-document body that changes
    which line ends it (the line after it is the delimiter, or the
    joined line is) or that is in a line with a `$(` or a backtick, a
    backslash-newline between two operator characters that join into one
    redirection or here-document operator (`<\<newline><`, `>\<newline>>`,
    `&\<newline>>`, `<\<newline>(`; a blank on either side joins nothing:
    `go test ./... \<newline>> out.txt` is read), a join that the second
    reading finds inside quotes, a comment or a quoted here-document
    body, and a here-document inside a command substitution with a line that starts
    with its delimiter (bash 5.2 ends the body there, later versions do
    not). Such a line is denied outright with `internal-error`, with a
    fixed message naming the construct and asking for the command line
    written plainly, as separate simple commands; nothing in it is judged
    in part and no tree is scanned. (Decided 2026-11-02.) A differential
    test runs generated lines under real bash with stub programs, in
    repositories whose finding is in the working tree, in history only,
    and staged only, and checks that the gate denies every one in which
    bash runs a dangerous command; whether git prints the finding is
    asked of real git, run where the stub runs, and a stub `make`
    rewrites a protected file as a formatter would;
  - a program is known by the last element of its path, without a
    trailing `.exe` in any case, as Windows, Git Bash and WSL run it:
    `rg.exe`, `/usr/bin/rg.exe` and `git.exe` are `rg` and `git`. Its case
    is folded only where the file system folds case (Windows, macOS):
    there `RG.EXE` is `rg`; elsewhere `RG` is another program.
    **Divergence (`.exe` names):** the predecessor matches a search tool
    by its exact name, so `rg.exe` is not a search there;
  - wrappers are skipped, with their options and operands, to the command
    they run: `env`, `command`, `exec`, `nohup`, `builtin`, `time`, `nice`,
    `sudo`, `doas`, `xargs`, `stdbuf`, `timeout`, `ionice`, `setsid`,
    `caffeinate`, `unbuffer`, `winpty`, `chronic`, `taskset`, `flock`, `coproc`,
    `busybox`, `xvfb-run` and a few tracers and sandboxes. Their long
    options that take a value given apart (`timeout --signal KILL 5`) are
    read as such. The value of a wrapper's option the table says takes one
    (timeout's duration and `-s`/`-k`, nice's `-n`, sudo's `-u` and `-g`,
    xargs's `-P`, `-n`, `-L`, `-s`, `-d`, `-E`, ionice, taskset and stdbuf
    values) and an `env NAME=value` operand are not the command word, even
    when the gate cannot expand them: `timeout "$T" go test ./...`, `env
    GOOS=$(go env GOOS) go build`, `sudo -u "$USER" make` are read as `go
    test`, `go build`, `make`. An unknown word in the program position
    stays unknown (built at run time), and so does one after a wrapper
    option the tables do not know: for the wrappers with operands
    (`timeout`, `taskset`, `flock`), whose no-value options are listed,
    an unknown option leaves its operands unread, since it may take a
    value of its own (`timeout --frobnicate "$X" 5 go test` is built at
    run time); for the others the unknown word is in the program's place
    already (`nice`, `sudo`, `env`, `stdbuf`). `time` and `coproc` are
    followed by
    the reserved words after them, as bash reads them (`time { git log
    -p; }`, `time -p ! cmd`, `time function f { ...; }`). git run by
    `xargs` or `parallel`, and git under `find -exec` given `{}`, `{/}`
    or `{.}`, is given words
    the line does not show: they are unknown words of git's, which rule 2
    denies wherever an unknown word could be an option (`echo -p | xargs
    git log`; `xargs git push` stays allowed). The words `xargs` and GNU
    `parallel` read from their input are unknown wherever the command
    they run uses them: xargs's replacement string (`-I R`, `-IR`, `-i`,
    `--replace[=R]`, `{}` by default), parallel's placeholders (`{}`,
    `{.}`, `{/}`, `{//}`, `{/.}`, `{#}`, `{%}`, `{n}`, `{= ... =}`, and
    `-I R`), and, with no placeholder, the words added after the command.
    They count in a nested `sh -c` or `bash -c` string, in a function or
    any other nested line (`script -qc`, `flock -c`, `watch`, `eval`,
    `find -exec` included), as any program's argument, and as the file an
    input redirection names. Only a wrapper of the command or its program
    is a runner: `git checkout -b parallel`, `git commit -m xargs`, `rg
    parallel src` and `head xargs` name a runner as a word. `rush`,
    `rargs`, `xe`, `entr`, `sem`, `env_parallel` and the like are runners
    whose options are not read, so every word of their command counts. A
    runner's output piped into a shell is a script built at run time
    (`... | xargs echo cat | sh`). A reader or
    search given one, or an interpreter (`perl -e`, `python3`) given one
    or the appended input, touches every protected root, as `xargs cat`
    does: `git ls-files | xargs -I{} sh -c 'cat {}'`, `... | xargs -I{}
    sh -c 'jq . {}'`, `... | parallel 'cat {}'` and `... | parallel cat`
    scan the tree. `parallel` with literal `:::` arguments reads nothing
    from input. When the gate cannot read a runner's options completely
    (an option it does not know, a cluster it cannot split such as
    `-n1I{}`, a value that cannot be told or is no number where one is
    due, a word it cannot expand among them or as the command), every
    word of the command the runner runs counts as an input word, nested
    lines included: `xargs -Z -I{} sh -c 'cat {}'` and `xargs -Z sh -c
    'cat README.md'` scan the tree. find's `{}` is not one: find's own
    walk names the files, which the path it searches covers (`find src
    -exec sh -c 'jq . {}' \;` is allowed when src holds no root, `find .
    -exec sh -c 'cat {}' \;` scans). `command -v` and `-V` run nothing.
    Any other program, unless it is one known never to run its words
    (`echo`, `which`, `man`, package managers and the like), whose first
    word that is not an option is a reader, a search, git, `find`, `fd`,
    a shell or `eval`, is judged as that program: an unknown wrapper is
    judged by what it runs;
  - `sh`, `bash`, `zsh` and kin with `-c` (after `--`, and after
    long options that take a value such as `--rcfile`), `script -c`,
    `flock FILE -c`, `watch` (its words, joined), `env -S`, GNU `parallel`
    (its template with each argument after `:::`, or each argument alone),
    `eval` and every command substitution (one inside a `${...}`, as in
    `${X:-$(cmd)}`, nested ones and those in `$((...))` included) are
    read as nested lines, at most four deep, with every rule applied to
    them. So is the script a
    shell reads from a here-document, a here-string (`<<<`), `source
    /dev/stdin`, or a pipe from a here-document, a here-string, `echo` or
    `printf`; a shell reading any other pipe runs a script built at run
    time. A deeper line is one the gate cannot read (above). `eval`'s `cd`
    moves the line after it; the others' does not;
  - code built at run time is code the gate cannot see, and it could run
    git printing history, which no tree scan covers: a line holding any
    is denied with `git-content` (and touches every protected root). It
    is a nested line whose text is not literal (`bash -c "$CMD"`, `eval
    "$x"`, an unquoted here-document with an expansion fed to a shell),
    a shell or interpreter reading a pipe or input built at run time,
    `source` or `.` of a process substitution, `/dev/fd/N` or a pipe
    (`source` and `.` alike), a command word the gate cannot expand or
    that holds a glob, from the first word to the program's (`$(which
    git) log -p`, `${GIT:-git} log -p`, `$g diff`, `/usr/bin/gi? log`),
    but a path whose directory cannot be told to a program named by its
    literal last element, which is that program as any path to it is
    (`"$(go env GOPATH)/bin/golangci-lint" run`,
    `${GOBIN:-$HOME/go/bin}/staticcheck`, and `"$(dirname x)/git"`, judged
    as git), unless that program is a shell, an interpreter, a wrapper,
    `eval`, `source` or `.`, or the word can split into others (an
    unquoted command substitution, positional parameters, or a variable
    the line names other than as an expansion, where it could set it),
    a name that `hash -p` or `alias` made stand for another program, a
    function named as a program the gate models (`cd() { ...; }`, `cat()
    { ...; }`; a function definition's name is never run as a command),
    `mapfile` or `readarray` with a callback (`-C`), quoted code that bash
    evaluates later: a quoted `$(`, backtick or `${` that can become a
    variable's value, in a line that also evaluates one. It can become a
    value in an assignment word (`v=...`, `v+=...`, `a[i]=...`), a word
    of `read`, `printf -v`, `declare`, `typeset`, `local`, `export`,
    `readonly`, `mapfile`, `readarray` or `let`, of a `for` or `select`
    list or of a compound array (`a=(...)`), a `${x=...}` or `${x:=...}`
    default, a here-string or here-document a value setter reads, a word
    `test -v`, `[[ -v` or `unset` reads as a name, and anything a command
    in a substitution or a nested line prints or a pipe or a process
    substitution passes on (`v=$(echo '...')`, `echo '...' | read v`);
    also the arguments of `set` and `getopts`, of a function the line
    defines when it is called, and of any command in a line that reads a
    positional parameter or OPTARG (`$1`, `${1}`, `$@`, `$*`, `$#`, in a
    word, an expansion or arithmetic, `(( $1 ))` included) and evaluates
    (`set -- '...'; echo $(( $1 ))`, `f(){ echo $(( $1 )); }; f '...'`,
    `getopts 'a:' o -a '...'; echo $((OPTARG))`).
    When a `read` or `mapfile` reads a value from a file, which the gate
    cannot see, any quoted code in the line counts. A git message, a sed
    script or an echo at the top of the line becomes no value:
    ``for ((i=0; i<3; i++)); do ...; done && git commit -qm 'Use `make`'``
    is allowed. A line evaluates a value when it has an array subscript or
    name read by `test -v`, `[[ -v`, `printf -v`, `read`, `mapfile`,
    `declare -n`, `let` or `unset`, a variable declared with `-i`
    (`declare`, `typeset`, `local`, `export`, `readonly`), whose every
    assignment is evaluated, an operand of `[[` with `-eq`, `-ne`, `-lt`,
    `-le`, `-gt` or `-ge`, or any arithmetic context that names a variable
    or holds an expansion, anywhere in the line, a case pattern included:
    `$((...))`, `$[...]`, `((...))`, a subscript in an expansion
    (`${a[x]}`, `${b[$x]}`) or in an assignment (`b[x]=1`, `a=([x]=1)`), a
    substring offset (`${PWD:x:1}`, `${@:x}`), after a subscript too
    (`${a[@]:x}`, `${a[0]:x}`), and any of these nested in an operator's
    operand (`${y:-${s:x}}`, `${s#${s:x}}`, `${s/a/${s:x}}`); or a `${x@P}`
    or `${x@E}` expansion. Code built at run time is also setting a
    variable whose value bash
    runs later (`PS0`, `PS1`, `PS2`, `PS4`, `PROMPT_COMMAND`, `BASH_ENV`,
    `ENV`, `SHELLOPTS`, `BASHOPTS`, after reserved words such as `time`
    and under a wrapper such as `env` included), `tmux send-keys`, `at
    -f`, and `pwsh`, `powershell` or `cmd` running a command. **Known
    over-block:** `eval "$(pyenv init -)"` and the like, whose code is a
    tool's output, deny; run the tool's setup outside the agent's
    commands. (Decided 2026-11-02.)
  - there is no parser for PowerShell or `cmd`: `pwsh`, `powershell` or
    `cmd` with any argument but a version or help request, or reading a
    pipe, counts as touching every protected root, and running a command
    as code built at run time (above). `ssh` with a command after the
    host or reading a pipe, `su` or `runuser` with a command, and
    `machinectl shell` touch every protected root.
  1. The tool itself alone (one simple command, optionally after environment
     assignments, with no `$(` or backtick) is allowed when its subcommand
     is on an explicit table of what an agent may run, because those
     redact on their own: `scan`, `scan-text`, `explain`, `baseline`
     (`record` writes a reviewable file and refuses under `CI`; `verify`
     reads), `version`, `help`, `--help`, `-h`, `--version`, and
     `policy status`; a help flag anywhere (`pbipx policy --help`, bare
     `pbipx policy`) prints help and runs nothing, so it allows unless a
     person-only step is named. In pbipx, any other word in the
     subcommand position is not the tool alone: it falls through to the
     ordinary rules, which count it as reaching every protected root, so
     it denies while the tree holds a finding (as `pwsh` does). A word
     there that the gate cannot tell (`pbipx $SUB`, a glob) could be a
     person-only step and denies outright (below). `stage`, `collect` and
     `perf`, planned subcommands, are off the table: each joins it, or the
     person-only list, in the change that builds it. `perf` is decided
     now: an agent may run it, since it prints counts and durations only
     ([perf.md](perf.md)), so its change adds it to the table in one line;
     `stage` and `collect` stay undecided. The predecessor
     allows any subcommand but `policy approve`. (Decided 2026-11-04.)
  2. A git command that prints file content is **denied** outright (git
     history cannot be vetted), unless it provably prints nothing from a
     protected root. In pbipx, for the commands that have a name-only form,
     the gate asks git itself (see Clarifications, "Gate", divergence on
     git's name-only check); for the others, its pathspecs must provably
     avoid every protected root:
     - always content: `blame`, `annotate`, `grep`, `archive`,
       `format-patch`, `difftool`, `range-diff`;
     - content that cannot be placed, whatever the pathspecs:
       `fast-export`, `pack-objects`, `unpack-file`, `filter-branch`;
       `rebase` and `am` with `--show-current-patch`; `rebase` with `-x` or
       `--exec`, `submodule foreach` and `bisect run`, which run a command;
       `request-pull -p`; `bundle create` to `-` or a `/dev/` path (to a
       file it is allowed); `merge-tree --trivial-merge`, or with three
       commits and no `--write-tree`; `merge-file --object-id`;
     - `diff` unless a summary flag is given (`--stat`, `--numstat`,
       `--shortstat`, `--name-only`, `--name-status`, `--dirstat`,
       `--compact-summary`, `--summary`, `--raw`, `--no-patch`, `-s`,
       `--quiet`), and a patch flag overrides the summary;
     - `log`, `reflog` and `whatchanged`, and `diff-index` and `diff-files`,
       only with a patch flag (`-p`, `-u`, `--patch` and kin, `--cc`, `-c`,
       `--dd`, `--remerge-diff`, `--full-diff`, `-L`, and the content
       searches `-S` and `-G`); `diff-tree` with a patch flag or `--stdin`;
     - `show`: a `REV:path` or `:path` argument prints that blob; any
       argument holding `:/` or `^{/` (a search of commit messages) cannot
       be placed. Every other argument is resolved in the repository with
       `git cat-file -t`: a commit, a range of commits or a tag of a commit
       prints a diff, as `diff` does (an annotated tag prints its target);
       a blob, a tree, a tag of anything else, or a name that does not
       resolve prints content that cannot be placed, whatever the summary
       flags say. `--all`, `--branches`, `--tags`, `--remotes`, `--glob`,
       `--stdin` and the like cannot be placed either;
     - `stash show` with any option but the summary formats (`--stat`,
       `--numstat`, `--shortstat`, `--name-only`, `--name-status`,
       `--summary`, `--dirstat`, `--compact-summary`, `--raw`): git 2.53
       prints a patch for any diff option that is not an output format
       (`-w`, `-R`, `--color`, `--histogram`, `--abbrev`, `--pretty=raw`,
       `--graph` ...), so such a command goes through the name-only run.
       `-u`, `--include-untracked` and `--only-untracked` say which files
       the stash holds and are no output format: with no format given,
       alone or not, `stash.showPatch` decides (true: the name-only run),
       as with no option at all; a summary format given as well settles
       it; `cat-file` except `-t`, `-s`, `-e`,
       `--batch-check`, and an object name other than `REV:path` cannot be
       placed;
     - hunks shown interactively: `add -p` and `add -i`, `checkout -p`,
       `restore -p`, `reset -p`, `stash -p` (and `stash push -p`), `commit
       -p` and `commit --interactive`, short options clustered too (`-ap`);
     - an alias: a subcommand that is not one of git's own is looked up
       with `git config --get alias.<name>`. A shell alias (`!`), an alias
       that does not split, or an alias of an alias is denied; otherwise the
       expansion, split as git splits it, replaces the subcommand and is
       judged with the rest of the arguments. In pbipx a subcommand that is
       neither one of git's own nor an alias is denied too: it may be an
       alias defined earlier in the same line (`git config alias.lp 'log
       -p' && git lp`, looked up before it exists) or a `git-<name>`
       program, whose output cannot be told;
     - git's global options `-C`, `-c`, `--config-env`, `--git-dir`,
       `--work-tree`, `--namespace` and `--attr-source` consume their
       argument. `--git-dir` and `--work-tree` make every content
       unprovable. `-c` or `--config-env` with a key not known to be
       harmless (an alias, a pager, an editor, `diff.external`, a textconv
       or other driver, `stash.showPatch`, `log.*`, anything that can run a
       command) and `--exec-path=` deny any subcommand. The known-harmless
       keys are `color.*`, `user.*`, `advice.*` and similar, a pager set to
       `cat`, empty or a boolean, and an editor set to `true`;
       `--literal-pathspecs`, `--glob-pathspecs` and `--icase-pathspecs`
       make every pathspec unprovable;
     - an argument that cannot be told (`$X`, `"$X"`, `${X}ip`, `$(...)`)
       could be any pathspec, revision, option or directory (`-C $D`).
       Before `--` it could be an option that turns content on (`-p`,
       `-v`, `--cc`, `--file`, a stash or reflog subcommand), so it denies
       every subcommand but those that print no file content whatever
       their options (`branch`, `fetch`, `push`, `remote`, `tag`, `init`,
       `ls-remote`, `ls-files`, `ls-tree`, `rev-parse`, `merge-base`,
       `describe`, `show-ref`, `for-each-ref`, `count-objects`, `version`,
       `help`, `gc`): `git log $(echo -p)`, `git log --oneline $REV`,
       `git status $X`, `git add $X` and `git config --get $K` are denied.
       A message is never an option: the value of `-m` or `--message` for
       `commit`, `merge`, `tag`, `stash push` (or `stash` given options
       alone) and `notes add` or `append`, given apart (`git commit -m
       "$(cat <<'EOF' ...)"`), attached (`-m"$MSG"`, `--message="$MSG"`)
       or after a cluster of short options that take no value (`git
       commit -am "$(...)"`, `-sm`, `-am"$MSG"`), is judged as usual. The
       options are read as git reads them, from the gate's table of each
       subcommand's options: an `m` after a letter that takes a value is
       that value, attached or apart (commit's `-c`, `-C`, `-F`, `-t`;
       `-S` and `-u` take one only attached; merge's `-s`, `-X`, `-F` and
       `-S`; notes' `-F`, `-c`, `-C`; tag's `-u`, `-F` and `-n`), so `git
       commit -Fm "$X"` is denied, and so is a `-m` that is itself the
       value of the option before it (`-F -m "$X"`, `--author -m "$X"`).
       After a letter or a long option the gate does not know (an
       abbreviation such as `--auth` included), given without `=`, no
       later word is a message. A cluster is still read for the options it
       holds, so `git commit -pm "$(...)"` and `git stash push -pm ...`
       are denied as interactive hunks. A message that holds an expansion
       is one only when every expansion in it (`$X`, `${...}`, `$(...)`,
       backticks, `$1`) is inside double quotes: outside them the shell
       splits its value into words, which could be options (`--dry-run
       -v` prints the staged diff). Some expansions are several words
       even inside double quotes: `"$@"`, and any `${...}` holding `@`
       (`"${@:2}"`, `"${a[@]}"`, `"${!p@}"`, `"${!a[@]}"`); they are
       taken to split wherever they are (`"$*"` and `"${*}"` are one
       word), and so is `"$a[@]"`, a name followed by a subscript, which
       zsh (the Bash tool's shell on some systems) expands to every
       element, and any `"${(...)...}"`, zsh's parameter flags (`(f)`,
       `(s: :)`, `(@)`), which split. A glob character outside quotes
       expands to the names it
       matches, so a message word holding one is denied as well. So `git
       commit -m $X`, `-am $X`, `-m$X`, `--message=$X`, `-m "$X"$Y`, `-m
       $(cat <<'EOF' ...)`, `-m "$@"`, `-qm "${a[@]}"`, `-m *` and `-m
       "$X"*` are denied, while `-m "$X"`, `-m "$*"`, `-m "fix *"` and
       `-m "fix: $X done"` are not. In
       PowerShell the message must likewise be a double-quoted string
       (`"..."` or a here-string): an unquoted variable, subexpression,
       group or splat may be an array, passed as several arguments, and
       an expansion given to `Start-Process -ArgumentList` goes into a
       command line git splits. This also decides git's message text in
       rule 3 below. After
       `--` it is a pathspec, and a subcommand that prints content is
       denied without asking git anything with the word unexpanded (the
       stash lookup included), while one that does not
       (`log --oneline -- $X`, `status -- $X`, `add -- $X`) is allowed.
       A subcommand or a global option that cannot be told (`git $SUB`,
       `git --namespace=$N`) is denied. The message says which of these it
       is, never the word, and its hint is to write git's arguments out in
       full. **Divergence (unknown words):**
       the predecessor does not judge such a word;
     - environment assignments before git: any `GIT_` variable but the
       author and committer identities, tracing and a few others, a
       `GIT_PAGER` or `PAGER` other than `cat`, an editor other than `true`
       and `LESSOPEN` deny any subcommand;
     - pathspecs avoid the roots when every root is excluded (`:!`, `:^`,
       `:(exclude)`), or when there is no glob and no positive pathspec
       touches a root. Pathspecs are those after `--` (or
       `--pathspec-from-file`, which cannot be placed); for `show` and
       `cat-file`, `REV:path`, relative to the repository root; for `blame`
       and `annotate`, every positional argument before or after `--`, and
       `--contents`, since git takes the file before or after the
       revision; for `add -p` and `restore -p`, every positional argument.
       In pbipx, positional arguments are read as git reads them: the
       value of an option that takes the next word (`-n`, `--max-count`,
       `--since`, `--author`, `--grep`, `--diff-filter`, `--date`, `-S`,
       `-L`, `--output`, `restore -s` and the like) is neither a revision
       nor a path; for `show`, positionals are revisions up to the first
       that is not one and names an existing path, which with the rest is
       a pathspec; for `checkout -p` and `reset -p`, a first positional
       naming a tree-ish is the source (denied when it also names a path),
       the rest pathspecs. `REV:path` is cut at the first `:` outside
       braces, so `HEAD@{10:00}` is a revision and `HEAD@{10:00}:x` the
       path `x`.

     In pbipx the message names the subcommand only when it is one of
     git's own; any other word, an alias's name included, is printed as
     `git`. It then says what makes the content unsafe: the first file
     the name-only run would print content from and the protected root
     it is in, the root a pathspec reaches (named from the policy, never
     the pathspec as typed), or what makes it unprovable (a `-c` key, an
     alias, a `:/` search, an object name, a glob or magic pathspec,
     `--pathspec-from-file`, `--stdin`, a timeout ...).

     **Divergence (a read after a tree change).** In pbipx, once a command
     in the line changes the working tree with content the gate has not
     seen, every later command in the line, nested lines included, that
     reads is **denied** with `git-content`: its read would be judged
     against the tree as it was before the change. The changes are git's
     `switch`, `checkout`, `restore` (not `--staged` alone), `merge`,
     `pull`, `cherry-pick`, `revert`, `am`, `apply` (not `--check`,
     `--stat`, `--numstat`, `--summary` or `--cached`), `rebase`, `clone`,
     `stash` (but `list`, `show`, `create`, `store`, `drop`, `clear`),
     `reset --hard`, `--merge` or `--keep`, `read-tree -u`,
     `checkout-index`, `sparse-checkout` (but `list`), `bisect` (but `log`
     and the like), `submodule update`, `add` or `foreach`, `worktree add`,
     `bundle unbundle`, `merge-file` (but `-p`), `filter-branch` and
     `mergetool`, and any subcommand that is not git's own (an alias, a
     plugin); and an archive extracted or a patch applied: `tar -x`,
     `unzip`, `7z x`, `cpio -i`, `ar x`, `gunzip` and kin (not to stdout),
     `patch`. Creating a branch is not a change: `git checkout -b` or `-B`
     and `git switch -c` or `-C` with no paths and no start point, or a
     start point that resolves to HEAD's commit (`--orphan` and
     `--detach` are changes). A command reads when it is a reader, a
     search, `find`, `fd`, a shell or `eval`; or when a word of it (not an
     output redirection's target; an option by its `=value`), placed
     against the working directory it runs in, is, is inside or contains a
     protected root, names local state or a file that controls the gate, or
     cannot be placed (an unknown expansion, an unknown working
     directory). A word that only looks like a path (`./...`, `src/`, `-o
     out.txt`) does not count, so `git checkout main && go test ./...`,
     `git switch feature && make build` and `git pull && npm install` are
     allowed (principle 6). `echo`, `printf` and the like never read, and
     git's own reads are judged by rule 2. The change alone, with nothing
     reading after it, is allowed: the next call is judged against the
     changed tree. `git stash` stays a change: the read after it sees
     HEAD's content, which is history. The command substitutions
     (`$(...)`, backticks) in a command's own words and here-documents run
     before the command, so they are judged as before its change, and a
     change in one counts before the command: `git merge -m "$(cat <<'EOF'
     ...)" main` and `git stash push -m "$(cat <<'EOF' ...)"` are allowed,
     and so is `git merge -m "$(cat notes.txt)" topic`, while `cat f
     "$(git checkout old)"` is denied. A substitution in any later command
     counts as after the change. Some commands do not run in their order
     in the line, and a line holding one is unordered: with a change
     anywhere in it, every read anywhere in it, before the change or
     after, in the same command or another, denies as after the change.
     They are a loop (`for`, `while`, `until`, `select`, and zsh's
     `repeat` and `foreach`, found after any grouping or reserved words,
     `! for`, `{ for`, and a body in braces included), which runs its
     body again; a function definition (an unquoted word, of any
     characters, followed by `()` after any reserved words, or `function
     name`), `alias` (whose command is read as a nested line), `eval`,
     `source` and `.`, which run where they are used; a background job
     (`cmd &`), `coproc` and a process substitution (`<(...)`, `>(...)`),
     which run alongside the rest; `trap`, whose command (read as a
     nested line) runs later; a detached job, which finishes after the
     line: `disown`, `daemonize`, `start-stop-daemon`, `systemd-run`,
     `at` and `batch` (whose commands, from a pipe or a here-document,
     are read as a nested line; from a file, they reach every root),
     `setsid -f`, `screen -dm` and `tmux` with `-d` (whose shell command
     is read as a nested line; `tmux send-keys` reaches every root); and
     `xargs`, `find -exec` (`-execdir`, `-ok`, `-okdir`), `fd -x`/`-X`,
     `parallel` and `watch`, which run a command repeatedly. A change
     inside a process substitution counts so too. A pipeline is unordered
     among its own stages only: a change in one stage and a read in
     another deny, while a pipeline before or after a change is judged in
     its place (`go test ./... | tail -5; git stash pop` is allowed); in a
     line with a group or a subshell, a stage is taken to reach back to
     the line's start. A filter whose only input is the pipe reads no
     file and is not a read here: `head`, `tail`, `wc`, `sort`, `uniq`,
     `cut` and `cat` with no operand and only the options the gate lists
     for each, and `grep` and the searches the search rule takes to read
     their input alone (no operand, no option naming a file, no
     configuration), run under no wrapper and with no input redirection
     (`git pull && make | tail -5` is allowed). `cat` with no file operand
     whose only input is one here-document reads no file either (`git
     stash pop && git commit -am "$(cat <<'EOF' ...)"`). Neither holds
     when the line may redefine the program (below: a function, alias,
     `hash`, `PATH` and the like); a filter given a file, and the
     substitutions an unquoted body runs, still read.

     git's own reads are judged by rule 2, against the repository (its
     index, HEAD, refs, configuration and objects) as it is before the
     line runs. So every git command that is not known to be read-only
     makes any later git command in the line that could print file
     content given its arguments deny with `git-content` (`git add -A &&
     git diff --cached`, `git add -A && git commit -qm wip && git show`,
     `git add -A && git stash && git stash show -p`, `git tag -f main
     <tree> && git diff --cached main`, `git fetch && git diff --cached`);
     in an unordered line, any such command anywhere. The read-only git
     commands are an allowlist: `status`, `log`, `show`, `diff` and
     `whatchanged` without `--output`, `rev-parse`, `rev-list`,
     `ls-files`, `ls-tree`, `ls-remote`, `cat-file`, `describe`,
     `merge-base`, `name-rev`, `for-each-ref`, `show-ref`, `show-branch`,
     `count-objects`, `version`, `help`, `var`, `check-ignore`,
     `check-attr`, `check-ref-format`, `check-mailmap`, `blame`,
     `annotate`, `cherry`, `diff-tree`, `diff-index`, `diff-files`,
     `verify-commit`, `verify-tag`, `shortlog`, `range-diff`, `grep`
     without `-O`, `stash list` and `show`, `notes list` and `show`,
     `worktree list`, `reflog` (but `expire`, `delete` and the like),
     `config` only reading, `branch` and `tag` listing (no name but with
     `--list`, and none of the options that create, move or delete one),
     `remote` with no subcommand, `show` or `get-url`, `symbolic-ref`
     with one name and no `-d`, and `replace` with nothing or `--list`;
     `checkout -b` and `switch -c` creating a branch at HEAD change
     nothing a later command prints. Every other git command, an alias, a
     subcommand that is not git's own, an ambiguous abbreviation, `gh pr
     checkout`, `gh repo sync`, `gh repo clone` and hub's tree-changing
     git commands count as a change. After a change, a later git command
     is still taken to print no content when:
     - it is a summary of HEAD, `@` or a revision relative to them
       (`HEAD~1`, `@^2`, `HEAD^..HEAD`), which stay commits unless the
       line runs `symbolic-ref`, `update-ref` or `replace` other than
       reading, or a subcommand that is not git's own: `git add -A && git
       commit -m "x" && git show --stat HEAD` is allowed, and so are `-s`,
       `--name-only` and `--name-status`;
     - its pathspecs, after `--`, provably avoid every root, as rule 2
       proves them without asking git (no glob or magic, each one outside
       every root, a nested repository's roots included), for `diff`,
       `log`, `whatchanged`, `grep` and the diff plumbing, but with
       `--full-diff`, `-L`, `--follow`, `--no-index` or
       `--pathspec-from-file`, or a submodule diff: `git add -A && git diff
       --cached -- src` is allowed. With no pathspec, git's name-only
       answer, asked before the line runs, is not trusted after a change;
     - it is the working-tree diff (`git diff` with no revision, no
       `--cached`), and every change before it can only shrink that diff
       or leaves the working tree and the index alone: `git add` (not
       `-N`, `-p`, `-i`, `-e`, `--renormalize` or `--chmod`), which makes
       the index match the working tree, `git stash` or `stash push` and
       `save` (not `-p` or `--staged`), which make both match HEAD, and
       `fetch`, `tag`, `push`, `remote` and `branch`, which move refs only.
       That diff was judged before the line (`git add src && git diff`,
       `git stash && git diff`, `git fetch && git diff`). `git reset` and
       `git restore --staged` make it grow, and still deny. The rules for
       a command that may write files (below) apply to it too: a writer
       after a stash denies (`git stash && make fmt && git diff`).
     In PowerShell HEAD is not taken to stay a commit. **Known
     over-blocks:** `git fetch && git diff --cached`, `git branch
     --contains X && git diff --cached`, and `git switch main && git
     pull && git diff`, deny, though the first command may move nothing
     the second reads; so does `git add -A && git show HEAD -- src`.
     (Decided 2026-11-02.) "Later"
     follows the order commands run in: a command's own substitutions
     before it. A git command that prints no file content whatever the
     files hold (`git push`, `git commit -m msg`, `git status` without
     `-v`, `git log --oneline`, and the commands that never print content)
     stays allowed: `git add -A && git commit -m "..." && git push` is. The
     same holds through PowerShell, statement by statement.

     A command the gate does not know may write files under a root
     without naming them: a formatter, a build, code generation,
     `pre-commit run`, `npm run x`, a script. git's own reads were judged
     against the working tree as it was before the line. So when a git
     command that can print the working tree's content runs after a
     command that is not known to write nothing (before it, or anywhere
     in an unordered line), the protected tree is scanned as for Grep, on
     top of rule 2: a block or an error anywhere denies, a clean tree
     allows (`make fmt && git diff` denies while a root holds a finding).
     The git commands that print the working tree are `diff` but
     `--cached` or a summary alone (with or without a revision),
     `diff-files` with a patch, `diff-index` with a patch but `--cached`,
     `status -v -v`, `commit -vv`, `add`, `checkout`, `reset` and
     `restore` with `-p` (and `add -i`, `-e`), a dry run of `commit` with
     `-v` and `-a`,
     `-o`, `-i` or paths, `stash show`, `difftool`, `mergetool`, `blame`,
     `annotate`, `grep` but `--cached`, and any subcommand that is not
     git's own. Index-only and history-only forms (`diff --cached`, `log
     -p`, `show`) are left to the rules above. A print whose pathspecs
     provably avoid every root, as rule 2 proves them without asking git,
     is left out: only pathspecs after `--` count, since an option's value
     can read as a path (`git diff -I x src/main.go`), and only for
     `diff`, `diff-files`, `diff-index`, `add` and `grep`, whose patch
     honours them; `status -v -v` prints its diff whatever the pathspec
     (checked with git 2.53). `make fmt && git diff -- src` is allowed;
     `make fmt && git diff src/main.go` and `make fmt && git status -vv --
     src` scan. The commands known to write nothing are an assignment
     alone; a git command that changes no working tree and runs none of
     the hooks that commands writing files run (`add`, `stash`, `fetch`,
     `tag`, `branch`, `remote`, `notes`, `config`, `update-ref` and the
     read-only forms); and programs that print, list, read or search or
     set the shell's state (`echo`, `printf`, `cd`, `ls`, `cat`, `head`,
     `grep`, `rg`, `jq`, `wc`, `find` without `-exec` or `-delete`, `fd`
     without `-x`, `sort` without `-o` and the like), each with no output
     redirection but to `/dev/null` or a descriptor. Every other command
     may write: a git command that runs `pre-commit`, `commit-msg`,
     `post-commit`, `post-checkout`, `post-merge`, `post-rewrite`,
     `pre-push`, `pre-rebase`, `pre-auto-gc` and the like (`commit`,
     `merge`, `rebase`, `pull`, `am`, `checkout`, `switch`, `cherry-pick`,
     `revert`, `push`, `clone`, `worktree`, `gc`) or changes the working
     tree, any other program, and a function or alias. The
     `post-index-change` and `reference-transaction` hooks also run on
     `status` (refreshing the index), `add`, `tag`, `branch`, `fetch` and
     `update-ref`: when the repository has either (an executable file of
     that name in `core.hooksPath`, or in the common git directory's
     `hooks`; a `.sample` is none, and a hooks directory the gate cannot
     tell counts as having one), every git command is taken to write,
     which is broader than the lists above: the read-only forms (`status`,
     `log`, `diff`) and `add`, `stash`, `fetch`, `tag` and `branch` too.
     Other hooks those commands run are not counted. So `git add src &&
     git diff` and `git stash && git diff` are allowed while a root holds
     a finding, and scan when such a hook is there.

     A git command that puts content from history in the working tree (a
     stash, a checkout or restore of paths, a reset `--hard`, a switch, a
     merge, a pull, a rebase, a cherry-pick, a revert, `am`, `apply`),
     then a command that may write files, then a git command that prints
     the working tree, denies with `git-content`, the message naming the
     git command: the writer may rewrite what history put back, and the
     tree scan sees the tree as it was before both (`git stash && make
     fmt && git diff`, while HEAD holds a finding its staged fix hides).
     With no writer between, the print stays judged as above (`git stash
     && git diff` prints nothing). **Known limitation:** a command after
     such a write that reads a file by name, other than git (`make && cat
     pbip/x`, where the file did not exist before), is judged against the
     tree as it was. (Decided 2026-11-02.)

     The tokenizer fails closed on its own uncertainty: a line that does
     not end at the plain top level (a `case` left open, a case pattern
     being read, a parenthesis left open, a here-document whose delimiter
     line never comes) is denied with `internal-error` (above). A
     `case` is opened only by `case WORD in` as a command (after any
     reserved words), so `echo case x in` opens none, and closed by `esac`
     as a command word or right after `in`; a case pattern's `)` closes
     no parenthesis, so a pattern inside a process substitution keeps the
     rest of it inside. In a PowerShell line, a
     subexpression in git's arguments is read as a statement before
     git's, so it is judged the same way.
  3. If the working directory is inside a protected root, or the command
     names a root as a whole word, the protected tree is scanned as for Grep.
     In pbipx the tree is scanned too when:
     - any command runs from a working directory that is inside a root or
       cannot be told;
     - any word of any command, dequoted, expanded and resolved against
       that command's working directory (relative to it or absolute,
       symlinks resolved component by component before a `..` after them,
       as the operating system walks the path; the stricter of that and the
       path as written), is a protected root, is inside one, or contains
       one (but the target of `cd`, `pushd` or `chdir`, which moves the
       working directory, judged for the commands after it, and reads
       nothing: `cd ..` is not a read of the root it holds; nor the
       first operand of `jq`, `yq` or `gojq`, its filter, unless `-f`
       gives the filter: `jq . package.json` reads package.json; the
       values of its options, `--arg NAME VALUE`, `-L DIR` and the like,
       are not the filter, and are placed as any word. A filter that holds
       `import` or `include` reads modules from its search path, so the
       working directory counts as read too). That
       includes the value of a `--flag=value`, what an unquoted
       glob matches, and for a word with `**` (which zsh, and bash with
       `globstar`, read as any depth) everything under its literal prefix.
       Each word is expanded once per decision, whichever rule asks. All
       the globs of one decision together read or walk at most 200,000
       directory entries (a match costs ten); a glob past that, or with
       more than 64 segments after its literal prefix, counts as touching
       every root, and so does every glob after it. `**` is matched in time
       proportional to the pattern's and the path's lengths;
     - a word of a reading or searching command (`cat`, `head`, `less`,
       the grep family, `rg`, `sed`, `awk`, `sort`, `jq`, `cp`, `tar` and
       the like) is unknown;
     - an operand of any other program is unknown, unless the program is
       in the table of programs known not to read their operands as files:
       a content printer outside the readers (`shuf`, `expand`,
       `unexpand`, `ptx`, `nl`, `od`, `fmt`, `python3 -m json.tool`,
       `perl -pe`, `node -p`, `curl -d @file`, `tee`) reads it as `cat`
       does. The table is one list in the code, checked by a test: `echo`,
       `printf`, `mkdir`, `rmdir`, `touch`, `rm`, `mv`, `ln`, `chmod`,
       `chown`, `chgrp`, `ls`, `stat`, `du`, `df`, `tree`, `test`, `[`,
       `[[`, `basename`, `dirname`, `realpath`, `readlink`, `file` (it
       reads a file's start to print its type, never a line of it),
       `which`, `type`, `sleep`, `kill`, `ps`, `pwd`, `id`, the shell's
       own commands (`cd`, `export`, `set`, `declare`, `read`, `getopts`,
       `trap`, `shift` ...), `git` (its own rule), `hub` and `pbipx`.
       Build and package tools are not in it: `make -f "$f"`, `go run
       "$f"`, `go vet "$f"`, `npm`/`yarn`/`pnpm` with a path, `pip install
       -r "$f"` (which quotes invalid lines), `cargo --manifest-path`,
       `gem install "$f"`, `apt install ./"$f"`, `brew install "$f"`,
       `man "$f"` and `info "$f"` read the files their operands name; nor
       is `date` (`date -f`), `cp`, `tar`, `zip`, `unzip`, `rsync`, the
       interpreters or anything not listed. `gh` is read by its options:
       a word that is the value of `-F`, `-f`, `--field`, `--raw-field`,
       `--body-file`, `--input`, `--notes-file` or `--file` is a file gh
       reads; its other words, a title or a body (`--body "$(cat <<'EOF'
       ...)"`), are text. The unknown operand also covers a word that
       stands for a runner's input (below); the words of an option, the
       program's own name and the wrappers' operands (below) are not
       operands;
     - a file an input redirection names that the gate cannot tell
       (`cat < "$f"`, `tee /dev/null < "$f"`, `while read l; do ...; done
       < "$f"`, `exec 3< "$f"`) is read by whatever the redirection is
       for, whatever the program, and so is a placeholder of a runner used
       as one (`xargs -I{} sh -c 'jq . < {}'`). A here-string is text, not
       a file;
     - `find P ... -exec` (or `-execdir`, `-ok`, `-okdir`), or
       `find P ... | xargs ...`, has a path P (default `.`) that touches a
       root; `xargs` running a reading command counts as reading under its
       working directory. The command `-exec` runs is judged as a command
       of its own;
     - `fd` (or `fdfind`) with `-x`, `--exec`, `-X` or `--exec-batch`, or
       piped to `xargs` or `parallel`, has a search path (the positional
       arguments after the pattern and `--search-path`, under
       `--base-directory`; default `.`) that touches a root, as for `find`;
       the command it runs is judged as a command of its own. `fd` alone
       prints names, which are schema;
     - a nested line is built at run time.

     A root's name is compared without regard to case in these tests, on
     every platform: they only decide whether the real roots are scanned.

     In pbipx git's message text is left out of these tests, both the
     whole-word test of the line and the placing of words: it is prose,
     and people name folders in commit messages. It is the value of `-m`
     or `--message` as rule 2 reads it (apart, attached, `--message=`, or
     after a cluster: `commit`, `merge`, `tag`, `stash push`, `notes add`
     and `append`), and, when that value is a command substitution whose
     one command is `cat` with no argument reading a here-document
     (`git commit -m "$(cat <<'EOF' ...)"`), that here-document's body;
     and the here-documents of git reading its message from standard
     input (`-F -`, `--file=-`, `--file -` for `commit`, `tag` and `notes
     add`; `git merge` reads a file named `-`), and its here-strings
     (`git commit -F- <<<"..."`). A message word that is several words
     (above), or that holds a `${...}` running a command
     (`"${X:-$(cat f)}"`), is not message text. Only git run under no
     wrapper counts: under `xargs`, say, its input and words are not what
     the line shows. The command substitutions an
     unquoted such body runs are still read as nested lines, and so is any
     other substitution in a message (`git commit -m "$(cat pbip/x)"`
     scans). Every other word of the command (a pathspec after `--`, the
     value of `-F`), and every other command, is tested as before. Text a
     program may read as code keeps the test: `python3 -c "open('pbip/x')"`,
     `node -e`, `perl -e`, an awk program, and a here-document fed to an
     interpreter (`python3 - <<'EOF' ...`, `cat <<'EOF' | python3`).
     A message is still run if something feeds it to an interpreter
     (`git log -1 --format=%B | python3`): a script interpreter reading
     its program from its input runs a program built at run time, as a
     shell reading a pipe does, and touches every protected root. The
     interpreters are `python`, `pypy`, `perl`, `ruby`, `php` and `lua`
     with any version after the name (`python3.14`, `perl5.36`), `node`
     with a number (`node22`) and `nodejs`, `ipython`, `R`, `Rscript`,
     `luajit` and `tclsh`, given no script operand, no code option (`-c`,
     `-e`, `-r` ...) and no module or file option (`-m`, `-f`), or given
     `-`, `/dev/stdin`, `/dev/fd/0` or `/proc/self/fd/0` as the script,
     or given an option the gate does not know before its first operand
     (that operand may be the option's value, not the script);
     interpreter names are compared in lower case on every platform;
     `deno` alone or `deno run` and `bun run` read the same way; `awk`,
     `gawk`, `mawk` and `nawk` only with `-f` naming their input. `uv
     run`, `poetry run`, `pipenv run`, `conda run`, `pdm run`, `hatch
     run`, `rye run`, `pnpm exec`, `npx` and `bunx` are wrappers of the
     program after them (their options skipped; a value given apart is
     taken for the program). Another interpreter, or one under a name not
     listed, is not judged so. Its input is shown only when it is a
     quoted here-document, or one or a here-string with no expansion, and
     no pipe: that text is tested as above.

     The message text is tested as any other text when the line may make
     `cat` or `git` run another program: it defines a function (`name()`
     unquoted, or `function name`), runs `alias`, `hash`, `enable`,
     `source`, `.` or `eval`, or assigns or exports `PATH`, anywhere in the
     line or a line nested in it. **Known limitation:** a function or
     alias the shell already has when the line starts (from a startup
     file, or a snapshot of the user's shell the agent runs commands in)
     is not seen.
     **Known over-blocks:** `echo pbip` and `printf` naming a root still
     scan the tree, since their output can feed a pipe into an
     interpreter. Only git's message text is exempt: the text another
     program takes, such as `gh pr create --body "$(cat <<'EOF' ...)"`,
     is still tested for a root's name (`gh` itself is a program that runs
     none of its words, so such a body is allowed when it names no root;
     a file it reads, `--body-file` or `--input`, is placed as any word;
     `gh pr checkout`, `repo sync` and `repo clone` change the tree, and
     `gh pr diff` prints a remote patch, which denies with `git-content`
     unless `--name-only` is given; gh's command group is found after
     its options, `gh -R owner/repo pr diff` included; `hub` is judged as
     git, its subcommand found as git's and never taken for a program it
     wraps (`hub log -p`, `hub grep`). **Known
     limitation:** other remote content gh prints, `gh api` of a
     `/contents/` or `/raw/` path, `gh gist view` and `gh run view
     --log`, is not judged),
     and so is git's message under a wrapper (`env GIT_AUTHOR_NAME=x git
     commit -m "..."`). **Known limitation:** a message written to a file
     and run as a script in a later step (`git log -1 --format=%B > x.py
     && python3 x.py`) is not followed, as no script file is.

     In pbipx a command that only lists names is left out of these tests:
     names are schema (principle 1), so it is allowed over a protected
     root whatever the tree holds, from any working directory that can be
     told. The listers are `ls`, `dir`, `vdir` and `stat`; `du`, but not
     with `--files0-from`, `--exclude-from` or `-X`; `tree`, but not with
     `--fromfile` (which prints a listing read from a file), `-o` or `-R`
     (which write); and `find` whose expression after its paths holds only
     tests of names and metadata (`-name`, `-path`, `-regex`, `-type`,
     `-maxdepth`, `-size`, `-newer` and `-newerXY`, `-mtime`, `-perm`,
     `-user`, `-empty`, `-prune` and the like), operators, and `-print`,
     `-print0`, `-ls` or `-printf` with only name and metadata directives
     (`%p`, `%f`, `%h`, `%P`, `%s`, `%k`, `%m`, `%M`, `%u`, `%g`, `%y`,
     `%d`, `%i`, `%n`, `%l`, times and escapes). `-exec`, `-execdir`,
     `-ok`, `-okdir`, `-fprint`, `-fprintf`, `-delete`, any other primary
     and any other directive keep the rules above. `file` reads content to
     tell a type, and `wc` counts it: neither is a lister. A root named
     in a substitution or here-document of a lister, or in another command
     of the line, is judged as before, and a listing fed to a reader
     (`ls pbip | xargs cat`) is judged by the reader. Naming local state
     still denies.
  4. A recursive search (`grep`, `egrep` and `fgrep` with `-r` or `-R`;
     `rg`, `ag` and `ack` always) over a path that touches a root scans the
     tree. Its paths are the positional arguments after the pattern (unless
     `-e` or `-f` gave it), default `.`, from the command's working
     directory.

     In pbipx a search is taken to read **recursively** under its paths
     unless it is provably confined to the files it names: recursion is
     not told by a flag's shape. `grep` can be ugrep under another name,
     and ugrep recurses on `-NUM`, `--depth`, `-d recurse`, `-g`,
     `--include`, `--index` or a `.ugrep` file. The searches are `grep`,
     `egrep`, `fgrep`, `rgrep`, `ugrep`, `ug`, `ugrep-indexer`, `rg`,
     `rga`, `ripgrep-all`, `ag`, `ack`, `ack-grep`, `sift` and `pt`
     (`git grep` is rule 2's). For each:
     - its operands are the positional arguments after the pattern (all of
       them for `ugrep-indexer`, which takes none), and each is read under,
       recursively; a file operand is read alone, which comes to the same;
     - it also reads under its working directory (`.`) when it has no
       operand, when an option is not one the gate knows for that program,
       when an operand holds a glob character, or when a configuration
       could apply: a `.ugrep` file in the working directory or the home
       directory for the grep family and ugrep (an unknown home counts as
       one); `GREP_OPTIONS`; `RIPGREP_CONFIG_PATH` for `rg` and `rga`
       unless `--no-config`; `.ackrc`, `ACKRC` or `ACK_OPTIONS` for `ack`
       unless `--noenv`; `.sift.conf` and `.ptconfig.toml`. A variable
       counts when it is set in the gate's environment or named anywhere in
       the command line, and so does a line that sets `HOME`;
     - `grep`, `egrep` and `fgrep` with no operand read their input, not
       `.`, but only when every option is in the program's allowlist of
       options that neither widen the files searched nor read another file
       (`-i`, `-n`, `-c`, `-l`, `-L`, `-h`, `-H`, `-o`, `-q`, `-s`, `-v`,
       `-w`, `-x`, `-E`, `-F`, `-G`, `-P`, `-e`, `-f`, `-m`, `-A`, `-B`,
       `-C`, `--color` and their long forms) and no configuration applies;
     - a file an option names for reading (`-f FILE`, `--file`,
       `--exclude-from`, `--include-from`, `--ignore-file`,
       `--path-to-ignore`) is read too, as `cat` would read it;
     - an option that reads files the command line does not name or runs a
       command on what it reads (ugrep's `--from`, `--config`, `--filter`
       and `-Q`; `rg --pre`; ack's `--files-from`, `--ackrc` and `-x`, or
       an abbreviation of one), or an argument that cannot be told, counts
       as touching every root.

     So `grep -rn TODO src`, `rg -n x src/main.go` and `cat f | grep x`
     read what they name, and `grep --depth=99 x` from the repository's
     top reads the protected tree. As everywhere, a scan that finds
     nothing allows (principle 6).
  5. Otherwise allowed.

  Before rule 1, a command that names a Claude Code settings file in the
  repository (below) as a word, a redirection target, the value of a
  `--flag=value` or what an unquoted glob matches is **denied** with
  `agent-settings`. The message does not name the file: it can exist only in
  the command's text. So, in pbipx, is a command that:
  - writes into a `.claude` directory inside the repository: as a
    redirection's target, the destination of `cp`, `mv`, `ln`, `install`
    or `rsync` (the last argument, or `-t`/`--target-directory`), a file
    `tee` writes or `dd`'s `of=`, or copies a directory named `.claude`
    into the repository;
  - names, in any word, a path inside the approval store or a user-level
    or managed Claude Code settings file (the files the Write rule below
    protects), or copies or writes into a user-level Claude Code settings
    directory; the message says which and does not name it;
  - runs a person-only step of the tool, with no tree consulted: `pbipx
    policy approve` or `pbipx policy unenroll` (`policy`, then a word that
    begins with `approve` or `unenroll`), `pbipx init` and `pbipx gate`
    with any flag (a help request alone excepted), or `--show-values`, by
    any path to the tool (in any case where the file system folds case),
    through a pipe, or under `script`, `expect`, `unbuffer` or `winpty`,
    which give it a terminal and so defeat the flag's refusal without one;
    `parallel` too, whose input words are added. Approval is a person's
    step (ADR-0006), and so are removing a repository's adoption, setting
    pbipx up and printing values. A word the gate cannot tell (a
    variable, a substitution, a glob) in the subcommand position or after
    `policy`, or words a runner adds (`... | xargs pbipx policy`, an
    accepted over-block), could be one of them, so it denies the same; so
    does an unknown word before an `approve` or `unenroll` word, which
    could be `policy`. Under a terminal program, a word of the tool's that
    cannot be told could be the flag. The PowerShell check reads the same
    words in any case, splits the strings given to `Invoke-Expression`,
    `pwsh -c`, `Start-Process` and the like into words, and counts
    `policy` followed by a group, a hashtable or a variable, or a string
    built from pieces that names the tool, as unreadable. The message
    names neither `unenroll` nor how to run a step. The drift-guard test
    (`cmd/pbipx`) requires every command and every `policy` subcommand
    cobra registers to be on the agent table or the person-only list.
  - Limits: a terminal provider the gate cannot list (a `python` or `perl`
    pty module, `socat`) is not seen, and relies on the tool's own refusal
    without a terminal. `find -exec pbipx {} approve` is not exploitable:
    the tool gets a file name first, not `policy`.

  In pbipx, naming a settings file in the repository, or a path in the
  approval store or a user-level or managed settings file, denies only when
  the command could write it: reading one is allowed (`cat
  .claude/settings.json`, `jq .hooks ~/.claude/settings.json`, `cat` of an
  approval record). A word could be written when it is a redirection's
  target (input redirections are not told apart, so `cat <
  .claude/settings.json` denies), an option (`--output=FILE`, `-oFILE`:
  only an operand of a reading program counts as read), an assignment or a
  wrapper's own argument, or when the command is not one that only reads.
  The commands that only read are `cat`, `head`, `tail`, `more`, `wc`,
  `diff`, `cmp`, `stat`, `ls`, `grep`, `egrep`, `fgrep`, and, with these
  exceptions, `less` (not with `-o`, `-O`, `--log-file` or a `+command`),
  `rg` (not with `--pre`), `jq` (not with `-i` or `--in-place`), `bat` (not
  with `--pager`), `file` (not with `-C`) and `sed` (not with `-i`,
  `--in-place`, `-f` or `--file`, and only when every script is made of
  commands that print, delete, quit or substitute: `p`, `d`, `q`, `s///`
  with flags among `gpiImM` and digits, `y///`, `=` and the like, after line
  or `/regex/` addresses; `w`, `e`, `r`, `a`, `i`, `c` and `s///w` or
  `s///e` are refused). Each runs with no environment assignment before it
  and under no wrapper but `command`, `builtin`, `nohup`, `nice`, `stdbuf`,
  `ionice`, `setsid`, `timeout` and `xargs`, and with every argument
  known. `awk` is not among them: its program can write the file it reads
  (`print > FILENAME`), and the gate does not read awk. Every other command
  that names such a file, `cp` and `mv` included, denies.
- **PowerShell** `command` (pbipx only; Claude Code's PowerShell tool, which
  is separate from Bash and on by default on Windows for some accounts).
  The gate has no parser for PowerShell, so a call is judged as if it
  touched every protected root of each adopted repository it reaches. It
  reaches the repositories its working directory is in or above, and
  those of every word that looks like a path. The line is split into
  words as far as the gate reads it: `'...'` and `"..."` (and the
  typographic quotes) group, a backtick escapes the next character, and
  whitespace and `; | & ( ) { } , < > =` separate; `-Name:value` also
  yields `value`. A word looks like a path when it holds `/` or `\`, is
  `~`, or names an entry of the working directory; `\` is also read as
  `/` where the system does not take it as a separator, and a leading `~`
  is the gate's home directory. A path-like word holding `$` cannot be
  placed, so it touches every indexed work tree. In each such repository:
  - any word, under any spelling, that is a Claude Code settings file of
    the repository or a `.claude` directory in it denies with
    `agent-settings`; so does any word naming a file outside it that
    controls the gate (as for Bash, but reading counts too: a PowerShell
    read cannot be told from a write), and the words `policy approve` in
    sequence. These deny from every working directory, adopted or not;
  - git printing content git history holds denies with `git-content`,
    judged as for Bash (Bash's rule 2, above). For this the line is also read
    into statements: one ends at `;`, `|`, `&&`, `||`, a background `&`, a
    line break, and the bounds of a group (`(...)`, `$(...)`, `@(...)`,
    `@{...}`) or a script block (`{...}`), whose own statements are read
    too; a comment, a redirection and its target, and an assignment to a
    variable before the command are left out. A statement runs git when
    its first word is `git` (any case, a path to it, with `.exe`, `.cmd`
    or `.bat`), after `&` or the dot-source `.` too, or when git is the
    first word that is not an option after another command (`cmd /c git
    ...`); the words to the end of the statement are git's arguments. A
    word holding `$` outside single quotes, a backtick escape, a group or
    a splat (`@name`) cannot be told, and neither can a word holding `%`
    after `--%`: as for Bash, before `--` it could be an option that
    prints content, and after it a pathspec. Also judged as git: a
    command word that cannot be told (`& $exe log -p`, `& (Get-Command
    git) ...`) given arguments; `Start-Process` of git or of a program
    that cannot be told, with `-ArgumentList` (or the second positional
    argument) split as Windows splits a command line; and the command
    line given to `Invoke-Expression` (`iex`), or to `pwsh`, `powershell`,
    `cmd` or a Unix shell as one string, read as a PowerShell line, up to
    4 deep. `Invoke-Expression` or a shell given an argument that cannot
    be told, or fed by a pipe, `pwsh -EncodedCommand`, a script block
    built from a string (`[scriptblock]::Create`, `InvokeScript`), and
    `Set-Alias`
    or `New-Alias` naming git deny, since git could run in a way the gate
    cannot follow. An assignment to `$env:NAME` (or a word naming such a
    variable, as `Set-Item env:NAME` does) of a variable git reads is
    judged as an assignment before git in Bash. After `cd`,
    `Set-Location`, `Push-Location` or `Pop-Location` anywhere in the
    line, or with `Start-Process -WorkingDirectory`, git is judged from
    the working directory, the repository's root and each protected root.
    A git command that changes the working tree (as for Bash) followed by
    a statement other than git denies, since the later one is judged
    against the tree as it was before. A nested command line shares the
    session of the line around it: its statements run in order with the
    line's, and a location change, an environment assignment or a change
    to the tree in either counts for both (`iex 'Set-Location pbip'; git
    log -p -- x` is judged as moved);
  - any word naming local state denies with `local-state`;
  - otherwise the protected tree is scanned, as for Grep: a block or an
    error anywhere there denies, a clean tree allows.

  A line of more than 20,000 words, or with groups nested more than 64
  deep, is denied with `internal-error`. A PowerShell call that builds a
  path at run time (`Join-Path`, a variable set from a computation)
  reaches only what its words show; this is the same limit as for Bash
  (threat model). A script it runs (`.\x.ps1`, `pwsh -File`) and a
  function or alias defined in an earlier call are not read.
  Windows PowerShell 5.1, and PowerShell 7 with
  `$PSNativeCommandArgumentPassing` set to `Legacy` or `Windows`, do not
  escape a `"` inside an argument passed to a program, which can then
  reach git as several arguments. So any argument to git whose literal
  text holds a `"` cannot be told and may be several words, in every
  PowerShell (`git commit --dry-run -m 'a" -v "b'` is denied); and in a
  line that names `$PSNativeCommandArgumentPassing` in any form
  (`$global:...`, `Set-Variable`, `sv`) with `Legacy` or `Windows`, or
  with no `Standard`, so may a double-quoted argument holding an
  expansion (`-m "$m"`). **Known limitation:** Windows PowerShell 5.1 passes arguments
  that way without the variable being set; the gate takes `-m "$m"` there
  as one argument, as PowerShell 7 passes it, so a value of `$m` holding
  a `"` can still reach git as several arguments.
- **LSP** (pbipx only; Claude Code's code intelligence tool). Its input is
  not documented, so the adapter takes every string, at any depth of
  `tool_input`, under a key whose name holds `path`, `file` or `uri` (case
  ignored) as a path, a `file://` URI decoded to its path (a host other
  than `localhost` kept as a UNC prefix). Such a key holding a number or a
  boolean, or a `file://` URI that does not parse, is an `event-error`.
  Each path is judged as a Read, or as a Grep when it is a directory; any
  deny wins. A call that names no path is taken to touch the working
  directory's whole project, and scans the protected tree as for Grep.
  An LSP answer about a named file can quote other files (references,
  definitions); those are not judged one by one.
- **Write**, **Edit** and **MultiEdit** of `file_path`, and **NotebookEdit**
  of `notebook_path`, or `file_path` when it has none (in pbipx, a leading
  `~` expanded from the gate's home directory, as for Read and Grep):
  denied with `agent-settings` when the path, under any spelling (relative
  to the working directory, absolute, symlinks resolved), is a Claude Code
  settings file inside the repository: `.claude/settings.json` or
  `.claude/settings.local.json` in the repository root or any directory
  below it, names compared without regard to case. Such a file can set
  `disableAllHooks`, which turns off the gate's own user-level hook
  (threat model, layer 5).

  A write outside the repository is denied with `agent-settings` too when,
  under any spelling (and with the symlinks of the deepest existing
  ancestor resolved, for a path not yet there), it is one of the files that
  control the gate:
  - inside the pbipx approval store: `$PBIPX_CONFIG_DIR`, and
    `<user config dir>/pbipx` in any case (layer 6; a forged record would
    approve a weakened policy);
  - a user-level Claude Code settings file: a file named `settings*.json`
    (case ignored) directly in `$CLAUDE_CONFIG_DIR` or in `~/.claude`
    (on Windows under both `%USERPROFILE%` and `%HOME%`, which Git Bash
    may set as `/c/Users/me`, read as `C:\Users\me`);
  - inside Claude Code's managed settings directory (`/etc/claude-code`,
    `/Library/Application Support/ClaudeCode` or
    `C:\Program Files\ClaudeCode`), `managed-settings.json` and the
    `managed-settings.d` drop-ins included.

  The message says which of these the path is, and does not name it. Every
  other write is allowed: the gate does not otherwise judge writes.

  **Edit, MultiEdit and NotebookEdit (pbipx)** are judged first as a Read
  of the same path (above): Claude Code shows the model a snippet of the
  file around the edit in the tool result, so an edit of a file with a
  finding, of local state, or of a file that cannot be vetted is denied
  with that file's reason, whatever Claude Code's own rule that a file be
  read before it is edited (not relied on). Then the Write rules apply. A
  Write is not a Read: its result does not show the file's old content as
  far as Claude Code documents (it says the file was created or updated),
  so it is judged as a write alone. **Known limitation:** if Claude Code's
  Write result ever shows a diff against the old file, a write over a file
  with a finding would show it; this has not been seen.

**Nested work trees (pbipx).** A directory inside the repository that is
itself a git work tree, because it holds a `.git` directory or file, is a
nested work tree: a linked work tree (Claude Code adds one under
`.claude/worktrees/<name>` for an isolated subagent), a submodule or a
clone. It holds a copy of the protected roots, so wherever the gate asks
whether a path is under, is, or contains a protected root, and wherever it
classifies a file, the path is also judged relative to each nested work
tree it is in, under the outer policy's roots and classes as written, and
under the nested work tree's own `.pbipx.toml` when it has one that loads.
The strictest decision wins: a nested policy can add roots, never remove
the outer ones, and one that fails to load is ignored. So:

- a Read of `.claude/worktrees/wt/pbip/.../visual.json` is scanned (or
  denied as local state or unvettable) as `pbip/.../visual.json` is;
- a path that is a nested work tree, or contains one (`.claude`,
  `.claude/worktrees`), touches a protected root for Grep and for rules 3
  and 4 of Bash, and so does a working directory inside a nested work
  tree's root;
- a tree scan walks every nested work tree's roots as well as the
  repository's own, wherever it was triggered, as it walks every root
  whichever one was touched. A leak in any work tree inside the repository
  denies a tree scan from the main one.

Nested work trees are found by walking the directories in question on
disk, without following symlinks and skipping `.git` directories; a
directory the gate cannot read is skipped, since the agent, running as the
same user, cannot read it either.

**Known limitation.** A copy of a protected root that is not inside a
nested work tree is not recognised: a vendored project, a backup folder
(`backup/pbip/...`), an unzipped export, or a directory named like a PBIP
item (`*.Report`, `*.SemanticModel`) outside the roots. Such a copy is
judged only by its own path under the policy, which usually puts it outside
every root. Keep copies out of the repository, or list them as protected
roots.

A deny writes the Claude Code decision JSON (`permissionDecision: deny` and a
reason) to stdout, writes the reason to stderr, and exits 2. An allow writes
nothing and exits 0. The reason names the file, the column and the reasons,
and never a value.

Every path a deny message prints, from the event, the command or the
tree, is printed segment by segment as a finding prints a name: a segment
with a personal-data shape or a control character is `{?}`. The command
line is never printed.

**Divergence (what triggered a refusal).** The predecessor's messages
said only that a call was denied and why in general ("a shell command
over a protected root could reach ..."). A refusal people cannot act on
gets the hook disabled (principle 6), so in pbipx every deny message
states its trigger concretely, in structure only, and ends with one short
hint of what to do instead:

- a tree scan says why the call reaches the protected tree: the working
  directory is in a root, a `cd` moves a command into one, a word names
  a path that is in or holds one, the line names a root, a word has an
  expansion the gate cannot resolve, the working directory cannot be
  told, a line is built at run time, `find`, `fd` or
  `xargs` read under a root, or a program the gate cannot read
  (PowerShell, `ssh`) runs a command;
- a search taken to be recursive says why: the option, printed from the
  gate's own table of option names (`--depth`, `-r`, `-d recurse`,
  `--glob`, `-NUM` ...), never echoed from the command; "an option the
  gate does not know", which is not printed; "no file operands"; a file
  operand that is a pattern; a configuration file (`.ugrep`) or variable
  (`GREP_OPTIONS`); or an option that reads files the command does not
  name;
- `git-content` names the subcommand (git's own only) and the file or
  root, or the reason the content cannot be placed (rule 2); for an
  argument the gate cannot expand, what it is in fixed words: "a
  variable or a command substitution", "a brace expansion of more than
  64 words", or "a message word with a glob outside quotes";
- a read after a tree change names the command that changes the tree,
  and a content-printing git command after a git command not known to be
  read-only names that command;
- code built at run time denies with `git-content` and says, in fixed
  words, what in the line is built at run time;
- a line the gate cannot read as bash would denies with `internal-error`,
  says which construct in fixed words, and ends with "Write the command
  line plainly, as separate simple commands."

Programs are named only when they are in the gate's tables (a reader, a
search, `git`'s own subcommands); a root is named from the policy. The
hints are fixed sentences, and lead with something the agent can do
itself, keeping a person's step where only a person can act: for a Read of
a finding in a report's selection, "Use `pbipx explain` for its
structure; to clear the selection, ask a person to clear it in Power BI
Desktop and save, or to run `pbipx scan` at a terminal."; for any other
finding on a Read, "Use `pbipx explain` for its structure; to clear the
value, ask a person to remove it in Power BI Desktop or an editor, or to
run `pbipx scan` at a terminal."; for a search, "Name the files to search
explicitly, or exclude the protected folders."; for git, "Limit the paths
(`-- src`) or use --stat."; for git given an argument the gate cannot
expand, "Write git's arguments out in full, without variables or command
substitutions.", followed for `commit` and `tag`, which read a message
from standard input with `-F -`, by "`git <subcommand> -F - <<'EOF'`
takes the message from a here-document." (for `notes`, "`git notes add
-F - <<'EOF'`"; `git merge -F -` reads a file named `-`, and `stash` has
no `-F`); for a PowerShell command, which the gate cannot read whatever
the working directory, "Use the Read tool for a single file, or
the Bash tool with the paths written out."; and for `agent-settings`,
"Show the person the exact change, for them to make at a terminal."

Gate reason classes: `finding`, `unvettable`, `scan-error`,
`local-state`, `perf-export` (pbipx only), `agent-settings` (pbipx only),
`git-content`, `working-copy` (pbipx only), `policy-error`,
`policy-unapproved` (pbipx only), `event-error`, `not-a-repository`,
`internal-error`.

Glob is not gated: it prints file names, and names are schema (principle 1).
It is left out of the hook's matcher.

Some routes to file content no PreToolUse hook sees: a file a person
`@`-mentions in a prompt, and tools from MCP servers. See the threat model.

`pbipx gate --agent claude-code` (the default and only agent) exits only 0
or 2. A usage error of its own flags, a help request and a panic each deny
with `internal-error`. The configured hook command fails closed on its own
(principle 7): Claude Code proceeds on any exit but 0 and 2, so the command
passes 0 and 2 through and turns every other outcome of running the binary
(missing, not executable, killed, crashed, exit 1) into a deny with exit 2.
`HookCommand` in [internal/gate/claudecode](../../internal/gate/claudecode)
writes it:

- POSIX `sh`, which Claude Code uses on Linux and macOS:
  `set +e; '/path/to/pbipx' gate --agent claude-code; s=$?;
  [ "$s" -eq 0 ] && exit 0; [ "$s" -eq 2 ] && exit 2; echo '...' >&2;
  exit 2`. `set +e` comes first because errexit can be on before the first
  line runs (`SHELLOPTS=errexit` in the environment, or a `BASH_ENV` script
  when the shell is bash); under it, a binary exiting 1 would end the shell
  with 1, which proceeds.
- Windows: Claude Code runs hooks through Git Bash, or PowerShell when Git
  Bash is missing, and a command written for one is a parse error (exit 1,
  proceed) in the other. So the hook sets `"shell": "powershell"` and the
  command is PowerShell: `$s` starts at -1, the binary runs inside
  `try`/`catch`, only an exit code of 0 or 2 is passed through, and
  everything else writes a reason to stderr and exits 2.

`pbipx init` registers the hook with `"timeout": 300` (seconds). Claude
Code treats a hook that times out as a non-blocking error and lets the call
proceed, so a search or shell call that scans a large protected tree must
not reach Claude Code's default timeout. A hook this binary registered in
an earlier command form, or under an earlier, narrower matcher, is
recognised by the binary's path and rewritten in place, its matcher
widened to the current one. A tool the matcher does not name never reaches
the gate, so after upgrading pbipx, re-run `pbipx init`.

For the same reason the adapter denies, with `internal-error` and a fixed
message, an event whose command line or path is longer than 1 MiB, or an
LSP event naming more than 64 paths or 1 MiB of them: the
gate's work grows with the command's length. Below that, reading a command
line is linear in its length, and the glob budget above bounds the file
system work. A line holding more than 20,000 simple commands, nested lines
included, is denied with `internal-error` rather than judged in part.

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
  so `not-a-repository` and `policy-error` deny every tool. In pbipx so does
  `policy-unapproved`, checked right after the policy loads (see "The gate
  (Claude Code)", divergence on policy integrity). In pbipx these deny every
  tool in, or reaching into, an adopted repository only (divergence on
  adoption).
- When a scan finds both a block and an error, the gate reports `finding`.
  (A scan's exit code reports the error: 3.)
- A shell command that reads local state (`cat .pbi/localSettings.json`) is
  allowed by the predecessor when the rest of the tree is clean, because tree
  scans skip local state, while a Read of the same file is denied.
  **Divergence (local state in shell):** pbipx denies a Bash command that
  names a local-state path or directory, with `local-state`. Tree scans
  still skip local state, so a recursive search that does not name it is
  decided as before. (Decided 2026-10-29.)
- **Divergence (search default):** the predecessor tells a recursive
  search by `-r` or `-R`, so `grep --depth=99 x` from the repository's top
  is allowed where `grep` is ugrep, which then reads the protected tree.
  pbipx takes every search to read recursively under its paths, default
  `.`, unless it is confined to the files it names (Bash, rule 4).
  (Decided 2026-10-30.)
- **Divergence (listings):** the predecessor scans the tree for any command
  that names a root, so `ls -R pbip` is denied while the tree leaks. pbipx
  allows a command that only lists names (Bash, rule 3), since names are
  schema. (Decided 2026-10-30.)
- **Divergence (agent settings):** the predecessor neither gates writes nor
  checks Claude Code's settings. pbipx denies an agent's write to, or shell
  command naming, `.claude/settings.json` or `.claude/settings.local.json`
  in the repository with `agent-settings`, and `pbipx scan` reports a
  committed one that sets `disableAllHooks`, or whose `env` sets a variable
  that can defeat the hook, with `hooks-disabled`, because such a file
  turns off the gate's user-level hook. (Decided 2026-10-29.) A shell
  command that only reads such a file is allowed; one that could write it
  denies (see "The gate (Claude Code)", Bash). (Decided 2026-10-30.) The
  code text given to an interpreter (`python3 -c`, `node -e`, `perl -e`,
  `ruby -e`, or a here-document fed to one) is read for the file's name as
  the root-name test reads it: a word of the text that is a path ending in
  `.claude/settings.json` or `.claude/settings.local.json`, in either
  slash and any case, denies. **Known limitation:** a name built at run
  time in the code (`d=.claude; echo > $d/settings.json`) is not seen.
- **Divergence (reading a command line):** the predecessor finds roots in a
  Bash command only as literal whole words of the line, and a command's
  program only as its first word. A command that reaches a root without
  spelling it is allowed there: after `cd ..` from a subdirectory, with the
  name split by quotes (`pb""ip`) or in another case, after `if`, `!` or
  `{`, through `~`, `$HOME` or `$PWD`, a `**` glob, `find -exec`, `xargs`,
  `sh -c` or `eval`, or through a symlink followed by `..`. pbipx reads the
  line as a shell reads it, as the Bash rule above describes, and errs
  towards scanning the tree when it cannot tell: an unknown working
  directory or an unknown word of a reading command counts as touching
  every root. It also denies writing into a `.claude` directory and running
  `pbipx policy approve` with `agent-settings`. The corpus cases for it are
  marked `known = "predecessor"`. (Decided 2026-10-29.) pbipx also reads
  here-documents as bash does, so a body is data (a commit message that
  mentions `git show` is allowed) and an apostrophe in one cannot hide the
  commands after it; unwraps more wrappers, and judges an unknown one by
  what it runs; and denies a read after a tree-changing command in the
  same line (rule 2). (Decided 2026-10-30.)
- **Divergence (git's name-only check):** the predecessor denies `git
  diff`, `git show`, `git log -p`, `git stash show -p` and `git grep`
  unless pathspecs after `--` exclude every root, so the commands agents
  run most (`git diff` on a clean tree, `git show HEAD`, `git log -p
  src/main.go`) are denied. pbipx runs the same git command in a form
  that prints only file names, reads its output (never printed) and
  allows the command when every file it would print content from is
  outside every protected root, a nested work tree's roots included
  (local state lives under the roots, so it is covered too). The files
  that control the gate (`.pbipx.toml`, the baseline, Claude Code's
  settings files) are readable with Read and `cat`, so git may print them
  as well: `git show` of a commit that only changes `.pbipx.toml` is
  allowed. (Decided 2026-10-30; until then they blocked the name-only
  check, which denied what a Read allows.) Every rule for writing them is
  unchanged:
  - `diff`, `show` of commits, `log` and `whatchanged` with a patch flag,
    `reflog` (show) with one, `stash show` with one (or with
    `stash.showPatch`), `stash list` with one, and `diff-tree`,
    `diff-index` and `diff-files` with one: the same arguments with the
    options that only change how the diff is printed removed (`-p`,
    `-U<n>`, `--stat`, `--format`, `--output`, `--color`, `--word-diff`,
    `--check` and kin) and `--name-status -z --no-color --no-ext-diff
    --no-textconv --no-relative` added; `--format=` for the log-like
    commands, `-r --no-commit-id` for `diff-tree`. Both sides of a rename
    or copy are judged. A merge printed as a combined diff can show a
    parent's file under a renamed path, so `--cc --combined-all-paths` is
    added to `show` (unless `--first-parent` alone is given), to a log
    with a merge option, and `-c --combined-all-paths` to `diff`. A
    remerge diff (`--remerge-diff`, `--diff-merges=remerge`, or `-m` with
    `log.diffMerges` set to it), `-L`, and `--stdin` are denied. A tag
    given to `show` is shown as its commit; a blob, tree, unresolvable
    name, message search or ref set given to `show` is judged as before.
  - `git diff --no-index`, or a diff naming an existing path outside the
    work tree (git then compares two files): denied when a path it names
    is under a root, or is a directory in the repository; with
    `--no-index`, allowed otherwise.
  - `grep`: allowed outright with `-l`, `-L`, `-c` or `-q` (names, counts
    or nothing), denied with `-O` (it runs a pager on the files), `-f -`
    or a negation of those options; otherwise run as `git grep -l -z
    --full-name --no-color --no-textconv` with the same arguments. A name
    printed as `REV:path` is judged as the path, and under the tree
    `REV:dir` as `dir/path`.
  - `status -v` (the staged diff) and `-vv` (and the unstaged one), and
    `commit --dry-run -v` (both): `git diff --cached` and `git diff` in
    name-only form. `--short`, `--porcelain` and `--long` imply
    `--dry-run`. With no `-v`, `--verbose` or `--no-verbose` given, git
    commit reads `commit.verbose` (a boolean or a level): the gate asks
    `git config --get --type=bool-or-int commit.verbose`; anything but
    unset, false or 0 counts as `-v`, and a level of 2 or more as `-vv`.
    A commit with a verbose count of 2 or more, from `-vv`, `-v -v`,
    `--verbose --verbose`, a cluster with `-m` or `-F` (`-vvm x`) or
    `commit.verbose` 2, prints the working tree's diff too when it fails
    with nothing staged, so it is judged as `status -vv` is, dry run or
    not (`git commit -vv` with a file deleted under a root denies; with
    one `-v` the rule is the dry run's alone). With `--amend`, git prints the index
    against HEAD's parent, so `git diff --cached HEAD^` is judged in
    name-only form too; a root commit has no parent, that run fails, and
    the command is denied. The short and porcelain formats, and
    NUL-terminated output, print no diff whatever `-v` says (checked with
    git 2.53). The format options are read in order, as git reads them:
    `--short` (for `status` also `-s`) and `--porcelain[=...]` set a short
    format, `--long` the long one, and a negation (`--no-short`,
    `--no-porcelain`, `--no-long`, abbreviated too) none; `-z` and
    `--null` turn NUL-terminated output on and `--no-null` off. Short
    options are read in clusters only up to a letter that takes a value
    (`status -u<mode>`; commit's `-m`, `-F`, `-c`, `-C`, `-t`, `-S`,
    `-u`), so `git status -v -ufalse` is not short. When the result is a
    short format or NUL-terminated output, `-v` is not judged; anything
    else, a configured `status.short` included, is taken to print the
    diff. `-c commit.verbose` or `-c stash.showPatch` set to false,
    `no`, `off` or `0` is harmless.
  - `--` is the end of the options only in option position: the value of
    an option that takes the next word may be `--` (`git commit -m --
    -v` is a message `--` and `-v`). For the subcommands with option
    lists (those that read their options with parse-options, and the
    short options of `commit`, `merge`, `stash`, `notes`, `tag`,
    `checkout`, `switch`, `restore`, `grep`, `rebase` and the like) the
    options are read to find it. For the others (`log`, `show`, `diff`
    and kin), a `--` after an option the gate knows to take a value
    (`--grep`, `-n`, `--author` ...) is that value, and one after an
    option the gate cannot tell takes no value denies when a word after
    it starts with `-`.
  - a diff shown with `--submodule=diff`, or with no `--submodule` given
    and `diff.submodule` set to `diff`, prints the changes inside each
    submodule, whose files the name-only form does not list: denied when
    the command would print a patch. (Decided 2026-11-02.) Set in a
    user's or the system's configuration, `diff.submodule=diff` makes
    every `git diff`, `log -p` or `show` that would print a patch deny,
    in every repository; `--submodule=short` or `--stat` is allowed.
  - long options abbreviated: the subcommands that read their options
    with git's parse-options (`commit`, `status`, `stash push`, `save`,
    `apply` and `pop`, `notes add` and `append`, `merge`, `add`,
    `checkout`, `switch`, `restore`, `reset`, `rebase`, `am`, `grep`,
    `cat-file`, `merge-file`, `merge-tree`, `apply` and `config`) take
    an unambiguous prefix of a long option, or of its `--no-` form, for
    the option: `git commit --amen --dry --verb` is `--amend --dry-run
    --verbose`. The gate expands each from its list of the subcommand's
    long options (git's documented ones) before any rule reads the
    options; the value of one that takes the next word is not read as an
    option. A prefix of more than one option denies, and, for a command
    that can change the working tree, counts as a change. Revision and
    diff options (`log`, `show`, `diff` and kin) are not abbreviated by
    git, and are matched in full.
  - git config changing the configuration in the same line, anywhere in
    the line, a nested line or a PowerShell statement included, denies
    every other git command in the line but those that print no file
    content whatever their options: git reads the configuration when it
    runs, after the gate read it (`git config commit.verbose true && git
    commit -a --dry-run`). `git config` is read as git reads it: the
    value of an option that takes one (`--comment`, `--file`, `-f`,
    `--blob`, `--type`, `--default`, `--value`, `--url`) is never an
    option, and its long options are expanded as above. Any write mode
    changes it whatever the keys (`--add`, `--replace-all`, `--unset`,
    `--unset-all`, `--rename-section`, `--remove-section`, `--edit`, and
    the subcommands `unset`, `rename-section`, `remove-section` and
    `edit`), and so does an option the gate does not know, an ambiguous
    abbreviation or a word that cannot be told. A plain set (`git config
    KEY VALUE`, `git config set KEY VALUE`) of one key known to be
    harmless (as for `-c`: `user.*`, `color.*` and the like) does not.
    A read (`--get` and kin, `--list`, `get`, `list`, or one key alone,
    `git config commit.verbose`) does not. The configuration is changed
    too by any command but git's own that could write a path inside a git
    directory (a path with a `.git` component, or inside this repository's
    common git directory), as the files that control the gate are judged
    (a redirection's target, the destination of `cp`, `mv`, `install`,
    `rsync`, `tee` or `dd`, `sed -i` and any command not known to only
    read): `printf ... >> .git/config`; by a write to any file named
    `gitconfig` or `.gitconfig`, or `config` in a directory named `git` or
    `.git`, wherever it is (Git for Windows keeps its system file under
    its install prefix, not `/etc`), and to the files `git var
    GIT_CONFIG_GLOBAL` and `GIT_CONFIG_SYSTEM` name; and by setting or
    exporting `HOME`
    or `XDG_CONFIG_HOME` anywhere in the line, which points git at another
    user configuration.
  - a variable git reads, set or exported by a command of its own
    anywhere in the line (`export GIT_PAGER=less; git log`, `GIT_CONFIG_COUNT=1;
    export GIT_CONFIG_COUNT`, `declare -x`), denies every git command in
    the line, as an assignment before git does: any `GIT_` variable but
    the identities, tracing and the like (`GIT_CONFIG_COUNT`,
    `GIT_CONFIG_KEY_*`, `GIT_CONFIG_VALUE_*`, `GIT_CONFIG_GLOBAL`,
    `GIT_CONFIG_SYSTEM` and `GIT_CONFIG_PARAMETERS` included), a pager
    other than `cat`, an editor other than `true`, and `LESSOPEN`. A name
    exported alone counts with its value unknown. In PowerShell an
    assignment to `$env:NAME` anywhere in the line already counts for
    every git command in it.

  The configuration keys checked for turning content on in a command
  the gate allows: `commit.verbose` and `stash.showPatch` (read, above),
  `diff.submodule` (above), `diff.relative` (the name-only form adds
  `--no-relative`), `log.diffMerges` (it applies only with a merge option,
  judged above), `status.*`, `grep.*` (the name-only run of `grep` uses
  the same configuration), `diff.renames` and `diff.noprefix` (they change
  names, which the name-only form reads with the same configuration),
  and `format.*` (`format-patch` is always denied). Keys that run a
  program (`diff.external`, a diff driver's `command` or `textconv`,
  `core.pager` and `pager.*`, `gpg.program`, `core.fsmonitor`,
  `core.hooksPath`, filters) are not judged from the repository's
  configuration: a program they name is not read, as a script a command
  runs is not (threat model). Set with `-c` or in the same line, they
  deny as above. A write in the same line to the user's or the system's
  git configuration (`~/.gitconfig`, `$XDG_CONFIG_HOME/git/config`,
  `~/.config/git/config`, `/etc/gitconfig`, and the files
  `GIT_CONFIG_GLOBAL` and `GIT_CONFIG_SYSTEM` name in the gate's
  environment), or a write whose target cannot be told (an output
  redirection's or `<>`'s target, or a copy's destination, holding an
  expansion: `>> "$(git rev-parse --git-path config)"`), counts as a
  configuration write too. An input redirection and a here-string write
  nothing, under a `.git` directory too (`cat < "$f"; git status`, `cat <
  .git/config; git status`, `read -r x <<< "$(git log
  --oneline -1)"`).
  **Known limitation:** a configuration file a git configuration
  includes (`include.path`, `includeIf`) outside a git directory, written
  in the same line, is not told apart from any other file.
  - `stash show` takes no pathspec: every word not starting with `-`,
    before or after `--`, names the stash (a number `n` is
    `refs/stash@{n}`, none is `refs/stash@{0}`). With at most one such
    word, the gate first asks `git rev-parse --verify --quiet
    --end-of-options <stash>`; exit 1 with no output means the stash does
    not exist, `stash show` fails before printing anything, and the
    command is allowed. Any other answer goes to the name-only run. git
    2.56 made a failing `stash` exit 128 rather than 1, so the absent
    stash is not left to the exit code of the name-only run.

  Each git command runs with an argv, the global options given (but `-C`,
  which becomes its directory), `GIT_PAGER=cat`, `GIT_EXTERNAL_DIFF`
  unset, `GIT_NO_LAZY_FETCH=1`, `GIT_OPTIONAL_LOCKS=0`, standard input
  empty and standard error discarded, within 10 seconds, and all of one
  decision's within 30. A timeout, a failure (any exit but 0 or 1), a
  name holding a control character or output that does not parse
  denies. Every existing deny for options that change what is printed
  (`-c`, `--git-dir`, environment, aliases) still applies.

  A git command whose directory (`-C`, or `cd`) is outside this
  repository's work tree, in another repository or in none, cannot print
  this repository's history: it is allowed unless a word it names (`git -C
  ../other diff --no-index x ../repo/pbip/y`) is a path under a root here.
  A directory that does not exist, a repository whose work tree holds this
  one, and a directory above it count as unknown, and content is denied
  there. A work tree linked to this repository shares its history and is
  judged as this one; a repository nested in the work tree is judged with
  its paths placed under it. (Decided 2026-10-30.)
- **Divergence (nested work trees):** the predecessor judges a path only
  against the protected roots as written from the repository root, so a
  copy of a root in a linked work tree under `.claude/worktrees/`, in a
  submodule or in a nested clone can be read and searched from the main
  repository. pbipx applies the roots relative to each nested work tree as
  well (see "The gate (Claude Code)", nested work trees). The corpus cases
  for it are marked `known = "predecessor"`. (Decided 2026-10-30.)
- **Divergence (case and paths):** on macOS and Windows, whose file systems
  fold case, pbipx compares paths and root names without regard to case
  everywhere the gate compares them (roots, local state, Claude Code
  settings, the repository root itself); a path is classified as spelled
  on disk and with each root as the policy writes it. Read, Grep and Write
  expand a leading `~`. Symlinks are resolved component by component before
  a `..` after them, so `notes/lnk/../x` with `notes/lnk` linking into a root
  is judged where the operating system opens it. (Decided 2026-10-29.)
- **Windows paths:** Claude Code on Windows gives events Windows paths and
  runs Bash tool commands in Git Bash, so the shell rule's POSIX reading
  applies (a backslash outside quotes is an escape) and a path may be
  written `C:\x` (quoted), `C:/x`, `/c/x` or `\x`. pbipx reads each as the
  same place: Git Bash's `/c/x` is `C:\x`, and is also judged as `\c\x` on
  the working directory's drive, where a file tool opens it; `\x` and `/x`
  are rooted on the working directory's drive, and with that directory
  unknown are not placed (the tree is scanned); a drive-relative `D:x` is
  against the working directory when it is on `D:`, else against `D:\`.
  Resolving a path spells each existing component by its long name, so a
  short (8.3) name such as `RUNNER~1` compares equal to the long one git and
  the repository root use, and follows a junction as a link. `~` and `$HOME`
  are `%HOME%` when it is set (in any of those forms), as Git Bash and
  Claude Code read it, and `%USERPROFILE%` otherwise. Paths git prints (the
  work tree's top, its git directory) are resolved the same way before they
  are compared. (Decided 2026-10-30.)
  - **Git Bash mounts.** Git Bash resolves a path that starts with one
    slash and is not a drive (`/tmp/x`, `/usr/x`, a mount in `fstab`)
    through its mount table, so `\tmp\x` on the working drive is the wrong
    place for it. On Windows only, such a word, the value of a
    `--flag=value`, and a Read, Grep, Write, Edit or LSP path are also
    placed by Git Bash's own `cygpath -w`, and judged as written and as
    placed: placement adds a spelling and never removes one. A path that
    does not exist is placed all the same. The word's text is never
    changed, so a script in it (`sh -c '/usr/bin/cat /tmp/x'`) is read as
    written. Off Windows none of this applies and nothing runs.
    - **Never placed, and nothing runs:** the standard devices and
      descriptors, `/dev/null`, `stdin`, `stdout`, `stderr`, `tty`,
      `zero`, `urandom`, `random`, `/dev/fd/N`, `/proc/self/fd/N` and
      `/proc/N/fd/N` (never a file in a repository; `source /dev/stdin`
      is read as such), `/x` and `/x/...` with one drive letter (today's
      reading), `C:/x`, `C:\x`, `//server/share/x`, a relative word, and
      anything in a PowerShell command. A word with a `..` segment is
      not taken for one of these (`/c/../d/x`); it is placed, unless the
      `..` leads into `/dev` or `/proc`. The names `dev` and `proc` are
      matched without regard to case, as Git Bash matches a mount, after
      the dots are applied (`/./proc/self/cwd`, `/tmp/../proc/self/cwd`),
      and a glob in the first segment that could match either
      (`/pro?/self/cwd`, `/p*/...`) is unknown.
    - **Measured:** in Git Bash, `/proc/self/cwd` is the working
      directory, and a read of `/proc/self/cwd/x` reads `x` there, though
      the gate would read the word as a folder on a drive. So every other
      word under `/dev` or `/proc` (`/proc/self/cwd`, `/proc/self/root`,
      `/proc/N/cwd`, `/dev/sda`) is unknown, in a word, a redirection, a
      `cd`, a git argument and a file tool's path, and `cygpath` is not
      asked about it.
    - **Placed:** every other word that starts with one slash, so a
      `/cygdrive/x/...` word keeps its reading as a drive path and is
      placed too, which covers a custom cygdrive prefix. A glob is not
      given to `cygpath`: the directory before it is placed and the glob
      is matched below that. A `..` after a glob, a backslash and a
      control character in the word cannot be placed.
    - **Not a path:** a word in a program's pattern or script slot is not
      placed, though it starts with a slash: sed's and awk's script (and
      their `-e` values; an option the gate cannot classify, or one that names
      a file a program reads as source (`gawk -E`, `--exec`, `-i`,
      `--include`, `mawk -W exec`), leaves nothing after it a script), the
      pattern of `grep`, `rg` and the other
      searches (and the value of `-e` and `--regexp`), `jq`'s filter, and
      the value of `find`'s `-name` and `-path` tests. `sed '/^#.*/d' f`
      and `grep '/api/v[0-9]*' f` are not held up by it. A word a reader
      takes as a file is a file, quoted or not (`cat '/tm*/x'`).
    - **Unknown:** `/`, and any word that cannot be placed (no `cygpath`,
      a run that fails or prints anything but one absolute valid UTF-8
      line, a limit reached). A command word written out in full
      (`/opt/tool`) that cannot be placed is known by its last element,
      as a path with an expansion before it is; one that names a shell,
      an interpreter or a wrapper (`/usr/bin/env`) is not, nor one with a
      glob anywhere in it (`/x/ba?h`, which could be a shell). When `cygpath`
      is not found, the deny says so and names `CLAUDE_CODE_GIT_BASH_PATH`.
      A reading command or a search given one
      touches every protected root, and in a call from outside every
      repository, every indexed work tree; `cd` to one makes the working
      directory unknown; git given one prints no content (`git-content`);
      a file tool path that cannot be placed is denied with
      `internal-error`.
    - **Finding `cygpath`:** beside `CLAUDE_CODE_GIT_BASH_PATH`, then in
      the installation the `git` that runs is in (up to three levels above
      its directory, to `usr\bin\cygpath.exe`), then on `PATH`. The `git` is
      walked up from only in a Git for Windows
      layout (a `git.exe` in `cmd` or `mingw64\bin` beside `usr\bin`), so a
      shim in a user's folder leads nowhere; a candidate on `PATH` under the
      home directory or a directory that holds a `.git` is passed over
      unless it is in such a layout (a per-user installation is). A candidate
      inside the working directory, the repository that holds it or a
      repository loaded for the call is passed over: a repository could
      plant it. It runs with an
      argument vector and no shell, `MSYS=noglob`, `LC_ALL=C.UTF-8`, in its
      own directory, for at most 2 seconds a run, 32 runs and 10 seconds a
      decision, and the result of each word, a failure included, is kept for
      the decision. Nothing is cached on disk.
    - **Measured, not assumed.** In a default Git for Windows there is no
      `/cygdrive` drive mount: `cygpath -w /cygdrive/c/x` printed a folder
      under the installation and listing `/cygdrive/c` failed. A read
      through that form fails there and leaks nothing, so the gate's
      reading of it as `C:\cygdrive\c\x` is wrong but harmless; only a
      custom `cygdrive` prefix or mount makes it a drive, and `cygpath`
      says which.
    - **Limitations.** The mount table is not read, only asked about one
      word at a time, so `/` is unknown and a directory word with a mount
      below it that the gate cannot see (`ls /usr`, with a repository
      mounted at `/usr/src`) is not told from one without. A mount that
      `fstab` changes between the gate's run and the command's is not seen.
      Git's own pathspec magic in front of a path (`:(top)/tmp/x`) and a
      `REV:/tmp/x` word are not placed. `PATH`-style lists
      (`PATH=/a:/b`) are not split. A `git -C` or pathspec word is judged
      as written and as placed, so a placed path is still denied when the
      path as written would be (`git -C /tmp/x log -p` is denied while
      `/tmp/x` does not exist as a folder on the working drive).
      Every command word that starts with a slash (`/usr/bin/env`) is
      placed, so one run is spent on it. A glob below a directory that
      holds only a mount (`cat /usr/*/x`, where a mount lies below `/usr`)
      is matched in the folder `cygpath` names for the directory, so it
      does not see what the mount holds. `/tmp` is placed by the gate's own
      environment (`TEMP`), which the hook and the Bash tool might not
      share. A double slash (`//proc/self/cwd`) is a UNC path to Windows
      and is not placed, and a backslash spelling (`'\proc\self\cwd\x'`)
      is read as the backslash it is. Measured in Git Bash, neither reaches
      the working directory: each is "no such file or directory". A test
      logs what the gate decides for them. (Decided 2026-11-04.)
- **Known over-blocks of the shell rule, in full.** Each denies though
  the line may print nothing protected; each is the price of a rule
  above, and each has a plain form that is allowed:
  - code built at run time (`git-content`):
    - `eval "$(pyenv init -)"` and any `eval`, `bash -c`, `sh -c` or
      `source` of text built at run time: `eval "$(ssh-agent -s)"`,
      `eval "$(direnv export bash)"`, `eval "$(pyenv init -)"`;
    - a shell reading a script from a pipe the gate cannot read: `curl
      https://example.com/install.sh | sh`, `wget -O- https://... |
      bash`;
    - an echo whose text holds a backslash, with neither `-e` nor `-E`,
      fed to a shell where the echo might read escapes (the top level, any
      `sh -c`; `echo 'git log \055p' | bash`);
    - a script fed to a shell by `echo` or `printf` that the gate does
      not read exactly: an escape outside its tables (`\x`, `\0NNN`,
      `\NNN`, `\u`, `\U`, `\c`, `\e`, and `\"` or `\'` in an `echo -e`
      or `%b` argument), or a conversion but `%s`, `%b` and `%%`
      (`printf '%5s\n' x | sh`, `printf '%d\n' 1 | bash`);
    - a command word whose value cannot be told and could split, or
      whose last element is a shell, an interpreter or a wrapper
      (`"$(dirname x)/bash" -c ...`), and an unknown word in the program
      position or after a wrapper option the table does not know:
      `$CMD test`, `env $X go test`, `nice --frobnicate "$X" cmd`, a
      function that runs `"$@"` (`run() { "$@"; }; run go test ./...`), a
      loop that runs what it reads (`while read l; do $l; done <
      cmds.txt`);
    - quoted code that can become a variable's value (a `$(`, backtick
      or `${` in an assignment, a value setter's word, a `for` list, a
      substitution's output) in a line with any arithmetic context or
      evaluated name, though the value is never evaluated
      (`v='${HOME}'; echo $((n+1))`), and any quoted code in such a line
      once `read` or `mapfile` reads a value from a file;
  - git's history and index (`git-content`):
    - a git command after one not known to be read-only that could print
      content with no pathspec proof: `git fetch && git diff --cached`,
      `git branch --contains X && git diff --cached`, `git switch main
      && git pull && git diff`, `git add -A && git show HEAD -- src`,
      `git reset && git diff`, and, in PowerShell, a summary of `HEAD`
      after a commit;
    - a read-only git form the allowlist does not hold (`git apply
      --check`, `git branch --contains X`, `git tag --points-at X`),
      which then counts as a change;
    - a git command that puts history in the working tree, then any
      command that may write, then a working-tree print (`git checkout
      main && make && git diff`), whatever the tree holds;
    - a write to a target that cannot be told before git (`echo x >
      "$LOG"; git log --oneline -1`, `go test | tee "$LOG"; git status`),
      which may be git's configuration;
    - git given a word the gate cannot expand where it could be an
      option: `git commit -m "$msg"` through PowerShell (`git commit -m
      $m`), `xargs git log`, and `xargs git add`, `git rm` and `git
      restore` (put `--` before the input words: `xargs git add --` is
      allowed; the message says so);
    - `gh pr diff` (a remote patch; `--name-only` is allowed), `git
      checkout X && git diff` and `git stash apply && git diff` (a change
      that puts history in the tree, then a print), and a line that reads
      a positional parameter or OPTARG and evaluates arithmetic, with any
      quoted code in it (`git commit -m 'Use `x`'; echo $(( $1 ))`);
    - names fed to a reader by `xargs` or `parallel`, whatever they
      name: `git ls-files | xargs cat` scans the tree (a clean tree
      allows), `xargs -I{} sh -c 'jq . {}'` likewise;
  - a tree scan (`finding` while a root holds one):
    - `echo pbip` and `printf` naming a root;
    - a program not in the table of programs that leave their operands
      unread, given a word the gate cannot tell, or fed names by a
      runner: `curl -H "$H" https://...`, `docker run "$IMG"`, `gofmt -l
      "$d"`, `git ls-files | xargs gofmt -l`, `shuf "$f"`, and the build
      and package tools: `go test "$pkg"`, `make "$target"`, `npm run
      "$script"`, `pip install "$p"`, `date -d "$when"`. A clean tree
      allows; a leaky one denies;
    - an input redirection from a file the gate cannot tell, whatever the
      program: `sort < "$f"`, `while read l; do ...; done < "$f"`;
    - an Edit, MultiEdit or NotebookEdit of a file with a finding, even
      though Claude Code requires it be read first;
    - a git command that prints the working tree after any command not
      known to write nothing (`make fmt && git diff`), even when that
      command wrote nothing, unless the print's pathspecs after `--`
      provably avoid every root: `git diff src/main.go` without `--`,
      and `git status -vv -- src`, scan;
    - any git command before such a print in a repository with a
      `post-index-change` or `reference-transaction` hook (`git status
      && git diff`);
  - lines the gate cannot read as bash would (`internal-error`):
    - a backslash-newline between two operator characters that join into
      one redirection operator (`echo x >\<newline>> log`);
    - a here-document delimiter holding `$` or a backtick;
    - a backslash-newline in an unquoted here-document body next to its
      delimiter line or in a line with a `$(` or a backtick
      (`cat > build.sh <<EOF` with `x=$(date) \<newline>`).

### Baseline

- Recording an acceptance under `CI` or `PRE_COMMIT` is an operational
  refusal (exit 3). Recording a block or an unknown finding is a usage error
  (exit 4).

## Known predecessor bugs

Found by the predecessor's corpus runner (2026-10-29). Each case is marked
`known = "predecessor"`. pbipx handles them; the predecessor misses them
until cutover replaces it. This list is the record of them; there is no
separate issue tracker.

Fixed in the predecessor since, and unmarked: SQL backslash escapes and `#`
comments (2026-10-29).

Re-run 2026-11-03 over 355 cases (the `explain` cases are not run there):
no new predecessor bug. Four failures were documented divergences not yet
marked (adoption, and git's name-only check) and are now marked; eight
marked cases the predecessor passes, mostly because it denies every git
command that prints content, are now unmarked.

| Case | What the predecessor misses |
|---|---|
| `tmdl/dax-function-body-blocks` | a DAX user-defined function body is judged as free text, without its columns |
| `tmdl/query-partition-sql-comparison-blocks` | a legacy `query` partition's SQL is judged as free text, not lexed |
| `sql/dollar-quoted-value-on-block-column-blocks` | a `$$` string is lexed as code |
| `yaml/alias-takes-site-association` | an alias does not take the key at the alias site |
| `yaml/tagged-scalar-judged-by-text` | tagged nulls and booleans are skipped; tagged numbers get no shape checks |
