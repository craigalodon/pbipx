package gate_test

import (
	"testing"

	"github.com/craigalodon/pbipx/internal/gate"
)

// TestShellPositionalArithmetic: a positional parameter or OPTARG that
// arithmetic reads is a variable's value too: the arguments of set, of a
// function, of getopts, of any command in a line whose arithmetic names
// one are places quoted code can become a value.
func TestShellPositionalArithmetic(t *testing.T) {
	root, _ := historyRepo(t)
	sh := func(cmd string) gate.Event { return gate.Event{Tool: gate.Shell, Command: cmd} }
	git := gate.ReasonGitContent
	code := `'a[$(git log -p >&2)]'`
	run(t, root, []decideCase{
		{"set --, then $(( $1 ))", sh("set -- " + code + "; echo $(( $1 ))"), git},
		{"a function call, then $(( $1 ))", sh("f(){ echo $(( $1 )); }; f " + code), git},
		{"set --, then (( $1 ))", sh("set -- " + code + "; (( $1 ))"), git},
		{"set --, then [[ $1 -eq 1 ]]", sh("set -- " + code + "; [[ $1 -eq 1 ]]"), git},
		{"getopts, then $((OPTARG))", sh("getopts 'a:' o -a " + code + "; echo $((OPTARG))"), git},
		{"getopts, then (( OPTARG ))", sh("getopts 'a:' o -a " + code + "; (( OPTARG ))"), git},
		{"set --, then ${1} in a subscript", sh("set -- " + code + "; echo ${arr[$1]}"), git},
		{"set --, then ${@} in a substring", sh("set -- " + code + "; echo ${s:${1}}"), git},
		{"a variable from $1, then arithmetic", sh("set -- " + code + "; n=$1; echo $((n))"), git},
		{"a function with a local from $1", sh("f(){ local n=$1; echo $((n)); }; f " + code), git},
		// No positional parameter is read by arithmetic.
		{"set --, then an echo", sh("set -- " + code + "; echo \"$1\""), ""},
		{"a function that echoes", sh("f(){ echo \"$1\"; }; f " + code), ""},
		{"getopts, then a plain case", sh("getopts 'a:' o -a x; echo \"$OPTARG\""), ""},
		{"arithmetic on a literal, a message nearby", sh("echo $((1+1)); git commit -qm 'Use `make`'"), ""},
	})
}
