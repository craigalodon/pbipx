# `explain` output

`pbipx explain <paths...>` prints the structure of reports, models and SQL so
an agent never needs to read a protected file to learn its shape (principle
11). It prints names, types, relationships, storage modes, sources and counts.
It never prints a value, and it has no flag that would.

Settled 2026-10-29. The behavior it follows is in
[behavior.md](behavior.md#explain).

## Output

With `--format json`, one entry per file, in path order. A directory expands
to the files under it.

```json
{
  "files": [
    {
      "path": "pbip/Contoso.Report/definition/pages/p1/visuals/v1/visual.json",
      "kind": "report",
      "contexts": [
        {
          "context": "slicer-selection",
          "strings": 1,
          "columns": ["student.full_name"]
        }
      ]
    },
    {
      "path": "pbip/School.SemanticModel/definition/tables/student.tmdl",
      "kind": "model",
      "tables": [
        {
          "name": "student",
          "columns": [
            { "name": "full_name", "type": "string", "calculated": false }
          ],
          "measures": ["Student Count"],
          "partitions": [
            {
              "name": "student", "source": "m", "mode": "import",
              "connectors": ["Sql.Database"],
              "reads": ["warehouse.dbo.student"],
              "native_sql_reads": []
            }
          ]
        }
      ],
      "relationships": [
        { "from": "enrollment.student_id", "to": "student.student_id" }
      ],
      "roles": [ { "name": "Advisor", "filters": ["student"], "members": 2 } ],
      "expressions": [ { "name": "Server", "kind": "parameter" } ],
      "model": { "default_mode": "import" },
      "literals": { "tmdl-dax": 1, "tmdl-m": 3 }
    },
    {
      "path": "sql/views/roster.sql",
      "kind": "sql",
      "creates": [ { "kind": "view", "name": "analytics.roster" } ],
      "writes": [],
      "reads": ["warehouse.dbo.student"],
      "reads_unqualified": ["active"],
      "literals": { "sql": 2 }
    },
    {
      "path": "pbip/School.SemanticModel/definition/tables/broken.tmdl",
      "kind": "model",
      "error": "unparseable-tmdl"
    },
    {
      "path": "pbip/Contoso.Report/StaticResources/logo.png",
      "kind": "not-explained"
    }
  ]
}
```

- `kind` is `report`, `model`, `sql` or `not-explained` (any other file,
  YAML included).
- **Reports:** each data-bearing context, with the number of string
  literals it holds and its column names, sorted.
- **Models:** tables with columns (data type, calculated or not), measures
  and partitions (source type, storage mode, connectors, the navigation path
  read, and relations native SQL reads); relationships; roles with the
  tables they filter and a count of members, never the members; named
  expressions as `parameter` or `query`; the model's default mode; a count
  of literals by context. A partition's navigation can use a parameter
  defined in another file, so all model files given are parsed before any
  is described.
- **SQL:** relations created (view or table), written and read, and a count
  of literals by context. A template's `ref()` and `source()` calls are
  reads.
- Empty lists are written as `[]`; absent sections are omitted.

## Names and placeholders

Names are schema and are printed, with three exceptions, each replaced by a
placeholder:

| Placeholder | When |
|---|---|
| `<withheld>` | a model object name (table, column, measure, role, partition, expression) with any personal-data shape |
| `<not an identifier>` | a part of a relation name, or a navigation step, that is not a plain identifier (letters, digits and `_`, not all digits) or has a shape |
| `<dynamic>` | a template call whose arguments are not quoted names |

A report's column names come from the file's `Column` references and pass
the same test as model names.

## Errors and exit codes

A file that does not parse gets an entry with `error`, the error reason from
the vocabulary, and nothing else. The command then exits 3, as `scan` does,
so a partial answer is never mistaken for a full one. Otherwise it exits 0.
Usage errors exit 4. Without `--format json` the same content is printed as
indented text.
