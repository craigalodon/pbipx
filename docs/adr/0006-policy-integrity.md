# ADR-0006: Policy integrity through an approved digest

- **Status:** Accepted
- **Date:** 2026-10-29

## Context

The policy decides what the gate denies, and it lives in the repository the
agent edits. An agent that adds a column to `warn` instead of `block`, adds a
value to `[values] allow`, or widens `[scan] exclude` weakens the gate as
surely as one that edits the gate's code (threat model, layer 6). Installing
the binary outside the repository (layer 4) closes the code path, not the
policy path.

The control must not depend on the agent's cooperation, must fail closed,
and must cost a person little: policies change rarely, and each change is
already a reviewed diff.

## Decision

- `pbipx policy approve` records the SHA-256 digest of the policy file's
  exact bytes, outside the repository:
  `${PBIPX_CONFIG_DIR:-<user config dir>/pbipx}/approved/<key>`, where the
  key identifies the repository by its absolute root path. The file holds
  the digest and the date; it never holds policy content. (Amended
  2026-10-30, below: the key is the clone's git directory, and the record
  holds a set of digests.)
- Approval is a person's act. It is refused, with a usage error, when stdin
  or stdout is not an interactive terminal, or when `CI`, `PRE_COMMIT` or
  `PBIPX_NO_REVEAL` is set, so a hook or an agent's shell cannot approve. It
  shows the policy's digest and asks for confirmation.
- The gate checks the digest before it decides anything. A policy that is
  missing an approval, or whose digest differs from the approved one,
  denies every gated call with the reason class `policy-unapproved`, as a
  policy that fails to load denies with `policy-error`.
  (Amended 2026-10-30, second amendment below: in the repositories that
  adopted pbipx.)
- The digest covers the whole file. A comment or whitespace edit needs
  re-approval too; normalizing first would make the normalizer itself
  security-critical.
- `scan` does not check approval. Commit hooks and CI judge staged content
  under the policy in the commit, where a policy change is a reviewed diff.
- `pbipx policy status` reports approved, unapproved or changed, with no
  content.

## Consequences

An agent can still edit the policy, but the gate then denies every call
until a person approves the change at a terminal, so the edit cannot take
effect silently. Every policy change costs one command.

Approval cannot be granted through the gate's own agent paths. The terminal
check stops `pbipx policy approve` from a hook or a plain shell, but two
deliberate escapes remain open to a process that sets out to get round it:
writing a forged record straight into the store, and running the approval
under a program that supplies a terminal, such as `script`. The gate denies
both to an agent: its write rule denies a write anywhere in the store
(`agent-settings`), and its shell rule denies a command that reaches the
store or runs the approval. Neither stops a process outside the gate, which
the threat model leaves to the agent's sandbox (layer 7).

The approval is checked against the exact bytes the gate parsed: the
policy file is read once, so a policy swapped between a parse and a digest
cannot be enforced while another is checked. A store inside the repository
(`PBIPX_CONFIG_DIR` there, as given or with symlinks resolved) is refused
like a relative one: the gate denies every call, and the policy commands
exit 3. So is a repository's committed Claude Code settings file whose
`env` sets `PBIPX_CONFIG_DIR`, which `pbipx scan` reports as
`hooks-disabled`.

An approval is per machine and per clone path. A second clone, or a move,
needs its own approval. (Amended 2026-10-30, below: per clone, shared by
its work trees.)

A person can approve a weakened policy without reading it. The control makes
the change visible and deliberate; review is still the person's job.

## Alternatives considered

**A digest of a normalized policy.** Comment edits would not need
re-approval, but the normalization becomes part of the trusted base, and a
mistake in it could let a meaningful change through unapproved.

**Signing the policy.** Stronger against tampering at rest, but it needs key
management for every adopter, which an analyst setting up a repository
should not have to learn.

**The policy outside the repository.** The policy is repository
configuration, reviewed with the code it governs (ADR-0004). Moving it out
loses that review.

## Amendment (2026-10-30): one record per clone, holding a set of digests

Keying the record by the work tree's path denied every new work tree of an
approved clone until a person approved it at a terminal. Claude Code adds
one under `.claude/worktrees/<name>` for each isolated subagent, so the
gate over-blocked exactly where it is used most, and a control that gets in
the way gets turned off.

- **The key** is now the clone's shared git directory, as
  `git rev-parse --git-common-dir` names it, made absolute and with symlinks
  resolved, then hashed as before. Every work tree of one clone, linked
  ones included, shares one record. A second clone, or a move of the git
  directory, still needs its own approval.
- **The record holds a set of digests**, at most the 16 most recently
  approved (`version = 2`, `digests = [...]`, oldest first, and the time of
  the last approval). `policy approve` adds the policy's digest, or moves
  it to the end when it is already there, and drops the oldest past 16. A
  policy is approved when its digest is in the set, so work trees on
  branches with different policies can each be approved once.
- **Migration.** A `version = 1` record, keyed by a work tree's path, is
  still read for that work tree, and its digest counts as approved. The
  next approval there merges it into the clone's record and removes it.
- A store inside the git directory, or inside the main work tree when the
  gate runs in a linked one (`.claude/worktrees` lies inside it), is
  refused like a store inside the repository.

The cost is that an approval no longer lapses when a newer policy is
approved: a policy approved once stays approved until 15 newer ones push it
out, so checking out an older, weaker policy that was approved before
enforces it without a new prompt. Each such policy was a person's
deliberate approval, and a branch can only bring back a policy that was
approved for this clone, never a new one.

## Amendment (2026-10-30): adoption, and an index of adopted work trees

`pbipx init` registers the gate in the user's Claude Code settings, so it
runs in every session on the machine, in every directory. Judging every
call as if a policy were due denied every call in every other repository
(`policy-error`) and outside any repository (`not-a-repository`):
installing pbipx blocked the machine. The gate now acts only for
repositories that adopted pbipx, and stays closed in them.

- **Adopted** means the work tree has a `.pbipx.toml`, or the clone has a
  record in the store, or the work tree is in the store's index. The record
  is the tamper-resistant signal: it lives outside the repository, so once
  a person ran `pbipx init` or approved a policy, deleting or breaking the
  policy denies with `policy-error` rather than switching the gate off.
- **`pbipx init` records adoption** at once: a record with no digest, so
  the gate denies with `policy-unapproved` between init and approval
  instead of allowing.
- **Reaching in from elsewhere.** A call is judged against every adopted
  repository it touches, wherever it is made from: a Read of an absolute
  path, a Grep of a directory above one, a `cat` of a path into one from
  `/tmp`. Each path the call touches is placed in the git work tree that
  holds it by walking up for `.git`.
- **The index.** The record holds the work-tree paths it was written from
  (`version = 3`, `roots = [...]`, at most 64, the most recent last), so
  the gate can find adopted repositories without being told: a call that
  touches something it cannot place (a word it cannot expand, a working
  directory it cannot tell) is judged against every indexed one, and a
  path below which an indexed work tree lies reaches it. A version 2
  record is still read, and adds no path until it is next written.
- **Paths are not policy content.** Until now the record held no path: its
  key is a hash. The rule that a record never holds policy content stands;
  it guards what an agent could learn from the store, and what could carry
  a value. A work-tree path is neither, and it stays in the user's own
  configuration directory, mode 0600.
- **What controls the gate is guarded from anywhere.** A write into the
  store or the user-level or managed Claude Code settings, and a run of
  `pbipx policy approve`, are denied from every directory, adopted or not:
  each switches the gate off, or forges an approval, for every adopted
  repository on the machine. The run guarded is the whole class of the
  tool's person-only steps, not the one name: `policy approve` and
  `policy unenroll` (third amendment, below), `init`, `gate` and
  `--show-values`, under `script`, `expect`, `unbuffer` or `winpty`, and
  for a word after `policy` the gate cannot tell. What an agent may run
  of the tool alone is an explicit table (behavior.md, Bash rule 1), so a
  new subcommand is closed to agents until it is added there.

The cost: an index entry outlives the repository at its path. An entry
whose directory is gone is skipped, but one whose directory is still there
keeps the gate acting there until a person removes the record from the
store (by hand, or with `pbipx policy unenroll`; third amendment below).
And a repository that has a committed `.pbipx.toml` is adopted on first
clone, before anyone ran `init` or approved it, so the gate denies
there until a person approves the policy: the same prompt as before, now
asked only where pbipx is in use.

## Amendment (2026-11-04): adoption ends by a person's act

The first and second amendments made adoption one-way: a record, or an
index entry, outlived the repository, and a person who wanted a clone out
of the gate's reach edited the store by hand. That is `pbipx policy
unenroll` now. It removes the clone's adoption, for every one of its work
trees, and is a person's step as approval is.

- **Adoption ends only by a person's act at a terminal.** The command is
  refused unless stdin and stdout are terminals and none of `CI`,
  `PRE_COMMIT` and `PBIPX_NO_REVEAL` is set, asks for `unenroll` typed in
  full, and is denied to an agent from every directory (the person-only
  list). Nothing else removes an adoption except a person editing the store
  by hand, which the gate also denies an agent.
- **Every approval is forgotten.** The clone's record goes, with its
  digests. Nothing is kept to restore.
- **A policy file adopts again.** It refuses while the work tree has a
  `.pbipx.toml`, since the file would adopt the repository again and the
  gate would deny every call with `policy-unapproved`. And a policy file
  that arrives later (a pull, another branch) adopts the repository again
  with no approval, so the gate denies until a person approves. There is
  no tombstone: a marker that outlives the removal would be one more thing
  to forge, or to forget, and "a policy file means adopted" is the rule
  that fails closed.
- **The clone's record goes last.** The work trees are stripped from other
  records' roots first, then the records keyed by a work tree, so an error
  part way leaves the clone adopted, never half removed. A record left
  with no root and no digest is deleted, since a version 3 record with
  neither is invalid and an invalid record counts as adopted. One left
  with digests and no roots is another clone's and is kept.
- **The result is checked with the gate's own logic**, exported from the
  gate, not copied: a policy file or an enclosing adopted repository can
  still adopt the work tree, and then the command exits 3 and says which.
- **A mismatched `PBIPX_CONFIG_DIR` cannot be detected.** If the person's
  shell sets one that the gate's hook does not see, the command removes the
  adoption from a store the gate does not read, and the gate goes on
  acting. Printing the store directory before the prompt is the mitigation.
- **A drift guard.** A test walks cobra's top-level and `policy` commands
  and requires each to be on the gate's agent table or its person-only
  list, so a new command is closed to agents until someone decides.
