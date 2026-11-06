# pbipx guide for analysts

This guide is for people who build Power BI reports in Power BI Desktop,
use git a little, and have started to use an AI coding agent such as Claude
Code. You do not need to know anything about security.

pbipx is pre-alpha. Try it on the sample project first
([below](#try-it-on-the-sample-first)).

The messages here are real output from the sample project. To fit the page,
long lines are wrapped and long folder names are shortened with `...`.

## The problem

When you save a report as a Power BI project (PBIP), Desktop writes real
values into the files, not only the layout. If you leave a slicer on one
person and save, that person's name is now in a file. Commit the file and the
name is in git history for good. Let an agent read the file and the name is in
a chat transcript you cannot take back. Desktop does not warn you.

pbipx checks for those values before you commit and before an agent reads
them. It never prints a value it finds. It shows where the value is, not what
it says.

Some words used below:

- **Finding.** A value pbipx thinks is personal data, with the file and the
  column it came from. A finding is a *block* (stop), a *warning* (look at
  it) or an *error* (pbipx could not check the file).
- **Protected root.** A folder pbipx watches, normally the folder that holds
  your `*.Report` and `*.SemanticModel` folders.
- **Gate.** The part of pbipx that sits between your AI agent and your
  files. Each time the agent tries to read a file, search or run a command,
  the gate decides: allow or refuse. A refusal is not a crash. It is the gate
  doing its job.

## Install

Releases are built for Linux. Download the archive for your machine from the
[releases page](https://github.com/craigalodon/pbipx/releases), check it as
the [README](../README.md#install) describes, and put the `pbipx` program in a
folder on your `PATH` that is outside your repository, such as
`~/.local/bin`. It has to be outside so an agent working in the repository
cannot change it.

On Windows and macOS there is no current release. The code is written for
them but has only been tested there by hand. Build it from source with Go:

```sh
go install github.com/craigalodon/pbipx/cmd/pbipx@latest
```

Make sure the folder Go installs into (`~/go/bin` by default) is on your
`PATH`, and that it is outside any repository. Check it with `pbipx version`.

## Try it on the sample first

[`examples/sample-pbip/`](../examples/sample-pbip/README.md) is a small
invented school report with no real data in it. Its walkthrough copies it to a
new folder, sets pbipx up, then plants a fake student's name so you can watch
pbipx catch it. The examples in this guide use that project, so you can
follow along.

## Set up a repository

Do this once per repository, in a terminal, inside the repository that holds
your Power BI projects.

1. Run `pbipx init`. It does three things:
   - writes `.pbipx.toml`, the **policy**: the rules pbipx follows. It finds
     the folders that hold your `*.Report` and `*.SemanticModel` folders and
     protects them (in the sample, `pbip`);
   - tells pbipx's gate about this repository, on this machine;
   - registers the gate with Claude Code, in your user settings.

   Then start a new Claude Code session. A session that was already open
   keeps the hooks it started with.

   On a shared machine, or for a team, ask your administrator about
   `pbipx init --managed`. A project can switch off user-level hooks, and the
   managed setup cannot be switched off that way.

2. Open `.pbipx.toml` and review it. The part that matters is `[columns]`.
   It lists column names that identify a person (`full_name`, `email`,
   `student_id` and so on). A value saved from a column on the `block` list
   is a block. A column on the `warn` list gives a warning. The lists are a
   generic start. Add your own models' identifying columns, and remove
   what does not apply.

3. Approve the policy: run `pbipx policy approve` at a real terminal. It
   shows you a fingerprint of the file and asks you to type `approve`. Until
   you do, the gate refuses everything in the repository, so an agent cannot
   quietly loosen the rules. Approve again after every change to the policy.
   An agent cannot do this for you: the command refuses to run anywhere
   except a terminal you are typing in.

   ```text
   $ pbipx policy status
   .pbipx.toml: approved
   ```

4. Commit `.pbipx.toml`.

5. Add the commit hooks. They scan what you are about to commit and the
   commit message. They use [pre-commit](https://pre-commit.com), a separate
   tool you install once. Add this to `.pre-commit-config.yaml`, using a
   release tag for `rev`:

   ```yaml
   - repo: https://github.com/craigalodon/pbipx
     rev: <a release tag>
     hooks:
       - id: pbipx-scan
       - id: pbipx-commit-msg
   ```

   Then run:

   ```sh
   pre-commit install --hook-type pre-commit --hook-type commit-msg
   ```

   From now on, a commit that holds a block stops with the same report
   `pbipx scan` prints (see [Scan yourself](#scan-yourself)).

If you want `.pbi/`, `*.pbix` and `*.abf` kept out of git, run
`pbipx init --gitignore`. Desktop keeps a cache of real data in those.

## A day's work

You build in Desktop, and you ask your agent for help with the files. Most of
the time you will not notice pbipx. This is what you see when it steps in.

A refusal reaches you as a message from Claude Code, such as
`pbipx: finding: ...`. The word after `pbipx:` is the reason. The agent reads
the message too, and usually knows what to do. If it does not, the sections
below say what is going on.

pbipx does not stop the agent from reading files that are clean.

### A saved selection in a report

You left a slicer on a person, and the agent tried to read that file:

```text
pbipx: finding: pbip/.../visuals/1b4e7a0d3c6f9285e1a7/visual.json has a
  block finding (context slicer-selection; column student.full_name; reasons
  block-column, person-name-shape). Use `pbipx explain` for its structure; to
  clear the selection, ask a person to clear it in Power BI Desktop and save,
  or to run `pbipx scan` at a terminal.
```

Read it like this:

- the file is `visual.json` in the report's folder;
- the **context** says where in the file it was found (`slicer-selection`: a
  saved slicer choice. Others are `filter`, `expansion-state`, `bookmark`);
- the **column** is `student.full_name`, the column the value came from;
- the **reasons** say why it counts: `block-column` (it is on the policy's
  block list) and `person-name-shape` (it looks like a name).

You are the "person" the message means. Clear the selection in Desktop
([next section](#clear-a-finding-in-desktop)), then save.

### A search over the project folder

An agent that searches the whole protected folder could read a file with a
finding. While any file there has a finding, the gate refuses the search:

```text
pbipx: finding: the search covers a path that is the protected root pbip, so
  the call could reach pbip/.../visuals/1b4e7a0d3c6f9285e1a7/visual.json,
  which has a block finding (context slicer-selection; column
  student.full_name; reasons block-column, person-name-shape). Name the files
  to search explicitly, or exclude the protected folders.
```

A shell command such as `grep -r Literal pbip` gets the same refusal, ending
in "Leave the protected folders out of the command; use `pbipx explain` for
the structure of what is in them." Either clear the finding, or ask the agent
to name the files it wants. Once the folder is clean again, searches work.

### `git diff` printing history

```text
pbipx: git-content: git diff would print content from
  pbip/School.Report/definition/report.json, which is in the protected root
  pbip; git history cannot be vetted. Limit the paths (`-- src`) or use
  --stat.
```

`git diff` and `git log -p` print file contents, and pbipx cannot
check what an old commit holds. So the gate refuses them when they would reach
a protected folder, even if the files are clean today. The hint's `src` is only
an example: give a path outside your protected folder, or use
`git diff --stat`, which lists the files and counts without printing their
contents. `git status` is fine. To see what changed inside a Power BI file,
ask for `pbipx explain` on it.

### A file the gate cannot vet

The gate only reads files it knows how to scan. Anything else inside a
protected folder is refused, because it could hold anything:

```text
pbipx: unvettable: pbip/School.Report/definition/blob.dat is under a
  protected root and no file class covers it, so it cannot be vetted. Ask a
  person to cover the file in the policy (.pbipx.toml), or leave it out of the
  call.
```

`pbipx scan` reports the same file as an error, not a block:

```text
error  pbip/School.Report/definition/blob.dat
  reasons      unvettable
```

Usually this is a file that does not belong in the project, such as a
stray export or a backup copy. Move it out of the folder. If it does
belong, ask whoever owns the policy to cover it in `.pbipx.toml`: under `[scan]`
if pbipx can read it, or `[files]` `unscanned_allow` for an opaque file such
as an image. Then approve the policy again.

### Other refusals

- `policy-unapproved`: the policy is new or changed. Review it and run
  `pbipx policy approve`.
- `policy-error`: `.pbipx.toml` is missing or will not load. Fix it, then
  approve.
- `local-state`: the agent tried to read Desktop's own state in `.pbi/`. That
  is by design.
- `working-copy`: the agent tried to read a working copy outside the
  repository. This applies to `pbipx stage`, which is not built yet.

## Clear a finding in Desktop

Fix it where it started, so the next save is clean.

1. Open the report in Desktop and go to the page the finding names.
2. Clear the selection. For a slicer, use its clear-selections (eraser)
   button. Check the filter pane too: clear any filter that picked a single
   person or value. Collapse matrix rows you expanded to a person if the
   finding was `expansion-state`, and delete a bookmark that captured a
   selection.
3. Save, and run `pbipx scan` to check it is gone.

A single-select slicer saves its first value without anyone clicking. For one
of those, add a row such as "(none selected)" to the model, select it, and ask
whoever owns the policy to list it under `[values]` `allow` in
`.pbipx.toml`. Keep that list short.

## Scan yourself

`pbipx scan` checks the whole repository. Run it before you commit if you want
to see what the commit hook will see. A clean repository:

```text
pbipx: 0 blocks, 0 warnings, 0 errors; 0 accepted by policy, 0 suppressed by
  the baseline
```

With a block:

```text
block  pbip/.../visuals/1b4e7a0d3c6f9285e1a7/visual.json
  context      slicer-selection
  locator      visual.objects.general[0].properties.filter.filter.Where[0].Condition.In.Values[0][0].Literal.Value
  column       student.full_name
  reasons      block-column, person-name-shape
  fingerprint  fp1:ee709953b443119b3ad558e6
  value        <redacted>

pbipx: 1 block, 0 warnings, 0 errors; 0 accepted by policy, 0 suppressed by
  the baseline
```

The `value` is always `<redacted>`. The `fingerprint` is a label for this one
finding. The command exits with a code the hooks use: 0 is clean, 1 a block,
2 warnings when you pass `--strict`, 3 an error. Use `--staged` to check only
what you have staged. Pass file or folder names to check just those.

`pbipx scan --show-shape` prints each value's pattern (letter types and
lengths, no letters), which can help you tell a real hit from a false one.
`--show-values` prints the value itself. It works only at an interactive
terminal, so do not use it with your screen shared.

## Look at a file's structure: `pbipx explain`

`pbipx explain` lists the names in a report or model: pages and visuals,
tables, columns, measures, relationships, sources. It never prints a value, so
it is safe to run on a file with a finding, and it is what the gate tells an
agent to use instead of reading the file:

```text
$ pbipx explain pbip/.../visuals/1b4e7a0d3c6f9285e1a7/visual.json
pbip/.../visuals/1b4e7a0d3c6f9285e1a7/visual.json  (report)
  contexts:
    - context: slicer-selection
      strings: 1
      columns:
        - student.full_name
```

You can give it a folder to describe everything in it.

## Accept a warning: the baseline

A warning is a value in a column the policy lists as a quasi-identifier,
such as `cohort_year`. It does not block a commit, and the gate still lets
an agent read the file:

```text
warn  pbip/.../visuals/1b4e7a0d3c6f9285e1a7/visual.json
  context      slicer-selection
  column       student.cohort_year
  reasons      warn-column
  fingerprint  fp1:5a2263cf6d88a3036ff39d9c
  value        <redacted>
```

If the saved selection is intended, such as a report that opens on one
cohort, accept it with its fingerprint. You must say why, and the acceptance
expires (180 days unless you choose otherwise):

```text
$ pbipx baseline record fp1:5a2263cf6d88a3036ff39d9c \
    --note "Intended: the report opens on one cohort" --expires-days 90
pbipx: recorded fp1:5a2263cf6d88a3036ff39d9c in .pbipx-baseline.toml,
  expires 2027-02-04
```

Commit `.pbipx-baseline.toml` so your reviewer can see it. After that the scan
says `1 suppressed by the baseline`. `pbipx baseline verify` lists which
accepted warnings have expired. A block can never go in the baseline. A false
positive on a block is fixed in the policy, in a change a person reviews.

## What pbipx does not protect

pbipx is a safety net for accidents: the selection you forgot, the agent that
searches a folder without thinking. It is not a wall against an agent set on
getting around it, and it is not a security product. These get past it:

- **Files you `@`-mention in a prompt.** Claude Code puts the file in the
  conversation without a tool call, so the gate never sees it. Do not
  `@`-mention a file in a protected folder. Your administrator can turn
  file mentions off with `CLAUDE_CODE_DISABLE_FILE_MENTIONS=1`.
- **MCP tools.** Servers you add to the agent, such as one that reads files,
  use their own tools, and the gate does not judge them. Do not add one that
  reads files in a session in a protected repository, or deny its tools in
  Claude Code's permissions.
- **An agent working to get around it.** A command that builds a path while
  it runs, such as `$(find ...)`, `xargs` or a script, is invisible to the
  gate.
- Tools that Claude Code adds after your pbipx was built. Run
  `pbipx init` again after you upgrade pbipx so the gate covers more tools.
- Personal data in images, anything that lives only in the Power BI service,
  and a person determined to leak.

The commit hooks and `pbipx scan` still catch a value before it reaches git.
Run `pbipx scan` yourself before you push.

The full list, with the reasons, is in the
[threat model](design/threat-model.md).

## Turn it off for a repository

To stop pbipx protecting one repository on this machine, run
`pbipx policy unenroll` at a real terminal. It shows what it will remove and
asks you to type `unenroll`. It forgets every approval for the clone. The gate
then stops protecting its files from agents.

It refuses while the work tree has a `.pbipx.toml`, because that file would
bring the repository back and the gate would refuse everything until you
approve it. Move or delete the file first. To set pbipx up again later, run
`pbipx init`.

An agent cannot run `unenroll`. It needs a terminal you are typing in.

## Where to go next

- [`examples/sample-pbip/`](../examples/sample-pbip/README.md): try it safely.
- [README](../README.md): what exists, and what is not built yet.
- [Principles](design/principles.md) and [threat model](design/threat-model.md):
  why it works this way.
