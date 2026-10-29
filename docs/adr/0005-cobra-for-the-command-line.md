# ADR-0005: Cobra for the command line

- **Status:** Accepted
- **Date:** 2026-10-29

## Context

ADR-0001 prefers the standard library where it suffices. For the command line
it does not suffice for long: pbipx will have seven or more commands, several
with subcommand-specific flags (`scan --staged | --all`, `--strict`,
`--show-values`, `--show-shape`), shared flags such as a policy path, help per
command, and shell completion that analysts on PowerShell will want. Written
on `flag`, that is a hand-rolled dispatcher and help system to maintain.

A CLI library also prints things on its own: usage errors, "unknown command"
messages and suggestions. Those echo what the user typed, and a command line
can carry a value (principle 2).

## Decision

Use [Cobra](https://github.com/spf13/cobra) (with `pflag`) for command
dispatch, flags and help.

Cobra never writes an error itself. The root command sets `SilenceErrors` and
`SilenceUsage`, and `cmd/pbipx` turns every error Cobra returns into a fixed
message that names no argument. Its built-in `help` command echoes an unknown
topic, so pbipx replaces it. The `completion` command stays off until it is
added deliberately. Tests check that no argument appears in the output of a
failed command.

## Consequences

Commands, flags and help come with consistent behavior and little code. Cobra
and `pflag` become dependencies of a security tool. Both are widely used, and
Dependabot and govulncheck watch them.

Cobra's own messages are not usable, so a usage error says less than Cobra
would ("usage error" rather than "unknown flag --x"). That is the price of
principle 2, and per-command help is one command away.

## Alternatives considered

**`flag` from the standard library, with a hand-written dispatcher.** It has
no dependencies and prints nothing on its own, but help, per-command flags and
completion all become code to maintain.

**urfave/cli, kong.** Both are capable. Cobra has the widest use, the most
examples, and completion for PowerShell, bash, zsh and fish.
