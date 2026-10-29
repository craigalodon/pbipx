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
  the digest and the date; it never holds policy content.
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

An approval is per machine and per clone path. A second clone, or a move,
needs its own approval.

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
