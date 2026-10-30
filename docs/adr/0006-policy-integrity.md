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
