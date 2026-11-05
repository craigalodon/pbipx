# Threat model

## What is protected

Values persisted into a Power BI project and its neighbouring files: report
JSON, semantic-model TMDL, SQL, YAML and Markdown. They can leak to:

| Where | Why it matters |
|---|---|
| git history | removal needs a history rewrite and a force push, and clones keep the old history |
| an AI agent's transcript | stored outside the repository, where no repository control reaches, and cannot be retracted |
| commit and merge messages | the most permanent text in a repository |
| the tool's own output | captured by CI logs and by agents driving commits |

## Who acts

- **The developer** saves in Desktop with a selection still set. This is the
  common case, and it is structural, not careless: nothing warns them.
- **The AI agent** is honest but fallible. It reads widely, fast, and follows
  instructions mostly. Instruction files are advisory, so anything that must
  hold regardless of the agent's judgement belongs in a hook.
- **CI** is the backstop that does not depend on any local setup.

## What pbipx is, and is not

pbipx is a **safety net against accidents**: the value saved by mistake and
the agent that greps a folder without thinking. It is not a sandbox against an
agent working to get around it. A shell command that builds a path at run time
(`$(find ...)`, `xargs`, a script) is invisible to a pre-tool hook that
inspects the command text.

Out of scope: a malicious insider; an agent deliberately escaping; personal
data shown in images; data that lives in the Power BI service rather than in
files.

## What no pre-tool hook sees

The gate sees a tool call, and only one whose tool its matcher names
(`pbipx init` registers it for every built-in Claude Code tool that can
read file content: Read, Grep, Bash, PowerShell, LSP, and the writing
tools). Content that reaches the agent without such a call passes it by:

- **A file a person `@`-mentions in a prompt.** Claude Code inserts it into
  the conversation without a tool call, so no hook runs. Setting
  `CLAUDE_CODE_DISABLE_FILE_MENTIONS=1` (in managed settings' `env`, which
  `pbipx init --managed` prints) turns file mentions off.
- **MCP tools**, such as a filesystem server's. Their names and inputs are
  the server's own, so the gate does not judge them. Do not install an MCP
  server that reads files in a session that works in an adopted
  repository, or deny its tools with permission rules
  (`"deny": ["mcp__<server>"]`).
- **A tool the matcher does not name**: a built-in tool added to Claude
  Code after the installed pbipx, or a hook registered by an earlier pbipx
  with a narrower matcher until `pbipx init` is re-run.
- **What a tool checks before its hook runs.** Claude Code validates a
  tool call's input before PreToolUse hooks run, and for Edit that
  validation reads the file: an Edit of a file the gate would refuse to
  Read can come back with "Found N matches of the string to replace"
  without the gate seeing the call, which tells the agent whether a string
  occurs in the file. Claude Code's documentation places a `Read` deny rule
  before Edit's checks, and a `Read` deny rule also blocks Edit and Write
  on its paths. So a repository that needs this closed can add
  `"deny": ["Read(<root>/**)"]` for each protected root (`//` for an
  absolute path), at the cost of the agent no longer reading or editing
  clean files there either.
- **PowerShell that builds a path at run time.** The gate cannot parse
  PowerShell; it scans the whole protected tree of each adopted repository
  a call reaches, but it finds those repositories from the working
  directory and the words that look like paths.

## Layers

Each layer covers what the one before it misses. In order of what an adopter
should set up:

1. **Commit-time scan** (`pbipx scan --staged`) as a pre-commit hook, and a
   **commit-message scan** (`pbipx scan-text`) as a commit-msg hook.
2. **CI scan** of the tree and of every commit message in a pull request. A
   message typed into a web merge dialog is checked only after it lands; the
   pull-request checklist is the control there.
3. **The gate** as an agent pre-tool hook: it denies reads of files with
   findings or that cannot be vetted, and denies searches and shell commands
   over a protected root while anything there is unclean. It is scoped to
   the repositories that adopted pbipx: a repository becomes adopted when a
   person runs `pbipx init` or approves its policy (a record in the
   approval store, outside the repository), or when it has a
   `.pbipx.toml`. Elsewhere the gate allows, except a call that reaches
   into an adopted repository, which it judges against that repository
   from any working directory. Once adopted, a repository stays adopted:
   deleting or breaking its policy denies every call there
   ([ADR-0006](../adr/0006-policy-integrity.md), amendment on adoption).
   Adoption ends only by a person's act at a terminal: `pbipx policy
   unenroll` (refused to a hook, CI or a shell without a terminal, and
   denied to an agent), or a person editing the store by hand. It forgets
   every approval of the clone, and a policy file that arrives later adopts
   the repository again with no approval. It cannot tell that
   `PBIPX_CONFIG_DIR` differs from the one the gate runs with, so it prints
   the store directory for the person to check.

   The gate also protects **working copies**, the folders `pbipx stage`
   makes outside every repository for Power BI Desktop, which writes a
   data cache of real data into them that a scan would pass
   ([ADR-0007](../adr/0007-the-desktop-round-trip.md)). They are
   registered in the store, and the gate denies every path under a
   registered one with `working-copy`, with no scan, from any working
   directory, adopted repository or not: a Read, Edit, Write, Grep or LSP
   call, a Bash word or redirection, a working directory inside one, a
   search over a directory holding one, and the words of a PowerShell line
   that look like paths, each placed as the gate places a path into an
   adopted repository (links, `..`, `~`, globs, case, Git Bash forms).
   A call that touches something it cannot place counts as touching every
   registered working copy, and a registry the gate cannot read denies
   every call. Listing a working copy is denied too. An agent cannot add,
   change or remove an entry: the registry is in the store, which the gate
   denies an agent's write into. The limits are the gate's own: a path an
   interpreter builds at run time inside a script the gate is not shown, and
   a PowerShell variable outside a word that looks like a path, pass it, as
   does any process outside the gate (layer 7).
4. **The gate binary outside the repository**, installed at a pinned
   version. When the gate runs code from the working tree, an agent edit to
   that code changes the gate on its next call. A binary on the user's `PATH`
   closes that.
5. **The hook registered where the project cannot override it**, such as an
   agent's managed or user-level settings rather than the repository's.
   `pbipx init` registers the hook at user level by default. That protects
   it against a repository removing it, but not against a repository turning
   it off: Claude Code's `.claude/settings.json` or
   `.claude/settings.local.json` in the repository can set
   `"disableAllHooks": true`, which disables every user-level hook, the
   gate's included. pbipx guards that path from both sides: the gate denies
   an agent's write to, or shell command naming, such a file in the
   repository (`agent-settings`), and `pbipx scan` reports a committed one
   that disables hooks, sets an environment variable that can defeat the
   hook (`PBIPX_CONFIG_DIR`, `CLAUDE_CONFIG_DIR`, `PATH`, `BASH_ENV`, `ENV`,
   `SHELLOPTS`), or does not parse, as an error (`hooks-disabled`). The gate
   also denies an agent's write to the user-level settings
   (`settings*.json` in `$CLAUDE_CONFIG_DIR` or `~/.claude`) and to the
   managed settings directory, from every working directory, adopted
   repository or not: the user-level hook runs in every session on the
   machine, and removing it from anywhere would switch the gate off for
   every adopted repository. None of this stops a person from editing the
   file by hand, or an agent from building the path at run time. The hook
   command itself fails closed under a shell with errexit on, and is
   registered with a timeout long enough that a slow scan never reaches
   Claude Code's default, since a hook that times out lets the call
   proceed. Only managed settings are immune: the
   hook registered there (`pbipx init --managed` prints a
   `managed-settings.d` drop-in), ideally with `"allowManagedHooksOnly":
   true`, cannot be turned off by any repository or user setting.
6. **Policy integrity.** The policy file stays in the repository, and an edit
   to it weakens the gate as much as an edit to the code would. The gate
   checks the policy against a digest a person approved at a terminal,
   stored outside the repository, and denies every call until a changed
   policy is re-approved
   ([ADR-0006](../adr/0006-policy-integrity.md)). It digests the same bytes
   it parsed, refuses a store inside the repository, and denies an agent's
   write into the store, so an agent cannot forge an approval through the
   gate's own paths.
7. **The agent's own sandbox and file-system deny rules** for Desktop's local
   state and caches. These are the real containment; the gate explains each
   denial.

## Agents

The gate's decision logic does not depend on any one agent. Each agent's
hook protocol (event shape, exit codes, decision format) is a thin adapter.
Claude Code is the first adapter, because the predecessor ran there. Others
come when someone needs them, with their own end-to-end tests of the
configured command (principle 7).
