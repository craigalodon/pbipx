package gate_test

import (
	"testing"

	"github.com/craigalodon/pbipx/internal/gate"
)

// TestShellWrapperOperands: the value of a wrapper's option that takes one
// (timeout's duration, nice -n, sudo -u, xargs -P ...) and an env NAME=value
// operand are not the command word, even when the gate cannot expand
// them. An unknown word in the program position, and a wrapper option the
// gate does not know, stay unknown.
func TestShellWrapperOperands(t *testing.T) {
	clean := newRepo(t, cleanFiles())
	leaky := newRepo(t, leakyFiles())
	sh := func(cmd string) gate.Event { return gate.Event{Tool: gate.Shell, Command: cmd} }
	git := gate.ReasonGitContent
	allowed := []string{
		`env FOO="$X" go test ./...`,
		`env GOOS=$(go env GOOS) go build ./...`,
		`timeout "$T" go test ./...`,
		`timeout $((5*60)) go test ./...`,
		`timeout -s KILL "$T" go test ./...`,
		`timeout --signal="$S" 5 go test ./...`,
		`nice -n "$N" go test ./...`,
		`nice --adjustment "$N" go test ./...`,
		`sudo -u "$USER" make`,
		`sudo --user "$USER" make`,
		`xargs -P "$(nproc)" -n1 echo`,
		`ionice -c "$C" go test ./...`,
		`taskset "$MASK" go test ./...`,
		`stdbuf -o "$M" go test ./...`,
		`env -u "$V" go test ./...`,
		`env -C "$D" go test ./...`,
	}
	for _, l := range allowed {
		run(t, clean, []decideCase{{"clean: " + l, sh(l), ""}})
		run(t, leaky, []decideCase{{"leaky: " + l, sh(l), ""}})
	}
	// Unknown where the program is, or after an option the tables do not
	// know, is built at run time.
	denied := []string{
		`run() { "$@"; }; run go test ./...`,
		`$CMD test`,
		`while read l; do $l; done < cmds.txt`,
		`env $X go test ./...`,
		`env FOO=1 "$X" test`,
		`timeout 5 "$X" test`,
		`nice -n 1 "$X" test`,
		`nice --frobnicate "$X" go test ./...`,
		`sudo --frobnicate "$X" make`,
		`xargs -Z "$X" go test`,
	}
	for _, l := range denied {
		run(t, clean, []decideCase{{"clean: " + l, sh(l), git}})
	}
	// A wrapper option the gate does not know leaves the words after it
	// unread: an operand is not skipped, so an unknown word is taken for
	// the command.
	for _, l := range []string{
		`timeout --frobnicate "$X" 5 go test`,
		`timeout -Z "$X" 5 go test`,
		`timeout --frobnicate "$X" go test`,
		`taskset --frobnicate "$X" go test`,
		`flock --frobnicate "$X" go test`,
		`nice --frobnicate "$X" go test`,
		`nice -Z "$X" go test`,
		`sudo --frobnicate "$X" go test`,
		`sudo -Z "$X" go test`,
		`env --frobnicate "$X" go test`,
		`env -Z "$X" go test`,
		`stdbuf --frobnicate "$X" go test`,
		`stdbuf -Z "$X" go test`,
	} {
		run(t, clean, []decideCase{{"clean: " + l, sh(l), git}})
	}
	// The same wrappers with options the gate knows.
	for _, l := range []string{
		`timeout -v --foreground "$T" go test`,
		`timeout --preserve-status -k 1 "$T" go test`,
		`taskset -c "$M" go test`,
		`flock -n "$F" go test`,
	} {
		run(t, clean, []decideCase{{"clean: " + l, sh(l), ""}})
	}
}
