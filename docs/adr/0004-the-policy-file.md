# ADR-0004: The policy file

- **Status:** Proposed
- **Date:** 2026-10-29

## Context

Everything specific to an adopter lives in its policy (principle 13):
protected roots, which files are scanned, block and warn columns, allowlisted
values, lasting acceptances, local state and allowlisted unscanned files. The
predecessor's policy grew one key at a time. Its scan scope is one large
regular expression spanning every file kind, which is hard to review and easy
to get subtly wrong.

## Decision (proposed)

- The policy is `.pbipx.toml` at the repository root. The baseline is
  `.pbipx-baseline.toml`.
- It keeps the predecessor's concepts and their meaning, so an adopter
  converts once, by hand, in one reviewed change:
  - `protected_roots`
  - block and warn column lists, and a block-column pattern
  - `allowlist_literals`
  - lasting acceptances, as `allow_quasi`
  - local-state and unscanned-allow patterns
- Scan scope is declared **per file kind** (report, model, SQL, YAML, prose),
  each with its own globs, rather than as one regular expression.
- `pbipx init` writes a commented starting policy for a standard PBIP layout.
- An unknown key, a pattern that does not compile, or a protected root that
  no scan scope covers is a policy error. A policy error denies every gated
  call.

## Open questions

- Globs or regular expressions for scope? Globs are easier to read. The
  predecessor needed negative lookahead to exclude `.pbi/`, which a separate
  exclude list would replace.
- Should the default column lists ship in the binary, as a named preset that
  a policy extends, or always be written out in the policy?
- How does policy integrity (threat model, layer 6) bind to this file: a
  digest of the whole file, or of a normalized form?

## Alternatives considered

**Read the predecessor's file unchanged.** Cutover would be a binary swap,
but its warts would be frozen into a public format.

**A converter command.** There is exactly one existing adopter, so a reviewed
hand conversion costs less than a tool that is then maintained forever.
