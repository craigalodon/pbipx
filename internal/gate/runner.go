package gate

import (
	"slices"
	"strings"
)

// Runners that run a command on words they read from their input: xargs,
// and GNU parallel. The words are file names or anything else, so for the
// command they run they are words the gate cannot tell: the replacement
// string of xargs (-I R, -i, --replace), parallel's placeholders ({},
// {.}, {/}, {//}, {/.}, {#}, {%}, {n}), and, with no placeholder, the
// words appended to the command. find -exec's {} is not one: find's own
// walk names the files, which the path find searches covers.

// runnerFeed says how a runner in a command's words feeds the command it
// runs. The zero value is no runner.
type runnerFeed struct {
	// runner is "xargs" or "parallel", "" when the command is under
	// neither.
	runner string
	// fromInput is true when the words come from standard input or a file
	// the gate does not read (parallel with no ::: source of its own).
	fromInput bool
	// placeholders are the texts in the command that stand for an input
	// word, wherever they appear in what the runner runs.
	placeholders []string
	// appends is true when the input words are added after the command:
	// from input, with no placeholder.
	appends bool
}

// parallelPlaceholders are the texts GNU parallel replaces with an input
// word: {} and its variants, {#} and {%} (job and slot numbers, not
// names), the positional {1}...{9} with the same variants, and {= perl
// expression =}.
var parallelPlaceholders = func() []string {
	out := []string{"{}", "{.}", "{/}", "{//}", "{/.}", "{#}", "{%}", "{="}
	for _, d := range "123456789" {
		n := string(d)
		out = append(out, "{"+n+"}", "{"+n+".}", "{"+n+"/}", "{"+n+"//}", "{"+n+"/.}")
	}
	return out
}()

// feedOf reads argv, a command's words, for a runner (xargs or parallel,
// the first of them) and how it feeds what it runs.
func feedOf(argv []string) runnerFeed {
	for i, a := range argv {
		switch progName(a) {
		case "xargs":
			return xargsFeed(argv[i+1:])
		case "parallel":
			return parallelFeed(argv[i+1:])
		}
	}
	return runnerFeed{}
}

// xargsFeed reads xargs's options, in args (the words after xargs): its
// replacement string -I R, -IR, -i[R] (default {}), --replace[=R]; with
// one, the input replaces the string in the command, with none it is
// appended.
func xargsFeed(args []string) runnerFeed {
	f := runnerFeed{runner: "xargs", fromInput: true}
	withValue := wrappers["xargs"]
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" || !strings.HasPrefix(a, "-") || a == "-" {
			break
		}
		if strings.HasPrefix(a, "--") {
			name, value, hasValue := strings.Cut(a[2:], "=")
			switch {
			case name == "replace" || name == "replace-str":
				if !hasValue || value == "" {
					value = "{}"
				}
				f.placeholders = append(f.placeholders, value)
			case slices.Contains(withValue.long, name) && !hasValue:
				i++
			}
			continue
		}
		// A cluster of short options, to the first that takes a value.
		for j := 1; j < len(a); j++ {
			c := a[j]
			if c == 'i' {
				rest := a[j+1:]
				if rest == "" {
					rest = "{}"
				}
				f.placeholders = append(f.placeholders, rest)
				break
			}
			if strings.IndexByte(withValue.short, c) >= 0 {
				value := a[j+1:]
				if value == "" && i+1 < len(args) {
					i++
					value = args[i]
				}
				if c == 'I' {
					f.placeholders = append(f.placeholders, value)
				}
				break
			}
		}
	}
	f.appends = len(f.placeholders) == 0
	return f
}

// parallelFeed reads GNU parallel's words after parallel: with ::: (literal
// arguments) and no :::: or -a, the arguments are its own and nothing
// comes from input; otherwise every input line is a word. -I R / --replace
// R changes {} to R. Placeholders are the defaults and R; with none in the
// command, the input is appended.
func parallelFeed(args []string) runnerFeed {
	f := runnerFeed{runner: "parallel", fromInput: true}
	template := parallelTemplate(args)
	literal := false
	for _, a := range args {
		switch {
		case a == "::::" || a == "-a" || a == "--arg-file" || a == "--pipe" || a == "--pipepart" ||
			strings.HasPrefix(a, "--arg-file="):
			f.fromInput, literal = true, false
			goto parsed
		case strings.HasPrefix(a, ":::"):
			literal = true
		}
	}
parsed:
	if literal {
		f.fromInput = false
	}
	for i, a := range args {
		if (a == "-I" || a == "--replace" || a == "--replace-str") && i+1 < len(args) {
			f.placeholders = append(f.placeholders, args[i+1])
		} else if strings.HasPrefix(a, "-I") && len(a) > 2 || strings.HasPrefix(a, "--replace=") {
			f.placeholders = append(f.placeholders, strings.TrimPrefix(strings.TrimPrefix(a, "--replace="), "-I"))
		}
	}
	if !f.fromInput {
		f.placeholders = nil
		return f
	}
	f.placeholders = append(f.placeholders, parallelPlaceholders...)
	f.appends = !slices.ContainsFunc(template, func(w string) bool { return containsAny(w, f.placeholders) })
	return f
}

// containsAny reports whether s holds one of subs.
func containsAny(s string, subs []string) bool {
	return slices.ContainsFunc(subs, func(p string) bool { return p != "" && strings.Contains(s, p) })
}

// carriesLines are the commands whose words are command lines read in
// turn, where a runner's placeholder is judged as the line is read.
func carriesLines(name string) bool {
	switch name {
	case "eval", "watch", "trap", "parallel", "find", "fd", "fdfind", "tmux", "at", "batch", "script", "flock":
		return true
	}
	return false
}

// markFed marks the words of v after its program that hold a placeholder
// (phs) as words the gate cannot tell, standing for input words (fed): a
// reader or search given one touches every root. A shell's command
// string is left to the line it holds, which is read with the same
// placeholders. known is the expanded words' known flags, shared with
// v.argvKnown and v.restKnown; first is the index in them of the first
// word of v.argv.
func markFed(v *view, known []bool, first int, phs []string) {
	if carriesLines(v.name) {
		return
	}
	skip := -1
	if shells[v.name] {
		skip = shellString(v.rest)
	}
	k, at := 0, 0
	for wi := range v.words {
		if v.words[wi].redirect {
			continue
		}
		pos := k - first - (len(v.argv) - len(v.rest))
		k++
		at = wi
		if pos < 0 || pos == skip || !containsAny(v.words[at].text, phs) {
			continue
		}
		v.words[at].known, v.words[at].fed = false, true
		known[k-1] = false
	}
}

// isInterpreter reports whether name is a script interpreter the gate
// knows (interpreters): one that reads the files it is given as programs
// or data.
func isInterpreter(name string) bool {
	_, ok := interpreters[interpreterFamily(name)]
	return ok
}
