# ADR-0001: Go, shipped as one binary

- **Status:** Accepted
- **Date:** 2026-10-29

## Context

The predecessor is a Python package run with `uv`, inside the repository it
protects. Three things are wrong with that for a shared tool:

- **The people who would adopt it** are Power BI analysts, mostly on Windows,
  most without a Python toolchain. "Install uv, then sync" loses most of them.
- **The gate runs on every agent tool call.** Starting an interpreter and
  resolving an environment each time adds latency to every Read and every
  shell command.
- **The gate ran from the working tree**, so an agent's edit to the gate's
  code changed the gate on the next call (threat model, layer 4).

## Decision

Write pbipx in Go and ship one static binary per platform: Windows, macOS and
Linux, on amd64 and arm64, with published checksums.

Use the standard library where it suffices. Where a format has an established
parser (TOML, YAML), use it rather than writing one. Write our own only where
none exists: TMDL, and the DAX, M and SQL lexing needed for association.

## Consequences

An analyst installs one file. The gate starts in milliseconds. A pinned binary
outside the repository is the natural way to install it.

The predecessor's tests do not port; see ADR-0003. Contributors need Go, which
is one download.

## Alternatives considered

**Keep Python, install with `uv tool install` from a tagged release.** It
solves the working-tree problem but not the analyst's toolchain.

**Rust.** Also one binary, but slower to write and to review for the
maintainer, and a security tool its maintainer cannot read fluently is a
liability.

**.NET, for the Tabular Object Model.** TOM would parse models for us, but it
ties the tool to a runtime and to the model formats TOM supports, and the
scanner needs positions and literals that TOM normalizes away.
