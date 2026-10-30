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
  the working directory alone does not reach below itself. When the call
  touches something that cannot be placed (the shell rule's "touches
  every root": a working directory that cannot be told, a reading
  command's word that cannot be placed, a line nested too deeply or built
  at run time), every indexed work tree counts. The repositories that hold
  the working directory are judged first, innermost first; any deny wins.
- A call that touches **no adopted repository** is allowed: in a
  repository that has not adopted pbipx, and outside every repository. One
  exception holds everywhere: a call that changes what controls the gate
  (a Write into the approval store or to a user-level or managed Claude
  Code settings file, a Bash command naming one or writing into a
  user-level settings directory, a PowerShell command naming one or such a
  directory, or one running `pbipx policy approve`) is
  denied with `agent-settings`, since from any directory it would switch
  the gate off, or forge an approval, for every adopted repository.
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
reach is a person's edit of the store.

The event is JSON on stdin. Every failure denies:

- the event does not parse, or is not an object;
- in an adopted repository, the working directory is not in a work tree
  (adoption, above);
- the policy of an adopted repository the call touches fails to load. It
  is loaded before anything is classified, so a broken policy denies every
  call there, not only calls near a protected root;
- any other error.

**Divergence (policy integrity).** In pbipx, the first check after the
policy loads, before the baseline or the tool is looked at, is its approval
([ADR-0006](../adr/0006-policy-integrity.md)). The SHA-256 digest of
`.pbipx.toml`'s exact bytes must be one a person recorded with
`pbipx policy approve` for this clone on this machine. With no approval,
or a policy changed since (a comment or whitespace edit included), every call
is denied with `policy-unapproved`. An approval store that cannot be found
or read denies with `internal-error`. A policy that fails to load still
denies with `policy-error`, since that check runs first. The predecessor has
no integrity check; the corpus cases for it are marked
`known = "predecessor"`.

`pbipx policy approve` is refused with a usage error (exit 4) unless stdin
and stdout are terminals and none of `CI`, `PRE_COMMIT` and
`PBIPX_NO_REVEAL` is set (set but empty counts). It loads the policy first
(a policy error exits 3 and approves nothing), shows `.pbipx.toml` and its
digest, and records the approval only when `approve` is typed in full;
anything else exits 4. `pbipx policy status` prints `approved`,
`unapproved`, `changed` or `missing` (no policy file), and exits 0 only
when approved, 3 otherwise. Neither prints policy content.

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
| Write, Edit, MultiEdit | `file_path` | Write, below |
| NotebookEdit | `notebook_path`, else `file_path`; one is required | Write, below |
| Glob | not gated | prints names only |
| any other | not gated | allowed |

A PowerShell call with no `command` string, or a NotebookEdit with no
path, is denied with `event-error`: the call cannot be judged.

- **Read** of `file_path` (in pbipx, a leading `~` expanded from the gate's
  home directory, and the file judged where the operating system opens it,
  symlinks before `..`): allowed if it is empty, outside the repository, a
  directory, outside the protected roots and not in scan scope, or
  allowlisted. Denied if it is local state or unvettable, whether or not it
  exists. Allowed if it does not exist. Otherwise the file is scanned, strict,
  with the baseline: denied on a block or an error, allowed on warnings.
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
    carries forward). `cd` to a target that cannot be told (`cd $X`,
    `cd -` with no earlier `cd`, `popd`, a bare name while `CDPATH` is set,
    `cd` with `HOME` unknown) makes it unknown;
  - `~`, `$HOME`, `${HOME}` (the gate process's home directory), `$PWD`,
    `${PWD}` (the working directory as the line leaves it) and `$OLDPWD`
    are expanded; any other expansion (`$X`, `${X:-y}`, `$1`, `$(...)`,
    backticks) leaves its word unknown;
  - a here-document (`<<WORD`, `<<-WORD`) is read as bash reads it: its
    body runs from the line after the operator to the line that is the
    delimiter (leading tabs removed for `<<-`), or to the end of the line
    when there is none, and the command line goes on after it. A body is
    data, never commands: with a quoted delimiter (`<<'EOF'`, `<<"EOF"`,
    `<<\EOF`) all of it; with an unquoted one, all but its command
    substitutions (`$(...)` and backticks), which are read as nested lines.
    A here-document inside a command substitution is read the same way;
  - with unbalanced quotes or a trailing backslash, the line is split on
    whitespace, at newlines and at `;`, `&`, `|`, `(` and `)` whether or
    not a space sets them apart, quotes kept as written. A reading or
    searching command in such a line, or any `$(` or backtick in it,
    counts as touching every protected root;
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
    `caffeinate`, `unbuffer`, `chronic`, `taskset`, `flock`, `coproc`,
    `busybox`, `xvfb-run` and a few tracers and sandboxes. Their long
    options that take a value given apart (`timeout --signal KILL 5`) are
    read as such. `command -v` and `-V` run nothing. Any other program,
    unless it is one known never to run its words (`echo`, `which`, `man`,
    package managers and the like), whose first word that is not an option
    is a reader, a search, git, `find`, `fd`, a shell or `eval`, is judged
    as that program: an unknown wrapper is judged by what it runs;
  - `sh`, `bash`, `zsh` and kin with `-c` (after `--`, and after
    long options that take a value such as `--rcfile`), `script -c`,
    `flock FILE -c`, `watch` (its words, joined), `env -S`, GNU `parallel`
    (its template with each argument after `:::`, or each argument alone),
    `eval` and every command substitution are read as nested lines, at
    most four deep, with every rule applied to them. So is the script a
    shell reads from a here-document, a here-string (`<<<`), `source
    /dev/stdin`, or a pipe from a here-document, a here-string, `echo` or
    `printf`; a shell reading any other pipe runs a script built at run
    time. A deeper line, or one whose text is built at run time
    (`bash -c "$CMD"`, an unquoted here-document with an expansion), counts
    as touching every protected root. `eval`'s `cd` moves the line after
    it; the others' does not;
  - there is no parser for PowerShell or `cmd`: `pwsh`, `powershell` or
    `cmd` with any argument but a version or help request, or reading a
    pipe, counts as touching every protected root. So do `ssh` with a
    command after the host or reading a pipe, `su` or `runuser` with a
    command, and `machinectl shell`.
  1. The tool itself alone (one simple command, optionally after environment
     assignments, with no `$(` or backtick) is allowed: it redacts on its own.
     `policy approve` is never the tool alone (below).
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
     - `stash show` with a patch flag, or with no summary flag when
       `stash.showPatch` is true; `cat-file` except `-t`, `-s`, `-e`,
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
     HEAD's content, which is history.
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
       one. That includes the value of a `--flag=value`, what an unquoted
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
     - a nested line is too deep or built at run time.

     A root's name is compared without regard to case in these tests, on
     every platform: they only decide whether the real roots are scanned.

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
  - runs `pbipx policy approve` (`policy`, then `approve`, by any path to
    the tool, through a pipe, or under `script`, `expect` or `unbuffer`,
    which give it a terminal): approval is a person's step (ADR-0006).

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
  - any word naming local state denies with `local-state`;
  - otherwise the protected tree is scanned, as for Grep: a block or an
    error anywhere there denies, a clean tree allows.

  A line of more than 20,000 words is denied with `internal-error`. A
  PowerShell call that builds a path at run time (`Join-Path`, a variable
  set from a computation) reaches only what its words show; this is the
  same limit as for Bash (threat model).
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
    (case ignored) directly in `$CLAUDE_CONFIG_DIR` or in `~/.claude`;
  - inside Claude Code's managed settings directory (`/etc/claude-code`,
    `/Library/Application Support/ClaudeCode` or
    `C:\Program Files\ClaudeCode`), `managed-settings.json` and the
    `managed-settings.d` drop-ins included.

  The message says which of these the path is, and does not name it. Every
  other write is allowed: the gate does not otherwise judge writes.

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
  told, a line is built at run time or nested too deep, `find`, `fd` or
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
  root, or the reason the content cannot be placed (rule 2);
- a read after a tree change names the command that changes the tree.

Programs are named only when they are in the gate's tables (a reader, a
search, `git`'s own subcommands); a root is named from the policy. The
hints are fixed sentences: for a finding in a report's selection, "Clear
the selection in Power BI Desktop and save, or ask a person to run `pbipx
scan` at a terminal."; for a search, "Name the files to search
explicitly, or exclude the protected folders."; for git, "Limit the paths
(`-- src`) or use --stat."

Gate reason classes: `finding`, `unvettable`, `scan-error`,
`local-state`, `agent-settings` (pbipx only), `git-content`, `policy-error`,
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
  denies (see "The gate (Claude Code)", Bash). (Decided 2026-10-30.)
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
    name-only form.
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

| Case | What the predecessor misses |
|---|---|
| `tmdl/dax-function-body-blocks` | a DAX user-defined function body is judged as free text, without its columns |
| `tmdl/query-partition-sql-comparison-blocks` | a legacy `query` partition's SQL is judged as free text, not lexed |
| `sql/dollar-quoted-value-on-block-column-blocks` | a `$$` string is lexed as code |
| `yaml/alias-takes-site-association` | an alias does not take the key at the alias site |
| `yaml/tagged-scalar-judged-by-text` | tagged nulls and booleans are skipped; tagged numbers get no shape checks |
