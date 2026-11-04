# ADR-0003: Parity through a language-neutral corpus

- **Status:** Accepted
- **Date:** 2026-10-29

## Context

The predecessor has a large test suite, but it is written against the
predecessor's Python functions and builders. None of it runs against a Go
binary. Porting those tests by hand would mean re-deriving each expected
result while writing the code it tests, and two implementations of a safety
tool that drift apart are worse than one.

## Decision

Specify behavior as a corpus of cases that does not depend on language
([docs/design/corpus.md](../design/corpus.md)), then use it in this order:

1. **Write the corpus here, synthetic**, covering the predecessor's
   behavior. In the predecessor's own repository, add a normalized JSON output
   and a small runner, and make the predecessor pass the corpus. That fixes
   current behavior as the specification. Where the predecessor is wrong, a
   case says so, and both get fixed.
2. **Make pbipx pass the same corpus**, area by area.
3. **Cut the predecessor's repository over** to the pinned pbipx binary and
   delete the predecessor. The two are never maintained side by side past
   cutover.

Every case checks redaction: the synthetic values it plants must not appear in
any output.

## Consequences

Parity becomes a test result, not a judgement. The corpus outlives both
implementations, as the regression suite.

The cost is phase 1, before any Go scanner code, spent partly in the
predecessor's repository. The predecessor's cases are rewritten, not
converted, so that no fixture from a real repository reaches this one
(ADR-0002).

## Alternatives considered

**Port the pytest suite to Go tests.** Expected results would be re-derived
while writing the code that produces them, so they would test the port
against itself.

**Differential testing on real repositories.** Agreement on a real
repository is strong evidence, and the predecessor's maintainer can run it
privately at cutover. But its inputs can never be committed here, so it cannot
be the specification.

## Amendment 2026-11-04: areas that only pbipx has

The corpus is also the specification of behavior the predecessor never had.
The `stage` and `collect` areas ([ADR-0007](0007-the-desktop-round-trip.md))
are pbipx-only: no predecessor run is needed for them, and step 1 of the
decision does not apply. Their cases are written from the design and enabled
in the change that builds the commands. Because the predecessor is retiring,
no case in them carries `known = "predecessor"`, and nothing is added to the
predecessor's repository for them.
