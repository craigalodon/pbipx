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
   over a protected root while anything there is unclean.
4. **The gate binary outside the repository**, installed at a pinned
   version. When the gate runs code from the working tree, an agent edit to
   that code changes the gate on its next call. A binary on the user's `PATH`
   closes that.
5. **The hook registered where the project cannot override it**, such as an
   agent's managed or user-level settings rather than the repository's.
6. **Policy integrity.** The policy file stays in the repository, and an edit
   to it weakens the gate as much as an edit to the code would. *Design open:*
   the gate checks the policy against a digest a human approved, stored
   outside the repository, and denies until a changed policy is re-approved.
   See [ROADMAP.md](../ROADMAP.md), phase 4.
7. **The agent's own sandbox and file-system deny rules** for Desktop's local
   state and caches. These are the real containment; the gate explains each
   denial.

## Agents

The gate's decision logic does not depend on any one agent. Each agent's
hook protocol (event shape, exit codes, decision format) is a thin adapter.
Claude Code is the first adapter, because the predecessor ran there. Others
come when someone needs them, with their own end-to-end tests of the
configured command (principle 7).
