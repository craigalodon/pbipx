package claudecode

import "strings"

// The configured hook command must fail closed on its own (principle 7).
// Claude Code proceeds on every exit code but 0 and 2, so a gate that never
// runs — the binary missing (127), not executable (126), killed by a signal
// (128+n), crashed, or exiting 1 — would allow every call. The command
// therefore runs the binary, passes 0 and 2 through, and turns every other
// outcome into exit 2 with a reason on stderr.
//
// Claude Code runs a command hook's string through a shell: `sh -c` on Linux
// and macOS; on Windows, Git Bash, or PowerShell when Git Bash is not
// installed, or whichever the hook's "shell" field names ("bash" or
// "powershell"). A string written for one shell is a parse error in the
// other, and a parse error exits 1, which proceeds. So the Windows hook names
// its shell explicitly (HookShell), and its command is written for that
// shell.

// failedReason is the shell's deny reason when the gate did not decide.
const failedReason = "pbipx: " + ReasonInternalError + ": the gate did not run or did not decide"

// HookCommand is the command string to configure for a PreToolUse hook that
// runs the pbipx binary at binaryPath. goos is a GOOS value: "windows" gets
// the PowerShell form, every other system the POSIX sh form.
func HookCommand(binaryPath, goos string) string {
	if goos == "windows" {
		return powerShellCommand(binaryPath)
	}
	return posixCommand(binaryPath)
}

// HookShell is the value for the hook's "shell" field, or "" to leave it out
// (sh on Linux and macOS). On Windows it is "powershell", so the command is
// never run by a shell it was not written for.
func HookShell(goos string) string {
	if goos == "windows" {
		return "powershell"
	}
	return ""
}

// posixCommand runs the binary as a simple command, so a missing or
// unexecutable file is an exit status (127, 126), not a fatal shell error,
// and the lines after it still run. stdin, the event, is inherited.
func posixCommand(binaryPath string) string {
	return shQuote(binaryPath) + " gate --agent " + Agent + `; s=$?; ` +
		`[ "$s" -eq 0 ] && exit 0; [ "$s" -eq 2 ] && exit 2; ` +
		`echo '` + failedReason + `' >&2; exit 2`
}

// powerShellCommand starts with an exit status that denies, so every path
// that does not set it from the binary — a missing file, a file that will not
// start, an exception under $ErrorActionPreference = 'Stop' — denies. stdin
// is inherited by the native command; if it were not, the gate would read an
// empty event and deny.
func powerShellCommand(binaryPath string) string {
	return `$s = -1; try { & ` + psQuote(binaryPath) + ` gate --agent ` + Agent +
		`; if ($null -ne $LASTEXITCODE) { $s = $LASTEXITCODE } } catch { $s = -1 }; ` +
		`if ($s -eq 0) { exit 0 }; if ($s -eq 2) { exit 2 }; ` +
		`[Console]::Error.WriteLine('` + failedReason + `'); exit 2`
}

// shQuote quotes s as one POSIX shell word.
func shQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// psQuote quotes s as a PowerShell verbatim string. PowerShell also reads
// the typographic single quotes as quotes, so each of them is doubled too.
func psQuote(s string) string {
	var b strings.Builder
	b.WriteByte('\'')
	for _, r := range s {
		switch r {
		case '\'', '‘', '’', '‚', '‛':
			b.WriteRune(r)
		}
		b.WriteRune(r)
	}
	b.WriteByte('\'')
	return b.String()
}
