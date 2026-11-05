# Sample PBIP: a school report to demo pbipx

A small, invented Power BI project for trying pbipx: a report with a person
slicer, a card and a bar chart, over a three-table school model (`student`,
`course`, `enrollment`). Everything in it is synthetic: Contoso names, a
source on `example.cloud`, all-zero GUIDs, and no data at all. It contains no
value for pbipx to find, until you plant one.

```text
examples/sample-pbip/
  project/    the clean project: copy this out, it is your repository
  planted/    one file that plants a finding; copy it over the clean one
  README.md   this walkthrough (not part of the project)
```

## Why the finding is a separate file

The clean project is what is tracked as the project. The finding is one
file, `planted/visual.json`: the slicer's file with a saved selection of the
fake person `Doe, Jane`. It sits outside `project/` so that:

- the project you copy out is clean, and its first scan is clean;
- the finding appears only when you copy the file over, which is the moment
  worth showing live;
- this repository holds no `.pbipx.toml` and no protected folder, so nothing
  here is "adopted" and no check treats the planted value as a leak. The
  planted file is not under a `*.Report` folder, so `pbipx init` run in this
  repository would not mistake it for part of a project.

`pbipx init` writes `.pbipx.toml` into your copy. Do not add one to
`examples/`: a policy that is not at a repository's root is ignored, and a
copy of the sample inside another repository would not be protected by it.

## Walkthrough

You need `pbipx` on your `PATH` ([install](../../README.md#install)), git, and
a terminal. Copy the project out of this repository first, to a place that is
not inside another git repository. The commands are for a POSIX shell; on
Windows use Git Bash.

1. Make a fresh repository from the clean project.

   ```sh
   cp -r examples/sample-pbip/project ~/school-demo
   cd ~/school-demo
   git init
   git add .
   git commit -m "Add the school report"
   ```

2. Set up pbipx. This writes `.pbipx.toml`, which protects the `pbip`
   folder, and registers the gate as a Claude Code hook in your user
   settings.

   ```sh
   pbipx init
   ```

3. Review the policy, above all the `[columns]` lists: `full_name` and
   `student_id` are direct identifiers. Then approve it. This needs a real
   terminal.

   ```sh
   pbipx policy approve
   git add .pbipx.toml
   git commit -m "Add the pbipx policy"
   ```

4. A clean read is allowed. In a new Claude Code session in this folder, ask
   it to describe the slicer's file. To see the gate's verdict without an
   agent, give it the hook event an agent would send. Exit code `0` is allow.

   ```sh
   F=pbip/School.Report/definition/pages/5d7a0c2e9b1f4a368d02/visuals/1b4e7a0d3c6f9285e1a7/visual.json
   read_event() {
     printf '{"hook_event_name":"PreToolUse","cwd":"%s",' "$PWD"
     printf '"tool_name":"Read","tool_input":{"file_path":"%s/%s"}}' "$PWD" "$F"
   }
   read_event | pbipx gate
   echo "exit $?"
   pbipx scan
   ```

   The scan reports `0 blocks`.

5. Plant the finding. This is what Desktop does when someone leaves the
   slicer on one student and saves.

   ```sh
   cp /path/to/examples/sample-pbip/planted/visual.json "$F"
   ```

6. The gate now denies the read (exit code `2`), and the scan blocks (exit
   code `1`). Neither prints the name: the scan shows `value <redacted>`, the
   column `student.full_name` and the slicer's context.

   ```sh
   read_event | pbipx gate
   echo "exit $?"
   pbipx scan
   ```

7. Ask for the structure instead. `pbipx explain` prints names, never
   values, so an agent can learn the shape of the file without reading it.

   ```sh
   pbipx explain "$F"
   pbipx explain pbip/School.SemanticModel/definition/tables/student.tmdl
   ```

8. Reset for the next run: `git checkout -- "$F"` puts the clean slicer
   back.

## What is checked

`go test ./examples/` runs these steps on a temporary copy, with temporary
`PBIPX_CONFIG_DIR` and `CLAUDE_CONFIG_DIR`. It records the approval the way
the behavior corpus does, since a test has no terminal, and asserts every
outcome above. It also asserts that no planted value appears in any output
of any command.

## Opening it in Power BI Desktop

Open `pbip/School.pbip`. The source is a made-up server, so do not refresh:
the visuals stay empty, which is fine. When Desktop saves, it may add or
rewrite files, and some are not safe to commit.

- `.pbi/` folders (the data cache, `localSettings.json`): ignored by the
  project's `.gitignore`.
- Anything naming a tenant, workspace, user or sensitivity label: do not
  commit it. Run `pbipx scan` and read the diff before every commit.
- Desktop writes CRLF; the project's `.gitattributes` keeps LF. Report JSON
  has no final newline, so no whitespace hook may touch it.
