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
}
