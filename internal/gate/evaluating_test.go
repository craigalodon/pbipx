package gate_test

import (
	"testing"

	"github.com/craigalodon/pbipx/internal/gate"
)

// TestShellEvaluatingBuiltins: builtins that evaluate a string later.
func TestShellEvaluatingBuiltins(t *testing.T) {
	root, _ := historyRepo(t)
	sh := func(cmd string) gate.Event { return gate.Event{Tool: gate.Shell, Command: cmd} }
	git := gate.ReasonGitContent
	run(t, root, []decideCase{
		{"mapfile -C", sh(`mapfile -C 'git log -p >&2' -c 1 < README.md`), git},
		{"readarray --callback", sh(`readarray -t --callback 'git log -p >&2' a < README.md`), git},
		{"readarray -C clustered", sh(`readarray -tC 'git log -p >&2' a < README.md`), git},
		{"declare -i, then a quoted subscript", sh(`declare -i x; x='a[$(git log -p >&2)]'`), git},
		{"local -i", sh(`f() { local -i x='a[$(git log -p >&2)]'; }; f`), git},
		{"[[ -eq with a quoted subscript", sh(`x='a[$(git log -p >&2)]'; [[ 1 -eq x ]]`), git},
		{"[[ -lt naming a variable set to code", sh("x='a[`git log -p >&2`]'; [[ $n -lt x ]]"), git},
		{"mapfile alone", sh(`mapfile -t a < README.md`), ""},
		{"declare -i alone", sh(`declare -i n=1; echo $n`), ""},
		{"[[ -eq of literals", sh(`[[ 1 -eq 1 ]] && echo y`), ""},
	})
}

// TestShellArithmeticContexts: every arithmetic context evaluates a
// variable whose value holds code; with quoted code in the line, it is
// code built at run time.
func TestShellArithmeticContexts(t *testing.T) {
	root, _ := historyRepo(t)
	sh := func(cmd string) gate.Event { return gate.Event{Tool: gate.Shell, Command: cmd} }
	git := gate.ReasonGitContent
	set := `x='a[$(git log -p >&2)]'; `
	run(t, root, []decideCase{
		{"a subscript in an expansion", sh(set + `echo ${y[x]}`), git},
		{"a subscript in a named array", sh(set + `echo ${arr[x]}`), git},
		{"a subscript with an expansion", sh(set + `echo ${b[$x]}`), git},
		{"a substring offset", sh(set + `echo ${PWD:x:1}`), git},
		{"a positional offset", sh(set + `echo ${@:x}`), git},
		{"$(( in a case word", sh(set + `case 1 in $((x))) ;; esac`), git},
		{"an assignment's subscript", sh(set + `b[x]=1`), git},
		{"a compound assignment's subscript", sh(set + `a=([x]=1)`), git},
		{"another assignment's subscript", sh(set + `m[x]=1`), git},
		{"a literal subscript", sh(`a[0]=1; echo ${a[0]} ${PWD:1:2}`), ""},
		{"quoted code with no arithmetic", sh(`git commit -m 'use $(foo)'`), ""},
	})
}
