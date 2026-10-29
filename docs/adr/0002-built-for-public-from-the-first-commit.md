# ADR-0002: Built for public from the first commit

- **Status:** Accepted
- **Date:** 2026-10-29

## Context

pbipx starts in a private repository, and whether it goes public is decided
later. But an exposure is hard to reverse: once a name or identifier is in
history and a clone exists, it cannot be taken back. A repository written as
private and cleaned up later depends on that cleanup finding everything.

The predecessor lived in an organization's private repository, beside real
projects, and its tests, policy and records name that organization's systems.

## Decision

Treat this repository as public from its first commit.

- **No history is imported.** pbipx is new code. Nothing is copied from the
  predecessor's repository, including its fixtures, policy and prose; its
  lessons are rewritten generically (`docs/design/principles.md`).
- **Fixtures are synthetic**, by the rules in `CONTRIBUTING.md`.
- **A private-terms check** runs on every commit: staged paths, content and
  message against a maintainer's term list kept outside the repository. It
  fails closed.
- **Sample projects** use placeholder connection strings, tenants and
  workspaces. A real connection string identifies a tenant and a workspace.

Going public then needs only the checks in roadmap phase 7: a license, the
maintainer's institutional approval, and the full history checked against the
private list.

## Consequences

Making it public is a settings change, not a cleanup project. The cost is
discipline from the first day, and a term list that each maintainer keeps up
to date.

The term list cannot be checked in CI, because CI would need the list. Before
publishing, the history is checked by hand (phase 7).

## Alternatives considered

**Write privately now, scrub before publishing.** It depends on the scrub
being complete, which is exactly what cannot be verified.

**Fork the predecessor's history and filter it.** Filtering history removes
known strings, not the unknown ones.
