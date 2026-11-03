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

// TestShellQuotedValueCode: quoted code counts only where it can become a
// variable's value (an assignment, a value read or declared, a for list,
// a ${x:=...} default, a command whose output can be captured or piped),
// so arithmetic beside a git message, a sed script or an echo is allowed,
// while every way a value can carry the code still denies.
func TestShellQuotedValueCode(t *testing.T) {
	root, _ := historyRepo(t)
	sh := func(cmd string) gate.Event { return gate.Event{Tool: gate.Shell, Command: cmd} }
	git := gate.ReasonGitContent
	code := `'a[$(git log -p >&2)]'`
	run(t, root, []decideCase{
		// Allowed: the quoted code is no variable's value.
		{"a for loop, then a commit message with backticks",
			sh("for ((i=0; i<3; i++)); do echo \"$i\"; done && git commit -qm 'Use `make fmt`'"), ""},
		{"an array loop, then a commit message",
			sh("arr=(a b); for i in 0 1; do echo \"${arr[$i]}\"; done; git commit -qm 'Fix `x`'"), ""},
		{"arithmetic, then an echo of ${...}", sh(`total=$((count + 1)); echo 'Done: ${total}'`), ""},
		{"a sed script, then a subscript", sh(`sed -i 's/${VERSION}/1.0/' f; echo ${arr[n]}`), ""},
		// Denied: the code becomes a value an arithmetic context reads.
		{"for (( with an assigned value", sh("v=" + code + "; for ((x=v;x<1;x++)); do :; done"), git},
		{"a subscript with an assigned value", sh("v=" + code + "; echo ${arr[v]}"), git},
		{"a substring with an assigned value", sh("v=" + code + "; echo ${s:v}"), git},
		{"an element assignment with an assigned value", sh("v=" + code + "; b[v]=1"), git},
		{"an array element value", sh("a[0]=" + code + "; echo $((a[0]))"), git},
		{"+= of the code", sh("v=; v+=" + code + "; echo $((v))"), git},
		{"declare of the code", sh("declare v=" + code + "; echo $((v))"), git},
		{"export of the code", sh("export v=" + code + "; echo $((v))"), git},
		{"printf -v of the code", sh("printf -v v '%s' " + code + "; echo $((v))"), git},
		{"read from a here-string", sh("read -r v <<< " + code + "; echo $((v))"), git},
		{"mapfile from a here-document",
			sh("mapfile -t a <<'E'\na[$(git log -p >&2)]\nE\necho $((a[0]))"), git},
		{"let of the code", sh("let v=" + code + " 2>/dev/null; echo $((v))"), git},
		{"a for list", sh("for v in " + code + "; do echo $((v)); done"), git},
		{"a ${x:=...} default", sh("echo ${v:=" + code + "}; echo $((v))"), git},
		{"echo captured by a substitution", sh("v=$(echo " + code + "); echo $((v))"), git},
		{"echo piped into read", sh("echo " + code + " | { read -r v; echo $((v)); }"), git},
		{"echo into a process substitution read by mapfile",
			sh("mapfile -t a < <(echo " + code + "); echo $((a[0]))"), git},
		{"echo to a file read back", sh("echo " + code + " > f; read -r v < f; echo $((v))"), git},
		{"a compound array", sh("a=(" + code + "); echo $((a[0]))"), git},
	})
}
