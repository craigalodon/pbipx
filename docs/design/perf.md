# `pbipx perf`: Performance Analyzer summaries

> **Status: draft.** This is the specification of a command that is not built
> yet (phase 6 of the [roadmap](../ROADMAP.md)). The gate already denies a
> Read of an export ([behavior.md](behavior.md), "Files and the default
> deny"); this document says what the command that replaces that read may
> print. The export's field names below are from memory of its general shape
> and need confirming against a real export, schema only, before the command
> is built.

## Why

Power BI Desktop's Performance Analyzer exports a JSON file of the events a
report page ran: each visual's queries and render steps, with timings. The
file also holds the **text of every query** and the **titles of the visuals**.
Query text carries the filter values a report applied, and a title is typed
text. Both can hold a value (principle 1), and no scanner vets the file
(principle 8), so an agent cannot read it. The questions an analyst asks of it
are structural: which visuals are slow, which step dominates, how many queries
ran. `perf` answers those from the file and prints nothing else.

## Input

`pbipx perf <export.json>`, one file, read from a path. The file is held in
memory as bytes and decoded once. It is recognized by the same shape check
the gate uses (`internal/gate/perfexport.go`, to move to a shared package in
the change that builds the command), so the command and the deny agree on
what an export is.

An export that is not UTF-8 or UTF-16 text, is not one JSON object, or does
not have the expected shape is an error (below), not a summary.

## Output

Counts and durations, grouped by names that are schema, never by anything the
report's author or its data typed. Each group is a row of numbers, plus a key
that comes from a **closed list** the command compiles in.

| Key | Source | Printed as |
|---|---|---|
| Event kind | the event's `name`, only when it is on the list of known kinds (below) | the kind |
| Component | the part of Power BI the event ran in (the event's component), only when it is on the list | the component |
| Visual type | the visual's type (`card`, `slicer`, `tableEx` ...), only when it is on the list of built-in types | the type |

For each key, and for each pair of event kind and visual type:

- the number of events;
- the total, mean, median, 95th percentile and maximum duration, in
  milliseconds, from the event's start and end;
- the share of the whole export's duration.

Also, once per export: the format version (only when it is a version number
the command knows), the number of events, the number of distinct visuals (a
count, not their ids), and the span from the first start to the last end.

A kind, component or visual type not on the list is **not dropped from the
numbers and not named**. It is counted under the key `other`, so the totals
add up and the output cannot be used to read a custom visual's name, a
page's name or a title.

### Never in the output

- Query text, in any field that holds it.
- Visual titles, page names, report names, and every other string the author
  typed.
- Event, visual and parent ids, which key into the report.
- Any field the command does not know. The decoder reads **only** the fields
  the table above needs, by an allowlist; a field that is present and not on
  it is skipped without being kept, and a field's value is never copied into
  an error, a log or the output.
- A hash, a length or a prefix of any of these (principle 3; principle 2 on
  partial masking).

The output uses the same type discipline as the scanner: a string read from
the file is a `secret.Value` until it has been matched against a compiled-in
list, and only the list's own constant is printed. There is no
`--show-values` for `perf`, and no flag that prints anything the table above
does not.

## Formats

A table for a terminal, and `--format json` for tools, with the same fields.
The JSON has a `version` of its own for the summary's schema, and no free
text. A number is printed as a number.

## Failing closed

Every case below prints a fixed message with the file's path and a class, and
exits 3. None prints a byte of the file, an offset into it, or the decoder's
own error text, which can quote source text (principle 2).

| Class | When |
|---|---|
| `not-an-export` | the file does not have the shape of an export |
| `unknown-version` | the format version is missing, or is not one the command knows |
| `unknown-shape` | the version is known and the structure is not what it specifies: a field of the wrong type, an event without a start or end, an end before a start, a list that is not a list |
| `unreadable`, `not-utf8` | as for the scanners |
| `too-large` | the file exceeds the command's size limit |

A version the command does not know is an error, not a best effort: a newer
Performance Analyzer could add a field that holds text, and a decoder that
guessed would have to guess whether to print it. Adding a version is a
reviewed change with a new fixture.

The decoder never returns a partial summary. A summary of part of an export
would be a wrong summary.

## Who may run it: the agent

`perf` joins the gate's table of subcommands an agent may run alone
([behavior.md](behavior.md), "The gate", Bash rule 1), beside `scan`,
`explain` and `baseline`: its output is redacted by construction, as theirs
is, and the point of the command is that an analyst can ask an agent for the
summary. This is the plan's recommendation and is decided here.

The change that builds the command adds `perf` to that table in one line, with
its gate case (`bash-pbipx-perf-denies` becomes an allow, still marked
`known = "predecessor"`). Until then the command is off the table and the
ordinary rules judge it.

The agent runs `perf` on a file it may not read: the gate denies a Read of it,
a shell or PowerShell command that reads it (`cat`, `jq`, `grep`, an input
redirection) and a tree scan that reaches it, all with `perf-export`, and
`pbipx perf <file>` is the way through. That is the
intended shape (principle 8: the capability arrives after the deny, in a
reviewed change). The command takes a path, so the gate's rules for a path
word apply to its argument as for any command.

Two follow-ups belong to the change that builds the command. `pbipx perf
<file> | head` is not the tool alone, so its argument would be checked as a
reader's operand and denied: the check must skip the tool's own arguments.
And the gate's rule that a search with no root in its paths reads nothing it
can name (behavior.md, "Not covered") leaves a recursive search of such a
directory able to read an export; closing it needs the search rule to say
which files it reads.

## Tests the command needs

Written before the decoder, as for everything here.

- A corpus area `perf`, with fixtures written by hand from the shape above and
  planted values (a query with a literal, a visual title with a name) listed
  as sentinels that must not appear in stdout or stderr.
- A fixture per failing-closed class, and a fixture with an extra field whose
  value is a sentinel, which must be skipped.
- A fuzz test over the decoder: whatever bytes it is given, its output and
  its errors hold none of the input's string values.
- The deny and the command agree: every fixture the command summarizes is
  denied by the gate when read, and every file the gate lets through is
  refused by the command as `not-an-export`.

## Open questions for the maintainer

- The real field names, versions and event kinds, from a real export's
  schema only: no values, no file.
- Whether the export is UTF-8 or UTF-16 on Windows, and whether it has a
  byte order mark. The gate accepts both.
- Whether a `--compare a.json b.json` (the same page before and after a
  change) belongs in the first version. It is the question analysts ask
  next, and it adds no new field to the output.
