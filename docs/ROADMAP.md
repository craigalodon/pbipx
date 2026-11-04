# Roadmap

Each phase ends on an exit criterion that can be checked, not a feeling. The
order matters: the corpus comes before the scanners, and the deny comes before
the capability (principle 8).

**Target for the first talk (about two months out, late 2026-12): v0.1**, which
is phases 1 to 4. Stage, collect and perf can follow it.

## Phase 0: setup (done 2026-10-29)

- [x] Repository, README, agent instructions, contributing rules
- [x] Design principles, threat model, corpus format (draft)
- [x] ADRs 0001 to 0004
- [x] Go module, `pbipx version`, the `secret.Value` type and its tests
- [x] CI workflow (format, vet, test), private-terms commit hooks
- [x] pre-commit for every lint (golangci-lint, rumdl, actionlint), run
      locally and in CI; govulncheck; Dependabot
- [x] CLI on cobra (ADR-0005)
- [x] Push to a private GitHub repository; CI green there

## Phase 1: the corpus, and the predecessor passing it (done 2026-10-29)

Most of this phase is spent in the predecessor's repository, which stays
private (ADR-0003).

- [x] Settle the vocabulary: finding contexts, reasons, gate reason classes
      (`testdata/corpus/vocabulary.toml`)
- [x] `policies/default.toml`: a generic policy for the corpus
- [x] Cases, written fresh and synthetic, for each area (247 cases):
  - [x] `pbir`: the seven report contexts that carry values; which literals are
        judged; column association; neutral-filter sentinels
  - [x] `tmdl`: partitions and expressions, "Enter data" tables, `DATATABLE`,
        RLS filters and members, descriptions, annotations; parse errors
  - [x] `sql`, `yaml`: comparisons with identifier columns; shapes in
        comments and descriptions
  - [x] `text`: prose rules (anchored names, no date shape)
  - [x] `baseline`, `policy`: blocks never baselined; acceptances; policy errors
  - [x] `gate`: the classification table; Read, search and shell decisions;
        every error path
  - [x] `explain`: structure only; non-identifier names replaced (written in
        phase 3, with its output schema)
- [x] A Go corpus runner and case validator (`internal/corpus`)
- [x] In the predecessor's repository: a normalized JSON output and a corpus
      runner pointed at a local pbipx checkout

**Exit:** the predecessor passes every case, or each case it fails is marked as
a known predecessor bug and recorded in behavior.md.

Met 2026-10-29: of 250 cases, the predecessor passes 242 and fails the 8
marked as known differences. None showed a predecessor bug.

## Phase 2: the core scanner

- [x] Policy loading (ADR-0004, accepted), policy errors
- [x] Personal-data shapes and the block/warn verdict
- [x] Report (PBIR) scanning
- [x] Prose scanning: `scan-text`, commit-message use
- [x] `scan-text --commits <range>`, for CI over a pull request's messages
- [x] Baseline: fingerprints of location, column and reasons; never values
- [x] `scan --staged | --all | <paths>`, `--strict`, redacted output,
      `--show-values` refused off an interactive terminal and under
      `CI`, `PRE_COMMIT` or `PBIPX_NO_REVEAL`, `--show-shape`

**Exit:** the `pbir`, `text`, `baseline` and `policy` areas pass.

## Phase 3: models and code

- [x] TMDL parser; DAX, M and embedded SQL lexing; association rules
- [x] "Enter data" decoding (deflate and base64); an undecodable value is an error
- [x] SQL and YAML scanning
- [x] `explain` for reports, models and SQL

**Exit:** the `tmdl`, `sql`, `yaml` and `explain` areas pass.

## Phase 4: the gate, and hardening (done 2026-11-03)

- [x] Gate decision logic, independent of any agent
- [x] Claude Code adapter; a configured hook command that fails closed;
      end-to-end tests of that command string, both directions
- [x] `pbipx init`: a starting policy and hook configuration for a standard
      PBIP repository, including the managed or user-level hook registration
- [x] Policy integrity: design, then build (threat model, layer 6; ADR-0006)
- [x] Release builds for six platforms, with checksums; install docs

**Exit:** the `gate` area passes; a fresh clone of a sample repository, set up
with `pbipx init`, denies a planted finding through the real hook.
**This is v0.1.**

Met on a synthetic sample, 2026-10-29: a fresh clone set up with `pbipx init`
denied a planted finding, a recursive search, `git show`, a write to Claude
Code settings and every call after a policy edit through the exact hook
command init registered, and allowed a clean read; a missing binary denied.
Met in full 2026-11-03: CI passed on Linux and Windows for the release
commit, and on macOS for the release candidates (GitHub found no macOS
runner for the final commit's runs; the tag's run covers it). Sessions in
Claude Code itself on the release candidate denied every planted read that
reached the registered hook. Before tagging, the gate went
through thirteen rounds of independent security review, closed with a
differential test against real bash (`TestDifferentialBash`); behavior.md
records the known over-blocks and limitations.

## Phase 5: cutover of the predecessor's repository

Done in that repository, privately.

- [ ] Its policy converted by hand to `.pbipx.toml` (one reviewed change)
- [ ] Baseline regenerated and reviewed
- [ ] Differential run: predecessor and pbipx agree on its real tree,
      compared by counts and fingerprints only
- [ ] Hooks, pre-commit and CI switched to the pinned binary; the hook
      registered outside the repository
- [ ] Predecessor deleted

**Exit:** the predecessor's code is gone and its repository's own tests of the
gate pass against pbipx.

## Phase 6: the Desktop round trip

- [ ] `stage` and `collect`: copy outside the repository, rebind the report to
      the model beside it, bring back only what Desktop saved, keep LF and
      the absence of a final newline
- [ ] Windows and WSL path handling
- [ ] `perf`: Performance Analyzer summaries, no query text or titles
      ([specified](design/perf.md); the gate already denies a Read of an
      export by its shape, `perf-export`)

**Exit:** a staged edit round-trips through Desktop with `git status` showing
only real changes.

## Phase 7: going public

- [ ] Institutional approval to publish
- [ ] License chosen and added
- [ ] The full history checked against the private-terms list: run
  `scripts/check-private-terms.sh --history` in a `git clone --mirror` clone
  (this covers `refs/pull/*`), and `--github` for pull request, issue and
  release text
- [ ] A synthetic sample PBIP repository to demonstrate with
- [ ] Docs written for analysts, not maintainers
- [ ] Repository made public; issue tracker and `SECURITY.md` contact live

## Open questions

- Personal namespace or an organization for the module path and releases?
- Code signing for the Windows binary (SmartScreen), and packaging (winget,
  Scoop, Homebrew)?
- Which other agents' hook protocols are worth an adapter, and when?
- Should `stage` and `collect` handle a model shared by several reports?
