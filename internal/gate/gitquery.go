package gate

import (
	"bytes"
	"errors"
	"os/exec"
	"strings"
)

// The gate asks git a few questions about the repository a command runs
// in: what an object name resolves to, and what an alias or a setting is.
// Each is one git command, run with an argv (never a shell), standard input
// empty and standard error discarded. Its output is read here and never
// put in a message: a type name or a setting decides, and nothing else
// leaves this file.

// errNotSet is a configuration key that is not set (git config exits 1).
var errNotSet = errors.New("not set")

// gitOutput runs git with args in dir and returns its standard output,
// trimmed of the final newline.
func gitOutput(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...) //nolint:gosec // G204: fixed git subcommands; the one operand is a word from the command being judged, passed as argv after --end-of-options or as a config key
	cmd.Dir = dir
	cmd.Stdin = nil
	cmd.Stderr = nil
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 && args[0] == "config" {
			return "", errNotSet
		}
		return "", err
	}
	return strings.TrimSuffix(out.String(), "\n"), nil
}

// objectType is what git cat-file -t says rev names in dir: "blob",
// "tree", "commit" or "tag", or "" when it names nothing git can resolve.
// A rev starting with "-" is refused, though --end-of-options would keep
// git from reading it as an option.
func objectType(dir, rev string) string {
	if rev == "" || strings.HasPrefix(rev, "-") {
		return ""
	}
	t, err := gitOutput(dir, "cat-file", "-t", "--end-of-options", rev)
	if err != nil {
		return ""
	}
	switch t {
	case "blob", "tree", "commit", "tag":
		return t
	}
	return ""
}

// configValue is the value of key in dir's configuration. err is errNotSet
// when the key is not set, and another error when git could not tell.
// typ, if not empty, is passed as --type.
func configValue(dir, key, typ string) (string, error) {
	if key == "" || strings.HasPrefix(key, "-") {
		return "", errors.New("not a key")
	}
	args := []string{"config", "--get"}
	if typ != "" {
		args = append(args, "--type="+typ)
	}
	return gitOutput(dir, append(args, key)...)
}
